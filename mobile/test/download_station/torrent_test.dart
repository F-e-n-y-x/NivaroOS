// Torrents in Download Station: the models read the sidecar's real JSON
// (fixtures/ds/torrents.json, torrent.json - TorrentInfo / TorrentDetail),
// the list's actions and the detail's file/option switches reach the
// right routes with the right bodies, Add torrent sends what the sidecar
// expects (magnet or .torrent bytes), and a magnet shared or opened from
// another app opens Add torrent in the shell.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/screens/download_station/torrent_detail_screen.dart';
import 'package:nivaroos_mobile/screens/download_station/torrents_screen.dart';
import 'package:nivaroos_mobile/screens/home_shell.dart';
import 'package:nivaroos_mobile/services/download_station_api.dart';
import 'package:nivaroos_mobile/services/share_intent.dart';

import '../screenshots/harness.dart';

const _ds = '/v1/download-station';
const _debian = '7acf8fb590b2060dd9c3146ef770169d593433b0';
const _magnet = 'magnet:?xt=urn:btih:7acf8fb590b2060dd9c3146ef770169d593433b0&dn=debian-13.7.0-amd64-netinst.iso';

Map<String, Object> torrentOverrides() => {
      'GET $_ds/torrents': fixture('ds/torrents'),
      'GET $_ds/torrents/$_debian': fixture('ds/torrent'),
      'GET $_ds/torrents/trackers': fixture('ds/torrent_trackers'),
      'GET $_ds/settings': fixture('ds/settings'),
      'GET $_ds/storage/roots': fixture('ds/roots'),
    };

class _Server {
  _Server(Map<String, Object> overrides) : _fake = FakeServer(overrides: overrides);

  final FakeServer _fake;
  final calls = <(String, String)>[];

  Iterable<String> bodiesOf(String key) => calls.where((c) => c.$1 == key).map((c) => c.$2);
  Iterable<String> get routes => calls.map((c) => c.$1);

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
  tester.view.physicalSize = const Size(412, 1600) * 3;
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
    test('the real TorrentInfo list parses', () {
      final d = DsTorrents.fromJson(fixture('ds/torrents'));
      expect((d.engine, d.running, d.qbittorrent), ('qbittorrent', true, true));
      expect(d.supports('lsd'), isTrue);
      expect(d.torrents.map((t) => t.state), ['downloading', 'seeding', 'paused', 'metadata', 'completed', 'error']);
      final t = d.torrents.first;
      expect(t.hash, _debian);
      expect(t.private, isFalse);
      expect(torrentStateLabel(t), '41.2%');
      expect(torrentFacts(t), ['311 MB / 756 MB', '6.2 MB/s down', '310 KB/s up', '1 min left', '24/212 seeds · 3 peers', 'ratio 0.04']);
      expect(d.torrents[3].private, isNull);
      expect(d.torrents[3].hasMetadata, isFalse);
      expect(d.torrents[4].private, isTrue);
      expect(d.torrents[4].isPaused, isTrue);
      expect(torrentFacts(d.torrents.last).single, contains('missingFiles'));
      expect(TorrentFilter.values.map((f) => d.torrents.where(f.matches).length), [6, 2, 1, 2, 2]);
      expect(const DsTorrents(engine: 'external', running: true, torrents: [], unsupported: ['all']).supports('speed'), isFalse);
    });

    test('the detail has files and trackers', () {
      final d = DsTorrentDetail.fromJson(fixture('ds/torrent'));
      expect(d.files.map((f) => f.priority), [6, 1, 0]);
      expect(d.trackers.map((t) => t.status), ['working', 'working', 'not_working', 'updating']);
      expect(d.trackers[2].message, 'Connection timed out');
    });

    test('magnets and .torrent links are told apart from downloads', () {
      expect(extractTorrentSources('get $_magnet now'), [_magnet]);
      expect(extractTorrentSources('https://x.org/a.torrent?key=1 and https://x.org/a.iso'), ['https://x.org/a.torrent?key=1']);
      expect(extractTorrentSources('https://x.org/a.iso'), isEmpty);
    });

    test('ratio', () {
      expect(formatRatio(0.5), '0.50');
      expect(formatRatio(123.4), '123');
    });
  });

  testWidgets('the list pauses and resumes, filters, and opens the detail', (tester) async {
    final server = _Server({...torrentOverrides(), 'POST $_ds/torrents/$_debian/pause': '', 'POST $_ds/torrents/${'b' * 40}/resume': ''});
    await _run(tester, server, const TorrentsScreen(), () async {
      expect(find.text('debian-13.7.0-amd64-netinst.iso'), findsOneWidget);
      expect(find.text('41.2%'), findsOneWidget);
      await tester.tap(find.byTooltip('Pause debian-13.7.0-amd64-netinst.iso'));
      await _settle(tester);
      await tester.tap(find.byTooltip('Resume Big Buck Bunny (4K, 60 fps)'));
      await _settle(tester);
      expect(server.routes, containsAll(['POST $_ds/torrents/$_debian/pause', 'POST $_ds/torrents/${'b' * 40}/resume']));

      await tester.tap(find.widgetWithText(ChoiceChip, 'Seeding 1'));
      await tester.pump();
      expect(find.text('ubuntu-24.04.3-desktop-amd64.iso'), findsOneWidget);
      expect(find.text('debian-13.7.0-amd64-netinst.iso'), findsNothing);
    });
  });

  testWidgets('the detail switches files, priority and sequential, and removes with files', (tester) async {
    final server = _Server({
      ...torrentOverrides(),
      'PUT $_ds/torrents/$_debian/files': '',
      'PUT $_ds/torrents/$_debian/options': '',
      'DELETE $_ds/torrents/$_debian': '',
    });
    await _run(tester, server, const TorrentDetailScreen(hash: _debian), () async {
      expect(find.text('SHA256SUMS.sign'), findsOneWidget);
      expect(find.text('Connection timed out', findRichText: true), findsNothing); // in a FactLine, drawn not as Text
      await tester.tap(find.widgetWithText(CheckboxListTile, 'SHA256SUMS.sign'));
      await _settle(tester);
      expect(jsonDecode(server.bodiesOf('PUT $_ds/torrents/$_debian/files').last), {'ids': [2], 'priority': 1});

      await tester.tap(find.widgetWithText(SwitchListTile, 'Download in sequential order'));
      await _settle(tester);
      expect(jsonDecode(server.bodiesOf('PUT $_ds/torrents/$_debian/options').last), {'sequential': true, 'first_last': true});

      await tester.tap(find.byTooltip('More'));
      await _settle(tester);
      await tester.tap(find.text('Remove'));
      await _settle(tester);
      await tester.tap(find.text('Also delete the downloaded files'));
      await tester.pump();
      expect(find.text('The files are permanently deleted from disk.'), findsOneWidget);
      await tester.tap(find.widgetWithText(FilledButton, 'Remove'));
      await _settle(tester);
      expect(server.routes, contains('DELETE $_ds/torrents/$_debian?delete_files=true'));
    });
  });

  testWidgets('Add torrent sends the magnet, category and options', (tester) async {
    final server = _Server({...torrentOverrides(), 'POST $_ds/torrents': const FakeResponse({'hash': _debian, 'name': 'x'}, status: 201)});
    await _run(tester, server, const TorrentsScreen(), () async {
      await tester.tap(find.text('Add torrent'));
      await _settle(tester);
      await tester.enterText(find.widgetWithText(TextField, 'Magnet links or links to .torrent files'), _magnet);
      await tester.tap(find.widgetWithText(SwitchListTile, 'Start paused'));
      await tester.pump();
      await tester.tap(find.byType(DropdownButtonFormField<String>));
      await _settle(tester);
      await tester.tap(find.text('Linux').last);
      await _settle(tester);
      expect(find.text('/DATA/ISOs'), findsOneWidget);
      await tester.tap(find.widgetWithText(FilledButton, 'Add torrent'));
      await _settle(tester);
      expect(jsonDecode(server.bodiesOf('POST $_ds/torrents').single), {'source': _magnet, 'category': 'Linux', 'paused': true, 'sequential': false, 'first_last': false});
    });
  });

  testWidgets('a magnet shared from another app opens Add torrent, a .torrent file opened with the app too', (tester) async {
    final messenger = TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    final pending = <Object>['Sharing $_magnet', {'name': 'sintel.torrent', 'data': Uint8List.fromList(utf8.encode('d4:infoe'))}];
    messenger.setMockMethodCallHandler(const MethodChannel('com.fenyx.nivaroos/share_intent'), (call) async {
      if (call.method != 'take' || pending.isEmpty) return null;
      return pending.removeAt(0);
    });
    addTearDown(() => messenger.setMockMethodCallHandler(const MethodChannel('com.fenyx.nivaroos/share_intent'), null));
    final server = _Server({...torrentOverrides(), 'POST $_ds/torrents': const FakeResponse({'hash': _debian, 'name': 'x'}, status: 201)});
    await _run(tester, server, const HomeShell(), () async {
      expect(find.text('Add torrent'), findsWidgets);
      expect(find.text('Download on server'), findsNothing);
      await tester.tap(find.widgetWithText(FilledButton, 'Add torrent'));
      await _settle(tester);
      expect(jsonDecode(server.bodiesOf('POST $_ds/torrents').single)['source'], _magnet);
      expect(find.text('Torrent added to Download Station'), findsOneWidget);

      // The .torrent file: the next message to the channel.
      await ShareIntent.listen();
      await _settle(tester);
      expect(find.text('sintel.torrent'), findsOneWidget);
      await tester.tap(find.widgetWithText(FilledButton, 'Add torrent'));
      await _settle(tester);
      expect(jsonDecode(server.bodiesOf('POST $_ds/torrents').last)['torrent'], base64Encode(utf8.encode('d4:infoe')));
    });
  });
}
