import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../backup/backup_api.dart';
import '../../backup/backup_models.dart';
import '../../backup/backup_state.dart' show cronText;
import '../../backup/backup_strings.dart';
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

/// Exclude presets, in the web's order (engine.ExcludePresets).
const excludePresets = ['caches', 'trash', 'temp', 'thumbs', 'node_modules'];

/// Under TeraBox's (free plan) and FAT32's 4 GB file limit (draft.js).
const _defaultVolumeBytes = 3900000000;

String _appOf(BackupEndpoint e) => e.preset.startsWith('appdata:') ? e.preset.substring(8) : '';
String _vmOf(BackupEndpoint e) => e.preset.startsWith('vm:') ? e.preset.substring(3) : '';
List<String> _lines(String text) => [for (final l in text.split('\n')) if (l.trim().isNotEmpty) l.trim()];
int? _int(TextEditingController c) => int.tryParse(c.text.trim());
bool _intIn(TextEditingController c, int min, int max) {
  final n = _int(c);
  return n != null && n >= min && n <= max;
}

/// Create or edit a job (the web's job wizard on one page). The common
/// choices come first - kind, what, where, when, what it keeps, its name -
/// and every other option the web has sits in a collapsed group: what to
/// skip, stopping apps and VMs during the run, when it may run, safety
/// limits, and the advanced ones (priority, time limit, include patterns,
/// notifications). An edit sends back every field it doesn't show
/// unchanged, with the revision it was read at.
///
/// [copyOf] starts a new job from a stored one (Duplicate, "Start a new
/// encrypted copy"): its settings with a new name and a folder of its own;
/// an encrypted one asks for a new password.
class BackupJobFormScreen extends StatefulWidget {
  const BackupJobFormScreen({super.key, this.job, this.copyOf, this.api});

  /// The job to edit; null creates one.
  final BackupJob? job;
  final BackupJob? copyOf;
  final BackupApi? api;

  @override
  State<BackupJobFormScreen> createState() => BackupJobFormScreenState();
}

@visibleForTesting
class BackupJobFormScreenState extends State<BackupJobFormScreen> {
  late final BackupApi _api = widget.api ?? BackupApi();

  /// The job the form starts from (edited or copied), null for a new one.
  late final BackupJob? _from = widget.job ?? widget.copyOf;
  late final Map<String, Object?> _raw = _from?.raw ?? const {};
  Map<String, Object?> _section(String k) => Map<String, Object?>.from(_raw[k] as Map? ?? const {});

  late final _name = TextEditingController(text: widget.job?.name ?? (widget.copyOf == null ? '' : bt('backup.jobs.copy_name', {'name': widget.copyOf!.name})));
  late final _folder = TextEditingController(text: widget.job?.dest.subPath ?? '');
  late final _cron = TextEditingController();
  late final _keep = TextEditingController();

  late String _type = _from?.type ?? 'mirror';
  late List<BackupEndpoint> _sources = [...?_from?.sources];
  late BackupEndpoint? _dest = _from?.dest.withSubPath('');
  bool _folderTouched = false;
  late BackupSchedule _schedule;
  int _hour = 3, _minute = 0, _weekday = 0;
  late bool _plug = _from?.triggers.any((t) => t.kind == 'volume_mounted') ?? false;
  late bool _runNow = widget.copyOf == null;
  bool _saving = false;
  BackupError? _error;

  // What to skip (filters).
  late final Set<String> _skip = {...((_section('filters')['exclude_presets'] as List?) ?? (_from == null ? const ['caches', 'trash', 'temp'] : const [])).map((e) => '$e')};
  late final _exclude = TextEditingController(text: ((_section('filters')['exclude'] as List?) ?? const []).join('\n'));
  late final _include = TextEditingController(text: ((_section('filters')['include'] as List?) ?? const []).join('\n'));
  late final num _maxSizeBytes = (_section('filters')['max_size_bytes'] as num?) ?? 0;
  late bool _maxSizeOn = _maxSizeBytes > 0;
  late final _maxSizeGb = TextEditingController(text: _maxSizeBytes > 0 ? _trimGb(_maxSizeBytes / 1e9) : '4');

  // Apps and VMs stopped during the run (hooks).
  Map<String, bool> _stopApps = {};
  Map<String, bool> _shutdownVms = {};
  List<String> _appsFromJob = const [], _vmsFromJob = const [];
  String _appMode = 'together';
  final List<Map<String, Object?>> _extraHooks = [];
  final Map<String, Map<String, Object?>> _hookTimeouts = {};

  // When it may run (conditions, triggers, retry).
  late bool _destAvailable = _section('conditions')['dest_available'] != false;
  late String _whenUnmet = '${_section('conditions')['when_unmet'] ?? ''}';
  late int _waitMaxMin = (_section('conditions')['wait_max_min'] as num?)?.round() ?? 360;
  late bool _windowOn = (_from?.windowStart ?? '').isNotEmpty && (_from?.windowEnd ?? '').isNotEmpty;
  late String _windowStart = (_from?.windowStart ?? '').isNotEmpty ? _from!.windowStart : '22:00';
  late String _windowEnd = (_from?.windowEnd ?? '').isNotEmpty ? _from!.windowEnd : '07:00';
  late bool _catchUp = (_from?.triggers ?? const []).isEmpty || _from!.triggers.any((t) => t.catchUp);
  late final _plugGap = TextEditingController(text: '${_from?.triggers.where((t) => t.kind == 'volume_mounted').firstOrNull?.minGapHours ?? 24}');
  late int _retryMax = (_section('retry')['max'] as num?)?.round() ?? 3;
  late final List<int> _backoff = [for (final s in (_section('retry')['backoff_sec'] as List?) ?? const [60, 600, 3600]) if (s is num) s.round()];

  // Safety (guards) and options.
  late final _deletePct = TextEditingController(text: '${(_section('guards')['delete_pct'] as num?)?.round() ?? 10}');
  late final _changePct = TextEditingController(text: '${(_section('guards')['change_pct'] as num?)?.round() ?? 30}');
  late final _emptyPct = TextEditingController(text: '${(_section('guards')['empty_source_pct'] as num?)?.round() ?? 50}');
  late bool _allowEmpty = _section('guards')['allow_empty_source'] == true;
  late bool _previewFirst = _from == null ? _type == 'mirror' : _section('options')['preview_first'] == true;
  late bool _previewTouched = _from != null;
  late bool _verify = _section('options')['verify'] == true;
  late bool _lowPriority = _section('options')['low_priority'] != false;
  late bool _copyEmptyDirs = _section('options')['copy_empty_dirs'] == true;
  late final _maxHours = TextEditingController(
      text: '${(_section('options')['max_duration_sec'] as num?) == null ? 24 : ((_section('options')['max_duration_sec'] as num) / 3600).round().clamp(1, 720)}');
  late bool _notifySuccess = _section('notify')['on_success'] == true;

  // Encryption: chosen when a job is created, never changed after.
  late String _encryptMode = _from?.dest.encryptionMode ?? '';
  final _password = TextEditingController();
  final _confirm = TextEditingController();
  final _recoveryKey = TextEditingController();
  bool _makeRecovery = true;
  bool _useRecovery = false;
  bool _showPassword = false;

  /// The collapsed groups, opened when one of their fields has an error.
  final Map<String, ExpansibleController> _groups = {for (final g in const ['apps', 'skip', 'when', 'safety', 'advanced']) g: ExpansibleController()};

  bool get _isEdit => widget.job != null;

  static String _trimGb(num gb) {
    final r = (gb * 10).round() / 10;
    return r == r.roundToDouble() ? '${r.round()}' : '$r';
  }

  @override
  void initState() {
    super.initState();
    final s = parseSchedule(_from?.triggers ?? const []);
    _schedule = _from == null ? BackupSchedule.daily : s.kind;
    _hour = s.hour;
    _minute = s.minute;
    _weekday = s.weekday;
    _cron.text = s.cron;
    final j = _from;
    _keep.text = j == null ? '30' : (j.type == 'archive' ? '${j.keepLast > 0 ? j.keepLast : 8}' : '${j.versionsDays}');
    _folderTouched = _isEdit;
    if (!_isEdit && _dest != null) _syncFolder();
    _readHooks();
  }

  /// The stop/start pairs the form shows as switches; any other hook is
  /// kept as it is (draft.js draftFromJob).
  void _readHooks() {
    for (final h in (_raw['hooks'] as List?) ?? const []) {
      if (h is! Map) continue;
      final m = Map<String, Object?>.from(h);
      final pre = m['phase'] == 'pre';
      if (pre && m['action'] == 'stop_apps') {
        for (final a in (m['apps'] as List?) ?? const []) {
          _stopApps['$a'] = true;
        }
        if (m['app_mode'] != null) _appMode = '${m['app_mode']}';
        _hookTimeouts['apps'] = {'timeout_sec': m['timeout_sec'], 'fail_policy': m['fail_policy']};
      } else if (pre && m['action'] == 'shutdown_vm' && '${m['vm'] ?? ''}'.isNotEmpty) {
        _shutdownVms['${m['vm']}'] = true;
        _hookTimeouts['vm:${m['vm']}'] = {'timeout_sec': m['timeout_sec'], 'fail_policy': m['fail_policy']};
      } else if (m['phase'] == 'post' && (m['action'] == 'start_apps' || m['action'] == 'start_vm')) {
        // Rebuilt from the pre hooks.
      } else {
        _extraHooks.add(m);
      }
    }
    if (_from != null) {
      for (final s in _sources) {
        if (_appOf(s).isNotEmpty) _stopApps.putIfAbsent(_appOf(s), () => false);
        if (_vmOf(s).isNotEmpty) _shutdownVms.putIfAbsent(_vmOf(s), () => false);
      }
      _appsFromJob = _stopApps.keys.toList();
      _vmsFromJob = _shutdownVms.keys.toList();
    }
    _syncHooks();
  }

  /// A "stop" switch (on by default: app data copied while the app runs
  /// can be corrupt) for every app or VM the sources hold; switches of
  /// apps no longer backed up go, unless the job had them (syncHookChoices).
  void _syncHooks() {
    final keepApps = {for (final h in _extraHooks) for (final a in (h['apps'] as List?) ?? const []) '$a'};
    final stop = <String, bool>{};
    for (final a in _sources.map(_appOf).where((a) => a.isNotEmpty)) {
      stop[a] = _stopApps[a] ?? true;
    }
    for (final a in _stopApps.keys) {
      if (!stop.containsKey(a) && _appsFromJob.contains(a)) stop[a] = _stopApps[a]!;
    }
    for (final a in keepApps) {
      stop.putIfAbsent(a, () => true);
    }
    final vms = <String, bool>{};
    for (final v in _sources.map(_vmOf).where((v) => v.isNotEmpty)) {
      vms[v] = _shutdownVms[v] ?? true;
    }
    for (final v in _shutdownVms.keys) {
      if (!vms.containsKey(v) && _vmsFromJob.contains(v)) vms[v] = _shutdownVms[v]!;
    }
    _stopApps = stop;
    _shutdownVms = vms;
  }

  List<Map<String, Object?>> _hooks() {
    final apps = [for (final e in _stopApps.entries) if (e.value) e.key];
    final mode = _appMode == 'one_at_a_time' ? 'one_at_a_time' : 'together';
    final ta = _hookTimeouts['apps'] ?? const {};
    return [
      if (apps.isNotEmpty) ...[
        {'phase': 'pre', 'action': 'stop_apps', 'apps': apps, 'app_mode': mode, 'timeout_sec': ta['timeout_sec'] ?? 300, 'fail_policy': ta['fail_policy'] ?? 'abort'},
        {'phase': 'post', 'action': 'start_apps', 'apps': [...apps], 'app_mode': mode, 'timeout_sec': ta['timeout_sec'] ?? 300, 'fail_policy': 'continue'},
      ],
      for (final vm in [for (final e in _shutdownVms.entries) if (e.value) e.key]) ...[
        {'phase': 'pre', 'action': 'shutdown_vm', 'vm': vm, 'timeout_sec': _hookTimeouts['vm:$vm']?['timeout_sec'] ?? 600, 'fail_policy': _hookTimeouts['vm:$vm']?['fail_policy'] ?? 'abort'},
        {'phase': 'post', 'action': 'start_vm', 'vm': vm, 'timeout_sec': _hookTimeouts['vm:$vm']?['timeout_sec'] ?? 600, 'fail_policy': 'continue'},
      ],
      ..._extraHooks,
    ];
  }

  @override
  void dispose() {
    for (final c in [_name, _folder, _cron, _keep, _exclude, _include, _maxSizeGb, _plugGap, _deletePct, _changePct, _emptyPct, _maxHours, _password, _confirm, _recoveryKey]) {
      c.dispose();
    }
    for (final g in _groups.values) {
      g.dispose();
    }
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

  /// "wait" for scheduled jobs, "skip" otherwise, unless chosen.
  String get _unmet => _whenUnmet.isNotEmpty ? _whenUnmet : (_schedule != BackupSchedule.manual ? 'wait' : 'skip');

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
      _syncHooks();
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

  Future<TimeOfDay?> _askTime(int hour, int minute) => showTimePicker(context: context, initialTime: TimeOfDay(hour: hour, minute: minute));

  Future<void> _pickTime() async {
    final t = await _askTime(_hour, _minute);
    if (t != null && mounted) {
      setState(() {
        _hour = t.hour;
        _minute = t.minute;
      });
    }
  }

  static String _hhmm(TimeOfDay t) => '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';

  Future<void> _pickWindow(bool start) async {
    final cur = (start ? _windowStart : _windowEnd).split(':');
    final t = await _askTime(int.tryParse(cur.first) ?? 22, int.tryParse(cur.last) ?? 0);
    if (t != null && mounted) setState(() => start ? _windowStart = _hhmm(t) : _windowEnd = _hhmm(t));
  }

  /// Problems the form can see before asking the server (the web's
  /// validateDraft); field (the server's path) -> sentence.
  @visibleForTesting
  Map<String, String> validate() {
    final e = <String, String>{};
    String f(String code) => BackupError.fieldText(code);
    if (_sources.isEmpty) e['sources'] = f('required');
    if (_type != 'archive' && _sources.length > 1) e['sources'] = bt('backup.wizard.what.many_needs_archive');
    if (_sources.length > 16) e['sources'] = f('too_many');
    if (_dest == null) e['dest'] = f('required');
    if (_effectiveName.isEmpty) e['name'] = f('required');
    if (_effectiveName.length > 200) e['name'] = f('out_of_range');
    if (_schedule == BackupSchedule.custom && _cron.text.trim().split(RegExp(r'\s+')).length != 5 && !_cron.text.trim().startsWith('@')) {
      e['cron'] = f('invalid_cron');
    }
    if (_type == 'mirror' && !_intIn(_keep, 0, 3650)) e['retention'] = f('out_of_range');
    if (_type == 'archive' && !_intIn(_keep, 1, 1000)) e['retention'] = f('out_of_range');
    if (_maxSizeOn && !((double.tryParse(_maxSizeGb.text.trim()) ?? 0) > 0)) e['filters.max_size_bytes'] = f('out_of_range');
    if (_plug && _plugPossible && !_intIn(_plugGap, 0, 720)) e['plug'] = f('out_of_range');
    if (_windowOn && _windowStart == _windowEnd) e['conditions.window'] = f('invalid');
    for (final (c, k) in [(_deletePct, 'guards.delete_pct'), (_changePct, 'guards.change_pct'), (_emptyPct, 'guards.empty_source_pct')]) {
      if (!_intIn(c, 1, 100)) e[k] = f('out_of_range');
    }
    if (!_intIn(_maxHours, 1, 720)) e['options.max_duration_sec'] = f('out_of_range');
    if (_encryptMode == 'archive' && _type != 'archive') e['dest.encryption'] = f('invalid');
    if (!_isEdit && _encryptMode.isNotEmpty) {
      if (_useRecovery) {
        if (_recoveryKey.text.trim().isEmpty) e['encryption_secret.recovery_key'] = f('required');
      } else {
        final pw = _password.text;
        if (pw.isEmpty) {
          e['encryption_secret.password'] = f('required');
        } else if (pw.runes.length < 8 || pw.length > 1024) {
          e['encryption_secret.password'] = f('password_short');
        } else if (RegExp('[\x00\r\n]').hasMatch(pw)) {
          e['encryption_secret.password'] = f('invalid');
        } else if (_confirm.text != pw) {
          e['encryption_secret.confirm'] = f('mismatch');
        }
      }
    }
    return e;
  }

  Map<String, String> _errors = const {};

  /// The Job JSON for POST /jobs or PUT /jobs/:id (draft.js draftToJob).
  @visibleForTesting
  Map<String, Object?> buildJob() {
    final base = Map<String, Object?>.from(_raw);
    // What the server owns or computes, and a copy's identity.
    for (final k in const ['health', 'last_run', 'active_run', 'next_run', 'dest_online', 'stats', 'created_at', 'updated_at', 'recovery_key', 'id', 'revision']) {
      base.remove(k);
    }
    final oldTriggers = [for (final t in (base['triggers'] as List? ?? const [])) if (t is Map) Map<String, Object?>.from(t)];
    final oldPlug = oldTriggers.where((t) => t['kind'] == 'volume_mounted').firstOrNull;
    final gap = _int(_plugGap) ?? 24;
    final triggers = <Map<String, Object?>>[
      if (_schedule != BackupSchedule.manual) {'kind': 'schedule', 'cron': _cronSpec, 'catch_up': _catchUp},
      if (_plug && _plugPossible) {...?oldPlug, 'kind': 'volume_mounted', 'catch_up': _catchUp, 'min_gap_hours': gap}..removeWhere((k, v) => k == 'min_gap_hours' && gap == 0),
      // Anything else the job had (a second schedule) stays.
      for (final t in [...oldTriggers.where((t) => t['kind'] != 'schedule' && t['kind'] != 'volume_mounted'), ...oldTriggers.where((t) => t['kind'] == 'schedule').skip(1)]) {...t, 'catch_up': _catchUp},
    ];
    final keep = _int(_keep) ?? 0;
    final conditions = <String, Object?>{
      'dest_available': _type == 'mirror' ? true : _destAvailable,
      'when_unmet': _unmet,
      if (_unmet == 'wait') 'wait_max_min': _waitMaxMin,
      if (_windowOn) 'window': {'start': _windowStart, 'end': _windowEnd},
    };
    final include = _lines(_include.text);
    final gb = double.tryParse(_maxSizeGb.text.trim()) ?? 0;
    final filters = <String, Object?>{
      'exclude_presets': [for (final p in excludePresets) if (_skip.contains(p)) p],
      'exclude': _lines(_exclude.text),
      if (include.isNotEmpty) 'include': include,
      if (_maxSizeOn && gb > 0) 'max_size_bytes': (gb * 1e9).round(),
    };
    final oldOptions = _section('options');
    final options = <String, Object?>{
      'verify': _verify,
      'preview_first': _type == 'archive' ? false : _previewFirst,
      'low_priority': _lowPriority,
      'max_duration_sec': (_int(_maxHours) ?? 24) * 3600,
      'copy_empty_dirs': _copyEmptyDirs,
      if (oldOptions['preserve_meta'] != null) 'preserve_meta': oldOptions['preserve_meta'],
    };
    final dest = _dest!.withSubPath(_folder.text.trim().replaceAll(RegExp(r'^/+|/+$'), '')).toJson()..remove('encryption');
    // An edit keeps its encryption on whatever destination it moves to:
    // the keys belong to the job, not to the folder.
    final oldEnc = _from?.dest.encryption;
    if (_isEdit && oldEnc != null) {
      dest['encryption'] = oldEnc;
    } else if (_encryptMode.isNotEmpty) {
      dest['encryption'] = {
        'mode': _encryptMode,
        if (_encryptMode == 'archive') 'volume_bytes': (oldEnc?['volume_bytes'] as num?)?.round() ?? _defaultVolumeBytes,
        'recovery_key': false,
      };
    }
    final notify = _section('notify');
    return {
      ...base,
      'name': _effectiveName,
      'type': _type,
      // A copy starts paused when it came from a job that was paused.
      'enabled': base['enabled'] ?? true,
      'sources': [for (final s in _sources) s.toJson()],
      'dest': dest,
      'triggers': triggers,
      'conditions': conditions,
      'filters': filters,
      'options': options,
      'guards': {
        'empty_source_pct': _int(_emptyPct) ?? 50,
        'delete_pct': _int(_deletePct) ?? 10,
        'change_pct': _int(_changePct) ?? 30,
        'allow_empty_source': _allowEmpty,
      },
      'retention': {
        if (_type == 'mirror') 'versions_days': keep,
        if (_type == 'archive') 'keep_last': keep,
      },
      'hooks': _hooks(),
      'retry': {'max': _retryMax, 'backoff_sec': _backoff.isEmpty ? const [60, 600, 3600] : _backoff},
      // 0 lets the server pick (twice the schedule's interval, 48 h at least).
      'notify': {'on_success': _notifySuccess, 'on_failure': true, 'stale_after_hours': (notify['stale_after_hours'] as num?)?.round() ?? 0},
      'needs_attention': _isEdit ? (base['needs_attention'] ?? '') : '',
      'migrated_from': _isEdit ? base['migrated_from'] : null,
      if (!_isEdit && _encryptMode.isNotEmpty)
        'encryption_secret': _useRecovery ? {'recovery_key': _recoveryKey.text.trim()} : {'password': _password.text, 'make_recovery_key': _makeRecovery},
      if (_isEdit) 'id': widget.job!.id,
      if (_isEdit) 'revision': widget.job!.revision,
    };
  }

  /// The collapsed group a field lives in.
  static String? _groupOf(String field) {
    if (field.startsWith('hooks')) return 'apps';
    if (field.startsWith('filters.include')) return 'advanced';
    if (field.startsWith('filters')) return 'skip';
    if (field.startsWith('conditions') || field.startsWith('retry') || field == 'plug') return 'when';
    if (field.startsWith('guards.allow')) return 'advanced';
    if (field.startsWith('guards')) return 'safety';
    if (field.startsWith('options')) return 'advanced';
    return null;
  }

  void _openGroupsWithErrors() {
    final fields = [..._errors.keys, ...?_error?.fieldErrors.keys];
    for (final g in fields.map(_groupOf).nonNulls.toSet()) {
      _groups[g]!.expand();
    }
  }

  Future<void> _save() async {
    final errors = validate();
    setState(() {
      _errors = errors;
      _error = null;
    });
    if (errors.isNotEmpty || _saving) {
      _openGroupsWithErrors();
      return;
    }
    setState(() => _saving = true);
    try {
      final body = buildJob();
      final saved = _isEdit ? await _api.update(widget.job!.id, body) : await _api.create(body);
      if (!mounted) return;
      final recovery = '${saved.raw['recovery_key'] ?? ''}';
      if (recovery.isNotEmpty) await _showRecoveryKey(recovery);
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
      if (!mounted) return;
      setState(() => _error = BackupError.from(e));
      _openGroupsWithErrors();
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  /// The recovery key of a new encrypted job: shown once, never again.
  Future<void> _showRecoveryKey(String key) {
    var saved = false;
    return showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (context) => StatefulBuilder(
        builder: (context, setLocal) => PopScope(
          canPop: saved,
          child: AlertDialog(
            title: Text(bt('backup.encrypt.recovery_title')),
            content: SingleChildScrollView(
              child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(bt('backup.encrypt.recovery_text')),
                const SizedBox(height: Space.md),
                SelectableText(key, style: Theme.of(context).textTheme.titleMedium?.copyWith(fontFamily: 'monospace')),
                TextButton.icon(
                  icon: const Icon(Icons.copy_outlined),
                  label: Text(bt('backup.encrypt.copy')),
                  onPressed: () async {
                    await Clipboard.setData(ClipboardData(text: key));
                    if (context.mounted) showBackupSnack(context, bt('backup.encrypt.copied'));
                  },
                ),
                Text('${bt('backup.encrypt.warning_title')} ${bt('backup.encrypt.recovery_once')}'),
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  value: saved,
                  onChanged: (v) => setLocal(() => saved = v ?? false),
                  title: Text(bt('backup.encrypt.recovery_saved')),
                ),
              ]),
            ),
            actions: [TextButton(onPressed: saved ? () => Navigator.of(context).pop() : null, child: Text(bt('backup.encrypt.recovery_done')))],
          ),
        ),
      ),
    );
  }

  /// Where the server's field errors show: the form's fields by the
  /// server's path prefixes. 'cron' and 'plug' are the schedule and plug-in
  /// triggers, in the order [buildJob] sends them.
  String? _err(String field) {
    final local = _errors[field];
    if (local != null) return local;
    final e = _error;
    if (e == null) return null;
    if (field == 'cron') return _schedule == BackupSchedule.manual ? null : e.fieldError('triggers[0]');
    if (field == 'plug') return e.fieldError('triggers[${_schedule == BackupSchedule.manual ? 0 : 1}]');
    return e.fieldError(field);
  }

  static const _shownFields = [
    'name', 'type', 'sources', 'dest', 'triggers', 'retention', 'filters', 'hooks', 'conditions', 'guards', 'options', 'retry', 'encryption_secret' //
  ];

  /// Server field errors no field of the form shows, as sentences.
  List<String> get _unshownErrors {
    final e = _error;
    if (e == null) return const [];
    return [
      for (final f in e.fieldErrors.entries)
        if (!_shownFields.any((p) => f.key == p || f.key.startsWith('$p.') || f.key.startsWith('$p['))) BackupError.fieldText(f.value),
    ];
  }

  Widget _errorText(String? text) => text == null
      ? const SizedBox.shrink()
      : Padding(
          padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.xs, Space.gutter(context), 0),
          child: Text(text, style: Theme.of(context).textTheme.bodySmall?.copyWith(color: Theme.of(context).colorScheme.error)),
        );

  Widget _number(TextEditingController c, String label, {String? suffix, String? error, String? helper, bool decimal = false, String? key}) => Padding(
        padding: const EdgeInsets.fromLTRB(Space.lg, Space.xs, Space.lg, Space.sm),
        child: TextField(
          key: key == null ? null : Key(key),
          controller: c,
          keyboardType: TextInputType.numberWithOptions(decimal: decimal),
          onChanged: (_) => setState(() {}),
          decoration: InputDecoration(labelText: label, suffixText: suffix, errorText: error, helperText: helper, helperMaxLines: 4),
        ),
      );

  /// A collapsed group of options (progressive disclosure: the common
  /// choices stay on the page, the rest one tap away).
  Widget _group(String id, String title, String? summary, List<Widget> children) => TileGroup(children: [
        ExpansionTile(
          key: ValueKey('backup-form-$id'),
          controller: _groups[id],
          title: Text(title),
          subtitle: summary == null || summary.isEmpty ? null : Text(summary, maxLines: 2, overflow: TextOverflow.ellipsis),
          shape: const Border(),
          collapsedShape: const Border(),
          expandedCrossAxisAlignment: CrossAxisAlignment.stretch,
          childrenPadding: const EdgeInsets.only(bottom: Space.sm),
          children: children,
        ),
      ]);

  Widget _hint(String text) => Padding(
        padding: const EdgeInsets.fromLTRB(Space.lg, 0, Space.lg, Space.sm),
        child: Text(text, style: Theme.of(context).textTheme.bodySmall?.copyWith(color: Theme.of(context).colorScheme.onSurfaceVariant)),
      );

  List<Widget> _appsGroup() {
    if (_stopApps.isEmpty && _shutdownVms.isEmpty) return const [];
    final checked = _stopApps.values.where((v) => v).length;
    return [
      _group('apps', bt('backup.wizard.what.consistency_title', {'n': '${_stopApps.length + _shutdownVms.length}'}), null, [
        _hint(bt('backup.wizard.what.consistency_hint')),
        for (final a in _stopApps.keys)
          SwitchListTile(
            value: _stopApps[a]!,
            onChanged: (v) => setState(() => _stopApps = {..._stopApps, a: v}),
            title: Text(bt('backup.wizard.what.stop_app', {'app': a})),
            subtitle: _stopApps[a]! ? null : Text(bt('backup.wizard.what.stop_app_off', {'app': a})),
          ),
        if (checked > 1)
          RadioGroup<String>(
            groupValue: _appMode == 'one_at_a_time' ? 'one_at_a_time' : 'together',
            onChanged: (v) => setState(() => _appMode = v ?? 'together'),
            child: Column(mainAxisSize: MainAxisSize.min, children: [
              ListTile(dense: true, title: Text(bt('backup.wizard.what.app_mode'))),
              RadioListTile(value: 'together', title: Text(bt('backup.wizard.what.app_mode_together'))),
              RadioListTile(value: 'one_at_a_time', title: Text(bt('backup.wizard.what.app_mode_each'))),
            ]),
          ),
        for (final v in _shutdownVms.keys)
          SwitchListTile(
            value: _shutdownVms[v]!,
            onChanged: (on) => setState(() => _shutdownVms = {..._shutdownVms, v: on}),
            title: Text(bt('backup.wizard.what.shutdown_vm', {'vm': v})),
            subtitle: Text(_shutdownVms[v]! ? bt('backup.wizard.what.shutdown_vm_note') : bt('backup.wizard.what.shutdown_vm_off')),
          ),
        _errorText(_err('hooks')),
      ]),
    ];
  }

  Widget _skipGroup() {
    final patterns = _lines(_exclude.text).length;
    final summary = [
      for (final p in excludePresets) if (_skip.contains(p)) bt('backup.exclude.$p'),
      if (_maxSizeOn) '> ${_maxSizeGb.text.trim()} GB',
      if (patterns > 0) '$patterns ${patterns == 1 ? 'pattern' : 'patterns'}',
    ].join(', ');
    return _group('skip', bt('backup.wizard.what.skip_heading'), summary.isEmpty ? 'Nothing is skipped' : summary, [
      Padding(
        padding: const EdgeInsets.symmetric(horizontal: Space.lg),
        child: Wrap(spacing: Space.sm, runSpacing: Space.sm, children: [
          for (final p in excludePresets)
            FilterChip(
              label: Text(bt('backup.exclude.$p')),
              selected: _skip.contains(p),
              onSelected: (on) => setState(() => on ? _skip.add(p) : _skip.remove(p)),
            ),
        ]),
      ),
      SwitchListTile(value: _maxSizeOn, onChanged: (v) => setState(() => _maxSizeOn = v), title: Text(bt('backup.wizard.what.skip_larger'))),
      if (_maxSizeOn) _number(_maxSizeGb, bt('backup.wizard.what.size_gb'), suffix: bt('backup.wizard.what.gb'), decimal: true, error: _err('filters.max_size_bytes'), key: 'max-size'),
      Padding(
        padding: const EdgeInsets.fromLTRB(Space.lg, Space.sm, Space.lg, 0),
        child: TextField(
          key: const Key('exclude'),
          controller: _exclude,
          minLines: 2,
          maxLines: 6,
          autocorrect: false,
          onChanged: (_) => setState(() {}),
          decoration: InputDecoration(
            labelText: bt('backup.wizard.what.patterns_label'),
            hintText: bt('backup.wizard.what.pattern_placeholder'),
            helperText: 'One pattern per line. ${bt('backup.wizard.what.pattern_hint')}',
            helperMaxLines: 4,
            errorText: _err('filters.exclude'),
          ),
        ),
      ),
    ]);
  }

  Widget _encryptionGroup(TextStyle? errStyle) {
    if (_isEdit) {
      final mode = _encryptMode;
      return TileGroup(title: bt('backup.encrypt.heading'), children: [
        ListTile(
          leading: Icon(mode.isEmpty ? Icons.lock_open_outlined : Icons.lock_outline),
          title: Text(bt('backup.encrypt.mode_${mode.isEmpty ? 'off' : mode}')),
          subtitle: Text(mode.isEmpty ? bt('backup.encrypt.cant_add') : bt('backup.encrypt.cant_change')),
        ),
      ]);
    }
    final pwError = _err('encryption_secret.password');
    return TileGroup(title: bt('backup.encrypt.heading'), footer: bt('backup.encrypt.intro'), children: [
      RadioGroup<String>(
        groupValue: _encryptMode,
        onChanged: (m) => setState(() => _encryptMode = m ?? ''),
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          for (final m in const ['', 'folder', 'archive'])
            RadioListTile<String>(
              value: m,
              enabled: m != 'archive' || _type == 'archive',
              title: Text(bt('backup.encrypt.mode_${m.isEmpty ? 'off' : m}')),
              subtitle: _encryptMode == m && m.isNotEmpty ? Text(bt('backup.encrypt.mode_${m}_hint', {'size': '3.9 GB'}), maxLines: 4, overflow: TextOverflow.ellipsis) : null,
            ),
        ]),
      ),
      if (_err('dest.encryption') != null) _errorText(_err('dest.encryption')),
      if (_encryptMode.isNotEmpty) ...[
        if (!_useRecovery) ...[
          Padding(
            padding: const EdgeInsets.fromLTRB(Space.lg, Space.sm, Space.lg, 0),
            child: TextField(
              key: const Key('password'),
              controller: _password,
              obscureText: !_showPassword,
              autocorrect: false,
              enableSuggestions: false,
              autofillHints: const [AutofillHints.newPassword],
              decoration: InputDecoration(
                labelText: bt('backup.encrypt.password'),
                helperText: bt('backup.encrypt.strength_hint'),
                helperMaxLines: 3,
                errorText: pwError,
                suffixIcon: IconButton(
                  tooltip: bt(_showPassword ? 'backup.encrypt.hide' : 'backup.encrypt.show'),
                  icon: Icon(_showPassword ? Icons.visibility_off_outlined : Icons.visibility_outlined),
                  onPressed: () => setState(() => _showPassword = !_showPassword),
                ),
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(Space.lg, Space.sm, Space.lg, 0),
            child: TextField(
              key: const Key('confirm'),
              controller: _confirm,
              obscureText: !_showPassword,
              autocorrect: false,
              enableSuggestions: false,
              decoration: InputDecoration(labelText: bt('backup.encrypt.confirm'), errorText: _err('encryption_secret.confirm')),
            ),
          ),
          SwitchListTile(
            value: _makeRecovery,
            onChanged: (v) => setState(() => _makeRecovery = v),
            title: Text(bt('backup.encrypt.make_recovery')),
            subtitle: Text(bt('backup.encrypt.make_recovery_hint')),
          ),
        ],
        SwitchListTile(
          value: _useRecovery,
          onChanged: (v) => setState(() => _useRecovery = v),
          title: Text(bt('backup.encrypt.use_recovery')),
        ),
        if (_useRecovery)
          Padding(
            padding: const EdgeInsets.fromLTRB(Space.lg, 0, Space.lg, Space.sm),
            child: TextField(
              key: const Key('recovery-key'),
              controller: _recoveryKey,
              autocorrect: false,
              decoration: InputDecoration(
                labelText: bt('backup.encrypt.recovery_key'),
                helperText: bt('backup.encrypt.recovery_key_hint'),
                helperMaxLines: 3,
                errorText: _err('encryption_secret.recovery_key') ?? (_useRecovery ? pwError : null),
              ),
            ),
          ),
        ListTile(
          leading: Icon(Icons.warning_amber_outlined, color: errStyle?.color),
          title: Text('${bt('backup.encrypt.warning_title')} ${bt('backup.encrypt.warning')}', style: Theme.of(context).textTheme.bodySmall),
        ),
      ],
    ]);
  }

  Widget _whenGroup() {
    final unmet = _unmet;
    final waitChoices = {1, 2, 3, 6, 12, 24, 48, (_waitMaxMin / 60).round().clamp(1, 168)}.toList()..sort();
    final retryChoices = {0, 1, 2, 3, 5, _retryMax}.toList()..sort();
    final summary = [
      if (_windowOn) '$_windowStart–$_windowEnd',
      bt('backup.wizard.when.unmet.$unmet'),
      _retryMax == 0 ? 'no retries' : 'retry ${bt('backup.wizard.when.retry_times', {'n': '$_retryMax'})}',
    ].join(' · ');
    return _group('when', bt('backup.wizard.when.only_heading'), summary, [
      SwitchListTile(
        value: _type == 'mirror' || _destAvailable,
        onChanged: _type == 'mirror' ? null : (v) => setState(() => _destAvailable = v),
        title: Text(bt('backup.wizard.when.dest_available')),
        subtitle: _type == 'mirror' ? Text(bt('backup.wizard.when.mirror_locked')) : null,
      ),
      _errorText(_err('conditions.dest_available')),
      SwitchListTile(
        value: _windowOn,
        onChanged: (v) => setState(() => _windowOn = v),
        title: Text(bt('backup.wizard.when.window')),
        subtitle: Text(_windowOn && _windowEnd.compareTo(_windowStart) < 0 ? bt('backup.wizard.when.window_wraps') : bt('backup.wizard.when.window_hint')),
      ),
      if (_windowOn)
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: Space.lg),
          child: Wrap(spacing: Space.sm, crossAxisAlignment: WrapCrossAlignment.center, children: [
            Text(bt('backup.wizard.when.window_from')),
            OutlinedButton(key: const Key('window-start'), onPressed: () => _pickWindow(true), child: Text(_windowStart)),
            Text(bt('backup.wizard.when.window_to')),
            OutlinedButton(key: const Key('window-end'), onPressed: () => _pickWindow(false), child: Text(_windowEnd)),
          ]),
        ),
      _errorText(_err('conditions.window')),
      ListTile(title: Text(bt('backup.wizard.when.if_not')), dense: true),
      RadioGroup<String>(
        groupValue: unmet,
        onChanged: (v) => setState(() => _whenUnmet = v ?? ''),
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          for (final u in const ['skip', 'wait', 'fail'])
            RadioListTile<String>(value: u, title: Text(bt('backup.wizard.when.unmet.$u')), subtitle: Text(bt('backup.wizard.when.unmet_hint.$u'))),
        ]),
      ),
      if (unmet == 'wait')
        ListTile(
          title: Text(bt('backup.wizard.field.wait_max_min')),
          trailing: DropdownButton<int>(
            key: const Key('wait-hours'),
            value: (_waitMaxMin / 60).round().clamp(1, 168),
            items: [for (final h in waitChoices) DropdownMenuItem(value: h, child: Text(bt('backup.wizard.when.n_hours', {'n': '$h'})))],
            onChanged: (h) => setState(() => _waitMaxMin = (h ?? 6) * 60),
          ),
        ),
      _errorText(_err('conditions.wait_max_min') ?? _err('conditions.when_unmet')),
      if (_schedule != BackupSchedule.manual || _plug)
        SwitchListTile(value: _catchUp, onChanged: (v) => setState(() => _catchUp = v), title: Text(bt('backup.wizard.when.catch_up'))),
      if (_plug && _plugPossible)
        _number(_plugGap, bt('backup.wizard.when.plug_gap'), suffix: bt('backup.wizard.when.hours'), helper: bt('backup.wizard.when.plug_gap_hint'), error: _err('plug'), key: 'plug-gap'),
      ListTile(
        title: Text(bt('backup.wizard.when.retry')),
        subtitle: Text(_retryMax == 0 ? bt('backup.wizard.when.retry_off_hint') : bt('backup.wizard.when.retry_hint', {'delays': _delays()})),
        trailing: DropdownButton<int>(
          key: const Key('retry'),
          value: _retryMax,
          items: [for (final n in retryChoices) DropdownMenuItem(value: n, child: Text(n == 0 ? bt('backup.wizard.when.retry_never') : bt('backup.wizard.when.retry_times', {'n': '$n'})))],
          onChanged: (n) => setState(() => _retryMax = n ?? 0),
        ),
      ),
      _errorText(_err('retry')),
    ]);
  }

  /// "1 min, 10 min and 1 hr": the wait before each retry, the last repeating.
  String _delays() {
    final b = _backoff.isEmpty ? const [60] : _backoff;
    return formatList([for (var i = 0; i < _retryMax; i++) formatSeconds(b[i < b.length ? i : b.length - 1])]);
  }

  Widget _safetyGroup() {
    final summary = _type == 'mirror'
        ? bt('backup.jobs.safety', {'delete': _deletePct.text.trim(), 'change': _changePct.text.trim()})
        : _type == 'copy'
            ? bt('backup.summary.guard_change', {'change': _changePct.text.trim()})
            : null;
    return _group('safety', bt('backup.wizard.keep.safety_heading'), summary, [
      if (_type != 'archive') ...[
        _hint('${bt('backup.wizard.keep.guard_intro')} …'),
        if (_type == 'mirror') _number(_deletePct, bt('backup.wizard.keep.delete_more'), suffix: '%', error: _err('guards.delete_pct'), key: 'delete-pct'),
        _number(_changePct, _type == 'mirror' ? bt('backup.wizard.keep.or_change_more') : bt('backup.wizard.keep.change_more'),
            suffix: '%', helper: '${bt('backup.wizard.keep.stop_and_ask')} ${bt('backup.wizard.keep.guard_hint')}', error: _err('guards.change_pct'), key: 'change-pct'),
      ],
      _number(_emptyPct, bt('backup.wizard.keep.empty_source'), suffix: '%', helper: bt('backup.wizard.keep.empty_source_after'), error: _err('guards.empty_source_pct'), key: 'empty-pct'),
      if (_type != 'archive')
        SwitchListTile(
          value: _previewFirst,
          onChanged: (v) => setState(() {
            _previewFirst = v;
            _previewTouched = true;
          }),
          title: Text(bt('backup.wizard.keep.preview_first')),
          subtitle: Text(bt('backup.wizard.keep.preview_first_hint')),
        ),
      SwitchListTile(
        value: _verify,
        onChanged: (v) => setState(() => _verify = v),
        title: Text(bt('backup.wizard.keep.verify')),
        subtitle: Text(bt('backup.wizard.keep.verify_hint')),
      ),
    ]);
  }

  Widget _advancedGroup() => _group('advanced', bt('backup.wizard.review.advanced'), 'Priority, time limit, include patterns, notifications', [
        SwitchListTile(
          value: _lowPriority,
          onChanged: (v) => setState(() => _lowPriority = v),
          title: Text(bt('backup.wizard.review.low_priority')),
          subtitle: Text(bt('backup.wizard.review.low_priority_hint')),
        ),
        _number(_maxHours, bt('backup.wizard.review.max_duration'), suffix: bt('backup.wizard.when.hours'), error: _err('options.max_duration_sec'), key: 'max-hours'),
        Padding(
          padding: const EdgeInsets.fromLTRB(Space.lg, Space.sm, Space.lg, 0),
          child: TextField(
            key: const Key('include'),
            controller: _include,
            minLines: 1,
            maxLines: 6,
            autocorrect: false,
            decoration: InputDecoration(
              labelText: bt('backup.wizard.review.include'),
              helperText: bt('backup.wizard.review.include_hint'),
              helperMaxLines: 3,
              errorText: _err('filters.include'),
            ),
          ),
        ),
        SwitchListTile(value: _copyEmptyDirs, onChanged: (v) => setState(() => _copyEmptyDirs = v), title: Text(bt('backup.wizard.review.empty_dirs'))),
        SwitchListTile(
          value: _allowEmpty,
          onChanged: (v) => setState(() => _allowEmpty = v),
          title: Text(bt('backup.wizard.review.allow_empty')),
          subtitle: Text(bt('backup.wizard.review.allow_empty_hint')),
        ),
        SwitchListTile(value: _notifySuccess, onChanged: (v) => setState(() => _notifySuccess = v), title: Text(bt('backup.wizard.review.notify_success'))),
        SwitchListTile(
          value: true,
          onChanged: null,
          title: Text(bt('backup.wizard.review.notify_failure')),
          subtitle: Text(bt('backup.wizard.review.notify_failure_hint')),
        ),
      ]);

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    final title = _isEdit ? bt('backup.jobs.menu.edit') : bt('backup.jobs.new');
    final errStyle = theme.textTheme.bodySmall?.copyWith(color: scheme.error);
    final error = _error;
    final unshown = _unshownErrors;
    final general = error != null && (error.fieldErrors.isEmpty || unshown.isNotEmpty);

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
            Notice(
              status: Status.error,
              title: error.title,
              message: error.code == 'revision_conflict'
                  ? 'Someone else changed this job. Go back and open it again.'
                  : unshown.isNotEmpty
                      ? unshown.join(' ')
                      : (error.fix.isNotEmpty ? error.fix : error.causeText),
            ),
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
                if (!_previewTouched) _previewFirst = _type == 'mirror';
                // An encrypted archive is an Archive's: others encrypt the folder.
                if (!_isEdit && _type != 'archive' && _encryptMode == 'archive') _encryptMode = 'folder';
              }),
            ),
          ),
          Padding(
            padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0),
            child: Text('${bt('backup.type.$_type.promise')} ${bt('backup.type.$_type.deleted')}', style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant)),
          ),
          if (_err('type') != null) _errorText(_err('type')),
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
                        onPressed: () => setState(() {
                          _sources = [..._sources]..removeAt(i);
                          _syncHooks();
                        }),
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
          ..._appsGroup(),
          _skipGroup(),
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
          if (_dest != null) _encryptionGroup(errStyle),
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
          _whenGroup(),
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
          _safetyGroup(),
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
          _advancedGroup(),
          if (_isEdit && widget.job!.type != _type)
            Notice(status: Status.warning, message: 'Changing the job type changes what happens at the destination: ${bt('backup.type.$_type.deleted')}'),
        ],
      ),
    );
  }
}
