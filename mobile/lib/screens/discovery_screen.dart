import 'package:flutter/material.dart';
import '../theme.dart';
import '../services/discovery_service.dart';
import '../services/storage_service.dart';
import '../services/api_client.dart';
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
          padding: const EdgeInsets.all(24),
          child: Column(
            children: [
              const SizedBox(height: 24),
              _Brand(),
              const SizedBox(height: 32),
              Row(
                children: [
                  Text(
                    _scanning ? 'Looking for NivaroOS servers on your network…' : 'Servers found nearby',
                    style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 15),
                  ),
                  const Spacer(),
                  if (_scanning)
                    const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
                  else
                    IconButton(onPressed: _scan, icon: const Icon(Icons.refresh)),
                ],
              ),
              const SizedBox(height: 8),
              Expanded(
                child: _found.isEmpty
                    ? _EmptyState(scanning: _scanning, onManual: () => setState(() => _showManual = true))
                    : ListView.separated(
                        itemCount: _found.length,
                        separatorBuilder: (_, __) => const SizedBox(height: 8),
                        itemBuilder: (context, i) {
                          final s = _found[i];
                          return Card(
                            child: ListTile(
                              leading: const CircleAvatar(
                                backgroundColor: NivaroColors.primary,
                                child: Icon(Icons.dns_outlined, color: Colors.white, size: 20),
                              ),
                              title: Text(s.name.isEmpty ? s.host : s.name, style: const TextStyle(fontWeight: FontWeight.w600)),
                              subtitle: Text('${s.host}:${s.port}'),
                              trailing: const Icon(Icons.chevron_right),
                              onTap: () => _connect(s.url),
                            ),
                          );
                        },
                      ),
              ),
              if (!_showManual)
                TextButton(
                  onPressed: () => setState(() => _showManual = true),
                  child: const Text("Can't find your server? Enter its address"),
                ),
              if (_showManual) ...[
                const Divider(height: 32),
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

class _Brand extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return const Column(
      children: [
        Icon(Icons.cloud_outlined, size: 48, color: NivaroColors.primary),
        SizedBox(height: 8),
        Text('NivaroOS', style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700)),
        Text('Your personal cloud, in your pocket.', style: TextStyle(color: NivaroColors.textMuted, fontSize: 13)),
      ],
    );
  }
}

class _EmptyState extends StatelessWidget {
  final bool scanning;
  final VoidCallback onManual;
  const _EmptyState({required this.scanning, required this.onManual});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.wifi_find_outlined, size: 40, color: NivaroColors.textMuted.withOpacity(0.6)),
          const SizedBox(height: 12),
          Text(
            scanning ? 'Scanning your local network…' : 'No servers found on this network yet.',
            style: const TextStyle(color: NivaroColors.textMuted),
            textAlign: TextAlign.center,
          ),
          if (!scanning) ...[
            const SizedBox(height: 4),
            const Text(
              'Some networks block this kind of scan - you can always enter\nyour server\'s address directly.',
              style: TextStyle(color: NivaroColors.textMuted, fontSize: 12),
              textAlign: TextAlign.center,
            ),
          ],
        ],
      ),
    );
  }
}
