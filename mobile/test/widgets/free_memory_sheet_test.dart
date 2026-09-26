// The "Free up memory" sheet: what it explains, when it offers to empty
// swap, what it sends, and what it shows afterwards or on a refusal.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/widgets/free_memory_sheet.dart';

import '../screenshots/harness.dart';
import '../ui/pump.dart';

const _gb = 1024 * 1024 * 1024;

MemoryClearResult _result({bool swap = false}) => MemoryClearResult(
      before: const MemorySnapshot(free: 1288490189, available: 16 * _gb, cached: 14 * _gb, swapUsed: 2 * _gb),
      after: MemorySnapshot(free: 3543348020, available: 16 * _gb, cached: 12 * _gb, swapUsed: swap ? 0 : 2 * _gb),
      freed: 3543348020 - 1288490189,
      swapReclaimed: swap,
    );

Future<void> _open(WidgetTester tester, {required int swapUsed, required FreeMemoryRunner run, VoidCallback? onDone}) async {
  await pumpUi(
    tester,
    Builder(
      builder: (context) => Center(
        child: TextButton(onPressed: () => showFreeMemorySheet(context, swapUsed: swapUsed, run: run, onDone: onDone), child: const Text('open')),
      ),
    ),
  );
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('explains the cache honestly; no swap choice without swap in use', (tester) async {
    await _open(tester, swapUsed: 0, run: ({required swap}) async => _result());
    expect(find.text('Free up memory?'), findsOneWidget);
    expect(
      find.text('Linux uses spare memory as a cache to speed things up; clearing it frees memory now, but things may be slower for a moment while the cache refills.'),
      findsOneWidget,
    );
    expect(find.text('Also empty swap'), findsNothing);
  });

  testWidgets('Cancel sends nothing', (tester) async {
    var calls = 0;
    await _open(tester, swapUsed: 2 * _gb, run: ({required swap}) async {
      calls++;
      return _result();
    });
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(calls, 0);
    expect(find.text('Free up memory?'), findsNothing);
  });

  testWidgets('frees memory, then shows what was freed with before → after', (tester) async {
    final semantics = tester.ensureSemantics();
    bool? sentSwap;
    var done = 0;
    await _open(tester, swapUsed: 2 * _gb, onDone: () => done++, run: ({required swap}) async {
      sentSwap = swap;
      return _result();
    });
    expect(find.text('Also empty swap'), findsOneWidget);
    await tester.tap(find.widgetWithText(FilledButton, 'Free up memory'));
    await tester.pumpAndSettle();
    expect(sentSwap, isFalse, reason: 'swap is only emptied when ticked');
    expect(find.text('Freed 2.1 GB'), findsOneWidget);
    expect(find.bySemanticsLabel(RegExp('1.2 GB to 3.3 GB')), findsOneWidget);
    expect(find.bySemanticsLabel(RegExp('14.0 GB to 12.0 GB')), findsOneWidget);
    expect(find.text('Swap in use'), findsNothing, reason: 'swap was left as it was');
    expect(done, 1);
    await tester.tap(find.text('Done'));
    await tester.pumpAndSettle();
    expect(find.text('Freed 2.1 GB'), findsNothing);
    semantics.dispose();
  });

  testWidgets('"Also empty swap" asks the server to empty it', (tester) async {
    final semantics = tester.ensureSemantics();
    bool? sentSwap;
    await _open(tester, swapUsed: 2 * _gb, run: ({required swap}) async {
      sentSwap = swap;
      return _result(swap: true);
    });
    await tester.tap(find.text('Also empty swap'));
    await tester.pump();
    await tester.tap(find.widgetWithText(FilledButton, 'Free up memory'));
    await tester.pumpAndSettle();
    expect(sentSwap, isTrue);
    expect(find.bySemanticsLabel(RegExp('2.0 GB to 0 B')), findsOneWidget);
    semantics.dispose();
  });

  testWidgets("a refusal (swap wouldn't fit, rate limit) stays in the sheet with the server's words", (tester) async {
    await _open(tester, swapUsed: 2 * _gb, run: ({required swap}) async => throw ApiException('Memory was just cleared - try again in 20 s', statusCode: 429));
    await tester.tap(find.widgetWithText(FilledButton, 'Free up memory'));
    await tester.pumpAndSettle();
    expect(find.text('Memory was just cleared - try again in 20 s'), findsOneWidget);
    expect(find.widgetWithText(FilledButton, 'Free up memory'), findsOneWidget, reason: 'can try again');
  });

  group('against the fake server', () {
    setUp(() async => signIn());

    testWidgets('POSTs /v1/sys/memory/clear and reads its answer', (tester) async {
      final server = FakeServer(overrides: {'POST /v1/sys/memory/clear': fixture('v1/sys/memory/clear')});
      final r = await tester.runAsync(() => http.runWithClient(() => freeServerMemory(swap: false), () => server.client));
      expect(server.requests, contains('POST /v1/sys/memory/clear'));
      expect(r!.freed, 2255291392);
      expect(r.before.free, 1446326272);
      expect(r.after.free, 3701617664);
      expect(r.before.swapUsed, 2147483648);
      expect(r.swapReclaimed, isFalse);
    });
  });
}
