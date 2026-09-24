import 'package:flutter/material.dart';

import '../theme/motion.dart';
import '../theme/spacing.dart';
import 'relative_time.dart';

/// The frame of a screen (design brief §4): top app bar, optional
/// pull-to-refresh, a slot for the offline banner, safe areas, optional FAB,
/// and a reading width cap on tablets.
///
/// Two shapes:
/// - `AppScaffold(body: ...)` - a small app bar over any body. With
///   [onRefresh], [body] must be scrollable (a ListView, or an
///   EmptyState/ErrorState, which always are).
/// - `AppScaffold.slivers(slivers: [...])` - a medium app bar that
///   collapses as the content scrolls, for top-level screens and long
///   pages. Put lists in as SliverList and friends, and the states as
///   `SliverLoadingList` / `EmptyState(sliver: true)` /
///   `ErrorState(sliver: true)`.
///
/// Both measure the pane they get (the window minus a NavigationRail) and
/// hand its gutter to [Space.gutter] and the ListTile padding, so the
/// title, headers, rows and skeletons share one left edge on phones and
/// tablets alike.
///
/// Scrollables lay out edge to edge; the bottom system inset is left to
/// them (ListView adds it by itself; the sliver form adds it after the last
/// sliver), so content scrolls under the gesture bar instead of stopping
/// short of it.
class AppScaffold extends StatelessWidget {
  const AppScaffold({
    super.key,
    required this.title,
    required Widget this.body,
    this.actions,
    this.leading,
    this.bottom,
    this.appBar,
    this.onRefresh,
    this.banner,
    this.floatingActionButton,
    this.floatingActionButtonLocation,
    this.maxContentWidth = Space.readingMaxWidth,
  })  : slivers = null,
        collapsingTitle = false;

  const AppScaffold.slivers({
    super.key,
    required this.title,
    required List<Widget> this.slivers,
    this.actions,
    this.leading,
    this.bottom,
    this.appBar,
    this.onRefresh,
    this.banner,
    this.floatingActionButton,
    this.floatingActionButtonLocation,
    this.maxContentWidth = Space.readingMaxWidth,
    this.collapsingTitle = true,
  }) : body = null;

  /// Heights of the M3 small app bar and of the medium one while expanded,
  /// where the refresh indicator has to start so it doesn't cover the
  /// title. (M3 bars are 64dp, not Flutter's older kToolbarHeight of 56.)
  static const double _smallBarHeight = 64;
  static const double _mediumBarExpandedHeight = 112;

  final String title;
  final Widget? body;
  final List<Widget>? slivers;
  final List<Widget>? actions;
  final Widget? leading;

  /// Pinned under the title: a TabBar, or a search field.
  final PreferredSizeWidget? bottom;

  /// Replaces the whole top app bar while set - a contextual bar for
  /// selection mode ("3 selected", with its own actions). Pass null to go
  /// back to the normal bar.
  final PreferredSizeWidget? appBar;

  /// Enables pull-to-refresh. Reload the data and complete when done.
  final Future<void> Function()? onRefresh;

  /// Shown under the app bar when set - normally an [OfflineBanner]. It
  /// animates in and out as it appears and disappears, and stays pinned
  /// while the content scrolls.
  final Widget? banner;
  final Widget? floatingActionButton;
  final FloatingActionButtonLocation? floatingActionButtonLocation;

  /// Content wider than this is centred. Pass null for full-width layouts
  /// (list-detail panes, the file grid).
  final double? maxContentWidth;

  /// Sliver form only: a medium app bar whose large title collapses into
  /// the bar on scroll. False gives a small pinned bar.
  final bool collapsingTitle;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final width = constraints.maxWidth;
        final gutter = Space.gutterFor(width);
        final max = maxContentWidth;
        final inset = max == null ? 0.0 : ((width - max) / 2).clamp(0.0, double.infinity);
        // Without a leading button the title starts where the content does
        // - on the gutter, inside the reading column on tablets - rather
        // than 16dp from the window edge. The bars place it 16dp in, so the
        // difference goes on the title itself (the medium bar's large title
        // has no spacing setting of its own).
        final hasLeading = leading != null || (ModalRoute.of(context)?.impliesAppBarDismissal ?? false);
        final titleShift = hasLeading ? 0.0 : inset + gutter - Space.lg;
        final titleWidget = Padding(padding: EdgeInsetsDirectional.only(start: titleShift), child: Text(title));

        return Scaffold(
          appBar: appBar ??
              (slivers == null ? AppBar(title: titleWidget, leading: leading, actions: actions, bottom: bottom) : null),
          floatingActionButton: floatingActionButton,
          floatingActionButtonLocation: floatingActionButtonLocation,
          body: ContentGutter(
            gutter: gutter,
            child: ListTileTheme.merge(
              contentPadding: EdgeInsets.symmetric(horizontal: gutter),
              child: SafeArea(
                top: false,
                bottom: false,
                child: slivers == null ? _boxBody(context) : _sliverBody(context, titleWidget),
              ),
            ),
          ),
        );
      },
    );
  }

  Widget _banner(BuildContext context) => AnimatedSize(
        duration: Motion.of(context).medium,
        curve: Motion.standard,
        alignment: Alignment.topCenter,
        child: banner ?? const SizedBox(width: double.infinity),
      );

  Widget _boxBody(BuildContext context) {
    Widget content = body!;
    if (maxContentWidth != null) {
      // Top-aligned, so a body that doesn't fill the height starts under
      // the app bar instead of floating in the middle.
      content = Align(
        alignment: Alignment.topCenter,
        child: ConstrainedBox(constraints: BoxConstraints(maxWidth: maxContentWidth!), child: content),
      );
    }
    if (onRefresh != null) content = RefreshIndicator(onRefresh: onRefresh!, child: content);
    return Column(children: [_banner(context), Expanded(child: content)]);
  }

  Widget _sliverBody(BuildContext context, Widget titleWidget) {
    final padding = MediaQuery.paddingOf(context);
    final ownBar = appBar == null;
    final Widget bar = collapsingTitle
        ? SliverAppBar.medium(title: titleWidget, leading: leading, actions: actions, bottom: bottom)
        : SliverAppBar(pinned: true, title: titleWidget, leading: leading, actions: actions, bottom: bottom);

    Widget scroll = LayoutBuilder(
      builder: (context, constraints) {
        final max = maxContentWidth;
        // The width cap is padding inside the scroll view rather than a box
        // around it, so drags in the side margins still scroll.
        final inset = max == null ? 0.0 : ((constraints.maxWidth - max) / 2).clamp(0.0, double.infinity);
        return CustomScrollView(
          physics: const AlwaysScrollableScrollPhysics(),
          slivers: [
            if (ownBar) bar,
            PinnedHeaderSliver(child: _banner(context)),
            SliverPadding(
              padding: EdgeInsets.symmetric(horizontal: inset),
              sliver: SliverMainAxisGroup(slivers: slivers!),
            ),
            SliverPadding(padding: EdgeInsets.only(bottom: padding.bottom + Space.lg)),
          ],
        );
      },
    );
    if (onRefresh != null) {
      final barHeight = (collapsingTitle ? _mediumBarExpandedHeight : _smallBarHeight) + (bottom?.preferredSize.height ?? 0);
      scroll = RefreshIndicator(
        onRefresh: onRefresh!,
        // Start the indicator below the expanded app bar rather than on top
        // of its title. A replacement [appBar] sits outside the scroll view,
        // so then the scroll view's own top is the edge.
        edgeOffset: ownBar ? padding.top + barHeight : 0,
        child: scroll,
      );
    }
    return scroll;
  }
}

/// "Offline · last updated 5 min ago" (or "Can't reach the server" when
/// nothing is cached), with Retry. Goes in [AppScaffold.banner] while
/// requests fail and cached data is on screen.
class OfflineBanner extends StatelessWidget {
  const OfflineBanner({super.key, this.lastUpdated, this.onRetry});

  /// When the data on screen was fetched; null if there is none.
  final DateTime? lastUpdated;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final updated = lastUpdated;
    final text = updated == null ? "Can't reach the server" : 'Offline · last updated ${_sentenceTail(formatRelative(updated))}';
    return Semantics(
      liveRegion: true,
      child: Material(
        color: scheme.surfaceContainerHighest,
        child: Padding(
          padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.xs, Space.sm, Space.xs),
          child: ConstrainedBox(
            constraints: const BoxConstraints(minHeight: 48),
            child: Row(
              children: [
                Icon(Icons.cloud_off_outlined, size: 20, color: scheme.onSurfaceVariant),
                const SizedBox(width: Space.md),
                Expanded(child: Text(text, style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurface))),
                if (onRetry != null) TextButton(onPressed: onRetry, child: const Text('Retry')),
              ],
            ),
          ),
        ),
      ),
    );
  }

  // formatRelative starts "Just now" / "Yesterday" with a capital for use
  // on its own; mid-sentence they read lower case. Dates stay as they are.
  static String _sentenceTail(String relative) =>
      relative == 'Just now' || relative == 'Yesterday' ? relative.toLowerCase() : relative;
}
