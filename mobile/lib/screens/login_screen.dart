import 'package:flutter/material.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../services/storage_service.dart';
import 'home_shell.dart';
import 'discovery_screen.dart';

class LoginScreen extends StatefulWidget {
  const LoginScreen({super.key});

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  final _usernameController = TextEditingController();
  final _passwordController = TextEditingController();
  bool _loading = false;
  bool _obscure = true;
  String? _error;

  Future<void> _login() async {
    final username = _usernameController.text.trim();
    final password = _passwordController.text;
    if (username.isEmpty || password.isEmpty) return;

    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final res = await ApiClient.instance.post('/users/login', body: {
        'username': username,
        'password': password,
      });
      final data = res['data'] as Map<String, dynamic>?;
      final token = data?['token'] as Map<String, dynamic>?;
      if (token == null || token['access_token'] == null) {
        throw Exception(res['message']?.toString() ?? 'Login failed.');
      }
      final accessToken = token['access_token'] as String;
      final refreshToken = token['refresh_token'] as String;
      ApiClient.instance.setSession(accessToken, refreshToken);
      await StorageService.instance.setSession(
        accessToken: accessToken,
        refreshToken: refreshToken,
        username: username,
      );
      if (!mounted) return;
      Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => const HomeShell()));
    } catch (e) {
      setState(() => _error = e.toString().replaceFirst('Exception: ', ''));
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _changeServer() async {
    await StorageService.instance.clearAll();
    ApiClient.instance.clearSession();
    if (!mounted) return;
    Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => const DiscoveryScreen()));
  }

  @override
  void dispose() {
    _usernameController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Icon(Icons.cloud_outlined, size: 48, color: NivaroColors.primary),
                const SizedBox(height: 8),
                const Text('Sign in to NivaroOS', style: TextStyle(fontSize: 20, fontWeight: FontWeight.w700)),
                const SizedBox(height: 4),
                FutureBuilder<String>(
                  future: Future.value(ApiClient.instance.baseUrl),
                  builder: (context, snap) => Text(
                    snap.data ?? '',
                    style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13),
                  ),
                ),
                const SizedBox(height: 32),
                TextField(
                  controller: _usernameController,
                  textInputAction: TextInputAction.next,
                  decoration: const InputDecoration(labelText: 'Username'),
                ),
                const SizedBox(height: 14),
                TextField(
                  controller: _passwordController,
                  obscureText: _obscure,
                  textInputAction: TextInputAction.done,
                  onSubmitted: (_) => _login(),
                  decoration: InputDecoration(
                    labelText: 'Password',
                    suffixIcon: IconButton(
                      icon: Icon(_obscure ? Icons.visibility_outlined : Icons.visibility_off_outlined),
                      onPressed: () => setState(() => _obscure = !_obscure),
                    ),
                  ),
                ),
                if (_error != null) ...[
                  const SizedBox(height: 12),
                  Text(_error!, style: const TextStyle(color: NivaroColors.danger, fontSize: 13)),
                ],
                const SizedBox(height: 24),
                SizedBox(
                  width: double.infinity,
                  child: ElevatedButton(
                    onPressed: _loading ? null : _login,
                    child: _loading
                        ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                        : const Text('Sign In'),
                  ),
                ),
                const SizedBox(height: 12),
                TextButton(onPressed: _changeServer, child: const Text('Not your server? Change it')),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
