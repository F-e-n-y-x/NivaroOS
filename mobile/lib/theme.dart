import 'package:flutter/material.dart';

import 'ui/theme/app_theme.dart';
import 'ui/theme/status_colors.dart';

/// Compatibility layer for screens that have not moved to the v2 design
/// system yet (docs/specs/2026-09-25-mobile-design-system.md). Everything in
/// this file is deprecated: new and migrated code uses
/// `Theme.of(context).colorScheme`, `StatusColors.of(context)`,
/// `Theme.of(context).textTheme` and the tokens in `lib/ui/theme/`. The file
/// is deleted once no screen imports it.
///
/// The old names used to be fixed dark-theme colours. They now read the
/// active theme's colour scheme (bound by [LegacyThemeBridge] in
/// `MaterialApp.builder`), so un-migrated screens follow light and dark mode
/// instead of drawing white text on a light background. Where an old name
/// has no role of its own it is folded onto the nearest one - see the table
/// in the design brief ("Adopting the foundation").
abstract final class NivaroColors {
  static ColorScheme _scheme = AppTheme.dark().colorScheme;
  static StatusColors _status = StatusColors.dark;
  static Brightness? _bound;

  /// Points the legacy names at [theme]'s colours. Returns true when the
  /// brightness actually changed.
  static bool _bind(ThemeData theme) {
    if (_bound == theme.brightness) return false;
    _bound = theme.brightness;
    _scheme = theme.colorScheme;
    _status = theme.extension<StatusColors>() ??
        (theme.brightness == Brightness.dark ? StatusColors.dark : StatusColors.light);
    return true;
  }

  // Brand. `primary` is the scheme's primary; the old light/glow/dark
  // variants were decorative steps of the same blue and fold onto it.
  static Color get primary => _scheme.primary;
  static Color get primaryLight => _scheme.primary;
  static Color get primaryGlow => _scheme.primary;
  static Color get primaryDark => _scheme.primary;
  static Color get primaryContainer => _scheme.primaryContainer;

  // Status: the *Light variants were the text/icon tone, which is what
  // `StatusTone.color` is now.
  static Color get success => _status.success.color;
  static Color get successLight => _status.success.color;
  static Color get successContainer => _status.success.container;
  static Color get warning => _status.warning.color;
  static Color get warningLight => _status.warning.color;
  static Color get warningContainer => _status.warning.container;
  static Color get danger => _scheme.error;
  static Color get dangerLight => _scheme.error;
  static Color get dangerContainer => _scheme.errorContainer;
  static Color get info => _status.info.color;
  static Color get infoLight => _status.info.color;
  static Color get infoContainer => _status.info.container;

  // Decorative accents (banned in v2): mapped onto the scheme's secondary
  // and tertiary roles so they stay distinguishable but harmonised.
  static Color get purple => _scheme.tertiary;
  static Color get purpleLight => _scheme.tertiary;
  static Color get purpleContainer => _scheme.tertiaryContainer;
  static Color get cyan => _scheme.secondary;
  static Color get cyanLight => _scheme.secondary;
  static Color get accent => _scheme.secondary;
  static Color get accentLight => _scheme.secondary;

  static Color get folderAccent => _scheme.primary;
  static Color get fileNeutral => _scheme.onSurfaceVariant;

  // Surfaces: same M3 role names, now from the scheme. `background` and
  // `surfaceDim` were the page canvas, which is `surface` in M3.
  static Color get surfaceDim => _scheme.surface;
  static Color get surfaceContainerLowest => _scheme.surfaceContainerLowest;
  static Color get surfaceContainerLow => _scheme.surfaceContainerLow;
  static Color get surfaceContainer => _scheme.surfaceContainer;
  static Color get surfaceContainerHigh => _scheme.surfaceContainerHigh;
  static Color get surfaceContainerHighest => _scheme.surfaceContainerHighest;
  static Color get background => _scheme.surface;
  static Color get surface => _scheme.surfaceContainerLow;
  static Color get surfaceRaised => _scheme.surfaceContainer;
  static Color get surfaceMuted => _scheme.surfaceContainerLowest;

  // Borders and text.
  static Color get border => _scheme.outlineVariant;
  static Color get borderSubtle => _scheme.outlineVariant;
  static Color get borderHighlight => _scheme.outline;
  static Color get textPrimary => _scheme.onSurface;
  static Color get textSecondary => _scheme.onSurfaceVariant;
  static Color get textMuted => _scheme.onSurfaceVariant;
  static Color get textFaint => _scheme.outline;
}

/// Deprecated: file-browser icon tints. Use the file-type icon helpers.
Color get folderColor => NivaroColors.folderAccent;
Color get fileColor => NivaroColors.fileNeutral;

/// Deprecated: the M3 corner radii, now in `Corners` (lib/ui/theme/spacing.dart).
/// `largeIncreased` and `round` have no v2 equivalent.
abstract final class NivaroShape {
  static const extraSmall = 4.0;
  static const small = 8.0;
  static const medium = 12.0;
  static const large = 16.0;
  static const largeIncreased = 20.0;
  static const extraLarge = 28.0;
  static const round = 999.0;
  static const full = 999.0;
}

/// Keeps [NivaroColors] in step with the active theme. `NivaroApp` puts it
/// in `MaterialApp.builder`; nothing else should use it.
///
/// The legacy names are plain getters, so a widget that read one does not
/// know the theme changed unless it happens to depend on `Theme` itself.
/// When the brightness flips this rebuilds the whole tree below it once,
/// after the frame, so every un-migrated screen repaints in the new colours.
class LegacyThemeBridge extends StatelessWidget {
  const LegacyThemeBridge({super.key, required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    final brightness = Theme.of(context).brightness;
    final wasBound = NivaroColors._bound != null;
    // Bind the target theme, not the one AnimatedTheme is lerping through.
    if (NivaroColors._bind(AppTheme.forBrightness(brightness)) && wasBound) {
      final element = context as Element;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (element.mounted) element.visitChildren(_rebuildAll);
      });
    }
    return child;
  }

  static void _rebuildAll(Element element) {
    element.markNeedsBuild();
    element.visitChildren(_rebuildAll);
  }
}

// --- Deprecated typography: use Theme.of(context).textTheme roles. ---
TextStyle get nivaroTitleStyle => TextStyle(
      fontSize: 28,
      height: 34 / 28,
      fontWeight: FontWeight.w800,
      letterSpacing: -0.6,
      color: NivaroColors.textPrimary,
    );

TextStyle get nivaroSectionLabelStyle => TextStyle(
      fontSize: 17,
      height: 22 / 17,
      fontWeight: FontWeight.w700,
      letterSpacing: -0.3,
      color: NivaroColors.textPrimary,
    );

TextStyle get nivaroItemTitleStyle => TextStyle(
      fontSize: 15,
      height: 20 / 15,
      fontWeight: FontWeight.w600,
      letterSpacing: -0.1,
      color: NivaroColors.textPrimary,
    );

TextStyle get nivaroBodyStyle => TextStyle(
      fontSize: 13.5,
      height: 19 / 13.5,
      fontWeight: FontWeight.w400,
      color: NivaroColors.textSecondary,
    );

TextStyle get nivaroMetaStyle => TextStyle(
      fontSize: 12,
      height: 16 / 12,
      fontWeight: FontWeight.w500,
      letterSpacing: 0.2,
      color: NivaroColors.textMuted,
    );

const TextStyle nivaroLabelLarge = TextStyle(
  fontSize: 14,
  height: 18 / 14,
  fontWeight: FontWeight.w600,
  letterSpacing: 0.1,
);
