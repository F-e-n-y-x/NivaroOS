// Backup & Sync as the REST API sends it (docs/specs/backup-api.json, the
// frozen contract; Go types in services/backup/jobs/apitypes.go and
// model.go, engine types in services/backup/engine/api.go). Parsing is
// lenient: a missing field reads as empty, never as a crash, because the
// module is versioned separately from the app.

import '../utils/format.dart';
import 'backup_strings.dart';

Map<String, Object?> _map(Object? v) => v is Map ? v.map((k, v) => MapEntry(k.toString(), v)) : const {};
List<Object?> _list(Object? v) => v is List ? v : const [];
String _str(Object? v) => v == null ? '' : v.toString();
int _int(Object? v) => v is num ? v.round() : int.tryParse(_str(v)) ?? 0;
double _double(Object? v) => v is num ? v.toDouble() : double.tryParse(_str(v)) ?? 0;
bool _bool(Object? v) => v == true;
DateTime? _time(Object? v) => v == null ? null : DateTime.tryParse(v.toString());

/// A {key, args} message the server sends instead of English.
class BackupMessage {
  const BackupMessage(this.key, [this.args = const {}]);

  final String key;
  final Map<String, Object?> args;

  static BackupMessage? fromJson(Object? j) {
    final m = _map(j);
    final key = _str(m['key']);
    return key.isEmpty ? null : BackupMessage(key, _map(m['args']));
  }

  String get text => renderMessage(key, args);

  /// The error code a summary names ({reason_key: "backup.err.<code>.title"});
  /// a job's last run carries the summary but no error code of its own.
  String get errorCode {
    final k = args['reason_key'];
    final m = k is String ? RegExp(r'^backup\.err\.([a-z0-9_]+)\.title$').firstMatch(k) : null;
    return m?.group(1) ?? '';
  }
}

/// Where a job reads or writes: a drive, a USB stick, merged storage, a
/// network share or a cloud account, plus a folder inside it.
class BackupEndpoint {
  const BackupEndpoint({required this.kind, required this.refId, this.subPath = '', this.label = '', this.preset = '', this.match, this.encryption});

  /// volume | usb | merge | smb | cloud
  final String kind;
  final String refId;
  final String subPath;
  final String label;
  final String preset;

  /// usb only: {serial, size_bytes}, sent back as it came.
  final Map<String, Object?>? match;

  /// A job's destination only: {mode: folder | archive, ...} when its
  /// backups are encrypted. Shown, and sent back as it came (the app
  /// can't set it up or change it; the web wizard does).
  final Map<String, Object?>? encryption;

  /// "folder", "archive" or "" (not encrypted).
  String get encryptionMode => encryption == null ? '' : _str(encryption!['mode']);

  factory BackupEndpoint.fromJson(Object? j) {
    final m = _map(j);
    return BackupEndpoint(
      kind: _str(m['kind']),
      refId: _str(m['ref_id']),
      subPath: _str(m['sub_path']),
      label: _str(m['label']),
      preset: _str(m['preset']),
      match: m['match'] is Map ? _map(m['match']) : null,
      encryption: m['encryption'] is Map ? _map(m['encryption']) : null,
    );
  }

  Map<String, Object?> toJson() => {
        'kind': kind,
        'ref_id': refId,
        if (match != null) 'match': match,
        'sub_path': subPath,
        'label': label,
        if (preset.isNotEmpty) 'preset': preset,
        if (encryption != null) 'encryption': encryption,
      };

  BackupEndpoint withSubPath(String path) => BackupEndpoint(kind: kind, refId: refId, subPath: path, label: label, preset: '', match: match, encryption: encryption);

  /// "tower › photos", as the web writes it.
  String get display {
    final base = label.isNotEmpty ? label : (btHas('backup.ep.$kind') ? bt('backup.ep.$kind') : refId);
    return subPath.isEmpty ? base : '$base › $subPath';
  }

  /// The folder's own name ("photos"), else the location's.
  String get shortName {
    final parts = subPath.split('/').where((p) => p.isNotEmpty).toList();
    return parts.isNotEmpty ? parts.last : (label.isNotEmpty ? label : refId);
  }

  bool sameLocation(BackupEndpoint o) => kind == o.kind && refId == o.refId;
}

/// A job's last or active run, as lists show it.
class RunBrief {
  const RunBrief({required this.id, required this.kind, required this.status, this.endedAt, this.summary});

  final String id;
  final String kind;
  final String status;
  final DateTime? endedAt;
  final BackupMessage? summary;

  static RunBrief? fromJson(Object? j) {
    if (j is! Map) return null;
    final m = _map(j);
    return RunBrief(id: _str(m['id']), kind: _str(m['kind']), status: _str(m['status']), endedAt: _time(m['ended_at']), summary: BackupMessage.fromJson(m['summary']));
  }
}

/// A schedule, a drive plugged in, or (none) by hand only.
class BackupTrigger {
  const BackupTrigger({required this.kind, this.cron = '', this.minGapHours = 0, this.catchUp = false});

  /// schedule | volume_mounted | manual
  final String kind;
  final String cron;
  final int minGapHours;
  final bool catchUp;

  factory BackupTrigger.fromJson(Object? j) {
    final m = _map(j);
    return BackupTrigger(kind: _str(m['kind']), cron: _str(m['cron']), minGapHours: _int(m['min_gap_hours']), catchUp: _bool(m['catch_up']));
  }
}

/// One job: GET /jobs (JobListItem) or GET /jobs/:id (JobDetail, with
/// [stats]). [raw] is the job as it came, so an edit sends back every
/// field the app doesn't show (hooks, filters, retry, notify) unchanged.
class BackupJob {
  BackupJob({
    required this.id,
    required this.name,
    required this.type,
    required this.enabled,
    required this.revision,
    required this.sources,
    required this.dest,
    required this.triggers,
    required this.health,
    required this.raw,
    this.needsAttention = '',
    this.migratedFrom,
    this.lastRun,
    this.activeRun,
    this.nextRun,
    this.destOnline = true,
    this.versionsDays = 0,
    this.keepLast = 0,
    this.deletePct = 0,
    this.changePct = 0,
    this.previewFirst = false,
    this.windowStart = '',
    this.windowEnd = '',
    this.whenUnmet = '',
    this.sizeBytes,
    this.versionsCount,
    this.lastSuccess,
  });

  final String id;
  final String name;

  /// copy | mirror | archive
  final String type;
  final bool enabled;
  final int revision;
  final List<BackupEndpoint> sources;
  final BackupEndpoint dest;
  final List<BackupTrigger> triggers;

  /// problem | offline | warning | ok | disabled
  final String health;
  final String needsAttention;
  final String? migratedFrom;
  final RunBrief? lastRun;

  /// A queued, running or waiting run.
  final RunBrief? activeRun;
  final DateTime? nextRun;
  final bool destOnline;
  final int versionsDays;
  final int keepLast;
  final int deletePct;
  final int changePct;
  final bool previewFirst;
  final String windowStart;
  final String windowEnd;
  final String whenUnmet;

  /// GET /jobs/:id only.
  final int? sizeBytes;
  final int? versionsCount;
  final DateTime? lastSuccess;

  final Map<String, Object?> raw;

  bool get isActive => activeRun != null && const ['queued', 'running', 'waiting_user'].contains(activeRun!.status);
  bool get isImported => migratedFrom != null && needsAttention == 'imported';

  factory BackupJob.fromJson(Object? j) {
    final m = _map(j);
    final retention = _map(m['retention']);
    final guards = _map(m['guards']);
    final options = _map(m['options']);
    final conditions = _map(m['conditions']);
    final window = _map(conditions['window']);
    final stats = m['stats'] is Map ? _map(m['stats']) : null;
    return BackupJob(
      id: _str(m['id']),
      name: _str(m['name']),
      type: _str(m['type']),
      enabled: _bool(m['enabled']),
      revision: _int(m['revision']),
      sources: _list(m['sources']).map(BackupEndpoint.fromJson).toList(),
      dest: BackupEndpoint.fromJson(m['dest']),
      triggers: _list(m['triggers']).map(BackupTrigger.fromJson).toList(),
      health: _str(m['health']),
      needsAttention: _str(m['needs_attention']),
      migratedFrom: m['migrated_from'] == null ? null : _str(m['migrated_from']),
      lastRun: RunBrief.fromJson(m['last_run']),
      activeRun: RunBrief.fromJson(m['active_run']),
      nextRun: _time(m['next_run']),
      destOnline: m['dest_online'] != false,
      versionsDays: _int(retention['versions_days']),
      keepLast: _int(retention['keep_last']),
      deletePct: _int(guards['delete_pct']),
      changePct: _int(guards['change_pct']),
      previewFirst: _bool(options['preview_first']),
      windowStart: _str(window['start']),
      windowEnd: _str(window['end']),
      whenUnmet: _str(conditions['when_unmet']),
      sizeBytes: stats == null ? null : _int(stats['size_bytes']),
      versionsCount: stats == null ? null : _int(stats['versions_count']),
      lastSuccess: stats == null ? null : _time(stats['last_success']),
      raw: m,
    );
  }

  /// A copy with [enabled] changed, for an optimistic toggle.
  BackupJob copyWith({bool? enabled}) => BackupJob.fromJson({...raw, 'enabled': ?enabled});
}

/// A run's file and byte counts.
class RunCounts {
  const RunCounts({this.added = 0, this.changed = 0, this.deleted = 0, this.skipped = 0, this.errored = 0, this.bytesTransferred = 0, this.bytesTotal = 0});

  final int added, changed, deleted, skipped, errored, bytesTransferred, bytesTotal;

  factory RunCounts.fromJson(Object? j) {
    final m = _map(j);
    return RunCounts(
      added: _int(m['added']),
      changed: _int(m['changed']),
      deleted: _int(m['deleted']),
      skipped: _int(m['skipped']),
      errored: _int(m['errored']),
      bytesTransferred: _int(m['bytes_transferred']),
      bytesTotal: _int(m['bytes_total']),
    );
  }
}

/// One line of a run's step list ("Check the destination (Sandisk 128G)").
class RunStep {
  const RunStep({required this.phase, required this.message, required this.state});

  final String phase;
  final BackupMessage? message;

  /// pending | active | done | failed | skipped
  final String state;

  factory RunStep.fromJson(Object? j) {
    final m = _map(j);
    return RunStep(phase: _str(m['phase']), message: BackupMessage.fromJson(m), state: _str(m['state']));
  }
}

/// Why a run stopped to ask: it would delete or change too much, or the
/// source looks empty.
class GuardInfo {
  const GuardInfo({required this.guard, this.pct = 0, this.limit = 0, this.count = 0, this.total = 0, this.sample = const []});

  /// delete | change | empty_source
  final String guard;
  final double pct;
  final int limit;
  final int count;
  final int total;
  final List<String> sample;

  static GuardInfo? fromJson(Object? j) {
    if (j is! Map) return null;
    final m = _map(j);
    return GuardInfo(
      guard: _str(m['guard']),
      pct: _double(m['pct']),
      limit: _int(m['limit']),
      count: _int(m['count']),
      total: _int(m['total']),
      sample: _list(m['sample']).map(_str).toList(),
    );
  }
}

/// Progress while a run is running (GET /runs/:id `live`).
class LiveStats {
  const LiveStats({this.bytes = 0, this.totalBytes = 0, this.files = 0, this.totalFiles = 0, this.speedBps = 0, this.etaSec, this.errors = 0, this.currentFile = ''});

  final int bytes, totalBytes, files, totalFiles, speedBps, errors;
  final int? etaSec;
  final String currentFile;

  static LiveStats? fromJson(Object? j) {
    if (j is! Map) return null;
    final m = _map(j);
    final eta = m['eta_sec'];
    return LiveStats(
      bytes: _int(m['bytes']),
      totalBytes: _int(m['total_bytes']),
      files: _int(m['files']),
      totalFiles: _int(m['total_files']),
      speedBps: _int(m['speed_bps']),
      etaSec: eta is num && eta >= 0 ? eta.round() : null,
      errors: _int(m['errors']),
      currentFile: _str(m['current_file']),
    );
  }

  /// 0..1, or null while the total is unknown (state.js runProgress).
  double? get ratio {
    if (totalBytes > 0) return (bytes / totalBytes).clamp(0.0, 1.0);
    if (totalFiles > 0) return (files / totalFiles).clamp(0.0, 1.0);
    return null;
  }
}

/// A file that failed in a partial run.
class FileError {
  const FileError({required this.path, required this.code});
  final String path;
  final String code;
}

/// What a restore run restores where.
class RestoreInfo {
  const RestoreInfo({required this.versionId, required this.paths, required this.target, required this.conflict, required this.dryRun});

  final String versionId;
  final List<String> paths;
  final BackupEndpoint target;
  final String conflict;
  final bool dryRun;

  static RestoreInfo? fromJson(Object? j) {
    if (j is! Map) return null;
    final m = _map(j);
    return RestoreInfo(
      versionId: _str(m['version_id']),
      paths: _list(m['paths']).map(_str).toList(),
      target: BackupEndpoint.fromJson(m['target']),
      conflict: _str(m['conflict']),
      dryRun: _bool(m['dry_run']),
    );
  }
}

/// One run: GET /runs (a list item) or GET /runs/:id (with [live]).
class BackupRun {
  const BackupRun({
    required this.id,
    required this.jobId,
    required this.jobName,
    required this.kind,
    required this.trigger,
    required this.status,
    this.phase = '',
    this.attempt = 1,
    this.errorCode = '',
    this.summary,
    this.queuedAt,
    this.startedAt,
    this.endedAt,
    this.counts = const RunCounts(),
    this.guard,
    this.steps = const [],
    this.restore,
    this.fileErrors = const [],
    this.hasLog = false,
    this.live,
  });

  final String id;
  final String jobId;
  final String jobName;

  /// backup | preview | restore | verify | prune
  final String kind;

  /// schedule | volume_mounted | catch_up | manual | retry
  final String trigger;

  /// queued | running | waiting_user | success | partial | failed |
  /// cancelled | skipped | interrupted
  final String status;
  final String phase;
  final int attempt;
  final String errorCode;
  final BackupMessage? summary;
  final DateTime? queuedAt;
  final DateTime? startedAt;
  final DateTime? endedAt;
  final RunCounts counts;
  final GuardInfo? guard;
  final List<RunStep> steps;
  final RestoreInfo? restore;
  final List<FileError> fileErrors;
  final bool hasLog;
  final LiveStats? live;

  static const finalStatuses = ['success', 'partial', 'failed', 'cancelled', 'skipped', 'interrupted'];

  bool get isFinal => finalStatuses.contains(status);

  /// How long it took; null until it has started and ended.
  Duration? get duration {
    final s = startedAt, e = endedAt;
    if (s == null || e == null) return null;
    final d = e.difference(s);
    return d.isNegative ? null : d;
  }

  /// When it happened, for lists: ended, else started, else queued.
  DateTime? get at => endedAt ?? startedAt ?? queuedAt;

  factory BackupRun.fromJson(Object? j) {
    final m = _map(j);
    return BackupRun(
      id: _str(m['id']),
      jobId: _str(m['job_id']),
      jobName: _str(m['job_name']),
      kind: _str(m['kind']),
      trigger: _str(m['trigger']),
      status: _str(m['status']),
      phase: _str(m['phase']),
      attempt: _int(m['attempt']),
      errorCode: _str(m['error_code']),
      summary: BackupMessage.fromJson(m['summary']),
      queuedAt: _time(m['queued_at']),
      startedAt: _time(m['started_at']),
      endedAt: _time(m['ended_at']),
      counts: RunCounts.fromJson(m['counts']),
      guard: GuardInfo.fromJson(m['guard']),
      steps: _list(m['steps']).map(RunStep.fromJson).toList(),
      restore: RestoreInfo.fromJson(m['restore']),
      fileErrors: [
        for (final e in _list(m['file_errors']))
          FileError(path: _str(_map(e)['path']), code: _str(_map(e)['code'])),
      ],
      hasLog: _bool(m['has_log']),
      live: LiveStats.fromJson(m['live']),
    );
  }
}

/// GET /runs: newest first; [nextBefore] pages on ('' = no more).
class RunList {
  const RunList(this.runs, this.nextBefore);
  final List<BackupRun> runs;
  final String nextBefore;

  factory RunList.fromJson(Object? j) {
    final m = _map(j);
    return RunList(_list(m['runs']).map(BackupRun.fromJson).toList(), _str(m['next_before']));
  }
}

/// One line of a run log. [message] is the i18n line, [raw] rclone's own.
class LogLine {
  const LogLine({this.time, required this.level, required this.code, this.message, this.raw = ''});

  final DateTime? time;

  /// info | warn | error
  final String level;
  final String code;
  final BackupMessage? message;
  final String raw;

  factory LogLine.fromJson(Object? j) {
    final m = _map(j);
    final key = _str(m['msg_key']);
    return LogLine(
      time: _time(m['t']),
      level: _str(m['lvl']),
      code: _str(m['code']),
      message: key.isEmpty ? null : BackupMessage(key, _map(m['args'])),
      raw: _str(m['raw']),
    );
  }

  /// What the line says: its message, else the raw rclone line.
  String get text {
    final t = message?.text ?? '';
    return t.isNotEmpty ? t : raw;
  }
}

/// GET /runs/:id/log?after=: [nextOffset] is where the next page starts;
/// [done] once the run is final and the end was read.
class LogPage {
  const LogPage(this.lines, this.nextOffset, this.done);
  final List<LogLine> lines;
  final int nextOffset;
  final bool done;

  factory LogPage.fromJson(Object? j) {
    final m = _map(j);
    return LogPage(_list(m['lines']).map(LogLine.fromJson).toList(), _int(m['next_offset']), _bool(m['done']));
  }
}

/// One file a planned run would add, update or delete.
class PreviewItem {
  const PreviewItem({required this.op, required this.path, this.size = 0});
  final String op;
  final String path;
  final int size;
}

/// GET /runs/:id/preview: the plan's counts and one page of it.
class PreviewPage {
  const PreviewPage({this.add = 0, this.update = 0, this.delete = 0, this.bytesAdd = 0, this.items = const [], this.total = 0, this.nextOffset = -1});

  final int add, update, delete, bytesAdd;
  final List<PreviewItem> items;
  final int total;

  /// -1 (or 0 with no items) at the end.
  final int nextOffset;

  factory PreviewPage.fromJson(Object? j) {
    final m = _map(j);
    final c = _map(m['counts']);
    return PreviewPage(
      add: _int(c['add']),
      update: _int(c['update']),
      delete: _int(c['delete']),
      bytesAdd: _int(c['bytes_add']),
      items: [
        for (final e in _list(m['items']))
          PreviewItem(op: _str(_map(e)['op']), path: _str(_map(e)['path']), size: _int(_map(e)['size'])),
      ],
      total: _int(m['total']),
      nextOffset: m['next_offset'] is num ? _int(m['next_offset']) : -1,
    );
  }
}

/// One restorable point of a job: the current copy, a recycle folder
/// (mirror) or an archive.
class BackupVersion {
  const BackupVersion({required this.id, required this.kind, this.time, this.labelKey = '', this.files = 0, this.bytes = 0});

  /// "current", `v_<ts>` or `a_<file>`.
  final String id;

  /// current | recycle | archive
  final String kind;
  final DateTime? time;
  final String labelKey;
  final int files;
  final int bytes;

  factory BackupVersion.fromJson(Object? j) {
    final m = _map(j);
    return BackupVersion(id: _str(m['id']), kind: _str(m['kind']), time: _time(m['time']), labelKey: _str(m['label_key']), files: _int(m['files']), bytes: _int(m['bytes']));
  }

  /// "Current copy (as of Today 03:12)", "Deleted or replaced on Wed, Sep 23 …".
  String label({DateTime? currentAt}) {
    if (kind == 'current') {
      return currentAt != null ? bt('backup.ver.current_as_of', {'at': formatWhen(currentAt)}) : bt(labelKey.isEmpty ? 'backup.ver.current' : labelKey);
    }
    final t = time;
    return bt(labelKey.isEmpty ? 'backup.ver.$kind' : labelKey, {'at': t == null ? '' : formatDateTime(t)});
  }

  /// "14 files · 76.3 MB".
  String get meta => [
        if (files > 0) btc('backup.ver.files', files),
        if (bytes > 0) formatSize(bytes),
      ].join(' · ');
}

/// A folder listing: a location's (for pickers) or a version's.
class BrowseEntry {
  const BrowseEntry({required this.name, required this.dir, this.size = 0, this.mtime});
  final String name;
  final bool dir;
  final int size;
  final DateTime? mtime;
}

class BrowseResult {
  const BrowseResult({required this.path, required this.entries, this.truncated = false});
  final String path;
  final List<BrowseEntry> entries;
  final bool truncated;

  factory BrowseResult.fromJson(Object? j) {
    final m = _map(j);
    return BrowseResult(
      path: _str(m['path']),
      entries: [
        for (final e in _list(m['entries']))
          BrowseEntry(name: _str(_map(e)['name']), dir: _bool(_map(e)['dir']), size: _int(_map(e)['size']), mtime: _time(_map(e)['mtime'])),
      ],
      truncated: _bool(m['truncated']),
    );
  }
}

/// A folder preset of a location ("Documents", an app's data).
class FolderPreset {
  const FolderPreset({required this.id, required this.subPath, required this.label});
  final String id;
  final String subPath;
  final String label;
}

/// One entry of GET /locations: somewhere a job can read or write.
class BackupLocation {
  const BackupLocation({
    required this.kind,
    required this.refId,
    required this.label,
    this.match,
    this.provider = '',
    this.mountPoint = '',
    this.online = true,
    this.free,
    this.total,
    this.removable = false,
    this.systemDisk = false,
    this.lastSeen,
    this.presets = const [],
  });

  final String kind;
  final String refId;
  final String label;
  final Map<String, Object?>? match;
  final String provider;
  final String mountPoint;
  final bool online;
  final int? free;
  final int? total;
  final bool removable;
  final bool systemDisk;
  final DateTime? lastSeen;
  final List<FolderPreset> presets;

  factory BackupLocation.fromJson(Object? j) {
    final m = _map(j);
    return BackupLocation(
      kind: _str(m['kind']),
      refId: _str(m['ref_id']),
      label: _str(m['label']),
      match: m['match'] is Map ? _map(m['match']) : null,
      provider: _str(m['provider']),
      mountPoint: _str(m['mount_point']),
      online: m['online'] != false,
      free: m['free'] is num ? _int(m['free']) : null,
      total: m['total'] is num ? _int(m['total']) : null,
      removable: _bool(m['removable']),
      systemDisk: _bool(m['system_disk']),
      lastSeen: _time(m['last_seen']),
      presets: [
        for (final p in _list(m['presets']))
          FolderPreset(id: _str(_map(p)['id']), subPath: _str(_map(p)['sub_path']), label: _str(_map(p)['label'])),
      ],
    );
  }

  /// This location as an endpoint, at [subPath].
  BackupEndpoint endpoint([String subPath = '']) => BackupEndpoint(kind: kind, refId: refId, subPath: subPath, label: label, match: match);
}

/// POST /cron/preview: the schedule in words and its next runs.
class CronPreview {
  const CronPreview({required this.valid, this.message, this.next = const [], this.error = ''});
  final bool valid;
  final BackupMessage? message;
  final List<DateTime> next;
  final String error;

  factory CronPreview.fromJson(Object? j) {
    final m = _map(j);
    final key = _str(m['human_key']);
    return CronPreview(
      valid: _bool(m['valid']),
      message: key.isEmpty ? null : BackupMessage(key, _map(m['args'])),
      next: [for (final t in _list(m['next'])) ?_time(t)],
      error: _str(m['error']),
    );
  }
}

/// The module's install probe (GET /health, no auth, no envelope).
class BackupHealth {
  const BackupHealth({required this.installed, required this.running, this.version = ''});
  final bool installed;
  final bool running;
  final String version;

  factory BackupHealth.fromJson(Object? j) {
    final m = _map(j);
    return BackupHealth(installed: _bool(m['installed']), running: _bool(m['running']), version: _str(m['version']));
  }
}
