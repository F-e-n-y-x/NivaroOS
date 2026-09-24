import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/widgets/metric_row.dart';

import '../pump.dart';

void main() {
  testWidgets('shows label, supporting text, value and a smaller unit', (tester) async {
    await pumpUi(tester, const MetricRow(icon: Icons.thermostat_outlined, label: 'CPU temperature', value: '48', unit: '°C', supporting: 'AMD Ryzen 5 3600'));
    expect(find.text('CPU temperature'), findsOneWidget);
    expect(find.text('AMD Ryzen 5 3600'), findsOneWidget);
    final rich = tester.widget<RichText>(find.descendant(of: find.byType(AnimatedSwitcher), matching: find.byType(RichText)));
    final root = rich.text as TextSpan;
    final spans = (root.children!.single as TextSpan).children!.cast<TextSpan>();
    expect(spans.first.text, '48');
    expect(spans.last.text, contains('°C'));
    expect(spans.last.style!.fontSize, lessThan(spans.first.style!.fontSize!));
    expect(spans.first.style!.fontFeatures, contains(const FontFeature.tabularFigures()));
  });

  testWidgets('reads as one sentence for TalkBack', (tester) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, const MetricRow(icon: Icons.memory_outlined, label: 'Memory', value: '42', unit: '%'));
    final node = tester.getSemantics(find.byType(ListTile));
    expect(node.label, contains('Memory'));
    expect(node.label, contains('42%'));
    handle.dispose();
  });

  testWidgets('no space before a percent sign, a space before other units', (tester) async {
    String shown() => tester.widget<RichText>(find.descendant(of: find.byType(AnimatedSwitcher), matching: find.byType(RichText))).text.toPlainText();
    await pumpUi(tester, const MetricRow(icon: Icons.memory_outlined, label: 'CPU', value: '12', unit: '%'));
    expect(shown(), '12%');
    await pumpUi(tester, const MetricRow(icon: Icons.upload_outlined, label: 'Upload', value: '1.2', unit: 'MB/s'));
    await tester.pumpAndSettle();
    expect(shown(), '1.2\u2009MB/s');
  });

  testWidgets('cross-fades when the value changes, and is tappable', (tester) async {
    var taps = 0;
    await pumpUi(tester, MetricRow(icon: Icons.speed, label: 'Upload', value: '1.2', unit: 'MB/s', onTap: () => taps++));
    await pumpUi(tester, MetricRow(icon: Icons.speed, label: 'Upload', value: '3.4', unit: 'MB/s', onTap: () => taps++));
    await tester.pump(const Duration(milliseconds: 50));
    expect(find.byType(RichText).evaluate().where((e) => (e.widget as RichText).text.toPlainText().contains('MB/s')).length, 2);
    // Both values keep the right edge while they cross-fade.
    final old = find.text('1.2\u2009MB/s', findRichText: true), next = find.text('3.4\u2009MB/s', findRichText: true);
    expect(tester.getTopRight(old).dx, tester.getTopRight(next).dx);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Upload'));
    expect(taps, 1);
  });
}
