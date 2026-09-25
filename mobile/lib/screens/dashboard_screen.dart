import 'dart:async';
import 'dart:convert';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../models/dashboard_stats.dart';
import '../services/api_client.dart';
import '../ui/ui.dart';
import '../utils/format.dart';
import '../widgets/monitor_modals.dart';
import '../widgets/server_power.dart';
import 'system_updates_screen.dart';

/// The last few minutes of live readings (one per poll), for Home's
/// sparklines. Kept in memory only: the server has no history endpoint, so
/// the lines start when Home opens and say so until there are two points.
class LiveHistory {
  LiveHistory({this.capacity = 30});

  /// 30 readings at the 4 s poll: the last two minutes.
  final int capacity;
  final List<double> cpu = [];
  final List<double> memory = [];

  /// Download rate in bytes per second.
  final List<double> netDown = [];

  void _push(List<double> list, double v) {
    list.add(v);
    if (list.length > capacity) list.removeAt(0);
  }

  void add(LiveStats live) {
    final s = live.stats;
    _push(cpu, s.cpuPercent);
    _push(memory, s.memTotal > 0 ? s.memUsed / s.memTotal * 100 : 0);
    final rate = live.rate;
    if (rate != null) _push(netDown, rate.downBytesPerSec);
  }
}

/// Loads everything Home shows and keeps the live part fresh.
///
/// Two speeds: utilization every few seconds (and drives every 30 s) while
/// someone is looking, and the slow checks - updates, backups, apps, VMs,
/// host facts - once on open and on pull-to-refresh. Each slow check fails
/// on its own and just leaves its part out; only the live reading decides
/// whether Home shows an error or the offline banner.
class HomeController extends ChangeNotifier {
  HomeController({ApiClient? api}) : _api = api ?? ApiClient.instance;

  final ApiClient _api;

  /// The latest utilization reading, shared with the detail screens.
  final ValueNotifier<LiveStats?> live = ValueNotifier(null);

  /// Recent readings, for the sparklines.
  final LiveHistory history = LiveHistory();

  /// Why the first reading failed; null once there is data.
  Object? liveError;

  HostInfo? host;
  UpdateSummary? updates;
  List<BackupJobBrief> backups = const [];
  AppCounts? apps;

  /// Running and total VMs; null when the VM manager didn't answer.
  ({int running, int total})? vms;

  List<DiskUsage> _disks = const [];
  DateTime? _disksAt;
  NetSample? _lastNet;
  DateTime? _lastNetAt;
  bool _liveBusy = false;
  bool _disposed = false;

  static const disksEvery = Duration(seconds: 30);

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
    super.dispose();
  }

  /// One utilization reading (plus drives when they're due).
  Future<void> refreshLive({bool forceDisks = false}) async {
    if (_liveBusy) return;
    _liveBusy = true;
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

  /// Everything, as on open and pull-to-refresh.
  Future<void> refreshAll() async {
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
  Future<void> _loadVms() async {
    try {
      final res = await _api.getRaw('/v1/vm-sidecar/vms');
      if (res.statusCode != 200) {
        vms = null;
        return;
      }
      final list = jsonDecode(res.body);
      if (list is! List) return;
      final all = list.whereType<Map>().toList();
      vms = (running: all.where((v) => v['state'] == 'running').length, total: all.length);
    } catch (_) {
      vms = null;
    }
  }
}

/// Home: the server at a glance. What state it's in first, then what needs
/// the owner, then the health meters. Tabs and tools live in the
/// navigation bar and More, so none are repeated here.
class DashboardScreen extends StatefulWidget {
  final VoidCallback? onOpenFiles;
  final VoidCallback? onOpenVms;
  final VoidCallback? onOpenApps;

  /// Tests pass their own; the app makes one per screen.
  final HomeController? controller;

  /// How often the live reading refreshes.
  final Duration pollEvery;

  const DashboardScreen({
    super.key,
    this.onOpenFiles,
    this.onOpenVms,
    this.onOpenApps,
    this.controller,
    this.pollEvery = const Duration(seconds: 4),
  });

  @override
  State<DashboardScreen> createState() => _DashboardScreenState();
}

class _DashboardScreenState extends State<DashboardScreen> {
  late final HomeController _c = widget.controller ?? HomeController();
  Timer? _timer;

  // Detail screens pushed from here that still want live numbers while
  // Home itself is covered.
  int _detailsOpen = 0;

  @override
  void initState() {
    super.initState();
    _c.addListener(_changed);
    _c.refreshAll();
    _timer = Timer.periodic(widget.pollEvery, (_) => _tick());
  }

  @override
  void dispose() {
    _timer?.cancel();
    _c.removeListener(_changed);
    if (widget.controller == null) _c.dispose();
    super.dispose();
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  // Poll only while someone can see the numbers: Home is the visible tab
  // (a hidden IndexedStack child and a covered route have their tickers
  // off) or one of its detail screens is open, and the app is in front.
  void _tick() {
    if (!mounted) return;
    final lifecycle = WidgetsBinding.instance.lifecycleState;
    if (lifecycle != null && lifecycle != AppLifecycleState.resumed) return;
    if (!TickerMode.valuesOf(context).enabled && _detailsOpen == 0) return;
    _c.refreshLive();
  }

  Future<void> _openDetail(Widget screen) async {
    _detailsOpen++;
    await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => screen));
    _detailsOpen--;
  }

  void _openCpu() => _openDetail(CpuDetailScreen(live: _c.live, onRetry: _c.refreshLive));
  void _openMemory() => _openDetail(MemoryDetailScreen(live: _c.live, onRetry: _c.refreshLive));
  void _openStorage() => _openDetail(StorageDetailScreen(live: _c.live, onRetry: _c.refreshLive, onOpenFiles: widget.onOpenFiles));
  void _openNetwork() => _openDetail(NetworkDetailScreen(live: _c.live, onRetry: _c.refreshLive));

  Future<void> _openUpdates(UpdatesPage page) async {
    await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => SystemUpdatesScreen(page: page)));
    _c.loadUpdates();
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
      slivers = const [SliverToBoxAdapter(child: _SummarySkeleton())];
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

    return AppScaffold.slivers(
      title: 'Home',
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
    final s = live.stats;
    final attention = _c.attention;
    final drives = s.dataDisks;
    final apps = _c.apps;
    final vms = _c.vms;

    final panel = ServerPanel(
      attention: attention,
      host: _c.host,
      fallbackName: Uri.tryParse(ApiClient.instance.baseUrl)?.host ?? '',
      live: live,
      history: _c.history,
      onOpenCpu: _openCpu,
      onOpenMemory: _openMemory,
      onOpenNetwork: _openNetwork,
    );
    final needs = attention.isEmpty
        ? null
        : TileGroup(title: 'Needs attention', children: [for (final a in attention) _attentionTile(context, a)]);
    final storage = TileGroup(title: 'Storage', children: [
      UsageTile(
        icon: Icons.storage_outlined,
        bar: UsageBar(
          value: s.storageUsed.toDouble(),
          max: s.storageTotal.toDouble(),
          label: drives.length == 1 ? '1 drive' : '${drives.length} drives',
          detail: drives.isEmpty ? 'No drives reported' : '${formatBytes(s.storageUsed)} of ${formatBytes(s.storageTotal)} used',
          warnAt: diskWarnAt,
          criticalAt: diskCriticalAt,
        ),
        onTap: _openStorage,
      ),
    ]);
    final running = apps == null && vms == null
        ? null
        : TileGroup(title: 'Running', children: [
            if (apps != null)
              ListTile(
                leading: const Icon(Icons.apps_outlined),
                title: const Text('Apps'),
                subtitle: Text(apps.total == 0 ? 'None installed' : '${apps.running} of ${apps.total} running'),
                trailing: const Icon(Icons.chevron_right),
                onTap: widget.onOpenApps,
              ),
            if (vms != null)
              ListTile(
                leading: const Icon(Icons.computer_outlined),
                title: const Text('Virtual machines'),
                subtitle: Text(vms.total == 0
                    ? 'None set up'
                    : vms.running == 0
                        ? 'None running · ${vms.total} in total'
                        : '${vms.running} of ${vms.total} running'),
                trailing: const Icon(Icons.chevron_right),
                onTap: widget.onOpenVms,
              ),
          ]);

    return [
      LayoutBuilder(builder: (context, constraints) {
        // Wide windows: the server and what needs attention on the left,
        // storage and what's running on the right, instead of one long
        // column of stretched rows.
        if (constraints.maxWidth >= 720) {
          return Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(child: Column(children: [panel, ?needs])),
              Expanded(
                child: Padding(
                  padding: const EdgeInsets.only(top: Space.sm),
                  child: Column(children: [storage, ?running]),
                ),
              ),
            ],
          );
        }
        return Column(children: [panel, ?needs, storage, ?running]);
      }),
      const SizedBox(height: Space.lg),
    ];
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

/// The one-second answer, and Home's one expressive moment: which server
/// this is, whether it is fine, how long it has been up, and its live
/// processor, memory and network readings with the last two minutes as
/// sparklines. A tonal panel, so it reads as the thing the screen is about.
class ServerPanel extends StatelessWidget {
  const ServerPanel({
    super.key,
    required this.attention,
    required this.host,
    required this.fallbackName,
    required this.live,
    required this.history,
    this.onOpenCpu,
    this.onOpenMemory,
    this.onOpenNetwork,
  });

  final List<AttentionItem> attention;
  final HostInfo? host;
  final String fallbackName;
  final LiveStats live;
  final LiveHistory history;
  final VoidCallback? onOpenCpu;
  final VoidCallback? onOpenMemory;
  final VoidCallback? onOpenNetwork;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
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
    final name = (h?.hostname.isNotEmpty ?? false) ? h!.hostname : fallbackName;
    final facts = [
      if (h != null && h.osName.isNotEmpty) h.osName,
      if (h != null && h.uptime.isNotEmpty) 'Up ${h.uptime}',
    ];
    final s = live.stats;
    final rate = live.rate;
    final (netValue, netUnit) = rate == null ? ('—', null) : splitUnit(formatBytes(rate.downBytesPerSec));
    final gutter = Space.gutter(context);

    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.sm),
      child: Card.filled(
        color: scheme.surfaceContainerHigh,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Corners.extraLarge)),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(Space.lg, Space.lg, Space.lg, Space.sm),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Semantics(
                container: true,
                liveRegion: true,
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    StatusDisc(status: status, icon: icon),
                    const SizedBox(width: Space.lg),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Semantics(
                            header: true,
                            child: Text(name, style: theme.textTheme.headlineSmall?.emphasized, maxLines: 2, overflow: TextOverflow.ellipsis),
                          ),
                          const SizedBox(height: 2),
                          AnimatedSwitcher(
                            duration: Motion.of(context).short,
                            child: Text(verdict, key: ValueKey(verdict), style: theme.textTheme.titleSmall?.copyWith(color: scheme.onSurface)),
                          ),
                          for (final f in facts) ...[
                            const SizedBox(height: 2),
                            Text(f, style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
                          ],
                        ],
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: Space.md),
              _Vitals(children: [
                _Vital(
                  label: 'Processor',
                  value: '${s.cpuPercent.round()}',
                  unit: '%',
                  values: history.cpu,
                  max: 100,
                  onTap: onOpenCpu,
                ),
                _Vital(
                  label: 'Memory',
                  value: s.memTotal > 0 ? '${(s.memUsed / s.memTotal * 100).round()}' : '—',
                  unit: s.memTotal > 0 ? '%' : null,
                  values: history.memory,
                  max: 100,
                  onTap: onOpenMemory,
                ),
                _Vital(
                  label: 'Network in',
                  value: netValue,
                  unit: netUnit == null ? null : '$netUnit/s',
                  values: history.netDown,
                  onTap: onOpenNetwork,
                ),
              ]),
            ],
          ),
        ),
      ),
    );
  }
}

/// A status as a 48dp tonal disc with its icon: the colour says the
/// state, the icon says it again for anyone who can't see colour.
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

/// Three readings side by side, or stacked one per row when the text is
/// large (a column at 200% text holds about four characters).
class _Vitals extends StatelessWidget {
  const _Vitals({required this.children});

  final List<_Vital> children;

  @override
  Widget build(BuildContext context) {
    final stacked = MediaQuery.textScalerOf(context).scale(10) > 13;
    if (stacked) {
      return Column(children: [for (final c in children) c.asRow()]);
    }
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [for (final c in children) Expanded(child: c)],
    );
  }
}

class _Vital extends StatelessWidget {
  const _Vital({required this.label, required this.value, this.unit, required this.values, this.max, this.onTap, this.row = false});

  final String label;
  final String value;
  final String? unit;
  final List<double> values;
  final double? max;
  final VoidCallback? onTap;
  final bool row;

  _Vital asRow() => _Vital(label: label, value: value, unit: unit, values: values, max: max, onTap: onTap, row: true);

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final reading = Text.rich(
      TextSpan(children: [
        TextSpan(text: value, style: theme.textTheme.titleLarge?.emphasized.tabular),
        if (unit != null) TextSpan(text: unit == '%' ? unit : ' $unit', style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant)),
      ]),
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
    );
    final name = Text(label, style: theme.textTheme.labelMedium?.copyWith(color: scheme.onSurfaceVariant), maxLines: 1, overflow: TextOverflow.ellipsis);
    final spark = Sparkline(values: values, max: max, height: 28, width: double.infinity);
    final Widget content = row
        ? Row(
            children: [
              Expanded(child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [name, reading])),
              const SizedBox(width: Space.md),
              SizedBox(width: 96, child: spark),
            ],
          )
        : Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [name, const SizedBox(height: 2), reading, const SizedBox(height: Space.sm), spark],
          );
    return Semantics(
      button: onTap != null,
      label: '$label, $value${unit == null ? '' : ' $unit'}',
      excludeSemantics: true,
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(Corners.medium),
        child: ConstrainedBox(
          constraints: const BoxConstraints(minHeight: 48),
          child: Padding(padding: const EdgeInsets.symmetric(horizontal: Space.xs, vertical: Space.sm), child: content),
        ),
      ),
    );
  }
}

/// The panel's shape while Home loads for the first time.
class _SummarySkeleton extends StatelessWidget {
  const _SummarySkeleton();

  @override
  Widget build(BuildContext context) {
    final gutter = Space.gutter(context);
    final scheme = Theme.of(context).colorScheme;
    return Semantics(
      label: 'Loading',
      liveRegion: true,
      child: ExcludeSemantics(
        child: SkeletonPulse(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Padding(
                padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.sm),
                child: Card.filled(
                  color: scheme.surfaceContainerHigh,
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Corners.extraLarge)),
                  child: const Padding(
                    padding: EdgeInsets.all(Space.lg),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
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
                        SizedBox(height: Space.xl),
                        Row(
                          children: [
                            Expanded(child: _SkeletonVital()),
                            SizedBox(width: Space.lg),
                            Expanded(child: _SkeletonVital()),
                            SizedBox(width: Space.lg),
                            Expanded(child: _SkeletonVital()),
                          ],
                        ),
                      ],
                    ),
                  ),
                ),
              ),
              TileGroup(title: 'Storage', children: [SkeletonRow(subtitle: true)]),
            ],
          ),
        ),
      ),
    );
  }
}

class _SkeletonVital extends StatelessWidget {
  const _SkeletonVital();

  @override
  Widget build(BuildContext context) => const Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          FractionallySizedBox(widthFactor: 0.6, child: SkeletonBox(height: 12)),
          SizedBox(height: Space.sm),
          FractionallySizedBox(widthFactor: 0.4, child: SkeletonBox(height: 20)),
          SizedBox(height: Space.sm),
          SkeletonBox(height: 28),
        ],
      );
}
