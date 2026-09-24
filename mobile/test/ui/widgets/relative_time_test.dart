import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/widgets/relative_time.dart';

import '../pump.dart';

void main() {
  final now = DateTime(2026, 9, 25, 14, 3);

  test('formats relative to now', () {
    expect(formatRelative(now.subtract(const Duration(seconds: 20)), now: now), 'Just now');
    expect(formatRelative(now.subtract(const Duration(minutes: 2)), now: now), '2 min ago');
    expect(formatRelative(now.subtract(const Duration(hours: 3)), now: now), '3 h ago');
    expect(formatRelative(DateTime(2026, 9, 24, 23, 50), now: now), 'Yesterday');
    expect(formatRelative(DateTime(2026, 9, 21, 9), now: now), '4 days ago');
    expect(formatRelative(DateTime(2026, 9, 2), now: now), 'Sep 2');
    expect(formatRelative(DateTime(2025, 12, 31), now: now), 'Dec 31, 2025');
    // Just after midnight, a few minutes back is still minutes, not "Yesterday".
    expect(formatRelative(DateTime(2026, 9, 24, 23, 58), now: DateTime(2026, 9, 25, 0, 3)), '5 min ago');
  });

  test('exact time', () {
    expect(formatExact(now), 'Sep 25, 2026 2:03 PM');
  });

  testWidgets('shows the exact time on long-press', (tester) async {
    final time = clock.now().subtract(const Duration(minutes: 5));
    await pumpUi(tester, Center(child: RelativeTime(time, prefix: 'Updated ')));
    expect(find.text('Updated 5 min ago'), findsOneWidget);
    await tester.longPress(find.byType(RelativeTime));
    await tester.pump(const Duration(milliseconds: 200));
    expect(find.text(formatExact(time)), findsOneWidget);
  });

  testWidgets('keeps itself current', (tester) async {
    await pumpUi(tester, RelativeTime(clock.now()));
    expect(find.text('Just now'), findsOneWidget);
    // Time in the test moves only with pump, so the label is re-read after a
    // tick rather than checked against a later clock.
    await tester.pump(const Duration(minutes: 1));
    expect(find.byType(RelativeTime), findsOneWidget);
  });
}
