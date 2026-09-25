import 'dart:ui' show Size;

import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/host_display_fit.dart';

void main() {
  test('a portrait phone gets its landscape shape at 100/75/66/50%', () {
    expect(phoneFitSizes(const Size(1080, 2400)), const [
      FitSize(2400, 1080, 100),
      FitSize(1800, 810, 75),
      FitSize(1584, 712, 66),
      FitSize(1200, 540, 50),
    ]);
  });

  test('sizes under 640x480 are left out; nothing without a screen', () {
    expect(phoneFitSizes(const Size(720, 1280)).map((s) => s.percent), [100, 75]);
    expect(phoneFitSizes(Size.zero), isEmpty);
  });

  test('a very dense screen is capped to what the server accepts, same shape', () {
    final top = phoneFitSizes(const Size(5000, 12000)).first;
    expect(top.width, lessThanOrEqualTo(7680));
    expect(top.height, lessThanOrEqualTo(4320));
    expect(top.width / top.height, closeTo(12000 / 5000, 0.01));
  });
}
