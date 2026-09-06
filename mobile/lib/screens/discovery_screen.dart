import 'package:flutter/material.dart';
import '../theme.dart';
import '../services/discovery_service.dart';
import '../services/storage_service.dart';
import '../services/api_client.dart';
import '../widgets/common.dart';
import 'login_screen.dart';

class DiscoveryScreen extends StatefulWidget {
  const DiscoveryScreen({super.key});

  @override
  State<DiscoveryScreen> createState() => _DiscoveryScreenState();
}

class _DiscoveryScreenState extends State<DiscoveryScreen> {
  final _service = DiscoveryService();
  final _manualController = TextEditingController();
  final List<DiscoveredServer> _found = [];
  bool _scanning = true;
  bool _showManual = false;

  @override
  void initState() {
    super.initState();
    _scan();
  }

  Future<void> _scan() async {
    setState(() {
      _scanning = true;
      _found.clear();
    });
    await for (final server in _service.discover()) {
      if (!mounted) return;
      setState(() => _found.add(server));
    }
    if (!mounted) return;
    setState(() => _scanning = false);
  }

  Future<void> _connect(String url) async {
    final normalized = url.startsWith('http') ? url : 'http://$url';
    await StorageService.instance.setServerUrl(normalized);
    await ApiClient.instance.init();
    if (!mounted) return;
    Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => const LoginScreen()));
  }

  @override
  void dispose() {
    _manualController.dispose();
    super.dispose();
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
            const Text('Connect', style: nivaroTitleStyle),
            const SizedBox(height: 8),
            Row(
              children: [
                Expanded(
                  child: Text(
                    _scanning ? 'Scanning for NivaroOS servers on your network' : 'Servers found nearby',
                    style: const TextStyle(color: NivaroColors.textMuted, fontSize: 14),
                  ),
                ),
                if (_scanning)
                  const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2))
                else
                  RoundIconButton(icon: Icons.refresh_rounded, onPressed: _scan, size: 34),
              ],
            ),
            const SizedBox(height: 16),
            Expanded(
              child: _found.isEmpty
                  ? _EmptyState(scanning: _scanning)
                  : ListView.separated(
                      itemCount: _found.length,
                      separatorBuilder: (_, __) => const SizedBox(height: 12),
                      itemBuilder: (context, i) {
                        final s = _found[i];
                        return InkWell(
                          borderRadius: BorderRadius.circular(22),
                          onTap: () => _connect(s.url),
                          child: DarkCard(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Row(
                                  children: [
                                    Expanded(
                                      child: Text(
                                        s.name.isEmpty ? s.host : s.name,
                                        style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 17),
                                      ),
                                    ),
                                    Container(
                                      width: 44,
                                      height: 44,
                                      decoration: BoxDecoration(color: Colors.white, borderRadius: BorderRadius.circular(12)),
                                      alignment: Alignment.center,
                                      child: const Icon(Icons.dns_rounded, color: NivaroColors.primary),
                                    ),
                                  ],
                                ),
                                const SizedBox(height: 16),
                                LanBadge(address: '${s.host}:${s.port}'),
                              ],
                            ),
                          ),
                        );
                      },
                    ),
            ),
            const SizedBox(height: 8),
            if (!_showManual)
              SizedBox(
                width: double.infinity,
                child: OutlinedButton(
                  onPressed: () => setState(() => _showManual = true),
                  child: const Text('Manual Connection'),
                ),
              ),
            if (_showManual) ...[
              TextField(
                controller: _manualController,
                keyboardType: TextInputType.url,
                autocorrect: false,
                decoration: const InputDecoration(
                  labelText: 'Server address',
                  hintText: '192.168.1.10 or nivaroos.mydomain.com',
                ),
              ),
              const SizedBox(height: 12),
              SizedBox(
                width: double.infinity,
                child: ElevatedButton(
                  onPressed: () {
                    final v = _manualController.text.trim();
                    if (v.isNotEmpty) _connect(v);
                  },
                  child: const Text('Connect'),
                ),
              ),
            ],
          ],
        ),
      ),
      ),
    );
  }
}

class _EmptyState extends StatelessWidget {
  final bool scanning;
  const _EmptyState({required this.scanning});

  @override
  Widget build(BuildContext context) {
    return DarkCard(
      padding: const EdgeInsets.all(24),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            scanning ? Icons.wifi_find_rounded : Icons.wifi_off_rounded,
            size: 36,
            color: NivaroColors.textMuted,
          ),
          const SizedBox(height: 12),
          Text(
            scanning ? 'Looking for your server…' : 'No Device Found',
            style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 16),
          ),
          const SizedBox(height: 8),
          const Text(
            'Make sure your device is powered on and connected to the same network as this phone.',
            style: TextStyle(color: NivaroColors.textMuted, fontSize: 13),
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }
}
