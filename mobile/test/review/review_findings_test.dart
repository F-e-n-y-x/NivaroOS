// Tests from the code + security review (2026-09-26). Each one proved a
// bug (they were red until the fix); they stay as regression tests.
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/file_entry.dart';
import 'package:nivaroos_mobile/screens/files/resumable_upload.dart';
import 'package:nivaroos_mobile/screens/files/transfers.dart';
import 'package:nivaroos_mobile/services/companion_file_server.dart';

void main() {
  group('companion file server: case-insensitive shared storage (M-20)', () {
    // Android's shared storage (FUSE/sdcardfs over /storage/emulated) and
    // FAT/exFAT SD cards are case-insensitive: "android/data" opens the
    // same folder as "Android/data". The private-folder check is a
    // case-sensitive regex, so another spelling walks straight past it.
    String? ok(String p) {
      try {
        return SharedStoragePolicy.resolve(p);
      } on PathRefused {
        return null;
      }
    }

    test("other apps' data folders are refused whatever the letter case", () {
      expect(ok('/storage/emulated/0/android/data/com.other.app'), isNull);
      expect(ok('/storage/emulated/0/ANDROID/DATA/com.other.app'), isNull);
      expect(ok('/storage/1A2B-3C4D/android/obb'), isNull);
    });
  });

  group('download to phone: temporary name (M-11)', () {
    late Directory tmp;
    setUp(() => tmp = Directory.systemTemp.createTempSync('nivaro_review'));
    tearDown(() => tmp.deleteSync(recursive: true));

    // The part file is ".<name>.nvpart": 8 characters longer than the name.
    // A name of 248-255 bytes (allowed on the server and on the phone, the
    // Files screen's own validateName accepts 255) then can't be created
    // (ENAMETOOLONG), so the download fails although the file itself would fit.
    test('a file with a long name still downloads', () async {
      final name = '${'a' * 246}.jpg'; // 250 characters, a valid name
      final outcome = await DownloadTask(
        items: [DownloadItem(FileEntry(name: name, path: '/DATA/$name', isDir: false, size: 1), name)],
        destDir: tmp.path,
        download: (remote, target, onProgress) async => target.writeAsBytes([1]),
      ).run((_) {}, TransferCancel());
      expect(outcome.failed, isFalse);
      expect(File('${tmp.path}/$name').existsSync(), isTrue);
    });
  });
}
