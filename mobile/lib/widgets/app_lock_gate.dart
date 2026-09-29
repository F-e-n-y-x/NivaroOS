import 'package:flutter/material.dart';

import '../services/app_lock.dart';
import '../ui/theme/spacing.dart';
import '../ui/widgets/brand_mark.dart';

/// Covers the whole app while [AppLock] is locked: the app underneath
/// keeps its state (the tab, the folder, a half-typed form) but isn't
/// drawn, focusable or read out until the owner unlocks. The system prompt
/// opens by itself each time the lock comes down; Unlock opens it again.
///
/// Goes in MaterialApp.builder, outside the Navigator, so dialogs and
/// sheets are covered too.
class AppLockGate extends StatefulWidget {
  const AppLockGate({super.key, required this.child, this.lock});

  final Widget child;
  /// The lock to show; [AppLock.instance] when null (tests pass their own).
  final AppLock? lock;

  @override
  State<AppLockGate> createState() => _AppLockGateState();
}

class _AppLockGateState extends State<AppLockGate> {
  AppLock get _lock => widget.lock ?? AppLock.instance;
  bool _wasLocked = false;
  bool _busy = false;
  String? _message;

  @override
  void initState() {
    super.initState();
    _lock.addListener(_changed);
    _changed();
  }

  @override
  void didUpdateWidget(AppLockGate oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.lock != widget.lock) {
      (oldWidget.lock ?? AppLock.instance).removeListener(_changed);
      _lock.addListener(_changed);
    }
  }

  @override
  void dispose() {
    _lock.removeListener(_changed);
    super.dispose();
  }

  void _changed() {
    final locked = _lock.locked;
    if (locked && !_wasLocked) {
      _message = null;
      // Ask straight away, once the cover is on screen.
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && _lock.locked) _unlock();
      });
    }
    _wasLocked = locked;
    if (mounted) setState(() {});
  }

  Future<void> _unlock() async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _message = null;
    });
    final outcome = await _lock.unlock();
    if (!mounted) return;
    setState(() {
      _busy = false;
      _message = switch (outcome) {
        AuthOutcome.success || AuthOutcome.cancelled => null,
        AuthOutcome.lockedOut => 'Too many attempts. Wait a moment, then try again.',
        AuthOutcome.unavailable => 'This phone can’t check its screen lock right now. Try again.',
        AuthOutcome.failed => 'That didn’t work. Try again.',
      };
    });
  }

  @override
  Widget build(BuildContext context) {
    final locked = _lock.locked;
    return Stack(
      fit: StackFit.expand,
      children: [
        // Kept, not drawn: nothing of it shows or can be reached while
        // locked.
        Offstage(
          offstage: locked,
          child: ExcludeFocus(excluding: locked, child: TickerMode(enabled: !locked, child: widget.child)),
        ),
        if (locked) _LockScreen(busy: _busy, message: _message, onUnlock: _unlock),
      ],
    );
  }
}

class _LockScreen extends StatelessWidget {
  const _LockScreen({required this.busy, required this.message, required this.onUnlock});

  final bool busy;
  final String? message;
  final VoidCallback onUnlock;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final s = theme.colorScheme;
    return Material(
      color: s.surface,
      child: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: EdgeInsets.symmetric(horizontal: Space.gutter(context), vertical: Space.xl),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const BrandMark(size: 56),
                const SizedBox(height: Space.lg),
                Semantics(
                  header: true,
                  child: Text('NivaroOS is locked', style: theme.textTheme.headlineSmall, textAlign: TextAlign.center),
                ),
                const SizedBox(height: Space.sm),
                Text(
                  'Use your fingerprint, face or screen lock to open it.',
                  style: theme.textTheme.bodyMedium?.copyWith(color: s.onSurfaceVariant),
                  textAlign: TextAlign.center,
                ),
                const SizedBox(height: Space.xl),
                FilledButton.icon(
                  onPressed: busy ? null : onUnlock,
                  icon: const Icon(Icons.fingerprint),
                  label: const Text('Unlock'),
                ),
                if (message != null) ...[
                  const SizedBox(height: Space.md),
                  Semantics(
                    liveRegion: true,
                    child: Text(message!, style: theme.textTheme.bodyMedium?.copyWith(color: s.error), textAlign: TextAlign.center),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}
