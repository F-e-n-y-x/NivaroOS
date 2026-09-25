import 'package:flutter/material.dart';

import '../theme/design_tokens.dart';
import '../theme/spacing.dart';
import 'section_header.dart';

/// A settings-style group: an optional [SectionHeader], then rows (usually
/// ListTiles) as one rounded block, with an optional footnote underneath.
/// Use it for settings, "More", and any detail screen that is a set of
/// labelled rows.
///
/// In the tonal directions rows are separate segments with a 2dp gap
/// between them instead of divider lines (the Android 16 settings style): the block has large outer
/// corners, the joins have small ones, and each row's ripple stays inside
/// its own segment. The segments are `surfaceContainer` on the `surface`
/// page, the pairing test/ui/theme_contrast_test.dart pins. Rack and
/// Console draw one hairline-edged panel with ruled rows instead
/// (`DesignTokens.ruledGroups`).
///
/// Inside the block the rows keep a 16dp inner padding whatever the screen
/// gutter, so the header - indented by the same 16dp - lines up with the
/// rows' leading icons. Don't mix TileGroups and flat full-width lists on
/// one screen: their headers sit on different edges.
class TileGroup extends StatelessWidget {
  const TileGroup({super.key, this.title, required this.children, this.footer});

  final String? title;
  final List<Widget> children;

  /// Small print under the group - what a setting does, or why it's off.
  final String? footer;

  /// The gap between two rows, showing the page through.
  static const double gap = 2;


  /// Rack and Console: one panel on the card colour with a hairline edge,
  /// rows ruled by inset hairlines - an instrument panel rather than a
  /// stack of tonal segments.
  Widget _ruled(ThemeData theme, DesignTokens tokens, Radius outer) {
    final line = theme.colorScheme.outlineVariant;
    return Material(
      color: tokens.cardColor,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.all(outer), side: BorderSide(color: line)),
      clipBehavior: Clip.antiAlias,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          for (var i = 0; i < children.length; i++) ...[
            if (i > 0) Divider(height: 1, thickness: 1, indent: Space.lg, color: line),
            children[i],
          ],
        ],
      ),
    );
  }

  Widget _segments(ThemeData theme, Radius outer, Radius inner, int last) => Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          for (var i = 0; i <= last; i++) ...[
            if (i > 0) const SizedBox(height: gap),
            Card.filled(
              color: theme.colorScheme.surfaceContainer,
              shape: RoundedRectangleBorder(
                // On true black the segments keep a hairline edge: a
                // near-black fill alone barely registers.
                side: theme.colorScheme.surface == Colors.black ? BorderSide(color: theme.colorScheme.outlineVariant) : BorderSide.none,
                borderRadius: BorderRadius.vertical(
                  top: i == 0 ? outer : inner,
                  bottom: i == last ? outer : inner,
                ),
              ),
              child: children[i],
            ),
          ],
        ],
      );

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final tokens = DesignTokens.of(context);
    final outer = Radius.circular(tokens.groupRadius);
    final inner = Radius.circular(tokens.groupInnerRadius);
    final gutter = Space.gutter(context);
    final last = children.length - 1;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: [
        if (title != null)
          Padding(padding: const EdgeInsetsDirectional.only(start: Space.lg), child: SectionHeader(title: title!))
        else
          const SizedBox(height: Space.sm),
        Padding(
          padding: EdgeInsets.symmetric(horizontal: gutter),
          child: ListTileTheme.merge(
            contentPadding: const EdgeInsets.symmetric(horizontal: Space.lg),
            child: tokens.ruledGroups ? _ruled(theme, tokens, outer) : _segments(theme, outer, inner, last),
          ),
        ),
        if (footer != null)
          Padding(
            padding: EdgeInsets.fromLTRB(gutter + Space.lg, Space.sm, gutter + Space.lg, 0),
            child: Text(footer!, style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
          ),
      ],
    );
  }
}
