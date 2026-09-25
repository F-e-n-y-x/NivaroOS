import 'package:flutter/material.dart';

/// Fades the ends of a horizontal row that scrolls (a key bar, a chip row)
/// where more is hidden, like Android's own fading edges: a key cut off at
/// the screen edge then reads as "there is more this way", not as a
/// layout bug. The fade is a mask on the content, not a coloured surface.
///
/// Give [controller] to the scrollable inside [child].
class FadingEdges extends StatefulWidget {
  const FadingEdges({super.key, required this.controller, required this.child, this.extent = 32});

  final ScrollController controller;
  final Widget child;

  /// How wide each fade is, in dp.
  final double extent;

  @override
  State<FadingEdges> createState() => _FadingEdgesState();
}

class _FadingEdgesState extends State<FadingEdges> {
  bool _start = false;
  bool _end = true;

  @override
  void initState() {
    super.initState();
    widget.controller.addListener(_update);
    WidgetsBinding.instance.addPostFrameCallback((_) => _update());
  }

  @override
  void dispose() {
    widget.controller.removeListener(_update);
    super.dispose();
  }

  void _update() {
    if (!mounted || !widget.controller.hasClients) return;
    final p = widget.controller.position;
    if (!p.hasContentDimensions) return;
    final start = p.pixels > p.minScrollExtent + 1;
    final end = p.pixels < p.maxScrollExtent - 1;
    if (start != _start || end != _end) {
      setState(() {
        _start = start;
        _end = end;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return NotificationListener<ScrollMetricsNotification>(
      onNotification: (_) {
        _update();
        return false;
      },
      child: LayoutBuilder(
        builder: (context, constraints) {
          final f = (widget.extent / constraints.maxWidth).clamp(0.0, 0.5);
          final rtl = Directionality.of(context) == TextDirection.rtl;
          final left = rtl ? _end : _start;
          final right = rtl ? _start : _end;
          return ShaderMask(
            blendMode: BlendMode.dstIn,
            shaderCallback: (rect) => LinearGradient(
              colors: [
                left ? Colors.transparent : Colors.black,
                Colors.black,
                Colors.black,
                right ? Colors.transparent : Colors.black,
              ],
              stops: [0, f, 1 - f, 1],
            ).createShader(rect),
            child: widget.child,
          );
        },
      ),
    );
  }
}
