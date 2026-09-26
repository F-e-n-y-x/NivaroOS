// Widget tests for Home, its detail screens, Updates and Logs against the
// fake server: the routes they call, the actions they take, and the fake
// data and bogus buttons they must no longer show.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:nivaroos_mobile/screens/dashboard_screen.dart';
import 'package:nivaroos_mobile/screens/system_logs_screen.dart';
import 'package:nivaroos_mobile/screens/system_updates_screen.dart';
import 'package:nivaroos_mobile/services/app_update_service.dart';

import '../screenshots/harness.dart';

/// Pumps [screen] against a [FakeServer], lets it load, runs [body], then
/// tears it down so timers stop.
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

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 8; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 200));
  }
}

void main() {
  setUp(() async {
    await signIn();
    AppUpdateService.releasesApi = '$fakeServer/test/github/releases';
  });

  group('Home', () {
    testWidgets('shows what needs attention and the health meters, no shortcut grid', (tester) async {
      final server = await _run(tester, const DashboardScreen(), () async {
        expect(find.text('3 things need attention'), findsOneWidget);
        expect(find.text("Photos to Sandisk didn't finish"), findsOneWidget);
        expect(find.text('112 system updates'), findsOneWidget);
        expect(find.text('Processor'), findsOneWidget);
        expect(find.text('Memory'), findsOneWidget);
        expect(find.text('Storage'), findsOneWidget);
        for (final gone in ['Quick Access', 'Terminal', 'Tailscale', 'Host Desktop', 'LAN', '14ms']) {
          expect(find.text(gone), findsNothing, reason: '"$gone" duplicated the navigation or was made up');
        }
      });
      // VMs through the gateway, never the sidecar's own port (plan M-01).
      expect(server.requests, contains('GET /v1/vm-sidecar/vms'));
      expect(server.requests.where((r) => r.startsWith('GET /vm/')), isEmpty);
      // The optional backup module is probed before it is used (M-26).
      expect(server.requests.indexOf('GET /v1/backup/health'), lessThan(server.requests.indexOf('GET /v1/backup/jobs')));
    });

    testWidgets('memory details have no "flush" button and nothing calls /sys/update', (tester) async {
      final server = await _run(tester, const DashboardScreen(), () async {
        await tester.tap(find.text('Memory'));
        await _settle(tester);
        expect(find.text('Cache and buffers'), findsOneWidget);
        expect(find.textContaining('Flush'), findsNothing);
        expect(find.textContaining('flush'), findsNothing);
      });
      expect(server.requests.where((r) => r.contains('/sys/update')), isEmpty);
    });

    testWidgets("the Memory card's Free up button asks first, then POSTs /v1/sys/memory/clear", (tester) async {
      final server = await _run(tester, const DashboardScreen(), () async {
        await tester.tap(find.byTooltip('Free up memory'));
        await _settle(tester);
        // The sheet, not the Memory page.
        expect(find.text('Free up memory?'), findsOneWidget);
        expect(find.text('Cache and buffers'), findsNothing);
        expect(find.text('Also empty swap'), findsOneWidget, reason: 'the fixture has 2 GB in swap');
        await tester.tap(find.widgetWithText(FilledButton, 'Free up memory'));
        await _settle(tester);
        expect(find.text('Freed 2.1 GB'), findsOneWidget);
        expect(find.textContaining('3.4 GB', findRichText: true), findsOneWidget);
      }, overrides: {'POST /v1/sys/memory/clear': fixture('v1/sys/memory/clear')});
      expect(server.requests.where((r) => r == 'POST /v1/sys/memory/clear'), hasLength(1));
    });

    testWidgets('the Memory page offers Free up memory too', (tester) async {
      final server = await _run(tester, const DashboardScreen(), () async {
        await tester.tap(find.text('Memory'));
        await _settle(tester);
        await tester.scrollUntilVisible(find.text('Free up memory'), 300, scrollable: find.byType(Scrollable).first);
        expect(find.text('Swap'), findsOneWidget);
        await tester.tap(find.text('Free up memory'));
        await _settle(tester);
        await tester.tap(find.text('Cancel'));
        await _settle(tester);
      });
      expect(server.requests.where((r) => r.startsWith('POST')), isEmpty);
    });

    testWidgets('restart asks first, then sends PUT /v1/sys/state/restart', (tester) async {
      final server = await _run(tester, const DashboardScreen(), () async {
        await tester.tap(find.byTooltip('Server power'));
        await _settle(tester);
        await tester.tap(find.text('Restart server'));
        await _settle(tester);
        expect(find.text('Restart nivaro.test?'), findsOneWidget);
        await tester.tap(find.widgetWithText(FilledButton, 'Restart'));
        await _settle(tester);
      }, overrides: {'PUT /v1/sys/state/restart': {'success': 200, 'message': 'ok'}});
      expect(server.requests, contains('PUT /v1/sys/state/restart'));
    });

    testWidgets('cancelling the restart sends nothing', (tester) async {
      final server = await _run(tester, const DashboardScreen(), () async {
        await tester.tap(find.byTooltip('Server power'));
        await _settle(tester);
        await tester.tap(find.text('Restart server'));
        await _settle(tester);
        await tester.tap(find.text('Cancel'));
        await _settle(tester);
      });
      expect(server.requests.where((r) => r.startsWith('PUT')), isEmpty);
    });

    testWidgets('a failed first load shows the error with Retry', (tester) async {
      await _run(tester, const DashboardScreen(), () async {
        expect(find.text("Couldn't load the server's status"), findsOneWidget);
        expect(find.text('Retry'), findsOneWidget);
      }, overrides: {'GET /v1/sys/utilization': const FakeResponse({'success': 500, 'message': 'monitor down'}, status: 500)});
    });

    testWidgets('network details label the three speed tests; the server test runs on the server', (tester) async {
      final server = await _run(tester, const DashboardScreen(), () async {
        await tester.tap(find.text('Network'));
        await _settle(tester);
        // Under the download and upload charts.
        await tester.scrollUntilVisible(find.text("Server's internet"), 300);
        expect(find.text("Server's internet"), findsOneWidget);
        await tester.scrollUntilVisible(find.text("This phone's internet"), 300);
        expect(find.text('This phone to the server'), findsOneWidget);
        // The server's last result is shown, so its row offers to run again.
        await tester.scrollUntilVisible(find.text('Run again'), -300);
        await tester.tap(find.text('Run again'));
        await _settle(tester);
      }, overrides: {
        'POST /v1/sys/speedtest': {'success': 200, 'message': 'ok'},
        'GET /v1/sys/speedtest/status': {
          'success': 200,
          'message': 'ok',
          'data': {'running': false, 'phase': 'done', 'result': {'download_mbps': 300, 'upload_mbps': 40, 'ping_ms': 3}},
        },
      });
      expect(server.requests, contains('POST /v1/sys/speedtest'));
    });
  });

  group('Logs', () {
    test('entries are newest first and repeats fold into one', () {
      const text = '2026-09-25T00:00:01.000+0530\tinfo\tTunnel up\t{"id": "a", "file": "/x/companion.go"}\n'
          '2026-09-25T00:00:02.000+0530\tinfo\tTunnel up\t{"id": "a", "file": "/x/companion.go"}\n'
          '2026-09-25T00:00:03.000+0530\terror\tMount failed\t{"file": "/x/storage.go", "line": 9}\n'
          'panic: something odd';
      final e = parseLogs(text);
      expect(e.map((x) => x.message), ['panic: something odd', 'Mount failed', 'Tunnel up']);
      expect(e[2].repeats, 2);
      expect(e[2].source, 'companion.go');
      expect(e[2].extra, {'id': 'a'});
      expect(e[1].level, LogLevel.error);
      expect(e[0].level, LogLevel.error);
    });

    testWidgets('a failed load says so instead of showing made-up log lines', (tester) async {
      await _run(tester, const SystemLogsScreen(), () async {
        expect(find.text("Couldn't load the log"), findsOneWidget);
        expect(find.textContaining('running smoothly'), findsNothing);
      }, overrides: {'GET /v1/sys/logs': const FakeResponse({'success': 500, 'message': 'nope'}, status: 500)});
    });

    testWidgets('the Errors filter shows only errors', (tester) async {
      await _run(tester, const SystemLogsScreen(), () async {
        expect(find.text('CPU temperature sensor'), findsWidgets);
        await tester.tap(find.widgetWithText(ChoiceChip, 'Errors'));
        await _settle(tester);
        expect(find.text('CPU temperature sensor'), findsNothing);
        expect(find.text('when CheckAndMountAll then'), findsWidgets);
      });
    });
  });

  group('Updates', () {
    test('version comparison reads tags (one implementation, AppUpdateService)', () {
      expect(AppRelease.versionFromTag('mobile-v1.3.0'), '1.3.0');
      expect(AppRelease.versionFromTag('nightly'), isNull);
      expect(compareVersions('1.3.0', '1.2.1'), 1);
      expect(compareVersions('1.2.1', '1.2.1'), 0);
      expect(compareVersions('1.10.0', '1.9.9'), 1);
      expect(compareVersions('1.2', '1.2.0'), 0);
    });

    testWidgets('the app update goes through the checked update sheet, not a raw download', (tester) async {
      await _run(tester, const SystemUpdatesScreen(), () async {
        expect(find.text('Version 1.3.0 is available · 24.1 MB'), findsOneWidget);
        await tester.tap(find.text('Update to 1.3.0'));
        await _settle(tester);
        expect(find.text('App version 1.3.0'), findsOneWidget);
        expect(find.widgetWithText(FilledButton, 'Download and install'), findsOneWidget);
      }, overrides: {
        'GET /test/github/releases': [
          {
            'tag_name': 'mobile-v1.3.0',
            'draft': false,
            'prerelease': false,
            'html_url': 'https://github.com/x',
            'body': '- Fixes',
            'assets': [
              {'name': 'nivaroos-android.apk', 'size': 25270000, 'browser_download_url': '$fakeServer/test/nivaroos-android.apk'},
            ],
          },
        ],
      });
    });

    testWidgets('shows real facts only and installs packages after asking', (tester) async {
      final server = await _run(tester, const SystemUpdatesScreen(page: UpdatesPage.packages), () async {
        for (final fake in ['KVM Sidecar Active', 'x86_64 / Linux 6.x', 'Stable (Production)', 'Latest Git Commit (master)']) {
          expect(find.textContaining(fake), findsNothing);
        }
        expect(find.text('112 updates available'), findsOneWidget);
        await tester.tap(find.text('Install 112 updates'));
        await _settle(tester);
        expect(find.text('Install 112 updates?'), findsOneWidget);
        await tester.tap(find.widgetWithText(TextButton, 'Install'));
        await _settle(tester);
      }, overrides: {
        'POST /v1/sys/packages/upgrade': {'success': 200, 'message': 'ok', 'data': {'status': 'started'}},
        'GET /test/github/releases': <Object>[],
      });
      expect(server.requests, contains('POST /v1/sys/packages/upgrade'));
      expect(server.requests.where((r) => r.contains('/sys/update')), isEmpty);
    });
  });
}
