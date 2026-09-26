// Screenshots of Edit app (owner request, 2026-09-26) in every style,
// light and true black: the top of the form (name, icon, Web UI link), the
// main service's image and ports, the variables with a hidden secret, a
// field with an error, and the compose file editor.
//
//   flutter test test/screenshots/app_edit_test.dart --update-goldens
//
// PNGs: goldens/apps/edit/<style>/<shot>_<mode>_412x915.png.
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/container_entry.dart';
import 'package:nivaroos_mobile/screens/app_edit_screen.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'harness.dart';

const _jellyfin = InstalledApp(id: 'jellyfin', title: 'Jellyfin', kind: AppKind.compose, status: 'running');

final _compose = {'GET /v2/app_management/compose/jellyfin': File('test/screenshots/fixtures/v2/app_management/compose/jellyfin.yaml').readAsStringSync()};

Future<void> _scrollTo(WidgetTester tester, Finder target, {double by = 300, double lead = 120}) async {
  final list = find.byType(Scrollable).first;
  await tester.scrollUntilVisible(target, by, scrollable: list);
  await tester.pump(const Duration(milliseconds: 300));
  // Put it near the top of the page, with a little of what's above.
  final top = tester.getTopLeft(target).dy;
  final listTop = tester.getTopLeft(list).dy;
  await tester.drag(list, Offset(0, -(top - listTop - lead)));
  await tester.pump(const Duration(seconds: 1));
}

final Map<String, Future<void> Function(WidgetTester)?> _shots = {
  'app_edit': null,
  'app_edit_container': (tester) => _scrollTo(tester, find.text('Service: jellyfin (main)')),
  // The database's variables: its password is hidden until revealed.
  'app_edit_env': (tester) => _scrollTo(tester, find.byTooltip('Show value'), lead: 260),
  'app_edit_error': (tester) async {
    await _scrollTo(tester, find.widgetWithText(TextFormField, 'Port'), lead: 260);
    await tester.enterText(find.widgetWithText(TextFormField, 'Port'), '99999');
    await tester.pump();
    await tester.tap(find.widgetWithText(TextButton, 'Save'));
    await tester.pump();
    await tester.pump(const Duration(seconds: 1));
  },
  'app_edit_yaml': (tester) async {
    await tester.tap(find.text('Compose file'));
    await tester.pump();
    await tester.pump(const Duration(seconds: 1));
  },
};

void main() {
  setUp(signIn);

  for (final d in DesignDirection.selectable) {
    for (final mode in [AppThemeMode.light, AppThemeMode.black]) {
      for (final shot in _shots.entries) {
        testWidgets(
          '${d.name} ${shot.key} ${mode.name}',
          (tester) => shoot(
            tester,
            dir: 'apps/edit/${d.name}',
            name: shot.key,
            screen: const AppEditScreen(app: _jellyfin),
            pushed: true,
            brightness: mode == AppThemeMode.light ? Brightness.light : Brightness.dark,
            themeName: mode.name,
            appearance: Appearance(mode: mode, direction: d),
            overrides: _compose,
            before: shot.value,
          ),
        );
      }
    }
  }

  // The app's page leads to it, and a long-press on the Apps tab too.
  testWidgets(
    'app_edit_small light',
    (tester) => shoot(
      tester,
      dir: 'apps/edit',
      name: 'app_edit',
      screen: const AppEditScreen(app: _jellyfin),
      pushed: true,
      size: smallPhone,
      overrides: _compose,
    ),
  );
  testWidgets(
    'app_edit 200% text light',
    (tester) => shoot(
      tester,
      dir: 'apps/edit',
      name: 'app_edit_container',
      screen: const AppEditScreen(app: _jellyfin),
      pushed: true,
      textScale: 2,
      overrides: _compose,
      before: (tester) => _scrollTo(tester, find.text('Service: jellyfin (main)')),
    ),
  );
  // Large text: the link's Test button goes under the link.
  testWidgets(
    'app_edit link 200% text light',
    (tester) => shoot(
      tester,
      dir: 'apps/edit',
      name: 'app_edit_link',
      screen: const AppEditScreen(app: _jellyfin),
      pushed: true,
      textScale: 2,
      overrides: _compose,
      before: (tester) => _scrollTo(tester, find.text('Web UI link'), lead: 40),
    ),
  );
}
