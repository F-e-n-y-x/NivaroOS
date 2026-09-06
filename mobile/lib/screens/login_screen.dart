import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../services/storage_service.dart';
import '../widgets/common.dart';
import 'home_shell.dart';
import 'discovery_screen.dart';

/// Clean, modern Material 3 login screen with server status badge,
/// password obscurity toggle, and haptic feedback.
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

    HapticFeedback.mediumImpact();
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
    HapticFeedback.lightImpact();
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
    final serverHost = ApiClient.instance.baseUrl.replaceFirst(RegExp(r'^https?://'), '');

    return Scaffold(
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(24, 20, 24, 24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              // Top Action Row
              Row(
                children: [
                  IconButton(
                    icon: const Icon(Icons.arrow_back_rounded),
                    onPressed: _changeServer,
                  ),
                  const Spacer(),
                  TextButton.icon(
                    onPressed: _changeServer,
                    icon: const Icon(Icons.dns_rounded, size: 16),
                    label: const Text('Change Server'),
                  ),
                ],
              ),
              const SizedBox(height: 16),

              // Glowing Header Card
              DarkCard(
                padding: const EdgeInsets.all(20),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Container(
                          width: 48,
                          height: 48,
                          decoration: const BoxDecoration(
                            shape: BoxShape.circle,
                            gradient: LinearGradient(
                              colors: [
                                NivaroColors.primary,
                                NivaroColors.primaryLight,
                              ],
                              begin: Alignment.topLeft,
                              end: Alignment.bottomRight,
                            ),
                          ),
                          alignment: Alignment.center,
                          child: const Icon(Icons.dns_rounded, color: Colors.white, size: 26),
                        ),
                        const SizedBox(width: 14),
                        const Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                'NivaroOS Server',
                                style: TextStyle(fontWeight: FontWeight.w800, fontSize: 17),
                              ),
                              SizedBox(height: 2),
                              Text(
                                'Sign in to manage server instances',
                                style: TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                              ),
                            ],
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 16),
                    LanBadge(address: serverHost),
                  ],
                ),
              ),
              const SizedBox(height: 28),

              // Title
              Text(
                'Welcome Back',
                style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                      fontWeight: FontWeight.w800,
                      letterSpacing: -0.5,
                    ),
              ),
              const SizedBox(height: 6),
              const Text(
                'Enter your NivaroOS administrator credentials to continue.',
                style: TextStyle(color: NivaroColors.textMuted, fontSize: 13.5),
              ),
              const SizedBox(height: 24),

              // Username Field
              TextField(
                controller: _usernameController,
                textInputAction: TextInputAction.next,
                decoration: InputDecoration(
                  labelText: 'Username or Email',
                  prefixIcon: const Icon(Icons.person_rounded, size: 20),
                  filled: true,
                  fillColor: NivaroColors.surfaceRaised,
                  border: OutlineInputBorder(
                    borderRadius: BorderRadius.circular(NivaroShape.large),
                    borderSide: const BorderSide(color: NivaroColors.borderSubtle),
                  ),
                ),
              ),
              const SizedBox(height: 14),

              // Password Field
              TextField(
                controller: _passwordController,
                obscureText: _obscure,
                textInputAction: TextInputAction.done,
                onSubmitted: (_) => _login(),
                decoration: InputDecoration(
                  labelText: 'Password',
                  prefixIcon: const Icon(Icons.lock_rounded, size: 20),
                  suffixIcon: IconButton(
                    icon: Icon(_obscure ? Icons.visibility_outlined : Icons.visibility_off_outlined, size: 20),
                    onPressed: () => setState(() => _obscure = !_obscure),
                  ),
                  filled: true,
                  fillColor: NivaroColors.surfaceRaised,
                  border: OutlineInputBorder(
                    borderRadius: BorderRadius.circular(NivaroShape.large),
                    borderSide: const BorderSide(color: NivaroColors.borderSubtle),
                  ),
                ),
              ),

              if (_error != null) ...[
                const SizedBox(height: 14),
                DarkCard(
                  color: NivaroColors.danger.withValues(alpha: 0.15),
                  child: Row(
                    children: [
                      const Icon(Icons.error_outline_rounded, color: NivaroColors.danger, size: 20),
                      const SizedBox(width: 12),
                      Expanded(child: Text(_error!, style: const TextStyle(color: NivaroColors.danger, fontSize: 13))),
                    ],
                  ),
                ),
              ],
              const SizedBox(height: 28),

              // Login Button
              SizedBox(
                width: double.infinity,
                height: 52,
                child: FilledButton(
                  onPressed: _loading ? null : _login,
                  style: FilledButton.styleFrom(
                    backgroundColor: NivaroColors.primary,
                    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
                  ),
                  child: _loading
                      ? const SizedBox(width: 22, height: 22, child: CircularProgressIndicator(strokeWidth: 2.5, color: Colors.white))
                      : const Text('Sign In', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 16)),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

