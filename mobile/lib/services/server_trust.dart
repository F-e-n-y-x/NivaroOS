import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';

import 'storage_service.dart';

/// The certificate authorities of NivaroOS servers that serve https with
/// their own certificate (Settings > Network > HTTPS on your network,
/// docs/specs/2026-10-16-lan-https.md), trusted on first use: the user saw
/// the fingerprint and accepted it.
///
/// Accepted CAs join one process-wide [SecurityContext] next to the system
/// roots, installed with [HttpOverrides.global], so package:http,
/// WebSockets and Image.network all check the chain and the host name as
/// usual - nothing is skipped. The server's CA is name-constrained to local
/// names and private addresses, so it can't vouch for internet sites.
class ServerTrust {
  ServerTrust._();
  static final ServerTrust instance = ServerTrust._();

  /// System roots plus every pinned CA. CAs are only ever added: a removed
  /// one stops counting at the next app start.
  final SecurityContext context = SecurityContext(withTrustedRoots: true);
  final Set<String> _pinned = {};
  bool _ready = false;

  /// Loads the pinned CAs and installs the overrides (per isolate; again is a no-op).
  Future<void> init() async {
    if (_ready) return;
    _ready = true;
    HttpOverrides.global = _PinnedOverrides(context);
    for (final pem in await StorageService.instance.getPinnedCas()) {
      _add(pem);
    }
  }

  bool get hasPins => _pinned.isNotEmpty;

  bool isPinned(Uint8List der) => _pinned.contains(fingerprint(der));

  /// Trusts [der] from now on, here and after restarts.
  Future<void> pin(Uint8List der) async {
    if (isPinned(der)) return;
    final pem = toPem(der);
    _add(pem);
    await StorageService.instance.setPinnedCas([...await StorageService.instance.getPinnedCas(), pem]);
  }

  void _add(String pem) {
    try {
      context.setTrustedCertificatesBytes(utf8.encode(pem));
      _pinned.add(fingerprint(_derOf(pem)));
    } catch (e) {
      debugPrint('[trust] bad pinned CA: ${e.runtimeType}');
    }
  }

  /// "AB:CD:..." - the SHA-256 the server's Settings and trust page show.
  static String fingerprint(List<int> der) =>
      sha256.convert(der).bytes.map((b) => b.toRadixString(16).padLeft(2, '0').toUpperCase()).join(':');

  static String toPem(List<int> der) {
    final b64 = base64.encode(der);
    final lines = [for (var i = 0; i < b64.length; i += 64) b64.substring(i, i + 64 > b64.length ? b64.length : i + 64)];
    return '-----BEGIN CERTIFICATE-----\n${lines.join('\n')}\n-----END CERTIFICATE-----\n';
  }

  static Uint8List _derOf(String pem) =>
      base64.decode(pem.replaceAll(RegExp(r'-----[A-Z ]+-----'), '').replaceAll(RegExp(r'\s'), ''));

  /// What the server says about its https listener (GET over [base], which
  /// may be http): null when it has none or it is off.
  static Future<({String port, String fingerprint})?> offeredHttps(String base) async {
    final client = HttpClient()..connectionTimeout = const Duration(seconds: 6);
    try {
      final req = await client.getUrl(Uri.parse('$base/v1/gateway/tls'));
      final res = await req.close().timeout(const Duration(seconds: 6));
      if (res.statusCode != 200) return null;
      final body = jsonDecode(await utf8.decodeStream(res));
      final d = body is Map ? body['data'] : null;
      if (d is! Map || d['enabled'] != true || d['running'] != true) return null;
      return (port: '${d['port'] ?? '443'}', fingerprint: '${d['fingerprint'] ?? ''}');
    } catch (_) {
      return null;
    } finally {
      client.close(force: true);
    }
  }

  /// The https address for [base]'s host on [port].
  static String httpsBase(String base, String port) {
    final u = Uri.parse(base);
    return Uri(scheme: 'https', host: u.host, port: port == '443' ? null : int.tryParse(port)).toString().replaceAll(RegExp(r'/$'), '');
  }

  /// Downloads the CA certificate (DER) from [httpsBase] without checking
  /// the TLS certificate - the user checks the fingerprint before it is
  /// trusted, and nothing else is sent on this connection.
  static Future<Uint8List> fetchCa(String httpsBase) async {
    final client = HttpClient(context: SecurityContext(withTrustedRoots: false))
      ..connectionTimeout = const Duration(seconds: 8)
      ..badCertificateCallback = (_, _, _) => true;
    try {
      final req = await client.getUrl(Uri.parse('$httpsBase/v1/gateway/tls/ca.crt'));
      final res = await req.close().timeout(const Duration(seconds: 8));
      if (res.statusCode != 200) throw HttpException('HTTP ${res.statusCode}');
      final bytes = await res.fold<BytesBuilder>(BytesBuilder(), (b, d) => b..add(d));
      return bytes.takeBytes();
    } finally {
      client.close(force: true);
    }
  }

  /// Whether [der] (a certificate a WebView was shown for [url]) is the one
  /// a connection that verifies against the pinned CAs receives from the
  /// same host and port - then the WebView may proceed.
  Future<bool> matchesServer(Uri url, Uint8List? der) async {
    if (der == null || url.scheme != 'https') return false;
    try {
      final s = await SecureSocket.connect(url.host, url.port, context: context, timeout: const Duration(seconds: 6));
      final peer = s.peerCertificate?.der;
      s.destroy();
      return peer != null && listEquals(peer, der);
    } catch (_) {
      return false;
    }
  }
}

class _PinnedOverrides extends HttpOverrides {
  _PinnedOverrides(this.ctx);
  final SecurityContext ctx;

  @override
  HttpClient createHttpClient(SecurityContext? context) => super.createHttpClient(context ?? ctx);
}
