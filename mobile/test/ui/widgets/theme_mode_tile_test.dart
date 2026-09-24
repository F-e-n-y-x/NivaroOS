import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/ui/theme/theme_controller.dart';
import 'package:nivaroos_mobile/ui/widgets/theme_mode_tile.dart';

import '../../screenshots/harness.dart' show stubPlatformChannels;
import '../pump.dart';

void main() {
  setUp(() async {
    stubPlatformChannels();
    FlutterSecureStorage.setMockInitialValues({});
    await StorageService.instance.init();
  });

  testWidgets('shows the current mode, and picking one applies and saves it', (tester) async {
    final controller = ThemeController();
    await pumpUi(tester, ThemeModeTile(controller: controller));
    expect(find.text('System default'), findsOneWidget);

    await tester.tap(find.text('Theme'));
    await tester.pumpAndSettle();
    expect(find.byType(RadioListTile<ThemeMode>), findsNWidgets(3));
    await tester.tap(find.text('Dark'));
    await tester.pumpAndSettle();

    expect(controller.value, ThemeMode.dark);
    expect(find.text('Dark'), findsOneWidget);
    expect(await StorageService.instance.getThemeMode(), 'dark');
  });

  testWidgets('closing the sheet without a choice changes nothing', (tester) async {
    final controller = ThemeController();
    await pumpUi(tester, ThemeModeTile(controller: controller));
    await tester.tap(find.text('Theme'));
    await tester.pumpAndSettle();
    await tester.tapAt(const Offset(10, 10));
    await tester.pumpAndSettle();
    expect(controller.value, ThemeMode.system);
  });
}
