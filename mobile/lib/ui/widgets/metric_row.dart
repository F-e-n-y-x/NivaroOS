import 'package:flutter/material.dart';

import '../theme/app_theme.dart';
import '../theme/motion.dart';
import '../theme/spacing.dart';

/// One stat as a list row: icon, label, optional supporting line, and the
/// value on the right in tabular figures with a smaller unit ("48 °C",
/// "1.2 MB/s", "12%"). Value changes
/// cross-fade in place instead of jumping.
///
/// Pass "—" as [value] when the number is unknown, and say why in
/// [supporting].
class MetricRow extends StatelessWidget {
  const MetricRow({
    super.key,
    required this.icon,
    required this.label,
    required this.value,
    this.unit,
    this.supporting,
    this.onTap,
  });

  final IconData icon;
  final String label;
  final String value;
  final String? unit;
  final String? supporting;

  /// Makes the row open a detail view.
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final valueStyle = theme.textTheme.titleMedium?.tabular;
    final unitStyle = theme.textTheme.labelMedium?.copyWith(color: scheme.onSurfaceVariant);
    // A thin space before a unit ("48 °C"), none before a percent sign.
    final unitText = switch (unit) {
      null => null,
      '%' => '%',
      final u => '\u2009$u',
    };
    final spoken = switch (unit) {
      null => value,
      '%' => '$value%',
      final u => '$value $u',
    };

    return MergeSemantics(
      child: ListTile(
        leading: Icon(icon, color: scheme.onSurfaceVariant),
        title: Text(label),
        subtitle: supporting == null ? null : Text(supporting!),
        onTap: onTap,
        trailing: Semantics(
          label: spoken,
          child: ExcludeSemantics(
            child: AnimatedSwitcher(
              duration: Motion.of(context).short,
              // Old and new value share the right edge while they cross-fade,
              // rather than the default centring, which would shift them
              // sideways when their widths differ.
              layoutBuilder: (current, previous) => Stack(
                alignment: AlignmentDirectional.centerEnd,
                children: [...previous, ?current],
              ),
              child: Text.rich(
                TextSpan(children: [
                  TextSpan(text: value, style: valueStyle),
                  if (unitText != null) TextSpan(text: unitText, style: unitStyle),
                ]),
                key: ValueKey(spoken),
              ),
            ),
          ),
        ),
        minLeadingWidth: Space.xl,
      ),
    );
  }
}
