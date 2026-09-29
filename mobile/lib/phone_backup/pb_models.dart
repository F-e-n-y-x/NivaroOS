// Phone backup: the device API's types (docs/specs/backup-api.json, ids
// device_*; docs/specs/2026-09-30-phone-backup-device-api.md). Parsing is
// lenient: a field the server leaves out reads as empty, never as a crash.

Map<String, dynamic> _map(Object? j) => j is Map ? j.map((k, v) => MapEntry(k.toString(), v)) : <String, dynamic>{};
String _str(Object? v) => v == null ? '' : v.toString();
int _int(Object? v) => v is num ? v.toInt() : int.tryParse(_str(v)) ?? 0;
int? _intOrNull(Object? v) => v is num ? v.toInt() : int.tryParse(_str(v));
bool _bool(Object? v) => v == true || v == 1 || v == '1' || v == 'true';
DateTime? _time(Object? v) => v is String && v.isNotEmpty ? DateTime.tryParse(v) : null;
List<Object?> _list(Object? v) => v is List ? v : const [];

/// The categories a phone backs up (§3 of the device API).
enum PhoneCategory {
  media('media', 'Photos & videos', isFiles: true),
  files('files', 'Folders', isFiles: true),
  contacts('contacts', 'Contacts'),
  calendar('calendar', 'Calendar'),
  sms('sms', 'Messages (SMS & MMS)'),
  calllog('calllog', 'Call log'),
  apps('apps', 'App list'),
  apks('apks', 'App files (APKs)', isFiles: true),
  settings('settings', 'Phone settings');

  const PhoneCategory(this.wire, this.label, {this.isFiles = false});

  /// The name on the wire ("media", "calllog").
  final String wire;
  final String label;

  /// A files kind (per-file manifest, check endpoint); otherwise an export.
  final bool isFiles;

  static PhoneCategory? fromWire(String s) {
    for (final c in values) {
      if (c.wire == s) return c;
    }
    return null;
  }
}

/// One enrolled phone (Device).
class PhoneDevice {
  const PhoneDevice({
    required this.id,
    this.name = '',
    this.platform = 'android',
    this.folder = '',
    this.destPath = '',
    this.lastBackupAt,
    this.lastStatus = '',
    this.sizeBytes = 0,
    this.moving = false,
    this.revokedAt,
    this.lastSeen,
  });

  final String id;
  final String name;
  final String platform;
  final String folder;
  final String destPath;
  final DateTime? lastBackupAt;
  final String lastStatus;
  final int sizeBytes;
  final bool moving;
  final DateTime? revokedAt;
  final DateTime? lastSeen;

  factory PhoneDevice.fromJson(Object? j) {
    final m = _map(j);
    return PhoneDevice(
      id: _str(m['id']),
      name: _str(m['name']),
      platform: _str(m['platform']),
      folder: _str(m['folder']),
      destPath: _str(m['dest_path']),
      lastBackupAt: _time(m['last_backup_at']),
      lastStatus: _str(m['last_status']),
      sizeBytes: _int(m['size_bytes']),
      moving: _bool(m['moving']),
      revokedAt: _time(m['revoked_at']),
      lastSeen: _time(m['last_seen']),
    );
  }
}

/// Where the phone's backups go, and whether backups can run there now.
class PhoneDestination {
  const PhoneDestination({
    this.location,
    this.isDefault = true,
    this.path = '',
    this.online = true,
    this.freeBytes,
    this.reserveBytes = 0,
    this.errorCode = '',
    this.detail = '',
  });

  /// The picked folder that holds the phone's folder (an Endpoint), null
  /// for the default /DATA/Backup.
  final Map<String, dynamic>? location;
  final bool isDefault;
  final String path;
  final bool online;
  final int? freeBytes;
  final int reserveBytes;

  /// dest_offline, dest_marker_mismatch, path_not_allowed; '' = usable.
  final String errorCode;
  final String detail;

  bool get usable => errorCode.isEmpty;
  bool get driveMissing => errorCode == 'dest_offline';

  /// The location's label ("Backup Stick"), '' for the default.
  String get label => _str(location?['label']);

  factory PhoneDestination.fromJson(Object? j) {
    final m = _map(j);
    return PhoneDestination(
      location: m['location'] is Map ? _map(m['location']) : null,
      isDefault: m['default'] == null ? true : _bool(m['default']),
      path: _str(m['path']),
      online: m['online'] == null ? true : _bool(m['online']),
      freeBytes: _intOrNull(m['free_bytes']),
      reserveBytes: _int(m['reserve_bytes']),
      errorCode: _str(m['error_code']),
      detail: _str(m['detail']),
    );
  }
}

/// The owner's retention settings (what's kept).
class PhoneRetention {
  const PhoneRetention({this.keepLast = 10, this.keepDays = 30, this.deletedPurgeDays = 0, this.staleDays = 7});

  final int keepLast;
  final int keepDays;
  final int deletedPurgeDays;
  final int staleDays;

  factory PhoneRetention.fromJson(Object? j) {
    final m = _map(j);
    return PhoneRetention(
      keepLast: _intOrNull(m['keep_last']) ?? 10,
      keepDays: _intOrNull(m['keep_days']) ?? 30,
      deletedPurgeDays: _int(m['deleted_purge_days']),
      staleDays: _intOrNull(m['stale_days']) ?? 7,
    );
  }

  /// "The newest 10 backups, and one a day for 30 days. Files deleted on
  /// the phone are kept."
  String get summary {
    final parts = <String>[
      keepLast == 1 ? 'The newest backup' : 'The newest $keepLast backups',
      if (keepDays > 0) keepDays == 1 ? 'and one for the last day' : 'and one a day for $keepDays days',
    ];
    final deleted = deletedPurgeDays == 0
        ? 'Files you delete on the phone are kept.'
        : 'Files you delete on the phone are kept for ${deletedPurgeDays == 1 ? '1 day' : '$deletedPurgeDays days'}.';
    return '${parts.join(', ')}. $deleted';
  }
}

/// The server limits (read them, never hard-code them).
class PhoneLimits {
  const PhoneLimits({
    this.chunkSize = 8 << 20,
    this.maxUploadSize = 64 << 30,
    this.checkBatch = 500,
    this.deletedBatch = 1000,
    this.itemKeysBatch = 1000,
    this.maxOpenUploads = 64,
    this.maxFinishErrors = 200,
    this.sessionIdleSec = 1800,
    this.checkPerMinute = 240,
  });

  final int chunkSize;
  final int maxUploadSize;
  final int checkBatch;
  final int deletedBatch;
  final int itemKeysBatch;
  final int maxOpenUploads;
  final int maxFinishErrors;
  final int sessionIdleSec;
  final int checkPerMinute;

  factory PhoneLimits.fromJson(Object? j) {
    final m = _map(j);
    int pos(String k, int d) {
      final v = _intOrNull(m[k]);
      return v == null || v <= 0 ? d : v;
    }

    return PhoneLimits(
      chunkSize: pos('chunk_size', 8 << 20),
      maxUploadSize: pos('max_upload_size', 64 << 30),
      checkBatch: pos('check_batch', 500),
      deletedBatch: pos('deleted_batch', 1000),
      itemKeysBatch: pos('item_keys_batch', 1000),
      maxOpenUploads: pos('max_open_uploads', 64),
      maxFinishErrors: pos('max_finish_errors', 200),
      sessionIdleSec: pos('session_idle_sec', 1800),
      checkPerMinute: pos('check_per_minute', 240),
    );
  }
}

/// GET /devices/:id/config.
class PhoneConfig {
  const PhoneConfig({required this.device, required this.destination, this.retention = const PhoneRetention(), this.limits = const PhoneLimits(), this.categories = const [], this.serverTime});

  final PhoneDevice device;
  final PhoneDestination destination;
  final PhoneRetention retention;
  final PhoneLimits limits;
  final List<String> categories;
  final DateTime? serverTime;

  factory PhoneConfig.fromJson(Object? j) {
    final m = _map(j);
    return PhoneConfig(
      device: PhoneDevice.fromJson(m['device']),
      destination: PhoneDestination.fromJson(m['destination']),
      retention: PhoneRetention.fromJson(m['settings']),
      limits: PhoneLimits.fromJson(m['limits']),
      categories: [for (final c in _list(m['categories'])) _str(_map(c)['category'])],
      serverTime: _time(m['server_time']),
    );
  }
}

/// One category's backup as the server keeps it (DeviceCategoryStatus).
class PhoneCategoryStatus {
  const PhoneCategoryStatus({required this.category, this.lastBackupAt, this.lastStatus = '', this.files = 0, this.deletedFiles = 0, this.exports = 0, this.items = 0, this.sizeBytes = 0});

  final String category;
  final DateTime? lastBackupAt;
  final String lastStatus;
  final int files;
  final int deletedFiles;
  final int exports;
  final int items;
  final int sizeBytes;

  factory PhoneCategoryStatus.fromJson(Object? j) {
    final m = _map(j);
    return PhoneCategoryStatus(
      category: _str(m['category']),
      lastBackupAt: _time(m['last_backup_at']),
      lastStatus: _str(m['last_status']),
      files: _int(m['files']),
      deletedFiles: _int(m['deleted_files']),
      exports: _int(m['exports']),
      items: _int(m['items']),
      sizeBytes: _int(m['size_bytes']),
    );
  }
}

/// A location change in progress (or failed).
class PhoneMove {
  const PhoneMove({this.state = '', this.targetPath = '', this.files = 0, this.totalFiles = 0, this.bytes = 0, this.totalBytes = 0, this.error = ''});

  final String state;
  final String targetPath;
  final int files;
  final int totalFiles;
  final int bytes;
  final int totalBytes;
  final String error;

  factory PhoneMove.fromJson(Object? j) {
    final m = _map(j);
    return PhoneMove(
      state: _str(m['state']),
      targetPath: _str(m['target_path']),
      files: _int(m['files']),
      totalFiles: _int(m['total_files']),
      bytes: _int(m['bytes']),
      totalBytes: _int(m['total_bytes']),
      error: _str(m['error']),
    );
  }
}

/// GET /devices/:id (owner): the phone's page.
class PhoneDeviceDetail {
  const PhoneDeviceDetail({required this.device, required this.destination, this.retention = const PhoneRetention(), this.categories = const [], this.move, this.snapshots = 0, this.openSessions = 0});

  final PhoneDevice device;
  final PhoneDestination destination;
  final PhoneRetention retention;
  final List<PhoneCategoryStatus> categories;
  final PhoneMove? move;
  final int snapshots;
  final int openSessions;

  PhoneCategoryStatus? category(String wire) {
    for (final c in categories) {
      if (c.category == wire) return c;
    }
    return null;
  }

  /// Whether a location change has anything to move.
  bool get hasBackups => snapshots > 0 || device.sizeBytes > 0;

  factory PhoneDeviceDetail.fromJson(Object? j) {
    final m = _map(j);
    return PhoneDeviceDetail(
      device: PhoneDevice.fromJson(m['device']),
      destination: PhoneDestination.fromJson(m['destination']),
      retention: PhoneRetention.fromJson(m['settings']),
      categories: [for (final c in _list(m['categories'])) PhoneCategoryStatus.fromJson(c)],
      move: m['move'] is Map ? PhoneMove.fromJson(m['move']) : null,
      snapshots: _int(m['snapshots']),
      openSessions: _list(m['open_sessions']).length,
    );
  }
}

/// A session's counts.
class PhoneCounts {
  const PhoneCounts({this.uploaded = 0, this.linked = 0, this.unchanged = 0, this.deleted = 0, this.exports = 0, this.bytes = 0, this.errors = 0});

  final int uploaded;
  final int linked;
  final int unchanged;
  final int deleted;
  final int exports;
  final int bytes;
  final int errors;

  factory PhoneCounts.fromJson(Object? j) {
    final m = _map(j);
    return PhoneCounts(
      uploaded: _int(m['uploaded']),
      linked: _int(m['linked']),
      unchanged: _int(m['unchanged']),
      deleted: _int(m['deleted']),
      exports: _int(m['exports']),
      bytes: _int(m['bytes']),
      errors: _int(m['errors']),
    );
  }
}

/// POST /devices/:id/sessions.
class PhoneSession {
  const PhoneSession({required this.id, this.categories = const [], this.status = '', this.resumed = false, this.snapshotId = '', this.counts = const PhoneCounts()});

  final String id;
  final List<String> categories;
  final String status;
  final bool resumed;
  final String snapshotId;
  final PhoneCounts counts;

  factory PhoneSession.fromJson(Object? j) {
    final m = _map(j);
    return PhoneSession(
      id: _str(m['id']),
      categories: [for (final c in _list(m['categories'])) _str(c)],
      status: _str(m['status']),
      resumed: _bool(m['resumed']),
      snapshotId: _str(m['snapshot_id']),
      counts: PhoneCounts.fromJson(m['counts']),
    );
  }
}

/// A finished backup: a point in time to browse and restore.
class PhoneSnapshot {
  const PhoneSnapshot({required this.id, this.takenAt, this.categories = const [], this.status = '', this.counts = const PhoneCounts()});

  final String id;
  final DateTime? takenAt;
  final List<String> categories;
  final String status;
  final PhoneCounts counts;

  factory PhoneSnapshot.fromJson(Object? j) {
    final m = _map(j);
    return PhoneSnapshot(
      id: _str(m['id']),
      takenAt: _time(m['taken_at']),
      categories: [for (final c in _list(m['categories'])) _str(c)],
      status: _str(m['status']),
      counts: PhoneCounts.fromJson(m['counts']),
    );
  }
}

/// POST .../finish.
class PhoneFinishResult {
  const PhoneFinishResult({this.snapshot, this.pendingUploads = 0});
  final PhoneSnapshot? snapshot;
  final int pendingUploads;

  factory PhoneFinishResult.fromJson(Object? j) {
    final m = _map(j);
    return PhoneFinishResult(snapshot: m['snapshot'] is Map ? PhoneSnapshot.fromJson(m['snapshot']) : null, pendingUploads: _int(m['pending_uploads']));
  }
}

/// One file offered to the check endpoint.
class CheckItem {
  const CheckItem({required this.path, required this.size, required this.mtime, this.sha256 = ''});

  final String path;
  final int size;

  /// Unix milliseconds.
  final int mtime;

  /// Lowercase hex, or '' when not hashed yet.
  final String sha256;

  Map<String, Object> toJson() => {'path': path, 'size': size, 'mtime': mtime, 'sha256': sha256};
}

/// The server's answer for one checked file.
enum CheckStatus { have, needHash, need, haveElsewhere, excluded, invalid, unknown }

CheckStatus checkStatusOf(String s) => switch (s) {
      'have' => CheckStatus.have,
      'need_hash' => CheckStatus.needHash,
      'need' => CheckStatus.need,
      'have_elsewhere' => CheckStatus.haveElsewhere,
      'excluded' => CheckStatus.excluded,
      'invalid' => CheckStatus.invalid,
      _ => CheckStatus.unknown,
    };

class CheckAnswer {
  const CheckAnswer({required this.path, required this.status, this.uploadId = '', this.offset = 0});

  final String path;
  final CheckStatus status;
  final String uploadId;
  final int offset;

  factory CheckAnswer.fromJson(Object? j) {
    final m = _map(j);
    return CheckAnswer(path: _str(m['path']), status: checkStatusOf(_str(m['status'])), uploadId: _str(m['upload_id']), offset: _int(m['offset']));
  }
}

/// POST /devices/:id/uploads.
class UploadCreated {
  const UploadCreated({this.uploadId = '', this.offset = 0, this.length = 0, this.status = ''});

  final String uploadId;
  final int offset;
  final int length;

  /// created | resumed | have | excluded | stored
  final String status;

  factory UploadCreated.fromJson(Object? j) {
    final m = _map(j);
    return UploadCreated(uploadId: _str(m['upload_id']), offset: _int(m['offset']), length: _int(m['length']), status: _str(m['status']));
  }
}

/// One entry of a snapshot's folder.
class PhoneBrowseEntry {
  const PhoneBrowseEntry({required this.name, required this.path, this.dir = false, this.size = 0, this.mtime = 0, this.sha256 = '', this.takenAt = 0, this.deletedOnDeviceAt, this.version = false});

  final String name;
  final String path;
  final bool dir;
  final int size;
  final int mtime;
  final String sha256;
  final int takenAt;
  final DateTime? deletedOnDeviceAt;
  final bool version;

  factory PhoneBrowseEntry.fromJson(Object? j) {
    final m = _map(j);
    return PhoneBrowseEntry(
      name: _str(m['name']),
      path: _str(m['path']),
      dir: _bool(m['dir']),
      size: _int(m['size']),
      mtime: _int(m['mtime']),
      sha256: _str(m['sha256']),
      takenAt: _int(m['taken_at']),
      deletedOnDeviceAt: _time(m['deleted_on_device_at']),
      version: _bool(m['version']),
    );
  }
}

class PhoneBrowseResult {
  const PhoneBrowseResult({this.entries = const [], this.truncated = false, this.takenAt});
  final List<PhoneBrowseEntry> entries;
  final bool truncated;
  final DateTime? takenAt;

  factory PhoneBrowseResult.fromJson(Object? j) {
    final m = _map(j);
    return PhoneBrowseResult(entries: [for (final e in _list(m['entries'])) PhoneBrowseEntry.fromJson(e)], truncated: _bool(m['truncated']), takenAt: _time(m['taken_at']));
  }
}

/// One export file (contacts, a calendar, an SMS batch).
class PhoneExport {
  const PhoneExport({required this.id, required this.category, this.name = '', this.takenAt, this.size = 0, this.sha256 = '', this.items = 0, this.encrypted = false});

  final String id;
  final String category;
  final String name;
  final DateTime? takenAt;
  final int size;
  final String sha256;
  final int items;
  final bool encrypted;

  factory PhoneExport.fromJson(Object? j) {
    final m = _map(j);
    return PhoneExport(
      id: _str(m['id']),
      category: _str(m['category']),
      name: _str(m['name']),
      takenAt: _time(m['taken_at']),
      size: _int(m['size']),
      sha256: _str(m['sha256']),
      items: _int(m['items']),
      encrypted: _bool(m['encrypted']),
    );
  }
}

/// One file of a restore manifest page.
class RestoreItem {
  const RestoreItem({required this.category, required this.path, this.size = 0, this.mtime = 0, this.sha256 = '', this.takenAt = 0, this.deletedOnDeviceAt, this.content = ''});

  final String category;
  final String path;
  final int size;
  final int mtime;
  final String sha256;
  final int takenAt;
  final DateTime? deletedOnDeviceAt;

  /// A GET path under /v1/backup.
  final String content;

  factory RestoreItem.fromJson(Object? j) {
    final m = _map(j);
    return RestoreItem(
      category: _str(m['category']),
      path: _str(m['path']),
      size: _int(m['size']),
      mtime: _int(m['mtime']),
      sha256: _str(m['sha256']),
      takenAt: _int(m['taken_at']),
      deletedOnDeviceAt: _time(m['deleted_on_device_at']),
      content: _str(m['content']),
    );
  }
}

class RestoreManifestPage {
  const RestoreManifestPage({this.items = const [], this.exports = const [], this.nextAfter = ''});
  final List<RestoreItem> items;
  final List<PhoneExport> exports;
  final String nextAfter;

  factory RestoreManifestPage.fromJson(Object? j) {
    final m = _map(j);
    return RestoreManifestPage(
      items: [for (final e in _list(m['items'])) RestoreItem.fromJson(e)],
      exports: [for (final e in _list(m['exports'])) PhoneExport.fromJson(e)],
      nextAfter: _str(m['next_after']),
    );
  }
}
