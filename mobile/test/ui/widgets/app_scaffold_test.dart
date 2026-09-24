import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/theme/app_theme.dart';
import 'package:nivaroos_mobile/ui/theme/spacing.dart';
import 'package:nivaroos_mobile/ui/widgets/app_scaffold.dart';
import 'package:nivaroos_mobile/ui/widgets/section_header.dart';

Future<void> _pump(WidgetTester tester, Widget scaffold, {Size size = const Size(412, 915)}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  tester.view.padding = const FakeViewPadding(top: 24, bottom: 24);
  addTearDown(tester.view.reset);
  await tester.pumpWidget(MaterialApp(theme: AppTheme.light(), home: scaffold));
}

List<Widget> _rows(int n) => [for (var i = 0; i < n; i++) ListTile(title: Text('Row $i'))];

void main() {
  testWidgets('box form: small app bar, body, FAB, pull-to-refresh', (tester) async {
    var refreshed = 0;
    await _pump(tester, AppScaffold(
      title: 'Logs',
      actions: [IconButton(tooltip: 'Search', onPressed: () {}, icon: const Icon(Icons.search))],
      onRefresh: () async => refreshed++,
      floatingActionButton: FloatingActionButton(onPressed: () {}, child: const Icon(Icons.add)),
      body: ListView(children: _rows(3)),
    ));
    expect(find.widgetWithText(AppBar, 'Logs'), findsOneWidget);
    expect(find.byTooltip('Search'), findsOneWidget);
    expect(find.byType(FloatingActionButton), findsOneWidget);
    await tester.fling(find.text('Row 0'), const Offset(0, 400), 1000);
    await tester.pumpAndSettle();
    expect(refreshed, 1);
  });

  testWidgets('sliver form: medium app bar that collapses, content after the bottom inset', (tester) async {
    await _pump(tester, AppScaffold.slivers(title: 'More', slivers: [SliverList.list(children: _rows(30))]));
    expect(find.byType(SliverAppBar), findsOneWidget);
    double barExtent() => tester.renderObject<RenderSliver>(find.byType(SliverAppBar)).geometry!.paintExtent;
    final expanded = barExtent();
    await tester.drag(find.text('Row 2'), const Offset(0, -300));
    await tester.pump();
    expect(barExtent(), lessThan(expanded));
    // Scrolled to the end, the last row clears the 24dp gesture inset.
    await tester.dragUntilVisible(find.text('Row 29'), find.byType(CustomScrollView), const Offset(0, -500));
    await tester.drag(find.byType(CustomScrollView), const Offset(0, -2000));
    await tester.pumpAndSettle();
    expect(tester.getBottomLeft(find.text('Row 29')).dy, lessThanOrEqualTo(915 - 24));
  });

  testWidgets('offline banner appears under the app bar with Retry', (tester) async {
    var retried = false;
    await _pump(tester, AppScaffold(
      title: 'Apps',
      banner: OfflineBanner(lastUpdated: clock.now().subtract(const Duration(minutes: 5)), onRetry: () => retried = true),
      body: ListView(children: _rows(2)),
    ));
    await tester.pumpAndSettle();
    expect(find.text('Offline · last updated 5 min ago'), findsOneWidget);
    expect(tester.getTopLeft(find.byType(OfflineBanner)).dy, greaterThanOrEqualTo(tester.getBottomLeft(find.byType(AppBar)).dy));
    await tester.tap(find.text('Retry'));
    expect(retried, isTrue);
  });

  testWidgets('offline banner with nothing cached says the server is unreachable', (tester) async {
    await _pump(tester, const AppScaffold(title: 'Apps', banner: OfflineBanner(), body: SizedBox()));
    await tester.pumpAndSettle();
    expect(find.text("Can't reach the server"), findsOneWidget);
    expect(find.text('Retry'), findsNothing);
  });

  testWidgets('content is capped to the reading width on tablets', (tester) async {
    await _pump(tester, AppScaffold(title: 'Settings', body: ListView(children: _rows(1))), size: const Size(1280, 800));
    expect(tester.getSize(find.byType(ListTile)).width, 840);
    await _pump(tester, AppScaffold(title: 'Files', maxContentWidth: null, body: ListView(children: _rows(1))), size: const Size(1280, 800));
    expect(tester.getSize(find.byType(ListTile)).width, 1280);
  });

  testWidgets('box form: a body that does not fill the height starts at the top', (tester) async {
    await _pump(tester, const AppScaffold(title: 'About', body: Text('Version 1.2')), size: const Size(1280, 800));
    expect(tester.getTopLeft(find.text('Version 1.2')).dy, tester.getBottomLeft(find.byType(AppBar)).dy);
  });

  testWidgets('sliver form: the offline banner stays pinned under the bar while scrolling', (tester) async {
    await _pump(tester, AppScaffold.slivers(
      title: 'Apps',
      banner: const OfflineBanner(),
      slivers: [SliverList.list(children: _rows(40))],
    ));
    await tester.pumpAndSettle();
    await tester.drag(find.byType(CustomScrollView), const Offset(0, -1500));
    await tester.pumpAndSettle();
    expect(find.text("Can't reach the server"), findsOneWidget);
    // Right under the collapsed bar: the status bar inset plus the 64dp M3 bar.
    expect(tester.getTopLeft(find.byType(OfflineBanner)).dy, closeTo(24 + 64, 0.5));
  });

  testWidgets('sliver form: pull-to-refresh starts below the expanded title', (tester) async {
    await _pump(tester, AppScaffold.slivers(title: 'More', onRefresh: () async {}, slivers: [SliverList.list(children: _rows(3))]));
    expect(tester.widget<RefreshIndicator>(find.byType(RefreshIndicator)).edgeOffset, 24 + 112);
    await _pump(tester, AppScaffold.slivers(
      title: 'Logs',
      collapsingTitle: false,
      onRefresh: () async {},
      slivers: [SliverList.list(children: _rows(3))],
    ));
    expect(tester.widget<RefreshIndicator>(find.byType(RefreshIndicator)).edgeOffset, 24 + 64);
  });

  testWidgets('takes a tab bar under the title, and a selection bar that replaces it', (tester) async {
    await _pump(tester, DefaultTabController(
      length: 2,
      child: AppScaffold(
        title: 'Files',
        bottom: const TabBar(tabs: [Tab(text: 'Storage'), Tab(text: 'Photos')]),
        body: ListView(children: _rows(2)),
      ),
    ));
    expect(find.descendant(of: find.byType(AppBar), matching: find.byType(TabBar)), findsOneWidget);

    await _pump(tester, AppScaffold.slivers(
      title: 'Files',
      appBar: AppBar(title: const Text('3 selected')),
      slivers: [SliverList.list(children: _rows(2))],
    ));
    expect(find.text('3 selected'), findsOneWidget);
    expect(find.byType(SliverAppBar), findsNothing);
  });

  testWidgets('headers and rows share the pane gutter: 16 on phones, 24 on tablets', (tester) async {
    Widget screen() => AppScaffold(
          title: 'Settings',
          body: ListView(children: const [SectionHeader(title: 'General'), ListTile(title: Text('Row'))]),
        );
    for (final (size, gutter) in [(const Size(412, 915), Space.lg), (const Size(1280, 800), Space.xl)]) {
      await _pump(tester, screen(), size: size);
      final content = tester.getTopLeft(find.byType(ListView)).dx;
      expect(tester.getTopLeft(find.text('General')).dx - content, gutter, reason: '$size');
      expect(tester.getTopLeft(find.text('Row')).dx - content, gutter, reason: '$size');
    }
  });

  testWidgets('the gutter follows the pane, not the window', (tester) async {
    // A 640dp window with an 80dp rail leaves a 560dp pane: a phone gutter.
    await _pump(tester, Row(children: [
      const SizedBox(width: 80),
      Expanded(child: AppScaffold(title: 'Apps', body: ListView(children: const [SectionHeader(title: 'Installed')]))),
    ]), size: const Size(640, 800));
    expect(tester.getTopLeft(find.text('Installed')).dx, 80 + Space.lg);
  });

  testWidgets('on tablets the title starts where the capped content does', (tester) async {
    await _pump(tester, AppScaffold.slivers(
      title: 'More',
      slivers: [SliverList.list(children: const [SectionHeader(title: 'Server')])],
    ), size: const Size(1280, 800));
    // (1280 - 840) / 2 = 220 inset, plus the 24dp tablet gutter.
    expect(tester.getTopLeft(find.text('Server')).dx, 244);
    // Both the large title and the collapsed one (hidden until it scrolls).
    for (final e in find.text('More').evaluate()) {
      expect((e.renderObject! as RenderBox).localToGlobal(Offset.zero).dx, 244);
    }
  });
}
