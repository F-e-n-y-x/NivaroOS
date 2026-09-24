import 'package:flutter/material.dart';

/// What a status colour means. Error comes from the colour scheme; the rest
/// come from [StatusColors].
enum Status { success, warning, error, info, neutral }

/// One status's colour roles, shaped like the scheme's own
/// primary/onPrimary/primaryContainer/onPrimaryContainer quartet.
@immutable
class StatusTone {
  const StatusTone({
    required this.color,
    required this.onColor,
    required this.container,
    required this.onContainer,
  });

  /// Text and icons on a normal surface, and solid fills.
  final Color color;

  /// Text and icons on [color].
  final Color onColor;

  /// Low-emphasis tonal fill (chips, banners).
  final Color container;

  /// Text and icons on [container].
  final Color onContainer;

  static StatusTone lerp(StatusTone a, StatusTone b, double t) => StatusTone(
        color: Color.lerp(a.color, b.color, t)!,
        onColor: Color.lerp(a.onColor, b.onColor, t)!,
        container: Color.lerp(a.container, b.container, t)!,
        onContainer: Color.lerp(a.onContainer, b.onContainer, t)!,
      );
}

/// Success, warning and info colours for both themes (design brief §3).
///
/// Values are the M3 tonal-palette tones for each hue - colour 40 / on 100 /
/// container 90 / on-container 10-30 in light, 80 / 20 / 30 / 90 in dark -
/// taken from the same algorithm `ColorScheme.fromSeed` uses, seeded with
/// the web UI's green #16A34A, amber #F59E0B and sky #0EA5E9. They sit
/// next to the scheme's error colour, which is built the same way.
/// test/ui/theme_contrast_test.dart checks every pair (≥4.5:1 text, ≥3:1
/// icons) in both themes; change a value only with that test passing.
///
/// Status colours are for status only: a stopped app, a full disk, a
/// finished backup. Never use them for decoration.
@immutable
class StatusColors extends ThemeExtension<StatusColors> {
  const StatusColors({required this.success, required this.warning, required this.info});

  final StatusTone success;
  final StatusTone warning;
  final StatusTone info;

  static const light = StatusColors(
    success: StatusTone(
      color: Color(0xFF006E2D),
      onColor: Color(0xFFFFFFFF),
      container: Color(0xFFB7F1BA),
      onContainer: Color(0xFF1C5128),
    ),
    warning: StatusTone(
      color: Color(0xFF855300),
      onColor: Color(0xFFFFFFFF),
      container: Color(0xFFFFDDB8),
      onContainer: Color(0xFF653E00),
    ),
    info: StatusTone(
      color: Color(0xFF006591),
      onColor: Color(0xFFFFFFFF),
      container: Color(0xFFC9E6FF),
      onContainer: Color(0xFF004C6E),
    ),
  );

  static const dark = StatusColors(
    success: StatusTone(
      color: Color(0xFF62DF7D),
      onColor: Color(0xFF003914),
      container: Color(0xFF1C5128),
      onContainer: Color(0xFFB7F1BA),
    ),
    warning: StatusTone(
      color: Color(0xFFFFB95F),
      onColor: Color(0xFF472A00),
      container: Color(0xFF653E00),
      onContainer: Color(0xFFFFDDB8),
    ),
    info: StatusTone(
      color: Color(0xFF89CEFF),
      onColor: Color(0xFF00344D),
      container: Color(0xFF004C6E),
      onContainer: Color(0xFFC9E6FF),
    ),
  );

  /// The extension from the nearest theme. Every theme built by `AppTheme`
  /// carries one; a bare `ThemeData()` (some tests) falls back to the
  /// values matching its brightness.
  static StatusColors of(BuildContext context) {
    final theme = Theme.of(context);
    return theme.extension<StatusColors>() ?? (theme.brightness == Brightness.dark ? dark : light);
  }

  /// The colour roles for [status]. Error and neutral map onto the scheme.
  static StatusTone toneOf(BuildContext context, Status status) {
    final scheme = Theme.of(context).colorScheme;
    return of(context).resolve(status, scheme);
  }

  StatusTone resolve(Status status, ColorScheme scheme) => switch (status) {
        Status.success => success,
        Status.warning => warning,
        Status.info => info,
        Status.error => StatusTone(
            color: scheme.error,
            onColor: scheme.onError,
            container: scheme.errorContainer,
            onContainer: scheme.onErrorContainer,
          ),
        Status.neutral => StatusTone(
            color: scheme.onSurfaceVariant,
            onColor: scheme.surface,
            container: scheme.surfaceContainerHighest,
            onContainer: scheme.onSurfaceVariant,
          ),
      };

  @override
  StatusColors copyWith({StatusTone? success, StatusTone? warning, StatusTone? info}) => StatusColors(
        success: success ?? this.success,
        warning: warning ?? this.warning,
        info: info ?? this.info,
      );

  @override
  StatusColors lerp(StatusColors? other, double t) {
    if (other == null) return this;
    return StatusColors(
      success: StatusTone.lerp(success, other.success, t),
      warning: StatusTone.lerp(warning, other.warning, t),
      info: StatusTone.lerp(info, other.info, t),
    );
  }
}
