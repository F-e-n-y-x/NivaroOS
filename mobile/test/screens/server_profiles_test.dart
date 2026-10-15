// Editing the server in use must not save the tokens the list was loaded
// with: the app may have refreshed since, and the server now ends a session
// whose old refresh token comes back. Nothing here reaches a real server.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:nivaroos_mobile/screens/server_profiles_screen.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';

import '../screenshots/harness.dart';

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 8; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 100));
  }
}

void main() {
  setUp(signIn);

  testWidgets('renaming the server in use keeps its newest tokens', (tester) async {
    final server = FakeServer(overrides: {
      'GET /v1/users/status': const {'success': 200, 'data': {'initialized': true}},
    });
    await http.runWithClient(() async {
      await tester.pumpWidget(testApp(const ServerProfilesScreen(), pushed: true));
      await _settle(tester);
      // A refresh while the list is on screen (any engine of the app).
      await StorageService.instance.setSession(accessToken: 'access-2', refreshToken: 'refresh-2', username: 'alex');

      await tester.tap(find.byTooltip(RegExp('^Options for ')).first);
      await _settle(tester);
      await tester.tap(find.text('Edit'));
      await _settle(tester);
      await tester.enterText(find.widgetWithText(TextField, 'Name (optional)'), 'Home');
      await tester.tap(find.text('Save'));
      await _settle(tester);

      final home = (await StorageService.instance.getProfiles()).singleWhere((p) => p.name == 'Home');
      expect(home.refreshToken, 'refresh-2');
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(minutes: 1));
    }, () => server.client);
  });
}
