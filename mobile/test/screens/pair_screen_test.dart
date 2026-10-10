// Pairing with the web's one-time code: the QR code's contents, the code
// field, and what the server's refusals say - against the fake server.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:nivaroos_mobile/models/server_profile.dart';
import 'package:nivaroos_mobile/screens/pair_screen.dart';
import 'package:nivaroos_mobile/services/device_sync_service.dart';
import 'package:nivaroos_mobile/utils/pair_link.dart';

import '../screenshots/harness.dart';

const status = FakeResponse({'success': 200, 'data': {'initialized': true}});

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 8; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 200));
  }
}

/// PairScreen(returnProfile) pushed over a page that records what it pops.
Future<FakeServer> _run(WidgetTester tester, Future<void> Function(List<ServerProfile?> popped) body,
    {Map<String, Object> overrides = const {}, PairLink? link}) async {
  await loadRealFonts();
  tester.view.physicalSize = const Size(412, 915) * 3;
  tester.view.devicePixelRatio = 3;
  addTearDown(tester.view.reset);
  final server = FakeServer(overrides: {'GET /v1/users/status': status, ...overrides});
  final popped = <ServerProfile?>[];
  await http.runWithClient(() async {
    await tester.pumpWidget(testApp(Builder(
      builder: (context) => Scaffold(
        body: Center(
          child: TextButton(
            onPressed: () async => popped.add(await Navigator.of(context)
                .push<ServerProfile>(MaterialPageRoute(builder: (_) => PairScreen(returnProfile: true, initialLink: link)))),
            child: const Text('open'),
          ),
        ),
      ),
    )));
    await tester.tap(find.text('open'));
    await _settle(tester);
    await body(popped);
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => server.client);
  return server;
}

Future<void> _type(WidgetTester tester, {String? address, String? code}) async {
  if (address != null) await tester.enterText(find.widgetWithText(TextField, 'Server address'), address);
  if (code != null) await tester.enterText(find.widgetWithText(TextField, 'Pairing code'), code);
  await tester.tap(find.widgetWithText(FilledButton, 'Pair'));
  await _settle(tester);
}

final paired = FakeResponse({
  'success': 200,
  'data': {
    'token': {'access_token': 'acc', 'refresh_token': 'ref', 'expires_at': 0},
    'user': {'id': 1, 'username': 'alex'},
  },
});

void main() {
  setUp(() async {
    await signIn();
    DeviceSyncService.deviceInfoOverride = ('Google', 'Pixel 8', 'Android 16 (SDK 36)');
  });

  group('PairLink', () {
    test('reads the web QR code: code and http(s) addresses in order', () {
      final l = PairLink.parse('nivaroos://pair?code=abcd-efgh&url=http%3A%2F%2F192.168.1.20%3A8080'
          '&url=https%3A%2F%2Fnas.example.com&url=javascript%3Aalert(1)&url=http%3A%2F%2F192.168.1.20%3A8080')!;
      expect(l.code, 'ABCDEFGH');
      expect(l.urls, ['http://192.168.1.20:8080', 'https://nas.example.com']);
    });

    test('anything else is not a pairing code', () {
      for (final raw in [
        '',
        'https://example.com/?code=ABCDEFGH',
        'nivaroos://other?code=ABCDEFGH',
        'nivaroos://pair?code=ABC',
        'nivaroos://pair?code=ABCDEFG0', // 0 isn't in the alphabet
        'WIFI:S:home;T:WPA;P:secret;;',
      ]) {
        expect(PairLink.parse(raw), isNull, reason: raw);
      }
    });

    test('a typed code', () {
      expect(PairLink.isCode(' abcd efgh '), isTrue);
      expect(PairLink.isCode('ABCD-EFG'), isFalse);
      expect(PairLink.formatCode('abcdefgh'), 'ABCD-EFGH');
    });
  });

  testWidgets('asks for the address and a whole code before calling the server', (tester) async {
    final server = await _run(tester, (_) async {
      await _type(tester, code: 'ABC');
      expect(find.text('Enter the server address.'), findsOneWidget);
      expect(find.text('Enter the 8-character code shown on the web.'), findsOneWidget);
    });
    expect(server.requests.where((r) => r.contains('/v1/users/pair')), isEmpty);
  });

  testWidgets('a typed code comes back as a signed-in profile', (tester) async {
    final server = await _run(tester, (popped) async {
      await _type(tester, address: fakeServer, code: 'abcd-efgh');
      expect(popped, hasLength(1));
      final p = popped.single!;
      expect((p.url, p.username, p.accessToken, p.refreshToken), (fakeServer, 'alex', 'acc', 'ref'));
    }, overrides: {'POST /v1/users/pair': paired});
    expect(server.requests, contains('POST /v1/users/pair'));
  });

  testWidgets('the server\'s refusals say what to do', (tester) async {
    for (final (reason, status, text) in [
      ('invalid', 400, 'That code is wrong. Check it and try again.'),
      ('expired', 410, 'That code has expired. Show a new one in Settings on the web.'),
      ('used', 410, 'That code was already used. Show a new one in Settings on the web.'),
      ('too_many_attempts', 429, 'Too many attempts. Wait a minute, then show a new code on the web.'),
    ]) {
      await _run(tester, (popped) async {
        await _type(tester, address: fakeServer, code: 'ABCDEFGH');
        expect(find.text(text), findsOneWidget, reason: reason);
        expect(popped, isEmpty);
      }, overrides: {'POST /v1/users/pair': FakeResponse({'success': status, 'data': {'reason': reason}}, status: status)});
    }
  });

  testWidgets('a scanned code pairs at once, at the first address that answers', (tester) async {
    final server = await _run(tester, (popped) async {
      expect(popped.single!.url, fakeServer);
    },
        overrides: {'POST /v1/users/pair': paired},
        link: PairLink(code: 'ABCDEFGH', urls: ['http://192.168.1.20:8080', fakeServer]));
    expect(server.requests, containsAllInOrder(['GET /v1/users/status', 'POST /v1/users/pair']));
  });
}
