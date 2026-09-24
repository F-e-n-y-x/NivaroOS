import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/widgets/loading_list.dart';

import '../pump.dart';

void main() {
  testWidgets('draws the requested number of skeleton rows', (tester) async {
    await pumpUi(tester, const LoadingList(rows: 4));
    // Each row: a leading box and two lines.
    expect(find.byType(SkeletonBox), findsNWidgets(12));
    await pumpUi(tester, const LoadingList(rows: 3, leading: SkeletonLeading.none, subtitle: false));
    expect(find.byType(SkeletonBox), findsNWidgets(3));
    await pumpUi(tester, const LoadingList(rows: 2, trailing: true));
    expect(find.byType(SkeletonBox), findsNWidgets(8));
  });

  testWidgets('the leading placeholder takes the shape of the real rows', (tester) async {
    const sizes = {SkeletonLeading.icon: 24.0, SkeletonLeading.avatar: 40.0, SkeletonLeading.thumbnail: 56.0};
    for (final e in sizes.entries) {
      await pumpUi(tester, LoadingList(rows: 1, leading: e.key));
      expect(tester.getSize(find.byType(SkeletonBox).first), Size(e.value, e.value), reason: e.key.name);
    }
  });

  testWidgets('rows grow with the text scale, like the ListTiles they stand for', (tester) async {
    // A two-line skeleton row: at least as tall as a two-line ListTile.
    final row = find.byWidgetPredicate((w) => w is ConstrainedBox && w.constraints.minHeight == 72);
    double rowHeight() => tester.getSize(row).height;

    await pumpUi(tester, const LoadingList(rows: 1));
    final normal = rowHeight();
    await tester.pumpWidget(MaterialApp(
      home: MediaQuery(
        data: const MediaQueryData(textScaler: TextScaler.linear(2)),
        child: const Scaffold(body: LoadingList(rows: 1)),
      ),
    ));
    expect(rowHeight(), greaterThan(normal));
  });

  testWidgets('the sliver form sits in a CustomScrollView and announces once', (tester) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, const CustomScrollView(slivers: [SliverLoadingList(rows: 3)]));
    expect(find.byType(SkeletonBox), findsNWidgets(9));
    expect(find.bySemanticsLabel('Loading'), findsOneWidget);
    handle.dispose();
  });

  testWidgets('announces "Loading" once instead of reading the boxes', (tester) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, const LoadingList());
    expect(find.bySemanticsLabel('Loading'), findsOneWidget);
    handle.dispose();
  });

  testWidgets('pulses, and holds still when animations are off', (tester) async {
    await pumpUi(tester, const LoadingList(rows: 1));
    final fade = find.descendant(of: find.byType(SkeletonPulse), matching: find.byType(FadeTransition));
    final a = tester.widget<FadeTransition>(fade).opacity.value;
    await tester.pump(const Duration(milliseconds: 450));
    final b = tester.widget<FadeTransition>(fade).opacity.value;
    expect(b, isNot(a));
    // Never dimmer than 60%, so the rows stay visible on a light page.
    expect(b, greaterThanOrEqualTo(SkeletonPulse.lowestOpacity));

    await pumpUi(tester, const LoadingList(rows: 1), disableAnimations: true);
    await tester.pump();
    expect(tester.widget<FadeTransition>(fade).opacity.value, 1);
    // No animation is left running, so the tree settles.
    await tester.pumpAndSettle();
  });
}
