/// Download Station (services/download-sidecar), through the gateway at
/// /v1/download-station with the app's session. The sidecar answers bare
/// JSON (no envelope); errors are `{"error": "..."}`, which ApiClient
/// turns into the exception's message. The lite and full browsers
/// (/b/*, /rb/*) are not used by the app.
library;

import '../services/api_client.dart';

enum DsState { queued, downloading, paused, completed, failed }

/// One download, as `DownloadView` in services/download-sidecar/engine.go.
class DsDownload {
  const DsDownload({
    required this.id,
    required this.url,
    required this.filename,
    required this.dir,
    required this.path,
    required this.size,
    required this.downloaded,
    required this.state,
    this.error = '',
    this.speed = 0,
    this.eta = -1,
    this.resumable = false,
    this.connections = 1,
    this.activeConnections = 0,
    this.createdAt,
    this.completedAt,
  });

  final String id;
  final String url;
  final String filename;

  /// The folder it is saved in.
  final String dir;

  /// The finished file; while incomplete, its `.part` file.
  final String path;

  /// Bytes; -1 while the server doesn't know.
  final int size;
  final int downloaded;
  final DsState state;
  final String error;

  /// Bytes per second.
  final double speed;

  /// Seconds left; -1 unknown.
  final int eta;
  final bool resumable;
  final int connections;
  final int activeConnections;
  final DateTime? createdAt;
  final DateTime? completedAt;

  bool get sizeKnown => size > 0;

  /// 0..1; null while the size is unknown.
  double? get fraction => state == DsState.completed ? 1 : (sizeKnown ? (downloaded / size).clamp(0, 1).toDouble() : null);

  /// Downloading or waiting its turn (the web's "Active" filter also
  /// counts paused ones).
  bool get isRunning => state == DsState.downloading || state == DsState.queued;

  factory DsDownload.fromJson(Map<String, dynamic> j) => DsDownload(
        id: j['id']?.toString() ?? '',
        url: j['url']?.toString() ?? '',
        filename: j['filename']?.toString() ?? '',
        dir: j['dir']?.toString() ?? '',
        path: j['path']?.toString() ?? '',
        size: (j['size'] as num?)?.toInt() ?? -1,
        downloaded: (j['downloaded'] as num?)?.toInt() ?? 0,
        state: DsState.values.asNameMap()[j['state']] ?? DsState.failed,
        error: j['error']?.toString() ?? '',
        speed: (j['speed'] as num?)?.toDouble() ?? 0,
        eta: (j['eta'] as num?)?.toInt() ?? -1,
        resumable: j['resumable'] == true,
        connections: (j['connections'] as num?)?.toInt() ?? 1,
        activeConnections: (j['active_connections'] as num?)?.toInt() ?? 0,
        createdAt: DateTime.tryParse(j['created_at']?.toString() ?? ''),
        completedAt: DateTime.tryParse(j['completed_at']?.toString() ?? ''),
      );
}

/// The settings the app shows (services/download-sidecar/settings.go);
/// the browser and ad-block ones stay on the web.
class DsSettings {
  const DsSettings({required this.defaultDir, required this.defaultConnections, required this.maxConcurrent, required this.speedLimit});

  final String defaultDir;
  final int defaultConnections;
  final int maxConcurrent;

  /// Bytes per second for all downloads together; 0 = unlimited.
  final int speedLimit;

  factory DsSettings.fromJson(Map<String, dynamic> j) => DsSettings(
        defaultDir: j['default_dir']?.toString() ?? '',
        defaultConnections: (j['default_connections'] as num?)?.toInt() ?? 8,
        maxConcurrent: (j['max_concurrent'] as num?)?.toInt() ?? 3,
        speedLimit: (j['speed_limit'] as num?)?.toInt() ?? 0,
      );
}

/// The links in shared or pasted text: http(s) only, which is all the
/// sidecar downloads. At most 200, like the web.
/// Punctuation that ends a sentence around a link is not part of it.
List<String> extractLinks(String text) => RegExp(r'''https?://[^\s"'<>]+''', caseSensitive: false)
    .allMatches(text)
    .map((m) => m[0]!.replaceFirst(RegExp(r'[.,;:!?)\]]+$'), ''))
    .take(200)
    .toList();

class DownloadStationApi {
  DownloadStationApi([ApiClient? client]) : _c = client ?? ApiClient.instance;

  final ApiClient _c;

  static const base = '/v1/download-station';

  /// True when [e] means Download Station isn't on this server (not
  /// installed, or not running).
  static bool isUnavailable(Object e) => e is ApiException && (e.statusCode == 404 || e.statusCode == 502 || e.statusCode == 503);

  Future<List<DsDownload>> downloads() async {
    final res = await _c.get('$base/downloads');
    final list = res['data'];
    return [for (final e in list is List ? list : const []) if (e is Map<String, dynamic>) DsDownload.fromJson(e)];
  }

  /// Adds one link. [start] false adds it paused ("Download later").
  Future<DsDownload> add(String url, {String dir = '', String filename = '', bool start = true}) async =>
      DsDownload.fromJson(await _c.post('$base/downloads', body: {
        'url': url,
        if (dir.isNotEmpty) 'dir': dir,
        if (filename.isNotEmpty) 'filename': filename,
        'start': start,
        'source': 'android',
      }));

  Future<void> pause(String id) => _c.post('$base/downloads/${Uri.encodeComponent(id)}/pause');

  /// Resume a paused download, or retry a failed one where it stopped.
  Future<void> resume(String id) => _c.post('$base/downloads/${Uri.encodeComponent(id)}/resume');

  /// From byte zero (a finished file is replaced once the new copy is done).
  Future<void> redownload(String id) => _c.post('$base/downloads/${Uri.encodeComponent(id)}/redownload');

  /// Off the list; an unfinished download's partial data always goes,
  /// a finished file only with [deleteFile].
  Future<void> remove(String id, {bool deleteFile = false}) =>
      _c.delete('$base/downloads/${Uri.encodeComponent(id)}', query: {'delete_file': '$deleteFile'});

  Future<void> pauseAll() => _c.post('$base/downloads/pause-all');
  Future<void> resumeAll() => _c.post('$base/downloads/resume-all');
  Future<void> clearCompleted() => _c.post('$base/downloads/clear-completed');

  Future<DsSettings> settings() async => DsSettings.fromJson(await _c.get('$base/settings'));

  /// Saves only the fields given.
  Future<DsSettings> saveSettings(Map<String, Object> changes) async => DsSettings.fromJson(await _c.put('$base/settings', body: changes));

  /// Where downloads may be saved: the folder picker's top level.
  Future<List<String>> roots() async {
    final res = await _c.get('$base/storage/roots');
    final r = res['roots'];
    return [for (final e in r is List ? r : const []) e.toString()];
  }

  Future<String> createFolder(String parent, String name) async =>
      (await _c.post('$base/storage/folders', body: {'parent': parent, 'name': name}))['path']?.toString() ?? '$parent/$name';
}
