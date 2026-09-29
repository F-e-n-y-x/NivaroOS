import 'dart:io';

/// How well a server address protects what the app sends over it.
///
/// Why this exists, and not an Android network security config that
/// forbids cleartext: NivaroOS servers are normally reached over plain
/// http on the owner's own networks - a home IP (192.168.x.x), a `.local`
/// name, a Tailscale 100.x address - where there is no certificate to
/// check. Android's config can only allow or forbid cleartext per domain
/// name (it can't express "private IP ranges" or "the Tailscale range"), so
/// cleartext stays allowed at the platform level and the app draws the
/// line itself, by where the address leads:
///
/// - [https]: TLS end to end.
/// - [tailscale]: http inside Tailscale (100.64.0.0/10, fd7a:115c:a1e0::/48,
///   `*.ts.net`): WireGuard encrypts it between the two devices.
/// - [localNetwork]: http on a private network (RFC 1918, link-local,
///   loopback, IPv6 ULA, `.local`, `.lan`, `.home.arpa`, `.internal`, a
///   single-word name, an RFC 6761 name that never reaches the internet
///   such as `.test`, or a name that resolves only to such addresses):
///   not encrypted, but it doesn't leave the home network.
/// - [insecure]: http to an address on the internet. The app refuses to
///   send a password or a refresh token there (see [allowsSecrets]) and
///   Server profiles marks the server "Not secure".
enum TransportSecurity {
  https,
  tailscale,
  localNetwork,
  insecure;

  /// Safe to send a password or session over.
  bool get allowsSecrets => this != insecure;

  String get label => switch (this) {
        https => 'Encrypted (https)',
        tailscale => 'Encrypted by Tailscale',
        localNetwork => 'Home network, not encrypted',
        insecure => 'Not secure',
      };

  String get description => switch (this) {
        https => 'The connection is encrypted and the server’s certificate is checked.',
        tailscale => 'Plain http, but inside Tailscale, which encrypts everything between this phone and the server.',
        localNetwork => 'Plain http on your own network. Fine at home; to reach the server from elsewhere, use Tailscale or an https address.',
        insecure =>
          'Plain http over the internet: anyone on the way could read your password and files. The app won’t sign in here. Use the server’s https address, or Tailscale.',
      };
}

abstract final class ConnectionSecurity {
  /// The security of [url] from the address alone. A name that isn't
  /// recognisably private counts as the internet; [resolve] looks it up.
  static TransportSecurity of(String url) {
    final uri = _parse(url);
    if (uri == null) return TransportSecurity.insecure;
    if (uri.scheme == 'https' || uri.scheme == 'wss') return TransportSecurity.https;
    return hostKind(uri.host) ?? TransportSecurity.insecure;
  }

  /// Like [of], but a name that isn't recognisably private is looked up:
  /// when every address it resolves to is private (split DNS at home:
  /// nas.example.com → 192.168.1.20) it counts as that. [lookup] is for
  /// tests.
  static Future<TransportSecurity> resolve(String url, {Future<List<InternetAddress>> Function(String host)? lookup}) async {
    final quick = of(url);
    if (quick != TransportSecurity.insecure) return quick;
    final uri = _parse(url);
    if (uri == null || uri.host.isEmpty || InternetAddress.tryParse(_bare(uri.host)) != null) return quick;
    try {
      final addresses = await (lookup ?? InternetAddress.lookup)(uri.host).timeout(const Duration(seconds: 3));
      if (addresses.isEmpty) return quick;
      TransportSecurity? worst;
      for (final a in addresses) {
        final kind = hostKind(a.address);
        if (kind == null) return TransportSecurity.insecure;
        worst = (worst == null || kind.index > worst.index) ? kind : worst;
      }
      return worst ?? quick;
    } catch (_) {
      // Unresolvable here: the request would fail anyway.
      return quick;
    }
  }

  /// Throws [InsecureConnectionException] when [url] is plain http to the
  /// internet: called before sending a password or a refresh token.
  static Future<void> ensureAllowsSecrets(String url) async {
    if (!(await resolve(url)).allowsSecrets) throw InsecureConnectionException(url);
  }

  /// [TransportSecurity.tailscale] or [TransportSecurity.localNetwork] for
  /// a private host (IP or name), null for anything else.
  static TransportSecurity? hostKind(String host) {
    final h = _bare(host).toLowerCase();
    if (h.isEmpty) return null;
    final ip = InternetAddress.tryParse(h);
    if (ip != null) return _ipKind(ip);
    if (h.endsWith('.ts.net')) return TransportSecurity.tailscale;
    if (h == 'localhost' || !h.contains('.')) return TransportSecurity.localNetwork;
    // Private-use names, plus the RFC 6761 special-use ones that are
    // never delegated on the internet (so can't lead there).
    const suffixes = ['.local', '.lan', '.home.arpa', '.internal', '.localdomain', '.home', '.localhost', '.test', '.invalid'];
    for (final s in suffixes) {
      if (h.endsWith(s)) return TransportSecurity.localNetwork;
    }
    return null;
  }

  static TransportSecurity? _ipKind(InternetAddress ip) {
    final b = ip.rawAddress;
    if (ip.type == InternetAddressType.IPv4) {
      if (b[0] == 100 && b[1] >= 64 && b[1] <= 127) return TransportSecurity.tailscale; // 100.64.0.0/10
      if (b[0] == 10 || b[0] == 127) return TransportSecurity.localNetwork;
      if (b[0] == 172 && b[1] >= 16 && b[1] <= 31) return TransportSecurity.localNetwork;
      if (b[0] == 192 && b[1] == 168) return TransportSecurity.localNetwork;
      if (b[0] == 169 && b[1] == 254) return TransportSecurity.localNetwork;
      return null;
    }
    // IPv4-mapped IPv6 (::ffff:a.b.c.d).
    final mapped = b.sublist(0, 10).every((x) => x == 0) && b[10] == 0xff && b[11] == 0xff;
    if (mapped) return _ipKind(InternetAddress.fromRawAddress(b.sublist(12)));
    if (ip.isLoopback) return TransportSecurity.localNetwork;
    if (b[0] == 0xfd && b[1] == 0x7a && b[2] == 0x11 && b[3] == 0x5c && b[4] == 0xa1 && b[5] == 0xe0) return TransportSecurity.tailscale;
    if ((b[0] & 0xfe) == 0xfc) return TransportSecurity.localNetwork; // fc00::/7
    if (b[0] == 0xfe && (b[1] & 0xc0) == 0x80) return TransportSecurity.localNetwork; // fe80::/10
    return null;
  }

  static Uri? _parse(String url) {
    final s = url.trim();
    if (s.isEmpty) return null;
    return Uri.tryParse(s.contains('://') ? s : 'http://$s');
  }

  static String _bare(String host) {
    var h = host;
    if (h.startsWith('[') && h.endsWith(']')) h = h.substring(1, h.length - 1);
    final zone = h.indexOf('%');
    return zone < 0 ? h : h.substring(0, zone);
  }
}

/// Refused before sending anything: [url] is plain http to the internet.
class InsecureConnectionException implements Exception {
  InsecureConnectionException(this.url);
  final String url;

  String get message =>
      'Not signed in: $url is plain http over the internet, so your password would travel unencrypted. '
      'Use the server’s https address, or connect through Tailscale.';

  @override
  String toString() => message;
}
