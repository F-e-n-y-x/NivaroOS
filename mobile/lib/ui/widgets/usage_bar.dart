import 'package:flutter/material.dart';

import '../theme/app_theme.dart';
import '../theme/motion.dart';
import '../theme/spacing.dart';
import '../theme/status_colors.dart';

/// How full something is - a disk, memory, a quota. A label and percentage
/// above the M3 linear progress bar (the 2024 look the theme opts into: a
/// gap and a stop dot), with an optional detail line ("230 GB of 422 GB")
/// below. Screens use this for every meter, so there is one bar style in
/// the app. The bar turns warning at [warnAt] and error at [criticalAt]; below
/// that it is the primary colour, because "fine" needs no colour of its own.
///
/// When [max] is zero or less the size is unknown: the bar stays empty and
/// the percentage reads "—" rather than a made-up number.
class UsageBar extends StatelessWidget {
  const UsageBar({
    super.key,
    required this.value,
    required this.max,
    required this.label,
    this.detail,
    this.warnAt = 0.8,
    this.criticalAt = 0.9,
  });

  final double value;
  final double max;
  final String label;
  final String? detail;
  final double warnAt;
  final double criticalAt;

  double? get fraction => max > 0 ? (value / max).clamp(0.0, 1.0) : null;

  /// The status the fill colour shows, or null while below [warnAt].
  Status? get status {
    final f = fraction;
    if (f == null) return null;
    if (f >= criticalAt) return Status.error;
    if (f >= warnAt) return Status.warning;
    return null;
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final f = fraction;
    final s = status;
    final fill = s == null ? scheme.primary : StatusColors.toneOf(context, s).color;
    final percent = f == null ? '—' : '${(f * 100).round()}%';

    return Semantics(
      container: true,
      label: label,
      value: [percent, ?detail].join(', '),
      child: ExcludeSemantics(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          mainAxisSize: MainAxisSize.min,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.baseline,
              textBaseline: TextBaseline.alphabetic,
              children: [
                Expanded(child: Text(label, style: theme.textTheme.bodyLarge, maxLines: 1, overflow: TextOverflow.ellipsis)),
                const SizedBox(width: Space.sm),
                // The number is what the row is for, so it outranks the name.
                Text(percent, style: theme.textTheme.titleMedium?.tabular),
              ],
            ),
            const SizedBox(height: Space.sm),
            TweenAnimationBuilder<double>(
              tween: Tween(end: f ?? 0),
              duration: Motion.of(context).medium,
              curve: Motion.standard,
              builder: (context, v, _) => LinearProgressIndicator(
                value: v,
                minHeight: Space.sm,
                borderRadius: BorderRadius.circular(Corners.extraSmall),
                color: fill,
                stopIndicatorColor: fill,
                backgroundColor: scheme.surfaceContainerHighest,
              ),
            ),
            if (detail != null) ...[
              const SizedBox(height: Space.xs),
              Text(detail!, style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant).tabular),
            ],
          ],
        ),
      ),
    );
  }
}
