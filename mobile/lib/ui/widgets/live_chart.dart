import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/material.dart';

import '../theme/design_tokens.dart';

/// One line on a [LiveChart].
@immutable
class ChartSeries {
  const ChartSeries(this.values, {this.color, this.dashed = false});

  final List<double> values;

  /// Null: the direction's main line colour (the first series) or its
  /// secondary colour (any other).
  final Color? color;

  /// A second series is dashed as well as a different colour, so the two
  /// differ in more than hue (network down and up).
  final bool dashed;
}

/// A live line chart of recent readings: the shape of the last few minutes
/// with no axes, the latest reading marked with a dot. Drawn in the
/// direction's chart style ([ChartTokens]): line weight, a flat fill (never
/// a gradient), grid, dot.
///
/// Readings sit on a fixed time grid of [capacity] slots anchored at the
/// right edge, so a fresh chart grows in from the right instead of
/// stretching two readings across the card. With fewer than two readings
/// it shows only its baseline and "Collecting…" - never a made-up line.
///
/// [max] fixes the top (100 for percentages); without it the scale follows
/// the highest reading plus 15% headroom (rates). [threshold] draws a
/// dashed guide at that value, and [alert] recolours the line and dot
/// (with the status word next to it in the card, so colour is never the
/// only signal).
class LiveChart extends StatelessWidget {
  const LiveChart({
    super.key,
    required this.series,
    required this.capacity,
    this.max,
    this.threshold,
    this.alert,
    this.height,
    this.axisLabels = const [],
    this.window,
    this.semanticLabel,
    this.grid,
    this.bleed = false,
    this.labelInset = 0,
  });

  final List<ChartSeries> series;
  final int capacity;
  final double? max;
  final double? threshold;
  final Color? alert;

  /// Defaults to the direction's card chart height.
  final double? height;

  /// Values to label on the right edge (detail pages): (value, "50%").
  final List<(double, String)> axisLabels;

  /// "2 min": what the chart spans, for the time labels some directions
  /// draw under it.
  final String? window;

  /// Read by TalkBack instead of the chart ("Processor over the last
  /// 2 minutes: 1% to 14%"). Null leaves the chart out of the tree, for a
  /// card that already says the number.
  final String? semanticLabel;

  /// Guides at 25/50/75% of the scale; null follows the direction (on for
  /// detail pages, which pass true).
  final bool? grid;

  /// The chart runs to the card's edges (a metric card's body): no
  /// baseline, a little room under the line, and the fill (if any) down to
  /// the edge. [labelInset] keeps the time labels on the card's padding.
  final bool bleed;
  final double labelInset;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final tokens = DesignTokens.of(context);
    final c = tokens.chart;
    final main = alert ?? (c.inkLine ? scheme.onSurface : scheme.primary);
    final secondary = c.inkLine || c.grid ? scheme.onSurfaceVariant : scheme.tertiary;
    final collecting = series.every((s) => s.values.length < 2);
    final labelStyle = tokens.chartLabel;
    final textScale = MediaQuery.textScalerOf(context).scale(10) / 10;

    final painter = _ChartPainter(
      series: [
        for (var i = 0; i < series.length; i++)
          (series[i].values, series[i].color ?? (i == 0 ? main : secondary), series[i].dashed),
      ],
      capacity: capacity,
      max: max,
      threshold: threshold,
      tokens: c,
      grid: grid ?? c.grid,
      bleed: bleed,
      dot: alert ?? scheme.primary,
      ring: tokens.cardColor,
      guide: scheme.outlineVariant,
      thresholdColor: scheme.outline,
      axis: [for (final (v, l) in axisLabels) (v, TextPainter(text: TextSpan(text: l, style: labelStyle), textDirection: TextDirection.ltr)..layout())],
    );

    // A fixed height when asked for; otherwise the direction's minimum,
    // grown with the text so a chart never shrinks to a sliver under
    // large text, and free to fill any spare height it is given.
    final Widget plot = height != null
        ? SizedBox(height: height, width: double.infinity, child: RepaintBoundary(child: CustomPaint(painter: painter)))
        : ConstrainedBox(
            constraints: BoxConstraints(minHeight: c.height * textScale.clamp(1.0, 1.6), minWidth: double.infinity),
            child: RepaintBoundary(child: CustomPaint(painter: painter)),
          );
    Widget chart = plot;
    if (collecting) {
      chart = Stack(fit: height == null ? StackFit.expand : StackFit.loose, children: [
        chart,
        Positioned(left: labelInset, bottom: 6, child: Text('Collecting…', style: labelStyle)),
      ]);
    }
    if (c.timeLabels && window != null) {
      final labels = Padding(
        padding: EdgeInsets.fromLTRB(labelInset, 4, labelInset, 0),
        child: Row(children: [
          Text('−$window', style: labelStyle),
          const Spacer(),
          Text('now', style: labelStyle),
        ]),
      );
      chart = Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [if (height == null) Flexible(child: chart) else chart, labels],
      );
    }
    return semanticLabel == null ? ExcludeSemantics(child: chart) : Semantics(label: semanticLabel, image: true, child: chart);
  }
}

class _ChartPainter extends CustomPainter {
  _ChartPainter({
    required this.series,
    required this.capacity,
    required this.max,
    required this.threshold,
    required this.tokens,
    required this.grid,
    required this.bleed,
    required this.dot,
    required this.ring,
    required this.guide,
    required this.thresholdColor,
    required this.axis,
  });

  final List<(List<double>, Color, bool)> series;
  final int capacity;
  final double? max;
  final double? threshold;
  final ChartTokens tokens;
  final bool grid;
  final bool bleed;
  final Color dot;
  final Color ring;
  final Color guide;
  final Color thresholdColor;
  final List<(double, TextPainter)> axis;

  double get _top {
    if (max != null) return max!;
    var m = 0.0;
    for (final (v, _, _) in series) {
      for (final x in v) {
        m = math.max(m, x);
      }
    }
    return math.max(m * 1.15, 1e-9);
  }

  @override
  void paint(Canvas canvas, Size size) {
    final axisWidth = axis.isEmpty ? 0.0 : axis.map((a) => a.$2.width).reduce(math.max) + 8;
    final marker = tokens.dotRadius + (tokens.dotRing ? 1.5 : 0);
    // Room on the right for the "now" dot (and Rack's tick), so it isn't
    // cut by the card's edge.
    final tick = tokens.accentTick ? 7.0 : 0.0;
    final w = size.width - axisWidth - (axis.isEmpty ? marker + tick + (bleed ? 10 : 0) : 0);
    final pad = math.max(tokens.lineWidth / 2, marker);
    final bottom = bleed ? pad + 8 : pad;
    final h = size.height - pad - bottom;
    final top = _top;
    double yOf(double v) => pad + h * (1 - (v / top).clamp(0.0, 1.0));

    // Guides: a baseline at zero, the grid, the threshold, the axis.
    final guidePaint = Paint()
      ..color = guide
      ..strokeWidth = 1;
    if (!bleed) _dotted(canvas, Offset(0, size.height - 0.5), Offset(w, size.height - 0.5), guidePaint, dash: 2, gap: 3);
    if (grid) {
      for (final f in const [.25, .5, .75]) {
        final y = pad + h * (1 - f);
        _dotted(canvas, Offset(0, y), Offset(w, y), guidePaint, dash: 1, gap: 3);
      }
    }
    final t = threshold;
    if (t != null && t < top) {
      final y = yOf(t);
      _dotted(canvas, Offset(0, y), Offset(w, y), Paint()
        ..color = thresholdColor.withValues(alpha: .7)
        ..strokeWidth = 1, dash: 4, gap: 4);
    }
    for (final (v, tp) in axis) {
      tp.paint(canvas, Offset(size.width - tp.width, (yOf(v) - tp.height / 2).clamp(0, size.height - tp.height)));
    }

    final dx = w / math.max(capacity - 1, 1);
    for (var s = series.length - 1; s >= 0; s--) {
      final (raw, color, dashed) = series[s];
      if (raw.length < 2) continue;
      final values = raw.length > capacity ? raw.sublist(raw.length - capacity) : raw;
      final start = w - dx * (values.length - 1);
      final pts = [for (var i = 0; i < values.length; i++) Offset(start + i * dx, yOf(values[i]))];
      final path = _monotone(pts);

      if (s == 0 && tokens.fillAlpha > 0) {
        // Down to the chart's bottom edge (the card's edge when bleeding).
        final floor = bleed ? size.height : pad + h;
        final area = Path.from(path)
          ..lineTo(pts.last.dx, floor)
          ..lineTo(pts.first.dx, floor)
          ..close();
        canvas.drawPath(area, Paint()..color = color.withValues(alpha: tokens.fillAlpha));
      }
      final stroke = Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeWidth = s == 0 ? tokens.lineWidth : math.max(1, tokens.lineWidth - .5)
        ..strokeJoin = StrokeJoin.round
        ..strokeCap = StrokeCap.round;
      canvas.drawPath(dashed ? _dash(path, 4, 3) : path, stroke);

      if (s == 0 && tokens.dotRadius > 0) {
        if (tokens.dotRing) canvas.drawCircle(pts.last, tokens.dotRadius + 1.5, Paint()..color = ring);
        canvas.drawCircle(pts.last, tokens.dotRadius, Paint()..color = dot);
        if (tick > 0) {
          canvas.drawLine(
            Offset(pts.last.dx + marker + 1.5, pts.last.dy),
            Offset(pts.last.dx + marker + 1.5 + tick - 1, pts.last.dy),
            Paint()
              ..color = dot
              ..strokeWidth = 1.5
              ..strokeCap = StrokeCap.round,
          );
        }
      }
    }
  }

  /// A smooth line through [p] that never overshoots between readings
  /// (monotone cubic, Fritsch-Carlson): a spike stays a spike and the line
  /// never dips below zero or above the top.
  static Path _monotone(List<Offset> p) {
    final path = Path()..moveTo(p.first.dx, p.first.dy);
    final n = p.length;
    if (n == 2) return path..lineTo(p[1].dx, p[1].dy);
    final d = [for (var i = 0; i < n - 1; i++) (p[i + 1].dy - p[i].dy) / (p[i + 1].dx - p[i].dx)];
    final m = List<double>.filled(n, 0);
    m[0] = d[0];
    m[n - 1] = d[n - 2];
    for (var i = 1; i < n - 1; i++) {
      m[i] = d[i - 1] * d[i] <= 0 ? 0 : (d[i - 1] + d[i]) / 2;
    }
    for (var i = 0; i < n - 1; i++) {
      if (d[i] == 0) {
        m[i] = 0;
        m[i + 1] = 0;
        continue;
      }
      final a = m[i] / d[i], b = m[i + 1] / d[i];
      final s = a * a + b * b;
      if (s > 9) {
        final tau = 3 / math.sqrt(s);
        m[i] = tau * a * d[i];
        m[i + 1] = tau * b * d[i];
      }
    }
    for (var i = 0; i < n - 1; i++) {
      final h = (p[i + 1].dx - p[i].dx) / 3;
      path.cubicTo(p[i].dx + h, p[i].dy + m[i] * h, p[i + 1].dx - h, p[i + 1].dy - m[i + 1] * h, p[i + 1].dx, p[i + 1].dy);
    }
    return path;
  }

  static Path _dash(Path source, double dash, double gap) {
    final out = Path();
    for (final ui.PathMetric metric in source.computeMetrics()) {
      var d = 0.0;
      while (d < metric.length) {
        out.addPath(metric.extractPath(d, math.min(d + dash, metric.length)), Offset.zero);
        d += dash + gap;
      }
    }
    return out;
  }

  static void _dotted(Canvas canvas, Offset a, Offset b, Paint paint, {required double dash, required double gap}) {
    var x = a.dx;
    while (x < b.dx) {
      canvas.drawLine(Offset(x, a.dy), Offset(math.min(x + dash, b.dx), b.dy), paint);
      x += dash + gap;
    }
  }

  @override
  bool shouldRepaint(_ChartPainter old) => true;
}
