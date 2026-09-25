import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'appearance.dart';
import 'design_tokens.dart';
import 'status_colors.dart';

/// The app's themes (design brief §3 and "Design system v3"): Material 3
/// from a seed colour, drawn in one of the [DesignDirection]s, in light,
/// dark or true black.
///
/// Colours come from `Theme.of(context).colorScheme` and
/// `StatusColors.of(context)`; type from `Theme.of(context).textTheme`;
/// shapes and the metric card/chart style from `DesignTokens.of(context)`.
/// Nothing in a screen should need a colour or font size of its own.
abstract final class AppTheme {
  /// NivaroOS blue, the same seed the web UI uses. The scheme uses the M3
  /// default (tonal spot) variant, so the primary role is a calmer tone of
  /// this blue rather than the raw hex.
  static const seed = Color(0xFF2563EB);

  // ThemeData is immutable and fromSeed is not free: one per combination.
  static final Map<String, ThemeData> _cache = {};

  /// The v2 themes with the brand blue, as before.
  static ThemeData light() => build(brightness: Brightness.light);
  static ThemeData dark() => build(brightness: Brightness.dark);

  static ThemeData forBrightness(Brightness brightness) =>
      brightness == Brightness.dark ? dark() : light();

  /// The theme for [appearance] at [brightness]: the black variant when
  /// the user chose true black and the theme is dark, the wallpaper's
  /// colour when they chose it and the phone has one.
  static ThemeData forAppearance(Appearance appearance, Brightness brightness, {Color? wallpaperSeed}) => build(
        brightness: brightness,
        black: brightness == Brightness.dark && appearance.mode == AppThemeMode.black,
        accent: appearance.accent,
        seedOverride: appearance.wallpaper ? wallpaperSeed : null,
        direction: appearance.direction,
      );

  /// One theme. [seedOverride] (the wallpaper's key colour) replaces the
  /// [accent]'s seed and is used as the system uses it: plain tonal spot.
  static ThemeData build({
    required Brightness brightness,
    bool black = false,
    AccentColor accent = AccentColor.blue,
    Color? seedOverride,
    DesignDirection direction = DesignDirection.defaultDirection,
  }) {
    final isBlack = black && brightness == Brightness.dark;
    final key = '${brightness.name}|$isBlack|${seedOverride?.toARGB32() ?? accent.name}|${direction.name}';
    // Console draws lime as a thin signal line on graphite; the raw seed
    // is close to neon there, so it gets a slightly deeper, calmer lime.
    final seed = seedOverride ?? (direction == DesignDirection.console && accent == AccentColor.lime ? const Color(0xFFB5D334) : accent.seed);
    return _cache[key] ??= _build(brightness, isBlack, seed, seedOverride == null && accent == AccentColor.blue, direction);
  }

  /// The same theme built with another scheme variant, so the owner can
  /// compare the seed choice from pixels (the `gallery_*_fidelity`
  /// screenshots). The app itself never uses it.
  @visibleForTesting
  static ThemeData withVariant(Brightness brightness, DynamicSchemeVariant variant) =>
      _build(brightness, false, seed, variant == DynamicSchemeVariant.tonalSpot, DesignDirection.v2, variant);

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

  static ColorScheme scheme(Brightness brightness, bool black, Color seed, bool brand, DesignDirection direction,
      [DynamicSchemeVariant variant = DynamicSchemeVariant.tonalSpot]) {
    var s = ColorScheme.fromSeed(seedColor: seed, brightness: brightness, dynamicSchemeVariant: variant);
    // Owner decision (2026-09-25): with the brand blue, actions carry the
    // exact NivaroOS blue the web UI uses, while containers, the navigation
    // indicator and surfaces keep the calm tonal-spot palette. So
    // primary/onPrimary come from the fidelity scheme. Other accents and
    // the wallpaper use plain tonal spot, as the system does.
    if (brand && variant == DynamicSchemeVariant.tonalSpot) {
      final fidelity = ColorScheme.fromSeed(seedColor: seed, brightness: brightness, dynamicSchemeVariant: DynamicSchemeVariant.fidelity);
      s = s.copyWith(primary: fidelity.primary, onPrimary: fidelity.onPrimary, surfaceTint: fidelity.primary);
    }
    // Console keeps the accent's own chroma (a signal colour on graphite);
    // tonal spot would mute lime to olive. Contrast holds: fidelity keeps
    // the same tones.
    if (direction == DesignDirection.console && !brand && variant == DynamicSchemeVariant.tonalSpot) {
      final fidelity = ColorScheme.fromSeed(seedColor: seed, brightness: brightness, dynamicSchemeVariant: DynamicSchemeVariant.fidelity);
      s = s.copyWith(primary: fidelity.primary, onPrimary: fidelity.onPrimary, primaryContainer: fidelity.primaryContainer, onPrimaryContainer: fidelity.onPrimaryContainer);
    }
    final dark = brightness == Brightness.dark;
    s = switch (direction) {
      DesignDirection.rack => _neutrals(s, dark ? (black ? _rackBlack : _rackDark) : _rackLight),
      DesignDirection.console => _neutrals(s, dark ? (black ? _consoleBlack : _consoleDark) : _consoleLight),
      _ => black ? _tonalBlack(s) : s,
    };
    return s;
  }

  /// True black for the tonal directions: only the backdrop is #000; the
  /// containers sit on a near-black ladder with a trace of the accent, so
  /// cards, sheets and menus stay visible and fewer pixels switch fully
  /// off and on while scrolling (black smear).
  static ColorScheme _tonalBlack(ColorScheme s) {
    Color step(double a, int base) => Color.alphaBlend(s.primary.withValues(alpha: a), Color(base));
    return s.copyWith(
      surface: Colors.black,
      surfaceDim: Colors.black,
      surfaceContainerLowest: Colors.black,
      surfaceContainerLow: step(.04, 0xFF0A0A0A),
      surfaceContainer: step(.05, 0xFF111111),
      surfaceContainerHigh: step(.06, 0xFF191919),
      surfaceContainerHighest: step(.07, 0xFF232323),
      surfaceTint: Colors.transparent,
    );
  }

  static ColorScheme _neutrals(ColorScheme s, _Neutrals n) => s.copyWith(
        surface: n.surface,
        surfaceDim: n.dim,
        surfaceBright: n.bright,
        surfaceContainerLowest: n.lowest,
        surfaceContainerLow: n.low,
        surfaceContainer: n.container,
        surfaceContainerHigh: n.high,
        surfaceContainerHighest: n.highest,
        onSurface: n.onSurface,
        onSurfaceVariant: n.onSurfaceVariant,
        outline: n.outline,
        outlineVariant: n.outlineVariant,
        inverseSurface: n.inverseSurface,
        onInverseSurface: n.onInverseSurface,
        secondaryContainer: n.neutralContainer,
        onSecondaryContainer: n.onSurface,
        surfaceTint: Colors.transparent,
      );

  // Rack (direction A): warm paper and graphite. Cards sit one step
  // lighter than the page (not pure white), with a hairline edge;
  // test/ui/theme_contrast_test.dart checks every pair.
  static const _rackLight = _Neutrals(
    surface: Color(0xFFEEEBE3),
    dim: Color(0xFFE2DED4),
    bright: Color(0xFFFBFAF6),
    lowest: Color(0xFFFBFAF6),
    low: Color(0xFFF4F2EC),
    container: Color(0xFFF8F6F1),
    high: Color(0xFFF8F6F1),
    highest: Color(0xFFE5E1D8),
    onSurface: Color(0xFF1D1C1A),
    onSurfaceVariant: Color(0xFF5F5B53),
    outline: Color(0xFF857F74),
    outlineVariant: Color(0xFFD9D4C9),
    inverseSurface: Color(0xFF2E2D2A),
    onInverseSurface: Color(0xFFEEEBE3),
    neutralContainer: Color(0xFFE3DED3),
  );
  // Warm graphite rather than neutral grey, so the warmth survives the
  // dark theme; the ink is a warm off-white.
  static const _rackDark = _Neutrals(
    surface: Color(0xFF161513),
    dim: Color(0xFF161513),
    bright: Color(0xFF3A3833),
    lowest: Color(0xFF100F0E),
    low: Color(0xFF1C1B18),
    container: Color(0xFF22201C),
    high: Color(0xFF282622),
    highest: Color(0xFF32302A),
    onSurface: Color(0xFFECE8E0),
    onSurfaceVariant: Color(0xFFA6A196),
    outline: Color(0xFF7F7A70),
    outlineVariant: Color(0xFF36342E),
    inverseSurface: Color(0xFFECE8E0),
    onInverseSurface: Color(0xFF2E2D2A),
    neutralContainer: Color(0xFF38362F),
  );
  static const _rackBlack = _Neutrals(
    surface: Color(0xFF000000),
    dim: Color(0xFF000000),
    bright: Color(0xFF2E2C28),
    lowest: Color(0xFF000000),
    low: Color(0xFF0A0A09),
    container: Color(0xFF121110),
    high: Color(0xFF1A1917),
    highest: Color(0xFF25231F),
    onSurface: Color(0xFFE8E4DC),
    onSurfaceVariant: Color(0xFF9F9A90),
    outline: Color(0xFF79746A),
    outlineVariant: Color(0xFF2E2C27),
    inverseSurface: Color(0xFFECE8E0),
    onInverseSurface: Color(0xFF2E2D2A),
    neutralContainer: Color(0xFF2C2A25),
  );

  // Console (direction C): cool graphite. Light is graphite on grey too,
  // not white cards on off-white.
  static const _consoleLight = _Neutrals(
    surface: Color(0xFFE9EAE6),
    dim: Color(0xFFDCDDD8),
    bright: Color(0xFFF7F8F5),
    lowest: Color(0xFFF7F8F5),
    low: Color(0xFFF0F1ED),
    container: Color(0xFFF4F5F2),
    high: Color(0xFFF4F5F2),
    highest: Color(0xFFDFE1DC),
    onSurface: Color(0xFF202224),
    onSurfaceVariant: Color(0xFF52575D),
    outline: Color(0xFF787D83),
    outlineVariant: Color(0xFFD0D3CD),
    inverseSurface: Color(0xFF25282B),
    onInverseSurface: Color(0xFFE9EAE6),
    neutralContainer: Color(0xFFDADCD6),
  );
  static const _consoleDark = _Neutrals(
    surface: Color(0xFF0B0C0D),
    dim: Color(0xFF0B0C0D),
    bright: Color(0xFF2C3034),
    lowest: Color(0xFF070808),
    low: Color(0xFF121416),
    container: Color(0xFF181A1D),
    high: Color(0xFF1D2023),
    highest: Color(0xFF272B2F),
    onSurface: Color(0xFFE8EAED),
    onSurfaceVariant: Color(0xFF8F959C),
    outline: Color(0xFF6B7179),
    outlineVariant: Color(0xFF262A2E),
    inverseSurface: Color(0xFFE8EAED),
    onInverseSurface: Color(0xFF181A1D),
    neutralContainer: Color(0xFF2A2E33),
  );
  static const _consoleBlack = _Neutrals(
    surface: Color(0xFF000000),
    dim: Color(0xFF000000),
    bright: Color(0xFF26292D),
    lowest: Color(0xFF000000),
    low: Color(0xFF0A0B0C),
    container: Color(0xFF111315),
    high: Color(0xFF171A1C),
    highest: Color(0xFF212427),
    onSurface: Color(0xFFE2E4E7),
    onSurfaceVariant: Color(0xFF878D94),
    outline: Color(0xFF666C74),
    outlineVariant: Color(0xFF212427),
    inverseSurface: Color(0xFFE8EAED),
    onInverseSurface: Color(0xFF181A1D),
    neutralContainer: Color(0xFF24282C),
  );

  static String? _family(DesignDirection d) => switch (d) {
        DesignDirection.v2 => null,
        DesignDirection.rack => 'Geist',
        DesignDirection.tonal => 'GoogleSansFlex',
        DesignDirection.console => 'IBMPlexSans',
      };

  static String? _mono(DesignDirection d) => switch (d) {
        DesignDirection.rack => 'GeistMono',
        DesignDirection.console => 'IBMPlexMono',
        _ => null,
      };

  static ThemeData _build(Brightness brightness, bool black, Color seed, bool brand, DesignDirection direction,
      [DynamicSchemeVariant variant = DynamicSchemeVariant.tonalSpot]) {
    final s = scheme(brightness, black, seed, brand, direction, variant);
    final isDark = brightness == Brightness.dark;
    final family = _family(direction);
    final flat = direction == DesignDirection.rack || direction == DesignDirection.console || black;

    // Shapes per direction: Rack precise (12-14), Tonal soft (M3 Expressive
    // large), Console tight (6-10); v2 keeps the M3 defaults.
    final (double button, double chip, double dialog, double sheet) = switch (direction) {
      DesignDirection.v2 => (-1, 8, 28, 28),
      DesignDirection.rack => (12, 8, 20, 24),
      DesignDirection.tonal => (-1, 12, 28, 28),
      DesignDirection.console => (8, 6, 14, 16),
    };
    final OutlinedBorder? shape = button < 0 ? null : RoundedRectangleBorder(borderRadius: BorderRadius.circular(button));
    final buttonStyle = ButtonStyle(shape: shape == null ? null : WidgetStatePropertyAll(shape));
    // v2 keeps Flutter's defaults untouched, so its screens render exactly
    // as the 1.3 build did.
    final custom = direction != DesignDirection.v2;

    // The primary button is part of each direction's identity: ink in Rack,
    // tonal in Tonal, an accent outline with a mono label in Console.
    final mono = _mono(direction);
    final ButtonStyle? filled = switch (direction) {
      DesignDirection.rack => FilledButton.styleFrom(
          backgroundColor: s.onSurface,
          foregroundColor: s.surface,
          disabledBackgroundColor: s.onSurface.withValues(alpha: .12),
          disabledForegroundColor: s.onSurface.withValues(alpha: .38),
          shape: shape,
        ),
      DesignDirection.tonal => FilledButton.styleFrom(backgroundColor: s.primaryContainer, foregroundColor: s.onPrimaryContainer),
      DesignDirection.console => FilledButton.styleFrom(
          backgroundColor: Colors.transparent,
          foregroundColor: s.primary,
          side: BorderSide(color: s.primary, width: 1.5),
          textStyle: TextStyle(fontFamily: mono, fontWeight: FontWeight.w500, fontSize: 14, letterSpacing: .2),
          shape: shape,
        ),
      DesignDirection.v2 => null,
    };

    final base = ThemeData(
      useMaterial3: true,
      colorScheme: s,
      fontFamily: family,
      fontFamilyFallback: family == null ? null : const ['Roboto'],
      appBarTheme: AppBarTheme(
        centerTitle: false,
        systemOverlayStyle: systemBarsStyle(brightness),
        // Flat directions and true black keep the bar the page colour when
        // content scrolls under it (no tint: on black it would turn navy).
        backgroundColor: flat ? s.surface : null,
        surfaceTintColor: flat ? Colors.transparent : null,
        scrolledUnderElevation: flat ? 0 : null,
      ),
      // Symmetric padding so trailing values line up with the 16dp phone
      // gutter. AppScaffold widens it to the pane's gutter on tablets, and
      // TileGroup sets it back to 16 inside its cards.
      listTileTheme: const ListTileThemeData(contentPadding: EdgeInsets.symmetric(horizontal: 16)),
      cardTheme: const CardThemeData(margin: EdgeInsets.zero, clipBehavior: Clip.antiAlias),
      snackBarTheme: const SnackBarThemeData(behavior: SnackBarBehavior.floating),
      inputDecorationTheme: InputDecorationThemeData(
        border: custom
            ? OutlineInputBorder(borderRadius: BorderRadius.circular(direction == DesignDirection.console ? 6 : direction == DesignDirection.rack ? 10 : 4))
            : const OutlineInputBorder(),
      ),
      filledButtonTheme: filled == null ? null : FilledButtonThemeData(style: filled),
      outlinedButtonTheme: shape == null ? null : OutlinedButtonThemeData(style: buttonStyle),
      textButtonTheme: shape == null ? null : TextButtonThemeData(style: buttonStyle),
      elevatedButtonTheme: shape == null ? null : ElevatedButtonThemeData(style: buttonStyle),
      segmentedButtonTheme: shape == null ? null : SegmentedButtonThemeData(style: buttonStyle),
      chipTheme: custom ? ChipThemeData(shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(chip), side: BorderSide(color: s.outlineVariant))) : null,
      dividerTheme: flat ? DividerThemeData(color: s.outlineVariant, space: 1, thickness: 1) : null,
      dialogTheme: custom || flat
          ? DialogThemeData(
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(dialog)),
              backgroundColor: flat ? s.surfaceContainerHigh : null,
            )
          : null,
      bottomSheetTheme: custom || flat
          ? BottomSheetThemeData(
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(sheet))),
              backgroundColor: flat ? s.surfaceContainerLow : null,
            )
          : null,
      navigationBarTheme: flat
          ? NavigationBarThemeData(backgroundColor: s.surfaceContainerLow, surfaceTintColor: Colors.transparent, elevation: 0)
          : null,
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

    final text = _text(base.textTheme, direction);
    // ThemeData's text theme carries colours and families only; Theme.of
    // adds the sizes (Typography.englishLike2021) later. The tokens need
    // real sizes, so they are built on the merged theme.
    final sized = Typography.englishLike2021.merge(text);
    return base.copyWith(
      textTheme: text,
      extensions: [isDark ? StatusColors.dark : StatusColors.light, _tokens(direction, s, sized, black)],
    );
  }

  /// Each direction owns its title and headline type, so page titles, app
  /// bars and dialogs change with it, not only the Home cards.
  static TextTheme _text(TextTheme t, DesignDirection d) => switch (d) {
        DesignDirection.v2 => t,
        // Geist Light for page titles and big numbers, set a touch tight.
        DesignDirection.rack => t.copyWith(
            displayLarge: t.displayLarge?.copyWith(fontWeight: FontWeight.w300, letterSpacing: -1.5),
            displayMedium: t.displayMedium?.copyWith(fontWeight: FontWeight.w300, letterSpacing: -1.2),
            displaySmall: t.displaySmall?.copyWith(fontWeight: FontWeight.w300, letterSpacing: -1),
            headlineLarge: t.headlineLarge?.copyWith(fontWeight: FontWeight.w300, letterSpacing: -0.8),
            headlineMedium: t.headlineMedium?.copyWith(fontWeight: FontWeight.w300, letterSpacing: -0.6),
            headlineSmall: t.headlineSmall?.copyWith(fontWeight: FontWeight.w300, letterSpacing: -0.4),
            titleLarge: t.titleLarge?.copyWith(fontWeight: FontWeight.w400, letterSpacing: -0.2),
            titleMedium: t.titleMedium?.copyWith(fontWeight: FontWeight.w500),
            titleSmall: t.titleSmall?.copyWith(fontWeight: FontWeight.w500),
          ),
        // Expressive: headlines and titles emphasised.
        DesignDirection.tonal => t.copyWith(
            headlineLarge: t.headlineLarge?.copyWith(fontWeight: FontWeight.w600),
            headlineMedium: t.headlineMedium?.copyWith(fontWeight: FontWeight.w600),
            headlineSmall: t.headlineSmall?.copyWith(fontWeight: FontWeight.w600),
            titleLarge: t.titleLarge?.copyWith(fontWeight: FontWeight.w600),
            titleMedium: t.titleMedium?.copyWith(fontWeight: FontWeight.w600),
          ),
        DesignDirection.console => t.copyWith(
            headlineLarge: t.headlineLarge?.copyWith(fontWeight: FontWeight.w500, letterSpacing: -0.5),
            headlineMedium: t.headlineMedium?.copyWith(fontWeight: FontWeight.w500, letterSpacing: -0.4),
            headlineSmall: t.headlineSmall?.copyWith(fontWeight: FontWeight.w500, letterSpacing: -0.3),
            titleLarge: t.titleLarge?.copyWith(fontWeight: FontWeight.w500, letterSpacing: -0.2),
            titleMedium: t.titleMedium?.copyWith(fontWeight: FontWeight.w600),
          ),
      };

  static DesignTokens _tokens(DesignDirection d, ColorScheme s, TextTheme t, bool black) {
    const tab = [FontFeature.tabularFigures()];
    final mono = _mono(d);
    TextStyle monoOf(TextStyle? x) => x!.copyWith(fontFamily: mono, fontFamilyFallback: const ['Roboto']);
    final variant = s.onSurfaceVariant;
    // True black: hairlines in every direction, since fills barely register
    // on #000.
    Color? border(Color? c) => black ? s.outlineVariant : c;
    return switch (d) {
      DesignDirection.v2 => DesignTokens(
          direction: d,
          cardRadius: 16,
          groupRadius: 16,
          groupInnerRadius: 4,
          cardColor: s.surfaceContainer,
          cardBorder: border(null),
          gap: 12,
          cardPadding: const EdgeInsets.all(16),
          heroValue: t.displaySmall!.copyWith(fontWeight: FontWeight.w500, fontFeatures: tab, color: s.onSurface),
          heroUnit: t.titleMedium!.copyWith(color: variant),
          detailValue: t.displayMedium!.copyWith(fontWeight: FontWeight.w500, fontFeatures: tab, color: s.onSurface),
          cardLabel: t.labelLarge!.copyWith(color: variant),
          sectionLabel: t.titleSmall!.copyWith(color: s.primary),
          data: t.bodySmall!.copyWith(color: variant, fontFeatures: tab),
          chartLabel: t.labelSmall!.copyWith(color: variant, fontFeatures: tab),
          cardIcons: true,
          iconBadge: false,
          ruledGroups: false,
          outlinedChips: false,
          meterHeight: 8,
          meterInk: false,
          button: ButtonTreatment.accent,
          statusPanel: false,
          chart: const ChartTokens(lineWidth: 2, fillAlpha: .14, inkLine: false, grid: false, dotRadius: 3, dotRing: true, height: 56, timeLabels: false),
        ),
      DesignDirection.rack => DesignTokens(
          direction: d,
          cardRadius: 14,
          groupRadius: 14,
          groupInnerRadius: 4,
          cardColor: s.surfaceContainer,
          cardBorder: s.outlineVariant,
          gap: 10,
          cardPadding: const EdgeInsets.fromLTRB(16, 14, 16, 14),
          heroValue: t.displayMedium!.copyWith(fontWeight: FontWeight.w300, letterSpacing: -1.5, fontFeatures: tab, color: s.onSurface, height: 1.05),
          heroUnit: t.titleLarge!.copyWith(fontWeight: FontWeight.w400, letterSpacing: 0, color: variant),
          detailValue: t.displayLarge!.copyWith(fontWeight: FontWeight.w300, letterSpacing: -2, fontFeatures: tab, color: s.onSurface, height: 1.05),
          cardLabel: t.titleSmall!.copyWith(fontWeight: FontWeight.w500, color: s.onSurface),
          sectionLabel: t.titleMedium!.copyWith(fontWeight: FontWeight.w500, color: s.onSurface),
          data: t.bodySmall!.copyWith(color: variant, fontFeatures: tab),
          chartLabel: t.labelSmall!.copyWith(color: variant, fontFeatures: tab),
          cardIcons: false,
          iconBadge: false,
          ruledGroups: true,
          outlinedChips: true,
          meterHeight: 3,
          meterInk: true,
          button: ButtonTreatment.ink,
          statusPanel: false,
          chart: const ChartTokens(lineWidth: 1.5, fillAlpha: 0, inkLine: true, grid: false, dotRadius: 3.5, dotRing: true, height: 64, timeLabels: false, accentTick: true),
        ),
      DesignDirection.tonal => DesignTokens(
          direction: d,
          cardRadius: 24,
          groupRadius: 24,
          groupInnerRadius: 6,
          cardColor: s.surfaceContainerHigh,
          cardBorder: border(null),
          gap: 12,
          cardPadding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
          heroValue: t.displayMedium!.copyWith(fontWeight: FontWeight.w600, letterSpacing: -1, fontFeatures: tab, color: s.onSurface, height: 1.05),
          heroUnit: t.titleLarge!.copyWith(fontWeight: FontWeight.w600, color: variant),
          detailValue: t.displayMedium!.copyWith(fontWeight: FontWeight.w600, letterSpacing: -0.5, fontFeatures: tab, color: s.onSurface, height: 1.05),
          cardLabel: t.titleSmall!.copyWith(fontWeight: FontWeight.w600, color: s.onSurface),
          sectionLabel: t.titleSmall!.copyWith(fontWeight: FontWeight.w600, color: s.primary),
          data: t.bodySmall!.copyWith(color: variant, fontFeatures: tab),
          chartLabel: t.labelSmall!.copyWith(color: variant, fontFeatures: tab),
          cardIcons: true,
          iconBadge: true,
          ruledGroups: false,
          outlinedChips: false,
          meterHeight: 8,
          meterInk: false,
          button: ButtonTreatment.tonal,
          statusPanel: true,
          chart: const ChartTokens(lineWidth: 2.5, fillAlpha: .12, inkLine: false, grid: false, dotRadius: 4.5, dotRing: true, height: 60, timeLabels: false),
          emphasisCard: s.primaryContainer,
          onEmphasisCard: s.onPrimaryContainer,
        ),
      DesignDirection.console => DesignTokens(
          direction: d,
          cardRadius: 10,
          groupRadius: 10,
          groupInnerRadius: 2,
          cardColor: s.surfaceContainer,
          cardBorder: s.outlineVariant,
          gap: 8,
          cardPadding: const EdgeInsets.all(14),
          // Mono only where it earns it: numbers, units and time labels.
          // Names, facts and headers stay in Plex Sans, in their own case.
          heroValue: monoOf(t.headlineLarge).copyWith(fontWeight: FontWeight.w500, letterSpacing: -1, fontFeatures: tab, color: s.onSurface, height: 1.1),
          heroUnit: monoOf(t.titleSmall).copyWith(fontWeight: FontWeight.w400, color: variant),
          detailValue: monoOf(t.displaySmall).copyWith(fontWeight: FontWeight.w500, letterSpacing: -1.5, fontFeatures: tab, color: s.onSurface, height: 1.1),
          cardLabel: t.labelLarge!.copyWith(fontWeight: FontWeight.w500, color: variant),
          sectionLabel: t.titleSmall!.copyWith(fontWeight: FontWeight.w600, color: s.onSurface),
          data: t.bodySmall!.copyWith(color: variant, fontFeatures: tab),
          chartLabel: monoOf(t.labelSmall).copyWith(fontWeight: FontWeight.w400, color: variant, fontFeatures: tab, letterSpacing: 0),
          cardIcons: false,
          iconBadge: false,
          ruledGroups: true,
          outlinedChips: true,
          meterHeight: 4,
          meterInk: false,
          button: ButtonTreatment.outlined,
          statusPanel: false,
          chart: const ChartTokens(lineWidth: 1.5, fillAlpha: 0, inkLine: false, grid: true, dotRadius: 2.5, dotRing: false, height: 60, timeLabels: true),
        ),
    };
  }
}

class _Neutrals {
  const _Neutrals({
    required this.surface,
    required this.dim,
    required this.bright,
    required this.lowest,
    required this.low,
    required this.container,
    required this.high,
    required this.highest,
    required this.onSurface,
    required this.onSurfaceVariant,
    required this.outline,
    required this.outlineVariant,
    required this.inverseSurface,
    required this.onInverseSurface,
    required this.neutralContainer,
  });

  final Color surface, dim, bright, lowest, low, container, high, highest;
  final Color onSurface, onSurfaceVariant, outline, outlineVariant;
  final Color inverseSurface, onInverseSurface, neutralContainer;
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
