import 'package:flutter/material.dart';

import '../theme/design_tokens.dart';
import '../theme/motion.dart';
import '../theme/spacing.dart';

/// The phone navigation bar as a floating surface (owner request,
/// 2026-09-26): a rounded bar a gutter in from the sides and a gap above
/// the gesture bar, drawn in the style's [NavBarTokens].
///
/// The selected destination sits in one indicator that holds both its
/// icon and its label (owner request, 2026-09-29: the stock M3 bar marks
/// only the icon, which read as "half highlighted"). The indicator's
/// corners are concentric with the bar's ([indicatorInset] in from it), in
/// the style's colours from `navigationBarTheme` (`StyleComponents.apply`):
/// ink in Rack, the secondary container in Tonal, the primary container
/// with an accent hairline in Console. Every destination is a full-height
/// target (at least 48dp), announced as a selected / unselected tab, and
/// its label keeps the M3 cap on text scaling so the bar keeps its height.
///
/// Put it in a [FloatingBarScaffold], which lets the content scroll behind
/// it and tells everything below where the bar ends.
class FloatingNavigationBar extends StatelessWidget {
  const FloatingNavigationBar({super.key, required this.selectedIndex, required this.onDestinationSelected, required this.destinations});

  final int selectedIndex;
  final ValueChanged<int> onDestinationSelected;
  final List<NavigationDestination> destinations;

  /// The gap between the bar and the gesture bar (or the screen's edge
  /// when the phone has no bottom inset).
  static double gapBelow(double inset) => inset > 0 ? Space.sm : Space.md;

  /// The indicator's distance from the bar's edges, top, bottom and at the
  /// ends; the styles' indicator corners are the bar's less this.
  static const double indicatorInset = 6;

  /// The label's text scale cap (the M3 navigation bar's), so large text
  /// grows the labels without pushing the bar taller.
  static const double maxLabelTextScale = 1.3;

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
        child: SizedBox(
          height: bar.height,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: indicatorInset - _gap / 2, vertical: indicatorInset),
            child: Row(
              children: [
                for (var i = 0; i < destinations.length; i++)
                  Expanded(
                    child: _Destination(
                      destination: destinations[i],
                      selected: i == selectedIndex,
                      index: i,
                      count: destinations.length,
                      onTap: () => onDestinationSelected(i),
                    ),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  /// The space between two indicators.
  static const double _gap = 4;
}

class _Destination extends StatelessWidget {
  const _Destination({required this.destination, required this.selected, required this.index, required this.count, required this.onTap});

  final NavigationDestination destination;
  final bool selected;
  final int index;
  final int count;
  final VoidCallback onTap;

  static const _states = {WidgetState.selected};
  static const Set<WidgetState> _none = {};

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final s = theme.colorScheme;
    final nav = theme.navigationBarTheme;
    final state = selected ? _states : _none;
    final shape = nav.indicatorShape ?? const StadiumBorder();
    final fill = nav.indicatorColor ?? s.secondaryContainer;
    final iconTheme = nav.iconTheme?.resolve(state) ??
        IconThemeData(size: 24, color: selected ? s.onSecondaryContainer : s.onSurfaceVariant);
    final labelStyle = (nav.labelTextStyle?.resolve(state) ?? theme.textTheme.labelMedium!)
        .copyWith(color: iconTheme.color);
    final duration = Motion.of(context).short;
    final label = MaterialLocalizations.of(context).tabLabel(tabIndex: index + 1, tabCount: count);

    final content = Column(
      mainAxisAlignment: MainAxisAlignment.center,
      mainAxisSize: MainAxisSize.min,
      children: [
        IconTheme.merge(
          data: iconTheme,
          child: AnimatedSwitcher(
            duration: duration,
            child: KeyedSubtree(
              key: ValueKey(selected),
              child: selected ? (destination.selectedIcon ?? destination.icon) : destination.icon,
            ),
          ),
        ),
        const SizedBox(height: 2),
        MediaQuery.withClampedTextScaling(
          maxScaleFactor: FloatingNavigationBar.maxLabelTextScale,
          child: FittedBox(
            fit: BoxFit.scaleDown,
            child: AnimatedDefaultTextStyle(
              duration: duration,
              style: labelStyle,
              maxLines: 1,
              child: Text(destination.label),
            ),
          ),
        ),
      ],
    );

    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: FloatingNavigationBar._gap / 2),
      child: Semantics(
        container: true,
        button: true,
        selected: selected,
        inMutuallyExclusiveGroup: true,
        label: destination.label,
        hint: label,
        excludeSemantics: true,
        onTap: onTap,
        child: Tooltip(
          message: destination.tooltip ?? destination.label,
          excludeFromSemantics: true,
          child: Stack(
            fit: StackFit.expand,
            children: [
              // The indicator: grows out from the centre and fades in
              // behind the icon and the label together.
              TweenAnimationBuilder<double>(
                tween: Tween(end: selected ? 1 : 0),
                duration: duration,
                curve: Motion.standard,
                builder: (context, t, _) => t == 0
                    ? const SizedBox.shrink()
                    : Center(
                        child: FractionallySizedBox(
                          widthFactor: .6 + .4 * t,
                          heightFactor: 1,
                          child: Opacity(
                            opacity: t.clamp(0.0, 1.0),
                            child: DecoratedBox(decoration: ShapeDecoration(color: fill, shape: shape)),
                          ),
                        ),
                      ),
              ),
              Material(
                type: MaterialType.transparency,
                child: InkWell(
                  onTap: onTap,
                  customBorder: shape,
                  overlayColor: nav.overlayColor,
                  child: Center(child: content),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// The shell around a [body] that scrolls behind a floating [bar]. The
/// body's MediaQuery bottom padding and view padding become the room the
/// bar takes (its height, its gap and the system inset), so every screen
/// below finds its end the way it finds the gesture bar's: `AppScaffold`
/// pads its last sliver with it, a ListView without padding of its own
/// adds it, and the tab's Scaffold floats its FAB (the Files paste bar)
/// above it.
///
/// It is deliberately not a [Scaffold] itself: the tab's own Scaffold is
/// then the root one under the app's ScaffoldMessenger, so snack bars show
/// there - above the bar (its room is the view padding they keep clear
/// of) and above the tab's FAB, which the tab's Scaffold knows about and a
/// shell Scaffold could not (the snack bar used to cover the FAB).
///
/// While the keyboard is up, the bar is under it and the body ends at the
/// keyboard, so then there is no room to leave.
class FloatingBarScaffold extends StatelessWidget {
  const FloatingBarScaffold({super.key, required this.body, required this.bar, this.appBar, this.floatingActionButton});

  final Widget body;
  final Widget bar;

  /// For a screen that is its own page (the component gallery); the app's
  /// tabs bring their own Scaffold with its bar and FAB. With either, the
  /// body is put in a Scaffold of its own, inside the bar's room.
  final PreferredSizeWidget? appBar;
  final Widget? floatingActionButton;

  @override
  Widget build(BuildContext context) {
    final data = MediaQuery.of(context);
    final keyboard = data.viewInsets.bottom > 0;
    final room = keyboard ? 0.0 : _room(context, data.padding.bottom);
    return Material(
      color: Theme.of(context).scaffoldBackgroundColor,
      child: Stack(
        children: [
          Positioned.fill(
            child: MediaQuery(
              data: data.copyWith(
                padding: data.padding.copyWith(bottom: room),
                viewPadding: data.viewPadding.copyWith(bottom: room),
              ),
              child: appBar == null && floatingActionButton == null
                  ? body
                  : Scaffold(appBar: appBar, floatingActionButton: floatingActionButton, body: body),
            ),
          ),
          Positioned(left: 0, right: 0, bottom: 0, child: bar),
        ],
      ),
    );
  }

  /// The bar's full height: its own, the gap under it and the system
  /// inset. Anything other than a [FloatingNavigationBar] takes the inset
  /// alone.
  double _room(BuildContext context, double inset) => bar is FloatingNavigationBar
      ? DesignTokens.of(context).navBar.height + FloatingNavigationBar.gapBelow(inset) + inset
      : inset;
}
