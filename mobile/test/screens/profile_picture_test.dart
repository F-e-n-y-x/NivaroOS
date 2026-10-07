import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/profile_picture.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

void main() {
  test('crop: zoom 1 shows the centred square; panning stops at the edges', () {
    // 400x200 picture in a 100 px view: scale 0.5 -> 200x100 on screen.
    expect(cropRect(Offset.zero, 400, 200, 100, 1), const Rect.fromLTWH(100, 0, 200, 200));
    expect(clampOffset(const Offset(500, 500), 400, 200, 100, 1), const Offset(50, 0));
    final left = clampOffset(const Offset(-500, 0), 400, 200, 100, 1);
    expect(cropRect(left, 400, 200, 100, 1), const Rect.fromLTWH(200, 0, 200, 200));
    expect(cropRect(Offset.zero, 400, 200, 100, 2).width, 100);
  });

  testWidgets('Use pops with a 512 px square PNG', (tester) async {
    final photo = File('test/screenshots/fixtures/avatar_photo.png').readAsBytesSync();
    Uint8List? result;
    await tester.pumpWidget(MaterialApp(
      theme: AppTheme.build(brightness: Brightness.light),
      home: Builder(
        builder: (context) => TextButton(
          onPressed: () async => result = await Navigator.of(context).push<Uint8List>(MaterialPageRoute(builder: (_) => AvatarCropScreen(bytes: photo))),
          child: const Text('open'),
        ),
      ),
    ));
    await tester.tap(find.text('open'));
    // Decoding happens off the fake clock (a spinner meanwhile).
    for (var i = 0; i < 20 && find.byType(Slider).evaluate().isEmpty; i++) {
      await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 50)));
      await tester.pump(const Duration(milliseconds: 100));
    }
    expect(find.byType(Slider), findsOneWidget);
    await tester.pump(const Duration(seconds: 1)); // the page transition
    await tester.drag(find.byType(Slider), const Offset(60, 0));
    await tester.pump();
    await tester.tap(find.text('Use'));
    for (var i = 0; i < 40 && result == null; i++) {
      await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 50)));
      await tester.pump(const Duration(milliseconds: 100));
    }
    expect(result, isNotNull);
    final img = await tester.runAsync(() => decodeImageFromList(result!));
    expect((img!.width, img.height), (avatarOutput, avatarOutput));
    expect(String.fromCharCodes(result!.sublist(1, 4)), 'PNG');
    img.dispose();
  });
}
