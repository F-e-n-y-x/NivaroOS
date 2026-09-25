// WCAG 2.2 contrast for every foreground/background pair the design system
// uses, in both themes: 4.5:1 for text, 3:1 for icons, bars and borders
// (design brief §6). A failure names the pair and prints its ratio.
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/theme/app_theme.dart';
import 'package:nivaroos_mobile/ui/theme/status_colors.dart';

double _channel(double c) => c <= 0.04045 ? c / 12.92 : math.pow((c + 0.055) / 1.055, 2.4).toDouble();

double _luminance(Color c) => 0.2126 * _channel(c.r) + 0.7152 * _channel(c.g) + 0.0722 * _channel(c.b);

double contrast(Color a, Color b) {
  final la = _luminance(a), lb = _luminance(b);
  return (math.max(la, lb) + 0.05) / (math.min(la, lb) + 0.05);
}

typedef _Pair = (String, Color, Color);

void main() {
  test('contrast of a known pair matches the WCAG formula', () {
    expect(contrast(Colors.black, Colors.white), closeTo(21, 0.01));
    expect(contrast(const Color(0xFF767676), Colors.white), closeTo(4.54, 0.01));
  });

  for (final theme in [AppTheme.light(), AppTheme.dark()]) {
    final s = theme.colorScheme;
    final st = theme.extension<StatusColors>()!;
    final name = theme.brightness.name;
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
    ];

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
