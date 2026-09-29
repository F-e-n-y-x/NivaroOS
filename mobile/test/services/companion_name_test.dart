// The server is the source of truth for this phone's name. A rename on the
// web used to be undone within a minute: the app sent its own locally kept
// name, flagged user_renamed, on every heartbeat, and showed that name on
// its companion screen whatever the server said.
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

const _server = 'http://nivaro.test';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late String serverName;
  late List<Map<String, dynamic>> registers;
  late List<Map<String, dynamic>> puts;
  var putFails = false;

  MockClient serverClient() => MockClient((req) async {
        final path = req.url.path;
        if (path.endsWith('/companion/register')) {
          final body = jsonDecode(req.body) as Map<String, dynamic>;
          registers.add(body);
          return http.Response(
              jsonEncode({
                'success': 200,
                'data': {
                  'secret': 's',
                  'device': {'id': body['id'], 'name': serverName}
                }
              }),
              200);
        }
        if (path.contains('/companion/devices/') && req.method == 'PUT') {
          if (putFails) throw const SocketExceptionLike();
          final body = jsonDecode(req.body) as Map<String, dynamic>;
          puts.add(body);
          serverName = (body['name'] as String).trim();
          return http.Response(jsonEncode({'success': 200, 'data': {'name': serverName}}), 200);
        }
        if (path.endsWith('/companion/devices')) {
          final id = await DeviceSyncService.instance.getDeviceId();
          return http.Response(
              jsonEncode({
                'success': 200,
                'data': [
                  {'id': id, 'name': serverName, 'model': 'SM-S938B', 'connection': 'lan'},
                  {'id': 'dev_tab', 'name': 'Tab S9', 'model': 'SM-X710'},
                ]
              }),
              200);
        }
        return http.Response('{}', 404);
      });

  setUp(() async {
    stubPlatformChannels();
    StorageService.instance.resetForTest();
    FlutterSecureStorage.setMockInitialValues({
      'server_url': _server,
      'access_token': 't',
      'refresh_token': 'r',
      'companion_device_id': 'dev_s25',
      // What an older app kept after its own rename.
      'companion_device_name': 'Old app name',
    });
    await StorageService.instance.init();
    ApiClient.instance.setBaseUrl(_server);
    ApiClient.instance.setSession('t', 'r');
    BackgroundService.debugIsAndroid = false;
    DeviceSyncService.deviceInfoOverride = ('Samsung', 'SM-S938B', 'Android 16 (SDK 36)');
    serverName = 'Renamed on the web';
    registers = [];
    puts = [];
    putFails = false;
  });

  test('the heartbeat never claims a rename; the app adopts the server name and shows it', () async {
    await http.runWithClient(() async {
      final sync = DeviceSyncService.instance;
      expect(await sync.syncWithServer(), isTrue);
      final sent = registers.single;
      expect((sent['custom_props'] as Map).containsKey('user_renamed'), isFalse);
      expect(sent['name'], 'Old app name', reason: 'only a suggestion, for a phone paired again');
      expect(sent['name_source'], 'user');

      // Adopted: shown on this phone, and what it suggests from now on.
      expect((await sync.getLocalDeviceInfo()).name, 'Renamed on the web');
      expect(await StorageService.instance.getCompanionDeviceName(), 'Renamed on the web');

      // Renamed on the web again: the device list shows it for this phone
      // too (it used to show the phone's local name).
      serverName = 'Kitchen phone';
      final list = await sync.fetchCompanionDevices();
      expect(list.first.isCurrentDevice, isTrue);
      expect(list.first.name, 'Kitchen phone');
      expect((await sync.getLocalDeviceInfo()).name, 'Kitchen phone');
    }, serverClient);
  });

  test("a phone nobody renamed suggests its own model name, not as the user's", () async {
    StorageService.instance.resetForTest();
    FlutterSecureStorage.setMockInitialValues({'server_url': _server, 'access_token': 't', 'refresh_token': 'r', 'companion_device_id': 'dev_s25'});
    await StorageService.instance.init();
    serverName = 'Samsung SM-S938B';
    await http.runWithClient(() async {
      await DeviceSyncService.instance.syncWithServer();
      expect(registers.single['name'], 'Samsung SM-S938B');
      expect(registers.single.containsKey('name_source'), isFalse);
    }, serverClient);
  });

  test('renaming in the app renames on the server, and the server name is kept', () async {
    await http.runWithClient(() async {
      final sync = DeviceSyncService.instance;
      await sync.updateRemoteDeviceName('dev_s25', '  Pocket ');
      expect(puts.single['name'], 'Pocket');
      expect(await StorageService.instance.getCompanionDeviceName(), 'Pocket');
      expect((await sync.getLocalDeviceInfo()).name, 'Pocket');
    }, serverClient);
  });

  test("a rename the server didn't get isn't kept on the phone", () async {
    putFails = true;
    await http.runWithClient(() async {
      await expectLater(DeviceSyncService.instance.updateRemoteDeviceName('dev_s25', 'Offline name'), throwsA(anything));
      expect(await StorageService.instance.getCompanionDeviceName(), 'Old app name');
    }, serverClient);
  });
}

class SocketExceptionLike implements Exception {
  const SocketExceptionLike();
}
