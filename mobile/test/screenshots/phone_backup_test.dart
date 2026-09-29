// "Back up this phone" in every style (Rack, Tonal, Console), light and
// true black, at 412 dp, against the fake server (phone_backup_fixtures).
//
//   flutter test test/screenshots/phone_backup_test.dart --update-goldens
//
// PNGs: goldens/phone_backup/<style>/<screen>_<mode>_412x915.png.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/phone_backup/phone_backup_history_screen.dart';
import 'package:nivaroos_mobile/screens/phone_backup/phone_backup_location.dart';
import 'package:nivaroos_mobile/screens/phone_backup/phone_backup_messages_restore.dart';
import 'package:nivaroos_mobile/screens/phone_backup/phone_backup_screen.dart';
import 'package:nivaroos_mobile/screens/phone_backup/phone_backup_setup_screen.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'harness.dart';
import 'phone_backup_fixtures.dart';

/// Shows [dialog] over a plain page once it is on screen.
class _DialogHost extends StatefulWidget {
  const _DialogHost(this.dialog);
  final Future<void> Function(BuildContext context) dialog;

  @override
  State<_DialogHost> createState() => _DialogHostState();
}

class _DialogHostState extends State<_DialogHost> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) widget.dialog(context);
    });
  }

  @override
  Widget build(BuildContext context) => PhoneBackupScreen(service: phoneService(state: phoneStateOk));
}

class _Shot {
  const _Shot(this.build, {this.overrides = const {}});
  final Widget Function() build;
  final Map<String, Object> overrides;
}

final Map<String, _Shot> _shots = {
  'phone_setup_new': _Shot(() => PhoneBackupSetupScreen(service: phoneService(credential: null, settings: null), deviceName: 'Pixel 8')),
  'phone_setup_edit': _Shot(() => PhoneBackupSetupScreen(service: phoneService(state: phoneStateOk))),
  'phone_not_set_up': _Shot(() => PhoneBackupScreen(service: phoneService(credential: null, settings: null))),
  'phone_status': _Shot(() => PhoneBackupScreen(service: phoneService(state: phoneStateOk))),
  'phone_status_running': _Shot(() => PhoneBackupScreen(service: phoneService(state: phoneStateRunning(shotTime)))),
  'phone_drive_missing': _Shot(() => PhoneBackupScreen(service: phoneService(state: phoneStateWaiting)), overrides: phoneDriveMissing),
  'phone_move_or_fresh': _Shot(() => _DialogHost((c) => askMoveOrFresh(c, target: 'Backup Stick › Phones'))),
  'phone_history': _Shot(() => PhoneBackupHistoryScreen(service: phoneService(state: phoneStateOk))),
  'phone_snapshot': _Shot(() => PhoneSnapshotScreen(service: phoneService(state: phoneStateOk), snapshot: 'snap_3', title: 'Thu, Sep 25 3:04 AM', categories: const ['calendar', 'contacts', 'media', 'sms'])),
  'phone_browse': _Shot(() => PhoneBrowseScreen(service: phoneService(state: phoneStateOk), snapshot: 'snap_3', category: 'media', path: '', title: 'Photos & videos')),
  'phone_sms_restore': _Shot(() => PhoneMessagesRestoreScreen(service: phoneService(state: phoneStateOk), snapshot: 'snap_3', category: 'sms')),
};

const _styles = [DesignDirection.rack, DesignDirection.tonal, DesignDirection.console];

void main() {
  setUp(() async {
    await signIn();
  });

  for (final d in _styles) {
    for (final MapEntry(key: name, value: s) in _shots.entries) {
      for (final m in const [AppThemeMode.light, AppThemeMode.black]) {
        testWidgets('${d.name} $name ${m.name}', (tester) {
          return shoot(
            tester,
            dir: 'phone_backup/${d.name}',
            name: name,
            screen: s.build(),
            brightness: m == AppThemeMode.light ? Brightness.light : Brightness.dark,
            themeName: m.name,
            appearance: Appearance(mode: m, direction: d),
            pushed: true,
            overrides: {...phoneServer, ...s.overrides},
          );
        });
      }
    }
  }
}
