import 'package:dynamic_color/dynamic_color.dart';
import 'package:flutter/material.dart';

import '../../services/storage_service.dart';
import 'appearance.dart';

/// The user's [Appearance] - mode, accent, wallpaper colours, design
/// direction - persisted in [StorageService]. `NivaroApp` listens to it;
/// the Appearance screen writes to it.
///
/// [wallpaperSeed] is the phone's wallpaper key colour on Android 12+, and
/// null everywhere else (older Android, iOS, tests); the Wallpaper option
/// is only offered when it is known.
class ThemeController extends ValueNotifier<Appearance> {
  /// Use [instance]; separate controllers exist only for tests.
  ThemeController([super.value = const Appearance()]);
  static final ThemeController instance = ThemeController();

  final ValueNotifier<Color?> wallpaperSeed = ValueNotifier(null);

  /// Reads the saved choice. Call once after `StorageService.init()`;
  /// anything unreadable keeps its default.
  Future<void> load() async {
    final st = StorageService.instance;
    final mode = AppThemeMode.values.asNameMap()[await st.getThemeMode()] ?? AppThemeMode.system;
    final accent = AccentColor.values.asNameMap()[await st.getThemeAccent()] ?? AccentColor.blue;
    final wallpaper = await st.getThemeWallpaper() == 'true';
    // Only the previewed directions can be picked; anything else (the 1.3
    // look saved by a preview build) falls back to the default.
    final saved = DesignDirection.values.asNameMap()[await st.getDesignDirection()];
    final direction = DesignDirection.selectable.contains(saved) ? saved! : DesignDirection.defaultDirection;
    value = Appearance(mode: mode, accent: accent, wallpaper: wallpaper, direction: direction);
    await refreshWallpaper();
  }

  /// Asks the phone for its wallpaper colours again: on launch and each
  /// time the app comes back to the front, since the wallpaper can change
  /// while the app is in the background.
  Future<void> refreshWallpaper() async {
    try {
      final palette = await DynamicColorPlugin.getCorePalette();
      wallpaperSeed.value = palette == null ? null : Color(palette.primary.keyColor.toInt());
    } catch (_) {
      // No plugin (tests) or no dynamic colour on this phone.
      wallpaperSeed.value = null;
    }
  }

  /// Applies [next] at once and saves what changed. If saving fails the
  /// choice still holds for this session; a theme is not worth an error.
  Future<void> set(Appearance next) async {
    if (next == value) return;
    final prev = value;
    value = next;
    final st = StorageService.instance;
    try {
      if (next.mode != prev.mode) await st.setThemeMode(next.mode.name);
      if (next.accent != prev.accent) await st.setThemeAccent(next.accent.name);
      if (next.wallpaper != prev.wallpaper) await st.setThemeWallpaper(next.wallpaper);
      if (next.direction != prev.direction) await st.setDesignDirection(next.direction.name);
    } catch (e) {
      debugPrint('[theme] Could not save the appearance: $e');
    }
  }

  Future<void> setMode(AppThemeMode mode) => set(value.copyWith(mode: mode));

  @override
  void dispose() {
    wallpaperSeed.dispose();
    super.dispose();
  }
}
