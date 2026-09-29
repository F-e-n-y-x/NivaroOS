/// How the server reaches a companion phone's files right now (the server's
/// `route` field, S-04): directly on the home network, directly over
/// Tailscale, through the phone's own connection to the server (works
/// anywhere, slower), only folder listings (an older app on the phone), or
/// not at all.
enum CompanionRoute {
  lan,
  tailscale,
  tunnel,
  tunnelList,
  none;

  static CompanionRoute parse(Object? v) => switch (v) {
        'lan' => lan,
        'tailscale' => tailscale,
        'tunnel' => tunnel,
        'tunnel_list' => tunnelList,
        _ => none,
      };

  /// Files open, stream and copy.
  bool get filesWork => this == lan || this == tailscale || this == tunnel;

  /// One short phrase for a status line; null when there is nothing to say.
  String? get label => switch (this) {
        lan => 'Direct · home network',
        tailscale => 'Direct · Tailscale',
        tunnel => 'Through the server · slower',
        tunnelList => 'Folders only · update the app on it',
        none => null,
      };
}
