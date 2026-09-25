import 'dart:async';

import 'package:clock/clock.dart';

import 'api_client.dart';
import 'storage_service.dart';

class TailscalePeer {
  final String hostName;
  final String dnsName;
  final String ip;
  final String os;
  final bool online;
  final bool exitNode;
  final bool exitNodeOption;
  final DateTime? lastSeen;
  final List<String> primaryRoutes;

  TailscalePeer({
    required this.hostName,
    this.dnsName = '',
    required this.ip,
    required this.os,
    required this.online,
    required this.exitNode,
    required this.exitNodeOption,
    this.lastSeen,
    required this.primaryRoutes,
  });

  /// A phone or tablet (for the icon).
  bool get isMobile {
    final o = os.toLowerCase();
    return o.contains('android') || o.contains('ios');
  }

  factory TailscalePeer.fromJson(Map<String, dynamic> j) {
    final ips = j['TailscaleIPs'] as List<dynamic>? ?? [];
    final firstIp = ips.isNotEmpty ? ips.first.toString() : '';
    final routes = (j['PrimaryRoutes'] as List<dynamic>? ?? []).map((e) => e.toString()).toList();
    final seen = DateTime.tryParse(j['LastSeen']?.toString() ?? '');
    return TailscalePeer(
      hostName: j['HostName'] as String? ?? '',
      dnsName: (j['DNSName'] as String? ?? '').replaceAll(RegExp(r'\.$'), ''),
      ip: firstIp,
      os: j['OS'] as String? ?? '',
      online: j['Online'] as bool? ?? false,
      exitNode: j['ExitNode'] as bool? ?? false,
      exitNodeOption: j['ExitNodeOption'] as bool? ?? false,
      // Tailscale writes the zero time for "never" / "now".
      lastSeen: seen != null && seen.year > 1 ? seen : null,
      primaryRoutes: routes,
    );
  }
}

/// Where the server's Tailscale stands, from `tailscale status`.
enum TailscaleState {
  /// Signed in and connected to the tailnet.
  running,

  /// Connecting (Tailscale's "Starting").
  starting,

  /// Installed and running, but the server isn't signed in to a tailnet.
  needsLogin,

  /// Signed in but switched off (`tailscale down`).
  stopped,

  /// Installed, but its service (tailscaled) isn't running. Connect
  /// starts it.
  noDaemon,

  /// Tailscale isn't installed on the server.
  notInstalled,
}

class TailscaleStatus {
  final String backendState;
  final String selfIp;
  final String selfIpv6;
  final String hostName;
  final String magicDns;
  final String dnsName;
  final String authUrl;
  final List<TailscalePeer> peers;
  final bool isExitNode;
  final bool hasExitNodeOption;
  final List<String> primaryRoutes;
  final bool installed;

  TailscaleStatus({
    required this.backendState,
    required this.selfIp,
    required this.selfIpv6,
    required this.hostName,
    required this.magicDns,
    this.dnsName = '',
    required this.authUrl,
    required this.peers,
    required this.isExitNode,
    required this.hasExitNodeOption,
    required this.primaryRoutes,
    this.installed = true,
  });

  TailscaleState get state {
    if (!installed) return TailscaleState.notInstalled;
    switch (backendState) {
      case 'Running':
        return TailscaleState.running;
      case 'Starting':
        return TailscaleState.starting;
      case 'NeedsLogin':
      case 'NeedsMachineAuth':
        return TailscaleState.needsLogin;
      case 'NoDaemon':
        return TailscaleState.noDaemon;
      default:
        return authUrl.isNotEmpty ? TailscaleState.needsLogin : TailscaleState.stopped;
    }
  }

  bool get isRunning => state == TailscaleState.running;
  bool get needsLogin => state == TailscaleState.needsLogin;

  factory TailscaleStatus.fromJson(Map<String, dynamic> j) {
    final self = j['Self'] as Map<String, dynamic>? ?? {};
    final selfIps = (self['TailscaleIPs'] as List<dynamic>?) ?? (j['TailscaleIPs'] as List<dynamic>?) ?? [];
    final ipv4 = selfIps.isNotEmpty ? selfIps[0].toString() : '';
    final ipv6 = selfIps.length > 1 ? selfIps[1].toString() : '';

    final peerMap = j['Peer'] as Map<String, dynamic>? ?? {};
    final peers = <TailscalePeer>[];
    peerMap.forEach((k, v) {
      if (v is Map<String, dynamic>) peers.add(TailscalePeer.fromJson(v));
    });
    peers.sort((a, b) {
      if (a.online != b.online) return a.online ? -1 : 1;
      return a.hostName.toLowerCase().compareTo(b.hostName.toLowerCase());
    });

    final routes = ((self['PrimaryRoutes'] ?? j['PrimaryRoutes']) as List<dynamic>? ?? []).map((e) => e.toString()).toList();
    final backendState = j['BackendState'] as String? ?? (ipv4.isNotEmpty ? 'Running' : 'Stopped');
    final suffix = j['MagicDNSSuffix']?.toString() ?? '';
    final dnsName = (self['DNSName']?.toString() ?? '').replaceAll(RegExp(r'\.$'), '');

    return TailscaleStatus(
      backendState: backendState,
      selfIp: ipv4,
      selfIpv6: ipv6,
      hostName: self['HostName']?.toString() ?? j['HostName']?.toString() ?? '',
      magicDns: suffix,
      dnsName: dnsName,
      authUrl: j['AuthURL']?.toString() ?? '',
      peers: peers,
      isExitNode: (self['ExitNode'] ?? j['ExitNode']) as bool? ?? false,
      hasExitNodeOption: (self['ExitNodeOption'] ?? j['ExitNodeOption']) as bool? ?? false,
      primaryRoutes: routes,
    );
  }

  static TailscaleStatus absent() => TailscaleStatus(
        backendState: 'NoDaemon',
        selfIp: '',
        selfIpv6: '',
        hostName: '',
        magicDns: '',
        authUrl: '',
        peers: const [],
        isExitNode: false,
        hasExitNodeOption: false,
        primaryRoutes: const [],
        installed: false,
      );
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

/// What `PUT /v1/tailscale/state/up` answered.
class TailscaleUpResult {
  const TailscaleUpResult({this.loginUrl});

  /// Set when the server must be signed in to a tailnet: open it in a
  /// browser (on any device) and approve.
  final String? loginUrl;
  bool get needsLogin => loginUrl != null && loginUrl!.isNotEmpty;
}

/// The server's Tailscale, through core's `/v1/tailscale/*` routes:
/// `status`, `state/up|down`, `prefs`, `installed`, `install`. Signing the
/// server in is Tailscale's own flow: "up" returns a login link, the user
/// approves it in a browser, and the status turns to Running (plan M-10).
/// The app never handles Tailscale auth keys.
class TailscaleService {
  TailscaleService._();
  static final TailscaleService instance = TailscaleService._();

  /// The status, or [TailscaleStatus.absent] when Tailscale isn't
  /// installed. Throws an [ApiException] when the server can't be asked.
  Future<TailscaleStatus> getStatus() async {
    final res = await ApiClient.instance.get('/v1/tailscale/status');
    final data = res['data'] as Map<String, dynamic>? ?? {};
    final status = TailscaleStatus.fromJson(data);
    if (status.backendState == 'NoDaemon' && !await isInstalled()) return TailscaleStatus.absent();
    return status;
  }

  Future<bool> isInstalled() async {
    try {
      final res = await ApiClient.instance.get('/v1/tailscale/installed');
      final data = res['data'];
      return data is Map ? data['installed'] == true : true;
    } on ApiException {
      return true;
    }
  }

  /// Connects (starting the Tailscale service if needed). Throws an
  /// [ApiException] with the server's reason when it fails.
  Future<TailscaleUpResult> connect() async {
    final res = await ApiClient.instance.put('/v1/tailscale/state/up');
    final data = res['data'];
    final url = data is Map ? data['login_url']?.toString() : null;
    return TailscaleUpResult(loginUrl: url);
  }

  Future<void> disconnect() async {
    await ApiClient.instance.put('/v1/tailscale/state/down');
  }

  /// Installs Tailscale on the server (Tailscale's install script) and
  /// starts it; returns the login link it prints, if any.
  Future<TailscaleUpResult> install() async {
    final res = await ApiClient.instance.post('/v1/tailscale/install');
    final data = res['data'];
    return TailscaleUpResult(loginUrl: data is Map ? data['login_url']?.toString() : null);
  }

  /// Polls the status until [done] says so or [timeout] passes; returns the
  /// last status read. For waiting on a sign-in approved in the browser.
  Future<TailscaleStatus?> waitFor(bool Function(TailscaleStatus s) done,
      {Duration timeout = const Duration(minutes: 3), Duration every = const Duration(seconds: 3), bool Function()? cancelled}) async {
    final end = clock.now().add(timeout);
    TailscaleStatus? last;
    while (clock.now().isBefore(end)) {
      if (cancelled?.call() ?? false) return last;
      try {
        last = await getStatus();
        if (done(last)) return last;
      } on ApiException {
        // Keep trying: the gateway can blip while Tailscale reconfigures.
      }
      await Future<void>.delayed(every);
    }
    return last;
  }

  Future<TailscalePrefs> getPrefs() async {
    final res = await ApiClient.instance.get('/v1/tailscale/prefs');
    final data = res['data'] as Map<String, dynamic>? ?? {};
    return TailscalePrefs.fromJson(data);
  }

  Future<void> setPref(String key, dynamic value) async {
    await ApiClient.instance.put('/v1/tailscale/prefs', body: {key: value});
  }

  /// Builds before 1.3 "signed in" by saving the Tailscale auth key the
  /// user typed as a plain custom setting on the server
  /// (`/users/current/custom/tailscale_auth`), where nothing read it.
  /// Deletes it once per server and account after sign-in.
  Future<void> removeLegacyAuthKey() async {
    final server = ApiClient.instance.baseUrl;
    final user = await StorageService.instance.getUsername() ?? '';
    final name = 'tailscale_auth_removed@$server#$user';
    if (server.isEmpty || await StorageService.instance.isMigrationDone(name)) return;
    try {
      final res = await ApiClient.instance.get('/v1/users/current/custom/tailscale_auth');
      final data = res['data'];
      final saved = data != null && data.toString().isNotEmpty;
      if (saved) await ApiClient.instance.delete('/v1/users/current/custom/tailscale_auth');
      await StorageService.instance.markMigrationDone(name);
    } on ApiException {
      // Try again after the next sign-in.
    }
  }

  /// True when [baseUrl] reaches the server through Tailscale (a
  /// 100.64.0.0/10 address or a *.ts.net name), so turning Tailscale off
  /// would cut this app off.
  static bool isTailscaleAddress(String baseUrl) {
    final host = Uri.tryParse(baseUrl)?.host ?? '';
    if (host.endsWith('.ts.net')) return true;
    final parts = host.split('.');
    if (parts.length != 4) return false;
    final a = int.tryParse(parts[0]);
    final b = int.tryParse(parts[1]);
    return a == 100 && b != null && b >= 64 && b <= 127;
  }
}
