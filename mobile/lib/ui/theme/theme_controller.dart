import 'package:flutter/material.dart';

import '../../services/storage_service.dart';

/// The user's System / Light / Dark choice, persisted in [StorageService].
/// `NivaroApp` listens to it; the Theme setting (ThemeModeTile) writes to it.
class ThemeController extends ValueNotifier<ThemeMode> {
  /// Use [instance]; separate controllers exist only for tests.
  ThemeController() : super(ThemeMode.system);
  static final ThemeController instance = ThemeController();

  /// Reads the saved choice. Call once after `StorageService.init()`;
  /// anything unreadable leaves the default, System.
  Future<void> load() async {
    final saved = await StorageService.instance.getThemeMode();
    value = ThemeMode.values.asNameMap()[saved] ?? ThemeMode.system;
  }

  /// Applies [mode] at once and saves it. If saving fails the choice
  /// still holds for this session and the next launch starts from the
  /// previous one; a theme is not worth an error message.
  Future<void> setMode(ThemeMode mode) async {
    if (mode == value) return;
    value = mode;
    try {
      await StorageService.instance.setThemeMode(mode.name);
    } catch (e) {
      debugPrint('[theme] Could not save the theme choice: $e');
    }
  }

  /// The label the settings UI shows for [mode].
  static String label(ThemeMode mode) => switch (mode) {
        ThemeMode.system => 'System default',
        ThemeMode.light => 'Light',
        ThemeMode.dark => 'Dark',
      };
}
