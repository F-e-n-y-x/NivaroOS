import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/companion_file_server.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';

// Minted by the server's companionPeerTicket for secret "s3cret", phone
// "phoneA", peer "phoneB", read of /storage/emulated/0/DCIM/x.mp4, expiring
// at 1800000000 (TestCompanionPeerTicketVector checks the same string).
const vector =
    'djEKMTgwMDAwMDAwMApyZWFkCnBob25lQgovc3RvcmFnZS9lbXVsYXRlZC8wL0RDSU0veC5tcDQ.12217092fc8e73bf0fe26a24db9c069536591a34335071a8303872f46352e180';
const file = '/storage/emulated/0/DCIM/x.mp4';
final before = DateTime.fromMillisecondsSinceEpoch(1799990000 * 1000);

bool allows(String ticket, {String method = 'GET', String endpoint = '/download', String? path = file, DateTime? now, String secret = 's3cret'}) =>
    CompanionFileServer.peerTicketAllows(ticket, secret: secret, deviceId: 'phoneA', method: method, endpoint: endpoint, path: path, now: now ?? before);

void main() {
  test("the server's ticket opens exactly its file, read-only, until it expires", () {
    expect(allows(vector), isTrue);
    expect(allows(vector, method: 'HEAD'), isTrue);
    expect(allows(vector, now: DateTime.fromMillisecondsSinceEpoch(1800000000 * 1000)), isFalse, reason: 'expired');
    expect(allows(vector, path: '/storage/emulated/0/DCIM/y.mp4'), isFalse, reason: 'another file');
    expect(allows(vector, path: '/storage/emulated/0/DCIM'), isFalse, reason: 'its folder');
    expect(allows(vector, path: null), isFalse, reason: 'a refused path (.. or outside shared storage)');
    for (final m in ['POST', 'PUT', 'DELETE']) {
      expect(allows(vector, method: m), isFalse, reason: m);
    }
    for (final e in ['/files', '/upload', '/delete', '/rename', '/mkdir']) {
      expect(allows(vector, endpoint: e), isFalse, reason: e);
    }
    expect(allows(vector, secret: 'other'), isFalse, reason: "another phone's secret");
    final tampered = vector.replaceFirst('cmVhZA', 'bGlzdA'); // read -> list
    expect(allows(tampered, endpoint: '/files', path: '/storage/emulated/0/DCIM/x.mp4'), isFalse);
    expect(allows('garbage'), isFalse);
    expect(allows(''), isFalse);
  });

  group('on the file server', () {
    late HttpServer lan, tailnet;
    setUp(() async {
      FlutterSecureStorage.setMockInitialValues({});
      await StorageService.instance.setServerUrl('http://nas.test');
      await StorageService.instance.setCompanionSecret('s3cret');
      await StorageService.instance.setCompanionDeviceId('phoneA');
      lan = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      lan.listen((r) => CompanionFileServer.instance.serveForTest(r, viaTailnet: false));
      tailnet = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      tailnet.listen((r) => CompanionFileServer.instance.serveForTest(r, viaTailnet: true));
    });
    tearDown(() async {
      await lan.close(force: true);
      await tailnet.close(force: true);
    });

    Future<int> get(HttpServer s, String ticket, {String path = file, String method = 'GET'}) async {
      final c = HttpClient();
      try {
        final req = await c.openUrl(method, Uri.parse('http://127.0.0.1:${s.port}/download').replace(queryParameters: {'path': path}));
        req.headers.set('X-Peer-Ticket', ticket);
        final res = await req.close();
        await res.drain<void>();
        return res.statusCode;
      } finally {
        c.close(force: true);
      }
    }

    test('only over Tailscale, never on the Wi-Fi listener', () async {
      // A fresh ticket for "now" (the vector's expiry is fixed).
      final ticket = mint(DateTime.now().add(const Duration(hours: 2)));
      expect(await get(lan, ticket), 401);
      expect(await get(tailnet, ticket), 404, reason: 'let in; the file just is not on this test machine');
      expect(await get(tailnet, ticket, path: '/storage/emulated/0/DCIM/../DCIM/x.mp4'), 404, reason: 'the same file');
      expect(await get(tailnet, ticket, path: '/storage/emulated/0/DCIM/../Download/x.mp4'), 401);
      expect(await get(tailnet, ticket, method: 'POST'), 401);
      expect(await get(tailnet, mint(DateTime.now().subtract(const Duration(seconds: 1)))), 401);
    });
  });
}

/// The server's companionPeerTicket, for a test expiry.
String mint(DateTime exp) {
  final payload = 'v1\n${exp.millisecondsSinceEpoch ~/ 1000}\nread\nphoneB\n$file';
  final mac = Hmac(sha256, utf8.encode('s3cret')).convert(utf8.encode('nivaroos-peer\nphoneA\n$payload'));
  return '${base64Url.encode(utf8.encode(payload)).replaceAll('=', '')}.$mac';
}
