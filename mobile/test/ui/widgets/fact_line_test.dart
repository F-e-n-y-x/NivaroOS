// A line of facts wraps between facts and never leaves a separator
// hanging at the end of a line ("Idle · 37 °C ·" / "VRAM ...").
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/widgets/fact_line.dart';

Future<RenderFactLine> _pump(WidgetTester tester, double width, Widget Function(Widget line) wrap, List<String> facts) async {
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: Align(
        alignment: Alignment.topLeft,
        child: SizedBox(width: width, child: wrap(FactLine.plain(facts, style: const TextStyle(fontSize: 10)))),
      ),
    ),
  ));
  return tester.renderObject<RenderFactLine>(find.byType(FactLine));
}

void main() {
  const facts = ['Idle', '37 °C', 'VRAM 298 MB of 11 GB'];

  testWidgets('one line when it fits', (tester) async {
    final r = await _pump(tester, 1000, (l) => l, facts);
    expect(r.debugLaidOutText, 'Idle · 37 °C · VRAM 298 MB of 11 GB'.replaceAll(' · ', ' · '));
  });

  testWidgets('a wrap ends the line at a fact, and drops its dot', (tester) async {
    // Room for "Idle · 37 °C" but not the VRAM phrase after it.
    final r = await _pump(tester, 160, (l) => l, facts);
    final lines = r.debugLaidOutText!.split('\n');
    expect(lines, hasLength(2));
    for (final line in lines) {
      expect(line.trim().endsWith('·'), isFalse, reason: '"$line" ends in a separator');
      expect(line.trim().startsWith('·'), isFalse, reason: '"$line" starts with a separator');
    }
    expect(lines.last, 'VRAM 298 MB of 11 GB', reason: 'a fact breaks as a whole');
    expect(tester.getSemantics(find.byType(FactLine)).label, 'Idle · 37 °C · VRAM 298 MB of 11 GB');
  });

  testWidgets('sizes itself in an IntrinsicHeight row, as metric cards are laid out', (tester) async {
    await _pump(tester, 160, (l) => l, facts);
    final plain = tester.getSize(find.byType(FactLine)).height;
    await _pump(tester, 160, (l) => IntrinsicHeight(child: Row(crossAxisAlignment: CrossAxisAlignment.stretch, children: [Expanded(child: l)])), facts);
    expect(tester.takeException(), isNull);
    expect(tester.getSize(find.byType(FactLine)).height, plain, reason: 'both lines, measured the same way');
  });
}
