// The Fans page and a fan's detail page in every style (Rack, Tonal,
// Console), light and true black, at 412 and 360 dp and at 200% text,
// against the fake server: fixtures/v1/fans/status.json (this repo's own
// test box: an NCT6793D with two fans in use and a GTX 1080 Ti) and the
// variants in fixtures/fans/.
//
//   flutter test test/screenshots/fans_test.dart --update-goldens
//
// PNGs: goldens/fans/<style>/<screen>_<mode>_<size>[_text2x].png.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/fans/fans_screen.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'harness.dart';

class _Shot {
  const _Shot(this.build, {this.overrides = const {}, this.full = false});
  final Widget Function() build;
  final Map<String, Object> overrides;

  /// Every size and text scale; otherwise 412 at 100%.
  final bool full;
}

final Map<String, _Shot> _shots = {
  'fans_overview': _Shot(() => const FansScreen(), full: true),
  'fans_curve': _Shot(() => FanDetailScreen(controller: FansController(), fanId: 'hw-nct6793-nct6775-656-pwm2'), full: true),
  'fans_fixed': _Shot(() => FanDetailScreen(controller: FansController(), fanId: 'hw-nct6793-nct6775-656-pwm1')),
  'fans_emergency': _Shot(() => const FansScreen(), overrides: {'GET /v1/fans/status': fixture('fans/emergency')}),
  'fans_readonly': _Shot(() => const FansScreen(), overrides: {'GET /v1/fans/status': fixture('fans/readonly')}),
  'fans_unavailable': _Shot(() => const FansScreen(), overrides: {'GET /v1/fans/status': const FakeResponse({'error': 'not found', 'message': 'not found'}, status: 404)}),
};

const _styles = [DesignDirection.rack, DesignDirection.tonal, DesignDirection.console];

void main() {
  setUp(signIn);

  for (final d in _styles) {
    for (final MapEntry(key: name, value: s) in _shots.entries) {
      for (final m in const [AppThemeMode.light, AppThemeMode.black]) {
        final sizes = s.full ? const [(phone, 1.0), (smallPhone, 1.0), (phone, 2.0)] : const [(phone, 1.0)];
        for (final (size, scale) in sizes) {
          testWidgets('${d.name} $name ${m.name} ${size.width.toInt()} ${scale}x', (tester) {
            return shoot(
              tester,
              dir: 'fans/${d.name}',
              name: name,
              screen: s.build(),
              brightness: m == AppThemeMode.light ? Brightness.light : Brightness.dark,
              themeName: m.name,
              appearance: Appearance(mode: m, direction: d),
              size: size,
              textScale: scale,
              pushed: true,
              overrides: s.overrides,
            );
          });
        }
      }
    }
  }
}
