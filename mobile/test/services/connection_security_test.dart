import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/connection_security.dart';

void main() {
  group('ConnectionSecurity.of', () {
    final cases = {
      'https://nas.example.com': TransportSecurity.https,
      'https://203.0.113.9:8443': TransportSecurity.https,
      'http://192.168.1.20': TransportSecurity.localNetwork,
      'http://10.0.0.5:8080': TransportSecurity.localNetwork,
      'http://172.16.4.1': TransportSecurity.localNetwork,
      'http://172.32.0.1': TransportSecurity.insecure,
      'http://169.254.1.1': TransportSecurity.localNetwork,
      'http://127.0.0.1': TransportSecurity.localNetwork,
      'http://nas.local': TransportSecurity.localNetwork,
      'http://nas.home.arpa': TransportSecurity.localNetwork,
      'http://nas': TransportSecurity.localNetwork,
      'http://100.64.0.1': TransportSecurity.tailscale,
      'http://100.127.255.254': TransportSecurity.tailscale,
      'http://100.128.0.1': TransportSecurity.insecure,
      'http://nas.tail1234.ts.net': TransportSecurity.tailscale,
      'http://[fd7a:115c:a1e0::1]': TransportSecurity.tailscale,
      'http://[fd12:3456::1]': TransportSecurity.localNetwork,
      'http://[fe80::1%25wlan0]': TransportSecurity.localNetwork,
      'http://[::ffff:192.168.1.20]': TransportSecurity.localNetwork,
      'http://[2001:db8::1]': TransportSecurity.insecure,
      'http://203.0.113.9': TransportSecurity.insecure,
      'http://nas.example.com': TransportSecurity.insecure,
      '192.168.1.20:8080': TransportSecurity.localNetwork,
      '': TransportSecurity.insecure,
    };
    cases.forEach((url, want) {
      test('$url -> ${want.name}', () => expect(ConnectionSecurity.of(url), want));
    });

    test('only plain http to the internet refuses secrets', () {
      expect([for (final t in TransportSecurity.values) if (!t.allowsSecrets) t], [TransportSecurity.insecure]);
    });
  });

  group('ConnectionSecurity.resolve', () {
    Future<List<InternetAddress>> Function(String) to(List<String> ips) => (_) async => [for (final ip in ips) InternetAddress(ip)];

    test('a public name that resolves only to home addresses counts as the home network (split DNS)', () async {
      expect(await ConnectionSecurity.resolve('http://nas.example.com', lookup: to(['192.168.1.20'])), TransportSecurity.localNetwork);
    });

    test('a name resolving to Tailscale counts as Tailscale', () async {
      expect(await ConnectionSecurity.resolve('http://nas.example.com', lookup: to(['100.101.102.103'])), TransportSecurity.tailscale);
    });

    test('any public address makes it insecure', () async {
      expect(await ConnectionSecurity.resolve('http://nas.example.com', lookup: to(['192.168.1.20', '203.0.113.9'])), TransportSecurity.insecure);
    });

    test('a failed lookup keeps the address-only answer', () async {
      expect(await ConnectionSecurity.resolve('http://nas.example.com', lookup: (_) async => throw const SocketException('no dns')), TransportSecurity.insecure);
    });

    test('https and private addresses are never looked up', () async {
      Future<List<InternetAddress>> never(String _) => fail('looked up');
      expect(await ConnectionSecurity.resolve('https://nas.example.com', lookup: never), TransportSecurity.https);
      expect(await ConnectionSecurity.resolve('http://192.168.1.20', lookup: never), TransportSecurity.localNetwork);
    });
  });
}
