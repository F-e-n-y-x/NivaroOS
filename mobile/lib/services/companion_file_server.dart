import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';

/// Why a path from the server was refused.
class PathRefused implements Exception {
  const PathRefused(this.reason);
  final String reason;
  @override
  String toString() => reason;
}

/// Which paths on this phone the server may touch: the shared storage
/// volumes only (`/storage/emulated/<user>` and SD cards,
/// `/storage/XXXX-XXXX`), never the app's private files, other apps' data
/// folders or the rest of the system (plan M-20). Paths are normalised and
/// symlinks resolved before the check, so `..` and links can't step out.
abstract final class SharedStoragePolicy {
  static final _root = RegExp(r'^/storage/(emulated/\d+|[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4})$');

  /// Other apps' private folders on shared storage. Case-insensitive:
  /// Android's shared storage and FAT/exFAT SD cards ignore case, so
  /// `android/DATA` is the same folder as `Android/data`.
  static final _private = RegExp(
    r'^/storage/(emulated/\d+|[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4})/Android/(data|obb)(/|$)',
    caseSensitive: false,
  );

  /// `/a/./b/../c` → `/a/c`; null for a relative path or one that climbs
  /// above `/`.
  static String? normalize(String path) {
    if (!path.startsWith('/')) return null;
    final out = <String>[];
    for (final part in path.split('/')) {
      if (part.isEmpty || part == '.') continue;
      if (part == '..') {
        if (out.isEmpty) return null;
        out.removeLast();
      } else {
        out.add(part);
      }
    }
    return '/${out.join('/')}';
  }

  /// The volume root [path] lies in, or null when it lies outside every
  /// shared volume.
  static String? rootOf(String path) {
    final parts = path.split('/');
    // /storage/emulated/0/... or /storage/ABCD-1234/...
    for (final n in [4, 3]) {
      if (parts.length >= n) {
        final candidate = parts.take(n).join('/');
        if (_root.hasMatch(candidate)) return candidate;
      }
    }
    return null;
  }

  /// Checks a normalised, symlink-free path.
  static bool allowsResolved(String path) => rootOf(path) != null && !_private.hasMatch(path);

  /// Resolves [path] for use: normalised, symlinks resolved (for a path
  /// that doesn't exist yet, its nearest existing parent's), then checked.
  /// Throws [PathRefused] when it is outside shared storage. With
  /// [allowRoot] false the volume root itself is refused too (deleting or
  /// renaming `/storage/emulated/0`).
  static String resolve(String path, {bool allowRoot = true}) {
    final normalized = normalize(path);
    if (normalized == null) throw const PathRefused('Only absolute paths inside shared storage are allowed');
    var resolved = normalized;
    try {
      if (FileSystemEntity.typeSync(normalized, followLinks: false) != FileSystemEntityType.notFound) {
        resolved = File(normalized).resolveSymbolicLinksSync();
      } else {
        // Resolve the nearest existing parent, then add the rest back.
        var parent = normalized;
        final rest = <String>[];
        while (parent != '/' && FileSystemEntity.typeSync(parent, followLinks: false) == FileSystemEntityType.notFound) {
          final i = parent.lastIndexOf('/');
          rest.insert(0, parent.substring(i + 1));
          parent = i == 0 ? '/' : parent.substring(0, i);
        }
        final base = parent == '/' ? '' : File(parent).resolveSymbolicLinksSync();
        resolved = normalize('$base/${rest.join('/')}') ?? normalized;
      }
    } on FileSystemException {
      throw const PathRefused('Path not accessible');
    }
    if (!allowsResolved(resolved)) throw const PathRefused('Path is outside the shared storage');
    if (!allowRoot && rootOf(resolved) == resolved) throw const PathRefused('The storage root itself cannot be changed');
    return resolved;
  }
}

/// The phone's file server for its server (plan M-20): runs only during a
/// storage-sharing session (CompanionShareService), serves shared storage
/// only ([SharedStoragePolicy]), requires the X-Companion-Secret the server
/// was given at registration (compared in constant time), listens on the
/// Wi-Fi address only, and keeps a reverse WebSocket tunnel to the server
/// for listing when the server can't reach the phone directly. Tokens never
/// go into a URL or a log line.
class CompanionFileServer {
  CompanionFileServer._();
  static final CompanionFileServer instance = CompanionFileServer._();

  HttpServer? _server;
  WebSocket? _ws;
  Timer? _reconnectTimer;
  bool _isRunning = false;
  int _port = 8765;
  String? _localIp;

  bool get isRunning => _isRunning;
  int get port => _port;
  String? get localIp => _localIp;
  String get baseUrl => _localIp != null ? 'http://$_localIp:$_port' : 'http://127.0.0.1:$_port';

  static const String defaultRootPath = '/storage/emulated/0';

  Future<void> start({int port = 8765}) async {
    if (_isRunning) return;
    _port = port;

    try {
      _localIp = await _detectLocalIp();
      // The Wi-Fi address only, not every interface (mobile data, VPNs).
      // With no Wi-Fi address the server can't reach the phone directly;
      // listing still works through the tunnel.
      final address = _localIp != null ? InternetAddress(_localIp!) : InternetAddress.loopbackIPv4;
      _server = await HttpServer.bind(address, _port);
      _isRunning = true;
      debugPrint('[CompanionFileServer] Listening on port $_port');

      _server!.listen(_handleRequest, onError: (e) {
        debugPrint('[CompanionFileServer] Server error: ${e.runtimeType}');
      });

      connectWebSocketTunnel();
    } catch (e) {
      debugPrint('[CompanionFileServer] Failed to start HTTP server: ${e.runtimeType}');
    }
  }

  Future<void> stop() async {
    _isRunning = false;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    await _ws?.close();
    _ws = null;
    await _server?.close(force: true);
    _server = null;
    debugPrint('[CompanionFileServer] Server stopped');
  }

  /// Restarts the listener when the phone's Wi-Fi address changed (a new
  /// network, a new lease) during a session.
  Future<void> refreshAddress() async {
    if (!_isRunning) return;
    final ip = await _detectLocalIp();
    if (ip == _localIp) return;
    await stop();
    await start(port: _port);
  }

  Future<String?> _detectLocalIp() async {
    try {
      final interfaces = await NetworkInterface.list(
        type: InternetAddressType.IPv4,
        includeLinkLocal: false,
      );
      for (final iface in interfaces) {
        final name = iface.name.toLowerCase();
        final isWifi = name.contains('wlan') || name.contains('wifi') || name.startsWith('en') || name.startsWith('eth');
        if (!isWifi) continue;
        for (final addr in iface.addresses) {
          if (!addr.isLoopback && !addr.address.startsWith('10.0.2.')) return addr.address;
        }
      }
    } catch (e) {
      debugPrint('[CompanionFileServer] Error detecting LAN IP: ${e.runtimeType}');
    }
    return null;
  }

  /// Constant-time string comparison, so response timing says nothing about
  /// how much of a guessed secret was right.
  @visibleForTesting
  static bool secretsMatch(String? provided, String expected) {
    if (provided == null) return false;
    final a = utf8.encode(provided);
    final b = utf8.encode(expected);
    var diff = a.length ^ b.length;
    for (var i = 0; i < b.length; i++) {
      diff |= (i < a.length ? a[i] : 0) ^ b[i];
    }
    return diff == 0;
  }

  // The NivaroOS server sends this back as X-Companion-Secret on every
  // /download, /upload, /delete, /files call once it's registered this
  // device (PostRegisterCompanionDevice generates it over the phone's own
  // authenticated session and hands it back). Without this check, this
  // HTTP server is an open LAN file server.
  Future<bool> _checkAuth(HttpRequest req) async {
    final expected = await StorageService.instance.getCompanionSecret();
    if (expected == null || expected.isEmpty) {
      // Not registered yet: fail closed.
      await _reply(req, HttpStatus.serviceUnavailable, {'success': false, 'message': 'Device not yet registered with server'});
      return false;
    }
    if (!secretsMatch(req.headers.value('x-companion-secret'), expected)) {
      await _reply(req, HttpStatus.unauthorized, {'success': false, 'message': 'Unauthorized'});
      return false;
    }
    return true;
  }

  Future<void> _reply(HttpRequest req, int status, Map<String, Object?> body) async {
    req.response.statusCode = status;
    req.response.headers.contentType = ContentType.json;
    req.response.write(jsonEncode(body));
    await req.response.close();
  }

  /// A path parameter, resolved inside shared storage; answers the request
  /// with 400/403 and returns null when it is missing or refused.
  Future<String?> _pathParam(HttpRequest req, String name, {bool allowRoot = true, String? fallback}) async {
    final raw = req.uri.queryParameters[name];
    final value = (raw == null || raw.isEmpty || raw == '/') ? fallback : raw;
    if (value == null) {
      await _reply(req, HttpStatus.badRequest, {'success': false, 'message': '$name required'});
      return null;
    }
    try {
      return SharedStoragePolicy.resolve(value, allowRoot: allowRoot);
    } on PathRefused catch (e) {
      await _reply(req, HttpStatus.forbidden, {'success': false, 'message': e.reason});
      return null;
    }
  }

  Future<void> _handleRequest(HttpRequest req) async {
    final path = req.uri.path;
    try {
      if (path == '/status' || path == '/') {
        // Unauthenticated liveness probe for the server
        // (probeCompanionLAN); says nothing about the phone.
        await _reply(req, HttpStatus.ok, {'success': true, 'shares_storage': true});
        return;
      }
      if (!await _checkAuth(req)) return;
      switch (path) {
        case '/files':
          await _handleListFiles(req);
        case '/download':
          await _handleDownload(req);
        case '/upload':
          await _handleUpload(req);
        case '/delete':
          await _handleDelete(req);
        case '/rename':
          await _handleRename(req);
        case '/mkdir':
          await _handleMkdir(req);
        default:
          await _reply(req, HttpStatus.notFound, {'success': false, 'message': 'Not found'});
      }
    } catch (e) {
      debugPrint('[CompanionFileServer] Request handler error: ${e.runtimeType}');
      try {
        await _reply(req, HttpStatus.internalServerError, {'success': false, 'message': 'The phone could not complete the request'});
      } catch (_) {}
    }
  }

  static List<Map<String, dynamic>> _listDir(Directory dir) {
    final List<Map<String, dynamic>> items = [];
    for (final entity in dir.listSync(followLinks: false)) {
      try {
        // Links are not followed, so a link can't show what lies behind it.
        if (entity is Link) continue;
        // Uploads in progress (see [uploadTempPath]).
        if (entity.path.split('/').last.startsWith('.nvupload-')) continue;
        final isDir = entity is Directory;
        final stat = entity.statSync();
        final name = entity.path.split('/').where((s) => s.isNotEmpty).lastOrNull ?? entity.path;
        items.add({
          'name': name,
          'path': entity.path,
          'is_dir': isDir,
          'size': isDir ? 0 : stat.size,
          'modified': stat.modified.toUtc().toIso8601String(),
        });
      } catch (_) {}
    }
    items.sort((a, b) {
      final aDir = a['is_dir'] as bool;
      final bDir = b['is_dir'] as bool;
      if (aDir && !bDir) return -1;
      if (!aDir && bDir) return 1;
      return (a['name'] as String).toLowerCase().compareTo((b['name'] as String).toLowerCase());
    });
    return items;
  }

  Future<void> _handleListFiles(HttpRequest req) async {
    final queryPath = await _pathParam(req, 'path', fallback: defaultRootPath);
    if (queryPath == null) return;
    final dir = Directory(queryPath);
    if (!dir.existsSync()) {
      await _reply(req, HttpStatus.notFound, {'success': false, 'message': 'Directory not found', 'files': []});
      return;
    }
    List<Map<String, dynamic>> items = [];
    try {
      items = _listDir(dir);
    } catch (e) {
      debugPrint('[CompanionFileServer] List dir error: ${e.runtimeType}');
    }
    await _reply(req, HttpStatus.ok, {'success': true, 'path': queryPath, 'files': items});
  }

  Future<void> _handleDownload(HttpRequest req) async {
    final filePath = await _pathParam(req, 'path');
    if (filePath == null) return;

    final file = File(filePath);
    if (!file.existsSync()) {
      await _reply(req, HttpStatus.notFound, {'success': false, 'message': 'File not found'});
      return;
    }

    final isDownload = req.uri.queryParameters['download'] == '1' || req.uri.queryParameters['download'] == 'true';
    final stat = file.statSync();
    final fileName = filePath.split('/').last.replaceAll('"', '');
    final totalSize = stat.size;
    final disposition = isDownload ? 'attachment; filename="$fileName"' : 'inline; filename="$fileName"';

    req.response.headers.set('Accept-Ranges', 'bytes');
    req.response.headers.set('Content-Disposition', disposition);
    req.response.headers.contentType = _getContentTypeForFile(fileName);

    // Support HTTP Range requests (crucial for video streaming & seekable audio/PDFs)
    final rangeHeader = req.headers.value('range');
    if (rangeHeader != null && rangeHeader.startsWith('bytes=')) {
      final rangeValue = rangeHeader.substring(6).trim();
      final parts = rangeValue.split('-');
      int start = 0;
      int end = totalSize - 1;

      if (parts[0].isNotEmpty) {
        start = int.tryParse(parts[0]) ?? 0;
        if (parts.length > 1 && parts[1].isNotEmpty) {
          end = int.tryParse(parts[1]) ?? (totalSize - 1);
        }
      } else if (parts.length > 1 && parts[1].isNotEmpty) {
        final suffix = int.tryParse(parts[1]) ?? 0;
        start = totalSize - suffix;
        if (start < 0) start = 0;
      }

      if (start < 0) start = 0;
      if (end >= totalSize) end = totalSize - 1;

      if (start <= end && start < totalSize) {
        final chunkSize = (end - start) + 1;
        req.response.statusCode = HttpStatus.partialContent;
        req.response.headers.set('Content-Range', 'bytes $start-$end/$totalSize');
        req.response.headers.set('Content-Length', chunkSize.toString());
        await file.openRead(start, end + 1).pipe(req.response);
        return;
      } else {
        req.response.statusCode = HttpStatus.requestedRangeNotSatisfiable;
        req.response.headers.set('Content-Range', 'bytes */$totalSize');
        await req.response.close();
        return;
      }
    }

    req.response.statusCode = HttpStatus.ok;
    req.response.headers.set('Content-Length', totalSize.toString());
    await file.openRead().pipe(req.response);
  }

  /// Writes the body to a hidden temporary file next to the target and
  /// renames it over the target only once every byte arrived (and the
  /// length matches Content-Length, when the server sent one). A dropped
  /// connection then leaves the original file as it was, not a truncated
  /// copy.
  Future<void> _handleUpload(HttpRequest req) async {
    final destPath = await _pathParam(req, 'path', allowRoot: false);
    if (destPath == null) return;
    if (FileSystemEntity.isDirectorySync(destPath)) {
      await _reply(req, HttpStatus.conflict, {'success': false, 'message': 'A folder has that name'});
      return;
    }
    final targetFile = File(destPath);
    await targetFile.parent.create(recursive: true);
    final temp = File(uploadTempPath(destPath));
    try {
      final sink = temp.openWrite();
      try {
        await sink.addStream(req);
      } finally {
        await sink.close();
      }
      final expected = req.contentLength;
      if (expected >= 0 && await temp.length() != expected) {
        await _deleteQuietly(temp);
        await _reply(req, HttpStatus.badRequest, {'success': false, 'message': 'The upload was incomplete'});
        return;
      }
      await temp.rename(destPath);
    } catch (_) {
      await _deleteQuietly(temp);
      rethrow;
    }
    await _reply(req, HttpStatus.ok, {'success': true, 'path': destPath});
  }

  /// The hidden temporary file an upload to [destPath] is written to: in
  /// the same folder (so the final rename stays on one volume) and short,
  /// so a 255-byte file name still fits.
  @visibleForTesting
  static String uploadTempPath(String destPath) {
    final dir = destPath.substring(0, destPath.lastIndexOf('/'));
    final stamp = DateTime.now().microsecondsSinceEpoch.toRadixString(36);
    return '$dir/.nvupload-$stamp';
  }

  static Future<void> _deleteQuietly(File f) async {
    try {
      if (await f.exists()) await f.delete();
    } catch (_) {}
  }

  Future<void> _handleDelete(HttpRequest req) async {
    final targetPath = await _pathParam(req, 'path', allowRoot: false);
    if (targetPath == null) return;
    if (FileSystemEntity.isDirectorySync(targetPath)) {
      await Directory(targetPath).delete(recursive: true);
    } else if (FileSystemEntity.isFileSync(targetPath)) {
      await File(targetPath).delete();
    }
    await _reply(req, HttpStatus.ok, {'success': true, 'path': targetPath});
  }

  Future<void> _handleRename(HttpRequest req) async {
    final oldPath = await _pathParam(req, 'old_path', allowRoot: false);
    if (oldPath == null) return;
    final newPath = await _pathParam(req, 'new_path', allowRoot: false);
    if (newPath == null) return;
    // File.rename and Directory.rename replace what is there; never let a
    // rename destroy another file.
    // (A change of letter case only is the same file on a case-insensitive
    // volume, so that is allowed.)
    if (newPath.toLowerCase() != oldPath.toLowerCase() && FileSystemEntity.typeSync(newPath, followLinks: false) != FileSystemEntityType.notFound) {
      await _reply(req, HttpStatus.conflict, {'success': false, 'message': 'Something with that name already exists'});
      return;
    }
    final file = File(oldPath);
    if (file.existsSync()) {
      await File(newPath).parent.create(recursive: true);
      await file.rename(newPath);
      await _reply(req, HttpStatus.ok, {'success': true, 'old_path': oldPath, 'new_path': newPath});
      return;
    }
    final dir = Directory(oldPath);
    if (dir.existsSync()) {
      await dir.rename(newPath);
      await _reply(req, HttpStatus.ok, {'success': true, 'old_path': oldPath, 'new_path': newPath});
      return;
    }
    await _reply(req, HttpStatus.notFound, {'success': false, 'message': 'Path not found'});
  }

  Future<void> _handleMkdir(HttpRequest req) async {
    final dirPath = await _pathParam(req, 'path', allowRoot: false);
    if (dirPath == null) return;
    await Directory(dirPath).create(recursive: true);
    await _reply(req, HttpStatus.ok, {'success': true, 'path': dirPath});
  }

  ContentType _getContentTypeForFile(String name) {
    final ext = name.contains('.') ? name.split('.').last.toLowerCase() : '';
    switch (ext) {
      // Images
      case 'jpg':
      case 'jpeg':
        return ContentType('image', 'jpeg');
      case 'png':
        return ContentType('image', 'png');
      case 'webp':
        return ContentType('image', 'webp');
      case 'gif':
        return ContentType('image', 'gif');
      case 'svg':
        return ContentType('image', 'svg+xml');
      case 'bmp':
        return ContentType('image', 'bmp');
      case 'ico':
        return ContentType('image', 'x-icon');
      case 'heic':
      case 'heif':
        return ContentType('image', 'heic');

      // Video
      case 'mp4':
        return ContentType('video', 'mp4');
      case 'mkv':
        return ContentType('video', 'x-matroska');
      case 'webm':
        return ContentType('video', 'webm');
      case 'mov':
        return ContentType('video', 'quicktime');
      case 'avi':
        return ContentType('video', 'x-msvideo');
      case '3gp':
        return ContentType('video', '3gpp');
      case 'ts':
        return ContentType('video', 'mp2t');

      // Audio
      case 'mp3':
        return ContentType('audio', 'mpeg');
      case 'wav':
        return ContentType('audio', 'wav');
      case 'ogg':
        return ContentType('audio', 'ogg');
      case 'm4a':
        return ContentType('audio', 'mp4');
      case 'aac':
        return ContentType('audio', 'aac');
      case 'flac':
        return ContentType('audio', 'flac');
      case 'opus':
        return ContentType('audio', 'opus');

      // Documents & Text
      case 'pdf':
        return ContentType('application', 'pdf');
      case 'json':
        return ContentType.json;
      case 'txt':
      case 'log':
      case 'md':
      case 'csv':
        return ContentType.text;
      case 'html':
      case 'htm':
        return ContentType.html;
      case 'xml':
        return ContentType('application', 'xml');
      case 'zip':
        return ContentType('application', 'zip');
      case 'tar':
        return ContentType('application', 'x-tar');
      case 'gz':
        return ContentType('application', 'gzip');
      case 'docx':
        return ContentType('application', 'vnd.openxmlformats-officedocument.wordprocessingml.document');
      case 'xlsx':
        return ContentType('application', 'vnd.openxmlformats-officedocument.spreadsheetml.sheet');
      case 'pptx':
        return ContentType('application', 'vnd.openxmlformats-officedocument.presentationml.presentation');
      case 'doc':
        return ContentType('application', 'msword');
      case 'xls':
        return ContentType('application', 'vnd.ms-excel');
      case 'ppt':
        return ContentType('application', 'vnd.ms-powerpoint');
      default:
        return ContentType.binary;
    }
  }

  /// Connects the reverse WebSocket tunnel to the server. The token goes
  /// in the Authorization header only - never in the URL, where proxies
  /// log it - and is refreshed first when it is about to expire.
  Future<void> connectWebSocketTunnel() async {
    _reconnectTimer?.cancel();
    if (!_isRunning) return;
    if (!ApiClient.instance.hasSession) {
      _scheduleReconnect();
      return;
    }
    if (_ws != null) return;

    try {
      if (ApiClient.instance.baseUrl.isEmpty) {
        _scheduleReconnect();
        return;
      }
      final devId = await StorageService.instance.getCompanionDeviceId();
      if (devId == null || devId.isEmpty) {
        _scheduleReconnect();
        return;
      }
      final headers = await ApiClient.instance.authHeaders();
      if (!headers.containsKey('Authorization')) {
        _scheduleReconnect();
        return;
      }
      final wsUri = ApiClient.instance.webSocketUri('/v1/companion/devices/$devId/ws');
      debugPrint('[CompanionFileServer] Connecting the tunnel to ${wsUri.host}');
      final ws = await WebSocket.connect(wsUri.toString(), headers: headers).timeout(const Duration(seconds: 10));
      ws.pingInterval = const Duration(seconds: 15);
      _ws = ws;

      ws.listen(_handleWebSocketMessage, onDone: () {
        debugPrint('[CompanionFileServer] Tunnel closed (${ws.closeCode ?? '-'})');
        _ws = null;
        _scheduleReconnect();
      }, onError: (e) {
        debugPrint('[CompanionFileServer] Tunnel error: ${e.runtimeType}');
        _ws = null;
        _scheduleReconnect();
      });

      ws.add(jsonEncode({
        'action': 'register',
        'port': _port,
        'ip': _localIp,
        'shares_storage': true,
        'root': defaultRootPath,
      }));
    } catch (e) {
      debugPrint('[CompanionFileServer] Tunnel connection failed: ${e.runtimeType}');
      _ws = null;
      _scheduleReconnect();
    }
  }

  void _scheduleReconnect() {
    _reconnectTimer?.cancel();
    if (!_isRunning) return;
    _reconnectTimer = Timer(const Duration(seconds: 15), () {
      if (_isRunning) connectWebSocketTunnel();
    });
  }

  void _handleWebSocketMessage(dynamic message) {
    if (message is! String) return;
    try {
      final data = jsonDecode(message) as Map<String, dynamic>;
      final reqId = data['id'];
      final action = data['action'];
      if (action == 'list') {
        _ws?.add(jsonEncode({'id': reqId, 'action': 'list_response', ...listForTunnel(data['path'] as String?)}));
      } else if (action == 'ping') {
        _ws?.add(jsonEncode({'action': 'pong', 'id': reqId}));
      }
    } catch (e) {
      debugPrint('[CompanionFileServer] Tunnel message error: ${e.runtimeType}');
    }
  }

  /// The answer to a tunnel `list` request for [path], under the same
  /// shared-storage rules as the HTTP server.
  @visibleForTesting
  static Map<String, dynamic> listForTunnel(String? path) {
    final requested = (path == null || path.isEmpty || path == '/') ? defaultRootPath : path;
    final String queryPath;
    try {
      queryPath = SharedStoragePolicy.resolve(requested);
    } on PathRefused catch (e) {
      return {'success': false, 'is_file': false, 'message': e.reason, 'files': []};
    }
    if (FileSystemEntity.isFileSync(queryPath)) {
      return {'success': false, 'is_file': true, 'message': 'Path is a file', 'files': []};
    }
    final dir = Directory(queryPath);
    if (!dir.existsSync()) {
      return {'success': false, 'is_file': false, 'message': 'Directory not found', 'files': []};
    }
    return {'success': true, 'path': queryPath, 'files': _listDir(dir)};
  }
}
