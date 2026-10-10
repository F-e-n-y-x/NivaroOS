// The server's notification feed in the app: parsing, where a tap goes,
// grouping by day, and which entries become phone notifications (each
// once, only unread and newer than the last seen, only enabled channels,
// never a replay of history on the first check).
import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/notification_feed.dart';

const _server = 'http://nivaro.test';

Map<String, dynamic> _entry(int id, {String source = 'nivaroos-download', String category = 'system', bool read = false, Object? action, Object? args}) => {
      'id': id,
      'time': '2026-09-25T12:00:00Z',
      'source_id': source,
      'event_name': 'x:notify',
      'category': category,
      'level': 'success',
      'title': 'Entry $id',
      'message': 'm',
      'key': '',
      'args': args ?? {},
      'action': ?action,
      'read': read,
    };

FeedItem _item(Map<String, dynamic> j) => FeedItem.tryParse(j)!;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('FeedItem', () {
    test('parses the REST shape and the socket event shape alike', () {
      final rest = _item(_entry(7, action: {'target': 'download', 'props': {'kind': 'torrent', 'id': 'abc'}}));
      final socket = _item({
        'id': '7', 'time': '2026-09-25T12:00:00Z', 'source_id': 'nivaroos-download', 'category': 'system',
        'level': 'success', 'title': 'Entry 7', 'args': '{}', 'action': '{"target":"download","props":{"kind":"torrent","id":"abc"}}',
      });
      expect(rest.id, 7);
      expect(socket.id, 7);
      expect(rest.target, const FeedTarget(FeedTargetKind.torrent, 'abc'));
      expect(socket.target, rest.target);
      expect(socket.read, isFalse);
      expect(FeedItem.tryParse({'title': 'no id'}), isNull);
    });

    test('deep links', () {
      FeedTarget? t(Map<String, dynamic> j) => _item(j).target;
      expect(t(_entry(1, action: {'target': 'download', 'props': {'kind': 'download', 'id': 'd1'}})), const FeedTarget(FeedTargetKind.download, 'd1'));
      // Older download entries without an action still go to Download Station.
      expect(t(_entry(1)), const FeedTarget(FeedTargetKind.download));
      expect(
          t(_entry(1, source: 'nivaroos-backup', category: 'backup', action: {
            'target': 'backup',
            'window': {'kind': 'app', 'props': {'section': 'activity', 'runId': 'r9'}}
          })),
          const FeedTarget(FeedTargetKind.backupRun, 'r9'));
      expect(t(_entry(1, source: 'nivaroos-backup', category: 'backup')), const FeedTarget(FeedTargetKind.backup));
      expect(t(_entry(1, source: 'app-management', category: 'app', action: {'target': 'app', 'props': {'name': 'immich'}})), const FeedTarget(FeedTargetKind.app, 'immich'));
      // Before the server sent the app's name: its title.
      expect(t(_entry(1, source: 'app-management', category: 'app', args: {'app': 'Immich'})), const FeedTarget(FeedTargetKind.app, 'Immich'));
      expect(
          t(_entry(1, source: 'local-storage', category: 'storage', args: {'path': '/dev/sda', 'reason': 'smart_watch'}, action: {'target': 'settings', 'props': {'section': 'storage'}})),
          const FeedTarget(FeedTargetKind.drive, '/dev/sda'));
      // A drive that didn't mount names a folder, not a drive: no page.
      expect(t(_entry(1, source: 'local-storage', category: 'storage', args: {'mount_point': '/media/x'})), isNull);
      expect(t(_entry(1, source: 'nivaroos', category: 'system')), isNull);
    });

    test('channels', () {
      expect(_item(_entry(1)).channel, FeedChannel.downloads);
      expect(_item(_entry(1, source: 'nivaroos-backup', category: 'backup')).channel, FeedChannel.backups);
      expect(_item(_entry(1, source: 'app-management', category: 'app')).channel, FeedChannel.system);
    });
  });

  test('toNotify: unread, newer than seen, enabled channels, at most 5, newest first', () {
    final items = [
      for (var i = 20; i > 0; i--) _item(_entry(i, read: i == 19)),
      _item(_entry(30, source: 'nivaroos-backup', category: 'backup')),
    ]..sort((a, b) => b.id.compareTo(a.id));
    expect(toNotify(items, 15, {FeedChannel.downloads}).map((i) => i.id), [20, 18, 17, 16]);
    expect(toNotify(items, 0, FeedChannel.values.toSet()).map((i) => i.id), [30, 20, 18, 17, 16]);
    expect(toNotify(items, 30, FeedChannel.values.toSet()), isEmpty);
  });

  test('groupByDay keeps the order and splits at local midnight', () {
    FeedItem at(int id, DateTime t) => FeedItem(id: id, time: t, title: '$id');
    final g = groupByDay([at(3, DateTime(2026, 9, 25, 9)), at(2, DateTime(2026, 9, 25, 0, 1)), at(1, DateTime(2026, 9, 24, 23, 59))]);
    expect(g.map((d) => d.day), [DateTime(2026, 9, 25), DateTime(2026, 9, 24)]);
    expect(g.first.items.map((i) => i.id), [3, 2]);
  });

  group('checkAndNotify', () {
    late Directory dir;
    late List<Map> shown;
    late List<Map<String, dynamic>> feed;
    late List<Uri> requests;

    setUp(() {
      dir = Directory.systemTemp.createTempSync('feed');
      NotifyPrefs.file = () async => File('${dir.path}/feed_notify.json');
      shown = [];
      requests = [];
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(
          const MethodChannel('com.fenyx.nivaroos/feed_notify'), (call) async {
        if (call.method == 'show') shown.add(call.arguments as Map);
        return true;
      });
      ApiClient.instance.setBaseUrl(_server);
      ApiClient.instance.setSession('test-token', 'test-refresh');
    });
    tearDown(() => dir.deleteSync(recursive: true));

    Future<void> check(NotificationFeed f) => http.runWithClient(f.checkAndNotify, () => MockClient((req) async {
          requests.add(req.url);
          final after = int.tryParse(req.url.queryParameters['after'] ?? '') ?? 0;
          final unread = req.url.queryParameters['unread'] == 'true';
          final data = [for (final e in feed) if ((e['id'] as int) > after && (!unread || e['read'] != true)) e];
          return http.Response(
              jsonEncode({'data': data, 'unread_count': data.length, 'latest_id': feed.isEmpty ? 0 : feed.first['id'], 'has_more': false}), 200);
        }));

    test('first check only notes where the feed is; later ones show each new entry once', () async {
      final f = NotificationFeed();
      feed = [_entry(5), _entry(4)];
      await check(f);
      expect(shown, isEmpty, reason: 'no replay of history after sign-in');
      expect((await NotifyPrefs.load()).seen[_server], 5);

      feed = [_entry(7), _entry(6, read: true), ...feed];
      await check(f);
      expect(shown.map((s) => s['id']), [7], reason: 'read on another device: not shown');
      expect(shown.single['channel'], 'downloads');
      expect(shown.single['server'], _server);
      expect(requests.last.queryParameters['after'], '5');
      expect(f.unread.value, 1);

      await check(f);
      expect(shown, hasLength(1), reason: 'nothing new: nothing shown again');
    });

    test('a channel turned off is not shown, but still counts as seen', () async {
      await NotifyPrefs(enabled: {FeedChannel.backups}, seen: {_server: 1}).save();
      feed = [_entry(2)];
      await check(NotificationFeed());
      expect(shown, isEmpty);
      final prefs = await NotifyPrefs.load();
      expect(prefs.seen[_server], 2);
      expect(prefs.enabled, {FeedChannel.backups});
    });

    test('seen is per server', () async {
      await NotifyPrefs(seen: {'http://other.test': 99}).save();
      feed = [_entry(3)];
      await check(NotificationFeed());
      expect(shown, isEmpty, reason: 'first check on this server');
      expect((await NotifyPrefs.load()).seen, {'http://other.test': 99, _server: 3});
    });
  });
}
