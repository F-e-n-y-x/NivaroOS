// A phone removed from the server's device list stays removed: the app
// stops re-registering it (it used to be back within 30 seconds) until the
// user taps "Pair again".
import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/background_service.dart';
import 'package:nivaroos_mobile/services/device_sync_service.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';

import '../screenshots/harness.dart' show stubPlatformChannels;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() async {
    stubPlatformChannels();
    StorageService.instance.resetForTest();
    FlutterSecureStorage.setMockInitialValues({'server_url': 'http://nivaro.test', 'access_token': 't', 'refresh_token': 'r'});
    await StorageService.instance.init();
    ApiClient.instance.setBaseUrl('http://nivaro.test');
    ApiClient.instance.setSession('t', 'r');
    BackgroundService.debugIsAndroid = false;
    DeviceSyncService.deviceInfoOverride = ('Samsung', 'SM-S938B', 'Android 16 (SDK 36)');
    DeviceSyncService.instance.registrationProblem.value = null;
  });

  test('410 from register: removed, then quiet; Pair again sends repair and resumes', () async {
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
      return http.Response('{}', 404);
    });

    await http.runWithClient(() async {
      final sync = DeviceSyncService.instance;
      expect(await sync.syncWithServer(), isFalse);
      expect(sync.registrationProblem.value?.removed, isTrue);
      expect(await StorageService.instance.getCompanionRemoved(), isTrue);

      // The heartbeat stays quiet - even after a restart.
      StorageService.instance.resetForTest();
      await StorageService.instance.init();
      sync.registrationProblem.value = null;
      expect(await sync.syncWithServer(), isFalse);
      expect(bodies, hasLength(1), reason: 'a removed phone must not re-register on its own');
      expect(sync.registrationProblem.value?.removed, isTrue);

      // "Pair again".
      expect(await sync.pairAgain(), isTrue);
      expect(bodies.last['repair'], isTrue);
      expect(sync.registrationProblem.value, isNull);
      expect(await StorageService.instance.getCompanionRemoved(), isFalse);
      expect(await StorageService.instance.getCompanionSecret(), 's3cret');

      // Back to normal heartbeats, without the repair flag.
      expect(await sync.syncWithServer(), isTrue);
      expect(bodies.last.containsKey('repair'), isFalse);
    }, () => client);
  });
}
