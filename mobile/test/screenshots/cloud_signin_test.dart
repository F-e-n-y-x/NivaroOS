// Files › Cloud drives › Add cloud drive (own app chosen for Google Drive,
// with the 7-day Testing-mode warning), the same while signing in, and
// Reconnect for an expired drive - light and true black, 412 and 360 dp
// and 200% text.
//
//   flutter test test/screenshots/cloud_signin_test.dart --update-goldens
//
// PNGs: goldens/cloud_signin/<shot>_<mode>_<size>[_text2x].png.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/cloud_account.dart';
import 'package:nivaroos_mobile/screens/files/cloud_signin_screen.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'harness.dart';

final _expired = CloudAccount.fromJson({
  'name': 'My Drive',
  'fs': 'gdrive:',
  'type': 'drive',
  'mount_point': '/mnt/gdrive',
  'status': 'sign_in_expired',
  'problem': 'Your Google app is in Testing mode, so Google signs it out every 7 days. Publish it, then Reconnect.',
  'client_id': '383322-abc.apps.googleusercontent.com',
});

Future<void> _ownApp(WidgetTester tester) async {
  await tester.tap(find.text('Your own app'));
  await tester.pumpAndSettle();
}

final Map<String, (Widget Function(), Future<void> Function(WidgetTester)?)> _shots = {
  'cloud_add': (() => const CloudSignInScreen(), _ownApp),
  'cloud_add_waiting': (
    () => CloudSignInScreen(startForwarder: (_, _) async => () async {}, openBrowser: (_) async => true, pollEvery: const Duration(seconds: 30)),
    (tester) async {
      await tester.scrollUntilVisible(find.byType(FilledButton), 200, scrollable: find.byType(Scrollable).last);
      await tester.tap(find.byType(FilledButton));
      for (var i = 0; i < 5; i++) {
        await tester.pump(const Duration(milliseconds: 50));
      }
      // To the end: the progress and its buttons.
      await tester.drag(find.byType(Scrollable).last, const Offset(0, -2000));
      await tester.pump(const Duration(seconds: 1));
    },
  ),
  'cloud_reconnect': (() => CloudSignInScreen(account: _expired), null),
};

void main() {
  setUp(signIn);

  for (final MapEntry(key: name, value: (build, before)) in _shots.entries) {
    for (final m in const [AppThemeMode.light, AppThemeMode.black]) {
      for (final (size, scale) in const [(phone, 1.0), (smallPhone, 1.0), (phone, 2.0)]) {
        testWidgets('$name ${m.name} ${size.width.toInt()} ${scale}x', (tester) {
          return shoot(
            tester,
            dir: 'cloud_signin',
            name: name,
            screen: build(),
            pushed: true,
            before: before,
            overrides: {
              'POST /v1/cloud/accounts/authorize': {
                'success': 200,
                'data': {'running': true, 'path': '/auth?state=xyz', 'name': 'drive_drive_1'},
              },
              'GET /v1/rclone-auth/auth': const FakeResponse('', status: 307, headers: {'location': 'https://accounts.google.com/o/oauth2/auth?state=xyz'}),
            },
            brightness: m == AppThemeMode.light ? Brightness.light : Brightness.dark,
            themeName: m.name,
            appearance: Appearance(mode: m),
            size: size,
            textScale: scale,
          );
        });
      }
    }
  }
}
