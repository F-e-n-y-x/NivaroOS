import 'package:flutter/material.dart';

import '../theme/design_tokens.dart';
import '../theme/spacing.dart';

/// The phone navigation bar as a floating surface (owner request,
/// 2026-09-26): a rounded bar a gutter in from the sides and a gap above
/// the gesture bar, drawn in the style's [NavBarTokens]. Inside it is a
/// stock [NavigationBar], so the destinations, labels, the selected
/// indicator, the 48dp targets and the semantics are Material's own.
///
/// Put it in a [FloatingBarScaffold], which lets the content scroll behind
/// it and tells everything below where the bar ends.
class FloatingNavigationBar extends StatelessWidget {
  const FloatingNavigationBar({super.key, required this.selectedIndex, required this.onDestinationSelected, required this.destinations});

  final int selectedIndex;
  final ValueChanged<int> onDestinationSelected;
  final List<Widget> destinations;

  /// The gap between the bar and the gesture bar (or the screen's edge
  /// when the phone has no bottom inset).
  static double gapBelow(double inset) => inset > 0 ? Space.sm : Space.md;

  @override
  Widget build(BuildContext context) {
    final bar = DesignTokens.of(context).navBar;
    final insets = MediaQuery.paddingOf(context);
    final gutter = Space.gutterFor(MediaQuery.sizeOf(context).width);
    return Padding(
      padding: EdgeInsets.fromLTRB(insets.left + gutter, 0, insets.right + gutter, insets.bottom + gapBelow(insets.bottom)),
      child: Material(
        color: bar.color,
        surfaceTintColor: Colors.transparent,
        elevation: bar.elevation,
        shape: bar.shape,
        clipBehavior: Clip.antiAlias,
        // The insets are the margin's; the bar itself sits clear of them.
        child: MediaQuery.removePadding(
          context: context,
          removeLeft: true,
          removeRight: true,
          removeBottom: true,
          child: LayoutBuilder(
            // Room inside the ends, so the first and last indicator don't
            // touch the bar's corners - but never so much that a
            // destination gets narrower than its 64dp indicator.
            builder: (context, box) => Padding(
              padding: EdgeInsets.symmetric(horizontal: ((box.maxWidth - destinations.length * _indicatorWidth) / 2).clamp(0.0, Space.sm)),
              child: _bar(bar),
            ),
          ),
        ),
      ),
    );
  }

  /// The M3 navigation indicator's width.
  static const double _indicatorWidth = 64;

  Widget _bar(NavBarTokens bar) => NavigationBar(
    height: bar.height,
    backgroundColor: Colors.transparent,
    surfaceTintColor: Colors.transparent,
    shadowColor: Colors.transparent,
    elevation: 0,
    selectedIndex: selectedIndex,
    onDestinationSelected: onDestinationSelected,
    destinations: destinations,
  );
}

/// A [Scaffold] whose [body] scrolls behind a floating [bar]. The body's
/// MediaQuery bottom padding and view padding become the room the bar
/// takes (its height, its gap and the system inset), so every screen
/// below finds its end the way it finds the gesture bar's: `AppScaffold`
/// pads its last sliver with it, a ListView without padding of its own
/// adds it, and a nested Scaffold floats its FAB (the Files paste bar)
/// above it. Snack bars are shown on this Scaffold, above the bar.
///
/// While the keyboard is up, the bar is under it and the body ends at the
/// keyboard, so then there is no room to leave.
class FloatingBarScaffold extends StatelessWidget {
  const FloatingBarScaffold({super.key, required this.body, required this.bar, this.appBar, this.floatingActionButton});

  final Widget body;
  final Widget bar;

  /// For a screen that is its own page (the component gallery); the app's
  /// tabs bring their own Scaffold with its bar and FAB.
  final PreferredSizeWidget? appBar;
  final Widget? floatingActionButton;

  @override
  Widget build(BuildContext context) {
    final keyboard = MediaQuery.viewInsetsOf(context).bottom > 0;
    return Scaffold(
      extendBody: true,
      appBar: appBar,
      floatingActionButton: floatingActionButton,
      bottomNavigationBar: bar,
      body: Builder(
        builder: (context) {
          final data = MediaQuery.of(context);
          final room = keyboard ? 0.0 : data.padding.bottom;
          return MediaQuery(
            data: data.copyWith(
              padding: data.padding.copyWith(bottom: room),
              viewPadding: data.viewPadding.copyWith(bottom: room),
            ),
            child: body,
          );
        },
      ),
    );
  }
}
