import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// NivaroOS Mobile Design System
/// Material Design 3 specification with tonal surface ladder,
/// continuous squircle corners, semantic accent colors, and refined typography.
class NivaroColors {
  // Brand & Accent Colors
  static const primary = Color(0xFF2563EB);
  static const primaryLight = Color(0xFF3B82F6);
  static const primaryDark = Color(0xFF1D4ED8);
  static const primaryContainer = Color(0xFF1E3A8A);

  // Semantic Status & Feature Colors
  static const success = Color(0xFF10B981);
  static const successContainer = Color(0xFF064E3B);
  static const warning = Color(0xFFF59E0B);
  static const warningContainer = Color(0xFF78350F);
  static const danger = Color(0xFFEF4444);
  static const dangerContainer = Color(0xFF7F1D1D);
  static const info = Color(0xFF0EA5E9);
  static const infoContainer = Color(0xFF0C4A6E);
  static const purple = Color(0xFF8B5CF6);
  static const purpleContainer = Color(0xFF4C1D95);
  static const folderAccent = Color(0xFFF59E0B);
  static const fileNeutral = Color(0xFF94A3B8);

  // Material 3 Dark-Theme Tonal Surface Hierarchy
  static const surfaceDim = Color(0xFF0B0C10); // Page background
  static const surfaceContainerLowest = Color(0xFF101217); // Recessed wells & inputs
  static const surfaceContainerLow = Color(0xFF16181F); // Level 1 Card
  static const surfaceContainer = Color(0xFF1C1F27); // Level 2 Card
  static const surfaceContainerHigh = Color(0xFF242732); // Level 3 Floating Chrome
  static const surfaceContainerHighest = Color(0xFF2D313E); // Level 4 Modals & Sheets

  static const background = surfaceDim;
  static const surface = surfaceContainerLow;
  static const surfaceRaised = surfaceContainerHigh;
  static const surfaceMuted = surfaceContainerLowest;

  // Borders & Text
  static const border = Color(0x1AFFFFFF);
  static const borderSubtle = Color(0x1AFFFFFF);
  static const borderHighlight = Color(0x33FFFFFF);
  static const textPrimary = Color(0xFFF8FAFC);
  static const textSecondary = Color(0xFFCBD5E1);
  static const textMuted = Color(0xFF94A3B8);
  static const textFaint = Color(0xFF64748B);
}

/// Semantic Colors for File Explorer
const Color folderColor = NivaroColors.primaryLight;
const Color fileColor = NivaroColors.textMuted;

/// Material 3 Shape Scale Tokens
class NivaroShape {
  static const extraSmall = 4.0;
  static const small = 8.0;
  static const medium = 12.0;
  static const large = 16.0;
  static const largeIncreased = 20.0;
  static const extraLarge = 28.0;
  static const round = 999.0;
  static const full = 999.0;
}

extension NivaroColorExtension on Color {
  Color withValues({double? alpha, double? red, double? green, double? blue}) {
    return withOpacity(alpha ?? opacity);
  }
}

ThemeData buildNivaroTheme() {
  final scheme = ColorScheme.fromSeed(
    seedColor: NivaroColors.primary,
    brightness: Brightness.dark,
  ).copyWith(
    primary: NivaroColors.primary,
    primaryContainer: NivaroColors.primaryContainer,
    error: NivaroColors.danger,
    surface: NivaroColors.surface,
    onSurface: NivaroColors.textPrimary,
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
      systemOverlayStyle: SystemUiOverlayStyle(
        statusBarColor: Colors.transparent,
        statusBarIconBrightness: Brightness.light,
        systemNavigationBarColor: NivaroColors.background,
        systemNavigationBarIconBrightness: Brightness.light,
      ),
      surfaceTintColor: Colors.transparent,
    ),
    cardTheme: CardTheme(
      color: NivaroColors.surfaceContainerLow,
      elevation: 0,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
        side: const BorderSide(color: NivaroColors.border),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: NivaroColors.surfaceContainerLowest,
      hintStyle: const TextStyle(color: NivaroColors.textFaint, fontSize: 14),
      labelStyle: const TextStyle(color: NivaroColors.textMuted, fontSize: 14),
      prefixIconColor: NivaroColors.textMuted,
      suffixIconColor: NivaroColors.textMuted,
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(NivaroShape.large),
        borderSide: const BorderSide(color: NivaroColors.border),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(NivaroShape.large),
        borderSide: const BorderSide(color: NivaroColors.border),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(NivaroShape.large),
        borderSide: const BorderSide(color: NivaroColors.primaryLight, width: 1.5),
      ),
      contentPadding: const EdgeInsets.symmetric(horizontal: 18, vertical: 16),
    ),
    elevatedButtonTheme: ElevatedButtonThemeData(
      style: ElevatedButton.styleFrom(
        backgroundColor: NivaroColors.primary,
        foregroundColor: Colors.white,
        disabledBackgroundColor: NivaroColors.primary.withOpacity(0.35),
        elevation: 0,
        padding: const EdgeInsets.symmetric(vertical: 14, horizontal: 20),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
        textStyle: nivaroLabelLarge,
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        foregroundColor: NivaroColors.textPrimary,
        side: const BorderSide(color: NivaroColors.border),
        padding: const EdgeInsets.symmetric(vertical: 12, horizontal: 18),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
        textStyle: nivaroLabelLarge,
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        foregroundColor: NivaroColors.primaryLight,
        textStyle: nivaroLabelLarge,
      ),
    ),
    dividerTheme: const DividerThemeData(color: NivaroColors.border, space: 1),
    popupMenuTheme: PopupMenuThemeData(
      color: NivaroColors.surfaceContainerHighest,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(NivaroShape.large),
        side: const BorderSide(color: NivaroColors.border),
      ),
    ),
    dialogTheme: DialogTheme(
      backgroundColor: NivaroColors.surfaceContainerHighest,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
        side: const BorderSide(color: NivaroColors.border),
      ),
    ),
    bottomSheetTheme: const BottomSheetThemeData(
      backgroundColor: NivaroColors.surfaceContainerHigh,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
    ),
    snackBarTheme: SnackBarThemeData(
      backgroundColor: NivaroColors.surfaceContainerHighest,
      contentTextStyle: const TextStyle(color: NivaroColors.textPrimary, fontSize: 13.5),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(NivaroShape.large),
        side: const BorderSide(color: NivaroColors.border),
      ),
      behavior: SnackBarBehavior.floating,
    ),
    progressIndicatorTheme: const ProgressIndicatorThemeData(color: NivaroColors.primaryLight),
  );
}

// --- Typography Scale -------------------------------------------------

/// Primary Screen Page Titles ("Files", "Connect", "Apps", "Virtual Machines")
const TextStyle nivaroTitleStyle = TextStyle(
  fontSize: 28,
  height: 34 / 28,
  fontWeight: FontWeight.w800,
  letterSpacing: -0.5,
  color: NivaroColors.textPrimary,
);

/// Section headers within a screen ("Monitor", "Favorites", "Drives")
const TextStyle nivaroSectionLabelStyle = TextStyle(
  fontSize: 18,
  height: 24 / 18,
  fontWeight: FontWeight.w700,
  letterSpacing: -0.2,
  color: NivaroColors.textPrimary,
);

/// Card / item title (App name, VM name, file name)
const TextStyle nivaroItemTitleStyle = TextStyle(
  fontSize: 15,
  height: 20 / 15,
  fontWeight: FontWeight.w600,
  letterSpacing: 0,
  color: NivaroColors.textPrimary,
);

/// Standard readable copy
const TextStyle nivaroBodyStyle = TextStyle(
  fontSize: 13.5,
  height: 19 / 13.5,
  fontWeight: FontWeight.w400,
  color: NivaroColors.textSecondary,
);

/// Muted metadata (File size, timestamp, status)
const TextStyle nivaroMetaStyle = TextStyle(
  fontSize: 12,
  height: 16 / 12,
  fontWeight: FontWeight.w500,
  letterSpacing: 0.2,
  color: NivaroColors.textMuted,
);

/// Action button text
const TextStyle nivaroLabelLarge = TextStyle(
  fontSize: 14,
  height: 18 / 14,
  fontWeight: FontWeight.w600,
  letterSpacing: 0.1,
);
