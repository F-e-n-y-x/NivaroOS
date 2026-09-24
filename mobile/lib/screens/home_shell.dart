import 'package:flutter/material.dart';
import 'dashboard_screen.dart';
import 'files_screen.dart';
import 'vm_list_screen.dart';
import 'apps_screen.dart';
import 'more_screen.dart';
import 'settings_screen.dart';
import 'login_screen.dart';
import '../services/storage_service.dart';
import '../services/permission_service.dart';
import '../services/device_sync_service.dart';
import '../services/api_client.dart';
import '../ui/theme/spacing.dart';

/// One top-level destination: its label and its outlined / filled icon
/// pair (filled only while selected, per the design brief).
typedef _Destination = ({String label, IconData icon, IconData selectedIcon});

/// The app shell (design brief §5): five destinations in a NavigationBar
/// on phones, or a NavigationRail from 600dp, with each tab kept alive in
/// an IndexedStack so switching doesn't reload it.
class HomeShell extends StatefulWidget {
  const HomeShell({super.key});

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<HomeShell> {
  static const _homeIndex = 0;
  static const _filesIndex = 1;
  static const _appsIndex = 2;
  static const _vmsIndex = 3;

  static const List<_Destination> _destinations = [
    (label: 'Home', icon: Icons.home_outlined, selectedIcon: Icons.home),
    (label: 'Files', icon: Icons.folder_outlined, selectedIcon: Icons.folder),
    (label: 'Apps', icon: Icons.apps_outlined, selectedIcon: Icons.apps),
    (label: 'VMs', icon: Icons.computer_outlined, selectedIcon: Icons.computer),
    // No filled "more" glyph exists; the indicator pill marks selection.
    (label: 'More', icon: Icons.more_horiz, selectedIcon: Icons.more_horiz),
  ];

  int _index = _homeIndex;
  bool _showingReauth = false;
  final _filesKey = GlobalKey<FilesScreenState>();

  void _switchToTab(int i) {
    setState(() => _index = i);
  }

  late final List<Widget> _screens = [
    DashboardScreen(
      onOpenFiles: () => _switchToTab(_filesIndex),
      onOpenVms: () => _switchToTab(_vmsIndex),
      onOpenApps: () => _switchToTab(_appsIndex),
    ),
    FilesScreen(key: _filesKey),
    AppsScreen(
      onOpenFiles: () => _switchToTab(_filesIndex),
      onOpenVms: () => _switchToTab(_vmsIndex),
      onOpenSettings: _openSettings,
    ),
    const VmListScreen(),
    const MoreScreen(),
  ];

  @override
  void initState() {
    super.initState();
    ApiClient.sessionExpiredNotifier.addListener(_onSessionExpired);
    _startBackgroundSync();
  }

  // The single place background sync gets started (previously also fired
  // from main.dart on a cold start with an existing session, and from
  // login_screen.dart right after a fresh login - HomeShell.initState()
  // runs in both of those cases too, since it's the very next screen either
  // way, so those were redundant, near-simultaneous duplicate calls firing
  // exactly when the Navigator was mid-transition. Confirmed via a real
  // on-device crash log (android.app.RemoteServiceException
  // $ForegroundServiceDidNotStartInTimeException) that the native foreground
  // service's startForeground() call was never reached within Android's
  // window when that happened.
  //
  // Requests the notification permission FIRST and waits for it - the
  // background service's whole visible purpose is its persistent
  // notification, and starting it before Android has decided whether
  // notifications are even allowed is asking for exactly the kind of
  // platform-level foreground-service edge case that got us here - then
  // defers the actual native service start to after this screen's first
  // frame (addPostFrameCallback) instead of doing it mid-build/transition.
  Future<void> _startBackgroundSync() async {
    await PermissionService.requestInitialPermissions();
    if (!mounted) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      DeviceSyncService.instance.enableBackgroundSync();
    });
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
        Navigator.of(context)
            .push<bool>(
          MaterialPageRoute(
            builder: (_) => LoginScreen(
              isReauth: true,
              initialUsername: username,
            ),
          ),
        )
            .then((_) {
          _showingReauth = false;
        });
      });
    }
  }

  void _openSettings() {
    Navigator.of(context)
        .push(MaterialPageRoute(builder: (_) => const SettingsScreen()));
  }

  // Back steps out of whatever the current tab has open (Files: selection,
  // search, sub-folder, extra tab), then returns to Home. On Home the shell
  // lets the pop through: it is always the only route (every way in -
  // launch, login, switching server - replaces the whole stack), so the
  // system closes the app and Android 14+ can play its predictive
  // back-to-home animation.
  void _onPopInvoked(bool didPop, Object? result) {
    if (didPop) return;
    if (_index == _filesIndex) {
      final handled = _filesKey.currentState?.handleBack() ?? false;
      if (handled) return;
    }
    setState(() => _index = _homeIndex);
  }

  @override
  Widget build(BuildContext context) {
    final useRail = MediaQuery.sizeOf(context).width >= Space.mediumWidth;
    final body = IndexedStack(index: _index, children: _screens);

    return PopScope(
      canPop: _index == _homeIndex,
      onPopInvokedWithResult: _onPopInvoked,
      child: useRail
          ? Scaffold(
              body: Row(
                children: [
                  // The rail sits on the leading edge, so it takes the
                  // status bar, cutout and gesture insets on that side; the
                  // tab's own Scaffold handles the rest.
                  SafeArea(
                    right: false,
                    child: NavigationRail(
                      selectedIndex: _index,
                      onDestinationSelected: _switchToTab,
                      labelType: NavigationRailLabelType.all,
                      groupAlignment: -0.85,
                      destinations: [
                        for (final d in _destinations)
                          NavigationRailDestination(
                            icon: Icon(d.icon),
                            selectedIcon: Icon(d.selectedIcon),
                            label: Text(d.label),
                          ),
                      ],
                    ),
                  ),
                  Expanded(
                    child: MediaQuery.removePadding(context: context, removeLeft: true, child: body),
                  ),
                ],
              ),
            )
          : Scaffold(
              body: body,
              bottomNavigationBar: NavigationBar(
                selectedIndex: _index,
                onDestinationSelected: _switchToTab,
                destinations: [
                  for (final d in _destinations)
                    NavigationDestination(
                      icon: Icon(d.icon),
                      selectedIcon: Icon(d.selectedIcon),
                      label: d.label,
                    ),
                ],
              ),
            ),
    );
  }
}
