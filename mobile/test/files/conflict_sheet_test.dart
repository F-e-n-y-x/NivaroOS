// The conflict sheet (plan M-29): nothing is overwritten without an answer,
// "Same answer for all" covers every item, and turning it off asks per item.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/files/file_ops.dart';
import 'package:nivaroos_mobile/screens/files/file_sheets.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

const _conflicts = ['/DATA/Downloads/a.jpg', '/DATA/Downloads/b.md', '/DATA/Downloads/C'];

Future<Future<Map<String, ConflictChoice>?>> _open(WidgetTester tester, List<String> conflicts) async {
  late Future<Map<String, ConflictChoice>?> result;
  await tester.pumpWidget(MaterialApp(
    theme: AppTheme.light(),
    home: Builder(
      builder: (context) => Scaffold(
        body: Center(
          child: TextButton(
            onPressed: () => result = showConflictSheet(context, conflicts: conflicts, destName: 'Documents', kind: TransferKind.copy),
            child: const Text('go'),
          ),
        ),
      ),
    ),
  ));
  await tester.tap(find.text('go'));
  await tester.pumpAndSettle();
  return result;
}

void main() {
  testWidgets('one answer for all items by default', (tester) async {
    final result = await _open(tester, _conflicts);
    expect(find.text('3 items are already in Documents'), findsOneWidget);
    await tester.tap(find.text('Keep both'));
    await tester.pumpAndSettle();
    expect(await result, {for (final c in _conflicts) c: ConflictChoice.keepBoth});
  });

  testWidgets('without "same answer" it asks for each item in turn', (tester) async {
    final result = await _open(tester, _conflicts);
    await tester.tap(find.text('Same answer for all 3'));
    await tester.pumpAndSettle();
    expect(find.text('“a.jpg” is already in Documents'), findsOneWidget);
    await tester.tap(find.text('Replace'));
    await tester.pumpAndSettle();
    expect(find.text('“b.md” is already in Documents'), findsOneWidget);
    expect(find.textContaining('Item 2 of 3'), findsOneWidget);
    await tester.tap(find.text('Skip'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Keep both'));
    await tester.pumpAndSettle();
    expect(await result, {
      _conflicts[0]: ConflictChoice.replace,
      _conflicts[1]: ConflictChoice.skip,
      _conflicts[2]: ConflictChoice.keepBoth,
    });
  });

  testWidgets('Cancel answers nothing', (tester) async {
    final result = await _open(tester, _conflicts.take(1).toList());
    expect(find.text('Same answer for all 1'), findsNothing);
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(await result, isNull);
  });

  test('keep both, replace and skip map to the server’s styles', () {
    expect(ConflictChoice.keepBoth.style, 'rename');
    expect(ConflictChoice.replace.style, 'overwrite');
    expect(ConflictChoice.skip.style, 'skip');
  });
}
