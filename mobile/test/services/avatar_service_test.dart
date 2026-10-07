import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/services/avatar_service.dart';

import '../screenshots/harness.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final png = File('test/screenshots/fixtures/v1/users/avatar.png').readAsBytesSync();
  final version = sha256.convert(png).toString().substring(0, 16);
  final avatars = AvatarService.instance;

  setUp(() async {
    await signIn();
    avatars.debugReset();
  });

  /// A server where alex's picture is [v] ('' = none); records requests.
  Future<List<String>> serve(String v, Future<void> Function() body) async {
    final seen = <String>[];
    await http.runWithClient(body, () => MockClient((req) async {
          seen.add('${req.method} ${req.url.path}${req.url.hasQuery ? '?${req.url.query}' : ''}');
          final user = {'id': 1, 'username': 'alex', 'avatar_version': req.method == 'DELETE' ? '' : v};
          if (req.method == 'PUT') {
            expect(base64Decode((jsonDecode(req.body) as Map)['file'] as String), png);
          }
          if (req.url.path == '/v1/users/avatar' && req.method == 'GET') return http.Response.bytes(png, 200);
          return http.Response(jsonEncode({'success': 200, 'data': user}), 200);
        }));
    return seen;
  }

  test('no picture on the server: none here', () async {
    final seen = await serve('', avatars.refresh);
    expect(avatars.current.value, isNull);
    expect(seen, ['GET /v1/users/current']);
  });

  test('fetched once per version, then kept for the server list and sign-in', () async {
    expect(await serve(version, avatars.refresh), ['GET /v1/users/current', 'GET /v1/users/avatar?v=$version']);
    expect(avatars.current.value, png);
    // Same version: the cached copy is current, no download.
    expect(await serve(version, avatars.refresh), ['GET /v1/users/current']);
    expect(await avatars.cachedFor(fakeServer, 'alex'), png);
    expect(await avatars.cachedFor(fakeServer, 'someone-else'), isNull);
    // A new version (set from the web): downloaded again.
    expect(await serve('0000000000000000', avatars.refresh), contains('GET /v1/users/avatar?v=0000000000000000'));
  });

  test('upload and remove', () async {
    await serve(version, () => avatars.upload(png));
    expect(avatars.current.value, png);
    await serve(version, avatars.remove);
    expect(avatars.current.value, isNull);
    expect(await avatars.cachedFor(fakeServer, 'alex'), isNull);
  });
}
