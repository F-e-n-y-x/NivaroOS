import 'dart:io';
import 'dart:math';

import 'connection_security.dart';
import 'server_trust.dart';

/// The video player (ExoPlayer) only trusts the system's certificates, not
/// a server's own CA the user pinned (ServerTrust). For such a server its
/// https media URLs are played through this relay on 127.0.0.1: the player
/// talks to the relay, the relay to the server with the pinned checks.
/// Only URLs handed out by [wrap] are served (a random path each), only GET
/// and HEAD, and only Authorization/Range/User-Agent/Accept go upstream.
class MediaRelay {
  MediaRelay._();
  static final MediaRelay instance = MediaRelay._();

  HttpServer? _server;
  // ponytail: entries are never dropped (a few bytes per video opened);
  // add release() when relayed URLs become long-lived or many.
  final _targets = <String, Uri>{};
  final _client = HttpClient()..autoUncompress = false;
  final _random = Random.secure();

  /// [url] as the player should open it: through the relay for an https
  /// home-network/Tailscale server once any CA is pinned, else unchanged.
  Future<Uri> wrapIfNeeded(Uri url) async {
    if (url.scheme != 'https' || !ServerTrust.instance.hasPins || ConnectionSecurity.hostKind(url.host) == null) return url;
    return wrap(url);
  }

  Future<Uri> wrap(Uri url) async {
    final server = _server ??= await _start();
    final id = List.generate(16, (_) => _random.nextInt(256).toRadixString(16).padLeft(2, '0')).join();
    _targets[id] = url;
    return Uri(scheme: 'http', host: '127.0.0.1', port: server.port, path: '/$id');
  }

  Future<HttpServer> _start() async {
    final s = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    s.listen(_serve);
    return s;
  }

  Future<void> _serve(HttpRequest req) async {
    final target = _targets[req.uri.pathSegments.firstOrNull];
    final res = req.response;
    if (target == null || (req.method != 'GET' && req.method != 'HEAD')) {
      res.statusCode = HttpStatus.notFound;
      await res.close();
      return;
    }
    try {
      final up = await _client.openUrl(req.method, target);
      for (final h in const ['authorization', 'range', 'user-agent', 'accept']) {
        final v = req.headers.value(h);
        if (v != null) up.headers.set(h, v);
      }
      final down = await up.close();
      res.statusCode = down.statusCode;
      down.headers.forEach((k, v) {
        if (k != HttpHeaders.transferEncodingHeader && k != HttpHeaders.connectionHeader) res.headers.set(k, v);
      });
      await res.addStream(down);
      await res.close();
    } catch (_) {
      try {
        res.statusCode = HttpStatus.badGateway;
        await res.close();
      } catch (_) {}
    }
  }
}
