import 'dart:async';
import 'dart:io';

import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;

/// Signing in to a cloud drive (Google Drive, OneDrive, Dropbox) from the
/// phone. The server runs `rclone authorize` (local-storage
/// route/v1/cloud_authorize.go), which waits for the provider's redirect on
/// its own 127.0.0.1:53682 - but the provider sends the browser to
/// http://127.0.0.1:53682/, which on a phone is the phone. So while the
/// sign-in runs, [OAuthRedirectForwarder] listens there on the phone and
/// passes that exact request on to the server's relay
/// (`<server>/v1/rclone-auth`, services/gateway/route/rclone_auth.go).

/// The port `rclone authorize` redirects to (hardcoded upstream).
const rcloneRedirectPort = 53682;

/// The scheme of the "Return to NivaroOS" link on the signed-in page
/// (CloudSignInReturnActivity brings the app back over the browser tab).
const cloudSignInReturnUrl = 'nivaroos-cloud:done';

class OAuthRedirectForwarder {
  OAuthRedirectForwarder._(this._servers, this.relayBase, this._client);

  final List<HttpServer> _servers;

  /// `<server>/v1/rclone-auth`, no trailing slash.
  final Uri relayBase;
  final http.Client _client;

  int get port => _servers.first.port;

  /// Listens on 127.0.0.1 (and ::1, for providers that redirect to
  /// localhost) at [port]. Throws a [SocketException] when another app
  /// holds it.
  static Future<OAuthRedirectForwarder> start(Uri relayBase, {int port = rcloneRedirectPort, http.Client? client}) async {
    final v4 = await HttpServer.bind(InternetAddress.loopbackIPv4, port);
    final servers = [v4];
    try {
      servers.add(await HttpServer.bind(InternetAddress.loopbackIPv6, v4.port));
    } catch (_) {
      // No IPv6 loopback: localhost resolves to 127.0.0.1 then.
    }
    final f = OAuthRedirectForwarder._(servers, relayBase, client ?? http.Client());
    for (final s in servers) {
      s.listen(f._handle);
    }
    return f;
  }

  Future<void> close() async {
    for (final s in _servers) {
      await s.close(force: true);
    }
    _client.close();
  }

  Future<void> _handle(HttpRequest req) async {
    final res = req.response;
    // Only the redirect itself (rclone answers nothing else there): no
    // favicon or other request from the page reaches the server.
    if (req.method != 'GET' || req.uri.path != '/') {
      res.statusCode = HttpStatus.notFound;
      await res.close();
      return;
    }
    String? problem;
    try {
      // The query exactly as the provider sent it (code, state, scope...),
      // without the browser's cookies or anything else.
      final query = req.uri.hasQuery ? '?${req.uri.query}' : '';
      final r = await _client.get(Uri.parse('$relayBase/$query')).timeout(const Duration(seconds: 30));
      if (r.statusCode != 200) problem = 'NivaroOS did not accept the sign-in (HTTP ${r.statusCode}).';
    } catch (_) {
      problem = "Couldn't reach NivaroOS to finish the sign-in.";
    }
    res.headers
      ..contentType = ContentType.html
      ..set('Cache-Control', 'no-store');
    res.write(signedInPage(problem));
    await res.close();
  }

  /// The page the browser shows after the provider's redirect.
  static String signedInPage(String? problem) {
    final title = problem == null ? 'Signed in' : 'Sign-in not finished';
    final text = problem == null ? 'You can return to NivaroOS.' : '$problem Return to NivaroOS and try again.';
    return '<!doctype html><html><head><meta charset="utf-8">'
        '<meta name="viewport" content="width=device-width,initial-scale=1">'
        '<title>$title</title>'
        '<style>body{font-family:system-ui,sans-serif;margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;'
        'background:#f6f6f8;color:#1b1b1f;text-align:center}main{padding:24px;max-width:420px}'
        'h1{font-size:24px;margin:0 0 8px}p{font-size:16px;line-height:1.5;margin:0 0 24px}'
        'a{display:inline-block;padding:12px 24px;border-radius:999px;background:#3a5ba9;color:#fff;text-decoration:none;font-weight:600}'
        '@media(prefers-color-scheme:dark){body{background:#121316;color:#e3e2e6}a{background:#b0c6ff;color:#002d6e}}</style>'
        '</head><body><main><h1>$title</h1><p>$text</p>'
        '<a href="$cloudSignInReturnUrl">Return to NivaroOS</a></main>'
        '${problem == null ? '<script>location.href="$cloudSignInReturnUrl"</script>' : ''}'
        '</body></html>';
  }
}

/// Where rclone's sign-in link (`<relay><path>`, "/auth?state=...") sends
/// the browser: the provider's own https sign-in page. Asked here, through
/// the app's connection, so the browser never needs to reach the server.
Future<Uri> providerSignInUrl(Uri relayBase, String path, {http.Client? client}) async {
  final c = client ?? http.Client();
  try {
    final req = http.Request('GET', Uri.parse('$relayBase$path'))..followRedirects = false;
    final res = await c.send(req).timeout(const Duration(seconds: 30));
    await res.stream.drain<void>();
    final to = Uri.tryParse(res.headers['location'] ?? '');
    // Only a provider's https page: whatever answers on the server's
    // loopback port can't send the browser anywhere else.
    if (res.statusCode >= 300 && res.statusCode < 400 && to != null && to.scheme == 'https' && to.host.isNotEmpty) return to;
    throw Exception("The server's sign-in link didn't lead to a sign-in page (HTTP ${res.statusCode}). Try again.");
  } finally {
    if (client == null) c.close();
  }
}

const _channel = MethodChannel('com.fenyx.nivaroos/cloud_signin');

/// Keeps the app running while the browser covers it, so Android doesn't
/// freeze the listener ([provider] shows in the notification); null lets it
/// go (CloudSignInService).
Future<void> keepAppAwake(String? provider) async {
  try {
    await (provider == null ? _channel.invokeMethod<void>('stop') : _channel.invokeMethod<void>('start', {'provider': provider}));
  } catch (_) {
    // No Android side (tests, iOS): nothing to keep awake.
  }
}
