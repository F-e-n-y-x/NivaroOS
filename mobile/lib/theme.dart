import 'package:flutter/material.dart';

/// Matches the design tokens introduced into the NivaroOS web app itself
/// this same project cycle (ui/src/assets/scss/common/_root.scss) - the
/// native app should feel like the same product, not a different one.
class NivaroColors {
  static const primary = Color(0xFF2563EB);
  static const primaryDark = Color(0xFF1D4ED8);
  static const danger = Color(0xFFEF4444);
  static const success = Color(0xFF16A34A);
  static const warning = Color(0xFFD97706);
  static const info = Color(0xFF0284C7);
  static const textMuted = Color(0xFF64748B);
  static const background = Color(0xFFF8FAFC);
  static const surface = Color(0xFFFFFFFF);
  static const border = Color(0x14000000);
}

ThemeData buildNivaroTheme() {
  final scheme = ColorScheme.fromSeed(
    seedColor: NivaroColors.primary,
    brightness: Brightness.light,
  ).copyWith(
    primary: NivaroColors.primary,
    error: NivaroColors.danger,
    surface: NivaroColors.surface,
  );

  return ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    scaffoldBackgroundColor: NivaroColors.background,
    appBarTheme: const AppBarTheme(
      backgroundColor: NivaroColors.background,
      foregroundColor: Color(0xFF1E293B),
      elevation: 0,
      centerTitle: false,
      surfaceTintColor: Colors.transparent,
    ),
    cardTheme: CardTheme(
      color: NivaroColors.surface,
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(14),
        side: const BorderSide(color: NivaroColors.border),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: Colors.white,
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(10),
        borderSide: const BorderSide(color: NivaroColors.border),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(10),
        borderSide: const BorderSide(color: NivaroColors.border),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(10),
        borderSide: const BorderSide(color: NivaroColors.primary, width: 1.5),
      ),
      contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
    ),
    elevatedButtonTheme: ElevatedButtonThemeData(
      style: ElevatedButton.styleFrom(
        backgroundColor: NivaroColors.primary,
        foregroundColor: Colors.white,
        elevation: 0,
        padding: const EdgeInsets.symmetric(vertical: 14),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
        textStyle: const TextStyle(fontWeight: FontWeight.w600, fontSize: 15),
      ),
    ),
    bottomNavigationBarTheme: const BottomNavigationBarThemeData(
      backgroundColor: Colors.white,
      selectedItemColor: NivaroColors.primary,
      unselectedItemColor: NivaroColors.textMuted,
      type: BottomNavigationBarType.fixed,
      showUnselectedLabels: true,
    ),
    dividerTheme: const DividerThemeData(color: NivaroColors.border, space: 1),
  );
}
