import 'dart:async';
import 'dart:convert';

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import '../models/dashboard_stats.dart';
import '../models/gpu_stats.dart';
import 'fans/fans_screen.dart';
import '../services/api_client.dart';
import '../services/background_service.dart';
import '../services/download_station_api.dart';
import '../services/tailscale_service.dart';
import '../services/utilization_feed.dart';
import '../services/vm_client.dart';
import '../services/widget_refresh.dart';
import '../ui/ui.dart';
import '../utils/format.dart';
import '../widgets/free_memory_sheet.dart';
import '../widgets/monitor_modals.dart';
import '../widgets/server_power.dart';
import '../widgets/vm_console_preview.dart';
import 'server_health_screen.dart';
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
  HomeController({ApiClient? api, VmClient? vmClient, Future<SharingBrief?> Function()? sharing})
      : _api = api ?? ApiClient.instance,
        _vm = vmClient,
        _sharing = sharing ?? _phoneSharing;

  final ApiClient _api;
  final VmClient? _vm;
  final Future<SharingBrief?> Function() _sharing;

  // This phone's storage sharing; nothing to say off Android.
  static Future<SharingBrief?> _phoneSharing() async {
    if (!BackgroundService.isAndroid) return null;
    final s = await BackgroundService.instance.sharingStatus();
    return SharingBrief(running: s.running, stopReason: s.lastStopReason);
  }
  VmClient get vmClient => _vm ?? VmClient();

  /// The latest utilization reading, shared with the detail screens.
  final ValueNotifier<LiveStats?> live = ValueNotifier(null);

  /// Recent readings, for the metric cards' charts.
  final LiveHistory history = LiveHistory();

  /// The GPU's latest reading; null when the server has no dedicated GPU
  /// (or its sidecar isn't answering), and then Home shows no GPU card.
  final ValueNotifier<GpuStats?> gpu = ValueNotifier(null);

  // Whether the server has a GPU worth polling; pull-to-refresh asks again.
  final GpuPresence _gpuPresence = GpuPresence();

  /// Why the first reading failed; null once there is data.
  Object? liveError;

  HostInfo? host;
  UpdateSummary? updates;
  List<BackupJobBrief> backups = const [];

  /// False when Backup & Sync isn't installed, null when it couldn't be
  /// asked.
  bool? backupsInstalled = false;
  AppCounts? apps;

  /// Each drive's own health; null when the drive list couldn't be read.
  List<DriveHealth>? drives;

  /// Drives that should be mounted but aren't (or are being repaired).
  List<MountProblem> mountProblems = const [];

  /// The server's Tailscale; null when it couldn't be asked.
  TailnetState? tailnet;
  String tailnetIp = '';
  SharingBrief? sharing;

  /// Download Station's failed downloads; 0 when it isn't installed.
  int failedDownloads = 0;

  /// When the slow checks (updates, backups, apps, drives, Tailscale) last
  /// answered, for the Server health page's "Checked 2 min ago".
  DateTime? checkedAt;

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
  DateTime? _fullAt;
  bool _liveBusy = false;
  bool _gpuBusy = false;
  bool _vmsBusy = false;

  // The last polled utilization: what bus readings lack (CPU model and
  // clock, memory modules) comes from it.
  Map<String, dynamic> _lastUtil = const {};

  /// The newest VM list request ([_loadVms]).
  int _vmsSeq = 0;
  bool _disposed = false;

  static const disksEvery = WidgetRefresh.drivesEvery;

  /// How many times everything was loaded (open, pull-to-refresh), so the
  /// console preview can take a new picture with it.
  int refreshes = 0;

  /// Every check (server_health.dart): the one source for Home's status
  /// header, its "Needs attention" group and the Server health page.
  ServerHealth get health => buildHealth(
        stats: live.value?.stats,
        updates: updates,
        backups: backups,
        backupsInstalled: backupsInstalled,
        apps: apps,
        drives: drives,
        tailnet: tailnet,
        tailnetIp: tailnetIp,
        sharing: sharing,
        failedDownloads: failedDownloads,
        mountProblems: mountProblems,
      );

  List<AttentionItem> get attention => health.attention;

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

  /// One utilization reading (plus drives when they're due). Skipped
  /// while the last one is still on its way.
  Future<void> refreshLive({bool forceDisks = false}) => _read(forceDisks: forceDisks);

  /// While the bus streams: refreshes only what its readings build on
  /// ([applyBusReading]) - the next of them shows it, and a polled reading
  /// between two pushed ones would skew the network rate.
  Future<void> refreshBase() => _read(show: false);

  Future<void> _read({bool forceDisks = false, bool show = true}) async {
    if (_liveBusy) return;
    _liveBusy = true;
    unawaited(pollGpu());
    try {
      final util = await _api.get('/sys/utilization');
      final data = util['data'] as Map<String, dynamic>? ?? const {};
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
      if (_disposed) return;
      _lastUtil = data;
      _fullAt = now;
      if (!show) return;
      _apply(data, now);
    } catch (e) {
      // The bus's readings say whether the server answers.
      if (_disposed || !show) return;
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

  /// A full reading (and the drives) is due even while the bus streams:
  /// the bus leaves out the drives, CPU model and memory modules.
  bool get fullReadingDue => _fullAt == null || clock.now().difference(_fullAt!) >= disksEvery;

  /// One reading from the message bus ([UtilizationFeed]): its CPU,
  /// memory and network over the last polled one.
  void applyBusReading(Map<String, String> properties) {
    Object? field(String k) {
      try {
        return jsonDecode(properties[k] ?? '');
      } catch (_) {
        return null;
      }
    }

    final cpu = field('sys_cpu'), mem = field('sys_mem'), net = field('sys_net');
    if (_disposed || cpu is! Map || mem is! Map) return;
    Map<String, dynamic> over(Object? base, Map top) => {if (base is Map) ...base.cast<String, dynamic>(), ...top.cast<String, dynamic>()};
    _apply({'cpu': over(_lastUtil['cpu'], cpu), 'mem': over(_lastUtil['mem'], mem), 'net': net is List ? net : _lastUtil['net']}, clock.now());
    _notify();
  }

  void _apply(Map<String, dynamic> data, DateTime now) {
    final stats = DashboardStats.fromUtilization(data).withDisks(_disks);
    final net = stats.primaryNet;
    final rate = NetRate.between(_lastNet, _lastNetAt, net, now) ?? (net?.name == _lastNet?.name ? live.value?.rate : null);
    _lastNet = net;
    _lastNetAt = now;
    final reading = LiveStats(stats: stats, rate: rate, updatedAt: now);
    live.value = reading;
    history.add(reading);
    liveError = null;
  }

  // The GPU sidecar, through the gateway like the web UI's GPU widget. It
  // answers raw JSON (no envelope), and an error or no name on a machine
  // without a dedicated GPU.
  Future<void> pollGpu() async {
    if (_gpuBusy || !_gpuPresence.worthPolling) return;
    _gpuBusy = true;
    try {
      final res = await _api.getRaw('/v1/gpu/gpu-stats');
      final g = res.statusCode == 200 ? GpuStats.tryParse(jsonDecode(res.body)) : null;
      if (_disposed) return;
      gpu.value = _gpuPresence.record(g, gpu.value);
      // A stale reading repeats old numbers; the chart shouldn't draw them as new.
      if (g != null && !g.stale) history.addGpu(g);
    } catch (_) {
      // Keep the last reading; the next poll asks again.
    } finally {
      _gpuBusy = false;
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
    _gpuPresence.reset();
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
      _loadDrives(),
      loadMountProblems(),
      _loadTailscale(),
      _loadSharing(),
      _loadDownloads(),
    ]);
    if (live.value != null && !live.value!.stale) checkedAt = clock.now();
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

  // Download Station is optional too: no answer, nothing to say.
  Future<void> _loadDownloads() async {
    try {
      failedDownloads = (await DownloadStationApi(_api).downloads()).where((d) => d.state == DsState.failed).length;
    } catch (_) {
      failedDownloads = 0;
    }
  }

  // Backup & Sync is optional: ask its health route first and show nothing
  // about backups when it doesn't answer (plan M-26).
  Future<void> _loadBackups() async {
    try {
      final health = await _api.get('/backup/health');
      if (health['installed'] == false) {
        backups = const [];
        backupsInstalled = false;
        return;
      }
    } catch (_) {
      // No answer: the module isn't installed (or its route is gone).
      backups = const [];
      backupsInstalled = false;
      return;
    }
    try {
      final res = await _api.get('/backup/jobs');
      backups = (res['data'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map((e) => BackupJobBrief.fromJson(Map<String, dynamic>.from(e)))
          .toList();
      backupsInstalled = true;
    } catch (_) {
      // Installed but its jobs couldn't be read: say so rather than
      // showing no backups as if all were fine.
      backups = const [];
      backupsInstalled = null;
    }
  }

  // Each drive's SMART health, as the server last read it (it caches the
  // reading and doesn't wake a sleeping drive for it).
  Future<void> _loadDrives() async {
    try {
      final res = await _api.get('/disks');
      drives = DriveHealth.fromDisksApi(res['data']);
    } catch (_) {
      drives = null;
    }
  }

  Future<void> loadMountProblems() async {
    try {
      final res = await _api.get('/storage/fstab');
      mountProblems = MountProblem.fromFstabApi(res['data']);
    } catch (_) {
      // Older server, or not reachable: nothing known to be wrong.
      mountProblems = const [];
    }
    _notify();
  }

  /// Admin only (the server says so otherwise): repairs a drive that
  /// isn't mounted, in the background, then mounts it.
  Future<void> repairDrive(String mountPoint) async {
    await _api.post('/storage/fstab/repair', body: {'mount_point': mountPoint});
    await loadMountProblems();
  }

  Future<void> _loadTailscale() async {
    try {
      final res = await _api.get('/tailscale/status');
      final s = TailscaleStatus.fromJson(res['data'] as Map<String, dynamic>? ?? const {});
      var state = s.state;
      if (state == TailscaleState.noDaemon) {
        try {
          final inst = await _api.get('/tailscale/installed');
          final d = inst['data'];
          if (d is Map && d['installed'] == false) state = TailscaleState.notInstalled;
        } catch (_) {}
      }
      tailnetIp = s.selfIp;
      tailnet = switch (state) {
        TailscaleState.running => TailnetState.connected,
        TailscaleState.starting => TailnetState.connecting,
        TailscaleState.needsLogin => TailnetState.signedOut,
        TailscaleState.stopped || TailscaleState.noDaemon => TailnetState.off,
        TailscaleState.notInstalled => TailnetState.notInstalled,
      };
    } catch (_) {
      tailnet = null;
    }
  }

  /// Tailscale again, after its sheet closes.
  Future<void> recheckTailscale() async {
    await _loadTailscale();
    _notify();
  }

  /// This phone's sharing again, after Companion devices closes.
  Future<void> recheckSharing() async {
    await _loadSharing();
    _notify();
  }

  Future<void> _loadSharing() async {
    try {
      sharing = await _sharing();
    } catch (_) {
      sharing = null;
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
  //
  // A poll, a pull-to-refresh and the reload after a shutdown can overlap;
  // only the answer to the newest request is kept, so an older list that
  // arrives late can't overwrite a newer one.
  Future<void> _loadVms({bool keepOnError = false}) async {
    _vmsAt = clock.now();
    final seq = ++_vmsSeq;
    try {
      final res = await _api.getRaw('/v1/vm-sidecar/vms');
      if (seq != _vmsSeq) return;
      if (res.statusCode != 200) {
        if (!keepOnError) vmList = null;
        return;
      }
      final list = jsonDecode(res.body);
      if (list is! List) return;
      vmList = list.whereType<Map<String, dynamic>>().map(Vm.fromJson).toList();
    } catch (_) {
      if (seq == _vmsSeq && !keepOnError) vmList = null;
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

  /// Makes the real-time feed; tests pass one with a fake socket.
  final UtilizationFeed Function(void Function(Map<String, String>) onReading)? feed;

  const DashboardScreen({
    super.key,
    this.onOpenFiles,
    this.onOpenVms,
    this.onOpenApps,
    this.controller,
    this.refresh,
    this.feed,
  });

  @override
  State<DashboardScreen> createState() => _DashboardScreenState();
}

class _DashboardScreenState extends State<DashboardScreen> {
  late final HomeController _c = widget.controller ?? HomeController();
  late final ValueListenable<WidgetRefresh> _refresh = widget.refresh ?? WidgetRefreshController.instance;
  late final UtilizationFeed _feed = widget.feed?.call(_c.applyBusReading) ?? UtilizationFeed(onReading: _c.applyBusReading);
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
    _feed.close();
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
    if (_refresh.value != WidgetRefresh.live) _feed.close();
    _timer?.cancel();
    _timer = every == null ? null : Timer.periodic(every, (_) => _tick());
    _c.history.retime(every);
  }

  // Poll only while someone can see the numbers: Home is the visible tab
  // (a hidden IndexedStack child and a covered route have their tickers
  // off) or one of its detail screens is open, and the app is in front.
  //
  // In real time the readings come over the bus while it streams, and
  // this tick only keeps the socket wanted, polls the GPU (not on the bus)
  // and takes a full reading every 30 s; until the bus streams it polls
  // every second like "1 second".
  void _tick() {
    if (!mounted) return;
    final lifecycle = WidgetsBinding.instance.lifecycleState;
    final visible = TickerMode.valuesOf(context).enabled;
    final watched = (lifecycle == null || lifecycle == AppLifecycleState.resumed) && (visible || _detailsOpen > 0);
    final live = _refresh.value == WidgetRefresh.live;
    if (live) _feed.want(watched);
    if (!watched) return;
    if (live && _feed.streaming) {
      _c.pollGpu();
      if (_c.fullReadingDue) _c.refreshBase();
    } else {
      _c.refreshLive();
    }
    // The VM rows (and whether there is one to preview) only matter on
    // Home itself.
    if (visible) _c.pollVms();
  }

  Future<void> _openDetail(Widget screen) async {
    _detailsOpen++;
    await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => screen));
    _detailsOpen--;
  }

  void _openCpu() => _openDetail(CpuDetailScreen(live: _c.live, onRetry: _c.refreshLive, history: _c.history, onOpenFans: _openFans));
  void _openMemory() => _openDetail(MemoryDetailScreen(live: _c.live, onRetry: _c.refreshLive, history: _c.history));
  void _freeMemory() => showFreeMemorySheet(context, swapUsed: _c.live.value?.stats.swapUsed ?? 0, onDone: _c.refreshLive);
  void _openStorage() => _openDetail(StorageDetailScreen(live: _c.live, onRetry: _c.refreshLive, onOpenFiles: widget.onOpenFiles));
  void _openNetwork() => _openDetail(NetworkDetailScreen(live: _c.live, onRetry: _c.refreshLive, history: _c.history));
  void _openGpu() => _openDetail(GpuDetailScreen(gpu: _c.gpu, history: _c.history, onOpenFans: _openFans));
  void _openFans() => _openDetail(const FansScreen());

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

  late final HealthActions _actions = HealthActions(
    controller: _c,
    pushDetail: _openDetail,
    onOpenApps: widget.onOpenApps,
    onOpenFiles: widget.onOpenFiles,
  );

  void _openHealth() => _openDetail(ServerHealthScreen(controller: _c, actions: _actions, onOpenFans: _openFans));

  Future<void> _power(String state) => confirmServerPower(context, restart: state == 'restart');

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
    final health = _c.health;
    final attention = health.attention;
    final apps = _c.apps;
    final vms = _c.vmList;

    final header = ServerHeader(health: health, host: _c.host, onTap: _openHealth);
    final metrics = MetricCards(
      live: live,
      history: _c.history,
      gpu: _c.gpu.value,
      window: _c.history.window,
      onOpenCpu: _openCpu,
      onOpenMemory: _openMemory,
      onFreeMemory: _freeMemory,
      onOpenNetwork: _openNetwork,
      onOpenStorage: _openStorage,
      onOpenGpu: _openGpu,
    );
    final needs = attention.isEmpty
        ? null
        : TileGroup(title: 'Needs attention', children: [
            for (final a in attention) AttentionTile(item: a, security: _c.updates?.security ?? 0, onTap: () => _actions.open(context, a)),
          ]);
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
        subtitle: vms.isEmpty
            ? const Text('Create one on the VMs tab')
            : FactLine.plain([
                if (active.length > 4) '${active.length - 4} more running',
                if (idle > 0) idle == 1 ? '1 turned off' : '$idle turned off',
                if (idle == 0 && active.length <= 4) '${vms.length} in total',
              ]),
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
    ];
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
            if (spec.isNotEmpty) FactLine.plain(spec, style: t.data),
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
}

/// The one-second answer under the server's name (the app bar's title):
/// whether it is fine, what it runs and how long it has been up. The whole
/// row opens Server health, where the answer is explained.
///
/// The icon says the state without the colour: a check (calm) when all is
/// good, a triangle (amber) for warnings, a circled "!" (red) for
/// problems, an "i" for things worth a look - with how many on a badge.
///
/// In a direction with a status panel (Tonal) this sits on a tonal panel
/// whose colour is the health itself: the status container when
/// something needs attention, a neutral container when all is clear.
class ServerHeader extends StatelessWidget {
  const ServerHeader({super.key, required this.health, required this.host, this.onTap});

  final ServerHealth health;
  final HostInfo? host;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final t = DesignTokens.of(context);
    final status = healthStatus(health.worst);
    final n = health.attention.length;
    final verdict = health.verdict;
    final icon = healthIcon(status);
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

    final Widget glyph = panel == null ? StatusDisc(status: status, icon: icon, size: 40) : Icon(icon, size: 28, color: calm ? tone.color : on);
    final Widget badged = n == 0
        ? glyph
        : Badge(
            label: Text('$n'),
            backgroundColor: tone.color,
            textColor: tone.onColor,
            offset: panel == null ? const Offset(2, -2) : const Offset(6, -6),
            child: glyph,
          );

    final Widget row = Row(
      crossAxisAlignment: CrossAxisAlignment.center,
      children: [
        badged,
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
                FactLine.plain(facts, style: theme.textTheme.bodySmall?.copyWith(color: muted)),
              ],
            ],
          ),
        ),
        if (onTap != null) ...[
          const SizedBox(width: Space.sm),
          Icon(Icons.chevron_right, color: muted),
        ],
      ],
    );

    // One button for TalkBack: "Server health, 3 things need attention,
    // Debian 12, Up 4 days".
    Widget semantics(Widget child) => Semantics(
          container: true,
          liveRegion: true,
          button: onTap != null,
          onTap: onTap,
          label: ['Server health', verdict, ...facts].join(', '),
          onTapHint: onTap == null ? null : 'see what was checked',
          child: ExcludeSemantics(child: child),
        );

    final radius = BorderRadius.circular(t.cardRadius);
    if (panel == null) {
      // No panel: the row itself is the target, its ripple as wide as
      // the content and rounded like a card.
      return Padding(
        padding: EdgeInsets.fromLTRB(gutter - Space.sm, Space.xs, gutter - Space.sm, Space.md),
        child: semantics(Material(
          type: MaterialType.transparency,
          child: InkWell(
            onTap: onTap,
            borderRadius: radius,
            child: Padding(padding: const EdgeInsets.all(Space.sm), child: row),
          ),
        )),
      );
    }
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, t.gap),
      child: semantics(Material(
        color: panel,
        shape: RoundedRectangleBorder(borderRadius: radius),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onTap,
          child: Padding(padding: const EdgeInsets.symmetric(horizontal: Space.lg, vertical: Space.md), child: row),
        ),
      )),
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
    this.onFreeMemory,
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

  /// The Memory card's "Free up" button; null leaves it out.
  final VoidCallback? onFreeMemory;
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
      action: onFreeMemory == null || s.memTotal <= 0
          ? null
          : MetricCardAction(icon: Icons.cleaning_services_outlined, tooltip: 'Free up memory', onPressed: onFreeMemory!),
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
        // A plain gap keeps the two charts apart; a rule between them read as clutter.
        const SizedBox(width: Space.md),
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
