import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';

/// Facts on one line, "Idle · 37 °C · VRAM 298 MB of 11 GB", that wrap
/// between facts, never inside one ("4 drives", never "4 / drives"), and
/// never leave a separator hanging at the end of a line: where the next
/// fact doesn't fit, the line ends at the fact before and its dot is
/// dropped, so the wrap reads "Idle · 37 °C" / "VRAM 298 MB of 11 GB".
///
/// Each fact is a [TextSpan] with plain text, so one can carry its own
/// style (a status word in its colour); [style] is the line's and the
/// separators', over the ambient [DefaultTextStyle] as with [Text].
///
/// It lays itself out rather than through a LayoutBuilder, so it can sit
/// in an IntrinsicHeight row (the metric cards').
class FactLine extends LeafRenderObjectWidget {
  const FactLine(this.facts, {super.key, this.style, this.maxLines});

  /// Plain facts in [style].
  FactLine.plain(List<String> facts, {Key? key, TextStyle? style, int? maxLines})
      : this([for (final f in facts) TextSpan(text: f)], key: key, style: style, maxLines: maxLines);

  final List<TextSpan> facts;
  final TextStyle? style;

  /// Past this many lines the text ends in an ellipsis.
  final int? maxLines;

  static const separator = ' · ';

  List<TextSpan> get _whole => [
        for (final f in facts)
          if ((f.text ?? '').isNotEmpty) TextSpan(text: f.text!.replaceAll(' ', '\u00A0'), style: f.style),
      ];

  @override
  RenderObject createRenderObject(BuildContext context) => RenderFactLine(
        facts: _whole,
        style: DefaultTextStyle.of(context).style.merge(style),
        textScaler: MediaQuery.textScalerOf(context),
        textDirection: Directionality.of(context),
        maxLines: maxLines,
      );

  @override
  void updateRenderObject(BuildContext context, RenderFactLine renderObject) => renderObject
    ..facts = _whole
    ..style = DefaultTextStyle.of(context).style.merge(style)
    ..textScaler = MediaQuery.textScalerOf(context)
    ..textDirection = Directionality.of(context)
    ..maxLines = maxLines;
}

/// The render object of [FactLine].
class RenderFactLine extends RenderBox {
  RenderFactLine({
    required this._facts,
    required this._style,
    required this._textScaler,
    required this._textDirection,
    this._maxLines,
  });

  List<TextSpan> _facts;
  set facts(List<TextSpan> v) {
    if (_sameFacts(v, _facts)) return;
    _facts = v;
    _changed();
  }

  TextStyle _style;
  set style(TextStyle v) {
    if (v == _style) return;
    _style = v;
    _changed();
  }

  TextScaler _textScaler;
  set textScaler(TextScaler v) {
    if (v == _textScaler) return;
    _textScaler = v;
    _changed();
  }

  TextDirection _textDirection;
  set textDirection(TextDirection v) {
    if (v == _textDirection) return;
    _textDirection = v;
    _changed();
  }

  int? _maxLines;
  set maxLines(int? v) {
    if (v == _maxLines) return;
    _maxLines = v;
    _changed();
  }

  static bool _sameFacts(List<TextSpan> a, List<TextSpan> b) {
    if (a.length != b.length) return false;
    for (var i = 0; i < a.length; i++) {
      if (a[i].text != b[i].text || a[i].style != b[i].style) return false;
    }
    return true;
  }

  void _changed() {
    _painter?.dispose();
    _painter = null;
    markNeedsLayout();
    markNeedsSemanticsUpdate();
  }

  /// The laid-out text for the last width asked about.
  TextPainter? _painter;
  double? _painterWidth;

  TextPainter _newPainter(InlineSpan text, {int? maxLines, String? ellipsis}) =>
      TextPainter(text: text, textDirection: _textDirection, textScaler: _textScaler, maxLines: maxLines, ellipsis: ellipsis);

  bool _fits(List<InlineSpan> line, double width) {
    final p = _newPainter(TextSpan(style: _style, children: line), maxLines: 1)..layout(maxWidth: width);
    final fit = !p.didExceedMaxLines;
    p.dispose();
    return fit;
  }

  /// The facts laid out at [width]: each line holds as many whole facts as
  /// fit, and a line ends with a break rather than a separator.
  TextPainter _layoutAt(double width) {
    if (_painter != null && _painterWidth == width) return _painter!;
    final spans = <InlineSpan>[];
    var line = <InlineSpan>[];
    for (final f in _facts) {
      final joined = [...line, const TextSpan(text: FactLine.separator), f];
      if (line.isEmpty) {
        line = [f];
      } else if (width.isInfinite || _fits(joined, width)) {
        line = joined;
      } else {
        spans
          ..addAll(line)
          ..add(const TextSpan(text: '\n'));
        line = [f];
      }
    }
    spans.addAll(line);
    _painter?.dispose();
    _painter = _newPainter(TextSpan(style: _style, children: spans), maxLines: _maxLines, ellipsis: _maxLines == null ? null : '\u2026')
      ..layout(maxWidth: width);
    _painterWidth = width;
    return _painter!;
  }

  /// The text as last laid out, line breaks included.
  @visibleForTesting
  String? get debugLaidOutText => _painter?.plainText;

  String get _plain => _facts.map((f) => f.text!.replaceAll('\u00A0', ' ')).join(FactLine.separator);

  @override
  double computeMinIntrinsicWidth(double height) {
    // The widest single fact: every fact on its own line.
    final p = _newPainter(TextSpan(style: _style, children: [
      for (final (i, f) in _facts.indexed) ...[if (i > 0) const TextSpan(text: '\n'), f],
    ]))
      ..layout();
    final w = p.maxIntrinsicWidth;
    p.dispose();
    return w;
  }

  @override
  double computeMaxIntrinsicWidth(double height) {
    final p = _newPainter(TextSpan(style: _style, children: [
      for (final (i, f) in _facts.indexed) ...[if (i > 0) const TextSpan(text: FactLine.separator), f],
    ]))
      ..layout();
    final w = p.maxIntrinsicWidth;
    p.dispose();
    return w;
  }

  @override
  double computeMinIntrinsicHeight(double width) => _layoutAt(width).height;

  @override
  double computeMaxIntrinsicHeight(double width) => _layoutAt(width).height;

  @override
  Size computeDryLayout(covariant BoxConstraints constraints) {
    final p = _layoutAt(constraints.maxWidth);
    return constraints.constrain(p.size);
  }

  @override
  double? computeDistanceToActualBaseline(TextBaseline baseline) => _layoutAt(constraints.maxWidth).computeDistanceToActualBaseline(baseline);

  @override
  void performLayout() {
    size = constraints.constrain(_layoutAt(constraints.maxWidth).size);
  }

  @override
  void paint(PaintingContext context, Offset offset) {
    _layoutAt(constraints.maxWidth).paint(context.canvas, offset);
  }

  @override
  void describeSemanticsConfiguration(SemanticsConfiguration config) {
    super.describeSemanticsConfiguration(config);
    config
      ..isSemanticBoundary = true
      ..label = _plain
      ..textDirection = _textDirection;
  }

  @override
  void dispose() {
    _painter?.dispose();
    _painter = null;
    super.dispose();
  }
}
