// Resumable uploads against the fake device API: offsets survive a cut
// connection, an offset mismatch, a new session, and the token rotates on
// the way.
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/phone_backup/pb_client.dart';
import 'package:nivaroos_mobile/phone_backup/pb_models.dart';
import 'package:nivaroos_mobile/phone_backup/pb_tus.dart';

import 'fake_device_server.dart';

void main() {
  late FakeDeviceServer server;
  late DeviceClient client;
  late List<DeviceCredential> rotated;

  Future<String> session() async => (await client.startSession(['media'])).id;

  Uint8List bytes(int n) => Uint8List.fromList(List.generate(n, (i) => i % 251));

  setUp(() {
    server = FakeDeviceServer();
    rotated = [];
    client = DeviceClient(const DeviceCredential(server: 'http://nas.test', deviceId: 'dev_1', token: tokenA), client: server.client, onRotated: rotated.add);
  });

  UploadMeta meta(String sid, Uint8List b, {String path = 'DCIM/a.jpg'}) =>
      UploadMeta(session: sid, category: 'media', path: path, sha256: FakeDeviceServer.sha(b), mtime: 1000);

  test('sends every chunk of 16 bytes and places the file', () async {
    final sid = await session();
    final b = bytes(50);
    final progress = <int>[];
    final tus = TusUploader(client, chunkSize: 16, sleep: (_) async {});
    final r = await tus.upload(MemoryByteSource(b), meta(sid, b), onProgress: (o, _) => progress.add(o));
    expect(r.result, 'stored');
    expect(r.bytesSent, 50);
    expect(progress, [0, 16, 32, 48, 50]);
    expect(server.stored['media/DCIM/a.jpg'], b);
    expect(server.log.where((l) => l.startsWith('PATCH')).length, 4);
  });

  test('a cut connection resumes from what the server confirmed', () async {
    final sid = await session();
    final b = bytes(40);
    server.cutPatches = 1; // the first PATCH stores 8 of 16 bytes, then drops
    final tus = TusUploader(client, chunkSize: 16, sleep: (_) async {});
    final r = await tus.upload(MemoryByteSource(b), meta(sid, b));
    expect(r.result, 'stored');
    expect(server.stored['media/DCIM/a.jpg'], b);
    // PATCH (cut), HEAD (offset 8), PATCH 8..24, 24..40
    final calls = server.log.where((l) => l.startsWith('PATCH') || l.startsWith('HEAD')).map((l) => l.split(' ').first).toList();
    expect(calls, ['PATCH', 'HEAD', 'PATCH', 'PATCH']);
  });

  test('an offset mismatch continues at the server offset', () async {
    final sid = await session();
    final b = bytes(20);
    server.mismatchNext = true;
    final tus = TusUploader(client, chunkSize: 16, sleep: (_) async {});
    final r = await tus.upload(MemoryByteSource(b), meta(sid, b));
    expect(r.result, 'stored');
    expect(server.stored['media/DCIM/a.jpg'], b);
  });

  test('after the app was killed, the next create resumes the unfinished upload', () async {
    final sid = await session();
    final b = bytes(48);
    // First attempt: stopped after one chunk (the app is killed).
    var chunks = 0;
    final tus = TusUploader(client, chunkSize: 16, sleep: (_) async {});
    await expectLater(tus.upload(MemoryByteSource(b), meta(sid, b), shouldStop: () => chunks++ >= 1), throwsA(isA<UploadStopped>()));
    final pending = server.uploads.values.single;
    expect(pending.offset, 16);
    // A new run: create answers "resumed" at 16; only 32 bytes go.
    final sent = <int>[];
    final r = await TusUploader(client, chunkSize: 16, sleep: (_) async {}).upload(MemoryByteSource(b), meta(sid, b), onProgress: (o, _) => sent.add(o));
    expect(sent.first, 16);
    expect(r.bytesSent, 32);
    expect(server.stored['media/DCIM/a.jpg'], b);
  });

  test('the check answer offset is used with HEAD skipped', () async {
    final sid = await session();
    final b = bytes(32);
    final tus = TusUploader(client, chunkSize: 16, sleep: (_) async {});
    var chunks = 0;
    await expectLater(tus.upload(MemoryByteSource(b), meta(sid, b), shouldStop: () => chunks++ >= 1), throwsA(isA<UploadStopped>()));
    final ans = await client.check(sid, 'media', [
      CheckItem(path: 'DCIM/a.jpg', size: 32, mtime: 1000, sha256: FakeDeviceServer.sha(b)),
    ]);
    expect(ans.single.uploadId, isNotEmpty);
    expect(ans.single.offset, 16);
    server.log.clear();
    final r = await tus.upload(MemoryByteSource(b), meta(sid, b), uploadId: ans.single.uploadId, offset: ans.single.offset);
    expect(r.result, 'stored');
    expect(server.log.any((l) => l.startsWith('HEAD') || l.startsWith('POST')), isFalse);
  });

  test('a void upload (location changed) starts over', () async {
    final sid = await session();
    final b = bytes(20);
    final r = await TusUploader(client, chunkSize: 16, sleep: (_) async {}).upload(MemoryByteSource(b), meta(sid, b), uploadId: 'up_gone');
    expect(r.result, 'stored');
    expect(server.log.where((l) => l.startsWith('HEAD')).length, 1);
  });

  test('checksum mismatch is reported to the caller', () async {
    final sid = await session();
    final b = bytes(20);
    final wrong = UploadMeta(session: sid, category: 'media', path: 'DCIM/a.jpg', sha256: FakeDeviceServer.sha(utf8.encode('other')));
    await expectLater(TusUploader(client, chunkSize: 16, sleep: (_) async {}).upload(MemoryByteSource(b), wrong), throwsA(isA<ChecksumMismatch>()));
  });

  test('nothing to send when the server already has the file', () async {
    final sid = await session();
    final b = bytes(20);
    final tus = TusUploader(client, chunkSize: 16, sleep: (_) async {});
    await tus.upload(MemoryByteSource(b), meta(sid, b));
    final again = await tus.upload(MemoryByteSource(b), meta(sid, b));
    expect(again.result, 'have');
    expect(again.sent, isFalse);
  });

  test('a rotated token is stored and used from then on', () async {
    server.rotateTo = tokenB;
    await client.config();
    expect(rotated.single.token, tokenB);
    expect(client.credential.token, tokenB);
    server.valid.remove(tokenA);
    await client.config();
  });

  test('401 is revoked, never retried', () async {
    server.revoked = true;
    final e = await client.config().then<Object?>((_) => null, onError: (Object e) => e);
    expect(e, isA<PhoneBackupError>());
    expect((e as PhoneBackupError).revoked, isTrue);
    expect(server.log.length, 1);
  });

  test('metadata is base64, keys as the spec lists them', () {
    const m = UploadMeta(session: 'ses_1', category: 'calendar', name: 'Work', sha256: 'ab', mtime: 5);
    final parts = m.encode().split(',');
    expect(parts.map((p) => p.split(' ').first), ['session', 'category', 'name', 'sha256', 'mtime']);
    expect(utf8.decode(base64.decode(parts[2].split(' ')[1])), 'Work');
  });
}
