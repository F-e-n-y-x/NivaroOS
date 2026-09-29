// What the Backup & Sync screens say about a set of jobs: the overview's
// verdict, which jobs need the owner and why, what's running and what's
// coming up, and a job's schedule in words. Pure: no I/O, no widgets.
// It follows the web app's state.js (ui/src/apps/backup/state.js) so both
// say the same thing about the same jobs.

import 'backup_models.dart';
import 'backup_strings.dart';

/// Worst first (jobs/apitypes.go Health*).
const healthOrder = ['problem', 'offline', 'warning', 'ok', 'disabled'];

const activeStatuses = ['queued', 'running', 'waiting_user'];

bool isActiveStatus(String s) => activeStatuses.contains(s);

/// How bad an attention item is.
enum BackupSeverity { problem, warning }

/// Why a job needs the owner (state.js attentionFor).
class BackupAttention {
  const BackupAttention({required this.job, required this.reason, required this.severity, this.code = '', this.runId = ''});

  final BackupJob job;

  /// waiting | error | migrated_unresolved | partial | stale
  final String reason;
  final BackupSeverity severity;

  /// For [reason] error: the error code that explains it.
  final String code;

  /// The run to open (the waiting one, or the last).
  final String runId;

  /// One line: the reason's title, or the error's.
  String get title => reason == 'error'
      ? bt('backup.err.${btHas('backup.err.$code.title') ? code : 'internal'}.title')
      : bt('backup.attention.$reason.title');

  /// What happened, in a sentence.
  String get cause => reason == 'error'
      ? bt('backup.err.${btHas('backup.err.$code.cause') ? code : 'internal'}.cause')
      : bt('backup.attention.$reason.cause');
}

BackupAttention? attentionFor(BackupJob job) {
  final active = job.activeRun;
  final last = job.lastRun;
  if (active != null && active.status == 'waiting_user') {
    return BackupAttention(job: job, reason: 'waiting', severity: BackupSeverity.problem, runId: active.id);
  }
  if (job.needsAttention == 'dest_changed') {
    return BackupAttention(job: job, reason: 'error', code: 'dest_marker_mismatch', severity: BackupSeverity.problem, runId: last?.id ?? '');
  }
  if (job.needsAttention == 'migrated_unresolved') {
    return BackupAttention(job: job, reason: 'migrated_unresolved', severity: BackupSeverity.warning);
  }
  switch (job.health) {
    case 'problem':
      final code = last?.summary?.errorCode ?? '';
      return BackupAttention(job: job, reason: 'error', code: code.isEmpty ? 'internal' : code, severity: BackupSeverity.problem, runId: last?.id ?? '');
    case 'offline':
      return BackupAttention(job: job, reason: 'error', code: 'dest_offline', severity: BackupSeverity.warning, runId: last?.id ?? '');
    case 'warning':
      if (last != null && last.status == 'partial') return BackupAttention(job: job, reason: 'partial', severity: BackupSeverity.warning, runId: last.id);
      return BackupAttention(job: job, reason: 'stale', severity: BackupSeverity.warning, runId: last?.id ?? '');
  }
  return null;
}

/// The jobs that need the owner, a waiting decision first, then problems.
List<BackupAttention> attentionItems(List<BackupJob> jobs) {
  final out = [for (final j in jobs) ?attentionFor(j)];
  int rank(BackupAttention i) => i.reason == 'waiting' ? 0 : (i.severity == BackupSeverity.problem ? 1 : 2);
  out.sort((a, b) {
    final r = rank(a) - rank(b);
    return r != 0 ? r : a.job.name.toLowerCase().compareTo(b.job.name.toLowerCase());
  });
  return out;
}

/// The overview's verdict: empty | problem | attention | ok.
class BackupOverall {
  const BackupOverall({required this.state, required this.jobs, required this.attention, this.lastSuccess});
  final String state;
  final int jobs;
  final int attention;
  final DateTime? lastSuccess;

  String get title => bt('backup.overview.state.$state');
}

BackupOverall overallState(List<BackupJob> jobs) {
  if (jobs.isEmpty) return const BackupOverall(state: 'empty', jobs: 0, attention: 0);
  final items = attentionItems(jobs);
  DateTime? last;
  for (final j in jobs) {
    final r = j.lastRun;
    final at = r?.endedAt;
    if (r != null && at != null && (r.status == 'success' || r.status == 'partial') && r.kind != 'restore') {
      if (last == null || at.isAfter(last)) last = at;
    }
  }
  final problem = items.any((i) => i.severity == BackupSeverity.problem);
  return BackupOverall(state: problem ? 'problem' : (items.isNotEmpty ? 'attention' : 'ok'), jobs: jobs.length, attention: items.length, lastSuccess: last);
}

/// Jobs with a run going, running ones first. A run waiting for a
/// decision is under "Needs attention" instead.
List<BackupJob> runningJobs(List<BackupJob> jobs) {
  const rank = {'running': 0, 'queued': 1};
  final out = jobs.where((j) => j.isActive && j.activeRun!.status != 'waiting_user').toList();
  out.sort((a, b) => (rank[a.activeRun!.status] ?? 3) - (rank[b.activeRun!.status] ?? 3));
  return out;
}

/// Jobs in the order the list shows them: worst health first, then by
/// the next run, then by name.
List<BackupJob> sortedJobs(List<BackupJob> jobs) {
  final out = jobs.toList();
  int h(BackupJob j) {
    final i = healthOrder.indexOf(j.health);
    return i < 0 ? healthOrder.length : i;
  }

  out.sort((a, b) {
    final r = h(a) - h(b);
    if (r != 0) return r;
    final an = a.nextRun, bn = b.nextRun;
    if (an != null && bn != null && an != bn) return an.compareTo(bn);
    if (an != null && bn == null) return -1;
    if (an == null && bn != null) return 1;
    return a.name.toLowerCase().compareTo(b.name.toLowerCase());
  });
  return out;
}

const _weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
const _weekdayShort = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

/// A cron expression in words: the server's HumanizeCron
/// (services/backup/jobs/cronhuman.go), ported so the phone needs no call
/// (and no admin role) to say when a job runs. What it can't describe is
/// "Custom schedule: `<expr>`", never a guess.
String cronText(String spec) {
  final s = spec.trim();
  String custom() => bt('backup.cron.custom', {'expr': spec});
  String two(int n) => n.toString().padLeft(2, '0');
  switch (s.toLowerCase()) {
    case '@hourly':
      return bt('backup.cron.hourly_at', {'minute': '0'});
    case '@daily':
    case '@midnight':
      return bt('backup.cron.daily_at', {'time': '00:00'});
    case '@weekly':
      return bt('backup.cron.weekly_at', {'day': _weekdays[0], 'time': '00:00'});
    case '@monthly':
      return bt('backup.cron.monthly_at', {'dom': '1', 'time': '00:00'});
  }
  if (s.startsWith('@')) return custom();
  final f = s.split(RegExp(r'\s+'));
  if (f.length != 5) return custom();
  final [min, hour, dom, mon, dow] = f;
  if (mon != '*') return custom();
  int? number(String v, int lo, int hi) {
    final n = int.tryParse(v);
    return n == null || n < lo || n > hi ? null : n;
  }

  int? step(String v, int max) {
    final parts = v.split('/');
    if (parts.length != 2 || (parts[0] != '*' && parts[0] != '0')) return null;
    final n = int.tryParse(parts[1]);
    return n == null || n < 2 || n > max ? null : n;
  }

  final m = number(min, 0, 59);
  final h = number(hour, 0, 23);
  String clock() => '${two(h!)}:${two(m!)}';
  if (min == '*' && hour == '*' && dom == '*' && dow == '*') return bt('backup.cron.every_minute');
  if (hour == '*' && dom == '*' && dow == '*') {
    final n = step(min, 59);
    if (n != null) return bt('backup.cron.every_n_minutes', {'n': '$n'});
    if (m != null) return bt('backup.cron.hourly_at', {'minute': '$m'});
    return custom();
  }
  if (m != null && dom == '*' && dow == '*') {
    final n = step(hour, 23);
    if (n != null) return bt('backup.cron.every_n_hours', {'n': '$n', 'minute': '$m'});
    if (h != null) return bt('backup.cron.daily_at', {'time': clock()});
    return custom();
  }
  if (m != null && h != null && dom == '*') {
    final days = cronWeekdays(dow);
    if (days == null) return custom();
    if (days.length == 1) return bt('backup.cron.weekly_at', {'day': _weekdays[days.first], 'time': clock()});
    return bt('backup.cron.weekdays_at', {'days': formatList([for (final d in days) _weekdayShort[d]]), 'time': clock()});
  }
  if (m != null && h != null && dow == '*') {
    final d = number(dom, 1, 31);
    if (d != null) return bt('backup.cron.monthly_at', {'dom': '$d', 'time': clock()});
  }
  return custom();
}

/// A day-of-week field ("1-5", "MON,WED", "0,6", "7") as sorted weekday
/// numbers, 0 = Sunday; null when it isn't a plain list or range.
List<int>? cronWeekdays(String field) {
  const names = {'sun': 0, 'mon': 1, 'tue': 2, 'wed': 3, 'thu': 4, 'fri': 5, 'sat': 6};
  final seen = List.filled(7, false);
  int? one(String s) {
    final n = names[s.toLowerCase()] ?? int.tryParse(s);
    return n == null || n < 0 || n > 7 ? null : n % 7;
  }

  for (final part in field.split(',')) {
    if (part.contains('/') || part == '*' || part == '?') return null;
    final range = part.split('-');
    final a = one(range[0]);
    if (a == null || range.length > 2) return null;
    if (range.length == 1) {
      seen[a] = true;
      continue;
    }
    var b = one(range[1]);
    if (b == null) return null;
    if (range[1] == '7') b = 7;
    if (b < a) return null;
    for (var d = a; d <= b; d++) {
      seen[d % 7] = true;
    }
  }
  final out = [for (var d = 0; d < 7; d++) if (seen[d]) d];
  return out.isEmpty ? null : out;
}

/// "Mirror · Keeps deleted files 30 days · Every Sunday at 03:00 · When
/// the drive is plugged in" (the web's job card line).
String jobDescribeLine(BackupJob job) {
  final parts = <String>[bt('backup.type.${job.type}.label')];
  final keep = retentionText(job);
  if (keep != null) parts.add(keep);
  final triggers = job.triggers.where((t) => t.kind == 'schedule' || t.kind == 'volume_mounted').toList();
  for (final t in triggers) {
    parts.add(t.kind == 'volume_mounted' ? bt('backup.jobs.on_plug_in') : cronText(t.cron));
  }
  if (triggers.isEmpty) parts.add(bt('backup.jobs.manual_only'));
  return parts.join(' · ');
}

/// What a job keeps ("Keeps deleted files 30 days"); null for copy.
String? retentionText(BackupJob job) => switch (job.type) {
      'mirror' => job.versionsDays > 0 ? bt('backup.keep.recycle_days', {'days': '${job.versionsDays}'}) : bt('backup.keep.recycle_forever'),
      'archive' => bt('backup.keep.last_archives', {'keep': '${job.keepLast > 0 ? job.keepLast : 8}'}),
      _ => null,
    };

/// The web's plain sentences for a job (summaries.js jobSummary), the
/// part the app shows: what, when, what it keeps and its safety checks.
List<String> jobSummary(BackupJob job) {
  final out = <String>[];
  final sources = job.sources;
  final source = sources.isEmpty
      ? ''
      : sources.length == 1
          ? sources.first.display
          : bt('backup.summary.sources_more', {'first': sources.first.display, 'count': '${sources.length - 1}'});
  if (btHas('backup.summary.${job.type}')) out.add(bt('backup.summary.${job.type}', {'source': source, 'dest': job.dest.display}));
  final schedules = job.triggers.where((t) => t.kind == 'schedule' && t.cron.isNotEmpty).toList();
  final plugs = job.triggers.where((t) => t.kind == 'volume_mounted').toList();
  for (final s in schedules) {
    out.add(bt('backup.summary.when_schedule', {'schedule': _lowerFirst(cronText(s.cron))}));
  }
  for (final p in plugs) {
    final drive = job.dest.kind == 'usb' || job.dest.kind == 'volume' ? job.dest.label : (sources.isEmpty ? '' : sources.first.label);
    out.add(p.minGapHours > 0
        ? bt('backup.summary.when_plug_gap', {'drive': drive, 'hours': '${p.minGapHours}'})
        : bt('backup.summary.when_plug', {'drive': drive}));
  }
  if (schedules.isEmpty && plugs.isEmpty) out.add(bt('backup.summary.when_manual'));
  if (job.windowStart.isNotEmpty && job.windowEnd.isNotEmpty) out.add(bt('backup.summary.window', {'start': job.windowStart, 'end': job.windowEnd}));
  final keep = retentionText(job);
  if (keep != null) out.add('$keep.');
  if (job.type == 'copy') out.add('${bt('backup.keep.copy_never_deletes')}.');
  if (job.type == 'mirror') out.add(bt('backup.summary.guards', {'delete': '${job.deletePct}', 'change': '${job.changePct}'}));
  if (job.type == 'copy') out.add(bt('backup.summary.guard_change', {'change': '${job.changePct}'}));
  if (job.previewFirst && job.type != 'archive') out.add(bt('backup.summary.preview_first'));
  return out;
}

String _lowerFirst(String s) => s.isEmpty ? s : s[0].toLowerCase() + s.substring(1);

/// A run's status in words ("Succeeded", "Waiting for you").
String statusText(String status) => btHas('backup.status.$status') ? bt('backup.status.$status') : status;

/// A job's health in words ("Destination not connected").
String healthText(String health) => btHas('backup.health.$health') ? bt('backup.health.$health') : health;
