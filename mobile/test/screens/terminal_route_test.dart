import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/terminal_route.dart';
import 'package:nivaroos_mobile/services/app_lock.dart';
import 'package:nivaroos_mobile/services/secure_window.dart';

class _Auth implements DeviceAuthenticator {
  AuthOutcome next = AuthOutcome.success;
  @override
  Future<bool> isAvailable() async => true;
  @override
  Future<AuthOutcome> authenticate(String reason) async => next;
}

class _Store implements AppLockStore {
  _Store(this.data);
  final Map<String, String> data;
  @override
  Future<Map<String, String>> readAll() async => data;
  @override
  Future<void> write(String key, String value) async => data[key] = value;
}

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

  Future<NavigatorState> pump(WidgetTester tester) async {
    final key = GlobalKey<NavigatorState>();
    await tester.pumpWidget(MaterialApp(navigatorKey: key, home: const Text('home')));
    return key.currentState!;
  }

  testWidgets('opens the terminal hidden from screenshots, and releases it on close', (tester) async {
    final lock = AppLock(authenticator: _Auth(), store: _Store({}), window: window);
    await lock.load();
    final nav = await pump(tester);
    pushTerminal<void>(nav, (_) => const Text('terminal'), lock: lock);
    await tester.pumpAndSettle();
    expect(find.text('terminal'), findsOneWidget);
    expect(window.secure, [true]);
    nav.pop();
    await tester.pumpAndSettle();
    expect(window.secure, [true, false]);
    lock.dispose();
  });

  testWidgets('with "Hide the terminal" off the window stays capturable', (tester) async {
    final lock = AppLock(authenticator: _Auth(), store: _Store({'secure_terminal': 'false'}), window: window);
    await lock.load();
    final nav = await pump(tester);
    pushTerminal<void>(nav, (_) => const Text('terminal'), lock: lock);
    await tester.pumpAndSettle();
    expect(find.text('terminal'), findsOneWidget);
    expect(window.secure, isEmpty);
    lock.dispose();
  });

  testWidgets('with the app lock on, a cancelled check opens nothing', (tester) async {
    final auth = _Auth();
    final lock = AppLock(authenticator: auth, store: _Store({'app_lock_enabled': 'true'}), window: window);
    await lock.load();
    final nav = await pump(tester);
    auth.next = AuthOutcome.cancelled;
    await pushTerminal<void>(nav, (_) => const Text('terminal'), lock: lock);
    await tester.pumpAndSettle();
    expect(find.text('terminal'), findsNothing);
    lock.dispose();
  });
}
