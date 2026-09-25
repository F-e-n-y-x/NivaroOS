/// A saved NivaroOS server: its address, the account used on it, and that
/// account's tokens (kept in secure storage with everything else).
class ServerProfile {
  final String id;
  final String name;
  final String url;
  final String username;
  final String? accessToken;
  final String? refreshToken;
  final int? lastPingMs;
  final DateTime? lastConnected;

  ServerProfile({
    required this.id,
    required this.name,
    required this.url,
    required this.username,
    this.accessToken,
    this.refreshToken,
    this.lastPingMs,
    this.lastConnected,
  });

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
      };

  ServerProfile copyWith({
    String? name,
    String? url,
    String? username,
    String? accessToken,
    String? refreshToken,
    int? lastPingMs,
    DateTime? lastConnected,
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
      );
}
