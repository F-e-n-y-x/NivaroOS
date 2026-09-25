import 'dart:async';

import 'package:flutter/material.dart';

import '../models/dashboard_stats.dart';
import '../services/api_client.dart';
import '../services/app_update_service.dart';
import '../ui/ui.dart';
import '../utils/format.dart';
import '../widgets/monitor_modals.dart' show FactTile;
import 'app_update_sheet.dart';

/// One upgradable package from `/sys/packages/check`.
@immutable
class PackageUpdate {
  const PackageUpdate({required this.name, required this.currentVersion, required this.newVersion, required this.isSecurity});

  final String name;
  final String currentVersion;
  final String newVersion;
  final bool isSecurity;

  factory PackageUpdate.fromJson(Map<String, dynamic> j) => PackageUpdate(
        name: j['name']?.toString() ?? '',
        currentVersion: j['current_version']?.toString() ?? '',
        newVersion: j['new_version']?.toString() ?? '',
        isSecurity: j['is_security'] == true,
      );
}

class _Packages {
  _Packages(this.list, this.security, this.lastChecked);
  final List<PackageUpdate> list;
  final int security;
  final DateTime? lastChecked;

  static _Packages fromJson(Map<String, dynamic> d) => _Packages(
        (d['packages'] as List<dynamic>? ?? const []).whereType<Map>().map((e) => PackageUpdate.fromJson(Map<String, dynamic>.from(e))).toList(),
        (d['security_count'] as num?)?.toInt() ?? 0,
        DateTime.tryParse(d['last_checked']?.toString() ?? ''),
      );
}

class _ServerVersion {
  _ServerVersion(this.current, this.needUpdate, this.latest, this.changeLog);
  final String current;
  final bool needUpdate;
  final String latest;
  final String changeLog;
}

/// Updates, in three separate tracks that are never mixed:
///
/// - NivaroOS itself: `GET /v1/sys/version/check`, installed with
///   `POST /v1/sys/update`.
/// - The server's Debian packages: `/v1/sys/packages/*`.
/// - This app: [AppUpdateService], the one self-update path (sideload
///   builds only, owner decision §7.1): the same release pick, checksum
///   and signing-key checks as everywhere; Settings links here.
/// The two update pages (owner decision, plan §7.5): NivaroOS itself (with
/// this app's own update and the server facts), and the server's Debian
/// packages - different jobs with different risks, kept apart as in the
/// web UI's settings.
enum UpdatesPage { nivaroos, packages }

class SystemUpdatesScreen extends StatefulWidget {
  const SystemUpdatesScreen({super.key, this.page = UpdatesPage.nivaroos});

  final UpdatesPage page;

  @override
  State<SystemUpdatesScreen> createState() => _SystemUpdatesScreenState();
}

class _SystemUpdatesScreenState extends State<SystemUpdatesScreen> {
  _ServerVersion? _version;
  Object? _versionError;
  _Packages? _packages;
  Object? _packagesError;
  HostInfo? _host;
  bool _loaded = false;

  bool _checkingPackages = false;
  bool _upgradeRunning = false;
  bool _installingServer = false;

  String _appInstalled = '';
  String _appBuild = '';
  UpdateCheck? _app;
  bool _appChecking = true;
  String? _appCheckError;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() => Future.wait([_loadServer(), _checkApp()]);

  Future<void> _loadServer() async {
    final api = ApiClient.instance;
    await Future.wait([
      () async {
        try {
          final res = await api.get('/sys/version/check');
          final d = res['data'];
          if (d is! Map) throw ApiException('Unexpected answer from the server.');
          final v = d['version'];
          final latest = v is Map ? v['version']?.toString() ?? '' : '';
          _version = _ServerVersion(d['current_version']?.toString() ?? '', d['need_update'] == true, latest,
              v is Map ? v['change_log']?.toString().trim() ?? '' : '');
          _versionError = null;
        } catch (e) {
          _versionError = e;
        }
      }(),
      () async {
        try {
          final res = await api.get('/sys/packages/check');
          final d = res['data'];
          if (d is! Map) throw ApiException('Unexpected answer from the server.');
          _packages = _Packages.fromJson(Map<String, dynamic>.from(d));
          _packagesError = null;
        } catch (e) {
          _packagesError = e;
        }
      }(),
      () async {
        try {
          final res = await api.get('/sys/packages/upgrade/status');
          final d = res['data'];
          _upgradeRunning = d is Map && d['running'] == true;
        } catch (_) {}
      }(),
      () async {
        try {
          final res = await api.get('/sys/hardware');
          final d = res['data'];
          if (d is Map) _host = HostInfo.fromJson(Map<String, dynamic>.from(d));
        } catch (_) {}
      }(),
    ]);
    if (mounted) setState(() => _loaded = true);
  }

  // The saved check first, so the row fills at once, then a fresh one:
  // someone who opened Updates wants the current answer.
  Future<void> _checkApp() async {
    final svc = AppUpdateService.instance;
    setState(() {
      _appChecking = true;
      _appCheckError = null;
    });
    try {
      final v = await svc.installedVersion();
      _appInstalled = v.version;
      _appBuild = v.build;
    } catch (_) {}
    final cached = await svc.cached();
    if (mounted && cached != null) setState(() => _app = cached);
    String? error;
    UpdateCheck? fresh;
    try {
      fresh = await svc.check(force: true);
    } on UpdateException catch (e) {
      error = e.message;
    }
    if (!mounted) return;
    setState(() {
      if (fresh != null) _app = fresh;
      _appChecking = false;
      _appCheckError = error;
    });
  }

  bool get _appHasUpdate => _app?.updateAvailable ?? false;

  void _snack(String text) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(text)));
  }

  static String _msg(Object e) => e.toString().replaceFirst('Exception: ', '');

  Future<void> _installServerUpdate() async {
    final v = _version!;
    final ok = await ConfirmDialog.confirm(
      context,
      title: 'Install NivaroOS ${v.latest}?',
      message: 'NivaroOS services restart while it installs, so this app and the web UI lose the connection for a few minutes.',
      confirmLabel: 'Install',
    );
    if (!ok || !mounted) return;
    setState(() => _installingServer = true);
    try {
      await ApiClient.instance.post('/sys/update');
      _snack('Installing NivaroOS ${v.latest}');
    } catch (e) {
      _snack("Couldn't start the update. ${_msg(e)}");
    } finally {
      if (mounted) setState(() => _installingServer = false);
    }
  }

  Future<void> _checkPackages() async {
    setState(() => _checkingPackages = true);
    try {
      final res = await ApiClient.instance.post('/sys/packages/refresh');
      final d = res['data'];
      if (d is Map && mounted) {
        setState(() {
          _packages = _Packages.fromJson(Map<String, dynamic>.from(d));
          _packagesError = null;
        });
      }
    } catch (e) {
      _snack("Couldn't check for updates. ${_msg(e)}");
    } finally {
      if (mounted) setState(() => _checkingPackages = false);
    }
  }

  Future<void> _installPackages() async {
    final p = _packages!;
    final n = p.list.length;
    final ok = await ConfirmDialog.confirm(
      context,
      title: n == 1 ? 'Install 1 update?' : 'Install $n updates?',
      message: 'The server upgrades its Debian packages (apt-get dist-upgrade). Some services may restart while it runs.',
      confirmLabel: 'Install',
    );
    if (!ok || !mounted) return;
    try {
      await ApiClient.instance.post('/sys/packages/upgrade');
      if (!mounted) return;
      setState(() => _upgradeRunning = true);
      _showUpgrade();
    } catch (e) {
      _snack("Couldn't start the upgrade. ${_msg(e)}");
    }
  }

  void _showUpgrade() {
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      useSafeArea: true,
      isScrollControlled: true,
      builder: (_) => const _UpgradeSheet(),
    ).then((_) {
      if (mounted) _loadServer();
    });
  }

  void _openPackages() {
    Navigator.of(context)
        .push<bool>(MaterialPageRoute(builder: (_) => PackagesScreen(packages: _packages!.list, busy: _upgradeRunning)))
        .then((install) {
      if (install == true && mounted) _installPackages();
    });
  }

  void _showNotes(String title, String text) {
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      useSafeArea: true,
      isScrollControlled: true,
      builder: (sheet) => SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(Space.lg, 0, Space.lg, Space.xl),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Semantics(header: true, child: Text(title, style: Theme.of(sheet).textTheme.titleLarge)),
              const SizedBox(height: Space.md),
              SelectableText(text, style: Theme.of(sheet).textTheme.bodyMedium),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _openAppUpdate() async {
    final release = _app?.latest;
    if (release == null) return;
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      useSafeArea: true,
      builder: (_) => UpdateSheet(release: release, installed: _appInstalled),
    );
  }

  @override
  Widget build(BuildContext context) {
    final List<Widget> slivers;
    final isPackages = widget.page == UpdatesPage.packages;
    final pageError = isPackages ? _packagesError : _versionError;
    if (!_loaded) {
      slivers = const [SliverLoadingList(rows: 7)];
    } else if (pageError != null && (isPackages || _host == null)) {
      final e = pageError;
      void retry() {
        setState(() => _loaded = false);
        _loadServer();
      }
      slivers = [
        e is ApiException && e.statusCode == null
            ? ErrorState.offline(onRetry: retry, sliver: true)
            : ErrorState(
                title: "Couldn't check for updates",
                message: _msg(e),
                onRetry: retry,
                details: isPackages ? 'GET /v1/sys/packages/check: $e' : 'GET /v1/sys/version/check: $e',
                sliver: true),
      ];
    } else {
      slivers = [
        SliverList.list(children: [
          if (isPackages)
            _packagesGroup(context)
          else ...[
            _nivaroGroup(context),
            _appGroup(context),
            if (_host != null) _aboutGroup(_host!),
          ],
          const SizedBox(height: Space.lg),
        ]),
      ];
    }
    return AppScaffold.slivers(title: isPackages ? 'System packages' : 'NivaroOS update', onRefresh: _refresh, slivers: slivers);
  }

  Widget _stateIcon(BuildContext context, {required bool upToDate, IconData pending = Icons.new_releases_outlined}) {
    final scheme = Theme.of(context).colorScheme;
    return upToDate
        ? Icon(Icons.check_circle_outline, color: StatusColors.of(context).success.color)
        : Icon(pending, color: scheme.primary);
  }

  Widget _progressSubtitle(String text, [double? value]) => Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(text),
          const SizedBox(height: Space.sm),
          LinearProgressIndicator(value: value),
        ],
      );

  Widget _nivaroGroup(BuildContext context) {
    final v = _version;
    if (v == null) {
      return TileGroup(title: 'NivaroOS', children: [
        ListTile(
          leading: Icon(Icons.error_outline, color: Theme.of(context).colorScheme.error),
          title: const Text("Couldn't check for a NivaroOS update"),
          subtitle: Text(_msg(_versionError ?? '')),
        ),
      ]);
    }
    final current = v.current.isEmpty ? 'NivaroOS' : 'NivaroOS ${v.current}';
    return TileGroup(title: 'NivaroOS', children: [
      MergeSemantics(
        child: ListTile(
          leading: _stateIcon(context, upToDate: !v.needUpdate),
          title: Text(current),
          subtitle: _installingServer
              ? _progressSubtitle('Starting the update')
              : Text(v.needUpdate ? (v.latest.isEmpty ? 'A newer version is available' : 'Version ${v.latest} is available') : 'Up to date'),
        ),
      ),
      if (v.changeLog.isNotEmpty)
        ListTile(
          leading: const Icon(Icons.notes_outlined),
          title: const Text("What's new"),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => _showNotes(v.needUpdate && v.latest.isNotEmpty ? 'NivaroOS ${v.latest}' : current, v.changeLog),
        ),
      if (v.needUpdate) _action(v.latest.isEmpty ? 'Install the update' : 'Install ${v.latest}', _installingServer ? null : _installServerUpdate),
    ]);
  }

  Widget _packagesGroup(BuildContext context) {
    final p = _packages;
    final checked = p?.lastChecked;
    final footer = checked == null ? null : 'Package list checked ${_tail(formatRelative(checked))}.';
    if (p == null) {
      // No group title: the page itself is "System packages".
      return TileGroup(children: [
        ListTile(
          leading: Icon(Icons.error_outline, color: Theme.of(context).colorScheme.error),
          title: const Text("Couldn't list package updates"),
          subtitle: Text(_msg(_packagesError ?? '')),
        ),
        _checkTile(),
      ]);
    }
    final n = p.list.length;
    return TileGroup(footer: footer, children: [
      MergeSemantics(
        child: ListTile(
          leading: _stateIcon(context, upToDate: n == 0, pending: Icons.update_outlined),
          title: Text(n == 0 ? 'Up to date' : (n == 1 ? '1 update available' : '$n updates available')),
          subtitle: n == 0
              ? const Text('Debian packages on the server')
              : Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Text('Debian packages on the server'),
                    if (p.security > 0) ...[
                      const SizedBox(height: Space.xs),
                      StatusChip(label: '${p.security} security', status: Status.warning, icon: Icons.shield_outlined),
                    ],
                  ],
                ),
          isThreeLine: p.security > 0,
          trailing: n == 0 ? null : const Icon(Icons.chevron_right),
          onTap: n == 0 ? null : _openPackages,
        ),
      ),
      if (_upgradeRunning)
        ListTile(
          leading: const Icon(Icons.downloading_outlined),
          title: const Text('Installing updates'),
          subtitle: _progressSubtitle('Tap to see the progress'),
          onTap: _showUpgrade,
        ),
      _checkTile(),
      if (!_upgradeRunning && n > 0)
        _action(n == 1 ? 'Install 1 update' : 'Install $n updates', _checkingPackages ? null : _installPackages),
    ]);
  }

  /// A group's one action: a button in its own segment, so it doesn't
  /// read as another fact row. The same shape in every group.
  Widget _action(String label, VoidCallback? onPressed) => Padding(
        padding: const EdgeInsets.all(Space.md),
        child: Align(
          alignment: AlignmentDirectional.centerStart,
          child: FilledButton.tonalIcon(style: tonalButtonStyle(context), onPressed: onPressed, icon: const Icon(Icons.download_outlined), label: Text(label)),
        ),
      );

  Widget _checkTile() => ListTile(
        leading: const Icon(Icons.refresh),
        title: const Text('Check again'),
        subtitle: _checkingPackages ? _progressSubtitle('This can take up to a minute') : const Text('Asks the package servers for new updates'),
        enabled: !_checkingPackages && !_upgradeRunning,
        onTap: _checkPackages,
      );

  static String _tail(String relative) => relative == 'Just now' || relative == 'Yesterday' ? relative.toLowerCase() : relative;

  Widget _appGroup(BuildContext context) {
    final installed = _appInstalled.isEmpty ? '' : (_appBuild.isEmpty ? _appInstalled : '$_appInstalled (build $_appBuild)');
    final latest = _app?.latest;
    final error = _appCheckError;
    final String subtitle;
    if (_appHasUpdate) {
      final size = latest!.apkSize > 0 ? ' · ${formatBytes(latest.apkSize)}' : '';
      subtitle = 'Version ${latest.version} is available$size';
    } else if (_appChecking && _app == null) {
      subtitle = 'Checking for a newer version';
    } else if (error != null && _app == null) {
      subtitle = "Couldn't check for a newer version";
    } else {
      subtitle = 'Up to date';
    }
    final checked = _app?.checkedAt;
    final footer = [
      if (error != null) error else if (checked != null) 'Checked ${_tail(formatRelative(checked))}.',
      'Updates come from the project’s GitHub releases and are checked against this app’s signing key before they install.',
    ].join(' ');
    final failed = error != null && _app == null;
    return TileGroup(title: 'This app', footer: footer, children: [
      MergeSemantics(
        child: ListTile(
          leading: failed
              ? Icon(Icons.cloud_off_outlined, color: Theme.of(context).colorScheme.onSurfaceVariant)
              : _stateIcon(context, upToDate: !_appHasUpdate && !(_appChecking && _app == null), pending: Icons.system_update_outlined),
          title: Text(installed.isEmpty ? 'NivaroOS for Android' : 'NivaroOS for Android $installed'),
          subtitle: Text(subtitle),
          // One action, as a button beside the facts it acts on (the sheet
          // has the notes, the progress and the checks).
          trailing: _appHasUpdate
              ? null
              : failed
                  ? TextButton(onPressed: _checkApp, child: const Text('Retry'))
                  : (_appChecking ? const SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2.5)) : null),
        ),
      ),
      if (_appHasUpdate) _action('Update to ${latest!.version}', _openAppUpdate),
    ]);
  }

  Widget _aboutGroup(HostInfo h) => TileGroup(title: 'About the server', children: [
        FactTile(icon: Icons.dns_outlined, label: 'Operating system', value: h.osName),
        FactTile(icon: Icons.memory_outlined, label: 'Kernel', value: h.kernel),
        FactTile(icon: Icons.developer_board_outlined, label: 'Architecture', value: h.arch),
        if (h.dockerVersion.isNotEmpty) FactTile(icon: Icons.inventory_2_outlined, label: 'Docker', value: h.dockerVersion),
      ]);
}

/// Every upgradable package, security updates first.
class PackagesScreen extends StatefulWidget {
  const PackagesScreen({super.key, required this.packages, this.busy = false});

  final List<PackageUpdate> packages;

  /// An upgrade is already running, so "Install all" is off.
  final bool busy;

  @override
  State<PackagesScreen> createState() => _PackagesScreenState();
}

class _PackagesScreenState extends State<PackagesScreen> {
  final _search = TextEditingController();

  @override
  void initState() {
    super.initState();
    _search.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  Widget _tile(BuildContext context, PackageUpdate p) {
    final style = Theme.of(context).textTheme.bodyMedium?.copyWith(color: Theme.of(context).colorScheme.onSurfaceVariant);
    return MergeSemantics(
      child: ListTile(
        leading: Icon(p.isSecurity ? Icons.shield_outlined : Icons.inventory_2_outlined),
        title: Text(p.name),
        subtitle: Text('${p.currentVersion} to ${p.newVersion}', style: style),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final q = _search.text.trim().toLowerCase();
    final shown = widget.packages.where((p) => q.isEmpty || p.name.toLowerCase().contains(q)).toList();
    final security = shown.where((p) => p.isSecurity).toList();
    final other = shown.where((p) => !p.isSecurity).toList();
    final gutter = Space.gutter(context);
    final n = widget.packages.length;

    return AppScaffold.slivers(
      title: 'Package updates',
      collapsingTitle: false,
      floatingActionButton: widget.busy
          ? null
          : FloatingActionButton.extended(
              onPressed: () => Navigator.of(context).pop(true),
              icon: const Icon(Icons.download_outlined),
              label: Text(n == 1 ? 'Install 1 update' : 'Install $n updates'),
            ),
      slivers: [
        PinnedHeaderSliver(
          child: ColoredBox(
            color: Theme.of(context).colorScheme.surface,
            child: Padding(
              padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.sm),
              child: SearchBar(
                controller: _search,
                hintText: 'Search packages',
                leading: const Icon(Icons.search),
                elevation: const WidgetStatePropertyAll(0),
                trailing: [
                  if (_search.text.isNotEmpty) IconButton(onPressed: _search.clear, tooltip: 'Clear search', icon: const Icon(Icons.close)),
                ],
              ),
            ),
          ),
        ),
        if (shown.isEmpty)
          EmptyState(
            sliver: true,
            icon: Icons.search_off_outlined,
            title: 'Nothing matches',
            message: 'No package update is called “${_search.text.trim()}”.',
            actionLabel: 'Clear search',
            onAction: _search.clear,
          )
        else
          SliverList.list(children: [
            if (security.isNotEmpty) ...[
              SectionHeader(title: security.length == 1 ? '1 security update' : '${security.length} security updates'),
              for (final p in security) _tile(context, p),
            ],
            if (other.isNotEmpty) ...[
              SectionHeader(title: security.isEmpty ? 'Updates' : 'Other updates'),
              for (final p in other) _tile(context, p),
            ],
            // Room for the extended FAB over the last row.
            const SizedBox(height: 88),
          ]),
      ],
    );
  }
}

/// The package upgrade's live log, polled from the server once a second.
class _UpgradeSheet extends StatefulWidget {
  const _UpgradeSheet();

  @override
  State<_UpgradeSheet> createState() => _UpgradeSheetState();
}

class _UpgradeSheetState extends State<_UpgradeSheet> {
  Timer? _timer;
  List<String> _logs = const [];
  bool _running = true;
  int _exitCode = 0;
  bool _unreachable = false;
  final _scroll = ScrollController();

  @override
  void initState() {
    super.initState();
    _poll();
    _timer = Timer.periodic(const Duration(seconds: 1), (_) => _poll());
  }

  @override
  void dispose() {
    _timer?.cancel();
    _scroll.dispose();
    super.dispose();
  }

  Future<void> _poll() async {
    try {
      final res = await ApiClient.instance.get('/sys/packages/upgrade/status');
      final d = res['data'];
      if (d is! Map || !mounted) return;
      final atEnd = !_scroll.hasClients || _scroll.position.pixels >= _scroll.position.maxScrollExtent - 24;
      setState(() {
        _unreachable = false;
        _running = d['running'] == true;
        _logs = (d['logs'] as List<dynamic>? ?? const []).map((e) => e.toString()).toList();
        _exitCode = (d['exit_code'] as num?)?.toInt() ?? 0;
      });
      if (!_running) _timer?.cancel();
      // Follow the output only while the reader is at the bottom.
      if (atEnd) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (_scroll.hasClients) _scroll.jumpTo(_scroll.position.maxScrollExtent);
        });
      }
    } catch (_) {
      // Services restart during an upgrade; keep polling.
      if (mounted) setState(() => _unreachable = true);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final failed = !_running && _exitCode != 0;
    final title = _running ? 'Installing updates' : (failed ? "The upgrade didn't finish" : 'Updates installed');
    final sub = _running
        ? (_unreachable ? 'Waiting for the server to answer again' : 'You can close this; the upgrade keeps running on the server.')
        : (failed ? 'apt-get stopped with exit code $_exitCode. The log below says why.' : 'All packages were upgraded.');
    return SizedBox(
      height: MediaQuery.sizeOf(context).height * 0.75,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(Space.lg, 0, Space.lg, Space.md),
            child: Semantics(
              liveRegion: true,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(children: [
                    if (!_running) ...[
                      Icon(failed ? Icons.error_outline : Icons.check_circle_outline,
                          color: failed ? scheme.error : StatusColors.of(context).success.color),
                      const SizedBox(width: Space.sm),
                    ],
                    Expanded(child: Text(title, style: theme.textTheme.titleLarge)),
                  ]),
                  const SizedBox(height: Space.xs),
                  Text(sub, style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
                  if (_running) ...[const SizedBox(height: Space.md), const LinearProgressIndicator()],
                ],
              ),
            ),
          ),
          Expanded(
            child: ColoredBox(
              color: scheme.surfaceContainerLow,
              child: _logs.isEmpty
                  ? Center(child: Text('Waiting for output', style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)))
                  : SelectionArea(
                      child: ListView.builder(
                        controller: _scroll,
                        padding: const EdgeInsets.all(Space.lg),
                        itemCount: _logs.length,
                        itemBuilder: (context, i) => Text(
                          _logs[i],
                          style: DesignTokens.of(context).mono(theme.textTheme.bodySmall).copyWith(color: scheme.onSurface),
                        ),
                      ),
                    ),
            ),
          ),
        ],
      ),
    );
  }
}

