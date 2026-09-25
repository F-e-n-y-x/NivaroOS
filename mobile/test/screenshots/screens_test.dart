// Screenshots of every screen in light and dark at 412x915, plus the
// extra sizes each screen asks for. See harness.dart, and brief §7-§8.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/home_shell.dart';

import 'harness.dart';

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

/// Every screen, by golden name. Home, its detail screens, Updates and Logs
/// are in home_test.dart.
// The Apps area (apps, app store, custom install, logs, terminal) has its
// own file with its states and detail pages: apps_screens_test.dart.
// The VMs area (list, form, VM console, host desktop) is in vms_test.dart.
// Onboarding, servers, settings, companion devices, Tailscale and More
// are in platform_screens_test.dart (goldens/platform/).
final Map<String, ScreenShots> screens = {
  'home_shell': ScreenShots(() => const HomeShell(), migrated: true, extra: {Extra.smallPhone, Extra.text2x, Extra.tablet}),
  // Files and the file viewer have their own, fuller set of shots in
  // files_screens_test.dart (goldens/files/).
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
