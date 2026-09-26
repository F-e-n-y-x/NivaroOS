import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import 'storage_service.dart';

/// What kind of failure an [ApiException] is, so screens can pick the
/// right state (offline, sign in again, certificate problem) without
/// parsing messages.
enum ApiErrorKind {
  /// The server could not be reached: no route, refused, DNS failure.
  network,

  /// The server did not answer in time.
  timeout,

  /// TLS failed: the server's certificate is not trusted (self-signed,
  /// expired, wrong name). Plan M-24.
  certificate,

  /// The session ended (a real 401 from /users/refresh), or the request
  /// was refused as unauthenticated.
  auth,

  /// Too many attempts (HTTP 429).
  rateLimited,

  /// Something answered, but not NivaroOS: a web page (a proxy, a captive
  /// portal, another app on that port) instead of the JSON API.
  notNivaro,

  /// The server answered with an error.
  server,
}

class ApiException implements Exception {
  ApiException(this.message, {this.statusCode, ApiErrorKind? kind, this.details, this.data})
      : kind = kind ?? (statusCode == 401 ? ApiErrorKind.auth : ApiErrorKind.server);

  /// A plain sentence for the user.
  final String message;
  final int? statusCode;
  final ApiErrorKind kind;

  /// Technical detail for "Copy details" (route, status, error text). Never
  /// contains tokens.
  final String? details;

  /// The `data` of the server's error answer, when it sent one (a compose
  /// app's validation errors: the ports already in use).
  final Object? data;

  /// True for the failures that mean "can't reach the server right now" -
  /// the offline state, not an error in the request itself.
  bool get isUnreachable => kind == ApiErrorKind.network || kind == ApiErrorKind.timeout;

  @override
  String toString() => message;
}

/// How a token refresh ended.
enum RefreshResult {
  /// New tokens are in place.
  refreshed,

  /// The server said the refresh token is no longer valid (401): the
  /// session is over and the user has to sign in again.
  rejected,

  /// The refresh could not be done right now (the server or a proxy was
  /// down, a 5xx, a captive portal, no network). The tokens are kept.
  unavailable,
}

/// A server address that answered as NivaroOS (see [ApiClient.probe]).
class ServerProbe {
  const ServerProbe({required this.url, required this.initialized});

  /// The normalised base URL that worked ("http://192.168.1.20").
  final String url;

  /// False while the server has no account yet (first-run setup happens in
  /// the web UI).
  final bool initialized;
}

/// Talks to the main NivaroOS gateway (the same host the browser dashboard
/// itself lives on). Auth uses a raw JWT in the Authorization header with
/// NO "Bearer " prefix - matching exactly what the web app itself sends
/// (see ui/src/service/service.js) - and every path implicitly gets a
/// "/v1" prefix unless it already starts with "/v2"+ or "http", the same
/// rule that file applies too.
///
/// Session rules (plan M-15, M-16):
/// - A 401 on a request triggers one refresh and one retry.
/// - Only a 401 from `/users/refresh` ends the session. A 502 from the
///   gateway while user-service restarts, a Cloudflare 5xx, a captive
///   portal or no network keep the tokens and report "can't reach".
/// - WebSockets, streamed and multipart requests get their token from
///   [freshToken] / [authHeaders] (refreshed when less than 5 minutes are
///   left) or go through [send], which does the same 401 dance.
class ApiClient {
  ApiClient._();
  static final ApiClient instance = ApiClient._();

  /// Set to true when the session ended (a real 401 on refresh). The shell
  /// listens and asks the user to sign in again.
  static final ValueNotifier<bool> sessionExpiredNotifier = ValueNotifier(false);

  /// How long a plain JSON request may take before it counts as
  /// unreachable. Long server jobs (installs, updates) answer at once and
  /// are polled, so this only catches dead connections.
  static const requestTimeout = Duration(seconds: 60);

  /// A token with less than this left is refreshed before use by
  /// [freshToken].
  static const refreshMargin = Duration(minutes: 5);

  String? _baseUrl;
  String? _accessToken;
  String? _refreshToken;
  Future<RefreshResult>? _refreshFuture;

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
    if (error is ApiException) return error.kind == ApiErrorKind.auth || error.statusCode == 401;
    final str = error.toString().toLowerCase();
    return str.contains('unauthorized') ||
        str.contains('verification failure') ||
        str.contains('token is invalid') ||
        str.contains('token expired') ||
        str.contains('session expired');
  }

  // ---------------------------------------------------------------------
  // Addresses
  // ---------------------------------------------------------------------

  /// Cleans up what someone typed as a server address: trims it, drops
  /// trailing slashes and adds a scheme when there is none (see
  /// [candidateUrls] for which one). Returns an empty string for nothing.
  static String normalizeServerUrl(String input) => candidateUrls(input).firstOrNull ?? '';

  /// The base URLs to try, in order, for what someone typed.
  ///
  /// With a scheme, just that. Without one: plain http for addresses that
  /// are almost always on the home network (an IP address, a `.local` or
  /// single-word name, or an explicit port), and for any other name https
  /// first - a reverse proxy or tunnel - then http.
  static List<String> candidateUrls(String input) {
    var s = input.trim();
    if (s.isEmpty) return const [];
    while (s.endsWith('/')) {
      s = s.substring(0, s.length - 1);
    }
    final lower = s.toLowerCase();
    if (lower.startsWith('http://') || lower.startsWith('https://')) {
      final i = s.indexOf('://');
      return ['${lower.substring(0, i)}${s.substring(i)}'];
    }
    final uri = Uri.tryParse('http://$s');
    final host = uri?.host ?? s;
    final hasPort = uri != null && uri.hasPort;
    final isIp = InternetAddress.tryParse(host.replaceAll('[', '').replaceAll(']', '')) != null;
    final isLocalName = host.endsWith('.local') || host.endsWith('.lan') || host.endsWith('.home.arpa') || !host.contains('.');
    if (isIp || isLocalName || hasPort) return ['http://$s'];
    return ['https://$s', 'http://$s'];
  }

  /// The host (and port, when not the default) of a base URL, for display:
  /// "nas.local", "192.168.1.20:8080".
  static String displayHost(String url) {
    final uri = Uri.tryParse(url.contains('://') ? url : 'http://$url');
    if (uri == null || uri.host.isEmpty) return url;
    return uri.hasPort ? '${uri.host}:${uri.port}' : uri.host;
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

  /// Exposes the resolved URI for a given path and query
  Uri buildUri(String path, [Map<String, dynamic>? query]) => _uri(path, query);

  /// The WebSocket URI for [path] on the current server: `wss` when the
  /// server is https, `ws` otherwise. No token in it - send the
  /// Authorization header from [authHeaders] with the handshake, so
  /// tokens stay out of proxy access logs.
  Uri webSocketUri(String path, [Map<String, dynamic>? query]) {
    final uri = _uri(path, query);
    return uri.replace(scheme: uri.scheme == 'https' ? 'wss' : 'ws');
  }

  // ---------------------------------------------------------------------
  // Tokens
  // ---------------------------------------------------------------------

  /// When [jwt] expires, from its `exp` claim; null when it has none or
  /// can't be read.
  static DateTime? tokenExpiry(String? jwt) {
    if (jwt == null) return null;
    final parts = jwt.split('.');
    if (parts.length != 3) return null;
    try {
      final payload = jsonDecode(utf8.decode(base64Url.decode(base64Url.normalize(parts[1]))));
      final exp = payload is Map ? payload['exp'] : null;
      if (exp is num) return DateTime.fromMillisecondsSinceEpoch(exp.toInt() * 1000, isUtc: true);
    } catch (_) {}
    return null;
  }

  /// The access token, refreshed first when it expires within
  /// [refreshMargin]. For WebSockets and other clients that can't retry on
  /// a 401. Null without a session. When the refresh can't be done right
  /// now the current token is returned as it is (it may still work).
  Future<String?> freshToken() async {
    final token = _accessToken;
    if (token == null || token.isEmpty) return null;
    final exp = tokenExpiry(token);
    if (exp != null && exp.difference(clock.now().toUtc()) < refreshMargin) {
      await refresh();
    }
    return _accessToken;
  }

  /// The Authorization header (from [freshToken]) and, with [json], the
  /// JSON content type - for requests this client can't send itself.
  Future<Map<String, String>> authHeaders({bool json = false}) async {
    final token = await freshToken();
    return {
      if (json) 'Content-Type': 'application/json',
      'Authorization': ?token,
    };
  }

  /// Exposes the current bearer value for callers that build their own
  /// request. Refreshed first when it is about to expire.
  Future<String> currentAuthHeader() async => await freshToken() ?? '';

  Map<String, String> _headers({bool json = true}) => {
        if (json) 'Content-Type': 'application/json',
        'Authorization': ?_accessToken,
      };

  // ---------------------------------------------------------------------
  // Requests
  // ---------------------------------------------------------------------

  Future<Map<String, dynamic>> get(String path, {Map<String, dynamic>? query}) async {
    final uri = _uri(path, query);
    return _send('GET', uri, () => http.get(uri, headers: _headers()));
  }

  /// [timeout] replaces [requestTimeout] for a request the server takes
  /// long over (freeing memory while swap is emptied).
  Future<Map<String, dynamic>> post(String path, {Object? body, Map<String, String>? headers, Duration? timeout}) async {
    final uri = _uri(path);
    return _send('POST', uri, () => http.post(uri, headers: {..._headers(), ...?headers}, body: jsonEncode(body ?? {})), timeout: timeout);
  }

  Future<Map<String, dynamic>> put(String path, {Object? body}) async {
    final uri = _uri(path);
    return _send('PUT', uri, () => http.put(uri, headers: _headers(), body: jsonEncode(body ?? {})));
  }

  Future<Map<String, dynamic>> delete(String path, {Map<String, dynamic>? query}) async {
    final uri = _uri(path, query);
    return _send('DELETE', uri, () => http.delete(uri, headers: _headers()));
  }

  /// A handful of endpoints (bulk file delete being the one this app
  /// actually calls) expect DELETE with a JSON array/object body rather
  /// than query params.
  Future<Map<String, dynamic>> deleteWithBody(String path, Object body) async {
    final uri = _uri(path);
    return _send('DELETE', uri, () => http.delete(uri, headers: _headers(), body: jsonEncode(body)));
  }

  /// POST with a raw non-JSON body and an explicit content type - used for
  /// installing a compose app, which the backend reads as raw YAML text
  /// rather than a JSON-encoded object.
  Future<Map<String, dynamic>> postBody(String path, String body, String contentType, {Map<String, dynamic>? query}) async {
    final uri = _uri(path, query);
    return _send('POST', uri, () => http.post(uri, headers: {..._headers(json: false), 'Content-Type': contentType}, body: body));
  }

  /// PUT with a raw non-JSON body - saving an installed compose app's
  /// settings, which the backend reads as raw YAML (Edit app).
  Future<Map<String, dynamic>> putBody(String path, String body, String contentType, {Map<String, dynamic>? query}) async {
    final uri = _uri(path, query);
    return _send('PUT', uri, () => http.put(uri, headers: {..._headers(json: false), 'Content-Type': contentType}, body: body));
  }

  Future<http.Response> getRaw(String path, {Map<String, dynamic>? query}) async {
    final uri = _uri(path, query);
    return _sendRaw('GET', uri, () => http.get(uri, headers: _headers(json: false)));
  }

  /// A GET request that needs a specific `Accept` header to get a raw
  /// non-JSON body back (app-management's "give me the compose YAML, not
  /// the JSON description" convention: same route, different Accept).
  Future<http.Response> getWithAccept(String path, String accept, {Map<String, dynamic>? query}) async {
    final uri = _uri(path, query);
    return _sendRaw('GET', uri, () => http.get(uri, headers: {..._headers(json: false), 'Accept': accept}));
  }

  /// Sends a request this client can't express with the helpers above - a
  /// streamed download, a multipart or chunked upload - with the session's
  /// Authorization header, one refresh and one retry on a 401. [build] is
  /// called again for the retry (a request can only be sent once), so it
  /// must make a fresh request each time. The caller reads and closes the
  /// response stream.
  Future<http.StreamedResponse> send(http.BaseRequest Function() build, {http.Client? client}) async {
    final c = client ?? http.Client();
    Future<http.StreamedResponse> once() async {
      final req = build();
      final token = await freshToken();
      if (token != null) req.headers['Authorization'] = token;
      return c.send(req);
    }

    try {
      var res = await once();
      if (res.statusCode == 401) {
        await res.stream.drain<void>();
        final result = await refresh();
        if (result != RefreshResult.refreshed) throw _refreshFailure(result);
        res = await once();
      }
      if (client != null) return res;
      // Our own client: close it once the caller has read the body.
      return http.StreamedResponse(
        res.stream.transform(StreamTransformer.fromHandlers(handleDone: (sink) {
          c.close();
          sink.close();
        })),
        res.statusCode,
        contentLength: res.contentLength,
        request: res.request,
        headers: res.headers,
        isRedirect: res.isRedirect,
        persistentConnection: res.persistentConnection,
        reasonPhrase: res.reasonPhrase,
      );
    } on ApiException {
      if (client == null) c.close();
      rethrow;
    } catch (e) {
      if (client == null) c.close();
      throw describeError(e, Uri.tryParse(baseUrl));
    }
  }

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
      final streamedRes = await send(() => http.Request('GET', uri), client: client);
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

  /// Sends [request], refreshing and retrying once on a 401, and returns
  /// the raw response (any status) - for the non-JSON helpers.
  Future<http.Response> _sendRaw(String method, Uri uri, Future<http.Response> Function() request) async {
    var res = await _attempt(method, uri, request);
    if (res.statusCode == 401) {
      final result = await refresh();
      if (result != RefreshResult.refreshed) throw _refreshFailure(result);
      res = await _attempt(method, uri, request);
    }
    return res;
  }

  Future<http.Response> _attempt(String method, Uri uri, Future<http.Response> Function() request, {Duration? timeout}) async {
    try {
      return await request().timeout(timeout ?? requestTimeout);
    } catch (e) {
      throw describeError(e, uri, method: method);
    }
  }

  Future<Map<String, dynamic>> _send(String method, Uri uri, Future<http.Response> Function() request, {Duration? timeout}) async {
    var res = await _attempt(method, uri, request, timeout: timeout);

    if (res.statusCode == 401) {
      final result = await refresh();
      if (result != RefreshResult.refreshed) throw _refreshFailure(result);
      res = await _attempt(method, uri, request, timeout: timeout);
      // Still 401 with a token the server just issued: this route refuses
      // the request for its own reasons. Report it, but the session stays -
      // only /users/refresh decides that.
    }

    final route = '$method ${uri.path}';
    Map<String, dynamic> decoded;
    try {
      final body = res.body;
      final parsed = body.isEmpty ? <String, dynamic>{} : jsonDecode(body);
      decoded = parsed is Map<String, dynamic> ? parsed : {'data': parsed};
    } catch (_) {
      if (res.statusCode == 401) {
        throw ApiException('The server refused the request. Sign in again if this keeps happening.',
            statusCode: 401, details: '$route → HTTP 401');
      }
      if (_looksLikeWebPage(res)) {
        throw ApiException(
          "The server answered with a web page instead of NivaroOS data. If it's behind a proxy or a Wi-Fi sign-in page, check that.",
          statusCode: res.statusCode,
          kind: res.statusCode >= 500 ? ApiErrorKind.network : ApiErrorKind.notNivaro,
          details: '$route → HTTP ${res.statusCode} (text/html)',
        );
      }
      throw ApiException('Unexpected response from the server (HTTP ${res.statusCode}).',
          statusCode: res.statusCode, details: '$route → HTTP ${res.statusCode}: ${_excerpt(res.body)}');
    }

    if (res.statusCode >= 200 && res.statusCode < 300) {
      return decoded;
    }

    final serverMessage = decoded['message']?.toString();
    final details = '$route → HTTP ${res.statusCode}: ${_excerpt(res.body)}';
    if (res.statusCode == 401) {
      throw ApiException(serverMessage ?? 'The server refused the request.', statusCode: 401, details: details);
    }
    if (res.statusCode == 429) {
      throw ApiException(serverMessage ?? 'Too many attempts. Wait a minute and try again.',
          statusCode: 429, kind: ApiErrorKind.rateLimited, details: details);
    }
    if (res.statusCode == 502 || res.statusCode == 503 || res.statusCode == 504) {
      throw ApiException(serverMessage ?? "The server isn't answering right now (HTTP ${res.statusCode}). Try again in a moment.",
          statusCode: res.statusCode, kind: ApiErrorKind.network, details: details);
    }
    throw ApiException(serverMessage ?? 'Request failed (HTTP ${res.statusCode}).', statusCode: res.statusCode, details: details, data: decoded['data']);
  }

  ApiException _refreshFailure(RefreshResult result) => result == RefreshResult.rejected
      ? ApiException('Your session ended. Sign in again.', statusCode: 401, kind: ApiErrorKind.auth)
      : ApiException(
          "Can't reach the server to renew your sign-in. You're still signed in; try again in a moment.",
          kind: ApiErrorKind.network,
        );

  static bool _looksLikeWebPage(http.Response res) {
    final type = res.headers['content-type'] ?? '';
    return type.contains('text/html') || res.body.trimLeft().startsWith('<');
  }

  static String _excerpt(String body) {
    final flat = body.replaceAll(RegExp(r'\s+'), ' ').trim();
    return flat.length > 200 ? '${flat.substring(0, 200)}…' : flat;
  }

  /// Turns a failure to talk to [uri] (a socket error, a TLS failure, a
  /// timeout) into an [ApiException] with a plain message.
  static ApiException describeError(Object error, Uri? uri, {String? method}) {
    if (error is ApiException) return error;
    final host = uri == null || uri.host.isEmpty ? 'the server' : displayHost(uri.toString());
    final route = uri == null ? '' : '${method ?? ''} ${uri.path} '.trimLeft();
    final text = error.toString();
    final details = '$route→ ${error.runtimeType}: ${text.split('\n').first}';
    if (error is HandshakeException || error is TlsException || text.contains('CERTIFICATE_VERIFY_FAILED') || text.contains('HandshakeException')) {
      return ApiException(
        "The server's certificate isn't trusted, so the connection was stopped. The app can't use a self-signed certificate yet: "
        'use the http:// address on your network, or a certificate from a trusted authority.',
        kind: ApiErrorKind.certificate,
        details: details,
      );
    }
    if (error is TimeoutException) {
      return ApiException("$host didn't answer in time. Check that the phone is on the same network as the server.",
          kind: ApiErrorKind.timeout, details: details);
    }
    final lower = text.toLowerCase();
    if (lower.contains('failed host lookup') || lower.contains('no address associated')) {
      return ApiException("Couldn't find $host. Check the address, or that the phone is on the same network.",
          kind: ApiErrorKind.network, details: details);
    }
    if (lower.contains('connection refused')) {
      return ApiException('$host refused the connection. Check the address and port.', kind: ApiErrorKind.network, details: details);
    }
    return ApiException("Couldn't reach $host. Check that the phone is on the same network as the server, or connected through Tailscale.",
        kind: ApiErrorKind.network, details: details);
  }

  // ---------------------------------------------------------------------
  // Sign-in
  // ---------------------------------------------------------------------

  /// Checks that [input] (what someone typed, or a discovered address) is a
  /// NivaroOS server, trying the [candidateUrls] in order. Throws an
  /// [ApiException] saying why not: unreachable, certificate, or not
  /// NivaroOS.
  static Future<ServerProbe> probe(String input, {Duration timeout = const Duration(seconds: 8)}) async {
    final candidates = candidateUrls(input);
    if (candidates.isEmpty) throw ApiException('Enter the server address.', kind: ApiErrorKind.server);
    ApiException? first;
    for (final base in candidates) {
      final uri = Uri.parse('$base/v1/users/status');
      try {
        final res = await http.get(uri).timeout(timeout);
        Object? body;
        try {
          body = jsonDecode(res.body);
        } catch (_) {}
        final data = body is Map ? body['data'] : null;
        if (res.statusCode == 200 && data is Map && data.containsKey('initialized')) {
          return ServerProbe(url: base, initialized: data['initialized'] == true);
        }
        // Something answers there, so don't try the next scheme behind it.
        throw ApiException(
          "${displayHost(base)} answered, but it isn't a NivaroOS server. Check the address and port.",
          statusCode: res.statusCode,
          kind: ApiErrorKind.notNivaro,
          details: 'GET /v1/users/status → HTTP ${res.statusCode}: ${_excerpt(res.body)}',
        );
      } catch (e) {
        if (e is ApiException) rethrow;
        final err = describeError(e, uri, method: 'GET');
        // A certificate problem on https is the answer; don't hide it
        // behind an http attempt that would fail differently.
        if (err.kind == ApiErrorKind.certificate) throw err;
        first ??= err;
      }
    }
    throw first!;
  }

  /// Signs in to the current server with a username and password and keeps
  /// the tokens in memory (the caller saves them, see SessionService).
  /// Throws an [ApiException] with a message fit for the login form (wrong
  /// password, too many attempts, unreachable).
  Future<({String accessToken, String refreshToken})> login(String username, String password) async {
    final tokens = await requestTokens(baseUrl, username, password);
    setSession(tokens.accessToken, tokens.refreshToken);
    return tokens;
  }

  /// Signs in to the server at [baseUrl] and returns its tokens without
  /// touching the current session - for saving another server's profile.
  static Future<({String accessToken, String refreshToken})> requestTokens(String baseUrl, String username, String password) async {
    var base = baseUrl.trim();
    while (base.endsWith('/')) {
      base = base.substring(0, base.length - 1);
    }
    final uri = Uri.parse('$base/v1/users/login');
    final http.Response res;
    try {
      res = await http
          .post(uri, headers: {'Content-Type': 'application/json'}, body: jsonEncode({'username': username, 'password': password}))
          .timeout(const Duration(seconds: 20));
    } catch (e) {
      throw describeError(e, uri, method: 'POST');
    }
    Map<String, dynamic>? body;
    try {
      final parsed = jsonDecode(res.body);
      if (parsed is Map<String, dynamic>) body = parsed;
    } catch (_) {}
    if (body == null) {
      if (res.statusCode >= 500) {
        throw ApiException("The server isn't answering right now (HTTP ${res.statusCode}). Try again in a moment.",
            statusCode: res.statusCode, kind: ApiErrorKind.network, details: 'POST /v1/users/login → HTTP ${res.statusCode}');
      }
      if (_looksLikeWebPage(res)) {
        throw ApiException(
          "The server answered with a web page instead of NivaroOS. If it's behind a proxy or a Wi-Fi sign-in page, check that.",
          statusCode: res.statusCode,
          kind: ApiErrorKind.notNivaro,
          details: 'POST /v1/users/login → HTTP ${res.statusCode} (text/html)',
        );
      }
      throw ApiException('Unexpected response from the server (HTTP ${res.statusCode}).', statusCode: res.statusCode);
    }
    if (res.statusCode == 429) {
      throw ApiException('Too many sign-in attempts. Wait a minute and try again.', statusCode: 429, kind: ApiErrorKind.rateLimited);
    }
    final data = body['data'];
    final token = data is Map ? data['token'] : null;
    final access = token is Map ? token['access_token']?.toString() : null;
    final refresh = token is Map ? token['refresh_token']?.toString() : null;
    if (res.statusCode == 200 && access != null && access.isNotEmpty && refresh != null && refresh.isNotEmpty) {
      return (accessToken: access, refreshToken: refresh);
    }
    if (res.statusCode == 400 || res.statusCode == 401 || res.statusCode == 403) {
      throw ApiException('Wrong username or password.', statusCode: res.statusCode, kind: ApiErrorKind.auth);
    }
    if (res.statusCode >= 500) {
      throw ApiException("The server couldn't sign you in right now (HTTP ${res.statusCode}). Try again in a moment.",
          statusCode: res.statusCode, kind: ApiErrorKind.network);
    }
    throw ApiException(body['message']?.toString() ?? 'Sign-in failed (HTTP ${res.statusCode}).', statusCode: res.statusCode);
  }

  // ---------------------------------------------------------------------
  // Refresh
  // ---------------------------------------------------------------------

  /// Gets a new access token with the refresh token. Concurrent callers
  /// share one request. Only a 401 from `/users/refresh` ends the session
  /// ([RefreshResult.rejected]); anything else keeps the tokens.
  Future<RefreshResult> refresh() {
    final pending = _refreshFuture;
    if (pending != null) return pending;
    final future = _executeRefresh();
    _refreshFuture = future;
    return future.whenComplete(() => _refreshFuture = null);
  }

  Future<RefreshResult> _executeRefresh() async {
    // Another engine (the UI, the sharing service, the heartbeat job)
    // shares this storage and may have refreshed already: adopt its
    // tokens instead of refreshing again with an older refresh token.
    try {
      final stored = await StorageService.instance.reloadSession();
      if (stored != null && stored.accessToken != _accessToken && stored.accessToken.isNotEmpty) {
        final exp = tokenExpiry(stored.accessToken);
        if (exp == null || exp.difference(clock.now().toUtc()) > refreshMargin) {
          _accessToken = stored.accessToken;
          _refreshToken = stored.refreshToken;
          return RefreshResult.refreshed;
        }
        _refreshToken = stored.refreshToken;
      }
    } catch (_) {}

    final refreshToken = _refreshToken;
    if (refreshToken == null || refreshToken.isEmpty) {
      // Nothing to renew with: the session can't continue.
      _handleAuthFailure();
      return RefreshResult.rejected;
    }

    final http.Response res;
    try {
      // No Authorization header: the expired access token isn't needed.
      final uri = _uri('/users/refresh');
      res = await http
          .post(uri, headers: {'Content-Type': 'application/json'}, body: jsonEncode({'refresh_token': refreshToken}))
          .timeout(const Duration(seconds: 20));
    } catch (e) {
      debugPrint('[ApiClient] refresh unavailable: ${e.runtimeType}');
      return RefreshResult.unavailable;
    }

    if (res.statusCode == 401) {
      _handleAuthFailure();
      return RefreshResult.rejected;
    }
    if (res.statusCode != 200) {
      debugPrint('[ApiClient] refresh unavailable: HTTP ${res.statusCode}');
      return RefreshResult.unavailable;
    }
    try {
      final decoded = jsonDecode(res.body) as Map<String, dynamic>;
      final data = decoded['data'] as Map<String, dynamic>?;
      final access = data?['access_token'] as String?;
      if (access == null || access.isEmpty) return RefreshResult.unavailable;
      _accessToken = access;
      final newRefresh = data?['refresh_token'] as String?;
      if (newRefresh != null && newRefresh.isNotEmpty) _refreshToken = newRefresh;
      // Signed out elsewhere while this refresh ran (sign-out stops the
      // sharing engine only by request, and it may be mid-refresh): don't
      // write a session back that the user just ended (review finding 9).
      if (!await StorageService.instance.hasStoredSession()) {
        _accessToken = null;
        _refreshToken = null;
        return RefreshResult.rejected;
      }
      sessionExpiredNotifier.value = false;
      final username = await StorageService.instance.getUsername() ?? '';
      await StorageService.instance.setSession(accessToken: _accessToken!, refreshToken: _refreshToken!, username: username);
      return RefreshResult.refreshed;
    } catch (_) {
      // A 200 that isn't the API (a captive portal answering everything).
      return RefreshResult.unavailable;
    }
  }

  void _handleAuthFailure() {
    _accessToken = null;
    _refreshToken = null;
    sessionExpiredNotifier.value = true;
    StorageService.instance.clearSession();
  }

  // --- apps additions ---
  /// PATCH with an optional JSON body: app-management's "update this
  /// compose app to the store's version" (`PATCH /v2/app_management/compose/{id}`).
  Future<Map<String, dynamic>> patch(String path, {Map<String, dynamic>? query, Object? body}) async {
    final uri = _uri(path, query);
    return _send('PATCH', uri, () => http.patch(uri, headers: _headers(), body: body == null ? null : jsonEncode(body)));
  }
  // --- end apps additions ---
}
