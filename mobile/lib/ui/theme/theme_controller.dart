import 'package:flutter/material.dart';

import '../../services/storage_service.dart';
import 'appearance.dart';

/// The user's [Appearance] - mode, accent, design direction - persisted in
/// [StorageService]. `NivaroApp` listens to it; the Appearance screen
/// writes to it.
class ThemeController extends ValueNotifier<Appearance> {
  /// Use [instance]; separate controllers exist only for tests.
  ThemeController([super.value = const Appearance()]);
  static final ThemeController instance = ThemeController();

  /// Reads the saved choice. Call once after `StorageService.init()`;
  /// anything unreadable keeps its default.
  Future<void> load() async {
    final st = StorageService.instance;
    final mode = AppThemeMode.values.asNameMap()[await st.getThemeMode()] ?? AppThemeMode.system;
    var accent = AccentColor.values.asNameMap()[await st.getThemeAccent()] ?? AccentColor.blue;
    // "Match wallpaper" was removed (2026-09-26); Monochrome is the
    // monotone choice now, so anyone who had it on moves there once.
    final legacyWallpaper = await st.getThemeWallpaper();
    if (legacyWallpaper != null) {
      try {
        if (legacyWallpaper == 'true') {
          accent = AccentColor.mono;
          await st.setThemeAccent(accent.name);
        }
        await st.clearThemeWallpaper();
      } catch (e) {
        debugPrint('[theme] Could not migrate the wallpaper setting: $e');
      }
    }
    // Only the previewed directions can be picked; anything else (the 1.3
    // look saved by a preview build) falls back to the default.
    final saved = DesignDirection.values.asNameMap()[await st.getDesignDirection()];
    final direction = DesignDirection.selectable.contains(saved) ? saved! : DesignDirection.defaultDirection;
    value = Appearance(mode: mode, accent: accent, direction: direction);
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
      if (next.direction != prev.direction) await st.setDesignDirection(next.direction.name);
    } catch (e) {
      debugPrint('[theme] Could not save the appearance: $e');
    }
  }

  Future<void> setMode(AppThemeMode mode) => set(value.copyWith(mode: mode));
}
