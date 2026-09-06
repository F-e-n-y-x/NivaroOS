import 'package:flutter/material.dart';
import 'dashboard_screen.dart';
import 'files_screen.dart';
import 'vm_list_screen.dart';
import 'apps_screen.dart';
import 'settings_screen.dart';
import '../services/storage_service.dart';
import '../widgets/common.dart';

/// The app's main navigation. Real native screens behind each tab - nothing
/// rendered via a webview here (the one deliberate exception, a VM's live
/// console, lives one level deeper - see vm_console_screen.dart's own doc
/// comment for why). The nav itself is a floating pill + a separate avatar
/// button (opens Settings), matching the reference app rather than a stock
/// full-width Material bottom bar.
class HomeShell extends StatefulWidget {
  const HomeShell({super.key});

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<HomeShell> {
  int _index = 0;
  String _avatarInitial = '?';

  static const _screens = [
    DashboardScreen(),
    FilesScreen(),
    VmListScreen(),
    AppsScreen(),
  ];

  @override
  void initState() {
    super.initState();
    _loadAvatar();
  }

  Future<void> _loadAvatar() async {
    final username = await StorageService.instance.getUsername();
    if (!mounted || username == null || username.isEmpty) return;
    setState(() => _avatarInitial = username.substring(0, 1).toUpperCase());
  }

  void _openSettings() {
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => const SettingsScreen()));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
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
              onTap: (i) => setState(() => _index = i),
              onAvatarTap: _openSettings,
              avatarInitial: _avatarInitial,
            ),
          ),
        ],
      ),
    );
  }
}
