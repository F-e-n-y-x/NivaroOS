import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/phone_backup/pb_manifest.dart';
import 'package:nivaroos_mobile/phone_backup/pb_messages.dart';
import 'package:nivaroos_mobile/phone_backup/pb_models.dart';
import 'package:nivaroos_mobile/phone_backup/pb_schedule.dart';
import 'package:nivaroos_mobile/phone_backup/pb_store.dart';

import 'fake_device_server.dart';

ScannedFile file(String path, {int size = 10, int mtime = 1000}) => ScannedFile(path: path, size: size, mtime: mtime, source: 'content://x/$path');

void main() {
  group('item keys', () {
    // The same vectors as services/backup jobs.ItemKey (phone_test.go).
    test('sms key matches the server formula', () {
      expect(smsKey('+4912345', '1700000000000', '1', 'Hello & welcome'), 'a00bcdb728166154875ea4993efb10a9f3bdbfb6c80c0be9e10ff88fe7a9c417');
    });
    test('mms and call keys', () {
      expect(mmsKey('+4912345~+4967890', '1700000000000', '2', '<abc@mms>'), 'f44a75bcef0bd1ef1004b3326df1cb8c66946c448f8a8bdc6269be903fd746e3');
      expect(callKey('+4912345', '1700000000000', '42'), '78bf869332a947a2b4923835891c7da1d64cbf34b177c6aedee8bbb3a46d6c88');
    });

    test('a row becomes the same key the server computes from the file', () async {
      final items = [
        smsFromRow({'_id': 1, 'address': '+4912345', 'date': 1700000000000, 'type': 1, 'body': 'Hello & welcome\n<b>"hi"</b>\u0001', 'read': 1}),
        smsFromRow({'_id': 2, 'address': null, 'date': 1700000000001, 'type': 2, 'body': null}),
        mmsFromRow({
          '_id': 7,
          'date': 1700000000, // seconds in the provider
          'msg_box': 2,
          'm_id': '<abc@mms>',
          'addrs': [
            {'address': '+4900000', 'type': 137},
            {'address': '+4912345', 'type': 151},
            {'address': '+4967890', 'type': 151},
          ],
        }),
      ];
      expect(items[2].attrs['date'], '1700000000000');
      expect(items[2].attrs['address'], '+4912345~+4967890');
      expect(items[2].key, 'f44a75bcef0bd1ef1004b3326df1cb8c66946c448f8a8bdc6269be903fd746e3');
      // The control character is dropped before the key is made.
      expect(items[0].attrs['body'], 'Hello & welcome\n<b>"hi"</b>');
      final dir = Directory.systemTemp.createTempSync('pbxml');
      addTearDown(() => dir.deleteSync(recursive: true));
      final f = File('${dir.path}/sms.xml');
      await writeSmsXml(f, items, now: DateTime.utc(2026, 9, 30));
      final xml = f.readAsStringSync();
      expect(xml, startsWith("<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>\n<smses count=\"3\""));
      expect(xml, contains('body="Hello &amp; welcome&#10;&lt;b&gt;&quot;hi&quot;&lt;/b&gt;"'));
      expect(FakeDeviceServer.serverKeys(xml), [for (final i in items) i.key]);
    });

    test('calls', () async {
      final c = callFromRow({'number': '+4912345', 'date': 1700000000000, 'duration': 42, 'type': 1, 'name': 'Alex'});
      expect(c.key, callKey('+4912345', '1700000000000', '42'));
      final dir = Directory.systemTemp.createTempSync('pbxml');
      addTearDown(() => dir.deleteSync(recursive: true));
      final f = File('${dir.path}/calls.xml');
      await writeCallsXml(f, [c]);
      final xml = f.readAsStringSync();
      expect(xml, contains('<calls count="1"'));
      expect(xml, contains('contact_name="Alex"'));
      expect(FakeDeviceServer.serverKeys(xml), [c.key]);
    });
  });

  group('manifest', () {
    test('known hash only while size and mtime are unchanged', () {
      final m = LocalManifest();
      m.record(file('DCIM/a.jpg'), 'aa');
      expect(m.knownHash(file('DCIM/a.jpg')), 'aa');
      expect(m.knownHash(file('DCIM/a.jpg', size: 11)), '');
      expect(m.knownHash(file('DCIM/a.jpg', mtime: 2000)), '');
      expect(m.checkItem(file('DCIM/a.jpg')).toJson(), {'path': 'DCIM/a.jpg', 'size': 10, 'mtime': 1000, 'sha256': 'aa'});
      expect(m.checkItem(file('DCIM/b.jpg')).sha256, '');
    });

    test('gone lists what the scan no longer has, and survives a round trip', () {
      final m = LocalManifest();
      for (final p in ['DCIM/a.jpg', 'DCIM/b.jpg', 'Pictures/c.png']) {
        m.record(file(p), 'h$p');
      }
      expect(m.gone(['DCIM/b.jpg']), ['DCIM/a.jpg', 'Pictures/c.png']);
      m.forget(['DCIM/a.jpg']);
      final back = LocalManifest.decode(m.encode());
      expect(back.entries.keys, unorderedEquals(['DCIM/b.jpg', 'Pictures/c.png']));
      expect(back.knownHash(file('DCIM/b.jpg')), 'hDCIM/b.jpg');
      expect(LocalManifest.decode('not json').entries, isEmpty);
    });

    test('backup path rules', () {
      expect(validBackupPath('DCIM/Camera/PXL_1.jpg'), isTrue);
      expect(validBackupPath('/DCIM/a.jpg'), isFalse);
      expect(validBackupPath('DCIM//a.jpg'), isFalse);
      expect(validBackupPath('DCIM/../a.jpg'), isFalse);
      expect(validBackupPath('DCIM/a\\b.jpg'), isFalse);
      expect(validBackupPath('DCIM/a\u0007.jpg'), isFalse);
      expect(validBackupPath('${'x' * 256}/a.jpg'), isFalse);
      expect(safeSegment('Work/Docs'), 'Work_Docs');
      expect(safeSegment('..'), '_');
    });
  });

  group('schedule', () {
    const daily = BackupSchedule(hour: 3, minute: 0);
    const weekly = BackupSchedule(kind: ScheduleKind.weekly, weekday: DateTime.sunday, hour: 2, minute: 30);

    test('next slot', () {
      expect(daily.slotAfter(DateTime(2026, 9, 30, 2, 59)), DateTime(2026, 9, 30, 3));
      expect(daily.slotAfter(DateTime(2026, 9, 30, 3)), DateTime(2026, 10, 1, 3));
      // 2026-09-30 is a Wednesday.
      expect(weekly.slotAfter(DateTime(2026, 9, 30, 12)), DateTime(2026, 10, 4, 2, 30));
      expect(weekly.slotAfter(DateTime(2026, 10, 4, 2, 30)), DateTime(2026, 10, 11, 2, 30));
      expect(BackupSchedule.manual.slotAfter(DateTime(2026)), isNull);
    });

    test('a missed slot runs now; the first run waits for its slot', () {
      final now = DateTime(2026, 9, 30, 10);
      expect(daily.nextRun(now: now), DateTime(2026, 10, 1, 3));
      expect(daily.nextRun(now: now, lastBackup: DateTime(2026, 9, 29, 3, 5)), now);
      expect(daily.nextRun(now: now, lastBackup: DateTime(2026, 9, 30, 3, 5)), DateTime(2026, 10, 1, 3));
      expect(weekly.nextRun(now: now, lastBackup: DateTime(2026, 9, 27, 2, 40)), DateTime(2026, 10, 4, 2, 30));
      expect(BackupSchedule.manual.nextRun(now: now, lastBackup: DateTime(2020)), isNull);
    });

    test('a waiting backup retries within the hour, or at an earlier slot', () {
      expect(daily.retryAt(now: DateTime(2026, 9, 30, 10)), DateTime(2026, 9, 30, 11));
      expect(daily.retryAt(now: DateTime(2026, 9, 30, 2, 30)), DateTime(2026, 9, 30, 3));
      expect(BackupSchedule.manual.retryAt(now: DateTime(2026, 9, 30, 10)), DateTime(2026, 9, 30, 11));
    });

    test('wire form round trip and labels', () {
      expect(daily.wire, 'daily 03:00');
      expect(weekly.wire, 'weekly sun 02:30');
      expect(BackupSchedule.parse('weekly sun 02:30'), weekly);
      expect(BackupSchedule.parse('daily 03:00'), daily);
      expect(BackupSchedule.parse('on new photos').kind, ScheduleKind.manual);
      expect(weekly.label, 'Every Sunday at 2:30');
      expect(const RunConditions(wifiOnly: true, chargingOnly: true).wire, ['wifi', 'charging']);
      expect(const RunConditions().allows(onUnmetered: false, charging: true), isFalse);
      expect(const RunConditions(wifiOnly: false).allows(onUnmetered: false, charging: false), isTrue);
    });
  });

  group('settings and state', () {
    test('settings round trip, folders only when picked', () {
      const s = PhoneBackupSettings(
        categories: {PhoneCategory.media, PhoneCategory.files, PhoneCategory.sms},
        conditions: RunConditions(wifiOnly: false, chargingOnly: true),
        schedule: BackupSchedule(kind: ScheduleKind.weekly, weekday: 1, hour: 4),
      );
      expect(s.runCategories, [PhoneCategory.media, PhoneCategory.sms]);
      final back = PhoneBackupSettings.fromJson(s.toJson());
      expect(back.categories, s.categories);
      expect(back.conditions.chargingOnly, isTrue);
      expect(back.conditions.wifiOnly, isFalse);
      expect(back.schedule, s.schedule);
      final withFolder = back.copyWith(folders: [const SafFolder(uri: 'content://tree/1', label: 'Documents')]);
      expect(withFolder.runCategories, [PhoneCategory.media, PhoneCategory.files, PhoneCategory.sms]);
      final ps = withFolder.toPhoneSettings(appVersion: '1.4.3', osVersion: 'Android 15', model: 'Pixel 8');
      expect((ps['categories'] as Map)['media'], {'enabled': true, 'schedule': 'weekly mon 04:00', 'conditions': ['charging'], 'encrypted': false});
      expect(((ps['categories'] as Map)['contacts'] as Map)['enabled'], isFalse);
    });

    test('state round trip', () {
      final s = PhoneBackupState(
        outcome: RunOutcomes.waitingDrive,
        lastSuccessAt: DateTime.utc(2026, 9, 29, 3),
        categories: {'media': CategoryRun(at: DateTime.utc(2026, 9, 29, 3), status: 'success', files: 3, sent: 1, bytes: 10)},
        errors: const [
          {'category': 'media', 'path': 'a', 'message': 'x'},
        ],
        sessionId: 'ses_1',
        sessionDone: const ['media'],
      );
      final back = PhoneBackupState.fromJson(s.toJson());
      expect(back.outcome, RunOutcomes.waitingDrive);
      expect(back.lastSuccessAt!.toUtc(), DateTime.utc(2026, 9, 29, 3));
      expect(back.categories['media']!.sent, 1);
      expect(back.errors.single['path'], 'a');
      expect(back.sessionDone, ['media']);
      expect(back.isRunning, isFalse);
    });
  });

  group('models', () {
    test('config with a missing drive', () {
      final c = PhoneConfig.fromJson({
        'device': {'id': 'dev_1', 'name': 'Pixel 8'},
        'destination': {'path': '/media/usb/Phones/Pixel 8', 'online': false, 'default': false, 'error_code': 'dest_offline', 'location': {'kind': 'usb', 'label': 'Backup Stick'}},
        'settings': {'keep_last': 5, 'keep_days': 0, 'deleted_purge_days': 90},
        'limits': {'chunk_size': 8388608, 'check_batch': 500},
        'categories': [
          {'category': 'media'},
        ],
      });
      expect(c.destination.driveMissing, isTrue);
      expect(c.destination.usable, isFalse);
      expect(c.destination.label, 'Backup Stick');
      expect(c.limits.chunkSize, 8 << 20);
      expect(c.limits.deletedBatch, 1000);
      expect(c.categories, ['media']);
      expect(c.retention.summary, 'The newest 5 backups. Files you delete on the phone are kept for 90 days.');
      expect(const PhoneRetention().summary, 'The newest 10 backups, and one a day for 30 days. Files you delete on the phone are kept.');
    });
  });
}
