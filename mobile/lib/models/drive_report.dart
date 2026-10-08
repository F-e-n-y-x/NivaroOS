// One drive's health in plain words, from `GET /v1/disks/health` (the
// server's smart_health.go): the verdict and why, the numbers that predict
// a failure, a daily history, self-test state and the raw SMART table.

enum DriveVerdict { good, watch, failing, unknown }

DriveVerdict driveVerdictOf(Object? s) => switch (s) {
      'good' => DriveVerdict.good,
      'watch' => DriveVerdict.watch,
      'failing' => DriveVerdict.failing,
      _ => DriveVerdict.unknown,
    };

/// "Good", "Watch", "Failing", "Unknown".
String driveVerdictLabel(DriveVerdict v) => switch (v) {
      DriveVerdict.good => 'Good',
      DriveVerdict.watch => 'Watch',
      DriveVerdict.failing => 'Failing',
      DriveVerdict.unknown => 'Unknown',
    };

List<String> _strings(Object? l) => l is List ? [for (final e in l) e.toString()] : const [];
int _int(Object? v) => v is num ? v.toInt() : 0;

class DriveMetric {
  const DriveMetric({required this.key, required this.label, required this.value, this.unit = '', this.explain = '', this.level = 'good'});

  final String key;
  final String label;
  final int value;

  /// "h", "°C", "%" or ''.
  final String unit;
  final String explain;

  /// good | note (old, not increasing) | watch | failing
  final String level;

  /// "17,549" (the unit goes next to it).
  String get valueText => groupDigits(value);

  /// "2.0 years" for hours; null otherwise.
  String? get hint {
    if (unit != 'h') return null;
    final years = value / 8766;
    return years >= 1 ? '${years.toStringAsFixed(1)} years' : '${(value / 24).round()} days';
  }

  factory DriveMetric.fromJson(Map<String, dynamic> j) => DriveMetric(
        key: j['key']?.toString() ?? '',
        label: j['label']?.toString() ?? '',
        value: _int(j['value']),
        unit: j['unit']?.toString() ?? '',
        explain: j['explain']?.toString() ?? '',
        level: j['level']?.toString() ?? 'good',
      );
}

/// "17,549".
String groupDigits(int n) {
  final s = n.abs().toString();
  final b = StringBuffer(n < 0 ? '-' : '');
  for (var i = 0; i < s.length; i++) {
    if (i > 0 && (s.length - i) % 3 == 0) b.write(',');
    b.write(s[i]);
  }
  return b.toString();
}

class DriveSelfTestEntry {
  const DriveSelfTestEntry({required this.type, required this.result, required this.passed, required this.hours});

  final String type;
  final String result;
  final bool passed;
  final int hours;

  factory DriveSelfTestEntry.fromJson(Map<String, dynamic> j) =>
      DriveSelfTestEntry(type: j['type']?.toString() ?? '', result: j['result']?.toString() ?? '', passed: j['passed'] != false, hours: _int(j['hours']));
}

class DriveSelfTest {
  const DriveSelfTest({this.supported = false, this.running = false, this.remainingPercent = 0, this.shortMinutes = 0, this.longMinutes = 0, this.last = const []});

  final bool supported;
  final bool running;
  final int remainingPercent;
  final int shortMinutes;
  final int longMinutes;
  final List<DriveSelfTestEntry> last;

  /// "Running · 70% left", "Short offline: Completed without error", "Never run".
  String get status {
    if (!supported) return "This drive doesn't support self-tests";
    if (running) return 'Running · $remainingPercent% left';
    if (last.isEmpty) return 'Never run';
    return '${last.first.type}: ${last.first.result}';
  }

  factory DriveSelfTest.fromJson(Object? o) {
    if (o is! Map) return const DriveSelfTest();
    return DriveSelfTest(
      supported: o['supported'] == true,
      running: o['running'] == true,
      remainingPercent: _int(o['remaining_percent']),
      shortMinutes: _int(o['short_minutes']),
      longMinutes: _int(o['long_minutes']),
      last: [for (final e in (o['last'] as List? ?? const [])) if (e is Map) DriveSelfTestEntry.fromJson(Map<String, dynamic>.from(e))],
    );
  }
}

class DriveSnapshot {
  const DriveSnapshot({required this.date, required this.values});

  /// 2026-10-09
  final String date;
  final Map<String, int> values;

  factory DriveSnapshot.fromJson(Map<String, dynamic> j) {
    final v = j['values'];
    return DriveSnapshot(
      date: j['date']?.toString() ?? '',
      values: v is Map ? {for (final e in v.entries) e.key.toString(): _int(e.value)} : const {},
    );
  }
}

class DriveRawRow {
  const DriveRawRow({required this.name, required this.raw, this.id = 0, this.value = '', this.worst = '', this.thresh = '', this.failed = ''});

  final int id;
  final String name;
  final String value;
  final String worst;
  final String thresh;
  final String raw;
  final String failed;

  factory DriveRawRow.fromJson(Map<String, dynamic> j) => DriveRawRow(
        id: _int(j['id']),
        name: j['name']?.toString() ?? '',
        value: j['value']?.toString() ?? '',
        worst: j['worst']?.toString() ?? '',
        thresh: j['thresh']?.toString() ?? '',
        raw: j['raw']?.toString() ?? '',
        failed: j['failed']?.toString() ?? '',
      );
}

class DriveReport {
  const DriveReport({
    required this.path,
    this.status = '',
    this.verdict = DriveVerdict.unknown,
    this.summary = '',
    this.reasons = const [],
    this.notes = const [],
    this.changes = const [],
    this.kind = '',
    this.model = '',
    this.stale = false,
    this.metrics = const [],
    this.selfTest = const DriveSelfTest(),
    this.history = const [],
    this.raw = const [],
  });

  final String path;

  /// ok | asleep | tools_missing | unsupported | unreadable
  final String status;
  final DriveVerdict verdict;
  final String summary;
  final List<String> reasons;
  final List<String> notes;

  /// "Sectors waiting to be remapped went 0 → 12 this week".
  final List<String> changes;

  /// hdd | ssd | nvme
  final String kind;
  final String model;

  /// Asleep: this is the last reading taken while it was awake.
  final bool stale;
  final List<DriveMetric> metrics;
  final DriveSelfTest selfTest;

  /// Oldest first, one per day.
  final List<DriveSnapshot> history;
  final List<DriveRawRow> raw;

  /// Keys with a history worth drawing, damage counters first.
  List<String> get chartKeys {
    const order = ['pending', 'reallocated', 'offline_uncorrectable', 'reported_uncorrect', 'crc_errors', 'media_errors', 'wear', 'temperature'];
    // a counter that stayed at 0 has nothing to show
    final seen = {for (final s in history) for (final e in s.values.entries) if (e.value != 0 || e.key == 'temperature') e.key};
    return [for (final k in order) if (seen.contains(k)) k];
  }

  /// The counter to chart first: one that changed, else the first one.
  String get defaultChartKey {
    final keys = chartKeys;
    for (final k in keys) {
      if (k != 'temperature' && {for (final s in history) s.values[k]}.length > 1) return k;
    }
    return keys.isEmpty ? '' : keys.first;
  }

  factory DriveReport.fromJson(Map<String, dynamic> j) => DriveReport(
        path: j['path']?.toString() ?? '',
        status: j['status']?.toString() ?? '',
        verdict: driveVerdictOf(j['verdict']),
        summary: j['summary']?.toString() ?? '',
        reasons: _strings(j['reasons']),
        notes: _strings(j['notes']),
        changes: _strings(j['changes']),
        kind: j['kind']?.toString() ?? '',
        model: j['model']?.toString() ?? '',
        stale: j['stale'] == true,
        metrics: [for (final e in (j['metrics'] as List? ?? const [])) if (e is Map) DriveMetric.fromJson(Map<String, dynamic>.from(e))],
        selfTest: DriveSelfTest.fromJson(j['self_test']),
        history: [for (final e in (j['history'] as List? ?? const [])) if (e is Map) DriveSnapshot.fromJson(Map<String, dynamic>.from(e))],
        raw: [for (final e in (j['raw'] as List? ?? const [])) if (e is Map) DriveRawRow.fromJson(Map<String, dynamic>.from(e))],
      );
}
