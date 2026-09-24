import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/theme/spacing.dart';
import 'package:nivaroos_mobile/ui/widgets/section_header.dart';
import 'package:nivaroos_mobile/ui/widgets/tile_group.dart';

import '../pump.dart';

void main() {
  testWidgets('header, rows as separate segments, and a footer', (tester) async {
    await pumpUi(tester, const TileGroup(
      title: 'Server',
      footer: 'Only admins see these.',
      children: [ListTile(title: Text('Updates')), ListTile(title: Text('Logs')), ListTile(title: Text('Terminal'))],
    ));
    expect(find.byType(SectionHeader), findsOneWidget);
    expect(find.byType(Divider), findsNothing);
    expect(find.text('Only admins see these.'), findsOneWidget);
    // One segment per row, a 2dp gap apart, above the footer.
    final cards = find.byType(Card);
    expect(cards, findsNWidgets(3));
    expect(tester.getTopLeft(cards.at(1)).dy - tester.getBottomLeft(cards.at(0)).dy, TileGroup.gap);
    expect(tester.getBottomLeft(cards.last).dy, lessThan(tester.getTopLeft(find.text('Only admins see these.')).dy));
    // Large corners outside the block, small ones at the joins.
    BorderRadius radius(int i) => (tester.widget<Card>(cards.at(i)).shape! as RoundedRectangleBorder).borderRadius as BorderRadius;
    expect(radius(0).topLeft, const Radius.circular(Corners.large));
    expect(radius(0).bottomLeft, const Radius.circular(Corners.extraSmall));
    expect(radius(2).bottomLeft, const Radius.circular(Corners.large));
  });

  testWidgets('the header lines up with the rows\' leading icons', (tester) async {
    await pumpUi(tester, const TileGroup(
      title: 'Server',
      children: [ListTile(leading: Icon(Icons.update_outlined), title: Text('Updates'))],
    ));
    expect(tester.getTopLeft(find.text('Server')).dx, tester.getTopLeft(find.byIcon(Icons.update_outlined)).dx);
  });

  testWidgets('without a title there is no header', (tester) async {
    await pumpUi(tester, const TileGroup(children: [ListTile(title: Text('Only'))]));
    expect(find.byType(SectionHeader), findsNothing);
    final shape = tester.widget<Card>(find.byType(Card)).shape! as RoundedRectangleBorder;
    expect(shape.borderRadius, BorderRadius.circular(Corners.large));
  });
}
