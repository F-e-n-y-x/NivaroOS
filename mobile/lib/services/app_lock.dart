import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:local_auth/local_auth.dart';

import 'secure_window.dart';

/// How one unlock attempt ended.
enum AuthOutcome {
  /// The owner proved it's them (fingerprint, face, PIN, pattern, password).
  success,

  /// They dismissed the prompt.
  cancelled,

  /// The phone has no screen lock (or no way to show the prompt): the app
  /// lock can't be used until one is set.
  unavailable,

  /// Too many failed attempts; the system asks them to wait.
  lockedOut,

  /// Anything else the platform reported.
  failed,
}

/// The phone's own screen-lock check - biometrics with the device PIN,
/// pattern or password as the fallback (never biometrics only, so a phone
/// without a fingerprint reader still works). Behind an interface so tests
/// can stand in for the system prompt.
abstract class DeviceAuthenticator {
  /// True when the phone can check its owner: a screen lock is set.
  Future<bool> isAvailable();

  Future<AuthOutcome> authenticate(String reason);
}

class LocalAuthAuthenticator implements DeviceAuthenticator {
  LocalAuthAuthenticator([LocalAuthentication? auth]) : _auth = auth ?? LocalAuthentication();
  final LocalAuthentication _auth;

  @override
  Future<bool> isAvailable() async {
    try {
      return await _auth.isDeviceSupported();
    } catch (_) {
      return false;
    }
  }

  @override
  Future<AuthOutcome> authenticate(String reason) async {
    try {
      // persistAcrossBackgrounding: switching to the password manager or
      // a notification mid-prompt shows the prompt again on return.
      final ok = await _auth.authenticate(localizedReason: reason, persistAcrossBackgrounding: true);
      return ok ? AuthOutcome.success : AuthOutcome.cancelled;
    } on LocalAuthException catch (e) {
      return switch (e.code) {
        LocalAuthExceptionCode.userCanceled || LocalAuthExceptionCode.systemCanceled || LocalAuthExceptionCode.timeout => AuthOutcome.cancelled,
        LocalAuthExceptionCode.noCredentialsSet || LocalAuthExceptionCode.uiUnavailable => AuthOutcome.unavailable,
        LocalAuthExceptionCode.temporaryLockout || LocalAuthExceptionCode.biometricLockout => AuthOutcome.lockedOut,
        _ => AuthOutcome.failed,
      };
    } on PlatformException catch (_) {
      return AuthOutcome.failed;
    } on MissingPluginException catch (_) {
      return AuthOutcome.unavailable;
    }
  }
}

/// Where the lock's settings live. Its own secure-storage namespace, not
/// StorageService's: signing out wipes that one (clearAll), and the lock
/// protects the phone's app, not one server's session.
abstract class AppLockStore {
  Future<Map<String, String>> readAll();
  Future<void> write(String key, String value);
}

class SecureAppLockStore implements AppLockStore {
  static const _storage = FlutterSecureStorage(
    aOptions: AndroidOptions(storageNamespace: 'nivaroos_app_lock'),
    iOptions: IOSOptions(accountName: 'nivaroos_app_lock'),
  );

  @override
  Future<Map<String, String>> readAll() => _storage.readAll();

  @override
  Future<void> write(String key, String value) => _storage.write(key: key, value: value);
}

/// The optional app lock (off by default) and the related privacy
/// settings:
///
/// - **App lock**: when on, the app asks for the phone's screen lock when
///   it opens and when it comes back after [lockAfter] in the background;
///   [AppLockGate] covers everything until then. While it's on, Android 13+
///   also leaves the app's content out of the recent-apps preview.
/// - **Confirm sensitive actions** ([confirm]): with the lock on, opening a
///   terminal, powering the server off or restarting it, and deleting a VM
///   ask again - unless the owner unlocked within the last [recentUnlock].
/// - **Hide the terminal** ([secureTerminal], on by default): terminal
///   screens are kept out of screenshots, screen recordings and the
///   recent-apps preview (FLAG_SECURE; see `SecureWindow`). The sign-in
///   screen always is.
class AppLock extends ChangeNotifier with WidgetsBindingObserver {
  AppLock({DeviceAuthenticator? authenticator, AppLockStore? store, SecureWindowPlatform? window})
      : _auth = authenticator ?? LocalAuthAuthenticator(),
        _store = store ?? SecureAppLockStore(),
        _window = window ?? SecureWindow.platform;

  static AppLock instance = AppLock();

  final DeviceAuthenticator _auth;
  final AppLockStore _store;
  final SecureWindowPlatform _window;

  static const _keyEnabled = 'app_lock_enabled';
  static const _keyLockAfter = 'app_lock_after_seconds';
  static const _keySecureTerminal = 'secure_terminal';

  /// The choices for [lockAfter]; zero is "immediately".
  static const lockAfterChoices = [Duration.zero, Duration(minutes: 1), Duration(minutes: 5), Duration(minutes: 15), Duration(minutes: 30)];
  static const defaultLockAfter = Duration(minutes: 1);

  /// A sensitive action this soon after an unlock doesn't ask again.
  static const recentUnlock = Duration(seconds: 30);

  bool _enabled = false;
  Duration _lockAfter = defaultLockAfter;
  bool _secureTerminal = true;
  bool _locked = false;
  bool _loaded = false;
  bool _prompting = false;
  DateTime? _backgroundedAt;
  DateTime? _lastUnlock;

  bool get enabled => _enabled;
  Duration get lockAfter => _lockAfter;
  bool get secureTerminal => _secureTerminal;

  /// True while [AppLockGate] should cover the app.
  bool get locked => _locked;

  /// The lock turned itself off because the phone no longer has a screen
  /// lock ([unlock]); the settings screen says so until it's seen.
  bool get turnedOffNoScreenLock => _turnedOffNoScreenLock;
  bool _turnedOffNoScreenLock = false;
  void acknowledgeTurnedOff() {
    if (!_turnedOffNoScreenLock) return;
    _turnedOffNoScreenLock = false;
    notifyListeners();
  }

  /// Reads the settings and, when the lock is on, starts locked (a cold
  /// start counts as opening the app). Call once at startup.
  Future<void> load() async {
    if (_loaded) return;
    _loaded = true;
    try {
      final all = await _store.readAll();
      _enabled = all[_keyEnabled] == 'true';
      final secs = int.tryParse(all[_keyLockAfter] ?? '');
      if (secs != null && secs >= 0) _lockAfter = Duration(seconds: secs);
      _secureTerminal = all[_keySecureTerminal] != 'false';
    } catch (e) {
      debugPrint('[AppLock] settings not read: ${e.runtimeType}');
    }
    _locked = _enabled;
    WidgetsBinding.instance.addObserver(this);
    unawaited(_applyRecents());
    notifyListeners();
  }

  /// Turns the lock on (after the owner proves the screen lock works, so
  /// they can't lock themselves out) or off (also after a check: someone
  /// holding the unlocked phone shouldn't switch it off unasked). Returns
  /// the outcome; the setting changes only on [AuthOutcome.success].
  Future<AuthOutcome> setEnabled(bool on) async {
    if (on == _enabled) return AuthOutcome.success;
    if (on && !await _auth.isAvailable()) return AuthOutcome.unavailable;
    final outcome = await _prompt(on ? 'Confirm it’s you to turn on the app lock' : 'Confirm it’s you to turn off the app lock');
    if (outcome != AuthOutcome.success) return outcome;
    _enabled = on;
    _locked = false;
    await _save(_keyEnabled, '$on');
    unawaited(_applyRecents());
    notifyListeners();
    return outcome;
  }

  Future<void> setLockAfter(Duration d) async {
    if (d == _lockAfter) return;
    _lockAfter = d;
    await _save(_keyLockAfter, '${d.inSeconds}');
    notifyListeners();
  }

  Future<void> setSecureTerminal(bool on) async {
    if (on == _secureTerminal) return;
    _secureTerminal = on;
    await _save(_keySecureTerminal, '$on');
    notifyListeners();
  }

  /// Asks for the screen lock to open the app ([AppLockGate]'s Unlock).
  Future<AuthOutcome> unlock() async {
    if (!_locked) return AuthOutcome.success;
    final outcome = await _prompt('Unlock NivaroOS');
    if (outcome == AuthOutcome.success) {
      _locked = false;
      notifyListeners();
    } else if (outcome == AuthOutcome.unavailable && !await _auth.isAvailable()) {
      // The phone's screen lock was removed since: there is nothing left
      // to check against, and the phone itself is open to whoever holds
      // it, so the app lock can't protect anything. It turns itself off
      // (the gate says so) rather than locking the owner out for good.
      _enabled = false;
      _locked = false;
      _turnedOffNoScreenLock = true;
      await _save(_keyEnabled, 'false');
      unawaited(_applyRecents());
      notifyListeners();
    }
    return outcome;
  }

  /// Before a sensitive action ([reason] names it: "Open the terminal"):
  /// true straight away when the lock is off or the owner unlocked moments
  /// ago, otherwise after the screen lock.
  Future<bool> confirm(String reason) async {
    if (!_enabled) return true;
    final last = _lastUnlock;
    if (last != null && clock.now().difference(last) < recentUnlock) return true;
    return await _prompt(reason) == AuthOutcome.success;
  }

  Future<AuthOutcome> _prompt(String reason) async {
    // The system prompt pauses the app; that must not count as leaving it.
    _prompting = true;
    try {
      final outcome = await _auth.authenticate(reason);
      if (outcome == AuthOutcome.success) _lastUnlock = clock.now();
      return outcome;
    } finally {
      _prompting = false;
      _backgroundedAt = null;
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (!_enabled || _prompting) return;
    switch (state) {
      case AppLifecycleState.hidden:
      case AppLifecycleState.paused:
        _backgroundedAt ??= clock.now();
      case AppLifecycleState.resumed:
        final since = _backgroundedAt;
        _backgroundedAt = null;
        if (since != null && !_locked && clock.now().difference(since) >= _lockAfter) {
          _locked = true;
          notifyListeners();
        }
      case AppLifecycleState.inactive:
      case AppLifecycleState.detached:
        break;
    }
  }

  Future<void> _applyRecents() async {
    try {
      await _window.setRecentsHidden(_enabled);
    } catch (_) {}
  }

  Future<void> _save(String key, String value) async {
    try {
      await _store.write(key, value);
    } catch (e) {
      debugPrint('[AppLock] setting not saved: ${e.runtimeType}');
    }
  }

  @override
  void dispose() {
    if (_loaded) WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }
}
