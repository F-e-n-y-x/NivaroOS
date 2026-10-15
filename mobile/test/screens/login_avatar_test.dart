// The sign-in screen shows the typed account's picture: asked of the server
// once typing pauses, the phone's own copy at once, the placeholder icon for
// an unknown name (and no error). Nothing here reaches a real server.
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/screens/login_screen.dart';
import 'package:nivaroos_mobile/services/avatar_service.dart';
import 'package:nivaroos_mobile/ui/widgets/user_avatar.dart';

import '../screenshots/harness.dart';

final _photo = File('test/screenshots/fixtures/avatar_photo.png').readAsBytesSync();

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 4; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 50));
  }
}

Uint8List? _shown(WidgetTester tester) {
  final f = find.byType(UserAvatar);
  return f.evaluate().isEmpty ? null : tester.widget<UserAvatar>(f).picture;
}

void main() {
  setUp(() async {
    await signIn();
    AvatarService.instance.debugReset();
  });

  testWidgets('debounced lookup: photo for a known name, placeholder for an unknown one', (tester) async {
    final asked = <String>[];
    var status = 200;
    final client = MockClient((req) async {
      if (req.url.path != '/v1/users/login-avatar') return http.Response('{}', 404);
      final name = req.url.queryParameters['username']!;
      asked.add(name);
      if (status != 200) return http.Response('', status);
      return name == 'owner' ? http.Response.bytes(_photo, 200, headers: {'content-type': 'image/jpeg'}) : http.Response('', 404);
    });
    await http.runWithClient(() async {
      await tester.pumpWidget(testApp(const LoginScreen(isReauth: true, initialUsername: ' '), pushed: true));
      await _settle(tester);
      final field = find.byType(TextField).first;
      for (final t in ['o', 'ow', 'own', 'owne', 'owner']) {
        await tester.enterText(field, t);
        await tester.pump(const Duration(milliseconds: 100));
      }
      expect(asked, isEmpty, reason: 'nothing asked while typing');
      await tester.pump(const Duration(milliseconds: 300));
      await _settle(tester);
      expect(asked, ['owner']);
      expect(_shown(tester), _photo);

      await tester.enterText(field, 'ghost');
      await tester.pump(const Duration(milliseconds: 300));
      await _settle(tester);
      await tester.pump(const Duration(seconds: 1)); // the crossfade
      expect(_shown(tester), isNull);
      expect(find.byIcon(Icons.person_outline), findsOneWidget);
      expect(tester.widget<TextField>(field).decoration!.errorText, isNull, reason: 'no error for an unknown name');

      // Rate-limited (or offline): the copy this phone kept.
      status = 429;
      await AvatarService.instance.debugRemember(fakeServer, 'alex', _photo);
      await tester.enterText(field, 'alex');
      await _settle(tester);
      expect(_shown(tester), _photo, reason: 'remembered picture at once');
      await tester.pump(const Duration(milliseconds: 300));
      await _settle(tester);
      expect(_shown(tester), _photo);
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(minutes: 1));
    }, () => client);
  });
}
