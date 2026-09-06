/// Saved NivaroOS Server Profile for multi-server management.
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

  factory ServerProfile.fromJson(Map<String, dynamic> j) => ServerProfile(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        url: j['url'] as String? ?? '',
        username: j['username'] as String? ?? '',
        accessToken: j['access_token'] as String?,
        refreshToken: j['refresh_token'] as String?,
        lastPingMs: (j['last_ping_ms'] as num?)?.toInt(),
        lastConnected: j['last_connected'] != null
            ? DateTime.tryParse(j['last_connected'] as String)
            : null,
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
}
