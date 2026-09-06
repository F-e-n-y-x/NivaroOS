import 'dart:async';
import 'package:flutter/material.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../services/storage_service.dart';
import '../models/dashboard_stats.dart';
import '../utils/format.dart';
import '../widgets/common.dart';

class DashboardScreen extends StatefulWidget {
  const DashboardScreen({super.key});

  @override
  State<DashboardScreen> createState() => _DashboardScreenState();
}

class _DashboardScreenState extends State<DashboardScreen> {
  DashboardStats? _stats;
  String? _error;
  Timer? _timer;
  String _username = '';

  NetSample? _lastNet;
  DateTime? _lastNetAt;
  double _netUpRate = 0;
  double _netDownRate = 0;

  @override
  void initState() {
    super.initState();
    StorageService.instance.getUsername().then((u) {
      if (mounted && u != null) setState(() => _username = u);
    });
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

      final net = stats.primaryNet;
      final now = DateTime.now();
      if (net != null && _lastNet != null && _lastNetAt != null) {
        final elapsed = now.difference(_lastNetAt!).inMilliseconds / 1000;
        if (elapsed > 0) {
          final upDelta = net.bytesSent - _lastNet!.bytesSent;
          final downDelta = net.bytesRecv - _lastNet!.bytesRecv;
          _netUpRate = upDelta > 0 ? upDelta / elapsed : 0;
          _netDownRate = downDelta > 0 ? downDelta / elapsed : 0;
        }
      }
      if (net != null) {
        _lastNet = net;
        _lastNetAt = now;
      }

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

  Future<void> _confirmAndSetState(String state, String title, String body) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(title),
        content: Text(body),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text(title, style: const TextStyle(color: NivaroColors.danger)),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      await ApiClient.instance.put('/sys/state/$state');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('The operation will complete shortly.')),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(e.toString().replaceFirst('Exception: ', '')), backgroundColor: NivaroColors.danger),
        );
      }
    }
  }

  String get _host {
    final base = ApiClient.instance.baseUrl;
    try {
      return Uri.parse(base).host;
    } catch (_) {
      return base;
    }
  }

  @override
  Widget build(BuildContext context) {
    final stats = _stats;
    return SafeArea(
      bottom: false,
      child: RefreshIndicator(
        onRefresh: _load,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 12, 20, 140),
          children: [
            Row(
              children: [
                CircleAvatar(
                  radius: 22,
                  backgroundColor: NivaroColors.surface,
                  child: Text(
                    _username.isEmpty ? '?' : _username.substring(0, 1).toUpperCase(),
                    style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 16, color: NivaroColors.textPrimary),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(_username.isEmpty ? 'Signed in' : _username, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 15)),
                      Text('NivaroOS', style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12.5)),
                    ],
                  ),
                ),
                PopupMenuButton<String>(
                  icon: const Icon(Icons.more_vert, color: NivaroColors.textMuted),
                  onSelected: (v) {
                    if (v == 'restart') {
                      _confirmAndSetState('restart', 'Restart', 'This restarts the whole server. Everything running on it will be interrupted.');
                    } else if (v == 'off') {
                      _confirmAndSetState('off', 'Shut Down', 'This powers off the whole server. You will need physical or remote-power access to turn it back on.');
                    }
                  },
                  itemBuilder: (context) => const [
                    PopupMenuItem(value: 'restart', child: Text('Restart')),
                    PopupMenuItem(value: 'off', child: Text('Shut Down', style: TextStyle(color: NivaroColors.danger))),
                  ],
                ),
              ],
            ),
            const SizedBox(height: 16),
            LanBadge(address: _host),
            const SizedBox(height: 28),
            const Text('Monitor', style: nivaroSectionLabelStyle),
            const SizedBox(height: 12),
            if (stats == null)
              _error != null
                  ? Padding(
                      padding: const EdgeInsets.symmetric(vertical: 40),
                      child: Center(child: Text(_error!, style: const TextStyle(color: NivaroColors.textMuted), textAlign: TextAlign.center)),
                    )
                  : const Padding(
                      padding: EdgeInsets.symmetric(vertical: 40),
                      child: Center(child: CircularProgressIndicator()),
                    )
            else ...[
              GridView.count(
                crossAxisCount: 2,
                shrinkWrap: true,
                physics: const NeverScrollableScrollPhysics(),
                mainAxisSpacing: 12,
                crossAxisSpacing: 12,
                childAspectRatio: 1.25,
                children: [
                  StatRingCard(
                    label: 'CPU',
                    percent: stats.cpuPercent / 100,
                    value: '${stats.cpuPercent.toStringAsFixed(0)}%',
                    color: NivaroColors.primary,
                  ),
                  StatValueCard(
                    label: 'CPU Temperature',
                    child: Text(
                      stats.cpuTemperature != null ? '${stats.cpuTemperature!.toStringAsFixed(0)}°C' : '—',
                      style: const TextStyle(fontSize: 26, fontWeight: FontWeight.w800, color: NivaroColors.success),
                    ),
                  ),
                  StatRingCard(
                    label: 'Memory',
                    percent: stats.memUsedPercent / 100,
                    value: '${stats.memUsedPercent.toStringAsFixed(0)}%',
                    color: NivaroColors.success,
                  ),
                  StatValueCard(
                    label: stats.primaryNet?.name.isNotEmpty == true ? 'Network · ${stats.primaryNet!.name}' : 'Network',
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        _NetRow(icon: Icons.arrow_upward_rounded, label: '${formatBytes(_netUpRate)}/s'),
                        const SizedBox(height: 6),
                        _NetRow(icon: Icons.arrow_downward_rounded, label: '${formatBytes(_netDownRate)}/s'),
                      ],
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 4),
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  '${formatBytes(stats.memUsed)} of ${formatBytes(stats.memTotal)} memory used',
                  style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                ),
              ),
              const SizedBox(height: 28),
              const Text('Storage', style: nivaroSectionLabelStyle),
              const SizedBox(height: 12),
              if (stats.disks.isEmpty)
                const Text('No storage devices reported.', style: TextStyle(color: NivaroColors.textMuted))
              else
                ...stats.disks.map((d) => Padding(
                      padding: const EdgeInsets.only(bottom: 10),
                      child: DarkCard(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Row(
                              children: [
                                Expanded(
                                  child: Text(
                                    d.label.isNotEmpty ? d.label : d.mountPoint,
                                    style: const TextStyle(fontWeight: FontWeight.w700),
                                    overflow: TextOverflow.ellipsis,
                                  ),
                                ),
                                Text(d.percent, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
                              ],
                            ),
                            const SizedBox(height: 10),
                            DotStorageBar(fraction: d.fraction),
                            const SizedBox(height: 8),
                            Text(
                              '${formatBytes(d.usedBytes)} of ${formatBytes(d.sizeBytes)}',
                              style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11.5),
                            ),
                          ],
                        ),
                      ),
                    )),
            ],
          ],
        ),
      ),
    );
  }
}

class _NetRow extends StatelessWidget {
  final IconData icon;
  final String label;
  const _NetRow({required this.icon, required this.label});

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Icon(icon, size: 14, color: NivaroColors.info),
        const SizedBox(width: 4),
        Text(label, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: NivaroColors.textPrimary)),
      ],
    );
  }
}
