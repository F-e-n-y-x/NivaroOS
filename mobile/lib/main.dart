import 'package:flutter/material.dart';
import 'theme.dart';
import 'services/api_client.dart';
import 'services/storage_service.dart';
import 'screens/discovery_screen.dart';
import 'screens/login_screen.dart';
import 'screens/home_shell.dart';
import 'services/permission_service.dart';
import 'services/device_sync_service.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(const NivaroApp());
}

class NivaroApp extends StatelessWidget {
  const NivaroApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'NivaroOS',
      debugShowCheckedModeBanner: false,
      theme: buildNivaroTheme(),
      home: const _Bootstrap(),
    );
  }
}

/// Decides where to land on cold start: no saved server -> discovery, a
/// saved server but no session -> login, both -> straight into the app.
class _Bootstrap extends StatefulWidget {
  const _Bootstrap();

  @override
  State<_Bootstrap> createState() => _BootstrapState();
}

class _BootstrapState extends State<_Bootstrap> {
  @override
  void initState() {
    super.initState();
    _decide();
  }

  Future<void> _decide() async {
    try {
      // Small pause to let initial Flutter frame render
      await Future.delayed(const Duration(milliseconds: 60));

      // Request essential notification permission in background
      PermissionService.requestInitialPermissions();

      try {
        await ApiClient.instance.init().timeout(const Duration(seconds: 2));
      } catch (e) {
        debugPrint('[_Bootstrap] ApiClient init warning: $e');
      }

      if (!mounted) return;

      String? serverUrl;
      try {
        serverUrl = await StorageService.instance.getServerUrl().timeout(
          const Duration(seconds: 2),
          onTimeout: () => null,
        );
      } catch (e) {
        debugPrint('[_Bootstrap] StorageService read warning: $e');
      }

      if (!mounted) return;

      if (serverUrl == null || serverUrl.trim().isEmpty) {
        Navigator.of(context).pushReplacement(
          MaterialPageRoute(builder: (_) => const DiscoveryScreen()),
        );
        return;
      }

      if (!ApiClient.instance.hasSession) {
        Navigator.of(context).pushReplacement(
          MaterialPageRoute(builder: (_) => const LoginScreen()),
        );
        return;
      }

      DeviceSyncService.instance.startAutoSync();

      Navigator.of(context).pushReplacement(
        MaterialPageRoute(builder: (_) => const HomeShell()),
      );
    } catch (e, st) {
      debugPrint('[_Bootstrap] Boot error: $e\n$st');
      if (mounted) {
        Navigator.of(context).pushReplacement(
          MaterialPageRoute(builder: (_) => const DiscoveryScreen()),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;

    return Scaffold(
      backgroundColor: colors.surface,
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(
              width: 80,
              height: 80,
              decoration: BoxDecoration(
                shape: BoxShape.circle,
                gradient: LinearGradient(
                  colors: [
                    colors.primary.withValues(alpha: 0.25),
                    colors.secondary.withValues(alpha: 0.1),
                  ],
                  begin: Alignment.topLeft,
                  end: Alignment.bottomRight,
                ),
                border: Border.all(
                  color: colors.primary.withValues(alpha: 0.4),
                  width: 1.5,
                ),
                boxShadow: [
                  BoxShadow(
                    color: colors.primary.withValues(alpha: 0.2),
                    blurRadius: 24,
                    spreadRadius: 2,
                  ),
                ],
              ),
              child: Icon(
                Icons.dns_rounded,
                size: 38,
                color: colors.primary,
              ),
            ),
            const SizedBox(height: 20),
            Text(
              'NivaroOS',
              style: Theme.of(context).textTheme.titleLarge?.copyWith(
                    fontWeight: FontWeight.w700,
                    letterSpacing: -0.5,
                  ),
            ),
            const SizedBox(height: 6),
            Text(
              'Personal Cloud & Server Hub',
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: colors.onSurfaceVariant.withValues(alpha: 0.7),
                  ),
            ),
            const SizedBox(height: 32),
            SizedBox(
              width: 24,
              height: 24,
              child: CircularProgressIndicator(
                strokeWidth: 2.5,
                valueColor: AlwaysStoppedAnimation<Color>(colors.primary),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
