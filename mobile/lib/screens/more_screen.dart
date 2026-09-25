import 'package:flutter/material.dart';

import '../services/api_client.dart';
import '../services/storage_service.dart';
import '../ui/ui.dart';
import '../widgets/tailscale_modal.dart';
import 'companion_devices_screen.dart';
import 'host_desktop_screen.dart';
import 'server_profiles_screen.dart';
import 'settings_screen.dart';
import 'system_logs_screen.dart';
import 'system_updates_screen.dart';
import 'terminal_screen.dart';

/// The fifth tab: everything that isn't Home, Files, Apps or VMs - server
/// tools, this phone, and the app's own settings. Built on the v2 design
/// system; each row pushes the existing screen.
class MoreScreen extends StatefulWidget {
  const MoreScreen({super.key});

  @override
  State<MoreScreen> createState() => _MoreScreenState();
}

class _MoreScreenState extends State<MoreScreen> {
  String? _username;
  String? _serverHost;

  // Pending server updates, for the Updates row. Null until known; a failed
  // check just leaves the row's plain description.
  int? _updateCount;
  int _securityCount = 0;

  // NivaroOS itself, for its own row: the installed version and, when one
  // is out, the new one. Null until known.
  String? _serverVersion;
  String? _serverLatest;

  @override
  void initState() {
    super.initState();
    _loadAccount();
    _loadUpdates();
  }

  Future<void> _loadAccount() async {
    final username = await StorageService.instance.getUsername();
    final url = await StorageService.instance.getServerUrl();
    if (!mounted) return;
    setState(() {
      _username = username;
      _serverHost = url == null ? null : (Uri.tryParse(url)?.host ?? url);
    });
  }

  Future<void> _loadUpdates() async {
    _loadServerVersion();
    try {
      final res = await ApiClient.instance.get('/sys/packages/check');
      final data = res['data'];
      if (data is! Map || !mounted) return;
      setState(() {
        _updateCount = (data['count'] as num?)?.toInt() ?? 0;
        _securityCount = (data['security_count'] as num?)?.toInt() ?? 0;
      });
    } catch (_) {}
  }

  Future<void> _loadServerVersion() async {
    try {
      final res = await ApiClient.instance.get('/sys/version/check');
      final data = res['data'];
      if (data is! Map || !mounted) return;
      final latest = data['version'] is Map ? (data['version'] as Map)['version']?.toString() : null;
      setState(() {
        _serverVersion = data['current_version']?.toString();
        _serverLatest = data['need_update'] == true && latest != null && latest.isNotEmpty ? latest : null;
      });
    } catch (_) {}
  }

  void _openUpdates(UpdatesPage page) {
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => SystemUpdatesScreen(page: page))).then((_) => _loadUpdates());
  }

  void _push(Widget screen) {
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => screen));
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final username = _username;
    final known = username != null && username.isNotEmpty;
    final updates = _updateCount;

    return AppScaffold.slivers(
      title: 'More',
      slivers: [
        SliverList.list(children: [
          // The account sits in its own group, so its 40dp avatar (which
          // moves the text to 88dp) never shares an edge with 24dp-icon rows.
          // Tapping it opens the server list, where switching happens.
          TileGroup(children: [
            ListTile(
              leading: CircleAvatar(
                backgroundColor: scheme.primaryContainer,
                foregroundColor: scheme.onPrimaryContainer,
                child: known ? Text(username.substring(0, 1).toUpperCase()) : const Icon(Icons.person_outline),
              ),
              title: Text(known ? username : 'Unknown user'),
              subtitle: Text(_serverHost == null ? 'No server' : '$_serverHost · Switch server'),
              onTap: () => _push(const ServerProfilesScreen()),
            ),
          ]),
          TileGroup(title: 'Server', children: [
            ListTile(
              leading: const Icon(Icons.system_update_alt_outlined),
              title: const Text('NivaroOS update'),
              subtitle: Text(_serverLatest != null
                  ? 'Version $_serverLatest is available'
                  : _serverVersion != null && _serverVersion!.isNotEmpty
                      ? 'Up to date · $_serverVersion'
                      : 'NivaroOS and this app'),
              trailing: _serverLatest != null ? const StatusChip(label: 'New', status: Status.info) : null,
              onTap: () => _openUpdates(UpdatesPage.nivaroos),
            ),
            ListTile(
              leading: const Icon(Icons.inventory_2_outlined),
              title: const Text('System packages'),
              // The security count sits under the summary rather than in
              // the trailing slot, where at large text sizes it squeezed the
              // summary into a narrow column. It deserves attention, not
              // alarm, so it is a warning.
              subtitle: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(updates == null
                      ? 'The server\'s Linux packages'
                      : updates == 0
                          ? 'Up to date'
                          : updates == 1
                              ? '1 update available'
                              : '$updates updates available'),
                  if (_securityCount > 0) ...[
                    const SizedBox(height: Space.xs),
                    StatusChip(label: '$_securityCount security', status: Status.warning, icon: Icons.shield_outlined),
                  ],
                ],
              ),
              isThreeLine: _securityCount > 0,
              onTap: () => _openUpdates(UpdatesPage.packages),
            ),
            ListTile(
              leading: const Icon(Icons.receipt_long_outlined),
              title: const Text('Logs'),
              subtitle: const Text('Gateway, hypervisor and kernel'),
              onTap: () => _push(const SystemLogsScreen()),
            ),
            ListTile(
              leading: const Icon(Icons.terminal_outlined),
              title: const Text('Terminal'),
              subtitle: const Text('A shell on the server'),
              onTap: () => _push(const TerminalScreen()),
            ),
            ListTile(
              leading: const Icon(Icons.screen_share_outlined),
              title: const Text('Host desktop'),
              subtitle: const Text("The server's own screen"),
              onTap: () => _push(const HostDesktopScreen()),
            ),
            ListTile(
              leading: const Icon(Icons.vpn_key_outlined),
              title: const Text('Tailscale'),
              subtitle: const Text('Reach the server from anywhere'),
              onTap: () => TailscaleModal.show(context),
            ),
          ]),
          TileGroup(title: 'Devices', children: [
            ListTile(
              leading: const Icon(Icons.devices_outlined),
              title: const Text('Companion devices'),
              subtitle: const Text('This phone, storage sharing and your other devices'),
              onTap: () => _push(const CompanionDevicesScreen()),
            ),
          ]),
          TileGroup(title: 'App', children: [
            const ThemeModeTile(),
            ListTile(
              leading: const Icon(Icons.settings_outlined),
              title: const Text('Settings'),
              subtitle: const Text('Notifications, battery, server power, sign out'),
              onTap: () => _push(const SettingsScreen()),
            ),
          ]),
        ]),
      ],
    );
  }
}
