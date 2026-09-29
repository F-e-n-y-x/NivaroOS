// A phone for the engine tests: media and folder files in memory, rows for
// messages and calls, and a record of what the engine scheduled and showed.
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:nivaroos_mobile/phone_backup/pb_manifest.dart';
import 'package:nivaroos_mobile/phone_backup/pb_platform.dart';
import 'package:nivaroos_mobile/phone_backup/pb_tus.dart';

class FakePhone implements PhoneSources {
  final Map<String, Uint8List> media = {}; // path -> bytes
  final Map<String, int> mtimes = {};
  final List<Map<Object?, Object?>> sms = [];
  final List<Map<Object?, Object?>> calls = [];
  String? contactsVcf;
  bool contactsDenied = false;
  int hashes = 0;
  final List<String> reads = [];
  final List<Duration> scheduled = [];
  int cancels = 0;
  final List<String> notifications = [];

  void addMedia(String path, String content, {int mtime = 1000}) {
    media[path] = Uint8List.fromList(utf8.encode(content));
    mtimes[path] = mtime;
  }

  @override
  Future<List<ScannedFile>> scanMedia() async => [
        for (final e in media.entries) ScannedFile(path: e.key, size: e.value.length, mtime: mtimes[e.key] ?? 1000, source: 'content://media/${e.key}', mediaId: 1),
      ];

  @override
  Future<List<ScannedFile>> scanTree(String treeUri, String label) async => const [];

  Uint8List _bytes(String source) => media[source.replaceFirst('content://media/', '')]!;

  @override
  Future<String> sha256(String source) async {
    hashes++;
    return sha256Of(_bytes(source));
  }

  static String sha256Of(List<int> b) => sha256Hash(b);

  @override
  ByteSource open(String source, int length) {
    reads.add(source);
    return MemoryByteSource(_bytes(source));
  }

  @override
  Future<String?> exportContacts(String dir) async {
    if (contactsDenied) throw Exception('SecurityException: Permission Denial: reading contacts');
    if (contactsVcf == null) return null;
    final f = File('$dir/contacts.vcf')..writeAsStringSync(contactsVcf!);
    return f.path;
  }

  @override
  Future<List<CalendarExport>> exportCalendars(String dir) async => const [];

  @override
  Future<List<Map<Object?, Object?>>> readSms(int offset, int limit) async => sms.skip(offset).take(limit).toList();

  @override
  Future<List<Map<Object?, Object?>>> readMms(int offset, int limit) async => const [];

  @override
  Future<List<Map<Object?, Object?>>> mmsParts(int mmsId, {int maxBytes = 20 << 20}) async => const [];

  @override
  Future<List<Map<Object?, Object?>>> readCallLog(int offset, int limit) async => calls.skip(offset).take(limit).toList();

  @override
  Future<List<InstalledApp>> installedApps() async => const [InstalledApp(package: 'org.example', label: 'Example', versionCode: 3)];

  @override
  Future<Map<String, Object?>> readSettings() async => {'screen_off_timeout': 30000};

  @override
  Future<Map<String, bool>> permissions(List<String> names) async => {for (final n in names) n: true};

  @override
  Future<void> progress({required String title, required String text, int done = 0, int total = 0, bool ongoing = true}) async => notifications.add(text);

  @override
  Future<void> endProgress({String? title, String? text}) async => notifications.add('end: $title');

  @override
  Future<void> schedule({required Duration delay, required bool wifiOnly, required bool charging}) async => scheduled.add(delay);

  @override
  Future<void> cancelSchedule() async => cancels++;
}

String sha256Hash(List<int> b) => sha256.convert(b).toString();
