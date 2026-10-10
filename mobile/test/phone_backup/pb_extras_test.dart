// Phone backup extras: the "on new photos" trigger settings, and reading
// another phone's backup with the owner's session (GET only) to restore
// it onto this phone.
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart' as crypto;
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/phone_backup/pb_client.dart';
import 'package:nivaroos_mobile/phone_backup/pb_models.dart';
import 'package:nivaroos_mobile/phone_backup/pb_platform.dart';
import 'package:nivaroos_mobile/phone_backup/pb_restore.dart';
import 'package:nivaroos_mobile/phone_backup/pb_schedule.dart';
import 'package:nivaroos_mobile/phone_backup/pb_service.dart';
import 'package:nivaroos_mobile/phone_backup/pb_store.dart';
import 'package:nivaroos_mobile/screens/phone_backup/phone_backup_other_phones.dart';

import '../screenshots/harness.dart';

const _other = 'dev_9b7e2c41d0a3f586';

Map<String, Object?> _env(Object? data) => {'success': 200, 'message': 'ok', 'data': data};

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('on new photos', () {
    test('kept with the settings; armed only with photos on and not paused', () {
      const s = PhoneBackupSettings(onNewPhotos: true);
      expect(PhoneBackupSettings.fromJson(s.toJson()).onNewPhotos, isTrue);
      expect(PhoneBackupSettings.fromJson(const PhoneBackupSettings().toJson()).onNewPhotos, isFalse);
      expect(PhoneBackupSettings.fromJson({'categories': ['media']}).onNewPhotos, isFalse, reason: 'settings saved before the option');
      expect(s.watchesNewPhotos, isTrue);
      expect(s.copyWith(paused: true).watchesNewPhotos, isFalse);
      expect(s.copyWith(categories: {PhoneCategory.contacts}).watchesNewPhotos, isFalse);
      expect(const PhoneBackupSettings().watchesNewPhotos, isFalse);
    });

    test('debounce: waits a few minutes after the last change, at most half an hour', () {
      expect(PhotoTrigger.settle, greaterThanOrEqualTo(const Duration(minutes: 1)));
      expect(PhotoTrigger.settle, lessThanOrEqualTo(const Duration(minutes: 10)));
      expect(PhotoTrigger.maxWait, greaterThan(PhotoTrigger.settle));
      final a = PhotoTrigger.args(on: true, conditions: const RunConditions(wifiOnly: true, chargingOnly: true));
      expect(a, {'on': true, 'settleMs': PhotoTrigger.settle.inMilliseconds, 'maxMs': PhotoTrigger.maxWait.inMilliseconds, 'wifiOnly': true, 'charging': true});
      expect(PhotoTrigger.args(on: false, conditions: const RunConditions(wifiOnly: false))['wifiOnly'], isFalse);
    });

    test('the summary says so, with the conditions even without a schedule', () {
      expect(scheduleSummary(const PhoneBackupSettings(schedule: BackupSchedule(hour: 3), onNewPhotos: true)), 'Every day at 3:00 · also on new photos · Wi-Fi only');
      expect(scheduleSummary(const PhoneBackupSettings(schedule: BackupSchedule.manual, onNewPhotos: true)), 'Only when you tap Back up now · also on new photos · Wi-Fi only');
      expect(scheduleSummary(const PhoneBackupSettings(schedule: BackupSchedule.manual)), 'Only when you tap Back up now');
    });
  });

  group('another phone', () {
    setUp(() async {
      await signIn();
    });

    test('the list leaves out this phone, most recent backup first', () {
      final list = otherPhones([
        PhoneDevice(id: 'dev_me', lastBackupAt: DateTime.utc(2026, 9, 30)),
        const PhoneDevice(id: 'dev_new'),
        PhoneDevice(id: 'dev_old', lastBackupAt: DateTime.utc(2026, 1, 1)),
        PhoneDevice(id: 'dev_recent', lastBackupAt: DateTime.utc(2026, 9, 1)),
        const PhoneDevice(id: ''),
      ], 'dev_me');
      expect([for (final d in list) d.id], ['dev_recent', 'dev_old', 'dev_new']);
      expect(otherPhones([const PhoneDevice(id: 'dev_a')], null).single.id, 'dev_a');
    });

    test('reads with the owner session, never a device token, and only GETs', () async {
      final seen = <String>[];
      final client = MockClient((req) async {
        seen.add('${req.method} ${req.url.path} ${req.headers['Authorization']}');
        return http.Response(jsonEncode(_env([
          {'id': 'snap_1', 'taken_at': '2026-08-30T19:12:00Z', 'categories': ['media'], 'status': 'success'},
        ])), 200);
      });
      final service = PhoneBackupService(credentials: MemoryCredentialStore(), store: PhoneBackupStore(Directory.systemTemp.createTempSync('pbx')), platform: false, httpClient: client);
      final c = (await service.readingPhone(_other).deviceClient())!;
      expect(c.readsOtherPhone, isTrue);
      final snaps = await c.snapshots();
      expect(snaps.single.id, 'snap_1');
      expect(seen, ['GET /v1/backup/devices/$_other/snapshots test-token']);

      for (final write in [
        () => c.startSession(['media']),
        () => c.finish('ses_1', 'success', const []),
        () => c.deleteUpload('up_1'),
        () => c.putPhoneSettings(const {}),
      ]) {
        await expectLater(write(), throwsA(isA<PhoneBackupError>().having((e) => e.code, 'code', 'read_only')));
      }
      expect(seen, hasLength(1), reason: 'nothing but the GET reached the server');
    });

    test('a refused session asks to sign in again, not to link this phone', () async {
      final client = MockClient((req) async => http.Response(jsonEncode({'success': 401, 'message': 'unauthorized'}), 401));
      await http.runWithClient(() async {
        final c = DeviceClient.owner(server: fakeServer, deviceId: _other, client: client);
        await expectLater(
          c.snapshots(),
          throwsA(isA<PhoneBackupError>().having((e) => e.code, 'code', 'signed_out').having((e) => e.revoked, 'revoked', isFalse)),
        );
      }, () => client);
    });

    test('restore everything missing of its photos onto this phone', () async {
      final bytes = utf8.encode('jpeg bytes of the other phone');
      final sha = crypto.sha256.convert(bytes).toString();
      final seen = <String>[];
      final client = MockClient((req) async {
        seen.add('${req.method} ${req.url.path}?${req.url.query} ${req.headers['Authorization']}');
        if (req.url.path.endsWith('/restore-manifest')) {
          return http.Response(jsonEncode(_env({
            'items': [
              {'category': 'media', 'path': 'DCIM/Camera/a.jpg', 'size': 5, 'sha256': 'x'},
              {'category': 'media', 'path': 'DCIM/Camera/b.jpg', 'size': bytes.length, 'sha256': sha, 'taken_at': 1756581120000, 'mtime': 1756581121000},
            ],
            'next_after': '',
          })), 200);
        }
        return http.Response.bytes(bytes, 200, headers: {'x-nivaroos-sha256': sha});
      });
      final calls = <MethodCall>[];
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(ChannelPhoneSources.channel, (call) async {
        calls.add(call);
        return switch (call.method) {
          'mediaExists' => (call.arguments as Map)['name'] == 'a.jpg',
          'saveToMedia' => 'content://media/external/images/media/9',
          _ => null,
        };
      });
      addTearDown(() => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(ChannelPhoneSources.channel, null));

      final service = PhoneBackupService(credentials: MemoryCredentialStore(), store: PhoneBackupStore(Directory.systemTemp.createTempSync('pbx')), platform: false, httpClient: client);
      final r = PhoneRestore((await service.readingPhone(_other).deviceClient())!, await service.store);
      final sum = await r.restoreMissing('snap_1', 'media');

      expect((sum.restored, sum.skipped, sum.failed), (1, 1, 0));
      expect(seen, hasLength(2));
      expect(seen.every((s) => s.contains('/v1/backup/devices/$_other/') && s.endsWith(' test-token')), isTrue, reason: '$seen');
      expect(seen.last, contains('/files/content?'));
      expect(seen.last, contains('path=DCIM%2FCamera%2Fb.jpg'));
      final save = calls.singleWhere((c) => c.method == 'saveToMedia').arguments as Map;
      expect(save['relativePath'], 'DCIM/Camera');
      expect(save['name'], 'b.jpg');
      expect(save['takenAt'], 1756581120000);
    });
  });
}
