import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/theme/app_theme.dart';
import 'package:nivaroos_mobile/ui/widgets/confirm_dialog.dart';

import '../pump.dart';

Future<Future<bool>> _open(WidgetTester tester, Future<bool> Function(BuildContext) show) async {
  late Future<bool> result;
  await pumpUi(tester, Builder(builder: (context) => TextButton(onPressed: () => result = show(context), child: const Text('open'))));
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
  return result;
}

Future<bool> _delete(BuildContext context) =>
    ConfirmDialog.destructive(context, title: 'Delete “mint”?', message: 'The VM and its disk are deleted.', confirmLabel: 'Delete');

void main() {
  testWidgets('destructive: names the thing, says it is permanent, confirms in the error colour', (tester) async {
    final result = await _open(tester, _delete);
    expect(find.text('Delete “mint”?'), findsOneWidget);
    expect(find.text("The VM and its disk are deleted. This can't be undone."), findsOneWidget);
    final button = tester.widget<FilledButton>(find.widgetWithText(FilledButton, 'Delete'));
    expect(button.style!.backgroundColor!.resolve({}), AppTheme.light().colorScheme.error);
    await tester.tap(find.text('Delete'));
    await tester.pumpAndSettle();
    expect(await result, isTrue);
  });

  testWidgets('destructive: Cancel has focus, and dismissing counts as Cancel', (tester) async {
    var result = await _open(tester, _delete);
    final cancel = tester.widget<TextButton>(find.widgetWithText(TextButton, 'Cancel'));
    expect(cancel.autofocus, isTrue);
    await tester.tapAt(const Offset(5, 5));
    await tester.pumpAndSettle();
    expect(await result, isFalse);

    result = await _open(tester, _delete);
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(await result, isFalse);
  });

  testWidgets('one haptic, on the confirm, not on opening', (tester) async {
    final haptics = <String>[];
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, (call) async {
      if (call.method == 'HapticFeedback.vibrate') haptics.add(call.arguments as String);
      return null;
    });
    await _open(tester, _delete);
    expect(haptics, isEmpty);
    await tester.tap(find.text('Delete'));
    await tester.pumpAndSettle();
    expect(haptics, ['HapticFeedbackType.heavyImpact']);
  });

  testWidgets('closes an unpunctuated message before adding the permanence line', (tester) async {
    await _open(tester, (c) => ConfirmDialog.destructive(c, title: 'Wipe “tank”?', message: 'Every file on the pool is erased', confirmLabel: 'Wipe'));
    expect(find.text("Every file on the pool is erased. This can't be undone."), findsOneWidget);
  });

  testWidgets('a non-permanent destructive action does not claim to be permanent', (tester) async {
    await _open(tester, (c) => ConfirmDialog.destructive(c, title: 'Restart the server?', message: 'Apps and VMs stop for a minute.', confirmLabel: 'Restart', permanent: false));
    expect(find.text('Apps and VMs stop for a minute.'), findsOneWidget);
  });

  testWidgets('confirm: a plain text-button choice', (tester) async {
    final result = await _open(tester, (c) => ConfirmDialog.confirm(c, title: 'Sign out?', message: 'You can sign in again at any time.', confirmLabel: 'Sign out'));
    expect(find.byType(FilledButton), findsNothing);
    await tester.tap(find.text('Sign out'));
    await tester.pumpAndSettle();
    expect(await result, isTrue);
  });
}
