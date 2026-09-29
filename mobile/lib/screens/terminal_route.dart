import 'package:flutter/material.dart';

import '../services/app_lock.dart';
import '../services/secure_window.dart';

/// Opens a terminal screen on [navigator]. With the app lock on, the owner
/// confirms it's them first ([AppLock.confirm]); while the terminal is on
/// the stack it's kept out of screenshots, recordings and the recent-apps
/// preview unless "Hide the terminal" is off ([AppLock.secureTerminal]).
///
/// Every way into a terminal goes through here, so the terminal screen
/// itself needs to know about neither.
Future<T?> pushTerminal<T>(NavigatorState navigator, WidgetBuilder builder, {AppLock? lock}) async {
  final l = lock ?? AppLock.instance;
  if (!await l.confirm('Confirm it’s you to open the terminal')) return null;
  if (!navigator.mounted) return null;
  return navigator.push<T>(MaterialPageRoute(
    builder: (context) => ListenableBuilder(
      listenable: l,
      builder: (context, child) => SecureWindowScope(enabled: l.secureTerminal, child: child!),
      child: Builder(builder: builder),
    ),
  ));
}
