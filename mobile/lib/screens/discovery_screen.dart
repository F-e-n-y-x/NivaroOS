import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme.dart';
import '../services/discovery_service.dart';
import '../services/storage_service.dart';
import '../services/api_client.dart';
import '../widgets/common.dart';
import 'login_screen.dart';

/// mDNS server discovery screen with real-time LAN scanner and manual connection fallback.
class DiscoveryScreen extends StatefulWidget {
  const DiscoveryScreen({super.key});

  @override
  State<DiscoveryScreen> createState() => _DiscoveryScreenState();
}

class _DiscoveryScreenState extends State<DiscoveryScreen> with SingleTickerProviderStateMixin {
  final _service = DiscoveryService();
  final _manualController = TextEditingController();
  final List<DiscoveredServer> _found = [];
  bool _scanning = true;
  bool _showManual = false;
  late final AnimationController _pulseAnim;

  @override
  void initState() {
    super.initState();
    _pulseAnim = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 1800),
    )..repeat(reverse: true);
    _scan();
  }

  @override
  void dispose() {
    _pulseAnim.dispose();
    _manualController.dispose();
    super.dispose();
  }

  Future<void> _scan() async {
    HapticFeedback.lightImpact();
    setState(() {
      _scanning = true;
      _found.clear();
    });
    try {
      await for (final server in _service.discover()) {
        if (!mounted) return;
        setState(() {
          if (!_found.any((s) => s.host == server.host && s.port == server.port)) {
            _found.add(server);
          }
        });
      }
    } catch (_) {}
    if (!mounted) return;
    setState(() => _scanning = false);
  }

  Future<void> _connect(String url) async {
    HapticFeedback.mediumImpact();
    final normalized = url.startsWith('http') ? url : 'http://$url';
    await StorageService.instance.setServerUrl(normalized);
    await ApiClient.instance.init();
    if (!mounted) return;
    Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => const LoginScreen()));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(24, 20, 24, 24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              // Header
              Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'Connect Server',
                          style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                                fontWeight: FontWeight.w800,
                                letterSpacing: -0.5,
                              ),
                        ),
                        const SizedBox(height: 2),
                        Text(
                          _scanning ? 'Searching for NivaroOS servers on Wi-Fi...' : '${_found.length} server(s) found on network',
                          style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13),
                        ),
                      ],
                    ),
                  ),
                  if (_scanning)
                    const SizedBox(
                      width: 22,
                      height: 22,
                      child: CircularProgressIndicator(strokeWidth: 2.5),
                    )
                  else
                    IconButton(
                      icon: const Icon(Icons.refresh_rounded, size: 22),
                      tooltip: 'Rescan Network',
                      onPressed: _scan,
                    ),
                ],
              ),
              const SizedBox(height: 20),

              // Server List / Scanning animation
              Expanded(
                child: _found.isEmpty
                    ? _EmptyState(scanning: _scanning, pulseAnim: _pulseAnim, onManual: () => setState(() => _showManual = true))
                    : RefreshIndicator(
                        onRefresh: _scan,
                        color: NivaroColors.primaryLight,
                        backgroundColor: NivaroColors.surfaceRaised,
                        child: ListView.separated(
                          itemCount: _found.length,
                          separatorBuilder: (_, __) => const SizedBox(height: 12),
                          itemBuilder: (context, i) {
                            final s = _found[i];
                            return DarkCard(
                              onTap: () => _connect(s.url),
                              child: Row(
                                children: [
                                  Container(
                                    width: 48,
                                    height: 48,
                                    decoration: const BoxDecoration(
                                      shape: BoxShape.circle,
                                      gradient: LinearGradient(
                                        colors: [
                                          NivaroColors.primary,
                                          NivaroColors.primaryLight,
                                        ],
                                        begin: Alignment.topLeft,
                                        end: Alignment.bottomRight,
                                      ),
                                    ),
                                    child: const Icon(Icons.dns_rounded, color: Colors.white, size: 24),
                                  ),
                                  const SizedBox(width: 14),
                                  Expanded(
                                    child: Column(
                                      crossAxisAlignment: CrossAxisAlignment.start,
                                      children: [
                                        Text(
                                          s.name.isNotEmpty ? s.name : s.host,
                                          style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 16),
                                        ),
                                        const SizedBox(height: 2),
                                        Text(
                                          '${s.host}:${s.port}',
                                          style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13),
                                        ),
                                      ],
                                    ),
                                  ),
                                  FilledButton(
                                    onPressed: () => _connect(s.url),
                                    style: FilledButton.styleFrom(
                                      backgroundColor: NivaroColors.primary,
                                      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
                                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.medium)),
                                    ),
                                    child: const Text('Connect', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 13)),
                                  ),
                                ],
                              ),
                            );
                          },
                        ),
                      ),
              ),
              const SizedBox(height: 12),

              // Manual Connection Drawer / Card
              if (!_showManual)
                SizedBox(
                  width: double.infinity,
                  height: 48,
                  child: OutlinedButton.icon(
                    onPressed: () => setState(() => _showManual = true),
                    icon: const Icon(Icons.edit_note_rounded, size: 20),
                    label: const Text('Connect Manually via IP / Domain'),
                    style: OutlinedButton.styleFrom(
                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
                    ),
                  ),
                ),
              if (_showManual)
                DarkCard(
                  padding: const EdgeInsets.all(16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          const Text('Manual Host Connection', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 15)),
                          const Spacer(),
                          IconButton(
                            icon: const Icon(Icons.close_rounded, size: 18),
                            onPressed: () => setState(() => _showManual = false),
                          ),
                        ],
                      ),
                      const SizedBox(height: 10),
                      TextField(
                        controller: _manualController,
                        keyboardType: TextInputType.url,
                        autocorrect: false,
                        decoration: InputDecoration(
                          labelText: 'Server Host / IP',
                          hintText: '192.168.1.100 or nivaro.local:80',
                          prefixIcon: const Icon(Icons.link_rounded, size: 20),
                          filled: true,
                          fillColor: NivaroColors.surfaceRaised,
                          border: OutlineInputBorder(
                            borderRadius: BorderRadius.circular(NivaroShape.medium),
                            borderSide: const BorderSide(color: NivaroColors.borderSubtle),
                          ),
                        ),
                      ),
                      const SizedBox(height: 12),
                      SizedBox(
                        width: double.infinity,
                        height: 46,
                        child: FilledButton(
                          onPressed: () {
                            final v = _manualController.text.trim();
                            if (v.isNotEmpty) _connect(v);
                          },
                          style: FilledButton.styleFrom(
                            backgroundColor: NivaroColors.primary,
                            shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.medium)),
                          ),
                          child: const Text('Connect', style: TextStyle(fontWeight: FontWeight.w700)),
                        ),
                      ),
                    ],
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

class _EmptyState extends StatelessWidget {
  final bool scanning;
  final AnimationController pulseAnim;
  final VoidCallback onManual;
  const _EmptyState({required this.scanning, required this.pulseAnim, required this.onManual});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          AnimatedBuilder(
            animation: pulseAnim,
            builder: (context, child) {
              return Container(
                width: 90,
                height: 90,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: NivaroColors.primary.withValues(alpha: 0.08 + (pulseAnim.value * 0.12)),
                  border: Border.all(
                    color: NivaroColors.primary.withValues(alpha: 0.2 + (pulseAnim.value * 0.3)),
                    width: 1.5,
                  ),
                ),
                child: Icon(
                  scanning ? Icons.radar_rounded : Icons.wifi_off_rounded,
                  size: 42,
                  color: NivaroColors.primaryLight,
                ),
              );
            },
          ),
          const SizedBox(height: 20),
          Text(
            scanning ? 'Scanning Local Network...' : 'No NivaroOS Instances Found',
            style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 17),
          ),
          const SizedBox(height: 8),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 32),
            child: Text(
              scanning
                  ? 'Searching for mDNS broadcast beacons from your NivaroOS server on this Wi-Fi network.'
                  : 'Make sure your server is powered on and connected to the same subnet, or enter its IP address directly.',
              style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13, height: 1.4),
              textAlign: TextAlign.center,
            ),
          ),
        ],
      ),
    );
  }
}

