import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/theme/app_theme.dart';

/// Pumps [child] in a Scaffold under the app's light (or dark) theme.
Future<void> pumpUi(WidgetTester tester, Widget child, {Brightness brightness = Brightness.light, bool disableAnimations = false}) {
  return tester.pumpWidget(MaterialApp(
    theme: AppTheme.forBrightness(brightness),
    home: MediaQuery(
      data: MediaQueryData(disableAnimations: disableAnimations),
      child: Scaffold(body: child),
    ),
  ));
}
