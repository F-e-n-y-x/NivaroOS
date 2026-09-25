// The session rules (plan M-15, M-16, WP0-1): a 401 refreshes once and
// retries; only a 401 from /users/refresh ends the session; outages and
// proxy errors keep it. Plus address handling and error wording (M-24).
import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:clock/clock.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';

const _base = 'http://nas.test';

/// A JWT whose `exp` is [exp] (the signature is not checked by the app).
String _jwt(DateTime exp) {
  String part(Object o) => base64Url.encode(utf8.encode(jsonEncode(o))).replaceAll('=', '');
  return '${part({'alg': 'ES256'})}.${part({'exp': exp.millisecondsSinceEpoch ~/ 1000, 'id': 1})}.sig';
}

Future<void> _session({String access = 'old-access', String refresh = 'old-refresh'}) async {
  StorageService.instance.resetForTest();
  FlutterSecureStorage.setMockInitialValues({
    'server_url': _base,
    'access_token': access,
    'refresh_token': refresh,
    'username': 'alex',
  });
  await StorageService.instance.init();
  await ApiClient.instance.init();
  ApiClient.sessionExpiredNotifier.value = false;
}

http.Response _json(Object body, [int status = 200]) =>
    http.Response(jsonEncode(body), status, headers: {'content-type': 'application/json'});

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late List<String> calls;
  setUp(() => calls = []);

  Future<T> run<T>(Future<T> Function() body, Future<http.Response> Function(http.Request r) handler) =>
      http.runWithClient(body, () => MockClient((r) {
            calls.add('${r.method} ${r.url.path} ${r.headers['Authorization'] ?? '-'}');
            return handler(r);
          }));

  group('refresh on 401', () {
    test('an expired token refreshes once and the request is retried', () async {
      await _session();
      final res = await run(() => ApiClient.instance.get('/sys/utilization'), (r) async {
        if (r.url.path == '/v1/users/refresh') {
          expect(jsonDecode(r.body), {'refresh_token': 'old-refresh'});
          expect(r.headers.containsKey('Authorization'), isFalse);
          return _json({'data': {'access_token': 'new-access', 'refresh_token': 'new-refresh'}});
        }
        return r.headers['Authorization'] == 'new-access' ? _json({'data': 'ok'}) : _json({'message': 'token expired'}, 401);
      });
      expect(res['data'], 'ok');
      expect(calls, ['GET /v1/sys/utilization old-access', 'POST /v1/users/refresh -', 'GET /v1/sys/utilization new-access']);
      expect(await StorageService.instance.getAccessToken(), 'new-access');
      expect(await StorageService.instance.getRefreshToken(), 'new-refresh');
      expect(ApiClient.sessionExpiredNotifier.value, isFalse);
    });

    test('a 502 from the refresh keeps the session and says the server is unreachable', () async {
      await _session();
      final error = await run(() => ApiClient.instance.get('/sys/utilization').then<Object?>((_) => null, onError: (Object e) => e), (r) async {
        if (r.url.path == '/v1/users/refresh') return http.Response('<html>502 Bad Gateway</html>', 502);
        return _json({'message': 'token expired'}, 401);
      });
      expect(error, isA<ApiException>().having((e) => e.kind, 'kind', ApiErrorKind.network));
      expect(ApiClient.instance.hasSession, isTrue);
      expect(ApiClient.sessionExpiredNotifier.value, isFalse);
      expect(await StorageService.instance.getRefreshToken(), 'old-refresh');
    });

    test('a captive portal answering 200 with a web page keeps the session', () async {
      await _session();
      final result = await run(() => ApiClient.instance.refresh(), (r) async => http.Response('<html>Sign in to Wi-Fi</html>', 200));
      expect(result, RefreshResult.unavailable);
      expect(ApiClient.instance.hasSession, isTrue);
    });

    test('no network during the refresh keeps the session', () async {
      await _session();
      final result = await run(() => ApiClient.instance.refresh(), (r) async => throw const SocketException('Network is unreachable'));
      expect(result, RefreshResult.unavailable);
      expect(ApiClient.sessionExpiredNotifier.value, isFalse);
    });

    test('a 401 from /users/refresh ends the session', () async {
      await _session();
      final error = await run(() => ApiClient.instance.get('/sys/utilization').then<Object?>((_) => null, onError: (Object e) => e), (r) async {
        if (r.url.path == '/v1/users/refresh') return _json({'message': 'this session has ended - sign in again'}, 401);
        return _json({'message': 'token expired'}, 401);
      });
      expect(error, isA<ApiException>().having((e) => e.kind, 'kind', ApiErrorKind.auth));
      expect(ApiClient.instance.hasSession, isFalse);
      expect(ApiClient.sessionExpiredNotifier.value, isTrue);
      expect(await StorageService.instance.getAccessToken(), isNull);
    });

    test('two requests failing at once share one refresh', () async {
      await _session();
      await run(
        () => Future.wait([ApiClient.instance.get('/a'), ApiClient.instance.get('/b')]),
        (r) async {
          if (r.url.path == '/v1/users/refresh') {
            await Future<void>.delayed(const Duration(milliseconds: 20));
            return _json({'data': {'access_token': 'new-access', 'refresh_token': 'new-refresh'}});
          }
          return r.headers['Authorization'] == 'new-access' ? _json({'data': 1}) : _json({}, 401);
        },
      );
      expect(calls.where((c) => c.startsWith('POST /v1/users/refresh')), hasLength(1));
    });

    test('a 401 again after a good refresh is reported but does not sign out', () async {
      await _session();
      final error = await run(() => ApiClient.instance.get('/backup/jobs').then<Object?>((_) => null, onError: (Object e) => e), (r) async {
        if (r.url.path == '/v1/users/refresh') return _json({'data': {'access_token': 'new-access', 'refresh_token': 'r2'}});
        return _json({'message': 'admin only'}, 401);
      });
      expect(error, isA<ApiException>().having((e) => e.statusCode, 'status', 401));
      expect(ApiClient.instance.hasSession, isTrue);
      expect(ApiClient.sessionExpiredNotifier.value, isFalse);
    });

    test('tokens another engine already refreshed are adopted without a second refresh', () async {
      await _session();
      final fresh = _jwt(DateTime.now().add(const Duration(hours: 3)));
      // The sharing service's engine wrote new tokens to secure storage.
      await const FlutterSecureStorage().write(key: 'access_token', value: fresh);
      await const FlutterSecureStorage().write(key: 'refresh_token', value: 'r-other');
      await run(() => ApiClient.instance.get('/a'), (r) async => r.headers['Authorization'] == fresh ? _json({'data': 1}) : _json({}, 401));
      expect(calls.where((c) => c.contains('/users/refresh')), isEmpty);
    });
  });

  group('freshToken', () {
    test('refreshes a token with less than 5 minutes left', () async {
      final now = DateTime.utc(2026, 9, 25, 12);
      await withClock(Clock.fixed(now), () async {
        await _session(access: _jwt(now.add(const Duration(minutes: 4))));
        final token = await run(() => ApiClient.instance.freshToken(),
            (r) async => _json({'data': {'access_token': 'renewed', 'refresh_token': 'r'}}));
        expect(token, 'renewed');
      });
    });

    test('keeps a token with time left', () async {
      final now = DateTime.utc(2026, 9, 25, 12);
      await withClock(Clock.fixed(now), () async {
        final access = _jwt(now.add(const Duration(hours: 2)));
        await _session(access: access);
        final token = await run(() => ApiClient.instance.freshToken(), (r) async => _json({}, 500));
        expect(token, access);
        expect(calls, isEmpty);
      });
    });

    test('reads exp from a JWT', () {
      final exp = DateTime.utc(2026, 9, 25, 15);
      expect(ApiClient.tokenExpiry(_jwt(exp)), exp);
      expect(ApiClient.tokenExpiry('not-a-jwt'), isNull);
    });
  });

  group('send (streamed and multipart requests)', () {
    test('retries once after a refresh', () async {
      await _session();
      final res = await run(() => ApiClient.instance.send(() => http.Request('GET', Uri.parse('$_base/v1/file?path=/a'))), (r) async {
        if (r.url.path == '/v1/users/refresh') return _json({'data': {'access_token': 'new-access', 'refresh_token': 'r'}});
        return r.headers['Authorization'] == 'new-access' ? http.Response('bytes', 200) : http.Response('', 401);
      });
      expect(res.statusCode, 200);
      expect(await res.stream.bytesToString(), 'bytes');
    });
  });

  group('addresses', () {
    test('LAN addresses get http, names get https first', () {
      expect(ApiClient.candidateUrls('192.168.1.20'), ['http://192.168.1.20']);
      expect(ApiClient.candidateUrls('nas.local/'), ['http://nas.local']);
      expect(ApiClient.candidateUrls('nas'), ['http://nas']);
      expect(ApiClient.candidateUrls('nas.example.com:8080'), ['http://nas.example.com:8080']);
      expect(ApiClient.candidateUrls('nas.example.com'), ['https://nas.example.com', 'http://nas.example.com']);
      expect(ApiClient.candidateUrls(' HTTPS://Nas.Example.com// '), ['https://Nas.Example.com']);
      expect(ApiClient.candidateUrls(''), isEmpty);
    });

    test('display host', () {
      expect(ApiClient.displayHost('http://192.168.1.20:8080'), '192.168.1.20:8080');
      expect(ApiClient.displayHost('https://nas.example.com'), 'nas.example.com');
    });

    test('WebSocket URIs follow the scheme and carry no token', () async {
      await _session();
      ApiClient.instance.setBaseUrl('https://nas.example.com');
      final uri = ApiClient.instance.webSocketUri('/v1/sys/wsterm');
      expect(uri.toString(), 'wss://nas.example.com/v1/sys/wsterm');
      ApiClient.instance.setBaseUrl(_base);
      expect(ApiClient.instance.webSocketUri('/v1/sys/wsterm').scheme, 'ws');
    });
  });

  group('errors in words', () {
    test('an untrusted certificate says so (M-24)', () {
      final e = ApiClient.describeError(const HandshakeException('CERTIFICATE_VERIFY_FAILED: self signed certificate'), Uri.parse('https://nas.example.com/v1/x'));
      expect(e.kind, ApiErrorKind.certificate);
      expect(e.message, contains("certificate isn't trusted"));
    });

    test('timeouts and DNS failures are unreachable', () {
      expect(ApiClient.describeError(TimeoutException('t'), Uri.parse(_base)).isUnreachable, isTrue);
      final dns = ApiClient.describeError(const SocketException('Failed host lookup: nas.test'), Uri.parse(_base));
      expect(dns.isUnreachable, isTrue);
      expect(dns.message, contains("Couldn't find nas.test"));
    });

    test('probe tells a web page from NivaroOS', () async {
      final ok = await run(() => ApiClient.probe('nas.local'), (r) async => _json({'data': {'initialized': true}}));
      expect(ok.url, 'http://nas.local');
      expect(ok.initialized, isTrue);
      final err = await run(() => ApiClient.probe('nas.local').then<Object?>((_) => null, onError: (Object e) => e),
          (r) async => http.Response('<html>router</html>', 200, headers: {'content-type': 'text/html'}));
      expect(err, isA<ApiException>().having((e) => e.kind, 'kind', ApiErrorKind.notNivaro));
    });

    test('probe falls back from https to http when nothing answers on https', () async {
      final ok = await run(() => ApiClient.probe('nas.example.com'), (r) async {
        if (r.url.scheme == 'https') throw const SocketException('Connection refused');
        return _json({'data': {'initialized': false}});
      });
      expect(ok.url, 'http://nas.example.com');
      expect(ok.initialized, isFalse);
    });

    test('login: wrong password and too many attempts', () async {
      final wrong = await run(() => ApiClient.requestTokens(_base, 'a', 'b').then<Object?>((_) => null, onError: (Object e) => e),
          (r) async => _json({'success': 10006, 'message': 'x'}, 400));
      expect(wrong, isA<ApiException>().having((e) => e.message, 'message', 'Wrong username or password.'));
      final limited = await run(() => ApiClient.requestTokens(_base, 'a', 'b').then<Object?>((_) => null, onError: (Object e) => e),
          (r) async => _json({'message': 'x'}, 429));
      expect(limited, isA<ApiException>().having((e) => e.kind, 'kind', ApiErrorKind.rateLimited));
    });
  });
}
