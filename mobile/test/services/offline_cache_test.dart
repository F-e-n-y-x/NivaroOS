// The offline copies (OfflineCache): what ApiClient keeps, that a replay
// answers from them without touching the network, that they survive a
// restart and stay per server, and that Home and the Files drives fall
// back to them when the server can't be reached. Plus the password change
// keeping this phone's session. No real server: every answer is a fake.
import 'dart:convert';
import 'dart:io';

import 'package:clock/clock.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/screens/dashboard_screen.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/offline_cache.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';

const _base = 'http://nas.test';

http.Response _json(Object body, [int status = 200]) =>
    http.Response(jsonEncode(body), status, headers: {'content-type': 'application/json'});

Map<String, dynamic> _fixture(String path) =>
    jsonDecode(File('test/screenshots/fixtures/$path.json').readAsStringSync()) as Map<String, dynamic>;

Future<void> _session() async {
  StorageService.instance.resetForTest();
  FlutterSecureStorage.setMockInitialValues({'server_url': _base, 'access_token': 'a', 'refresh_token': 'r', 'username': 'alex'});
  await StorageService.instance.init();
  ApiClient.instance.setBaseUrl(_base);
  ApiClient.instance.setSession('a', 'r');
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late Directory dir;
  final calls = <String>[];

  setUp(() async {
    calls.clear();
    dir = await Directory.systemTemp.createTemp('offline_cache_test');
    OfflineCache.instance.open(dir);
    await _session();
  });
  tearDown(() async {
    OfflineCache.instance.debugReset();
    await dir.delete(recursive: true);
  });

  Future<T> net<T>(Future<T> Function() body, Future<http.Response> Function(http.Request r) handler) =>
      http.runWithClient(body, () => MockClient((r) {
            calls.add('${r.method} ${r.url.path}');
            return handler(r);
          }));

  Future<T> offline<T>(Future<T> Function() body) =>
      net(body, (r) async => throw const SocketException('Network is unreachable'));

  test('keeps the answers of its routes only, and replays them without the network', () async {
    await net(() async {
      await ApiClient.instance.get('/sys/utilization');
      await ApiClient.instance.get('/sys/logs');
    }, (r) async => _json({'data': {'path': r.url.path, 'name': 'café'}}));

    final kept = await OfflineCache.replay(() => ApiClient.instance.get('/sys/utilization'));
    expect(kept['data'], {'path': '/v1/sys/utilization', 'name': 'café'});
    expect(calls, ['GET /v1/sys/utilization', 'GET /v1/sys/logs'], reason: 'the replay made no request');

    final notKept = await OfflineCache.replay(() => ApiClient.instance.get('/sys/logs').then<Object?>((_) => null, onError: (Object e) => e));
    expect(notKept, isA<ApiException>().having((e) => e.isUnreachable, 'unreachable', isTrue));
  });

  test('writes them to disk, per server, and forgets them on clear', () async {
    await net(() => ApiClient.instance.get('/sys/hardware'), (r) async => _json({'data': {'model': 'N100'}}));
    await Future<void>.delayed(const Duration(milliseconds: 50));

    // A restart: a fresh read from the same folder.
    OfflineCache.instance.open(dir);
    final uri = Uri.parse('$_base/v1/sys/hardware');
    expect(jsonDecode((await OfflineCache.instance.lookup(_base, uri))!.body), {'data': {'model': 'N100'}});
    expect(await OfflineCache.instance.lookup('http://other.test', Uri.parse('http://other.test/v1/sys/hardware')), isNull);

    await OfflineCache.instance.clear(_base);
    OfflineCache.instance.open(dir);
    expect(await OfflineCache.instance.lookup(_base, uri), isNull);
  });

  test('skips an answer over the size cap and failed answers', () async {
    final big = 'x' * (OfflineCache.maxBodyBytes + 1);
    await net(() => ApiClient.instance.get('/sys/hardware'), (r) async => _json({'data': big}));
    await net(() => ApiClient.instance.get('/disks').catchError((Object _) => <String, dynamic>{}), (r) async => _json({'message': 'boom'}, 500));
    expect(await OfflineCache.instance.savedAt(_base, '/v1/sys/hardware'), isNull);
    expect(await OfflineCache.instance.savedAt(_base, '/v1/disks'), isNull);
  });

  group('Home', () {
    final at = DateTime(2026, 9, 25, 9, 30);

    test('offline with nothing loaded: the kept readings, stale, dated when they were kept', () async {
      OfflineCache.instance.debugSeed(_base, {
        '/v1/sys/utilization': File('test/screenshots/fixtures/v1/sys/utilization.json').readAsStringSync(),
        '/v1/sys/hardware': jsonEncode(_fixture('v1/sys/hardware')),
      }, at);
      final c = HomeController();
      await offline(c.refreshAll);
      final live = c.live.value!;
      expect(live.stale, isTrue);
      expect(live.updatedAt, at);
      expect(c.checkedAt, at);
      expect(c.host, isNotNull);
      expect(c.liveError, isNull);
      c.dispose();
    });

    test('offline with nothing kept: the offline error, as before', () async {
      final c = HomeController();
      await offline(c.refreshAll);
      expect(c.live.value, isNull);
      expect(c.liveError, isA<ApiException>().having((e) => e.isUnreachable, 'unreachable', isTrue));
      c.dispose();
    });

    test('back online: a fresh reading clears the stale mark and reloads the rest', () async {
      OfflineCache.instance.debugSeed(_base, {'/v1/sys/utilization': File('test/screenshots/fixtures/v1/sys/utilization.json').readAsStringSync()}, at);
      final c = HomeController();
      await offline(c.refreshAll);
      expect(c.live.value!.stale, isTrue);
      calls.clear();
      await net(() async {
        await withClock(Clock.fixed(DateTime(2026, 9, 25, 14, 3)), c.refreshLive);
        await Future<void>.delayed(const Duration(milliseconds: 50));
      }, (r) async {
        final f = File('test/screenshots/fixtures${r.url.path}.json');
        return f.existsSync() ? http.Response.bytes(f.readAsBytesSync(), 200, headers: {'content-type': 'application/json'}) : _json({'message': 'nope'}, 404);
      });
      expect(c.live.value!.stale, isFalse);
      expect(calls, contains('GET /v1/sys/hardware'), reason: 'the slow checks were read again');
      c.dispose();
    });
  });

  group('change password', () {
    test('keeps the new tokens the server hands back', () async {
      final res = await net(() => ApiClient.instance.changePassword('old-pass', 'new-pass'), (r) async {
        expect(r.method, 'PUT');
        expect(r.url.path, '/v1/users/current/password');
        expect(jsonDecode(r.body), {'old_password': 'old-pass', 'password': 'new-pass'});
        return _json({'success': 200, 'data': {'user': {'username': 'alex'}, 'token': {'access_token': 'a2', 'refresh_token': 'r2', 'expires_at': 1}}});
      }).then((_) => true);
      expect(res, isTrue);
      expect(ApiClient.instance.accessToken, 'a2');
      expect(await StorageService.instance.getAccessToken(), 'a2');
      expect(await StorageService.instance.getRefreshToken(), 'r2');
    });

    test('a wrong current password is the server reason, and the session stays', () async {
      final error = await net(
        () => ApiClient.instance.changePassword('wrong', 'new-pass').then<Object?>((_) => null, onError: (Object e) => e),
        (r) async => _json({'success': 10003, 'message': 'Invalid old password'}, 400),
      );
      expect(error, isA<ApiException>().having((e) => e.message, 'message', 'Invalid old password'));
      expect(ApiClient.instance.accessToken, 'a');
    });
  });
}
