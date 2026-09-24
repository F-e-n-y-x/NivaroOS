import 'package:flutter/material.dart';

/// Motion tokens (design brief §3): M3 durations and easing, and one place
/// that honours the system "remove animations" setting.
///
/// Use [Motion.of] rather than the raw durations inside widgets so that a
/// user with animations turned off gets instant changes everywhere:
///
///   AnimatedSwitcher(duration: Motion.of(context).short, ...)
abstract final class Motion {
  /// Small, local changes: a number ticking over, a chip changing state.
  static const Duration short = Durations.short3; // 150ms

  /// Larger changes that move or resize content: an expanding section,
  /// a skeleton handing over to real rows.
  static const Duration medium = Durations.medium2; // 300ms

  /// One half-cycle of a looping "still working" effect (the skeleton
  /// pulse): slow enough to read as calm, not as flicker.
  static const Duration pulse = Durations.extralong4; // 1000ms

  /// Standard easing for anything that starts and ends on screen.
  static const Curve standard = Easing.standard;

  /// Easing for things entering the screen.
  static const Curve enter = Easing.emphasizedDecelerate;

  /// Easing for things leaving the screen.
  static const Curve exit = Easing.emphasizedAccelerate;

  /// Durations adjusted for the current accessibility settings.
  static MotionDurations of(BuildContext context) =>
      MediaQuery.disableAnimationsOf(context) ? MotionDurations.none : MotionDurations.standard;
}

/// The durations a widget should animate with, from [Motion.of].
class MotionDurations {
  const MotionDurations._(this.short, this.medium);

  final Duration short;
  final Duration medium;

  /// True when animations are turned off and widgets should skip
  /// looping effects (such as the skeleton pulse) entirely.
  bool get disabled => short == Duration.zero;

  static const standard = MotionDurations._(Motion.short, Motion.medium);
  static const none = MotionDurations._(Duration.zero, Duration.zero);
}
