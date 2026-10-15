import 'package:flutter/material.dart';

import '../ui/ui.dart';

/// Where a full-screen console keeps its bar or, hidden, its handle: on
/// the top edge, inside the safe area (cutouts, landscape notches), at
/// [x] from -1 (left) to 1 (right).
Widget consoleTopEdge(BuildContext context, {required double x, required Widget child}) {
  final pad = MediaQuery.paddingOf(context);
  return Positioned(
    top: pad.top,
    left: pad.left + Space.sm,
    right: pad.right + Space.sm,
    child: Align(alignment: Alignment(x, -1), child: child),
  );
}

/// [x] after sliding something [width] wide [dx] along [consoleTopEdge].
double slideAlongTopEdge(BuildContext context, double x, double dx, double width) {
  final pad = MediaQuery.paddingOf(context);
  final room = (MediaQuery.sizeOf(context).width - pad.horizontal - 2 * Space.sm - width) / 2;
  return room <= 0 ? 0 : (x + dx / room).clamp(-1.0, 1.0);
}

/// The small tab left on the top edge when a console's bar is hidden:
/// tap it to bring the bar back, slide it along the edge to keep it off
/// what you're looking at. Its 48dp target sits on the very edge, where
/// the system's swipe for the status bar is anyway.
class ConsoleEdgeHandle extends StatelessWidget {
  const ConsoleEdgeHandle({
    super.key,
    required this.label,
    required this.onTap,
    required this.onSlide,
    this.onSlideEnd,
  });

  static const size = Size(64, 48);

  final String label;
  final VoidCallback onTap;
  final ValueChanged<double> onSlide;
  final VoidCallback? onSlideEnd;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Semantics(
      button: true,
      label: label,
      excludeSemantics: true,
      onTap: onTap,
      child: GestureDetector(
        onHorizontalDragUpdate: (d) => onSlide(d.delta.dx),
        onHorizontalDragEnd: (_) => onSlideEnd?.call(),
        child: InkWell(
          onTap: onTap,
          customBorder: const StadiumBorder(),
          child: SizedBox.fromSize(
            size: size,
            child: Align(
              alignment: Alignment.topCenter,
              child: Container(
                width: 48,
                height: 20,
                decoration: ShapeDecoration(
                  color: scheme.surfaceContainerHigh.withValues(alpha: 0.9),
                  shape: StadiumBorder(side: BorderSide(color: scheme.outlineVariant)),
                ),
                child: Icon(Icons.expand_more, size: 18, color: scheme.onSurfaceVariant),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
