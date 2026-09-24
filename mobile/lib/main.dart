import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'theme.dart';
import 'ui/theme/app_theme.dart';
import 'ui/theme/theme_controller.dart';
import 'services/api_client.dart';
import 'services/storage_service.dart';
import 'screens/discovery_screen.dart';
import 'screens/login_screen.dart';
import 'screens/home_shell.dart';
import 'services/permission_service.dart';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();
  // Draw behind the status and navigation bars (Android 15 enforces this
  // for apps targeting 35+ anyway); screens handle the insets.
  SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);

  Widget initialScreen = const DiscoveryScreen();

  try {
    await StorageService.instance.init().timeout(const Duration(milliseconds: 1500));
    await ThemeController.instance.load();
    await ApiClient.instance.init().timeout(const Duration(milliseconds: 1500));

    final serverUrl = await StorageService.instance.getServerUrl();
    if (serverUrl != null && serverUrl.trim().isNotEmpty) {
      if (ApiClient.instance.hasSession) {
        // Not triggered here - HomeShell.initState() is the single place
        // that starts background sync now (see its comment for why: this
        // used to also fire from login_screen.dart, so a fresh login fired
        // it twice within milliseconds of each other, right as the
        // Navigator was mid-transition into HomeShell - confirmed via a
        // real on-device crash log (ForegroundServiceDidNotStartInTimeException)
        // that startForeground() was never actually reached in that window.
        initialScreen = const HomeShell();
      } else {
        initialScreen = const LoginScreen();
      }
    }
  } catch (e) {
    debugPrint('[main] Bootstrap notice: $e');
  }

  // Request essential notification permission in background
  PermissionService.requestInitialPermissions();

  runApp(NivaroApp(initialScreen: initialScreen));
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
          child: LegacyThemeBridge(child: child ?? const SizedBox.shrink()),
        ),
        home: initialScreen,
      ),
    );
  }
}
