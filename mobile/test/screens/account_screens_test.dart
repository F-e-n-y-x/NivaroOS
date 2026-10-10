// Change password and Scheduled tasks against a fake server: what each
// sends, what it checks before sending, and the session after a change.
// Nothing here reaches a real server.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/screens/change_password_screen.dart';
import 'package:nivaroos_mobile/screens/scheduled_tasks_screen.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';

import '../screenshots/harness.dart';

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 8; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 100));
  }
}

/// Pumps [screen] pushed over a blank route, with every request answered
/// by a [FakeServer] (bodies of non-GETs go to [sent]).
Future<void> _run(WidgetTester tester, Widget screen, Future<void> Function() body, {Map<String, Object> overrides = const {}, List<String>? sent}) async {
  final server = FakeServer(overrides: overrides);
  final client = MockClient((req) async {
    if (req.method != 'GET') sent?.add('${req.method} ${req.url.path} ${req.body}');
    final copy = http.Request(req.method, req.url)
      ..headers.addAll(req.headers)
      ..bodyBytes = req.bodyBytes;
    return http.Response.fromStream(await server.client.send(copy));
  });
  await http.runWithClient(() async {
    await tester.pumpWidget(testApp(screen, pushed: true));
    await _settle(tester);
    await body();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => client);
}

Future<void> _fill(WidgetTester tester, String current, String next, String confirm) async {
  await tester.enterText(find.widgetWithText(TextField, 'Current password'), current);
  await tester.enterText(find.widgetWithText(TextField, 'New password'), next);
  await tester.enterText(find.widgetWithText(TextField, 'Confirm new password'), confirm);
  await tester.tap(find.widgetWithText(FilledButton, 'Change password'));
  await _settle(tester);
}

void main() {
  setUp(signIn);

  group('Change password', () {
    testWidgets('checks the fields before asking the server', (tester) async {
      final sent = <String>[];
      await _run(tester, const ChangePasswordScreen(), sent: sent, () async {
        await _fill(tester, '', 'abc', 'abc');
        expect(find.text('Enter your current password'), findsOneWidget);
        expect(find.text('Use at least 6 characters'), findsOneWidget);
        await _fill(tester, 'old-pass', 'new-pass1', 'new-pass2');
        expect(find.text("The passwords don't match"), findsOneWidget);
      });
      expect(sent, isEmpty);
    });

    testWidgets('a wrong current password shows on its field', (tester) async {
      await _run(tester, const ChangePasswordScreen(),
          overrides: {'PUT /v1/users/current/password': const FakeResponse({'success': 10003, 'message': 'Invalid old password'}, status: 400)}, () async {
        await _fill(tester, 'nope', 'new-pass', 'new-pass');
        expect(find.text("That isn't your current password"), findsOneWidget);
      });
      expect(ApiClient.instance.accessToken, 'test-token');
    });

    testWidgets('success keeps the new tokens and closes', (tester) async {
      final sent = <String>[];
      await _run(tester, const ChangePasswordScreen(), sent: sent, overrides: {
        'PUT /v1/users/current/password': const {
          'success': 200,
          'data': {'user': {'username': 'alex'}, 'token': {'access_token': 'fresh-access', 'refresh_token': 'fresh-refresh', 'expires_at': 1}},
        },
      }, () async {
        await _fill(tester, 'old-pass', 'new-pass', 'new-pass');
        expect(find.byType(ChangePasswordScreen), findsNothing);
        expect(find.textContaining('Password changed'), findsOneWidget);
      });
      expect(sent.single, 'PUT /v1/users/current/password ${jsonEncode({'old_password': 'old-pass', 'password': 'new-pass'})}');
      expect(ApiClient.instance.accessToken, 'fresh-access');
      expect(await StorageService.instance.getRefreshToken(), 'fresh-refresh');
    });
  });

  group('Scheduled tasks', () {
    testWidgets('lists the tasks with their schedule and last result', (tester) async {
      await _run(tester, const ScheduledTasksScreen(), () async {
        expect(find.text('Nightly container restart'), findsOneWidget);
        expect(find.textContaining('Every day at 04:00 · next '), findsOneWidget);
        expect(find.text('Failed'), findsOneWidget);
        expect(find.text('Moved to Backup & Sync'), findsOneWidget);
        expect(find.text('Not run yet'), findsOneWidget);
      });
    });

    testWidgets('the switch turns a task off; a moved task has none to use', (tester) async {
      final sent = <String>[];
      await _run(tester, const ScheduledTasksScreen(), sent: sent, overrides: {'POST /v1/schedules/t1/toggle': const {'success': 200}}, () async {
        final switches = tester.widgetList<Switch>(find.byType(Switch)).toList();
        expect(switches.last.onChanged, isNull, reason: 'moved to Backup & Sync');
        await tester.tap(find.byType(Switch).first);
        await _settle(tester);
      });
      expect(sent, ['POST /v1/schedules/t1/toggle {"enabled":false}']);
    });

    testWidgets('Run now asks first, then starts it', (tester) async {
      final sent = <String>[];
      await _run(tester, const ScheduledTasksScreen(), sent: sent, overrides: {'POST /v1/schedules/t2/run': const {'success': 200, 'message': 'Execution started'}}, () async {
        await tester.tap(find.text('Weekly TRIM'));
        await _settle(tester);
        await tester.tap(find.widgetWithText(FilledButton, 'Run now'));
        await _settle(tester);
        expect(sent, isEmpty, reason: 'not before the confirmation');
        await tester.tap(find.widgetWithText(TextButton, 'Run now'));
        await _settle(tester);
      });
      expect(sent, ['POST /v1/schedules/t2/run {}']);
    });
  });
}
