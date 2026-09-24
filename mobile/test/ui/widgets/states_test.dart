import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/theme/app_theme.dart';
import 'package:nivaroos_mobile/ui/widgets/states.dart';

import '../pump.dart';

void main() {
  testWidgets('EmptyState shows its message and its one action', (tester) async {
    var taps = 0;
    await pumpUi(tester, EmptyState(
      icon: Icons.apps_outlined,
      title: 'No apps yet',
      message: 'Apps you install appear here.',
      actionLabel: 'Open app store',
      onAction: () => taps++,
    ));
    expect(find.text('No apps yet'), findsOneWidget);
    expect(find.text('Apps you install appear here.'), findsOneWidget);
    await tester.tap(find.text('Open app store'));
    expect(taps, 1);
  });

  testWidgets('EmptyState without an action shows no button', (tester) async {
    await pumpUi(tester, const EmptyState(icon: Icons.folder_outlined, title: 'Empty folder', message: 'Nothing here yet.'));
    expect(find.byType(FilledButton), findsNothing);
  });

  testWidgets('ErrorState retries, and copies details when there are some', (tester) async {
    String? copied;
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, (call) async {
      if (call.method == 'Clipboard.setData') copied = (call.arguments as Map)['text'] as String;
      return null;
    });
    var retries = 0;
    await pumpUi(tester, ErrorState(title: "Couldn't load the logs", message: 'The server answered with an error.', onRetry: () => retries++, details: 'GET /v1/sys/logs → 500'));
    expect(find.text("Couldn't load the logs"), findsOneWidget);
    expect(tester.widget<Icon>(find.byIcon(Icons.error_outline)).color, AppTheme.light().colorScheme.error);
    await tester.tap(find.text('Retry'));
    expect(retries, 1);
    await tester.tap(find.text('Copy details'));
    await tester.pump();
    expect(copied, 'GET /v1/sys/logs → 500');
    expect(find.text('Details copied'), findsOneWidget);
  });

  testWidgets('ErrorState.offline uses the shared wording and has no details button', (tester) async {
    await pumpUi(tester, ErrorState.offline(onRetry: () {}));
    expect(find.text("Can't reach the server"), findsOneWidget);
    expect(find.byIcon(Icons.cloud_off_outlined), findsOneWidget);
    expect(find.text('Copy details'), findsNothing);
    // Offline is a situation, not a failure: a neutral icon, not red.
    expect(tester.widget<Icon>(find.byIcon(Icons.cloud_off_outlined)).color, AppTheme.light().colorScheme.onSurfaceVariant);
  });

  testWidgets('the sliver forms fill the rest of a scroll view under other slivers', (tester) async {
    await pumpUi(tester, CustomScrollView(slivers: [
      const SliverToBoxAdapter(child: SizedBox(height: 100, child: Text('Header'))),
      ErrorState.offline(onRetry: () {}, sliver: true),
    ]));
    expect(find.text("Can't reach the server"), findsOneWidget);
    // Centred in the space below the header, not in the whole screen.
    final top = tester.getTopLeft(find.byIcon(Icons.cloud_off_outlined)).dy;
    expect(top, greaterThan(100));

    await pumpUi(tester, const CustomScrollView(slivers: [
      EmptyState(icon: Icons.inbox_outlined, title: 'Nothing here', message: 'Pull to refresh.', sliver: true),
    ]));
    expect(find.text('Nothing here'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('states stay scrollable so pull-to-refresh works on them', (tester) async {
    var refreshed = false;
    await pumpUi(tester, RefreshIndicator(
      onRefresh: () async => refreshed = true,
      child: const EmptyState(icon: Icons.inbox_outlined, title: 'Nothing', message: 'Pull to refresh.'),
    ));
    await tester.fling(find.text('Nothing'), const Offset(0, 400), 1000);
    await tester.pumpAndSettle();
    expect(refreshed, isTrue);
  });

  testWidgets('fits a small phone at 200% text without overflowing', (tester) async {
    tester.view.physicalSize = const Size(360, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(MaterialApp(
      home: MediaQuery(
        data: const MediaQueryData(size: Size(360, 640), textScaler: TextScaler.linear(2)),
        child: Scaffold(body: ErrorState(title: "Couldn't load apps", message: 'The server took too long to answer. Check the connection and try again.', onRetry: () {}, details: 'x')),
      ),
    ));
    expect(tester.takeException(), isNull);
  });
}
