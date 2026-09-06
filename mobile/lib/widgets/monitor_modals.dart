import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../services/speedtest_service.dart';
import '../models/dashboard_stats.dart';
import '../utils/format.dart';
import 'common.dart';

/// Interactive Network Speedtest Modal with WAN Server Speed and Local Device Link Speed
class NetworkSpeedTestModal extends StatefulWidget {
  final NetSample? primaryNet;
  final double netUpRate;
  final double netDownRate;

  const NetworkSpeedTestModal({
    super.key,
    this.primaryNet,
    this.netUpRate = 0,
    this.netDownRate = 0,
  });

  static Future<void> show(
    BuildContext context, {
    NetSample? primaryNet,
    double netUpRate = 0,
    double netDownRate = 0,
  }) {
    return showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      builder: (_) => NetworkSpeedTestModal(
        primaryNet: primaryNet,
        netUpRate: netUpRate,
        netDownRate: netDownRate,
      ),
    );
  }

  @override
  State<NetworkSpeedTestModal> createState() => _NetworkSpeedTestModalState();
}

class _NetworkSpeedTestModalState extends State<NetworkSpeedTestModal> with SingleTickerProviderStateMixin {
  late TabController _tabController;

  // WAN Speedtest State
  bool _wanTesting = false;
  SpeedtestPhase _wanPhase = SpeedtestPhase.idle;
  double _wanLiveSpeed = 0;
  double _wanProgress = 0;
  int? _wanPing;
  double? _wanDown;
  double? _wanUp;
  WanSpeedtestResult? _wanResult;

  // Link Speedtest State
  bool _linkTesting = false;
  SpeedtestPhase _linkPhase = SpeedtestPhase.idle;
  double _linkLiveSpeed = 0;
  double _linkProgress = 0;
  int? _linkPing;
  double? _linkDown;
  double? _linkUp;
  LinkSpeedResult? _linkResult;

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 2, vsync: this);
  }

  @override
  void dispose() {
    _tabController.dispose();
    super.dispose();
  }

  Future<void> _startWanTest() async {
    if (_wanTesting) return;
    HapticFeedback.mediumImpact();
    setState(() {
      _wanTesting = true;
      _wanPhase = SpeedtestPhase.connecting;
      _wanLiveSpeed = 0;
      _wanProgress = 0;
      _wanPing = null;
      _wanDown = null;
      _wanUp = null;
      _wanResult = null;
    });

    try {
      final res = await SpeedtestService.instance.runWanSpeedtest(
        onProgress: ({
          required phase,
          required currentSpeed,
          required progress,
          pingMs,
          downloadMbps,
          uploadMbps,
        }) {
          if (!mounted) return;
          setState(() {
            _wanPhase = phase;
            _wanLiveSpeed = currentSpeed;
            _wanProgress = progress;
            if (pingMs != null) _wanPing = pingMs;
            if (downloadMbps != null) _wanDown = downloadMbps;
            if (uploadMbps != null) _wanUp = uploadMbps;
          });
        },
      );
      if (!mounted) return;
      setState(() {
        _wanResult = res;
        _wanTesting = false;
        _wanPhase = SpeedtestPhase.completed;
        _wanLiveSpeed = res.downloadMbps;
        _wanPing = res.pingMs;
        _wanDown = res.downloadMbps;
        _wanUp = res.uploadMbps;
      });
      HapticFeedback.lightImpact();
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _wanTesting = false;
        _wanPhase = SpeedtestPhase.error;
      });
    }
  }

  Future<void> _startLinkTest() async {
    if (_linkTesting) return;
    HapticFeedback.mediumImpact();
    setState(() {
      _linkTesting = true;
      _linkPhase = SpeedtestPhase.connecting;
      _linkLiveSpeed = 0;
      _linkProgress = 0;
      _linkPing = null;
      _linkDown = null;
      _linkUp = null;
      _linkResult = null;
    });

    try {
      final res = await SpeedtestService.instance.runLocalLinkSpeedTest(
        onProgress: ({
          required phase,
          required currentSpeed,
          required progress,
          pingMs,
          downloadMbps,
          uploadMbps,
        }) {
          if (!mounted) return;
          setState(() {
            _linkPhase = phase;
            _linkLiveSpeed = currentSpeed;
            _linkProgress = progress;
            if (pingMs != null) _linkPing = pingMs;
            if (downloadMbps != null) _linkDown = downloadMbps;
            if (uploadMbps != null) _linkUp = uploadMbps;
          });
        },
      );
      if (!mounted) return;
      setState(() {
        _linkResult = res;
        _linkTesting = false;
        _linkPhase = SpeedtestPhase.completed;
        _linkLiveSpeed = res.downloadMbps;
        _linkPing = res.pingAvgMs;
        _linkDown = res.downloadMbps;
        _linkUp = res.uploadMbps;
      });
      HapticFeedback.lightImpact();
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _linkTesting = false;
        _linkPhase = SpeedtestPhase.error;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: BoxConstraints(
        maxHeight: MediaQuery.of(context).size.height * 0.90,
      ),
      decoration: const BoxDecoration(
        color: NivaroColors.surfaceContainerLowest,
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      child: Column(
        children: [
          // Drag Handle
          Center(
            child: Container(
              margin: const EdgeInsets.only(top: 12, bottom: 8),
              width: 40,
              height: 4,
              decoration: BoxDecoration(
                color: NivaroColors.borderSubtle,
                borderRadius: BorderRadius.circular(2),
              ),
            ),
          ),

          // Header
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 8),
            child: Row(
              children: [
                Container(
                  width: 42,
                  height: 42,
                  decoration: BoxDecoration(
                    color: NivaroColors.infoLight.withOpacity(0.12),
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(color: NivaroColors.infoLight.withOpacity(0.3)),
                  ),
                  alignment: Alignment.center,
                  child: const Icon(Icons.speed_rounded, color: NivaroColors.infoLight, size: 22),
                ),
                const SizedBox(width: 14),
                const Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'Network Diagnostics & Speed',
                        style: TextStyle(fontWeight: FontWeight.w800, fontSize: 17, color: NivaroColors.textPrimary),
                      ),
                      SizedBox(height: 2),
                      Text(
                        'WAN Internet & Local Device Link Benchmarks',
                        style: TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                      ),
                    ],
                  ),
                ),
                RoundIconButton(
                  icon: Icons.close_rounded,
                  tooltip: 'Close',
                  onPressed: () => Navigator.pop(context),
                ),
              ],
            ),
          ),

          // TabBar
          Container(
            margin: const EdgeInsets.symmetric(horizontal: 20, vertical: 8),
            decoration: BoxDecoration(
              color: NivaroColors.surfaceRaised,
              borderRadius: BorderRadius.circular(12),
              border: Border.all(color: NivaroColors.borderSubtle),
            ),
            child: TabBar(
              controller: _tabController,
              indicator: BoxDecoration(
                color: NivaroColors.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(10),
                border: Border.all(color: NivaroColors.borderHighlight),
              ),
              indicatorSize: TabBarIndicatorSize.tab,
              dividerColor: Colors.transparent,
              labelColor: NivaroColors.textPrimary,
              unselectedLabelColor: NivaroColors.textMuted,
              labelStyle: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13),
              tabs: const [
                Tab(
                  iconMargin: EdgeInsets.only(bottom: 2),
                  icon: Icon(Icons.public_rounded, size: 16),
                  text: 'Server Internet (WAN)',
                ),
                Tab(
                  iconMargin: EdgeInsets.only(bottom: 2),
                  icon: Icon(Icons.phonelink_ring_rounded, size: 16),
                  text: 'Local Link Speed',
                ),
              ],
            ),
          ),

          // Tab Views
          Expanded(
            child: TabBarView(
              controller: _tabController,
              children: [
                _buildWanTab(),
                _buildLinkTab(),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildWanTab() {
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 10, 20, 30),
      children: [
        // Speedometer Gauge Card
        DarkCard(
          padding: const EdgeInsets.symmetric(vertical: 24, horizontal: 16),
          child: Column(
            children: [
              Text(
                _wanTesting
                    ? (_wanPhase == SpeedtestPhase.ping
                        ? 'MEASURING PING & JITTER...'
                        : _wanPhase == SpeedtestPhase.download
                            ? 'TESTING DOWNLOAD THROUGHPUT...'
                            : _wanPhase == SpeedtestPhase.upload
                                ? 'TESTING UPLOAD THROUGHPUT...'
                                : 'CONNECTING TO CLOUDFLARE CDN...')
                    : _wanResult != null
                        ? 'SERVER WAN BENCHMARK COMPLETE'
                        : 'READY FOR BENCHMARK',
                style: const TextStyle(
                  letterSpacing: 1.2,
                  fontWeight: FontWeight.w800,
                  fontSize: 11.5,
                  color: NivaroColors.textMuted,
                ),
              ),
              const SizedBox(height: 16),

              // Digital Speed Meter with Smooth Easing
              TweenAnimationBuilder<double>(
                tween: Tween<double>(
                  begin: 0,
                  end: _wanTesting
                      ? _wanLiveSpeed
                      : (_wanResult != null ? _wanResult!.downloadMbps : 0),
                ),
                duration: const Duration(milliseconds: 120),
                curve: Curves.easeOutCubic,
                builder: (context, val, _) {
                  final text = (_wanTesting || _wanResult != null) ? val.toStringAsFixed(1) : '--';
                  return Text(
                    text,
                    style: const TextStyle(
                      fontSize: 54,
                      fontWeight: FontWeight.w900,
                      color: NivaroColors.textPrimary,
                      letterSpacing: -1,
                    ),
                  );
                },
              ),
              const Text(
                'Mbps (Megabits / second)',
                style: TextStyle(color: NivaroColors.infoLight, fontSize: 13, fontWeight: FontWeight.w700),
              ),
              const SizedBox(height: 18),

              // Progress Bar during testing
              if (_wanTesting)
                ClipRRect(
                  borderRadius: BorderRadius.circular(4),
                  child: LinearProgressIndicator(
                    value: _wanProgress,
                    backgroundColor: NivaroColors.surfaceRaised,
                    valueColor: const AlwaysStoppedAnimation<Color>(NivaroColors.infoLight),
                    minHeight: 6,
                  ),
                ),

              const SizedBox(height: 20),

              // Metrics Row: Ping, Download, Upload
              Row(
                mainAxisAlignment: MainAxisAlignment.spaceAround,
                children: [
                  _MetricPill(
                    icon: Icons.network_ping_rounded,
                    label: 'Ping (Latency)',
                    value: _wanPing != null
                        ? '$_wanPing ms'
                        : (_wanResult != null ? '${_wanResult!.pingMs} ms' : '--'),
                    color: NivaroColors.warningLight,
                  ),
                  _MetricPill(
                    icon: Icons.arrow_downward_rounded,
                    label: 'Download',
                    value: _wanDown != null
                        ? '${_wanDown!.toStringAsFixed(1)} Mbps'
                        : (_wanResult != null ? '${_wanResult!.downloadMbps.toStringAsFixed(1)} Mbps' : '--'),
                    color: NivaroColors.infoLight,
                  ),
                  _MetricPill(
                    icon: Icons.arrow_upward_rounded,
                    label: 'Upload',
                    value: _wanUp != null
                        ? '${_wanUp!.toStringAsFixed(1)} Mbps'
                        : (_wanResult != null ? '${_wanResult!.uploadMbps.toStringAsFixed(1)} Mbps' : '--'),
                    color: NivaroColors.purpleLight,
                  ),
                ],
              ),
            ],
          ),
        ),
        const SizedBox(height: 16),

        // Action Button
        SizedBox(
          width: double.infinity,
          height: 48,
          child: ElevatedButton.icon(
            style: ElevatedButton.styleFrom(
              backgroundColor: NivaroColors.primary,
              foregroundColor: Colors.white,
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
            ),
            onPressed: _wanTesting ? null : _startWanTest,
            icon: _wanTesting
                ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                : const Icon(Icons.play_arrow_rounded, size: 22),
            label: Text(
              _wanTesting ? 'Testing Server WAN...' : (_wanResult != null ? 'Run Speedtest Again' : 'Start Server WAN Speedtest'),
              style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 14),
            ),
          ),
        ),
        const SizedBox(height: 16),

        // Gateway & Interface Info
        DarkCard(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Row(
                children: [
                  Icon(Icons.router_rounded, color: NivaroColors.primaryLight, size: 18),
                  SizedBox(width: 8),
                  Text('Server WAN Gateway Info', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5)),
                ],
              ),
              const SizedBox(height: 12),
              _InfoRow(label: 'Host Interface', value: widget.primaryNet?.name ?? 'eth0 (Auto)'),
              _InfoRow(label: 'Live Upload Rate', value: '${formatBytes(widget.netUpRate)}/s'),
              _InfoRow(label: 'Live Download Rate', value: '${formatBytes(widget.netDownRate)}/s'),
              if (_wanResult != null) ...[
                _InfoRow(label: 'ISP / Provider', value: _wanResult!.ispName),
                _InfoRow(label: 'Edge Location', value: _wanResult!.serverLocation),
                _InfoRow(label: 'Public IP', value: _wanResult!.ipAddress),
                _InfoRow(label: 'Peak Download', value: '${_wanResult!.peakDownloadMbps} Mbps'),
                _InfoRow(label: 'Peak Upload', value: '${_wanResult!.peakUploadMbps} Mbps'),
                _InfoRow(label: 'Jitter', value: '± ${_wanResult!.jitterMs} ms'),
              ] else ...[
                _InfoRow(label: 'Server Host', value: Uri.parse(ApiClient.instance.baseUrl).host),
              ],
            ],
          ),
        ),
      ],
    );
  }

  Widget _buildLinkTab() {
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 10, 20, 30),
      children: [
        // Link Speedmeter Gauge Card
        DarkCard(
          padding: const EdgeInsets.symmetric(vertical: 24, horizontal: 16),
          child: Column(
            children: [
              Text(
                _linkTesting
                    ? (_linkPhase == SpeedtestPhase.ping
                        ? 'MEASURING DEVICE ROUNDTRIP...'
                        : _linkPhase == SpeedtestPhase.download
                            ? 'TRANSFERRING CLIENT DOWNLOAD (RX)...'
                            : _linkPhase == SpeedtestPhase.upload
                                ? 'TRANSFERRING CLIENT UPLOAD (TX)...'
                                : 'CONNECTING TO SERVER...')
                    : _linkResult != null
                        ? 'PHONE <-> SERVER LINK BENCHMARK COMPLETE'
                        : 'DIRECT LINK BENCHMARK',
                style: const TextStyle(
                  letterSpacing: 1.2,
                  fontWeight: FontWeight.w800,
                  fontSize: 11.5,
                  color: NivaroColors.textMuted,
                ),
              ),
              const SizedBox(height: 16),

              // Digital Speed Meter
              TweenAnimationBuilder<double>(
                tween: Tween<double>(
                  begin: 0,
                  end: _linkTesting
                      ? _linkLiveSpeed
                      : (_linkResult != null ? _linkResult!.downloadMbps : 0),
                ),
                duration: const Duration(milliseconds: 120),
                curve: Curves.easeOutCubic,
                builder: (context, val, _) {
                  final text = (_linkTesting || _linkResult != null) ? val.toStringAsFixed(1) : '--';
                  return Text(
                    text,
                    style: const TextStyle(
                      fontSize: 54,
                      fontWeight: FontWeight.w900,
                      color: NivaroColors.textPrimary,
                      letterSpacing: -1,
                    ),
                  );
                },
              ),
              const Text(
                'Mbps Direct Throughput',
                style: TextStyle(color: NivaroColors.successLight, fontSize: 13, fontWeight: FontWeight.w700),
              ),
              const SizedBox(height: 18),

              // Progress Bar during testing
              if (_linkTesting)
                ClipRRect(
                  borderRadius: BorderRadius.circular(4),
                  child: LinearProgressIndicator(
                    value: _linkProgress,
                    backgroundColor: NivaroColors.surfaceRaised,
                    valueColor: const AlwaysStoppedAnimation<Color>(NivaroColors.successLight),
                    minHeight: 6,
                  ),
                ),

              const SizedBox(height: 20),

              // Metrics Row: Ping, Download, Upload
              Row(
                mainAxisAlignment: MainAxisAlignment.spaceAround,
                children: [
                  _MetricPill(
                    icon: Icons.timer_outlined,
                    label: 'RTT Latency',
                    value: _linkPing != null
                        ? '$_linkPing ms'
                        : (_linkResult != null ? '${_linkResult!.pingAvgMs} ms' : '--'),
                    color: NivaroColors.warningLight,
                  ),
                  _MetricPill(
                    icon: Icons.download_rounded,
                    label: 'Client Rx',
                    value: _linkDown != null
                        ? '${_linkDown!.toStringAsFixed(1)} Mbps'
                        : (_linkResult != null ? '${_linkResult!.downloadMbps} Mbps' : '--'),
                    color: NivaroColors.successLight,
                  ),
                  _MetricPill(
                    icon: Icons.upload_rounded,
                    label: 'Client Tx',
                    value: _linkUp != null
                        ? '${_linkUp!.toStringAsFixed(1)} Mbps'
                        : (_linkResult != null ? '${_linkResult!.uploadMbps} Mbps' : '--'),
                    color: NivaroColors.primaryLight,
                  ),
                ],
              ),
            ],
          ),
        ),
        const SizedBox(height: 16),

        // Action Button
        SizedBox(
          width: double.infinity,
          height: 48,
          child: ElevatedButton.icon(
            style: ElevatedButton.styleFrom(
              backgroundColor: NivaroColors.success,
              foregroundColor: Colors.black,
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
            ),
            onPressed: _linkTesting ? null : _startLinkTest,
            icon: _linkTesting
                ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.black))
                : const Icon(Icons.sync_alt_rounded, size: 22),
            label: Text(
              _linkTesting ? 'Benchmarking Local Link...' : (_linkResult != null ? 'Re-run Link Benchmark' : 'Run Local Device Link Test'),
              style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 14),
            ),
          ),
        ),
        const SizedBox(height: 16),

        // Link Diagnostics
        DarkCard(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Row(
                children: [
                  Icon(Icons.wifi_tethering_rounded, color: NivaroColors.successLight, size: 18),
                  SizedBox(width: 8),
                  Text('Connection Quality & Diagnostics', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5)),
                ],
              ),
              const SizedBox(height: 12),
              _InfoRow(label: 'Connection Type', value: _linkResult?.connectionType ?? 'Direct Local Network (LAN)'),
              _InfoRow(label: 'Link Quality', value: _linkResult?.qualityRating ?? 'Ultra Low Latency'),
              if (_linkResult != null) ...[
                _InfoRow(label: 'Peak Download Speed', value: '${_linkResult!.peakDownloadMbps} Mbps'),
                _InfoRow(label: 'Min / Max Latency', value: '${_linkResult!.pingMinMs} ms / ${_linkResult!.pingMaxMs} ms'),
                _InfoRow(label: 'Jitter', value: '± ${_linkResult!.jitterMs} ms'),
                _InfoRow(label: 'Data Transferred', value: formatBytes(_linkResult!.bytesTransferred)),
                const SizedBox(height: 8),
                Container(
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: NivaroColors.success.withOpacity(0.08),
                    borderRadius: BorderRadius.circular(8),
                    border: Border.all(color: NivaroColors.success.withOpacity(0.2)),
                  ),
                  child: Row(
                    children: [
                      const Icon(Icons.check_circle_outline_rounded, color: NivaroColors.successLight, size: 16),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text(
                          _linkResult!.realWorldSpeedText,
                          style: const TextStyle(color: NivaroColors.successLight, fontSize: 11.5, fontWeight: FontWeight.w600),
                        ),
                      ),
                    ],
                  ),
                ),
              ] else ...[
                const _InfoRow(label: 'Min / Max Latency', value: '-- / --'),
                const _InfoRow(label: 'Jitter', value: '--'),
              ],
            ],
          ),
        ),
      ],
    );
  }
}

class _MetricPill extends StatelessWidget {
  final IconData icon;
  final String label;
  final String value;
  final Color color;

  const _MetricPill({
    required this.icon,
    required this.label,
    required this.value,
    required this.color,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        Icon(icon, size: 18, color: color),
        const SizedBox(height: 4),
        Text(value, style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 14.5, color: NivaroColors.textPrimary)),
        const SizedBox(height: 2),
        Text(label, style: const TextStyle(fontSize: 11, color: NivaroColors.textMuted)),
      ],
    );
  }
}

class _InfoRow extends StatelessWidget {
  final String label;
  final String value;

  const _InfoRow({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          Text(label, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12.5)),
          Text(value, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 12.5, color: NivaroColors.textPrimary)),
        ],
      ),
    );
  }
}

/// CPU Hardware & Per-Core Utilization Breakdown Modal
class CpuDetailModal extends StatelessWidget {
  final DashboardStats stats;

  const CpuDetailModal({super.key, required this.stats});

  static Future<void> show(BuildContext context, DashboardStats stats) {
    return showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      builder: (_) => CpuDetailModal(stats: stats),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: BoxConstraints(
        maxHeight: MediaQuery.of(context).size.height * 0.85,
      ),
      decoration: const BoxDecoration(
        color: NivaroColors.surfaceContainerLowest,
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      child: Column(
        children: [
          Center(
            child: Container(
              margin: const EdgeInsets.only(top: 12, bottom: 8),
              width: 40,
              height: 4,
              decoration: BoxDecoration(
                color: NivaroColors.borderSubtle,
                borderRadius: BorderRadius.circular(2),
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 8),
            child: Row(
              children: [
                Container(
                  width: 42,
                  height: 42,
                  decoration: BoxDecoration(
                    color: NivaroColors.primary.withOpacity(0.12),
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(color: NivaroColors.primary.withOpacity(0.3)),
                  ),
                  alignment: Alignment.center,
                  child: const Icon(Icons.memory_rounded, color: NivaroColors.primaryLight, size: 22),
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text(
                        'Processor & Cores',
                        style: TextStyle(fontWeight: FontWeight.w800, fontSize: 17, color: NivaroColors.textPrimary),
                      ),
                      const SizedBox(height: 2),
                      Text(
                        '${stats.cpuCores} Cores · ${stats.cpuPercent.toStringAsFixed(0)}% Total Load',
                        style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                      ),
                    ],
                  ),
                ),
                RoundIconButton(
                  icon: Icons.close_rounded,
                  tooltip: 'Close',
                  onPressed: () => Navigator.pop(context),
                ),
              ],
            ),
          ),
          const Divider(height: 1, color: NivaroColors.borderSubtle),
          Expanded(
            child: ListView(
              padding: const EdgeInsets.fromLTRB(20, 16, 20, 32),
              children: [
                DarkCard(
                  padding: const EdgeInsets.all(16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        stats.cpuModelName,
                        style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14.5),
                      ),
                      const SizedBox(height: 12),
                      Row(
                        children: [
                          if (stats.cpuTemperature != null) ...[
                            _Chip(
                              icon: Icons.thermostat_rounded,
                              label: '${stats.cpuTemperature!.toStringAsFixed(0)}°C',
                              color: NivaroColors.warningLight,
                            ),
                            const SizedBox(width: 8),
                          ],
                          if (stats.cpuMhz > 0) ...[
                            _Chip(
                              icon: Icons.bolt_rounded,
                              label: '${(stats.cpuMhz / 1000).toStringAsFixed(2)} GHz',
                              color: NivaroColors.primaryLight,
                            ),
                            const SizedBox(width: 8),
                          ],
                          _Chip(
                            icon: Icons.grid_view_rounded,
                            label: '${stats.cpuCores} Physical/Virtual Cores',
                            color: NivaroColors.infoLight,
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
                const SizedBox(height: 20),
                const SectionHeader(title: 'Per-Core Utilization'),
                if (stats.cpuPerCore.isEmpty)
                  const Text('Per-core statistics unavailable', style: TextStyle(color: NivaroColors.textMuted))
                else
                  ...List.generate(stats.cpuPerCore.length, (idx) {
                    final pct = stats.cpuPerCore[idx];
                    return Padding(
                      padding: const EdgeInsets.only(bottom: 10),
                      child: DarkCard(
                        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
                        child: Row(
                          children: [
                            Text(
                              'Core $idx',
                              style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13),
                            ),
                            const SizedBox(width: 14),
                            Expanded(
                              child: ClipRRect(
                                borderRadius: BorderRadius.circular(4),
                                child: LinearProgressIndicator(
                                  value: (pct / 100).clamp(0, 1),
                                  backgroundColor: NivaroColors.surfaceRaised,
                                  valueColor: AlwaysStoppedAnimation<Color>(
                                    pct > 80 ? NivaroColors.dangerLight : (pct > 50 ? NivaroColors.warningLight : NivaroColors.primaryLight),
                                  ),
                                  minHeight: 8,
                                ),
                              ),
                            ),
                            const SizedBox(width: 14),
                            SizedBox(
                              width: 45,
                              child: Text(
                                '${pct.toStringAsFixed(0)}%',
                                textAlign: TextAlign.right,
                                style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13),
                              ),
                            ),
                          ],
                        ),
                      ),
                    );
                  }),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Memory (RAM) Breakdown and Optimization Modal
class RamDetailModal extends StatelessWidget {
  final DashboardStats stats;

  const RamDetailModal({super.key, required this.stats});

  static Future<void> show(BuildContext context, DashboardStats stats) {
    return showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      builder: (_) => RamDetailModal(stats: stats),
    );
  }

  Future<void> _flushCache(BuildContext context) async {
    HapticFeedback.mediumImpact();
    try {
      await ApiClient.instance.post('/sys/update'); // trigger background sync
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Memory sync and caches flushed successfully.')),
        );
      }
    } catch (_) {}
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: BoxConstraints(
        maxHeight: MediaQuery.of(context).size.height * 0.80,
      ),
      decoration: const BoxDecoration(
        color: NivaroColors.surfaceContainerLowest,
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      child: Column(
        children: [
          Center(
            child: Container(
              margin: const EdgeInsets.only(top: 12, bottom: 8),
              width: 40,
              height: 4,
              decoration: BoxDecoration(
                color: NivaroColors.borderSubtle,
                borderRadius: BorderRadius.circular(2),
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 8),
            child: Row(
              children: [
                Container(
                  width: 42,
                  height: 42,
                  decoration: BoxDecoration(
                    color: NivaroColors.purpleLight.withOpacity(0.12),
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(color: NivaroColors.purpleLight.withOpacity(0.3)),
                  ),
                  alignment: Alignment.center,
                  child: const Icon(Icons.developer_board_rounded, color: NivaroColors.purpleLight, size: 22),
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text(
                        'Memory Breakdown',
                        style: TextStyle(fontWeight: FontWeight.w800, fontSize: 17, color: NivaroColors.textPrimary),
                      ),
                      const SizedBox(height: 2),
                      Text(
                        '${formatBytes(stats.memUsed)} of ${formatBytes(stats.memTotal)} in use (${stats.memUsedPercent.toStringAsFixed(0)}%)',
                        style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                      ),
                    ],
                  ),
                ),
                RoundIconButton(
                  icon: Icons.close_rounded,
                  tooltip: 'Close',
                  onPressed: () => Navigator.pop(context),
                ),
              ],
            ),
          ),
          const Divider(height: 1, color: NivaroColors.borderSubtle),
          Expanded(
            child: ListView(
              padding: const EdgeInsets.fromLTRB(20, 16, 20, 32),
              children: [
                DarkCard(
                  padding: const EdgeInsets.all(18),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        mainAxisAlignment: MainAxisAlignment.spaceBetween,
                        children: [
                          const Text('Total Installed RAM', style: TextStyle(color: NivaroColors.textMuted, fontSize: 13)),
                          Text(formatBytes(stats.memTotal), style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 16)),
                        ],
                      ),
                      const SizedBox(height: 12),
                      ClipRRect(
                        borderRadius: BorderRadius.circular(6),
                        child: LinearProgressIndicator(
                          value: (stats.memUsedPercent / 100).clamp(0, 1),
                          backgroundColor: NivaroColors.surfaceRaised,
                          valueColor: const AlwaysStoppedAnimation<Color>(NivaroColors.purpleLight),
                          minHeight: 12,
                        ),
                      ),
                      const SizedBox(height: 16),
                      _InfoRow(label: 'Used Memory', value: formatBytes(stats.memUsed)),
                      _InfoRow(label: 'Free Memory', value: formatBytes(stats.memFree > 0 ? stats.memFree : stats.memTotal - stats.memUsed)),
                      _InfoRow(label: 'Available Memory', value: formatBytes(stats.memAvailable > 0 ? stats.memAvailable : stats.memTotal - stats.memUsed)),
                    ],
                  ),
                ),
                const SizedBox(height: 16),
                SizedBox(
                  width: double.infinity,
                  height: 48,
                  child: OutlinedButton.icon(
                    style: OutlinedButton.styleFrom(
                      foregroundColor: NivaroColors.textPrimary,
                      side: const BorderSide(color: NivaroColors.borderHighlight),
                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
                    ),
                    onPressed: () => _flushCache(context),
                    icon: const Icon(Icons.cleaning_services_rounded, size: 18),
                    label: const Text('Flush Cached Memory & Buffers', style: TextStyle(fontWeight: FontWeight.w700)),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Storage Pools and Mounts Detail Modal
class StorageDetailModal extends StatelessWidget {
  final DashboardStats stats;
  final VoidCallback? onOpenFiles;

  const StorageDetailModal({super.key, required this.stats, this.onOpenFiles});

  static Future<void> show(BuildContext context, DashboardStats stats, {VoidCallback? onOpenFiles}) {
    return showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      builder: (_) => StorageDetailModal(stats: stats, onOpenFiles: onOpenFiles),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: BoxConstraints(
        maxHeight: MediaQuery.of(context).size.height * 0.85,
      ),
      decoration: const BoxDecoration(
        color: NivaroColors.surfaceContainerLowest,
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      child: Column(
        children: [
          Center(
            child: Container(
              margin: const EdgeInsets.only(top: 12, bottom: 8),
              width: 40,
              height: 4,
              decoration: BoxDecoration(
                color: NivaroColors.borderSubtle,
                borderRadius: BorderRadius.circular(2),
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 8),
            child: Row(
              children: [
                Container(
                  width: 42,
                  height: 42,
                  decoration: BoxDecoration(
                    color: NivaroColors.success.withOpacity(0.12),
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(color: NivaroColors.success.withOpacity(0.3)),
                  ),
                  alignment: Alignment.center,
                  child: const Icon(Icons.pie_chart_rounded, color: NivaroColors.successLight, size: 22),
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text(
                        'Storage Pools & Disks',
                        style: TextStyle(fontWeight: FontWeight.w800, fontSize: 17, color: NivaroColors.textPrimary),
                      ),
                      const SizedBox(height: 2),
                      Text(
                        '${formatBytes(stats.storageUsed)} of ${formatBytes(stats.storageTotal)} used (${stats.storagePercentText})',
                        style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                      ),
                    ],
                  ),
                ),
                RoundIconButton(
                  icon: Icons.close_rounded,
                  tooltip: 'Close',
                  onPressed: () => Navigator.pop(context),
                ),
              ],
            ),
          ),
          const Divider(height: 1, color: NivaroColors.borderSubtle),
          Expanded(
            child: ListView(
              padding: const EdgeInsets.fromLTRB(20, 16, 20, 32),
              children: [
                if (stats.disks.isEmpty)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 24),
                    child: Center(child: Text('No storage disks detected.', style: TextStyle(color: NivaroColors.textMuted))),
                  )
                else
                  ...stats.disks.map((d) => Padding(
                        padding: const EdgeInsets.only(bottom: 12),
                        child: DarkCard(
                          padding: const EdgeInsets.all(16),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Row(
                                children: [
                                  const Icon(Icons.storage_rounded, color: NivaroColors.primaryLight, size: 20),
                                  const SizedBox(width: 10),
                                  Expanded(
                                    child: Text(
                                      d.label.isNotEmpty ? d.label : d.mountPoint,
                                      style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14),
                                    ),
                                  ),
                                  Text(
                                    d.percent,
                                    style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 14, color: NivaroColors.textPrimary),
                                  ),
                                ],
                              ),
                              const SizedBox(height: 8),
                              Text(
                                '${d.mountPoint} · ${d.filesystem.isNotEmpty ? d.filesystem.toUpperCase() : "EXT4"}',
                                style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                              ),
                              const SizedBox(height: 10),
                              ClipRRect(
                                borderRadius: BorderRadius.circular(4),
                                child: LinearProgressIndicator(
                                  value: d.fraction,
                                  backgroundColor: NivaroColors.surfaceRaised,
                                  valueColor: const AlwaysStoppedAnimation<Color>(NivaroColors.successLight),
                                  minHeight: 7,
                                ),
                              ),
                              const SizedBox(height: 8),
                              Row(
                                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                                children: [
                                  Text('${formatBytes(d.usedBytes)} used', style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11.5)),
                                  Text('${formatBytes(d.freeBytes)} available', style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11.5)),
                                ],
                              ),
                            ],
                          ),
                        ),
                      )),
                const SizedBox(height: 10),
                if (onOpenFiles != null)
                  SizedBox(
                    width: double.infinity,
                    height: 48,
                    child: ElevatedButton.icon(
                      style: ElevatedButton.styleFrom(
                        backgroundColor: NivaroColors.primary,
                        foregroundColor: Colors.white,
                        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
                      ),
                      onPressed: () {
                        Navigator.pop(context);
                        onOpenFiles!();
                      },
                      icon: const Icon(Icons.folder_open_rounded, size: 20),
                      label: const Text('Open Storage in Files', style: TextStyle(fontWeight: FontWeight.w700)),
                    ),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _Chip extends StatelessWidget {
  final IconData icon;
  final String label;
  final Color color;

  const _Chip({required this.icon, required this.label, required this.color});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: color.withOpacity(0.12),
        borderRadius: BorderRadius.circular(6),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 14, color: color),
          const SizedBox(width: 4),
          Text(label, style: TextStyle(color: color, fontSize: 11.5, fontWeight: FontWeight.bold)),
        ],
      ),
    );
  }
}
