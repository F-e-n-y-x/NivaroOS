// Large downloads to this phone as an Android job (design 2026-10, phase 2).
//
// Files' "download to this phone" hands a pull of [PullJob.minBytes] or
// more to PullJobService.kt - a user-initiated data transfer job on
// Android 14+, like "Upload to NivaroOS" - so it carries on with the app
// closed and isn't cut off by the foreground-service limits. The job's
// headless engine runs [runPullJob]: each file with [ResumablePull] (direct
// from another phone over Tailscale when it can, else through the server,
// resuming after every drop), the batch saved after every file. When the
// network goes, the job is rescheduled and resumes from the part files.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import 'package:path_provider/path_provider.dart';

import '../screens/files/file_ops.dart';
import '../screens/files/resumable_pull.dart';
import '../screens/files/resumable_upload.dart';
import '../utils/format.dart';
import 'api_client.dart';

enum PullState { pending, done, failed }

class PullFile {
  PullFile({required this.remote, required this.size, required this.target, this.state = PullState.pending, this.error});
  final String remote;
  final int size;
  final String target;
  PullState state;
  String? error;

  Map<String, Object?> toJson() => {'remote': remote, 'size': size, 'target': target, 'state': state.name, 'error': error};

  static PullFile? fromJson(Object? j) {
    if (j is! Map || j['remote'] is! String || j['target'] is! String || j['size'] is! num) return null;
    return PullFile(
      remote: j['remote'] as String,
      size: (j['size'] as num).toInt(),
      target: j['target'] as String,
      state: PullState.values.asNameMap()[j['state']] ?? PullState.pending,
      error: j['error']?.toString(),
    );
  }
}

class PullBatch {
  PullBatch({required this.id, required this.server, required this.destDir, required this.files, this.waits = 0});
  final String id;
  final String server;
  final String destDir;
  final List<PullFile> files;

  /// Runs that stopped to wait for the network; gives up after [maxWaits].
  int waits;
  static const maxWaits = 30;

  static String newId() => '${clock.now().millisecondsSinceEpoch}-${math.Random().nextInt(1 << 32).toRadixString(36)}';

  int get totalBytes => files.fold(0, (s, f) => s + f.size);
  int count(PullState s) => files.where((f) => f.state == s).length;

  Map<String, Object?> toJson() => {'id': id, 'server': server, 'dest': destDir, 'waits': waits, 'files': [for (final f in files) f.toJson()]};

  static PullBatch? fromJson(Object? j) {
    if (j is! Map || j['id'] is! String || j['dest'] is! String) return null;
    return PullBatch(
      id: j['id'] as String,
      server: j['server']?.toString() ?? '',
      destDir: j['dest'] as String,
      waits: j['waits'] is num ? (j['waits'] as num).toInt() : 0,
      files: [for (final f in j['files'] is List ? j['files'] as List : const []) ?PullFile.fromJson(f)],
    );
  }
}

/// Batches as JSON in the app's private support folder, written atomically.
class PullStore {
  PullStore(this.dir);
  final Directory dir;

  static Future<PullStore> open() async => PullStore(Directory('${(await getApplicationSupportDirectory()).path}/pulls'));

  File _file(String id) => File('${dir.path}/${id.replaceAll(RegExp(r'[^A-Za-z0-9-]'), '')}.json');

  PullBatch? load(String id) {
    try {
      return PullBatch.fromJson(jsonDecode(_file(id).readAsStringSync()));
    } catch (_) {
      return null;
    }
  }

  void save(PullBatch b) {
    dir.createSync(recursive: true);
    final f = _file(b.id);
    File('${f.path}.tmp')
      ..writeAsStringSync(jsonEncode(b.toJson()), flush: true)
      ..renameSync(f.path);
  }
}

enum PullRunEnd { finished, waitNetwork, cancelled }

/// Pulls a batch's pending files; [save] after every file.
Future<PullRunEnd> runPull(PullBatch b, ResumablePull puller, TransferCancel cancel,
    {required void Function(PullBatch) save, void Function(int done, int total, String current)? onProgress}) async {
  final total = b.totalBytes;
  var done = b.files.where((f) => f.state == PullState.done).fold(0, (s, f) => s + f.size);
  for (final f in b.files.where((f) => f.state == PullState.pending).toList()) {
    final part = File(ResumablePull.partPath(f.target, f.remote, f.size));
    final before = done;
    try {
      await puller.fetch(f.remote, part, f.size, (have) => onProgress?.call(before + have, total, baseName(f.target)), cancel);
      if (await part.length() != f.size) {
        await ResumablePull.discard(part);
        f.state = PullState.failed;
        f.error = "It didn't arrive complete.";
      } else {
        final dest = File(f.target);
        await dest.parent.create(recursive: true);
        if (await dest.exists()) await dest.delete();
        await part.rename(f.target);
        await ResumablePull.discard(part);
        f.state = PullState.done;
        f.error = null;
      }
    } on TransferCancelled {
      await ResumablePull.discard(part);
      save(b);
      return PullRunEnd.cancelled;
    } on ApiException catch (e) {
      if (e.statusCode == null || e.statusCode! >= 500 || e.isUnreachable) {
        save(b);
        return PullRunEnd.waitNetwork; // the part file stays: the next run resumes
      }
      f.state = PullState.failed;
      f.error = e.message;
    } catch (e) {
      if (e is IOException || e is http.ClientException || e is TimeoutException) {
        save(b);
        return PullRunEnd.waitNetwork;
      }
      f.state = PullState.failed;
      f.error = e.toString();
    }
    done = before + f.size;
    save(b);
  }
  return PullRunEnd.finished;
}

String _files(int n) => formatCount(n, 'file');

({String title, String text}) pullFinalNotice(PullBatch b, PullRunEnd end) {
  final done = b.count(PullState.done);
  final failed = b.count(PullState.failed);
  final folder = baseName(b.destDir);
  return switch (end) {
    PullRunEnd.waitNetwork => (title: 'Download paused', text: 'It goes on when the network is back'),
    PullRunEnd.cancelled => (title: 'Download cancelled', text: done == 0 ? 'Nothing was downloaded' : '${_files(done)} saved in $folder'),
    _ when failed > 0 => (title: "Couldn't download ${_files(failed)}", text: b.files.firstWhere((f) => f.state == PullState.failed).error ?? ''),
    _ => (title: 'Downloaded ${_files(done)}', text: 'Saved in $folder'),
  };
}

abstract final class PullJob {
  /// Pulls this large go to the job; smaller ones finish in the app.
  static const minBytes = 512 << 20;

  static const _app = MethodChannel('com.fenyx.nivaroos/share_intent');

  /// Saves [b] and asks Android to run it; false when it refused.
  static Future<bool> start(PullBatch b) async {
    try {
      (await PullStore.open()).save(b);
      return await _app.invokeMethod<bool>('startPull', {'batch': b.id, 'bytes': b.totalBytes}) ?? false;
    } catch (_) {
      return false;
    }
  }
}

/// The job's Dart side (background_sync_isolate.dart `pullMain`).
Future<void> runPullJob() async {
  const job = MethodChannel('com.fenyx.nivaroos/pull_job');
  final cancel = TransferCancel();
  job.setMethodCallHandler((call) async {
    if (call.method == 'cancel') cancel.cancel();
  });
  Map<String, Object?> done = const {};
  try {
    final start = await job.invokeMapMethod<String, Object?>('start') ?? const {};
    final store = await PullStore.open();
    final b = store.load(start['batch']?.toString() ?? '');
    if (b == null) return;
    var end = PullRunEnd.finished;
    if (!ApiClient.instance.hasSession || ApiClient.instance.baseUrl != b.server) {
      for (final f in b.files.where((f) => f.state == PullState.pending)) {
        f.state = PullState.failed;
        f.error = 'NivaroOS is signed in to another server now.';
      }
      store.save(b);
    } else {
      var last = DateTime(0);
      final count = b.files.length;
      end = await runPull(b, ResumablePull(), cancel, save: store.save, onProgress: (d, total, current) {
        final now = clock.now();
        if (now.difference(last) < const Duration(seconds: 1)) return;
        last = now;
        final permille = total <= 0 ? 0 : (d * 1000 ~/ total).clamp(0, 1000);
        job.invokeMethod('progress', {
          'title': 'Downloading ${_files(count)} to this phone · ${permille ~/ 10}%',
          'text': current,
          'permille': permille,
        }).ignore();
      });
      if (end == PullRunEnd.waitNetwork && ++b.waits > PullBatch.maxWaits) {
        for (final f in b.files.where((f) => f.state == PullState.pending)) {
          f.state = PullState.failed;
          f.error = "Couldn't reach the server.";
        }
        end = PullRunEnd.finished;
      }
      store.save(b);
    }
    final n = pullFinalNotice(b, end);
    done = {'title': n.title, 'text': n.text, 'reschedule': end == PullRunEnd.waitNetwork};
  } catch (e) {
    debugPrint('[pull] ${e.runtimeType}');
  } finally {
    try {
      await job.invokeMethod('done', done);
    } catch (_) {}
  }
}
