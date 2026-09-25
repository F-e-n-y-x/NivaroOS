import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../models/container_entry.dart';
import '../services/api_client.dart';
import '../ui/ui.dart';
import '../utils/app_icons.dart';
import 'custom_install_screen.dart';

// ---------------------------------------------------------------------------
// Installing: one tracker for the whole app, so progress survives leaving
// the store and coming back.
// ---------------------------------------------------------------------------

/// Where one install stands.
@immutable
class InstallProgress {
  const InstallProgress({this.percent, this.error, this.done = false});

  /// 0-100 once the server reports it; null while it's preparing.
  final int? percent;

  /// Why it failed, in the server's words.
  final String? error;
  final bool done;

  bool get running => !done && error == null;
}

/// Installs store apps and follows their progress: app-management's
/// `app:install-*` events over the message bus WebSocket
/// (`/v2/message_bus/event/app-management`, token in the handshake header),
/// with a poll of the app grid as the fallback when events can't be had.
class StoreInstaller extends ChangeNotifier {
  StoreInstaller._();
  static final StoreInstaller instance = StoreInstaller._();

  final Map<String, InstallProgress> _jobs = {};
  WebSocket? _events;
  Timer? _poll;
  DateTime? _startedAt;

  /// How long an install may take before we stop watching and say so.
  static const giveUpAfter = Duration(minutes: 20);

  InstallProgress? progressOf(String id) => _jobs[id];

  /// Sets [id]'s state directly; for screenshots and tests.
  @visibleForTesting
  void debugSet(String id, InstallProgress? p) {
    if (p == null) {
      _jobs.remove(id);
    } else {
      _jobs[id] = p;
    }
    notifyListeners();
  }

  /// Fetches the app's compose file from the store and installs it
  /// (`POST /v2/app_management/compose`, YAML body, port conflicts
  /// checked), the way the web store does. Throws when the server refuses;
  /// progress arrives through [progressOf] afterwards.
  Future<void> install(String id) async {
    if (_jobs[id]?.running ?? false) return;
    _jobs[id] = const InstallProgress();
    notifyListeners();
    try {
      final res = await ApiClient.instance.getWithAccept('/v2/app_management/apps/${Uri.encodeComponent(id)}/compose', 'application/yaml');
      if (res.statusCode != 200 || res.body.trim().isEmpty) {
        throw ApiException("The store didn't send this app's setup (HTTP ${res.statusCode}).", statusCode: res.statusCode);
      }
      await ApiClient.instance.postBody('/v2/app_management/compose', res.body, 'application/yaml', query: {'check_port_conflict': 'true'});
    } catch (e) {
      _jobs.remove(id);
      notifyListeners();
      rethrow;
    }
    _startedAt ??= DateTime.now();
    _watch();
  }

  /// Forgets a finished or failed install.
  void dismiss(String id) {
    if (_jobs[id]?.running ?? false) return;
    _jobs.remove(id);
    notifyListeners();
  }

  void _update(String id, InstallProgress p) {
    _jobs[id] = p;
    notifyListeners();
    if (!_jobs.values.any((j) => j.running)) _stopWatching();
  }

  Future<void> _watch() async {
    _poll ??= Timer.periodic(const Duration(seconds: 4), (_) => _pollGrid());
    if (_events != null) return;
    try {
      final base = ApiClient.instance.webSocketUri('/v2/message_bus/event/app-management');
      final uri = base.replace(queryParameters: {
        'names': ['app:install-progress', 'app:install-end', 'app:install-error'],
      });
      final ws = await WebSocket.connect(uri.toString(), headers: await ApiClient.instance.authHeaders()).timeout(const Duration(seconds: 10));
      _events = ws;
      ws.listen(
        (frame) {
          if (frame is! String) return;
          try {
            final e = AppEvent.tryParse(jsonDecode(frame));
            if (e != null) handleEvent(e);
          } catch (_) {}
        },
        onDone: () => _events = null,
        onError: (Object _) => _events = null,
        cancelOnError: true,
      );
    } catch (_) {
      // No events (an old server, a proxy without WebSockets): the grid
      // poll still sees the app arrive.
      _events = null;
    }
  }

  /// Applies one app-management event.
  @visibleForTesting
  void handleEvent(AppEvent e) {
    final id = e.app;
    if (id == null || !_jobs.containsKey(id)) return;
    switch (e.name) {
      case 'app:install-progress':
        final p = e.progress;
        if (p != null) _update(id, InstallProgress(percent: p.clamp(0, 100)));
      case 'app:install-end':
        _update(id, const InstallProgress(percent: 100, done: true));
      case 'app:install-error':
        _update(id, InstallProgress(error: e.message?.isNotEmpty == true ? e.message : 'The install failed.'));
    }
  }

  Future<void> _pollGrid() async {
    final running = _jobs.entries.where((e) => e.value.running).map((e) => e.key).toSet();
    if (running.isEmpty) return _stopWatching();
    if (_startedAt != null && DateTime.now().difference(_startedAt!) > giveUpAfter) {
      for (final id in running) {
        _update(id, const InstallProgress(error: 'This is taking longer than usual. Check the Apps tab in a while.'));
      }
      return;
    }
    try {
      final res = await ApiClient.instance.get('/v2/app_management/web/appgrid');
      final data = res['data'];
      if (data is! List) return;
      for (final item in data.whereType<Map>()) {
        final name = item['name']?.toString();
        final store = item['store_app_id']?.toString();
        final id = running.contains(name) ? name : (running.contains(store) ? store : null);
        if (id != null && (item['status']?.toString() ?? '').contains('running')) {
          _update(id, const InstallProgress(percent: 100, done: true));
        }
      }
    } catch (_) {}
  }

  void _stopWatching() {
    _poll?.cancel();
    _poll = null;
    _events?.close();
    _events = null;
    _startedAt = null;
  }
}

/// A short label for a source URL: "owner/repo" on GitHub, else the host.
String storeSourceLabel(String url) {
  final gh = RegExp(r'github\.com/([^/]+)/([^/]+)', caseSensitive: false).firstMatch(url);
  if (gh != null) return '${gh.group(1)}/${gh.group(2)}';
  return Uri.tryParse(url)?.host.isNotEmpty == true ? Uri.parse(url).host : url;
}

// ---------------------------------------------------------------------------
// The store
// ---------------------------------------------------------------------------

/// The app store: the live catalogue from the server's store sources
/// (plan M-31: no built-in list), with search, categories, a CPU filter
/// and install progress.
class AppStoreScreen extends StatefulWidget {
  const AppStoreScreen({super.key});

  @override
  State<AppStoreScreen> createState() => _AppStoreScreenState();
}

class _AppStoreScreenState extends State<AppStoreScreen> {
  StoreCatalog? _catalog;
  List<StoreCategory> _categories = const [];
  List<String> _sources = const [];
  String? _arch;
  Object? _error;
  bool _loading = false;
  String? _category;
  bool _allCpus = false;
  final _search = TextEditingController();

  @override
  void initState() {
    super.initState();
    _search.addListener(() => setState(() {}));
    StoreInstaller.instance.addListener(_onInstall);
    _load();
  }

  @override
  void dispose() {
    StoreInstaller.instance.removeListener(_onInstall);
    _search.dispose();
    super.dispose();
  }

  void _onInstall() {
    if (mounted) setState(() {});
  }

  Future<void> _load() async {
    if (_loading) return;
    setState(() => _loading = true);
    Future<Map<String, dynamic>?> optional(String path) async {
      try {
        return await ApiClient.instance.get(path);
      } catch (_) {
        return null;
      }
    }

    try {
      final results = await Future.wait([
        ApiClient.instance.get('/v2/app_management/apps'),
        optional('/v2/app_management/categories'),
        optional('/v2/app_management/appstore'),
        optional('/v2/app_management/info'),
      ]);
      if (!mounted) return;
      final sources = results[2]?['data'];
      final arch = results[3]?['architecture']?.toString();
      setState(() {
        _catalog = StoreCatalog.fromResponse(results[0]!);
        if (results[1] != null) _categories = StoreCategory.listFrom(results[1]!);
        if (sources is List) _sources = [for (final s in sources.whereType<Map>()) s['url']?.toString() ?? ''];
        _arch = arch == null || arch.isEmpty ? _arch : normalizeArch(arch);
        _error = null;
      });
    } catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  List<StoreApp> _visible(StoreCatalog c) {
    final q = _search.text.trim().toLowerCase();
    return c.apps.where((a) {
      if (!_allCpus && !a.supports(_arch)) return false;
      if (_category != null && a.category.toLowerCase() != _category!.toLowerCase()) return false;
      if (q.isEmpty) return true;
      return a.title.toLowerCase().contains(q) ||
          a.tagline.toLowerCase().contains(q) ||
          a.id.toLowerCase().contains(q) ||
          a.category.toLowerCase().contains(q);
    }).toList();
  }

  void _openDetail(StoreApp app) {
    Navigator.of(context).push(MaterialPageRoute(
      builder: (_) => StoreAppDetailScreen(app: app, arch: _arch, installed: _catalog?.installed.contains(app.id) ?? false),
    ));
  }

  void _showSources() {
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      useSafeArea: true,
      isScrollControlled: true,
      builder: (sheet) {
        final theme = Theme.of(sheet);
        return SingleChildScrollView(
          padding: const EdgeInsets.only(bottom: Space.lg),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            mainAxisSize: MainAxisSize.min,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(Space.lg, 0, Space.lg, Space.sm),
                child: Text('Store sources', style: theme.textTheme.titleLarge),
              ),
              if (_sources.isEmpty)
                const ListTile(title: Text('No sources'), subtitle: Text("This server's store has no sources."))
              else
                for (final s in _sources)
                  ListTile(
                    leading: const Icon(Icons.source_outlined),
                    title: Text(storeSourceLabel(s)),
                    subtitle: Text(s, maxLines: 2, overflow: TextOverflow.ellipsis),
                  ),
              Padding(
                padding: const EdgeInsets.fromLTRB(Space.lg, Space.sm, Space.lg, 0),
                child: Text(
                  'Add or remove sources in the web dashboard, under App store.',
                  style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                ),
              ),
            ],
          ),
        );
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    final catalog = _catalog;
    final error = _error;
    final offline = error is ApiException && error.isUnreachable;

    final List<Widget> slivers;
    if (catalog == null) {
      if (error == null) {
        slivers = [const SliverLoadingList(rows: 8, leading: SkeletonLeading.thumbnail)];
      } else if (offline) {
        slivers = [ErrorState.offline(sliver: true, onRetry: _load, details: error.details)];
      } else {
        slivers = [
          ErrorState(
            sliver: true,
            title: "Couldn't load the app store",
            message: error is ApiException ? error.message : 'The store sources could not be read.',
            details: error is ApiException ? error.details : error.toString(),
            onRetry: _load,
          ),
        ];
      }
    } else if (catalog.apps.isEmpty) {
      slivers = [
        EmptyState(
          sliver: true,
          icon: Icons.storefront_outlined,
          title: 'The store is empty',
          message: "This server's store sources list no apps. Add a source in the web dashboard, or install from a compose file.",
          actionLabel: 'Show sources',
          onAction: _showSources,
        ),
      ];
    } else {
      final visible = _visible(catalog);
      final hidden = _allCpus ? 0 : catalog.apps.where((a) => !a.supports(_arch)).length;
      slivers = [
        SliverToBoxAdapter(child: _controls()),
        if (visible.isEmpty)
          EmptyState(
            sliver: true,
            icon: Icons.search_off_outlined,
            title: _search.text.trim().isEmpty ? 'No apps in this category' : 'No apps match “${_search.text.trim()}”',
            message: hidden > 0 ? 'Some apps are hidden because they don’t run on this server’s CPU.' : 'Try another name or category.',
            actionLabel: 'Show all apps',
            onAction: () => setState(() {
              _search.clear();
              _category = null;
            }),
          )
        else ...[
          SliverList.builder(
            itemCount: visible.length,
            itemBuilder: (context, i) {
              final app = visible[i];
              return StoreAppRow(
                app: app,
                installed: catalog.installed.contains(app.id),
                supported: app.supports(_arch),
                arch: _arch,
                progress: StoreInstaller.instance.progressOf(app.id),
                onTap: () => _openDetail(app),
              );
            },
          ),
          if (hidden > 0)
            SliverToBoxAdapter(
              child: Padding(
                padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.lg, Space.gutter(context), 0),
                child: Text(
                  '$hidden ${hidden == 1 ? 'app is' : 'apps are'} hidden because ${hidden == 1 ? 'it doesn’t' : 'they don’t'} run on ${_arch ?? 'this server'}.',
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(color: Theme.of(context).colorScheme.onSurfaceVariant),
                ),
              ),
            ),
        ],
      ];
    }

    return AppScaffold.slivers(
      title: 'App store',
      collapsingTitle: false,
      onRefresh: _load,
      banner: catalog != null && offline ? OfflineBanner(onRetry: _load) : null,
      actions: [
        PopupMenuButton<String>(
          tooltip: 'More options',
          onSelected: (v) {
            switch (v) {
              case 'cpu':
                setState(() => _allCpus = !_allCpus);
              case 'sources':
                _showSources();
              case 'custom':
                Navigator.of(context).push(MaterialPageRoute(builder: (_) => const CustomInstallScreen()));
              case 'refresh':
                _load();
            }
          },
          itemBuilder: (_) => [
            CheckedPopupMenuItem(value: 'cpu', checked: _allCpus, child: const Text('Show apps for other CPUs')),
            const PopupMenuItem(value: 'sources', child: Text('Store sources')),
            const PopupMenuItem(value: 'custom', child: Text('Install from compose file')),
            const PopupMenuItem(value: 'refresh', child: Text('Refresh')),
          ],
        ),
      ],
      slivers: slivers,
    );
  }

  Widget _controls() {
    final gutter = Space.gutter(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.sm),
          child: SearchBar(
            controller: _search,
            hintText: 'Search the store',
            leading: const Icon(Icons.search),
            elevation: const WidgetStatePropertyAll(0),
            trailing: [
              if (_search.text.isNotEmpty) IconButton(tooltip: 'Clear search', icon: const Icon(Icons.close), onPressed: _search.clear),
            ],
          ),
        ),
        if (_categories.isNotEmpty)
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            padding: EdgeInsets.symmetric(horizontal: gutter),
            child: Row(children: [
              ChoiceChip(label: const Text('All'), selected: _category == null, onSelected: (_) => setState(() => _category = null)),
              for (final c in _categories) ...[
                const SizedBox(width: Space.sm),
                ChoiceChip(
                  label: Text(c.name),
                  selected: _category == c.name,
                  onSelected: (sel) => setState(() => _category = sel ? c.name : null),
                ),
              ],
            ]),
          ),
        const SizedBox(height: Space.sm),
      ],
    );
  }
}

/// A store app as a list row: icon, name, tagline, and on the right its
/// install state.
class StoreAppRow extends StatelessWidget {
  const StoreAppRow({
    super.key,
    required this.app,
    required this.installed,
    required this.supported,
    this.arch,
    this.progress,
    this.onTap,
  });

  final StoreApp app;
  final bool installed;
  final bool supported;
  final String? arch;
  final InstallProgress? progress;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final p = progress;
    Widget? state;
    if (p != null && p.running) {
      state = StatusChip(label: p.percent == null ? 'Installing' : 'Installing ${p.percent}%', status: Status.info, icon: Icons.downloading_outlined);
    } else if (installed || (p?.done ?? false)) {
      state = const StatusChip(label: 'Installed', status: Status.success);
    } else if (p?.error != null) {
      state = const StatusChip(label: 'Failed', status: Status.error);
    } else if (!supported) {
      state = StatusChip(label: 'Not for ${arch ?? 'this CPU'}', status: Status.neutral, icon: Icons.block_outlined);
    }
    final supporting = app.tagline.isNotEmpty ? app.tagline : app.category;
    return MergeSemantics(
      child: ListTile(
        onTap: onTap,
        leading: NivaroAppIcon(iconUrl: app.icon, name: app.title, size: 56, radius: Corners.medium),
        title: Text(app.title, maxLines: 1, overflow: TextOverflow.ellipsis),
        subtitle: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(supporting, maxLines: 2, overflow: TextOverflow.ellipsis),
            if (state != null) ...[const SizedBox(height: Space.xs), state],
          ],
        ),
        // Three-line layout (icon at the top) only when there is a third
        // line; a short tagline keeps the row compact and centred.
        isThreeLine: state != null || supporting.length > 40 || MediaQuery.textScalerOf(context).scale(14) > 21,
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// One store app
// ---------------------------------------------------------------------------

/// A store app's page: what it is, screenshots, what to know before
/// installing, and Install with its progress.
class StoreAppDetailScreen extends StatefulWidget {
  const StoreAppDetailScreen({super.key, required this.app, this.arch, this.installed = false});

  /// The catalogue entry, shown at once while the full details load.
  final StoreApp app;
  final String? arch;
  final bool installed;

  @override
  State<StoreAppDetailScreen> createState() => _StoreAppDetailScreenState();
}

class _StoreAppDetailScreenState extends State<StoreAppDetailScreen> {
  late StoreApp _app = widget.app;
  bool _expanded = false;
  bool _starting = false;

  @override
  void initState() {
    super.initState();
    StoreInstaller.instance.addListener(_changed);
    _loadDetails();
  }

  @override
  void dispose() {
    StoreInstaller.instance.removeListener(_changed);
    super.dispose();
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  Future<void> _loadDetails() async {
    try {
      final res = await ApiClient.instance.get('/v2/app_management/apps/${Uri.encodeComponent(widget.app.id)}');
      final data = res['data'];
      if (data is Map && mounted) setState(() => _app = StoreApp.fromJson(widget.app.id, Map<String, dynamic>.from(data)));
    } catch (_) {
      // The catalogue entry is enough to install from.
    }
  }

  Future<void> _install() async {
    final tips = _app.tips;
    if (tips != null) {
      final go = await showDialog<bool>(
        context: context,
        builder: (d) => AlertDialog(
          title: Text('Before installing ${_app.title}'),
          content: SingleChildScrollView(child: SelectableText(tips)),
          actions: [
            TextButton(autofocus: true, onPressed: () => Navigator.of(d).pop(false), child: const Text('Cancel')),
            TextButton(onPressed: () => Navigator.of(d).pop(true), child: const Text('Install')),
          ],
        ),
      );
      if (go != true) return;
    }
    if (!mounted) return;
    final messenger = ScaffoldMessenger.of(context);
    setState(() => _starting = true);
    try {
      await StoreInstaller.instance.install(_app.id);
    } catch (e) {
      messenger.showSnackBar(SnackBar(content: Text("Couldn't install ${_app.title}. ${e is ApiException ? e.message : e}")));
    } finally {
      if (mounted) setState(() => _starting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final app = _app;
    final gutter = Space.gutter(context);
    final p = StoreInstaller.instance.progressOf(app.id);
    final installed = widget.installed || (p?.done ?? false);
    final supported = app.supports(widget.arch);
    final by = [if (app.by.isNotEmpty) app.by, if (app.category.isNotEmpty) app.category].join(' · ');

    Widget action;
    if (p != null && p.running || _starting) {
      final pct = p?.percent;
      action = Semantics(
        liveRegion: true,
        label: pct == null ? 'Installing' : 'Installing, $pct percent',
        child: ExcludeSemantics(
          child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            LinearProgressIndicator(value: pct == null || pct == 0 ? null : pct / 100),
            const SizedBox(height: Space.sm),
            Text(
              pct == null || pct == 0 ? 'Preparing…' : 'Downloading and setting up · $pct%',
              style: theme.textTheme.bodyMedium?.tabular.copyWith(color: scheme.onSurfaceVariant),
            ),
          ]),
        ),
      );
    } else if (installed) {
      action = Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        const Align(alignment: AlignmentDirectional.centerStart, child: StatusChip(label: 'Installed', status: Status.success)),
        const SizedBox(height: Space.sm),
        Text('Find it in the Apps tab.', style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
      ]);
    } else {
      action = Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        FilledButton.icon(
          onPressed: supported ? _install : null,
          icon: const Icon(Icons.download_outlined),
          label: Text(p?.error != null ? 'Try again' : 'Install'),
        ),
        if (p?.error != null) ...[
          const SizedBox(height: Space.sm),
          Text('The install failed: ${p!.error}', style: theme.textTheme.bodyMedium?.copyWith(color: scheme.error)),
        ] else if (!supported) ...[
          const SizedBox(height: Space.sm),
          Text(
            'This app doesn’t run on this server’s CPU (${widget.arch}). It supports ${app.architectures.join(', ')}.',
            style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
          ),
        ],
      ]);
    }

    final description = app.description.trim();
    final long = description.length > 280;

    // The name is the page's heading, right under the bar; repeating it
    // in the bar would say it twice.
    return AppScaffold(
      title: '',
      body: ListView(
        padding: const EdgeInsets.only(bottom: Space.xl),
        children: [
          Padding(
            padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.lg),
            child: Row(children: [
              NivaroAppIcon(iconUrl: app.icon, name: app.title, size: 64, radius: Corners.large),
              const SizedBox(width: Space.lg),
              Expanded(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Semantics(header: true, child: Text(app.title, style: theme.textTheme.headlineSmall)),
                  if (by.isNotEmpty) ...[
                    const SizedBox(height: Space.xs),
                    Text(by, style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
                  ],
                ]),
              ),
            ]),
          ),
          Padding(padding: EdgeInsets.symmetric(horizontal: gutter), child: action),
          if (app.tagline.isNotEmpty)
            Padding(
              padding: EdgeInsets.fromLTRB(gutter, Space.xl, gutter, 0),
              child: Text(app.tagline, style: theme.textTheme.titleMedium),
            ),
          if (app.screenshots.isNotEmpty) ...[
            const SizedBox(height: Space.lg),
            SizedBox(
              height: 180,
              child: ListView.separated(
                scrollDirection: Axis.horizontal,
                padding: EdgeInsets.symmetric(horizontal: gutter),
                itemCount: app.screenshots.length,
                separatorBuilder: (_, _) => const SizedBox(width: Space.sm),
                itemBuilder: (context, i) => _Screenshot(url: app.screenshots[i], index: i + 1, of: app.screenshots.length),
              ),
            ),
          ],
          if (description.isNotEmpty)
            Padding(
              padding: EdgeInsets.fromLTRB(gutter, Space.lg, gutter, 0),
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                AnimatedSize(
                  duration: Motion.of(context).medium,
                  curve: Motion.standard,
                  alignment: Alignment.topCenter,
                  child: Text(
                    description,
                    maxLines: _expanded || !long ? null : 4,
                    overflow: _expanded || !long ? null : TextOverflow.ellipsis,
                    style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
                  ),
                ),
                if (long)
                  TextButton(
                    style: TextButton.styleFrom(padding: EdgeInsets.zero),
                    onPressed: () => setState(() => _expanded = !_expanded),
                    child: Text(_expanded ? 'Show less' : 'Show more'),
                  ),
              ]),
            ),
          if (app.tips != null)
            TileGroup(title: 'Before you install', children: [
              ListTile(leading: const Icon(Icons.info_outline), title: SelectableText(app.tips!)),
            ]),
          TileGroup(title: 'Details', children: [
            if (app.developer.isNotEmpty) ListTile(leading: const Icon(Icons.code_outlined), title: const Text('Developer'), subtitle: Text(app.developer)),
            if (app.author.isNotEmpty && app.author != app.developer)
              ListTile(leading: const Icon(Icons.person_outline), title: const Text('Packaged by'), subtitle: Text(app.author)),
            if (app.image != null)
              ListTile(
                leading: const Icon(Icons.layers_outlined),
                title: const Text('Image'),
                subtitle: Text(app.image!),
                onLongPress: () {
                  Clipboard.setData(ClipboardData(text: app.image!));
                  ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Image name copied')));
                },
              ),
            if (app.portMap != null) ListTile(leading: const Icon(Icons.lan_outlined), title: const Text('Web port'), subtitle: Text(app.portMap!)),
            ListTile(
              leading: const Icon(Icons.memory_outlined),
              title: const Text('Runs on'),
              subtitle: Text(app.architectures.isEmpty ? 'Not stated' : app.architectures.join(', ')),
            ),
          ]),
        ],
      ),
    );
  }
}

class _Screenshot extends StatelessWidget {
  const _Screenshot({required this.url, required this.index, required this.of});

  final String url;
  final int index;
  final int of;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Semantics(
      image: true,
      label: 'Screenshot $index of $of',
      child: ClipRRect(
        borderRadius: BorderRadius.circular(Corners.medium),
        child: AspectRatio(
          aspectRatio: 16 / 10,
          child: ColoredBox(
            color: scheme.surfaceContainerHighest,
            child: Image.network(
              url,
              fit: BoxFit.cover,
              errorBuilder: (_, _, _) => Center(child: Icon(Icons.image_not_supported_outlined, color: scheme.onSurfaceVariant)),
            ),
          ),
        ),
      ),
    );
  }
}
