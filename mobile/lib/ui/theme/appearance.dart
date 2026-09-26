import 'package:flutter/material.dart';

/// Light, dark, or true black. [black] is an explicit choice: System never
/// picks it (a phone's dark setting means dark grey, not AMOLED black).
enum AppThemeMode {
  system('System default', Icons.brightness_auto_outlined),
  light('Light', Icons.light_mode_outlined),
  dark('Dark', Icons.dark_mode_outlined),
  black('True black', Icons.contrast_outlined);

  const AppThemeMode(this.label, this.icon);

  final String label;
  final IconData icon;

  /// What `MaterialApp.themeMode` gets. Black is a dark theme; the app
  /// hands `MaterialApp` the black variant as its dark theme instead.
  ThemeMode get themeMode => switch (this) {
        AppThemeMode.system => ThemeMode.system,
        AppThemeMode.light => ThemeMode.light,
        AppThemeMode.dark || AppThemeMode.black => ThemeMode.dark,
      };
}

/// The curated accent colours (design brief, "Design system v3"). Every
/// one goes through `ColorScheme.fromSeed`, so its primary, containers and
/// on-colours meet contrast by construction, and
/// test/ui/theme_contrast_test.dart checks each of them in every mode and
/// direction. There is no free colour wheel on purpose.
///
/// Lime, amber and rose sit near the status hues; status never relies on
/// colour alone (always icon plus word), so they stay usable.
///
/// [mono] is first on purpose (owner request, 2026-09-26): no hue at all.
/// It builds a monochrome scheme whose accent is the style's own ink -
/// near-black in light, near-white in dark and true black - with grey
/// containers; only the status colours keep their hues. Its [seed] is just
/// the light swatch; use [swatch] to draw it.
enum AccentColor {
  mono('Monochrome', Color(0xFF1C1B1A)),
  blue('NivaroOS blue', Color(0xFF2563EB)),
  teal('Teal', Color(0xFF0F9488)),
  lime('Lime', Color(0xFFA3D12E)),
  amber('Amber', Color(0xFFD97706)),
  ember('Ember', Color(0xFFC2410C)),
  rose('Rose', Color(0xFFE11D48)),
  violet('Violet', Color(0xFF7C3AED)),
  slate('Graphite', Color(0xFF64748B));

  const AccentColor(this.label, this.seed);

  final String label;
  final Color seed;

  bool get isMono => this == AccentColor.mono;

  /// The colour a swatch shows in a [brightness] theme: the seed, except
  /// that Monochrome is ink - dark on a light page, light on a dark one -
  /// as its accent is.
  Color swatch(Brightness brightness) => isMono && brightness == Brightness.dark ? const Color(0xFFECEAE6) : seed;
}

/// The design direction the app is drawn in (design brief "Design system
/// v3 - directions"). Screens have one code path; a direction only changes
/// tokens - palette and surfaces, typefaces, shape, card and chart style -
/// through `AppTheme` and the `DesignTokens` extension. The owner's pick
/// becomes [defaultDirection].
enum DesignDirection {
  /// The v2 "calm native" look the 1.3.0 build shipped: Roboto, tonal-spot
  /// surfaces, M3 default shapes. Kept for the comparison screenshots only;
  /// the Appearance screen doesn't offer it.
  v2('1.3', 'The 1.3 look: Roboto, soft tonal surfaces'),

  /// A: warm instrument panel. Paper/graphite neutrals, hairlines, big
  /// light numbers in Geist, ink charts with an accent "now" mark, ink
  /// buttons, ruled groups.
  rack('Rack', 'Warm paper, hairlines, big light numbers'),

  /// B: Material 3 Expressive. Tonal surfaces from the accent, Google Sans
  /// Flex, large soft shapes, bold numbers, filled charts.
  tonal('Tonal', 'Tonal colour, soft shapes, bold numbers'),

  /// C: operations console. Graphite, IBM Plex Sans with Plex Mono for
  /// numbers and time labels, tight corners, thin accent lines on a grid.
  console('Console', 'Graphite, mono numbers, lines on a grid');

  const DesignDirection(this.label, this.description);

  final String label;
  final String description;

  /// The recommended direction (design brief §10). Flip this to adopt the
  /// owner's final pick everywhere.
  static const defaultDirection = DesignDirection.rack;

  /// The directions the Appearance screen's Design preview offers.
  static const selectable = [rack, tonal, console];
}

/// Everything the user chose about how the app looks.
@immutable
class Appearance {
  const Appearance({
    this.mode = AppThemeMode.system,
    this.accent = AccentColor.blue,
    this.direction = DesignDirection.defaultDirection,
  });

  final AppThemeMode mode;
  final AccentColor accent;
  final DesignDirection direction;

  Appearance copyWith({AppThemeMode? mode, AccentColor? accent, DesignDirection? direction}) => Appearance(
        mode: mode ?? this.mode,
        accent: accent ?? this.accent,
        direction: direction ?? this.direction,
      );

  /// "Rack · Dark · Teal", for the settings row. The design comes first
  /// while the directions are being tried on the phone.
  String summary() => [direction.label, mode.label, accent.label].join(' · ');

  @override
  bool operator ==(Object other) =>
      other is Appearance && other.mode == mode && other.accent == accent && other.direction == direction;

  @override
  int get hashCode => Object.hash(mode, accent, direction);
}
