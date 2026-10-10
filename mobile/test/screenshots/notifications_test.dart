// The Notifications inbox (Home's bell, More) in Rack, Soft, Terminal and
// Bold, light and true black, against fixtures/v2/message_bus/notifications.json;
// plus the empty inbox and the "notify on this phone" settings sheet.
//
//   flutter test test/screenshots/notifications_test.dart --update-goldens
//
// PNGs: goldens/notifications/<style>/<shot>_<mode>_412x915[_text2x].png.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/notifications_screen.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'harness.dart';

const _styles = [DesignDirection.rack, DesignDirection.soft, DesignDirection.terminal, DesignDirection.bold];

void main() {
  setUp(signIn);

  for (final d in _styles) {
    for (final m in const [AppThemeMode.light, AppThemeMode.black]) {
      for (final scale in [1.0, if (d == DesignDirection.rack) 2.0]) {
        testWidgets('${d.name} inbox ${m.name} ${scale}x', (tester) => shoot(
              tester,
              dir: 'notifications/${d.name}',
              name: 'inbox',
              screen: const NotificationsScreen(),
              brightness: m == AppThemeMode.light ? Brightness.light : Brightness.dark,
              themeName: m.name,
              appearance: Appearance(mode: m, direction: d),
              textScale: scale,
              pushed: true,
            ));
      }
    }
  }

  testWidgets('rack empty', (tester) => shoot(
        tester,
        dir: 'notifications/rack',
        name: 'empty',
        screen: const NotificationsScreen(),
        appearance: const Appearance(direction: DesignDirection.rack),
        pushed: true,
        overrides: const {'GET /v2/message_bus/notifications': {'data': [], 'unread_count': 0, 'latest_id': 0, 'has_more': false}},
      ));

  testWidgets('rack settings sheet', (tester) => shoot(
        tester,
        dir: 'notifications/rack',
        name: 'settings',
        screen: const NotificationsScreen(),
        appearance: const Appearance(direction: DesignDirection.rack),
        pushed: true,
        before: (tester) async {
          await tester.tap(find.byTooltip('Notification settings'));
          for (var i = 0; i < 10; i++) {
            await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 20)));
            await tester.pump(const Duration(milliseconds: 100));
          }
        },
      ));
}
