// Getting a phone backup back (device API §8): files of a snapshot into
// the phone (photos back into their album, other files into
// Download/NivaroOS restore), exports for the contacts and calendar apps
// to import, and the merged SMS / call log files for the messages
// restore. Downloads resume with Range after a lost connection, and every
// file is checked against its SHA-256 before it is placed.
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart' as crypto;
import 'package:flutter/services.dart' show PlatformException;

import 'pb_client.dart';
import 'pb_messages.dart';
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

  /// The apps of [snapshot] to put back (see [appsToReinstall]), named from
  /// the backup's app list when it has one.
  Future<List<ApkBackup>> reinstallable(String snapshot, Map<String, int> installed) async {
    final items = <RestoreItem>[];
    var after = '';
    do {
      final page = await client.restoreManifest(snapshot: snapshot, category: 'apks', after: after);
      items.addAll(page.items.where((i) => i.category == 'apks'));
      after = page.nextAfter;
    } while (after.isNotEmpty);
    final labels = <String, String>{};
    try {
      final list = (await client.exports(category: 'apps', snapshot: snapshot)).where((e) => e.category == 'apps');
      if (list.isNotEmpty) {
        final f = await downloadExport(list.first);
        final j = jsonDecode(await f.readAsString());
        f.deleteSync();
        for (final a in (j is Map && j['apps'] is List ? j['apps'] as List : const [])) {
          if (a is Map && a['label'] is String) labels[a['package'].toString()] = a['label'] as String;
        }
      }
    } catch (_) {
      // Names are a nicety: package names do.
    }
    return appsToReinstall(items, installed, labels: labels);
  }

  /// Downloads the APKs of [app] (checked against their SHA-256) and hands
  /// them to Android's package installer, which asks the user to confirm.
  Future<void> install(String snapshot, ApkBackup app, {void Function(int, int)? onProgress}) async {
    final files = <File>[];
    try {
      for (final f in app.files) {
        files.add(await download(fileUri(snapshot, 'apks', f.path), f.path.split('/').last, sha256: f.sha256, onProgress: onProgress));
      }
      await ChannelPhoneSources.installApks([for (final f in files) f.path], app.package);
    } finally {
      for (final f in files) {
        if (f.existsSync()) f.deleteSync();
      }
    }
  }

  /// Every message (or call) up to [snapshot], each once, in one file.
  Future<File> downloadMerged(String category, String snapshot) =>
      download(client.deviceUri('/exports/full', {'category': category, 'snapshot': snapshot}), '$category-all.xml');
}

/// Writes the picture messages of an SMS Backup & Restore file into the
/// phone, skipping the ones already there ([mmsDedupKey] of the phone's
/// own, read with [readExisting]) and repeats within the file. [insert]
/// writes one (ChannelPhoneSources.insertMms; the default SMS app only).
/// {inserted, skipped, failed}.
Future<Map<String, int>> restoreMms(
  File xml, {
  required Future<List<Map<Object?, Object?>>> Function(int offset, int limit) readExisting,
  required Future<void> Function(MmsInsert m) insert,
  void Function(int done)? onProgress,
}) async {
  final have = <String>{};
  const page = 500;
  for (var off = 0;; off += page) {
    final rows = await readExisting(off, page);
    have.addAll(rows.map((r) => mmsDedupKey(mmsFromRow(r))));
    if (rows.length < page) break;
  }
  var inserted = 0, skipped = 0, failed = 0;
  await for (final m in readMmsXml(xml)) {
    if (!have.add(mmsDedupKey(m))) {
      skipped++;
    } else {
      try {
        await insert(mmsInsertOf(m));
        inserted++;
      } on PlatformException catch (e) {
        // No longer the default SMS app, or no access: the rest would fail too.
        if (e.code == 'NOT_DEFAULT_SMS' || e.code == 'PERMISSION') rethrow;
        failed++;
      } catch (_) {
        failed++;
      }
    }
    onProgress?.call(inserted + skipped + failed);
  }
  return {'inserted': inserted, 'skipped': skipped, 'failed': failed};
}

/// One app of the backup that can be put back: its newest backed-up
/// version, with base.apk first and then its splits.
class ApkBackup {
  const ApkBackup({required this.package, required this.versionCode, required this.files, this.installedVersion, this.label = ''});
  final String package;
  final int versionCode;
  final List<RestoreItem> files;

  /// The version on the phone (null: not installed).
  final int? installedVersion;
  final String label;

  bool get split => files.length > 1;
  int get size => files.fold(0, (n, f) => n + f.size);
}

/// The backed-up apps worth installing: not on the phone, or on it in an
/// older version than the newest backup. [apks] are the restore manifest's
/// `apks` items (`<package>/<versionCode>/base.apk`, `split_*.apk`);
/// [installed] maps package to version code; [labels] names them.
/// Versions without a base.apk are ignored. Sorted by name.
List<ApkBackup> appsToReinstall(Iterable<RestoreItem> apks, Map<String, int> installed, {Map<String, String> labels = const {}}) {
  final byVersion = <String, Map<int, List<RestoreItem>>>{};
  for (final i in apks) {
    final p = i.path.split('/');
    final v = p.length == 3 ? int.tryParse(p[1]) : null;
    if (v == null || !p[2].endsWith('.apk')) continue;
    byVersion.putIfAbsent(p[0], () => {}).putIfAbsent(v, () => []).add(i);
  }
  final out = <ApkBackup>[];
  byVersion.forEach((pkg, versions) {
    final usable = versions.entries.where((e) => e.value.any((f) => f.path.endsWith('/base.apk'))).toList()..sort((a, b) => b.key.compareTo(a.key));
    if (usable.isEmpty) return;
    final best = usable.first;
    final have = installed[pkg];
    if (have != null && have >= best.key) return;
    final files = [...best.value]..sort((a, b) => a.path.endsWith('/base.apk') ? -1 : b.path.endsWith('/base.apk') ? 1 : a.path.compareTo(b.path));
    out.add(ApkBackup(package: pkg, versionCode: best.key, files: files, installedVersion: have, label: labels[pkg] ?? ''));
  });
  String name(ApkBackup a) => (a.label.isEmpty ? a.package : a.label).toLowerCase();
  return out..sort((a, b) => name(a).compareTo(name(b)));
}
