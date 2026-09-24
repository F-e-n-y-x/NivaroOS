import 'package:flutter/widgets.dart';

/// The 8dp spacing scale (design brief §3). Every padding, gap and margin in
/// a screen comes from here, so rhythm stays consistent without anyone
/// eyeballing numbers.
abstract final class Space {
  static const double xs = 4;
  static const double sm = 8;
  static const double md = 12;
  static const double lg = 16;
  static const double xl = 24;
  static const double xxl = 32;

  /// Width at which the layout switches from the compact (phone) window
  /// class to medium: NavigationRail instead of NavigationBar, wider gutter.
  static const double mediumWidth = 600;

  /// Maximum width for reading views (settings, forms, long lists) on
  /// tablets and landscape, so lines stay readable.
  static const double readingMaxWidth = 840;

  /// The gutter for a pane [width] dp wide: 16dp on phones, 24dp from
  /// [mediumWidth] up.
  static double gutterFor(double width) => width >= mediumWidth ? xl : lg;

  /// Horizontal screen gutter for [context]. Inside an `AppScaffold` it is
  /// measured from the pane the screen actually gets (the window minus the
  /// NavigationRail), so section headers, list rows and skeletons all sit
  /// on the same edge; elsewhere it falls back to the window width.
  static double gutter(BuildContext context) =>
      ContentGutter.maybeOf(context) ?? gutterFor(MediaQuery.sizeOf(context).width);
}

/// Carries the gutter `AppScaffold` measured for its pane down to the
/// widgets that read [Space.gutter].
class ContentGutter extends InheritedWidget {
  const ContentGutter({super.key, required this.gutter, required super.child});

  final double gutter;

  static double? maybeOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<ContentGutter>()?.gutter;

  @override
  bool updateShouldNotify(ContentGutter oldWidget) => gutter != oldWidget.gutter;
}

/// Corner radii for the M3 shape scale. Components already use these by
/// default; reach for them only when building something M3 has no widget
/// for (the usage bar, skeleton rows, grouped list segments).
abstract final class Corners {
  static const double extraSmall = 4;
  static const double small = 8;
  static const double medium = 12;
  static const double large = 16;
  static const double extraLarge = 28;
}
