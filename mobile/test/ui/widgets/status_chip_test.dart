import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/theme/status_colors.dart';
import 'package:nivaroos_mobile/ui/widgets/status_chip.dart';

import '../pump.dart';

void main() {
  testWidgets('always pairs the label with an icon', (tester) async {
    for (final status in Status.values) {
      await pumpUi(tester, StatusChip(label: status.name, status: status));
      expect(find.text(status.name), findsOneWidget);
      expect(find.byIcon(StatusChip.defaultIcon(status)), findsOneWidget);
    }
  });

  testWidgets('uses the tonal container of its status', (tester) async {
    await pumpUi(tester, const StatusChip(label: 'Running', status: Status.success));
    final box = tester.widget<DecoratedBox>(find.descendant(of: find.byType(StatusChip), matching: find.byType(DecoratedBox)));
    expect((box.decoration as BoxDecoration).color, StatusColors.light.success.container);
    final text = tester.widget<Text>(find.text('Running'));
    expect(text.style?.color, StatusColors.light.success.onContainer);
  });

  testWidgets('a custom icon replaces the default, and TalkBack reads the label once', (tester) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, const StatusChip(label: 'Stopped', status: Status.neutral, icon: Icons.stop_circle_outlined));
    expect(find.byIcon(Icons.stop_circle_outlined), findsOneWidget);
    expect(find.bySemanticsLabel('Stopped'), findsOneWidget);
    handle.dispose();
  });
}
