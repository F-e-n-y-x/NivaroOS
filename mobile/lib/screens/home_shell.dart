import 'dart:async';

import 'package:flutter/material.dart';
import 'dashboard_screen.dart';
import 'files_screen.dart';
import 'vm_list_screen.dart';
import 'apps_screen.dart';
import 'more_screen.dart';
import 'settings_screen.dart';
import 'login_screen.dart';
import '../services/storage_service.dart';
import '../services/session_service.dart';
import '../services/api_client.dart';
import '../services/share_intent.dart';
import '../services/share_upload.dart';
import 'share_upload_sheet.dart';
import 'download_station/download_station_screen.dart';
import 'download_station/ds_add_sheet.dart';
import 'download_station/ds_add_torrent_sheet.dart';
import 'download_station/torrents_screen.dart';
import '../services/download_station_api.dart' show extractLinks, extractTorrentSources;
import '../ui/theme/spacing.dart';
import '../ui/widgets/floating_nav_bar.dart';

/// One top-level destination: its label and its outlined / filled icon
/// pair (filled only while selected, per the design brief).
typedef ShellDestination = ({String label, IconData icon, IconData selectedIcon});

/// The app shell (design brief §5): five destinations in a floating
/// navigation bar on phones, or a NavigationRail from 600dp, with each tab
/// kept alive in an IndexedStack so switching doesn't reload it.
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
    ShareIntent.pending.addListener(_onShared);
    ShareIntent.pendingTorrent.addListener(_onShared);
    ShareIntent.pendingFiles.addListener(_onShared);
    ShareIntent.pendingOpen.addListener(_onShared);
    // After the first frame, not during the route transition: registers
    // the phone and schedules the 15-minute heartbeat. Nothing here asks
    // for a permission; features ask when they need one.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && ApiClient.instance.hasSession) SessionService.started();
      ShareIntent.listen().then((_) => _onShared());
    });
  }

  @override
  void dispose() {
    ApiClient.sessionExpiredNotifier.removeListener(_onSessionExpired);
    ShareIntent.pending.removeListener(_onShared);
    ShareIntent.pendingTorrent.removeListener(_onShared);
    ShareIntent.pendingFiles.removeListener(_onShared);
    ShareIntent.pendingOpen.removeListener(_onShared);
    super.dispose();
  }

  bool _showingShare = false;

  // A link shared to "Download on server" (also the one that started the
  // app): offer to queue it in Download Station.
  Future<void> _onShared() async {
    if (ShareIntent.pendingFiles.value != null || ShareIntent.pendingOpen.value != null) return _onSharedFiles();
    var text = ShareIntent.pending.value;
    final file = ShareIntent.pendingTorrent.value;
    if ((text == null && file == null) || !mounted || _showingShare || !ApiClient.instance.hasSession) return;
    _showingShare = true;
    ShareIntent.pending.value = null;
    ShareIntent.pendingTorrent.value = null;
    final server = ApiClient.instance.baseUrl;
    try {
      // Magnets and .torrent files (links or opened) go to the torrent
      // engine; any other links left then go to the downloader.
      final torrents = text == null ? const <String>[] : extractTorrentSources(text);
      if (file != null || torrents.isNotEmpty) {
        final n = await showAddTorrentSheet(context, text: text ?? '', files: [?file]);
        if (!mounted) return;
        if (n != null && n > 0) {
          final nav = Navigator.of(context);
          ScaffoldMessenger.of(context).showSnackBar(SnackBar(
            content: Text(n == 1 ? 'Torrent added to Download Station' : '$n torrents added to Download Station'),
            action: SnackBarAction(label: 'Open', onPressed: () => nav.push(MaterialPageRoute<void>(builder: (_) => const TorrentsScreen()))),
          ));
        }
        for (final t in torrents) {
          text = text!.replaceAll(t, ' ');
        }
        if (text == null || extractLinks(text).isEmpty) return;
      }
      final added = await showAddDownloadSheet(context, text: text!, pickServer: true);
      if (!mounted) return;
      final nav = Navigator.of(context);
      final messenger = ScaffoldMessenger.of(context);
      // Picking another server in the sheet switched the app to it: start
      // over on that one, as the server list does.
      if (ApiClient.instance.baseUrl != server) nav.pushAndRemoveUntil(MaterialPageRoute(builder: (_) => const HomeShell()), (_) => false);
      if (added != null && added > 0) {
        messenger.showSnackBar(SnackBar(
          content: Text(added == 1 ? 'Added to Download Station' : '$added downloads added to Download Station'),
          action: SnackBarAction(label: 'Open', onPressed: () => nav.push(MaterialPageRoute<void>(builder: (_) => const DownloadStationScreen()))),
        ));
      }
    } finally {
      _showingShare = false;
    }
    // Another link shared while the sheet was open.
    if (_anyShared) unawaited(_onShared());
  }

  bool get _anyShared =>
      ShareIntent.pending.value != null || ShareIntent.pendingTorrent.value != null || ShareIntent.pendingFiles.value != null || ShareIntent.pendingOpen.value != null;

  // Files shared to "Upload to NivaroOS": offer the upload. A tapped
  // upload notification: its folder in Files, or how each file went.
  Future<void> _onSharedFiles() async {
    if (!mounted || _showingShare || !ApiClient.instance.hasSession) return;
    final files = ShareIntent.pendingFiles.value;
    final open = ShareIntent.pendingOpen.value;
    _showingShare = true;
    ShareIntent.pendingFiles.value = null;
    ShareIntent.pendingOpen.value = null;
    final server = ApiClient.instance.baseUrl;
    try {
      if (files != null) {
        final b = await showShareUploadSheet(context, shared: files);
        if (!mounted || b == null) return;
        final messenger = ScaffoldMessenger.of(context);
        if (ApiClient.instance.baseUrl != server) Navigator.of(context).pushAndRemoveUntil(MaterialPageRoute(builder: (_) => const HomeShell()), (_) => false);
        messenger.showSnackBar(SnackBar(content: Text('Uploading ${b.items.length == 1 ? '1 file' : '${b.items.length} files'} to ${b.destDir}. Progress is in your notifications.')));
      } else if (open != null) {
        final folder = open.folder;
        if (folder != null) {
          setState(() => _index = _filesIndex);
          _filesKey.currentState?.openServerFolder(folder);
          return;
        }
        final b = (await ShareUploadStore.open()).load(open.batch);
        if (b == null || !mounted) return;
        final again = await showShareUploadSheet(context, batch: b);
        if (again != null && mounted) {
          ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Trying again. Progress is in your notifications.')));
        }
      }
    } finally {
      _showingShare = false;
    }
    if (_anyShared) unawaited(_onShared());
  }

  // Only a real 401 from /users/refresh gets here (ApiClient keeps the
  // session through proxy errors and outages). Sign-in opens over the
  // current tab, so the user carries on where they were.
  void _onSessionExpired() {
    if (!mounted || _showingReauth || !ApiClient.sessionExpiredNotifier.value) return;
    _showingReauth = true;
    StorageService.instance.getUsername().then((username) {
      if (!mounted) return;
      Navigator.of(context)
          .push<bool>(MaterialPageRoute(builder: (_) => LoginScreen(isReauth: true, initialUsername: username)))
          .then((ok) {
        _showingReauth = false;
        if (ok == true) SessionService.started();
      });
    });
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
    return PopScope(
      canPop: _index == _homeIndex,
      onPopInvokedWithResult: _onPopInvoked,
      child: ShellFrame(
        selectedIndex: _index,
        onDestinationSelected: _switchToTab,
        body: IndexedStack(index: _index, children: _screens),
      ),
    );
  }
}

/// The shell's navigation around [body] (one tab, or the stack of them):
/// on phones the floating bar with the content scrolling behind it, from
/// 600dp a NavigationRail on the leading edge. The screenshot tests draw
/// tabs in it too, so they show as they do in the app.
class ShellFrame extends StatelessWidget {
  const ShellFrame({super.key, required this.selectedIndex, required this.onDestinationSelected, required this.body});

  final int selectedIndex;
  final ValueChanged<int> onDestinationSelected;
  final Widget body;

  static const List<ShellDestination> destinations = [
    (label: 'Home', icon: Icons.home_outlined, selectedIcon: Icons.home),
    (label: 'Files', icon: Icons.folder_outlined, selectedIcon: Icons.folder),
    (label: 'Apps', icon: Icons.apps_outlined, selectedIcon: Icons.apps),
    (label: 'VMs', icon: Icons.computer_outlined, selectedIcon: Icons.computer),
    // No filled "more" glyph exists; the indicator pill marks selection.
    (label: 'More', icon: Icons.more_horiz, selectedIcon: Icons.more_horiz),
  ];

  @override
  Widget build(BuildContext context) {
    final useRail = MediaQuery.sizeOf(context).width >= Space.mediumWidth;
    if (useRail) {
      // Not a Scaffold, like the floating bar's shell: the tab's own
      // Scaffold shows snack bars, clear of its FAB.
      return Material(
        color: Theme.of(context).scaffoldBackgroundColor,
        child: Row(
          children: [
            // The rail sits on the leading edge, so it takes the status
            // bar, cutout and gesture insets on that side; the tab's own
            // Scaffold handles the rest.
            SafeArea(
              right: false,
              child: NavigationRail(
                selectedIndex: selectedIndex,
                onDestinationSelected: onDestinationSelected,
                labelType: NavigationRailLabelType.all,
                groupAlignment: -0.85,
                destinations: [
                  for (final d in destinations)
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
      );
    }
    return FloatingBarScaffold(
      body: body,
      bar: FloatingNavigationBar(
        selectedIndex: selectedIndex,
        onDestinationSelected: onDestinationSelected,
        destinations: [
          for (final d in destinations)
            NavigationDestination(
              icon: Icon(d.icon),
              selectedIcon: Icon(d.selectedIcon),
              label: d.label,
            ),
        ],
      ),
    );
  }
}
