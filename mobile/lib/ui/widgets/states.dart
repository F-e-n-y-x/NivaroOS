import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../theme/design_tokens.dart';
import '../theme/spacing.dart';

/// The layout shared by [EmptyState] and [ErrorState].
///
/// As a box it is centred in whatever space it gets, scrollable so it
/// survives 200% text and short landscape screens, and always scrollable
/// so it can sit inside a RefreshIndicator. As a sliver it fills what is
/// left of the scroll view instead (and grows past it when the content is
/// taller), so it can go straight into `AppScaffold.slivers`.
class _StateLayout extends StatelessWidget {
  const _StateLayout({
    required this.icon,
    required this.iconColor,
    required this.title,
    required this.message,
    required this.actions,
    required this.sliver,
  });

  final IconData icon;
  final Color iconColor;
  final String title;
  final String message;
  final List<Widget> actions;
  final bool sliver;

  Widget _content(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 360),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 48, color: iconColor),
            const SizedBox(height: Space.lg),
            Semantics(
              header: true,
              child: Text(title, style: theme.textTheme.titleLarge, textAlign: TextAlign.center),
            ),
            const SizedBox(height: Space.sm),
            Text(
              message,
              style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
              textAlign: TextAlign.center,
            ),
            if (actions.isNotEmpty) ...[
              const SizedBox(height: Space.xl),
              Wrap(
                alignment: WrapAlignment.center,
                spacing: Space.sm,
                runSpacing: Space.sm,
                children: actions,
              ),
            ],
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final padding = EdgeInsets.symmetric(horizontal: Space.gutter(context), vertical: Space.xl);
    if (sliver) {
      return SliverFillRemaining(
        hasScrollBody: false,
        child: Padding(padding: padding, child: _content(context)),
      );
    }
    return LayoutBuilder(
      builder: (context, constraints) => SingleChildScrollView(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: padding,
        child: ConstrainedBox(
          constraints: BoxConstraints(
            minHeight: constraints.hasBoundedHeight ? (constraints.maxHeight - padding.vertical).clamp(0, double.infinity) : 0,
          ),
          child: _content(context),
        ),
      ),
    );
  }
}

/// What an empty list means and the one action that fills it:
/// "No apps yet / Apps you install appear here. / [Open app store]".
class EmptyState extends StatelessWidget {
  const EmptyState({
    super.key,
    required this.icon,
    required this.title,
    required this.message,
    this.actionLabel,
    this.onAction,
    this.sliver = false,
  });

  final IconData icon;
  final String title;
  final String message;
  final String? actionLabel;
  final VoidCallback? onAction;

  /// True to place it directly in a sliver list (`AppScaffold.slivers`).
  final bool sliver;

  @override
  Widget build(BuildContext context) {
    return _StateLayout(
      icon: icon,
      iconColor: Theme.of(context).colorScheme.onSurfaceVariant,
      title: title,
      message: message,
      actions: [
        if (actionLabel != null && onAction != null) FilledButton.tonal(style: tonalButtonStyle(context), onPressed: onAction, child: Text(actionLabel!)),
      ],
      sliver: sliver,
    );
  }
}

/// A failed load: what happened in plain words, Retry, and - when there is
/// something technical worth reporting - a button that copies it.
///
/// The title names what failed ("Couldn't load apps"), so it is required.
class ErrorState extends StatelessWidget {
  const ErrorState({
    super.key,
    required this.title,
    required this.message,
    required this.onRetry,
    this.details,
    this.icon = Icons.error_outline,
    this.sliver = false,
  }) : _offline = false;

  /// "Can't reach the server" with nothing cached to show: a network
  /// failure, with the wording and icon the whole app uses for it. Being
  /// offline is a situation, not a fault, so the icon is neutral rather
  /// than red. With cached data on screen, use an OfflineBanner instead.
  const ErrorState.offline({super.key, required this.onRetry, this.details, this.sliver = false})
      : title = "Can't reach the server",
        message = 'Check that this phone is on the same network as the server, or connected through Tailscale.',
        icon = Icons.cloud_off_outlined,
        _offline = true;

  final bool _offline;

  /// True to place it directly in a sliver list (`AppScaffold.slivers`).
  final bool sliver;

  final String title;
  final String message;
  final VoidCallback onRetry;

  /// Technical detail (route, HTTP status, a body excerpt) for a bug report.
  /// Never shown on screen; "Copy details" puts it on the clipboard.
  final String? details;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return _StateLayout(
      icon: icon,
      iconColor: _offline ? scheme.onSurfaceVariant : scheme.error,
      title: title,
      message: message,
      actions: [
        FilledButton.icon(onPressed: onRetry, icon: const Icon(Icons.refresh), label: const Text('Retry')),
        if (details != null)
          TextButton(
            onPressed: () async {
              await Clipboard.setData(ClipboardData(text: details!));
              if (!context.mounted) return;
              ScaffoldMessenger.maybeOf(context)?.showSnackBar(const SnackBar(content: Text('Details copied')));
            },
            child: const Text('Copy details'),
          ),
      ],
      sliver: sliver,
    );
  }
}
