import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import '../pump.dart';

void main() {
  test('initials', () {
    expect(UserAvatar.initials('alex'), 'A');
    expect(UserAvatar.initials('alex.morgan'), 'AM');
    expect(UserAvatar.initials('jo_doe-x'), 'JD');
    expect(UserAvatar.initials(''), '');
  });

  testWidgets('initials on the primary container until there is a picture, in every direction', (tester) async {
    for (final d in DesignDirection.values) {
      for (final b in Brightness.values) {
        await pumpUi(tester, const UserAvatar(username: 'alex'), direction: d, brightness: b);
        final scheme = Theme.of(tester.element(find.byType(UserAvatar))).colorScheme;
        final circle = tester.widget<CircleAvatar>(find.byType(CircleAvatar));
        expect(circle.backgroundColor, scheme.primaryContainer);
        expect(circle.foregroundImage, isNull);
        expect(find.text('A'), findsOneWidget);
      }
    }
  });

  testWidgets('a picture covers the initials; no name, a person icon', (tester) async {
    final png = File('test/screenshots/fixtures/v1/users/avatar.png').readAsBytesSync();
    await pumpUi(tester, UserAvatar(username: 'alex', picture: png));
    expect(tester.widget<CircleAvatar>(find.byType(CircleAvatar)).foregroundImage, isA<MemoryImage>());
    await pumpUi(tester, const UserAvatar(username: ''));
    expect(find.byIcon(Icons.person_outline), findsOneWidget);
  });
}
