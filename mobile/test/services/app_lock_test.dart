import 'package:clock/clock.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/app_lock.dart';
import 'package:nivaroos_mobile/services/secure_window.dart';

class _Auth implements DeviceAuthenticator {
  bool available = true;
  final outcomes = <AuthOutcome>[];
  int prompts = 0;

  @override
  Future<bool> isAvailable() async => available;

  @override
  Future<AuthOutcome> authenticate(String reason) async {
    prompts++;
    return outcomes.isEmpty ? AuthOutcome.success : outcomes.removeAt(0);
  }
}

class _Store implements AppLockStore {
  _Store([Map<String, String>? initial]) : data = {...?initial};
  final Map<String, String> data;
  @override
  Future<Map<String, String>> readAll() async => data;
  @override
  Future<void> write(String key, String value) async => data[key] = value;
}

class _Window implements SecureWindowPlatform {
  final secure = <bool>[];
  final recents = <bool>[];
  @override
  Future<void> setSecure(bool on) async => secure.add(on);
  @override
  Future<void> setRecentsHidden(bool hidden) async => recents.add(hidden);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _Auth auth;
  late _Window window;
  AppLock make([Map<String, String>? stored]) => AppLock(authenticator: auth, store: _Store(stored), window: window);

  setUp(() {
    auth = _Auth();
    window = _Window();
  });

  test('off by default: never locks, never asks, terminal hidden by default', () async {
    final lock = make();
    await lock.load();
    expect(lock.enabled, isFalse);
    expect(lock.locked, isFalse);
    expect(lock.secureTerminal, isTrue);
    expect(await lock.confirm('Open the terminal'), isTrue);
    expect(auth.prompts, 0);
    lock.dispose();
  });

  test('on: opens locked, unlocks with the screen lock, hides recents', () async {
    final lock = make({'app_lock_enabled': 'true'});
    await lock.load();
    expect(lock.locked, isTrue);
    expect(window.recents.last, isTrue);
    auth.outcomes.add(AuthOutcome.cancelled);
    expect(await lock.unlock(), AuthOutcome.cancelled);
    expect(lock.locked, isTrue);
    expect(await lock.unlock(), AuthOutcome.success);
    expect(lock.locked, isFalse);
    lock.dispose();
  });

  test('turning it on needs a screen lock and a successful check', () async {
    final lock = make();
    await lock.load();
    auth.available = false;
    expect(await lock.setEnabled(true), AuthOutcome.unavailable);
    expect(lock.enabled, isFalse);
    auth.available = true;
    auth.outcomes.add(AuthOutcome.cancelled);
    expect(await lock.setEnabled(true), AuthOutcome.cancelled);
    expect(lock.enabled, isFalse);
    expect(await lock.setEnabled(true), AuthOutcome.success);
    expect(lock.enabled, isTrue);
    expect(lock.locked, isFalse, reason: 'just proved it is them');
    lock.dispose();
  });

  test('locks again only after the chosen time in the background', () async {
    final lock = make({'app_lock_enabled': 'true', 'app_lock_after_seconds': '300'});
    var now = DateTime(2026, 9, 29, 12);
    await withClock(Clock(() => now), () async {
      await lock.load();
      await lock.unlock();
      lock.didChangeAppLifecycleState(AppLifecycleState.paused);
      now = now.add(const Duration(minutes: 4));
      lock.didChangeAppLifecycleState(AppLifecycleState.resumed);
      expect(lock.locked, isFalse);
      lock.didChangeAppLifecycleState(AppLifecycleState.hidden);
      now = now.add(const Duration(minutes: 5));
      lock.didChangeAppLifecycleState(AppLifecycleState.resumed);
      expect(lock.locked, isTrue);
    });
    lock.dispose();
  });

  test('sensitive actions ask again unless unlocked moments ago', () async {
    final lock = make({'app_lock_enabled': 'true'});
    var now = DateTime(2026, 9, 29, 12);
    await withClock(Clock(() => now), () async {
      await lock.load();
      await lock.unlock();
      expect(auth.prompts, 1);
      expect(await lock.confirm('Delete vm'), isTrue);
      expect(auth.prompts, 1, reason: 'within ${AppLock.recentUnlock}');
      now = now.add(const Duration(minutes: 2));
      auth.outcomes.add(AuthOutcome.cancelled);
      expect(await lock.confirm('Delete vm'), isFalse);
      expect(auth.prompts, 2);
    });
    lock.dispose();
  });

  test('turns itself off when the phone loses its screen lock', () async {
    final store = _Store({'app_lock_enabled': 'true'});
    final lock = AppLock(authenticator: auth, store: store, window: window);
    await lock.load();
    auth.available = false;
    auth.outcomes.add(AuthOutcome.unavailable);
    await lock.unlock();
    expect(lock.enabled, isFalse);
    expect(lock.locked, isFalse);
    expect(lock.turnedOffNoScreenLock, isTrue);
    expect(store.data['app_lock_enabled'], 'false');
    lock.dispose();
  });

  test('settings persist', () async {
    final store = _Store();
    final lock = AppLock(authenticator: auth, store: store, window: window);
    await lock.load();
    await lock.setSecureTerminal(false);
    await lock.setLockAfter(const Duration(minutes: 15));
    final again = AppLock(authenticator: auth, store: store, window: window);
    await again.load();
    expect(again.secureTerminal, isFalse);
    expect(again.lockAfter, const Duration(minutes: 15));
    lock.dispose();
    again.dispose();
  });
}
