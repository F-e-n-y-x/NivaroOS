import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../services/storage_service.dart';
import '../services/vm_client.dart';
import '../models/dashboard_stats.dart';
import '../utils/format.dart';
import '../widgets/common.dart';
import '../widgets/monitor_modals.dart';
import '../widgets/tailscale_modal.dart';
import 'vm_console_screen.dart';
import 'system_updates_screen.dart';
import 'system_logs_screen.dart';
import 'terminal_screen.dart';

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
  List<Vm> _runningVms = [];
  String? _error;
  Timer? _timer;
  String _username = '';
  int _vmThumbTick = 0;

  NetSample? _lastNet;
  DateTime? _lastNetAt;
  double _netUpRate = 0;
  double _netDownRate = 0;

  VmClient get _vmClient {
    final uri = Uri.parse(ApiClient.instance.baseUrl);
    return VmClient(uri.host);
  }

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

      // Fetch running VMs for live mini card
      try {
        final vms = await _vmClient.listVms();
        _runningVms = vms.where((v) => v.isRunning).toList();
      } catch (_) {}

      if (!mounted) return;
      setState(() {
        _stats = stats;
        _error = null;
        _vmThumbTick++;
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
        backgroundColor: NivaroColors.surfaceContainerHighest,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.largeIncreased)),
        title: Text(title, style: const TextStyle(fontWeight: FontWeight.w700)),
        content: Text(body, style: const TextStyle(color: NivaroColors.textMuted)),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text(title, style: const TextStyle(color: NivaroColors.dangerLight, fontWeight: FontWeight.bold)),
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

  Future<void> _flushCache() async {
    HapticFeedback.mediumImpact();
    try {
      await ApiClient.instance.post('/sys/update');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Memory sync and caches flushed successfully.')),
        );
      }
    } catch (_) {}
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
    if (pct > 85) return NivaroColors.dangerLight;
    if (pct > 65) return NivaroColors.warningLight;
    return NivaroColors.primaryLight;
  }

  Color _memColor(double pct) {
    if (pct > 85) return NivaroColors.dangerLight;
    if (pct > 70) return NivaroColors.purpleLight;
    return NivaroColors.successLight;
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
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 1100),
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
                    color: NivaroColors.primary.withOpacity(0.15),
                    shape: BoxShape.circle,
                    border: Border.all(color: NivaroColors.primary.withOpacity(0.35)),
                  ),
                  alignment: Alignment.center,
                  child: Text(
                    _username.isEmpty ? 'N' : _username.substring(0, 1).toUpperCase(),
                    style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 18, color: NivaroColors.primaryLight),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        _username.isEmpty ? 'Nivaro Administrator' : _username,
                        style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 16),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                      const SizedBox(height: 2),
                      const Row(
                        children: [
                          PulsingStatusDot(color: NivaroColors.success, size: 6.5),
                          SizedBox(width: 6),
                          Text('NivaroOS Server Online', style: TextStyle(color: NivaroColors.textMuted, fontSize: 12, fontWeight: FontWeight.w500)),
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
                      _confirmAndSetState('restart', 'Restart Server', 'This restarts the server hardware. All running apps and VMs will be temporarily interrupted.');
                    } else if (v == 'off') {
                      _confirmAndSetState('off', 'Power Off Server', 'This completely powers off the server hardware.');
                    }
                  },
                  itemBuilder: (context) => const [
                    PopupMenuItem(
                      value: 'restart',
                      child: Row(
                        children: [
                          Icon(Icons.restart_alt_rounded, size: 18, color: NivaroColors.textPrimary),
                          SizedBox(width: 10),
                          Text('Restart Host Server'),
                        ],
                      ),
                    ),
                    PopupMenuItem(
                      value: 'off',
                      child: Row(
                        children: [
                          Icon(Icons.power_settings_new_rounded, size: 18, color: NivaroColors.dangerLight),
                          SizedBox(width: 10),
                          Text('Power Off Server', style: TextStyle(color: NivaroColors.dangerLight)),
                        ],
                      ),
                    ),
                  ],
                ),
              ],
            ),
            const SizedBox(height: 14),

            // Connection Address Pill (Clickable for network test)
            LanBadge(
              address: _host,
              label: 'LAN',
              pingMs: 14,
              onTap: () {
                if (stats != null) {
                  NetworkSpeedTestModal.show(
                    context,
                    primaryNet: stats.primaryNet,
                    netUpRate: _netUpRate,
                    netDownRate: _netDownRate,
                  );
                }
              },
            ),
            const SizedBox(height: 18),

            // Live Virtual Machine Feeds (if any running)
            if (_runningVms.isNotEmpty) ...[
              SectionHeader(
                title: 'Live Virtual Machines',
                subtitle: '${_runningVms.length} running with real-time screen stream',
                trailing: TextButton(
                  onPressed: widget.onOpenVms,
                  child: const Text('View All', style: TextStyle(fontSize: 12.5)),
                ),
              ),
              Builder(builder: (context) {
                final width = MediaQuery.of(context).size.width;
                final isWide = width >= 650;
                final cols = width >= 1050 ? 3 : (isWide ? 2 : 1);

                if (cols > 1) {
                  return GridView.builder(
                    shrinkWrap: true,
                    physics: const NeverScrollableScrollPhysics(),
                    gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                      crossAxisCount: cols,
                      crossAxisSpacing: 12,
                      mainAxisSpacing: 12,
                      childAspectRatio: 1.45,
                    ),
                    itemCount: _runningVms.length,
                    itemBuilder: (context, i) => _buildLiveVmCard(_runningVms[i]),
                  );
                }

                return Column(
                  children: _runningVms.map((vm) => Padding(
                    padding: const EdgeInsets.only(bottom: 12),
                    child: _buildLiveVmCard(vm),
                  )).toList(),
                );
              }),
              const SizedBox(height: 10),
            ],

            // Real-Time System Monitor (Clickable Tiles)
            const SectionHeader(
              title: 'System Health & Metrics',
              subtitle: 'Tap any metric for hardware diagnostics & speedtests',
            ),
            if (stats == null)
              _error != null
                  ? DarkCard(
                      padding: const EdgeInsets.all(24),
                      child: Center(
                        child: Column(
                          children: [
                            const Icon(Icons.error_outline_rounded, color: NivaroColors.dangerLight, size: 36),
                            const SizedBox(height: 10),
                            Text(_error!, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13), textAlign: TextAlign.center),
                            const SizedBox(height: 12),
                            OutlinedButton(onPressed: _load, child: const Text('Retry Connection')),
                          ],
                        ),
                      ),
                    )
                  : const Padding(
                      padding: EdgeInsets.symmetric(vertical: 36),
                      child: Center(child: CircularProgressIndicator()),
                    )
            else ...[
              Builder(builder: (context) {
                final width = MediaQuery.of(context).size.width;
                final isLandscape = MediaQuery.of(context).orientation == Orientation.landscape;
                final cols = width >= 850 ? 4 : (width >= 600 || isLandscape ? 4 : 2);
                final ratio = width >= 850 ? 1.55 : (width >= 600 || isLandscape ? 1.40 : 1.12);

                return GridView.count(
                  crossAxisCount: cols,
                  shrinkWrap: true,
                  physics: const NeverScrollableScrollPhysics(),
                  mainAxisSpacing: 10,
                  crossAxisSpacing: 10,
                  childAspectRatio: ratio,
                  children: [
                    // CPU Card
                    MonitorCard(
                      label: 'CPU Utilization',
                      icon: Icons.memory_rounded,
                      color: _cpuColor(stats.cpuPercent),
                      percent: stats.cpuPercent / 100,
                      onTap: () => CpuDetailModal.show(context, stats),
                      value: Row(
                        crossAxisAlignment: CrossAxisAlignment.baseline,
                        textBaseline: TextBaseline.alphabetic,
                        children: [
                          Text(
                            '${stats.cpuPercent.toStringAsFixed(0)}%',
                            style: const TextStyle(fontSize: 23, fontWeight: FontWeight.w800, color: NivaroColors.textPrimary),
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
                                style: const TextStyle(color: NivaroColors.warningLight, fontSize: 10.5, fontWeight: FontWeight.w700),
                              ),
                            ),
                          ],
                        ],
                      ),
                      subtitle: '${stats.cpuCores} Cores · Hardware specs',
                    ),

                    // Memory Card
                    MonitorCard(
                      label: 'Memory (RAM)',
                      icon: Icons.developer_board_rounded,
                      color: _memColor(stats.memUsedPercent),
                      percent: stats.memUsedPercent / 100,
                      onTap: () => RamDetailModal.show(context, stats),
                      value: Text(
                        '${stats.memUsedPercent.toStringAsFixed(0)}%',
                        style: const TextStyle(fontSize: 23, fontWeight: FontWeight.w800, color: NivaroColors.textPrimary),
                      ),
                      subtitle: '${formatBytes(stats.memUsed)} of ${formatBytes(stats.memTotal)}',
                    ),

                    // Network Card (WAN Speedtest & Link Speed)
                    MonitorCard(
                      label: stats.primaryNet?.name.isNotEmpty == true ? 'Network (${stats.primaryNet!.name})' : 'Network Speed',
                      icon: Icons.swap_vert_rounded,
                      color: NivaroColors.infoLight,
                      onTap: () => NetworkSpeedTestModal.show(
                        context,
                        primaryNet: stats.primaryNet,
                        netUpRate: _netUpRate,
                        netDownRate: _netDownRate,
                      ),
                      value: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          _NetSpeedRow(icon: Icons.arrow_upward_rounded, rate: _netUpRate, label: 'Up'),
                          const SizedBox(height: 3),
                          _NetSpeedRow(icon: Icons.arrow_downward_rounded, rate: _netDownRate, label: 'Down'),
                        ],
                      ),
                      subtitle: 'Speedtest & Link test',
                    ),

                    // Storage Card
                    MonitorCard(
                      label: 'Storage Pools',
                      icon: Icons.pie_chart_rounded,
                      color: NivaroColors.successLight,
                      percent: stats.storageFraction,
                      onTap: () => StorageDetailModal.show(context, stats, onOpenFiles: widget.onOpenFiles),
                      value: Text(
                        stats.storagePercentText,
                        style: const TextStyle(fontSize: 23, fontWeight: FontWeight.w800, color: NivaroColors.textPrimary),
                      ),
                      subtitle: '${formatBytes(stats.storageUsed)} of ${formatBytes(stats.storageTotal)}',
                    ),
                  ],
                );
              }),
              const SizedBox(height: 18),

              // Quick Access Section (Adaptive columns on tablets)
              const SectionHeader(
                title: 'Quick Access',
                subtitle: 'Server management shortcuts & utilities',
              ),
              Builder(builder: (context) {
                final width = MediaQuery.of(context).size.width;
                final isLandscape = MediaQuery.of(context).orientation == Orientation.landscape;
                final cols = width >= 900 ? 8 : (width >= 600 || isLandscape ? 6 : 4);
                final ratio = width >= 900 ? 1.20 : (width >= 600 || isLandscape ? 1.15 : 0.92);

                return GridView.count(
                  crossAxisCount: cols,
                  shrinkWrap: true,
                  physics: const NeverScrollableScrollPhysics(),
                  mainAxisSpacing: 10,
                  crossAxisSpacing: 10,
                  childAspectRatio: ratio,
                  children: [
                    _QuickButton(
                      icon: Icons.vpn_lock_rounded,
                      label: 'Tailscale',
                      onTap: () => TailscaleModal.show(context),
                    ),
                    _QuickButton(
                      icon: Icons.folder_rounded,
                      label: 'Files',
                      onTap: widget.onOpenFiles,
                    ),
                    _QuickButton(
                      icon: Icons.monitor_rounded,
                      label: 'VMs',
                      onTap: widget.onOpenVms,
                    ),
                    _QuickButton(
                      icon: Icons.grid_view_rounded,
                      label: 'Apps',
                      onTap: widget.onOpenApps,
                    ),
                    _QuickButton(
                      icon: Icons.terminal_rounded,
                      label: 'Terminal',
                      onTap: () {
                        HapticFeedback.lightImpact();
                        Navigator.of(context).push(
                          MaterialPageRoute(builder: (_) => const TerminalScreen()),
                        );
                      },
                    ),
                    _QuickButton(
                      icon: Icons.system_update_rounded,
                      label: 'Updates',
                      onTap: () {
                        Navigator.of(context).push(
                          MaterialPageRoute(builder: (_) => const SystemUpdatesScreen()),
                        );
                      },
                    ),
                    _QuickButton(
                      icon: Icons.article_rounded,
                      label: 'Logs',
                      onTap: () {
                        Navigator.of(context).push(
                          MaterialPageRoute(builder: (_) => const SystemLogsScreen()),
                        );
                      },
                    ),
                    _QuickButton(
                      icon: Icons.cleaning_services_rounded,
                      label: 'Flush RAM',
                      onTap: _flushCache,
                    ),
                  ],
                );
              }),
              const SizedBox(height: 20),

              // Storage Drives List (Adaptive responsive grid on tablet)
              const SectionHeader(title: 'Storage Devices & Disks'),
              if (stats.disks.isEmpty)
                const Text('No storage devices detected.', style: TextStyle(color: NivaroColors.textMuted))
              else
                Builder(builder: (context) {
                  final width = MediaQuery.of(context).size.width;
                  final cols = width >= 1050 ? 3 : (width >= 650 ? 2 : 1);

                  if (cols > 1) {
                    return GridView.builder(
                      shrinkWrap: true,
                      physics: const NeverScrollableScrollPhysics(),
                      gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                        crossAxisCount: cols,
                        crossAxisSpacing: 10,
                        mainAxisSpacing: 10,
                        childAspectRatio: 3.2,
                      ),
                      itemCount: stats.disks.length,
                      itemBuilder: (context, i) {
                        final d = stats.disks[i];
                        return DriveCard(
                          label: d.label.isNotEmpty ? d.label : d.mountPoint,
                          percentText: d.percent,
                          fraction: d.fraction,
                          usedBytes: d.usedBytes,
                          sizeBytes: d.sizeBytes,
                          onTap: widget.onOpenFiles,
                        );
                      },
                    );
                  }

                  return Column(
                    children: stats.disks.map((d) => Padding(
                          padding: const EdgeInsets.only(bottom: 8),
                          child: DriveCard(
                            label: d.label.isNotEmpty ? d.label : d.mountPoint,
                            percentText: d.percent,
                            fraction: d.fraction,
                            usedBytes: d.usedBytes,
                            sizeBytes: d.sizeBytes,
                            onTap: widget.onOpenFiles,
                          ),
                        )).toList(),
                  );
                }),
            ],
          ],
        ),
      ),
    ),
  ),
);
}

  Widget _buildLiveVmCard(Vm vm) {
    return DarkCard(
      padding: EdgeInsets.zero,
      onTap: () {
        HapticFeedback.lightImpact();
        Navigator.of(context).push(
          MaterialPageRoute(builder: (_) => VmConsoleScreen(vmName: vm.name)),
        );
      },
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            height: 135,
            decoration: const BoxDecoration(
              color: Colors.black,
              borderRadius: BorderRadius.vertical(top: Radius.circular(NivaroShape.largeIncreased)),
            ),
            clipBehavior: Clip.antiAlias,
            child: Stack(
              children: [
                Image.network(
                  _vmClient.screenshotUrl(vm.name, _vmThumbTick),
                  fit: BoxFit.contain,
                  width: double.infinity,
                  height: double.infinity,
                  gaplessPlayback: true,
                  errorBuilder: (_, __, ___) => const Center(
                    child: Icon(Icons.monitor_rounded, color: NivaroColors.textFaint, size: 36),
                  ),
                ),
                Positioned(
                  top: 10,
                  right: 10,
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
                    decoration: BoxDecoration(
                      color: Colors.black.withOpacity(0.75),
                      borderRadius: BorderRadius.circular(6),
                      border: Border.all(color: NivaroColors.success.withOpacity(0.4)),
                    ),
                    child: const Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        PulsingStatusDot(color: NivaroColors.success, size: 5),
                        SizedBox(width: 4),
                        Text('LIVE', style: TextStyle(color: NivaroColors.successLight, fontSize: 9.5, fontWeight: FontWeight.bold)),
                      ],
                    ),
                  ),
                ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.all(12),
            child: Row(
              children: [
                const Icon(Icons.monitor_rounded, color: NivaroColors.primaryLight, size: 18),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    vm.name,
                    style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                Text(
                  '${vm.vcpus} vCPU · ${(vm.memoryMib / 1024).toStringAsFixed(1)} GB',
                  style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                ),
                const SizedBox(width: 6),
                const Icon(Icons.chevron_right_rounded, size: 18, color: NivaroColors.textFaint),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _QuickButton extends StatelessWidget {
  final IconData icon;
  final String label;
  final VoidCallback? onTap;

  const _QuickButton({
    required this.icon,
    required this.label,
    this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: BorderRadius.circular(16),
        onTap: () {
          HapticFeedback.lightImpact();
          if (onTap != null) onTap!();
        },
        child: Container(
          decoration: BoxDecoration(
            color: NivaroColors.surface,
            borderRadius: BorderRadius.circular(16),
            border: Border.all(color: NivaroColors.borderSubtle),
            boxShadow: const [
              BoxShadow(color: Color(0x2B000000), blurRadius: 8, offset: Offset(0, 2)),
            ],
          ),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Container(
                width: 36,
                height: 36,
                decoration: BoxDecoration(
                  color: NivaroColors.primary.withOpacity(0.12),
                  shape: BoxShape.circle,
                ),
                alignment: Alignment.center,
                child: Icon(icon, color: NivaroColors.primaryLight, size: 19),
              ),
              const SizedBox(height: 6),
              Text(
                label,
                style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 11.5, color: NivaroColors.textSecondary),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
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
        Icon(icon, size: 13, color: NivaroColors.infoLight),
        const SizedBox(width: 4),
        Text(
          '${formatBytes(rate)}/s',
          style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w700, color: NivaroColors.textPrimary),
        ),
      ],
    );
  }
}
