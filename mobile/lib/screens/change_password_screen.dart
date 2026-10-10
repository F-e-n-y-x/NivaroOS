import 'package:flutter/material.dart';

import '../services/api_client.dart';
import '../ui/ui.dart';

/// Settings › Account › Change password: the current one, the new one twice. The server
/// signs out every other device on the account; this phone continues with
/// the new tokens it hands back ([ApiClient.changePassword]).
class ChangePasswordScreen extends StatefulWidget {
  const ChangePasswordScreen({super.key});

  /// The server's minimum (services/user PutUserPassword).
  static const minLength = 6;

  @override
  State<ChangePasswordScreen> createState() => _ChangePasswordScreenState();
}

class _ChangePasswordScreenState extends State<ChangePasswordScreen> {
  final _current = TextEditingController();
  final _next = TextEditingController();
  final _confirm = TextEditingController();
  bool _obscure = true;
  bool _saving = false;
  String? _currentError;
  String? _nextError;
  String? _confirmError;
  String? _error;

  @override
  void dispose() {
    _current.dispose();
    _next.dispose();
    _confirm.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final current = _current.text, next = _next.text;
    setState(() {
      _currentError = current.isEmpty ? 'Enter your current password' : null;
      _nextError = next.isEmpty
          ? 'Enter a new password'
          : next.length < ChangePasswordScreen.minLength
              ? 'Use at least ${ChangePasswordScreen.minLength} characters'
              : null;
      _confirmError = _nextError == null && _confirm.text != next ? "The passwords don't match" : null;
      _error = null;
    });
    if (_currentError != null || _nextError != null || _confirmError != null) return;
    setState(() => _saving = true);
    final nav = Navigator.of(context);
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ApiClient.instance.changePassword(current, next);
      messenger.showSnackBar(const SnackBar(content: Text('Password changed. Other devices have to sign in again.')));
      nav.pop(true);
    } on ApiException catch (e) {
      if (!mounted) return;
      final m = e.message.toLowerCase();
      setState(() {
        _saving = false;
        if (m.contains('old password')) {
          _currentError = "That isn't your current password";
        } else if (m.contains('too simple')) {
          _nextError = 'Use at least ${ChangePasswordScreen.minLength} characters';
        } else {
          _error = e.message;
        }
      });
    }
  }

  Widget _field(TextEditingController c, String label, String? error, {List<String> hints = const [AutofillHints.newPassword], TextInputAction action = TextInputAction.next, String? helper}) =>
      TextField(
        controller: c,
        enabled: !_saving,
        obscureText: _obscure,
        autofillHints: hints,
        textInputAction: action,
        onSubmitted: action == TextInputAction.done ? (_) => _save() : null,
        onChanged: (_) {
          if (error != null) setState(() => _currentError = _nextError = _confirmError = null);
        },
        decoration: InputDecoration(
          labelText: label,
          helperText: helper,
          errorText: error,
          prefixIcon: const Icon(Icons.lock_outline),
          suffixIcon: IconButton(
            tooltip: _obscure ? 'Show passwords' : 'Hide passwords',
            icon: Icon(_obscure ? Icons.visibility_outlined : Icons.visibility_off_outlined),
            onPressed: () => setState(() => _obscure = !_obscure),
          ),
        ),
      );

  @override
  Widget build(BuildContext context) {
    final gutter = Space.gutter(context);
    final scheme = Theme.of(context).colorScheme;
    // Notices bring their own gutter; everything else gets it here.
    Widget pad(Widget w) => Padding(padding: EdgeInsets.symmetric(horizontal: gutter), child: w);
    return AppScaffold(
      title: 'Change password',
      body: AutofillGroup(
        child: ListView(
          padding: const EdgeInsets.only(top: Space.lg, bottom: Space.xl),
          children: [
            pad(_field(_current, 'Current password', _currentError, hints: const [AutofillHints.password])),
            const SizedBox(height: Space.lg),
            pad(_field(_next, 'New password', _nextError, helper: 'At least ${ChangePasswordScreen.minLength} characters')),
            const SizedBox(height: Space.lg),
            pad(_field(_confirm, 'Confirm new password', _confirmError, action: TextInputAction.done)),
            const SizedBox(height: Space.md),
            const Notice(
              status: Status.info,
              message: 'Browsers and other devices signed in to this account will have to sign in again. This phone stays signed in.',
            ),
            if (_error != null) Notice(status: Status.error, message: _error!),
            const SizedBox(height: Space.lg),
            pad(FilledButton(
              style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
              onPressed: _saving ? null : _save,
              child: _saving
                  ? Row(mainAxisSize: MainAxisSize.min, children: [
                      SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2, color: scheme.onSurface.withValues(alpha: 0.38))),
                      const SizedBox(width: Space.md),
                      const Text('Changing…'),
                    ])
                  : const Text('Change password'),
            )),
          ],
        ),
      ),
    );
  }
}
