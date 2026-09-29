// Whole runs of the phone backup engine against the fake device API and a
// fake phone: first backup, the incremental next one, deleted files, a
// missing drive, a revoked token, messages sent once, slices, cancel.
import 'dart:io';

import 'package:clock/clock.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/phone_backup/pb_client.dart';
import 'package:nivaroos_mobile/phone_backup/pb_engine.dart';
import 'package:nivaroos_mobile/phone_backup/pb_models.dart';
import 'package:nivaroos_mobile/phone_backup/pb_platform.dart';
import 'package:nivaroos_mobile/phone_backup/pb_schedule.dart';
import 'package:nivaroos_mobile/phone_backup/pb_store.dart';

import 'fake_device_server.dart';
import 'fake_phone.dart';

void main() {
  late FakeDeviceServer server;
  late FakePhone phone;
  late PhoneBackupStore store;
  late Directory dir;
  final now = DateTime(2026, 9, 30, 10);

  setUp(() {
    server = FakeDeviceServer();
    phone = FakePhone()
      ..addMedia('DCIM/Camera/a.jpg', 'photo a ' * 5)
      ..addMedia('DCIM/Camera/b.jpg', 'photo b ' * 3)
      ..addMedia('Pictures/c.png', 'c')
      ..addMedia('Movies/d.mp4', 'movie d ' * 9);
    dir = Directory.systemTemp.createTempSync('pbengine');
    store = PhoneBackupStore(dir);
  });
  tearDown(() => dir.deleteSync(recursive: true));

  PhoneBackupEngine engine({PhoneBackupSettings? settings}) => PhoneBackupEngine(
        client: DeviceClient(const DeviceCredential(server: 'http://nas.test', deviceId: 'dev_1', token: tokenA), client: server.client),
        phone: phone,
        store: store,
        settings: settings ?? const PhoneBackupSettings(categories: {PhoneCategory.media}, schedule: BackupSchedule(hour: 3)),
        sleep: (_) async {},
      );

  Future<RunResult> run({PhoneBackupSettings? settings, JobStart start = const JobStart(reason: 'manual'), DateTime? at}) =>
      withClock(Clock.fixed(at ?? now), () => engine(settings: settings).run(start));

  test('the first backup sends every photo, the next one nothing', () async {
    final r = await run();
    expect(r.end, RunEnd.done);
    expect(r.outcome, 'success');
    expect(r.snapshotId, isNotEmpty);
    expect(server.files['media']!.keys, unorderedEquals(phone.media.keys));
    for (final e in phone.media.entries) {
      expect(server.stored['media/${e.key}'], e.value);
    }
    expect(server.finished.single['status'], 'success');
    final st = store.loadState();
    expect(st.outcome, 'success');
    expect(st.isRunning, isFalse);
    expect(st.categories['media']!.sent, 4);
    expect(st.lastSuccessAt, now);
    // Daily at 3:00: next is tomorrow 3:00.
    expect(st.nextRunAt, DateTime(2026, 10, 1, 3));
    expect(phone.scheduled.last, DateTime(2026, 10, 1, 3).difference(now));

    // Second run: the hashes are known, the check answers have for all.
    final hashesBefore = phone.hashes;
    phone.reads.clear();
    final r2 = await run(at: now.add(const Duration(days: 1)));
    expect(r2.outcome, 'success');
    expect(phone.hashes, hashesBefore);
    expect(phone.reads, isEmpty);
    expect(store.loadState().categories['media']!.sent, 0);
  });

  test('a changed file is hashed and sent again; a deleted one is reported', () async {
    await run();
    phone.addMedia('DCIM/Camera/a.jpg', 'edited a', mtime: 2000);
    phone.media.remove('Pictures/c.png');
    final r = await run(at: now.add(const Duration(days: 1)));
    expect(r.outcome, 'success');
    expect(server.stored['media/DCIM/Camera/a.jpg'], phone.media['DCIM/Camera/a.jpg']);
    expect(server.deletedMarks['media'], {'Pictures/c.png'});
    expect(store.loadManifest('media').entries.containsKey('Pictures/c.png'), isFalse);
    expect(store.loadState().categories['media']!.sent, 1);
  });

  test('a missing drive waits and retries within the hour, no session', () async {
    server.destError = 'dest_offline';
    final r = await run();
    expect(r.end, RunEnd.waiting);
    expect(r.outcome, RunOutcomes.waitingDrive);
    expect(server.log.any((l) => l.contains('/sessions')), isFalse);
    final st = store.loadState();
    expect(st.outcome, RunOutcomes.waitingDrive);
    expect(st.message, contains('isn’t connected'));
    expect(st.nextRunAt, now.add(const Duration(hours: 1)));
    expect(phone.notifications.last, 'end: Backup drive not connected');
  });

  test('the drive going away mid-run is a wait, not an error loop', () async {
    // Uploads start, then the drive disappears.
    var patches = 0;
    final inner = server.handle;
    final c = DeviceClient(const DeviceCredential(server: 'http://nas.test', deviceId: 'dev_1', token: tokenA), client: MockClient((req) {
      if (req.method == 'PATCH' && ++patches == 2) server.destError = 'dest_offline';
      return inner(req);
    }));
    final e = PhoneBackupEngine(client: c, phone: phone, store: store, settings: const PhoneBackupSettings(categories: {PhoneCategory.media}), sleep: (_) async {});
    final r = await withClock(Clock.fixed(now), () => e.run(const JobStart()));
    expect(r.outcome, RunOutcomes.waitingDrive);
    expect(server.finished, isEmpty);
    // Back again: the same session goes on, the partial upload resumes.
    server.destError = '';
    final r2 = await run(at: now.add(const Duration(hours: 1)));
    expect(r2.outcome, 'success');
    for (final e in phone.media.entries) {
      expect(server.stored['media/${e.key}'], e.value);
    }
  });

  test('a revoked token stops and cancels the schedule', () async {
    server.revoked = true;
    final r = await run();
    expect(r.end, RunEnd.revoked);
    expect(store.loadState().outcome, RunOutcomes.revoked);
    expect(store.loadState().nextRunAt, isNull);
    expect(phone.cancels, 1);
    expect(server.log.length, 1);
  });

  test('messages: only new ones are exported, and each only once', () async {
    phone.sms.addAll([
      {'_id': 1, 'address': '+491', 'date': 1700000000000, 'type': 1, 'body': 'one'},
      {'_id': 2, 'address': '+492', 'date': 1700000001000, 'type': 2, 'body': 'two & more'},
      {'_id': 3, 'address': '+491', 'date': 1700000002000, 'type': 1, 'body': 'three'},
    ]);
    const s = PhoneBackupSettings(categories: {PhoneCategory.sms});
    final r = await run(settings: s);
    expect(r.outcome, 'success');
    expect(server.exports.single['items'], 3);
    expect(store.loadState().categories['sms']!.sent, 3);
    phone.sms.add({'_id': 4, 'address': '+493', 'date': 1700000003000, 'type': 1, 'body': 'four'});
    await run(settings: s, at: now.add(const Duration(days: 1)));
    expect(server.exports.length, 2);
    expect(server.exports.last['items'], 1);
    await run(settings: s, at: now.add(const Duration(days: 2)));
    expect(server.exports.length, 2, reason: 'nothing new: no upload');
    expect(store.loadState().categories['sms']!.message, 'No new messages');
  });

  test('exports: contacts, app list, settings; a refused permission is a partial backup', () async {
    phone.contactsVcf = 'BEGIN:VCARD\nVERSION:3.0\nFN:Alex\nEND:VCARD\n';
    const s = PhoneBackupSettings(categories: {PhoneCategory.contacts, PhoneCategory.apps, PhoneCategory.settings});
    final r = await run(settings: s);
    expect(r.outcome, 'success');
    expect(server.exports.map((e) => e['category']), ['contacts', 'apps', 'settings']);
    // Same contacts again: the server says unchanged.
    await run(settings: s, at: now.add(const Duration(days: 1)));
    expect(server.exports.where((e) => e['category'] == 'contacts').length, 1);
    expect(store.loadState().categories['contacts']!.message, 'No changes');

    phone.contactsDenied = true;
    final r3 = await run(settings: s, at: now.add(const Duration(days: 2)));
    expect(r3.outcome, 'partial');
    final st = store.loadState();
    expect(st.categories['contacts']!.status, 'failed');
    expect(st.categories['contacts']!.message, contains('Allow access'));
    expect(server.finished.last['status'], 'partial');
  });

  test('out of time: the slice stops, the next goes on in the same session', () async {
    // A budget already spent (the margin is 30 s).
    final r = await run(start: const JobStart(reason: 'schedule', budget: Duration(seconds: 10)));
    expect(r.end, RunEnd.more);
    final st = store.loadState();
    expect(st.running, isTrue);
    expect(st.sessionId, isNotEmpty);
    expect(server.finished, isEmpty);
    expect(phone.scheduled, isEmpty, reason: 'the job itself goes on');
    final r2 = await run(start: const JobStart(reason: 'continue'), at: now.add(const Duration(minutes: 1)));
    expect(r2.outcome, 'success');
    expect(server.log.where((l) => l == 'POST /v1/backup/devices/dev_1/sessions').length, 2);
    expect(server.finished.length, 1);
    expect(store.loadState().running, isFalse);
  });

  test('cancel finishes the session as cancelled', () async {
    store.requestCancel();
    final r = await run();
    expect(r.end, RunEnd.cancelled);
    expect(server.finished.single['status'], 'cancelled');
    expect(store.cancelRequested, isFalse);
    expect(store.loadState().outcome, RunOutcomes.cancelled);
  });

  test('manual schedule: no next run', () async {
    final r = await run(settings: const PhoneBackupSettings(categories: {PhoneCategory.media}, schedule: BackupSchedule.manual));
    expect(r.outcome, 'success');
    expect(store.loadState().nextRunAt, isNull);
    expect(phone.cancels, 1);
  });

  test('most files vanishing at once is not reported as deleted', () async {
    for (var i = 0; i < 30; i++) {
      phone.addMedia('DCIM/Camera/IMG_$i.jpg', 'img $i');
    }
    await run();
    // A lost permission (Android returns only the app's own files) or an
    // unmounted card: 30 of 34 gone.
    phone.media.removeWhere((k, _) => k.contains('IMG_'));
    final r = await run(at: now.add(const Duration(days: 1)));
    expect(server.deletedMarks, isEmpty);
    expect(r.outcome, 'partial');
    expect(store.loadState().errors.single['message'], contains('not reporting them as deleted'));
    expect(store.loadManifest('media').entries.length, 34, reason: 'kept, so they are checked again next time');
    expect(suspiciousDeletes(19, 20), isFalse);
    expect(suspiciousDeletes(20, 30), isTrue);
    expect(suspiciousDeletes(20, 50), isFalse);
  });

  test('photos without the permission: the category fails, the rest goes on', () async {
    phone.denied.add(AndroidPermissions.readMediaVideo);
    phone.contactsVcf = 'BEGIN:VCARD\nVERSION:3.0\nFN:A\nEND:VCARD\n';
    final r = await run(settings: const PhoneBackupSettings(categories: {PhoneCategory.media, PhoneCategory.contacts}));
    expect(r.outcome, 'partial');
    final st = store.loadState();
    expect(st.categories['media']!.status, 'failed');
    expect(st.categories['media']!.message, contains('Allow access'));
    expect(st.categories['contacts']!.status, 'success');
    expect(server.files['media'], isNull);
  });
}
