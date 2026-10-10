import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';

import '../models/container_entry.dart' show AppEvent;
import 'api_client.dart';
import 'utilization_feed.dart' show BusConnect, BusSocket;

/// The server's notification feed (message bus, `/v2/message_bus/notifications`;
/// what goes in it: services/message-bus/service/notification_classify.go):
/// the inbox (NotificationsScreen) and the same entries on the phone.
///
/// Phone notifications come from three places, all through [checkAndNotify]
/// or [_showNew], never Google's push service:
/// - while the app runs, live from the bus socket ([startLive]);
/// - in the background, the 15-minute heartbeat job (heartbeatMain) and a
///   running storage-sharing session;
/// - each one only once: an entry is shown when it is unread, newer than
///   the last id this phone saw on that server ([NotifyPrefs.seen]) and in
///   a channel the user left on. The first check on a server only notes
///   where the feed is, so signing in doesn't replay a month of history.

/// The phone's notification channels (and the inbox's settings).
enum FeedChannel {
  downloads('Downloads', 'Finished and failed downloads and torrents'),
  backups('Backups', 'Backup & Sync runs that failed or need you'),
  system('System', 'Apps installed or updated, drive health, storage');

  const FeedChannel(this.label, this.about);
  final String label;
  final String about;
}

/// Where tapping an entry goes.
enum FeedTargetKind { download, torrent, backupRun, backup, drive, app }

@immutable
class FeedTarget {
  const FeedTarget(this.kind, [this.id = '']);
  final FeedTargetKind kind;

  /// Download id, torrent hash, backup run id, drive path (/dev/sda) or app name.
  final String id;

  @override
  bool operator ==(Object other) => other is FeedTarget && other.kind == kind && other.id == id;

  @override
  int get hashCode => Object.hash(kind, id);

  @override
  String toString() => '${kind.name}:$id';
}

Map<String, dynamic> _object(Object? v) {
  if (v is Map) return v.map((k, v) => MapEntry('$k', v));
  if (v is String && v.isNotEmpty) {
    try {
      final o = jsonDecode(v);
      if (o is Map) return o.map((k, v) => MapEntry('$k', v));
    } catch (_) {}
  }
  return const {};
}

/// One feed entry.
@immutable
class FeedItem {
  const FeedItem({
    required this.id,
    required this.time,
    this.sourceId = '',
    this.eventName = '',
    this.category = 'system',
    this.level = 'info',
    required this.title,
    this.message = '',
    this.args = const {},
    this.action = const {},
    this.read = false,
  });

  final int id;
  final DateTime time;
  final String sourceId;
  final String eventName;

  /// backup | app | storage | system
  final String category;

  /// info | success | warning | error
  final String level;
  final String title;
  final String message;
  final Map<String, dynamic> args;
  final Map<String, dynamic> action;
  final bool read;

  /// The REST shape, or a `message-bus:notification:created` event's
  /// properties (all strings, args and action as JSON text). Null without
  /// an id.
  static FeedItem? tryParse(Map<String, dynamic> j) {
    final id = j['id'] is num ? (j['id'] as num).toInt() : int.tryParse('${j['id']}');
    if (id == null || id <= 0) return null;
    return FeedItem(
      id: id,
      time: DateTime.tryParse('${j['time']}')?.toLocal() ?? DateTime.fromMillisecondsSinceEpoch(0),
      sourceId: '${j['source_id'] ?? ''}',
      eventName: '${j['event_name'] ?? ''}',
      category: '${j['category'] ?? 'system'}',
      level: '${j['level'] ?? 'info'}',
      title: '${j['title'] ?? ''}',
      message: '${j['message'] ?? ''}',
      args: _object(j['args']),
      action: _object(j['action']),
      read: j['read'] == true || j['read'] == 'true',
    );
  }

  FeedItem asRead() => FeedItem(
      id: id, time: time, sourceId: sourceId, eventName: eventName, category: category, level: level,
      title: title, message: message, args: args, action: action, read: true);

  FeedChannel get channel => sourceId == 'nivaroos-download'
      ? FeedChannel.downloads
      : category == 'backup'
          ? FeedChannel.backups
          : FeedChannel.system;

  /// Where a tap goes; null when nowhere more useful than the inbox.
  FeedTarget? get target {
    String s(Object? v) => v is String ? v.trim() : '';
    final props = _object(action['props']);
    switch (s(action['target'])) {
      case 'download':
        return FeedTarget(s(props['kind']) == 'torrent' ? FeedTargetKind.torrent : FeedTargetKind.download, s(props['id']));
      case 'backup':
        final run = s(_object(_object(action['window'])['props'])['runId']);
        return run.isEmpty ? const FeedTarget(FeedTargetKind.backup) : FeedTarget(FeedTargetKind.backupRun, run);
      case 'app':
        final name = s(props['name']);
        if (name.isNotEmpty) return FeedTarget(FeedTargetKind.app, name);
    }
    // Entries from before the server said where they lead.
    if (sourceId == 'nivaroos-download') return const FeedTarget(FeedTargetKind.download);
    if (category == 'backup') return const FeedTarget(FeedTargetKind.backup);
    if (category == 'app' && !eventName.startsWith('app:uninstall') && s(args['app']).isNotEmpty) return FeedTarget(FeedTargetKind.app, s(args['app']));
    // Drive health (SMART) names the drive; a drive that didn't mount
    // only its folder, which has no page of its own.
    if (category == 'storage' && s(args['path']).startsWith('/dev/')) return FeedTarget(FeedTargetKind.drive, s(args['path']));
    return null;
  }
}

/// One page of the feed.
@immutable
class FeedPage {
  const FeedPage({this.items = const [], this.unreadCount = 0, this.latestId = 0, this.hasMore = false});

  /// Newest first.
  final List<FeedItem> items;
  final int unreadCount;
  final int latestId;
  final bool hasMore;

  factory FeedPage.fromJson(Map<String, dynamic> j) {
    int n(Object? v) => v is num ? v.toInt() : int.tryParse('$v') ?? 0;
    final data = j['data'];
    return FeedPage(
      items: [
        if (data is List)
          for (final e in data)
            if (e is Map) ?FeedItem.tryParse(e.map((k, v) => MapEntry('$k', v))),
      ],
      unreadCount: n(j['unread_count']),
      latestId: n(j['latest_id']),
      hasMore: j['has_more'] == true,
    );
  }
}

/// The entries to show on the phone, newest first: unread, newer than
/// [seenId], in an [enabled] channel - at most [max], so a long time
/// offline ends in a few notifications, not dozens (the inbox has the rest).
List<FeedItem> toNotify(Iterable<FeedItem> items, int seenId, Set<FeedChannel> enabled, {int max = 5}) =>
    items.where((i) => !i.read && i.id > seenId && enabled.contains(i.channel)).take(max).toList();

/// The inbox's day groups, in order: "Today", "Yesterday", then the date.
List<({DateTime day, List<FeedItem> items})> groupByDay(List<FeedItem> items) {
  final out = <({DateTime day, List<FeedItem> items})>[];
  for (final i in items) {
    final t = i.time.toLocal();
    final day = DateTime(t.year, t.month, t.day);
    if (out.isEmpty || out.last.day != day) out.add((day: day, items: <FeedItem>[]));
    out.last.items.add(i);
  }
  return out;
}

/// Which channels notify, and the newest entry id seen per server. A small
/// file read fresh each time, because the app and the background engines
/// are separate isolates that each change it.
class NotifyPrefs {
  NotifyPrefs({Set<FeedChannel>? enabled, Map<String, int>? seen})
      : enabled = enabled ?? FeedChannel.values.toSet(),
        seen = seen ?? {};

  final Set<FeedChannel> enabled;
  final Map<String, int> seen;

  @visibleForTesting
  static Future<File> Function() file = () async => File('${(await getApplicationSupportDirectory()).path}/feed_notify.json');

  static Future<NotifyPrefs> load() async {
    try {
      final j = jsonDecode(await (await file()).readAsString());
      if (j is Map) {
        return NotifyPrefs(
          enabled: j['enabled'] is List ? {for (final c in FeedChannel.values) if ((j['enabled'] as List).contains(c.name)) c} : null,
          seen: j['seen'] is Map ? {for (final e in (j['seen'] as Map).entries) if (e.value is num) '${e.key}': (e.value as num).toInt()} : null,
        );
      }
    } catch (_) {}
    return NotifyPrefs();
  }

  Future<void> save() async {
    try {
      final f = await file();
      final tmp = File('${f.path}.tmp');
      await tmp.writeAsString(jsonEncode({'enabled': [for (final c in enabled) c.name], 'seen': seen}), flush: true);
      await tmp.rename(f.path);
    } catch (e) {
      debugPrint('[feed] could not save: ${e.runtimeType}');
    }
  }
}

Future<BusSocket> _connect(Uri uri, Map<String, String> headers) async {
  final ws = await WebSocket.connect(uri.toString(), headers: headers).timeout(const Duration(seconds: 10));
  return (frames: ws, close: () => ws.close());
}

/// The feed for the signed-in server.
class NotificationFeed {
  NotificationFeed({ApiClient? api, BusConnect? connect})
      : _api = api ?? ApiClient.instance,
        _open = connect ?? _connect;

  static final NotificationFeed instance = NotificationFeed();

  static const _base = '/v2/message_bus/notifications';
  static const _channel = MethodChannel('com.fenyx.nivaroos/feed_notify');
  static const createdEvent = 'message-bus:notification:created';
  static const stateEvent = 'message-bus:notification:state';

  final ApiClient _api;
  final BusConnect _open;

  /// Unread entries on the server (the bell's badge); null until known.
  final ValueNotifier<int?> unread = ValueNotifier(null);

  /// Bumped when the feed changed (a new entry, read elsewhere): an open
  /// inbox loads again.
  final ValueNotifier<int> changed = ValueNotifier(0);

  Future<FeedPage> load({int? before, int limit = 50}) async {
    final page = FeedPage.fromJson(await _api.get(_base, query: {'limit': limit, 'before': ?before}));
    unread.value = page.unreadCount;
    return page;
  }

  /// One entry, by id (a tapped phone notification).
  Future<FeedItem?> item(int id) async {
    final page = await load(before: id + 1, limit: 1);
    return page.items.where((i) => i.id == id).firstOrNull;
  }

  Future<void> markRead(List<int> ids) async {
    if (ids.isEmpty) return;
    final res = await _api.post('$_base/read', body: {'ids': ids});
    unread.value = (res['unread_count'] as num?)?.toInt() ?? unread.value;
    final server = _api.baseUrl;
    for (final id in ids) {
      try {
        await _channel.invokeMethod('cancel', {'server': server, 'id': id});
      } catch (_) {}
    }
  }

  /// Everything up to [upTo] (an entry newer than what's on screen stays unread).
  Future<void> markAllRead(int upTo) async {
    final res = await _api.post('$_base/read', body: {'all': true, 'up_to': upTo});
    unread.value = (res['unread_count'] as num?)?.toInt() ?? 0;
  }

  /// Fetches what's new and shows it on the phone. Background engines and
  /// the live socket's (re)connect call it.
  Future<void> checkAndNotify() async {
    final server = _api.baseUrl;
    if (!_api.hasSession || server.isEmpty) return;
    final prefs = await NotifyPrefs.load();
    final seen = prefs.seen[server];
    final page = FeedPage.fromJson(await _api.get(_base, query: {'limit': 50, 'unread': true, 'after': ?seen}));
    unread.value = page.unreadCount;
    await _showNew(server, prefs, page.items, page.latestId);
  }

  Future<void> _showNew(String server, NotifyPrefs prefs, List<FeedItem> items, int latestId) async {
    final seen = prefs.seen[server];
    if (seen != null) {
      for (final i in toNotify(items, seen, prefs.enabled)) {
        try {
          await _channel.invokeMethod('show', {
            'channel': i.channel.name,
            'server': server,
            'id': i.id,
            'title': i.title,
            'text': i.message,
          });
        } catch (_) {
          // Not on Android (tests): nothing to post to.
        }
      }
    }
    if (seen == null || latestId > seen) {
      prefs.seen[server] = math.max(latestId, seen ?? 0);
      await prefs.save();
    }
  }

  // --- live, while the app runs ---

  BusSocket? _socket;
  StreamSubscription<Object?>? _sub;
  bool _live = false;
  String? _liveServer;
  int _gen = 0;
  int _failures = 0;
  Timer? _retry;

  /// Opens the bus socket (kept open, reconnecting with backoff, until
  /// [stopLive]): new entries count, notify and refresh an open inbox.
  void startLive() {
    // Already live for this server; after a switch, start over.
    if (_live && _liveServer == _api.baseUrl) return;
    stopLive();
    _live = true;
    _liveServer = _api.baseUrl;
    unawaited(_connectNow());
  }

  void stopLive() {
    _live = false;
    unread.value = null;
    _gen++;
    _retry?.cancel();
    _sub?.cancel();
    _socket?.close();
    _sub = null;
    _socket = null;
  }

  Future<void> _connectNow() async {
    final gen = _gen;
    try {
      final uri = _api.webSocketUri('/v2/message_bus/event/message-bus').replace(queryParameters: {
        'names': [createdEvent, stateEvent],
      });
      final socket = await _open(uri, await _api.authHeaders());
      if (gen != _gen || !_live) {
        socket.close();
        return;
      }
      _socket = socket;
      _sub = socket.frames.listen(_frame, onDone: () => _dropped(gen), onError: (Object _) => _dropped(gen), cancelOnError: true);
      _failures = 0;
      // Whatever happened while there was no socket.
      await checkAndNotify();
      changed.value++;
    } catch (_) {
      _dropped(gen);
    }
  }

  Future<void> _frame(Object? frame) async {
    if (frame is! String) return;
    try {
      final e = AppEvent.tryParse(jsonDecode(frame));
      if (e == null) return;
      final server = _api.baseUrl;
      if (e.name == createdEvent) {
        final item = FeedItem.tryParse(e.properties);
        if (item == null) return;
        unread.value = (unread.value ?? 0) + 1;
        changed.value++;
        await _showNew(server, await NotifyPrefs.load(), [item], item.id);
      } else if (e.name == stateEvent) {
        // Another device read or dismissed some: count again.
        await load(limit: 1);
        changed.value++;
      }
    } catch (_) {}
  }

  void _dropped(int gen) {
    if (gen != _gen) return;
    _sub = null;
    _socket = null;
    if (!_live) return;
    _failures++;
    final s = math.min(1 << math.min(_failures, 6), 60);
    _retry?.cancel();
    _retry = Timer(Duration(seconds: s), () {
      if (_live && gen == _gen) unawaited(_connectNow());
    });
  }
}
