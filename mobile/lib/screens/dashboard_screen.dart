import 'dart:async';
import 'package:flutter/material.dart';
import 'package:percent_indicator/percent_indicator.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../models/dashboard_stats.dart';
import '../utils/format.dart';

class DashboardScreen extends StatefulWidget {
  const DashboardScreen({super.key});

  @override
  State<DashboardScreen> createState() => _DashboardScreenState();
}

class _DashboardScreenState extends State<DashboardScreen> {
  DashboardStats? _stats;
  String? _error;
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _load();
    _timer = Timer.periodic(const Duration(seconds: 5), (_) => _load());
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final utilRes = await ApiClient.instance.get('/sys/utilization');
      var stats = DashboardStats.fromUtilization(utilRes['data'] as Map<String, dynamic>? ?? {});

      final disksRes = await ApiClient.instance.get('/sys/disks-usage');
      final disksData = disksRes['data'] as List<dynamic>? ?? [];
      final disks = disksData
          .map((e) => DiskUsage.fromJson(e as Map<String, dynamic>))
          .where((d) => d.mountPoint.isNotEmpty)
          .toList();
      stats = stats.withDisks(disks);

      if (!mounted) return;
      setState(() {
        _stats = stats;
        _error = null;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = e.toString().replaceFirst('Exception: ', ''));
    }
  }

  @override
  Widget build(BuildContext context) {
    final stats = _stats;
    return Scaffold(
      appBar: AppBar(title: const Text('Dashboard')),
      body: RefreshIndicator(
        onRefresh: _load,
        child: stats == null
            ? _error != null
                ? _ErrorState(message: _error!, onRetry: _load)
                : const Center(child: CircularProgressIndicator())
            : ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: _StatRing(
                          label: 'CPU',
                          percent: (stats.cpuPercent / 100).clamp(0, 1),
                          text: '${stats.cpuPercent.toStringAsFixed(0)}%',
                          color: NivaroColors.primary,
                        ),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: _StatRing(
                          label: 'Memory',
                          percent: (stats.memUsedPercent / 100).clamp(0, 1),
                          text: '${stats.memUsedPercent.toStringAsFixed(0)}%',
                          color: NivaroColors.success,
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 8),
                  Text(
                    '${formatBytes(stats.memUsed)} of ${formatBytes(stats.memTotal)} memory used',
                    textAlign: TextAlign.center,
                    style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                  ),
                  const SizedBox(height: 24),
                  const Text('Storage', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 16)),
                  const SizedBox(height: 12),
                  if (stats.disks.isEmpty)
                    const Text('No storage devices reported.', style: TextStyle(color: NivaroColors.textMuted)),
                  ...stats.disks.map((d) => Card(
                        margin: const EdgeInsets.only(bottom: 10),
                        child: Padding(
                          padding: const EdgeInsets.all(14),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Row(
                                children: [
                                  Expanded(
                                    child: Text(
                                      d.label.isNotEmpty ? d.label : d.mountPoint,
                                      style: const TextStyle(fontWeight: FontWeight.w600),
                                      overflow: TextOverflow.ellipsis,
                                    ),
                                  ),
                                  Text(d.percent, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
                                ],
                              ),
                              const SizedBox(height: 6),
                              LinearPercentIndicator(
                                percent: d.fraction,
                                lineHeight: 8,
                                barRadius: const Radius.circular(4),
                                progressColor: NivaroColors.primary,
                                backgroundColor: const Color(0xFFE2E8F0),
                                padding: EdgeInsets.zero,
                              ),
                              const SizedBox(height: 4),
                              Text(
                                '${formatBytes(d.usedBytes)} of ${formatBytes(d.sizeBytes)}',
                                style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11.5),
                              ),
                            ],
                          ),
                        ),
                      )),
                ],
              ),
      ),
    );
  }
}

class _StatRing extends StatelessWidget {
  final String label;
  final double percent;
  final String text;
  final Color color;
  const _StatRing({required this.label, required this.percent, required this.text, required this.color});

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 18),
        child: Column(
          children: [
            CircularPercentIndicator(
              radius: 44,
              lineWidth: 8,
              percent: percent,
              center: Text(text, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 16)),
              progressColor: color,
              backgroundColor: color.withOpacity(0.12),
              circularStrokeCap: CircularStrokeCap.round,
            ),
            const SizedBox(height: 8),
            Text(label, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12, fontWeight: FontWeight.w500)),
          ],
        ),
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  final String message;
  final VoidCallback onRetry;
  const _ErrorState({required this.message, required this.onRetry});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.error_outline, color: NivaroColors.danger, size: 36),
            const SizedBox(height: 8),
            Text(message, textAlign: TextAlign.center, style: const TextStyle(color: NivaroColors.textMuted)),
            const SizedBox(height: 12),
            OutlinedButton(onPressed: onRetry, child: const Text('Retry')),
          ],
        ),
      ),
    );
  }
}
