// A phone removed from the server's device list (on the web, or from
// another phone) is signed out: the server ends its session, and the app
// says why and pairs the phone again when the user signs in on it. It used
// to stay signed in and quietly keep a "removed" state with a Pair again
// button, while the web no longer listed it.
import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/background_service.dart';
import 'package:nivaroos_mobile/services/device_sync_service.dart';
import 'package:nivaroos_mobile/services/session_service.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';

import '../screenshots/harness.dart' show stubPlatformChannels;

const _server = 'http://nivaro.test';

Future<void> _signedIn() async {
  stubPlatformChannels();
  StorageService.instance.resetForTest();
  FlutterSecureStorage.setMockInitialValues({
    'server_url': _server,
    'access_token': 't',
    'refresh_token': 'r',
    'username': 'alex',
    'companion_secret@$_server': 'old-secret',
    'files_tabs@$_server': '[{"path":"/DATA"}]',
  });
  await StorageService.instance.init();
  ApiClient.instance.setBaseUrl(_server);
  ApiClient.instance.setSession('t', 'r');
  BackgroundService.debugIsAndroid = false;
  DeviceSyncService.deviceInfoOverride = ('Samsung', 'SM-S938B', 'Android 16 (SDK 36)');
  DeviceSyncService.instance.registrationProblem.value = null;
  DeviceSyncService.onRemovedByServer = null;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(_signedIn);

  test('410 from register: signed out with the reason, server state forgotten, quiet until signed in again', () async {
    final bodies = <Map<String, dynamic>>[];
    var removed = true;
    final client = MockClient((req) async {
      if (req.url.path.endsWith('/companion/register')) {
        final body = jsonDecode(req.body) as Map<String, dynamic>;
        bodies.add(body);
        if (removed && body['repair'] != true) {
          return http.Response(jsonEncode({'success': 410, 'message': 'this phone was removed'}), 410);
        }
        removed = false;
        return http.Response(jsonEncode({'success': 200, 'message': 'ok', 'data': {'secret': 's3cret'}}), 200);
      }
      if (req.url.path.endsWith('/users/login')) {
        return http.Response(jsonEncode({'success': 200, 'data': {'token': {'access_token': 't2', 'refresh_token': 'r2'}}}), 200);
      }
      return http.Response('{}', 404);
    });

    await http.runWithClient(() async {
      final sync = DeviceSyncService.instance;
      var expired = 0;
      void onExpired() => expired += ApiClient.sessionExpiredNotifier.value ? 1 : 0;
      ApiClient.sessionExpiredNotifier.addListener(onExpired);
      addTearDown(() => ApiClient.sessionExpiredNotifier.removeListener(onExpired));

      expect(await sync.syncWithServer(), isFalse);
      expect(ApiClient.instance.hasSession, isFalse, reason: 'a removed phone must not stay signed in');
      expect(ApiClient.sessionEndReason, SessionEndReason.companionRemoved);
      expect(expired, 1, reason: 'the shell is told to show the sign-in screen');
      expect(await StorageService.instance.hasStoredSession(), isFalse);
      expect(await StorageService.instance.getCompanionRemoved(), isTrue);
      expect(await StorageService.instance.getCompanionSecret(), isNull);
      expect(await StorageService.instance.getFilesTabs(), isNull);

      // Nothing registers it again on its own - not even after a restart.
      StorageService.instance.resetForTest();
      await StorageService.instance.init();
      expect(await sync.syncWithServer(), isFalse);
      expect(bodies, hasLength(1));

      // Signing in again pairs it again (repair) - once.
      await SessionService.signedIn(accessToken: 't2', refreshToken: 'r2', username: 'alex');
      await sync.onSignedIn();
      expect(bodies.last['repair'], isTrue);
      expect(await StorageService.instance.getCompanionRemoved(), isFalse);
      expect(await StorageService.instance.getCompanionSecret(), 's3cret');
      expect(await sync.syncWithServer(), isTrue);
      expect(bodies.last.containsKey('repair'), isFalse);
    }, () => client);
  });

  test('the server ended the session because the phone was removed: the 401 says so', () async {
    final client = MockClient((req) async {
      if (req.url.path.endsWith('/users/refresh')) {
        return http.Response(
            jsonEncode({'success': 40011, 'message': 'this phone was removed from NivaroOS - sign in again to reconnect it', 'data': {'reason': 'companion_removed'}}),
            401);
      }
      // Core refuses the revoked access token.
      return http.Response(jsonEncode({'success': 40011, 'message': 'removed', 'data': {'reason': 'companion_removed'}}), 401);
    });
    await http.runWithClient(() async {
      await expectLater(ApiClient.instance.get('/sys/version'), throwsA(isA<ApiException>()));
      expect(ApiClient.instance.hasSession, isFalse);
      expect(ApiClient.sessionEndReason, SessionEndReason.companionRemoved);
      expect(ApiClient.sessionExpiredNotifier.value, isTrue);
      expect(await StorageService.instance.getCompanionRemoved(), isTrue, reason: 'remembered for the sign-in screen after a restart');
      // And the heartbeat stays quiet.
      expect(await DeviceSyncService.instance.syncWithServer(), isFalse);
    }, () => client);
  });

  test('an ordinary session end is not reported as a removal', () async {
    final client = MockClient((req) async => http.Response(jsonEncode({'success': 10001, 'message': 'token is invalid'}), 401));
    await http.runWithClient(() async {
      await expectLater(ApiClient.instance.get('/sys/version'), throwsA(isA<ApiException>()));
      expect(ApiClient.sessionEndReason, SessionEndReason.ended);
      expect(await StorageService.instance.getCompanionRemoved(), isFalse);
    }, () => client);
  });

  test('removed over the tunnel (sharing engine): stops sharing and signs out', () async {
    var stopped = 0;
    DeviceSyncService.onRemovedByServer = () => stopped++;
    await DeviceSyncService.instance.markRemoved();
    expect(stopped, 1);
    expect(ApiClient.instance.hasSession, isFalse);
    expect(ApiClient.sessionEndReason, SessionEndReason.companionRemoved);
    expect(await StorageService.instance.getCompanionSecret(), isNull);
  });

  test('a phone left "removed" but signed in by an older app is signed out at launch', () async {
    await StorageService.instance.setCompanionRemoved(true);
    final client = MockClient((req) async {
      fail('must not register: ${req.url}');
    });
    await http.runWithClient(() async {
      await DeviceSyncService.instance.onSignedIn();
    }, () => client);
    expect(ApiClient.instance.hasSession, isFalse);
    expect(ApiClient.sessionEndReason, SessionEndReason.companionRemoved);
  });
}
