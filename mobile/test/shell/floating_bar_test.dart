// The floating navigation bar (owner request, 2026-09-26): content scrolls
// behind it, and nothing a screen needs ends up under it - the last row of
// a long list, a FAB (as the Files paste bar is), a snack bar.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/home_shell.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

const _phone = Size(412, 915);
const _inset = 24.0;

/// One tab in the shell's frame, in [direction], on a phone with a
/// gesture bar.
Future<void> _pumpTab(WidgetTester tester, Widget tab, {DesignDirection direction = DesignDirection.defaultDirection, Size size = _phone, double textScale = 1}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  tester.view.padding = const FakeViewPadding(top: 24, bottom: _inset);
  tester.view.viewPadding = const FakeViewPadding(top: 24, bottom: _inset);
  addTearDown(tester.view.reset);
  await tester.pumpWidget(MaterialApp(
    theme: AppTheme.build(brightness: Brightness.light, direction: direction),
    builder: (context, app) => MediaQuery(data: MediaQuery.of(context).copyWith(textScaler: TextScaler.linear(textScale)), child: app!),
    home: ShellFrame(selectedIndex: 0, onDestinationSelected: (_) {}, body: tab),
  ));
  await tester.pumpAndSettle();
}

/// The bar's own surface (inside its margins).
Rect _bar(WidgetTester tester) => tester.getRect(find.descendant(of: find.byType(FloatingNavigationBar), matching: find.byType(Material)).first);

Widget _longList({Widget? fab}) => AppScaffold.slivers(
      title: 'Long',
      floatingActionButton: fab,
      slivers: [
        SliverList.list(children: [for (var i = 0; i < 60; i++) ListTile(title: Text('Row $i'))]),
      ],
    );

void main() {
  for (final d in DesignDirection.values) {
    testWidgets('${d.name}: the bar floats clear of the edges and the gesture bar', (tester) async {
      await _pumpTab(tester, _longList(), direction: d);
      final bar = _bar(tester);
      expect(bar.left, 16);
      expect(bar.right, _phone.width - 16);
      expect(bar.bottom, _phone.height - _inset - Space.sm);
      expect(bar.height, DesignTokens.of(tester.element(find.byType(NavigationBar))).navBar.height);
      // The targets stay at least 48dp.
      for (final label in ['Home', 'Files', 'Apps', 'VMs', 'More']) {
        final target = tester.getRect(find.ancestor(of: find.text(label), matching: find.byWidgetPredicate((w) => w is InkResponse)).first);
        expect(target.width, greaterThanOrEqualTo(48), reason: label);
        expect(target.height, greaterThanOrEqualTo(48), reason: label);
      }
    });

    testWidgets('${d.name}: the last row of a long list scrolls fully above the bar', (tester) async {
      await _pumpTab(tester, _longList(), direction: d);
      // Content runs behind the bar before the end.
      expect(tester.getRect(find.byType(CustomScrollView)).bottom, _phone.height);
      await tester.drag(find.byType(CustomScrollView), const Offset(0, -10000));
      await tester.pumpAndSettle();
      final last = tester.getRect(find.widgetWithText(ListTile, 'Row 59'));
      expect(last.bottom, lessThanOrEqualTo(_bar(tester).top));
    });
  }

  testWidgets('at 200% text the bar keeps its height and the last row still clears it', (tester) async {
    await _pumpTab(tester, _longList(), size: const Size(360, 740), textScale: 2);
    expect(tester.takeException(), isNull);
    await tester.drag(find.byType(CustomScrollView), const Offset(0, -20000));
    await tester.pumpAndSettle();
    expect(tester.getRect(find.widgetWithText(ListTile, 'Row 59')).bottom, lessThanOrEqualTo(_bar(tester).top));
  });

  testWidgets('a tab\'s FAB and a snack bar sit above the bar', (tester) async {
    await _pumpTab(tester, _longList(fab: FloatingActionButton.extended(onPressed: () {}, label: const Text('New'), icon: const Icon(Icons.add))));
    final bar = _bar(tester);
    final fab = tester.getRect(find.byType(FloatingActionButton));
    expect(fab.bottom, lessThanOrEqualTo(bar.top - Space.sm));

    ScaffoldMessenger.of(tester.element(find.byType(CustomScrollView))).showSnackBar(const SnackBar(content: Text('Moved to the Trash')));
    await tester.pumpAndSettle();
    final snack = tester.getRect(find.byType(SnackBar));
    expect(snack.bottom, lessThanOrEqualTo(bar.top));
    expect(snack.overlaps(bar), isFalse);
  });

  testWidgets('with the keyboard up the tab leaves no room for the hidden bar', (tester) async {
    tester.view.viewInsets = const FakeViewPadding(bottom: 300);
    addTearDown(tester.view.resetViewInsets);
    late double room;
    await _pumpTab(tester, Builder(builder: (context) {
      room = MediaQuery.paddingOf(context).bottom;
      return const SizedBox.expand();
    }));
    expect(room, 0);
  });
}
