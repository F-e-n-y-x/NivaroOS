import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';

/// The last answer of a few GET routes, per server, kept on the phone so
/// Home, Server health and Files' drives can show what they last knew
/// ("Offline · last updated 2 h ago") when the server can't be reached,
/// instead of an error.
///
/// [ApiClient] records every 200 answer of a route in [routes]; a screen
/// whose first load found the server unreachable runs its loaders again
/// inside [replay], where those GETs are answered from here.
///
/// Bounded: only [routes] (with their query) are kept, an answer over
/// [maxBodyBytes] is skipped, and one file per server holds the lot.
class OfflineCache {
  OfflineCache._();
  static final OfflineCache instance = OfflineCache._();

  /// The routes worth keeping: what Home's cards, Server health and the
  /// Files drives list are built from. All small, all server-wide.
  static const routes = {
    '/v1/sys/utilization',
    '/v1/sys/disks-usage',
    '/v1/sys/hardware',
    '/v1/sys/version/check',
    '/v1/sys/packages/check',
    '/v1/disks',
    '/v1/disks/usb',
    '/v1/storage/fstab',
    '/v1/tailscale/status',
    '/v1/backup/health',
    '/v1/backup/jobs',
    '/v2/app_management/web/appgrid',
    '/v1/vm-sidecar/vms',
  };

  static const maxBodyBytes = 256 * 1024;

  /// The file is written at most this often; utilization comes every few
  /// seconds while Home is open.
  static const writeEvery = Duration(seconds: 30);

  static const _replayKey = #nivaroOfflineReplay;

  /// True inside [replay].
  static bool get replaying => Zone.current[_replayKey] == true;

  /// Runs [body] with its GETs answered from the cache (see [ApiClient]).
  static Future<T> replay<T>(Future<T> Function() body) => runZoned(body, zoneValues: {_replayKey: true});

  bool _enabled = false;
  Directory? _dir;
  final Map<String, Future<Map<String, CachedAnswer>>> _servers = {};
  final Map<String, DateTime> _writtenAt = {};

  /// Turns the cache on, kept in [dir]. Until then it neither records nor
  /// answers (tests, the background isolate before init).
  void open(Directory dir) {
    _dir = dir;
    _enabled = true;
    _servers.clear();
    _writtenAt.clear();
  }

  /// Whether [uri]'s answer is kept.
  static bool keeps(Uri uri) => routes.contains(uri.path);

  static String _key(Uri uri) => uri.hasQuery ? '${uri.path}?${uri.query}' : uri.path;

  /// Keeps [body], the answer to GET [uri] on [server].
  Future<void> record(String server, Uri uri, String body) async {
    if (!_enabled || server.isEmpty || !keeps(uri) || body.length > maxBodyBytes) return;
    final entries = await _entries(server);
    entries[_key(uri)] = CachedAnswer(clock.now(), body);
    final last = _writtenAt[server];
    if (last != null && clock.now().difference(last) < writeEvery) return;
    _writtenAt[server] = clock.now();
    await _write(server, entries);
  }

  /// The kept answer to GET [uri] on [server], if any.
  Future<CachedAnswer?> lookup(String server, Uri uri) async {
    if (!_enabled || server.isEmpty) return null;
    return (await _entries(server))[_key(uri)];
  }

  /// When [path] (a full route, "/v1/sys/utilization") was last kept.
  Future<DateTime?> savedAt(String server, String path) async {
    if (!_enabled || server.isEmpty) return null;
    return (await _entries(server))[path]?.at;
  }

  /// Forgets [server]'s answers (sign-out).
  Future<void> clear(String server) async {
    if (!_enabled) return;
    _servers[server] = Future.value({});
    final f = _file(server);
    try {
      if (f != null && await f.exists()) await f.delete();
    } catch (_) {}
  }

  File? _file(String server) {
    final dir = _dir;
    return dir == null ? null : File('${dir.path}/${base64Url.encode(utf8.encode(server)).replaceAll('=', '')}.json');
  }

  Future<Map<String, CachedAnswer>> _entries(String server) => _servers[server] ??= _read(server);

  Future<Map<String, CachedAnswer>> _read(String server) async {
    final out = <String, CachedAnswer>{};
    try {
      final f = _file(server);
      if (f == null || !await f.exists()) return out;
      final json = jsonDecode(await f.readAsString());
      if (json is! Map) return out;
      for (final MapEntry(:key, :value) in json.entries) {
        final at = value is Map ? DateTime.tryParse('${value['at']}') : null;
        final body = value is Map ? value['body'] : null;
        if (at != null && body is String) out['$key'] = CachedAnswer(at, body);
      }
    } catch (e) {
      debugPrint('[offline cache] unreadable, starting over: ${e.runtimeType}');
    }
    return out;
  }

  Future<void> _write(String server, Map<String, CachedAnswer> entries) async {
    final f = _file(server);
    if (f == null) return;
    try {
      await f.parent.create(recursive: true);
      final tmp = File('${f.path}.tmp');
      await tmp.writeAsString(jsonEncode({
        for (final MapEntry(:key, :value) in entries.entries) key: {'at': value.at.toUtc().toIso8601String(), 'body': value.body},
      }));
      await tmp.rename(f.path);
    } catch (e) {
      debugPrint('[offline cache] not written: ${e.runtimeType}');
    }
  }

  /// Tests: answers [bodies] (full route → JSON text) for [server], saved
  /// at [at], from memory only.
  @visibleForTesting
  void debugSeed(String server, Map<String, String> bodies, DateTime at) {
    _enabled = true;
    _servers[server] = Future.value({for (final e in bodies.entries) e.key: CachedAnswer(at, e.value)});
  }

  @visibleForTesting
  void debugReset() {
    _enabled = false;
    _dir = null;
    _servers.clear();
    _writtenAt.clear();
  }
}

/// One kept answer: when it came, and its body.
class CachedAnswer {
  const CachedAnswer(this.at, this.body);
  final DateTime at;
  final String body;
}
