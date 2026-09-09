import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'dashboard_screen.dart';
import 'files_screen.dart';
import 'vm_list_screen.dart';
import 'apps_screen.dart';
import 'settings_screen.dart';
import 'login_screen.dart';
import '../services/storage_service.dart';
import '../services/permission_service.dart';
import '../services/device_sync_service.dart';
import '../services/api_client.dart';
import '../widgets/common.dart';

/// Main Application Shell with floating pill navigation bar and smooth tab transitions.
class HomeShell extends StatefulWidget {
  const HomeShell({super.key});

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<HomeShell> {
  static const _filesTabIndex = 1;

  int _index = 0;
  String _avatarInitial = '?';
  bool _showingReauth = false;
  final _filesKey = GlobalKey<FilesScreenState>();

  void _switchToTab(int i) {
    setState(() => _index = i);
  }

  late final List<Widget> _screens = [
    DashboardScreen(
      onOpenFiles: () => _switchToTab(1),
      onOpenVms: () => _switchToTab(2),
      onOpenApps: () => _switchToTab(3),
    ),
    FilesScreen(key: _filesKey),
    const VmListScreen(),
    const AppsScreen(),
  ];

  @override
  void initState() {
    super.initState();
    _loadAvatar();
    PermissionService.requestInitialPermissions();
    DeviceSyncService.instance.startAutoSync();
    ApiClient.sessionExpiredNotifier.addListener(_onSessionExpired);
  }

  @override
  void dispose() {
    ApiClient.sessionExpiredNotifier.removeListener(_onSessionExpired);
    super.dispose();
  }

  void _onSessionExpired() {
    if (!mounted || _showingReauth) return;
    if (ApiClient.sessionExpiredNotifier.value) {
      _showingReauth = true;
      StorageService.instance.getUsername().then((username) {
        if (!mounted) return;
        Navigator.of(context).push<bool>(
          MaterialPageRoute(
            builder: (_) => LoginScreen(
              isReauth: true,
              initialUsername: username,
            ),
          ),
        ).then((_) {
          _showingReauth = false;
        });
      });
    }
  }

  Future<void> _loadAvatar() async {
    final username = await StorageService.instance.getUsername();
    if (!mounted || username == null || username.isEmpty) return;
    setState(() => _avatarInitial = username.substring(0, 1).toUpperCase());
  }

  void _openSettings() {
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => const SettingsScreen()));
  }

  Future<void> _onPopInvoked(bool didPop, Object? result) async {
    if (didPop) return;
    if (_index == _filesTabIndex) {
      final handled = _filesKey.currentState?.handleBack() ?? false;
      if (handled) return;
    }
    if (_index != 0) {
      setState(() => _index = 0);
      return;
    }
    SystemNavigator.pop();
  }

  @override
  Widget build(BuildContext context) {
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: _onPopInvoked,
      child: Scaffold(
        extendBody: true,
        body: Stack(
          children: [
            IndexedStack(index: _index, children: _screens),
            Positioned(
              left: 0,
              right: 0,
              bottom: 0,
              child: FloatingNavBar(
                currentIndex: _index,
                onTap: _switchToTab,
                onAvatarTap: _openSettings,
                avatarInitial: _avatarInitial,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
