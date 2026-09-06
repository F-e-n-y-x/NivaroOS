import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:package_info_plus/package_info_plus.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../services/storage_service.dart';
import '../widgets/common.dart';
import 'login_screen.dart';
import 'server_profiles_screen.dart';
import 'system_updates_screen.dart';
import 'system_logs_screen.dart';
import 'companion_devices_screen.dart';
import '../services/device_sync_service.dart';

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key});

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  String _username = '';
  String _serverUrl = '';
  String _appVersion = 'v1.0.0';

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final u = await StorageService.instance.getUsername();
    final s = await StorageService.instance.getServerUrl();
    final info = await PackageInfo.fromPlatform();
    if (mounted) {
      setState(() {
        _username = u ?? 'User';
        _serverUrl = s ?? '';
        _appVersion = 'v${info.version}+${info.buildNumber}';
      });
    }
  }

  Future<void> _logout() async {
    HapticFeedback.mediumImpact();
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Sign Out'),
        content: const Text('Are you sure you want to sign out of this NivaroOS server?'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.dangerLight),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Sign Out'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    DeviceSyncService.instance.stopAutoSync();
    await StorageService.instance.clearAll();
    ApiClient.instance.clearSession();
    if (mounted) {
      Navigator.of(context).pushAndRemoveUntil(
        MaterialPageRoute(builder: (_) => const LoginScreen()),
        (route) => false,
      );
    }
  }
  Future<void> _showPowerSheet() async {
    HapticFeedback.mediumImpact();
    showModalBottomSheet(
      context: context,
      backgroundColor: Colors.transparent,
      builder: (context) => Container(
        padding: const EdgeInsets.all(22),
        decoration: const BoxDecoration(
          color: NivaroColors.surfaceContainerLowest,
          borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Row(
              children: [
                Icon(Icons.power_settings_new_rounded, color: NivaroColors.dangerLight, size: 24),
                SizedBox(width: 12),
                Text('Server Power Operations', style: TextStyle(fontWeight: FontWeight.w800, fontSize: 17)),
              ],
            ),
            const SizedBox(height: 16),
            ListTile(
              leading: const Icon(Icons.restart_alt_rounded, color: NivaroColors.warningLight),
              title: const Text('Reboot Host Server', style: TextStyle(fontWeight: FontWeight.w700)),
              subtitle: const Text('Safely restarts operating system and running services', style: TextStyle(color: NivaroColors.textMuted, fontSize: 11.5)),
              onTap: () {
                Navigator.pop(context);
                _powerAction('restart');
              },
            ),
            ListTile(
              leading: const Icon(Icons.power_off_rounded, color: NivaroColors.dangerLight),
              title: const Text('Shutdown Host Server', style: TextStyle(fontWeight: FontWeight.w700)),
              subtitle: const Text('Completely powers off the host machine', style: TextStyle(color: NivaroColors.textMuted, fontSize: 11.5)),
              onTap: () {
                Navigator.pop(context);
                _powerAction('shutdown');
              },
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _powerAction(String action) async {
    final isReboot = action == 'restart';
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceContainerHighest,
        title: Text(isReboot ? 'Reboot Server?' : 'Shut Down Server?'),
        content: Text(
          isReboot
              ? 'This will safely reboot your NivaroOS host server and all running VMs/containers.'
              : 'This will safely power off your NivaroOS server completely.',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.danger),
            onPressed: () => Navigator.pop(context, true),
            child: Text(isReboot ? 'Reboot Now' : 'Power Off Now'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;

    try {
      await ApiClient.instance.post('/v1/sys/power/$action');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Server ${isReboot ? "reboot" : "shutdown"} dispatched.')),
        );
      }
    } catch (_) {
      try {
        await ApiClient.instance.put('/sys/power', body: {'action': action});
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('Power command error: $e'), backgroundColor: NivaroColors.danger),
          );
        }
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: NivaroColors.background,
      appBar: AppBar(
        title: const Text('Settings & Preferences', style: TextStyle(fontWeight: FontWeight.w800)),
      ),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 820),
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              // User Card
              DarkCard(
                padding: const EdgeInsets.all(16),
                child: Row(
                  children: [
                    Container(
                      width: 52,
                      height: 52,
                      decoration: BoxDecoration(
                        gradient: const LinearGradient(colors: [NivaroColors.primaryLight, NivaroColors.primaryDark]),
                        shape: BoxShape.circle,
                        border: Border.all(color: NivaroColors.borderHighlight, width: 1.5),
                      ),
                      alignment: Alignment.center,
                      child: Text(
                        _username.isNotEmpty ? _username[0].toUpperCase() : 'N',
                        style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 20, color: Colors.white),
                      ),
                    ),
                    const SizedBox(width: 14),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(_username.isNotEmpty ? _username : 'Administrator', style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 16)),
                          const SizedBox(height: 2),
                          Text(_serverUrl, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 24),

              // Server & Hypervisor Management
              const SectionHeader(title: 'Server & Hypervisor'),
              DarkCard(
                padding: EdgeInsets.zero,
                child: Column(
                  children: [
                    ListTile(
                      leading: const Icon(Icons.dns_rounded, color: NivaroColors.primaryLight),
                      title: const Text('Server Profiles', style: TextStyle(fontWeight: FontWeight.w600)),
                      subtitle: const Text('Manage multiple NivaroOS server connections'),
                      trailing: const Icon(Icons.chevron_right_rounded),
                      onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const ServerProfilesScreen())),
                    ),
                    const Divider(height: 1),
                    ListTile(
                      leading: const Icon(Icons.devices_rounded, color: NivaroColors.primaryLight),
                      title: const Text('Companion Devices', style: TextStyle(fontWeight: FontWeight.w600)),
                      subtitle: const Text('Connected client devices, device storage & sync'),
                      trailing: const Icon(Icons.chevron_right_rounded),
                      onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const CompanionDevicesScreen())),
                    ),
                    const Divider(height: 1),
                    ListTile(
                      leading: const Icon(Icons.system_update_rounded, color: NivaroColors.successLight),
                      title: const Text('System Updates', style: TextStyle(fontWeight: FontWeight.w600)),
                      subtitle: const Text('Check for NivaroOS release updates'),
                      trailing: const Icon(Icons.chevron_right_rounded),
                      onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const SystemUpdatesScreen())),
                    ),
                    const Divider(height: 1),
                    ListTile(
                      leading: const Icon(Icons.receipt_long_rounded, color: NivaroColors.infoLight),
                      title: const Text('System Logs', style: TextStyle(fontWeight: FontWeight.w600)),
                      subtitle: const Text('Gateway, libvirt hypervisor and kernel logs'),
                      trailing: const Icon(Icons.chevron_right_rounded),
                      onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const SystemLogsScreen())),
                    ),
                    const Divider(height: 1),
                    ListTile(
                      leading: const Icon(Icons.power_settings_new_rounded, color: NivaroColors.dangerLight),
                      title: const Text('Server Power Controls', style: TextStyle(fontWeight: FontWeight.w600)),
                      subtitle: const Text('Safely reboot or power off NivaroOS host server'),
                      trailing: const Icon(Icons.chevron_right_rounded),
                      onTap: _showPowerSheet,
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 24),

              // About Card
              const SectionHeader(title: 'About NivaroOS Mobile'),
              DarkCard(
                padding: const EdgeInsets.all(16),
                child: Row(
                  children: [
                    Container(
                      width: 44,
                      height: 44,
                      decoration: BoxDecoration(color: NivaroColors.primary.withOpacity(0.15), borderRadius: BorderRadius.circular(12)),
                      child: const Icon(Icons.cloud_done_rounded, color: NivaroColors.primaryLight),
                    ),
                    const SizedBox(width: 14),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          const Text('NivaroOS Mobile Companion', style: TextStyle(fontWeight: FontWeight.w800, fontSize: 14.5)),
                          const SizedBox(height: 2),
                          Text('$_appVersion · Personal Cloud in your pocket', style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 32),

              // Sign out button
              OutlinedButton.icon(
                onPressed: _logout,
                icon: const Icon(Icons.logout_rounded, color: NivaroColors.dangerLight, size: 18),
                label: const Text('Sign Out of Server', style: TextStyle(color: NivaroColors.dangerLight, fontWeight: FontWeight.w700)),
                style: OutlinedButton.styleFrom(
                  side: BorderSide(color: NivaroColors.dangerLight.withOpacity(0.4)),
                  padding: const EdgeInsets.symmetric(vertical: 14),
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
