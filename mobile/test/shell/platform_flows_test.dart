// Flows on the platform screens: server power uses the real route (M-09),
// signing out stops sharing and the heartbeat (M-17), Tailscale connects
// through the login link, never an auth key (M-10).
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:nivaroos_mobile/screens/settings_screen.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/background_service.dart';
import 'package:nivaroos_mobile/widgets/tailscale_modal.dart';

import '../screenshots/harness.dart';

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 8; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 200));
  }
}

void main() {
  late List<String> shareCalls;
  late List<String> launched;

  setUp(() async {
    await signIn();
    BackgroundService.debugIsAndroid = true;
    shareCalls = [];
    launched = [];
    final messenger = TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    messenger.setMockMethodCallHandler(const MethodChannel('com.fenyx.nivaroos/companion_share'), (call) async {
      shareCalls.add(call.method);
      return call.method == 'status' ? {'running': false, 'endsAt': 0} : true;
    });
    messenger.setMockMethodCallHandler(const MethodChannel('plugins.flutter.io/url_launcher'), (call) async {
      if (call.method == 'launch' || call.method == 'openUrlInApp' || call.method == 'launchUrl') {
        launched.add('${(call.arguments as Map)['url']}');
      }
      return true;
    });
  });
  tearDown(() => BackgroundService.debugIsAndroid = null);

  Future<FakeServer> pump(WidgetTester tester, Widget screen, {Map<String, Object> overrides = const {}}) async {
    await loadRealFonts();
    tester.view.physicalSize = phone * 3;
    tester.view.devicePixelRatio = 3;
    addTearDown(tester.view.reset);
    final server = FakeServer(overrides: overrides);
    await http.runWithClient(() async {
      await tester.pumpWidget(testApp(screen));
      await _settle(tester);
    }, () => server.client);
    return server;
  }

  testWidgets('restart asks first, then sends PUT /v1/sys/state/restart', (tester) async {
    final server = FakeServer(overrides: {'PUT /v1/sys/state/restart': {'success': 200, 'data': 'ok'}});
    await loadRealFonts();
    tester.view.physicalSize = phone * 3;
    tester.view.devicePixelRatio = 3;
    addTearDown(tester.view.reset);
    await http.runWithClient(() async {
      await tester.pumpWidget(testApp(const SettingsScreen()));
      await _settle(tester);
      final row = find.text('Restart server');
      await tester.scrollUntilVisible(row, 200, scrollable: find.byType(Scrollable).first);
      await tester.tap(row);
      await _settle(tester);
      expect(find.text('Restart nivaro.test?'), findsOneWidget);
      expect(server.requests.where((r) => r.contains('/sys/state')), isEmpty);
      await tester.tap(find.widgetWithText(FilledButton, 'Restart'));
      await _settle(tester);
      expect(server.requests, contains('PUT /v1/sys/state/restart'));
      expect(server.requests.where((r) => r.contains('/sys/power')), isEmpty);
    }, () => server.client);
  });

  testWidgets('signing out stops sharing and the heartbeat before the session goes', (tester) async {
    await pump(tester, const SettingsScreen());
    final row = find.text('Sign out');
    await tester.scrollUntilVisible(row, 300, scrollable: find.byType(Scrollable).first);
    await tester.tap(row);
    await _settle(tester);
    await tester.tap(find.widgetWithText(TextButton, 'Sign out'));
    await _settle(tester);
    expect(shareCalls, containsAllInOrder(['stop', 'cancelHeartbeat']));
    expect(ApiClient.instance.hasSession, isFalse);
  });

  testWidgets('Tailscale sign-in opens the login link from state/up, with no auth key', (tester) async {
    final server = FakeServer(overrides: {
      'GET /v1/tailscale/status': const FakeResponse({'success': 200, 'data': {'BackendState': 'NeedsLogin', 'Peer': {}}}),
      'PUT /v1/tailscale/state/up': const FakeResponse({'success': 200, 'data': {'state': 'needs_login', 'login_url': 'https://login.tailscale.com/a/abc'}}),
    });
    await loadRealFonts();
    tester.view.physicalSize = phone * 3;
    tester.view.devicePixelRatio = 3;
    addTearDown(tester.view.reset);
    await http.runWithClient(() async {
      await tester.pumpWidget(testApp(const TailscaleScreen()));
      await _settle(tester);
      await tester.tap(find.text('Sign in to Tailscale'));
      await _settle(tester);
      expect(server.requests, contains('PUT /v1/tailscale/state/up'));
      expect(server.requests.where((r) => r.contains('/tailscale/auth') || r.contains('custom/tailscale_auth')), isEmpty);
      expect(launched.single, 'https://login.tailscale.com/a/abc');
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(minutes: 4));
    }, () => server.client);
  });
}
