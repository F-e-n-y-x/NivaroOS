import 'api_client.dart';

class TailscalePeer {
  final String hostName;
  final String ip;
  final String os;
  final bool online;
  final bool exitNode;
  final bool exitNodeOption;
  final List<String> primaryRoutes;

  TailscalePeer({
    required this.hostName,
    required this.ip,
    required this.os,
    required this.online,
    required this.exitNode,
    required this.exitNodeOption,
    required this.primaryRoutes,
  });

  factory TailscalePeer.fromJson(Map<String, dynamic> j) {
    final ips = j['TailscaleIPs'] as List<dynamic>? ?? [];
    final firstIp = ips.isNotEmpty ? ips.first.toString() : '';
    final routes = (j['PrimaryRoutes'] as List<dynamic>? ?? []).map((e) => e.toString()).toList();
    return TailscalePeer(
      hostName: j['HostName'] as String? ?? 'Device',
      ip: firstIp,
      os: j['OS'] as String? ?? 'Linux',
      online: j['Online'] as bool? ?? false,
      exitNode: j['ExitNode'] as bool? ?? false,
      exitNodeOption: j['ExitNodeOption'] as bool? ?? false,
      primaryRoutes: routes,
    );
  }
}

class TailscaleStatus {
  final String backendState;
  final String selfIp;
  final String selfIpv6;
  final String hostName;
  final String magicDns;
  final String authUrl;
  final List<TailscalePeer> peers;
  final bool isExitNode;
  final bool hasExitNodeOption;
  final List<String> primaryRoutes;

  TailscaleStatus({
    required this.backendState,
    required this.selfIp,
    required this.selfIpv6,
    required this.hostName,
    required this.magicDns,
    required this.authUrl,
    required this.peers,
    required this.isExitNode,
    required this.hasExitNodeOption,
    required this.primaryRoutes,
  });

  bool get isRunning => backendState.toLowerCase() == 'running';
  bool get needsLogin => backendState == 'NeedsLogin' || authUrl.isNotEmpty;

  factory TailscaleStatus.fromJson(Map<String, dynamic> j) {
    final self = j['Self'] as Map<String, dynamic>? ?? {};
    final selfIps = (self['TailscaleIPs'] as List<dynamic>?) ?? (j['TailscaleIPs'] as List<dynamic>?) ?? [];
    final ipv4 = selfIps.isNotEmpty ? selfIps[0].toString() : '';
    final ipv6 = selfIps.length > 1 ? selfIps[1].toString() : '';

    final peerMap = j['Peer'] as Map<String, dynamic>? ?? {};
    final peers = <TailscalePeer>[];
    peerMap.forEach((k, v) {
      if (v is Map<String, dynamic>) {
        peers.add(TailscalePeer.fromJson(v));
      }
    });
    peers.sort((a, b) {
      if (a.online != b.online) return a.online ? -1 : 1;
      return a.hostName.toLowerCase().compareTo(b.hostName.toLowerCase());
    });

    final routes = ((self['PrimaryRoutes'] ?? j['PrimaryRoutes']) as List<dynamic>? ?? []).map((e) => e.toString()).toList();
    final backendState = j['BackendState'] as String? ?? (ipv4.isNotEmpty ? 'Running' : 'Stopped');
    final hostName = (self['HostName']?.toString() ?? j['HostName']?.toString() ?? 'NivaroOS Host');
    final magicDns = (j['MagicDNSSuffix']?.toString() ?? (self['DNSName']?.toString() ?? ''));

    return TailscaleStatus(
      backendState: backendState,
      selfIp: ipv4,
      selfIpv6: ipv6,
      hostName: hostName,
      magicDns: magicDns,
      authUrl: j['AuthURL']?.toString() ?? '',
      peers: peers,
      isExitNode: (self['ExitNode'] ?? j['ExitNode']) as bool? ?? false,
      hasExitNodeOption: (self['ExitNodeOption'] ?? j['ExitNodeOption']) as bool? ?? false,
      primaryRoutes: routes,
    );
  }
}

class TailscalePrefs {
  final bool acceptRoutes;
  final bool acceptDns;
  final bool runSsh;
  final bool shieldsUp;
  final bool exitNodeAllowLanAccess;
  final List<String> advertiseRoutes;

  TailscalePrefs({
    this.acceptRoutes = false,
    this.acceptDns = true,
    this.runSsh = false,
    this.shieldsUp = false,
    this.exitNodeAllowLanAccess = false,
    this.advertiseRoutes = const [],
  });

  factory TailscalePrefs.fromJson(Map<String, dynamic> j) {
    final routes = (j['advertise_routes'] as List<dynamic>? ?? []).map((e) => e.toString()).toList();
    return TailscalePrefs(
      acceptRoutes: j['accept_routes'] as bool? ?? false,
      acceptDns: j['accept_dns'] as bool? ?? true,
      runSsh: j['run_ssh'] as bool? ?? false,
      shieldsUp: j['shields_up'] as bool? ?? false,
      exitNodeAllowLanAccess: j['exit_node_allow_lan_access'] as bool? ?? false,
      advertiseRoutes: routes,
    );
  }
}

class TailscaleService {
  TailscaleService._();
  static final TailscaleService instance = TailscaleService._();

  Future<TailscaleStatus> getStatus() async {
    try {
      final res = await ApiClient.instance.get('/v1/tailscale/status');
      final data = res['data'] as Map<String, dynamic>? ?? {};
      return TailscaleStatus.fromJson(data);
    } catch (_) {
      // Fallback: check network interfaces for tailscale0
      try {
        final netRes = await ApiClient.instance.get('/sys/network-interfaces');
        final netData = netRes['data'] as List<dynamic>? ?? [];
        for (final iface in netData) {
          final name = iface['name']?.toString() ?? '';
          if (name.contains('tailscale') || name.contains('ts0') || name.contains('wg')) {
            final ip = iface['ip']?.toString() ?? iface['address']?.toString() ?? '';
            return TailscaleStatus(
              backendState: 'Running',
              selfIp: ip,
              selfIpv6: '',
              hostName: 'NivaroOS Host',
              magicDns: '',
              authUrl: '',
              peers: [],
              isExitNode: false,
              hasExitNodeOption: false,
              primaryRoutes: [],
            );
          }
        }
      } catch (_) {}

      return TailscaleStatus(
        backendState: 'Stopped',
        selfIp: '',
        selfIpv6: '',
        hostName: 'NivaroOS Server',
        magicDns: '',
        authUrl: '',
        peers: [],
        isExitNode: false,
        hasExitNodeOption: false,
        primaryRoutes: [],
      );
    }
  }

  Future<void> connectWithAuthKey(String authKey) async {
    try {
      await ApiClient.instance.post('/v1/tailscale/auth', body: {'auth_key': authKey});
    } catch (_) {
      // Fallback custom storage key
      await ApiClient.instance.post('/users/current/custom/tailscale_auth', body: {'key': authKey});
    }
  }

  Future<void> setState(bool up) async {
    try {
      await ApiClient.instance.put('/v1/tailscale/state/${up ? 'up' : 'down'}');
    } catch (_) {}
  }

  Future<TailscalePrefs> getPrefs() async {
    try {
      final res = await ApiClient.instance.get('/v1/tailscale/prefs');
      final data = res['data'] as Map<String, dynamic>? ?? {};
      return TailscalePrefs.fromJson(data);
    } catch (_) {
      return TailscalePrefs();
    }
  }

  Future<void> setPref(String key, dynamic value) async {
    try {
      await ApiClient.instance.put('/v1/tailscale/prefs', body: {key: value});
    } catch (_) {}
  }
}
