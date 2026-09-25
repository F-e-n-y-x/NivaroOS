import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'ui/theme/app_theme.dart';
import 'ui/theme/scaled_icons.dart';
import 'ui/theme/theme_controller.dart';
import 'services/api_client.dart';
import 'services/storage_service.dart';
import 'screens/discovery_screen.dart';
import 'screens/login_screen.dart';
import 'screens/home_shell.dart';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();
  // Draw behind the status and navigation bars (Android 15 enforces this
  // for apps targeting 35+ anyway); screens handle the insets.
  SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);

  // Most launches decide in a few milliseconds. When the secure storage is
  // slow (flutter_secure_storage 10 re-encrypts 9.x data on the first read
  // after an upgrade, which can take seconds on a slow Keystore), the app
  // starts on a blank frame and keeps waiting for it, instead of giving up
  // and showing sign-in to someone who is signed in (review finding 6).
  final start = startScreen();
  Widget initialScreen;
  try {
    initialScreen = await start.timeout(const Duration(milliseconds: 1500));
  } on TimeoutException {
    initialScreen = BootScreen(start: start);
  }

  // No permission prompts at launch: each feature asks when it needs one
  // (notifications when storage sharing starts).

  runApp(NivaroApp(initialScreen: initialScreen));
}

/// Where the app opens: Home with a saved session, sign-in with a saved
/// server, discovery otherwise. Waits for the stored session however long
/// that takes; only a storage failure falls back to discovery.
Future<Widget> startScreen() async {
  try {
    await StorageService.instance.init();
    await ThemeController.instance.load();
    await ApiClient.instance.init();
    final serverUrl = await StorageService.instance.getServerUrl();
    if (serverUrl != null && serverUrl.trim().isNotEmpty) {
      // HomeShell starts this server's background work (the heartbeat)
      // once it is on screen; nothing runs from here.
      return ApiClient.instance.hasSession ? const HomeShell() : const LoginScreen();
    }
  } catch (e) {
    debugPrint('[main] Bootstrap notice: ${e.runtimeType}');
  }
  return const DiscoveryScreen();
}

/// The first route while a slow start finishes: the page colour, then the
/// screen [start] picks, in place (it stays the only route, so back from
/// Home still closes the app).
class BootScreen extends StatelessWidget {
  const BootScreen({super.key, required this.start});

  final Future<Widget> start;

  @override
  Widget build(BuildContext context) => FutureBuilder<Widget>(
        future: start,
        builder: (context, snap) => snap.data ??
            Scaffold(
              body: Center(
                child: Semantics(label: 'Starting', child: const CircularProgressIndicator()),
              ),
            ),
      );
}

class NivaroApp extends StatelessWidget {
  final Widget initialScreen;
  const NivaroApp({super.key, required this.initialScreen});

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<ThemeMode>(
      valueListenable: ThemeController.instance,
      builder: (context, mode, _) => MaterialApp(
        title: 'NivaroOS',
        debugShowCheckedModeBanner: false,
        theme: AppTheme.light(),
        darkTheme: AppTheme.dark(),
        themeMode: mode,
        // System bar icons follow the app's theme on every screen, not only
        // under an app bar (Home, login and discovery have none), so a
        // Dark choice on a light phone still gets light status icons.
        builder: (context, child) => AnnotatedRegion<SystemUiOverlayStyle>(
          value: AppTheme.systemBarsStyle(Theme.of(context).brightness),
          child: ScaledIcons(child: child ?? const SizedBox.shrink()),
        ),
        home: initialScreen,
      ),
    );
  }
}
