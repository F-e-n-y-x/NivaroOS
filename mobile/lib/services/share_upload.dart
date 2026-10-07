// "Upload to NivaroOS": files another app shared (Android's Share sheet)
// uploaded to a folder on the server.
//
// The sheet (screens/share_upload_sheet.dart) saves a [ShareBatch] and
// asks Android to run it as a job (ShareUploadJobService.kt, a
// user-initiated data transfer job on 14+, like "Back up now"). The job
// starts a headless engine that runs [runShareUploadJob]: it reads each
// file through Android (the job holds the read grant) and sends it with
// the resumable upload Files uses (`/v2/casaos/file/upload`). The batch
// is saved after every file, so a run stopped by a lost network picks up
// where it was, and the server keeps the chunks it already has.
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';

import '../screens/files/file_ops.dart';
import '../screens/files/resumable_upload.dart';
import '../utils/format.dart';
import 'api_client.dart';

/// One file Android handed over: its content:// URI, name, size and type.
@immutable
class SharedFile {
  const SharedFile({required this.uri, required this.name, required this.size, this.mime = ''});

  final String uri;
  final String name;
  final int size;
  final String mime;

  bool get isImage => mime.startsWith('image/');
  bool get isVideo => mime.startsWith('video/');

  static SharedFile? fromMap(Object? m) {
    if (m is! Map) return null;
    final uri = m['uri']?.toString() ?? '';
    final size = m['size'];
    if (uri.isEmpty || size is! num || size < 0) return null;
    final name = m['name']?.toString().trim() ?? '';
    return SharedFile(uri: uri, name: name.isEmpty ? 'file' : name, size: size.toInt(), mime: m['mime']?.toString() ?? '');
  }

  Map<String, Object> toJson() => {'uri': uri, 'name': name, 'size': size, 'mime': mime};
}

/// What a share brought: the files that can be read, and how many could
/// not (no access, or the app's own files).
@immutable
class SharedFiles {
  const SharedFiles(this.files, {this.rejected = 0});
  final List<SharedFile> files;
  final int rejected;

  int get totalBytes => files.fold(0, (s, f) => s + f.size);

  /// MainActivity's `{files: [{uri, name, size, mime}], rejected}`.
  static SharedFiles? fromPayload(Object? v) {
    if (v is! Map || v['files'] is! List) return null;
    final files = [for (final f in v['files'] as List) ?SharedFile.fromMap(f)];
    final rejected = (v['rejected'] is num ? (v['rejected'] as num).toInt() : 0) + (v['files'] as List).length - files.length;
    return SharedFiles(files, rejected: rejected);
  }
}

enum ShareItemState { pending, done, skipped, failed, cancelled }

/// A file in a batch: the name it gets on the server once decided (kept,
/// so a resumed run uploads to the same name), and how it went.
class ShareItem {
  ShareItem(this.file, {this.target, this.state = ShareItemState.pending, this.error});

  final SharedFile file;
  String? target;
  ShareItemState state;
  String? error;

  Map<String, Object?> toJson() => {'file': file.toJson(), 'target': target, 'state': state.name, 'error': error};

  static ShareItem? fromJson(Object? j) {
    if (j is! Map) return null;
    final f = SharedFile.fromMap(j['file']);
    if (f == null) return null;
    return ShareItem(
      f,
      target: j['target']?.toString(),
      state: ShareItemState.values.asNameMap()[j['state']] ?? ShareItemState.pending,
      error: j['error']?.toString(),
    );
  }
}

/// One share's upload: where to, what to do with names already there,
/// and every file's state.
class ShareBatch {
  ShareBatch({
    required this.id,
    required this.server,
    required this.serverName,
    required this.destDir,
    required this.items,
    this.conflict = ConflictChoice.keepBoth,
    this.waits = 0,
    DateTime? created,
  }) : created = created ?? clock.now();

  final String id;

  /// The server's URL, and the name it's shown by.
  final String server;
  final String serverName;
  final String destDir;
  final ConflictChoice conflict;
  final List<ShareItem> items;
  final DateTime created;

  /// Runs that stopped to wait for the server; the batch gives up after
  /// [maxWaits].
  int waits;
  static const maxWaits = 30;

  static String newId() => '${clock.now().millisecondsSinceEpoch}-${math.Random().nextInt(1 << 32).toRadixString(36)}';

  int count(ShareItemState s) => items.where((i) => i.state == s).length;
  bool get hasPending => items.any((i) => i.state == ShareItemState.pending);
  int get totalBytes => items.where((i) => i.state != ShareItemState.skipped).fold(0, (s, i) => s + i.file.size);

  /// Failed and cancelled files go back in the queue.
  void retry() {
    for (final i in items) {
      if (i.state == ShareItemState.failed || i.state == ShareItemState.cancelled) {
        i.state = ShareItemState.pending;
        i.error = null;
      }
    }
    waits = 0;
  }

  Map<String, Object?> toJson() => {
        'id': id,
        'server': server,
        'server_name': serverName,
        'dest': destDir,
        'conflict': conflict.name,
        'waits': waits,
        'created': created.toIso8601String(),
        'items': [for (final i in items) i.toJson()],
      };

  static ShareBatch? fromJson(Object? j) {
    if (j is! Map || j['id'] is! String || j['dest'] is! String) return null;
    return ShareBatch(
      id: j['id'] as String,
      server: j['server']?.toString() ?? '',
      serverName: j['server_name']?.toString() ?? '',
      destDir: j['dest'] as String,
      conflict: ConflictChoice.values.asNameMap()[j['conflict']] ?? ConflictChoice.keepBoth,
      waits: j['waits'] is num ? (j['waits'] as num).toInt() : 0,
      created: DateTime.tryParse(j['created']?.toString() ?? ''),
      items: [for (final i in j['items'] is List ? j['items'] as List : const []) ?ShareItem.fromJson(i)],
    );
  }
}

/// Batches as JSON files in the app's private support folder, written
/// atomically; the app and the job's engine read them from disk each
/// time. Batches older than a week are dropped.
class ShareUploadStore {
  ShareUploadStore(this.dir);
  final Directory dir;

  static Future<ShareUploadStore> open() async => ShareUploadStore(Directory('${(await getApplicationSupportDirectory()).path}/share_uploads'));

  File _file(String id) => File('${dir.path}/${id.replaceAll(RegExp(r'[^A-Za-z0-9-]'), '')}.json');

  ShareBatch? load(String id) {
    try {
      return ShareBatch.fromJson(jsonDecode(_file(id).readAsStringSync()));
    } catch (_) {
      return null;
    }
  }

  void save(ShareBatch b) {
    dir.createSync(recursive: true);
    final f = _file(b.id);
    final tmp = File('${f.path}.tmp')..writeAsStringSync(jsonEncode(b.toJson()), flush: true);
    tmp.renameSync(f.path);
    _purge();
  }

  void _purge() {
    final old = clock.now().subtract(const Duration(days: 7));
    for (final f in dir.listSync().whereType<File>()) {
      try {
        if (f.statSync().modified.isBefore(old)) f.deleteSync();
      } catch (_) {}
    }
  }
}

/// A shared file's name as a name on the server: no folders in it.
String serverSafeName(String name) {
  final n = name.replaceAll(RegExp(r'[/\\\x00]'), '_').trim();
  return n.isEmpty || n == '.' || n == '..' ? 'file' : n;
}

/// Where a run is, for the notification.
@immutable
class ShareProgress {
  const ShareProgress({required this.doneBytes, required this.totalBytes, required this.index, required this.count, required this.current});
  final int doneBytes;
  final int totalBytes;
  final int index;
  final int count;
  final String current;

  int get permille => totalBytes <= 0 ? 0 : (doneBytes * 1000 ~/ totalBytes).clamp(0, 1000);
}

enum ShareRunEnd { finished, waitNetwork, cancelled }

/// Uploads a batch's pending files, saving it after every file.
class ShareUploader {
  ShareUploader({required this.read, required this.listNames, required this.save, ResumableUploader? uploader, this.onProgress})
      : uploader = uploader ?? ResumableUploader();

  /// Reads [length] bytes of a shared file from [offset].
  final Future<List<int>> Function(String uri, int offset, int length) read;

  /// The names in a server folder; an empty set when it doesn't exist.
  final Future<Set<String>> Function(String dir) listNames;
  final void Function(ShareBatch b) save;
  final ResumableUploader uploader;
  final void Function(ShareProgress p)? onProgress;

  Future<ShareRunEnd> run(ShareBatch b, TransferCancel cancel) async {
    if (!await _name(b)) return ShareRunEnd.waitNetwork;
    final total = b.totalBytes;
    var done = b.items.where((i) => i.state == ShareItemState.done).fold(0, (s, i) => s + i.file.size);
    final pending = [for (final i in b.items) if (i.state == ShareItemState.pending) i];
    final count = pending.length;
    for (var n = 0; n < count; n++) {
      final item = pending[n];
      final before = done;
      try {
        cancel.throwIfCancelled();
        onProgress?.call(ShareProgress(doneBytes: before, totalBytes: total, index: n, count: count, current: item.target!));
        await uploader.uploadFrom(
          size: item.file.size,
          read: (offset, length) => read(item.file.uri, offset, length),
          destDir: b.destDir,
          relativePath: item.target!,
          cancel: cancel,
          onProgress: (sent, _) => onProgress?.call(ShareProgress(doneBytes: before + sent, totalBytes: total, index: n, count: count, current: item.target!)),
        );
        item.state = ShareItemState.done;
        item.error = null;
      } on TransferCancelled {
        for (final i in pending.skip(n)) {
          i.state = ShareItemState.cancelled;
        }
        save(b);
        return ShareRunEnd.cancelled;
      } on UploadException catch (e) {
        if (_waitable(e)) {
          save(b);
          return ShareRunEnd.waitNetwork;
        }
        item.state = ShareItemState.failed;
        item.error = shareErrorText(e);
      } catch (e) {
        item.state = ShareItemState.failed;
        item.error = shareErrorText(e);
      }
      done = before + item.file.size;
      save(b);
    }
    return ShareRunEnd.finished;
  }

  /// Gives every pending file its name on the server, once: what's
  /// already in the folder follows the batch's conflict choice; two
  /// shared files with one name never overwrite each other. False when
  /// the server can't be reached to look.
  Future<bool> _name(ShareBatch b) async {
    if (!b.items.any((i) => i.state == ShareItemState.pending && i.target == null)) return true;
    Set<String> there;
    try {
      there = await listNames(b.destDir);
    } on ApiException catch (e) {
      if (e.isUnreachable) return false;
      there = {};
    }
    final named = {for (final i in b.items) if (i.target != null && i.state != ShareItemState.skipped) i.target!};
    for (final i in b.items) {
      if (i.state != ShareItemState.pending || i.target != null) continue;
      final name = serverSafeName(i.file.name);
      final taken = {...there, ...named};
      String? target;
      if (!taken.contains(name)) {
        target = name;
      } else if (named.contains(name) || b.conflict == ConflictChoice.keepBoth) {
        target = uniqueName(name, taken);
      } else if (b.conflict == ConflictChoice.replace) {
        target = name;
      }
      if (target == null) {
        i.state = ShareItemState.skipped;
        continue;
      }
      i.target = target;
      named.add(target);
    }
    save(b);
    return true;
  }

  static bool _waitable(UploadException e) => e.unreachable || const {502, 503, 504}.contains(e.statusCode);
}

/// Why a file didn't upload, in words for its row.
String shareErrorText(Object e) {
  if (e is PlatformException) {
    return e.code == 'PERMISSION' ? "NivaroOS can't read this file any more. Share it again." : "Couldn't read this file on the phone.";
  }
  final m = e.toString();
  final lower = m.toLowerCase();
  if (lower.contains('no space left') || lower.contains('disk quota')) return 'Not enough space on the server.';
  if (lower.contains('permission denied') || lower.contains('read-only file system')) return "The server can't write to this folder.";
  if (e is UploadException && e.unreachable) return "Couldn't reach the server.";
  return m;
}

String _files(int n) => formatCount(n, 'file');

/// The progress notification: "Uploading 3 files to atom · 45%".
({String title, String text}) shareProgressNotice(ShareBatch b, ShareProgress p) => (
      title: 'Uploading ${_files(b.items.where((i) => i.state != ShareItemState.skipped).length)} to ${b.serverName} · ${p.permille ~/ 10}%',
      text: p.count > 1 ? '${p.current} · ${p.index + 1} of ${p.count}' : p.current,
    );

/// The notification a run ends with; [folder] makes a tap open it in
/// Files (otherwise a tap shows the batch, with Retry).
({String title, String text, String? folder}) shareFinalNotice(ShareBatch b, ShareRunEnd end) {
  final done = b.count(ShareItemState.done);
  final failed = b.count(ShareItemState.failed);
  final skipped = b.count(ShareItemState.skipped);
  final folder = baseName(b.destDir).isEmpty ? b.destDir : baseName(b.destDir);
  if (end == ShareRunEnd.waitNetwork) {
    return (title: 'Upload paused', text: 'It goes on when ${b.serverName} is reachable', folder: null);
  }
  if (end == ShareRunEnd.cancelled) {
    return (title: 'Upload cancelled', text: done == 0 ? 'Nothing was uploaded' : '${_files(done)} uploaded to $folder', folder: done > 0 ? b.destDir : null);
  }
  if (failed > 0) {
    final first = b.items.firstWhere((i) => i.state == ShareItemState.failed).error ?? '';
    return (
      title: done == 0 ? "Couldn't upload ${_files(failed)}" : 'Uploaded $done of ${done + failed} files',
      text: failed == 1 ? '$first Tap to retry.' : '$failed failed. Tap to see why and retry.',
      folder: null,
    );
  }
  if (done == 0) return (title: 'Nothing uploaded', text: '${skipped == 1 ? 'It was' : 'All $skipped were'} already in $folder', folder: b.destDir);
  return (title: 'Uploaded ${_files(done)}', text: 'Open $folder on ${b.serverName}', folder: b.destDir);
}

/// The names in a server folder (an empty set when it isn't there yet:
/// the upload makes it).
Future<Set<String>> serverFolderNames(String dir) async {
  try {
    final res = await ApiClient.instance.get('/folder', query: {'path': dir});
    final data = res['data'];
    final content = data is Map ? (data['content'] as List? ?? const []) : const [];
    return {for (final e in content) if (e is Map && e['name'] != null) e['name'].toString()};
  } on ApiException catch (e) {
    if (e.isUnreachable) rethrow;
    return {};
  }
}

const shareUploadJobChannel = MethodChannel('com.fenyx.nivaroos/share_upload_job');

/// The job's Dart side (background_sync_isolate.dart `shareUploadMain`):
/// runs one batch, keeps the notification up to date, then says "done"
/// with the notification to end on and whether to run again later.
Future<void> runShareUploadJob() async {
  const job = shareUploadJobChannel;
  final cancel = TransferCancel();
  job.setMethodCallHandler((call) async {
    if (call.method == 'cancel') cancel.cancel();
  });
  Map<String, Object?> done = const {};
  try {
    final start = await job.invokeMapMethod<String, Object?>('start') ?? const {};
    final store = await ShareUploadStore.open();
    final b = store.load(start['batch']?.toString() ?? '');
    if (b == null) return;
    var end = ShareRunEnd.finished;
    if (!ApiClient.instance.hasSession || ApiClient.instance.baseUrl != b.server) {
      for (final i in b.items.where((i) => i.state == ShareItemState.pending)) {
        i.state = ShareItemState.failed;
        i.error = 'NivaroOS is no longer signed in to ${b.serverName}. Switch to it, then retry.';
      }
      store.save(b);
    } else {
      var last = DateTime(0);
      var lastPermille = -1;
      final runner = ShareUploader(
        read: (uri, offset, length) async => await job.invokeMethod<Uint8List>('read', {'uri': uri, 'offset': offset, 'length': length}) ?? Uint8List(0),
        listNames: serverFolderNames,
        save: store.save,
        onProgress: (p) {
          final now = clock.now();
          if (now.difference(last) < const Duration(seconds: 1) && p.permille == lastPermille) return;
          last = now;
          lastPermille = p.permille;
          final n = shareProgressNotice(b, p);
          job.invokeMethod('progress', {'title': n.title, 'text': n.text, 'permille': p.permille}).ignore();
        },
      );
      end = await runner.run(b, cancel);
      if (end == ShareRunEnd.waitNetwork) {
        b.waits++;
        if (b.waits > ShareBatch.maxWaits) {
          for (final i in b.items.where((i) => i.state == ShareItemState.pending)) {
            i.state = ShareItemState.failed;
            i.error = "Couldn't reach the server.";
          }
          end = ShareRunEnd.finished;
        }
        store.save(b);
      }
    }
    final n = shareFinalNotice(b, end);
    done = {'title': n.title, 'text': n.text, 'folder': n.folder, 'reschedule': end == ShareRunEnd.waitNetwork};
  } catch (e) {
    debugPrint('[share-upload] ${e.runtimeType}');
  } finally {
    try {
      await job.invokeMethod('done', done);
    } catch (_) {}
  }
}
