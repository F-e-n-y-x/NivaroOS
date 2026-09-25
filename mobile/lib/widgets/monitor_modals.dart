import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import '../models/dashboard_stats.dart';
import '../services/speedtest_service.dart';
import '../ui/ui.dart';
import '../utils/format.dart';

// The detail screens behind Home's health rows: processor, memory, storage
// and network. Each one follows the same live reading Home polls (a
// [ValueListenable] of [LiveStats]), so its numbers keep moving while it's
// open, and shows the offline banner over the last reading when the server
// stops answering.

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
    final padding = ListTileTheme.of(context).contentPadding?.resolve(Directionality.of(context)) ??
        const EdgeInsets.symmetric(horizontal: Space.lg);
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
                  if (icon != null) ...[
                    ExcludeSemantics(child: Icon(icon, color: scheme.onSurfaceVariant)),
                    const SizedBox(width: Space.lg),
                  ],
                  Expanded(child: bar),
                  if (onTap != null) ...[
                    const SizedBox(width: Space.xs),
                    ExcludeSemantics(child: Icon(Icons.chevron_right, color: scheme.onSurfaceVariant)),
                  ],
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
        child: ListTile(
          leading: Icon(icon),
          title: Text(label),
          subtitle: Text(value.isEmpty ? 'Unknown' : value),
        ),
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
        slivers: value == null
            ? const [SliverLoadingList(rows: 5, trailing: true)]
            : builder(context, value),
      ),
    );
  }
}

List<Widget> _list(List<Widget> children) => [SliverList.list(children: children)];

String _cores(DashboardStats s) {
  final parts = <String>[
    if (s.cpuCores > 0) s.cpuCores == 1 ? '1 core' : '${s.cpuCores} cores',
    if (s.cpuThreads > 0 && s.cpuThreads != s.cpuCores) '${s.cpuThreads} threads',
  ];
  return parts.join(' · ');
}

/// Processor: load, model, cores, clock, temperature, and each thread.
class CpuDetailScreen extends StatelessWidget {
  const CpuDetailScreen({super.key, required this.live, required this.onRetry});

  final ValueListenable<LiveStats?> live;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return _LiveScaffold(
      title: 'Processor',
      live: live,
      onRetry: onRetry,
      builder: (context, l) {
        final s = l.stats;
        final cores = _cores(s);
        return _list([
          TileGroup(children: [
            UsageTile(
              icon: Icons.speed_outlined,
              bar: UsageBar(value: s.cpuPercent, max: 100, label: 'Load', detail: cores.isEmpty ? null : cores),
            ),
            FactTile(icon: Icons.memory_outlined, label: 'Model', value: s.cpuModelName),
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
          ]),
          if (s.cpuPerCore.isNotEmpty)
            TileGroup(
              title: 'Load per thread',
              children: [
                for (var i = 0; i < s.cpuPerCore.length; i++)
                  UsageTile(bar: UsageBar(value: s.cpuPerCore[i], max: 100, label: 'Thread ${i + 1}')),
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
  const MemoryDetailScreen({super.key, required this.live, required this.onRetry});

  final ValueListenable<LiveStats?> live;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return _LiveScaffold(
      title: 'Memory',
      live: live,
      onRetry: onRetry,
      builder: (context, l) {
        final s = l.stats;
        return _list([
          TileGroup(
            footer: "Linux keeps recently used files in spare memory and frees it by itself when apps need it, so there's nothing to clear.",
            children: [
              UsageTile(
                icon: Icons.developer_board_outlined,
                bar: UsageBar(
                  value: s.memUsed.toDouble(),
                  max: s.memTotal.toDouble(),
                  label: 'In use',
                  detail: s.memTotal > 0 ? '${formatBytes(s.memUsed)} of ${formatBytes(s.memTotal)}' : null,
                ),
              ),
              if (s.memAvailable > 0)
                bytesRow(icon: Icons.task_alt_outlined, label: 'Available', bytes: s.memAvailable, supporting: 'What apps can still use'),
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
          return const [
            EmptyState(
              sliver: true,
              icon: Icons.storage_outlined,
              title: 'No drives found',
              message: "The server didn't report any mounted drives.",
            ),
          ];
        }
        return _list([
          TileGroup(children: [
            UsageTile(
              icon: Icons.pie_chart_outline,
              bar: UsageBar(
                value: s.storageUsed.toDouble(),
                max: s.storageTotal.toDouble(),
                label: drives.length == 1 ? 'All storage' : 'All ${drives.length} drives',
                detail: '${formatBytes(s.storageUsed)} of ${formatBytes(s.storageTotal)}',
              ),
            ),
          ]),
          TileGroup(
            title: 'Drives',
            children: [
              for (final d in drives)
                UsageTile(
                  icon: d.isUsb ? Icons.usb_outlined : Icons.storage_outlined,
                  bar: UsageBar(
                    value: d.usedBytes.toDouble(),
                    max: d.sizeBytes.toDouble(),
                    label: d.label,
                    detail: driveDetail(d),
                    warnAt: diskWarnAt,
                    criticalAt: diskCriticalAt,
                  ),
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
  const NetworkDetailScreen({super.key, required this.live, required this.onRetry, this.service});

  final ValueListenable<LiveStats?> live;
  final VoidCallback onRetry;

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
        return _list([
          TileGroup(
            title: net == null || net.name.isEmpty ? 'Traffic' : 'Traffic on ${net.name}',
            children: [
              rateRow(icon: Icons.arrow_downward, label: 'Download', bytesPerSec: l.rate?.downBytesPerSec),
              rateRow(icon: Icons.arrow_upward, label: 'Upload', bytesPerSec: l.rate?.upBytesPerSec),
              if (net != null) ...[
                bytesRow(icon: Icons.south_outlined, label: 'Received', bytes: net.bytesRecv, supporting: 'Since the server started'),
                bytesRow(icon: Icons.north_outlined, label: 'Sent', bytes: net.bytesSent, supporting: 'Since the server started'),
              ],
              if (state.isNotEmpty)
                FactTile(icon: Icons.settings_ethernet_outlined, label: 'Link', value: state == 'up' ? 'Connected' : 'Disconnected ($state)'),
            ],
          ),
          _speedGroup(
            SpeedtestKind.server,
            title: "Server's internet",
            about: 'The server tests its own internet connection against the nearest speedtest.net server.',
          ),
          _speedGroup(
            SpeedtestKind.phoneToServer,
            title: 'This phone to the server',
            about: 'How fast this phone reaches the server over its current connection. No internet involved.',
          ),
          _speedGroup(
            SpeedtestKind.phoneInternet,
            title: "This phone's internet",
            about: "Runs on this phone against public test servers. It says nothing about the server's connection.",
          ),
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
    final footer = [
      about,
      if (p == null && testedAt != null) 'Tested ${_relativeTail(testedAt)}${server == null ? '' : ' · $server'}.',
    ].join(' ');

    MetricRow mbps(IconData icon, String label, double? v, {bool measuring = false}) => MetricRow(
          icon: icon,
          label: label,
          value: v == null ? '—' : formatMbps(v),
          unit: v == null ? null : 'Mbps',
          supporting: measuring ? 'Measuring' : null,
        );

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
            leading: Icon(t.error != null ? Icons.error_outline : Icons.play_arrow_outlined,
                color: t.error != null ? Theme.of(context).colorScheme.error : null),
            title: Text(t.result == null && t.error == null ? 'Run test' : 'Run again'),
            subtitle: t.error == null ? null : Text(t.error!),
            enabled: !_anyRunning,
            onTap: () => _run(kind),
          ),
      ],
    );
  }
}
