// The phone's 127.0.0.1:53682 listener during a cloud drive sign-in hands
// the provider's redirect to the server's relay unchanged, and nothing else.
// The "server" is a local fake; no real provider or server is reached.
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/services/cloud_signin.dart';

void main() {
  late HttpServer relay;
  late List<HttpRequest> relayed;
  late int relayStatus;
  late OAuthRedirectForwarder fwd;

  setUp(() async {
    relayed = [];
    relayStatus = 200;
    relay = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    relay.listen((req) async {
      relayed.add(req);
      req.response.statusCode = relayStatus;
      req.response.write('rclone says hi');
      await req.response.close();
    });
    fwd = await OAuthRedirectForwarder.start(Uri.parse('http://127.0.0.1:${relay.port}/v1/rclone-auth'), port: 0);
  });

  tearDown(() async {
    await fwd.close();
    await relay.close(force: true);
  });

  Future<(int, String)> browser(String pathAndQuery, {String method = 'GET'}) async {
    final c = HttpClient();
    try {
      final req = await c.openUrl(method, Uri.parse('http://127.0.0.1:${fwd.port}$pathAndQuery'));
      req.headers.set('Cookie', 'browser=1');
      req.headers.set('Authorization', 'Bearer not-for-the-server');
      final res = await req.close();
      return (res.statusCode, await res.transform(utf8.decoder).join());
    } finally {
      c.close(force: true);
    }
  }

  test('the redirect reaches the relay with the same path and query, and says signed in', () async {
    const query = '?state=Abc-123_x&code=4%2F0AQSTgQ&scope=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fdrive';
    final (status, page) = await browser('/$query');
    expect(status, 200);
    expect(page, contains('Signed in'));
    expect(page, contains('href="$cloudSignInReturnUrl"'));
    expect(relayed, hasLength(1));
    expect(relayed.single.method, 'GET');
    expect(relayed.single.uri.toString(), '/v1/rclone-auth/$query');
    expect(relayed.single.headers.value('cookie'), isNull);
    expect(relayed.single.headers.value('authorization'), isNull);
  });

  test('a refused sign-in says so and does not send the app back by itself', () async {
    relayStatus = 400;
    final (status, page) = await browser('/?state=wrong&code=x');
    expect(status, 200);
    expect(page, contains('Sign-in not finished'));
    expect(page, contains('HTTP 400'));
    expect(page, isNot(contains('<script>')));
  });

  test('nothing but GET / is passed on', () async {
    expect((await browser('/favicon.ico')).$1, 404);
    expect((await browser('/auth?state=x')).$1, 404);
    expect((await browser('/?code=x', method: 'POST')).$1, 404);
    expect(relayed, isEmpty);
  });

  test('the sign-in link is followed to the provider here, only to https', () async {
    final relayBase = Uri.parse('http://nivaro.test/v1/rclone-auth');
    var location = 'https://accounts.google.com/o/oauth2/auth?client_id=1&state=xyz';
    final client = MockClient((req) async {
      expect(req.url.toString(), 'http://nivaro.test/v1/rclone-auth/auth?state=xyz');
      expect(req.followRedirects, isFalse);
      return http.Response('', 307, headers: {'location': location});
    });
    expect((await providerSignInUrl(relayBase, '/auth?state=xyz', client: client)).toString(), location);
    location = 'http://evil.example/';
    await expectLater(providerSignInUrl(relayBase, '/auth?state=xyz', client: client), throwsException);
  });
}
