import 'package:flutter/material.dart';
import 'theme.dart';
import 'services/api_client.dart';
import 'services/storage_service.dart';
import 'screens/discovery_screen.dart';
import 'screens/login_screen.dart';
import 'screens/home_shell.dart';

void main() {
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
    await ApiClient.instance.init();
    if (!mounted) return;
    final serverUrl = await StorageService.instance.getServerUrl();
    if (serverUrl == null || serverUrl.isEmpty) {
      Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => const DiscoveryScreen()));
      return;
    }
    if (!ApiClient.instance.hasSession) {
      Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => const LoginScreen()));
      return;
    }
    Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => const HomeShell()));
  }

  @override
  Widget build(BuildContext context) {
    return const Scaffold(
      body: Center(child: CircularProgressIndicator()),
    );
  }
}
