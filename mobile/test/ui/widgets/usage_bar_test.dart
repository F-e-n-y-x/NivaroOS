import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/theme/app_theme.dart';
import 'package:nivaroos_mobile/ui/theme/design_tokens.dart';
import 'package:nivaroos_mobile/ui/theme/status_colors.dart';
import 'package:nivaroos_mobile/ui/widgets/usage_bar.dart';

import '../pump.dart';

LinearProgressIndicator _bar(WidgetTester tester) => tester.widget<LinearProgressIndicator>(find.byType(LinearProgressIndicator));

Color _fill(WidgetTester tester) => _bar(tester).color!;

void main() {
  test('status follows the thresholds', () {
    expect(const UsageBar(value: 50, max: 100, label: 'x').status, isNull);
    expect(const UsageBar(value: 80, max: 100, label: 'x').status, Status.warning);
    expect(const UsageBar(value: 95, max: 100, label: 'x').status, Status.error);
    expect(const UsageBar(value: 5, max: 0, label: 'x').status, isNull);
  });

  testWidgets('shows the rounded percentage and detail, and fills to the fraction', (tester) async {
    await pumpUi(tester, const UsageBar(value: 230, max: 422, label: 'System disk', detail: '230 GB of 422 GB'), disableAnimations: true);
    await tester.pump();
    expect(find.text('55%'), findsOneWidget);
    expect(find.text('230 GB of 422 GB'), findsOneWidget);
    expect(_bar(tester).value, closeTo(230 / 422, 1e-9));
    // Below the warning level: the direction's meter fill (ink in Rack).
    expect(_fill(tester), AppTheme.light().extension<DesignTokens>()!.meterFill(AppTheme.light().colorScheme));
    // The percentage outranks the label.
    final label = tester.widget<Text>(find.text('System disk')).style!;
    final percent = tester.widget<Text>(find.text('55%')).style!;
    expect(percent.fontWeight!.value, greaterThan(label.fontWeight!.value));
    expect(percent.fontFeatures, contains(const FontFeature.tabularFigures()));
  });

  testWidgets('turns warning, then error, as it fills', (tester) async {
    await pumpUi(tester, const UsageBar(value: 85, max: 100, label: 'Disk'), disableAnimations: true);
    await tester.pump();
    expect(_fill(tester), StatusColors.light.warning.color);
    await pumpUi(tester, const UsageBar(value: 97, max: 100, label: 'Disk'), disableAnimations: true);
    await tester.pump();
    expect(_fill(tester), AppTheme.light().colorScheme.error);
  });

  testWidgets('an unknown size reads "—", not a number', (tester) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, const UsageBar(value: 3, max: 0, label: 'USB drive', detail: 'Size unknown'));
    await tester.pumpAndSettle();
    expect(find.text('—'), findsOneWidget);
    expect(_bar(tester).value, 0);
    expect(find.bySemanticsLabel('USB drive'), findsOneWidget);
    handle.dispose();
  });

  testWidgets('animates to a new value', (tester) async {
    await pumpUi(tester, const UsageBar(value: 10, max: 100, label: 'Memory'));
    await tester.pumpAndSettle();
    await pumpUi(tester, const UsageBar(value: 60, max: 100, label: 'Memory'));
    await tester.pump(const Duration(milliseconds: 100));
    final mid = _bar(tester).value!;
    expect(mid, inExclusiveRange(0.1, 0.6));
    await tester.pumpAndSettle();
    expect(_bar(tester).value, closeTo(0.6, 1e-9));
  });
}
