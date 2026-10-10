// Phone backup restore extras in every style, light and true black, at
// 412 dp: reinstalling apps from backed-up APKs (and the "Install unknown
// apps" ask), and the messages restore writing picture messages.
//
//   flutter test test/screenshots/phone_backup_restore_test.dart --update-goldens
//
// PNGs: goldens/phone_backup/<style>/<screen>_<mode>_412x915.png.
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/phone_backup/phone_backup_apps_restore.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'harness.dart';
import 'phone_backup_fixtures.dart';

Map<String, Object?> _env(Object? data) => {'success': 200, 'message': 'ok', 'data': data};

Map<String, Object?> _apk(String path, int size) => {'category': 'apks', 'path': path, 'size': size, 'mtime': 1758700000000, 'content': ''};

/// A backup with APKs of four apps (one split) and its app list.
final Map<String, Object> phoneAppsServer = {
  'GET /v1/backup/devices/$phoneId/restore-manifest': _env({
    'items': [
      _apk('org.example.maps/31/base.apk', 48210000),
      _apk('org.example.maps/31/split_config.arm64_v8a.apk', 12400000),
      _apk('org.example.notes/5/base.apk', 6120000),
      _apk('org.example.podcasts/120/base.apk', 23800000),
      _apk('org.example.same/9/base.apk', 3100000),
    ],
    'next_after': '',
  }),
  'GET /v1/backup/devices/$phoneId/exports?category=apps&snapshot=snap_3': _env([
    {'id': 'exp_apps', 'category': 'apps', 'name': '', 'taken_at': '2026-09-25T01:02:10Z', 'size': 412, 'items': 4},
  ]),
  'GET /v1/backup/devices/$phoneId/exports/exp_apps/content': {
    'apps': [
      {'package': 'org.example.maps', 'label': 'Maps', 'version_code': 31},
      {'package': 'org.example.notes', 'label': 'Notes', 'version_code': 5},
      {'package': 'org.example.podcasts', 'label': 'Podcasts', 'version_code': 120},
      {'package': 'org.example.same', 'label': 'Same Version', 'version_code': 9},
    ],
  },
};

/// The phone's side for the apps restore: what is installed and whether
/// NivaroOS may install apps; [installs] records the installer calls.
void stubAppsPhone({bool canInstall = true, List<Object?>? installs}) {
  final messenger = TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  messenger.setMockMethodCallHandler(const MethodChannel('com.fenyx.nivaroos/phone_backup'), (call) async => switch (call.method) {
        'installedApps' => [
            {'package': 'org.example.maps', 'version_code': 30},
            {'package': 'org.example.same', 'version_code': 9},
          ],
        _ => null,
      });
  messenger.setMockMethodCallHandler(const MethodChannel('com.fenyx.nivaroos/app_update'), (call) async {
    if (call.method == 'canRequestInstalls') return canInstall;
    if (call.method == 'install') installs?.add(call.arguments);
    return true;
  });
}

final Map<String, (bool, Map<String, Object>)> _shots = {
  'phone_apps_restore': (true, phoneAppsServer),
  'phone_apps_restore_allow': (false, phoneAppsServer),
};

const _styles = [DesignDirection.rack, DesignDirection.tonal, DesignDirection.console];

void main() {
  setUp(() async {
    await signIn();
  });

  for (final d in _styles) {
    for (final MapEntry(key: name, value: (canInstall, server)) in _shots.entries) {
      for (final m in const [AppThemeMode.light, AppThemeMode.black]) {
        testWidgets('${d.name} $name ${m.name}', (tester) {
          stubAppsPhone(canInstall: canInstall);
          return shoot(
            tester,
            dir: 'phone_backup/${d.name}',
            name: name,
            screen: PhoneAppsRestoreScreen(service: phoneService(state: phoneStateOk), snapshot: 'snap_3'),
            brightness: m == AppThemeMode.light ? Brightness.light : Brightness.dark,
            themeName: m.name,
            appearance: Appearance(mode: m, direction: d),
            pushed: true,
            overrides: {...phoneServer, ...server},
          );
        });
      }
    }
  }
}
