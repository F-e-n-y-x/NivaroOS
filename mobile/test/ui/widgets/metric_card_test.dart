import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import '../pump.dart';

void main() {
  // Memory's "Free up": its own 48dp target and label in every style, and
  // a tap on it doesn't open the card.
  for (final d in DesignDirection.values) {
    testWidgets('a header action has its own target and semantics (${d.name})', (tester) async {
      final handle = tester.ensureSemantics();
      var opened = 0, pressed = 0;
      await pumpUi(
        tester,
        Center(
          child: SizedBox(
            width: 190,
            height: 190,
            child: MetricCard(
              icon: Icons.developer_board_outlined,
              label: 'Memory',
              value: '42',
              unit: '%',
              detail: '14 of 31 GB',
              onTap: () => opened++,
              action: MetricCardAction(icon: Icons.cleaning_services_outlined, tooltip: 'Free up memory', onPressed: () => pressed++),
              semanticLabel: 'Memory, 42 percent in use.',
              body: const SizedBox.expand(),
            ),
          ),
        ),
        direction: d,
      );
      final button = find.byType(IconButton);
      final size = tester.getSize(button);
      expect(size.width, greaterThanOrEqualTo(48));
      expect(size.height, greaterThanOrEqualTo(48));
      // Inside the card, at its top end.
      final card = tester.getRect(find.byType(MetricCard));
      final b = tester.getRect(button);
      expect(card.contains(b.topLeft) && card.contains(b.bottomRight - const Offset(1, 1)), isTrue);
      expect(b.right, closeTo(card.right, 12));
      expect(tester.getSemantics(button), isSemantics(tooltip: 'Free up memory', isButton: true, hasTapAction: true));
      expect(find.bySemanticsLabel('Memory, 42 percent in use.'), findsOneWidget);

      await tester.tap(button);
      await tester.pump();
      expect(pressed, 1);
      expect(opened, 0);
      await tester.tapAt(card.center);
      await tester.pump();
      expect(opened, 1);
      handle.dispose();
    });
  }
}
