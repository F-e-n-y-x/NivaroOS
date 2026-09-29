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
Future<void> _pumpTab(WidgetTester tester, Widget tab, {DesignDirection direction = DesignDirection.defaultDirection, Size size = _phone, double textScale = 1, Brightness brightness = Brightness.light, int selected = 0}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  tester.view.padding = const FakeViewPadding(top: 24, bottom: _inset);
  tester.view.viewPadding = const FakeViewPadding(top: 24, bottom: _inset);
  addTearDown(tester.view.reset);
  await tester.pumpWidget(MaterialApp(
    theme: AppTheme.build(brightness: brightness, direction: direction),
    builder: (context, app) => MediaQuery(data: MediaQuery.of(context).copyWith(textScaler: TextScaler.linear(textScale)), child: app!),
    home: ShellFrame(selectedIndex: selected, onDestinationSelected: (_) {}, body: tab),
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
      expect(bar.height, DesignTokens.of(tester.element(find.byType(FloatingNavigationBar))).navBar.height);
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

  for (final location in [FloatingActionButtonLocation.endFloat, FloatingActionButtonLocation.centerFloat]) {
    testWidgets('a tab\'s FAB and a snack bar sit above the bar, and the snack bar clears the FAB ($location)', (tester) async {
      await _pumpTab(
        tester,
        AppScaffold.slivers(
          title: 'Long',
          floatingActionButton: FloatingActionButton.extended(onPressed: () {}, label: const Text('New'), icon: const Icon(Icons.add)),
          floatingActionButtonLocation: location,
          slivers: [SliverList.list(children: [for (var i = 0; i < 60; i++) ListTile(title: Text('Row $i'))])],
        ),
      );
      final bar = _bar(tester);
      expect(tester.getRect(find.byType(FloatingActionButton)).bottom, lessThanOrEqualTo(bar.top - Space.sm));

      // Shown from the tab, as every screen does: the tab's own Scaffold
      // (the root one: the shell is not a Scaffold) draws it, so it knows
      // the FAB. It used to be drawn on the shell's and cover it.
      ScaffoldMessenger.of(tester.element(find.byType(CustomScrollView))).showSnackBar(const SnackBar(content: Text('Moved to the Trash')));
      await tester.pumpAndSettle();
      final snack = tester.getRect(find.byType(SnackBar));
      final fab = tester.getRect(find.byType(FloatingActionButton));
      expect(snack.bottom, lessThanOrEqualTo(bar.top));
      expect(snack.overlaps(bar), isFalse);
      expect(snack.overlaps(fab), isFalse, reason: 'snack $snack over FAB $fab');
      expect(find.byType(SnackBar), findsOneWidget);
    });
  }

  testWidgets('a snack bar without a FAB sits just above the bar', (tester) async {
    await _pumpTab(tester, _longList());
    ScaffoldMessenger.of(tester.element(find.byType(CustomScrollView))).showSnackBar(const SnackBar(content: Text('Saved')));
    await tester.pumpAndSettle();
    final snack = tester.getRect(find.byType(SnackBar));
    final bar = _bar(tester);
    expect(snack.bottom, lessThanOrEqualTo(bar.top));
    expect(bar.top - snack.bottom, lessThan(Space.xl), reason: 'the snack bar floats away from the bar');
  });

  // Owner request (2026-09-29): the selected destination is highlighted as
  // a whole - one indicator behind both its icon and its label.
  for (final d in DesignDirection.values) {
    for (final b in Brightness.values) {
      testWidgets('${d.name} ${b.name}: the indicator holds the selected icon and label', (tester) async {
        await _pumpTab(tester, _longList(), direction: d, brightness: b, selected: 1);
        final indicator = find.descendant(
          of: find.byType(FloatingNavigationBar),
          matching: find.byWidgetPredicate((w) => w is DecoratedBox && w.decoration is ShapeDecoration),
        );
        expect(indicator, findsOneWidget, reason: 'one indicator, on the selected destination');
        final pill = tester.getRect(indicator);
        final label = tester.getRect(find.text('Files'));
        final icon = tester.getRect(find.byIcon(Icons.folder));
        for (final r in [label, icon]) {
          expect(pill.contains(r.topLeft) && pill.contains(r.bottomRight - const Offset(.01, .01)), isTrue, reason: '$r outside the indicator $pill');
        }
        expect(pill.contains(tester.getRect(find.text('Home')).center), isFalse);
        final bar = _bar(tester);
        expect(pill.top - bar.top, closeTo(FloatingNavigationBar.indicatorInset, .01));
        expect(bar.bottom - pill.bottom, closeTo(FloatingNavigationBar.indicatorInset, .01));
        // The selected label takes the indicator's colours.
        final theme = Theme.of(tester.element(find.byType(FloatingNavigationBar)));
        final text = tester.widget<RichText>(find.descendant(of: find.text('Files'), matching: find.byType(RichText)));
        final expected = theme.navigationBarTheme.iconTheme?.resolve({WidgetState.selected})?.color ?? theme.colorScheme.onSecondaryContainer;
        expect(text.text.style!.color, expected);
      });
    }
  }

  testWidgets('switching animates the indicator to the new destination', (tester) async {
    var selected = 0;
    tester.view.physicalSize = _phone;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(MaterialApp(
      theme: AppTheme.build(brightness: Brightness.light),
      home: StatefulBuilder(
        builder: (context, setState) => ShellFrame(selectedIndex: selected, onDestinationSelected: (i) => setState(() => selected = i), body: _longList()),
      ),
    ));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Apps'));
    expect(selected, 2);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 60));
    // Mid-way both are drawn: the old fading out, the new growing in.
    expect(find.descendant(of: find.byType(FloatingNavigationBar), matching: find.byType(Opacity)), findsNWidgets(2));
    await tester.pumpAndSettle();
    expect(find.descendant(of: find.byType(FloatingNavigationBar), matching: find.byType(Opacity)), findsOneWidget);
  });

  testWidgets('each destination is a selectable tab to assistive tech', (tester) async {
    final handle = tester.ensureSemantics();
    await _pumpTab(tester, _longList(), selected: 3);
    expect(
      tester.getSemantics(find.descendant(of: find.byType(FloatingNavigationBar), matching: find.bySemanticsLabel('VMs'))),
      matchesSemantics(label: 'VMs', hint: 'Tab 4 of 5', isButton: true, isSelected: true, hasSelectedState: true, isInMutuallyExclusiveGroup: true, hasTapAction: true),
    );
    expect(
      tester.getSemantics(find.descendant(of: find.byType(FloatingNavigationBar), matching: find.bySemanticsLabel('Home'))),
      matchesSemantics(label: 'Home', hint: 'Tab 1 of 5', isButton: true, hasSelectedState: true, isInMutuallyExclusiveGroup: true, hasTapAction: true),
    );
    handle.dispose();
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
