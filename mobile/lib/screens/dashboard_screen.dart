import 'dart:async';
import 'dart:convert';

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../models/dashboard_stats.dart';
import '../models/gpu_stats.dart';
import '../services/api_client.dart';
import '../services/vm_client.dart';
import '../services/widget_refresh.dart';
import '../ui/ui.dart';
import '../utils/format.dart';
import '../widgets/monitor_modals.dart';
import '../widgets/server_power.dart';
import '../widgets/vm_console_preview.dart';
import 'system_updates_screen.dart';
import 'vm_console_screen.dart';
import 'vm_list_screen.dart' show VmStateChip;

export '../widgets/monitor_modals.dart' show LiveHistory;

/// Loads everything Home shows and keeps the live part fresh.
///
/// Two speeds: utilization at the phone's [WidgetRefresh] interval (drives
/// no more than every 30 s, the VM list no more than every 5 s) while
/// someone is looking, and the slow checks - updates, backups, apps, host
/// facts - once on open and on pull-to-refresh. Each slow check fails
/// on its own and just leaves its part out; only the live reading decides
/// whether Home shows an error or the offline banner.
class HomeController extends ChangeNotifier {
  HomeController({ApiClient? api, VmClient? vmClient})
      : _api = api ?? ApiClient.instance,
        _vm = vmClient;

  final ApiClient _api;
  final VmClient? _vm;
  VmClient get vmClient => _vm ?? VmClient();

  /// The latest utilization reading, shared with the detail screens.
  final ValueNotifier<LiveStats?> live = ValueNotifier(null);

  /// Recent readings, for the metric cards' charts.
  final LiveHistory history = LiveHistory();

  /// The GPU's latest reading; null when the server has no dedicated GPU
  /// (or its sidecar isn't answering), and then Home shows no GPU card.
  final ValueNotifier<GpuStats?> gpu = ValueNotifier(null);

  // Set once the GPU sidecar said there is no GPU, so the live polls stop
  // asking; pull-to-refresh asks again.
  bool _noGpu = false;

  /// Why the first reading failed; null once there is data.
  Object? liveError;

  HostInfo? host;
  UpdateSummary? updates;
  List<BackupJobBrief> backups = const [];
  AppCounts? apps;

  /// Every VM; null when the VM manager didn't answer.
  List<Vm>? vmList;

  /// Running and total VMs; null when the VM manager didn't answer.
  ({int running, int total})? get vms {
    final l = vmList;
    return l == null ? null : (running: l.where((v) => v.isRunning).length, total: l.length);
  }

  List<DiskUsage> _disks = const [];
  DateTime? _disksAt;
  NetSample? _lastNet;
  DateTime? _lastNetAt;
  DateTime? _vmsAt;
  bool _liveBusy = false;
  bool _vmsBusy = false;
  bool _disposed = false;

  static const disksEvery = WidgetRefresh.drivesEvery;

  /// How many times everything was loaded (open, pull-to-refresh), so the
  /// console preview can take a new picture with it.
  int refreshes = 0;

  List<AttentionItem> get attention => buildAttention(
        updates: updates,
        disks: live.value?.stats.disks ?? const [],
        backups: backups,
        apps: apps,
      );

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    live.dispose();
    gpu.dispose();
    super.dispose();
  }

  /// One utilization reading (plus drives when they're due).
  Future<void> refreshLive({bool forceDisks = false}) async {
    if (_liveBusy) return;
    _liveBusy = true;
    if (!_noGpu) unawaited(_loadGpu());
    try {
      final util = await _api.get('/sys/utilization');
      var stats = DashboardStats.fromUtilization(util['data'] as Map<String, dynamic>? ?? const {});
      final now = clock.now();
      if (forceDisks || _disksAt == null || now.difference(_disksAt!) >= disksEvery) {
        try {
          final res = await _api.get('/sys/disks-usage');
          _disks = (res['data'] as List<dynamic>? ?? const [])
              .whereType<Map>()
              .map((e) => DiskUsage.fromJson(Map<String, dynamic>.from(e)))
              .where((d) => d.mountPoint.isNotEmpty)
              .toList();
          _disksAt = now;
        } catch (_) {
          // Keep the last drive list; the next reading tries again.
        }
      }
      stats = stats.withDisks(_disks);
      final net = stats.primaryNet;
      final rate = NetRate.between(_lastNet, _lastNetAt, net, now) ?? (net?.name == _lastNet?.name ? live.value?.rate : null);
      _lastNet = net;
      _lastNetAt = now;
      if (_disposed) return;
      final reading = LiveStats(stats: stats, rate: rate, updatedAt: now);
      live.value = reading;
      history.add(reading);
      liveError = null;
    } catch (e) {
      if (_disposed) return;
      final last = live.value;
      if (last != null) {
        if (!last.stale) live.value = last.copyWith(stale: true);
      } else {
        liveError = e;
      }
    } finally {
      _liveBusy = false;
    }
    _notify();
  }

  // The GPU sidecar, through the gateway like the web UI's GPU widget. It
  // answers raw JSON (no envelope), and an error or no name on a machine
  // without a dedicated GPU.
  Future<void> _loadGpu() async {
    try {
      final res = await _api.getRaw('/v1/gpu/gpu-stats');
      final g = res.statusCode == 200 ? GpuStats.tryParse(jsonDecode(res.body)) : null;
      if (_disposed) return;
      if (g == null) {
        _noGpu = true;
        gpu.value = null;
        return;
      }
      gpu.value = g;
      history.addGpu(g);
    } catch (_) {
      // Keep the last reading; the next poll asks again.
    }
  }

  /// The VM list again, between pull-to-refreshes: no more often than
  /// every [WidgetRefresh.previewFloor], and a failed poll keeps the last
  /// list rather than dropping the section.
  Future<void> pollVms() async {
    final now = clock.now();
    if (_vmsBusy || (_vmsAt != null && now.difference(_vmsAt!) < WidgetRefresh.previewFloor)) return;
    _vmsBusy = true;
    try {
      await _loadVms(keepOnError: true);
    } finally {
      _vmsBusy = false;
    }
    _notify();
  }

  /// The running VM's screen as PNG bytes, for Home's console preview;
  /// null when the VM has no picture to give (not running, no display).
  /// Throws when the server can't be reached, so the preview can back off.
  Future<Uint8List?> vmScreenshot(String name) async {
    final res = await _api.getRaw('/v1/vm-sidecar/vms/${Uri.encodeComponent(name)}/screenshot');
    if (res.statusCode == 400 || res.statusCode == 404) return null;
    if (res.statusCode != 200) throw ApiException('Screenshot failed (${res.statusCode})', statusCode: res.statusCode);
    return pngSize(res.bodyBytes) == null ? null : res.bodyBytes;
  }

  /// Asks a VM to shut down (like pressing its power button), then reads
  /// the list again a little later.
  Future<void> shutdownVm(String name) async {
    await vmClient.shutdown(name);
    await _loadVms();
    _notify();
  }

  /// Everything, as on open and pull-to-refresh.
  Future<void> refreshAll() async {
    _noGpu = false;
    refreshes++;
    if (live.value == null) {
      liveError = null;
      _notify();
    }
    await Future.wait([
      refreshLive(forceDisks: true),
      _loadHost(),
      loadUpdates(),
      _loadBackups(),
      _loadApps(),
      _loadVms(),
    ]);
    _notify();
  }

  Future<void> _loadHost() async {
    try {
      final res = await _api.get('/sys/hardware');
      final d = res['data'];
      if (d is Map) host = HostInfo.fromJson(Map<String, dynamic>.from(d));
    } catch (_) {}
  }

  /// Pending NivaroOS and package updates. Public so Home can recheck after
  /// the Updates screen closes.
  Future<void> loadUpdates() async {
    bool? serverUpdate;
    String? serverVersion;
    int? packages;
    var security = 0;
    await Future.wait([
      () async {
        try {
          final res = await _api.get('/sys/version/check');
          final d = res['data'];
          if (d is Map) {
            serverUpdate = d['need_update'] == true;
            final v = d['version'];
            if (v is Map) serverVersion = v['version']?.toString();
          }
        } catch (_) {}
      }(),
      () async {
        try {
          final res = await _api.get('/sys/packages/check');
          final d = res['data'];
          if (d is Map) {
            packages = (d['count'] as num?)?.toInt();
            security = (d['security_count'] as num?)?.toInt() ?? 0;
          }
        } catch (_) {}
      }(),
    ]);
    updates = UpdateSummary(serverUpdate: serverUpdate, serverVersion: serverVersion, packages: packages, security: security);
    _notify();
  }

  // Backup & Sync is optional: ask its health route first and show nothing
  // about backups when it doesn't answer (plan M-26).
  Future<void> _loadBackups() async {
    try {
      final health = await _api.get('/backup/health');
      if (health['installed'] == false) {
        backups = const [];
        return;
      }
      final res = await _api.get('/backup/jobs');
      backups = (res['data'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map((e) => BackupJobBrief.fromJson(Map<String, dynamic>.from(e)))
          .toList();
    } catch (_) {
      backups = const [];
    }
  }

  Future<void> _loadApps() async {
    try {
      final res = await _api.get('/v2/app_management/web/appgrid');
      final d = res['data'];
      if (d is List) apps = AppCounts.fromAppGrid(d);
    } catch (_) {}
  }

  // Through the gateway's same-origin route, so it works behind a tunnel
  // or reverse proxy too (plan M-01). The sidecar answers a bare list, so
  // it can't go through the JSON-envelope helper.
  Future<void> _loadVms({bool keepOnError = false}) async {
    _vmsAt = clock.now();
    try {
      final res = await _api.getRaw('/v1/vm-sidecar/vms');
      if (res.statusCode != 200) {
        if (!keepOnError) vmList = null;
        return;
      }
      final list = jsonDecode(res.body);
      if (list is! List) return;
      vmList = list.whereType<Map<String, dynamic>>().map(Vm.fromJson).toList();
    } catch (_) {
      if (!keepOnError) vmList = null;
    }
  }
}

/// Home: the server at a glance. Which server and whether it is fine
/// first, then one card per metric - processor, memory, network, storage,
/// graphics - each with its own live chart, then what needs the owner and
/// what is running. Tabs and tools live in the navigation bar and More,
/// so none are repeated here.
class DashboardScreen extends StatefulWidget {
  final VoidCallback? onOpenFiles;
  final VoidCallback? onOpenVms;
  final VoidCallback? onOpenApps;

  /// Tests pass their own; the app makes one per screen.
  final HomeController? controller;

  /// How often the live widgets refresh; the phone's setting by default.
  final ValueListenable<WidgetRefresh>? refresh;

  const DashboardScreen({
    super.key,
    this.onOpenFiles,
    this.onOpenVms,
    this.onOpenApps,
    this.controller,
    this.refresh,
  });

  @override
  State<DashboardScreen> createState() => _DashboardScreenState();
}

class _DashboardScreenState extends State<DashboardScreen> {
  late final HomeController _c = widget.controller ?? HomeController();
  late final ValueListenable<WidgetRefresh> _refresh = widget.refresh ?? WidgetRefreshController.instance;
  Timer? _timer;

  // Detail screens pushed from here that still want live numbers while
  // Home itself is covered.
  int _detailsOpen = 0;

  @override
  void initState() {
    super.initState();
    _c.addListener(_changed);
    _c.gpu.addListener(_changed);
    _refresh.addListener(_retime);
    _c.refreshAll();
    _startPolling();
  }

  @override
  void dispose() {
    _timer?.cancel();
    _refresh.removeListener(_retime);
    _c.removeListener(_changed);
    _c.gpu.removeListener(_changed);
    if (widget.controller == null) _c.dispose();
    super.dispose();
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  // A new refresh interval applies at once: the poll timer restarts (or
  // stops, for pull-to-refresh only) and the charts re-time to keep
  // their span.
  void _retime() {
    _startPolling();
    _changed();
  }

  void _startPolling() {
    final every = _refresh.value.every;
    _timer?.cancel();
    _timer = every == null ? null : Timer.periodic(every, (_) => _tick());
    _c.history.retime(every);
  }

  // Poll only while someone can see the numbers: Home is the visible tab
  // (a hidden IndexedStack child and a covered route have their tickers
  // off) or one of its detail screens is open, and the app is in front.
  void _tick() {
    if (!mounted) return;
    final lifecycle = WidgetsBinding.instance.lifecycleState;
    if (lifecycle != null && lifecycle != AppLifecycleState.resumed) return;
    final visible = TickerMode.valuesOf(context).enabled;
    if (!visible && _detailsOpen == 0) return;
    _c.refreshLive();
    // The VM rows (and whether there is one to preview) only matter on
    // Home itself.
    if (visible) _c.pollVms();
  }

  Future<void> _openDetail(Widget screen) async {
    _detailsOpen++;
    await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => screen));
    _detailsOpen--;
  }

  void _openCpu() => _openDetail(CpuDetailScreen(live: _c.live, onRetry: _c.refreshLive, history: _c.history));
  void _openMemory() => _openDetail(MemoryDetailScreen(live: _c.live, onRetry: _c.refreshLive, history: _c.history));
  void _openStorage() => _openDetail(StorageDetailScreen(live: _c.live, onRetry: _c.refreshLive, onOpenFiles: widget.onOpenFiles));
  void _openNetwork() => _openDetail(NetworkDetailScreen(live: _c.live, onRetry: _c.refreshLive, history: _c.history));
  void _openGpu() => _openDetail(GpuDetailScreen(gpu: _c.gpu, history: _c.history));

  Future<void> _openUpdates(UpdatesPage page) async {
    await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => SystemUpdatesScreen(page: page)));
    _c.loadUpdates();
  }

  Future<void> _openConsole(Vm vm) async {
    await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => VmConsoleScreen(vmName: vm.name, client: _c.vmClient)));
    if (mounted) _c.refreshAll();
  }

  Future<void> _shutdownVm(Vm vm) async {
    final ok = await ConfirmDialog.confirm(
      context,
      title: 'Shut down “${vm.name}”?',
      message: 'It gets the same signal as pressing its power button, so it can close its programs first. Force stop is on the VMs tab.',
      confirmLabel: 'Shut down',
    );
    if (!ok || !mounted) return;
    final messenger = ScaffoldMessenger.maybeOf(context);
    try {
      await _c.shutdownVm(vm.name);
      messenger?.showSnackBar(SnackBar(content: Text('Shutting down ${vm.name}')));
    } on VmException catch (e) {
      messenger?.showSnackBar(SnackBar(content: Text(e.message)));
    }
  }

  Future<void> _power(String state) => confirmServerPower(context, restart: state == 'restart');

  // Backups are managed in the web UI (the app has no backup screen yet),
  // so the row opens it in the browser rather than doing nothing.
  Future<void> _openWebUi() async {
    final url = ApiClient.instance.baseUrl;
    if (url.isEmpty) return;
    try {
      await launchUrl(Uri.parse(url), mode: LaunchMode.externalApplication);
    } catch (_) {}
  }

  @override
  Widget build(BuildContext context) {
    final live = _c.live.value;
    final error = _c.liveError;

    final List<Widget> slivers;
    if (live == null && error == null) {
      slivers = const [SliverToBoxAdapter(child: _HomeSkeleton())];
    } else if (live == null) {
      final offline = error is ApiException && error.statusCode == null;
      slivers = [
        offline
            ? ErrorState.offline(onRetry: _c.refreshAll, sliver: true)
            : ErrorState(
                title: "Couldn't load the server's status",
                message: error.toString(),
                onRetry: _c.refreshAll,
                details: 'GET /v1/sys/utilization: $error',
                sliver: true,
              ),
      ];
    } else {
      slivers = [SliverList.list(children: _content(context, live))];
    }

    // The server's name is the page's title: Home is the navigation tab
    // already, and a "Home" large title over the name was two headings
    // and a band of empty space. A small bar keeps the cards above the
    // fold.
    final host = _c.host;
    final name = (host?.hostname.isNotEmpty ?? false) ? host!.hostname : (Uri.tryParse(ApiClient.instance.baseUrl)?.host ?? 'Home');
    return AppScaffold.slivers(
      title: name.isEmpty ? 'Home' : name,
      collapsingTitle: false,
      onRefresh: _c.refreshAll,
      banner: live != null && live.stale ? OfflineBanner(lastUpdated: live.updatedAt, onRetry: _c.refreshLive) : null,
      actions: [
        // Power only once the server has answered; before that it would
        // just fail.
        if (live != null) PopupMenuButton<String>(
          tooltip: 'Server power',
          icon: const Icon(Icons.power_settings_new_outlined),
          onSelected: _power,
          itemBuilder: (context) => const [
            PopupMenuItem(value: 'restart', child: ListTile(leading: Icon(Icons.restart_alt_outlined), title: Text('Restart server'))),
            PopupMenuItem(value: 'off', child: ListTile(leading: Icon(Icons.power_settings_new_outlined), title: Text('Shut down server'))),
          ],
        ),
      ],
      slivers: slivers,
    );
  }

  List<Widget> _content(BuildContext context, LiveStats live) {
    final attention = _c.attention;
    final apps = _c.apps;
    final vms = _c.vmList;

    final header = ServerHeader(attention: attention, host: _c.host);
    final metrics = MetricCards(
      live: live,
      history: _c.history,
      gpu: _c.gpu.value,
      window: _c.history.window,
      onOpenCpu: _openCpu,
      onOpenMemory: _openMemory,
      onOpenNetwork: _openNetwork,
      onOpenStorage: _openStorage,
      onOpenGpu: _openGpu,
    );
    final needs = attention.isEmpty
        ? null
        : TileGroup(title: 'Needs attention', children: [for (final a in attention) _attentionTile(context, a)]);
    final machines = vms == null ? null : _vmGroup(context, vms);
    final appsGroup = apps == null
        ? null
        : TileGroup(title: 'Apps', children: [
            ListTile(
              leading: const Icon(Icons.apps_outlined),
              title: Text(apps.total == 0 ? 'No apps installed' : '${apps.running} of ${apps.total} running'),
              subtitle: apps.total == 0 ? const Text('Install one from the app store') : null,
              trailing: const Icon(Icons.chevron_right),
              onTap: widget.onOpenApps,
            ),
          ]);

    return [
      LayoutBuilder(builder: (context, constraints) {
        // Wide windows: the metric cards across the top, then what needs
        // attention beside what's running.
        if (constraints.maxWidth >= 720) {
          return Column(children: [
            header,
            metrics,
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(child: Column(children: [?needs])),
                Expanded(child: Column(children: [?machines, ?appsGroup])),
              ],
            ),
          ]);
        }
        return Column(children: [header, metrics, ?needs, ?machines, ?appsGroup]);
      }),
      const SizedBox(height: Space.lg),
    ];
  }

  /// Running and paused VMs, each with its console and a stop button, like
  /// the apps; the rest are one row that opens the VMs tab. When exactly
  /// one VM is running its screen leads the group, live; with two or more
  /// there are only the rows.
  Widget _vmGroup(BuildContext context, List<Vm> vms) {
    final active = vms.where((v) => v.isActive).toList();
    final idle = vms.length - active.length;
    final running = vms.where((v) => v.isRunning).toList();
    final only = running.length == 1 ? running.single : null;
    return TileGroup(title: 'Virtual machines', children: [
      if (only != null)
        VmConsolePreview(
          // A different VM starts from a blank picture.
          key: ValueKey('preview:${only.name}'),
          name: only.name,
          aspectRatio: only.displayAspect,
          every: _refresh.value.previewEvery,
          refreshToken: _c.refreshes,
          fetch: () => _c.vmScreenshot(only.name),
          onOpen: () => _openConsole(only),
        ),
      for (final vm in active.take(4)) _vmTile(context, vm),
      ListTile(
        leading: const Icon(Icons.computer_outlined),
        title: Text(vms.isEmpty
            ? 'No virtual machines'
            : active.isEmpty
                ? 'None running'
                : 'All virtual machines'),
        subtitle: Text(vms.isEmpty
            ? 'Create one on the VMs tab'
            : [
                if (active.length > 4) '${active.length - 4} more running',
                if (idle > 0) idle == 1 ? '1 turned off' : '$idle turned off',
                if (idle == 0 && active.length <= 4) '${vms.length} in total',
              ].join(' · ')),
        trailing: const Icon(Icons.chevron_right),
        onTap: widget.onOpenVms,
      ),
    ]);
  }

  Widget _vmTile(BuildContext context, Vm vm) {
    final t = DesignTokens.of(context);
    final running = vm.isRunning;
    final spec = [
      if (vm.vcpus > 0) vm.vcpus == 1 ? '1 vCPU' : '${vm.vcpus} vCPU',
      if (vm.memoryMib > 0) formatBytes(vm.memoryMib * 1024 * 1024, decimals: vm.memoryMib % 1024 == 0 ? 0 : 1),
    ].join(' · ');
    return ListTile(
      leading: const Icon(Icons.computer_outlined),
      title: Text(vm.name, maxLines: 1, overflow: TextOverflow.ellipsis),
      subtitle: Padding(
        padding: const EdgeInsets.only(top: Space.xs),
        child: Wrap(
          spacing: Space.sm,
          runSpacing: Space.xs,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            VmStateChip(state: vm.powerState),
            if (spec.isNotEmpty) Text(spec, style: t.data),
          ],
        ),
      ),
      isThreeLine: false,
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (running)
            IconButton(
              tooltip: 'Open console of ${vm.name}',
              icon: const Icon(Icons.desktop_windows_outlined),
              onPressed: () => _openConsole(vm),
            ),
          IconButton(
            tooltip: 'Shut down ${vm.name}',
            icon: const Icon(Icons.stop_circle_outlined),
            onPressed: () => _shutdownVm(vm),
          ),
        ],
      ),
      onTap: running ? () => _openConsole(vm) : widget.onOpenVms,
    );
  }

  Widget _attentionTile(BuildContext context, AttentionItem a) {
    final scheme = Theme.of(context).colorScheme;
    final status = switch (a.severity) {
      AttentionSeverity.error => Status.error,
      AttentionSeverity.warning => Status.warning,
      AttentionSeverity.info => Status.neutral,
    };
    final icon = switch (a.kind) {
      AttentionKind.serverUpdate => Icons.update_outlined,
      AttentionKind.packages => Icons.update_outlined,
      AttentionKind.disk => Icons.storage_outlined,
      AttentionKind.backup => Icons.backup_outlined,
      AttentionKind.apps => Icons.apps_outlined,
    };
    final VoidCallback? onTap = switch (a.kind) {
      AttentionKind.serverUpdate => () => _openUpdates(UpdatesPage.nivaroos),
      AttentionKind.packages => () => _openUpdates(UpdatesPage.packages),
      AttentionKind.disk => _openStorage,
      AttentionKind.apps => widget.onOpenApps,
      // The app has no backup screen yet: the web UI fixes it.
      AttentionKind.backup => _openWebUi,
    };
    final security = a.kind == AttentionKind.packages ? (_c.updates?.security ?? 0) : 0;
    final color = status == Status.neutral ? scheme.onSurfaceVariant : StatusColors.toneOf(context, status).color;

    return MergeSemantics(
      child: ListTile(
        leading: Icon(icon, color: color),
        title: Text(a.title),
        subtitle: security > 0
            ? Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Text('Debian packages'),
                  const SizedBox(height: Space.xs),
                  StatusChip(label: '$security security', status: Status.warning, icon: Icons.shield_outlined),
                ],
              )
            : Text(a.detail),
        isThreeLine: security > 0,
        trailing: onTap == null
            ? null
            : Icon(a.kind == AttentionKind.backup ? Icons.open_in_new_outlined : Icons.chevron_right),
        onTap: onTap,
      ),
    );
  }
}

/// The one-second answer under the server's name (the app bar's title):
/// whether it is fine, what it runs and how long it has been up.
///
/// In a direction with a status panel (Tonal) this sits on a tonal panel
/// whose colour is the health itself: the status container when
/// something needs attention, a neutral container when all is clear.
class ServerHeader extends StatelessWidget {
  const ServerHeader({super.key, required this.attention, required this.host});

  final List<AttentionItem> attention;
  final HostInfo? host;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final t = DesignTokens.of(context);
    final worst = attention.isEmpty ? null : attention.first.severity;
    final status = switch (worst) {
      null => Status.success,
      AttentionSeverity.error => Status.error,
      AttentionSeverity.warning => Status.warning,
      AttentionSeverity.info => Status.info,
    };
    final n = attention.length;
    final verdict = switch (worst) {
      null => 'Everything looks fine',
      AttentionSeverity.info => n == 1 ? '1 thing to look at' : '$n things to look at',
      _ => n == 1 ? '1 thing needs attention' : '$n things need attention',
    };
    final icon = switch (status) {
      Status.success => Icons.check_circle_outline,
      Status.error => Icons.error_outline,
      Status.warning => Icons.warning_amber_outlined,
      _ => Icons.info_outline,
    };
    final h = host;
    final facts = [
      if (h != null && h.osName.isNotEmpty) h.osName,
      if (h != null && h.uptime.isNotEmpty) 'Up ${h.uptime}',
    ];
    final gutter = Space.gutter(context);
    final tone = StatusColors.toneOf(context, status);
    // In dark theme a full status container (a deep red block) is the
    // loudest thing on the page; a wash of the status colour over the
    // card tone says the same with less weight, in ordinary ink.
    final dark = theme.brightness == Brightness.dark;
    final calm = status == Status.success || dark;
    final panel = !t.statusPanel
        ? null
        : status == Status.success
            ? scheme.surfaceContainerHighest
            : dark
                ? Color.alphaBlend(tone.color.withValues(alpha: .16), scheme.surfaceContainerHigh)
                : tone.container;
    final on = panel == null || calm ? scheme.onSurface : tone.onContainer;
    final muted = panel == null || calm ? scheme.onSurfaceVariant : tone.onContainer;

    final Widget content = Semantics(
      container: true,
      liveRegion: true,
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          if (panel == null) StatusDisc(status: status, icon: icon, size: 40) else ExcludeSemantics(child: Icon(icon, size: 28, color: calm ? tone.color : on)),
          const SizedBox(width: Space.md),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                AnimatedSwitcher(
                  duration: Motion.of(context).short,
                  child: Text(verdict, key: ValueKey(verdict), style: theme.textTheme.titleMedium?.copyWith(color: on)),
                ),
                if (facts.isNotEmpty) ...[
                  const SizedBox(height: 2),
                  Text(facts.map((f) => f.replaceAll(' ', '\u00A0')).join(' · '), style: theme.textTheme.bodySmall?.copyWith(color: muted)),
                ],
              ],
            ),
          ),
        ],
      ),
    );

    if (panel == null) {
      return Padding(padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.lg), child: content);
    }
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, 0, gutter, t.gap),
      child: Material(
        color: panel,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(t.cardRadius)),
        child: Padding(padding: const EdgeInsets.symmetric(horizontal: Space.lg, vertical: Space.md), child: content),
      ),
    );
  }
}

/// A status as a tonal disc with its icon: the colour says the state, the
/// icon says it again for anyone who can't see colour.
class StatusDisc extends StatelessWidget {
  const StatusDisc({super.key, required this.status, required this.icon, this.size = 48});

  final Status status;
  final IconData icon;
  final double size;

  @override
  Widget build(BuildContext context) {
    final tone = StatusColors.toneOf(context, status);
    // In dark theme the full container tones (a deep red, a deep amber)
    // are the heaviest thing on the page; a light wash of the status colour
    // says the same with less weight.
    final dark = Theme.of(context).brightness == Brightness.dark;
    return ExcludeSemantics(
      child: Container(
        width: size,
        height: size,
        decoration: BoxDecoration(color: dark ? tone.color.withValues(alpha: 0.18) : tone.container, shape: BoxShape.circle),
        child: Icon(icon, color: dark ? tone.color : tone.onContainer),
      ),
    );
  }
}

/// The metric cards, one per resource, in a grid that fits the window:
/// two columns on a phone, three or four on a tablet, one at large text.
class MetricCards extends StatelessWidget {
  const MetricCards({
    super.key,
    required this.live,
    required this.history,
    required this.gpu,
    required this.window,
    this.onOpenCpu,
    this.onOpenMemory,
    this.onOpenNetwork,
    this.onOpenStorage,
    this.onOpenGpu,
  });

  final LiveStats live;
  final LiveHistory history;
  final GpuStats? gpu;
  final String window;
  final VoidCallback? onOpenCpu;
  final VoidCallback? onOpenMemory;
  final VoidCallback? onOpenNetwork;
  final VoidCallback? onOpenStorage;
  final VoidCallback? onOpenGpu;

  static String _range(List<double> v, String Function(double) f) {
    if (v.length < 2) return 'Collecting readings.';
    final lo = v.reduce((a, b) => a < b ? a : b), hi = v.reduce((a, b) => a > b ? a : b);
    return 'Over the last minutes: ${f(lo)} to ${f(hi)}.';
  }

  @override
  Widget build(BuildContext context) {
    final s = live.stats;
    final gutter = Space.gutter(context);
    final inset = DesignTokens.of(context).cardPadding.left;
    String pct(double v) => '${v.round()} percent';
    LiveChart chart(List<double> values, MetricLevel? level) =>
        LiveChart(series: [ChartSeries(values)], capacity: history.capacity, max: 100, alert: alertColor(context, level), window: window, bleed: true, labelInset: inset);

    // The busiest of the percentage metrics, for a direction that
    // emphasises it (Tonal) - unless it is already alerting, when the
    // status word and colour say it instead.
    final memPct = s.memTotal > 0 ? s.memUsed / s.memTotal * 100 : 0.0;
    final loads = {'cpu': s.cpuPercent, 'memory': memPct, if (gpu != null) 'gpu': gpu!.utilizationPercent};
    final busiest = loads.entries.reduce((a, b) => b.value > a.value ? b : a).key;

    // CPU
    final cpuLevel = loadLevel(s.cpuPercent);
    final cpuFacts = [
      if (s.cpuTemperature != null) '${s.cpuTemperature!.toStringAsFixed(0)}\u00A0°C',
      if (s.cpuMhz > 0) '${(s.cpuMhz / 1000).toStringAsFixed(1)} GHz',
    ].join(' · ');
    final cpu = MetricCard(
      icon: Icons.memory_outlined,
      label: 'Processor',
      value: '${s.cpuPercent.round()}',
      unit: '%',
      level: cpuLevel,
      detail: cpuFacts.isEmpty ? null : cpuFacts,
      onTap: onOpenCpu,
      emphasized: busiest == 'cpu' && !cpuLevel.alerting,
      semanticLabel: 'Processor, ${pct(s.cpuPercent)}, ${cpuLevel.word.toLowerCase()} load. ${_range(history.cpu, pct)}',
      body: chart(history.cpu, cpuLevel),
    );

    // Memory
    final memLevel = memoryLevel(memPct);
    final memory = MetricCard(
      icon: Icons.developer_board_outlined,
      label: 'Memory',
      value: s.memTotal > 0 ? '${memPct.round()}' : '—',
      unit: s.memTotal > 0 ? '%' : null,
      level: memLevel,
      detail: s.memTotal > 0 ? usedOf(s.memUsed, s.memTotal) : 'Not reported',
      onTap: onOpenMemory,
      emphasized: busiest == 'memory' && !(memLevel?.alerting ?? false),
      semanticLabel: 'Memory, ${pct(memPct)} in use${memLevel == null ? '' : ', ${memLevel.word.toLowerCase()}'}. ${_range(history.memory, pct)}',
      body: chart(history.memory, memLevel),
    );

    // Network: download and upload side by side, each with its own chart
    // on its own scale - on one scale, upload is a flat line at the
    // bottom whenever download is busy.
    final rate = live.rate;
    final net = s.primaryNet;
    final (down, downUnit) = rate == null ? ('—', null) : splitUnit(formatBytes(rate.downBytesPerSec));
    final (up, upUnit) = rate == null ? ('—', null) : splitUnit(formatBytes(rate.upBytesPerSec));
    final network = MetricCard(
      icon: Icons.swap_vert,
      label: 'Network',
      value: down,
      meta: net == null || net.name.isEmpty ? null : '${net.name}${net.state.isEmpty ? '' : ' · ${net.state}'}',
      values: _NetValues(
        down: down,
        downUnit: downUnit,
        up: up,
        upUnit: upUnit,
        downSeries: history.netDown,
        upSeries: history.netUp,
        capacity: history.capacity,
        window: window,
      ),
      onTap: onOpenNetwork,
      semanticLabel: rate == null
          ? 'Network, measuring'
          : 'Network, download ${formatBytes(rate.downBytesPerSec)} per second, upload ${formatBytes(rate.upBytesPerSec)} per second',
      body: const SizedBox.shrink(),
    );

    // Storage: capacity changes too slowly for a line, so one thin bar per
    // drive, as the web UI's Disks widget shows them.
    final drives = s.dataDisks;
    final storeLevel = drives.isEmpty ? null : storageLevel(s.storageFraction);
    final storage = MetricCard(
      icon: Icons.storage_outlined,
      label: 'Storage',
      value: drives.isEmpty ? '—' : '${(s.storageFraction * 100).round()}',
      unit: drives.isEmpty ? null : '%',
      level: storeLevel,
      detail: drives.isEmpty
          ? 'No drives reported'
          : '${usedOf(s.storageUsed, s.storageTotal)} · ${drives.length == 1 ? '1 drive' : '${drives.length} drives'}',
      onTap: onOpenStorage,
      padBody: true,
      semanticLabel: drives.isEmpty
          ? 'Storage, no drives reported'
          : 'Storage, ${pct(s.storageFraction * 100)} used, ${formatBytes(s.storageUsed)} of ${formatBytes(s.storageTotal)} on ${drives.length} drives',
      body: MeterBars(
        items: [for (final d in drives.take(3)) (d.label, d.sizeKnown ? d.usedBytes / d.sizeBytes : 0.0)],
        warnAt: diskWarnAt,
        criticalAt: diskCriticalAt,
      ),
    );

    // Graphics, only when the server reports a GPU.
    final g = gpu;
    MetricCard? graphics;
    if (g != null) {
      final level = loadLevel(g.utilizationPercent, gpu: true);
      graphics = MetricCard(
        icon: Icons.videogame_asset_outlined,
        label: 'Graphics',
        value: '${g.utilizationPercent.round()}',
        unit: '%',
        level: level,
        detail: [
          if (g.temperatureC != null) '${g.temperatureC!.toStringAsFixed(0)}\u00A0°C',
          // One unbreakable phrase, so it wraps as a whole.
          if (g.memoryTotalMib > 0) 'VRAM ${usedOf(g.memoryUsedBytes, g.memoryTotalBytes)}'.replaceAll(' ', '\u00A0'),
        ].join(' · '),
        onTap: onOpenGpu,
        emphasized: busiest == 'gpu' && !level.alerting,
        semanticLabel: 'Graphics, ${g.name}, ${pct(g.utilizationPercent)}, ${level.word.toLowerCase()}. ${_range(history.gpu, pct)}',
        body: chart(history.gpu, level),
      );
    }

    return Padding(
      padding: EdgeInsets.symmetric(horizontal: gutter),
      child: LayoutBuilder(builder: (context, c) {
        final big = MediaQuery.textScalerOf(context).scale(10) > 13;
        final columns = big ? 1 : c.maxWidth < 560 ? 2 : c.maxWidth < 840 ? 3 : 4;
        final List<(int, Widget)> cards = switch (columns) {
          1 || 2 => [(1, cpu), (1, memory), (2, network), (1, storage), if (graphics != null) (1, graphics)],
          3 => [(1, cpu), (1, memory), if (graphics != null) (1, graphics), (2, network), (1, storage)],
          _ => [(1, cpu), (1, memory), if (graphics != null) (1, graphics), (1, storage), (2, network)],
        };
        return MetricGrid(columns: columns, children: cards);
      }),
    );
  }
}

/// Network's two readings side by side, each over its own chart with its
/// own scale, and the chart's peak named so the scale is readable.
class _NetValues extends StatelessWidget {
  const _NetValues({
    required this.down,
    required this.downUnit,
    required this.up,
    required this.upUnit,
    required this.downSeries,
    required this.upSeries,
    required this.capacity,
    required this.window,
  });

  final String down;
  final String? downUnit;
  final String up;
  final String? upUnit;
  final List<double> downSeries;
  final List<double> upSeries;
  final int capacity;
  final String window;

  static String _peak(List<double> v) => v.length < 2 ? '' : ' · peak\u00A0${formatBytes(v.reduce((a, b) => a > b ? a : b)).replaceAll(' ', '\u00A0')}/s';

  @override
  Widget build(BuildContext context) {
    final t = DesignTokens.of(context);
    final pad = t.cardPadding;
    Widget half(String name, String value, String? unit, List<double> series, {required bool first}) => Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: EdgeInsetsDirectional.only(start: first ? pad.left : Space.sm, end: first ? Space.sm : pad.right),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  MetricValue(value: value, unit: unit == null ? null : '$unit/s'),
                  const SizedBox(height: 2),
                  Text.rich(
                    TextSpan(children: [
                      TextSpan(text: name, style: t.data.copyWith(color: Theme.of(context).colorScheme.onSurface, fontWeight: FontWeight.w600)),
                      TextSpan(text: _peak(series), style: t.data),
                    ]),
                    maxLines: 2,
                  ),
                ],
              ),
            ),
            const SizedBox(height: Space.sm),
            Expanded(
              child: LiveChart(
                series: [ChartSeries(series)],
                capacity: capacity,
                window: window,
                bleed: true,
                labelInset: first ? pad.left : Space.sm,
              ),
            ),
          ],
        );
    return Row(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Expanded(child: half('Download', down, downUnit, downSeries, first: true)),
        VerticalDivider(width: 1, thickness: 1, color: Theme.of(context).colorScheme.outlineVariant.withValues(alpha: .6)),
        Expanded(child: half('Upload', up, upUnit, upSeries, first: false)),
      ],
    );
  }
}

/// Home's shape while it loads for the first time: the header and the
/// first four cards.
class _HomeSkeleton extends StatelessWidget {
  const _HomeSkeleton();

  @override
  Widget build(BuildContext context) {
    final gutter = Space.gutter(context);
    final t = DesignTokens.of(context);
    Widget card() => DecoratedBox(
          decoration: ShapeDecoration(color: t.cardColor, shape: t.cardShape()),
          child: const Padding(
            padding: EdgeInsets.all(Space.lg),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                FractionallySizedBox(widthFactor: 0.6, child: SkeletonBox(height: 14)),
                SizedBox(height: Space.lg),
                FractionallySizedBox(widthFactor: 0.4, child: SkeletonBox(height: 36)),
                SizedBox(height: Space.sm),
                FractionallySizedBox(widthFactor: 0.7, child: SkeletonBox(height: 12)),
                SizedBox(height: Space.lg),
                SkeletonBox(height: 44),
              ],
            ),
          ),
        );
    return Semantics(
      label: 'Loading',
      liveRegion: true,
      child: ExcludeSemantics(
        child: SkeletonPulse(
          child: Padding(
            padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const Row(
                  children: [
                    SkeletonBox(width: 48, height: 48, radius: 24),
                    SizedBox(width: Space.lg),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          FractionallySizedBox(widthFactor: 0.5, child: SkeletonBox(height: 24)),
                          SizedBox(height: Space.sm),
                          FractionallySizedBox(widthFactor: 0.7, child: SkeletonBox(height: 14)),
                        ],
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: Space.xl),
                for (var r = 0; r < 2; r++) ...[
                  if (r > 0) SizedBox(height: t.gap),
                  Row(children: [Expanded(child: card()), SizedBox(width: t.gap), Expanded(child: card())]),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}
