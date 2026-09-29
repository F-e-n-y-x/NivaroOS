import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../backup/backup_api.dart';
import '../../backup/backup_models.dart';
import '../../backup/backup_state.dart' show cronText;
import '../../backup/backup_strings.dart';
import '../../services/api_client.dart';
import '../../ui/ui.dart';
import 'backup_folder_picker.dart';
import 'backup_job_screen.dart';
import 'backup_widgets.dart';

/// How often a job runs, as the form offers it.
enum BackupSchedule { manual, daily, weekly, custom }

/// A job's schedule read back into the form: the first schedule trigger
/// as daily ("M H * * *"), weekly ("M H * * D") or custom.
({BackupSchedule kind, int hour, int minute, int weekday, String cron}) parseSchedule(List<BackupTrigger> triggers) {
  final t = triggers.where((t) => t.kind == 'schedule' && t.cron.isNotEmpty).firstOrNull;
  if (t == null) return (kind: BackupSchedule.manual, hour: 3, minute: 0, weekday: 0, cron: '0 3 * * *');
  final f = t.cron.trim().split(RegExp(r'\s+'));
  if (f.length == 5 && f[2] == '*' && f[3] == '*') {
    final m = int.tryParse(f[0]), h = int.tryParse(f[1]);
    if (m != null && h != null && m >= 0 && m < 60 && h >= 0 && h < 24) {
      if (f[4] == '*') return (kind: BackupSchedule.daily, hour: h, minute: m, weekday: 0, cron: t.cron);
      final d = int.tryParse(f[4]);
      if (d != null && d >= 0 && d <= 7) return (kind: BackupSchedule.weekly, hour: h, minute: m, weekday: d % 7, cron: t.cron);
    }
  }
  return (kind: BackupSchedule.custom, hour: 3, minute: 0, weekday: 0, cron: t.cron);
}

/// Create or edit a job (the web's job wizard, its core steps on one
/// page): name, what kind of job, what to back up, where to save it (a
/// drive, a USB stick, a network share or a cloud account, and the
/// folder there), when it runs and what it keeps. Everything else - what
/// to skip, time windows, stopping apps, safety limits, retries,
/// notifications - keeps the job's current or default values and is set
/// in the web interface. An edit sends back every field it doesn't show
/// unchanged, with the revision it was read at.
class BackupJobFormScreen extends StatefulWidget {
  const BackupJobFormScreen({super.key, this.job, this.api});

  /// The job to edit; null creates one.
  final BackupJob? job;
  final BackupApi? api;

  @override
  State<BackupJobFormScreen> createState() => BackupJobFormScreenState();
}

@visibleForTesting
class BackupJobFormScreenState extends State<BackupJobFormScreen> {
  late final BackupApi _api = widget.api ?? BackupApi();
  late final _name = TextEditingController(text: widget.job?.name ?? '');
  late final _folder = TextEditingController(text: widget.job?.dest.subPath ?? '');
  late final _cron = TextEditingController();
  late final _keep = TextEditingController();

  late String _type = widget.job?.type ?? 'mirror';
  late List<BackupEndpoint> _sources = [...?widget.job?.sources];
  late BackupEndpoint? _dest = widget.job?.dest;
  bool _folderTouched = false;
  late BackupSchedule _schedule;
  int _hour = 3, _minute = 0, _weekday = 0;
  late bool _plug = widget.job?.triggers.any((t) => t.kind == 'volume_mounted') ?? false;
  bool _runNow = true;
  bool _saving = false;
  BackupError? _error;

  bool get _isEdit => widget.job != null;

  @override
  void initState() {
    super.initState();
    final s = parseSchedule(widget.job?.triggers ?? const []);
    _schedule = widget.job == null ? BackupSchedule.daily : s.kind;
    _hour = s.hour;
    _minute = s.minute;
    _weekday = s.weekday;
    _cron.text = s.cron;
    final j = widget.job;
    _keep.text = j == null ? '30' : (j.type == 'archive' ? '${j.keepLast > 0 ? j.keepLast : 8}' : '${j.versionsDays}');
    _folderTouched = _isEdit;
  }

  @override
  void dispose() {
    _name.dispose();
    _folder.dispose();
    _cron.dispose();
    _keep.dispose();
    super.dispose();
  }

  /// "photos → Sandisk 128G", the web's default name.
  String get _defaultName {
    final src = _sources.isEmpty ? '' : _sources.first.shortName;
    final extra = _sources.length > 1 ? ' +${_sources.length - 1}' : '';
    final dst = _dest == null ? '' : (_dest!.label.isNotEmpty ? _dest!.label : _dest!.refId);
    if (src.isEmpty && dst.isEmpty) return '';
    if (dst.isEmpty) return '$src$extra';
    if (src.isEmpty) return dst;
    return '$src$extra → $dst';
  }

  String get _effectiveName => _name.text.trim().isNotEmpty ? _name.text.trim() : _defaultName;

  /// `NivaroOS Backups/<name>`, the job's own folder at the destination.
  String _defaultFolder() {
    var n = (_name.text.trim().isNotEmpty ? _name.text.trim() : (_sources.isEmpty ? '' : _sources.first.shortName))
        .replaceAll(RegExp(r'[/\\\x00]+'), '-')
        .replaceAll(RegExp(r'\s+'), ' ')
        .trim();
    if (n.length > 120) n = n.substring(0, 120);
    return 'NivaroOS Backups/${n.isEmpty || n == '.' || n == '..' ? 'Backup' : n}';
  }

  void _syncFolder() {
    if (!_folderTouched && _dest != null) _folder.text = _defaultFolder();
  }

  bool get _plugPossible {
    bool drive(BackupEndpoint? e) => e != null && (e.kind == 'usb' || e.kind == 'volume');
    return drive(_dest) || _sources.any(drive);
  }

  String get _cronSpec => switch (_schedule) {
        BackupSchedule.daily => '$_minute $_hour * * *',
        BackupSchedule.weekly => '$_minute $_hour * * $_weekday',
        BackupSchedule.custom => _cron.text.trim(),
        BackupSchedule.manual => '',
      };

  Future<void> _pickSource([int? index]) async {
    final picked = await pickBackupFolder(context, role: 'source', api: widget.api);
    if (picked == null || !mounted) return;
    setState(() {
      if (index == null || index >= _sources.length) {
        _sources = [..._sources, picked];
      } else {
        _sources = [..._sources]..[index] = picked;
      }
      _error = null;
      _syncFolder();
    });
  }

  Future<void> _pickDest() async {
    final picked = await pickBackupFolder(context, role: 'dest', api: widget.api);
    if (picked == null || !mounted) return;
    setState(() {
      _dest = picked.withSubPath('');
      if (picked.subPath.isNotEmpty) {
        _folder.text = picked.subPath;
        _folderTouched = true;
      } else {
        _folderTouched = false;
        _syncFolder();
      }
      _error = null;
    });
  }

  Future<void> _pickTime() async {
    final t = await showTimePicker(context: context, initialTime: TimeOfDay(hour: _hour, minute: _minute));
    if (t != null && mounted) {
      setState(() {
        _hour = t.hour;
        _minute = t.minute;
      });
    }
  }

  /// Problems the form can see before asking the server (the web's
  /// validateDraft, the part this form covers); field -> sentence.
  @visibleForTesting
  Map<String, String> validate() {
    final e = <String, String>{};
    if (_sources.isEmpty) e['sources'] = BackupError.fieldText('required');
    if (_type != 'archive' && _sources.length > 1) e['sources'] = bt('backup.wizard.what.many_needs_archive');
    if (_dest == null) e['dest'] = BackupError.fieldText('required');
    if (_effectiveName.isEmpty) e['name'] = BackupError.fieldText('required');
    if (_effectiveName.length > 200) e['name'] = BackupError.fieldText('out_of_range');
    if (_schedule == BackupSchedule.custom && _cron.text.trim().split(RegExp(r'\s+')).length != 5 && !_cron.text.trim().startsWith('@')) {
      e['cron'] = BackupError.fieldText('invalid_cron');
    }
    final keep = int.tryParse(_keep.text.trim());
    if (_type == 'mirror' && (keep == null || keep < 0 || keep > 3650)) e['retention'] = BackupError.fieldText('out_of_range');
    if (_type == 'archive' && (keep == null || keep < 1 || keep > 1000)) e['retention'] = BackupError.fieldText('out_of_range');
    return e;
  }

  Map<String, String> _errors = const {};

  /// The Job JSON for POST /jobs or PUT /jobs/:id.
  @visibleForTesting
  Map<String, Object?> buildJob() {
    final base = Map<String, Object?>.from(widget.job?.raw ?? const {});
    // What the server owns or computes; it ignores them, so leave them out.
    for (final k in const ['health', 'last_run', 'active_run', 'next_run', 'dest_online', 'stats', 'created_at', 'updated_at']) {
      base.remove(k);
    }
    final oldTriggers = [for (final t in (base['triggers'] as List? ?? const [])) if (t is Map) Map<String, Object?>.from(t)];
    final catchUp = oldTriggers.isEmpty ? true : oldTriggers.any((t) => t['catch_up'] == true);
    final triggers = <Map<String, Object?>>[
      if (_schedule != BackupSchedule.manual) {'kind': 'schedule', 'cron': _cronSpec, 'catch_up': catchUp},
      if (_plug && _plugPossible)
        oldTriggers.where((t) => t['kind'] == 'volume_mounted').firstOrNull ?? {'kind': 'volume_mounted', 'min_gap_hours': 24, 'catch_up': catchUp},
      // Anything else the job had (a second schedule) stays.
      ...oldTriggers.where((t) => t['kind'] != 'schedule' && t['kind'] != 'volume_mounted'),
      ...oldTriggers.where((t) => t['kind'] == 'schedule').skip(1),
    ];
    final keep = int.tryParse(_keep.text.trim()) ?? 0;
    final conditions = Map<String, Object?>.from(base['conditions'] as Map? ?? const {});
    conditions['dest_available'] = _type == 'mirror' ? true : (conditions['dest_available'] ?? true);
    conditions['when_unmet'] ??= _schedule != BackupSchedule.manual ? 'wait' : 'skip';
    if (conditions['when_unmet'] == 'wait') conditions['wait_max_min'] ??= 360;
    final options = Map<String, Object?>.from(base['options'] as Map? ?? const {'verify': false, 'low_priority': true, 'max_duration_sec': 86400, 'copy_empty_dirs': false});
    options['preview_first'] = _type == 'archive' ? false : (_isEdit ? (options['preview_first'] ?? false) : _type == 'mirror');
    final dest = _dest!;
    return {
      ...base,
      'name': _effectiveName,
      'type': _type,
      'enabled': base['enabled'] ?? true,
      'sources': [for (final s in _sources) s.toJson()],
      'dest': dest.withSubPath(_folder.text.trim().replaceAll(RegExp(r'^/+|/+$'), '')).toJson(),
      'triggers': triggers,
      'conditions': conditions,
      'filters': base['filters'] ?? {'exclude_presets': ['caches', 'trash', 'temp'], 'exclude': []},
      'options': options,
      'guards': base['guards'] ?? {'empty_source_pct': 50, 'delete_pct': 10, 'change_pct': 30, 'allow_empty_source': false},
      'retention': {
        if (_type == 'mirror') 'versions_days': keep,
        if (_type == 'archive') 'keep_last': keep,
      },
      'hooks': base['hooks'] ?? const [],
      'retry': base['retry'] ?? {'max': 3, 'backoff_sec': [60, 600, 3600]},
      'notify': base['notify'] ?? {'on_success': false, 'on_failure': true, 'stale_after_hours': _plug && _schedule == BackupSchedule.manual ? 336 : 48},
      'needs_attention': base['needs_attention'] ?? '',
      'migrated_from': base['migrated_from'],
      if (_isEdit) 'id': widget.job!.id,
      if (_isEdit) 'revision': widget.job!.revision,
    };
  }

  Future<void> _save() async {
    final errors = validate();
    setState(() {
      _errors = errors;
      _error = null;
    });
    if (errors.isNotEmpty || _saving) return;
    setState(() => _saving = true);
    try {
      final body = buildJob();
      final saved = _isEdit ? await _api.update(widget.job!.id, body) : await _api.create(body);
      if (!mounted) return;
      showBackupSnack(context, bt(_isEdit ? 'backup.wizard.saved' : 'backup.wizard.created', {'name': saved.name}));
      if (!_isEdit && _runNow) {
        try {
          await _api.run(saved.id);
        } catch (_) {
          // The job exists; its page offers Back up now.
        }
      }
      if (!mounted) return;
      if (_isEdit) {
        Navigator.of(context).pop(saved);
      } else {
        await Navigator.of(context).pushReplacement(MaterialPageRoute<void>(builder: (_) => BackupJobScreen(jobId: saved.id, initial: saved, api: widget.api)));
      }
    } catch (e) {
      if (mounted) setState(() => _error = BackupError.from(e));
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _openWeb() async {
    final url = ApiClient.instance.baseUrl;
    if (url.isEmpty) return;
    try {
      await launchUrl(Uri.parse(url), mode: LaunchMode.externalApplication);
    } catch (_) {}
  }

  String? _err(String field) => _errors[field] ?? _error?.fieldError(field == 'cron' ? 'triggers' : field);

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    final title = _isEdit ? bt('backup.jobs.menu.edit') : bt('backup.jobs.new');
    final errStyle = theme.textTheme.bodySmall?.copyWith(color: scheme.error);
    final general = _error != null && _error!.fieldErrors.isEmpty;

    return AppScaffold(
      title: title,
      leading: IconButton(tooltip: bt('backup.close'), icon: const Icon(Icons.close), onPressed: () => Navigator.of(context).maybePop()),
      actions: [
        Padding(
          padding: const EdgeInsets.only(right: Space.sm),
          child: _saving
              ? const Padding(padding: EdgeInsets.all(Space.md), child: SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2)))
              : TextButton(onPressed: _save, child: Text(_isEdit ? bt('backup.save') : bt('backup.wizard.create'))),
        ),
      ],
      body: ListView(
        padding: EdgeInsets.only(bottom: Space.xl + MediaQuery.paddingOf(context).bottom),
        children: [
          if (general)
            Notice(status: Status.error, title: _error!.title, message: _error!.code == 'revision_conflict' ? 'Someone else changed this job. Go back and open it again.' : (_error!.fix.isNotEmpty ? _error!.fix : _error!.causeText)),
          SectionHeader(title: bt('backup.wizard.what.type_heading')),
          Padding(
            padding: EdgeInsets.symmetric(horizontal: gutter),
            child: SegmentedButton<String>(
              segments: [
                for (final t in const ['mirror', 'copy', 'archive'])
                  ButtonSegment(value: t, label: Text(t == 'copy' ? 'Copy' : bt('backup.type.$t.label'))),
              ],
              selected: {_type},
              onSelectionChanged: (s) => setState(() {
                final was = _type;
                _type = s.first;
                if (was != _type && (_type == 'archive' || was == 'archive')) _keep.text = _type == 'archive' ? '8' : '30';
              }),
            ),
          ),
          Padding(
            padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0),
            child: Text('${bt('backup.type.$_type.promise')} ${bt('backup.type.$_type.deleted')}', style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant)),
          ),
          TileGroup(title: bt('backup.wizard.field.sources'), children: [
            for (var i = 0; i < _sources.length; i++)
              ListTile(
                leading: const Icon(Icons.folder_outlined),
                title: Text(_sources[i].display),
                onTap: () => _pickSource(i),
                trailing: _sources.length > 1
                    ? IconButton(
                        tooltip: bt('backup.wizard.what.remove_source', {'name': _sources[i].shortName}),
                        icon: const Icon(Icons.close),
                        onPressed: () => setState(() => _sources = [..._sources]..removeAt(i)),
                      )
                    : const Icon(Icons.chevron_right),
              ),
            if (_sources.isEmpty || _type == 'archive')
              ListTile(
                leading: const Icon(Icons.add),
                title: Text(_sources.isEmpty ? bt('backup.wizard.what.choose_source') : bt('backup.wizard.what.add_folder')),
                subtitle: _err('sources') != null ? Text(_err('sources')!, style: errStyle) : null,
                onTap: () => _pickSource(),
              ),
          ]),
          if (_sources.length > 1 && _type != 'archive')
            Padding(padding: EdgeInsets.symmetric(horizontal: gutter), child: Text(bt('backup.wizard.what.many_needs_archive'), style: errStyle)),
          TileGroup(title: bt('backup.wizard.field.dest'), children: [
            ListTile(
              leading: Icon(_dest == null ? Icons.add : Icons.save_outlined),
              title: Text(_dest == null ? bt('backup.wizard.where.choose') : (_dest!.label.isNotEmpty ? _dest!.label : _dest!.refId)),
              subtitle: _err('dest') != null ? Text(_err('dest')!, style: errStyle) : (_dest == null ? null : Text(bt('backup.ep.${_dest!.kind}'))),
              trailing: const Icon(Icons.chevron_right),
              onTap: _pickDest,
            ),
          ]),
          if (_dest != null)
            Padding(
              padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0),
              child: TextField(
                controller: _folder,
                onChanged: (_) => _folderTouched = true,
                decoration: InputDecoration(
                  labelText: bt('backup.wizard.where.folder'),
                  helperText: bt('backup.wizard.where.folder_hint'),
                  helperMaxLines: 3,
                  errorText: _error?.fieldError('dest.sub_path'),
                ),
              ),
            ),
          SectionHeader(title: bt('backup.wizard.when.run_heading')),
          RadioGroup<BackupSchedule>(
            groupValue: _schedule,
            onChanged: (s) => setState(() => _schedule = s ?? BackupSchedule.manual),
            child: Column(mainAxisSize: MainAxisSize.min, children: [
              RadioListTile(value: BackupSchedule.manual, title: Text(bt('backup.jobs.manual_only'))),
              RadioListTile(value: BackupSchedule.daily, title: const Text('Every day')),
              RadioListTile(value: BackupSchedule.weekly, title: const Text('Every week')),
              RadioListTile(value: BackupSchedule.custom, title: const Text('Custom schedule')),
            ]),
          ),
          if (_schedule == BackupSchedule.weekly)
            Padding(
              padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.sm),
              child: Wrap(spacing: Space.sm, runSpacing: Space.sm, children: [
                for (var d = 0; d < 7; d++)
                  ChoiceChip(
                    label: Text(const ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][d]),
                    selected: _weekday == d,
                    onSelected: (_) => setState(() => _weekday = d),
                  ),
              ]),
            ),
          if (_schedule == BackupSchedule.daily || _schedule == BackupSchedule.weekly)
            ListTile(
              leading: const Icon(Icons.schedule_outlined),
              title: const Text('At'),
              subtitle: Text('${_hour.toString().padLeft(2, '0')}:${_minute.toString().padLeft(2, '0')} (server time)'),
              onTap: _pickTime,
            ),
          if (_schedule == BackupSchedule.custom)
            Padding(
              padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.sm),
              child: TextField(
                controller: _cron,
                onChanged: (_) => setState(() {}),
                decoration: InputDecoration(
                  labelText: bt('backup.wizard.field.cron'),
                  helperText: _cron.text.trim().isEmpty ? 'Five fields: minute hour day month weekday' : cronText(_cron.text),
                  helperMaxLines: 2,
                  errorText: _err('cron'),
                ),
              ),
            ),
          if (_schedule != BackupSchedule.manual && _schedule != BackupSchedule.custom)
            Padding(
              padding: EdgeInsets.symmetric(horizontal: gutter),
              child: Text(cronText(_cronSpec), style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant)),
            ),
          if (_plugPossible)
            SwitchListTile(
              value: _plug,
              onChanged: (v) => setState(() => _plug = v),
              title: Text(bt('backup.wizard.when.on_plug_generic')),
              subtitle: Text(bt('backup.wizard.when.plug_gap_hint')),
            ),
          if (_type != 'copy') ...[
            SectionHeader(title: bt('backup.wizard.keep.title')),
            Padding(
              padding: EdgeInsets.symmetric(horizontal: gutter),
              child: TextField(
                controller: _keep,
                keyboardType: TextInputType.number,
                decoration: InputDecoration(
                  labelText: _type == 'mirror' ? bt('backup.wizard.field.retention_versions_days') : bt('backup.wizard.field.retention_keep_last'),
                  helperText: _type == 'mirror'
                      ? '${bt('backup.wizard.keep.recycle_hint')} ${bt('backup.wizard.keep.recycle_forever_hint')}'
                      : bt('backup.wizard.keep.keep_last_hint'),
                  helperMaxLines: 4,
                  errorText: _err('retention'),
                ),
              ),
            ),
          ],
          SectionHeader(title: bt('backup.wizard.review.name')),
          Padding(
            padding: EdgeInsets.symmetric(horizontal: gutter),
            child: TextField(
              controller: _name,
              onChanged: (_) => setState(_syncFolder),
              decoration: InputDecoration(
                hintText: _defaultName.isEmpty ? bt('backup.wizard.review.name_placeholder') : _defaultName,
                helperText: bt('backup.wizard.review.name_hint'),
                errorText: _err('name'),
              ),
            ),
          ),
          if (!_isEdit)
            SwitchListTile(
              value: _runNow,
              onChanged: (v) => setState(() => _runNow = v),
              title: Text(_type == 'mirror' ? bt('backup.wizard.review.run_now_preview') : bt('backup.wizard.review.run_now')),
            ),
          const SizedBox(height: Space.md),
          TileGroup(
            footer: 'What to skip, time windows, stopping apps while they are backed up, safety limits, retries and notifications keep '
                '${_isEdit ? 'their current settings' : 'the recommended settings'}. Change them in the web interface.',
            children: [
              ListTile(
                leading: const Icon(Icons.open_in_new_outlined),
                title: const Text('More options in the web interface'),
                onTap: _openWeb,
              ),
            ],
          ),
          if (_isEdit && widget.job!.type != _type)
            Notice(status: Status.warning, message: 'Changing the job type changes what happens at the destination: ${bt('backup.type.$_type.deleted')}'),
        ],
      ),
    );
  }
}
