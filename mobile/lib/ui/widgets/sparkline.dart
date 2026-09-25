import 'dart:math' as math;

import 'package:flutter/material.dart';

/// A small line chart of recent readings (CPU load, memory, network
/// traffic) with no axes: the shape of the last few minutes at a glance.
/// Flat tonal fill under a primary line; nothing when there are fewer
/// than two readings, rather than a made-up line.
///
/// [max] fixes the top of the scale (100 for percentages); without it the
/// scale follows the highest reading, for rates.
class Sparkline extends StatelessWidget {
  const Sparkline({super.key, required this.values, this.max, this.color, this.height = 32, this.width, this.semanticLabel});

  final List<double> values;
  final double? max;
  final Color? color;
  final double height;
  final double? width;

  /// Read by TalkBack instead of the chart ("Processor over the last
  /// 2 minutes: 1% to 14%"). Null leaves the chart out of the tree, for a
  /// row that already says the number.
  final String? semanticLabel;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final line = color ?? scheme.primary;
    final chart = SizedBox(
      width: width,
      height: height,
      child: values.length < 2
          ? null
          : CustomPaint(painter: _SparkPainter(values, max, line, line.withValues(alpha: 0.14))),
    );
    return semanticLabel == null ? ExcludeSemantics(child: chart) : Semantics(label: semanticLabel, image: true, child: chart);
  }
}

class _SparkPainter extends CustomPainter {
  _SparkPainter(this.values, this.max, this.line, this.fill);

  final List<double> values;
  final double? max;
  final Color line;
  final Color fill;

  @override
  void paint(Canvas canvas, Size size) {
    final top = math.max(max ?? values.reduce(math.max), 1e-9);
    const stroke = 2.0;
    final h = size.height - stroke;
    final dx = size.width / (values.length - 1);
    final path = Path();
    for (var i = 0; i < values.length; i++) {
      final x = i * dx;
      final y = stroke / 2 + h * (1 - (values[i] / top).clamp(0.0, 1.0));
      i == 0 ? path.moveTo(x, y) : path.lineTo(x, y);
    }
    final area = Path.from(path)
      ..lineTo(size.width, size.height)
      ..lineTo(0, size.height)
      ..close();
    canvas.drawPath(area, Paint()..color = fill);
    canvas.drawPath(
      path,
      Paint()
        ..color = line
        ..style = PaintingStyle.stroke
        ..strokeWidth = stroke
        ..strokeJoin = StrokeJoin.round
        ..strokeCap = StrokeCap.round,
    );
  }

  @override
  bool shouldRepaint(_SparkPainter old) => old.values != values || old.max != max || old.line != line;
}
