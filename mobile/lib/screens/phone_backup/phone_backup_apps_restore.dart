import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../phone_backup/pb_client.dart';
import '../../phone_backup/pb_platform.dart';
import '../../phone_backup/pb_restore.dart';
import '../../phone_backup/pb_service.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import '../backup/backup_widgets.dart';
import 'phone_backup_common.dart';

String _errorText(Object e) => switch (e) {
      PhoneBackupError(driveMissing: true) => 'The drive that holds this phone’s backups isn’t connected to the server.',
      PhoneBackupError(unreachable: true) => 'The server can’t be reached.',
      PhoneBackupError(:final message, :final code) => message.isNotEmpty ? message : code,
      PlatformException(:final message, :final code) => message ?? code,
      _ => '$e',
    };

/// Apps of a backup that aren't on this phone (or are older here), put
/// back from their backed-up APKs through Android's package installer.
/// Android asks the user to confirm each one, after "Install unknown apps"
/// is allowed for NivaroOS (asked here, when it is needed).
class PhoneAppsRestoreScreen extends StatefulWidget {
  const PhoneAppsRestoreScreen({super.key, this.service, required this.snapshot});
  final PhoneBackupService? service;
  final String snapshot;

  @override
  State<PhoneAppsRestoreScreen> createState() => _PhoneAppsRestoreScreenState();
}

class _PhoneAppsRestoreScreenState extends State<PhoneAppsRestoreScreen> with WidgetsBindingObserver {
  late final PhoneBackupService _service = widget.service ?? PhoneBackupService.instance;
  List<ApkBackup>? _apps;
  Object? _error;
  bool _canInstall = true;
  String _busy = '';
  final Set<String> _sent = {};

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

  // Back from the settings page or the installer: whether installing is
  // allowed now, and which apps are on the phone.
  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) _load(quiet: true);
  }

  Future<PhoneRestore> _restore() async => PhoneRestore((await _service.deviceClient())!, await _service.store);

  Future<void> _load({bool quiet = false}) async {
    if (!quiet) setState(() => _error = null);
    try {
      final can = await ChannelPhoneSources.canInstallApps();
      if (mounted) setState(() => _canInstall = can);
      if (quiet && _sent.isEmpty) return;
      final installed = {for (final a in await const ChannelPhoneSources().installedApps()) a.package: a.versionCode};
      final apps = await (await _restore()).reinstallable(widget.snapshot, installed);
      if (mounted) setState(() => _apps = apps);
    } catch (e) {
      if (mounted && !quiet) setState(() => _error = e);
    }
  }

  Future<void> _install(ApkBackup a) async {
    setState(() => _busy = a.package);
    try {
      await (await _restore()).install(widget.snapshot, a);
      if (mounted) setState(() => _sent.add(a.package));
    } catch (e) {
      if (mounted) showBackupSnack(context, 'Couldn’t install ${a.label.isEmpty ? a.package : a.label}: ${_errorText(e)}');
    } finally {
      if (mounted) setState(() => _busy = '');
    }
  }

  @override
  Widget build(BuildContext context) {
    final apps = _apps;
    final Widget body;
    if (apps == null && _error != null) {
      body = ErrorState(title: 'Couldn’t load the apps', message: _errorText(_error!), onRetry: _load);
    } else if (apps == null) {
      body = const LoadingList(rows: 6);
    } else if (apps.isEmpty) {
      body = const EmptyState(icon: Icons.apps_outlined, title: 'Nothing to reinstall', message: 'Every app with install files in this backup is on the phone, in the same or a newer version.');
    } else {
      body = ListView(children: [
        const PhoneBackupHint('Apps of this backup that aren’t on the phone, or are older here. Android asks you to confirm each one. Their data isn’t in the backup: you sign in again.'),
        if (!_canInstall)
          Notice(
            message: 'Android asks you to allow NivaroOS to install apps first.',
            actionLabel: 'Allow',
            onAction: ChannelPhoneSources.openInstallSettings,
          ),
        TileGroup(children: [
          for (final a in apps)
            ListTile(
              leading: const Icon(Icons.android_outlined),
              title: Text(a.label.isEmpty ? a.package : a.label),
              subtitle: Text([
                if (_sent.contains(a.package)) 'Sent to the installer' else if (a.installedVersion == null) 'Not on the phone' else 'Older version on the phone',
                formatSize(a.size),
                if (a.split) '${a.files.length} files',
              ].join(' · ')),
              trailing: _busy == a.package
                  ? const SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2))
                  : FilledButton.tonal(
                      style: tonalButtonStyle(context),
                      onPressed: _busy.isEmpty && _canInstall ? () => _install(a) : null,
                      child: Text(a.installedVersion == null ? 'Install' : 'Update', semanticsLabel: '${a.installedVersion == null ? 'Install' : 'Update'} ${a.label.isEmpty ? a.package : a.label}'),
                    ),
            ),
        ]),
        const SizedBox(height: Space.xl),
      ]);
    }
    return AppScaffold(title: 'Reinstall apps', body: body);
  }
}
