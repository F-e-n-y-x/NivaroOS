// What "Back up this phone" remembers: the owner's choices (settings), how
// the last runs went (state) and the per-category file lists (manifests),
// as JSON files in the app's private support folder, written atomically.
// The app and the background job run in different isolates and read them
// from disk every time, so each sees the other's writes. The device token
// is not here: it lives in Keystore-backed secure storage
// ([SecureCredentialStore]).
import 'dart:convert';
import 'dart:io';

import 'package:clock/clock.dart';
import 'package:path_provider/path_provider.dart';

import '../services/storage_service.dart';
import 'pb_client.dart';
import 'pb_manifest.dart';
import 'pb_models.dart';
import 'pb_schedule.dart';

/// A folder picked with the system folder picker (SAF), with persisted
/// read access.
class SafFolder {
  const SafFolder({required this.uri, required this.label});
  final String uri;
  final String label;

  Map<String, String> toJson() => {'uri': uri, 'label': label};
  static SafFolder? fromJson(Object? j) =>
      j is Map && (j['uri']?.toString() ?? '').isNotEmpty ? SafFolder(uri: j['uri'].toString(), label: j['label']?.toString() ?? 'Folder') : null;

  @override
  bool operator ==(Object other) => other is SafFolder && other.uri == uri;
  @override
  int get hashCode => uri.hashCode;
}

class PhoneBackupSettings {
  const PhoneBackupSettings({
    this.categories = const {PhoneCategory.media, PhoneCategory.contacts, PhoneCategory.calendar},
    this.folders = const [],
    this.conditions = const RunConditions(),
    this.schedule = const BackupSchedule(),
    this.paused = false,
  });

  final Set<PhoneCategory> categories;
  final List<SafFolder> folders;
  final RunConditions conditions;
  final BackupSchedule schedule;
  final bool paused;

  /// What a run backs up: folders only when some are picked.
  List<PhoneCategory> get runCategories => [
        for (final c in PhoneCategory.values)
          if (categories.contains(c) && (c != PhoneCategory.files || folders.isNotEmpty)) c,
      ];

  PhoneBackupSettings copyWith({Set<PhoneCategory>? categories, List<SafFolder>? folders, RunConditions? conditions, BackupSchedule? schedule, bool? paused}) =>
      PhoneBackupSettings(
        categories: categories ?? this.categories,
        folders: folders ?? this.folders,
        conditions: conditions ?? this.conditions,
        schedule: schedule ?? this.schedule,
        paused: paused ?? this.paused,
      );

  Map<String, Object?> toJson() => {
        'categories': [for (final c in PhoneCategory.values) if (categories.contains(c)) c.wire],
        'folders': [for (final f in folders) f.toJson()],
        'wifi_only': conditions.wifiOnly,
        'charging_only': conditions.chargingOnly,
        'schedule': schedule.wire,
        'paused': paused,
      };

  factory PhoneBackupSettings.fromJson(Object? j) {
    if (j is! Map) return const PhoneBackupSettings();
    return PhoneBackupSettings(
      categories: {
        for (final c in (j['categories'] is List ? j['categories'] as List : const []))
          if (PhoneCategory.fromWire(c.toString()) != null) PhoneCategory.fromWire(c.toString())!,
      },
      folders: [for (final f in (j['folders'] is List ? j['folders'] as List : const [])) ?SafFolder.fromJson(f)],
      conditions: RunConditions(wifiOnly: j['wifi_only'] != false, chargingOnly: j['charging_only'] == true),
      schedule: BackupSchedule.parse(j['schedule']?.toString() ?? 'daily 02:00'),
      paused: j['paused'] == true,
    );
  }

  /// PUT /devices/:id/phone-settings (display only on the server).
  Map<String, Object?> toPhoneSettings({required String appVersion, required String osVersion, required String model}) => {
        'app_version': appVersion,
        'os_version': osVersion,
        'model': model,
        'paused': paused,
        'categories': {
          for (final c in PhoneCategory.values)
            c.wire: {'enabled': runCategories.contains(c), 'schedule': schedule.wire, 'conditions': conditions.wire, 'encrypted': false},
        },
      };
}

/// How one category did in the last run that covered it.
class CategoryRun {
  const CategoryRun({this.at, this.status = '', this.files = 0, this.sent = 0, this.bytes = 0, this.errors = 0, this.message = ''});

  final DateTime? at;

  /// success | partial | failed | skipped
  final String status;

  /// Files (or items) looked at, sent, and bytes sent.
  final int files;
  final int sent;
  final int bytes;
  final int errors;
  final String message;

  Map<String, Object?> toJson() => {
        if (at != null) 'at': at!.toUtc().toIso8601String(),
        'status': status,
        'files': files,
        'sent': sent,
        'bytes': bytes,
        'errors': errors,
        if (message.isNotEmpty) 'message': message,
      };

  factory CategoryRun.fromJson(Object? j) {
    if (j is! Map) return const CategoryRun();
    int n(String k) => j[k] is num ? (j[k] as num).toInt() : 0;
    return CategoryRun(
      at: DateTime.tryParse(j['at']?.toString() ?? '')?.toLocal(),
      status: j['status']?.toString() ?? '',
      files: n('files'),
      sent: n('sent'),
      bytes: n('bytes'),
      errors: n('errors'),
      message: j['message']?.toString() ?? '',
    );
  }
}

/// Where a running backup is.
class RunProgress {
  const RunProgress({this.category = '', this.phase = '', this.done = 0, this.total = 0, this.bytes = 0, this.totalBytes = 0});

  final String category;

  /// "Looking for changes", "Sending", "Exporting".
  final String phase;
  final int done;
  final int total;
  final int bytes;
  final int totalBytes;

  Map<String, Object?> toJson() => {'category': category, 'phase': phase, 'done': done, 'total': total, 'bytes': bytes, 'total_bytes': totalBytes};

  factory RunProgress.fromJson(Object? j) {
    if (j is! Map) return const RunProgress();
    int n(String k) => j[k] is num ? (j[k] as num).toInt() : 0;
    return RunProgress(category: j['category']?.toString() ?? '', phase: j['phase']?.toString() ?? '', done: n('done'), total: n('total'), bytes: n('bytes'), totalBytes: n('total_bytes'));
  }
}

/// How the last run ended.
abstract final class RunOutcomes {
  static const success = 'success';
  static const partial = 'partial';

  /// The drive holding the backups isn't connected (dest_offline):
  /// a waiting state, never an error loop.
  static const waitingDrive = 'waiting_drive';
  static const waitingNetwork = 'waiting_network';
  static const noSpace = 'no_space';

  /// The token was refused: link this phone again.
  static const revoked = 'revoked';
  static const failed = 'failed';
  static const cancelled = 'cancelled';

  /// The location needs the owner (dest_marker_mismatch, path_not_allowed).
  static const destProblem = 'dest_problem';
}

class PhoneBackupState {
  const PhoneBackupState({
    this.running = false,
    this.startedAt,
    this.heartbeat,
    this.progress,
    this.outcome = '',
    this.message = '',
    this.lastRunAt,
    this.lastSuccessAt,
    this.nextRunAt,
    this.categories = const {},
    this.errors = const [],
    this.sessionId = '',
    this.sessionDone = const [],
    this.lastSnapshotId = '',
  });

  final bool running;
  final DateTime? startedAt;

  /// Written every few seconds while running; a run whose heartbeat is
  /// older than [staleAfter] was killed and isn't running any more.
  final DateTime? heartbeat;
  final RunProgress? progress;
  final String outcome;
  final String message;
  final DateTime? lastRunAt;
  final DateTime? lastSuccessAt;
  final DateTime? nextRunAt;
  final Map<String, CategoryRun> categories;

  /// The last run's errors: {category, path, message}.
  final List<Map<String, String>> errors;

  /// The open session this phone is filling, and the categories already
  /// done in it (a run cut into slices goes on where it stopped).
  final String sessionId;
  final List<String> sessionDone;
  final String lastSnapshotId;

  static const staleAfter = Duration(minutes: 3);

  bool get isRunning => running && heartbeat != null && clock.now().difference(heartbeat!) < staleAfter;

  PhoneBackupState copyWith({
    bool? running,
    DateTime? startedAt,
    DateTime? heartbeat,
    RunProgress? progress,
    bool clearProgress = false,
    String? outcome,
    String? message,
    DateTime? lastRunAt,
    DateTime? lastSuccessAt,
    DateTime? nextRunAt,
    bool clearNextRun = false,
    Map<String, CategoryRun>? categories,
    List<Map<String, String>>? errors,
    String? sessionId,
    List<String>? sessionDone,
    String? lastSnapshotId,
  }) =>
      PhoneBackupState(
        running: running ?? this.running,
        startedAt: startedAt ?? this.startedAt,
        heartbeat: heartbeat ?? this.heartbeat,
        progress: clearProgress ? null : (progress ?? this.progress),
        outcome: outcome ?? this.outcome,
        message: message ?? this.message,
        lastRunAt: lastRunAt ?? this.lastRunAt,
        lastSuccessAt: lastSuccessAt ?? this.lastSuccessAt,
        nextRunAt: clearNextRun ? null : (nextRunAt ?? this.nextRunAt),
        categories: categories ?? this.categories,
        errors: errors ?? this.errors,
        sessionId: sessionId ?? this.sessionId,
        sessionDone: sessionDone ?? this.sessionDone,
        lastSnapshotId: lastSnapshotId ?? this.lastSnapshotId,
      );

  static String? _t(DateTime? t) => t?.toUtc().toIso8601String();
  static DateTime? _p(Object? v) => DateTime.tryParse(v?.toString() ?? '')?.toLocal();

  Map<String, Object?> toJson() => {
        'running': running,
        'started_at': _t(startedAt),
        'heartbeat': _t(heartbeat),
        if (progress != null) 'progress': progress!.toJson(),
        'outcome': outcome,
        'message': message,
        'last_run_at': _t(lastRunAt),
        'last_success_at': _t(lastSuccessAt),
        'next_run_at': _t(nextRunAt),
        'categories': {for (final e in categories.entries) e.key: e.value.toJson()},
        'errors': errors,
        'session_id': sessionId,
        'session_done': sessionDone,
        'last_snapshot_id': lastSnapshotId,
      };

  factory PhoneBackupState.fromJson(Object? j) {
    if (j is! Map) return const PhoneBackupState();
    final cats = j['categories'];
    return PhoneBackupState(
      running: j['running'] == true,
      startedAt: _p(j['started_at']),
      heartbeat: _p(j['heartbeat']),
      progress: j['progress'] is Map ? RunProgress.fromJson(j['progress']) : null,
      outcome: j['outcome']?.toString() ?? '',
      message: j['message']?.toString() ?? '',
      lastRunAt: _p(j['last_run_at']),
      lastSuccessAt: _p(j['last_success_at']),
      nextRunAt: _p(j['next_run_at']),
      categories: cats is Map ? {for (final e in cats.entries) e.key.toString(): CategoryRun.fromJson(e.value)} : const {},
      errors: [
        for (final e in (j['errors'] is List ? j['errors'] as List : const []))
          if (e is Map) {for (final f in e.entries) f.key.toString(): f.value?.toString() ?? ''},
      ],
      sessionId: j['session_id']?.toString() ?? '',
      sessionDone: [for (final c in (j['session_done'] is List ? j['session_done'] as List : const [])) c.toString()],
      lastSnapshotId: j['last_snapshot_id']?.toString() ?? '',
    );
  }
}

/// The files, in [dir].
class PhoneBackupStore {
  PhoneBackupStore(this.dir);

  final Directory dir;

  static Future<PhoneBackupStore> open() async {
    final base = await getApplicationSupportDirectory();
    final d = Directory('${base.path}/phone_backup');
    if (!d.existsSync()) d.createSync(recursive: true);
    return PhoneBackupStore(d);
  }

  File _f(String name) => File('${dir.path}/$name');

  Object? _read(String name) {
    try {
      final f = _f(name);
      if (!f.existsSync()) return null;
      return jsonDecode(f.readAsStringSync());
    } catch (_) {
      return null;
    }
  }

  void _write(String name, String text) {
    if (!dir.existsSync()) dir.createSync(recursive: true);
    final tmp = _f('$name.tmp');
    tmp.writeAsStringSync(text, flush: true);
    tmp.renameSync(_f(name).path);
  }

  /// Null until the owner has set it up.
  PhoneBackupSettings? loadSettings() {
    final j = _read('settings.json');
    return j == null ? null : PhoneBackupSettings.fromJson(j);
  }

  void saveSettings(PhoneBackupSettings s) => _write('settings.json', jsonEncode(s.toJson()));

  PhoneBackupState loadState() => PhoneBackupState.fromJson(_read('state.json'));

  void saveState(PhoneBackupState s) => _write('state.json', jsonEncode(s.toJson()));

  PhoneBackupState updateState(PhoneBackupState Function(PhoneBackupState s) f) {
    final next = f(loadState());
    saveState(next);
    return next;
  }

  LocalManifest loadManifest(String category) {
    try {
      final f = _f('manifest_$category.json');
      return f.existsSync() ? LocalManifest.decode(f.readAsStringSync()) : LocalManifest();
    } catch (_) {
      return LocalManifest();
    }
  }

  void saveManifest(String category, LocalManifest m) {
    _write('manifest_$category.json', m.encode());
    m.dirty = false;
  }

  /// Asks the running backup to stop at its next checkpoint.
  void requestCancel() => _f('cancel').writeAsStringSync('1');
  bool get cancelRequested => _f('cancel').existsSync();
  void clearCancel() {
    final f = _f('cancel');
    if (f.existsSync()) f.deleteSync();
  }

  /// A scratch folder for exports and restore downloads.
  Directory scratch(String name) {
    final d = Directory('${dir.path}/tmp/$name');
    if (!d.existsSync()) d.createSync(recursive: true);
    return d;
  }

  /// Everything but the settings (after unlinking, a new start).
  void resetRunData() {
    for (final f in dir.listSync().whereType<File>()) {
      if (!f.path.endsWith('/settings.json')) f.deleteSync();
    }
  }
}

/// Where the device credential is kept.
abstract class CredentialStore {
  Future<DeviceCredential?> load();
  Future<void> save(DeviceCredential c);
  Future<void> clear();
}

class SecureCredentialStore implements CredentialStore {
  const SecureCredentialStore();

  @override
  Future<DeviceCredential?> load() async {
    final s = await StorageService.instance.getPhoneBackupCredential();
    if (s == null || s.isEmpty) return null;
    try {
      return DeviceCredential.fromJson(jsonDecode(s));
    } catch (_) {
      return null;
    }
  }

  @override
  Future<void> save(DeviceCredential c) => StorageService.instance.setPhoneBackupCredential(jsonEncode(c.toJson()));

  @override
  Future<void> clear() => StorageService.instance.setPhoneBackupCredential(null);
}

class MemoryCredentialStore implements CredentialStore {
  MemoryCredentialStore([this.value]);
  DeviceCredential? value;
  @override
  Future<DeviceCredential?> load() async => value;
  @override
  Future<void> save(DeviceCredential c) async => value = c;
  @override
  Future<void> clear() async => value = null;
}
