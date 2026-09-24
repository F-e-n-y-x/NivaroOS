// Screenshots of every screen in light and dark at 412x915, plus the
// extra sizes each screen asks for. See harness.dart, and brief §7-§8.
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/file_entry.dart';
import 'package:nivaroos_mobile/screens/app_store_screen.dart';
import 'package:nivaroos_mobile/screens/apps_screen.dart';
import 'package:nivaroos_mobile/screens/companion_devices_screen.dart';
import 'package:nivaroos_mobile/screens/container_logs_screen.dart';
import 'package:nivaroos_mobile/screens/custom_install_screen.dart';
import 'package:nivaroos_mobile/screens/dashboard_screen.dart';
import 'package:nivaroos_mobile/screens/discovery_screen.dart';
import 'package:nivaroos_mobile/screens/file_viewer_screen.dart';
import 'package:nivaroos_mobile/screens/files_screen.dart';
import 'package:nivaroos_mobile/screens/home_shell.dart';
import 'package:nivaroos_mobile/screens/host_desktop_screen.dart';
import 'package:nivaroos_mobile/screens/login_screen.dart';
import 'package:nivaroos_mobile/screens/more_screen.dart';
import 'package:nivaroos_mobile/screens/server_profiles_screen.dart';
import 'package:nivaroos_mobile/screens/settings_screen.dart';
import 'package:nivaroos_mobile/screens/system_logs_screen.dart';
import 'package:nivaroos_mobile/screens/system_updates_screen.dart';
import 'package:nivaroos_mobile/screens/terminal_screen.dart';
import 'package:nivaroos_mobile/screens/vm_console_screen.dart';
import 'package:nivaroos_mobile/screens/vm_form_screen.dart';
import 'package:nivaroos_mobile/screens/vm_list_screen.dart';
import 'package:nivaroos_mobile/services/vm_client.dart';

import 'harness.dart';

/// A Markdown file on the test machine for the file viewer to open.
final _readme = () {
  final dir = Directory.systemTemp.createTempSync('nivaro_shots');
  final f = File('${dir.path}/notes.md')
    ..writeAsStringSync('# Notes\n\nThings to do on the server this week:\n\n'
        '- Move the photo library to **tank**\n- Update Jellyfin\n- Check the backup of `/DATA/Documents`\n');
  return f;
}();

/// Extra shots beyond light and dark at 412x915.
enum Extra {
  /// 360x740, light and dark: for screens with dense rows.
  smallPhone,

  /// 200% text, light and dark.
  text2x,

  /// 1024x768 landscape tablet, light: rail and list-detail layouts.
  tablet,
}

/// One screen's shots.
class ScreenShots {
  const ScreenShots(this.build, {this.migrated = false, this.tab = false, this.extra = const {}});

  /// A fresh widget for each shot.
  final Widget Function() build;

  /// Built on the v2 design system, so its shots run strict: an overflow
  /// or an uncaught error fails the test (brief §8). Set it when a screen
  /// is migrated, together with its [extra] shots.
  final bool migrated;

  /// One of HomeShell's tabs, drawn on the shell's Scaffold.
  final bool tab;
  final Set<Extra> extra;
}

/// Every screen, by golden name.
final Map<String, ScreenShots> screens = {
  'discovery': ScreenShots(() => const DiscoveryScreen()),
  'login': ScreenShots(() => const LoginScreen()),
  'server_profiles': ScreenShots(() => const ServerProfilesScreen()),
  'home_shell': ScreenShots(() => const HomeShell(), extra: {Extra.smallPhone, Extra.text2x, Extra.tablet}),
  'dashboard': ScreenShots(() => const DashboardScreen(), tab: true),
  'files': ScreenShots(() => const FilesScreen(), tab: true),
  'file_viewer_markdown': ScreenShots(() => FileViewerScreen(
        file: FileEntry(name: 'notes.md', path: _readme.path, isDir: false, size: _readme.lengthSync()),
        path: _readme.path,
        isLocal: true,
      )),
  'apps': ScreenShots(() => const AppsScreen(), tab: true),
  'app_store': ScreenShots(() => const AppStoreScreen()),
  'custom_install': ScreenShots(() => const CustomInstallScreen()),
  'container_logs': ScreenShots(() => const ContainerLogsScreen(appId: 'jellyfin', appTitle: 'Jellyfin')),
  'vm_list': ScreenShots(() => const VmListScreen(), tab: true),
  'vm_form_new': ScreenShots(() => VmFormScreen(client: VmClient('nivaro.test'))),
  'vm_console': ScreenShots(() => const VmConsoleScreen(vmName: 'mint')),
  'host_desktop': ScreenShots(() => const HostDesktopScreen()),
  'terminal': ScreenShots(() => const TerminalScreen()),
  'system_updates': ScreenShots(() => const SystemUpdatesScreen()),
  'system_logs': ScreenShots(() => const SystemLogsScreen()),
  'settings': ScreenShots(() => const SettingsScreen()),
  'companion_devices': ScreenShots(() => const CompanionDevicesScreen()),
  'more': ScreenShots(
    () => const MoreScreen(),
    migrated: true,
    tab: true,
    extra: {Extra.smallPhone, Extra.text2x, Extra.tablet},
  ),
};

void main() {
  setUp(signIn);

  for (final MapEntry(key: name, value: s) in screens.entries) {
    Future<void> shot(WidgetTester tester, Brightness brightness, {Size size = phone, double textScale = 1}) => shoot(
          tester,
          name: name,
          screen: s.build(),
          brightness: brightness,
          size: size,
          textScale: textScale,
          strict: s.migrated,
          tab: s.tab,
        );

    for (final b in Brightness.values) {
      testWidgets('$name ${b.name}', (tester) => shot(tester, b));
      if (s.extra.contains(Extra.smallPhone)) {
        testWidgets('$name small ${b.name}', (tester) => shot(tester, b, size: smallPhone));
      }
      if (s.extra.contains(Extra.text2x)) {
        testWidgets('$name 200% text ${b.name}', (tester) => shot(tester, b, textScale: 2));
      }
    }
    if (s.extra.contains(Extra.tablet)) {
      testWidgets('$name tablet', (tester) => shot(tester, Brightness.light, size: tablet));
    }
  }
}
