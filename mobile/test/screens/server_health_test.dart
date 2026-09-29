// Home's status header opens Server health: "All good" with what was
// checked, or what needs attention (the same list as Home's) with where to
// fix each, against the fake server.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:nivaroos_mobile/screens/dashboard_screen.dart';
import 'package:nivaroos_mobile/screens/server_health_screen.dart';
import 'package:nivaroos_mobile/screens/system_updates_screen.dart';

import '../screenshots/harness.dart';

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 8; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 200));
  }
}

Future<FakeServer> _run(WidgetTester tester, Widget screen, Future<void> Function() body, {Map<String, Object> overrides = const {}}) async {
  await loadRealFonts();
  tester.view.physicalSize = const Size(412, 915) * 3;
  tester.view.devicePixelRatio = 3;
  addTearDown(tester.view.reset);
  final server = FakeServer(overrides: overrides);
  await http.runWithClient(() async {
    await tester.pumpWidget(testApp(Scaffold(body: screen)));
    await _settle(tester);
    await body();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => server.client);
  return server;
}

/// Nothing to fix: no updates, no backup jobs, every app running.
final allGood = <String, Object>{
  'GET /v1/sys/packages/check': {
    'success': 200,
    'message': 'ok',
    'data': {'count': 0, 'security_count': 0, 'packages': []},
  },
  'GET /v1/backup/jobs': {'success': 200, 'message': 'ok', 'data': []},
  'GET /v2/app_management/web/appgrid': {
    'success': 200,
    'message': 'ok',
    'data': [
      {'name': 'jellyfin', 'title': {'en_us': 'Jellyfin'}, 'status': 'running'},
    ],
  },
};

Future<void> _openHealth(WidgetTester tester) async {
  await tester.tap(find.bySemanticsLabel(RegExp('^Server health, ')));
  await _settle(tester);
}

void main() {
  setUp(signIn);

  testWidgets('the status header is one button that says what it is for', (tester) async {
    final semantics = tester.ensureSemantics();
    await _run(tester, const DashboardScreen(), () async {
      final header = find.bySemanticsLabel(RegExp('^Server health, 3 things need attention'));
      expect(header, findsOneWidget);
      expect(tester.getSemantics(header), isSemantics(isButton: true, hasTapAction: true));
      // The count is on the icon too.
      expect(find.descendant(of: find.byType(Badge), matching: find.text('3')), findsOneWidget);
    });
    semantics.dispose();
  });

  testWidgets('with issues: tap opens the page with the same items as Home, then what is fine', (tester) async {
    await _run(tester, const DashboardScreen(), () async {
      final onHome = [for (final t in tester.widgetList<AttentionTile>(find.byType(AttentionTile))) t.item.title];
      expect(onHome, hasLength(3));
      await _openHealth(tester);
      expect(find.byType(ServerHealthScreen), findsOneWidget);
      final page = find.byType(ServerHealthScreen);
      expect(find.descendant(of: page, matching: find.text('3 things need attention')), findsOneWidget);
      expect(find.descendant(of: page, matching: find.text('Needs attention')), findsOneWidget);
      expect([for (final t in tester.widgetList<AttentionTile>(find.descendant(of: page, matching: find.byType(AttentionTile)))) t.item.title], onHome);
      expect(find.text('Everything else is fine'), findsOneWidget);
      expect(find.text('All good'), findsNothing);
      final list = find.descendant(of: page, matching: find.byType(Scrollable)).first;
      await tester.scrollUntilVisible(find.text('Processor temperature'), 200, scrollable: list);
      expect(find.text('48\u00A0°C · Normal'), findsOneWidget);
      await tester.scrollUntilVisible(find.text('Tailscale'), 200, scrollable: list);
      expect(find.text('Connected · 100.64.0.1'), findsOneWidget);
    });
  });

  testWidgets('all good: a calm "All good" with when it was checked and every check that passed', (tester) async {
    await _run(tester, const DashboardScreen(), overrides: allGood, () async {
      expect(find.bySemanticsLabel(RegExp('^Server health, All good')), findsOneWidget);
      expect(find.byType(Badge), findsNothing);
      expect(find.text('Needs attention'), findsNothing);
      await _openHealth(tester);
      final page = find.byType(ServerHealthScreen);
      expect(find.descendant(of: page, matching: find.text('All good')), findsOneWidget);
      expect(find.descendant(of: page, matching: find.byType(AttentionTile)), findsNothing);
      expect(find.text('What was checked'), findsOneWidget);
      expect(find.bySemanticsLabel(RegExp(r'^All good\. Checked just now\. \d+ checks passed\. Nothing needs you right now')), findsOneWidget);
      // Drive health on each drive's own row.
      expect(find.text('blue'), findsOneWidget);
      expect(find.text('17% used · 1.5 TB free · Healthy'), findsOneWidget);
    });
  });

  testWidgets('an item goes where it is fixed: system updates open the packages page', (tester) async {
    await _run(tester, const DashboardScreen(), () async {
      await _openHealth(tester);
      await tester.tap(find.descendant(of: find.byType(ServerHealthScreen), matching: find.text('112 system updates')));
      await _settle(tester);
      expect(tester.widget<SystemUpdatesScreen>(find.byType(SystemUpdatesScreen)).page, UpdatesPage.packages);
    });
  });

  testWidgets('pull to refresh on the page checks everything again', (tester) async {
    final server = await _run(tester, const DashboardScreen(), () async {
      await _openHealth(tester);
    });
    final before = server.requests.where((r) => r == 'GET /v1/disks').length;
    expect(before, 1);
    await _run(tester, ServerHealthScreen(controller: HomeController()), () async {
      await tester.fling(find.byType(Scrollable).first, const Offset(0, 400), 1000);
      await _settle(tester);
    }).then((s) => expect(s.requests.where((r) => r == 'GET /v1/disks').length, 2, reason: 'open + pull to refresh'));
  });

  testWidgets('offline: the last answer stays, with its age', (tester) async {
    final c = HomeController();
    await _run(tester, ServerHealthScreen(controller: c), () async {
      c.live.value = c.live.value!.copyWith(stale: true);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(find.textContaining('Offline · last updated'), findsOneWidget);
      expect(find.bySemanticsLabel(RegExp('Last checked just now')), findsOneWidget);
      expect(find.text('3 things need attention'), findsOneWidget);
    });
  });
}
