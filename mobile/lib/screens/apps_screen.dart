import 'dart:async';
import 'dart:convert';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:url_launcher/url_launcher.dart';

import '../models/container_entry.dart';
import '../services/api_client.dart';
import '../ui/ui.dart';
import '../utils/app_icons.dart';
import 'app_store_screen.dart';
import 'container_logs_screen.dart';
import 'custom_install_screen.dart';
import 'terminal_screen.dart';

// ---------------------------------------------------------------------------
// Data: what the Apps tab loads and the calls it makes
// ---------------------------------------------------------------------------

/// The installed-apps API in one place, so the screens stay about layout
/// and the routes can be tested against a fake server.
abstract final class AppsApi {
  /// Everything the Apps tab shows. The app grid is required; containers,
  /// links, overrides and the compose update list only add detail, so
  /// their failures are ignored.
  static Future<List<InstalledApp>> load() async {
    final gridRes = await ApiClient.instance.get('/v2/app_management/web/appgrid');
    final results = await Future.wait([
      _optional(() => ApiClient.instance.get('/v1/container/all')),
      _optional(() => ApiClient.instance.get('/v1/users/current/custom/link')),
      _optional(() => ApiClient.instance.get('/v1/users/current/custom/legacy_app_overrides')),
      _optional(() => ApiClient.instance.get('/v2/app_management/apps/upgradable')),
    ]);
    List<dynamic> list(Object? v) => v is List ? v : const [];
    final links = _decoded(results[1]?['data']);
    final overrides = _decoded(results[2]?['data']);
    final upgradable = <String>{
      for (final u in list(results[3]?['data']).whereType<Map>())
        if (u['store_app_id'] != null) u['store_app_id'].toString(),
    };
    final apps = buildInstalledApps(
      grid: list(gridRes['data']),
      containers: list(results[0]?['data']),
      links: list(links),
      overrides: overrides is Map<String, dynamic> ? overrides : const {},
    );
    if (upgradable.isEmpty) return apps;
    return [
      for (final a in apps)
        a.kind == AppKind.compose && (upgradable.contains(a.storeAppId) || upgradable.contains(a.id)) ? a.copyWith(hasUpdate: true) : a,
    ];
  }

  static Future<Map<String, dynamic>?> _optional(Future<Map<String, dynamic>> Function() call) async {
    try {
      return await call();
    } catch (_) {
      return null;
    }
  }

  // The dashboard stores its custom settings as JSON text inside `data`.
  static Object? _decoded(Object? data) {
    if (data is String && data.isNotEmpty) {
      try {
        return jsonDecode(data);
      } catch (_) {
        return null;
      }
    }
    return data;
  }

  /// Starts, stops or restarts [app] (plan M-08: compose apps use
  /// `PUT …/compose/{id}/status` with a bare string body), then waits until
  /// the server reports the new state, or [timeout]. Returns the state it
  /// saw last.
  static Future<String> setStatus(
    InstalledApp app,
    AppAction action, {
    Duration timeout = const Duration(seconds: 30),
    Duration interval = const Duration(seconds: 1),
  }) async {
    final req = app.statusRequest(action);
    final res = await ApiClient.instance.put(req.path, body: req.body);
    final want = action == AppAction.stop ? AppRunState.stopped : AppRunState.running;
    // The v1 container route is synchronous and answers with the state.
    final immediate = res['data'];
    if (app.kind != AppKind.compose && immediate is String && immediate.isNotEmpty) return immediate;
    // Compose apps change asynchronously: poll until they get there.
    final deadline = clock.now().add(timeout);
    var last = app.status;
    while (clock.now().isBefore(deadline)) {
      await Future<void>.delayed(interval);
      final s = await status(app);
      if (s != null) {
        last = s;
        if (app.copyWith(status: s).runState == want) break;
      }
    }
    return last;
  }

  /// The app's current raw state, or null when it can't be read.
  static Future<String?> status(InstalledApp app) async {
    try {
      if (app.kind == AppKind.compose) {
        final res = await ApiClient.instance.get('/v2/app_management/compose/${Uri.encodeComponent(app.id)}');
        final data = res['data'];
        return data is Map ? data['status']?.toString() : null;
      }
      final res = await ApiClient.instance.get('/v2/app_management/web/appgrid');
      final data = res['data'];
      if (data is! List) return null;
      for (final item in data.whereType<Map>()) {
        if (item['name']?.toString() == app.id) return item['status']?.toString();
      }
    } catch (_) {}
    return null;
  }

  /// A compose app's store facts: its notes ("tips": default passwords,
  /// first steps) and its store category ("Media"). Both null when the
  /// store doesn't say.
  static Future<({String? tips, String? category})> storeInfo(InstalledApp app) async {
    if (app.kind != AppKind.compose) return (tips: null, category: null);
    try {
      final res = await ApiClient.instance.get('/v2/app_management/compose/${Uri.encodeComponent(app.id)}');
      final info = (res['data'] as Map?)?['store_info'];
      if (info is! Map) return (tips: null, category: null);
      final tips = info['tips'];
      final text = tips is Map ? (pickLocale(tips['custom']) ?? pickLocale(tips['before_install'])) : null;
      final category = info['category']?.toString().trim();
      return (
        tips: text == null || text.trim().isEmpty ? null : text.trim(),
        category: category == null || category.isEmpty ? null : category,
      );
    } catch (_) {
      return (tips: null, category: null);
    }
  }

  /// The container a compose app's shell should open in: its main
  /// service's container, else the first one.
  static Future<String?> mainContainer(InstalledApp app) async {
    if (app.kind != AppKind.compose) return app.containerId ?? app.id;
    final res = await ApiClient.instance.get('/v2/app_management/compose/${Uri.encodeComponent(app.id)}/containers');
    final data = res['data'];
    if (data is! Map) return null;
    final main = data['main']?.toString();
    final containers = data['containers'];
    if (containers is! Map || containers.isEmpty) return null;
    for (final e in containers.entries) {
      final c = e.value;
      if (c is Map && main != null && (c['Service'] == main || c['service'] == main)) return (c['ID'] ?? c['Name'] ?? e.key).toString();
    }
    final first = containers.entries.first;
    final c = first.value;
    return c is Map ? (c['ID'] ?? c['Name'] ?? first.key).toString() : first.key.toString();
  }

  /// Starts updating [app]: a compose app to the store's version
  /// (`PATCH /compose/{id}`), a container by pulling its image and
  /// recreating it (`POST /v1/container/{id}/update`).
  static Future<void> startUpdate(InstalledApp app) async {
    if (app.kind == AppKind.compose) {
      await ApiClient.instance.patch('/v2/app_management/compose/${Uri.encodeComponent(app.id)}');
    } else {
      await ApiClient.instance.post('/v1/container/${Uri.encodeComponent(app.id)}/update');
    }
  }

  /// A container update's job state: "running", "done" or "failed" (with
  /// its error), "none" when the server has no job for it (404), or null
  /// when the check itself failed and is worth repeating.
  static Future<({String state, String? error})?> updateStatus(InstalledApp app) async {
    try {
      final res = await ApiClient.instance.get('/v1/container/${Uri.encodeComponent(app.id)}/update/status');
      final data = res['data'];
      if (data is! Map) return (state: 'none', error: null);
      return (state: data['state']?.toString() ?? 'running', error: data['error']?.toString());
    } on ApiException catch (e) {
      return e.statusCode == 404 ? (state: 'none', error: null) : null;
    } catch (_) {
      return null;
    }
  }

  /// Removes [app], and its data folder with [deleteData].
  static Future<void> uninstall(InstalledApp app, {required bool deleteData}) async {
    if (app.kind == AppKind.compose) {
      await ApiClient.instance.delete('/v2/app_management/compose/${Uri.encodeComponent(app.id)}', query: {'delete_config_folder': '$deleteData'});
    } else {
      await ApiClient.instance.deleteWithBody('/v1/container/${Uri.encodeComponent(app.id)}', {'delete_config_folder': deleteData});
    }
  }
}

/// A short plain-words label for [app]'s state, for chips and TalkBack.
String appStateLabel(InstalledApp app) => switch (app.runState) {
      AppRunState.running => app.isLink ? 'Link' : 'Running',
      AppRunState.stopped => 'Stopped',
      AppRunState.starting => 'Starting',
      AppRunState.stopping => 'Stopping',
      AppRunState.restarting => 'Restarting',
      AppRunState.paused => 'Paused',
      AppRunState.unknown => app.status.isEmpty ? 'Unknown' : '${app.status[0].toUpperCase()}${app.status.substring(1)}',
    };

Status _stateStatus(AppRunState s) => switch (s) {
      AppRunState.running => Status.success,
      AppRunState.stopped => Status.neutral,
      AppRunState.paused || AppRunState.unknown => Status.warning,
      _ => Status.info,
    };

IconData _stateIcon(AppRunState s) => switch (s) {
      AppRunState.running => Icons.check_circle_outline,
      AppRunState.stopped => Icons.stop_circle_outlined,
      AppRunState.paused => Icons.pause_circle_outline,
      AppRunState.unknown => Icons.help_outline,
      _ => Icons.hourglass_empty_outlined,
    };

/// The state chip for an app, or for an action in progress on it.
class AppStateChip extends StatelessWidget {
  const AppStateChip({super.key, required this.app, this.pending});

  final InstalledApp app;

  /// The action running right now, which the chip shows instead.
  final AppAction? pending;

  @override
  Widget build(BuildContext context) {
    if (app.isLink) return const StatusChip(label: 'Link', status: Status.neutral, icon: Icons.link_outlined);
    final p = pending;
    if (p != null) {
      final label = switch (p) { AppAction.start => 'Starting', AppAction.stop => 'Stopping', AppAction.restart => 'Restarting' };
      return StatusChip(label: label, status: Status.info, icon: Icons.hourglass_empty_outlined);
    }
    final s = app.runState;
    return StatusChip(label: appStateLabel(app), status: _stateStatus(s), icon: _stateIcon(s));
  }
}

/// Opens [url] in a Custom Tab; says so when it can't.
Future<void> openAppUrl(BuildContext context, Uri url) async {
  final messenger = ScaffoldMessenger.maybeOf(context);
  var ok = false;
  try {
    ok = await launchUrl(url, mode: LaunchMode.inAppBrowserView);
    if (!ok) ok = await launchUrl(url, mode: LaunchMode.externalApplication);
  } catch (_) {}
  if (!ok) messenger?.showSnackBar(SnackBar(content: Text("Couldn't open $url")));
}

/// Opens [app]'s web interface, after a word of warning when the address
/// only works on the server's own network (plan M-13).
Future<void> openApp(BuildContext context, InstalledApp app) async {
  final address = app.address(ApiClient.instance.baseUrl);
  if (address == null) return;
  if (address.lanOnly) {
    final go = await ConfirmDialog.confirm(
      context,
      title: 'Only on your network',
      message: '${app.title} is on port ${address.url.port} of the server. You reach the server over the internet, '
          'where that port is usually closed. It opens on your home network or over Tailscale.',
      confirmLabel: 'Open anyway',
    );
    if (!go || !context.mounted) return;
  }
  await openAppUrl(context, address.url);
}

// ---------------------------------------------------------------------------
// The Apps tab
// ---------------------------------------------------------------------------

enum _Filter { all, running, stopped, updates }

/// The Apps tab: installed apps with their state, search and a state
/// filter; tap for the app's page, long-press for quick actions. The app
/// store and custom install are in the top bar.
class AppsScreen extends StatefulWidget {
  // The Files/VMs/Settings callbacks date from when this tab also listed
  // the built-in "system apps"; those now live in the navigation and More,
  // so the callbacks are unused but kept for HomeShell.
  final VoidCallback? onOpenFiles;
  final VoidCallback? onOpenVms;
  final VoidCallback? onOpenSettings;

  const AppsScreen({super.key, this.onOpenFiles, this.onOpenVms, this.onOpenSettings});

  /// Forgets the list kept between visits (tests, sign-out).
  @visibleForTesting
  static void clearCache() => _AppsScreenState._clearCache();

  @override
  State<AppsScreen> createState() => _AppsScreenState();
}

class _AppsScreenState extends State<AppsScreen> {
  // The last list, kept across visits so the tab opens instantly and can
  // stay useful offline.
  static List<InstalledApp>? _cache;
  static DateTime? _cachedAt;
  static String? _cacheServer;

  static void _clearCache() {
    _cache = null;
    _cachedAt = null;
    _cacheServer = null;
  }

  List<InstalledApp>? _apps;
  DateTime? _loadedAt;
  Object? _error;
  bool _loading = false;
  _Filter _filter = _Filter.all;
  final Map<String, AppAction> _pending = {};
  final _search = TextEditingController();
  bool _updatingAll = false;

  @override
  void initState() {
    super.initState();
    if (_cacheServer == ApiClient.instance.baseUrl) {
      _apps = _cache;
      _loadedAt = _cachedAt;
    }
    _search.addListener(() => setState(() {}));
    _load();
  }

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    if (_loading) return;
    setState(() => _loading = true);
    try {
      final apps = await AppsApi.load();
      if (!mounted) return;
      setState(() {
        _apps = apps;
        _loadedAt = clock.now();
        _error = null;
      });
      _cache = apps;
      _cachedAt = _loadedAt;
      _cacheServer = ApiClient.instance.baseUrl;
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = e);
      // With a list on screen, a server error (not being offline, which
      // the banner covers) is said once instead of hiding the list.
      final offline = e is ApiException && e.isUnreachable;
      if (_apps != null && !offline && !ApiClient.isAuthError(e)) {
        ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text("Couldn't refresh apps. ${_reason(e)}")));
      }
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _openStore() {
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => const AppStoreScreen())).then((_) => _load());
  }

  void _openCustomInstall() {
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => const CustomInstallScreen())).then((_) => _load());
  }

  void _openDetail(InstalledApp app) {
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => AppDetailScreen(app: app))).then((_) => _load());
  }

  Future<void> _run(InstalledApp app, AppAction action) async {
    if (_pending.containsKey(app.id)) return;
    setState(() => _pending[app.id] = action);
    final messenger = ScaffoldMessenger.of(context);
    try {
      final state = await AppsApi.setStatus(app, action);
      _replace(app.copyWith(status: state));
    } catch (e) {
      messenger.showSnackBar(SnackBar(content: Text("Couldn't ${action.name} ${app.title}. ${_reason(e)}")));
    } finally {
      if (mounted) setState(() => _pending.remove(app.id));
    }
    _load();
  }

  /// Starts the update of every app that has one (plan WP1-9 "Update
  /// all"). Each update runs on the server; the list refreshes after.
  Future<void> _updateAll(List<InstalledApp> apps) async {
    final todo = apps.where((a) => a.hasUpdate).toList();
    if (todo.isEmpty || _updatingAll) return;
    setState(() => _updatingAll = true);
    final messenger = ScaffoldMessenger.of(context);
    final failed = <String>[];
    for (final a in todo) {
      try {
        await AppsApi.startUpdate(a);
      } catch (_) {
        failed.add(a.title);
      }
    }
    if (!mounted) return;
    setState(() => _updatingAll = false);
    final started = todo.length - failed.length;
    messenger.showSnackBar(SnackBar(
      content: Text(failed.isEmpty
          ? (started == 1 ? 'Updating 1 app. It restarts when the new version is ready.' : 'Updating $started apps. Each restarts when its new version is ready.')
          : "Couldn't start the update of ${failed.join(', ')}."),
    ));
    _load();
  }

  void _replace(InstalledApp app) {
    final apps = _apps;
    if (apps == null || !mounted) return;
    setState(() => _apps = [for (final a in apps) a.id == app.id && a.kind == app.kind ? app : a]);
  }

  void _showActions(InstalledApp app) {
    HapticFeedback.mediumImpact();
    showAppActionsSheet(
      context,
      app: app,
      onAction: (action) => _run(app, action),
      onDetails: () => _openDetail(app),
    );
  }

  List<InstalledApp> _visible(List<InstalledApp> apps) {
    final q = _search.text.trim().toLowerCase();
    return apps.where((a) {
      final keep = switch (_filter) {
        _Filter.all => true,
        _Filter.running => a.isRunning && !a.isLink,
        _Filter.stopped => a.runState == AppRunState.stopped,
        _Filter.updates => a.hasUpdate,
      };
      if (!keep) return false;
      if (q.isEmpty) return true;
      return a.title.toLowerCase().contains(q) || a.id.toLowerCase().contains(q) || a.image.toLowerCase().contains(q);
    }).toList();
  }

  @override
  Widget build(BuildContext context) {
    final apps = _apps;
    final error = _error;
    final offline = error is ApiException && error.isUnreachable;
    final auth = error != null && ApiClient.isAuthError(error);

    final actions = [
      IconButton(tooltip: 'App store', icon: const Icon(Icons.storefront_outlined), onPressed: _openStore),
      PopupMenuButton<String>(
        tooltip: 'More options',
        onSelected: (v) => v == 'custom' ? _openCustomInstall() : _load(),
        itemBuilder: (_) => const [
          PopupMenuItem(value: 'custom', child: Text('Install from compose file')),
          PopupMenuItem(value: 'refresh', child: Text('Refresh')),
        ],
      ),
    ];

    final List<Widget> slivers;
    if (apps == null) {
      if (error == null) {
        slivers = [const _SliverAppRowsSkeleton()];
      } else if (auth) {
        slivers = [
          EmptyState(
            sliver: true,
            icon: Icons.lock_outline,
            title: 'Signed out',
            message: 'Your session on this server ended. Sign in again, then try once more.',
            actionLabel: 'Try again',
            onAction: _load,
          ),
        ];
      } else if (offline) {
        slivers = [ErrorState.offline(sliver: true, onRetry: _load, details: error.details)];
      } else {
        slivers = [
          ErrorState(
            sliver: true,
            title: "Couldn't load apps",
            message: _reason(error),
            onRetry: _load,
            details: error is ApiException ? error.details : error.toString(),
          ),
        ];
      }
    } else if (apps.isEmpty) {
      slivers = [
        EmptyState(
          sliver: true,
          icon: Icons.apps_outlined,
          title: 'No apps yet',
          message: 'Apps you install from the app store appear here.',
          actionLabel: 'Open app store',
          onAction: _openStore,
        ),
      ];
    } else {
      final visible = _visible(apps);
      slivers = [
        SliverToBoxAdapter(child: _controls(apps)),
        if (_filter == _Filter.updates && visible.isNotEmpty) SliverToBoxAdapter(child: _updateAllRow(visible)),
        if (visible.isEmpty)
          EmptyState(
            sliver: true,
            icon: Icons.search_off_outlined,
            title: _search.text.trim().isEmpty ? 'Nothing here' : 'No apps match “${_search.text.trim()}”',
            message: _search.text.trim().isEmpty ? 'No app is in this state right now.' : 'Try another name, or clear the search.',
            actionLabel: 'Show all apps',
            onAction: () => setState(() {
              _search.clear();
              _filter = _Filter.all;
            }),
          )
        else
          SliverList.builder(
            itemCount: visible.length,
            itemBuilder: (context, i) {
              final app = visible[i];
              return AppRow(
                app: app,
                pending: _pending[app.id],
                onTap: () => _openDetail(app),
                onLongPress: () => _showActions(app),
              );
            },
          ),
      ];
    }

    return AppScaffold.slivers(
      title: 'Apps',
      actions: actions,
      onRefresh: _load,
      banner: apps != null && offline ? OfflineBanner(lastUpdated: _loadedAt, onRetry: _load) : null,
      slivers: slivers,
    );
  }

  Widget _updateAllRow(List<InstalledApp> visible) {
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    final n = visible.where((a) => a.hasUpdate).length;
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, 0, gutter - Space.sm, 0),
      child: Row(children: [
        Expanded(
          child: Text(
            n == 1 ? 'A new version is ready for 1 app' : 'New versions are ready for $n apps',
            style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
        ),
        const SizedBox(width: Space.sm),
        _updatingAll
            ? const Padding(
                padding: EdgeInsets.all(Space.md),
                child: SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 3)),
              )
            : TextButton.icon(onPressed: () => _updateAll(visible), icon: const Icon(Icons.upgrade_outlined), label: const Text('Update all')),
      ]),
    );
  }

  Widget _controls(List<InstalledApp> apps) {
    final running = apps.where((a) => a.isRunning && !a.isLink).length;
    final stopped = apps.where((a) => a.runState == AppRunState.stopped).length;
    final updates = apps.where((a) => a.hasUpdate).length;
    final gutter = Space.gutter(context);
    Widget chip(_Filter f, String label) => ChoiceChip(
          label: Text(label),
          selected: _filter == f,
          onSelected: (_) => setState(() => _filter = f),
        );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.sm),
          child: SearchBar(
            controller: _search,
            hintText: 'Search apps',
            leading: const Icon(Icons.search),
            elevation: const WidgetStatePropertyAll(0),
            trailing: [
              if (_search.text.isNotEmpty)
                IconButton(tooltip: 'Clear search', icon: const Icon(Icons.close), onPressed: _search.clear),
            ],
          ),
        ),
        SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          padding: EdgeInsets.symmetric(horizontal: gutter),
          child: Row(
            children: [
              chip(_Filter.all, 'All ${apps.length}'),
              const SizedBox(width: Space.sm),
              chip(_Filter.running, 'Running $running'),
              const SizedBox(width: Space.sm),
              chip(_Filter.stopped, 'Stopped $stopped'),
              if (updates > 0) ...[
                const SizedBox(width: Space.sm),
                chip(_Filter.updates, updates == 1 ? '1 update' : '$updates updates'),
              ],
            ],
          ),
        ),
        const SizedBox(height: Space.sm),
      ],
    );
  }
}

String _reason(Object e) {
  final s = e is ApiException ? e.message : e.toString().replaceFirst('Exception: ', '');
  return s.endsWith('.') ? s : '$s.';
}

/// One installed app as a list row: icon, name, what it runs, and its state.
class AppRow extends StatelessWidget {
  const AppRow({super.key, required this.app, this.pending, this.onTap, this.onLongPress});

  final InstalledApp app;
  final AppAction? pending;
  final VoidCallback? onTap;
  final VoidCallback? onLongPress;

  static String supporting(InstalledApp app) {
    if (app.isLink) {
      final u = app.address(ApiClient.instance.baseUrl)?.url;
      return u == null ? 'Web link' : ApiClient.displayHost(u.toString());
    }
    return app.image.isNotEmpty ? app.image : 'Container';
  }

  /// Running is the normal state, so a running app gets no chip (a column
  /// of green "Running" chips drowns out the ones that matter) and says
  /// how long it has been up instead. Anything else - stopped, starting,
  /// an update - gets a chip, which always carries its word and icon.
  static bool showsChip(InstalledApp app, AppAction? pending) => pending != null || (!app.isRunning && !app.isLink);

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    // At large text sizes the chip moves under the name, where it has the
    // whole width, instead of squeezing the name into a narrow column.
    final large = MediaQuery.textScalerOf(context).scale(14) > 21;
    final hasChip = showsChip(app, pending);
    final chip = AppStateChip(app: app, pending: pending);
    final upText = app.isRunning && !app.isLink && app.statusText != null ? app.statusText! : supporting(app);
    final sub = Text(upText, maxLines: 1, overflow: TextOverflow.ellipsis);
    final below = [
      if (large && hasChip) chip,
      if (app.hasUpdate) const StatusChip(label: 'Update', status: Status.info, icon: Icons.upgrade_outlined),
    ];
    return Semantics(
      // The state is always spoken, even where it isn't drawn as a chip.
      value: hasChip ? null : appStateLabel(app),
      child: MergeSemantics(
        child: ListTile(
          onTap: onTap,
          onLongPress: onLongPress,
          leading: NivaroAppIcon(iconUrl: app.icon, name: app.title, size: 40, radius: DesignTokens.of(context).radii.md, customRadiusPercent: app.iconRadius),
          title: Text(app.title, maxLines: 1, overflow: TextOverflow.ellipsis),
          subtitle: below.isEmpty
              ? sub
              : Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    sub,
                    const SizedBox(height: Space.xs),
                    Wrap(spacing: Space.sm, runSpacing: Space.xs, children: below),
                  ],
                ),
          isThreeLine: below.isNotEmpty,
          trailing: large || !hasChip ? null : chip,
          titleTextStyle: theme.textTheme.bodyLarge?.copyWith(color: theme.colorScheme.onSurface),
        ),
      ),
    );
  }
}

/// Skeleton rows shaped like [AppRow]: a 40dp rounded icon, two lines and
/// a chip.
class _SliverAppRowsSkeleton extends StatelessWidget {
  const _SliverAppRowsSkeleton();

  static const _widths = [0.42, 0.3, 0.5, 0.36, 0.46, 0.28, 0.4, 0.34];

  @override
  Widget build(BuildContext context) {
    final scaler = MediaQuery.textScalerOf(context);
    final gutter = Space.gutter(context);
    return SliverSemantics(
      label: 'Loading',
      liveRegion: true,
      sliver: SkeletonPulse.sliver(
        child: SliverList.builder(
          itemCount: 8,
          itemBuilder: (context, i) => ExcludeSemantics(
            child: Padding(
              padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.md),
              child: Row(children: [
                const SkeletonBox(width: 40, height: 40, corner: Corner.md),
                const SizedBox(width: Space.lg),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      FractionallySizedBox(widthFactor: _widths[i % _widths.length], child: SkeletonBox(height: scaler.scale(14))),
                      const SizedBox(height: Space.sm),
                      FractionallySizedBox(widthFactor: _widths[(i + 3) % _widths.length] + 0.2, child: SkeletonBox(height: scaler.scale(12))),
                    ],
                  ),
                ),
                const SizedBox(width: Space.lg),
                SkeletonBox(width: scaler.scale(72), height: scaler.scale(24), corner: Corner.sm),
              ]),
            ),
          ),
        ),
      ),
    );
  }
}

/// Quick actions for [app] (long-press on a row): open, start or stop,
/// restart, and the app's page.
Future<void> showAppActionsSheet(
  BuildContext context, {
  required InstalledApp app,
  required void Function(AppAction action) onAction,
  required VoidCallback onDetails,
}) {
  final address = app.address(ApiClient.instance.baseUrl);
  return showModalBottomSheet<void>(
    context: context,
    showDragHandle: true,
    useSafeArea: true,
    isScrollControlled: true,
    builder: (sheet) {
      void close(VoidCallback then) {
        Navigator.of(sheet).pop();
        then();
      }

      return SingleChildScrollView(
        padding: const EdgeInsets.only(bottom: Space.lg),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            ListTile(
              leading: NivaroAppIcon(iconUrl: app.icon, name: app.title, size: 40, radius: DesignTokens.of(context).radii.md, customRadiusPercent: app.iconRadius),
              title: Text(app.title, style: Theme.of(sheet).textTheme.titleMedium),
              subtitle: Text(AppRow.supporting(app), maxLines: 1, overflow: TextOverflow.ellipsis),
            ),
            const Divider(),
            if (address != null)
              ListTile(
                leading: const Icon(Icons.open_in_new),
                title: const Text('Open'),
                subtitle: Text(ApiClient.displayHost(address.url.toString())),
                onTap: () => close(() => openApp(context, app)),
              ),
            if (app.canControl && !app.isRunning)
              ListTile(
                leading: const Icon(Icons.play_arrow_outlined),
                title: const Text('Start'),
                onTap: () => close(() => onAction(AppAction.start)),
              ),
            if (app.canControl && app.isRunning) ...[
              ListTile(
                leading: const Icon(Icons.stop_outlined),
                title: const Text('Stop'),
                onTap: () => close(() => onAction(AppAction.stop)),
              ),
              ListTile(
                leading: const Icon(Icons.restart_alt),
                title: const Text('Restart'),
                onTap: () => close(() => onAction(AppAction.restart)),
              ),
            ],
            ListTile(
              leading: const Icon(Icons.info_outline),
              title: const Text('App info'),
              onTap: () => close(onDetails),
            ),
          ],
        ),
      );
    },
  );
}

// ---------------------------------------------------------------------------
// App info
// ---------------------------------------------------------------------------

/// One app's page: state and the main actions up top, then its address,
/// image, updates, logs, shell, notes from the store, and uninstall.
class AppDetailScreen extends StatefulWidget {
  const AppDetailScreen({super.key, required this.app});

  final InstalledApp app;

  @override
  State<AppDetailScreen> createState() => _AppDetailScreenState();
}

class _AppDetailScreenState extends State<AppDetailScreen> {
  late InstalledApp _app = widget.app;
  AppAction? _pending;
  String? _tips;
  String? _category;
  bool _updating = false;
  String? _updateError;
  Timer? _updatePoll;
  static const _updateDeadline = Duration(minutes: 15);

  @override
  void initState() {
    super.initState();
    AppsApi.storeInfo(_app).then((i) {
      if (!mounted || (i.tips == null && i.category == null)) return;
      setState(() {
        _tips = i.tips;
        _category = i.category;
      });
    });
  }

  @override
  void dispose() {
    _updatePoll?.cancel();
    super.dispose();
  }

  Future<void> _run(AppAction action) async {
    if (_pending != null) return;
    setState(() => _pending = action);
    final messenger = ScaffoldMessenger.of(context);
    try {
      final state = await AppsApi.setStatus(_app, action);
      if (mounted) setState(() => _app = _app.copyWith(status: state));
    } catch (e) {
      messenger.showSnackBar(SnackBar(content: Text("Couldn't ${action.name} ${_app.title}. ${_reason(e)}")));
    } finally {
      if (mounted) setState(() => _pending = null);
    }
  }

  Future<void> _stop() async {
    final ok = await ConfirmDialog.destructive(
      context,
      title: 'Stop “${_app.title}”?',
      message: 'It stops answering until you start it again.',
      confirmLabel: 'Stop',
      permanent: false,
    );
    if (ok) _run(AppAction.stop);
  }

  Future<void> _update() async {
    setState(() {
      _updating = true;
      _updateError = null;
    });
    final messenger = ScaffoldMessenger.of(context);
    try {
      await AppsApi.startUpdate(_app);
    } catch (e) {
      if (mounted) setState(() => _updating = false);
      messenger.showSnackBar(SnackBar(content: Text("Couldn't update ${_app.title}. ${_reason(e)}")));
      return;
    }
    if (_app.kind == AppKind.compose) {
      // Compose updates report through events; say it started and let the
      // list refresh show the result.
      messenger.showSnackBar(SnackBar(content: Text('Updating ${_app.title}. It restarts when the new version is ready.')));
      if (mounted) setState(() => _updating = false);
      return;
    }
    // One check at a time, and not forever: a job that never reports
    // back stops the spinner after [_updateDeadline].
    final deadline = clock.now().add(_updateDeadline);
    var busy = false;
    _updatePoll = Timer.periodic(const Duration(seconds: 2), (_) async {
      if (busy) return;
      busy = true;
      final ({String state, String? error})? s;
      try {
        s = await AppsApi.updateStatus(_app);
      } finally {
        busy = false;
      }
      if (!mounted) return;
      final timedOut = clock.now().isAfter(deadline);
      if (!timedOut && (s == null || s.state == 'running')) return;
      _updatePoll?.cancel();
      setState(() {
        _updating = false;
        if (s?.state == 'failed') {
          _updateError = s?.error ?? 'The update failed.';
        } else if (timedOut && (s == null || s.state == 'running')) {
          _updateError = 'The update is taking longer than usual. Check this app again in a few minutes.';
        } else if (s?.state == 'done') {
          _app = _app.copyWith(hasUpdate: false);
        }
      });
      if (s?.state == 'done') messenger.showSnackBar(SnackBar(content: Text('${_app.title} is up to date')));
      if (s?.state == 'none') messenger.showSnackBar(SnackBar(content: Text('The update of ${_app.title} has finished')));
    });
  }

  Future<void> _uninstall() async {
    final deleteData = await showDialog<bool>(
      context: context,
      builder: (_) => _UninstallDialog(title: _app.title),
    );
    if (deleteData == null || !mounted) return;
    final nav = Navigator.of(context);
    final messenger = ScaffoldMessenger.of(context);
    try {
      await AppsApi.uninstall(_app, deleteData: deleteData);
      messenger.showSnackBar(SnackBar(content: Text('Removing ${_app.title}')));
      nav.pop();
    } catch (e) {
      messenger.showSnackBar(SnackBar(content: Text("Couldn't remove ${_app.title}. ${_reason(e)}")));
    }
  }

  Future<void> _openTerminal() async {
    final messenger = ScaffoldMessenger.of(context);
    final nav = Navigator.of(context);
    String? container;
    try {
      container = await AppsApi.mainContainer(_app);
    } catch (_) {}
    if (container == null) {
      messenger.showSnackBar(SnackBar(content: Text("${_app.title} has no running container to open a shell in")));
      return;
    }
    nav.push(MaterialPageRoute(
      builder: (_) => TerminalScreen(title: _app.title, path: _app.terminalPath(container), subtitle: 'Shell in the app’s container'),
    ));
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final app = _app;
    final address = app.address(ApiClient.instance.baseUrl);
    final busy = _pending != null;
    final gutter = Space.gutter(context);

    // One primary action: Open while it runs (or for a link), Start while
    // it's stopped. Stop and Restart are secondary.
    final Widget? main = address != null && (app.isRunning || !app.canControl)
        ? FilledButton.icon(onPressed: () => openApp(context, app), icon: const Icon(Icons.open_in_new), label: const Text('Open'))
        : app.canControl && !app.isRunning
            ? FilledButton.icon(
                onPressed: busy ? null : () => _run(AppAction.start),
                icon: const Icon(Icons.play_arrow_outlined),
                label: const Text('Start'),
              )
            : null;
    final secondary = <Widget>[
      if (app.canControl && app.isRunning) ...[
        OutlinedButton.icon(onPressed: busy ? null : _stop, icon: const Icon(Icons.stop_outlined), label: const Text('Stop')),
        OutlinedButton.icon(
          onPressed: busy ? null : () => _run(AppAction.restart),
          icon: const Icon(Icons.restart_alt_outlined),
          label: const Text('Restart'),
        ),
      ],
    ];

    final version = appVersion(app.image);
    final kindLabel = [
      switch (app.kind) {
        AppKind.compose => _category ?? 'App',
        AppKind.legacy => 'App',
        AppKind.container => 'Docker container',
        AppKind.link => 'Web link',
      },
      if (version != null) 'Version $version',
    ].join(' · ');
    final large = MediaQuery.textScalerOf(context).scale(10) > 13;
    return AppScaffold(
      title: 'App info',
      body: ListView(
        padding: const EdgeInsets.only(bottom: Space.xl),
        children: [
          // The page leads with the app itself: its icon, name, state and
          // the one thing to do next, on a tonal panel.
          Padding(
            padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0),
            child: Card.filled(
              color: DesignTokens.of(context).cardColor,
              shape: DesignTokens.of(context).cardShape(),
              child: Padding(
                padding: const EdgeInsets.all(Space.lg),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        NivaroAppIcon(iconUrl: app.icon, name: app.title, size: 64, radius: DesignTokens.of(context).radii.lg, customRadiusPercent: app.iconRadius),
                        const SizedBox(width: Space.lg),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Semantics(header: true, child: Text(app.title, style: theme.textTheme.headlineSmall?.emphasized)),
                              const SizedBox(height: 2),
                              Text(kindLabel, style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
                              const SizedBox(height: Space.sm),
                              AnimatedSwitcher(
                                duration: Motion.of(context).short,
                                child: AppStateChip(key: ValueKey('${app.status}$_pending'), app: app, pending: _pending),
                              ),
                            ],
                          ),
                        ),
                      ],
                    ),
                    if (main != null || secondary.isNotEmpty) ...[
                      const SizedBox(height: Space.lg),
                      // The primary action on its own line, the others
                      // sharing the one below, so no label is squeezed.
                      ?main,
                      if (secondary.isNotEmpty) ...[
                        if (main != null) const SizedBox(height: Space.sm),
                        if (large)
                          for (final (i, b) in secondary.indexed) ...[if (i > 0) const SizedBox(height: Space.sm), b]
                        else
                          Row(children: [
                            for (final (i, b) in secondary.indexed) ...[if (i > 0) const SizedBox(width: Space.sm), Expanded(child: b)],
                          ]),
                      ],
                    ],
                    if (address != null && address.lanOnly) ...[
                      const SizedBox(height: Space.md),
                      Text(
                        'Only reachable on your home network or over Tailscale.',
                        style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
                      ),
                    ],
                  ],
                ),
              ),
            ),
          ),
          TileGroup(title: 'About', children: [
            ListTile(
              leading: const Icon(Icons.link_outlined),
              title: const Text('Web address'),
              subtitle: Text(address == null
                  ? (app.kind == AppKind.container ? 'Not set. Add one with Edit app in the web dashboard.' : 'This app has no web page')
                  : address.url.toString()),
              onTap: address == null ? null : () => openApp(context, app),
            ),
            if (!app.isLink) ...[
              ListTile(
                leading: const Icon(Icons.schedule_outlined),
                title: const Text('Status'),
                subtitle: Text(app.statusText ?? appStateLabel(app)),
              ),
              if (app.image.isNotEmpty)
                ListTile(
                  leading: const Icon(Icons.layers_outlined),
                  title: const Text('Image'),
                  subtitle: Text(app.image),
                  onLongPress: () {
                    HapticFeedback.mediumImpact();
                    Clipboard.setData(ClipboardData(text: app.image));
                    ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Image name copied')));
                  },
                ),
              ListTile(
                leading: const Icon(Icons.upgrade_outlined),
                title: const Text('Updates'),
                subtitle: Text(_updating
                    ? 'Updating…'
                    : _updateError ??
                        (app.hasUpdate
                            ? 'A new version is available'
                            : [
                                'Up to date',
                                if (app.autoUpdate != null) app.autoUpdate! ? 'automatic updates on' : 'automatic updates off',
                              ].join(' · '))),
                trailing: _updating
                    ? const SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 3))
                    : app.hasUpdate || _updateError != null
                        ? TextButton(onPressed: _update, child: Text(_updateError != null ? 'Retry' : 'Update'))
                        : null,
              ),
            ],
          ]),
          if (app.hasContainer)
            TileGroup(title: 'Tools', children: [
              ListTile(
                leading: const Icon(Icons.receipt_long_outlined),
                title: const Text('Logs'),
                subtitle: const Text('What the app has printed lately'),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => ContainerLogsScreen.forApp(app))),
              ),
              ListTile(
                leading: const Icon(Icons.terminal_outlined),
                title: const Text('Terminal'),
                subtitle: Text(app.isRunning ? 'A shell inside the app’s container' : 'Start the app to open a shell'),
                trailing: const Icon(Icons.chevron_right),
                enabled: app.isRunning,
                onTap: _openTerminal,
              ),
            ]),
          if (_tips != null) TileGroup(title: 'Notes from the app store', children: [ListTile(title: SelectableText(_tips!))]),
          if (!app.isLink)
            TileGroup(children: [
              ListTile(
                leading: Icon(Icons.delete_outline, color: scheme.error),
                title: Text('Uninstall', style: TextStyle(color: scheme.error)),
                onTap: _uninstall,
              ),
            ]),
        ],
      ),
    );
  }
}

/// Uninstall confirmation with the web's "delete app data too" question.
/// Pops true/false for the checkbox when confirmed, null when cancelled.
class _UninstallDialog extends StatefulWidget {
  const _UninstallDialog({required this.title});

  final String title;

  @override
  State<_UninstallDialog> createState() => _UninstallDialogState();
}

class _UninstallDialogState extends State<_UninstallDialog> {
  bool _deleteData = false;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return AlertDialog(
      title: Text('Uninstall “${widget.title}”?'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text('The app and its containers are removed.'),
          const SizedBox(height: Space.sm),
          CheckboxListTile(
            contentPadding: EdgeInsets.zero,
            controlAffinity: ListTileControlAffinity.leading,
            value: _deleteData,
            onChanged: (v) => setState(() => _deleteData = v ?? false),
            title: const Text('Also delete its data'),
            subtitle: const Text("Settings and files in the app's folder. This can't be undone."),
          ),
        ],
      ),
      actions: [
        TextButton(autofocus: true, onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton(
          style: FilledButton.styleFrom(backgroundColor: scheme.error, foregroundColor: scheme.onError, side: BorderSide.none),
          onPressed: () {
            HapticFeedback.heavyImpact();
            Navigator.of(context).pop(_deleteData);
          },
          child: const Text('Uninstall'),
        ),
      ],
    );
  }
}

/// The version in a Docker image reference ("linuxserver/jellyfin:10.11.10"
/// gives "10.11.10"); null for "latest", a digest or no tag.
String? appVersion(String image) {
  final at = image.indexOf('@');
  final ref = at >= 0 ? image.substring(0, at) : image;
  final colon = ref.lastIndexOf(':');
  if (colon < 0 || colon < ref.lastIndexOf('/')) return null;
  final tag = ref.substring(colon + 1).replaceFirst(RegExp('^v'), '');
  if (tag.isEmpty || tag == 'latest' || !RegExp(r'^\d').hasMatch(tag)) return null;
  return tag;
}
