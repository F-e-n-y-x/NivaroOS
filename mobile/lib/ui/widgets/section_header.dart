import 'package:flutter/material.dart';

import '../theme/spacing.dart';

/// The label above a group of rows: titleSmall in the primary colour, on
/// the screen gutter, with an optional text action on the right ("See all").
class SectionHeader extends StatelessWidget {
  const SectionHeader({super.key, required this.title, this.actionLabel, this.onAction});

  final String title;

  /// Label of the optional trailing action. Shown only with [onAction].
  final String? actionLabel;
  final VoidCallback? onAction;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    final hasAction = actionLabel != null && onAction != null;
    return Padding(
      // A text button brings its own 48dp height and 12dp inner padding, so
      // the row trims its right padding to keep the label on the gutter.
      padding: EdgeInsets.fromLTRB(gutter, Space.lg, hasAction ? gutter - Space.md : gutter, Space.sm),
      child: Row(
        children: [
          Expanded(
            child: Semantics(
              header: true,
              child: Text(title, style: theme.textTheme.titleSmall?.copyWith(color: theme.colorScheme.primary)),
            ),
          ),
          if (hasAction) TextButton(onPressed: onAction, child: Text(actionLabel!)),
        ],
      ),
    );
  }
}
