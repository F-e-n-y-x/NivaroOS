import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:url_launcher/url_launcher.dart';

import '../services/api_client.dart';
import '../services/session_service.dart';
import '../services/storage_service.dart';
import '../ui/ui.dart';
import 'discovery_screen.dart';
import 'home_shell.dart';

/// Sign in to the current server. Also the "sign in again" screen when the
/// server ended the session ([isReauth]): it then returns true to whoever
/// pushed it, so the screen underneath carries on.
///
/// Username and password are an autofill group, so password managers fill
/// and save them.
class LoginScreen extends StatefulWidget {
  final bool isReauth;
  final String? initialUsername;

  /// False when the server has no account yet (from the address check).
  final bool serverInitialized;

  const LoginScreen({super.key, this.isReauth = false, this.initialUsername, this.serverInitialized = true});

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  final _usernameController = TextEditingController();
  final _passwordController = TextEditingController();
  final _passwordFocus = FocusNode();
  bool _loading = false;
  bool _obscure = true;
  String? _usernameError;
  String? _passwordError;
  String? _error;
  String? _errorDetails;

  @override
  void initState() {
    super.initState();
    if (widget.initialUsername != null && widget.initialUsername!.isNotEmpty) {
      _usernameController.text = widget.initialUsername!;
    } else {
      StorageService.instance.getUsername().then((u) {
        if (mounted && u != null && u.isNotEmpty && _usernameController.text.isEmpty) {
          setState(() => _usernameController.text = u);
        }
      });
    }
  }

  @override
  void dispose() {
    _usernameController.dispose();
    _passwordController.dispose();
    _passwordFocus.dispose();
    super.dispose();
  }

  Future<void> _login() async {
    final username = _usernameController.text.trim();
    final password = _passwordController.text;
    setState(() {
      _usernameError = username.isEmpty ? 'Enter your username' : null;
      _passwordError = password.isEmpty ? 'Enter your password' : null;
      _error = null;
      _errorDetails = null;
    });
    if (username.isEmpty || password.isEmpty) return;

    setState(() => _loading = true);
    try {
      final tokens = await ApiClient.instance.login(username, password);
      await SessionService.signedIn(accessToken: tokens.accessToken, refreshToken: tokens.refreshToken, username: username);
      TextInput.finishAutofillContext();
      if (!mounted) return;
      if (widget.isReauth && Navigator.canPop(context)) {
        Navigator.of(context).pop(true);
      } else {
        // The shell becomes the only route, so discovery, a server switch
        // or an old shell can't sit under it and back at Home leaves the
        // app (with Android's predictive back-to-home animation).
        Navigator.of(context).pushAndRemoveUntil(MaterialPageRoute(builder: (_) => const HomeShell()), (_) => false);
      }
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        if (e.kind == ApiErrorKind.auth) {
          _passwordError = e.message;
          _passwordController.clear();
          _passwordFocus.requestFocus();
        } else {
          _error = e.message;
          _errorDetails = e.details;
        }
      });
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _changeServer() {
    if (widget.isReauth && Navigator.canPop(context)) {
      Navigator.of(context).pop(false);
      return;
    }
    if (Navigator.canPop(context)) {
      Navigator.of(context).pop();
    } else {
      Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => const DiscoveryScreen()));
    }
  }

  Future<void> _openInBrowser() async {
    final uri = Uri.tryParse(ApiClient.instance.baseUrl);
    if (uri == null) return;
    try {
      await launchUrl(uri, mode: LaunchMode.externalApplication);
    } catch (_) {}
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final base = ApiClient.instance.baseUrl;
    final host = ApiClient.displayHost(base);
    final encrypted = base.startsWith('https://');
    final gutter = Space.gutter(context);
    final large = MediaQuery.textScalerOf(context).scale(10) > 13;

    return AppScaffold(
      title: widget.isReauth ? 'Sign in again' : 'Sign in',
      leading: Navigator.canPop(context) ? null : const BrandMark(),
      body: ListView(
        padding: const EdgeInsets.only(bottom: Space.xl),
        children: [
          if (widget.isReauth)
            Padding(
              padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.sm),
              child: Text(
                'Your session on $host ended. Sign in to carry on where you were.',
                style: theme.textTheme.bodyLarge?.copyWith(color: scheme.onSurfaceVariant),
              ),
            ),
          TileGroup(children: [
            ListTile(
              leading: const Icon(Icons.dns_outlined),
              title: Text(host.isEmpty ? 'No server' : host),
              // Whether the password travels encrypted matters, so it is a
              // status, not grey small print.
              subtitle: Padding(
                padding: const EdgeInsets.only(top: Space.xs),
                child: Align(
                  alignment: AlignmentDirectional.centerStart,
                  child: encrypted
                      ? const StatusChip(label: 'Encrypted', status: Status.success, icon: Icons.lock_outline)
                      : const StatusChip(label: 'Not encrypted', status: Status.warning, icon: Icons.lock_open_outlined),
                ),
              ),
              // At large text the button would squeeze the chip; it gets
              // a row of its own instead.
              trailing: large
                  ? null
                  : TextButton(
                      onPressed: _loading ? null : _changeServer,
                      child: Text(widget.isReauth ? 'Cancel' : 'Change'),
                    ),
            ),
            if (large)
              ListTile(
                leading: Icon(widget.isReauth ? Icons.close_outlined : Icons.swap_horiz_outlined),
                title: Text(widget.isReauth ? 'Cancel' : 'Change server'),
                enabled: !_loading,
                onTap: _changeServer,
              ),
          ]),
          if (!widget.serverInitialized) ...[
            const SizedBox(height: Space.lg),
            Padding(
              padding: EdgeInsets.symmetric(horizontal: gutter),
              child: Card.filled(
                color: scheme.secondaryContainer,
                child: Padding(
                  padding: const EdgeInsets.all(Space.lg),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text('This server has no account yet',
                          style: theme.textTheme.titleMedium?.copyWith(color: scheme.onSecondaryContainer)),
                      const SizedBox(height: Space.xs),
                      Text(
                        'Create the first account in a browser, then sign in here with it.',
                        style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSecondaryContainer),
                      ),
                      const SizedBox(height: Space.md),
                      OutlinedButton.icon(
                        onPressed: _openInBrowser,
                        icon: const Icon(Icons.open_in_new_outlined),
                        label: const Text('Open in browser'),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ],
          const SizedBox(height: Space.xl),
          Padding(
            padding: EdgeInsets.symmetric(horizontal: gutter),
            child: AutofillGroup(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  TextField(
                    controller: _usernameController,
                    enabled: !_loading,
                    autofillHints: const [AutofillHints.username],
                    autocorrect: false,
                    enableSuggestions: false,
                    textInputAction: TextInputAction.next,
                    onSubmitted: (_) => _passwordFocus.requestFocus(),
                    onChanged: (_) {
                      if (_usernameError != null) setState(() => _usernameError = null);
                    },
                    decoration: InputDecoration(
                      labelText: 'Username',
                      prefixIcon: const Icon(Icons.person_outline),
                      errorText: _usernameError,
                    ),
                  ),
                  const SizedBox(height: Space.lg),
                  TextField(
                    controller: _passwordController,
                    focusNode: _passwordFocus,
                    enabled: !_loading,
                    obscureText: _obscure,
                    autofillHints: const [AutofillHints.password],
                    textInputAction: TextInputAction.done,
                    onSubmitted: (_) => _login(),
                    onChanged: (_) {
                      if (_passwordError != null) setState(() => _passwordError = null);
                    },
                    decoration: InputDecoration(
                      labelText: 'Password',
                      prefixIcon: const Icon(Icons.lock_outline),
                      errorText: _passwordError,
                      suffixIcon: IconButton(
                        tooltip: _obscure ? 'Show password' : 'Hide password',
                        icon: Icon(_obscure ? Icons.visibility_outlined : Icons.visibility_off_outlined),
                        onPressed: () => setState(() => _obscure = !_obscure),
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
          if (_error != null)
            Padding(
              padding: EdgeInsets.fromLTRB(gutter, Space.lg, gutter, 0),
              child: _ErrorNote(message: _error!, details: _errorDetails),
            ),
          const SizedBox(height: Space.xl),
          Padding(
            padding: EdgeInsets.symmetric(horizontal: gutter),
            child: FilledButton(
              style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
              onPressed: _loading ? null : _login,
              child: _loading
                  ? Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2, color: scheme.onSurface.withValues(alpha: 0.38))),
                        const SizedBox(width: Space.md),
                        const Text('Signing in…'),
                      ],
                    )
                  : const Text('Sign in'),
            ),
          ),
        ],
      ),
    );
  }
}

/// A failed sign-in that isn't about the password: unreachable, the
/// certificate, a proxy's web page. Announced to TalkBack as it appears.
class _ErrorNote extends StatelessWidget {
  const _ErrorNote({required this.message, this.details});

  final String message;
  final String? details;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Semantics(
      liveRegion: true,
      child: Card.filled(
        color: scheme.errorContainer,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(Space.lg, Space.md, Space.sm, Space.md),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Padding(
                padding: const EdgeInsets.only(top: 2),
                child: Icon(Icons.error_outline, size: 20, color: scheme.onErrorContainer),
              ),
              const SizedBox(width: Space.md),
              Expanded(
                child: Text(message, style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onErrorContainer)),
              ),
              if (details != null)
                IconButton(
                  tooltip: 'Copy details',
                  icon: Icon(Icons.content_copy_outlined, color: scheme.onErrorContainer),
                  onPressed: () async {
                    await Clipboard.setData(ClipboardData(text: details!));
                    if (!context.mounted) return;
                    ScaffoldMessenger.maybeOf(context)?.showSnackBar(const SnackBar(content: Text('Details copied')));
                  },
                ),
            ],
          ),
        ),
      ),
    );
  }
}
