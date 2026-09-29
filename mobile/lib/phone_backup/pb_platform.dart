// The phone's side of the backup: what only Android can read or do
// (MediaStore, SAF folders, contacts, calendars, messages, the call log,
// installed apps, the job scheduler, the progress notification), through
// the com.fenyx.nivaroos/phone_backup channel
// (android/.../PhoneBackupBridge.kt). The engine only sees
// [PhoneSources], so tests run it against a fake phone.
import 'package:flutter/services.dart';

import 'pb_manifest.dart';
import 'pb_tus.dart';

/// A calendar exported to an .ics file.
class CalendarExport {
  const CalendarExport({required this.name, required this.path, this.events = 0});
  final String name;
  final String path;
  final int events;
}

/// An installed app.
class InstalledApp {
  const InstalledApp({required this.package, this.label = '', this.versionName = '', this.versionCode = 0, this.system = false, this.installer = '', this.firstInstall = 0, this.lastUpdate = 0, this.apks = const [], this.size = 0});

  final String package;
  final String label;
  final String versionName;
  final int versionCode;
  final bool system;
  final String installer;
  final int firstInstall;
  final int lastUpdate;

  /// base.apk first, then split_*.apk: {name, path, size, mtime}.
  final List<Map<String, Object?>> apks;
  final int size;

  factory InstalledApp.fromMap(Map<Object?, Object?> m) {
    int n(Object? v) => v is num ? v.toInt() : 0;
    return InstalledApp(
      package: m['package']?.toString() ?? '',
      label: m['label']?.toString() ?? '',
      versionName: m['version_name']?.toString() ?? '',
      versionCode: n(m['version_code']),
      system: m['system'] == true,
      installer: m['installer']?.toString() ?? '',
      firstInstall: n(m['first_install']),
      lastUpdate: n(m['last_update']),
      apks: [for (final a in (m['apks'] is List ? m['apks'] as List : const [])) if (a is Map) a.map((k, v) => MapEntry(k.toString(), v))],
      size: n(m['size']),
    );
  }

  Map<String, Object?> toJson() => {
        'package': package,
        'label': label,
        'version_name': versionName,
        'version_code': versionCode,
        'system': system,
        if (installer.isNotEmpty) 'installer': installer,
        'first_install': firstInstall,
        'last_update': lastUpdate,
        'size': size,
      };
}

/// Why a run started (the job tells Dart).
class JobStart {
  const JobStart({this.reason = 'schedule', this.budget, this.userInitiated = false});

  /// schedule | manual | continue
  final String reason;

  /// How long this slice may run (null: no limit, a user-initiated job).
  final Duration? budget;
  final bool userInitiated;
}

abstract class PhoneSources {
  /// Photos and videos (MediaStore, every volume, no pending or trashed).
  Future<List<ScannedFile>> scanMedia();

  /// Every file under a picked folder, paths starting with [label].
  Future<List<ScannedFile>> scanTree(String treeUri, String label);

  Future<String> sha256(String source);
  ByteSource open(String source, int length);

  /// Writes every contact into one vCard file; its path (null: none).
  Future<String?> exportContacts(String dir);
  Future<List<CalendarExport>> exportCalendars(String dir);

  /// Rows of content://sms, content://mms (with addrs) and the call log,
  /// [limit] at a time, oldest first.
  Future<List<Map<Object?, Object?>>> readSms(int offset, int limit);
  Future<List<Map<Object?, Object?>>> readMms(int offset, int limit);
  Future<List<Map<Object?, Object?>>> mmsParts(int mmsId, {int maxBytes = 20 << 20});
  Future<List<Map<Object?, Object?>>> readCallLog(int offset, int limit);

  Future<List<InstalledApp>> installedApps();
  Future<Map<String, Object?>> readSettings();

  /// Permissions granted, by Android name.
  Future<Map<String, bool>> permissions(List<String> names);

  Future<void> progress({required String title, required String text, int done = 0, int total = 0, bool ongoing = true});
  Future<void> endProgress({String? title, String? text});

  /// Schedules the next run of the backup job.
  Future<void> schedule({required Duration delay, required bool wifiOnly, required bool charging});
  Future<void> cancelSchedule();
}

class ChannelPhoneSources implements PhoneSources {
  const ChannelPhoneSources();

  static const channel = MethodChannel('com.fenyx.nivaroos/phone_backup');

  static Future<T?> _call<T>(String method, [Map<String, Object?>? args]) => channel.invokeMethod<T>(method, args);

  static List<Map<Object?, Object?>> _rows(Object? v) => [for (final r in (v is List ? v : const [])) if (r is Map) r];

  static ScannedFile _file(Map<Object?, Object?> m) {
    int n(Object? v) => v is num ? v.toInt() : 0;
    return ScannedFile(path: m['path']?.toString() ?? '', size: n(m['size']), mtime: n(m['mtime']), source: m['uri']?.toString() ?? '', takenAt: n(m['taken_at']), mediaId: n(m['id']));
  }

  @override
  Future<List<ScannedFile>> scanMedia() async => [for (final r in _rows(await _call<List<Object?>>('scanMedia'))) _file(r)];

  @override
  Future<List<ScannedFile>> scanTree(String treeUri, String label) async =>
      [for (final r in _rows(await _call<List<Object?>>('scanTree', {'uri': treeUri, 'label': label}))) _file(r)];

  @override
  Future<String> sha256(String source) async => await _call<String>('sha256', {'source': source}) ?? '';

  @override
  ByteSource open(String source, int length) => _ChannelSource(source, length);

  @override
  Future<String?> exportContacts(String dir) => _call<String>('exportContacts', {'dir': dir});

  @override
  Future<List<CalendarExport>> exportCalendars(String dir) async => [
        for (final r in _rows(await _call<List<Object?>>('exportCalendars', {'dir': dir})))
          CalendarExport(name: r['name']?.toString() ?? '', path: r['path']?.toString() ?? '', events: r['events'] is num ? (r['events'] as num).toInt() : 0),
      ];

  @override
  Future<List<Map<Object?, Object?>>> readSms(int offset, int limit) async => _rows(await _call<List<Object?>>('readSms', {'offset': offset, 'limit': limit}));

  @override
  Future<List<Map<Object?, Object?>>> readMms(int offset, int limit) async => _rows(await _call<List<Object?>>('readMms', {'offset': offset, 'limit': limit}));

  @override
  Future<List<Map<Object?, Object?>>> mmsParts(int mmsId, {int maxBytes = 20 << 20}) async => _rows(await _call<List<Object?>>('mmsParts', {'id': mmsId, 'maxBytes': maxBytes}));

  @override
  Future<List<Map<Object?, Object?>>> readCallLog(int offset, int limit) async => _rows(await _call<List<Object?>>('readCallLog', {'offset': offset, 'limit': limit}));

  @override
  Future<List<InstalledApp>> installedApps() async => [for (final r in _rows(await _call<List<Object?>>('installedApps'))) InstalledApp.fromMap(r)];

  @override
  Future<Map<String, Object?>> readSettings() async {
    final m = await _call<Map<Object?, Object?>>('readSettings');
    return {for (final e in (m ?? const {}).entries) e.key.toString(): e.value};
  }

  @override
  Future<Map<String, bool>> permissions(List<String> names) async {
    final m = await _call<Map<Object?, Object?>>('permissions', {'names': names});
    return {for (final e in (m ?? const {}).entries) e.key.toString(): e.value == true};
  }

  @override
  Future<void> progress({required String title, required String text, int done = 0, int total = 0, bool ongoing = true}) =>
      _call('progress', {'title': title, 'text': text, 'done': done, 'total': total, 'ongoing': ongoing});

  @override
  Future<void> endProgress({String? title, String? text}) => _call('endProgress', {'title': title, 'text': text});

  @override
  Future<void> schedule({required Duration delay, required bool wifiOnly, required bool charging}) =>
      _call('schedule', {'delayMs': delay.inMilliseconds, 'wifiOnly': wifiOnly, 'charging': charging});

  @override
  Future<void> cancelSchedule() => _call('cancelSchedule');

  // --- App-only (need the Activity) --------------------------------------

  /// Starts a backup now: a user-initiated job on Android 14+, else an
  /// expedited one. [anyNetwork] lets it run on mobile data.
  static Future<bool> runNow({required bool anyNetwork}) async => await _call<bool>('runNow', {'anyNetwork': anyNetwork}) ?? false;

  /// Asks for Android permissions (the screen has explained why first).
  static Future<Map<String, bool>> request(List<String> names) async {
    final m = await _call<Map<Object?, Object?>>('requestPermissions', {'names': names});
    return {for (final e in (m ?? const {}).entries) e.key.toString(): e.value == true};
  }

  /// The system folder picker; the folder with persisted read access.
  static Future<({String uri, String label})?> pickFolder() async {
    final m = await _call<Map<Object?, Object?>>('pickFolder');
    if (m == null || (m['uri']?.toString() ?? '').isEmpty) return null;
    return (uri: m['uri'].toString(), label: m['label']?.toString() ?? 'Folder');
  }

  static Future<void> releaseFolder(String uri) => _call('releaseFolder', {'uri': uri});

  /// Opens this app's page in system settings (permissions, restricted
  /// settings).
  static Future<void> openAppSettings() => _call('openAppSettings');

  /// Info for the phone-settings report.
  static Future<Map<String, String>> deviceInfo() async {
    final m = await _call<Map<Object?, Object?>>('deviceInfo');
    return {for (final e in (m ?? const {}).entries) e.key.toString(): e.value?.toString() ?? ''};
  }

  static Future<bool> isRunning() async => await _call<bool>('isRunning') ?? false;

  // Restore

  /// Copies a downloaded file into the phone's photos (MediaStore) at
  /// [relativePath] ("DCIM/Camera/"); the new item's URI.
  static Future<String?> saveToMedia({required String tmpPath, required String relativePath, required String name, int takenAt = 0, int mtime = 0}) =>
      _call<String>('saveToMedia', {'tmp': tmpPath, 'relativePath': relativePath, 'name': name, 'takenAt': takenAt, 'mtime': mtime});

  /// Copies a downloaded file into Download/[subPath].
  static Future<String?> saveToDownloads({required String tmpPath, required String subPath, required String name}) =>
      _call<String>('saveToDownloads', {'tmp': tmpPath, 'subPath': subPath, 'name': name});

  /// Whether a media file with this relative path, name and size is
  /// already on the phone.
  static Future<bool> mediaExists({required String relativePath, required String name, required int size}) async =>
      await _call<bool>('mediaExists', {'relativePath': relativePath, 'name': name, 'size': size}) ?? false;

  static Future<bool> isDefaultSmsApp() async => await _call<bool>('isDefaultSmsApp') ?? false;

  /// Asks to become the default SMS app (RoleManager); whether it is now.
  static Future<bool> requestSmsRole() async => await _call<bool>('requestSmsRole') ?? false;

  /// Opens the default apps settings (to switch back after a restore).
  static Future<void> openDefaultApps() => _call('openDefaultApps');

  /// Writes the messages of an SMS Backup & Restore file into the phone
  /// (only as the default SMS app); {inserted, skipped, failed}.
  static Future<Map<String, int>> restoreSms(String path) async => _counts(await _call<Map<Object?, Object?>>('restoreSms', {'path': path}));

  static Future<Map<String, int>> restoreCallLog(String path) async => _counts(await _call<Map<Object?, Object?>>('restoreCallLog', {'path': path}));

  static Map<String, int> _counts(Map<Object?, Object?>? m) => {for (final e in (m ?? const {}).entries) e.key.toString(): e.value is num ? (e.value as num).toInt() : 0};
}

class _ChannelSource implements ByteSource {
  _ChannelSource(this.source, this.length);
  final String source;
  @override
  final int length;

  @override
  Future<Uint8List> read(int offset, int len) async =>
      await ChannelPhoneSources.channel.invokeMethod<Uint8List>('read', {'source': source, 'offset': offset, 'length': len}) ?? Uint8List(0);
}

/// Android permission names the categories need.
abstract final class AndroidPermissions {
  static const readMediaImages = 'android.permission.READ_MEDIA_IMAGES';
  static const readMediaVideo = 'android.permission.READ_MEDIA_VIDEO';
  static const readExternalStorage = 'android.permission.READ_EXTERNAL_STORAGE';
  static const accessMediaLocation = 'android.permission.ACCESS_MEDIA_LOCATION';
  static const readContacts = 'android.permission.READ_CONTACTS';
  static const readCalendar = 'android.permission.READ_CALENDAR';
  static const readSms = 'android.permission.READ_SMS';
  static const readCallLog = 'android.permission.READ_CALL_LOG';
  static const writeCallLog = 'android.permission.WRITE_CALL_LOG';
  static const postNotifications = 'android.permission.POST_NOTIFICATIONS';
}
