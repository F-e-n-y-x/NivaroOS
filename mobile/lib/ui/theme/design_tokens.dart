import 'package:flutter/material.dart';

import 'appearance.dart';

/// How a direction draws its line charts (`LiveChart`).
@immutable
class ChartTokens {
  const ChartTokens({
    required this.lineWidth,
    required this.fillAlpha,
    required this.inkLine,
    required this.grid,
    required this.dotRadius,
    required this.dotRing,
    required this.height,
    required this.timeLabels,
    this.accentTick = false,
  });

  /// Stroke of the main line, in dp.
  final double lineWidth;

  /// Flat fill under the line as a fraction of the line colour (0 = none).
  /// Never a gradient (design brief §2).
  final double fillAlpha;

  /// Draw the line in the text colour and keep the accent for the "now"
  /// dot and tick only (Rack), instead of drawing the line in the accent.
  final bool inkLine;

  /// Dotted horizontal guides at 25/50/75% on the Home cards too (Console).
  /// Detail pages always draw them.
  final bool grid;

  /// The "now" marker's radius; 0 for none.
  final double dotRadius;

  /// A ring of the card colour around the dot, so it sits on the line.
  final bool dotRing;

  /// The smallest chart height inside a metric card at 100% text. The
  /// chart is the card's body and grows into any spare height.
  final double height;

  /// "−2 min" and "now" under the chart (Console).
  final bool timeLabels;

  /// A short accent tick on the right edge at the latest reading, so the
  /// accent reads as a marker rather than a stray dot (Rack).
  final bool accentTick;
}

/// A direction's corner scale, in the same five steps as [Corners] (which
/// v2 keeps): Rack precise, Tonal soft, Console tight. Screens and widgets
/// read these instead of fixed radii, so a panel, a thumbnail or a skeleton
/// takes the corners of the style it's drawn in.
@immutable
class Radii {
  const Radii({required this.xs, required this.sm, required this.md, required this.lg, required this.xl});

  /// Small marks: progress tracks, tooltips, skeleton lines.
  final double xs;

  /// Chips, text fields in Rack and Console, thumbnails in lists.
  final double sm;

  /// Buttons (except Tonal's stadium), menus, snack bars, app icons.
  final double md;

  /// Cards, panels, notices.
  final double lg;

  /// Dialogs and large surfaces.
  final double xl;

  /// The M3 shape scale (`Corners`), for v2 and bare themes.
  static const material = Radii(xs: 4, sm: 8, md: 12, lg: 16, xl: 28);

  double operator [](Corner c) => switch (c) {
        Corner.xs => xs,
        Corner.sm => sm,
        Corner.md => md,
        Corner.lg => lg,
        Corner.xl => xl,
      };
}

/// A step on the [Radii] scale, for widgets that take their corner as a
/// constructor argument (`SkeletonBox(corner: Corner.md)`) and resolve it
/// against the style when they build.
enum Corner { xs, sm, md, lg, xl }

/// How a direction draws a primary (filled) button.
enum ButtonTreatment {
  /// The M3 default: filled in the primary colour.
  accent,

  /// Filled in ink (the text colour) with page-coloured text (Rack).
  ink,

  /// Filled with the primary container (Tonal).
  tonal,

  /// Outlined in the primary colour with a mono label (Console).
  outlined,
}

/// Direction tokens that the colour scheme and text theme can't carry:
/// card and group shapes, the metric card's type, chips, meters and the
/// chart style. Every theme `AppTheme` builds has one; read it with
/// [DesignTokens.of].
@immutable
class DesignTokens extends ThemeExtension<DesignTokens> {
  const DesignTokens({
    required this.direction,
    required this.cardRadius,
    required this.groupRadius,
    required this.groupInnerRadius,
    required this.cardColor,
    required this.cardBorder,
    required this.gap,
    required this.cardPadding,
    required this.heroValue,
    required this.heroUnit,
    required this.detailValue,
    required this.cardLabel,
    required this.sectionLabel,
    required this.data,
    required this.chartLabel,
    required this.cardIcons,
    required this.iconBadge,
    required this.ruledGroups,
    required this.outlinedChips,
    required this.meterHeight,
    required this.meterInk,
    required this.button,
    required this.statusPanel,
    required this.chart,
    this.radii = Radii.material,
    this.segmentBorder,
    this.monoFamily,
    this.emphasisCard,
    this.onEmphasisCard,
  });

  final DesignDirection direction;

  /// Metric cards and other free-standing cards and panels.
  final double cardRadius;

  /// TileGroup's outer and inner (between segments) corners.
  final double groupRadius;
  final double groupInnerRadius;

  /// The fill of metric cards and panels.
  final Color cardColor;

  /// A hairline around cards, or null for tonal cards with no edge.
  final Color? cardBorder;

  /// Space between cards in a grid.
  final double gap;

  /// Inside a metric card.
  final EdgeInsets cardPadding;

  /// The one number a metric card is about, and its unit. [MetricValue]
  /// sets the unit at about half the number's size.
  final TextStyle heroValue;
  final TextStyle heroUnit;

  /// The big number on a metric's detail page.
  final TextStyle detailValue;

  /// A metric card's name ("Processor").
  final TextStyle cardLabel;

  /// Section headers over groups ("Needs attention"). Always sentence
  /// case: no spaced-capital eyebrows.
  final TextStyle sectionLabel;

  /// Facts and specs: "48 °C · 4.2 GHz", sizes, host names.
  final TextStyle data;

  /// Chart scales and time labels (mono in Console).
  final TextStyle chartLabel;

  /// A glyph next to each metric card's name (off in Rack and Console,
  /// where the number leads).
  final bool cardIcons;

  /// Card icons in a tonal circle (Tonal), rather than a bare glyph.
  final bool iconBadge;

  /// TileGroup as one hairline-edged panel with ruled rows (Rack,
  /// Console) instead of separate tonal segments.
  final bool ruledGroups;

  /// Status chips as an outline with a coloured icon and ink text (Rack,
  /// Console) instead of a filled tonal chip.
  final bool outlinedChips;

  /// Usage bars and meters: track height, and ink fill instead of the
  /// primary colour below the warning level.
  final double meterHeight;
  final bool meterInk;

  final ButtonTreatment button;

  /// Home's verdict on a tonal panel whose colour follows the server's
  /// health: the status container when something needs attention, a
  /// neutral container when all is clear (Tonal). False: on the page.
  final bool statusPanel;

  final ChartTokens chart;

  /// Corners for everything that isn't a card or a stock component.
  final Radii radii;

  /// A hairline around TileGroup's separate segments (v2 on true black,
  /// where a near-black fill alone barely registers), or null for none.
  final Color? segmentBorder;

  /// The direction's monospace face (Geist Mono, Plex Mono), or null for
  /// the platform's. Read it through [mono].
  final String? monoFamily;

  /// [base] in the direction's monospace face, for paths, logs, commands
  /// and code. Characters the bundled Latin cuts lack fall back to the
  /// platform's monospace font.
  TextStyle mono(TextStyle? base) => (base ?? const TextStyle()).copyWith(
        fontFamily: monoFamily ?? 'monospace',
        fontFamilyFallback: const ['monospace', 'Roboto'],
      );

  /// The busiest metric's card (Tonal): primary container, so the one
  /// thing working hardest stands out. Null: all cards alike.
  final Color? emphasisCard;
  final Color? onEmphasisCard;

  ShapeBorder cardShape([double? radius]) => RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(radius ?? cardRadius),
        side: cardBorder == null ? BorderSide.none : BorderSide(color: cardBorder!),
      );

  /// The fill for meters and usage bars below their warning level.
  Color meterFill(ColorScheme s) => meterInk ? s.onSurface : s.primary;

  /// These tokens for content drawn on [background] in [foreground] (the
  /// emphasised card): type and card colour follow it.
  DesignTokens on(Color background, Color foreground) {
    final muted = foreground.withValues(alpha: .84);
    return DesignTokens(
      direction: direction,
      cardRadius: cardRadius,
      groupRadius: groupRadius,
      groupInnerRadius: groupInnerRadius,
      cardColor: background,
      cardBorder: null,
      gap: gap,
      cardPadding: cardPadding,
      heroValue: heroValue.copyWith(color: foreground),
      heroUnit: heroUnit.copyWith(color: muted),
      detailValue: detailValue.copyWith(color: foreground),
      cardLabel: cardLabel.copyWith(color: foreground),
      sectionLabel: sectionLabel,
      data: data.copyWith(color: muted),
      chartLabel: chartLabel.copyWith(color: muted),
      cardIcons: cardIcons,
      iconBadge: iconBadge,
      ruledGroups: ruledGroups,
      outlinedChips: outlinedChips,
      meterHeight: meterHeight,
      meterInk: meterInk,
      button: button,
      statusPanel: statusPanel,
      chart: chart,
      radii: radii,
      segmentBorder: segmentBorder,
      monoFamily: monoFamily,
    );
  }

  static DesignTokens of(BuildContext context) {
    final t = Theme.of(context);
    return t.extension<DesignTokens>() ?? fallback(t);
  }

  /// For a bare `ThemeData()` (some tests): the v2 tokens from its scheme.
  static DesignTokens fallback(ThemeData theme) => _fallback(theme, Typography.englishLike2021.merge(theme.textTheme));

  static DesignTokens _fallback(ThemeData t, TextTheme text) => DesignTokens(
        direction: DesignDirection.v2,
        cardRadius: 16,
        groupRadius: 16,
        groupInnerRadius: 4,
        cardColor: t.colorScheme.surfaceContainer,
        cardBorder: null,
        gap: 12,
        cardPadding: const EdgeInsets.all(16),
        heroValue: text.displaySmall!,
        heroUnit: text.titleMedium!,
        detailValue: text.displayMedium!,
        cardLabel: text.labelLarge!,
        sectionLabel: text.titleSmall!.copyWith(color: t.colorScheme.primary),
        data: text.bodySmall!,
        chartLabel: text.labelSmall!,
        cardIcons: true,
        iconBadge: false,
        ruledGroups: false,
        outlinedChips: false,
        meterHeight: 8,
        meterInk: false,
        button: ButtonTreatment.accent,
        statusPanel: false,
        chart: const ChartTokens(lineWidth: 2, fillAlpha: .14, inkLine: false, grid: false, dotRadius: 3, dotRing: true, height: 56, timeLabels: false),
      );

  @override
  DesignTokens copyWith() => this;

  // Theme changes animate colours through ThemeData.lerp; tokens swap at
  // the midpoint (shapes and fonts don't blend usefully).
  @override
  DesignTokens lerp(DesignTokens? other, double t) => other == null || t < .5 ? this : other;
}

/// The style for a secondary, tonal button (`FilledButton.tonal`) in
/// directions whose primary button isn't the M3 default: the theme's
/// filled-button style applies to both variants, so tonal buttons restate
/// their own fill. Null keeps the default.
ButtonStyle? tonalButtonStyle(BuildContext context) {
  final t = DesignTokens.of(context);
  if (t.button == ButtonTreatment.accent || t.button == ButtonTreatment.tonal) return null;
  final s = Theme.of(context).colorScheme;
  return FilledButton.styleFrom(
    backgroundColor: s.secondaryContainer,
    foregroundColor: s.onSecondaryContainer,
    side: BorderSide.none,
  );
}
