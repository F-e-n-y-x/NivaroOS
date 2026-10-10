import 'dart:convert';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/models/dashboard_stats.dart';
import 'package:nivaroos_mobile/phone_backup/pb_client.dart';
import 'package:nivaroos_mobile/phone_backup/pb_service.dart';
import 'package:nivaroos_mobile/phone_backup/pb_store.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/background_service.dart';
import 'package:nivaroos_mobile/services/download_station_api.dart';
import 'package:nivaroos_mobile/services/home_widgets.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final now = DateTime(2026, 10, 11, 14, 5);
  final stats = DashboardStats.fromUtilization({
    'cpu': {'percent': 12.6},
    'mem': {'total': 1000, 'used': 400},
  }).withDisks([DiskUsage.fromJson({'mount_point': '/DATA', 'size_bytes': 1000, 'used_bytes': 630})]);

  test('percent and time formatting', () {
    expect(widgetPercent(12.6), '13%');
    expect(widgetPercent(140), '100%');
    expect(widgetPercent(null), '—');
    expect(widgetPercent(double.nan), '—');
    expect(widgetPercent(-1), '—');
    expect(widgetTime(DateTime(2026, 10, 11, 9, 3), now), '09:03');
    expect(widgetTime(DateTime(2026, 10, 3, 22, 40), now), '3 Oct, 22:40');
  });

  test('online reading: numbers, when, and when it turns stale', () {
    final f = serverFields(stats, now);
    expect(f['status'], 'Online');
    expect(f['cpu'], '13%');
    expect(f['ram'], '40%');
    expect(f['disk'], '63%');
    expect(f['updated'], 'Updated 14:05');
    expect(f['stale'], 'Not updated since 14:05');
    expect(f['stale_at'], now.add(HomeWidgets.staleAfter).millisecondsSinceEpoch);
  });

  test('no drives or memory read: dashes, not 0%', () {
    final f = serverFields(DashboardStats.fromUtilization({'cpu': {'percent': 5}}), now);
    expect(f['ram'], '—');
    expect(f['disk'], '—');
  });

  test('offline keeps the last numbers: only the status and stale time', () {
    expect(serverFields(null, now), {'status': 'Offline', 'stale_at': now.add(HomeWidgets.staleAfter).millisecondsSinceEpoch});
  });

  test('downloads line', () {
    expect(downloadsText(null), 'Download Station not installed');
    expect(downloadsText(0), 'No active downloads');
    expect(downloadsText(1), '1 active download');
    expect(downloadsText(3), '3 active downloads');
  });

  group('backup line', () {
    final cred = DeviceCredential(server: 'http://nas', deviceId: 'd', token: 't');
    const settings = PhoneBackupSettings();

    test('not set up: the tile opens the app', () {
      final f = backupFields(const PhoneBackupStatus(), now);
      expect(f, {'backup': 'Phone backup not set up', 'backup_ready': false, 'backup_any_network': false});
    });

    test('running, with progress', () {
      final st = PhoneBackupState(running: true, heartbeat: DateTime.now(), progress: const RunProgress(done: 5, total: 20));
      expect(backupFields(PhoneBackupStatus(credential: cred, settings: settings, state: st), now)['backup'], 'Backing up · 5 of 20');
    });

    test('a dead run (old heartbeat) is not running', () {
      final st = PhoneBackupState(running: true, heartbeat: DateTime(2020), outcome: RunOutcomes.failed);
      expect(backupFields(PhoneBackupStatus(credential: cred, settings: settings, state: st), now)['backup'], 'Backup failed');
    });

    test('paused, done, never run; mobile data follows Wi-Fi only', () {
      expect(backupFields(PhoneBackupStatus(credential: cred, settings: settings.copyWith(paused: true)), now)['backup'], 'Phone backup paused');
      final done = PhoneBackupState(outcome: RunOutcomes.success, lastSuccessAt: DateTime(2026, 10, 11, 2, 0));
      final f = backupFields(PhoneBackupStatus(credential: cred, settings: settings, state: done), now);
      expect(f['backup'], 'Backed up 02:00');
      expect(f['backup_ready'], true);
      expect(f['backup_any_network'], false);
      expect(backupFields(PhoneBackupStatus(credential: cred, settings: settings), now)['backup'], 'Not backed up yet');
    });
  });

  group('refresh', () {
    const channel = MethodChannel('com.fenyx.nivaroos/home_widgets');
    late List<Map<Object?, Object?>> pushed;

    setUp(() {
      pushed = [];
      BackgroundService.debugIsAndroid = true;
      ApiClient.instance.setBaseUrl('http://nas.test');
      ApiClient.instance.setSession('t', 'r');
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(channel, (call) async {
        pushed.add(call.arguments as Map<Object?, Object?>);
        return true;
      });
    });

    tearDown(() {
      BackgroundService.debugIsAndroid = null;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(channel, null);
    });

    test('a server that answers: numbers and active downloads (links + torrents still downloading)', () async {
      final client = MockClient((req) async => switch (req.url.path) {
            '/v1/sys/utilization' => http.Response(jsonEncode({'data': {'cpu': {'percent': 50}, 'mem': {'usedPercent': 25}}}), 200),
            '/v1/sys/disks-usage' => http.Response(jsonEncode({'data': []}), 200),
            '/v1/download-station/downloads' => http.Response(jsonEncode({'data': [{'state': 'downloading'}, {'state': 'completed'}, {'state': 'queued'}]}), 200),
            '/v1/download-station/torrents' =>
              http.Response(jsonEncode({'torrents': [{'state': 'downloading', 'progress': 0.5}, {'state': 'seeding', 'progress': 1}, {'state': 'paused', 'progress': 0.1}]}), 200),
            _ => http.Response('{}', 404),
          });
      await http.runWithClient(() => HomeWidgets.instance.refresh(), () => client);
      expect(pushed, hasLength(1));
      final f = pushed.single;
      expect(f['signed_in'], true);
      expect(f['server'], 'nas.test');
      expect([f['status'], f['cpu'], f['ram'], f['disk']], ['Online', '50%', '25%', '—']);
      expect(f['downloads'], '3 active downloads');
    });

    test('a server that does not answer: Offline, the last numbers and downloads stay', () async {
      final client = MockClient((req) async => http.Response('{}', 503));
      await http.runWithClient(() => HomeWidgets.instance.refresh(), () => client);
      final f = pushed.single;
      expect(f['status'], 'Offline');
      expect(f.containsKey('cpu'), isFalse);
      expect(f.containsKey('downloads'), isFalse);
    });

    test('Download Station not installed', () async {
      final client = MockClient((req) async => http.Response('{}', 404));
      expect(await http.runWithClient(() => activeDownloads(DownloadStationApi()), () => client), isNull);
    });
  });
}
