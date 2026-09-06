import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:package_info_plus/package_info_plus.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../services/storage_service.dart';
import '../widgets/common.dart';
import 'discovery_screen.dart';
import 'login_screen.dart';

/// Settings screen for managing account sessions, server connection,
/// system power, and app diagnostics.
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key});

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  String _username = '';
  String _version = '1.0.0';

  @override
  void initState() {
    super.initState();
    _loadInfo();
  }

  Future<void> _loadInfo() async {
    final username = await StorageService.instance.getUsername();
    try {
      final info = await PackageInfo.fromPlatform();
      if (!mounted) return;
      setState(() {
        _username = username ?? '';
        _version = info.version;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _username = username ?? '';
      });
    }
  }

  Future<void> _logout() async {
    HapticFeedback.lightImpact();
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceRaised,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
        title: const Text('Log Out'),
        content: const Text("You will be logged out of this NivaroOS instance. You'll need your password to reconnect."),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.danger),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Log Out'),
          ),
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

  Future<void> _confirmSystemState(String state, String title, String body) async {
    HapticFeedback.mediumImpact();
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceRaised,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
        title: Text(title),
        content: Text(body),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.danger),
            onPressed: () => Navigator.pop(context, true),
            child: Text(title),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      await ApiClient.instance.put('/sys/state/$state');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Server $state signal sent.'),
            backgroundColor: NivaroColors.success,
            behavior: SnackBarBehavior.floating,
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(e.toString().replaceFirst('Exception: ', '')),
            backgroundColor: NivaroColors.danger,
            behavior: SnackBarBehavior.floating,
          ),
        );
      }
    }
  }

  Future<void> _changeServer() async {
    HapticFeedback.lightImpact();
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceRaised,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
        title: const Text('Change Server'),
        content: const Text("This disconnects from the current server and opens the discovery scanner."),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Continue'),
          ),
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
          padding: const EdgeInsets.fromLTRB(20, 14, 20, 40),
          children: [
            // Top Bar
            Row(
              children: [
                IconButton(
                  icon: const Icon(Icons.arrow_back_rounded),
                  onPressed: () => Navigator.of(context).pop(),
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    'Settings',
                    style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                          fontWeight: FontWeight.w800,
                          letterSpacing: -0.5,
                        ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 20),

            // Account & Server Card
            const SectionHeader(title: 'Account & Connection'),
            const SizedBox(height: 10),
            DarkCard(
              padding: const EdgeInsets.all(16),
              child: Column(
                children: [
                  Row(
                    children: [
                      Container(
                        width: 52,
                        height: 52,
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
                        alignment: Alignment.center,
                        child: Text(
                          _username.isNotEmpty ? _username.substring(0, 1).toUpperCase() : 'N',
                          style: const TextStyle(color: Colors.white, fontWeight: FontWeight.w800, fontSize: 22),
                        ),
                      ),
                      const SizedBox(width: 14),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              _username.isNotEmpty ? _username : 'Administrator',
                              style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 16),
                            ),
                            const SizedBox(height: 2),
                            Text(
                              ApiClient.instance.baseUrl,
                              style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                            ),
                          ],
                        ),
                      ),
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                        decoration: BoxDecoration(
                          color: NivaroColors.success.withValues(alpha: 0.15),
                          borderRadius: BorderRadius.circular(NivaroShape.full),
                          border: Border.all(color: NivaroColors.success.withValues(alpha: 0.3)),
                        ),
                        child: const Text('Online', style: TextStyle(color: NivaroColors.success, fontSize: 11, fontWeight: FontWeight.w700)),
                      ),
                    ],
                  ),
                  const SizedBox(height: 16),
                  const Divider(height: 1),
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: const Icon(Icons.dns_rounded, color: NivaroColors.primaryLight),
                    title: const Text('Change Server Host', style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600)),
                    subtitle: const Text('Switch to another NivaroOS machine', style: TextStyle(fontSize: 12)),
                    trailing: const Icon(Icons.chevron_right_rounded, color: NivaroColors.textMuted),
                    onTap: _changeServer,
                  ),
                  const Divider(height: 1),
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: const Icon(Icons.logout_rounded, color: NivaroColors.danger),
                    title: const Text('Log Out', style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: NivaroColors.danger)),
                    subtitle: const Text('Clear current session authentication', style: TextStyle(fontSize: 12)),
                    trailing: const Icon(Icons.chevron_right_rounded, color: NivaroColors.textMuted),
                    onTap: _logout,
                  ),
                ],
              ),
            ),
            const SizedBox(height: 24),

            // System Management Card
            const SectionHeader(title: 'Host Power & Operations'),
            const SizedBox(height: 10),
            DarkCard(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
              child: Column(
                children: [
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: const Icon(Icons.restart_alt_rounded, color: NivaroColors.warning),
                    title: const Text('Restart NivaroOS Host', style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600)),
                    subtitle: const Text('Reboots the underlying OS and all containers', style: TextStyle(fontSize: 12)),
                    onTap: () => _confirmSystemState('restart', 'Restart Host', 'Are you sure you want to reboot the NivaroOS host? All running apps and VMs will be restarted.'),
                  ),
                  const Divider(height: 1),
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: const Icon(Icons.power_settings_new_rounded, color: NivaroColors.danger),
                    title: const Text('Shut Down Host', style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: NivaroColors.danger)),
                    subtitle: const Text('Powers off the server completely', style: TextStyle(fontSize: 12)),
                    onTap: () => _confirmSystemState('off', 'Power Off Host', 'Are you sure you want to power off the NivaroOS machine? You will need physical or IPMI power access to turn it back on.'),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 24),

            // About & Diagnostics Card
            const SectionHeader(title: 'About NivaroOS Mobile'),
            const SizedBox(height: 10),
            DarkCard(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
              child: Column(
                children: [
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: const Icon(Icons.info_outline_rounded, color: NivaroColors.primaryLight),
                    title: const Text('Client Version', style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600)),
                    trailing: Text('v$_version', style: const TextStyle(color: NivaroColors.textMuted, fontWeight: FontWeight.w600)),
                  ),
                  const Divider(height: 1),
                  const ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: Icon(Icons.verified_rounded, color: NivaroColors.success),
                    title: Text('Architecture', style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600)),
                    trailing: Text('Material 3 · RFB 6143 Native', style: TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

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

