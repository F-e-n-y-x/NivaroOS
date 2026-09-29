import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

/// The Android window's privacy switches (SecureWindowChannel.kt).
abstract class SecureWindowPlatform {
  /// FLAG_SECURE: the window is left out of screenshots, screen
  /// recordings, casting and the recent-apps preview.
  Future<void> setSecure(bool secure);

  /// Android 13+: the recent-apps preview shows a blank card instead of
  /// the app's content (screenshots still work). A no-op before 13.
  Future<void> setRecentsHidden(bool hidden);
}

class MethodChannelSecureWindow implements SecureWindowPlatform {
  static const _channel = MethodChannel('com.fenyx.nivaroos/secure_window');

  @override
  Future<void> setSecure(bool secure) => _call('setSecure', {'secure': secure});

  @override
  Future<void> setRecentsHidden(bool hidden) => _call('setRecentsHidden', {'hidden': hidden});

  Future<void> _call(String method, Map<String, Object> args) async {
    if (defaultTargetPlatform != TargetPlatform.android) return;
    try {
      await _channel.invokeMethod<void>(method, args);
    } on MissingPluginException {
      // Tests and platforms without the channel.
    } on PlatformException catch (e) {
      debugPrint('[SecureWindow] $method failed: ${e.code}');
    }
  }
}

/// Keeps sensitive screens out of screenshots and the recent-apps
/// preview. FLAG_SECURE belongs to the whole window, so screens hold it
/// with a count: it's set while at least one [SecureWindowScope] (or
/// [SecureWindowState]) is mounted and cleared when the last one goes.
///
/// Use it at the route: `SecureWindowScope(child: TerminalScreen(...))`
/// covers exactly the time that route is on the stack.
abstract final class SecureWindow {
  static SecureWindowPlatform platform = MethodChannelSecureWindow();

  static int _holds = 0;

  /// How many screens hold the flag (for tests).
  static int get holds => _holds;

  static void acquire() {
    _holds++;
    if (_holds == 1) unawaited(platform.setSecure(true));
  }

  static void release() {
    if (_holds == 0) return;
    _holds--;
    if (_holds == 0) unawaited(platform.setSecure(false));
  }
}

/// Holds [SecureWindow] while mounted, when [enabled].
class SecureWindowScope extends StatefulWidget {
  const SecureWindowScope({super.key, this.enabled = true, required this.child});

  final bool enabled;
  final Widget child;

  @override
  State<SecureWindowScope> createState() => _SecureWindowScopeState();
}

class _SecureWindowScopeState extends State<SecureWindowScope> {
  bool _held = false;

  void _sync() {
    if (widget.enabled == _held) return;
    _held = widget.enabled;
    _held ? SecureWindow.acquire() : SecureWindow.release();
  }

  @override
  void initState() {
    super.initState();
    _sync();
  }

  @override
  void didUpdateWidget(SecureWindowScope oldWidget) {
    super.didUpdateWidget(oldWidget);
    _sync();
  }

  @override
  void dispose() {
    if (_held) SecureWindow.release();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

/// [SecureWindowScope] for a screen's own State, for screens that are
/// always secure (sign-in): `class _S extends State<X> with SecureWindowState<X>`.
mixin SecureWindowState<T extends StatefulWidget> on State<T> {
  @override
  void initState() {
    super.initState();
    SecureWindow.acquire();
  }

  @override
  void dispose() {
    SecureWindow.release();
    super.dispose();
  }
}
