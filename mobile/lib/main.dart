import 'package:flutter/material.dart';
import 'theme.dart';
import 'services/api_client.dart';
import 'services/storage_service.dart';
import 'screens/discovery_screen.dart';
import 'screens/login_screen.dart';
import 'screens/home_shell.dart';
import 'services/permission_service.dart';
import 'services/device_sync_service.dart';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();

  Widget initialScreen = const DiscoveryScreen();

  try {
    await StorageService.instance.init().timeout(const Duration(milliseconds: 1500));
    await ApiClient.instance.init().timeout(const Duration(milliseconds: 1500));

    final serverUrl = await StorageService.instance.getServerUrl();
    if (serverUrl != null && serverUrl.trim().isNotEmpty) {
      if (ApiClient.instance.hasSession) {
        DeviceSyncService.instance.startAutoSync();
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
    return MaterialApp(
      title: 'NivaroOS',
      debugShowCheckedModeBanner: false,
      theme: buildNivaroTheme(),
      home: initialScreen,
    );
  }
}
