import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/theme/app_theme.dart';
import 'package:nivaroos_mobile/ui/theme/appearance.dart';

/// Pumps [child] in a Scaffold under the app's light (or dark) theme, in
/// the default design direction or [direction].
Future<void> pumpUi(WidgetTester tester, Widget child,
    {Brightness brightness = Brightness.light, bool disableAnimations = false, DesignDirection direction = DesignDirection.defaultDirection}) {
  return tester.pumpWidget(MaterialApp(
    theme: AppTheme.build(brightness: brightness, direction: direction),
    home: MediaQuery(
      data: MediaQueryData(disableAnimations: disableAnimations),
      child: Scaffold(body: child),
    ),
  ));
}
