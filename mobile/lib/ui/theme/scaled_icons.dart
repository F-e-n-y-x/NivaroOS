import 'dart:math' as math;

import 'package:flutter/material.dart';

/// How much icons grow with the user's text size: 1 at normal text, up to
/// 1.4 at 200% and beyond. Material's icons are fixed at 18-24dp, so at
/// large text they turn into specks next to their labels; growing them
/// with the text, but less, keeps rows and buttons balanced without
/// letting icons take over.
double iconScaleFor(BuildContext context) {
  final text = MediaQuery.textScalerOf(context).scale(14) / 14;
  return math.min(1.4, math.max(1.0, 1 + (text - 1) * 0.4));
}

/// Applies [iconScaleFor] to the theme's icons: plain icons (list rows,
/// app bar actions), button icons and chip icons. `NivaroApp` puts it
/// around every screen; the screenshot harness does the same.
class ScaledIcons extends StatelessWidget {
  const ScaledIcons({super.key, required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    final f = iconScaleFor(context);
    if (f == 1) return child;
    final theme = Theme.of(context);
    final button = ButtonStyle(iconSize: WidgetStatePropertyAll(18 * f));
    ButtonStyle merge(ButtonStyle? s) => s == null ? button : s.merge(button);
    return Theme(
      data: theme.copyWith(
        iconTheme: theme.iconTheme.copyWith(size: 24 * f),
        filledButtonTheme: FilledButtonThemeData(style: merge(theme.filledButtonTheme.style)),
        outlinedButtonTheme: OutlinedButtonThemeData(style: merge(theme.outlinedButtonTheme.style)),
        textButtonTheme: TextButtonThemeData(style: merge(theme.textButtonTheme.style)),
        elevatedButtonTheme: ElevatedButtonThemeData(style: merge(theme.elevatedButtonTheme.style)),
        segmentedButtonTheme: SegmentedButtonThemeData(style: merge(theme.segmentedButtonTheme.style)),
        chipTheme: theme.chipTheme.copyWith(iconTheme: (theme.chipTheme.iconTheme ?? const IconThemeData()).copyWith(size: 18 * f)),
      ),
      child: child,
    );
  }
}
