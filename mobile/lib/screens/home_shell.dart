import 'package:flutter/material.dart';
import 'dashboard_screen.dart';
import 'files_screen.dart';
import 'vm_list_screen.dart';
import 'settings_screen.dart';

/// The app's main navigation - a bottom tab bar, real native screens behind
/// each tab, nothing rendered via a webview here (the one deliberate
/// exception, a VM's live console, lives one level deeper - see
/// vm_console_screen.dart's own doc comment for why).
class HomeShell extends StatefulWidget {
  const HomeShell({super.key});

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<HomeShell> {
  int _index = 0;

  static const _screens = [
    DashboardScreen(),
    FilesScreen(),
    VmListScreen(),
    SettingsScreen(),
  ];

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: IndexedStack(index: _index, children: _screens),
      bottomNavigationBar: BottomNavigationBar(
        currentIndex: _index,
        onTap: (i) => setState(() => _index = i),
        items: const [
          BottomNavigationBarItem(icon: Icon(Icons.dashboard_outlined), activeIcon: Icon(Icons.dashboard), label: 'Dashboard'),
          BottomNavigationBarItem(icon: Icon(Icons.folder_outlined), activeIcon: Icon(Icons.folder), label: 'Files'),
          BottomNavigationBarItem(icon: Icon(Icons.dns_outlined), activeIcon: Icon(Icons.dns), label: 'VMs'),
          BottomNavigationBarItem(icon: Icon(Icons.settings_outlined), activeIcon: Icon(Icons.settings), label: 'Settings'),
        ],
      ),
    );
  }
}
