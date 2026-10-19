import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/cloud_account.dart';
import 'package:nivaroos_mobile/screens/files/cloud_account_tile.dart';

void main() {
  Future<(int, int)> pump(WidgetTester tester, Map<String, dynamic> json) async {
    var opened = 0, reconnected = 0;
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: CloudAccountTile(
          account: CloudAccount.fromJson(json),
          onOpen: () => opened++,
          onReconnect: () => reconnected++,
        ),
      ),
    ));
    await tester.tap(find.byType(CloudAccountTile));
    if (find.text('Reconnect').evaluate().isNotEmpty) await tester.tap(find.text('Reconnect'));
    return (opened, reconnected);
  }

  testWidgets('an expired sign-in says why and offers Reconnect, not open', (tester) async {
    final (opened, reconnected) = await pump(tester, {
      'name': 'My Drive',
      'type': 'drive',
      'mount_point': '/mnt/gd',
      'status': 'sign_in_expired',
      'problem': 'Your Google app is in Testing mode, so Google signs it out every 7 days.',
    });
    expect(find.textContaining('Sign-in expired', findRichText: true), findsOneWidget);
    expect(find.textContaining('Testing mode', findRichText: true), findsOneWidget);
    expect(opened, 0);
    expect(reconnected, 1);
  });

  testWidgets('a mounted account opens and has no Reconnect', (tester) async {
    final (opened, reconnected) = await pump(tester, {'name': 'Work', 'type': 'drive', 'mount_point': '/mnt/w'});
    expect(find.text('Reconnect'), findsNothing);
    expect(find.text('Google Drive'), findsOneWidget);
    expect((opened, reconnected), (1, 0));
  });

  test('a listed but unmounted account is not browsable', () {
    final a = CloudAccount.fromJson({'mount_point': '/mnt/x', 'status': 'not_mounted'});
    expect(a.isMounted, isFalse);
    expect(a.signInExpired, isFalse);
  });
}
