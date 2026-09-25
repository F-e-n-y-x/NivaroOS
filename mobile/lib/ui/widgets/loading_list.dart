import 'package:flutter/material.dart';

import '../theme/motion.dart';
import '../theme/spacing.dart';

/// What sits at the start of the rows a skeleton stands in for, so the
/// placeholder has the same shape as the list that replaces it.
enum SkeletonLeading {
  /// No leading element.
  none,

  /// A 24dp icon (settings rows, logs, folders).
  icon,

  /// A 40dp circle (people, devices, server profiles).
  avatar,

  /// A 56dp rounded square (app icons, media thumbnails).
  thumbnail,
}

/// Skeleton rows shown while a list loads, laid out like the ListTiles that
/// replace them so nothing jumps when the data arrives. The rows pulse
/// gently (a fade, no shimmer) and hold still when animations are off.
///
/// This is a box scrollable, for `AppScaffold(body:)`. In
/// `AppScaffold.slivers` use [SliverLoadingList]. For a screen whose loaded
/// layout is not a plain list, build the skeleton from [SkeletonBox]es
/// inside a [SkeletonPulse] instead.
class LoadingList extends StatelessWidget {
  const LoadingList({
    super.key,
    this.rows = 6,
    this.leading = SkeletonLeading.icon,
    this.subtitle = true,
    this.trailing = false,
  });

  final int rows;

  /// The real rows' leading element.
  final SkeletonLeading leading;

  /// Whether the real rows have a second line.
  final bool subtitle;

  /// Whether the real rows have a short trailing value ("48 °C", "2.1 GB").
  final bool trailing;

  @override
  Widget build(BuildContext context) {
    return _LoadingSemantics(
      child: SkeletonPulse(
        child: ListView.builder(
          // Fixed padding rather than the automatic system-inset padding:
          // it is a placeholder, and must sit exactly where the real rows
          // will.
          padding: const EdgeInsets.symmetric(vertical: Space.sm),
          physics: const AlwaysScrollableScrollPhysics(),
          itemCount: rows,
          itemBuilder: (context, i) => SkeletonRow(leading: leading, subtitle: subtitle, trailing: trailing, index: i),
        ),
      ),
    );
  }
}

/// [LoadingList] as a sliver, for `AppScaffold.slivers` and any other
/// CustomScrollView.
class SliverLoadingList extends StatelessWidget {
  const SliverLoadingList({
    super.key,
    this.rows = 6,
    this.leading = SkeletonLeading.icon,
    this.subtitle = true,
    this.trailing = false,
  });

  final int rows;
  final SkeletonLeading leading;
  final bool subtitle;
  final bool trailing;

  @override
  Widget build(BuildContext context) {
    return SliverSemantics(
      label: 'Loading',
      liveRegion: true,
      sliver: SliverPadding(
        padding: const EdgeInsets.symmetric(vertical: Space.sm),
        sliver: SkeletonPulse.sliver(
          child: SliverList.builder(
            itemCount: rows,
            itemBuilder: (context, i) => ExcludeSemantics(
              child: SkeletonRow(leading: leading, subtitle: subtitle, trailing: trailing, index: i),
            ),
          ),
        ),
      ),
    );
  }
}

/// "Loading", announced once, instead of TalkBack reading every box.
class _LoadingSemantics extends StatelessWidget {
  const _LoadingSemantics({required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) =>
      Semantics(label: 'Loading', liveRegion: true, child: ExcludeSemantics(child: child));
}

/// One skeleton row, shaped like the ListTile it stands in for. Lists of
/// them come from [LoadingList] / [SliverLoadingList]; use it on its own
/// for rows inside a TileGroup, so the placeholder sits in the same
/// rounded segments as the real rows.
class SkeletonRow extends StatelessWidget {
  const SkeletonRow({super.key, this.leading = SkeletonLeading.icon, this.subtitle = true, this.trailing = false, this.index = 0});

  /// Placeholder rows for a TileGroup's children: [rows] of them, pulsing,
  /// announced as "Loading".
  static List<Widget> group({int rows = 3, SkeletonLeading leading = SkeletonLeading.icon, bool subtitle = true, bool trailing = false}) => [
        for (var i = 0; i < rows; i++)
          _LoadingSemantics(
            child: SkeletonPulse(child: SkeletonRow(leading: leading, subtitle: subtitle, trailing: trailing, index: i)),
          ),
      ];

  final SkeletonLeading leading;
  final bool subtitle;
  final bool trailing;
  final int index;

  // Vary the line lengths a little so it reads as a list of different
  // items, not a barcode.
  static const _titleFactors = [0.62, 0.48, 0.7, 0.54, 0.4, 0.66];

  @override
  Widget build(BuildContext context) {
    final scaler = MediaQuery.textScalerOf(context);
    final theme = Theme.of(context).textTheme;
    // Each line of the skeleton is as tall as the line of text it stands
    // for, with a slimmer bar centred in it, all following the text scale,
    // so a row is as tall as the ListTile that replaces it at any size.
    final titleLine = scaler.scale(theme.bodyLarge?.fontSize ?? 16) * (theme.bodyLarge?.height ?? 1.5);
    final subtitleLine = scaler.scale(theme.bodyMedium?.fontSize ?? 14) * (theme.bodyMedium?.height ?? 1.43);
    final titleBar = scaler.scale(14);
    final subtitleBar = scaler.scale(12);
    final titleFactor = _titleFactors[index % _titleFactors.length];

    final Widget? lead = switch (leading) {
      SkeletonLeading.none => null,
      SkeletonLeading.icon => const SkeletonBox(width: 24, height: 24, radius: Corners.extraSmall),
      SkeletonLeading.avatar => const SkeletonBox(width: 40, height: 40, radius: 20),
      SkeletonLeading.thumbnail => const SkeletonBox(width: 56, height: 56, radius: Corners.medium),
    };

    return ConstrainedBox(
      // Same minimum heights as ListTile: one line 56, two lines 72, and
      // 88 when a 56dp thumbnail leads.
      constraints: BoxConstraints(minHeight: leading == SkeletonLeading.thumbnail ? 88 : (subtitle ? 72 : 56)),
      child: Padding(
        // The ListTile padding in force here: the screen gutter in a list,
        // 16dp inside a TileGroup.
        padding: (ListTileTheme.of(context).contentPadding ?? EdgeInsets.symmetric(horizontal: Space.gutter(context)))
            .add(const EdgeInsets.symmetric(vertical: Space.sm)),
        child: Row(
          children: [
            if (lead != null) ...[lead, const SizedBox(width: Space.lg)],
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  SizedBox(
                    height: titleLine,
                    child: Align(
                      alignment: AlignmentDirectional.centerStart,
                      child: FractionallySizedBox(widthFactor: titleFactor, child: SkeletonBox(height: titleBar)),
                    ),
                  ),
                  if (subtitle)
                    SizedBox(
                      height: subtitleLine,
                      child: Align(
                        alignment: AlignmentDirectional.centerStart,
                        child: FractionallySizedBox(widthFactor: titleFactor * 0.7, child: SkeletonBox(height: subtitleBar)),
                      ),
                    ),
                ],
              ),
            ),
            if (trailing) ...[const SizedBox(width: Space.lg), SkeletonBox(width: scaler.scale(40), height: titleBar)],
          ],
        ),
      ),
    );
  }
}

/// A placeholder block in the colour of a filled but empty surface: the
/// highest container tone in dark, and the dim surface tone in light,
/// where the highest container is too close to the page to read.
class SkeletonBox extends StatelessWidget {
  const SkeletonBox({super.key, this.width, required this.height, this.radius = Corners.extraSmall});

  final double? width;
  final double height;
  final double radius;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      width: width,
      height: height,
      decoration: BoxDecoration(
        color: scheme.brightness == Brightness.light ? scheme.surfaceDim : scheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(radius),
      ),
    );
  }
}

/// Fades its child between 60% and 100% opacity while mounted, so skeletons
/// read as "working" rather than "broken" without dropping out of sight on
/// a light background. Static when animations are off.
class SkeletonPulse extends StatefulWidget {
  const SkeletonPulse({super.key, required Widget this.child}) : sliver = null;

  /// Pulses a sliver instead of a box.
  const SkeletonPulse.sliver({super.key, required Widget child})
      : sliver = child,
        child = null;

  final Widget? child;
  final Widget? sliver;

  /// The dimmest point of the pulse.
  static const double lowestOpacity = 0.6;

  @override
  State<SkeletonPulse> createState() => _SkeletonPulseState();
}

class _SkeletonPulseState extends State<SkeletonPulse> with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: Motion.pulse,
    lowerBound: SkeletonPulse.lowestOpacity,
  );

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (Motion.of(context).disabled) {
      _controller
        ..stop()
        ..value = 1;
    } else if (!_controller.isAnimating) {
      _controller.repeat(reverse: true);
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final sliver = widget.sliver;
    return sliver != null
        ? SliverFadeTransition(opacity: _controller, sliver: sliver)
        : FadeTransition(opacity: _controller, child: widget.child);
  }
}
