import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:nivaroos_mobile/screens/apps_screen.dart';
import 'package:nivaroos_mobile/screens/dashboard_screen.dart';
import 'package:nivaroos_mobile/screens/files_screen.dart';
import 'package:nivaroos_mobile/screens/home_shell.dart';
import 'package:nivaroos_mobile/screens/more_screen.dart';
import 'package:nivaroos_mobile/screens/settings_screen.dart';
import 'package:nivaroos_mobile/screens/vm_list_screen.dart';

import '../screenshots/harness.dart';

/// Runs [body] with HomeShell on screen at [size], against the fake server.
Future<void> _withShell(WidgetTester tester, Size size, Future<void> Function() body) async {
  // Real fonts: the pre-v2 tab contents overflow with the test font's
  // wide boxes, which would fail these tests for reasons unrelated to the
  // shell.
  await loadRealFonts();
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await http.runWithClient(() async {
    await tester.pumpWidget(testApp(const HomeShell()));
    for (var i = 0; i < 5; i++) {
      await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
      await tester.pump(const Duration(milliseconds: 100));
    }
    await body();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => FakeServer().client);
}

bool _onStage(WidgetTester tester, Type screen) {
  final stack = tester.widget<IndexedStack>(find.byType(IndexedStack).first);
  return stack.children[stack.index!].runtimeType == screen;
}

void main() {
  setUp(signIn);

  testWidgets('phones get a five-destination NavigationBar that switches tabs', (tester) async {
    await _withShell(tester, phone, () async {
      expect(find.byType(NavigationBar), findsOneWidget);
      expect(find.byType(NavigationRail), findsNothing);
      for (final label in ['Home', 'Files', 'Apps', 'VMs', 'More']) {
        expect(find.descendant(of: find.byType(NavigationBar), matching: find.text(label)), findsOneWidget);
      }
      expect(_onStage(tester, DashboardScreen), isTrue);
      final tabs = {'Files': FilesScreen, 'Apps': AppsScreen, 'VMs': VmListScreen, 'More': MoreScreen};
      for (final e in tabs.entries) {
        await tester.tap(find.descendant(of: find.byType(NavigationBar), matching: find.text(e.key)));
        await tester.pump();
        expect(_onStage(tester, e.value), isTrue, reason: e.key);
      }
    });
  });

  testWidgets('600dp and wider get a NavigationRail with the same destinations', (tester) async {
    await _withShell(tester, tablet, () async {
      expect(find.byType(NavigationRail), findsOneWidget);
      expect(find.byType(NavigationBar), findsNothing);
      expect(tester.widget<NavigationRail>(find.byType(NavigationRail)).destinations, hasLength(5));
      await tester.tap(find.descendant(of: find.byType(NavigationRail), matching: find.text('VMs')));
      await tester.pump();
      expect(_onStage(tester, VmListScreen), isTrue);
    });
  });

  testWidgets('back goes to Home first, then leaves the app', (tester) async {
    var exits = 0;
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, (call) async {
      if (call.method == 'SystemNavigator.pop') exits++;
      return null;
    });
    await _withShell(tester, phone, () async {
      await tester.tap(find.descendant(of: find.byType(NavigationBar), matching: find.text('Apps')));
      await tester.pump();
      // Off Home the shell holds back; on Home it lets the pop through to
      // the system, so Android can animate back-to-home.
      bool canPop() => (tester.widget(find.byWidgetPredicate((w) => w is PopScope).first) as PopScope).canPop;
      expect(canPop(), isFalse);
      await tester.binding.handlePopRoute();
      await tester.pump();
      expect(_onStage(tester, DashboardScreen), isTrue);
      expect(exits, 0);
      expect(canPop(), isTrue);
      await tester.binding.handlePopRoute();
      await tester.pump();
      expect(exits, 1);
    });
  });

  testWidgets('More keeps settings reachable', (tester) async {
    await _withShell(tester, phone, () async {
      await tester.tap(find.descendant(of: find.byType(NavigationBar), matching: find.text('More')));
      await tester.pump();
      final settings = find.descendant(of: find.byType(MoreScreen), matching: find.text('Settings'));
      await tester.scrollUntilVisible(settings, 200,
          scrollable: find.descendant(of: find.byType(MoreScreen), matching: find.byType(Scrollable)).first);
      await tester.ensureVisible(settings);
      await tester.pump();
      await tester.tap(settings);
      for (var i = 0; i < 5; i++) {
        await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
        await tester.pump(const Duration(milliseconds: 200));
      }
      expect(find.byType(SettingsScreen), findsOneWidget);
    });
  });
}
