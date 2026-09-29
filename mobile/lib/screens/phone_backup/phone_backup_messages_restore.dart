import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../backup/backup_strings.dart' show formatNumber;
import '../../phone_backup/pb_client.dart';
import '../../phone_backup/pb_models.dart';
import '../../phone_backup/pb_platform.dart';
import '../../phone_backup/pb_restore.dart';
import '../../phone_backup/pb_service.dart';
import '../../ui/ui.dart';
import '../backup/backup_widgets.dart';
import 'phone_backup_common.dart';

/// Putting messages (or the call log) back into this phone, step by step.
///
/// Android lets only the default SMS app write messages, so for SMS the
/// app asks to be the default for the restore and then sends you to
/// switch back; MMS stay in the file (they can be restored with SMS Backup
/// & Restore from the saved file). The call log needs the call log
/// permission, asked here.
class PhoneMessagesRestoreScreen extends StatefulWidget {
  const PhoneMessagesRestoreScreen({super.key, this.service, required this.snapshot, required this.category, this.permissions});
  final PhoneBackupService? service;
  final String snapshot;

  /// sms | calllog
  final String category;
  final PhonePermissions? permissions;

  @override
  State<PhoneMessagesRestoreScreen> createState() => _PhoneMessagesRestoreScreenState();
}

class _PhoneMessagesRestoreScreenState extends State<PhoneMessagesRestoreScreen> {
  late final PhoneBackupService _service = widget.service ?? PhoneBackupService.instance;
  late final PhonePermissions _perms = widget.permissions ?? PhonePermissions.instance;
  bool get _sms => widget.category == 'sms';

  File? _file;
  int _items = 0;
  bool? _isDefault;
  Map<String, int>? _result;
  String _busy = '';
  String _error = '';

  @override
  void initState() {
    super.initState();
    _checkDefault();
  }

  Future<void> _checkDefault() async {
    if (!_sms) return;
    try {
      final d = await ChannelPhoneSources.isDefaultSmsApp();
      if (mounted) setState(() => _isDefault = d);
    } on MissingPluginException {
      if (mounted) setState(() => _isDefault = false);
    }
  }

  Future<void> _run(String what, Future<void> Function() f) async {
    setState(() {
      _busy = what;
      _error = '';
    });
    try {
      await f();
    } on PhoneBackupError catch (e) {
      if (mounted) setState(() => _error = e.driveMissing ? 'The drive that holds this phone’s backups isn’t connected to the server.' : (e.message.isNotEmpty ? e.message : e.code));
    } on PlatformException catch (e) {
      if (mounted) setState(() => _error = e.message ?? e.code);
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    } finally {
      if (mounted) setState(() => _busy = '');
    }
  }

  Future<void> _download() => _run('download', () async {
        final r = PhoneRestore((await _service.deviceClient())!, await _service.store);
        final f = await r.downloadMerged(widget.category, widget.snapshot);
        final text = await f.readAsString();
        final n = RegExp(_sms ? r'<(sms|mms) ' : r'<call ').allMatches(text).length;
        if (mounted) {
          setState(() {
            _file = f;
            _items = n;
          });
        }
      });

  Future<void> _becomeDefault() => _run('role', () async {
        final ok = await ChannelPhoneSources.requestSmsRole();
        if (mounted) setState(() => _isDefault = ok);
      });

  Future<void> _restore() => _run('restore', () async {
        final f = _file!;
        if (!_sms) {
          const names = [AndroidPermissions.readCallLog, AndroidPermissions.writeCallLog];
          var have = await _perms.check(names);
          if (have.values.any((v) => !v)) {
            if (!mounted) return;
            const info = CategoryInfo(
              PhoneCategory.calllog,
              Icons.call_outlined,
              '',
              permissions: names,
              restricted: true,
              why: 'To put your calls back, NivaroOS needs to read and write the call log. Calls already on the phone are skipped.',
            );
            if (!await askForCategory(context, info, _perms, title: 'Restore the call log?')) return;
            have = await _perms.check(names);
            if (have.values.any((v) => !v)) return;
          }
        }
        final r = _sms ? await ChannelPhoneSources.restoreSms(f.path) : await ChannelPhoneSources.restoreCallLog(f.path);
        if (mounted) setState(() => _result = r);
      });

  Future<void> _saveFile() => _run('save', () async {
        final f = _file!;
        await ChannelPhoneSources.saveToDownloads(tmpPath: f.path, subPath: 'Messages', name: _sms ? 'sms-backup.xml' : 'calls-backup.xml');
        if (mounted) showBackupSnack(context, 'Saved to Download/NivaroOS restore/Messages.');
      });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final done = _result != null;
    Widget step(int n, String title, String text, {Widget? action, bool finished = false, bool enabled = true}) {
      return ListTile(
        enabled: enabled,
        leading: CircleAvatar(
          radius: 14,
          backgroundColor: finished ? StatusColors.toneOf(context, Status.success).container : theme.colorScheme.surfaceContainerHighest,
          child: finished ? Icon(Icons.check, size: 16, color: StatusColors.toneOf(context, Status.success).onContainer) : Text('$n', style: theme.textTheme.labelLarge),
        ),
        title: Text(title),
        subtitle: Text(text),
        trailing: action,
      );
    }

    Widget busyOr(String what, Widget button) => _busy == what ? const SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2)) : button;
    final r = _result;
    return AppScaffold(
      title: _sms ? 'Restore messages' : 'Restore the call log',
      body: ListView(children: [
        PhoneBackupHint(_sms
            ? 'Android lets only the default messaging app write messages. For the restore, NivaroOS becomes the default for a few minutes; afterwards you switch back. Messages already on the phone are skipped. Picture messages (MMS) aren’t written back: save the file and restore them with SMS Backup & Restore if you need them.'
            : 'The calls of this backup are added to the phone’s call log. Calls already there are skipped.'),
        if (_error.isNotEmpty) Notice(status: Status.error, message: _error),
        TileGroup(children: [
          step(1, 'Get them from the server', _file == null ? 'Every ${_sms ? 'message' : 'call'} in this backup, each once' : '${formatNumber(_items)} ${_sms ? 'messages' : 'calls'} ready',
              finished: _file != null, action: _file == null ? busyOr('download', FilledButton.tonal(style: tonalButtonStyle(context), onPressed: _busy.isEmpty ? _download : null, child: const Text('Get'))) : null),
          if (_sms)
            step(2, 'Make NivaroOS the default SMS app', 'Android asks you to confirm', finished: _isDefault == true, enabled: _file != null,
                action: _isDefault == true || _file == null ? null : busyOr('role', FilledButton.tonal(style: tonalButtonStyle(context), onPressed: _busy.isEmpty ? _becomeDefault : null, child: const Text('Allow')))),
          step(_sms ? 3 : 2, _sms ? 'Write the messages' : 'Write the calls',
              done ? '${formatNumber(r!['inserted'] ?? 0)} added, ${formatNumber(r['skipped'] ?? 0)} already there${(r['failed'] ?? 0) > 0 ? ', ${formatNumber(r['failed']!)} failed' : ''}${_sms && (r['mms'] ?? 0) > 0 ? ', ${formatNumber(r['mms']!)} picture messages left in the file' : ''}' : 'Takes a minute for many thousands',
              finished: done,
              enabled: _file != null && (!_sms || _isDefault == true),
              action: done || _file == null || (_sms && _isDefault != true) ? null : busyOr('restore', FilledButton(onPressed: _busy.isEmpty ? _restore : null, child: const Text('Restore')))),
          if (_sms)
            step(4, 'Switch back to your messaging app', 'Choose it as the default SMS app again', enabled: done,
                action: done ? FilledButton.tonal(style: tonalButtonStyle(context), onPressed: () => ChannelPhoneSources.openDefaultApps(), child: const Text('Open')) : null),
        ]),
        if (_file != null)
          TileGroup(children: [
            ListTile(
              leading: const Icon(Icons.save_alt),
              title: const Text('Save the file to Downloads'),
              subtitle: const Text('In the SMS Backup & Restore format, for that app or another phone'),
              trailing: busyOr('save', const Icon(Icons.chevron_right)),
              onTap: _busy.isEmpty ? _saveFile : null,
            ),
          ]),
        const SizedBox(height: Space.xl),
      ]),
    );
  }
}
