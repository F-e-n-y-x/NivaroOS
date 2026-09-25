// Home's live polling at the phone's "Refresh widgets" interval, and the
// console preview of the one running VM: when it shows, how often it
// asks for a picture, and that it stops while it can't be seen.
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/screens/dashboard_screen.dart';
import 'package:nivaroos_mobile/screens/settings_screen.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/services/widget_refresh.dart';
import 'package:nivaroos_mobile/widgets/vm_console_preview.dart';

import '../screenshots/harness.dart';

const _screenshot = 'GET /v1/vm-sidecar/vms/Ghost-Windows-11/screenshot';

final Uint8List _png = File('test/screenshots/fixtures/v1/vm-sidecar/vms/Ghost-Windows-11/screenshot.png').readAsBytesSync();

/// The fixture's VMs with [running] on.
Map<String, Object> _vms(Set<String> running) {
  final list = (jsonDecode(File('test/screenshots/fixtures/v1/vm-sidecar/vms.json').readAsStringSync()) as List).cast<Map<String, dynamic>>();
  return {
    'GET /v1/vm-sidecar/vms': [
      for (final v in list) {...v, 'state': running.contains(v['name']) ? 'running' : v['state']},
    ],
  };
}

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 8; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 100));
  }
}

/// Pumps Home against a [FakeServer] (every request's headers go to
/// [headers]), runs [body], then tears it down so timers stop.
Future<FakeServer> _home(
  WidgetTester tester,
  WidgetRefreshController refresh,
  Future<void> Function(FakeServer server) body, {
  Map<String, Object> overrides = const {},
  List<(String, Map<String, String>)>? headers,
}) async {
  tester.view.physicalSize = const Size(412, 915) * 3;
  tester.view.devicePixelRatio = 3;
  addTearDown(tester.view.reset);
  final server = FakeServer(overrides: overrides);
  final client = MockClient((req) async {
    headers?.add((req.url.path, req.headers));
    // A fresh copy: the one handed in here is already finalized.
    final copy = http.Request(req.method, req.url)
      ..headers.addAll(req.headers)
      ..bodyBytes = req.bodyBytes;
    return http.Response.fromStream(await server.client.send(copy));
  });
  await http.runWithClient(() async {
    await tester.pumpWidget(testApp(Scaffold(body: DashboardScreen(refresh: refresh))));
    await _settle(tester);
    await body(server);
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => client);
  return server;
}

int _count(FakeServer s, String key) => s.requests.where((r) => r == key).length;

/// Lets [time] pass on the fake clock, a second at a time.
Future<void> _wait(WidgetTester tester, Duration time) async {
  for (var i = 0; i < time.inSeconds; i++) {
    // The fake server's answers need a little real time to arrive.
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 2)));
    await tester.pump(const Duration(seconds: 1));
  }
}

void main() {
  setUp(signIn);

  group('Home polling', () {
    testWidgets('reads utilization at the chosen interval, drives no faster than 30 s', (tester) async {
      final refresh = WidgetRefreshController(WidgetRefresh.s2);
      await _home(tester, refresh, (server) async {
        final cpu = _count(server, 'GET /v1/sys/utilization');
        final disks = _count(server, 'GET /v1/sys/disks-usage');
        await _wait(tester, const Duration(seconds: 20));
        expect(_count(server, 'GET /v1/sys/utilization') - cpu, 10);
        // One drive reading on open; the next is not due for 30 s.
        expect(_count(server, 'GET /v1/sys/disks-usage') - disks, 0);
        await _wait(tester, const Duration(seconds: 12));
        expect(_count(server, 'GET /v1/sys/disks-usage') - disks, 1);
      });
    });

    testWidgets('a new interval applies at once, and pull-to-refresh only stops polling', (tester) async {
      final refresh = WidgetRefreshController(WidgetRefresh.s10);
      await _home(tester, refresh, (server) async {
        var before = _count(server, 'GET /v1/sys/utilization');
        await _wait(tester, const Duration(seconds: 20));
        expect(_count(server, 'GET /v1/sys/utilization') - before, 2);

        await refresh.set(WidgetRefresh.manual);
        await tester.pump();
        before = _count(server, 'GET /v1/sys/utilization');
        final vms = _count(server, 'GET /v1/vm-sidecar/vms');
        await _wait(tester, const Duration(seconds: 60));
        expect(_count(server, 'GET /v1/sys/utilization'), before);
        expect(_count(server, 'GET /v1/vm-sidecar/vms'), vms);

        await refresh.set(WidgetRefresh.s4);
        await tester.pump();
        before = _count(server, 'GET /v1/sys/utilization');
        await _wait(tester, const Duration(seconds: 8));
        expect(_count(server, 'GET /v1/sys/utilization') - before, 2);
      });
    });

    testWidgets('the VM list is polled too, but no faster than every 5 s', (tester) async {
      final refresh = WidgetRefreshController(WidgetRefresh.s2);
      await _home(tester, refresh, (server) async {
        final before = _count(server, 'GET /v1/vm-sidecar/vms');
        await _wait(tester, const Duration(seconds: 20));
        final polled = _count(server, 'GET /v1/vm-sidecar/vms') - before;
        expect(polled, inInclusiveRange(3, 4));
      });
    });

    testWidgets('the chart history follows the interval', (tester) async {
      final refresh = WidgetRefreshController(WidgetRefresh.s30);
      final controller = HomeController();
      tester.view.physicalSize = const Size(412, 915) * 3;
      tester.view.devicePixelRatio = 3;
      addTearDown(tester.view.reset);
      final server = FakeServer();
      await http.runWithClient(() async {
        await tester.pumpWidget(testApp(Scaffold(body: DashboardScreen(controller: controller, refresh: refresh))));
        await _settle(tester);
        expect(controller.history.capacity, 5);
        await refresh.set(WidgetRefresh.s2);
        await tester.pump();
        expect(controller.history.capacity, 61);
        await tester.pumpWidget(const SizedBox.shrink());
        await tester.pump(const Duration(minutes: 1));
      }, () => server.client);
    });
  });

  group('Home console preview', () {
    testWidgets('one running VM: a preview, fetched through the gateway with the Authorization header', (tester) async {
      final headers = <(String, Map<String, String>)>[];
      final server = await _home(tester, WidgetRefreshController(), (server) async {
        expect(find.byType(VmConsolePreview), findsOneWidget);
        expect(find.bySemanticsLabel('Console of Ghost-Windows-11, live picture'), findsOneWidget);
        final first = _count(server, _screenshot);
        expect(first, 1);
        // The 4 s default: a picture every 5 s at most.
        await _wait(tester, const Duration(seconds: 20));
        expect(_count(server, _screenshot) - first, 4);
      }, overrides: _vms({'Ghost-Windows-11'}), headers: headers);
      final shot = headers.firstWhere((h) => h.$1.endsWith('/screenshot'));
      expect(shot.$2['Authorization'], 'test-token');
      expect(server.requests.where((r) => r.contains('token=')), isEmpty);
    });

    testWidgets('two running VMs: no preview, just the rows', (tester) async {
      final server = await _home(tester, WidgetRefreshController(), (server) async {
        expect(find.byType(VmConsolePreview), findsNothing);
        expect(find.byTooltip('Open console of Ghost-Windows-11'), findsOneWidget);
        expect(find.byTooltip('Open console of mint'), findsOneWidget);
      }, overrides: _vms({'Ghost-Windows-11', 'mint'}));
      expect(_count(server, _screenshot), 0);
    });

    testWidgets('no running VM: no preview', (tester) async {
      await _home(tester, WidgetRefreshController(), (server) async {
        expect(find.byType(VmConsolePreview), findsNothing);
      });
    });

    testWidgets('a VM without a picture shows the quiet placeholder', (tester) async {
      await _home(tester, WidgetRefreshController(), (server) async {
        await tester.scrollUntilVisible(find.byType(VmConsolePreview), 300, scrollable: find.byType(Scrollable).first);
        expect(find.text('No picture yet'), findsOneWidget);
        expect(find.byType(CircularProgressIndicator), findsNothing);
      }, overrides: {
        ..._vms({'Ghost-Windows-11'}),
        _screenshot: const FakeResponse({'error': 'domain has no graphics'}, status: 400),
      });
    });
  });

  group('VmConsolePreview', () {
    Future<List<DateTime>> pumpPreview(
      WidgetTester tester, {
      Duration? every = const Duration(seconds: 5),
      Future<Uint8List?> Function()? fetch,
      bool visible = true,
      int token = 0,
      List<DateTime>? into,
    }) async {
      final calls = into ?? <DateTime>[];
      await tester.pumpWidget(testApp(Scaffold(
        body: TickerMode(
          enabled: visible,
          child: VmConsolePreview(
            name: 'Ghost-Windows-11',
            every: every,
            refreshToken: token,
            fetch: () {
              calls.add(DateTime.now());
              return fetch?.call() ?? Future.value(_png);
            },
          ),
        ),
      )));
      await tester.pump();
      return calls;
    }

    testWidgets('keeps the picture shape from the PNG header', (tester) async {
      expect(pngSize(_png), (width: 960, height: 540));
      expect(pngSize(Uint8List.fromList(utf8.encode('<html>502 Bad Gateway</html>'))), isNull);
      await pumpPreview(tester);
      final ratio = tester.widget<AspectRatio>(find.descendant(of: find.byType(VmConsolePreview), matching: find.byType(AspectRatio)));
      expect(ratio.aspectRatio, closeTo(16 / 9, 1e-9));
    });

    testWidgets('refreshes at its interval and pauses while hidden', (tester) async {
      final calls = await pumpPreview(tester);
      expect(calls, hasLength(1));
      await _wait(tester, const Duration(seconds: 10));
      expect(calls, hasLength(3));

      // Home becomes a hidden tab (or a screen covers it): no more tries.
      await pumpPreview(tester, visible: false, into: calls);
      await _wait(tester, const Duration(seconds: 30));
      expect(calls, hasLength(3));
    });

    testWidgets('pauses in the background and takes a picture on return', (tester) async {
      final calls = await pumpPreview(tester);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
      await _wait(tester, const Duration(seconds: 30));
      expect(calls, hasLength(1));
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      await tester.pump();
      expect(calls, hasLength(2));
    });

    testWidgets('failures back off instead of retrying at full speed', (tester) async {
      final calls = await pumpPreview(tester, fetch: () => Future.error(const SocketException('unreachable')));
      // Tries at 0, then after 10, 20 and 40 s: 4 in the first 70 s, not 14.
      await _wait(tester, const Duration(seconds: 70));
      expect(calls, hasLength(4));
      expect(find.text('No picture yet'), findsOneWidget);
    });

    testWidgets('with refreshing off, one picture on open and one per pull-to-refresh', (tester) async {
      final calls = await pumpPreview(tester, every: null);
      await _wait(tester, const Duration(seconds: 30));
      expect(calls, hasLength(1));
      await pumpPreview(tester, every: null, token: 1, into: calls);
      expect(calls, hasLength(2));
    });
  });

  group('Settings', () {
    testWidgets('Refresh widgets lists the choices and saves the one picked', (tester) async {
      final refresh = WidgetRefreshController();
      await tester.pumpWidget(testApp(SettingsScreen(refresh: refresh)));
      await _settle(tester);
      expect(find.text('Every 4 seconds'), findsOneWidget);

      await tester.tap(find.text('Refresh widgets'));
      await _settle(tester);
      for (final choice in ['Every 2 seconds', 'Every 4 seconds (default)', 'Every 10 seconds', 'Every 30 seconds', 'Every minute', 'Only when I pull to refresh']) {
        expect(find.text(choice), findsOneWidget);
      }
      await tester.tap(find.text('Every 10 seconds'));
      await _settle(tester);

      expect(refresh.value, WidgetRefresh.s10);
      expect(await StorageService.instance.getWidgetRefresh(), 's10');
      // The row says so without reopening the screen.
      expect(find.text('Every 10 seconds'), findsOneWidget);
      await refresh.set(WidgetRefresh.defaultValue);
    });
  });
}
