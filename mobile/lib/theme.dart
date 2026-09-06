import 'package:flutter/material.dart';

/// A real dark UI, deliberately closer to ZimaOS Client's look (the actual
/// reference the app was redesigned against) than to stock Material: a
/// near-black background, bold oversized section titles instead of default
/// AppBars, fully-rounded "pill" buttons and pill badges, dark cards with a
/// hairline border instead of a drop shadow. The accent blue still matches
/// the NivaroOS web app's own design tokens
/// (ui/src/assets/scss/common/_root.scss) so it reads as the same product,
/// just with a UI actually designed for a phone instead of a stock
/// Material scaffold.
class NivaroColors {
  static const primary = Color(0xFF2563EB);
  static const primaryDark = Color(0xFF1D4ED8);
  static const danger = Color(0xFFEF4444);
  static const success = Color(0xFF22C55E);
  static const warning = Color(0xFFF59E0B);
  static const info = Color(0xFF38BDF8);

  static const background = Color(0xFF0A0A0C);
  static const surface = Color(0xFF17181C);
  static const surfaceRaised = Color(0xFF1E1F24);
  static const surfaceMuted = Color(0xFF141518);
  static const border = Color(0x1AFFFFFF);
  static const textPrimary = Color(0xFFF4F5F7);
  static const textMuted = Color(0xFF9AA0AC);
  static const textFaint = Color(0xFF5B6069);
}

/// Distinct accent per well-known folder kind, used by the Files screen's
/// colored folder tiles - the single most visually distinctive trait of the
/// reference app's file browser.
const Map<String, Color> folderAccents = {
  'documents': Color(0xFFF2A93B),
  'downloads': Color(0xFF3BA1F2),
  'media': Color(0xFFE84D8A),
  'gallery': Color(0xFF8B5CF6),
  'backup': Color(0xFF34D399),
  'appdata': Color(0xFF64748B),
};

const Color folderDefaultAccent = Color(0xFFF2A93B);

ThemeData buildNivaroTheme() {
  final scheme = ColorScheme.fromSeed(
    seedColor: NivaroColors.primary,
    brightness: Brightness.dark,
  ).copyWith(
    primary: NivaroColors.primary,
    error: NivaroColors.danger,
    surface: NivaroColors.surface,
  );

  return ThemeData(
    useMaterial3: true,
    brightness: Brightness.dark,
    colorScheme: scheme,
    scaffoldBackgroundColor: NivaroColors.background,
    fontFamily: 'Roboto',
    appBarTheme: const AppBarTheme(
      backgroundColor: NivaroColors.background,
      foregroundColor: NivaroColors.textPrimary,
      elevation: 0,
      centerTitle: false,
      surfaceTintColor: Colors.transparent,
    ),
    cardTheme: CardTheme(
      color: NivaroColors.surface,
      elevation: 0,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(20),
        side: const BorderSide(color: NivaroColors.border),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: NivaroColors.surfaceMuted,
      hintStyle: const TextStyle(color: NivaroColors.textFaint),
      labelStyle: const TextStyle(color: NivaroColors.textMuted),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(16),
        borderSide: BorderSide.none,
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(16),
        borderSide: BorderSide.none,
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(16),
        borderSide: const BorderSide(color: NivaroColors.primary, width: 1.5),
      ),
      contentPadding: const EdgeInsets.symmetric(horizontal: 18, vertical: 16),
    ),
    elevatedButtonTheme: ElevatedButtonThemeData(
      style: ElevatedButton.styleFrom(
        backgroundColor: NivaroColors.primary,
        foregroundColor: Colors.white,
        disabledBackgroundColor: NivaroColors.primary.withOpacity(0.4),
        elevation: 0,
        padding: const EdgeInsets.symmetric(vertical: 16),
        shape: const StadiumBorder(),
        textStyle: const TextStyle(fontWeight: FontWeight.w700, fontSize: 15.5),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        foregroundColor: NivaroColors.textPrimary,
        side: const BorderSide(color: NivaroColors.border),
        padding: const EdgeInsets.symmetric(vertical: 12),
        shape: const StadiumBorder(),
        textStyle: const TextStyle(fontWeight: FontWeight.w600, fontSize: 14),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(foregroundColor: NivaroColors.primary),
    ),
    dividerTheme: const DividerThemeData(color: NivaroColors.border, space: 1),
    popupMenuTheme: PopupMenuThemeData(
      color: NivaroColors.surfaceRaised,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
    ),
    dialogTheme: DialogTheme(
      backgroundColor: NivaroColors.surfaceRaised,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
    ),
    snackBarTheme: SnackBarThemeData(
      backgroundColor: NivaroColors.surfaceRaised,
      contentTextStyle: const TextStyle(color: NivaroColors.textPrimary),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
      behavior: SnackBarBehavior.floating,
    ),
    progressIndicatorTheme: const ProgressIndicatorThemeData(color: NivaroColors.primary),
  );
}

/// Big, bold, un-decorated titles ("Files", "Connect", "Apps") - the
/// reference app never uses an eyebrow label or a standard AppBar title,
/// just a large heading at the top of the screen's own scroll content.
const TextStyle nivaroTitleStyle = TextStyle(
  fontSize: 32,
  fontWeight: FontWeight.w800,
  color: NivaroColors.textPrimary,
  height: 1.1,
);

const TextStyle nivaroSectionLabelStyle = TextStyle(
  fontSize: 17,
  fontWeight: FontWeight.w700,
  color: NivaroColors.textPrimary,
);
