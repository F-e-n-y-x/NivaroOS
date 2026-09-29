import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/secure_window.dart';

class _Window implements SecureWindowPlatform {
  final secure = <bool>[];
  @override
  Future<void> setSecure(bool on) async => secure.add(on);
  @override
  Future<void> setRecentsHidden(bool hidden) async {}
}

void main() {
  late _Window window;
  setUp(() {
    window = _Window();
    SecureWindow.platform = window;
  });

  testWidgets('the flag is held while any scope is mounted, and cleared after the last', (tester) async {
    await tester.pumpWidget(const Column(children: [SecureWindowScope(child: SizedBox()), SecureWindowScope(child: SizedBox())]));
    expect(SecureWindow.holds, 2);
    expect(window.secure, [true]);
    await tester.pumpWidget(const Column(children: [SecureWindowScope(child: SizedBox())]));
    expect(window.secure, [true]);
    await tester.pumpWidget(const SizedBox());
    expect(SecureWindow.holds, 0);
    expect(window.secure, [true, false]);
  });

  testWidgets('a disabled scope holds nothing, and follows its setting', (tester) async {
    await tester.pumpWidget(const SecureWindowScope(enabled: false, child: SizedBox()));
    expect(window.secure, isEmpty);
    await tester.pumpWidget(const SecureWindowScope(child: SizedBox()));
    expect(window.secure, [true]);
    await tester.pumpWidget(const SecureWindowScope(enabled: false, child: SizedBox()));
    expect(window.secure, [true, false]);
  });
}
