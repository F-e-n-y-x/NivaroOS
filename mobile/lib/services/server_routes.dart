import 'dart:async';
import 'dart:io';

import 'package:clock/clock.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:http/http.dart' as http;

import '../models/server_profile.dart';
import 'api_client.dart';
import 'storage_service.dart';

/// How the phone reaches the server right now, as the app names it.
enum RouteKind {
  /// Straight over the home network (Wi-Fi at home).
  home,

  /// Through Tailscale: the server's 100.x address or MagicDNS name, or a
  /// home address the server shares as a Tailscale subnet route.
  tailscale,

  /// Through the internet: a public name (a Cloudflare tunnel, a reverse
  /// proxy).
  internet;

  String get label => switch (this) {
        RouteKind.home => 'Home network',
        RouteKind.tailscale => 'Tailscale',
        RouteKind.internet => 'Internet',
      };

  /// "Through Tailscale", for sentences.
  String get through => switch (this) {
        RouteKind.home => 'On the home network',
        RouteKind.tailscale => 'Through Tailscale',
        RouteKind.internet => 'Through the internet',
      };
}

/// What the phone's own network looks like: its addresses outside any VPN,
/// and whether Tailscale is connected on it.
class PhoneNetwork {
  const PhoneNetwork({this.localIps = const [], this.tailscaleUp = false, this.known = true});

  /// IPv4 addresses of the phone's Wi-Fi, Ethernet and mobile data
  /// interfaces (not VPN tunnels).
  final List<String> localIps;

  /// Tailscale's tunnel is up on this phone (a tun interface with an
  /// address in Tailscale's 100.64.0.0/10 or fd7a:115c:a1e0::/48).
  final bool tailscaleUp;

  /// False when the interfaces couldn't be read: nothing is inferred then.
  final bool known;

  static const unknown = PhoneNetwork(known: false);

  /// Reads the phone's interfaces.
  static Future<PhoneNetwork> read() async {
    try {
      final ifaces = await NetworkInterface.list(includeLoopback: false, includeLinkLocal: false, type: InternetAddressType.any);
      return fromInterfaces([
        for (final i in ifaces) (name: i.name, addresses: [for (final a in i.addresses) a.address]),
      ]);
    } catch (_) {
      return unknown;
    }
  }

  /// The pure part of [read], for tests.
  static PhoneNetwork fromInterfaces(List<({String name, List<String> addresses})> ifaces) {
    final local = <String>[];
    var tailscale = false;
    for (final i in ifaces) {
      final name = i.name.toLowerCase();
      final tunnel = name.startsWith('tun') || name.contains('tailscale') || name.startsWith('utun') || name.startsWith('ipsec') || name.startsWith('ppp') || name.startsWith('wg');
      for (final a in i.addresses) {
        if (tunnel) {
          // Carrier-grade NAT hands mobile data 100.64/10 addresses too, so
          // only a tunnel interface's address counts as Tailscale.
          if (RouteRules.isTailscaleHost(a)) tailscale = true;
        } else if (RouteRules.parseV4(a) != null) {
          local.add(a);
        }
      }
    }
    return PhoneNetwork(localIps: local, tailscaleUp: tailscale);
  }
}

/// The address rules, all pure (see test/services/server_routes_test.dart).
abstract final class RouteRules {
  static List<int>? parseV4(String host) {
    final m = RegExp(r'^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$').firstMatch(host);
    if (m == null) return null;
    final p = [for (var i = 1; i <= 4; i++) int.parse(m.group(i)!)];
    return p.any((x) => x > 255) ? null : p;
  }

  static String _host(String urlOrHost) {
    final s = urlOrHost.trim();
    final uri = Uri.tryParse(s.contains('://') ? s : 'http://$s');
    return (uri?.host ?? s).toLowerCase().replaceAll(RegExp(r'^\[|\]$'), '');
  }

  /// A Tailscale address or name: 100.64.0.0/10, fd7a:115c:a1e0::/48, or
  /// a MagicDNS name (*.ts.net).
  static bool isTailscaleHost(String urlOrHost) {
    final h = _host(urlOrHost);
    if (h.endsWith('.ts.net')) return true;
    if (h.startsWith('fd7a:115c:a1e0:')) return true;
    final p = parseV4(h);
    return p != null && p[0] == 100 && p[1] >= 64 && p[1] <= 127;
  }

  /// An address that only exists on a home network: RFC 1918 and
  /// link-local IPv4, IPv6 ULA/link-local, `.local`, `.lan`, `.home.arpa`,
  /// `.internal` and single-label names.
  static bool isHomeHost(String urlOrHost) {
    final h = _host(urlOrHost);
    if (h.isEmpty || isTailscaleHost(h)) return false;
    if (h == 'localhost') return true;
    if (!h.contains('.') && !h.contains(':')) return true;
    if (h.endsWith('.local') || h.endsWith('.lan') || h.endsWith('.home.arpa') || h.endsWith('.internal')) return true;
    final p = parseV4(h);
    if (p != null) {
      final a = p[0], b = p[1];
      return a == 10 || a == 127 || (a == 172 && b >= 16 && b <= 31) || (a == 192 && b == 168) || (a == 169 && b == 254);
    }
    if (h.contains(':')) return h == '::1' || h.startsWith('fd') || h.startsWith('fc') || h.startsWith('fe80');
    return false;
  }

  /// What kind of address [url] is, from the address alone.
  static RouteKind kindOf(String url) {
    if (isTailscaleHost(url)) return RouteKind.tailscale;
    if (isHomeHost(url)) return RouteKind.home;
    return RouteKind.internet;
  }

  /// How the phone reaches [url], given its own network: a home address
  /// the phone isn't on the same network as, while Tailscale is up (or
  /// inside the server's Tailscale subnet routes), goes through Tailscale.
  static RouteKind describe(String url, {PhoneNetwork net = PhoneNetwork.unknown, List<String> subnetRoutes = const []}) {
    final kind = kindOf(url);
    if (kind != RouteKind.home || !net.known) return kind;
    final ip = parseV4(_host(url));
    if (ip == null) return kind; // a .local name: only answers at home
    final routes = subnetRoutes.where((r) => inCidr(_host(url), r)).toList();
    if (routes.isNotEmpty) {
      final local = net.localIps.any((l) => routes.any((r) => inCidr(l, r)));
      if (local) return RouteKind.home;
      return net.tailscaleUp ? RouteKind.tailscale : RouteKind.home;
    }
    final sameSubnet = net.localIps.any((l) {
      final p = parseV4(l);
      return p != null && p[0] == ip[0] && p[1] == ip[1] && p[2] == ip[2];
    });
    if (!sameSubnet && net.tailscaleUp) return RouteKind.tailscale;
    return RouteKind.home;
  }

  /// Whether IPv4 [ip] lies in [cidr] ("192.168.1.0/24").
  static bool inCidr(String ip, String cidr) {
    final parts = cidr.split('/');
    final net = parseV4(parts.first);
    final addr = parseV4(ip);
    final bits = parts.length > 1 ? int.tryParse(parts[1]) : 32;
    if (net == null || addr == null || bits == null || bits < 0 || bits > 32) return false;
    int v(List<int> p) => (p[0] << 24) | (p[1] << 16) | (p[2] << 8) | p[3];
    final mask = bits == 0 ? 0 : (0xFFFFFFFF << (32 - bits)) & 0xFFFFFFFF;
    return (v(net) & mask) == (v(addr) & mask);
  }

  static int rank(String url) => switch (kindOf(url)) {
        RouteKind.home => 0,
        RouteKind.tailscale => 1,
        RouteKind.internet => 2,
      };

  static String _norm(String url) {
    var s = url.trim().toLowerCase();
    while (s.endsWith('/')) {
      s = s.substring(0, s.length - 1);
    }
    return s;
  }

  static bool same(String a, String b) => _norm(a) == _norm(b);

  /// [url] (under [fromBase]) moved to [toBase]: the same path and query on
  /// the other address. Anything not under [fromBase] is returned as is.
  static String rebase(String url, String fromBase, String toBase) {
    var from = fromBase;
    while (from.endsWith('/')) {
      from = from.substring(0, from.length - 1);
    }
    var to = toBase;
    while (to.endsWith('/')) {
      to = to.substring(0, to.length - 1);
    }
    if (url.toLowerCase().startsWith(from.toLowerCase())) {
      final rest = url.substring(from.length);
      if (rest.isEmpty || rest.startsWith('/') || rest.startsWith('?')) return '$to$rest';
    }
    return url;
  }

  /// [current] with what was just learned from the server added. Each
  /// learned address goes after the last one of the same or a preferred
  /// kind (home, then Tailscale, then internet), so the default order
  /// prefers the home network; the user's own order is kept otherwise.
  /// For every kind in [authoritative], earlier learned addresses of that
  /// kind that the server no longer reports are dropped (a new Tailscale
  /// address). Addresses the user typed, [keep] and [ignored] ones are
  /// never touched.
  static List<ServerAddress> merge(
    List<ServerAddress> current,
    List<String> learned, {
    Set<RouteKind> authoritative = const {},
    List<String> ignored = const [],
    String? keep,
  }) {
    final out = [
      for (final a in current)
        if (!(a.learned && authoritative.contains(kindOf(a.url)) && !learned.any((l) => same(l, a.url)) && !(keep != null && same(keep, a.url)))) a,
    ];
    for (final l in learned) {
      if (out.any((a) => same(a.url, l)) || ignored.any((i) => same(i, l))) continue;
      final r = rank(l);
      var at = 0;
      for (var i = 0; i < out.length; i++) {
        if (rank(out[i].url) <= r) at = i + 1;
      }
      out.insert(at, ServerAddress(l, learned: true));
    }
    return out;
  }
}

/// Whether a route check is running, found one, or found none.
enum RouteStatus { idle, checking, connected, unreachable }

/// The connection as the app shows it.
@immutable
class RouteState {
  const RouteState({
    this.status = RouteStatus.idle,
    this.url,
    this.kind,
    this.phoneTailscaleUp,
    this.hasTailscaleAddress = false,
    this.alternatives = false,
    this.serverName,
  });

  final RouteStatus status;

  /// The address in use (the last one that answered).
  final String? url;
  final RouteKind? kind;

  /// Tailscale on this phone: null when unknown.
  final bool? phoneTailscaleUp;

  /// The server has a Tailscale address saved.
  final bool hasTailscaleAddress;

  /// The server has more than one address.
  final bool alternatives;

  /// "atom": the server's name for messages.
  final String? serverName;

  /// Nothing answers, and turning Tailscale on would likely help.
  bool get suggestTailscale => status == RouteStatus.unreachable && hasTailscaleAddress && phoneTailscaleUp != true;

  RouteState copyWith({RouteStatus? status, String? url, RouteKind? kind, bool? phoneTailscaleUp, bool? hasTailscaleAddress, bool? alternatives, String? serverName}) =>
      RouteState(
        status: status ?? this.status,
        url: url ?? this.url,
        kind: kind ?? this.kind,
        phoneTailscaleUp: phoneTailscaleUp ?? this.phoneTailscaleUp,
        hasTailscaleAddress: hasTailscaleAddress ?? this.hasTailscaleAddress,
        alternatives: alternatives ?? this.alternatives,
        serverName: serverName ?? this.serverName,
      );

  @override
  bool operator ==(Object other) =>
      other is RouteState &&
      other.status == status &&
      other.url == url &&
      other.kind == kind &&
      other.phoneTailscaleUp == phoneTailscaleUp &&
      other.hasTailscaleAddress == hasTailscaleAddress &&
      other.alternatives == alternatives &&
      other.serverName == serverName;

  @override
  int get hashCode => Object.hash(status, url, kind, phoneTailscaleUp, hasTailscaleAddress, alternatives, serverName);
}

/// One address's answer to a probe.
class RouteProbe {
  const RouteProbe(this.url, this.ok, {this.ms});
  final String url;
  final bool ok;
  final int? ms;
}

/// Picks which of the current server's addresses the app uses (plan:
/// "works at home and away"). Every address of the server - home network,
/// Tailscale, public URL - is probed in parallel with a cheap unauthenticated
/// `GET /ping`, and the first one in the profile's order that answers wins
/// (a better one gets a moment to answer after a worse one did). The same
/// sign-in works on all of them, so switching needs nothing else: every
/// request, WebSocket and download builds its URL from
/// [ApiClient.baseUrl] when it starts.
///
/// Runs in every engine (the UI, the sharing service, the heartbeat job):
/// [load] happens in [ApiClient.init], and a request that can't reach the
/// server calls [failover]. The UI engine also checks again when the app
/// comes back to the front and when the phone's network changes
/// ([startWatching]).
class ServerRoutes with WidgetsBindingObserver {
  ServerRoutes._();
  static final ServerRoutes instance = ServerRoutes._();

  /// The connection, for the banner, Settings and Home.
  final ValueNotifier<RouteState> state = ValueNotifier(const RouteState());

  /// How long a probe may take in the quick round, and in the patient one
  /// that follows when nothing answered (Tailscale can take a few seconds
  /// to wake a connection).
  static const quickTimeout = Duration(seconds: 3);
  static const patientTimeout = Duration(seconds: 8);

  /// After an address answers, how long a preferred one still pending may
  /// take to answer too.
  static const preferredGrace = Duration(milliseconds: 600);

  /// A GET that hasn't answered after this long makes the app check that
  /// the address in use still works.
  static const stallAfter = Duration(seconds: 6);

  /// Probes one base URL; tests replace it.
  @visibleForTesting
  static Future<RouteProbe> Function(String base, Duration timeout) probe = ping;

  /// Reads the phone's network; tests replace it.
  @visibleForTesting
  static Future<PhoneNetwork> Function() readNetwork = PhoneNetwork.read;

  ServerProfile? _profile;
  List<String> _candidates = const [];
  Future<String?>? _selecting;
  DateTime? _selectedAt;
  String? _selected;
  PhoneNetwork _net = PhoneNetwork.unknown;
  bool _watching = false;
  StreamSubscription<dynamic>? _networkEvents;
  Timer? _networkDebounce;
  DateTime? _lastResumeCheck;

  /// The addresses to try, in order.
  List<String> get candidates => _candidates;

  /// More than one address: route checks are worth doing.
  bool get hasAlternatives => _candidates.length > 1;

  ServerProfile? get profile => _profile;

  /// Reads the phone's network again (for [describe]).
  Future<void> refreshNetwork() async {
    _net = await readNetwork();
  }

  /// How the phone reaches [url] now (see [RouteRules.describe]).
  RouteKind describe(String url) => RouteRules.describe(url, net: _net, subnetRoutes: _profile?.subnetRoutes ?? const []);

  /// Reads the current server's profile and starts on the address it was
  /// last reached on, if that is still one of its addresses.
  Future<void> load() async {
    final storage = StorageService.instance;
    final serverUrl = await storage.getServerUrl();
    if (serverUrl == null || serverUrl.isEmpty) {
      _profile = null;
      _candidates = const [];
      _publish(const RouteState());
      return;
    }
    final profile = await storage.getActiveProfile();
    _setProfile(profile, serverUrl);
    final last = await storage.getActiveRoute();
    if (last != null && _candidates.any((c) => RouteRules.same(c, last))) {
      ApiClient.instance.useRoute(last);
    } else if (!_candidates.any((c) => RouteRules.same(c, ApiClient.instance.baseUrl))) {
      ApiClient.instance.useRoute(_candidates.first);
    }
    final base = ApiClient.instance.baseUrl;
    _publish(state.value.copyWith(url: base, kind: describe(base)));
  }

  void _setProfile(ServerProfile? profile, String serverUrl) {
    _profile = profile;
    final list = <String>[];
    for (final a in (profile?.effectiveAddresses ?? [ServerAddress(serverUrl)])) {
      final u = ApiClient.normalizeServerUrl(a.url);
      if (u.isNotEmpty && !list.any((x) => RouteRules.same(x, u))) list.add(u);
    }
    if (list.isEmpty) list.add(serverUrl);
    _candidates = list;
    _publish(state.value.copyWith(
      alternatives: list.length > 1,
      hasTailscaleAddress: list.any(RouteRules.isTailscaleHost) || (profile?.subnetRoutes.isNotEmpty ?? false),
      serverName: _serverName(profile, serverUrl),
    ));
  }

  /// The server's own name ("atom", from Tailscale) when known, else the
  /// profile's.
  static String _serverName(ServerProfile? p, String url) {
    final host = p?.hostName;
    if (host != null && host.isNotEmpty) return host;
    return p?.displayName ?? ServerProfile.defaultName(url);
  }

  /// Forgets the server (signed out, switched): the next [load] starts
  /// over.
  void reset() {
    _profile = null;
    _candidates = const [];
    _selecting = null;
    _selectedAt = null;
    _selected = null;
    _publish(const RouteState());
  }

  void _publish(RouteState s) {
    if (state.value != s) state.value = s;
  }

  /// Probes every address and switches to the best one that answers.
  /// Returns it, or null when none answered (the address in use stays).
  /// Concurrent callers share one check.
  Future<String?> select() {
    final pending = _selecting;
    if (pending != null) return pending;
    final f = _select();
    _selecting = f;
    return f.whenComplete(() => _selecting = null);
  }

  Future<String?> _select() async {
    if (_candidates.isEmpty) await load();
    final cands = List<String>.of(_candidates);
    if (cands.isEmpty) return null;
    _publish(state.value.copyWith(status: RouteStatus.checking));
    _net = await readNetwork();
    var winner = await pickFirst(cands, quickTimeout);
    winner ??= await pickFirst(cands, patientTimeout);
    _selectedAt = clock.now();
    _selected = winner;
    if (winner == null) {
      _publish(RouteState(
        status: RouteStatus.unreachable,
        url: ApiClient.instance.baseUrl,
        kind: describe(ApiClient.instance.baseUrl),
        phoneTailscaleUp: _net.known ? _net.tailscaleUp : null,
        hasTailscaleAddress: state.value.hasTailscaleAddress,
        alternatives: cands.length > 1,
        serverName: state.value.serverName,
      ));
      return null;
    }
    _use(winner);
    return winner;
  }

  void _use(String url) {
    final changed = !RouteRules.same(ApiClient.instance.baseUrl, url);
    ApiClient.instance.useRoute(url);
    if (changed) debugPrint('[routes] now using ${RouteRules.kindOf(url).name} ${ApiClient.displayHost(url)}');
    unawaited(StorageService.instance.setActiveRoute(url).catchError((_) {}));
    _publish(RouteState(
      status: RouteStatus.connected,
      url: url,
      kind: describe(url),
      phoneTailscaleUp: _net.known ? _net.tailscaleUp : null,
      hasTailscaleAddress: state.value.hasTailscaleAddress,
      alternatives: _candidates.length > 1,
      serverName: state.value.serverName,
    ));
  }

  /// The first of [cands] (in order) that answers within [timeout]. All
  /// are probed at once; once one answered, the ones before it get
  /// [preferredGrace] more to answer.
  static Future<String?> pickFirst(List<String> cands, Duration timeout) async {
    if (cands.isEmpty) return null;
    final results = List<bool?>.filled(cands.length, null);
    final done = Completer<void>();
    Timer? grace;
    void settle() {
      if (done.isCompleted) return;
      final firstOk = results.indexWhere((r) => r == true);
      final allBeforeFailed = firstOk >= 0 && results.take(firstOk).every((r) => r == false);
      if (allBeforeFailed || results.every((r) => r != null)) {
        grace?.cancel();
        done.complete();
      } else if (firstOk >= 0 && grace == null) {
        grace = Timer(preferredGrace, () {
          if (!done.isCompleted) done.complete();
        });
      }
    }

    for (var i = 0; i < cands.length; i++) {
      final index = i;
      probe(cands[i], timeout).then((p) => p.ok, onError: (_) => false).then((ok) {
        results[index] = ok;
        settle();
      });
    }
    await done.future;
    final i = results.indexWhere((r) => r == true);
    return i < 0 ? null : cands[i];
  }

  /// Probes each address and reports how it answered, for the addresses
  /// screen ("Answers · 34 ms").
  Future<List<RouteProbe>> probeAll() async {
    final cands = List<String>.of(_candidates);
    return Future.wait([
      for (final c in cands) probe(c, quickTimeout).catchError((_) => RouteProbe(c, false)),
    ]);
  }

  /// A request to [failedBase] couldn't reach the server: checks the
  /// addresses and returns the one to retry on, or null when there is no
  /// other (nothing else answers, or [failedBase] itself just answered).
  Future<String?> failover(String failedBase) async {
    if (!hasAlternatives) {
      if (_candidates.isEmpty) return null;
      // One address: nothing to switch to, but say it's unreachable.
      return null;
    }
    final current = ApiClient.instance.baseUrl;
    if (!RouteRules.same(current, failedBase)) return current; // switched meanwhile
    final pending = _selecting;
    if (pending != null) {
      final r = await pending;
      return r != null && !RouteRules.same(r, failedBase) ? r : null;
    }
    final at = _selectedAt;
    if (at != null && clock.now().difference(at) < const Duration(seconds: 5)) {
      // Just checked: don't probe again for every failing request.
      final r = _selected;
      return r != null && !RouteRules.same(r, failedBase) ? r : null;
    }
    final r = await select();
    return r != null && !RouteRules.same(r, failedBase) ? r : null;
  }

  /// A request to [base] is taking long: checks it still answers, and when
  /// it doesn't, switches. Returns the new address, or null.
  Future<String?> checkStalled(String base) async {
    if (!hasAlternatives || !RouteRules.same(ApiClient.instance.baseUrl, base)) return null;
    final p = await probe(base, quickTimeout).catchError((_) => RouteProbe(base, false));
    if (p.ok) return null;
    return failover(base);
  }

  /// A cheap unauthenticated check that [base] is this NivaroOS gateway:
  /// `GET /ping` answers "pong from gateway service".
  static Future<RouteProbe> ping(String base, Duration timeout) async {
    final sw = Stopwatch()..start();
    try {
      final res = await http.get(Uri.parse('$base/ping')).timeout(timeout);
      final ok = res.statusCode == 200 && res.body.contains('pong');
      return RouteProbe(base, ok, ms: ok ? sw.elapsedMilliseconds : null);
    } catch (_) {
      return RouteProbe(base, false);
    }
  }

  // -------------------------------------------------------------------
  // Learning the server's addresses
  // -------------------------------------------------------------------

  /// Asks the signed-in server for its other addresses (its Tailscale
  /// address, MagicDNS name and subnet routes, its home-network addresses)
  /// and adds them to its profile. At most every six hours unless [force].
  /// Failures are quiet.
  Future<void> learn({bool force = false}) async {
    if (!ApiClient.instance.hasSession) return;
    final profile = await StorageService.instance.getActiveProfile();
    if (profile == null) return;
    final at = profile.learnedAt;
    if (!force && at != null && clock.now().difference(at) < const Duration(hours: 6)) return;

    final api = ApiClient.instance;
    int port = Uri.tryParse(profile.url)?.port ?? 80;
    try {
      final p = int.tryParse('${(await api.get('/gateway/port'))['data']}');
      if (p != null && p > 0) port = p;
    } catch (_) {}
    if (Uri.tryParse(profile.url)?.scheme == 'https' && port == 443) port = 80;
    String url(String host) => 'http://${host.contains(':') ? '[$host]' : host}${port == 80 ? '' : ':$port'}';

    final learned = <String>[];
    final authoritative = <RouteKind>{};
    List<String>? routes;
    String? hostName;

    try {
      final res = await api.get('/v1/tailscale/status');
      final ts = parseTailscale(res['data']);
      if (ts != null) {
        authoritative.add(RouteKind.tailscale);
        learned.addAll([for (final ip in ts.ips) url(ip)]);
        if (ts.magicDns && ts.dnsName.isNotEmpty) learned.add(url(ts.dnsName));
        routes = ts.subnetRoutes;
        if (ts.hostName.isNotEmpty) hostName = ts.hostName;
      }
    } catch (_) {}

    try {
      final res = await api.get('/sys/network-interfaces');
      final data = res['data'];
      if (data is List) {
        authoritative.add(RouteKind.home);
        for (final i in data) {
          final ip = i is Map ? i['ip']?.toString() ?? '' : '';
          final p = RouteRules.parseV4(ip);
          // The real LAN only: not Docker's or libvirt's bridges.
          if (p == null || !RouteRules.isHomeHost(ip) || p[0] == 127 || p[0] == 169) continue;
          if (p[0] == 172 && p[1] >= 17 && p[1] <= 31) continue;
          if (p[0] == 192 && p[1] == 168 && p[2] == 122) continue;
          learned.add(url(ip));
        }
      }
    } catch (_) {}

    if (learned.isEmpty && routes == null) return;
    final saved = await StorageService.instance.updateActiveProfile((p) => p.copyWith(
          addresses: RouteRules.merge(p.effectiveAddresses, learned, authoritative: authoritative, ignored: p.ignoredAddresses, keep: p.url),
          subnetRoutes: routes ?? p.subnetRoutes,
          hostName: hostName,
          learnedAt: clock.now(),
        ));
    if (saved != null) _setProfile(saved, saved.url);
  }

  /// The parts of `GET /v1/tailscale/status` (`tailscale status --json`)
  /// the app uses; null when Tailscale isn't running on the server.
  static ({List<String> ips, String dnsName, bool magicDns, List<String> subnetRoutes, String hostName})? parseTailscale(Object? data) {
    if (data is! Map) return null;
    if (data['BackendState'] != 'Running') return null;
    final self = data['Self'] is Map ? data['Self'] as Map : const {};
    final ips = [
      for (final ip in (self['TailscaleIPs'] as List? ?? data['TailscaleIPs'] as List? ?? const []))
        if (RouteRules.parseV4(ip.toString()) != null) ip.toString(),
    ];
    final tailnet = data['CurrentTailnet'] is Map ? data['CurrentTailnet'] as Map : const {};
    final magic = tailnet['MagicDNSEnabled'] == true;
    final dns = (self['DNSName']?.toString() ?? '').replaceAll(RegExp(r'\.$'), '');
    final routes = [
      for (final r in (self['PrimaryRoutes'] as List? ?? const []))
        if (r.toString() != '0.0.0.0/0' && r.toString() != '::/0' && RouteRules.parseV4(r.toString().split('/').first) != null) r.toString(),
    ];
    return (ips: ips, dnsName: dns, magicDns: magic, subnetRoutes: routes, hostName: self['HostName']?.toString() ?? '');
  }

  /// Saves the user's own list for the active server (the addresses
  /// screen), and uses it from now on.
  Future<void> saveAddresses(List<ServerAddress> addresses, {List<String> ignored = const []}) async {
    final saved = await StorageService.instance.updateActiveProfile((p) => p.copyWith(
          addresses: addresses,
          ignoredAddresses: {...p.ignoredAddresses, ...ignored}.toList(),
        ));
    if (saved == null) return;
    _setProfile(saved, saved.url);
    if (!_candidates.any((c) => RouteRules.same(c, ApiClient.instance.baseUrl))) {
      ApiClient.instance.useRoute(_candidates.first);
    }
    unawaited(select());
  }

  // -------------------------------------------------------------------
  // Watching (UI engine)
  // -------------------------------------------------------------------

  static const _networkChannel = EventChannel('com.fenyx.nivaroos/network_events');

  /// Checks again when the app comes back to the front and when the
  /// phone's network changes (Wi-Fi to mobile data, Tailscale on or off).
  /// Once per engine.
  void startWatching() {
    if (_watching) return;
    _watching = true;
    WidgetsBinding.instance.addObserver(this);
    try {
      _networkEvents = _networkChannel.receiveBroadcastStream().listen((_) {
        _networkDebounce?.cancel();
        _networkDebounce = Timer(const Duration(milliseconds: 800), () => unawaited(select()));
      }, onError: (_) {});
    } catch (_) {}
  }

  @visibleForTesting
  void stopWatching() {
    if (!_watching) return;
    _watching = false;
    WidgetsBinding.instance.removeObserver(this);
    _networkEvents?.cancel();
    _networkEvents = null;
    _networkDebounce?.cancel();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed || _candidates.isEmpty) return;
    final last = _lastResumeCheck;
    if (last != null && clock.now().difference(last) < const Duration(seconds: 3)) return;
    _lastResumeCheck = clock.now();
    unawaited(select());
  }

  // -------------------------------------------------------------------
  // Tailscale on the phone
  // -------------------------------------------------------------------

  static const _appChannel = MethodChannel('com.fenyx.nivaroos/network');

  /// Opens the Tailscale app (or its store page when it isn't installed).
  /// False when neither could be opened.
  static Future<bool> openTailscale() async {
    try {
      return await _appChannel.invokeMethod<bool>('openTailscale') ?? false;
    } catch (_) {
      return false;
    }
  }
}
