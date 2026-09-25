import 'package:flutter/material.dart';

import '../theme/spacing.dart';
import '../theme/status_colors.dart';

/// An inline notice inside a page: an icon, a sentence and an optional
/// action, on the status tone's container ("Sharing stopped after 6
/// hours. [Share again]"). For something the reader should notice but
/// that doesn't block the page; errors that replace the content are
/// [ErrorState]s.
///
/// It sits on the same edge as TileGroup rows (the screen gutter).
class Notice extends StatelessWidget {
  const Notice({super.key, required this.message, this.status = Status.warning, this.icon, this.actionLabel, this.onAction, this.title});

  final String? title;
  final String message;
  final Status status;
  final IconData? icon;
  final String? actionLabel;
  final VoidCallback? onAction;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final tone = StatusColors.toneOf(context, status);
    final gutter = Space.gutter(context);
    final hasAction = actionLabel != null && onAction != null;
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.sm),
      child: Semantics(
        container: true,
        liveRegion: true,
        child: Material(
          color: tone.container,
          borderRadius: BorderRadius.circular(Corners.large),
          child: Padding(
            padding: EdgeInsets.fromLTRB(Space.lg, Space.md, Space.lg, hasAction ? Space.xs : Space.md),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Padding(
                      padding: const EdgeInsets.only(top: 2),
                      child: Icon(icon ?? _icon(status), color: tone.onContainer),
                    ),
                    const SizedBox(width: Space.lg),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          if (title != null) ...[
                            Text(title!, style: theme.textTheme.titleSmall?.copyWith(color: tone.onContainer)),
                            const SizedBox(height: 2),
                          ],
                          Text(message, style: theme.textTheme.bodyMedium?.copyWith(color: tone.onContainer)),
                        ],
                      ),
                    ),
                  ],
                ),
                if (hasAction)
                  Align(
                    alignment: AlignmentDirectional.centerEnd,
                    child: TextButton(
                      style: TextButton.styleFrom(foregroundColor: tone.onContainer),
                      onPressed: onAction,
                      child: Text(actionLabel!),
                    ),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  static IconData _icon(Status s) => switch (s) {
        Status.error => Icons.error_outline,
        Status.success => Icons.check_circle_outline,
        Status.info || Status.neutral => Icons.info_outline,
        Status.warning => Icons.warning_amber_outlined,
      };
}

/// A free-standing footnote on a TileGroup page ("Only reachable on your
/// network"), on the same edge as the group headers and footers, so a
/// screen keeps one text edge.
class GroupNote extends StatelessWidget {
  const GroupNote(this.text, {super.key});

  final String text;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter + Space.lg, Space.sm, gutter + Space.lg, Space.sm),
      child: Text(text, style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
    );
  }
}
