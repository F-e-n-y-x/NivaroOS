import 'dart:convert';
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

  String? _baseUrl;
  String? _accessToken;
  String? _refreshToken;

  Future<void> init() async {
    _baseUrl = await StorageService.instance.getServerUrl();
    _accessToken = await StorageService.instance.getAccessToken();
    _refreshToken = await StorageService.instance.getRefreshToken();
  }

  String get baseUrl => _baseUrl ?? '';
  bool get hasSession => _accessToken != null && _accessToken!.isNotEmpty;

  void setSession(String accessToken, String refreshToken) {
    _accessToken = accessToken;
    _refreshToken = refreshToken;
  }

  void clearSession() {
    _accessToken = null;
    _refreshToken = null;
  }

  Uri _uri(String path, [Map<String, dynamic>? query]) {
    final resolved = path.startsWith('http') || path.startsWith('/v2')
        ? path
        : '/v1$path';
    final full = '$_baseUrl$resolved';
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

    if (res.statusCode == 401 && !isRetry && _refreshToken != null) {
      final refreshed = await _tryRefresh();
      if (refreshed) return _send(request, isRetry: true);
    }

    Map<String, dynamic> decoded;
    try {
      decoded = res.body.isEmpty ? {} : jsonDecode(res.body) as Map<String, dynamic>;
    } catch (_) {
      throw ApiException('Unexpected response from server (HTTP ${res.statusCode}).', statusCode: res.statusCode);
    }

    if (res.statusCode >= 200 && res.statusCode < 300) {
      return decoded;
    }
    final message = decoded['message']?.toString() ?? 'Request failed (HTTP ${res.statusCode}).';
    throw ApiException(message, statusCode: res.statusCode);
  }

  Future<bool> _tryRefresh() async {
    if (_refreshToken == null) return false;
    try {
      final res = await http.post(
        _uri('/users/refresh'),
        headers: _headers(),
        body: jsonEncode({'refresh_token': _refreshToken}),
      );
      if (res.statusCode != 200) return false;
      final decoded = jsonDecode(res.body) as Map<String, dynamic>;
      final data = decoded['data'] as Map<String, dynamic>?;
      if (data == null || data['access_token'] == null) return false;
      _accessToken = data['access_token'] as String;
      _refreshToken = data['refresh_token'] as String;
      await StorageService.instance.setSession(
        accessToken: _accessToken!,
        refreshToken: _refreshToken!,
        username: await StorageService.instance.getUsername() ?? '',
      );
      return true;
    } catch (_) {
      return false;
    }
  }
}
