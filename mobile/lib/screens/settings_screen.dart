import 'package:flutter/material.dart';
import 'package:package_info_plus/package_info_plus.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../services/storage_service.dart';
import '../widgets/common.dart';
import 'discovery_screen.dart';
import 'login_screen.dart';

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key});

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  String _username = '';
  String _version = '';

  @override
  void initState() {
    super.initState();
    _loadInfo();
  }

  Future<void> _loadInfo() async {
    final username = await StorageService.instance.getUsername();
    final info = await PackageInfo.fromPlatform();
    if (!mounted) return;
    setState(() {
      _username = username ?? '';
      _version = info.version;
    });
  }

  Future<void> _logout() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Log Out'),
        content: const Text("You'll need to sign in again to use this server."),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          TextButton(onPressed: () => Navigator.pop(context, true), child: const Text('Log Out')),
        ],
      ),
    );
    if (confirmed != true) return;
    await StorageService.instance.clearSession();
    ApiClient.instance.clearSession();
    if (!mounted) return;
    Navigator.of(context).pushAndRemoveUntil(
      MaterialPageRoute(builder: (_) => const _RestartToLogin()),
      (route) => false,
    );
  }

  Future<void> _changeServer() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Change Server'),
        content: const Text("This forgets the current server and account - you'll pick a new one to connect to."),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          TextButton(onPressed: () => Navigator.pop(context, true), child: const Text('Continue')),
        ],
      ),
    );
    if (confirmed != true) return;
    await StorageService.instance.clearAll();
    ApiClient.instance.clearSession();
    if (!mounted) return;
    Navigator.of(context).pushAndRemoveUntil(
      MaterialPageRoute(builder: (_) => const DiscoveryScreen()),
      (route) => false,
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 12, 20, 24),
          children: [
            Row(
              children: [
                RoundIconButton(icon: Icons.arrow_back_rounded, onPressed: () => Navigator.of(context).pop()),
                const SizedBox(width: 12),
                const Expanded(child: Text('Settings', style: nivaroTitleStyle)),
              ],
            ),
            const SizedBox(height: 24),
            _SectionLabel('Account'),
            const SizedBox(height: 10),
            DarkCard(
              padding: EdgeInsets.zero,
              child: Column(
                children: [
                  ListTile(
                    leading: CircleAvatar(
                      backgroundColor: NivaroColors.primary,
                      child: Text(
                        _username.isEmpty ? '?' : _username.substring(0, 1).toUpperCase(),
                        style: const TextStyle(color: Colors.white, fontWeight: FontWeight.w700),
                      ),
                    ),
                    title: Text(_username.isEmpty ? 'Signed in' : _username, style: const TextStyle(fontWeight: FontWeight.w600)),
                    subtitle: Text(ApiClient.instance.baseUrl, style: const TextStyle(fontSize: 12, color: NivaroColors.textMuted)),
                  ),
                  const Divider(height: 1),
                  ListTile(
                    leading: const Icon(Icons.dns_outlined, color: NivaroColors.textMuted),
                    title: const Text('Change Server'),
                    onTap: _changeServer,
                  ),
                  const Divider(height: 1),
                  ListTile(
                    leading: const Icon(Icons.logout, color: NivaroColors.danger),
                    title: const Text('Log Out', style: TextStyle(color: NivaroColors.danger)),
                    onTap: _logout,
                  ),
                ],
              ),
            ),
            const SizedBox(height: 24),
            _SectionLabel('About'),
            const SizedBox(height: 10),
            DarkCard(
              padding: EdgeInsets.zero,
              child: ListTile(
                leading: const Icon(Icons.info_outline, color: NivaroColors.textMuted),
                title: const Text('App Version'),
                trailing: Text(_version, style: const TextStyle(color: NivaroColors.textMuted)),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _SectionLabel extends StatelessWidget {
  final String text;
  const _SectionLabel(this.text);

  @override
  Widget build(BuildContext context) {
    return Text(text, style: nivaroSectionLabelStyle);
  }
}

/// A trivial re-bootstrap after logout - avoids importing main.dart's
/// private state class from here just to reuse three lines of logic.
class _RestartToLogin extends StatefulWidget {
  const _RestartToLogin();
  @override
  State<_RestartToLogin> createState() => _RestartToLoginState();
}

class _RestartToLoginState extends State<_RestartToLogin> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      final serverUrl = await StorageService.instance.getServerUrl();
      if (!mounted) return;
      if (serverUrl == null || serverUrl.isEmpty) {
        Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => const DiscoveryScreen()));
      } else {
        Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => const LoginScreen()));
      }
    });
  }

  @override
  Widget build(BuildContext context) => const Scaffold(body: Center(child: CircularProgressIndicator()));
}
