import 'package:flutter/material.dart';

import '../services/api_client.dart';
import '../services/app_lock.dart';
import '../services/connection_security.dart';
import '../ui/ui.dart';
import '../widgets/connection_security_icon.dart';

/// Settings › Privacy & security: the app lock, hiding the terminal from
/// screenshots, and how safe the connection to the current server is.
class SecuritySettingsScreen extends StatefulWidget {
  const SecuritySettingsScreen({super.key, this.lock});

  /// The lock to show; [AppLock.instance] when null (tests pass their own).
  final AppLock? lock;

  /// One line for the Settings row that opens this screen.
  static String summary(AppLock lock) => [
        lock.enabled ? 'App lock on' : 'App lock off',
        if (lock.secureTerminal) 'Terminal hidden from screenshots',
      ].join(' · ');

  @override
  State<SecuritySettingsScreen> createState() => _SecuritySettingsScreenState();
}

class _SecuritySettingsScreenState extends State<SecuritySettingsScreen> {
  AppLock get _lock => widget.lock ?? AppLock.instance;
  bool _busy = false;

  static String _after(Duration d) => d == Duration.zero ? 'Immediately' : (d.inMinutes == 1 ? 'After 1 minute' : 'After ${d.inMinutes} minutes');

  Future<void> _toggleLock(bool on) async {
    if (_busy) return;
    setState(() => _busy = true);
    final messenger = ScaffoldMessenger.of(context);
    final outcome = await _lock.setEnabled(on);
    if (!mounted) return;
    setState(() => _busy = false);
    final text = switch (outcome) {
      AuthOutcome.success => null,
      AuthOutcome.cancelled => null,
      AuthOutcome.unavailable => 'Set a screen lock on this phone first (a PIN, pattern or password), then turn on the app lock.',
      AuthOutcome.lockedOut => 'Too many attempts. Wait a moment, then try again.',
      AuthOutcome.failed => 'Couldn’t check your screen lock. Try again.',
    };
    if (text != null) messenger.showSnackBar(SnackBar(content: Text(text)));
  }

  Future<void> _pickLockAfter() async {
    final picked = await showDialog<Duration>(
      context: context,
      builder: (context) => SimpleDialog(
        title: const Text('Lock the app'),
        children: [
          RadioGroup<Duration>(
            groupValue: _lock.lockAfter,
            onChanged: (d) => Navigator.of(context).pop(d),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                for (final d in AppLock.lockAfterChoices)
                  RadioListTile<Duration>(value: d, title: Text(d == Duration.zero ? 'As soon as you leave it' : _after(d))),
              ],
            ),
          ),
        ],
      ),
    );
    if (picked != null) await _lock.setLockAfter(picked);
  }

  @override
  Widget build(BuildContext context) {
    final url = ApiClient.instance.baseUrl;
    return ListenableBuilder(
      listenable: _lock,
      builder: (context, _) {
        final enabled = _lock.enabled;
        final security = url.isEmpty ? null : ConnectionSecurity.of(url);
        return AppScaffold(
          title: 'Privacy & security',
          body: ListView(
            children: [
              if (_lock.turnedOffNoScreenLock)
                Padding(
                  padding: EdgeInsets.fromLTRB(Space.gutter(context), 0, Space.gutter(context), Space.md),
                  child: Notice(
                    title: 'The app lock turned itself off',
                    message: 'This phone no longer has a screen lock, so there was nothing to unlock with. Set one, then turn the app lock on again.',
                    actionLabel: 'OK',
                    onAction: _lock.acknowledgeTurnedOff,
                  ),
                ),
              TileGroup(
                title: 'App lock',
                footer: 'With the app lock on, opening a terminal, restarting or shutting down the server and deleting a VM also ask for it. '
                    'The app’s content is hidden in recent apps (Android 13 and later).',
                children: [
                  SwitchListTile(
                    secondary: Icon(enabled ? Icons.lock_outline : Icons.lock_open_outlined),
                    title: const Text('App lock'),
                    subtitle: const Text('Ask for your fingerprint, face or screen lock to open the app'),
                    value: enabled,
                    onChanged: _busy ? null : _toggleLock,
                  ),
                  ListTile(
                    leading: const Icon(Icons.timer_outlined),
                    title: const Text('Lock the app'),
                    subtitle: Text(_lock.lockAfter == Duration.zero ? 'As soon as you leave it' : '${_after(_lock.lockAfter)} in the background'),
                    enabled: enabled,
                    onTap: enabled ? _pickLockAfter : null,
                  ),
                ],
              ),
              TileGroup(
                title: 'Screenshots',
                footer: 'The sign-in screen is always hidden from screenshots and recent apps.',
                children: [
                  SwitchListTile(
                    secondary: const Icon(Icons.terminal_outlined),
                    title: const Text('Hide the terminal'),
                    subtitle: const Text('Keep terminals out of screenshots, screen recordings and recent apps'),
                    value: _lock.secureTerminal,
                    onChanged: _lock.setSecureTerminal,
                  ),
                ],
              ),
              if (security != null)
                TileGroup(
                  title: 'Connection',
                  children: [
                    ListTile(
                      leading: ConnectionSecurityIcon(url: url),
                      title: Text(security.label),
                      subtitle: Text('${ApiClient.displayHost(url)} · ${security.description}'),
                    ),
                  ],
                ),
            ],
          ),
        );
      },
    );
  }
}
