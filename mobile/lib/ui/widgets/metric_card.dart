import 'package:flutter/material.dart';

import '../theme/design_tokens.dart';
import '../theme/spacing.dart';
import '../theme/status_colors.dart';
import 'fact_line.dart';

/// A word for how a metric is doing ("Moderate", "Running low"), with its
/// status. The word is always shown with the colour, never colour alone.
@immutable
class MetricLevel {
  const MetricLevel(this.word, [this.status = Status.neutral]);

  final String word;
  final Status status;

  bool get alerting => status == Status.warning || status == Status.error;
}

/// One reading as a large tabular number with a smaller unit: "42" "%".
/// The unit is set at about half the number's size on the same baseline,
/// a hair apart, so "42%" reads as one value rather than a number with a
/// subscript. A value too wide for its space scales down instead of being
/// cut off.
class MetricValue extends StatelessWidget {
  const MetricValue({super.key, required this.value, this.unit, this.style, this.unitStyle});

  final String value;
  final String? unit;
  final TextStyle? style;
  final TextStyle? unitStyle;

  @override
  Widget build(BuildContext context) {
    final t = DesignTokens.of(context);
    final v = style ?? t.heroValue;
    final u = unit;
    final symbol = u != null && (u == '%' || u.startsWith('°'));
    final size = v.fontSize ?? 36;
    final us = (unitStyle ?? t.heroUnit).copyWith(fontSize: size * (symbol ? .55 : .45), letterSpacing: 0, height: v.height);
    return FittedBox(
      fit: BoxFit.scaleDown,
      alignment: AlignmentDirectional.centerStart,
      child: Text.rich(
        TextSpan(children: [
          TextSpan(text: value, style: v),
          if (u != null) TextSpan(text: symbol ? '\u200A$u' : ' $u', style: us),
        ]),
        maxLines: 1,
        softWrap: false,
      ),
    );
  }
}

/// Home's widget for one metric - Processor, Memory, Network, Storage,
/// Graphics - like the web UI's desktop widgets: its name, the reading as
/// the card's one big number, a status word and a fact line, and its own
/// chart as the card's body ([body], usually a `LiveChart` with `bleed`).
/// The whole card opens the metric's page.
///
/// The body takes every bit of height the text leaves, so the chart is
/// the card's second half rather than a footer, and runs to the card's
/// edges. A body that isn't a chart (storage's drive bars) passes
/// [padBody] to sit on the card's padding instead.
///
/// Drawn from `DesignTokens`: card colour and edge, corners, padding,
/// type, icon style. With [emphasized], a direction that has an emphasis
/// colour (Tonal) draws the card in it.
class MetricCard extends StatelessWidget {
  const MetricCard({
    super.key,
    required this.icon,
    required this.label,
    required this.value,
    this.unit,
    this.level,
    this.detail,
    this.meta,
    this.values,
    required this.body,
    this.padBody = false,
    this.emphasized = false,
    this.onTap,
    required this.semanticLabel,
  });

  final IconData icon;
  final String label;
  final String value;
  final String? unit;
  final MetricLevel? level;

  /// Facts after the level word: "48 °C · 4.21 GHz".
  final String? detail;

  /// Right side of the header, for wide cards: "enp7s0 · up".
  final String? meta;

  /// Replaces the value, facts and body with custom content (network:
  /// download and upload side by side, each with its own chart).
  final Widget? values;

  final Widget body;
  final bool padBody;
  final bool emphasized;
  final VoidCallback? onTap;

  /// What TalkBack reads for the whole card.
  final String semanticLabel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final base = DesignTokens.of(context);
    final panel = emphasized ? base.emphasisCard : null;
    if (panel != null) {
      final on = base.onEmphasisCard!;
      final s = theme.colorScheme;
      return Theme(
        data: theme.copyWith(
          colorScheme: s.copyWith(primary: on, onSurface: on, onSurfaceVariant: on.withValues(alpha: .84), outlineVariant: on.withValues(alpha: .25)),
          extensions: [...theme.extensions.values.where((e) => e is! DesignTokens), base.on(panel, on)],
        ),
        child: Builder(builder: _card),
      );
    }
    return _card(context);
  }

  Widget _card(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final t = DesignTokens.of(context);
    final pad = t.cardPadding;

    // On the emphasised card (itself the primary container) the badge is a
    // light wash of the page instead.
    final onPanel = t.cardColor == scheme.primaryContainer;
    final Widget glyph = t.iconBadge
        ? Container(
            width: 32,
            height: 32,
            decoration: BoxDecoration(color: onPanel ? scheme.surface.withValues(alpha: .45) : scheme.primaryContainer, shape: BoxShape.circle),
            child: Icon(icon, size: 18, color: onPanel ? scheme.onSurface : scheme.onPrimaryContainer),
          )
        : Icon(icon, size: 20, color: scheme.onSurfaceVariant);

    final header = Row(
      children: [
        if (t.cardIcons) ...[glyph, SizedBox(width: t.iconBadge ? Space.sm + 2 : Space.sm)],
        Expanded(child: Text(label, style: t.cardLabel, maxLines: 1, overflow: TextOverflow.ellipsis)),
        if (meta != null) ...[
          const SizedBox(width: Space.sm),
          Flexible(child: Text(meta!, style: t.data, maxLines: 1, overflow: TextOverflow.ellipsis)),
        ],
      ],
    );

    final lvl = level;
    final tone = lvl == null ? null : StatusColors.toneOf(context, lvl.status);
    // Each fact breaks as a whole ("4 drives", never "4 / drives"), and a
    // wrap never leaves a separator hanging.
    final facts = FactLine(
      [
        if (lvl != null)
          TextSpan(
            text: lvl.word,
            style: t.data.copyWith(color: lvl.alerting ? tone!.color : scheme.onSurface, fontWeight: FontWeight.w600),
          ),
        if (detail != null)
          for (final f in detail!.split(FactLine.separator)) TextSpan(text: f),
      ],
      style: t.data,
      maxLines: 2,
    );

    final top = values == null
        ? Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              header,
              SizedBox(height: t.iconBadge ? Space.sm : Space.md),
              MetricValue(value: value, unit: unit),
              if (lvl != null || detail != null) ...[const SizedBox(height: 2), facts],
            ],
          )
        : header;

    return Semantics(
      button: onTap != null,
      label: semanticLabel,
      excludeSemantics: true,
      child: Material(
        color: t.cardColor,
        shape: t.cardShape(),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onTap,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Padding(padding: EdgeInsets.fromLTRB(pad.left, pad.top, pad.right, 0), child: top),
              if (values != null)
                Expanded(child: Padding(padding: EdgeInsets.only(top: t.iconBadge ? Space.sm : Space.md), child: values))
              else ...[
                const SizedBox(height: Space.sm),
                Expanded(
                  child: padBody
                      ? Padding(padding: EdgeInsets.fromLTRB(pad.left, 0, pad.right, pad.bottom), child: Align(alignment: Alignment.bottomCenter, child: body))
                      : body,
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// Cards in a responsive grid: [columns] across, each card spanning one or
/// more columns. A row that can't fit the next card is closed by widening
/// its last card, so every row is full and cards in a row share a height.
class MetricGrid extends StatelessWidget {
  const MetricGrid({super.key, required this.columns, required this.children});

  final int columns;

  /// (span, card) pairs, in reading order.
  final List<(int, Widget)> children;

  @override
  Widget build(BuildContext context) {
    final gap = DesignTokens.of(context).gap;
    final rows = <List<(int, Widget)>>[];
    var used = 0;
    for (final (span, w) in children) {
      final s = span.clamp(1, columns);
      if (rows.isEmpty || used + s > columns) {
        if (rows.isNotEmpty) _fill(rows.last, columns);
        rows.add([]);
        used = 0;
      }
      rows.last.add((s, w));
      used += s;
    }
    if (rows.isNotEmpty) _fill(rows.last, columns);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (var r = 0; r < rows.length; r++) ...[
          if (r > 0) SizedBox(height: gap),
          IntrinsicHeight(
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                for (var i = 0; i < rows[r].length; i++) ...[
                  if (i > 0) SizedBox(width: gap),
                  Expanded(flex: rows[r][i].$1, child: rows[r][i].$2),
                ],
              ],
            ),
          ),
        ],
      ],
    );
  }

  static void _fill(List<(int, Widget)> row, int columns) {
    final used = row.fold(0, (s, e) => s + e.$1);
    if (used < columns) row[row.length - 1] = (row.last.$1 + columns - used, row.last.$2);
  }
}

/// Thin capacity bars, one per item: "data  ▇▇▇▁▁  63%". For storage,
/// which changes too slowly for a line. Track height and fill follow the
/// direction (a hairline with an ink fill in Rack).
class MeterBars extends StatelessWidget {
  const MeterBars({super.key, required this.items, this.warnAt = .8, this.criticalAt = .9});

  final List<(String, double)> items;
  final double warnAt;
  final double criticalAt;

  @override
  Widget build(BuildContext context) {
    final t = DesignTokens.of(context);
    final scheme = Theme.of(context).colorScheme;
    final style = t.data;
    final h = t.meterHeight.clamp(3.0, 5.0);
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        for (final (label, f) in items)
          Padding(
            padding: const EdgeInsets.only(top: Space.xs),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(children: [
                  Expanded(child: Text(label, style: style, maxLines: 1, overflow: TextOverflow.ellipsis)),
                  const SizedBox(width: Space.sm),
                  Text('${(f * 100).round()}%', style: style.copyWith(color: scheme.onSurface)),
                ]),
                const SizedBox(height: 3),
                ClipRRect(
                  borderRadius: BorderRadius.circular(h / 2),
                  child: SizedBox(
                    height: h,
                    child: Stack(children: [
                      Positioned.fill(child: ColoredBox(color: scheme.surfaceContainerHighest)),
                      FractionallySizedBox(
                        widthFactor: f.clamp(0.0, 1.0),
                        heightFactor: 1,
                        child: ColoredBox(
                          color: f >= criticalAt
                              ? scheme.error
                              : f >= warnAt
                                  ? StatusColors.of(context).warning.color
                                  : t.meterFill(scheme),
                        ),
                      ),
                    ]),
                  ),
                ),
              ],
            ),
          ),
      ],
    );
  }
}
