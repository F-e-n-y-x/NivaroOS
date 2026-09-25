import 'dart:async';

import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../services/api_client.dart';
import '../services/app_update_service.dart';
import '../services/background_service.dart';
import '../services/permission_service.dart';
import '../services/session_service.dart';
import '../services/storage_service.dart';
import '../services/widget_refresh.dart';
import '../ui/ui.dart';
import '../widgets/server_power.dart';
import 'login_screen.dart';
import 'system_updates_screen.dart';

/// The app's own settings: how often Home refreshes, this phone's
/// permissions, server power, about, and sign out. The account and the theme live on More (one home each);
/// the app's own update lives on Updates, which About links to.
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key, this.refresh});

  /// The refresh setting to show and change; the phone's by default.
  final WidgetRefreshController? refresh;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> with WidgetsBindingObserver {
  String? _username;
  String _serverUrl = '';
  String _version = '';
  String _build = '';
  PermissionState? _notifications;
  bool? _batteryUnrestricted;
  bool _isSamsung = false;

  UpdateCheck? _update;

  WidgetRefreshController get _refresh => widget.refresh ?? WidgetRefreshController.instance;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _load();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  // Coming back from the system settings: permissions may have changed.
  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) _loadPhone();
  }

  Future<void> _load() async {
    final u = await StorageService.instance.getUsername();
    final s = await StorageService.instance.getServerUrl();
    final v = await AppUpdateService.instance.installedVersion();
    final cached = await AppUpdateService.instance.cached();
    if (!mounted) return;
    setState(() {
      _username = u;
      _serverUrl = s ?? '';
      _version = v.version;
      _build = v.build;
      _update = cached;
    });
    await _loadPhone();
    _checkForUpdate();
  }

  Future<void> _loadPhone() async {
    final notifications = await PermissionService.notificationState();
    final battery = await BackgroundService.instance.isIgnoringBatteryOptimizations();
    final samsung = await BackgroundService.instance.isSamsungDevice();
    if (!mounted) return;
    setState(() {
      _notifications = notifications;
      _batteryUnrestricted = battery;
      _isSamsung = samsung;
    });
  }

  // A quiet check at most once a day (the service throttles it), so About
  // can say when an update is waiting; checking on demand is on Updates.
  Future<void> _checkForUpdate() async {
    try {
      final result = await AppUpdateService.instance.check();
      if (mounted) setState(() => _update = result);
    } on UpdateException {
      // Keep the saved result; Updates shows why a check fails.
    }
  }

  Future<void> _openUpdates() async {
    await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => const SystemUpdatesScreen()));
    final cached = await AppUpdateService.instance.cached();
    if (mounted && cached != null) setState(() => _update = cached);
  }

  Future<void> _notificationsTapped() async {
    final state = _notifications;
    if (state == PermissionState.askable && !await StorageService.instance.getNotificationsAsked()) {
      await PermissionService.requestNotifications();
    } else {
      await PermissionService.openSettings();
    }
    await _loadPhone();
  }

  Future<void> _batteryTapped() async {
    if (_batteryUnrestricted == true) {
      await BackgroundService.instance.openBatteryOptimizationSettings();
    } else {
      await BackgroundService.instance.requestIgnoreBatteryOptimizations();
    }
  }

  Future<void> _signOut() async {
    final host = ApiClient.displayHost(_serverUrl);
    final ok = await ConfirmDialog.confirm(
      context,
      title: 'Sign out of $host?',
      message: 'This phone stops sharing its storage and checking in with the server. Your saved servers stay on the phone.',
      confirmLabel: 'Sign out',
    );
    if (!ok) return;
    await SessionService.signOut();
    if (!mounted) return;
    Navigator.of(context).pushAndRemoveUntil(
      MaterialPageRoute(builder: (_) => LoginScreen(initialUsername: _username)),
      (route) => false,
    );
  }

  Future<void> _open(String url) async {
    try {
      await launchUrl(Uri.parse(url), mode: LaunchMode.externalApplication);
    } catch (_) {}
  }

  Future<void> _pickRefresh() async {
    final picked = await showRefreshSheet(context, _refresh.value);
    if (picked != null) await _refresh.set(picked);
  }

  String _versionLine() => _build.isEmpty ? _version : '$_version (build $_build)';

  @override
  Widget build(BuildContext context) {
    final host = _serverUrl.isEmpty ? null : ApiClient.displayHost(_serverUrl);
    final update = _update;
    final hasUpdate = update?.updateAvailable ?? false;

    return AppScaffold.slivers(
      title: 'Settings',
      slivers: [
        SliverList.list(children: [
          TileGroup(
            title: 'Home',
            footer: 'Faster refreshes use more battery and data. Drive usage refreshes at most every 30 seconds and the VM preview at most every 5 seconds.',
            children: [
              ValueListenableBuilder<WidgetRefresh>(
                valueListenable: _refresh,
                builder: (context, value, _) => ListTile(
                  leading: const Icon(Icons.update_outlined),
                  title: const Text('Refresh widgets'),
                  subtitle: Text(value.summary),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: _pickRefresh,
                ),
              ),
            ],
          ),
          if (BackgroundService.isAndroid)
            TileGroup(
              title: 'This phone',
              children: [
                ListTile(
                  leading: Icon(_notifications == PermissionState.granted ? Icons.notifications_outlined : Icons.notifications_off_outlined),
                  title: const Text('Notifications'),
                  subtitle: Text(switch (_notifications) {
                    null => '—',
                    PermissionState.granted => 'Allowed',
                    _ => 'Off · Needed to show and stop storage sharing',
                  }),
                  onTap: _notifications == null ? null : _notificationsTapped,
                ),
                ListTile(
                  leading: const Icon(Icons.eco_outlined),
                  title: const Text('Battery use'),
                  subtitle: Text(switch (_batteryUnrestricted) {
                    null => '—',
                    true => 'Unrestricted',
                    false => 'Optimized · Check-ins with the server may be late',
                  }),
                  onTap: _batteryUnrestricted == null ? null : _batteryTapped,
                ),
                if (_isSamsung)
                  ListTile(
                    leading: const Icon(Icons.bedtime_outlined),
                    title: const Text('Samsung sleeping apps'),
                    subtitle: const Text('Add NivaroOS to “Never sleeping apps” in its battery settings'),
                    onTap: () => BackgroundService.instance.openSamsungBatterySettings(),
                  ),
              ],
            ),
          TileGroup(title: 'Server power', children: [
            ListTile(
              leading: const Icon(Icons.restart_alt_outlined),
              title: const Text('Restart server'),
              enabled: host != null,
              onTap: () => confirmServerPower(context, restart: true),
            ),
            ListTile(
              leading: const Icon(Icons.power_settings_new_outlined),
              title: const Text('Shut down server'),
              enabled: host != null,
              onTap: () => confirmServerPower(context, restart: false),
            ),
          ]),
          TileGroup(
            title: 'About',
            children: [
              // The app's update lives on Updates, next to the server's;
              // this row only says whether one is waiting and goes there.
              ListTile(
                leading: Icon(hasUpdate ? Icons.system_update_outlined : Icons.info_outline),
                title: Text('NivaroOS for Android ${_versionLine()}'),
                subtitle: Text(hasUpdate ? 'Version ${update!.latest!.version} is available' : 'App updates are on the Updates screen'),
                trailing: hasUpdate
                    ? FilledButton.tonal(style: tonalButtonStyle(context), onPressed: _openUpdates, child: const Text('View'))
                    : const Icon(Icons.chevron_right),
                onTap: _openUpdates,
              ),
              ListTile(
                leading: const Icon(Icons.code_outlined),
                title: const Text('Source code and releases'),
                subtitle: const Text('github.com/F-e-n-y-x/NivaroOS'),
                trailing: const Icon(Icons.open_in_new_outlined),
                onTap: () => _open(AppUpdateService.releasesPage),
              ),
              ListTile(
                leading: const Icon(Icons.description_outlined),
                title: const Text('Open-source licenses'),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => showLicensePage(context: context, applicationName: 'NivaroOS', applicationVersion: _version),
              ),
            ],
          ),
          const SizedBox(height: Space.sm),
          TileGroup(children: [
            ListTile(
              leading: const Icon(Icons.logout_outlined),
              title: const Text('Sign out'),
              subtitle: Text(host == null ? 'Of this server' : 'Of $host'),
              onTap: _signOut,
            ),
          ]),
        ]),
      ],
    );
  }
}

/// Asks how often Home's widgets refresh; null when dismissed.
Future<WidgetRefresh?> showRefreshSheet(BuildContext context, WidgetRefresh current) => showModalBottomSheet<WidgetRefresh>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (context) {
        final theme = Theme.of(context);
        return ListTileTheme.merge(
          contentPadding: const EdgeInsets.symmetric(horizontal: Space.xl),
          child: SafeArea(
            top: false,
            child: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Padding(
                    padding: const EdgeInsets.fromLTRB(Space.xl, 0, Space.xl, Space.sm),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Semantics(header: true, child: Text('Refresh widgets', style: theme.textTheme.titleLarge)),
                        const SizedBox(height: Space.xs),
                        Text(
                          'How often Home and its detail pages ask the server for new readings while you look at them.',
                          style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                        ),
                      ],
                    ),
                  ),
                  RadioGroup<WidgetRefresh>(
                    groupValue: current,
                    onChanged: (r) => Navigator.of(context).pop(r),
                    child: Column(
                      children: [
                        for (final r in WidgetRefresh.values)
                          RadioListTile<WidgetRefresh>(
                            value: r,
                            title: Text(r == WidgetRefresh.defaultValue ? '${r.summary} (default)' : r.summary),
                          ),
                      ],
                    ),
                  ),
                  const SizedBox(height: Space.sm),
                ],
              ),
            ),
          ),
        );
      },
    );
