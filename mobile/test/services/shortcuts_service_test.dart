import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/services/shortcuts_service.dart';

import '../screenshots/harness.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(signIn);

  test("a failed read never overwrites the saved shortcuts", () async {
    final posts = <String>[];
    final ok = await http.runWithClient(
      () => ShortcutsService.instance.toggleFavorite('Photos', '/DATA/Photos'),
      () => MockClient((req) async {
        if (req.method == 'POST') posts.add(req.body);
        return http.Response(jsonEncode({'success': 500, 'message': 'boom'}), 500);
      }),
    );
    expect(ok, isFalse);
    expect(posts, isEmpty);
  });

  test('toggling adds to the list that is already saved', () async {
    final posts = <String>[];
    final saved = [
      {'name': 'Music', 'path': '/DATA/Music', 'icon': 'folder-outline'},
    ];
    final ok = await http.runWithClient(
      () => ShortcutsService.instance.toggleFavorite('Photos', '/DATA/Photos'),
      () => MockClient((req) async {
        if (req.method == 'POST') {
          posts.add(req.body);
          return http.Response(jsonEncode({'success': 200, 'message': 'ok'}), 200);
        }
        return http.Response(jsonEncode({'success': 200, 'message': 'ok', 'data': saved}), 200);
      }),
    );
    expect(ok, isTrue);
    final body = jsonDecode(posts.single) as List;
    expect(body.map((e) => (e as Map)['path']), ['/DATA/Music', '/DATA/Photos']);
  });
}
