// Download Station: the models read the sidecar's real JSON
// (fixtures/ds, captured from DownloadView), the list's actions reach the
// right routes, adding a link sends what the sidecar expects, and a link
// shared from another app (Android's share sheet) opens "Download on
// server" in the shell and queues it. Home flags failed downloads.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/models/server_health.dart';
import 'package:nivaroos_mobile/screens/download_station/download_station_screen.dart';
import 'package:nivaroos_mobile/screens/home_shell.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/download_station_api.dart';
import 'package:nivaroos_mobile/services/share_intent.dart';

import '../screenshots/harness.dart';

const _ds = '/v1/download-station';

Object _list() => jsonDecode(File('test/screenshots/fixtures/ds/downloads.json').readAsStringSync());

Map<String, Object> dsOverrides() => {
      'GET $_ds/downloads': _list(),
      'GET $_ds/settings': fixture('ds/settings'),
      'GET $_ds/storage/roots': fixture('ds/roots'),
    };

/// The fixture server, plus a log of every request with its body.
class _Server {
  _Server(Map<String, Object> overrides) : _fake = FakeServer(overrides: overrides);

  final FakeServer _fake;
  final calls = <(String, String)>[];

  Iterable<String> bodiesOf(String key) => calls.where((c) => c.$1 == key).map((c) => c.$2);

  late final client = MockClient((req) async {
    calls.add(('${req.method} ${req.url.path}${req.url.hasQuery ? '?${req.url.query}' : ''}', req.body));
    final copy = http.Request(req.method, req.url)
      ..headers.addAll(req.headers)
      ..body = req.body;
    return http.Response.fromStream(await _fake.client.send(copy));
  });
}

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 8; i++) {
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
    await tester.pumpWidget(testApp(screen));
    await _settle(tester);
    await body();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => server.client);
}

void main() {
  setUp(signIn);

  group('models', () {
    test('every state of the real DownloadView parses', () {
      final list = [for (final e in _list() as List) DsDownload.fromJson(e as Map<String, dynamic>)];
      expect(list.map((d) => d.state), [DsState.downloading, DsState.queued, DsState.paused, DsState.failed, DsState.completed]);
      final running = list.first;
      expect(running.fraction, closeTo(0.526, 0.001));
      expect(running.isRunning, isTrue);
      expect(dsStateLabel(running), '52.6%');
      expect(dsMetaLine(running), '393 MB / 747 MB · 11 MB/s · 32 sec left');
      expect(list[1].sizeKnown, isFalse);
      expect(dsMetaLine(list[1]), 'Size unknown');
      expect(dsMetaLine(list[3]), contains('403 Forbidden'));
      final done = list.last;
      expect(done.fraction, 1);
      expect(done.path, '/DATA/Downloads/big-buck-bunny-1080p.mkv');
      expect(done.completedAt, isNotNull);
      final s = DsSettings.fromJson(fixture('ds/settings'));
      expect((s.defaultDir, s.maxConcurrent, s.defaultConnections, s.speedLimit), ('/DATA/Downloads', 3, 8, 0));
    });

    test('links are picked out of shared text', () {
      expect(extractLinks('Look at this https://example.com/a.iso, and http://x.org/b'), ['https://example.com/a.iso', 'http://x.org/b']);
      expect(extractLinks('magnet:?xt=urn:btih:abc'), isEmpty);
    });

    test('failed downloads need attention on Home', () {
      final h = buildHealth(failedDownloads: 2);
      final a = h.attention.singleWhere((a) => a.kind == AttentionKind.downloads);
      expect(a.title, '2 downloads failed');
      expect(buildHealth().attention.where((a) => a.kind == AttentionKind.downloads), isEmpty);
    });

    test('the sidecar\'s {"error"} is the message', () async {
      await http.runWithClient(() async {
        await expectLater(
          DownloadStationApi().add('ftp://x'),
          throwsA(isA<ApiException>().having((e) => e.message, 'message', 'only http:// and https:// links are supported')),
        );
      }, () => FakeServer(overrides: {'POST $_ds/downloads': const FakeResponse({'error': 'only http:// and https:// links are supported'}, status: 400)}).client);
    });
  });

  testWidgets('the list shows each state, filters, and pauses and removes', (tester) async {
    final server = _Server({...dsOverrides(), 'POST $_ds/downloads/a1/pause': {}, 'DELETE $_ds/downloads/a5': ''});
    await _run(tester, server, const DownloadStationScreen(), () async {
      expect(find.text('debian-13.1.0-amd64-netinst.iso'), findsOneWidget);
      expect(find.text('52.6%'), findsOneWidget);
      await tester.tap(find.byTooltip('Pause debian-13.1.0-amd64-netinst.iso'));
      await _settle(tester);
      expect(server.calls.map((c) => c.$1), contains('POST $_ds/downloads/a1/pause'));

      await tester.tap(find.widgetWithText(ChoiceChip, 'Failed 1'));
      await tester.pump();
      expect(find.text('holiday-video.mp4'), findsOneWidget);
      expect(find.text('debian-13.1.0-amd64-netinst.iso'), findsNothing);

      await tester.tap(find.widgetWithText(ChoiceChip, 'All 5'));
      await tester.enterText(find.byType(TextField), 'bunny');
      await tester.pump();
      expect(find.text('big-buck-bunny-1080p.mkv'), findsOneWidget);
      expect(find.text('holiday-video.mp4'), findsNothing);

      // Remove a finished one, keeping the file.
      await tester.tap(find.text('big-buck-bunny-1080p.mkv'));
      await _settle(tester);
      await tester.tap(find.text('Remove'));
      await _settle(tester);
      expect(find.text('The downloaded file is kept.'), findsOneWidget);
      await tester.tap(find.text('Remove').last);
      await _settle(tester);
      expect(server.calls.map((c) => c.$1), contains('DELETE $_ds/downloads/a5?delete_file=false'));
    });
  });

  testWidgets('Add download sends the link, folder and name', (tester) async {
    final server = _Server({...dsOverrides(), 'POST $_ds/downloads': const FakeResponse({'id': 'n1', 'state': 'queued'}, status: 201)});
    await _run(tester, server, const DownloadStationScreen(), () async {
      await tester.tap(find.text('Add download'));
      await _settle(tester);
      expect(find.text('/DATA/Downloads'), findsOneWidget);
      await tester.enterText(find.widgetWithText(TextField, 'Link'), 'https://example.com/file.zip');
      await tester.enterText(find.widgetWithText(TextField, 'Save as (optional)'), 'mine.zip');
      await tester.pump();
      await tester.tap(find.widgetWithText(FilledButton, 'Download'));
      await _settle(tester);
      final body = jsonDecode(server.bodiesOf('POST $_ds/downloads').single);
      expect(body, {'url': 'https://example.com/file.zip', 'filename': 'mine.zip', 'start': true, 'source': 'android'});
    });
  });

  testWidgets('a link shared from another app opens Download on server and queues it', (tester) async {
    final messenger = TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    var shared = 'Check this out https://cdn.example.com/big.iso';
    messenger.setMockMethodCallHandler(const MethodChannel('com.fenyx.nivaroos/share_intent'), (call) async {
      if (call.method != 'take') return null;
      final t = shared;
      shared = '';
      return t.isEmpty ? null : t;
    });
    addTearDown(() => messenger.setMockMethodCallHandler(const MethodChannel('com.fenyx.nivaroos/share_intent'), null));
    final server = _Server({...dsOverrides(), 'POST $_ds/downloads': const FakeResponse({'id': 'n1', 'state': 'queued'}, status: 201)});
    await _run(tester, server, const HomeShell(), () async {
      expect(find.text('Download on server'), findsOneWidget);
      expect(find.text('https://cdn.example.com/big.iso'), findsOneWidget);
      await tester.tap(find.widgetWithText(FilledButton, 'Download'));
      await _settle(tester);
      expect(jsonDecode(server.bodiesOf('POST $_ds/downloads').single)['url'], 'https://cdn.example.com/big.iso');
      expect(find.text('Added to Download Station'), findsOneWidget);
      expect(ShareIntent.pending.value, isNull);
    });
  });
}
