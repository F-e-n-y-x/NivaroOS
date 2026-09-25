// Copies and moves between the server and this phone, one at a time, with
// progress the Files screen shows in a strip under its app bar.
//
// - server → server: the server's own transfer engine (`POST /v1/batch/task`,
//   polled through `GET /v1/batch/tasks`), with the conflict style the user
//   picked (plan M-29) instead of a forced overwrite;
// - phone → server: the resumable v2 upload (resumable_upload.dart, M-12);
// - server → phone: a streamed download of every file, checked against the
//   server's size, and for a move the originals are deleted only when every
//   file matched, with `DELETE /v1/batch`; a failed delete is reported
//   (M-11: it used to call a route that doesn't exist and say it moved);
// - phone → phone: plain file-system copies and renames.
import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../../models/file_entry.dart';
import '../../services/api_client.dart';
import 'file_ops.dart';
import 'resumable_upload.dart';

/// What the strip shows while a transfer runs.
@immutable
class TransferProgress {
  const TransferProgress({
    required this.title,
    this.current,
    this.doneBytes = 0,
    this.totalBytes,
    this.doneItems = 0,
    this.totalItems,
  });

  /// "Uploading 3 items to Documents".
  final String title;

  /// The file being worked on.
  final String? current;
  final int doneBytes;

  /// Null while the size is still being worked out.
  final int? totalBytes;
  final int doneItems;
  final int? totalItems;

  /// 0-1, or null when unknown (an indeterminate bar).
  double? get fraction {
    final t = totalBytes;
    if (t == null || t <= 0) return null;
    return (doneBytes / t).clamp(0.0, 1.0);
  }

  TransferProgress copyWith({String? current, int? doneBytes, int? totalBytes, int? doneItems, int? totalItems}) =>
      TransferProgress(
        title: title,
        current: current ?? this.current,
        doneBytes: doneBytes ?? this.doneBytes,
        totalBytes: totalBytes ?? this.totalBytes,
        doneItems: doneItems ?? this.doneItems,
        totalItems: totalItems ?? this.totalItems,
      );
}

/// How a transfer ended, in words for a snackbar.
@immutable
class TransferOutcome {
  const TransferOutcome({required this.message, this.failed = false, this.affectedDirs = const {}});

  final String message;

  /// Something didn't work (the snackbar says what).
  final bool failed;

  /// Folders whose listing changed, to reload.
  final Set<String> affectedDirs;
}

/// One queued piece of work.
abstract class TransferTask {
  /// The strip's title before any progress is known.
  String get title;

  Future<TransferOutcome> run(void Function(TransferProgress) report, TransferCancel cancel);
}

/// Runs [TransferTask]s one after another. [current] drives the progress
/// strip; each task's outcome goes to [onFinished].
class TransferQueue {
  TransferQueue({required this.onFinished});

  final void Function(TransferOutcome outcome) onFinished;
  final ValueNotifier<TransferProgress?> current = ValueNotifier(null);
  final _pending = <TransferTask>[];
  TransferCancel? _cancel;
  bool _running = false;

  int get queued => _pending.length;
  bool get isBusy => _running;

  void add(TransferTask task) {
    _pending.add(task);
    if (!_running) _pump();
  }

  /// Stops the running transfer. What already arrived stays.
  void cancelCurrent() => _cancel?.cancel();

  Future<void> _pump() async {
    _running = true;
    while (_pending.isNotEmpty) {
      final task = _pending.removeAt(0);
      final cancel = _cancel = TransferCancel();
      current.value = TransferProgress(title: task.title);
      TransferOutcome outcome;
      try {
        outcome = await task.run((p) => current.value = p, cancel);
      } on TransferCancelled {
        outcome = const TransferOutcome(message: 'Stopped. What was already copied stays.', failed: true);
      } catch (e) {
        outcome = TransferOutcome(message: _plain(e), failed: true);
      }
      onFinished(outcome);
    }
    _cancel = null;
    current.value = null;
    _running = false;
  }

  void dispose() {
    _cancel?.cancel();
    _pending.clear();
    current.dispose();
  }
}

String _plain(Object e) => e.toString().replaceFirst('Exception: ', '');

/// "3 items" or "“photo.jpg”".
String describeItems(List<String> paths) =>
    paths.length == 1 ? '“${baseName(paths.first)}”' : '${paths.length} items';

// ---------------------------------------------------------------------------
// server → server

/// Copies or moves on the server with its transfer engine, one job per
/// [TransferBatch] (see [planTransfer]).
class ServerTransferTask extends TransferTask {
  ServerTransferTask({
    required this.kind,
    required this.batches,
    required this.destDir,
    this.pollInterval = const Duration(milliseconds: 800),
  });

  final TransferKind kind;
  final List<TransferBatch> batches;
  final String destDir;
  final Duration pollInterval;

  List<String> get _sources => [for (final b in batches) ...b.sources];

  @override
  String get title => '${kind == TransferKind.move ? 'Moving' : 'Copying'} ${describeItems(_sources)} to ${baseName(destDir)}';

  @override
  Future<TransferOutcome> run(void Function(TransferProgress) report, TransferCancel cancel) async {
    final ids = <String>[];
    final detach = cancel.onCancel(() {
      for (final id in ids) {
        ApiClient.instance.delete('/batch/$id/task').ignore();
      }
    });
    try {
      for (final b in batches) {
        final res = await ApiClient.instance.post('/batch/task', body: {
          'type': kind.name,
          'item': [for (final s in b.sources) {'from': s}],
          'to': destDir,
          'style': b.style,
        });
        final id = (res['data'] is Map ? (res['data'] as Map)['id'] : null)?.toString();
        if (id == null || id.isEmpty) throw Exception(res['message']?.toString() ?? "The server didn't start the job.");
        ids.add(id);
      }
      var progress = TransferProgress(title: title, totalItems: _sources.length);
      report(progress);
      while (true) {
        cancel.throwIfCancelled();
        await Future<void>.delayed(pollInterval);
        cancel.throwIfCancelled();
        final res = await ApiClient.instance.get('/batch/tasks');
        final jobs = [
          for (final j in (res['data'] as List? ?? const []))
            if (j is Map && ids.contains(j['id']?.toString())) j,
        ];
        if (jobs.isEmpty) break; // dismissed elsewhere: treat as finished
        final job = ServerJobsSnapshot(jobs);
        progress = TransferProgress(
          title: title,
          current: job.current == null ? null : baseName(job.current!),
          doneBytes: job.bytesDone,
          totalBytes: job.bytesTotal,
          doneItems: job.filesDone,
          totalItems: job.filesTotal,
        );
        report(progress);
        if (job.finished) return job.outcome(kind, destDir, _sources);
      }
      return TransferOutcome(message: _doneMessage(kind, _sources.length, destDir), affectedDirs: _dirs(_sources, destDir, kind));
    } finally {
      detach();
    }
  }
}

/// The server's jobs for one transfer, summed.
class ServerJobsSnapshot {
  ServerJobsSnapshot(this.jobs);
  final List<Map> jobs;

  static int _int(Object? v) => v is num ? v.toInt() : 0;
  static const _terminal = {'done', 'done_with_errors', 'failed', 'cancelled', 'interrupted'};

  bool get finished => jobs.every((j) => _terminal.contains(j['state']));
  int get bytesDone => jobs.fold(0, (s, j) => s + _int(j['bytes_done']));

  /// Null while any job is still counting (the server reports 0 or -1).
  int? get bytesTotal {
    var total = 0;
    for (final j in jobs) {
      final t = _int(j['bytes_total']);
      if (t <= 0 && !_terminal.contains(j['state'])) return null;
      total += t;
    }
    return total;
  }

  int get filesDone => jobs.fold(0, (s, j) => s + _int(j['files_done']));
  int get filesTotal => jobs.fold(0, (s, j) => s + _int(j['files_total']));
  int get filesFailed => jobs.fold(0, (s, j) => s + _int(j['files_failed']));
  int get filesSkipped => jobs.fold(0, (s, j) => s + _int(j['files_skipped']));
  String? get current => jobs.map((j) => j['current']?.toString()).whereType<String>().where((c) => c.isNotEmpty).firstOrNull;

  TransferOutcome outcome(TransferKind kind, String destDir, List<String> sources) {
    final dirs = <String>{
      ..._dirs(sources, destDir, kind),
      for (final j in jobs)
        for (final d in (j['affected_dirs'] as List? ?? const [])) d.toString(),
    };
    final states = jobs.map((j) => j['state']).toSet();
    final error = jobs.map((j) => j['error']?.toString()).whereType<String>().where((e) => e.isNotEmpty).firstOrNull;
    if (states.contains('cancelled')) {
      return TransferOutcome(message: 'Stopped. What was already copied stays.', failed: true, affectedDirs: dirs);
    }
    if (states.contains('failed') || states.contains('interrupted')) {
      return TransferOutcome(
        message: error ?? "Couldn't ${kind.name} ${describeItems(sources)}.",
        failed: true,
        affectedDirs: dirs,
      );
    }
    if (filesFailed > 0) {
      final failure = jobs
          .expand((j) => (j['failures'] as List? ?? const []))
          .whereType<Map>()
          .map((f) => f['error']?.toString())
          .whereType<String>()
          .firstOrNull;
      return TransferOutcome(
        message: '${filesFailed == 1 ? '1 file' : '$filesFailed files'} couldn’t be ${kind == TransferKind.move ? 'moved' : 'copied'}${failure == null ? '' : ': $failure'}',
        failed: true,
        affectedDirs: dirs,
      );
    }
    final skipped = filesSkipped > 0 ? ' · ${filesSkipped == 1 ? '1 skipped' : '$filesSkipped skipped'}' : '';
    return TransferOutcome(message: '${_doneMessage(kind, sources.length, destDir)}$skipped', affectedDirs: dirs);
  }
}

String _doneMessage(TransferKind kind, int count, String destDir) =>
    '${kind == TransferKind.move ? 'Moved' : 'Copied'} ${count == 1 ? '1 item' : '$count items'} to ${baseName(destDir)}';

Set<String> _dirs(List<String> sources, String destDir, TransferKind kind) =>
    {destDir, if (kind == TransferKind.move) ...sources.map(parentOf)};

// ---------------------------------------------------------------------------
// phone → server

/// One thing to upload: a local file or folder and the name it gets on the
/// server (already resolved against conflicts).
class UploadItem {
  const UploadItem(this.localPath, this.targetName);
  final String localPath;
  final String targetName;
}

/// Uploads local files and folders into [destDir] with the resumable v2
/// protocol. For a move, each item is deleted from the phone once all of
/// its files are on the server.
class UploadTask extends TransferTask {
  UploadTask({required this.items, required this.destDir, this.move = false, ResumableUploader? uploader})
      : uploader = uploader ?? ResumableUploader();

  final List<UploadItem> items;
  final String destDir;
  final bool move;
  final ResumableUploader uploader;

  @override
  String get title => '${move ? 'Moving' : 'Uploading'} ${describeItems(items.map((i) => i.localPath).toList())} to ${baseName(destDir)}';

  @override
  Future<TransferOutcome> run(void Function(TransferProgress) report, TransferCancel cancel) async {
    // Every file with the path it gets under destDir.
    final plan = <(UploadItem, File, String, int)>[];
    // Folders with links inside: links aren't uploaded, so a move leaves
    // the folder on the phone instead of losing them.
    final withLinks = <String>{};
    for (final item in items) {
      final type = FileSystemEntity.typeSync(item.localPath, followLinks: false);
      if (type == FileSystemEntityType.directory) {
        final root = Directory(item.localPath);
        String rel(FileSystemEntity e) =>
            e.path.substring(root.path.length).replaceAll(r'\', '/').replaceFirst(RegExp('^/+'), '');
        final all = root.listSync(recursive: true, followLinks: false);
        final files = all.whereType<File>().toList()..sort((a, b) => a.path.compareTo(b.path));
        if (all.whereType<Link>().isNotEmpty) withLinks.add(item.localPath);
        for (final f in files) {
          plan.add((item, f, '${item.targetName}/${rel(f)}', f.lengthSync()));
        }
        // Folders with nothing in them have nothing to upload; create them
        // instead, so a move doesn't lose them.
        final empty = [
          if (all.isEmpty) '',
          for (final d in all.whereType<Directory>())
            if (d.listSync(followLinks: false).isEmpty) rel(d),
        ];
        for (final r in empty) {
          await ApiClient.instance.post('/folder', body: {'path': joinPath(destDir, r.isEmpty ? item.targetName : '${item.targetName}/$r')});
        }
      } else if (type == FileSystemEntityType.file) {
        final f = File(item.localPath);
        plan.add((item, f, item.targetName, f.lengthSync()));
      }
    }
    final total = plan.fold<int>(0, (s, p) => s + p.$4);
    var done = 0;
    var progress = TransferProgress(title: title, totalBytes: total, totalItems: plan.length);
    report(progress);
    for (var i = 0; i < plan.length; i++) {
      final (_, file, rel, size) = plan[i];
      final before = done;
      report(progress = progress.copyWith(current: baseName(rel), doneItems: i));
      await uploader.upload(
        file: file,
        destDir: destDir,
        relativePath: rel,
        cancel: cancel,
        onProgress: (sent, _) => report(progress = progress.copyWith(doneBytes: before + sent)),
      );
      done = before + size;
    }
    report(progress.copyWith(doneBytes: total, doneItems: plan.length));
    var deleteFailed = 0;
    if (move) {
      for (final item in items) {
        if (withLinks.contains(item.localPath)) {
          deleteFailed++;
          continue;
        }
        try {
          await _deleteLocal(item.localPath);
        } catch (_) {
          deleteFailed++;
        }
      }
    }
    final what = items.length == 1 ? '1 item' : '${items.length} items';
    if (deleteFailed > 0) {
      return TransferOutcome(
        message: 'Uploaded $what to ${baseName(destDir)}, but couldn’t remove ${deleteFailed == 1 ? 'it' : '$deleteFailed of them'} from this phone',
        failed: true,
        affectedDirs: {destDir, ...items.map((i) => parentOf(i.localPath))},
      );
    }
    return TransferOutcome(
      message: '${move ? 'Moved' : 'Uploaded'} $what to ${baseName(destDir)}',
      affectedDirs: {destDir, if (move) ...items.map((i) => parentOf(i.localPath))},
    );
  }
}

Future<void> _deleteLocal(String path) async {
  final type = FileSystemEntity.typeSync(path, followLinks: false);
  if (type == FileSystemEntityType.directory) {
    await Directory(path).delete(recursive: true);
  } else if (type != FileSystemEntityType.notFound) {
    await File(path).delete();
  }
}

// ---------------------------------------------------------------------------
// server → phone

/// One thing to download: a server entry and the name it gets on the phone.
class DownloadItem {
  const DownloadItem(this.entry, this.targetName, {this.replace = false});
  final FileEntry entry;
  final String targetName;

  /// The user chose to replace what's there.
  final bool replace;
}

/// Downloads server files and folders into a phone folder. Each file lands
/// under a temporary name and is renamed only after its size matches the
/// server's. For a move the originals are deleted from the server only when
/// every file matched, and a failed delete is reported (plan M-11).
class DownloadTask extends TransferTask {
  DownloadTask({required this.items, required this.destDir, this.move = false, this.listFolder, this.download, this.deleteRemote});

  final List<DownloadItem> items;
  final String destDir;
  final bool move;

  /// Seams for tests; default to the server.
  final Future<RemoteListing> Function(String path)? listFolder;
  final Future<void> Function(String remotePath, File target, void Function(int received) onProgress)? download;
  final Future<void> Function(List<String> paths)? deleteRemote;

  @override
  String get title => '${move ? 'Moving' : 'Downloading'} ${describeItems(items.map((i) => i.entry.path).toList())} to this phone';

  Future<RemoteListing> _list(String path) async {
    if (listFolder != null) return listFolder!(path);
    final res = await ApiClient.instance.get('/folder', query: {'path': path});
    return RemoteListing.fromResponse(res['data']);
  }

  Future<void> _download(String remote, File target, void Function(int) onProgress, TransferCancel cancel) async {
    if (download != null) return download!(remote, target, onProgress);
    // Our own client, so Cancel can close it mid-file.
    final client = http.Client();
    final detach = cancel.onCancel(client.close);
    IOSink? sink;
    try {
      final uri = ApiClient.instance.buildUri('/file', {'path': remote});
      final res = await ApiClient.instance.send(() => http.Request('GET', uri), client: client);
      if (res.statusCode != 200) {
        await res.stream.drain<void>();
        throw ApiException("Couldn't download “${baseName(remote)}” (HTTP ${res.statusCode})", statusCode: res.statusCode);
      }
      sink = target.openWrite();
      var received = 0;
      await for (final chunk in res.stream) {
        sink.add(chunk);
        received += chunk.length;
        onProgress(received);
      }
      await sink.flush();
    } catch (_) {
      cancel.throwIfCancelled();
      rethrow;
    } finally {
      await sink?.close();
      detach();
      client.close();
    }
  }

  Future<void> _delete(List<String> paths) async {
    if (deleteRemote != null) return deleteRemote!(paths);
    await ApiClient.instance.deleteWithBody('/batch', [for (final p in paths) {'path': p}]);
  }

  @override
  Future<TransferOutcome> run(void Function(TransferProgress) report, TransferCancel cancel) async {
    var progress = TransferProgress(title: title);
    report(progress);
    // Every remote file with its local target.
    final files = <(FileEntry, String)>[];
    final folders = <String>[];
    // Remote folders that hold entries the listing leaves out (the
    // server hides `.temp` folders and in-flight copies), or that lie
    // above one: a move must not delete them wholesale, because those
    // entries were never downloaded.
    final keep = <String>{};
    // Each remote folder's direct children, for the move's delete plan.
    final children = <String, List<FileEntry>>{};
    Future<bool> walk(FileEntry dir, String localDir) async {
      cancel.throwIfCancelled();
      folders.add(localDir);
      final listing = await _list(dir.path);
      children[dir.path] = listing.entries;
      var hidden = listing.hidden > 0;
      for (final child in listing.entries) {
        final target = joinPath(localDir, child.name);
        if (child.isDir) {
          if (await walk(child, target)) hidden = true;
        } else {
          files.add((child, target));
        }
      }
      if (hidden) keep.add(dir.path);
      return hidden;
    }

    for (final item in items) {
      final target = joinPath(destDir, item.targetName);
      if (item.entry.isDir) {
        await walk(item.entry, target);
      } else {
        files.add((item.entry, target));
      }
    }
    final total = files.fold<int>(0, (s, f) => s + f.$1.size);
    report(progress = progress.copyWith(totalBytes: total, totalItems: files.length));

    for (final dir in folders) {
      await Directory(dir).create(recursive: true);
    }
    var done = 0;
    final mismatched = <String>[];
    final stamp = DateTime.now().microsecondsSinceEpoch.toRadixString(36);
    for (var i = 0; i < files.length; i++) {
      cancel.throwIfCancelled();
      final (entry, target) = files[i];
      final part = File(partPath(target, '$stamp$i'));
      final before = done;
      report(progress = progress.copyWith(current: entry.name, doneItems: i));
      try {
        await _download(entry.path, part, (received) => report(progress = progress.copyWith(doneBytes: before + received)), cancel);
        cancel.throwIfCancelled();
        final got = await part.length();
        if (got != entry.size) {
          mismatched.add(entry.name);
          await part.delete();
        } else {
          final dest = File(target);
          if (await dest.exists()) await dest.delete();
          await part.rename(target);
        }
      } catch (e) {
        if (await part.exists()) await part.delete();
        rethrow;
      }
      done = before + entry.size;
    }
    report(progress.copyWith(doneBytes: total, doneItems: files.length));

    final what = items.length == 1 ? '1 item' : '${items.length} items';
    if (mismatched.isNotEmpty) {
      return TransferOutcome(
        message: '${mismatched.length == 1 ? '“${mismatched.first}” didn’t' : '${mismatched.length} files didn’t'} arrive complete${move ? ', so nothing was removed from the server' : ''}. Try again.',
        failed: true,
        affectedDirs: {destDir},
      );
    }
    if (move) {
      // Delete what arrived: whole folders when nothing in them was
      // hidden from the listing, otherwise the downloaded files inside and
      // the clean subfolders, leaving the rest where it is.
      final paths = <String>[];
      void plan(String dir) {
        for (final child in children[dir] ?? const <FileEntry>[]) {
          if (!child.isDir || !keep.contains(child.path)) {
            paths.add(child.path);
          } else {
            plan(child.path);
          }
        }
      }

      for (final i in items) {
        if (i.entry.isDir && keep.contains(i.entry.path)) {
          plan(i.entry.path);
        } else {
          paths.add(i.entry.path);
        }
      }
      try {
        if (paths.isNotEmpty) await _delete(paths);
      } catch (e) {
        return TransferOutcome(
          message: 'Copied $what to this phone, but couldn’t remove the originals from the server: ${_plain(e)}',
          failed: true,
          affectedDirs: {destDir},
        );
      }
      final dirs = {destDir, ...items.map((i) => parentOf(i.entry.path)), ...keep};
      if (keep.isNotEmpty) {
        final kept = items.where((i) => keep.contains(i.entry.path)).map((i) => '“${i.entry.name}”').toList();
        final where = kept.length == 1 ? kept.first : 'some folders';
        return TransferOutcome(
          message: 'Moved $what to this phone. $where stays on the server because it holds hidden files that weren’t copied.',
          affectedDirs: dirs,
        );
      }
      return TransferOutcome(message: 'Moved $what to this phone', affectedDirs: dirs);
    }
    return TransferOutcome(message: 'Downloaded $what to this phone', affectedDirs: {destDir});
  }

  /// The temporary file [target] is downloaded to before its size is
  /// checked: hidden, next to it, and short, so a name of up to 255 bytes
  /// still fits (the real name is used only for the final rename).
  @visibleForTesting
  static String partPath(String target, String tag) => joinPath(parentOf(target), '.nvpart-$tag');
}

/// One server folder listing: what it shows, and how many entries it
/// leaves out (`.temp` folders and copies in flight, which `GET /v1/folder`
/// hides but still counts in `total`).
@immutable
class RemoteListing {
  const RemoteListing(this.entries, {this.hidden = 0});

  factory RemoteListing.fromResponse(Object? data) {
    final content = data is Map ? (data['content'] as List? ?? const []) : const [];
    final entries = [for (final e in content) if (e is Map<String, dynamic>) FileEntry.fromJson(e)];
    final total = data is Map && data['total'] is num ? (data['total'] as num).toInt() : entries.length;
    return RemoteListing(entries, hidden: total > entries.length ? total - entries.length : 0);
  }

  final List<FileEntry> entries;
  final int hidden;
}

// ---------------------------------------------------------------------------
// phone → phone

/// Copies or moves files between folders on this phone.
class LocalTransferTask extends TransferTask {
  LocalTransferTask({required this.items, required this.destDir, this.move = false});

  /// Local paths with the names they get in [destDir].
  final List<UploadItem> items;
  final String destDir;
  final bool move;

  @override
  String get title => '${move ? 'Moving' : 'Copying'} ${describeItems(items.map((i) => i.localPath).toList())} to ${baseName(destDir)}';

  @override
  Future<TransferOutcome> run(void Function(TransferProgress) report, TransferCancel cancel) async {
    var progress = TransferProgress(title: title, totalItems: items.length);
    for (var i = 0; i < items.length; i++) {
      cancel.throwIfCancelled();
      final item = items[i];
      report(progress = progress.copyWith(current: baseName(item.localPath), doneItems: i));
      final target = joinPath(destDir, item.targetName);
      if (target == item.localPath) continue;
      final isDir = FileSystemEntity.isDirectorySync(item.localPath);
      if (FileSystemEntity.typeSync(target, followLinks: false) != FileSystemEntityType.notFound) {
        await _deleteLocal(target); // the user chose Replace
      }
      if (move) {
        try {
          isDir ? await Directory(item.localPath).rename(target) : await File(item.localPath).rename(target);
          continue;
        } on FileSystemException {
          // Across storage volumes: copy, then delete.
        }
      }
      isDir ? await _copyDir(Directory(item.localPath), Directory(target), cancel) : await File(item.localPath).copy(target);
      if (move) await _deleteLocal(item.localPath);
    }
    final what = items.length == 1 ? '1 item' : '${items.length} items';
    return TransferOutcome(
      message: '${move ? 'Moved' : 'Copied'} $what to ${baseName(destDir)}',
      affectedDirs: {destDir, if (move) ...items.map((i) => parentOf(i.localPath))},
    );
  }

  static Future<void> _copyDir(Directory src, Directory dest, TransferCancel cancel) async {
    await dest.create(recursive: true);
    await for (final e in src.list(followLinks: false)) {
      cancel.throwIfCancelled();
      final target = joinPath(dest.path, baseName(e.path));
      if (e is Directory) {
        await _copyDir(e, Directory(target), cancel);
      } else if (e is File) {
        await e.copy(target);
      }
    }
  }
}

// ---------------------------------------------------------------------------
// one long server call

/// A single server request that can take a while (compress, extract),
/// shown in the strip with an indeterminate bar so the screen stays usable.
class ServerCallTask extends TransferTask {
  ServerCallTask({required this.title, required this.call});

  @override
  final String title;

  /// Does the work and says how it went.
  final Future<TransferOutcome> Function() call;

  @override
  Future<TransferOutcome> run(void Function(TransferProgress) report, TransferCancel cancel) async {
    report(TransferProgress(title: title));
    final outcome = await call();
    cancel.throwIfCancelled();
    return outcome;
  }
}
