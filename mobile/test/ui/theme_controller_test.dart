import 'package:flutter/material.dart' show ThemeMode;
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
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
    expect(c.value, ThemeMode.light);

    await StorageService.instance.setThemeMode('bogus');
    await c.load();
    expect(c.value, ThemeMode.system);
  });

  test('signing out keeps the theme choice', () async {
    await StorageService.instance.setThemeMode('dark');
    await StorageService.instance.setSession(accessToken: 't', refreshToken: 'r', username: 'alex');
    await StorageService.instance.clearAll();
    expect(await StorageService.instance.getThemeMode(), 'dark');
    expect(await StorageService.instance.getAccessToken(), isNull);
  });
}
