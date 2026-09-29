/// Fan control (nivaroos-fans, GET /v1/fans/status): every fan the server
/// found, the temperatures it steers by, and the rules a change must keep.
/// The curve helpers mirror services/fans/curve.go so the app can never
/// send a curve the server would refuse.
library;

import 'dart:math' as math;

double _d(Object? v, [double fallback = 0]) => v is num ? v.toDouble() : fallback;
int _i(Object? v, [int fallback = 0]) => v is num ? v.round() : fallback;
int? _ni(Object? v) => v is num ? v.round() : null;
double? _nd(Object? v) => v is num ? v.toDouble() : null;
String _s(Object? v) => v is String ? v : '';

class FanPoint {
  const FanPoint(this.t, this.p);

  factory FanPoint.fromJson(Map<String, dynamic> j) => FanPoint(_d(j['t']), _i(j['p']));

  /// Temperature, °C.
  final double t;

  /// Speed, %.
  final int p;

  Map<String, dynamic> toJson() => {'t': t, 'p': p};

  @override
  bool operator ==(Object other) => other is FanPoint && other.t == t && other.p == p;

  @override
  int get hashCode => Object.hash(t, p);

  @override
  String toString() => '(${t.round()} °C, $p%)';
}

class FanLimits {
  const FanLimits({
    this.minPct = 20,
    this.minCriticalC = 60,
    this.maxCriticalCpuC = 95,
    this.maxCriticalGpuC = 90,
    this.minPoints = 2,
    this.maxPoints = 8,
    this.minTempC = 20,
    this.maxTempC = 100,
    this.maxHysteresisC = 10,
  });

  factory FanLimits.fromJson(Map<String, dynamic>? j) {
    if (j == null) return const FanLimits();
    return FanLimits(
      minPct: _i(j['min_pct'], 20),
      minCriticalC: _i(j['min_critical_c'], 60),
      maxCriticalCpuC: _i(j['max_critical_cpu_c'], 95),
      maxCriticalGpuC: _i(j['max_critical_gpu_c'], 90),
      minPoints: _i(j['min_points'], 2),
      maxPoints: _i(j['max_points'], 8),
      minTempC: _i(j['min_temp_c'], 20),
      maxTempC: _i(j['max_temp_c'], 100),
      maxHysteresisC: _i(j['max_hysteresis_c'], 10),
    );
  }

  final int minPct;
  final int minCriticalC;
  final int maxCriticalCpuC;
  final int maxCriticalGpuC;
  final int minPoints;
  final int maxPoints;
  final int minTempC;
  final int maxTempC;
  final int maxHysteresisC;
}

enum FanMode {
  auto('Auto'),
  fixed('Fixed'),
  curve('Curve');

  const FanMode(this.label);
  final String label;

  static FanMode parse(String s) => FanMode.values.firstWhere((m) => m.name == s, orElse: () => FanMode.auto);
}

/// What the fan is doing right now.
enum FanState {
  auto,
  manual,
  emergency,
  readonly,
  identifying;

  static FanState parse(String s) => FanState.values.firstWhere((m) => m.name == s, orElse: () => FanState.auto);
}

class FanInfo {
  const FanInfo({
    required this.id,
    required this.label,
    this.defaultLabel = '',
    this.kind = 'hwmon',
    this.fanClass = 'motherboard',
    this.chip = '',
    this.channel = '',
    this.rpm,
    this.pct,
    this.mode = FanMode.auto,
    this.state = FanState.auto,
    this.fixedPct = 50,
    this.curve = const [],
    this.source = 'cpu',
    this.minPct = 20,
    this.floorPct = 20,
    this.hysteresisC = 3,
    this.controllable = false,
    this.readonlyReason = '',
    this.hasTach = false,
    this.tach = '',
    this.canIdentify = false,
    this.alert = '',
  });

  factory FanInfo.fromJson(Map<String, dynamic> j) => FanInfo(
        id: _s(j['id']),
        label: _s(j['label']),
        defaultLabel: _s(j['default_label']),
        kind: _s(j['kind']),
        fanClass: _s(j['class']),
        chip: _s(j['chip']),
        channel: _s(j['channel']),
        rpm: _ni(j['rpm']),
        pct: _ni(j['pct']),
        mode: FanMode.parse(_s(j['mode'])),
        state: FanState.parse(_s(j['state'])),
        fixedPct: _i(j['fixed_pct'], 50),
        curve: [for (final p in (j['curve'] as List? ?? const [])) if (p is Map<String, dynamic>) FanPoint.fromJson(p)],
        source: _s(j['source']).isEmpty ? 'cpu' : _s(j['source']),
        minPct: _i(j['min_pct'], 20),
        floorPct: _i(j['floor_pct'], 20),
        hysteresisC: _d(j['hysteresis_c'], 3),
        controllable: j['controllable'] == true,
        readonlyReason: _s(j['readonly_reason']),
        hasTach: j['has_tach'] == true,
        tach: _s(j['tach']),
        canIdentify: j['can_identify'] == true,
        alert: _s(j['alert']),
      );

  final String id;
  final String label;
  final String defaultLabel;
  final String kind;

  /// motherboard | gpu | laptop | board
  final String fanClass;
  final String chip;
  final String channel;

  /// Null when the fan reports no speed (most GPUs through NVML).
  final int? rpm;
  final int? pct;
  final FanMode mode;
  final FanState state;
  final int fixedPct;
  final List<FanPoint> curve;
  final String source;
  final int minPct;
  final int floorPct;
  final double hysteresisC;
  final bool controllable;
  final String readonlyReason;
  final bool hasTach;
  final String tach;
  final bool canIdentify;
  final String alert;

  bool get isGpu => fanClass == 'gpu';

  /// "2,380 RPM · 100%", or what is known of it.
  String get reading {
    final parts = <String>[
      if (rpm != null && rpm! > 0) '${groupThousands(rpm!)} RPM',
      if (rpm == 0 && hasTach) 'Stopped',
      if (pct != null) '$pct%',
    ];
    return parts.isEmpty ? '—' : parts.join(' · ');
  }

  String get stateLabel => switch (state) {
        FanState.emergency => 'Emergency: full speed',
        FanState.manual => mode == FanMode.curve ? 'Curve' : 'Fixed',
        FanState.identifying => 'Identifying',
        FanState.readonly => 'Read-only',
        FanState.auto => 'Auto',
      };
}

String groupThousands(int v) {
  final s = v.abs().toString();
  final b = StringBuffer(v < 0 ? '-' : '');
  for (var i = 0; i < s.length; i++) {
    if (i > 0 && (s.length - i) % 3 == 0) b.write(',');
    b.write(s[i]);
  }
  return b.toString();
}

class FanTemp {
  const FanTemp({required this.id, required this.label, this.c, this.criticalC = 85, this.hot = false, this.detail = ''});

  factory FanTemp.fromJson(Map<String, dynamic> j) => FanTemp(
        id: _s(j['id']),
        label: _s(j['label']),
        c: _nd(j['c']),
        criticalC: _i(j['critical_c'], 85),
        hot: j['hot'] == true,
        detail: _s(j['detail']),
      );

  final String id;
  final String label;
  final double? c;
  final int criticalC;
  final bool hot;
  final String detail;

  bool get isGpu => id.startsWith('gpu:');
}

class FanSource {
  const FanSource(this.id, this.label);
  final String id;
  final String label;
}

class FanNote {
  const FanNote({required this.level, required this.code, required this.message, this.guidance = ''});

  factory FanNote.fromJson(Map<String, dynamic> j) => FanNote(level: _s(j['level']), code: _s(j['code']), message: _s(j['message']), guidance: _s(j['guidance']));

  final String level;
  final String code;
  final String message;
  final String guidance;
}

class FansStatus {
  const FansStatus({
    this.controllable = false,
    this.emergency = false,
    this.criticalCpuC = 85,
    this.criticalGpuC = 83,
    this.limits = const FanLimits(),
    this.preset = 'auto',
    this.fans = const [],
    this.temps = const [],
    this.sources = const [],
    this.notes = const [],
  });

  factory FansStatus.fromJson(Map<String, dynamic> j) => FansStatus(
        controllable: j['controllable'] == true,
        emergency: j['emergency'] == true,
        criticalCpuC: _i(j['critical_cpu_c'], 85),
        criticalGpuC: _i(j['critical_gpu_c'], 83),
        limits: FanLimits.fromJson(j['limits'] as Map<String, dynamic>?),
        preset: _s(j['preset']).isEmpty ? 'auto' : _s(j['preset']),
        fans: [for (final f in (j['fans'] as List? ?? const [])) if (f is Map<String, dynamic>) FanInfo.fromJson(f)],
        temps: [for (final t in (j['temps'] as List? ?? const [])) if (t is Map<String, dynamic>) FanTemp.fromJson(t)],
        sources: [
          for (final s in (j['sources'] as List? ?? const []))
            if (s is Map<String, dynamic>) FanSource(_s(s['id']), _s(s['label'])),
        ],
        notes: [for (final n in (j['notes'] as List? ?? const [])) if (n is Map<String, dynamic>) FanNote.fromJson(n)],
      );

  final bool controllable;
  final bool emergency;
  final int criticalCpuC;
  final int criticalGpuC;
  final FanLimits limits;
  final String preset;
  final List<FanInfo> fans;
  final List<FanTemp> temps;
  final List<FanSource> sources;
  final List<FanNote> notes;

  FanInfo? fan(String id) {
    for (final f in fans) {
      if (f.id == id) return f;
    }
    return null;
  }

  /// The temperature a fan follows ("max" = the hottest).
  double? sourceTemp(String source) {
    if (source == 'max') {
      final all = [for (final t in temps) if (t.c != null) t.c!];
      return all.isEmpty ? null : all.reduce(math.max);
    }
    for (final t in temps) {
      if (t.id == source) return t.c;
    }
    return null;
  }

  String sourceLabel(String source) {
    for (final s in sources) {
      if (s.id == source) return s.label;
    }
    return source == 'cpu' ? 'CPU' : source;
  }

  int criticalFor(String source) => source.startsWith('gpu:') ? criticalGpuC : criticalCpuC;
}

/// The presets the server offers, in order.
const fanPresets = <(String, String, String)>[
  ('auto', 'Auto', 'The BIOS and drivers control every fan'),
  ('quiet', 'Quiet', 'Slow and silent until it gets warm'),
  ('balanced', 'Balanced', 'A middle ground between noise and cooling'),
  ('performance', 'Performance', 'Cooler, and louder'),
];

/// Linear between points, flat outside them (the server's Curve.eval).
double evalCurve(List<FanPoint> c, double t) {
  if (c.isEmpty) return 100;
  if (t <= c.first.t) return c.first.p.toDouble();
  if (t >= c.last.t) return c.last.p.toDouble();
  for (var i = 1; i < c.length; i++) {
    final a = c[i - 1], b = c[i];
    if (t <= b.t) return a.p + (t - a.t) / (b.t - a.t) * (b.p - a.p);
  }
  return c.last.p.toDouble();
}

/// Null when the server would accept the curve, else why not.
String? validateCurve(List<FanPoint> c, int minPct, [FanLimits l = const FanLimits()]) {
  if (c.length < l.minPoints || c.length > l.maxPoints) return 'A curve needs ${l.minPoints} to ${l.maxPoints} points.';
  for (var i = 0; i < c.length; i++) {
    final p = c[i];
    if (p.t < l.minTempC || p.t > l.maxTempC) return 'Temperatures must be between ${l.minTempC} and ${l.maxTempC} °C.';
    if (p.p < minPct || p.p > 100) return 'Speeds must be between $minPct% and 100%.';
    if (i > 0 && p.t <= c[i - 1].t) return 'Temperatures must rise from point to point.';
    if (i > 0 && p.p < c[i - 1].p) return "A fan curve can't slow the fan down as it gets hotter.";
  }
  return null;
}

/// Moves point [i] as close to ([t], [p]) as the rules allow: 1 °C from its
/// neighbours, a speed between theirs, within the fan's limits. Whole
/// degrees and percent.
List<FanPoint> movePoint(List<FanPoint> c, int i, double t, double p, int minPct, [FanLimits l = const FanLimits()]) {
  final out = [...c];
  final prev = i > 0 ? c[i - 1] : null;
  final next = i < c.length - 1 ? c[i + 1] : null;
  final tLo = prev != null ? prev.t + 1 : l.minTempC.toDouble();
  final tHi = next != null ? next.t - 1 : l.maxTempC.toDouble();
  final pLo = prev != null ? math.max(prev.p, minPct) : minPct;
  final pHi = next?.p ?? 100;
  out[i] = FanPoint(t.roundToDouble().clamp(tLo, math.max(tLo, tHi)), p.round().clamp(pLo, math.max(pLo, pHi)));
  return out;
}
