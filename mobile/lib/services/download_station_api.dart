/// Download Station (services/download-sidecar), through the gateway at
/// /v1/download-station with the app's session. The sidecar answers bare
/// JSON (no envelope); errors are `{"error": "..."}`, which ApiClient
/// turns into the exception's message. The lite and full browsers
/// (/b/*, /rb/*) are not used by the app.
library;

import 'dart:convert';

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

/// One torrent, as `TorrentInfo` in services/download-sidecar/torrent.go -
/// the same whichever engine runs. [state] is one of downloading, stalled,
/// metadata, seeding, queued, checking, moving, paused, completed, error.
class DsTorrent {
  const DsTorrent({
    required this.hash,
    required this.name,
    required this.state,
    this.progress = 0,
    this.size = 0,
    this.done = 0,
    this.downloaded = 0,
    this.uploaded = 0,
    this.dlSpeed = 0,
    this.upSpeed = 0,
    this.seeds = 0,
    this.seedsTotal = 0,
    this.peers = 0,
    this.peersTotal = 0,
    this.ratio = 0,
    this.eta = -1,
    this.dir = '',
    this.category = '',
    this.private,
    this.hasMetadata = true,
    this.sequential = false,
    this.firstLast = false,
    this.seedingTime = 0,
    this.error = '',
  });

  final String hash;
  final String name;
  final String state;

  /// 0..1 of the selected files.
  final double progress;
  final int size;
  final int done;
  final int downloaded;
  final int uploaded;

  /// Bytes per second.
  final int dlSpeed;
  final int upSpeed;
  final int seeds;
  final int seedsTotal;
  final int peers;
  final int peersTotal;
  final double ratio;

  /// Seconds left; -1 unknown.
  final int eta;
  final String dir;
  final String category;

  /// Null while unknown (no metadata yet).
  final bool? private;
  final bool hasMetadata;
  final bool sequential;
  final bool firstLast;

  /// Seconds.
  final int seedingTime;
  final String error;

  /// Stopped: resume to start it again.
  bool get isPaused => state == 'paused' || state == 'completed' || state == 'error';
  bool get isFinished => progress >= 1;

  static int _i(Object? v, [int d = 0]) => (v as num?)?.toInt() ?? d;

  factory DsTorrent.fromJson(Map<String, dynamic> j) => DsTorrent(
        hash: j['hash']?.toString() ?? '',
        name: j['name']?.toString() ?? '',
        state: j['state']?.toString() ?? 'error',
        progress: (j['progress'] as num?)?.toDouble() ?? 0,
        size: _i(j['size']),
        done: _i(j['done']),
        downloaded: _i(j['downloaded']),
        uploaded: _i(j['uploaded']),
        dlSpeed: _i(j['dl_speed']),
        upSpeed: _i(j['up_speed']),
        seeds: _i(j['seeds']),
        seedsTotal: _i(j['seeds_total']),
        peers: _i(j['peers']),
        peersTotal: _i(j['peers_total']),
        ratio: (j['ratio'] as num?)?.toDouble() ?? 0,
        eta: _i(j['eta'], -1),
        dir: j['dir']?.toString() ?? '',
        category: j['category']?.toString() ?? '',
        private: j['private'] as bool?,
        hasMetadata: j['has_metadata'] != false,
        sequential: j['sequential'] == true,
        firstLast: j['first_last'] == true,
        seedingTime: _i(j['seeding_time']),
        error: j['error']?.toString() ?? '',
      );
}

class DsTorrentFile {
  const DsTorrentFile({required this.index, required this.name, required this.size, required this.progress, required this.priority});

  final int index;
  final String name;
  final int size;
  final double progress;

  /// 0 = don't download, 1 normal, 6 high, 7 maximum.
  final int priority;

  factory DsTorrentFile.fromJson(Map<String, dynamic> j) => DsTorrentFile(
        index: (j['index'] as num?)?.toInt() ?? 0,
        name: j['name']?.toString() ?? '',
        size: (j['size'] as num?)?.toInt() ?? 0,
        progress: (j['progress'] as num?)?.toDouble() ?? 0,
        priority: (j['priority'] as num?)?.toInt() ?? 1,
      );
}

class DsTracker {
  const DsTracker({required this.url, required this.status, this.peers = 0, this.message = ''});

  final String url;

  /// working, updating, not_working, not_contacted, disabled.
  final String status;
  final int peers;
  final String message;

  factory DsTracker.fromJson(Map<String, dynamic> j) => DsTracker(
        url: j['url']?.toString() ?? '',
        status: j['status']?.toString() ?? '',
        peers: (j['peers'] as num?)?.toInt() ?? 0,
        message: j['message']?.toString() ?? '',
      );
}

class DsTorrentDetail {
  const DsTorrentDetail(this.torrent, this.files, this.trackers);

  final DsTorrent torrent;
  final List<DsTorrentFile> files;
  final List<DsTracker> trackers;

  factory DsTorrentDetail.fromJson(Map<String, dynamic> j) => DsTorrentDetail(
        DsTorrent.fromJson(j),
        [for (final e in j['files'] is List ? j['files'] as List : const []) if (e is Map<String, dynamic>) DsTorrentFile.fromJson(e)],
        [for (final e in j['trackers'] is List ? j['trackers'] as List : const []) if (e is Map<String, dynamic>) DsTracker.fromJson(e)],
      );
}

/// GET /torrents: the list plus which engine runs it.
class DsTorrents {
  const DsTorrents({required this.engine, required this.running, required this.torrents, this.qbittorrent = false, this.unsupported = const [], this.altSpeedActive = false, this.dlSpeed = 0, this.upSpeed = 0});

  /// qbittorrent, external or builtin.
  final String engine;

  /// False while the engine is stopped for being idle (the list is then
  /// the last one it reported).
  final bool running;
  final bool qbittorrent;

  /// Settings the engine can't honour ('all' for the user's own qBittorrent).
  final List<String> unsupported;
  final bool altSpeedActive;
  final int dlSpeed;
  final int upSpeed;
  final List<DsTorrent> torrents;

  bool supports(String key) => !unsupported.contains('all') && !unsupported.contains(key);

  factory DsTorrents.fromJson(Map<String, dynamic> j) => DsTorrents(
        engine: j['engine']?.toString() ?? '',
        running: j['running'] == true,
        qbittorrent: j['qbittorrent'] == true,
        unsupported: [for (final e in j['unsupported'] is List ? j['unsupported'] as List : const []) e.toString()],
        altSpeedActive: j['alt_speed_active'] == true,
        dlSpeed: (j['dl_speed'] as num?)?.toInt() ?? 0,
        upSpeed: (j['up_speed'] as num?)?.toInt() ?? 0,
        torrents: [for (final e in j['torrents'] is List ? j['torrents'] as List : const []) if (e is Map<String, dynamic>) DsTorrent.fromJson(e)],
      );
}

/// A .torrent file to add: picked in the app, or opened with NivaroOS
/// from another app (MainActivity's ACTION_VIEW).
class TorrentFile {
  const TorrentFile(this.name, this.bytes);
  final String name;
  final List<int> bytes;
}

/// Magnet links and http(s) links to .torrent files in shared or pasted
/// text - what goes to the torrent engine rather than the downloader.
List<String> extractTorrentSources(String text) => [
      for (final m in RegExp(r'''magnet:\?[^\s"'<>]+''', caseSensitive: false).allMatches(text)) m[0]!,
      for (final l in extractLinks(text))
        if (RegExp(r'\.torrent$', caseSensitive: false).hasMatch(Uri.tryParse(l)?.path ?? '')) l,
    ].take(200).toList();

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

  // ---- torrents ----

  Future<DsTorrents> torrents() async => DsTorrents.fromJson(await _c.get('$base/torrents'));

  /// One of [source] (a magnet link or a link to a .torrent) or [file]
  /// (a .torrent's bytes). The sidecar root-checks [dir].
  Future<void> addTorrent({String? source, List<int>? file, String dir = '', String category = '', bool paused = false, bool sequential = false, bool firstLast = false}) =>
      _c.post('$base/torrents', body: {
        'source': ?source,
        if (file != null) 'torrent': base64Encode(file),
        if (dir.isNotEmpty) 'dir': dir,
        if (category.isNotEmpty) 'category': category,
        'paused': paused,
        'sequential': sequential,
        'first_last': firstLast,
      });

  Future<DsTorrentDetail> torrent(String hash) async => DsTorrentDetail.fromJson(await _c.get('$base/torrents/${Uri.encodeComponent(hash)}'));

  /// pause, resume or recheck.
  Future<void> torrentAction(String hash, String action) => _c.post('$base/torrents/${Uri.encodeComponent(hash)}/$action');

  Future<void> removeTorrent(String hash, {bool deleteFiles = false}) =>
      _c.delete('$base/torrents/${Uri.encodeComponent(hash)}', query: {'delete_files': '$deleteFiles'});

  Future<void> setTorrentFiles(String hash, List<int> ids, int priority) =>
      _c.put('$base/torrents/${Uri.encodeComponent(hash)}/files', body: {'ids': ids, 'priority': priority});

  Future<void> setTorrentOptions(String hash, {required bool sequential, required bool firstLast}) =>
      _c.put('$base/torrents/${Uri.encodeComponent(hash)}/options', body: {'sequential': sequential, 'first_last': firstLast});

  /// The torrent settings (TorrentSettings in torrent.go), as JSON.
  Future<Map<String, dynamic>> torrentSettings() async => _torrentPart(await _c.get('$base/settings'));

  /// Saves only the fields given; returns them all.
  Future<Map<String, dynamic>> saveTorrentSettings(Map<String, Object?> changes) async => _torrentPart(await _c.put('$base/settings', body: {'torrent': changes}));

  static Map<String, dynamic> _torrentPart(Map<String, dynamic> s) => (s['torrent'] as Map?)?.cast<String, dynamic>() ?? {};

  /// The public trackers list: {url, trackers, updated_at, error}.
  Future<Map<String, dynamic>> trackerList() => _c.get('$base/torrents/trackers');
  Future<Map<String, dynamic>> refreshTrackerList() => _c.post('$base/torrents/trackers/refresh', timeout: const Duration(seconds: 45));

  /// Where downloads may be saved: the folder picker's top level.
  Future<List<String>> roots() async {
    final res = await _c.get('$base/storage/roots');
    final r = res['roots'];
    return [for (final e in r is List ? r : const []) e.toString()];
  }

  Future<String> createFolder(String parent, String name) async =>
      (await _c.post('$base/storage/folders', body: {'parent': parent, 'name': name}))['path']?.toString() ?? '$parent/$name';
}
