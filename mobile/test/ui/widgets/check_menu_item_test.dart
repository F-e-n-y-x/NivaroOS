import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

void main() {
  Future<void> openMenu(WidgetTester tester, {required bool checked}) async {
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        appBar: AppBar(actions: [
          PopupMenuButton<String>(
            itemBuilder: (_) => [
              const PopupMenuItem(value: 'folder', child: Text('New folder')),
              checkMenuItem(value: 'hidden', checked: checked, label: 'Show hidden files'),
            ],
          ),
        ]),
      ),
    ));
    await tester.tap(find.byType(PopupMenuButton<String>));
    await tester.pumpAndSettle();
  }

  for (final checked in [false, true]) {
    testWidgets('the label lines up with plain entries (checked: $checked)', (tester) async {
      await openMenu(tester, checked: checked);
      expect(tester.getTopLeft(find.text('Show hidden files')).dx, tester.getTopLeft(find.text('New folder')).dx);
      expect(find.byIcon(Icons.check), checked ? findsOneWidget : findsNothing);
    });
  }

  testWidgets('reports its checked state to accessibility services', (tester) async {
    final handle = tester.ensureSemantics();
    await openMenu(tester, checked: true);
    expect(tester.getSemantics(find.text('Show hidden files')), matchesSemantics(isChecked: true, hasCheckedState: true, label: 'Show hidden files', isEnabled: true, hasEnabledState: true, isFocusable: true, hasTapAction: true, hasFocusAction: true, isButton: true));
    handle.dispose();
  });
}
