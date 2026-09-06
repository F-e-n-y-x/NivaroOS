import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../services/storage_service.dart';
import '../models/dashboard_stats.dart';
import '../utils/format.dart';
import '../widgets/common.dart';

class DashboardScreen extends StatefulWidget {
  final VoidCallback? onOpenFiles;
  final VoidCallback? onOpenVms;
  final VoidCallback? onOpenApps;

  const DashboardScreen({
    super.key,
    this.onOpenFiles,
    this.onOpenVms,
    this.onOpenApps,
  });

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
    _timer = Timer.periodic(const Duration(seconds: 4), (_) => _loadSilently());
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    await _fetchStats();
  }

  Future<void> _loadSilently() async {
    await _fetchStats();
  }

  Future<void> _fetchStats() async {
    try {
      final utilRes = await ApiClient.instance.get('/sys/utilization');
      var stats = DashboardStats.fromUtilization(utilRes['data'] as Map<String, dynamic>? ?? {});

      final disksRes = await ApiClient.instance.get('/sys/disks-usage');
      final disksData = disksRes['data'] as List<dynamic>? ?? [];
      final disks = disksData
          .map((e) => DiskUsage.fromJson(e as Map<String, dynamic>))
          .where((d) => d.mountPoint.isNotEmpty && !d.isSystemPartition)
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
      if (_stats == null) {
        setState(() => _error = e.toString().replaceFirst('Exception: ', ''));
      }
    }
  }

  Future<void> _confirmAndSetState(String state, String title, String body) async {
    HapticFeedback.mediumImpact();
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(title),
        content: Text(body),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text(title, style: const TextStyle(color: NivaroColors.danger, fontWeight: FontWeight.bold)),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      await ApiClient.instance.put('/sys/state/$state');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Server $state command sent successfully.')),
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

  Color _cpuColor(double pct) {
    if (pct > 85) return NivaroColors.danger;
    if (pct > 65) return NivaroColors.warning;
    return NivaroColors.primaryLight;
  }

  Color _memColor(double pct) {
    if (pct > 85) return NivaroColors.danger;
    if (pct > 70) return NivaroColors.purple;
    return NivaroColors.success;
  }

  @override
  Widget build(BuildContext context) {
    final stats = _stats;
    return SafeArea(
      bottom: false,
      child: RefreshIndicator(
        onRefresh: _load,
        color: NivaroColors.primaryLight,
        backgroundColor: NivaroColors.surfaceRaised,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 14, 20, 140),
          children: [
            // Top App Bar / Server Header
            Row(
              children: [
                Container(
                  width: 46,
                  height: 46,
                  decoration: BoxDecoration(
                    gradient: const LinearGradient(
                      colors: [NivaroColors.primaryLight, NivaroColors.primaryDark],
                    ),
                    shape: BoxShape.circle,
                    border: Border.all(color: NivaroColors.borderHighlight, width: 1.5),
                    boxShadow: const [
                      BoxShadow(color: Color(0x33000000), blurRadius: 10, offset: Offset(0, 3)),
                    ],
                  ),
                  alignment: Alignment.center,
                  child: Text(
                    _username.isEmpty ? '?' : _username.substring(0, 1).toUpperCase(),
                    style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 18, color: Colors.white),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        _username.isEmpty ? 'Signed in' : _username,
                        style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 16),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                      const SizedBox(height: 1),
                      const Row(
                        children: [
                          Text('NivaroOS Server', style: TextStyle(color: NivaroColors.textMuted, fontSize: 12.5, fontWeight: FontWeight.w500)),
                          SizedBox(width: 6),
                          Icon(Icons.check_circle_rounded, color: NivaroColors.success, size: 13),
                        ],
                      ),
                    ],
                  ),
                ),
                RoundIconButton(
                  icon: Icons.refresh_rounded,
                  tooltip: 'Refresh metrics',
                  onPressed: _load,
                ),
                const SizedBox(width: 8),
                PopupMenuButton<String>(
                  icon: const Icon(Icons.more_vert_rounded, color: NivaroColors.textMuted),
                  color: NivaroColors.surfaceContainerHighest,
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
                  onSelected: (v) {
                    if (v == 'restart') {
                      _confirmAndSetState('restart', 'Restart Server', 'This restarts the server. All running apps and VMs will be temporarily interrupted.');
                    } else if (v == 'off') {
                      _confirmAndSetState('off', 'Power Off Server', 'This shuts down the server hardware completely.');
                    }
                  },
                  itemBuilder: (context) => const [
                    PopupMenuItem(
                      value: 'restart',
                      child: Row(
                        children: [
                          Icon(Icons.restart_alt_rounded, size: 18, color: NivaroColors.textPrimary),
                          SizedBox(width: 10),
                          Text('Restart Server'),
                        ],
                      ),
                    ),
                    PopupMenuItem(
                      value: 'off',
                      child: Row(
                        children: [
                          Icon(Icons.power_settings_new_rounded, size: 18, color: NivaroColors.danger),
                          SizedBox(width: 10),
                          Text('Power Off', style: TextStyle(color: NivaroColors.danger)),
                        ],
                      ),
                    ),
                  ],
                ),
              ],
            ),
            const SizedBox(height: 16),

            // Connection Address Pill
            LanBadge(address: _host, label: 'LAN'),
            const SizedBox(height: 24),

            // Quick Actions Bar
            const SectionHeader(title: 'Quick Access'),
            Row(
              children: [
                Expanded(
                  child: _QuickActionCard(
                    icon: Icons.folder_rounded,
                    label: 'Files',
                    color: NivaroColors.primaryLight,
                    onTap: widget.onOpenFiles,
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: _QuickActionCard(
                    icon: Icons.monitor_rounded,
                    label: 'VMs',
                    color: NivaroColors.info,
                    onTap: widget.onOpenVms,
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: _QuickActionCard(
                    icon: Icons.grid_view_rounded,
                    label: 'Apps',
                    color: NivaroColors.purple,
                    onTap: widget.onOpenApps,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 24),

            // Real-Time System Monitor
            const SectionHeader(title: 'System Monitor'),
            if (stats == null)
              _error != null
                  ? DarkCard(
                      padding: const EdgeInsets.all(28),
                      child: Center(
                        child: Column(
                          children: [
                            const Icon(Icons.error_outline_rounded, color: NivaroColors.danger, size: 36),
                            const SizedBox(height: 10),
                            Text(_error!, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13), textAlign: TextAlign.center),
                            const SizedBox(height: 12),
                            OutlinedButton(onPressed: _load, child: const Text('Retry Connection')),
                          ],
                        ),
                      ),
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
                childAspectRatio: 1.12,
                children: [
                  MonitorCard(
                    label: 'CPU Usage',
                    icon: Icons.memory_rounded,
                    color: _cpuColor(stats.cpuPercent),
                    percent: stats.cpuPercent / 100,
                    value: Row(
                      crossAxisAlignment: CrossAxisAlignment.baseline,
                      textBaseline: TextBaseline.alphabetic,
                      children: [
                        Text(
                          '${stats.cpuPercent.toStringAsFixed(0)}%',
                          style: const TextStyle(fontSize: 24, fontWeight: FontWeight.w800, color: NivaroColors.textPrimary),
                        ),
                        if (stats.cpuTemperature != null) ...[
                          const Spacer(),
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                            decoration: BoxDecoration(
                              color: NivaroColors.warning.withOpacity(0.14),
                              borderRadius: BorderRadius.circular(NivaroShape.small),
                            ),
                            child: Text(
                              '${stats.cpuTemperature!.toStringAsFixed(0)}°C',
                              style: const TextStyle(color: NivaroColors.warning, fontSize: 11, fontWeight: FontWeight.w700),
                            ),
                          ),
                        ],
                      ],
                    ),
                  ),
                  MonitorCard(
                    label: 'Memory (RAM)',
                    icon: Icons.developer_board_rounded,
                    color: _memColor(stats.memUsedPercent),
                    percent: stats.memUsedPercent / 100,
                    value: Text(
                      '${stats.memUsedPercent.toStringAsFixed(0)}%',
                      style: const TextStyle(fontSize: 24, fontWeight: FontWeight.w800, color: NivaroColors.textPrimary),
                    ),
                    subtitle: '${formatBytes(stats.memUsed)} of ${formatBytes(stats.memTotal)}',
                  ),
                  MonitorCard(
                    label: stats.primaryNet?.name.isNotEmpty == true ? 'Network (${stats.primaryNet!.name})' : 'Network I/O',
                    icon: Icons.swap_vert_rounded,
                    color: NivaroColors.info,
                    value: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        _NetSpeedRow(icon: Icons.arrow_upward_rounded, rate: _netUpRate, label: 'Upload'),
                        const SizedBox(height: 4),
                        _NetSpeedRow(icon: Icons.arrow_downward_rounded, rate: _netDownRate, label: 'Download'),
                      ],
                    ),
                  ),
                  MonitorCard(
                    label: 'Storage Summary',
                    icon: Icons.pie_chart_rounded,
                    color: NivaroColors.success,
                    percent: stats.storageFraction,
                    value: Text(
                      stats.storagePercentText,
                      style: const TextStyle(fontSize: 24, fontWeight: FontWeight.w800, color: NivaroColors.textPrimary),
                    ),
                    subtitle: '${formatBytes(stats.storageUsed)} of ${formatBytes(stats.storageTotal)}',
                  ),
                ],
              ),
              const SizedBox(height: 24),

              // Storage Drives List
              const SectionHeader(title: 'Storage Drives'),
              if (stats.disks.isEmpty)
                const Text('No storage devices found.', style: TextStyle(color: NivaroColors.textMuted))
              else
                ...stats.disks.map((d) => Padding(
                      padding: const EdgeInsets.only(bottom: 10),
                      child: DriveCard(
                        label: d.label.isNotEmpty ? d.label : d.mountPoint,
                        percentText: d.percent,
                        fraction: d.fraction,
                        usedBytes: d.usedBytes,
                        sizeBytes: d.sizeBytes,
                        onTap: widget.onOpenFiles,
                      ),
                    )),
            ],
          ],
        ),
      ),
    );
  }
}

class _QuickActionCard extends StatelessWidget {
  final IconData icon;
  final String label;
  final Color color;
  final VoidCallback? onTap;

  const _QuickActionCard({
    required this.icon,
    required this.label,
    required this.color,
    this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
        onTap: () {
          HapticFeedback.lightImpact();
          if (onTap != null) onTap!();
        },
        child: Container(
          padding: const EdgeInsets.symmetric(vertical: 14),
          decoration: BoxDecoration(
            color: NivaroColors.surface,
            borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
            border: Border.all(color: NivaroColors.border),
            boxShadow: const [
              BoxShadow(color: Color(0x2B000000), blurRadius: 10, offset: Offset(0, 3)),
            ],
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Container(
                width: 36,
                height: 36,
                decoration: BoxDecoration(
                  color: color.withOpacity(0.16),
                  shape: BoxShape.circle,
                ),
                alignment: Alignment.center,
                child: Icon(icon, color: color, size: 20),
              ),
              const SizedBox(height: 8),
              Text(
                label,
                style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13, color: NivaroColors.textPrimary),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _NetSpeedRow extends StatelessWidget {
  final IconData icon;
  final double rate;
  final String label;

  const _NetSpeedRow({
    required this.icon,
    required this.rate,
    required this.label,
  });

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Icon(icon, size: 14, color: NivaroColors.info),
        const SizedBox(width: 4),
        Text(
          '${formatBytes(rate)}/s',
          style: const TextStyle(fontSize: 12.5, fontWeight: FontWeight.w700, color: NivaroColors.textPrimary),
        ),
      ],
    );
  }
}
