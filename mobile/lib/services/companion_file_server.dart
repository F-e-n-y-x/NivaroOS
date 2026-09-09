import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';

/// Embedded HTTP and WebSocket server running inside the Android/iOS companion app.
/// Exposes whole phone storage (/storage/emulated/0) to the NivaroOS server,
/// WebUI Files app, and other companion devices.
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
      _server = await HttpServer.bind(InternetAddress.anyIPv4, _port);
      _isRunning = true;
      debugPrint('[CompanionFileServer] HTTP server running on port $_port (LAN IP: $_localIp)');

      _server!.listen(_handleRequest, onError: (e) {
        debugPrint('[CompanionFileServer] Server error: $e');
      });

      // Connect reverse WebSocket tunnel to NivaroOS server
      connectWebSocketTunnel();
    } catch (e) {
      debugPrint('[CompanionFileServer] Failed to start HTTP server: $e');
    }
  }

  Future<void> stop() async {
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    await _ws?.close();
    _ws = null;
    await _server?.close(force: true);
    _server = null;
    _isRunning = false;
    debugPrint('[CompanionFileServer] Server stopped');
  }

  Future<String?> _detectLocalIp() async {
    try {
      final interfaces = await NetworkInterface.list(
        type: InternetAddressType.IPv4,
        includeLinkLocal: false,
      );
      for (final iface in interfaces) {
        final isWifi = iface.name.toLowerCase().contains('wlan') ||
            iface.name.toLowerCase().contains('wifi') ||
            iface.name.toLowerCase().contains('en');
        for (final addr in iface.addresses) {
          if (!addr.isLoopback && !addr.address.startsWith('172.') && !addr.address.startsWith('10.0.2.')) {
            if (isWifi) return addr.address;
          }
        }
      }
      for (final iface in interfaces) {
        for (final addr in iface.addresses) {
          if (!addr.isLoopback) return addr.address;
        }
      }
    } catch (e) {
      debugPrint('[CompanionFileServer] Error detecting LAN IP: $e');
    }
    return null;
  }

  void _addCorsHeaders(HttpResponse res) {
    res.headers.set('Access-Control-Allow-Origin', '*');
    res.headers.set('Access-Control-Allow-Methods', 'GET, POST, PUT, DELETE, OPTIONS');
    res.headers.set('Access-Control-Allow-Headers', '*');
  }

  Future<void> _handleRequest(HttpRequest req) async {
    _addCorsHeaders(req.response);
    if (req.method == 'OPTIONS') {
      req.response.statusCode = HttpStatus.ok;
      await req.response.close();
      return;
    }

    final path = req.uri.path;
    try {
      if (path == '/status' || path == '/') {
        await _handleStatus(req);
      } else if (path == '/files') {
        await _handleListFiles(req);
      } else if (path == '/download') {
        await _handleDownload(req);
      } else if (path == '/upload') {
        await _handleUpload(req);
      } else if (path == '/delete') {
        await _handleDelete(req);
      } else {
        req.response.statusCode = HttpStatus.notFound;
        req.response.write(jsonEncode({'success': false, 'message': 'Not found'}));
        await req.response.close();
      }
    } catch (e) {
      debugPrint('[CompanionFileServer] Request handler error: $e');
      try {
        req.response.statusCode = HttpStatus.internalServerError;
        req.response.write(jsonEncode({'success': false, 'error': e.toString()}));
        await req.response.close();
      } catch (_) {}
    }
  }

  Future<void> _handleStatus(HttpRequest req) async {
    final customName = await StorageService.instance.getCompanionDeviceName();
    req.response.headers.contentType = ContentType.json;
    req.response.write(jsonEncode({
      'success': true,
      'name': customName ?? 'Companion Device',
      'shares_storage': true,
      'root': defaultRootPath,
      'port': _port,
      'ip': _localIp,
    }));
    await req.response.close();
  }

  Future<void> _handleListFiles(HttpRequest req) async {
    String queryPath = req.uri.queryParameters['path'] ?? defaultRootPath;
    if (queryPath.isEmpty || queryPath == '/') {
      queryPath = defaultRootPath;
    }

    final dir = Directory(queryPath);
    if (!dir.existsSync()) {
      req.response.statusCode = HttpStatus.notFound;
      req.response.headers.contentType = ContentType.json;
      req.response.write(jsonEncode({
        'success': false,
        'message': 'Directory not found: $queryPath',
        'files': [],
      }));
      await req.response.close();
      return;
    }

    final List<Map<String, dynamic>> items = [];
    try {
      final entities = dir.listSync(followLinks: false);
      for (final entity in entities) {
        try {
          final isDir = entity is Directory;
          final stat = entity.statSync();
          final name = entity.path.split(Platform.pathSeparator).where((s) => s.isNotEmpty).lastOrNull ?? entity.path;
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
    } catch (e) {
      debugPrint('[CompanionFileServer] List dir error: $e');
    }

    req.response.headers.contentType = ContentType.json;
    req.response.write(jsonEncode({
      'success': true,
      'path': queryPath,
      'files': items,
    }));
    await req.response.close();
  }

  Future<void> _handleDownload(HttpRequest req) async {
    final filePath = req.uri.queryParameters['path'];
    if (filePath == null || filePath.isEmpty) {
      req.response.statusCode = HttpStatus.badRequest;
      req.response.write(jsonEncode({'success': false, 'message': 'path required'}));
      await req.response.close();
      return;
    }

    final file = File(filePath);
    if (!file.existsSync()) {
      req.response.statusCode = HttpStatus.notFound;
      req.response.write(jsonEncode({'success': false, 'message': 'File not found'}));
      await req.response.close();
      return;
    }

    final isDownload = req.uri.queryParameters['download'] == '1' || req.uri.queryParameters['download'] == 'true';
    final stat = file.statSync();
    final fileName = filePath.split(Platform.pathSeparator).last;
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

  Future<void> _handleUpload(HttpRequest req) async {
    final destPath = req.uri.queryParameters['path'];
    if (destPath == null || destPath.isEmpty) {
      req.response.statusCode = HttpStatus.badRequest;
      req.response.write(jsonEncode({'success': false, 'message': 'destination path required'}));
      await req.response.close();
      return;
    }

    final targetFile = File(destPath);
    await targetFile.parent.create(recursive: true);
    final sink = targetFile.openWrite();
    await sink.addStream(req);
    await sink.close();

    req.response.headers.contentType = ContentType.json;
    req.response.write(jsonEncode({'success': true, 'path': destPath}));
    await req.response.close();
  }

  Future<void> _handleDelete(HttpRequest req) async {
    final targetPath = req.uri.queryParameters['path'];
    if (targetPath == null || targetPath.isEmpty) {
      req.response.statusCode = HttpStatus.badRequest;
      req.response.write(jsonEncode({'success': false, 'message': 'path required'}));
      await req.response.close();
      return;
    }

    if (FileSystemEntity.isDirectorySync(targetPath)) {
      await Directory(targetPath).delete(recursive: true);
    } else if (FileSystemEntity.isFileSync(targetPath)) {
      await File(targetPath).delete();
    }

    req.response.headers.contentType = ContentType.json;
    req.response.write(jsonEncode({'success': true, 'path': targetPath}));
    await req.response.close();
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

  /// Connects reverse WebSocket tunnel to NivaroOS server
  Future<void> connectWebSocketTunnel() async {
    _reconnectTimer?.cancel();
    if (!ApiClient.instance.hasSession) {
      _scheduleReconnect();
      return;
    }

    try {
      final base = ApiClient.instance.baseUrl;
      if (base.isEmpty) {
        _scheduleReconnect();
        return;
      }
      final uri = Uri.parse(base);
      final wsScheme = uri.scheme == 'https' ? 'wss' : 'ws';
      final devId = await StorageService.instance.getCompanionDeviceId();
      if (devId == null || devId.isEmpty) {
        _scheduleReconnect();
        return;
      }

      final token = ApiClient.instance.accessToken ?? (await StorageService.instance.getAccessToken());
      if (token == null || token.isEmpty) {
        _scheduleReconnect();
        return;
      }

      final wsUri = uri.replace(
        scheme: wsScheme,
        path: '/v1/companion/devices/$devId/ws',
        queryParameters: {
          'token': token,
        },
      );

      debugPrint('[CompanionFileServer] Connecting WebSocket tunnel to $wsUri');
      _ws = await WebSocket.connect(
        wsUri.toString(),
        headers: {
          'Authorization': token,
        },
      ).timeout(const Duration(seconds: 10));
      _ws!.pingInterval = const Duration(seconds: 15);

      _ws!.listen((message) {
        _handleWebSocketMessage(message);
      }, onDone: () {
        debugPrint('[CompanionFileServer] WebSocket tunnel closed');
        _ws = null;
        _scheduleReconnect();
      }, onError: (e) {
        debugPrint('[CompanionFileServer] WebSocket tunnel error: $e');
        _ws = null;
        _scheduleReconnect();
      });

      _ws!.add(jsonEncode({
        'action': 'register',
        'port': _port,
        'ip': _localIp,
        'shares_storage': true,
        'root': defaultRootPath,
      }));
    } catch (e) {
      debugPrint('[CompanionFileServer] WebSocket tunnel connection failed: $e');
      _scheduleReconnect();
    }
  }

  void _scheduleReconnect() {
    _reconnectTimer?.cancel();
    _reconnectTimer = Timer(const Duration(seconds: 15), () {
      if (_isRunning) {
        connectWebSocketTunnel();
      }
    });
  }

  void _handleWebSocketMessage(dynamic message) {
    if (message is! String) return;
    try {
      final data = jsonDecode(message) as Map<String, dynamic>;
      final reqId = data['id'];
      final action = data['action'];

      if (action == 'list') {
        final queryPath = data['path'] as String? ?? defaultRootPath;
        final dir = Directory(queryPath);
        final List<Map<String, dynamic>> items = [];
        if (dir.existsSync()) {
          final entities = dir.listSync(followLinks: false);
          for (final entity in entities) {
            try {
              final isDir = entity is Directory;
              final stat = entity.statSync();
              final name = entity.path.split(Platform.pathSeparator).where((s) => s.isNotEmpty).lastOrNull ?? entity.path;
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
        }

        _ws?.add(jsonEncode({
          'id': reqId,
          'action': 'list_response',
          'success': true,
          'path': queryPath,
          'files': items,
        }));
      } else if (action == 'ping') {
        _ws?.add(jsonEncode({'action': 'pong', 'id': reqId}));
      }
    } catch (e) {
      debugPrint('[CompanionFileServer] WS message error: $e');
    }
  }
}
