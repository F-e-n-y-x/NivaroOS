import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:intl/intl.dart';

import '../services/api_client.dart';
import '../services/background_service.dart';
import '../services/device_sync_service.dart';
import '../services/permission_service.dart';
import '../services/storage_service.dart';
import '../ui/ui.dart';
import '../utils/format.dart';
import '../widgets/tailscale_modal.dart' show seenPhrase;

/// This phone and the account's other devices on the server.
///
/// "Share this phone's storage" is off by default (plan M-20) and runs as a
/// session with an end time (M-18, WP1-7): while it is on, the server can
/// browse and copy files in the phone's shared storage; it stops by itself.
class CompanionDevicesScreen extends StatefulWidget {
  const CompanionDevicesScreen({super.key});

  @override
  State<CompanionDevicesScreen> createState() => _CompanionDevicesScreenState();
}

class _CompanionDevicesScreenState extends State<CompanionDevicesScreen> with WidgetsBindingObserver {
  List<CompanionDevice>? _devices;
  CompanionDevice? _thisPhone;
  ApiException? _error;
  DateTime? _loadedAt;
  ShareStatus _share = ShareStatus.off;
  bool _shareBusy = false;
  bool? _batteryUnrestricted;
  Timer? _poll;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    DeviceSyncService.instance.registrationProblem.addListener(_onProblem);
    _load();
  }

  @override
  void dispose() {
    _poll?.cancel();
    WidgetsBinding.instance.removeObserver(this);
    DeviceSyncService.instance.registrationProblem.removeListener(_onProblem);
    super.dispose();
  }

  void _onProblem() {
    if (mounted) setState(() {});
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) _loadShare();
  }

  Future<void> _load() async {
    final thisPhone = await DeviceSyncService.instance.getLocalDeviceInfo();
    if (mounted) setState(() => _thisPhone = thisPhone);
    await _loadShare();
    try {
      final list = await DeviceSyncService.instance.fetchCompanionDevices();
      if (!mounted) return;
      setState(() {
        _devices = list;
        _thisPhone = list.firstWhere((d) => d.isCurrentDevice, orElse: () => thisPhone);
        _error = null;
        _loadedAt = clock.now();
      });
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _loadShare() async {
    final status = await BackgroundService.instance.sharingStatus();
    final battery = await BackgroundService.instance.isIgnoringBatteryOptimizations();
    if (!mounted) return;
    setState(() {
      _share = status;
      _batteryUnrestricted = battery;
    });
    _poll?.cancel();
    // While sharing, keep the end time and "stopped" state current.
    if (status.running) _poll = Timer.periodic(const Duration(seconds: 5), (_) => _refreshShareOnly());
  }

  Future<void> _refreshShareOnly() async {
    final status = await BackgroundService.instance.sharingStatus();
    if (!mounted) return;
    if (status != _share) setState(() => _share = status);
    if (!status.running) _poll?.cancel();
  }

  Future<void> _toggleSharing(bool on) async {
    if (_shareBusy) return;
    if (!on) {
      setState(() => _shareBusy = true);
      await BackgroundService.instance.stopSharing();
      await Future<void>.delayed(const Duration(milliseconds: 300));
      await _loadShare();
      if (mounted) setState(() => _shareBusy = false);
      return;
    }
    final length = await _askShareLength();
    if (length == null || !mounted) return;
    setState(() => _shareBusy = true);
    try {
      if (await PermissionService.storageState() != PermissionState.granted) {
        final granted = await PermissionService.requestManageStorage();
        if (!granted) {
          _snack("Sharing needs access to this phone's files. Allow it for NivaroOS in the settings that opened.");
          return;
        }
      }
      // The notification is how sharing is seen and stopped; ask for it
      // now, when it is needed, once.
      await PermissionService.requestNotifications();
      await StorageService.instance.setShareMinutes(length.inMinutes);
      final started = await BackgroundService.instance.startSharing(length, server: ApiClient.displayHost(ApiClient.instance.baseUrl));
      if (!started) _snack("Couldn't start sharing. Try again.");
      await Future<void>.delayed(const Duration(milliseconds: 500));
      await _loadShare();
    } finally {
      if (mounted) setState(() => _shareBusy = false);
    }
  }

  Future<Duration?> _askShareLength() async {
    final saved = await StorageService.instance.getShareMinutes();
    if (!mounted) return null;
    var choice = BackgroundService.shareChoices.firstWhere((d) => d.inMinutes == saved, orElse: () => const Duration(hours: 1));
    return showModalBottomSheet<Duration>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (context) {
        final theme = Theme.of(context);
        return StatefulBuilder(
          builder: (context, setSheet) => SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(Space.xl, 0, Space.xl, Space.xl),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              mainAxisSize: MainAxisSize.min,
              children: [
                Semantics(header: true, child: Text("Share this phone's storage?", style: theme.textTheme.headlineSmall)),
                const SizedBox(height: Space.md),
                Text(
                  'While sharing is on, the server can browse, copy, add and delete files in this phone’s shared storage '
                  '(Downloads, Pictures, Documents and other folders). Apps’ private data stays private.',
                  style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                ),
                const SizedBox(height: Space.xl),
                Text('Stop sharing after', style: theme.textTheme.titleSmall),
                const SizedBox(height: Space.sm),
                SegmentedButton<Duration>(
                  segments: [
                    for (final d in BackgroundService.shareChoices) ButtonSegment(value: d, label: Text(_lengthLabel(d))),
                  ],
                  selected: {choice},
                  onSelectionChanged: (s) => setSheet(() => choice = s.first),
                ),
                const SizedBox(height: Space.md),
                Text(
                  'You can stop it earlier from here or from its notification.',
                  style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                ),
                const SizedBox(height: Space.xl),
                FilledButton(
                  style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
                  onPressed: () => Navigator.of(context).pop(choice),
                  child: const Text('Start sharing'),
                ),
                const SizedBox(height: Space.sm),
                TextButton(
                  style: TextButton.styleFrom(minimumSize: const Size.fromHeight(48)),
                  onPressed: () => Navigator.of(context).pop(),
                  child: const Text('Cancel'),
                ),
              ],
            ),
          ),
        );
      },
    );
  }

  static String _lengthLabel(Duration d) => d.inMinutes < 60 ? '${d.inMinutes} min' : (d.inHours == 1 ? '1 hour' : '${d.inHours} hours');

  void _snack(String text) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(text)));
  }

  Future<void> _rename(CompanionDevice dev) async {
    final controller = TextEditingController(text: dev.name);
    final newName = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(dev.isCurrentDevice ? 'Rename this phone' : 'Rename “${dev.name}”'),
        content: TextField(
          controller: controller,
          autofocus: true,
          textCapitalization: TextCapitalization.sentences,
          decoration: const InputDecoration(labelText: 'Name', helperText: 'Also the name of its folder on the server'),
          onSubmitted: (v) => Navigator.pop(ctx, v.trim()),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('Cancel')),
          TextButton(onPressed: () => Navigator.pop(ctx, controller.text.trim()), child: const Text('Rename')),
        ],
      ),
    );
    controller.dispose();
    if (newName == null || newName.isEmpty || newName == dev.name) return;
    try {
      await DeviceSyncService.instance.updateRemoteDeviceName(dev.id, newName);
      await _load();
      _snack('Renamed to “$newName”');
    } on ApiException catch (e) {
      _snack("Couldn't rename it. ${e.message}");
    }
  }

  Future<void> _remove(CompanionDevice dev) async {
    final ok = await ConfirmDialog.destructive(
      context,
      title: 'Remove “${dev.name}”?',
      message: 'It disappears from the server until its app signs in again. Files it backed up to the server stay in its folder.',
      confirmLabel: 'Remove',
      permanent: false,
    );
    if (!ok) return;
    try {
      await DeviceSyncService.instance.deleteCompanionDevice(dev.id);
      if (mounted) Navigator.of(context).maybePop();
      await _load();
      _snack('Removed “${dev.name}”');
    } on ApiException catch (e) {
      _snack("Couldn't remove it. ${e.message}");
    }
  }

  void _showDetails(CompanionDevice dev) {
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (context) => _DeviceSheet(
        device: dev,
        onRename: () {
          Navigator.pop(context);
          _rename(dev);
        },
        onRemove: () => _remove(dev),
      ),
    );
  }

  static IconData deviceIcon(CompanionDevice dev) {
    final m = '${dev.model} ${dev.name}'.toLowerCase();
    final tablet = m.contains('tablet') || m.contains('pad') || RegExp(r'\btab\b').hasMatch(m) || RegExp(r'^sm-[xtp]').hasMatch(dev.model.toLowerCase());
    return tablet ? Icons.tablet_android_outlined : Icons.phone_android_outlined;
  }

  static String statusLine(CompanionDevice dev) {
    final parts = <String>[
      switch (dev.connection) {
        CompanionConnection.lan => 'Online',
        CompanionConnection.remote => 'Online, away from home',
        CompanionConnection.offline => dev.lastSeen == null ? 'Offline' : 'Offline · seen ${seenPhrase(dev.lastSeen!)}',
        CompanionConnection.unknown => dev.isOnline ? 'Online' : 'Offline',
      },
      if (dev.battery != null && dev.connection != CompanionConnection.offline) '${dev.battery}% battery',
    ];
    return parts.join(' · ');
  }

  String? _stopNote() {
    if (_share.running) return null;
    return switch (_share.lastStopReason) {
      'ended' => null,
      'timeout' => "Sharing stopped: Android allows this kind of background work for 6 hours a day. You can share again later today or tomorrow.",
      'not_allowed' => "Android didn't let sharing start. Open the app and try again.",
      'signed_out' => 'Sharing stopped because the sign-in on this server ended.',
      'error' => "Sharing stopped because of a problem on this phone. Try again.",
      _ => null,
    };
  }

  @override
  Widget build(BuildContext context) {
    final devices = _devices;
    final others = devices?.where((d) => !d.isCurrentDevice).toList();
    final showError = _error != null && devices == null;

    return AppScaffold.slivers(
      title: 'Companion devices',
      onRefresh: _load,
      banner: _error != null && devices != null && _error!.isUnreachable ? OfflineBanner(lastUpdated: _loadedAt, onRetry: _load) : null,
      slivers: [
        SliverToBoxAdapter(child: _thisPhoneGroup(context)),
        if (showError) ...[
          // The group's header stays, so it is clear which part failed.
          const SliverToBoxAdapter(child: TileGroup(title: 'Other devices', children: [])),
          _error!.isUnreachable
              ? ErrorState.offline(sliver: true, onRetry: _load, details: _error!.details)
              : ErrorState(
                  sliver: true,
                  title: "Couldn't load your devices",
                  // The server's own text is for a bug report, not for the
                  // screen ("companion registry is unavailable").
                  message: "The server's device list isn't responding. Try again in a moment.",
                  onRetry: _load,
                  details: _error!.details ?? _error!.message,
                ),
        ] else if (others == null)
          SliverToBoxAdapter(
            child: TileGroup(title: 'Other devices', children: SkeletonRow.group(rows: 2)),
          )
        else
          SliverToBoxAdapter(
            child: TileGroup(
              title: 'Other devices',
              children: others.isEmpty
                  ? [
                      const ListTile(
                        leading: Icon(Icons.devices_other_outlined),
                        title: Text('No other devices yet'),
                        subtitle: Text('Phones and tablets you sign in to this server with show up here.'),
                      ),
                    ]
                  : [
                      for (final d in others)
                        ListTile(
                          leading: Icon(deviceIcon(d)),
                          title: Text(d.name, maxLines: 1, overflow: TextOverflow.ellipsis),
                          subtitle: Text(statusLine(d)),
                          trailing: const Icon(Icons.chevron_right),
                          onTap: () => _showDetails(d),
                        ),
                    ],
            ),
          ),
      ],
    );
  }

  Widget _thisPhoneGroup(BuildContext context) {
    final phone = _thisPhone;
    final problem = DeviceSyncService.instance.registrationProblem.value;
    final android = BackgroundService.isAndroid;
    final running = _share.running;
    final endsAt = _share.endsAt;
    final until = endsAt == null ? null : DateFormat.jm().format(endsAt);

    final subtitleParts = [
      if (phone != null && phone.model.isNotEmpty && phone.model != phone.name) phone.model,
      if (phone != null && phone.osVersion.isNotEmpty) phone.osVersion.replaceAll(RegExp(r' \(SDK \d+\)'), ''),
    ];

    final stopNote = _stopNote();
    final group = TileGroup(
      title: 'This phone',
      children: [
        ListTile(
          leading: Icon(phone == null ? Icons.phone_android_outlined : deviceIcon(phone)),
          title: Text(phone?.name ?? 'This phone', maxLines: 1, overflow: TextOverflow.ellipsis),
          subtitle: Text(subtitleParts.isEmpty ? '—' : subtitleParts.join(' · ')),
          trailing: IconButton(
            tooltip: 'Rename this phone',
            icon: const Icon(Icons.edit_outlined),
            onPressed: phone == null ? null : () => _rename(phone),
          ),
        ),
        if (problem != null)
          ListTile(
            leading: Icon(Icons.warning_amber_outlined, color: StatusColors.of(context).warning.color),
            title: Text(problem.otherAccount ? 'Paired with another account' : 'Not linked to your account yet'),
            subtitle: Text(problem.message),
          ),
        if (android)
          SwitchListTile(
            secondary: Icon(running ? Icons.folder_shared_outlined : Icons.folder_off_outlined),
            title: const Text('Share storage with the server'),
            subtitle: Text(running ? 'On until $until' : 'Off · Lets the server browse this phone’s files'),
            value: running,
            onChanged: _shareBusy ? null : _toggleSharing,
          ),
        if (phone != null && phone.storageTotal != null)
          // Icon and text on the same edges as the rows above it.
          Padding(
            padding: const EdgeInsetsDirectional.fromSTEB(Space.lg, Space.md, Space.xl, Space.lg),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const ExcludeSemantics(child: Icon(Icons.sd_storage_outlined)),
                const SizedBox(width: Space.lg),
                Expanded(
                  child: UsageBar(
                    value: (phone.storageUsed ?? 0).toDouble(),
                    max: phone.storageTotal!.toDouble(),
                    label: 'Storage',
                    detail: '${formatSize(phone.storageUsed ?? 0)} of ${formatSize(phone.storageTotal!)} used',
                  ),
                ),
              ],
            ),
          ),
        if (android && _batteryUnrestricted == false)
          ListTile(
            leading: const Icon(Icons.battery_alert_outlined),
            title: const Text('Battery optimization is on'),
            subtitle: const Text('Android may delay this phone’s check-ins with the server'),
            trailing: TextButton(
              onPressed: () async {
                await BackgroundService.instance.requestIgnoreBatteryOptimizations();
              },
              child: const Text('Allow'),
            ),
          ),
      ],
    );
    if (stopNote == null) return group;
    // Why sharing stopped is worth noticing, and has a next step.
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        group,
        const SizedBox(height: Space.sm),
        Notice(
          message: stopNote,
          icon: Icons.folder_off_outlined,
          actionLabel: android && _share.lastStopReason != 'signed_out' ? 'Share again' : null,
          onAction: _shareBusy ? null : () => _toggleSharing(true),
        ),
      ],
    );
  }
}

class _DeviceSheet extends StatelessWidget {
  const _DeviceSheet({required this.device, required this.onRename, required this.onRemove});

  final CompanionDevice device;
  final VoidCallback onRename;
  final VoidCallback onRemove;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final d = device;
    final rows = <(IconData, String, String)>[
      if (d.model.isNotEmpty) (Icons.smartphone_outlined, 'Model', d.model),
      if (d.osVersion.isNotEmpty) (Icons.android_outlined, 'System', d.osVersion.replaceAll(RegExp(r' \(SDK \d+\)'), '')),
      if (d.appVersion.isNotEmpty) (Icons.apps_outlined, 'App version', d.appVersion.replaceFirst(RegExp(r'^v'), '')),
      (Icons.eco_outlined, 'Battery', d.battery == null ? '—' : '${d.battery}%'),
      (Icons.backup_outlined, 'Kept on the server', d.serverStorageUsed > 0 ? formatSize(d.serverStorageUsed) : 'Nothing yet'),
    ];
    return SingleChildScrollView(
      padding: const EdgeInsets.only(bottom: Space.xl),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(Space.xl, 0, Space.xl, Space.xs),
            child: Semantics(header: true, child: Text(d.name, style: theme.textTheme.headlineSmall)),
          ),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: Space.xl),
            child: Text(_CompanionDevicesScreenState.statusLine(d),
                style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
          ),
          const SizedBox(height: Space.lg),
          if (d.storageTotal != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(Space.xl, 0, Space.xl, Space.md),
              child: UsageBar(
                value: (d.storageUsed ?? 0).toDouble(),
                max: d.storageTotal!.toDouble(),
                label: 'Storage',
                detail: '${formatSize(d.storageUsed ?? 0)} of ${formatSize(d.storageTotal!)} used',
              ),
            ),
          for (final (icon, label, value) in rows)
            ListTile(
              contentPadding: const EdgeInsets.symmetric(horizontal: Space.xl),
              leading: Icon(icon),
              title: Text(label),
              trailing: Text(value, style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
            ),
          if (d.storagePath.isNotEmpty)
            ListTile(
              contentPadding: const EdgeInsets.symmetric(horizontal: Space.xl),
              leading: const Icon(Icons.folder_outlined),
              title: const Text('Folder on the server'),
              subtitle: Text(d.storagePath),
              onLongPress: () => Clipboard.setData(ClipboardData(text: d.storagePath)),
            ),
          const Divider(height: Space.xl),
          ListTile(
            contentPadding: const EdgeInsets.symmetric(horizontal: Space.xl),
            leading: const Icon(Icons.edit_outlined),
            title: const Text('Rename'),
            onTap: onRename,
          ),
          ListTile(
            contentPadding: const EdgeInsets.symmetric(horizontal: Space.xl),
            leading: Icon(Icons.delete_outline, color: theme.colorScheme.error),
            title: Text('Remove from the server', style: TextStyle(color: theme.colorScheme.error)),
            onTap: onRemove,
          ),
        ],
      ),
    );
  }
}
