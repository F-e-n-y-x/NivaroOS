// WCAG 2.2 contrast for every foreground/background pair the design system
// and the stock component themes (StyleComponents) use, in every style ×
// mode (light, dark, true black) × accent (Monochrome included): 4.5:1 for
// text, 3:1 for icons, bars, borders and state marks (design brief §6).
// A failure names the pair and prints its ratio.
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/theme/app_theme.dart';
import 'package:nivaroos_mobile/ui/theme/appearance.dart';
import 'package:nivaroos_mobile/ui/theme/design_tokens.dart';
import 'package:nivaroos_mobile/ui/theme/status_colors.dart';

double _channel(double c) => c <= 0.04045 ? c / 12.92 : math.pow((c + 0.055) / 1.055, 2.4).toDouble();

double _luminance(Color c) => 0.2126 * _channel(c.r) + 0.7152 * _channel(c.g) + 0.0722 * _channel(c.b);

double contrast(Color a, Color b) {
  final la = _luminance(a), lb = _luminance(b);
  return (math.max(la, lb) + 0.05) / (math.min(la, lb) + 0.05);
}

typedef _Pair = (String, Color, Color);

const _none = <WidgetState>{};
const _selected = {WidgetState.selected};

/// A colour as it lands on [bg] (translucent fills and transparent
/// buttons).
Color _on(Color c, Color bg) => Color.alphaBlend(c, bg);

/// The pairs the component themes introduce, in [theme]'s own colours.
/// A null theme value falls back to the M3 default role.
({List<_Pair> text, List<_Pair> nonText}) _components(ThemeData theme) {
  final s = theme.colorScheme;
  final page = s.surface;
  final filled = theme.filledButtonTheme.style!;
  final filledBg = _on(filled.backgroundColor!.resolve(_none)!, page);
  final filledFg = filled.foregroundColor!.resolve(_none)!;
  final outlinedFg = theme.outlinedButtonTheme.style?.foregroundColor?.resolve(_none) ?? s.primary;
  final outlinedSide = theme.outlinedButtonTheme.style?.side?.resolve(_none)?.color ?? s.outline;

  final chip = theme.chipTheme;
  final chipFill = _on(chip.color!.resolve(_selected)!, page);
  final chipLabel = WidgetStateProperty.resolveAs(chip.labelStyle!.color!, _selected);
  final chipIdle = _on(chip.color!.resolve(_none)!, page);
  final chipIdleLabel = WidgetStateProperty.resolveAs(chip.labelStyle!.color!, _none);

  final seg = theme.segmentedButtonTheme.style!;
  final segFill = _on(seg.backgroundColor!.resolve(_selected)!, page);
  final segLabel = seg.foregroundColor!.resolve(_selected)!;

  final sw = theme.switchTheme;
  final onTrack = sw.trackColor?.resolve(_selected) ?? s.primary;
  final onThumb = sw.thumbColor?.resolve(_selected) ?? s.onPrimary;
  final offTrack = sw.trackColor?.resolve(_none) ?? s.surfaceContainerHighest;
  final offThumb = sw.thumbColor?.resolve(_none) ?? s.outline;
  final offOutline = sw.trackOutlineColor?.resolve(_none) ?? s.outline;

  final checkFill = theme.checkboxTheme.fillColor!.resolve(_selected)!;
  final check = theme.checkboxTheme.checkColor!.resolve(_selected)!;
  final checkEdge = WidgetStateProperty.resolveAs<BorderSide?>(theme.checkboxTheme.side, _none)!.color;
  final radio = theme.radioTheme.fillColor!.resolve(_selected)!;

  final fab = theme.floatingActionButtonTheme;
  final fabBg = fab.backgroundColor ?? s.primaryContainer;
  final fabFg = fab.foregroundColor ?? s.onPrimaryContainer;

  final nav = theme.navigationBarTheme;
  final navBg = nav.backgroundColor ?? s.surfaceContainer;
  final navSelected = nav.labelTextStyle!.resolve(_selected)!.color!;
  final navIdle = nav.labelTextStyle!.resolve(_none)!.color!;
  final navIcon = nav.iconTheme!.resolve(_selected)!.color!;
  final navIdleIcon = nav.iconTheme!.resolve(_none)!.color!;

  final snack = theme.snackBarTheme;
  final tooltip = (theme.tooltipTheme.decoration! as BoxDecoration).color!;
  final dialogBg = theme.dialogTheme.backgroundColor ?? s.surfaceContainerHigh;
  final sheetBg = theme.bottomSheetTheme.backgroundColor ?? s.surfaceContainerLow;
  final menuBg = theme.popupMenuTheme.color!;
  final input = theme.inputDecorationTheme;
  final inputBg = input.filled ? input.fillColor! : page;
  final tab = theme.tabBarTheme;
  final tabLine = (tab.indicator! as UnderlineTabIndicator).borderSide.color;
  final progress = theme.progressIndicatorTheme;
  final slider = theme.sliderTheme;

  return (
    text: [
      ('filled button label', filledFg, filledBg),
      ('outlined button label', outlinedFg, page),
      ('text button label', s.primary, page),
      ('selected chip label', chipLabel, chipFill),
      ('unselected chip label', chipIdleLabel, chipIdle),
      ('selected segment label', segLabel, segFill),
      ('FAB label', fabFg, fabBg),
      ('navigation label, selected', navSelected, navBg),
      ('navigation label', navIdle, navBg),
      ('snack bar text', snack.contentTextStyle!.color!, snack.backgroundColor!),
      ('snack bar action', snack.actionTextColor!, snack.backgroundColor!),
      ('tooltip', theme.tooltipTheme.textStyle!.color!, tooltip),
      ('dialog title', theme.dialogTheme.titleTextStyle!.color!, dialogBg),
      ('dialog text', theme.dialogTheme.contentTextStyle!.color!, dialogBg),
      ('sheet text', s.onSurfaceVariant, sheetBg),
      ('menu item', theme.popupMenuTheme.labelTextStyle!.resolve(_none)!.color!, menuBg),
      ('field label', input.labelStyle!.color!, inputBg),
      ('field hint', input.hintStyle!.color!, inputBg),
      ('tab label', tab.labelColor!, page),
      ('tab label, unselected', tab.unselectedLabelColor!, page),
      ('list trailing value', theme.listTileTheme.leadingAndTrailingTextStyle!.color!, page),
      ('slider value label', slider.valueIndicatorTextStyle!.color!, slider.valueIndicatorColor!),
    ],
    nonText: [
      // A state must read without colour alone, but where colour marks it,
      // it must show (WCAG 1.4.11): on against off, thumb against track.
      ('switch on: track on page', onTrack, page),
      ('switch on: thumb on track', onThumb, onTrack),
      ('switch off: thumb on track', offThumb, offTrack),
      ('switch off: track edge on page', offOutline, page),
      ('switch on track against off track', onTrack, offTrack),
      ('checkbox: fill on page', checkFill, page),
      ('checkbox: check on fill', check, checkFill),
      ('checkbox: empty box on page', checkEdge, page),
      ('radio: selected on page', radio, page),
      ('outlined button edge', outlinedSide, page),
      ('FAB icon', fabFg, fabBg),
      ('navigation icon, selected', navIcon, nav.indicatorColor!),
      ('navigation icon', navIdleIcon, navBg),
      ('text field edge', input.enabledBorder!.borderSide.color, inputBg),
      ('text field focus', input.focusedBorder!.borderSide.color, inputBg),
      ('tab indicator', tabLine, page),
      ('progress on its track', progress.color!, progress.linearTrackColor ?? s.secondaryContainer),
      ('slider value on its track', slider.activeTrackColor!, slider.inactiveTrackColor ?? s.secondaryContainer),
    ],
  );
}

void main() {
  test('contrast of a known pair matches the WCAG formula', () {
    expect(contrast(Colors.black, Colors.white), closeTo(21, 0.01));
    expect(contrast(const Color(0xFF767676), Colors.white), closeTo(4.54, 0.01));
  });

  // Every theme the app can show: each design direction, each accent, in
  // light, dark and true black.
  final themes = <String, ThemeData>{
    for (final d in DesignDirection.values)
      for (final a in AccentColor.values)
        for (final (mode, b, black) in const [('light', Brightness.light, false), ('dark', Brightness.dark, false), ('black', Brightness.dark, true)])
          '${d.name} ${a.name} $mode': AppTheme.build(brightness: b, black: black, accent: a, direction: d),
  };
  for (final MapEntry(key: name, value: theme) in themes.entries) {
    final s = theme.colorScheme;
    final st = theme.extension<StatusColors>()!;
    final tk = theme.extension<DesignTokens>()!;
    final surfaces = {
      'surface': s.surface,
      'surfaceContainerLowest': s.surfaceContainerLowest,
      'surfaceContainerLow': s.surfaceContainerLow,
      'surfaceContainer': s.surfaceContainer,
      'surfaceContainerHigh': s.surfaceContainerHigh,
      'surfaceContainerHighest': s.surfaceContainerHighest,
    };
    final tones = {
      'success': st.success,
      'warning': st.warning,
      'info': st.info,
      'error': st.resolve(Status.error, s),
      'neutral': st.resolve(Status.neutral, s),
    };

    final text = <_Pair>[
      for (final e in surfaces.entries) ...[
        ('onSurface on ${e.key}', s.onSurface, e.value),
        ('onSurfaceVariant on ${e.key}', s.onSurfaceVariant, e.value),
        ('primary on ${e.key}', s.primary, e.value),
        ('error on ${e.key}', s.error, e.value),
        for (final t in tones.entries) ('${t.key} on ${e.key}', t.value.color, e.value),
      ],
      ('onPrimary on primary', s.onPrimary, s.primary),
      ('onPrimaryContainer on primaryContainer', s.onPrimaryContainer, s.primaryContainer),
      ('onSecondaryContainer on secondaryContainer', s.onSecondaryContainer, s.secondaryContainer),
      ('onInverseSurface on inverseSurface', s.onInverseSurface, s.inverseSurface),
      for (final t in tones.entries) ...[
        ('on-${t.key} on ${t.key}', t.value.onColor, t.value.color),
        ('on-${t.key}-container on ${t.key}-container', t.value.onContainer, t.value.container),
      ],
    ];

    final nonText = <_Pair>[
      ('outline on surface', s.outline, s.surface),
      ('outline on surfaceContainerLow', s.outline, s.surfaceContainerLow),
      // Usage bar fills against their track.
      ('primary bar on track', s.primary, s.surfaceContainerHighest),
      ('warning bar on track', st.warning.color, s.surfaceContainerHighest),
      ('error bar on track', s.error, s.surfaceContainerHighest),
      // Status icons on their chip.
      for (final t in tones.entries) ('${t.key} icon on ${t.key}-container', t.value.onContainer, t.value.container),
      // Bars and status marks inside a TileGroup segment.
      ('primary on surfaceContainer', s.primary, s.surfaceContainer),
      for (final t in tones.entries) ('${t.key} on surfaceContainer', t.value.color, s.surfaceContainer),
      // The offline banner's icon on its fill (its text is in the text list).
      ('onSurfaceVariant icon on surfaceContainerHighest', s.onSurfaceVariant, s.surfaceContainerHighest),
      // Home's status disc in dark theme: the status colour on an 18% wash
      // of itself over the tonal panel (StatusDisc).
      if (theme.brightness == Brightness.dark)
        for (final t in tones.entries)
          ('${t.key} disc icon on its wash', t.value.color, Color.alphaBlend(t.value.color.withValues(alpha: 0.18), s.surfaceContainerHigh)),
      // Sparklines on the tonal panels.
      ('primary line on surfaceContainerHigh', s.primary, s.surfaceContainerHigh),
      // Metric cards: the chart lines (main and second series) and the
      // storage bars on the card colour.
      ('chart line on card', tk.chart.inkLine ? s.onSurface : s.primary, tk.cardColor),
      ('second chart line on card', tk.chart.inkLine || tk.chart.grid ? s.onSurfaceVariant : s.tertiary, tk.cardColor),
      ('warning line on card', st.warning.color, tk.cardColor),
      ('error line on card', s.error, tk.cardColor),
      ('meter bar on its track', tk.meterFill(s), s.surfaceContainerHighest),
      ('meter bar on card', tk.meterFill(s), tk.cardColor),
      if (tk.emphasisCard != null) ('chart line on the emphasised card', tk.onEmphasisCard!, tk.emphasisCard!),
      // Outlined status chips: the status icon on the page and on cards.
      if (tk.outlinedChips)
        for (final t in tones.entries) ...[
          ('${t.key} chip icon on card', t.value.color, tk.cardColor),
        ],
    ];
    text.addAll([
      ('card label on card', tk.cardLabel.color!, tk.cardColor),
      ('card value on card', tk.heroValue.color!, tk.cardColor),
      ('card unit on card', tk.heroUnit.color!, tk.cardColor),
      ('card facts on card', tk.data.color!, tk.cardColor),
      ('section header on page', tk.sectionLabel.color!, s.surface),
      ('chart label on card', tk.chartLabel.color!, tk.cardColor),
      // The emphasised metric card (Tonal): its type and muted type.
      if (tk.emphasisCard != null) ...[
        ('emphasised card text', tk.onEmphasisCard!, tk.emphasisCard!),
        ('emphasised card muted text', Color.alphaBlend(tk.onEmphasisCard!.withValues(alpha: .84), tk.emphasisCard!), tk.emphasisCard!),
      ],
      // Tonal's Home verdict panel in dark theme: ink on a wash of the
      // status colour.
      if (tk.statusPanel && theme.brightness == Brightness.dark)
        for (final t in tones.entries) ...[
          ('verdict on the ${t.key} wash', s.onSurface, Color.alphaBlend(t.value.color.withValues(alpha: .16), s.surfaceContainerHigh)),
          ('verdict facts on the ${t.key} wash', s.onSurfaceVariant, Color.alphaBlend(t.value.color.withValues(alpha: .16), s.surfaceContainerHigh)),
        ],
      // Rack's ink button and Console's outlined one.
      if (tk.button == ButtonTreatment.ink) ('ink button label', s.surface, s.onSurface),
      if (tk.button == ButtonTreatment.outlined) ('outlined button label', s.primary, s.surface),
    ]);

    // The stock components as each style draws them (StyleComponents),
    // read from the theme itself so a change there is checked here. v2
    // keeps the Material defaults, which M3 already guarantees.
    if (tk.direction != DesignDirection.v2) {
      final c = _components(theme);
      text.addAll(c.text);
      nonText.addAll(c.nonText);
    }

    // The floating navigation bar in every style, v2 included: its labels
    // and icons on its own fill (the theme's navigation bar colour is the
    // bar's), and the selected icon on its indicator.
    final bar = tk.navBar;
    final nav = theme.navigationBarTheme;
    final navIndicator = nav.indicatorColor ?? s.secondaryContainer;
    text.addAll([
      ('floating bar label, selected', nav.labelTextStyle?.resolve(_selected)?.color ?? s.onSurface, bar.color),
      ('floating bar label', nav.labelTextStyle?.resolve(_none)?.color ?? s.onSurfaceVariant, bar.color),
    ]);
    nonText.addAll([
      ('floating bar icon', nav.iconTheme?.resolve(_none)?.color ?? s.onSurfaceVariant, bar.color),
      ('floating bar icon, selected', nav.iconTheme?.resolve(_selected)?.color ?? s.onSecondaryContainer, navIndicator),
    ]);

    // Not a WCAG pair (the bar is a container, not a control): pins that
    // the floating bar stays visibly apart from the page and from the
    // cards scrolling under it - by its edge, a tonal step or, in light,
    // its shadow - and that true black always draws an edge.
    test('$name: the floating bar stands apart from the page and the cards', () {
      expect(nav.backgroundColor, bar.color, reason: 'the bar and its theme disagree on the fill');
      final light = theme.brightness == Brightness.light;
      final black = s.surface == Colors.black;
      final edge = bar.edge;
      if (edge != null) {
        final r = contrast(edge, s.surface);
        expect(r, greaterThanOrEqualTo(1.2), reason: 'bar edge on the page is ${r.toStringAsFixed(3)}:1');
      } else {
        final r = contrast(bar.color, s.surface);
        expect(r, greaterThanOrEqualTo(1.1), reason: 'bar fill on the page is ${r.toStringAsFixed(3)}:1');
      }
      final overCards = edge != null || (light && bar.elevation > 0) || contrast(bar.color, tk.cardColor) >= 1.1;
      expect(overCards, isTrue, reason: 'nothing sets the bar apart from the cards under it');
      if (black) expect(edge, isNotNull, reason: 'true black draws the bar\'s edge');
      if (!light) expect(bar.elevation, 0, reason: 'no shadow in dark, where it would not show');
    });

    // Not a WCAG pair: pins the choice of TileGroup segment colour on the
    // page, so they stay visibly apart (surfaceContainerLow was 1.05:1 and
    // read as one flat sheet).
    test('$name: TileGroup segments stand apart from the page', () {
      final r = contrast(s.surfaceContainer, s.surface);
      expect(r, greaterThanOrEqualTo(1.1), reason: 'surfaceContainer on surface is ${r.toStringAsFixed(3)}:1');
    });

    for (final (label, fg, bg) in text) {
      test('$name text: $label ≥ 4.5', () {
        final r = contrast(fg, bg);
        expect(r, greaterThanOrEqualTo(4.5), reason: '$label is ${r.toStringAsFixed(2)}:1');
      });
    }
    for (final (label, fg, bg) in nonText) {
      test('$name non-text: $label ≥ 3', () {
        final r = contrast(fg, bg);
        expect(r, greaterThanOrEqualTo(3), reason: '$label is ${r.toStringAsFixed(2)}:1');
      });
    }
  }
}
