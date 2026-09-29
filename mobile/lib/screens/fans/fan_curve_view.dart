/// A fan curve: speed (%) by temperature, drawn in the style's chart look
/// (line weight, flat fill, ink or accent line). Editable: drag a point;
/// every move is held to the server's rules by [movePoint], and the curve
/// is sent once the finger lifts.
library;

import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../../models/fans.dart';
import '../../ui/ui.dart';

class FanCurveView extends StatefulWidget {
  const FanCurveView({
    super.key,
    required this.points,
    required this.minPct,
    this.limits = const FanLimits(),
    this.currentTemp,
    this.criticalC,
    this.editable = false,
    this.onChanged,
    this.height = 200,
  });

  final List<FanPoint> points;
  final int minPct;
  final FanLimits limits;

  /// The temperature the fan follows now: a marker on the curve.
  final double? currentTemp;

  /// Where the emergency starts (shaded).
  final int? criticalC;
  final bool editable;
  final ValueChanged<List<FanPoint>>? onChanged;
  final double height;

  @override
  State<FanCurveView> createState() => _FanCurveViewState();
}

class _FanCurveViewState extends State<FanCurveView> {
  late List<FanPoint> _pts = widget.points;
  int? _drag;

  @override
  void didUpdateWidget(FanCurveView old) {
    super.didUpdateWidget(old);
    if (_drag == null) _pts = widget.points;
  }

  _Geometry _geo(Size size) => _Geometry(size, widget.limits.minTempC.toDouble(), widget.limits.maxTempC.toDouble());

  int? _hit(Offset pos, _Geometry g) {
    int? best;
    var bestD = 28.0; // touch slop, dp
    for (var i = 0; i < _pts.length; i++) {
      final d = (g.at(_pts[i]) - pos).distance;
      if (d < bestD) {
        best = i;
        bestD = d;
      }
    }
    return best;
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final tokens = DesignTokens.of(context);
    final scheme = theme.colorScheme;
    final label = tokens.chartLabel;
    final line = tokens.chart.inkLine ? scheme.onSurface : scheme.primary;
    final desc = _pts.map((p) => '${p.t.round()} °C ${p.p}%').join(', ');
    return Semantics(
      label: 'Fan curve: $desc',
      child: LayoutBuilder(
        builder: (context, box) {
          final size = Size(box.maxWidth, widget.height);
          final g = _geo(size);
          final paint = CustomPaint(
            size: size,
            painter: _CurvePainter(
              points: _pts,
              geo: g,
              minPct: widget.minPct,
              currentTemp: widget.currentTemp,
              criticalC: widget.criticalC,
              line: line,
              lineWidth: math.max(2, tokens.chart.lineWidth),
              fillAlpha: tokens.chart.fillAlpha > 0 ? tokens.chart.fillAlpha : 0.08,
              grid: scheme.outlineVariant,
              muted: scheme.onSurfaceVariant,
              danger: StatusColors.toneOf(context, Status.error).color,
              surface: tokens.cardColor,
              label: label,
              active: _drag,
            ),
          );
          if (!widget.editable) return paint;
          return GestureDetector(
            behavior: HitTestBehavior.opaque,
            onPanStart: (d) => setState(() => _drag = _hit(d.localPosition, g)),
            onPanUpdate: (d) {
              final i = _drag;
              if (i == null) return;
              setState(() => _pts = movePoint(_pts, i, g.tAt(d.localPosition.dx), g.pAt(d.localPosition.dy), widget.minPct, widget.limits));
            },
            onPanEnd: (_) {
              if (_drag == null) return;
              setState(() => _drag = null);
              if (!_listEq(_pts, widget.points)) widget.onChanged?.call(_pts);
            },
            onPanCancel: () => setState(() => _drag = null),
            child: paint,
          );
        },
      ),
    );
  }
}

bool _listEq(List<FanPoint> a, List<FanPoint> b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

class _Geometry {
  _Geometry(this.size, this.tMin, this.tMax);

  final Size size;
  final double tMin, tMax;

  static const left = 36.0, right = 8.0, top = 8.0, bottom = 22.0;

  double get w => size.width - left - right;
  double get h => size.height - top - bottom;

  double x(double t) => left + (t - tMin) / (tMax - tMin) * w;
  double y(num p) => top + (1 - p / 100) * h;
  Offset at(FanPoint p) => Offset(x(p.t), y(p.p));
  double tAt(double dx) => tMin + (dx - left) / w * (tMax - tMin);
  double pAt(double dy) => (1 - (dy - top) / h) * 100;
}

class _CurvePainter extends CustomPainter {
  _CurvePainter({
    required this.points,
    required this.geo,
    required this.minPct,
    required this.currentTemp,
    required this.criticalC,
    required this.line,
    required this.lineWidth,
    required this.fillAlpha,
    required this.grid,
    required this.muted,
    required this.danger,
    required this.surface,
    required this.label,
    required this.active,
  });

  final List<FanPoint> points;
  final _Geometry geo;
  final int minPct;
  final double? currentTemp;
  final int? criticalC;
  final Color line, grid, muted, danger, surface;
  final double lineWidth, fillAlpha;
  final TextStyle label;
  final int? active;

  void _text(Canvas c, String s, Offset at, {bool right = false, bool center = false}) {
    final tp = TextPainter(text: TextSpan(text: s, style: label), textDirection: TextDirection.ltr)..layout();
    final dx = right ? at.dx - tp.width : (center ? at.dx - tp.width / 2 : at.dx);
    tp.paint(c, Offset(dx, at.dy - tp.height / 2));
  }

  @override
  void paint(Canvas canvas, Size size) {
    final g = geo;
    final gridPaint = Paint()
      ..color = grid
      ..strokeWidth = 1;
    // Grid and scales.
    for (final p in const [0, 25, 50, 75, 100]) {
      canvas.drawLine(Offset(g.x(g.tMin), g.y(p)), Offset(g.x(g.tMax), g.y(p)), gridPaint);
      _text(canvas, '$p%', Offset(_Geometry.left - 6, g.y(p)), right: true);
    }
    for (var t = g.tMin; t <= g.tMax; t += 20) {
      _text(canvas, '${t.round()}°', Offset(g.x(t), size.height - _Geometry.bottom / 2), center: true);
    }
    // Below the fan's minimum: never used.
    canvas.drawRect(Rect.fromLTRB(g.x(g.tMin), g.y(minPct), g.x(g.tMax), g.y(0)), Paint()..color = muted.withValues(alpha: 0.10));
    // Emergency zone.
    final crit = criticalC;
    if (crit != null && crit < g.tMax) {
      canvas.drawRect(Rect.fromLTRB(g.x(crit.toDouble()), g.y(100), g.x(g.tMax), g.y(0)), Paint()..color = danger.withValues(alpha: 0.10));
    }
    if (points.isEmpty) return;
    // The curve, flat before the first and after the last point.
    final path = Path()..moveTo(g.x(g.tMin), g.y(points.first.p));
    for (final p in points) {
      path.lineTo(g.x(p.t), g.y(p.p));
    }
    path.lineTo(g.x(g.tMax), g.y(points.last.p));
    final fill = Path.from(path)
      ..lineTo(g.x(g.tMax), g.y(0))
      ..lineTo(g.x(g.tMin), g.y(0))
      ..close();
    canvas.drawPath(fill, Paint()..color = line.withValues(alpha: fillAlpha));
    canvas.drawPath(
      path,
      Paint()
        ..color = line
        ..style = PaintingStyle.stroke
        ..strokeWidth = lineWidth
        ..strokeJoin = StrokeJoin.round,
    );
    // Now.
    final now = currentTemp;
    if (now != null) {
      final t = now.clamp(g.tMin, g.tMax);
      final x = g.x(t);
      final dash = Paint()
        ..color = muted
        ..strokeWidth = 1;
      for (var y = g.y(100); y < g.y(0); y += 6) {
        canvas.drawLine(Offset(x, y), Offset(x, math.min(y + 3, g.y(0))), dash);
      }
      final p = math.max(minPct.toDouble(), evalCurve(points, t));
      canvas.drawCircle(Offset(x, g.y(p)), 5, Paint()..color = surface);
      canvas.drawCircle(Offset(x, g.y(p)), 3.5, Paint()..color = muted);
    }
    // Points.
    for (var i = 0; i < points.length; i++) {
      final c = g.at(points[i]);
      final r = i == active ? 8.0 : 6.0;
      canvas.drawCircle(c, r, Paint()..color = i == active ? line : surface);
      canvas.drawCircle(
        c,
        r,
        Paint()
          ..color = line
          ..style = PaintingStyle.stroke
          ..strokeWidth = 2.25,
      );
    }
  }

  @override
  bool shouldRepaint(_CurvePainter old) =>
      old.points != points || old.currentTemp != currentTemp || old.active != active || old.minPct != minPct || old.criticalC != criticalC || old.line != line || old.label != label;
}
