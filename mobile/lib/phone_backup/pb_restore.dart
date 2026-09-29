// Getting a phone backup back (device API §8): files of a snapshot into
// the phone (photos back into their album, other files into
// Download/NivaroOS restore), exports for the contacts and calendar apps
// to import, and the merged SMS / call log files for the messages
// restore. Downloads resume with Range after a lost connection, and every
// file is checked against its SHA-256 before it is placed.
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart' as crypto;

import 'pb_client.dart';
import 'pb_models.dart';
import 'pb_platform.dart';
import 'pb_store.dart';

/// Where a restored media file goes: its album folder and name. A path
/// from a second volume ("1234-5678/DCIM/x.jpg") goes to the same album
/// on the phone's own storage.
({String relativePath, String name}) mediaTarget(String path) {
  final parts = path.split('/').where((p) => p.isNotEmpty).toList();
  final name = parts.isEmpty ? 'file' : parts.removeLast();
  if (parts.isNotEmpty && RegExp(r'^[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}$|^external_').hasMatch(parts.first)) parts.removeAt(0);
  return (relativePath: parts.isEmpty ? 'Pictures/Restored' : parts.join('/'), name: name);
}

/// Where a restored folder or APK file goes, under Download/NivaroOS restore.
({String subPath, String name}) downloadTarget(String category, String path) {
  final parts = path.split('/').where((p) => p.isNotEmpty).toList();
  final name = parts.isEmpty ? 'file' : parts.removeLast();
  return (subPath: [category == 'apks' ? 'Apps' : 'Folders', ...parts].join('/'), name: name);
}

class RestoreSummary {
  RestoreSummary();
  int restored = 0;
  int skipped = 0;
  int failed = 0;
  int bytes = 0;
  final List<String> errors = [];
}

class PhoneRestore {
  PhoneRestore(this.client, this.store);

  final DeviceClient client;
  final PhoneBackupStore store;

  File _tmpFor(String key, String name) {
    final id = crypto.sha256.convert(utf8.encode(key)).toString().substring(0, 16);
    final safe = name.replaceAll(RegExp(r'[^A-Za-z0-9._-]'), '_');
    return File('${store.scratch('restore').path}/$id-$safe');
  }

  static Future<String> _sha(File f) async => (await crypto.sha256.bind(f.openRead()).first).toString();

  /// Downloads [uri] (resuming a partial file) and checks [sha256].
  Future<File> download(Uri uri, String name, {String sha256 = '', void Function(int, int)? onProgress}) async {
    final tmp = _tmpFor(uri.toString(), name);
    for (var attempt = 0;; attempt++) {
      try {
        final served = await client.download(uri, tmp, onProgress: onProgress);
        final want = sha256.isNotEmpty ? sha256 : served;
        if (want.isNotEmpty && await _sha(tmp) != want) {
          tmp.deleteSync();
          if (attempt < 1) continue;
          throw PhoneBackupError('checksum_mismatch', message: 'The downloaded file doesn’t match the backup');
        }
        return tmp;
      } on PhoneBackupError catch (e) {
        if (e.unreachable && attempt < 4) {
          await Future<void>.delayed(Duration(seconds: 2 * (attempt + 1)));
          continue;
        }
        rethrow;
      }
    }
  }

  Uri fileUri(String snapshot, String category, String path) => client.deviceUri('/files/content', {'category': category, 'path': path, 'snapshot': snapshot});

  /// One file of a snapshot back onto the phone.
  Future<void> restoreFile(String snapshot, String category, String path, {String sha256 = '', int takenAt = 0, int mtime = 0, void Function(int, int)? onProgress}) async {
    final name = path.split('/').last;
    final tmp = await download(fileUri(snapshot, category, path), name, sha256: sha256, onProgress: onProgress);
    try {
      if (category == 'media') {
        final t = mediaTarget(path);
        await ChannelPhoneSources.saveToMedia(tmpPath: tmp.path, relativePath: t.relativePath, name: t.name, takenAt: takenAt, mtime: mtime);
      } else {
        final t = downloadTarget(category, path);
        await ChannelPhoneSources.saveToDownloads(tmpPath: tmp.path, subPath: t.subPath, name: t.name);
      }
    } finally {
      if (tmp.existsSync()) tmp.deleteSync();
    }
  }

  /// Everything of [category] in [snapshot] that the phone doesn't have
  /// (photos: by album, name and size; other files are always saved).
  /// [includeDeleted] also brings back files deleted on the phone.
  Future<RestoreSummary> restoreMissing(String snapshot, String category,
      {bool includeDeleted = true, void Function(int done, int total, RestoreSummary s)? onProgress, bool Function()? shouldStop}) async {
    final sum = RestoreSummary();
    final items = <RestoreItem>[];
    var after = '';
    do {
      final page = await client.restoreManifest(snapshot: snapshot, category: category, after: after);
      items.addAll(page.items.where((i) => i.category == category && (includeDeleted || i.deletedOnDeviceAt == null)));
      after = page.nextAfter;
    } while (after.isNotEmpty);
    var done = 0;
    for (final i in items) {
      if (shouldStop?.call() ?? false) break;
      try {
        if (category == 'media') {
          final t = mediaTarget(i.path);
          if (await ChannelPhoneSources.mediaExists(relativePath: t.relativePath, name: t.name, size: i.size)) {
            sum.skipped++;
            done++;
            onProgress?.call(done, items.length, sum);
            continue;
          }
        }
        await restoreFile(snapshot, category, i.path, sha256: i.sha256, takenAt: i.takenAt, mtime: i.mtime);
        sum.restored++;
        sum.bytes += i.size;
      } catch (e) {
        sum.failed++;
        if (sum.errors.length < 50) sum.errors.add('${i.path}: $e');
      }
      done++;
      onProgress?.call(done, items.length, sum);
    }
    return sum;
  }

  /// One export file (contacts .vcf, a calendar's .ics).
  Future<File> downloadExport(PhoneExport e) {
    final ext = switch (e.category) {
      'contacts' => '.vcf',
      'calendar' => '.ics',
      'apps' || 'settings' => '.json',
      _ => '.xml',
    };
    return download(client.deviceUri('/exports/${Uri.encodeComponent(e.id)}/content'), '${e.category}${e.name.isEmpty ? '' : '-${e.name}'}$ext', sha256: e.sha256);
  }

  /// Every message (or call) up to [snapshot], each once, in one file.
  Future<File> downloadMerged(String category, String snapshot) =>
      download(client.deviceUri('/exports/full', {'category': category, 'snapshot': snapshot}), '$category-all.xml');
}
