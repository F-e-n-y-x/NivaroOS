/// One way to reach a server: its home-network address, its Tailscale
/// address or name, or a public (tunnel, reverse proxy) URL. The same
/// sign-in works on all of them.
class ServerAddress {
  const ServerAddress(this.url, {this.learned = false});

  /// A normalised base URL ("http://192.168.1.20", "https://nas.example.com").
  final String url;

  /// True when the app learned it from the server (its Tailscale address,
  /// its other network addresses) rather than the user typing it.
  final bool learned;

  factory ServerAddress.fromJson(Object? j) {
    if (j is String) return ServerAddress(j);
    final m = j is Map ? j : const {};
    return ServerAddress(m['url']?.toString() ?? '', learned: m['learned'] == true);
  }

  Map<String, dynamic> toJson() => {'url': url, if (learned) 'learned': true};

  @override
  bool operator ==(Object other) => other is ServerAddress && other.url == url && other.learned == learned;

  @override
  int get hashCode => Object.hash(url, learned);
}

/// A saved NivaroOS server: its address, the account used on it, and that
/// account's tokens (kept in secure storage with everything else).
///
/// [url] is the server's identity in this app (what per-server settings
/// such as the storage-sharing secret are keyed by) and never changes when
/// the app reaches the server some other way; [addresses] are all the ways
/// to reach it, in the order they are tried (see ServerRoutes).
class ServerProfile {
  final String id;
  final String name;
  final String url;
  final String username;
  final String? accessToken;
  final String? refreshToken;
  final int? lastPingMs;
  final DateTime? lastConnected;

  /// Every address of this server, in order of preference. Empty for a
  /// profile saved before addresses existed: then [url] is the only one.
  final List<ServerAddress> addresses;

  /// Learned addresses the user removed: never learned again.
  final List<String> ignoredAddresses;

  /// Home-network ranges the server shares over Tailscale (its subnet
  /// routes, "192.168.1.0/24"), so a home address reached through
  /// Tailscale is shown as Tailscale, not as the home network.
  final List<String> subnetRoutes;

  /// The server's own name on the tailnet ("atom"), for messages.
  final String? hostName;

  /// When the addresses were last learned from the server.
  final DateTime? learnedAt;

  ServerProfile({
    required this.id,
    required this.name,
    required this.url,
    required this.username,
    this.accessToken,
    this.refreshToken,
    this.lastPingMs,
    this.lastConnected,
    this.addresses = const [],
    this.ignoredAddresses = const [],
    this.subnetRoutes = const [],
    this.hostName,
    this.learnedAt,
  });

  /// The addresses to try, in order: [addresses], or just [url] for an
  /// older profile.
  List<ServerAddress> get effectiveAddresses => addresses.isEmpty ? [ServerAddress(url)] : addresses;

  /// True when the profile holds a session to switch to without signing in.
  bool get hasSession => (accessToken?.isNotEmpty ?? false) && (refreshToken?.isNotEmpty ?? false);

  /// The name a new profile gets when nobody typed one: the server's host
  /// ("nas.local", "192.168.1.20:8080").
  static String defaultName(String url) {
    final uri = Uri.tryParse(url.contains('://') ? url : 'http://$url');
    final host = uri?.host ?? '';
    if (host.isEmpty) return url;
    return uri!.hasPort ? '$host:${uri.port}' : host;
  }

  /// Older builds named every first profile "Primary Server" or "Nivaro
  /// Server"; show the host for those instead.
  String get displayName {
    final n = name.trim();
    if (n.isEmpty || n == 'Primary Server' || n == 'Nivaro Server') return defaultName(url);
    return n;
  }

  factory ServerProfile.fromJson(Map<String, dynamic> j) => ServerProfile(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        url: j['url'] as String? ?? '',
        username: j['username'] as String? ?? '',
        accessToken: j['access_token'] as String?,
        refreshToken: j['refresh_token'] as String?,
        lastPingMs: (j['last_ping_ms'] as num?)?.toInt(),
        lastConnected: j['last_connected'] != null ? DateTime.tryParse(j['last_connected'] as String) : null,
        addresses: [
          for (final a in (j['addresses'] as List? ?? const []))
            if (ServerAddress.fromJson(a).url.isNotEmpty) ServerAddress.fromJson(a),
        ],
        ignoredAddresses: [for (final a in (j['ignored_addresses'] as List? ?? const [])) a.toString()],
        subnetRoutes: [for (final a in (j['subnet_routes'] as List? ?? const [])) a.toString()],
        hostName: j['host_name'] as String?,
        learnedAt: j['learned_at'] != null ? DateTime.tryParse(j['learned_at'] as String) : null,
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'url': url,
        'username': username,
        'access_token': accessToken,
        'refresh_token': refreshToken,
        'last_ping_ms': lastPingMs,
        'last_connected': lastConnected?.toIso8601String(),
        if (addresses.isNotEmpty) 'addresses': [for (final a in addresses) a.toJson()],
        if (ignoredAddresses.isNotEmpty) 'ignored_addresses': ignoredAddresses,
        if (subnetRoutes.isNotEmpty) 'subnet_routes': subnetRoutes,
        'host_name': ?hostName,
        if (learnedAt != null) 'learned_at': learnedAt!.toIso8601String(),
      };

  ServerProfile copyWith({
    String? name,
    String? url,
    String? username,
    String? accessToken,
    String? refreshToken,
    int? lastPingMs,
    DateTime? lastConnected,
    List<ServerAddress>? addresses,
    List<String>? ignoredAddresses,
    List<String>? subnetRoutes,
    String? hostName,
    DateTime? learnedAt,
  }) =>
      ServerProfile(
        id: id,
        name: name ?? this.name,
        url: url ?? this.url,
        username: username ?? this.username,
        accessToken: accessToken ?? this.accessToken,
        refreshToken: refreshToken ?? this.refreshToken,
        lastPingMs: lastPingMs ?? this.lastPingMs,
        lastConnected: lastConnected ?? this.lastConnected,
        addresses: addresses ?? this.addresses,
        ignoredAddresses: ignoredAddresses ?? this.ignoredAddresses,
        subnetRoutes: subnetRoutes ?? this.subnetRoutes,
        hostName: hostName ?? this.hostName,
        learnedAt: learnedAt ?? this.learnedAt,
      );

  /// The same profile with its session removed (signed out, or the server
  /// ended the session).
  ServerProfile withoutTokens() => ServerProfile(
        id: id,
        name: name,
        url: url,
        username: username,
        lastPingMs: lastPingMs,
        lastConnected: lastConnected,
        addresses: addresses,
        ignoredAddresses: ignoredAddresses,
        subnetRoutes: subnetRoutes,
        hostName: hostName,
        learnedAt: learnedAt,
      );
}
