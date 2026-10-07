// Share links: the model reads the core's quickShareItem, a link made on
// the home network says so, "Share link…" sends the options the server
// takes (and refuses a server that would silently drop them), and Revoke
// deletes the link after a confirm.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/models/file_entry.dart';
import 'package:nivaroos_mobile/screens/files/share_link.dart';

import '../screenshots/harness.dart';

class _Server {
  _Server(Map<String, Object> overrides) : _fake = FakeServer(overrides: overrides);

  final FakeServer _fake;
  final calls = <(String, String)>[];

  Iterable<String> bodiesOf(String key) => calls.where((c) => c.$1 == key).map((c) => c.$2);
  bool called(String key) => calls.any((c) => c.$1 == key);

  late final client = MockClient((req) async {
    calls.add(('${req.method} ${req.url.path}', req.body));
    final copy = http.Request(req.method, req.url)
      ..headers.addAll(req.headers)
      ..body = req.body;
    return http.Response.fromStream(await _fake.client.send(copy));
  });
}

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 6; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 200));
  }
}

Future<void> _run(WidgetTester tester, _Server server, Widget screen, Future<void> Function() body) async {
  await loadRealFonts();
  tester.view.physicalSize = const Size(412, 1400) * 3;
  tester.view.devicePixelRatio = 3;
  addTearDown(tester.view.reset);
  await http.runWithClient(() async {
    await tester.pumpWidget(testApp(Scaffold(body: screen)));
    await _settle(tester);
    await body();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => server.client);
}

const _file = FileEntry(name: 'report.pdf', path: '/DATA/Documents/report.pdf', isDir: false, size: 1000);

Map<String, Object?> _item({int max = 0, bool password = false, String host = 'nas.example.net'}) => {
      'success': 200,
      'message': 'ok',
      'data': {
        'id': 'abc',
        'name': 'report.pdf',
        'path': '/DATA/Documents/report.pdf',
        'url': 'https://$host/v1/qs/abc',
        'expires_at': 0,
        'created': 1,
        'max_downloads': max,
        'downloads': 0,
        'has_password': password,
        'is_dir': false,
      },
    };

void main() {
  setUp(signIn);

  test('quickShareItem parses, with what is left of each link', () {
    final list = [for (final e in fixture('v1/quickshare')['data'] as List) QuickShare.fromJson(e as Map<String, dynamic>)];
    expect(list, hasLength(3));
    expect((list[0].isDir, list[0].hasPassword, list[0].remaining), (true, true, null));
    expect((list[1].maxDownloads, list[1].remaining), (1, 1));
    expect(list[2].expiresAt, isNull);
    expect(list[2].summary, 'Never expires');
  });

  test('links that only work at home are recognised', () {
    for (final u in ['http://192.168.1.20/v1/qs/a', 'http://10.0.0.2/x', 'http://100.101.1.2/x', 'http://nas.local/x', 'http://nas/x', 'https://box.tail1234.ts.net/x', 'http://[fd00::1]/x']) {
      expect(isLocalOnlyLink(u), isTrue, reason: u);
    }
    for (final u in ['https://nas.example.net/v1/qs/a', 'http://203.0.113.9/x']) {
      expect(isLocalOnlyLink(u), isFalse, reason: u);
    }
  });

  testWidgets('a one-time, 7-day, password link sends those options and offers copy, share and QR', (tester) async {
    final server = _Server({'POST /v1/quickshare': _item(max: 1, password: true)});
    await _run(tester, server, const ShareLinkSheet(entry: _file), () async {
      await tester.tap(find.text('One-time link'));
      await tester.tap(find.widgetWithText(ChoiceChip, '7 days'));
      await tester.tap(find.text('Asked before the download starts'));
      await tester.pump();
      await tester.enterText(find.widgetWithText(TextField, 'Password'), 's3cret');
      await tester.pump();
      await tester.tap(find.text('Create link'));
      await _settle(tester);
      expect(jsonDecode(server.bodiesOf('POST /v1/quickshare').single),
          {'path': '/DATA/Documents/report.pdf', 'expiry': '7d', 'max_downloads': 1, 'password': 's3cret'});
      expect(find.text('https://nas.example.net/v1/qs/abc'), findsOneWidget);
      expect(find.textContaining('only works on your home network'), findsNothing);
      expect(find.text('Copy link'), findsOneWidget);
      expect(find.text('Share'), findsOneWidget);
      await tester.tap(find.text('QR code'));
      await tester.pump();
      expect(find.byType(QrCodeView), findsOneWidget);
    });
  });

  testWidgets('a link on the home network warns', (tester) async {
    final server = _Server({'POST /v1/quickshare': _item(host: '192.168.1.20')});
    await _run(tester, server, const ShareLinkSheet(entry: _file), () async {
      await tester.tap(find.text('Create link'));
      await _settle(tester);
      expect(find.textContaining('192.168.1.20, so it only works on your home network or over Tailscale'), findsOneWidget);
    });
  });

  testWidgets('a server that drops one-time links gets its link revoked, not handed out', (tester) async {
    final server = _Server({'POST /v1/quickshare': _item(), 'DELETE /v1/quickshare/abc': const {'success': 200, 'message': 'ok'}});
    await _run(tester, server, const ShareLinkSheet(entry: _file), () async {
      await tester.tap(find.text('One-time link'));
      await tester.pump();
      await tester.tap(find.text('Create link'));
      await _settle(tester);
      expect(server.called('DELETE /v1/quickshare/abc'), isTrue);
      expect(find.textContaining('needs a NivaroOS update'), findsOneWidget);
      expect(find.text('https://nas.example.net/v1/qs/abc'), findsNothing);
    });
  });

  testWidgets('Revoke asks, then deletes the link', (tester) async {
    final server = _Server({'DELETE /v1/quickshare/q7mbn3w2xhtd5k4rlz6p2a9e': const {'success': 200, 'message': 'ok'}});
    await _run(tester, server, const SharedLinksScreen(), () async {
      expect(find.text('tax-return-2025.pdf'), findsOneWidget);
      expect(find.textContaining('One-time'), findsOneWidget);
      await tester.tap(find.byTooltip('Revoke link to tax-return-2025.pdf'));
      await _settle(tester);
      expect(server.called('DELETE /v1/quickshare/q7mbn3w2xhtd5k4rlz6p2a9e'), isFalse);
      await tester.tap(find.widgetWithText(FilledButton, 'Revoke'));
      await _settle(tester);
      expect(server.called('DELETE /v1/quickshare/q7mbn3w2xhtd5k4rlz6p2a9e'), isTrue);
    });
  });
}
