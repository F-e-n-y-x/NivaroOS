import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/ui/theme/appearance.dart';
import 'package:nivaroos_mobile/ui/theme/theme_controller.dart';

import '../screenshots/harness.dart' show stubPlatformChannels;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(stubPlatformChannels);

  // StorageService is a process-wide singleton that reads the mock store
  // once, so these tests write through it rather than re-seeding the mock.
  setUpAll(() async {
    FlutterSecureStorage.setMockInitialValues({});
    await StorageService.instance.init();
  });

  test('loads the saved mode, defaulting to System', () async {
    await StorageService.instance.setThemeMode('light');
    final c = ThemeController();
    await c.load();
    expect(c.value.mode, AppThemeMode.light);

    await StorageService.instance.setThemeMode('bogus');
    await c.load();
    expect(c.value.mode, AppThemeMode.system);
  });

  test('saves mode, accent, wallpaper and style, and reads them back', () async {
    final c = ThemeController();
    await c.set(const Appearance(mode: AppThemeMode.black, accent: AccentColor.teal, wallpaper: true, direction: DesignDirection.console));
    final d = ThemeController();
    await d.load();
    expect(d.value, const Appearance(mode: AppThemeMode.black, accent: AccentColor.teal, wallpaper: true, direction: DesignDirection.console));
    // No dynamic colour in tests: the wallpaper option falls back to the accent.
    expect(d.wallpaperSeed.value, isNull);
    await c.set(const Appearance());
  });

  test('true black is a dark theme for MaterialApp', () {
    expect(AppThemeMode.black.themeMode.name, 'dark');
    expect(AppThemeMode.system.themeMode.name, 'system');
  });

  test('signing out keeps the theme choice', () async {
    await StorageService.instance.setThemeMode('dark');
    await StorageService.instance.setThemeAccent('rose');
    await StorageService.instance.setSession(accessToken: 't', refreshToken: 'r', username: 'alex');
    await StorageService.instance.clearAll();
    expect(await StorageService.instance.getThemeMode(), 'dark');
    expect(await StorageService.instance.getThemeAccent(), 'rose');
    expect(await StorageService.instance.getAccessToken(), isNull);
  });
}
