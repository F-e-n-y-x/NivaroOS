import 'package:flutter/material.dart';

import '../theme/design_tokens.dart';
import '../theme/scaled_icons.dart';
import '../theme/spacing.dart';
import '../theme/status_colors.dart';

/// A small tonal label for a state - "Running", "Stopped", "Update
/// available". Always icon plus text, so the state never relies on colour
/// alone. It is not a button; put actions next to it, not on it.
class StatusChip extends StatelessWidget {
  const StatusChip({super.key, required this.label, required this.status, this.icon});

  final String label;
  final Status status;

  /// Overrides the default icon for [status].
  final IconData? icon;

  static IconData defaultIcon(Status status) => switch (status) {
        Status.success => Icons.check_circle_outline,
        Status.warning => Icons.warning_amber_outlined,
        Status.error => Icons.error_outline,
        Status.info => Icons.info_outline,
        Status.neutral => Icons.radio_button_unchecked,
      };

  @override
  Widget build(BuildContext context) {
    final tone = StatusColors.toneOf(context, status);
    final scheme = Theme.of(context).colorScheme;
    // Rack and Console: an outline with the icon in the status colour and
    // the word in ink, so a list of states doesn't turn into a row of
    // coloured pills.
    final outlined = DesignTokens.of(context).outlinedChips;
    final ink = outlined ? scheme.onSurface : tone.onContainer;
    final iconColor = outlined ? (status == Status.neutral ? scheme.onSurfaceVariant : tone.color) : tone.onContainer;
    final style = Theme.of(context).textTheme.labelMedium?.copyWith(color: ink);
    // The icon grows with the text (up to 1.4×), so at 200% text it
    // doesn't shrink to a speck next to the label.
    final iconSize = 16 * iconScaleFor(context);
    return Semantics(
      container: true,
      label: label,
      child: ExcludeSemantics(
        child: DecoratedBox(
          decoration: BoxDecoration(
            color: outlined ? null : tone.container,
            border: outlined ? Border.all(color: scheme.outline.withValues(alpha: .55)) : null,
            borderRadius: BorderRadius.circular(DesignTokens.of(context).radii.sm),
          ),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: Space.sm, vertical: Space.xs),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(icon ?? defaultIcon(status), size: iconSize, color: iconColor),
                const SizedBox(width: Space.xs),
                Flexible(child: Text(label, style: style, maxLines: 1, overflow: TextOverflow.ellipsis)),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
