import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../backup/backup_api.dart';
import '../../phone_backup/pb_models.dart';
import '../../phone_backup/pb_platform.dart';
import '../../phone_backup/pb_schedule.dart';
import '../../phone_backup/pb_service.dart';
import '../../phone_backup/pb_store.dart';
import '../../ui/ui.dart';
import '../backup/backup_widgets.dart';
import 'phone_backup_common.dart';
import 'phone_backup_location.dart';

/// "Back up this phone": what to back up, when, and where (plan WP2-4).
/// First time: links the phone to the server with the owner's session
/// (POST /devices; the token goes to secure storage), then saves the
/// choices and schedules the job. Later: the same screen edits them.
class PhoneBackupSetupScreen extends StatefulWidget {
  const PhoneBackupSetupScreen({super.key, this.service, this.permissions, this.deviceName, this.pickFolder});

  final PhoneBackupService? service;
  final PhonePermissions? permissions;

  /// The name the phone gets on the server (the model, by default).
  final String? deviceName;

  /// The system folder picker (a seam for tests).
  final Future<SafFolder?> Function()? pickFolder;

  @override
  State<PhoneBackupSetupScreen> createState() => _PhoneBackupSetupScreenState();
}

class _PhoneBackupSetupScreenState extends State<PhoneBackupSetupScreen> {
  late final PhoneBackupService _service = widget.service ?? PhoneBackupService.instance;
  late final PhonePermissions _perms = widget.permissions ?? PhonePermissions.instance;

  PhoneBackupSettings _s = const PhoneBackupSettings();
  PhoneBackupStatus? _status;
  PhoneConfig? _config;
  PhoneDeviceDetail? _detail;
  Object? _configError;
  bool _saving = false;
  bool _loaded = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final st = await _service.status();
    if (!mounted) return;
    setState(() {
      _status = st;
      _s = st.settings ?? const PhoneBackupSettings();
      _loaded = true;
    });
    final c = st.credential;
    if (c == null) return;
    final client = await _service.deviceClient();
    try {
      final cfg = await client!.config();
      if (mounted) setState(() => _config = cfg);
    } catch (e) {
      if (mounted) setState(() => _configError = e);
    }
    try {
      final d = await _service.owner.detail(c.deviceId);
      if (mounted) setState(() => _detail = d);
    } catch (_) {}
  }

  Future<void> _toggle(PhoneCategory c, bool on) async {
    if (on) {
      if (c == PhoneCategory.files && _s.folders.isEmpty) {
        final added = await _addFolder();
        if (!added) return;
      }
      if (!mounted) return;
      final ok = await askForCategory(context, categoryInfo[c]!, _perms);
      if (!ok || !mounted) return;
    }
    setState(() => _s = _s.copyWith(categories: on ? {..._s.categories, c} : ({..._s.categories}..remove(c))));
  }

  Future<bool> _addFolder() async {
    SafFolder? f;
    try {
      f = await (widget.pickFolder ?? _systemPicker)();
    } on PlatformException catch (e) {
      if (mounted) showBackupSnack(context, 'The folder picker didn’t open: ${e.message}');
    }
    if (f == null || !mounted) return false;
    if (_s.folders.contains(f)) return true;
    setState(() => _s = _s.copyWith(folders: [..._s.folders, f!], categories: {..._s.categories, PhoneCategory.files}));
    return true;
  }

  static Future<SafFolder?> _systemPicker() async {
    try {
      final r = await ChannelPhoneSources.pickFolder();
      return r == null ? null : SafFolder(uri: r.uri, label: r.label);
    } on MissingPluginException {
      return null;
    }
  }

  void _removeFolder(SafFolder f) {
    final left = [..._s.folders]..remove(f);
    setState(() => _s = _s.copyWith(folders: left, categories: left.isEmpty ? ({..._s.categories}..remove(PhoneCategory.files)) : null));
    unawaited(ChannelPhoneSources.releaseFolder(f.uri).catchError((_) {}));
  }

  Future<void> _pickSchedule() async {
    final picked = await showModalBottomSheet<ScheduleKind>(
      context: context,
      showDragHandle: true,
      builder: (context) => SafeArea(
        child: RadioGroup<ScheduleKind>(
          groupValue: _s.schedule.kind,
          onChanged: (k) => Navigator.of(context).pop(k),
          child: Column(mainAxisSize: MainAxisSize.min, children: const [
            RadioListTile(value: ScheduleKind.daily, title: Text('Every day')),
            RadioListTile(value: ScheduleKind.weekly, title: Text('Every week')),
            RadioListTile(value: ScheduleKind.manual, title: Text('Only when I tap Back up now')),
          ]),
        ),
      ),
    );
    if (picked == null || !mounted) return;
    final cur = _s.schedule;
    setState(() => _s = _s.copyWith(schedule: BackupSchedule(kind: picked, hour: cur.hour, minute: cur.minute, weekday: cur.weekday)));
  }

  Future<void> _pickTime() async {
    final cur = _s.schedule;
    final t = await showTimePicker(context: context, initialTime: TimeOfDay(hour: cur.hour, minute: cur.minute));
    if (t == null || !mounted) return;
    setState(() => _s = _s.copyWith(schedule: BackupSchedule(kind: cur.kind, hour: t.hour, minute: t.minute, weekday: cur.weekday)));
  }

  Future<void> _pickDay() async {
    final cur = _s.schedule;
    final d = await showModalBottomSheet<int>(
      context: context,
      showDragHandle: true,
      builder: (context) => SafeArea(
        child: SingleChildScrollView(
          child: RadioGroup<int>(
            groupValue: cur.weekday,
            onChanged: (v) => Navigator.of(context).pop(v),
            child: Column(mainAxisSize: MainAxisSize.min, children: [
              for (var i = 1; i <= 7; i++) RadioListTile<int>(value: i, title: Text(BackupSchedule.dayName(i))),
            ]),
          ),
        ),
      ),
    );
    if (d == null || !mounted) return;
    setState(() => _s = _s.copyWith(schedule: BackupSchedule(kind: cur.kind, hour: cur.hour, minute: cur.minute, weekday: d)));
  }

  Future<void> _changeLocation() async {
    final c = _status?.credential;
    final d = _detail;
    if (c == null || d == null) return;
    final next = await changePhoneBackupLocation(context, deviceId: c.deviceId, detail: d, owner: _service.owner);
    if (next != null && mounted) {
      setState(() => _detail = next);
      unawaited(_load());
    }
  }

  Future<void> _save() async {
    if (_s.runCategories.isEmpty) {
      showBackupSnack(context, 'Choose at least one thing to back up.');
      return;
    }
    setState(() => _saving = true);
    try {
      if (_status?.credential == null) {
        var name = widget.deviceName ?? '';
        if (name.isEmpty) {
          try {
            name = (await ChannelPhoneSources.deviceInfo())['model'] ?? '';
          } catch (_) {}
        }
        await _service.enroll(name.trim().isEmpty ? 'Android phone' : name.trim());
      }
      // The progress notification.
      await _perms.request([AndroidPermissions.postNotifications]);
      await _service.saveSettings(_s);
      if (mounted) Navigator.of(context).pop(true);
    } catch (e) {
      if (mounted) showBackupError(context, e, prefix: 'Couldn’t set up the backup');
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final enrolled = _status?.credential != null;
    final s = _s;
    final dest = _detail?.destination ?? _config?.destination;
    final retention = _detail?.retention ?? _config?.retention;
    final sched = s.schedule;
    final Widget body;
    if (!_loaded) {
      body = const LoadingList(rows: 8);
    } else {
      body = ListView(
        padding: EdgeInsets.only(bottom: Space.xl + MediaQuery.paddingOf(context).bottom),
        children: [
          if (!enrolled)
            const PhoneBackupHint('Copies of this phone’s photos, messages and more go to your NivaroOS server, on a schedule and in the background. Only what’s new or changed is sent each time.'),
          TileGroup(title: 'What to back up', children: [
            for (final c in PhoneCategory.values) ...[
              SwitchListTile(
                secondary: Icon(categoryInfo[c]!.icon),
                title: Text(c.label),
                subtitle: Text(categoryInfo[c]!.description),
                value: s.categories.contains(c) && (c != PhoneCategory.files || s.folders.isNotEmpty),
                onChanged: (v) => _toggle(c, v),
              ),
              if (c == PhoneCategory.files) ...[
                for (final f in s.folders)
                  ListTile(
                    contentPadding: EdgeInsetsDirectional.only(start: Space.gutter(context) + 40, end: Space.sm),
                    leading: const Icon(Icons.subdirectory_arrow_right),
                    title: Text(f.label),
                    trailing: IconButton(tooltip: 'Remove ${f.label}', icon: const Icon(Icons.close), onPressed: () => _removeFolder(f)),
                  ),
                ListTile(
                  contentPadding: EdgeInsetsDirectional.only(start: Space.gutter(context) + 40, end: Space.lg),
                  leading: const Icon(Icons.create_new_folder_outlined),
                  title: const Text('Add folder…'),
                  onTap: _addFolder,
                ),
              ],
            ],
          ]),
          TileGroup(
            title: 'When',
            footer: sched.kind == ScheduleKind.manual
                ? null
                : 'Runs in the background when the phone allows it. A backup that can’t finish in one go picks up where it stopped.',
            children: [
              ListTile(
                leading: const Icon(Icons.schedule_outlined),
                title: const Text('Schedule'),
                subtitle: Text(sched.label),
                trailing: const Icon(Icons.chevron_right),
                onTap: _pickSchedule,
              ),
              if (sched.kind == ScheduleKind.weekly)
                ListTile(leading: const Icon(Icons.calendar_today_outlined), title: const Text('Day'), subtitle: Text(BackupSchedule.dayName(sched.weekday)), onTap: _pickDay),
              if (sched.kind != ScheduleKind.manual)
                ListTile(
                  leading: const Icon(Icons.access_time),
                  title: const Text('Time'),
                  subtitle: Text(MaterialLocalizations.of(context).formatTimeOfDay(TimeOfDay(hour: sched.hour, minute: sched.minute))),
                  onTap: _pickTime,
                ),
              SwitchListTile(
                secondary: const Icon(Icons.wifi),
                title: const Text('Wi-Fi only'),
                subtitle: const Text('Don’t use mobile data'),
                value: s.conditions.wifiOnly,
                onChanged: (v) => setState(() => _s = _s.copyWith(conditions: RunConditions(wifiOnly: v, chargingOnly: s.conditions.chargingOnly))),
              ),
              SwitchListTile(
                secondary: const Icon(Icons.battery_charging_full_outlined),
                title: const Text('Only while charging'),
                value: s.conditions.chargingOnly,
                onChanged: (v) => setState(() => _s = _s.copyWith(conditions: RunConditions(wifiOnly: s.conditions.wifiOnly, chargingOnly: v))),
              ),
            ],
          ),
          if (dest != null)
            PhoneDestinationGroup(
              destination: dest,
              move: _detail?.move,
              onChange: _detail == null ? null : _changeLocation,
              footer: _detail == null ? 'Sign in as an administrator to change where this phone’s backups go.' : null,
            )
          else
            TileGroup(title: 'Where backups go', footer: enrolled && _configError != null ? BackupError.from(_configError!).title : null, children: [
              ListTile(
                leading: const Icon(Icons.dns_outlined),
                title: const Text('On your server, in /DATA/Backup'),
                subtitle: Text(enrolled ? 'Loading…' : 'In a folder named after this phone. You can choose another drive or folder after setup.'),
              ),
            ]),
          if (retention != null)
            TileGroup(title: 'What’s kept', footer: 'Set by the server’s owner, in Backup & Sync > Phones on the web.', children: [
              ListTile(leading: const Icon(Icons.history), title: Text(retention.summary)),
            ]),
          Padding(
            padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.lg, Space.gutter(context), 0),
            child: FilledButton(
              onPressed: _saving ? null : _save,
              child: Text(_saving ? 'Saving…' : (enrolled ? 'Save' : 'Start backing up')),
            ),
          ),
        ],
      );
    }
    return AppScaffold(title: enrolled ? 'Backup settings' : 'Back up this phone', body: body);
  }
}
