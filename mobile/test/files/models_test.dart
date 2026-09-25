import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/cloud_account.dart';
import 'package:nivaroos_mobile/models/favorite_folder.dart';
import 'package:nivaroos_mobile/models/file_entry.dart';
import 'package:nivaroos_mobile/utils/format.dart';

FileEntry f(String name, {bool dir = false}) => FileEntry(name: name, path: '/x/$name', isDir: dir, size: 0);

void main() {
  group('FileEntry', () {
    test('parses the server listing', () {
      final e = FileEntry.fromJson({
        'name': 'Holiday.jpg',
        'size': 2048,
        'is_dir': false,
        'modified': '2026-09-17T16:40:24.713051072+05:30',
        'path': '/DATA/Holiday.jpg',
      });
      expect(e.name, 'Holiday.jpg');
      expect(e.size, 2048);
      expect(e.modified!.toUtc(), DateTime.utc(2026, 9, 17, 11, 10, 24, 713, 51));
      expect(e.kind, FileKind.image);
      expect(e.hasThumbnail, isTrue);
    });

    test('epoch seconds and a missing name', () {
      final e = FileEntry.fromJson({'path': '/DATA/notes.md', 'modified': 1758800000});
      expect(e.name, 'notes.md');
      expect(e.modified, DateTime.fromMillisecondsSinceEpoch(1758800000 * 1000));
    });

    test('kinds, one per file', () {
      expect(f('a', dir: true).kind, FileKind.folder);
      expect(f('Budget 2026.xlsx').kind, FileKind.spreadsheet);
      expect(f('data.csv').kind, FileKind.spreadsheet);
      expect(f('talk.pptx').kind, FileKind.presentation);
      expect(f('letter.docx').kind, FileKind.document);
      expect(f('x.pdf').kind, FileKind.pdf);
      expect(f('film.mkv').kind, FileKind.video);
      expect(f('song.flac').kind, FileKind.audio);
      expect(f('backup.tar.gz').kind, FileKind.archive);
      expect(f('main.go').kind, FileKind.code);
      expect(f('config.json').kind, FileKind.code);
      expect(f('notes.md').kind, FileKind.text);
      expect(f('README').kind, FileKind.text);
      expect(f('blob.bin').kind, FileKind.other);
      // A folder named like a file is still a folder.
      expect(f('photos.jpg', dir: true).isImage, isFalse);
    });

    test('labels are sentence case', () {
      expect(f('x.pdf').categoryLabel, 'PDF document');
      expect(f('x.zip').categoryLabel, 'ZIP archive');
      expect(f('x.tar.gz').categoryLabel, 'TAR.GZ archive');
      expect(f('x.md').categoryLabel, 'Markdown');
      expect(f('x.bin').categoryLabel, 'BIN file');
      expect(f('x').categoryLabel, 'File');
    });

    test('extension and stem', () {
      expect(f('.bashrc').extension, '');
      expect(f('a.TAR.GZ').extension, 'gz');
      expect(f('report.pdf').stem, 'report');
      expect(f('x.zip').isExtractable, isTrue);
      expect(f('x.iso').isExtractable, isFalse);
    });
  });

  test('CloudAccount names the provider', () {
    final a = CloudAccount.fromJson({'name': '', 'fs': 'gdrive:', 'mount_point': '/DATA/Cloud/G', 'type': 'drive'});
    expect(a.displayName, 'gdrive');
    expect(a.providerTitle, 'Google Drive');
    expect(a.isMounted, isTrue);
    expect(CloudAccount.fromJson({'type': 'something'}).providerTitle, 'something');
    expect(CloudAccount.fromJson({}).providerTitle, 'Cloud drive');
  });

  test('FavoriteFolder icons are outlined', () {
    expect(FavoriteFolder.resolveIcon('', 'Downloads'), Icons.download_outlined);
    expect(FavoriteFolder.resolveIcon('', 'Projects'), Icons.code_outlined);
    expect(FavoriteFolder.resolveIcon('', 'Stuff'), Icons.folder_outlined);
    expect(FavoriteFolder.defaultWebUiFavorites.map((f) => f.path), contains('/DATA/Downloads'));
    final custom = FavoriteFolder.fromCustomJson({'name': 'Projects', 'path': '/DATA/Projects'});
    expect(custom.isCustom, isTrue);
    expect(custom.toJson()['path'], '/DATA/Projects');
  });

  group('format', () {
    test('formatBytes is unchanged for other screens', () {
      expect(formatBytes(0), '0 B');
      expect(formatBytes(512), '512 B');
      expect(formatBytes(1536), '1.5 KB');
      expect(formatBytes(246938120192), '230.0 GB');
    });

    test('formatSize is compact', () {
      expect(formatSize(0), '0 B');
      expect(formatSize(4096), '4 KB');
      expect(formatSize(2516582), '2.4 MB');
      expect(formatSize(183509667840), '171 GB');
      expect(formatSize(1048575), '1 MB');
      expect(formatSpeed(3355443), '3.2 MB/s');
    });

    test('formatCount and formatDuration', () {
      expect(formatCount(1, 'item'), '1 item');
      expect(formatCount(3, 'item'), '3 items');
      expect(formatDuration(const Duration(minutes: 4, seconds: 5)), '4:05');
      expect(formatDuration(const Duration(hours: 1, minutes: 2, seconds: 9)), '1:02:09');
    });
  });
}
