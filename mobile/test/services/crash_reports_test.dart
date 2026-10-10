// Crash reports stay on the phone (last 20, survive a restart) and leave
// only when the user sends them, to their own server.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/crash_reports.dart';
import 'package:nivaroos_mobile/services/device_sync_service.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late Directory dir;
  final c = CrashReports.instance;

  setUp(() async {
    dir = await Directory.systemTemp.createTemp('crash');
    CrashReports.fileOverride = () async => File('${dir.path}/crash_reports.json');
    await c.clear();
  });
  tearDown(() => dir.delete(recursive: true));

  test('keeps the newest 20 and reads them back after a restart', () async {
    for (var i = 0; i < 25; i++) {
      c.record(StateError('boom $i'), StackTrace.current, source: 'platform');
    }
    expect(c.reports.value, hasLength(CrashReports.keep));
    expect(c.reports.value.first.error, contains('boom 24'));
    await c.clear();
    c.record(StateError('kept'), StackTrace.current, source: 'flutter');
    await Future<void>.delayed(const Duration(milliseconds: 50));
    c.reports.value = const [];
    await c.load();
    expect(c.reports.value.single.error, contains('kept'));
    expect(c.reports.value.single.source, 'flutter');
  });

  test('install catches framework errors', () async {
    final before = FlutterError.onError;
    addTearDown(() => FlutterError.onError = before);
    FlutterError.onError = (_) {};
    c.install();
    FlutterError.onError!(FlutterErrorDetails(exception: Exception('layout broke'), stack: StackTrace.current));
    expect(c.reports.value.first.error, contains('layout broke'));
  });

  test('send posts every report to the signed-in server', () async {
    StorageService.instance.resetForTest();
    FlutterSecureStorage.setMockInitialValues({'server_url': 'http://nas.test', 'access_token': 'tok', 'refresh_token': 'r', 'username': 'alex'});
    await StorageService.instance.init();
    await ApiClient.instance.init();
    DeviceSyncService.deviceInfoOverride = ('Google', 'Pixel 8', 'Android 16');
    addTearDown(() => DeviceSyncService.deviceInfoOverride = null);
    c.record(StateError('boom'), StackTrace.current, source: 'platform');
    http.Request? sent;
    await http.runWithClient(c.send, () => MockClient((r) async {
          if (r.url.path == '/v1/sys/crash-reports') sent = r;
          return http.Response(jsonEncode({'success': 200, 'data': {'id': 'x'}}), 200, headers: {'content-type': 'application/json'});
        }));
    expect(sent, isNotNull);
    expect(sent!.method, 'POST');
    expect(sent!.headers['Authorization'], contains('tok'));
    final body = jsonDecode(sent!.body) as Map<String, dynamic>;
    expect((body['reports'] as List).single['error'], contains('boom'));
  });
}
