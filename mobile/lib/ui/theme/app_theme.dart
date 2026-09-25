import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'status_colors.dart';

/// The app's two themes (design brief §3): Material 3 built from the
/// NivaroOS blue, with component themes only where a default needs a nudge.
///
/// Colours come from `Theme.of(context).colorScheme` and
/// `StatusColors.of(context)`; type from `Theme.of(context).textTheme`.
/// Nothing in a screen should need a colour or font size of its own.
abstract final class AppTheme {
  /// NivaroOS blue, the same seed the web UI uses. The scheme uses the M3
  /// default (tonal spot) variant, so the primary role is a calmer tone of
  /// this blue rather than the raw hex.
  static const seed = Color(0xFF2563EB);

  // Built once: ThemeData is immutable and fromSeed is not free.
  static final ThemeData _light = _build(Brightness.light);
  static final ThemeData _dark = _build(Brightness.dark);

  static ThemeData light() => _light;
  static ThemeData dark() => _dark;

  static ThemeData forBrightness(Brightness brightness) =>
      brightness == Brightness.dark ? dark() : light();

  /// The same theme built with another scheme variant, so the owner can
  /// compare the seed choice from pixels (the `gallery_*_fidelity`
  /// screenshots). The app itself always uses [light] and [dark].
  @visibleForTesting
  static ThemeData withVariant(Brightness brightness, DynamicSchemeVariant variant) => _build(brightness, variant);

  /// Status and navigation bar styling for [brightness]: transparent bars
  /// (the app draws edge to edge) with icons that contrast with the app's
  /// theme, not the phone's. App bars apply it through [AppBarTheme];
  /// `NivaroApp` applies it app-wide for screens without an app bar.
  static SystemUiOverlayStyle systemBarsStyle(Brightness brightness) {
    final icons = brightness == Brightness.dark ? Brightness.light : Brightness.dark;
    return SystemUiOverlayStyle(
      statusBarColor: Colors.transparent,
      systemNavigationBarColor: Colors.transparent,
      systemNavigationBarDividerColor: Colors.transparent,
      systemNavigationBarContrastEnforced: false,
      statusBarIconBrightness: icons,
      statusBarBrightness: brightness,
      systemNavigationBarIconBrightness: icons,
    );
  }

  static ThemeData _build(Brightness brightness, [DynamicSchemeVariant variant = DynamicSchemeVariant.tonalSpot]) {
    // Owner decision (2026-09-25): actions carry the exact NivaroOS blue the
    // web UI uses, while containers, the navigation indicator and surfaces
    // keep the calm tonal-spot palette. So primary/onPrimary come from the
    // fidelity scheme and everything else from the requested variant.
    final base = ColorScheme.fromSeed(seedColor: seed, brightness: brightness, dynamicSchemeVariant: variant);
    final brand = ColorScheme.fromSeed(seedColor: seed, brightness: brightness, dynamicSchemeVariant: DynamicSchemeVariant.fidelity);
    final scheme = variant == DynamicSchemeVariant.tonalSpot
        ? base.copyWith(primary: brand.primary, onPrimary: brand.onPrimary, surfaceTint: brand.primary)
        : base;
    final isDark = brightness == Brightness.dark;

    return ThemeData(
      useMaterial3: true,
      colorScheme: scheme,
      extensions: [isDark ? StatusColors.dark : StatusColors.light],
      appBarTheme: AppBarTheme(centerTitle: false, systemOverlayStyle: systemBarsStyle(brightness)),
      // Symmetric padding so trailing values line up with the 16dp phone
      // gutter. AppScaffold widens it to the pane's gutter on tablets, and
      // TileGroup sets it back to 16 inside its cards.
      listTileTheme: const ListTileThemeData(
        contentPadding: EdgeInsets.symmetric(horizontal: 16),
      ),
      cardTheme: const CardThemeData(margin: EdgeInsets.zero, clipBehavior: Clip.antiAlias),
      snackBarTheme: const SnackBarThemeData(behavior: SnackBarBehavior.floating),
      inputDecorationTheme: const InputDecorationThemeData(border: OutlineInputBorder()),
      // The current M3 look for progress and sliders (rounded, with a gap
      // and stop indicator) instead of the 2023 one Flutter still defaults
      // to. The flag itself is marked deprecated because it will go away
      // once false is the default; setting it to false is the documented
      // way to opt in until then.
      // ignore: deprecated_member_use
      progressIndicatorTheme: const ProgressIndicatorThemeData(year2023: false),
      // ignore: deprecated_member_use
      sliderTheme: const SliderThemeData(year2023: false),
    );
  }
}

/// Tabular (fixed-width) digits, for numbers that change in place - rates,
/// percentages, counters - so they don't jitter as they update.
///
///   Text('42%', style: textTheme.titleMedium?.tabular)
extension TabularFigures on TextStyle {
  TextStyle get tabular => copyWith(fontFeatures: const [FontFeature.tabularFigures()]);
}

/// The M3 Expressive "emphasized" weight for the one number or name that
/// matters on a screen (the server's name on Home, a key value): the same
/// role, heavier. Use it once per screen, not for every label.
extension EmphasizedType on TextStyle {
  TextStyle get emphasized => copyWith(fontWeight: FontWeight.w600);
}
