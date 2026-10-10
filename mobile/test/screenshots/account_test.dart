// Screenshots of Change password, Scheduled tasks and the offline copies
// (Home, Server health and Files' drives from the phone's cache while the
// server can't be reached), in light and dark. See harness.dart.
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/change_password_screen.dart';
import 'package:nivaroos_mobile/screens/dashboard_screen.dart';
import 'package:nivaroos_mobile/screens/files_screen.dart';
import 'package:nivaroos_mobile/screens/scheduled_tasks_screen.dart';
import 'package:nivaroos_mobile/screens/server_health_screen.dart';
import 'package:nivaroos_mobile/services/offline_cache.dart';

import 'harness.dart';

/// Every cached route down: what a phone away from home sees.
final Map<String, Object> _down = {
  for (final r in OfflineCache.routes) 'GET $r': const FakeResponse('<html>502 Bad Gateway</html>', status: 502),
  'GET /v1/storage': const FakeResponse('<html>502 Bad Gateway</html>', status: 502),
};

/// The fixtures of the cached routes, kept two hours before the shot.
void _seedCache() {
  final bodies = <String, String>{};
  for (final r in OfflineCache.routes) {
    final f = File('test/screenshots/fixtures$r.json');
    if (f.existsSync()) bodies[r] = f.readAsStringSync();
  }
  OfflineCache.instance.debugSeed(fakeServer, bodies, shotTime.subtract(const Duration(hours: 2)));
}

Future<void> Function(WidgetTester) _fill(String current, String next, String confirm) => (tester) async {
      await tester.enterText(find.widgetWithText(TextField, 'Current password'), current);
      await tester.enterText(find.widgetWithText(TextField, 'New password'), next);
      await tester.enterText(find.widgetWithText(TextField, 'Confirm new password'), confirm);
      await tester.tap(find.widgetWithText(FilledButton, 'Change password'));
      for (var i = 0; i < 6; i++) {
        await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 20)));
        await tester.pump(const Duration(milliseconds: 100));
      }
      FocusManager.instance.primaryFocus?.unfocus();
      await tester.pump(const Duration(seconds: 1));
    };

class _Shot {
  const _Shot(this.build, {this.overrides = const {}, this.before, this.tab = false, this.dense = false, this.cached = false});
  final Widget Function() build;
  final Map<String, Object> overrides;
  final Future<void> Function(WidgetTester)? before;
  final bool tab;

  /// Also at 360x740 and 200% text.
  final bool dense;

  /// With the offline copies seeded.
  final bool cached;
}

final Map<String, _Shot> _shots = {
  'change_password': _Shot(() => const ChangePasswordScreen(), dense: true),
  'change_password_mismatch': _Shot(() => const ChangePasswordScreen(), before: _fill('secret1', 'longer-secret', 'longer-secrte')),
  'change_password_wrong': _Shot(
    () => const ChangePasswordScreen(),
    overrides: {'PUT /v1/users/current/password': const FakeResponse({'success': 10003, 'message': 'Invalid old password'}, status: 400)},
    before: _fill('not-it', 'longer-secret', 'longer-secret'),
  ),
  'scheduled_tasks': _Shot(() => const ScheduledTasksScreen(), dense: true),
  'scheduled_tasks_empty': _Shot(() => const ScheduledTasksScreen(), overrides: {'GET /v1/schedules': const {'success': 200, 'data': []}}),
  'scheduled_tasks_details': _Shot(
    () => const ScheduledTasksScreen(),
    before: (tester) async {
      await tester.tap(find.text('Weekly TRIM'));
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
    },
  ),
  'home_offline_cached': _Shot(() => const DashboardScreen(), tab: true, overrides: _down, cached: true),
  'server_health_offline_cached': _Shot(() => ServerHealthScreen(controller: HomeController()), overrides: _down, cached: true),
  'files_offline_cached': _Shot(() => const FilesScreen(), tab: true, overrides: _down, cached: true),
};

void main() {
  setUp(signIn);
  tearDown(OfflineCache.instance.debugReset);

  for (final MapEntry(key: name, value: s) in _shots.entries) {
    final sizes = [(phone, 1.0), if (s.dense) ...[(smallPhone, 1.0), (phone, 2.0)]];
    for (final b in Brightness.values) {
      for (final (size, scale) in sizes) {
        testWidgets(goldenName(name, b, size, scale), (tester) async {
          if (s.cached) _seedCache();
          await shoot(
            tester,
            name: name,
            dir: 'account',
            screen: s.build(),
            brightness: b,
            size: size,
            textScale: scale,
            overrides: s.overrides,
            before: s.before,
            tab: s.tab,
            pushed: !s.tab,
          );
        });
      }
    }
  }
}
