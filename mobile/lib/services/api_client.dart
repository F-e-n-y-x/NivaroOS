import 'dart:io';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'storage_service.dart';

class ApiException implements Exception {
  final String message;
  final int? statusCode;
  ApiException(this.message, {this.statusCode});
  @override
  String toString() => message;
}

/// Talks to the main NivaroOS gateway (the same host the browser dashboard
/// itself lives on). Auth uses a raw JWT in the Authorization header with
/// NO "Bearer " prefix - matching exactly what the web app itself sends
/// (see ui/src/service/service.js) - and every path implicitly gets a
/// "/v1" prefix unless it already starts with "/v2"+ or "http", the same
/// rule that file applies too.
class ApiClient {
  ApiClient._();
  static final ApiClient instance = ApiClient._();

  static final ValueNotifier<bool> sessionExpiredNotifier = ValueNotifier(false);

  String? _baseUrl;
  String? _accessToken;
  String? _refreshToken;
  Future<bool>? _refreshFuture;

  Future<void> init() async {
    _baseUrl = await StorageService.instance.getServerUrl();
    _accessToken = await StorageService.instance.getAccessToken();
    _refreshToken = await StorageService.instance.getRefreshToken();
  }

  void setBaseUrl(String url) {
    _baseUrl = url;
  }

  String get baseUrl => _baseUrl ?? '';
  String? get accessToken => _accessToken;
  bool get hasSession => _accessToken != null && _accessToken!.isNotEmpty;

  void setSession(String accessToken, String refreshToken) {
    _accessToken = accessToken;
    _refreshToken = refreshToken;
    sessionExpiredNotifier.value = false;
  }

  void clearSession() {
    _accessToken = null;
    _refreshToken = null;
    sessionExpiredNotifier.value = false;
  }

  static bool isAuthError(dynamic error) {
    if (error == null) return false;
    if (error is ApiException && error.statusCode == 401) return true;
    final str = error.toString().toLowerCase();
    return str.contains('unauthorized') ||
        str.contains('verification failure') ||
        str.contains('token is invalid') ||
        str.contains('token expired') ||
        str.contains('session expired');
  }

  Uri _uri(String path, [Map<String, dynamic>? query]) {
    if (path.startsWith('http://') || path.startsWith('https://')) {
      final uri = Uri.parse(path);
      if (query == null || query.isEmpty) return uri;
      return uri.replace(queryParameters: {
        ...uri.queryParameters,
        ...query.map((k, v) => MapEntry(k, '$v')),
      });
    }

    var base = (_baseUrl ?? '').trim();
    while (base.endsWith('/')) {
      base = base.substring(0, base.length - 1);
    }
    if (!base.startsWith('http://') && !base.startsWith('https://') && base.isNotEmpty) {
      base = 'http://$base';
    }

    var normalizedPath = path.trim();
    if (!normalizedPath.startsWith('/')) {
      normalizedPath = '/$normalizedPath';
    }

    final String fullPath;
    if (normalizedPath.startsWith('/v1/') ||
        normalizedPath == '/v1' ||
        normalizedPath.startsWith('/v2/') ||
        normalizedPath == '/v2' ||
        normalizedPath.startsWith('/speedtest') ||
        normalizedPath.startsWith('/ping') ||
        normalizedPath.startsWith('/files/')) {
      fullPath = normalizedPath;
    } else {
      fullPath = '/v1$normalizedPath';
    }

    final full = '$base$fullPath';
    final uri = Uri.parse(full);
    if (query == null || query.isEmpty) return uri;
    return uri.replace(queryParameters: {
      ...uri.queryParameters,
      ...query.map((k, v) => MapEntry(k, '$v')),
    });
  }

  Map<String, String> _headers({bool json = true}) => {
        if (json) 'Content-Type': 'application/json',
        if (_accessToken != null) 'Authorization': _accessToken!,
      };

  Future<Map<String, dynamic>> get(String path, {Map<String, dynamic>? query}) async {
    return _send(() => http.get(_uri(path, query), headers: _headers()));
  }

  Future<Map<String, dynamic>> post(String path, {Object? body}) async {
    return _send(() => http.post(_uri(path), headers: _headers(), body: jsonEncode(body ?? {})));
  }

  Future<Map<String, dynamic>> put(String path, {Object? body}) async {
    return _send(() => http.put(_uri(path), headers: _headers(), body: jsonEncode(body ?? {})));
  }

  Future<Map<String, dynamic>> delete(String path, {Map<String, dynamic>? query}) async {
    return _send(() => http.delete(_uri(path, query), headers: _headers()));
  }

  /// A handful of endpoints (bulk file delete being the one this app
  /// actually calls) expect DELETE with a JSON array/object body rather
  /// than query params.
  Future<Map<String, dynamic>> deleteWithBody(String path, Object body) async {
    return _send(() => http.delete(_uri(path), headers: _headers(), body: jsonEncode(body)));
  }

  /// Exposes the current bearer value for the one call site
  /// (files_screen.dart's upload, a MultipartRequest) that can't go
  /// through this client's own get/post/put/delete helpers.
  Future<String> currentAuthHeader() async => _accessToken ?? '';

  /// A GET request's raw bytes (file download) rather than a JSON envelope.

  /// Exposes the resolved URI for a given path and query
  Uri buildUri(String path, [Map<String, dynamic>? query]) => _uri(path, query);

  /// Streams download of a remote file chunk-by-chunk directly into [targetFile],
  /// reporting real-time byte count and download speed.
  Future<void> downloadFileStream(
    String remotePath,
    File targetFile, {
    void Function(int received, int total, double speedBps)? onProgress,
  }) async {
    final client = http.Client();
    try {
      final uri = _uri('/file', {'path': remotePath});
      final req = http.Request('GET', uri);
      if (_accessToken != null && _accessToken!.isNotEmpty) {
        req.headers['Authorization'] = _accessToken!;
      }
      final streamedRes = await client.send(req);
      if (streamedRes.statusCode != 200) {
        throw ApiException('Failed to download file (HTTP ${streamedRes.statusCode})', statusCode: streamedRes.statusCode);
      }
      final total = streamedRes.contentLength ?? 0;
      final sink = targetFile.openWrite();
      int received = 0;
      final stopwatch = Stopwatch()..start();
      int lastNotifyMs = 0;

      await for (final chunk in streamedRes.stream) {
        sink.add(chunk);
        received += chunk.length;
        final nowMs = stopwatch.elapsedMilliseconds;
        if (nowMs - lastNotifyMs >= 80 || (total > 0 && received == total)) {
          lastNotifyMs = nowMs;
          final speed = nowMs > 0 ? (received / (nowMs / 1000.0)) : 0.0;
          onProgress?.call(received, total, speed);
        }
      }
      await sink.flush();
      await sink.close();
    } finally {
      client.close();
    }
  }

  Future<http.Response> getRaw(String path, {Map<String, dynamic>? query}) {
    return http.get(_uri(path, query), headers: _headers(json: false));
  }

  /// A GET request that needs a specific `Accept` header to get a raw
  /// non-JSON body back (app-management's "give me the compose YAML, not
  /// the JSON description" convention: same route, different Accept).
  Future<http.Response> getWithAccept(String path, String accept, {Map<String, dynamic>? query}) {
    return http.get(_uri(path, query), headers: {..._headers(json: false), 'Accept': accept});
  }

  /// POST with a raw non-JSON body and an explicit content type - used for
  /// installing a compose app, which the backend reads as raw YAML text
  /// rather than a JSON-encoded object.
  Future<Map<String, dynamic>> postBody(String path, String body, String contentType, {Map<String, dynamic>? query}) async {
    return _send(() => http.post(_uri(path, query), headers: {..._headers(json: false), 'Content-Type': contentType}, body: body));
  }

  Future<Map<String, dynamic>> _send(Future<http.Response> Function() request, {bool isRetry = false}) async {
    http.Response res;
    try {
      res = await request();
    } catch (e) {
      throw ApiException('Could not reach the server. Check your connection and the server address.');
    }

    if (res.statusCode == 401 && !isRetry) {
      final refreshed = await _tryRefresh();
      if (refreshed) return _send(request, isRetry: true);
    }

    Map<String, dynamic> decoded;
    try {
      decoded = res.body.isEmpty ? {} : jsonDecode(res.body) as Map<String, dynamic>;
    } catch (_) {
      if (res.statusCode == 401) {
        _handleAuthFailure();
        throw ApiException('Unauthorized - please sign in again.', statusCode: 401);
      }
      throw ApiException('Unexpected response from server (HTTP ${res.statusCode}).', statusCode: res.statusCode);
    }

    if (res.statusCode >= 200 && res.statusCode < 300) {
      return decoded;
    }

    if (res.statusCode == 401) {
      _handleAuthFailure();
      throw ApiException('Unauthorized - please sign in again.', statusCode: 401);
    }

    final message = decoded['message']?.toString() ?? 'Request failed (HTTP ${res.statusCode}).';
    throw ApiException(message, statusCode: res.statusCode);
  }

  Future<bool> _tryRefresh() async {
    if (_refreshToken == null || _refreshToken!.isEmpty) {
      _handleAuthFailure();
      return false;
    }

    if (_refreshFuture != null) {
      return _refreshFuture!;
    }

    _refreshFuture = _executeRefresh();
    try {
      return await _refreshFuture!;
    } finally {
      _refreshFuture = null;
    }
  }

  Future<bool> _executeRefresh() async {
    try {
      // Send refresh request without expired Authorization header
      final res = await http.post(
        _uri('/users/refresh'),
        headers: {'Content-Type': 'application/json'},
        body: jsonEncode({'refresh_token': _refreshToken}),
      );
      if (res.statusCode != 200) {
        _handleAuthFailure();
        return false;
      }
      final decoded = jsonDecode(res.body) as Map<String, dynamic>;
      final data = decoded['data'] as Map<String, dynamic>?;
      if (data == null || data['access_token'] == null) {
        _handleAuthFailure();
        return false;
      }
      _accessToken = data['access_token'] as String;
      if (data['refresh_token'] != null) {
        _refreshToken = data['refresh_token'] as String;
      }
      sessionExpiredNotifier.value = false;
      final username = await StorageService.instance.getUsername() ?? '';
      await StorageService.instance.setSession(
        accessToken: _accessToken!,
        refreshToken: _refreshToken!,
        username: username,
      );
      return true;
    } catch (_) {
      return false;
    }
  }

  void _handleAuthFailure() {
    _accessToken = null;
    sessionExpiredNotifier.value = true;
    StorageService.instance.clearSession();
  }
}
