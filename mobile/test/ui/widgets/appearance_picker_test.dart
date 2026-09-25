import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/ui/theme/app_theme.dart';
import 'package:nivaroos_mobile/ui/theme/appearance.dart';
import 'package:nivaroos_mobile/ui/theme/theme_controller.dart';
import 'package:nivaroos_mobile/ui/widgets/appearance_picker.dart';

import '../../screenshots/harness.dart' show stubPlatformChannels;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(stubPlatformChannels);
  setUpAll(() async {
    FlutterSecureStorage.setMockInitialValues({});
    await StorageService.instance.init();
  });

  Future<void> pump(WidgetTester tester, ThemeController c) async {
    tester.view.physicalSize = const Size(1080, 4000);
    tester.view.devicePixelRatio = 2.625;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(ListenableBuilder(
      listenable: c,
      builder: (context, _) => MaterialApp(
        theme: AppTheme.forAppearance(c.value, Brightness.light),
        darkTheme: AppTheme.forAppearance(c.value, Brightness.dark),
        themeMode: c.value.mode.themeMode,
        home: AppearanceScreen(controller: c),
      ),
    ));
    await tester.pumpAndSettle();
  }

  testWidgets('Monochrome is the first colour and can be picked', (tester) async {
    final c = ThemeController();
    await pump(tester, c);
    final mono = find.bySemanticsLabel('Monochrome accent');
    final blue = find.bySemanticsLabel('NivaroOS blue accent');
    expect(mono, findsOneWidget);
    final m = tester.getCenter(mono), b = tester.getCenter(blue);
    expect(m.dy, b.dy, reason: 'same row');
    expect(m.dx, lessThan(b.dx), reason: 'Monochrome comes first');

    await tester.tap(mono);
    await tester.pumpAndSettle();
    expect(c.value.accent, AccentColor.mono);
    // The app re-themes with ink as the accent.
    final theme = Theme.of(tester.element(find.byType(AppearanceScreen)));
    expect(theme.colorScheme.primary, theme.colorScheme.onSurface);
    expect(tester.takeException(), isNull);
    await c.set(const Appearance());
  });

  for (final platform in Brightness.values) {
    testWidgets('System mode shows light and dark, on a ${platform.name} phone too', (tester) async {
      tester.platformDispatcher.platformBrightnessTestValue = platform;
      addTearDown(tester.platformDispatcher.clearPlatformBrightnessTestValue);
      final c = ThemeController();
      await pump(tester, c);
      Set<Brightness> shown(String mode) => {
            for (final t in tester.widgetList<Theme>(find.descendant(
              of: find.byWidgetPredicate((w) => w is Semantics && w.properties.label == '$mode mode'),
              matching: find.byType(Theme),
            )))
              t.data.brightness,
          };
      expect(shown('System default'), {Brightness.light, Brightness.dark});
      expect(shown('Light'), {Brightness.light});
      expect(shown('Dark'), {Brightness.dark});
    });
  }

  testWidgets('the swatches are a 3 × 3 grid', (tester) async {
    final c = ThemeController();
    await pump(tester, c);
    final rows = {for (final a in AccentColor.values) tester.getCenter(find.bySemanticsLabel('${a.label} accent')).dy};
    expect(rows, hasLength(3));
  });

  testWidgets("true black's note says what the style keeps", (tester) async {
    for (final (d, words) in [
      (DesignDirection.rack, 'graphite fill and hairline edge'),
      (DesignDirection.tonal, 'tonal colour, dimmed'),
      (DesignDirection.console, 'like a terminal'),
    ]) {
      final c = ThemeController(Appearance(mode: AppThemeMode.black, direction: d));
      await pump(tester, c);
      expect(find.textContaining(words), findsOneWidget, reason: d.name);
    }
  });

  testWidgets('the preview shows the stock controls in every style, mode and Monochrome', (tester) async {
    for (final d in DesignDirection.selectable) {
      for (final m in [AppThemeMode.light, AppThemeMode.dark, AppThemeMode.black]) {
        final c = ThemeController(Appearance(mode: m, direction: d, accent: AccentColor.mono));
        await pump(tester, c);
        expect(find.widgetWithText(FilledButton, 'Start'), findsOneWidget);
        expect(find.byType(Switch), findsWidgets);
        expect(tester.takeException(), isNull, reason: '${d.name} ${m.name}');
      }
    }
  });
}
