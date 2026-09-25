import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import '../models/dashboard_stats.dart';
import '../models/gpu_stats.dart';
import '../services/speedtest_service.dart';
import '../ui/ui.dart';
import '../utils/format.dart';

// The detail screens behind Home's health rows: processor, memory, storage
// and network. Each one follows the same live reading Home polls (a
// [ValueListenable] of [LiveStats]), so its numbers keep moving while it's
// open, and shows the offline banner over the last reading when the server
// stops answering.

/// The last few minutes of live readings (one per poll), for Home's metric
/// cards and the detail pages' charts. Kept in memory only: the server has
/// no history endpoint, so the lines start when Home opens and say so
/// until there are two points.
class LiveHistory {
  LiveHistory({this.capacity = 31});

  /// The span the charts cover, whatever the refresh interval: at the
  /// default 4 s that is 31 readings.
  static const span = Duration(minutes: 2);

  /// How many readings the charts hold; [retime] keeps it at [span].
  int capacity;

  /// The time between two readings; null when Home only refreshes on
  /// pull-to-refresh (then [capacity] stays as it was).
  Duration? every = const Duration(seconds: 4);

  final List<double> cpu = [];
  final List<double> memory = [];

  /// Download and upload rates in bytes per second.
  final List<double> netDown = [];
  final List<double> netUp = [];

  /// GPU load in percent, while the server reports a GPU.
  final List<double> gpu = [];

  /// Follows a new refresh interval: the charts keep covering [span], so
  /// the number of readings changes (61 at 2 s, 3 at 1 min), and the
  /// oldest readings beyond it are dropped.
  void retime(Duration? every) {
    this.every = every;
    if (every == null) return;
    capacity = math.max(span.inMilliseconds ~/ every.inMilliseconds + 1, 3);
    for (final list in [cpu, memory, netDown, netUp, gpu]) {
      if (list.length > capacity) list.removeRange(0, list.length - capacity);
    }
  }

  /// What the charts span, for their labels: "2 min"; "30 refreshes" when
  /// readings only come on pull-to-refresh.
  String get window {
    final e = every;
    if (e == null) return '${capacity - 1} refreshes';
    final s = e.inMilliseconds * (capacity - 1) / 1000;
    return s >= 90 ? '${(s / 60).round()} min' : '${s.round()} s';
  }

  void _push(List<double> list, double v) {
    list.add(v);
    if (list.length > capacity) list.removeAt(0);
  }

  void add(LiveStats live) {
    final s = live.stats;
    _push(cpu, s.cpuPercent);
    _push(memory, s.memTotal > 0 ? s.memUsed / s.memTotal * 100 : 0);
    final rate = live.rate;
    if (rate != null) {
      _push(netDown, rate.downBytesPerSec);
      _push(netUp, rate.upBytesPerSec);
    }
  }

  void addGpu(GpuStats g) => _push(gpu, g.utilizationPercent);
}

/// How busy a processor (or GPU) is, in the web UI's words: light below
/// 30%, moderate below 75%, then heavy; from 90% it is a warning.
MetricLevel loadLevel(double percent, {bool gpu = false}) => switch (percent) {
  < 5 when gpu => const MetricLevel('Idle'),
  < 30 => const MetricLevel('Light'),
  < 75 => const MetricLevel('Moderate'),
  < 90 => const MetricLevel('Heavy'),
  _ => const MetricLevel('Maxed out', Status.warning),
};

/// Memory only gets a word when it is running out.
MetricLevel? memoryLevel(double percent) => switch (percent) {
  < 85 => null,
  < 95 => const MetricLevel('Running low', Status.warning),
  _ => const MetricLevel('Almost full', Status.error),
};

/// Storage, on the same thresholds as the drives' warnings.
MetricLevel? storageLevel(double fraction) => fraction >= diskCriticalAt
    ? const MetricLevel('Almost full', Status.error)
    : fraction >= diskWarnAt
    ? const MetricLevel('Filling up', Status.warning)
    : null;

/// The colour a chart line turns when its reading is a warning; null
/// otherwise (the direction's line colour).
Color? alertColor(BuildContext context, MetricLevel? level) => level == null || !level.alerting ? null : StatusColors.toneOf(context, level.status).color;

/// "0.3 of 11 GB" / "1.6 of 6.8 TB": used of total, one unit when both
/// share it.
String usedOf(int used, int total) {
  final (u, uu) = splitUnit(formatSize(used));
  final (t, tu) = splitUnit(formatSize(total));
  return uu == tu ? '$u of $t ${tu ?? ''}'.trim() : '${formatSize(used)} of ${formatSize(total)}';
}

/// A detail page's lead: the reading now, the low, average and high over
/// the chart's span as the chart's header, and a large chart of the last
/// minutes with a scale and a 25/50/75 grid.
///
/// A percentage chart ([max] 100) zooms to the smallest of 25, 50 or 100%
/// that holds its readings, and its scale says so, so a quiet processor
/// isn't a flat line under three quarters of empty chart. [second] adds a
/// smaller chart with its own scale (network upload under download).
class HistoryPanel extends StatelessWidget {
  const HistoryPanel({
    super.key,
    required this.label,
    required this.value,
    this.unit,
    this.level,
    this.detail,
    required this.series,
    required this.capacity,
    required this.window,
    this.max,
    this.threshold,
    required this.format,
    this.second,
  });

  final String label;
  final String value;
  final String? unit;
  final MetricLevel? level;

  /// Facts after the level word ("13.1 of 31.3 GB").
  final String? detail;
  final List<ChartSeries> series;
  final int capacity;
  final String window;
  final double? max;
  final double? threshold;

  /// Formats a reading for the scale and the low/average/high line.
  final String Function(double) format;

  /// (label, value, unit, readings) of a second chart on its own scale.
  final (String, String, String?, List<double>)? second;

  static double? _ceiling(double? max, List<double> v) {
    if (max != 100 || v.isEmpty) return max;
    final peak = v.reduce((a, b) => a > b ? a : b);
    for (final c in const [25.0, 50.0]) {
      if (peak <= c * .9) return c;
    }
    return 100;
  }

  @override
  Widget build(BuildContext context) {
    final t = DesignTokens.of(context);
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final gutter = Space.gutter(context);
    final main = series.first.values;
    final ceiling = _ceiling(max, main);
    final top = ceiling ?? (main.isEmpty ? 1.0 : main.reduce((a, b) => a > b ? a : b) * 1.15);
    final enough = main.length >= 2;
    final lo = enough ? main.reduce((a, b) => a < b ? a : b) : 0.0;
    final hi = enough ? main.reduce((a, b) => a > b ? a : b) : 0.0;
    final avg = enough ? main.reduce((a, b) => a + b) / main.length : 0.0;
    final stats = enough ? 'Low ${format(lo)}, average ${format(avg)}, high ${format(hi)}' : 'Collecting readings';
    final lvl = level;

    Widget stat(String name, double v) => Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(name, style: t.data),
        Text(format(v), style: text.titleMedium?.copyWith(color: scheme.onSurface).tabular),
      ],
    );
    Widget times() => Row(
      children: [
        Text('−$window', style: t.chartLabel),
        const Spacer(),
        Text('now', style: t.chartLabel),
      ],
    );

    final sec = second;
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.sm),
      child: Material(
        color: t.cardColor,
        shape: t.cardShape(),
        child: Padding(
          padding: const EdgeInsets.all(Space.lg),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(label, style: t.cardLabel),
              const SizedBox(height: Space.sm),
              Semantics(
                liveRegion: true,
                child: MetricValue(value: value, unit: unit, style: t.detailValue),
              ),
              if (lvl != null || detail != null)
                Text.rich(
                  TextSpan(
                    children: [
                      if (lvl != null)
                        TextSpan(
                          text: lvl.word,
                          style: t.data.copyWith(color: lvl.alerting ? StatusColors.toneOf(context, lvl.status).color : scheme.onSurface, fontWeight: FontWeight.w600),
                        ),
                      if (lvl != null && detail != null) TextSpan(text: ' · ', style: t.data),
                      if (detail != null) TextSpan(text: detail, style: t.data),
                    ],
                  ),
                ),
              const SizedBox(height: Space.lg),
              // The chart's header: what it spans, in three numbers.
              ExcludeSemantics(
                child: enough ? Wrap(spacing: Space.xl, runSpacing: Space.sm, children: [stat('Low', lo), stat('Average', avg), stat('High', hi)]) : Text('Collecting readings', style: t.data),
              ),
              const SizedBox(height: Space.md),
              LiveChart(
                series: series,
                capacity: capacity,
                max: ceiling,
                threshold: threshold,
                alert: alertColor(context, lvl),
                height: 160,
                grid: true,
                // The middle label only where it is a round number (not 12.5%).
                axisLabels: [(0, format(0)), if (ceiling == null || (top / 2) % 1 == 0) (top / 2, format(top / 2)), (top, format(top))],
                semanticLabel: '$label over the last $window. $stats.',
              ),
              const SizedBox(height: Space.xs),
              times(),
              if (sec != null) ...[
                const SizedBox(height: Space.lg),
                Divider(height: 1, color: scheme.outlineVariant),
                const SizedBox(height: Space.lg),
                Text(sec.$1, style: t.cardLabel),
                const SizedBox(height: Space.xs),
                MetricValue(value: sec.$2, unit: sec.$3, style: t.heroValue),
                const SizedBox(height: Space.md),
                Builder(
                  builder: (context) {
                    final v = sec.$4;
                    final top2 = v.isEmpty ? 1.0 : v.reduce((a, b) => a > b ? a : b) * 1.15;
                    return LiveChart(
                      series: [ChartSeries(v)],
                      capacity: capacity,
                      height: 96,
                      grid: true,
                      axisLabels: [(0, format(0)), (top2, format(top2))],
                      semanticLabel: v.length < 2 ? '${sec.$1}: collecting readings.' : '${sec.$1} over the last $window: high ${format(v.reduce((a, b) => a > b ? a : b))}.',
                    );
                  },
                ),
                const SizedBox(height: Space.xs),
                times(),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// "13.1 GB" -> ("13.1", "GB"), for MetricRow's value and unit.
(String, String?) splitUnit(String formatted) {
  final i = formatted.lastIndexOf(' ');
  if (i <= 0) return (formatted, null);
  return (formatted.substring(0, i), formatted.substring(i + 1));
}

/// A MetricRow for a byte count ("13.1 GB").
MetricRow bytesRow({required IconData icon, required String label, required int bytes, String? supporting}) {
  final (value, unit) = splitUnit(formatBytes(bytes));
  return MetricRow(icon: icon, label: label, value: value, unit: unit, supporting: supporting);
}

/// A MetricRow for a rate in bytes per second; "—" while unknown.
MetricRow rateRow({required IconData icon, required String label, required double? bytesPerSec, String? supporting}) {
  if (bytesPerSec == null) {
    return MetricRow(icon: icon, label: label, value: '—', supporting: supporting ?? 'Measuring');
  }
  final (value, unit) = splitUnit(formatBytes(bytesPerSec));
  return MetricRow(icon: icon, label: label, value: value, unit: '${unit ?? 'B'}/s', supporting: supporting);
}

/// "289" / "41.5" / "3.2" - fewer decimals as numbers grow.
String formatMbps(double v) => v >= 100 ? v.toStringAsFixed(0) : v.toStringAsFixed(1);
String formatMs(double v) => v >= 10 ? v.toStringAsFixed(0) : v.toStringAsFixed(1);

/// A [UsageBar] as a list row, lined up with ListTile rows: a leading
/// icon on the same 16dp column, the bar where a title would start, and a
/// chevron when it opens something.
class UsageTile extends StatelessWidget {
  const UsageTile({super.key, this.icon, required this.bar, this.onTap});

  /// Null for rows in a group of like items (per-thread load), where one
  /// icon repeated on every row would only add noise.
  final IconData? icon;
  final UsageBar bar;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final padding = ListTileTheme.of(context).contentPadding?.resolve(Directionality.of(context)) ?? const EdgeInsets.symmetric(horizontal: Space.lg);
    return MergeSemantics(
      child: Semantics(
        button: onTap != null,
        child: InkWell(
          onTap: onTap,
          child: ConstrainedBox(
            constraints: const BoxConstraints(minHeight: 72),
            child: Padding(
              padding: EdgeInsets.fromLTRB(padding.left, Space.md, onTap == null ? padding.right : Space.sm, Space.md),
              child: Row(
                children: [
                  if (icon != null) ...[ExcludeSemantics(child: Icon(icon, color: scheme.onSurfaceVariant)), const SizedBox(width: Space.lg)],
                  Expanded(child: bar),
                  if (onTap != null) ...[const SizedBox(width: Space.xs), ExcludeSemantics(child: Icon(Icons.chevron_right, color: scheme.onSurfaceVariant))],
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// A fact as a settings-style row: what it is, and its value under it.
class FactTile extends StatelessWidget {
  const FactTile({super.key, required this.icon, required this.label, required this.value});

  final IconData icon;
  final String label;
  final String value;

  @override
  Widget build(BuildContext context) => MergeSemantics(
    child: ListTile(leading: Icon(icon), title: Text(label), subtitle: Text(value.isEmpty ? 'Unknown' : value)),
  );
}

/// The frame every detail screen shares: the live reading, the offline
/// banner, and "waiting for the first reading".
class _LiveScaffold extends StatelessWidget {
  const _LiveScaffold({required this.title, required this.live, required this.onRetry, required this.builder});

  final String title;
  final ValueListenable<LiveStats?> live;
  final VoidCallback onRetry;

  /// The content as slivers.
  final List<Widget> Function(BuildContext context, LiveStats live) builder;
  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<LiveStats?>(
      valueListenable: live,
      builder: (context, value, _) => AppScaffold.slivers(
        title: title,
        banner: value != null && value.stale ? OfflineBanner(lastUpdated: value.updatedAt, onRetry: onRetry) : null,
        slivers: value == null ? const [SliverLoadingList(rows: 5, trailing: true)] : builder(context, value),
      ),
    );
  }
}

List<Widget> _list(List<Widget> children) => [SliverList.list(children: children)];

String _pct(double v) => '${v.round()}%';

String _cores(DashboardStats s) {
  final parts = <String>[if (s.cpuCores > 0) s.cpuCores == 1 ? '1 core' : '${s.cpuCores} cores', if (s.cpuThreads > 0 && s.cpuThreads != s.cpuCores) '${s.cpuThreads} threads'];
  return parts.join(' · ');
}

/// Processor: load, model, cores, clock, temperature, and each thread.
class CpuDetailScreen extends StatelessWidget {
  const CpuDetailScreen({super.key, required this.live, required this.onRetry, this.history});

  final ValueListenable<LiveStats?> live;
  final VoidCallback onRetry;

  /// Home's readings so far, for the chart; null leaves the chart out.
  final LiveHistory? history;

  @override
  Widget build(BuildContext context) {
    return _LiveScaffold(
      title: 'Processor',
      live: live,
      onRetry: onRetry,
      builder: (context, l) {
        final s = l.stats;
        final cores = _cores(s);
        final h = history;
        return _list([
          if (h != null)
            HistoryPanel(
              label: 'Load',
              value: '${s.cpuPercent.round()}',
              unit: '%',
              level: loadLevel(s.cpuPercent),
              series: [ChartSeries(h.cpu)],
              capacity: h.capacity,
              window: h.window,
              max: 100,
              threshold: 90,
              format: _pct,
            ),
          TileGroup(
            children: [
              // The panel above already shows the load; without it (no
              // history) the bar does.
              if (h == null)
                UsageTile(
                  icon: Icons.speed_outlined,
                  bar: UsageBar(value: s.cpuPercent, max: 100, label: 'Load', detail: cores.isEmpty ? null : cores),
                ),
              FactTile(icon: Icons.memory_outlined, label: 'Model', value: s.cpuModelName),
              if (h != null && cores.isNotEmpty) FactTile(icon: Icons.grid_view_outlined, label: 'Cores', value: cores),
              MetricRow(
                icon: Icons.av_timer_outlined,
                label: 'Clock speed',
                value: s.cpuMhz > 0 ? (s.cpuMhz / 1000).toStringAsFixed(2) : '—',
                unit: s.cpuMhz > 0 ? 'GHz' : null,
                supporting: s.cpuMhz > 0 ? null : 'Not reported by the server',
              ),
              MetricRow(
                icon: Icons.thermostat_outlined,
                label: 'Temperature',
                value: s.cpuTemperature == null ? '—' : s.cpuTemperature!.toStringAsFixed(0),
                unit: s.cpuTemperature == null ? null : '°C',
                supporting: s.cpuTemperature == null ? 'No temperature sensor found' : null,
              ),
            ],
          ),
          if (s.cpuPerCore.isNotEmpty)
            TileGroup(
              title: 'Load per thread',
              children: [
                for (var i = 0; i < s.cpuPerCore.length; i++)
                  UsageTile(
                    bar: UsageBar(value: s.cpuPerCore[i], max: 100, label: 'Thread ${i + 1}'),
                  ),
              ],
            ),
        ]);
      },
    );
  }
}

/// Memory: how much is in use, what the rest is doing, and the modules.
/// There is deliberately no "free up memory" button: Linux gives cache back
/// by itself, and the one the app used to have started a system update
/// instead (plan M-28).
class MemoryDetailScreen extends StatelessWidget {
  const MemoryDetailScreen({super.key, required this.live, required this.onRetry, this.history});

  final ValueListenable<LiveStats?> live;
  final VoidCallback onRetry;
  final LiveHistory? history;

  @override
  Widget build(BuildContext context) {
    return _LiveScaffold(
      title: 'Memory',
      live: live,
      onRetry: onRetry,
      builder: (context, l) {
        final s = l.stats;
        final h = history;
        final pct = s.memTotal > 0 ? s.memUsed / s.memTotal * 100 : 0.0;
        return _list([
          if (h != null && s.memTotal > 0)
            HistoryPanel(
              label: 'In use',
              value: '${pct.round()}',
              unit: '%',
              level: memoryLevel(pct),
              detail: '${formatBytes(s.memUsed)} of ${formatBytes(s.memTotal)}',
              series: [ChartSeries(h.memory)],
              capacity: h.capacity,
              window: h.window,
              max: 100,
              threshold: 90,
              format: _pct,
            ),
          TileGroup(
            footer: "Linux keeps recently used files in spare memory and frees it by itself when apps need it, so there's nothing to clear.",
            children: [
              if (h == null || s.memTotal <= 0)
                UsageTile(
                  icon: Icons.developer_board_outlined,
                  bar: UsageBar(value: s.memUsed.toDouble(), max: s.memTotal.toDouble(), label: 'In use', detail: s.memTotal > 0 ? '${formatBytes(s.memUsed)} of ${formatBytes(s.memTotal)}' : null),
                ),
              if (s.memAvailable > 0) bytesRow(icon: Icons.task_alt_outlined, label: 'Available', bytes: s.memAvailable, supporting: 'What apps can still use'),
              bytesRow(icon: Icons.cached_outlined, label: 'Cache and buffers', bytes: s.memCache, supporting: 'Given back when needed'),
              bytesRow(icon: Icons.check_box_outline_blank, label: 'Free', bytes: s.memFree),
            ],
          ),
          if (s.memModules.isNotEmpty)
            TileGroup(
              title: 'Modules',
              children: [
                for (final m in s.memModules)
                  MergeSemantics(
                    child: ListTile(
                      leading: const Icon(Icons.developer_board_outlined),
                      title: Text([m.size, m.type].where((e) => e.isNotEmpty).join(' ')),
                      subtitle: Text([m.speed, m.locator, m.partNumber].where((e) => e.isNotEmpty).join(' · ')),
                    ),
                  ),
              ],
            ),
        ]);
      },
    );
  }
}

String _driveKind(DiskUsage d) => switch (d.kind.toLowerCase()) {
  'ssd' => 'SSD',
  'hdd' => 'Hard drive',
  'nvme' => 'NVMe SSD',
  'usb' => 'USB drive',
  _ => d.isUsb ? 'USB drive' : '',
};

/// "317 GB of 1.8 TB · Hard drive".
String driveDetail(DiskUsage d) {
  final kind = _driveKind(d);
  final size = d.sizeKnown ? '${formatBytes(d.usedBytes)} of ${formatBytes(d.sizeBytes)}' : 'Size unknown';
  return kind.isEmpty ? size : '$size · $kind';
}

/// Storage: every data drive with how full it is.
class StorageDetailScreen extends StatelessWidget {
  const StorageDetailScreen({super.key, required this.live, required this.onRetry, this.onOpenFiles});

  final ValueListenable<LiveStats?> live;
  final VoidCallback onRetry;
  final VoidCallback? onOpenFiles;

  void _showDrive(BuildContext context, DiskUsage d) {
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      useSafeArea: true,
      isScrollControlled: true,
      builder: (sheet) => SafeArea(
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            mainAxisSize: MainAxisSize.min,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(Space.lg, 0, Space.lg, Space.sm),
                child: Semantics(header: true, child: Text(d.label, style: Theme.of(sheet).textTheme.titleLarge)),
              ),
              bytesRow(icon: Icons.pie_chart_outline, label: 'Used', bytes: d.usedBytes),
              bytesRow(icon: Icons.check_box_outline_blank, label: 'Free', bytes: d.freeBytes),
              bytesRow(icon: Icons.storage_outlined, label: 'Size', bytes: d.sizeBytes),
              FactTile(icon: Icons.folder_outlined, label: 'Mounted at', value: d.mountPoint),
              FactTile(icon: Icons.description_outlined, label: 'File system', value: d.filesystem),
              if (d.model.isNotEmpty) FactTile(icon: Icons.album_outlined, label: 'Drive', value: d.model),
              if (onOpenFiles != null)
                Padding(
                  padding: const EdgeInsets.fromLTRB(Space.lg, Space.lg, Space.lg, Space.lg),
                  child: FilledButton.tonalIcon(
                    style: tonalButtonStyle(context),
                    onPressed: () {
                      Navigator.of(sheet).pop();
                      Navigator.of(context).pop();
                      onOpenFiles!();
                    },
                    icon: const Icon(Icons.folder_open_outlined),
                    label: const Text('Open Files'),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return _LiveScaffold(
      title: 'Storage',
      live: live,
      onRetry: onRetry,
      builder: (context, l) {
        final s = l.stats;
        final drives = s.dataDisks;
        if (drives.isEmpty) {
          return const [EmptyState(sliver: true, icon: Icons.storage_outlined, title: 'No drives found', message: "The server didn't report any mounted drives.")];
        }
        return _list([
          TileGroup(
            children: [
              UsageTile(
                icon: Icons.pie_chart_outline,
                bar: UsageBar(
                  value: s.storageUsed.toDouble(),
                  max: s.storageTotal.toDouble(),
                  label: drives.length == 1 ? 'All storage' : 'All ${drives.length} drives',
                  detail: '${formatBytes(s.storageUsed)} of ${formatBytes(s.storageTotal)}',
                ),
              ),
            ],
          ),
          TileGroup(
            title: 'Drives',
            children: [
              for (final d in drives)
                UsageTile(
                  icon: d.isUsb ? Icons.usb_outlined : Icons.storage_outlined,
                  bar: UsageBar(value: d.usedBytes.toDouble(), max: d.sizeBytes.toDouble(), label: d.label, detail: driveDetail(d), warnAt: diskWarnAt, criticalAt: diskCriticalAt),
                  onTap: () => _showDrive(context, d),
                ),
            ],
          ),
        ]);
      },
    );
  }
}

/// Network: traffic right now, and the three speed tests, each saying
/// plainly what it measures.
class NetworkDetailScreen extends StatefulWidget {
  const NetworkDetailScreen({super.key, required this.live, required this.onRetry, this.service, this.history});

  final ValueListenable<LiveStats?> live;
  final VoidCallback onRetry;
  final LiveHistory? history;

  /// Tests pass their own; the app uses [SpeedtestService.instance].
  final SpeedtestService? service;

  @override
  State<NetworkDetailScreen> createState() => _NetworkDetailScreenState();
}

class _TestState {
  SpeedResult? result;
  SpeedtestProgress? progress;
  String? error;
  bool get running => progress != null;
}

class _NetworkDetailScreenState extends State<NetworkDetailScreen> {
  final Map<SpeedtestKind, _TestState> _tests = {for (final k in SpeedtestKind.values) k: _TestState()};

  SpeedtestService get _service => widget.service ?? SpeedtestService.instance;
  bool get _anyRunning => _tests.values.any((t) => t.running);

  @override
  void initState() {
    super.initState();
    _loadLastServerResult();
  }

  Future<void> _loadLastServerResult() async {
    try {
      final r = await _service.lastServerResult();
      if (!mounted || r == null) return;
      setState(() => _tests[SpeedtestKind.server]!.result ??= r);
    } catch (_) {
      // No earlier result to show; the test can still be run.
    }
  }

  Future<void> _run(SpeedtestKind kind) async {
    if (_anyRunning) return;
    if (kind == SpeedtestKind.phoneInternet) {
      final ok = await ConfirmDialog.confirm(
        context,
        title: "Test this phone's internet?",
        message: 'The test downloads and uploads a few hundred MB. On mobile data, that counts against your plan.',
        confirmLabel: 'Run test',
      );
      if (!ok || !mounted) return;
    }
    final t = _tests[kind]!;
    setState(() {
      t.error = null;
      t.progress = const SpeedtestProgress(phase: SpeedtestPhase.connecting);
    });
    void onProgress(SpeedtestProgress p) {
      if (mounted) setState(() => t.progress = p);
    }

    try {
      final r = switch (kind) {
        SpeedtestKind.server => await _service.runServerSpeedtest(onProgress: onProgress),
        SpeedtestKind.phoneToServer => await _service.runPhoneToServerTest(onProgress: onProgress),
        SpeedtestKind.phoneInternet => await _service.runPhoneInternetTest(onProgress: onProgress),
      };
      if (!mounted) return;
      setState(() {
        t.result = r;
        t.progress = null;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        t.progress = null;
        t.error = e.toString().replaceFirst('Exception: ', '');
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return _LiveScaffold(
      title: 'Network',
      live: widget.live,
      onRetry: widget.onRetry,
      builder: (context, l) {
        final net = l.stats.primaryNet;
        final state = net?.state ?? '';
        final h = widget.history;
        final down = l.rate == null ? null : splitUnit(formatBytes(l.rate!.downBytesPerSec));
        final up = l.rate == null ? null : splitUnit(formatBytes(l.rate!.upBytesPerSec));
        return _list([
          if (h != null)
            HistoryPanel(
              label: 'Download',
              value: down?.$1 ?? '—',
              unit: down?.$2 == null ? null : '${down!.$2}/s',
              series: [ChartSeries(h.netDown)],
              capacity: h.capacity,
              window: h.window,
              format: (v) => formatSpeed(v),
              second: ('Upload', up?.$1 ?? '—', up?.$2 == null ? null : '${up!.$2}/s', h.netUp),
            ),
          TileGroup(
            title: net == null || net.name.isEmpty ? 'Traffic' : 'Traffic on ${net.name}',
            children: [
              rateRow(icon: Icons.arrow_downward, label: 'Download', bytesPerSec: l.rate?.downBytesPerSec),
              rateRow(icon: Icons.arrow_upward, label: 'Upload', bytesPerSec: l.rate?.upBytesPerSec),
              if (net != null) ...[
                bytesRow(icon: Icons.south_outlined, label: 'Received', bytes: net.bytesRecv, supporting: 'Since the server started'),
                bytesRow(icon: Icons.north_outlined, label: 'Sent', bytes: net.bytesSent, supporting: 'Since the server started'),
              ],
              if (state.isNotEmpty) FactTile(icon: Icons.settings_ethernet_outlined, label: 'Link', value: state == 'up' ? 'Connected' : 'Disconnected ($state)'),
            ],
          ),
          _speedGroup(SpeedtestKind.server, title: "Server's internet", about: 'The server tests its own internet connection against the nearest speedtest.net server.'),
          _speedGroup(SpeedtestKind.phoneToServer, title: 'This phone to the server', about: 'How fast this phone reaches the server over its current connection. No internet involved.'),
          _speedGroup(SpeedtestKind.phoneInternet, title: "This phone's internet", about: "Runs on this phone against public test servers. It says nothing about the server's connection."),
        ]);
      },
    );
  }

  // "Just now" and "Yesterday" read lower case mid-sentence.
  static String _relativeTail(DateTime t) {
    final r = formatRelative(t);
    return r == 'Just now' || r == 'Yesterday' ? r.toLowerCase() : r;
  }

  static String _phaseText(SpeedtestPhase p) => switch (p) {
    SpeedtestPhase.ping => 'Measuring latency',
    SpeedtestPhase.download => 'Testing download',
    SpeedtestPhase.upload => 'Testing upload',
    _ => 'Starting',
  };

  Widget _speedGroup(SpeedtestKind kind, {required String title, required String about}) {
    final t = _tests[kind]!;
    final p = t.progress;
    final shown = p?.partial ?? t.result;
    final live = p?.liveMbps;

    double? down = shown?.downloadMbps;
    double? up = shown?.uploadMbps;
    if (p != null && live != null) {
      if (p.phase == SpeedtestPhase.download) down = live;
      if (p.phase == SpeedtestPhase.upload) up = live;
    }

    final testedAt = t.result?.testedAt;
    final server = t.result?.server;
    final footer = [about, if (p == null && testedAt != null) 'Tested ${_relativeTail(testedAt)}${server == null ? '' : ' · $server'}.'].join(' ');

    MetricRow mbps(IconData icon, String label, double? v, {bool measuring = false}) =>
        MetricRow(icon: icon, label: label, value: v == null ? '—' : formatMbps(v), unit: v == null ? null : 'Mbps', supporting: measuring ? 'Measuring' : null);

    final hasNumbers = p != null || t.result != null;
    return TileGroup(
      title: title,
      footer: footer,
      children: [
        if (hasNumbers) ...[
          mbps(Icons.arrow_downward, 'Download', down, measuring: p?.phase == SpeedtestPhase.download),
          mbps(Icons.arrow_upward, 'Upload', up, measuring: p?.phase == SpeedtestPhase.upload),
          MetricRow(
            icon: Icons.timer_outlined,
            label: 'Latency',
            value: shown?.pingMs == null ? '—' : formatMs(shown!.pingMs!),
            unit: shown?.pingMs == null ? null : 'ms',
            supporting: shown?.jitterMs == null ? null : 'Jitter ${formatMs(shown!.jitterMs!)} ms',
          ),
        ],
        if (p != null)
          Semantics(
            liveRegion: true,
            child: ListTile(
              leading: const Icon(Icons.hourglass_empty_outlined),
              title: Text(_phaseText(p.phase)),
              subtitle: Padding(
                padding: const EdgeInsets.only(top: Space.sm),
                child: LinearProgressIndicator(value: p.fraction),
              ),
            ),
          )
        else
          ListTile(
            leading: Icon(t.error != null ? Icons.error_outline : Icons.play_arrow_outlined, color: t.error != null ? Theme.of(context).colorScheme.error : null),
            title: Text(t.result == null && t.error == null ? 'Run test' : 'Run again'),
            subtitle: t.error == null ? null : Text(t.error!),
            enabled: !_anyRunning,
            onTap: () => _run(kind),
          ),
      ],
    );
  }
}

/// Graphics: the GPU's load, video memory, temperature and power, and what
/// is using it - the facts the web UI's GPU widget shows.
class GpuDetailScreen extends StatelessWidget {
  const GpuDetailScreen({super.key, required this.gpu, this.history});

  final ValueListenable<GpuStats?> gpu;
  final LiveHistory? history;

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<GpuStats?>(
      valueListenable: gpu,
      builder: (context, g, _) {
        final h = history;
        return AppScaffold.slivers(
          title: 'Graphics',
          slivers: g == null
              ? const [EmptyState(sliver: true, icon: Icons.developer_board_outlined, title: 'No graphics card reading', message: "The server didn't report a dedicated GPU with a working driver.")]
              : _list([
                  if (h != null)
                    HistoryPanel(
                      label: 'Load',
                      value: '${g.utilizationPercent.round()}',
                      unit: '%',
                      level: loadLevel(g.utilizationPercent, gpu: true),
                      series: [ChartSeries(h.gpu)],
                      capacity: h.capacity,
                      window: h.window,
                      max: 100,
                      format: _pct,
                    ),
                  TileGroup(
                    children: [
                      FactTile(icon: Icons.developer_board_outlined, label: 'Model', value: g.name),
                      if (g.memoryTotalMib > 0)
                        UsageTile(
                          icon: Icons.memory_outlined,
                          bar: UsageBar(value: g.memoryUsedBytes.toDouble(), max: g.memoryTotalBytes.toDouble(), label: 'Video memory', detail: usedOf(g.memoryUsedBytes, g.memoryTotalBytes)),
                        ),
                      MetricRow(
                        icon: Icons.thermostat_outlined,
                        label: 'Temperature',
                        value: g.temperatureC == null ? '—' : g.temperatureC!.toStringAsFixed(0),
                        unit: g.temperatureC == null ? null : '°C',
                      ),
                      MetricRow(
                        icon: Icons.bolt_outlined,
                        label: 'Power',
                        value: g.powerDrawW == null ? '—' : g.powerDrawW!.toStringAsFixed(0),
                        unit: g.powerDrawW == null ? null : 'W',
                        supporting: g.powerLimitW == null ? null : 'Limit ${g.powerLimitW!.toStringAsFixed(0)} W',
                      ),
                      if (g.driverVersion.isNotEmpty) FactTile(icon: Icons.extension_outlined, label: 'Driver', value: g.driverVersion),
                    ],
                  ),
                  TileGroup(
                    title: 'Using the GPU',
                    children: [
                      if (g.processes.isEmpty)
                        const ListTile(leading: Icon(Icons.check_circle_outline), title: Text('Nothing is using it'))
                      else
                        for (final p in g.processes.take(8))
                          MetricRow(
                            icon: Icons.memory_outlined,
                            label: p.command.isEmpty ? 'Process ${p.pid}' : p.command,
                            value: p.utilizationPercent.toStringAsFixed(0),
                            unit: '%',
                            supporting: 'PID ${p.pid}',
                          ),
                    ],
                  ),
                ]),
        );
      },
    );
  }
}
