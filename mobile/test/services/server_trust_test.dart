import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:nivaroos_mobile/services/media_relay.dart';
import 'package:nivaroos_mobile/services/server_trust.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';

// Throwaway test-only CA and server cert (127.0.0.1, localhost), made with
// openssl for these tests; they protect nothing.
const _dir = 'test/fixtures/tls';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late HttpServer server;
  late String base;
  final caDer = File('$_dir/ca.der').readAsBytesSync();

  setUpAll(() async {
    StorageService.instance.resetForTest();
    FlutterSecureStorage.setMockInitialValues({});
    // The widget test binding fakes HttpClient (every request gets a 400);
    // these tests need real sockets, the way the app runs.
    HttpOverrides.global = null;
    await ServerTrust.instance.init();
    final ctx = SecurityContext()
      ..useCertificateChain('$_dir/server.crt')
      ..usePrivateKey('$_dir/server.key');
    server = await HttpServer.bindSecure(InternetAddress.loopbackIPv4, 0, ctx);
    base = 'https://127.0.0.1:${server.port}';
    server.listen((req) async {
      final res = req.response;
      switch (req.uri.path) {
        case '/v1/gateway/tls/ca.crt':
          res.add(caDer);
        case '/v1/users/status':
          res.headers.contentType = ContentType.json;
          res.write(jsonEncode({'data': {'initialized': true}}));
        case '/video':
          final range = req.headers.value('range');
          res.statusCode = range == 'bytes=2-4' ? 206 : 200;
          res.write(range == 'bytes=2-4' ? 'cde' : 'abcdef');
        default:
          res.statusCode = 404;
      }
      await res.close();
    });
  });

  tearDownAll(() => server.close(force: true));

  test('fingerprint is the SHA-256 the server shows (openssl -fingerprint -sha256)', () {
    expect(ServerTrust.fingerprint(caDer),
        '37:9C:C9:EA:7B:5E:FF:F6:81:95:4F:66:04:B4:A3:C9:64:83:56:68:BC:B1:F9:4F:18:7C:FD:E8:53:08:11:8A');
  });

  test('pem round-trips', () {
    expect(ServerTrust.toPem(caDer).trim(), File('$_dir/ca.crt').readAsStringSync().trim());
  });

  test('httpsBase keeps the host, drops port 443', () {
    expect(ServerTrust.httpsBase('http://192.168.1.20', '443'), 'https://192.168.1.20');
    expect(ServerTrust.httpsBase('http://nas.local:8080', '8443'), 'https://nas.local:8443');
  });

  test('untrusted until pinned, then checked for real; CA downloaded without trust', () async {
    // Before: package:http (through HttpOverrides) refuses the self-signed CA.
    await expectLater(http.get(Uri.parse('$base/v1/users/status')), throwsA(isA<HandshakeException>()));
    expect(ServerTrust.instance.hasPins, isFalse);

    final fetched = await ServerTrust.fetchCa(base);
    expect(fetched, caDer);
    await ServerTrust.instance.pin(fetched);
    expect(ServerTrust.instance.isPinned(Uint8List.fromList(caDer)), isTrue);

    final res = await http.get(Uri.parse('$base/v1/users/status'));
    expect(res.statusCode, 200);
    expect((await StorageService.instance.getPinnedCas()).single, contains('BEGIN CERTIFICATE'));
  });

  test('WebView proceeds only for the certificate the pinned connection sees', () async {
    final leaf = await SecureSocket.connect('127.0.0.1', server.port, context: ServerTrust.instance.context);
    final der = leaf.peerCertificate!.der;
    leaf.destroy();
    expect(await ServerTrust.instance.matchesServer(Uri.parse(base), der), isTrue);
    expect(await ServerTrust.instance.matchesServer(Uri.parse(base), Uint8List.fromList(caDer)), isFalse);
    expect(await ServerTrust.instance.matchesServer(Uri.parse(base), null), isFalse);
  });

  test('media relay plays https video from a pinned server over loopback, Range kept', () async {
    final url = Uri.parse('$base/video');
    // Only home-network/Tailscale hosts are relayed; 127.0.0.1 counts.
    final relayed = await MediaRelay.instance.wrapIfNeeded(url);
    expect(relayed.scheme, 'http');
    expect(relayed.host, '127.0.0.1');
    final res = await http.get(relayed, headers: {'Range': 'bytes=2-4'});
    expect(res.statusCode, 206);
    expect(res.body, 'cde');
    // Unknown paths are not an open proxy.
    final other = await http.get(relayed.replace(path: '/nope'));
    expect(other.statusCode, 404);
    expect(await MediaRelay.instance.wrapIfNeeded(Uri.parse('https://example.com/v')), Uri.parse('https://example.com/v'));
  });
}
