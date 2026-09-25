// Regression tests for the stage-2 review fixes (code review 2026-09-26).
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/dashboard_stats.dart';
import 'package:nivaroos_mobile/models/server_profile.dart';
import 'package:nivaroos_mobile/screens/apps_screen.dart';
import 'package:nivaroos_mobile/screens/dashboard_screen.dart';
import 'package:nivaroos_mobile/services/companion_file_server.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import '../screenshots/harness.dart' show testApp;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('uploads land in a short hidden temporary file next to the target (finding 2)', () {
    final name = '${'a' * 250}.jpg';
    final temp = CompanionFileServer.uploadTempPath('/storage/emulated/0/Download/$name');
    expect(temp, startsWith('/storage/emulated/0/Download/.nvupload-'));
    expect(temp.split('/').last.length, lessThan(40));
  });

  group('saved servers are changed in storage, not in a stale copy (finding 8)', () {
    setUp(() {
      StorageService.instance.resetForTest();
      FlutterSecureStorage.setMockInitialValues({
        'server_url': 'http://a.test',
        'access_token': 't',
        'refresh_token': 'r',
        'saved_server_profiles': jsonEncode([ServerProfile(id: 'a', name: 'A', url: 'http://a.test', username: 'u').toJson()]),
      });
    });

    test('a profile another engine added survives a token refresh here', () async {
      await StorageService.instance.init();
      // The UI engine adds a server while this (sharing) engine runs.
      const storage = FlutterSecureStorage();
      await storage.write(
        key: 'saved_server_profiles',
        value: jsonEncode([
          ServerProfile(id: 'a', name: 'A', url: 'http://a.test', username: 'u').toJson(),
          ServerProfile(id: 'b', name: 'B', url: 'http://b.test', username: 'u').toJson(),
        ]),
      );
      await StorageService.instance.setSession(accessToken: 't2', refreshToken: 'r2', username: 'u');
      final saved = jsonDecode((await storage.read(key: 'saved_server_profiles'))!) as List;
      expect(saved.map((p) => (p as Map)['id']), containsAll(['a', 'b']));
    });

    test('a sign-out elsewhere is seen before a refresh writes tokens back (finding 9)', () async {
      await StorageService.instance.init();
      expect(await StorageService.instance.hasStoredSession(), isTrue);
      await const FlutterSecureStorage().delete(key: 'refresh_token');
      expect(await StorageService.instance.hasStoredSession(), isFalse);
    });
  });

  test('app versions come from the image tag', () {
    expect(appVersion('linuxserver/jellyfin:10.11.10'), '10.11.10');
    expect(appVersion('ghcr.io/org/app:v2.1'), '2.1');
    expect(appVersion('searxng/searxng:latest'), isNull);
    expect(appVersion('localhost:5000/app'), isNull);
    expect(appVersion('nginx'), isNull);
  });

  test("Home's sparkline history keeps the last two minutes", () {
    final h = LiveHistory(capacity: 3);
    for (var i = 0; i < 5; i++) {
      h.add(LiveStats(
        stats: DashboardStats.fromUtilization({'cpu': {'percent': i * 10}}),
        rate: NetRate(upBytesPerSec: 0, downBytesPerSec: i.toDouble()),
        updatedAt: DateTime(2026),
      ));
    }
    expect(h.cpu, hasLength(3));
    expect(h.netDown, [2, 3, 4]);
  });

  // Stage 3 (design brief §10): tabs no longer open with the medium bar's
  // 112dp band; every screen gets the small bar, tabs with a larger title.
  testWidgets('pushed screens get the small bar with a back arrow, tabs the small bar with a larger title', (tester) async {
    Widget page(String t) => AppScaffold.slivers(title: t, slivers: const [SliverToBoxAdapter(child: SizedBox(height: 10))]);
    await tester.pumpWidget(testApp(page('Root')));
    // The status bar inset is 0 here: the small bar is 64.
    double lowestTitle(String t) {
      final f = find.text(t);
      return [for (var i = 0; i < f.evaluate().length; i++) tester.getBottomLeft(f.at(i)).dy].reduce((a, b) => a > b ? a : b);
    }
    expect(lowestTitle('Root'), lessThan(64), reason: 'small bar on the root too');
    final rootSize = tester.widget<Text>(find.text('Root')).style?.fontSize;
    expect(find.byType(BackButton), findsNothing);
    Navigator.of(tester.element(find.text('Root').first)).push(MaterialPageRoute<void>(builder: (_) => page('Pushed')));
    await tester.pumpAndSettle();
    expect(find.byType(BackButton), findsOneWidget);
    expect(lowestTitle('Pushed'), lessThan(64), reason: 'small bar when pushed');
    expect(tester.widget<Text>(find.text('Pushed')).style?.fontSize, isNot(rootSize), reason: 'the root title is a size up');
  });
}
