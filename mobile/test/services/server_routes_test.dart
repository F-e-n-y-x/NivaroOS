// Works at home and away (ServerRoutes): which of a server's addresses the
// app uses, failing over when one stops answering, WebSocket URLs following
// the switch, and the addresses learned from the server.
import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/models/server_profile.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/server_routes.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/widgets/connection_banner.dart';

const _home = 'http://192.168.1.20';
const _tailscale = 'http://100.64.0.1';
const _public = 'https://nas.example.com';

/// Signed in to a server with [addresses] saved (none: an older profile
/// with only its URL), plus whatever [extra] storage says.
Future<void> _signedIn({List<ServerAddress> addresses = const [], Map<String, String> extra = const {}}) async {
  StorageService.instance.resetForTest();
  ServerRoutes.instance.reset();
  FlutterSecureStorage.setMockInitialValues({
    'server_url': _home,
    'access_token': 'access',
    'refresh_token': 'refresh',
    'username': 'alex',
    'active_profile_id': 'srv_home',
    'saved_server_profiles': jsonEncode([
      ServerProfile(id: 'srv_home', name: 'Home', url: _home, username: 'alex', accessToken: 'access', refreshToken: 'refresh', addresses: addresses)
          .toJson(),
    ]),
    ...extra,
  });
  await StorageService.instance.init();
  await ApiClient.instance.init();
}

/// A probe that answers for [up] only, after [delay] (per address).
Future<RouteProbe> Function(String, Duration) _probe(Set<String> up, {Map<String, Duration> delay = const {}}) => (base, timeout) async {
      await Future<void>.delayed(delay[base] ?? Duration.zero);
      return RouteProbe(base, up.contains(base), ms: up.contains(base) ? 12 : null);
    };

http.Response _json(Object body, [int status = 200]) => http.Response(jsonEncode(body), status, headers: {'content-type': 'application/json'});

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    ServerRoutes.readNetwork = () async => PhoneNetwork.unknown;
  });
  tearDown(() {
    ServerRoutes.probe = ServerRoutes.ping;
    ServerRoutes.readNetwork = PhoneNetwork.read;
  });

  group('address kinds', () {
    test('home, Tailscale and internet addresses', () {
      expect(RouteRules.kindOf(_home), RouteKind.home);
      expect(RouteRules.kindOf('http://nas.local'), RouteKind.home);
      expect(RouteRules.kindOf('http://10.0.0.5:8080'), RouteKind.home);
      expect(RouteRules.kindOf(_tailscale), RouteKind.tailscale);
      expect(RouteRules.kindOf('http://100.127.255.1'), RouteKind.tailscale);
      expect(RouteRules.kindOf('http://atom.tailnet-x.ts.net'), RouteKind.tailscale);
      expect(RouteRules.kindOf(_public), RouteKind.internet);
      // 100.63/100.128 are outside Tailscale's range.
      expect(RouteRules.kindOf('http://100.63.0.1'), RouteKind.internet);
      expect(RouteRules.kindOf('http://100.128.0.1'), RouteKind.internet);
    });

    test('a home address reached through Tailscale away from home is Tailscale', () {
      const routes = ['192.168.1.0/24'];
      const atHome = PhoneNetwork(localIps: ['192.168.1.50'], tailscaleUp: true);
      const away = PhoneNetwork(localIps: ['10.44.0.7'], tailscaleUp: true);
      const awayNoTailscale = PhoneNetwork(localIps: ['10.44.0.7']);
      expect(RouteRules.describe(_home, net: atHome, subnetRoutes: routes), RouteKind.home);
      expect(RouteRules.describe(_home, net: away, subnetRoutes: routes), RouteKind.tailscale);
      expect(RouteRules.describe(_home, net: awayNoTailscale, subnetRoutes: routes), RouteKind.home);
      // Without known subnet routes: another /24 while Tailscale is up.
      expect(RouteRules.describe(_home, net: away), RouteKind.tailscale);
      expect(RouteRules.describe(_home, net: atHome), RouteKind.home);
      // Unknown network: by the address alone.
      expect(RouteRules.describe(_home), RouteKind.home);
      expect(RouteRules.describe(_tailscale, net: atHome), RouteKind.tailscale);
      expect(RouteRules.describe(_public, net: away), RouteKind.internet);
    });

    test("the phone's Tailscale is its tunnel, not a carrier's 100.64/10 address", () {
      final cgnat = PhoneNetwork.fromInterfaces([
        (name: 'rmnet_data0', addresses: ['100.70.1.2']),
        (name: 'wlan0', addresses: ['192.168.1.50']),
      ]);
      expect(cgnat.tailscaleUp, isFalse);
      expect(cgnat.localIps, ['100.70.1.2', '192.168.1.50']);
      final tailscale = PhoneNetwork.fromInterfaces([
        (name: 'rmnet_data0', addresses: ['10.44.0.7']),
        (name: 'tun0', addresses: ['100.64.0.9', 'fd7a:115c:a1e0::9']),
      ]);
      expect(tailscale.tailscaleUp, isTrue);
      expect(tailscale.localIps, ['10.44.0.7']);
    });

    test('CIDR and rebase', () {
      expect(RouteRules.inCidr('192.168.1.20', '192.168.1.0/24'), isTrue);
      expect(RouteRules.inCidr('192.168.2.20', '192.168.1.0/24'), isFalse);
      expect(RouteRules.inCidr('10.1.2.3', '10.0.0.0/8'), isTrue);
      expect(RouteRules.rebase('$_home/v1/file?path=/a', _home, _tailscale), '$_tailscale/v1/file?path=/a');
      expect(RouteRules.rebase('$_home/', '$_home/', _public), '$_public/');
      // Not under the old address: left alone.
      expect(RouteRules.rebase('http://192.168.1.200/x', _home, _tailscale), 'http://192.168.1.200/x');
      expect(RouteRules.rebase('https://cdn.example.com/icon.png', _home, _tailscale), 'https://cdn.example.com/icon.png');
    });
  });

  group('learned addresses', () {
    test('go in by kind after the typed ones: home, then Tailscale, then internet', () {
      final merged = RouteRules.merge(
        const [ServerAddress(_home), ServerAddress(_public)],
        [_tailscale, 'http://192.168.1.21'],
      );
      expect(merged.map((a) => a.url), [_home, 'http://192.168.1.21', _tailscale, _public]);
      expect(merged.where((a) => a.learned).map((a) => a.url), ['http://192.168.1.21', _tailscale]);
    });

    test("keep the user's order, skip removed ones, replace stale Tailscale ones", () {
      final merged = RouteRules.merge(
        const [ServerAddress(_public), ServerAddress(_home), ServerAddress('http://100.64.0.99', learned: true)],
        [_tailscale, 'http://192.168.1.21'],
        authoritative: {RouteKind.tailscale},
        ignored: ['http://192.168.1.21'],
      );
      expect(merged.map((a) => a.url), [_public, _home, _tailscale]);
    });

    test('never drop the main address or one the user typed', () {
      final merged = RouteRules.merge(
        const [ServerAddress(_tailscale), ServerAddress('http://100.64.0.5')],
        const [],
        authoritative: {RouteKind.tailscale},
        keep: _tailscale,
      );
      expect(merged.map((a) => a.url), [_tailscale, 'http://100.64.0.5']);
    });

    test('from the server: Tailscale address, MagicDNS name, subnet routes and the real LAN', () async {
      await _signedIn();
      await http.runWithClient(() => ServerRoutes.instance.learn(), () => MockClient((r) async {
            return switch (r.url.path) {
              '/v1/gateway/port' => _json({'data': '80'}),
              '/v1/tailscale/status' => _json({
                  'data': {
                    'BackendState': 'Running',
                    'CurrentTailnet': {'MagicDNSEnabled': true},
                    'Self': {
                      'HostName': 'atom',
                      'DNSName': 'atom.tailnet-x.ts.net.',
                      'TailscaleIPs': ['100.64.0.1', 'fd7a:115c:a1e0::1'],
                      'PrimaryRoutes': ['192.168.1.0/24'],
                    },
                  },
                }),
              '/v1/sys/network-interfaces' => _json({
                  'data': [
                    {'interface': 'br0', 'ip': '192.168.1.20'},
                    {'interface': 'docker0', 'ip': '172.17.0.1'},
                    {'interface': 'virbr0', 'ip': '192.168.122.1'},
                  ],
                }),
              _ => _json({'message': 'not found'}, 404),
            };
          }));
      final p = (await StorageService.instance.getActiveProfile())!;
      expect(p.addresses.map((a) => a.url), [_home, _tailscale, 'http://atom.tailnet-x.ts.net']);
      expect(p.subnetRoutes, ['192.168.1.0/24']);
      expect(p.hostName, 'atom');
      expect(p.learnedAt, isNotNull);
      expect(ServerRoutes.instance.candidates, [_home, _tailscale, 'http://atom.tailnet-x.ts.net']);
      expect(ServerRoutes.instance.state.value.hasTailscaleAddress, isTrue);
    });

    test('no MagicDNS name when MagicDNS is off on the tailnet', () {
      final ts = ServerRoutes.parseTailscale({
        'BackendState': 'Running',
        'CurrentTailnet': {'MagicDNSEnabled': false},
        'Self': {'DNSName': 'atom.tailnet-x.ts.net.', 'TailscaleIPs': ['100.64.0.1'], 'PrimaryRoutes': ['0.0.0.0/0', '192.168.1.0/24']},
      })!;
      expect(ts.magicDns, isFalse);
      expect(ts.ips, ['100.64.0.1']);
      expect(ts.subnetRoutes, ['192.168.1.0/24']);
      expect(ServerRoutes.parseTailscale({'BackendState': 'Stopped'}), isNull);
    });
  });

  group('choosing an address', () {
    test('the first one in order that answers', () async {
      ServerRoutes.probe = _probe({_tailscale, _public});
      expect(await ServerRoutes.pickFirst([_home, _tailscale, _public], const Duration(seconds: 1)), _tailscale);
      ServerRoutes.probe = _probe({_home, _tailscale, _public});
      expect(await ServerRoutes.pickFirst([_home, _tailscale, _public], const Duration(seconds: 1)), _home);
      ServerRoutes.probe = _probe({});
      expect(await ServerRoutes.pickFirst([_home, _tailscale], const Duration(seconds: 1)), isNull);
    });

    test('a preferred address answering a moment later still wins', () async {
      ServerRoutes.probe = _probe({_home, _tailscale}, delay: {_home: const Duration(milliseconds: 200)});
      expect(await ServerRoutes.pickFirst([_home, _tailscale], const Duration(seconds: 2)), _home);
    });

    test("one that doesn't answer in time doesn't hold up the others", () async {
      ServerRoutes.probe = _probe({_tailscale}, delay: {_home: const Duration(seconds: 5)});
      final sw = Stopwatch()..start();
      expect(await ServerRoutes.pickFirst([_home, _tailscale], const Duration(seconds: 5)), _tailscale);
      expect(sw.elapsed, lessThan(ServerRoutes.preferredGrace + const Duration(milliseconds: 500)));
    });

    test('away from home: Tailscale is used, saved, and WebSockets follow', () async {
      await _signedIn(addresses: const [ServerAddress(_home), ServerAddress(_tailscale, learned: true), ServerAddress(_public)]);
      ServerRoutes.probe = _probe({_tailscale, _public});
      ServerRoutes.readNetwork = () async => const PhoneNetwork(localIps: ['10.44.0.7'], tailscaleUp: true);
      expect(ApiClient.instance.webSocketUri('/v1/terminal/ws').toString(), 'ws://192.168.1.20/v1/terminal/ws');

      expect(await ServerRoutes.instance.select(), _tailscale);
      expect(ApiClient.instance.baseUrl, _tailscale);
      expect(ApiClient.instance.webSocketUri('/v1/terminal/ws', {'cols': '80'}).toString(), 'ws://100.64.0.1/v1/terminal/ws?cols=80');
      expect(ApiClient.instance.buildUri('/image', {'path': '/DATA/a.jpg'}).toString(), 'http://100.64.0.1/v1/image?path=%2FDATA%2Fa.jpg');
      final s = ServerRoutes.instance.state.value;
      expect(s.status, RouteStatus.connected);
      expect(s.kind, RouteKind.tailscale);
      expect(await StorageService.instance.getActiveRoute(), _tailscale);

      // The public URL: wss.
      ServerRoutes.probe = _probe({_public});
      expect(await ServerRoutes.instance.select(), _public);
      expect(ApiClient.instance.webSocketUri('/v2/message_bus/event/x').toString(), 'wss://nas.example.com/v2/message_bus/event/x');
      expect(ServerRoutes.instance.state.value.kind, RouteKind.internet);

      // Back home, on the home Wi-Fi.
      ServerRoutes.probe = _probe({_home, _tailscale, _public});
      ServerRoutes.readNetwork = () async => const PhoneNetwork(localIps: ['192.168.1.50'], tailscaleUp: true);
      expect(await ServerRoutes.instance.select(), _home);
      expect(ServerRoutes.instance.state.value.kind, RouteKind.home);
    });

    test('a new engine (sharing, heartbeat) starts on the address that worked last', () async {
      await _signedIn(
        addresses: const [ServerAddress(_home), ServerAddress(_tailscale)],
        extra: {'active_route@$_home': _tailscale},
      );
      expect(ApiClient.instance.baseUrl, _tailscale);
      // A stale saved address that is no longer one of the server's is ignored.
      await _signedIn(addresses: const [ServerAddress(_home)], extra: {'active_route@$_home': 'http://100.64.0.77'});
      expect(ApiClient.instance.baseUrl, _home);
    });

    test('nothing answers: unreachable, and the Tailscale hint when the phone has it off', () async {
      await _signedIn(addresses: const [ServerAddress(_home), ServerAddress(_tailscale)]);
      ServerRoutes.probe = _probe({});
      ServerRoutes.readNetwork = () async => const PhoneNetwork(localIps: ['10.44.0.7']);
      expect(await ServerRoutes.instance.select(), isNull);
      final s = ServerRoutes.instance.state.value;
      expect(s.status, RouteStatus.unreachable);
      expect(s.suggestTailscale, isTrue);
      expect(ApiClient.instance.baseUrl, _home); // unchanged
      final problem = connectionProblem(s)!;
      expect(problem.title, "Can't reach Home — turn on Tailscale");
      // With the name learned from Tailscale: "atom".
      await StorageService.instance.updateActiveProfile((p) => p.copyWith(hostName: 'atom'));
      await ServerRoutes.instance.load();
      await ServerRoutes.instance.select();
      expect(connectionProblem(ServerRoutes.instance.state.value)!.title, "Can't reach atom — turn on Tailscale");
      expect(problem.openTailscale, isTrue);

      ServerRoutes.readNetwork = () async => const PhoneNetwork(localIps: ['10.44.0.7'], tailscaleUp: true);
      await ServerRoutes.instance.select();
      expect(connectionProblem(ServerRoutes.instance.state.value)!.openTailscale, isFalse);
    });

    test('no hint for a server without a Tailscale address', () {
      const s = RouteState(status: RouteStatus.unreachable, phoneTailscaleUp: false);
      expect(connectionProblem(s), isNull);
      expect(connectionProblem(const RouteState(status: RouteStatus.connected, hasTailscaleAddress: true)), isNull);
    });
  });

  group('failover on a failed request', () {
    Future<http.Response> server(http.Request r, Set<String> up, List<String> seen) async {
      seen.add('${r.method} ${r.url.host}${r.url.path}');
      if (!up.contains('${r.url.scheme}://${r.url.host}')) throw const SocketException('No route to host');
      if (r.url.path == '/ping') return http.Response('pong from gateway service', 200);
      return _json({'data': 'ok from ${r.url.host}'});
    }

    test('a GET that cannot reach the home address is sent again over Tailscale', () async {
      await _signedIn(addresses: const [ServerAddress(_home), ServerAddress(_tailscale)]);
      final seen = <String>[];
      final res = await http.runWithClient(() => ApiClient.instance.get('/sys/utilization'), () => MockClient((r) => server(r, {_tailscale}, seen)));
      expect(res['data'], 'ok from 100.64.0.1');
      expect(ApiClient.instance.baseUrl, _tailscale);
      expect(seen.first, 'GET 192.168.1.20/v1/sys/utilization');
      expect(seen.last, 'GET 100.64.0.1/v1/sys/utilization');
    });

    test('a POST whose connection failed is sent again too', () async {
      await _signedIn(addresses: const [ServerAddress(_home), ServerAddress(_tailscale)]);
      final seen = <String>[];
      final res = await http.runWithClient(() => ApiClient.instance.post('/companion/register', body: {'id': 'x'}),
          () => MockClient((r) => server(r, {_tailscale}, seen)));
      expect(res['data'], 'ok from 100.64.0.1');
      expect(seen.where((s) => s.startsWith('POST')).toList(), ['POST 192.168.1.20/v1/companion/register', 'POST 100.64.0.1/v1/companion/register']);
    });

    test('a server with one address: the error is reported as before', () async {
      await _signedIn();
      final seen = <String>[];
      await expectLater(
        http.runWithClient(() => ApiClient.instance.get('/sys/utilization'), () => MockClient((r) => server(r, {}, seen))),
        throwsA(isA<ApiException>().having((e) => e.isUnreachable, 'unreachable', isTrue)),
      );
      expect(seen, ['GET 192.168.1.20/v1/sys/utilization']);
    });

    test('sign-in away from home uses the address that answers', () async {
      await _signedIn(addresses: const [ServerAddress(_home), ServerAddress(_tailscale)]);
      final seen = <String>[];
      final tokens = await http.runWithClient(() => ApiClient.instance.login('alex', 'pw'), () => MockClient((r) async {
            if (r.url.path == '/v1/users/login') {
              await server(r, {_tailscale}, seen);
              return _json({
                'data': {'token': {'access_token': 'a2', 'refresh_token': 'r2'}},
              });
            }
            return server(r, {_tailscale}, seen);
          }));
      expect(tokens.accessToken, 'a2');
      expect(ApiClient.instance.baseUrl, _tailscale);
    });

    test('a hung GET moves to another address once the one in use stops answering', () async {
      await _signedIn(addresses: const [ServerAddress(_home), ServerAddress(_tailscale)]);
      ServerRoutes.probe = _probe({_tailscale});
      final hang = Completer<http.Response>();
      final res = await http.runWithClient(
        () => ApiClient.instance.get('/sys/utilization'),
        () => MockClient((r) => r.url.host == '192.168.1.20' ? hang.future : Future.value(_json({'data': 'ok from ${r.url.host}'}))),
      );
      expect(res['data'], 'ok from 100.64.0.1');
      expect(ApiClient.instance.baseUrl, _tailscale);
    }, timeout: const Timeout(Duration(seconds: 30)));
  });

  group('profiles', () {
    test('addresses round-trip; an older profile has just its URL', () {
      final p = ServerProfile(
        id: 'a',
        name: 'Home',
        url: _home,
        username: 'alex',
        addresses: const [ServerAddress(_home), ServerAddress(_tailscale, learned: true)],
        ignoredAddresses: const ['http://100.64.0.9'],
        subnetRoutes: const ['192.168.1.0/24'],
        hostName: 'atom',
      );
      final back = ServerProfile.fromJson(jsonDecode(jsonEncode(p.toJson())) as Map<String, dynamic>);
      expect(back.addresses, p.addresses);
      expect(back.ignoredAddresses, p.ignoredAddresses);
      expect(back.subnetRoutes, p.subnetRoutes);
      expect(back.hostName, 'atom');
      // Tokens gone, addresses kept.
      expect(back.withoutTokens().addresses, p.addresses);

      final old = ServerProfile.fromJson({'id': 'b', 'name': '', 'url': _home, 'username': 'alex'});
      expect(old.addresses, isEmpty);
      expect(old.effectiveAddresses.map((a) => a.url), [_home]);
      expect(old.toJson().containsKey('addresses'), isFalse);
    });

    test('a refreshed session keeps the addresses', () async {
      await _signedIn(addresses: const [ServerAddress(_home), ServerAddress(_tailscale)]);
      await StorageService.instance.setSession(accessToken: 'new', refreshToken: 'new-r', username: 'alex');
      final p = (await StorageService.instance.getActiveProfile())!;
      expect(p.accessToken, 'new');
      expect(p.addresses.map((a) => a.url), [_home, _tailscale]);
    });
  });
}
