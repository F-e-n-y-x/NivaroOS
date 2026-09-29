// One backup run of this phone (device API §4): config, session, then per
// category check + upload (files kinds) or export + upload (the others),
// the deleted report, and finish, which makes the snapshot.
//
// A run may be cut into slices (a background job has about 10 minutes):
// when its time is up it stops at a checkpoint and says "more"; the next
// slice starts the session again (the server hands back the open one),
// skips the categories already done in it, and the check answers `have`
// for everything sent, so nothing is sent twice. Unfinished uploads are
// resumed where the server stopped (tus). A missing drive is a waiting
// state, retried later, never an error loop; a refused token stops
// everything until the phone is linked again.
import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:clock/clock.dart';
import 'package:crypto/crypto.dart' as crypto;

import 'pb_client.dart';
import 'pb_manifest.dart';
import 'pb_messages.dart';
import 'pb_models.dart';
import 'pb_platform.dart';
import 'pb_schedule.dart';
import 'pb_store.dart';
import 'pb_tus.dart';

/// How a run (or a slice of one) ended.
enum RunEnd {
  /// The session finished (a snapshot was made, or there was nothing to do).
  done,

  /// Out of time: the next slice goes on.
  more,

  /// Can't run now (drive missing, no network, no space): try later.
  waiting,

  /// The token was refused: stop until the phone is linked again.
  revoked,

  /// Stopped by the owner.
  cancelled,

  failed,
}

class RunResult {
  const RunResult(this.end, {this.outcome = '', this.message = '', this.snapshotId = ''});
  final RunEnd end;
  final String outcome;
  final String message;
  final String snapshotId;
}

class _OutOfTime implements Exception {
  const _OutOfTime();
}

class _Cancelled implements Exception {
  const _Cancelled();
}

/// A stop for the whole run, with its outcome.
class _Abort implements Exception {
  const _Abort(this.outcome, this.message);
  final String outcome;
  final String message;
}

/// Wording for the notification and the status screen.
String categoryLabel(String wire) => PhoneCategory.fromWire(wire)?.label ?? wire;

class PhoneBackupEngine {
  PhoneBackupEngine({
    required this.client,
    required this.phone,
    required this.store,
    required this.settings,
    this.appVersion = '',
    this.osVersion = '',
    this.model = '',
    this.parallel = 2,
    Future<void> Function(Duration)? sleep,
  }) : _sleep = sleep ?? Future<void>.delayed;

  final DeviceClient client;
  final PhoneSources phone;
  final PhoneBackupStore store;
  final PhoneBackupSettings settings;
  final String appVersion;
  final String osVersion;
  final String model;

  /// Uploads at a time (the spec: 2-4 is plenty).
  final int parallel;
  final Future<void> Function(Duration) _sleep;

  DateTime? _deadline;
  PhoneLimits _limits = const PhoneLimits();
  late TusUploader _tus;
  String _sid = '';
  final List<Map<String, String>> _errors = [];
  final Map<String, CategoryRun> _cats = {};
  RunProgress _progress = const RunProgress();
  DateTime _lastReport = DateTime.fromMillisecondsSinceEpoch(0);
  int _sessionRestarts = 0;
  List<String> _runCats = const [];

  // A worker's fatal error (revoked, drive gone): the others stop too.
  Object? _fatal;

  void _checkpoint() {
    final f = _fatal;
    if (f != null) throw f;
    if (store.cancelRequested) throw const _Cancelled();
    final d = _deadline;
    if (d != null && clock.now().isAfter(d)) throw const _OutOfTime();
  }

  bool _shouldStop() {
    if (_fatal != null || store.cancelRequested) return true;
    final d = _deadline;
    return d != null && clock.now().isAfter(d);
  }

  void _error(String category, String path, String message) {
    if (_errors.length < 1000) _errors.add({'category': category, 'path': path, 'message': message});
  }

  Future<void> _report({bool force = false}) async {
    final now = clock.now();
    if (!force && now.difference(_lastReport) < const Duration(seconds: 1)) return;
    _lastReport = now;
    store.updateState((s) => s.copyWith(running: true, heartbeat: now, progress: _progress));
    final p = _progress;
    final what = categoryLabel(p.category);
    final text = p.total > 0 ? '$what · ${p.done} of ${p.total}' : (p.phase.isEmpty ? what : '$what · ${p.phase}');
    try {
      await phone.progress(title: 'Backing up this phone', text: text, done: p.done, total: p.total);
    } catch (_) {}
  }

  void _setProgress(String category, String phase, {int? done, int? total, int? bytes, int? totalBytes}) {
    _progress = RunProgress(
      category: category,
      phase: phase,
      done: done ?? (_progress.category == category ? _progress.done : 0),
      total: total ?? (_progress.category == category ? _progress.total : 0),
      bytes: bytes ?? (_progress.category == category ? _progress.bytes : 0),
      totalBytes: totalBytes ?? (_progress.category == category ? _progress.totalBytes : 0),
    );
  }

  /// Retries what a busy server refuses for a moment (429).
  Future<T> _patient<T>(Future<T> Function() call) async {
    for (var i = 0;; i++) {
      try {
        return await call();
      } on PhoneBackupError catch (e) {
        if (e.rateLimited && i < 6) {
          await _sleep(e.retryAfter ?? Duration(seconds: 10 * (i + 1)));
          _checkpoint();
          continue;
        }
        if (e.unreachable && i < 3) {
          await _sleep(Duration(seconds: 3 * (i + 1)));
          _checkpoint();
          continue;
        }
        rethrow;
      }
    }
  }

  Future<RunResult> run(JobStart start) async {
    final now = clock.now();
    _deadline = start.budget == null ? null : now.add(start.budget! - const Duration(seconds: 30));
    final before = store.loadState();
    store.saveState(before.copyWith(
      running: true,
      heartbeat: now,
      startedAt: start.reason == 'continue' && before.startedAt != null ? before.startedAt : now,
      progress: const RunProgress(phase: 'Starting'),
    ));
    RunResult result;
    try {
      result = await _run(start, before);
    } on _OutOfTime {
      result = const RunResult(RunEnd.more);
    } on _Cancelled {
      result = await _cancelled();
    } on UploadStopped {
      result = store.cancelRequested ? await _cancelled() : const RunResult(RunEnd.more);
    } on PhoneBackupError catch (e) {
      result = _errorResult(e);
    } on _Abort catch (a) {
      result = RunResult(RunEnd.waiting, outcome: a.outcome, message: a.message);
    } catch (e) {
      result = RunResult(RunEnd.failed, outcome: RunOutcomes.failed, message: 'Something went wrong: ${e.runtimeType}');
    }
    await _conclude(result);
    return result;
  }

  Future<RunResult> _cancelled() async {
    if (_sid.isNotEmpty) {
      try {
        await client.finish(_sid, 'cancelled', const []);
      } catch (_) {}
    }
    return const RunResult(RunEnd.cancelled, outcome: RunOutcomes.cancelled, message: 'The backup was stopped. What was sent is kept.');
  }

  RunResult _errorResult(PhoneBackupError e) {
    if (e.revoked) {
      return const RunResult(RunEnd.revoked, outcome: RunOutcomes.revoked, message: 'The server no longer accepts this phone. Link it again to keep backing up.');
    }
    if (e.driveMissing) {
      return const RunResult(RunEnd.waiting, outcome: RunOutcomes.waitingDrive, message: 'The drive that holds this phone’s backups isn’t connected. The backup waits for it.');
    }
    if (e.unreachable) {
      return const RunResult(RunEnd.waiting, outcome: RunOutcomes.waitingNetwork, message: 'The server can’t be reached. The backup tries again later.');
    }
    if (e.code == 'no_space') {
      return const RunResult(RunEnd.waiting, outcome: RunOutcomes.noSpace, message: 'The backup drive is full. Free up space on it, or choose another location.');
    }
    if (e.code == 'dest_marker_mismatch' || e.code == 'path_not_allowed') {
      return const RunResult(RunEnd.waiting, outcome: RunOutcomes.destProblem, message: 'The backup location can’t be used. Choose it again under Change location.');
    }
    if (e.code == 'invalid_state') {
      return const RunResult(RunEnd.waiting, outcome: RunOutcomes.waitingDrive, message: 'The backups are being moved or another backup is running. Trying again later.');
    }
    return RunResult(RunEnd.failed, outcome: RunOutcomes.failed, message: e.message.isNotEmpty ? e.message : 'The server refused the backup (${e.code}).');
  }

  Future<RunResult> _run(JobStart start, PhoneBackupState before) async {
    final config = await _patient(client.config);
    _limits = config.limits;
    _tus = TusUploader(client, chunkSize: _limits.chunkSize, sleep: _sleep);
    final dest = config.destination;
    if (!dest.usable) {
      if (dest.driveMissing) throw PhoneBackupError('dest_offline');
      throw PhoneBackupError(dest.errorCode);
    }
    final offered = config.categories.toSet();
    _runCats = [
      for (final c in settings.runCategories)
        if (offered.isEmpty || offered.contains(c.wire)) c.wire,
    ];
    if (_runCats.isEmpty) return const RunResult(RunEnd.done, outcome: '', message: 'Nothing is chosen to back up.');

    try {
      await client.putPhoneSettings(settings.toPhoneSettings(appVersion: appVersion, osVersion: osVersion, model: model));
    } on PhoneBackupError catch (e) {
      if (e.revoked) rethrow;
    }

    await _openSession(start.reason, before);
    var done = store.loadState().sessionDone.toSet();
    for (final cat in _runCats) {
      if (done.contains(cat)) continue;
      _checkpoint();
      try {
        _cats[cat] = await _category(cat);
      } on PhoneBackupError catch (e) {
        if (e.sessionClosed && _sessionRestarts < 2) {
          // The session ended (idle, or cancelled on the server): a new
          // one; the unfinished uploads resume in it.
          _sessionRestarts++;
          await _openSession(start.reason, store.loadState().copyWith(sessionId: ''));
          done = {};
          _cats[cat] = await _category(cat);
        } else if (e.revoked || e.driveMissing || e.unreachable || e.code == 'no_space' || e.sessionClosed) {
          rethrow;
        } else {
          _cats[cat] = CategoryRun(at: clock.now(), status: 'failed', message: _plain(e));
          _error(cat, '', _plain(e));
        }
      } on PlatformReadError catch (e) {
        _cats[cat] = CategoryRun(at: clock.now(), status: 'failed', message: e.message);
        _error(cat, '', e.message);
      } on _OutOfTime {
        rethrow;
      } on _Cancelled {
        rethrow;
      } on UploadStopped {
        rethrow;
      } on FileSystemException catch (e) {
        _cats[cat] = CategoryRun(at: clock.now(), status: 'failed', message: 'Couldn’t write the export: ${e.message}');
        _error(cat, '', 'Couldn’t write the export');
      }
      done.add(cat);
      store.updateState((s) => s.copyWith(sessionDone: done.toList(), categories: {...s.categories, cat: _cats[cat]!}));
    }

    final failedAll = _cats.isNotEmpty && _cats.values.every((c) => c.status == 'failed');
    final status = failedAll ? 'failed' : (_errors.isEmpty ? 'success' : 'partial');
    final errs = _errors.take(_limits.maxFinishErrors).toList();
    final fin = await _patient(() => client.finish(_sid, status, errs));
    final snap = fin.snapshot?.id ?? '';
    return RunResult(RunEnd.done, outcome: failedAll ? RunOutcomes.failed : status, snapshotId: snap, message: _summary());
  }

  String _plain(PhoneBackupError e) => switch (e.code) {
        'too_large' => 'Too large for the server',
        'validation' => 'The server refused it (${e.message.isEmpty ? 'invalid' : e.message})',
        _ => e.message.isNotEmpty ? e.message : e.code,
      };

  String _summary() {
    final sent = _cats.values.fold<int>(0, (a, c) => a + c.sent);
    final bytes = _cats.values.fold<int>(0, (a, c) => a + c.bytes);
    if (sent == 0) return _errors.isEmpty ? 'Everything was already backed up.' : 'Backed up with ${_errors.length == 1 ? '1 problem' : '${_errors.length} problems'}.';
    final what = sent == 1 ? '1 new item' : '$sent new items';
    return _errors.isEmpty ? 'Backed up $what (${_mb(bytes)}).' : 'Backed up $what, ${_errors.length == 1 ? '1 problem' : '${_errors.length} problems'}.';
  }

  static String _mb(int b) {
    if (b < 1024 * 1024) return '${(b / 1024).ceil()} KB';
    if (b < 1024 * 1024 * 1024) return '${(b / (1024 * 1024)).toStringAsFixed(1)} MB';
    return '${(b / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
  }

  Future<void> _openSession(String reason, PhoneBackupState before) async {
    final s = await _patient(() => client.startSession(_runCats, reason: reason == 'continue' ? 'schedule' : reason));
    _sid = s.id;
    final keep = s.resumed && s.id == before.sessionId;
    store.updateState((st) => st.copyWith(sessionId: s.id, sessionDone: keep ? st.sessionDone : const []));
  }

  // --- Categories ----------------------------------------------------------

  Future<CategoryRun> _category(String cat) async {
    _setProgress(cat, 'Looking for changes', done: 0, total: 0, bytes: 0, totalBytes: 0);
    await _report(force: true);
    switch (cat) {
      case 'media':
        return _files(cat, await _platform(() => phone.scanMedia(), 'Photos and videos can’t be read. Allow access to them in the app’s settings.'));
      case 'files':
        final all = <ScannedFile>[];
        final labels = <String>{};
        for (final f in settings.folders) {
          var label = safeSegment(f.label);
          for (var n = 2; labels.contains(label); n++) {
            label = '${safeSegment(f.label)} ($n)';
          }
          labels.add(label);
          try {
            all.addAll(await phone.scanTree(f.uri, label));
          } catch (e) {
            _error(cat, label, 'This folder can’t be read any more. Pick it again.');
          }
        }
        return _files(cat, all);
      case 'apks':
        final apps = await _platform(() => phone.installedApps(), 'The app list can’t be read.');
        return _files(cat, [
          for (final a in apps)
            if (!a.system)
              for (final apk in a.apks)
                ScannedFile(
                  path: '${a.package}/${a.versionCode}/${apk['name']}',
                  size: apk['size'] is num ? (apk['size'] as num).toInt() : 0,
                  mtime: apk['mtime'] is num ? (apk['mtime'] as num).toInt() : 0,
                  source: apk['path']?.toString() ?? '',
                ),
        ]);
      case 'contacts':
        final dir = store.scratch('export').path;
        final path = await _platform(() => phone.exportContacts(dir), 'Contacts can’t be read. Allow access to them in the app’s settings.');
        if (path == null) return CategoryRun(at: clock.now(), status: 'success', message: 'No contacts on this phone');
        return _export(cat, File(path), items: 0);
      case 'calendar':
        final dir = store.scratch('export').path;
        final cals = await _platform(() => phone.exportCalendars(dir), 'The calendar can’t be read. Allow access to it in the app’s settings.');
        var sent = 0, bytes = 0;
        final names = <String>{};
        for (final c in cals) {
          _checkpoint();
          var name = safeSegment(c.name.isEmpty ? 'Calendar' : c.name);
          for (var n = 2; names.contains(name); n++) {
            name = '${safeSegment(c.name)} ($n)';
          }
          names.add(name);
          final r = await _export(cat, File(c.path), name: name, items: c.events);
          sent += r.sent;
          bytes += r.bytes;
        }
        return CategoryRun(at: clock.now(), status: 'success', files: cals.length, sent: sent, bytes: bytes);
      case 'sms':
        return _messages(cat);
      case 'calllog':
        return _messages(cat);
      case 'apps':
        final apps = await _platform(() => phone.installedApps(), 'The app list can’t be read.');
        final dir = store.scratch('export');
        final f = File('${dir.path}/apps.json');
        final list = [for (final a in apps) a.toJson()]..sort((a, b) => (a['package'] as String).compareTo(b['package'] as String));
        f.writeAsStringSync(const JsonEncoder.withIndent(' ').convert({'apps': list}));
        return _export(cat, f, items: list.length);
      case 'settings':
        final s = await _platform(() => phone.readSettings(), 'The phone’s settings can’t be read.');
        final dir = store.scratch('export');
        final f = File('${dir.path}/settings.json');
        f.writeAsStringSync(const JsonEncoder.withIndent(' ').convert(s));
        return _export(cat, f, items: s.length);
    }
    return CategoryRun(at: clock.now(), status: 'skipped');
  }

  /// Runs a platform read; a refused permission becomes a plain message.
  Future<T> _platform<T>(Future<T> Function() read, String denied) async {
    try {
      return await read();
    } on PhoneBackupError {
      rethrow;
    } catch (e) {
      final text = e.toString();
      throw PlatformReadError(text.contains('SecurityException') || text.contains('PERMISSION') || text.contains('ermission') ? denied : 'Couldn’t read it: ${text.split('\n').first}');
    }
  }

  // Files kinds: media, files, apks.
  Future<CategoryRun> _files(String cat, List<ScannedFile> scanned) async {
    final manifest = store.loadManifest(cat);
    final files = <ScannedFile>[];
    final seen = <String>{};
    for (final f in scanned) {
      if (!validBackupPath(f.path)) {
        _error(cat, f.path, 'This name can’t be backed up');
        continue;
      }
      if (seen.add(f.path)) files.add(f);
    }
    // Gone from the phone since the last backup: kept on the server,
    // marked deleted there.
    final gone = manifest.gone(seen);
    for (var i = 0; i < gone.length; i += _limits.deletedBatch) {
      _checkpoint();
      final batch = gone.sublist(i, (i + _limits.deletedBatch).clamp(0, gone.length));
      await _patient(() => client.deleted(_sid, cat, batch));
      manifest.forget(batch);
    }
    if (manifest.dirty) store.saveManifest(cat, manifest);

    var sent = 0, bytes = 0, errors = 0;
    _setProgress(cat, 'Looking for changes', done: 0, total: files.length);
    await _report(force: true);
    for (var i = 0; i < files.length; i += _limits.checkBatch) {
      _checkpoint();
      final batch = files.sublist(i, (i + _limits.checkBatch).clamp(0, files.length));
      final uploads = <(ScannedFile, String, CheckAnswer)>[];
      var answers = await _patient(() => client.check(_sid, cat, [for (final f in batch) manifest.checkItem(f)]));
      final byPath = {for (final f in batch) f.path: f};
      final hashed = <String, String>{};
      final again = <ScannedFile>[];
      for (final a in answers) {
        final f = byPath[a.path];
        if (f == null) continue;
        switch (a.status) {
          case CheckStatus.needHash:
            again.add(f);
          case CheckStatus.have:
          case CheckStatus.haveElsewhere:
            manifest.record(f, manifest.knownHash(f));
          case CheckStatus.need:
            final h = manifest.knownHash(f);
            if (h.isEmpty) {
              again.add(f);
            } else {
              uploads.add((f, h, a));
            }
          case CheckStatus.excluded:
            manifest.record(f, manifest.knownHash(f));
          case CheckStatus.invalid:
            _error(cat, f.path, 'This name can’t be backed up');
            errors++;
          case CheckStatus.unknown:
            break;
        }
      }
      if (again.isNotEmpty) {
        _setProgress(cat, 'Checking files');
        for (final f in again) {
          _checkpoint();
          try {
            final h = await _hash(f);
            hashed[f.path] = h;
            manifest.record(f, h);
          } catch (e) {
            _error(cat, f.path, 'Couldn’t read this file');
            errors++;
          }
          await _report();
        }
        final withHash = [for (final f in again) if (hashed.containsKey(f.path)) f];
        if (withHash.isNotEmpty) {
          answers = await _patient(() => client.check(_sid, cat, [for (final f in withHash) manifest.checkItem(f, sha256: hashed[f.path])]));
          for (final a in answers) {
            final f = byPath[a.path];
            if (f == null) continue;
            if (a.status == CheckStatus.need) uploads.add((f, hashed[f.path]!, a));
            if (a.status == CheckStatus.invalid) {
              _error(cat, f.path, 'This name can’t be backed up');
              errors++;
            }
          }
        }
      }
      store.saveManifest(cat, manifest);

      // Send what's needed, [parallel] at a time.
      final queue = [...uploads];
      final totalBytes = uploads.fold<int>(0, (a, u) => a + u.$1.size);
      _setProgress(cat, 'Sending', totalBytes: _progress.totalBytes + totalBytes);
      Future<void> worker() async {
        while (queue.isNotEmpty) {
          _checkpoint();
          final (f, h, a) = queue.removeAt(0);
          try {
            final r = await _uploadFile(cat, f, h, a);
            if (r.sent) {
              sent++;
              bytes += f.size;
            }
          } on ChecksumMismatch {
            _error(cat, f.path, 'The file changed while it was being backed up; it goes next time');
            errors++;
            manifest.forget([f.path]);
          } on PhoneBackupError catch (e) {
            if (e.revoked || e.driveMissing || e.sessionClosed || e.code == 'no_space' || e.unreachable) {
              _fatal ??= e;
              rethrow;
            }
            _error(cat, f.path, _plain(e));
            errors++;
          } on FileSystemException {
            _error(cat, f.path, 'Couldn’t read this file');
            errors++;
          }
          _setProgress(cat, 'Sending', bytes: _progress.bytes + f.size);
          await _report();
        }
      }

      try {
        await Future.wait([for (var w = 0; w < parallel.clamp(1, 4); w++) worker()]);
      } on UploadStopped {
        _checkpoint();
        rethrow;
      }
      _setProgress(cat, 'Looking for changes', done: (i + batch.length).clamp(0, files.length));
      await _report();
    }
    if (manifest.dirty) store.saveManifest(cat, manifest);
    return CategoryRun(at: clock.now(), status: errors == 0 ? 'success' : 'partial', files: files.length, sent: sent, bytes: bytes, errors: errors);
  }

  Future<String> _hash(ScannedFile f) async {
    if (f.source.startsWith('/')) {
      final d = await crypto.sha256.bind(File(f.source).openRead()).first;
      return d.toString();
    }
    final h = await phone.sha256(f.source);
    if (h.isEmpty) throw const FileSystemException('unreadable');
    return h;
  }

  ByteSource _open(ScannedFile f) => f.source.startsWith('/') ? FileByteSource(File(f.source)) : phone.open(f.source, f.size);

  Future<UploadOutcome> _uploadFile(String cat, ScannedFile f, String sha, CheckAnswer a) async {
    final meta = UploadMeta(
      session: _sid,
      category: cat,
      path: f.path,
      sha256: sha,
      mtime: f.mtime,
      takenAt: cat == 'media' ? f.takenAt : null,
      mediaId: cat == 'media' ? f.mediaId : null,
    );
    try {
      return await _patient(() => _tus.upload(_open(f), meta, uploadId: a.uploadId, offset: a.uploadId.isEmpty ? null : a.offset, shouldStop: _shouldStop, onProgress: _onBytes));
    } on UploadStopped {
      _checkpoint();
      rethrow;
    } on ChecksumMismatch {
      // Read it again once: the file may have been written while hashing.
      final h = await _hash(f);
      if (h == sha) rethrow;
      return _patient(() => _tus.upload(_open(f), UploadMeta(session: _sid, category: cat, path: f.path, sha256: h, mtime: f.mtime, takenAt: meta.takenAt, mediaId: meta.mediaId), shouldStop: _shouldStop));
    }
  }

  // Long uploads keep the heartbeat going (the status screen and the
  // "still running" check read it).
  void _onBytes(int confirmed, int total) => unawaited(_report());

  /// A full or incremental export file.
  Future<CategoryRun> _export(String cat, File f, {String name = '', int items = 0}) async {
    _setProgress(cat, 'Sending');
    await _report();
    final sha = (await crypto.sha256.bind(f.openRead()).first).toString();
    final size = f.lengthSync();
    final r = await _patient(() => _tus.upload(FileByteSource(f), UploadMeta(session: _sid, category: cat, name: name, sha256: sha), shouldStop: _shouldStop));
    try {
      f.deleteSync();
    } catch (_) {}
    final stored = r.result == 'stored';
    return CategoryRun(at: clock.now(), status: 'success', files: items, sent: stored ? 1 : 0, bytes: stored ? size : 0, message: r.result == 'unchanged' || r.result == 'have' ? 'No changes' : '');
  }

  /// sms (with mms) or calllog: only the items the backup lacks.
  Future<CategoryRun> _messages(String cat) async {
    const page = 2000;
    final items = <MessageItem>[];
    final denied = cat == 'sms'
        ? 'Messages can’t be read. Allow SMS access for NivaroOS in the app’s settings (on Android 13 and later, first allow restricted settings).'
        : 'The call log can’t be read. Allow call log access for NivaroOS in the app’s settings (on Android 13 and later, first allow restricted settings).';
    Future<void> readAll(Future<List<Map<Object?, Object?>>> Function(int, int) read, MessageItem Function(Map<Object?, Object?>) make) async {
      for (var off = 0;; off += page) {
        _checkpoint();
        final rows = await _platform(() => read(off, page), denied);
        items.addAll(rows.map(make));
        _setProgress(cat, 'Reading', done: items.length);
        await _report();
        if (rows.length < page) break;
      }
    }

    if (cat == 'sms') {
      await readAll(phone.readSms, smsFromRow);
      await readAll(phone.readMms, mmsFromRow);
    } else {
      await readAll(phone.readCallLog, callFromRow);
    }
    if (items.isEmpty) return CategoryRun(at: clock.now(), status: 'success', message: cat == 'sms' ? 'No messages on this phone' : 'No calls on this phone');

    // Which are new to the backup.
    final byKey = <String, MessageItem>{};
    for (final i in items) {
      byKey.putIfAbsent(i.key, () => i);
    }
    final keys = byKey.keys.toList();
    final fresh = <String>{};
    _setProgress(cat, 'Looking for changes', done: 0, total: keys.length);
    for (var i = 0; i < keys.length; i += _limits.itemKeysBatch) {
      _checkpoint();
      final batch = keys.sublist(i, (i + _limits.itemKeysBatch).clamp(0, keys.length));
      fresh.addAll(await _patient(() => client.checkItems(_sid, cat, batch)));
      _setProgress(cat, 'Looking for changes', done: (i + batch.length).clamp(0, keys.length));
      await _report();
    }
    if (fresh.isEmpty) return CategoryRun(at: clock.now(), status: 'success', files: keys.length, message: 'No new ${cat == 'sms' ? 'messages' : 'calls'}');

    final out = <MessageItem>[];
    for (final k in keys) {
      if (!fresh.contains(k)) continue;
      var item = byKey[k]!;
      if (item.element == 'mms') {
        _checkpoint();
        try {
          final parts = await phone.mmsParts(item.providerId);
          item = item.withParts([for (final p in parts) mmsPart(p)]);
        } catch (_) {
          _error(cat, 'mms ${item.providerId}', 'Couldn’t read this message’s attachments');
        }
      }
      out.add(item);
    }
    out.sort((a, b) => a.dateMs.compareTo(b.dateMs));
    final dir = store.scratch('export');
    final f = File('${dir.path}/$cat.xml');
    if (cat == 'sms') {
      await writeSmsXml(f, out);
    } else {
      await writeCallsXml(f, out);
    }
    final r = await _export(cat, f, items: out.length);
    return CategoryRun(at: clock.now(), status: 'success', files: keys.length, sent: out.length, bytes: r.bytes, message: r.message);
  }

  // --- After a run ---------------------------------------------------------

  Future<void> _conclude(RunResult r) async {
    final now = clock.now();
    final sched = settings.schedule;
    DateTime? next;
    switch (r.end) {
      case RunEnd.done:
        next = settings.paused ? null : sched.nextRun(now: now, lastBackup: now);
      case RunEnd.more:
        next = null; // the job goes on at once
      case RunEnd.waiting:
      case RunEnd.failed:
        next = settings.paused ? null : sched.retryAt(now: now);
      case RunEnd.cancelled:
      case RunEnd.revoked:
        next = settings.paused || r.end == RunEnd.revoked ? null : sched.nextRun(now: now, lastBackup: now);
    }
    final st = store.updateState((s) {
      final ended = r.end != RunEnd.more;
      final cats = {...s.categories, ..._cats};
      return PhoneBackupState(
        running: !ended,
        startedAt: s.startedAt,
        heartbeat: now,
        progress: ended ? null : s.progress,
        outcome: ended ? r.outcome : s.outcome,
        message: ended ? r.message : s.message,
        lastRunAt: ended ? now : s.lastRunAt,
        lastSuccessAt: r.end == RunEnd.done && (r.outcome == RunOutcomes.success || r.outcome == RunOutcomes.partial) ? now : s.lastSuccessAt,
        nextRunAt: next,
        categories: cats,
        errors: ended ? _errors.take(200).toList() : s.errors,
        sessionId: r.end == RunEnd.done || r.end == RunEnd.cancelled ? '' : s.sessionId,
        sessionDone: r.end == RunEnd.done || r.end == RunEnd.cancelled ? const [] : s.sessionDone,
        lastSnapshotId: r.snapshotId.isNotEmpty ? r.snapshotId : s.lastSnapshotId,
      );
    });
    if (r.end != RunEnd.more) store.clearCancel();
    try {
      if (r.end == RunEnd.more) return;
      if (next != null) {
        await phone.schedule(delay: next.difference(now).isNegative ? Duration.zero : next.difference(now), wifiOnly: settings.conditions.wifiOnly, charging: settings.conditions.chargingOnly);
      } else if (r.end == RunEnd.revoked || settings.paused || sched.kind == ScheduleKind.manual) {
        await phone.cancelSchedule();
      }
      await phone.endProgress(title: _endTitle(r), text: st.message);
    } catch (_) {}
  }

  static String _endTitle(RunResult r) => switch (r.outcome) {
        RunOutcomes.success => 'Phone backed up',
        RunOutcomes.partial => 'Phone backed up, with problems',
        RunOutcomes.waitingDrive => 'Backup drive not connected',
        RunOutcomes.waitingNetwork => 'Backup waiting for the server',
        RunOutcomes.noSpace => 'Backup drive is full',
        RunOutcomes.revoked => 'Link this phone again',
        RunOutcomes.cancelled => 'Backup stopped',
        RunOutcomes.destProblem => 'Backup location needs you',
        _ => r.end == RunEnd.done ? 'Phone backup' : 'Phone backup failed',
      };
}

/// A category the phone couldn't read (a permission, a vanished folder).
class PlatformReadError implements Exception {
  const PlatformReadError(this.message);
  final String message;
  @override
  String toString() => message;
}
