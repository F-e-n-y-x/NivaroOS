// Screenshots of Pair with a code (Add server > Pair with a code from the
// web): empty, a wrong code, and a scanned code that had expired. See
// harness.dart.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/pair_screen.dart';
import 'package:nivaroos_mobile/utils/pair_link.dart';

import 'harness.dart';

const _status = FakeResponse({'success': 200, 'data': {'initialized': true}});

Future<void> _wrongCode(WidgetTester tester) async {
  await tester.enterText(find.widgetWithText(TextField, 'Server address'), fakeServer);
  await tester.enterText(find.widgetWithText(TextField, 'Pairing code'), 'ABCD-EFGX');
  await tester.tap(find.widgetWithText(FilledButton, 'Pair'));
  for (var i = 0; i < 10; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 20)));
    await tester.pump(const Duration(milliseconds: 100));
  }
  FocusManager.instance.primaryFocus?.unfocus();
  await tester.pump(const Duration(seconds: 1));
}

final _shots = <String, (Widget Function(), Map<String, Object>, Future<void> Function(WidgetTester)?)>{
  'pair': (() => const PairScreen(), const {}, null),
  'pair_wrong_code': (
    () => const PairScreen(),
    {
      'GET /v1/users/status': _status,
      'POST /v1/users/pair': const FakeResponse({'success': 400, 'data': {'reason': 'invalid'}}, status: 400),
    },
    _wrongCode,
  ),
  'pair_scanned_expired': (
    () => PairScreen(initialLink: PairLink(code: 'ABCDEFGH', urls: [fakeServer, 'http://100.64.0.5'])),
    {
      'GET /v1/users/status': _status,
      'POST /v1/users/pair': const FakeResponse({'success': 410, 'data': {'reason': 'expired'}}, status: 410),
    },
    null,
  ),
};

void main() {
  setUp(() async {
    FocusManager.instance.highlightStrategy = FocusHighlightStrategy.alwaysTouch;
    await signIn();
  });

  for (final MapEntry(key: name, value: (build, overrides, before)) in _shots.entries) {
    for (final b in Brightness.values) {
      testWidgets('$name ${b.name}', (tester) => shoot(tester, name: name, dir: 'pair', screen: build(), brightness: b, overrides: overrides, before: before, pushed: true));
    }
    testWidgets('$name 200% text', (tester) => shoot(tester, name: name, dir: 'pair', screen: build(), textScale: 2, overrides: overrides, before: before, pushed: true));
  }
}
