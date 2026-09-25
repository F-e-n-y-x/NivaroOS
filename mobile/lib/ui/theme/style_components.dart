import 'package:flutter/cupertino.dart' show CupertinoPageTransitionsBuilder;
import 'package:flutter/material.dart';

import 'appearance.dart';
import 'design_tokens.dart';

/// How a style draws the stock Material components (owner request,
/// 2026-09-26: "a style styles the whole app"). `AppTheme` hands every
/// Rack, Tonal and Console theme through [StyleComponents.apply], so an
/// app bar, a switch, a text field or a menu on any screen takes the
/// style's corners, edges, type, density and fills with no code of its
/// own. v2 (the 1.3 reference look) never comes here.
///
/// The three, in one line each:
/// - **Rack**: paper and ink. Precise corners (8-14), hairline edges on
///   anything that floats, ink for what you press (filled buttons, the
///   selected chip or segment, the FAB), the accent for what is on or live
///   (switches, checks, sliders, progress, focus, links). Standard density.
/// - **Tonal**: Material 3 Expressive. Stadium buttons, large soft corners
///   (12-28), tonal fills with no edges, filled text fields, a check in the
///   switch thumb, emphasised labels.
/// - **Console**: graphite and signal. Tight corners (2-8), hairlines
///   everywhere including under the app bar, the accent as a line rather
///   than a fill, mono for values and actions, one step denser.
///
/// Every colour pair these introduce is checked in every style × mode ×
/// accent by test/ui/theme_contrast_test.dart.
abstract final class StyleComponents {
  /// The corner scale of [d].
  static Radii radii(DesignDirection d) => switch (d) {
        DesignDirection.v2 => Radii.material,
        DesignDirection.rack => const Radii(xs: 4, sm: 8, md: 12, lg: 14, xl: 20),
        DesignDirection.tonal => const Radii(xs: 8, sm: 12, md: 16, lg: 24, xl: 28),
        DesignDirection.console => const Radii(xs: 2, sm: 6, md: 8, lg: 10, xl: 14),
      };

  /// The component themes of [d] on [base], whose [ThemeData.textTheme]
  /// carries the style's faces; [text] is that theme with real sizes.
  static ThemeData apply(ThemeData base, DesignDirection d, TextTheme text, {required bool black, required String? mono}) {
    assert(d != DesignDirection.v2, 'v2 keeps the Material defaults');
    final s = base.colorScheme;
    final r = radii(d);
    final rack = d == DesignDirection.rack;
    final tonal = d == DesignDirection.tonal;
    final console = d == DesignDirection.console;
    // Rack and Console are flat (no scroll-under tint, no elevation tint),
    // and so is every style on true black, where a tint turns navy.
    final flat = !tonal || black;

    RoundedRectangleBorder rounded(double radius, [BorderSide side = BorderSide.none]) =>
        RoundedRectangleBorder(borderRadius: BorderRadius.circular(radius), side: side);
    final hairline = BorderSide(color: s.outlineVariant);
    // Floating surfaces (dialogs, sheets, menus, snack bars) get a hairline
    // in Rack and Console; Tonal separates them by tone alone.
    final edge = tonal ? BorderSide.none : hairline;
    TextStyle monoOf(TextStyle? t) => t!.copyWith(fontFamily: mono, fontFamilyFallback: const ['Roboto']);

    final OutlinedBorder buttonShape = tonal ? const StadiumBorder() : rounded(r.md);
    // Button labels: Console's actions are mono, Tonal's emphasised.
    final buttonLabel = console
        ? monoOf(text.labelLarge).copyWith(fontWeight: FontWeight.w500, letterSpacing: .2)
        : text.labelLarge!.copyWith(fontWeight: tonal ? FontWeight.w600 : FontWeight.w500);

    // The primary button is part of each style's identity: ink in Rack,
    // tonal in Tonal, an accent outline with a mono label in Console.
    final filled = switch (d) {
      DesignDirection.rack => FilledButton.styleFrom(
          backgroundColor: s.onSurface,
          foregroundColor: s.surface,
          disabledBackgroundColor: s.onSurface.withValues(alpha: .12),
          disabledForegroundColor: s.onSurface.withValues(alpha: .38),
          textStyle: buttonLabel,
          shape: buttonShape,
        ),
      DesignDirection.tonal => FilledButton.styleFrom(
          backgroundColor: s.primaryContainer,
          foregroundColor: s.onPrimaryContainer,
          textStyle: buttonLabel,
        ),
      _ => FilledButton.styleFrom(
          backgroundColor: Colors.transparent,
          foregroundColor: s.primary,
          side: BorderSide(color: s.primary, width: 1.5),
          textStyle: buttonLabel,
          shape: buttonShape,
        ),
    };
    // Outlined buttons are the quiet alternative: ink on a hairline in Rack
    // and Console (the accent outline is Console's primary), the M3 accent
    // label in Tonal.
    final outlined = OutlinedButton.styleFrom(
      foregroundColor: tonal ? null : s.onSurface,
      side: tonal ? null : BorderSide(color: s.outline),
      textStyle: buttonLabel,
      shape: buttonShape,
    );
    final textButton = TextButton.styleFrom(textStyle: buttonLabel, shape: buttonShape);
    final elevated = ElevatedButton.styleFrom(
      textStyle: buttonLabel,
      shape: buttonShape,
      elevation: flat ? 0 : null,
      backgroundColor: flat ? s.surfaceContainerHigh : null,
      side: flat && !tonal ? hairline : null,
    );
    final iconButton = IconButton.styleFrom(shape: console ? rounded(r.sm) : null);

    // Selected chips and segments: ink in Rack (a pressed key), the
    // secondary container in Tonal, the primary container in Console. The
    // label and check follow the fill; unselected ones stay on the page.
    final (Color selectedFill, Color onSelected) = switch (d) {
      DesignDirection.rack => (s.onSurface, s.surface),
      DesignDirection.console => (s.primaryContainer, s.onPrimaryContainer),
      _ => (s.secondaryContainer, s.onSecondaryContainer),
    };
    Color selectable(Set<WidgetState> st, Color unselected) => st.contains(WidgetState.selected) ? onSelected : unselected;

    // Toggles are the accent's: on is the primary colour with an onPrimary
    // thumb in every style (Monochrome makes that ink). Off differs: Rack
    // a paper track with an outline, Console a graphite track with a
    // hairline, Tonal the M3 default.
    final switchTheme = tonal
        ? SwitchThemeData(
            thumbIcon: WidgetStateProperty.resolveWith((st) => st.contains(WidgetState.selected) ? const Icon(Icons.check) : null),
          )
        : SwitchThemeData(
            thumbColor: WidgetStateProperty.resolveWith((st) {
              if (st.contains(WidgetState.disabled)) return s.onSurface.withValues(alpha: .38);
              return st.contains(WidgetState.selected) ? s.onPrimary : (console ? s.onSurfaceVariant : s.outline);
            }),
            trackColor: WidgetStateProperty.resolveWith((st) {
              if (st.contains(WidgetState.disabled)) return s.onSurface.withValues(alpha: .12);
              if (st.contains(WidgetState.selected)) return s.primary;
              return rack ? s.surfaceContainerLowest : s.surfaceContainerHighest;
            }),
            trackOutlineColor: WidgetStateProperty.resolveWith((st) {
              if (st.contains(WidgetState.selected)) return Colors.transparent;
              return st.contains(WidgetState.disabled) ? s.onSurface.withValues(alpha: .12) : s.outline;
            }),
            trackOutlineWidth: const WidgetStatePropertyAll(1),
          );
    Color? toggleFill(Set<WidgetState> st) {
      if (st.contains(WidgetState.disabled)) return st.contains(WidgetState.selected) ? s.onSurface.withValues(alpha: .38) : null;
      return st.contains(WidgetState.selected) ? s.primary : null;
    }

    final navLabel = (console ? text.labelSmall! : text.labelMedium!);
    final navIndicator = tonal ? const StadiumBorder() : rounded(r.sm);
    final navBackground = switch (d) {
      DesignDirection.tonal => black ? s.surfaceContainer : null,
      DesignDirection.console => black ? s.surface : s.surfaceContainerLow,
      _ => s.surfaceContainerLow,
    };

    final fieldBorder = tonal
        // Expressive filled fields: a tonal fill with rounded top corners
        // and the M3 active indicator, which gives the 3:1 edge.
        ? UnderlineInputBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(r.sm)), borderSide: BorderSide(color: s.onSurfaceVariant))
        : OutlineInputBorder(borderRadius: BorderRadius.circular(r.sm), borderSide: BorderSide(color: s.outline));
    InputBorder focused(double width) => fieldBorder.copyWith(borderSide: BorderSide(color: s.primary, width: width));

    final menuShape = rounded(r.md, edge);
    final menuColor = console ? s.surfaceContainer : s.surfaceContainerHigh;
    final menuElevation = console ? 0.0 : (rack ? 2.0 : 3.0);

    return base.copyWith(
      visualDensity: console ? const VisualDensity(horizontal: 0, vertical: -1) : VisualDensity.standard,
      scaffoldBackgroundColor: s.surface,
      appBarTheme: base.appBarTheme.copyWith(
        backgroundColor: flat ? s.surface : null,
        surfaceTintColor: flat ? Colors.transparent : null,
        scrolledUnderElevation: flat ? 0 : null,
        foregroundColor: s.onSurface,
        titleTextStyle: text.titleLarge!.copyWith(color: s.onSurface),
        // Console rules the bar off from the page, like a terminal's title
        // bar; the others let content scroll under a clean edge.
        shape: console ? Border(bottom: hairline) : null,
      ),
      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: navBackground,
        surfaceTintColor: flat ? Colors.transparent : null,
        elevation: flat ? 0 : null,
        height: console ? 68 : null,
        indicatorShape: navIndicator,
        indicatorColor: s.secondaryContainer,
        labelTextStyle: WidgetStateProperty.resolveWith((st) => navLabel.copyWith(
              color: st.contains(WidgetState.selected) ? s.onSurface : s.onSurfaceVariant,
              fontWeight: st.contains(WidgetState.selected) ? (tonal ? FontWeight.w700 : FontWeight.w600) : FontWeight.w500,
            )),
        iconTheme: WidgetStateProperty.resolveWith(
            (st) => IconThemeData(color: st.contains(WidgetState.selected) ? s.onSecondaryContainer : s.onSurfaceVariant)),
      ),
      navigationRailTheme: NavigationRailThemeData(
        backgroundColor: navBackground,
        indicatorShape: navIndicator,
        indicatorColor: s.secondaryContainer,
        selectedLabelTextStyle: navLabel.copyWith(color: s.onSurface, fontWeight: tonal ? FontWeight.w700 : FontWeight.w600),
        unselectedLabelTextStyle: navLabel.copyWith(color: s.onSurfaceVariant, fontWeight: FontWeight.w500),
        selectedIconTheme: IconThemeData(color: s.onSecondaryContainer),
        unselectedIconTheme: IconThemeData(color: s.onSurfaceVariant),
      ),
      // Symmetric padding so trailing values line up with the 16dp phone
      // gutter (AppScaffold and TileGroup adjust it). Trailing values are
      // tabular everywhere and mono in Console.
      listTileTheme: ListTileThemeData(
        contentPadding: const EdgeInsets.symmetric(horizontal: 16),
        iconColor: s.onSurfaceVariant,
        titleTextStyle: text.bodyLarge!.copyWith(color: s.onSurface, fontWeight: tonal ? FontWeight.w500 : null),
        subtitleTextStyle: text.bodyMedium!.copyWith(color: s.onSurfaceVariant),
        leadingAndTrailingTextStyle: (console ? monoOf(text.labelMedium) : text.labelMedium!)
            .copyWith(color: s.onSurfaceVariant, fontFeatures: const [FontFeature.tabularFigures()]),
        selectedColor: s.onSurface,
        selectedTileColor: s.secondaryContainer,
        shape: tonal ? rounded(r.md) : null,
      ),
      cardTheme: CardThemeData(
        margin: EdgeInsets.zero,
        clipBehavior: Clip.antiAlias,
        elevation: flat ? 0 : null,
        surfaceTintColor: flat ? Colors.transparent : null,
        shape: rounded(r.lg, edge),
      ),
      switchTheme: switchTheme,
      checkboxTheme: CheckboxThemeData(
        fillColor: WidgetStateProperty.resolveWith(toggleFill),
        checkColor: WidgetStatePropertyAll(s.onPrimary),
        shape: rounded(switch (d) { DesignDirection.console => 2.0, DesignDirection.rack => 3.0, _ => 6.0 }),
        side: WidgetStateBorderSide.resolveWith((st) => st.contains(WidgetState.selected)
            ? const BorderSide(color: Colors.transparent, width: 0)
            : BorderSide(color: st.contains(WidgetState.disabled) ? s.onSurface.withValues(alpha: .38) : s.onSurfaceVariant, width: rack || console ? 1.5 : 2)),
      ),
      radioTheme: RadioThemeData(
        fillColor: WidgetStateProperty.resolveWith((st) {
          if (st.contains(WidgetState.disabled)) return s.onSurface.withValues(alpha: .38);
          return st.contains(WidgetState.selected) ? s.primary : s.onSurfaceVariant;
        }),
      ),
      // The current M3 look for sliders and progress (rounded, with a gap
      // and stop indicator) instead of the 2023 one Flutter still defaults
      // to. The flag itself is marked deprecated because it will go away
      // once false is the default; setting it to false is the documented
      // way to opt in until then. Rack and Console draw them thin.
      sliderTheme: SliderThemeData(
        // ignore: deprecated_member_use
        year2023: false,
        trackHeight: switch (d) { DesignDirection.rack => 6.0, DesignDirection.console => 4.0, _ => null },
        activeTrackColor: s.primary,
        inactiveTrackColor: tonal ? null : s.surfaceContainerHighest,
        thumbColor: s.primary,
        valueIndicatorColor: s.inverseSurface,
        valueIndicatorTextStyle: (console ? monoOf(text.labelMedium) : text.labelMedium!).copyWith(color: s.onInverseSurface),
      ),
      progressIndicatorTheme: ProgressIndicatorThemeData(
        // ignore: deprecated_member_use
        year2023: false,
        color: s.primary,
        linearTrackColor: tonal ? null : s.surfaceContainerHighest,
        circularTrackColor: tonal ? null : s.surfaceContainerHighest,
        linearMinHeight: switch (d) { DesignDirection.rack => 3.0, DesignDirection.console => 2.0, _ => null },
        strokeWidth: switch (d) { DesignDirection.rack => 3.0, DesignDirection.console => 2.0, _ => null },
        borderRadius: BorderRadius.circular(r.xs),
      ),
      segmentedButtonTheme: SegmentedButtonThemeData(
        style: ButtonStyle(
          shape: WidgetStatePropertyAll(buttonShape),
          textStyle: WidgetStatePropertyAll(buttonLabel),
          side: WidgetStatePropertyAll(BorderSide(color: s.outline)),
          backgroundColor: WidgetStateProperty.resolveWith((st) => st.contains(WidgetState.selected) ? selectedFill : Colors.transparent),
          foregroundColor: WidgetStateProperty.resolveWith((st) {
            if (st.contains(WidgetState.disabled)) return s.onSurface.withValues(alpha: .38);
            return selectable(st, s.onSurface);
          }),
          iconColor: WidgetStateProperty.resolveWith((st) => selectable(st, s.onSurfaceVariant)),
        ),
      ),
      chipTheme: ChipThemeData(
        shape: rounded(r.sm),
        side: WidgetStateBorderSide.resolveWith((st) => st.contains(WidgetState.selected) ? BorderSide(color: selectedFill) : BorderSide(color: tonal ? s.outlineVariant : s.outline.withValues(alpha: .55))),
        color: WidgetStateProperty.resolveWith((st) {
          if (st.contains(WidgetState.selected)) return st.contains(WidgetState.disabled) ? s.onSurface.withValues(alpha: .12) : selectedFill;
          return tonal ? s.surfaceContainerLow : Colors.transparent;
        }),
        checkmarkColor: onSelected,
        labelStyle: text.labelLarge!.copyWith(
          fontWeight: tonal ? FontWeight.w600 : FontWeight.w500,
          color: WidgetStateColor.resolveWith((st) {
            if (st.contains(WidgetState.disabled)) return s.onSurface.withValues(alpha: .38);
            return selectable(st, s.onSurfaceVariant);
          }),
        ),
        iconTheme: IconThemeData(color: s.onSurfaceVariant, size: 18),
      ),
      filledButtonTheme: FilledButtonThemeData(style: filled),
      outlinedButtonTheme: OutlinedButtonThemeData(style: outlined),
      textButtonTheme: TextButtonThemeData(style: textButton),
      elevatedButtonTheme: ElevatedButtonThemeData(style: elevated),
      iconButtonTheme: IconButtonThemeData(style: iconButton),
      floatingActionButtonTheme: switch (d) {
        DesignDirection.rack => FloatingActionButtonThemeData(
            backgroundColor: s.onSurface,
            foregroundColor: s.surface,
            shape: rounded(r.lg),
            elevation: 2,
            extendedTextStyle: buttonLabel,
          ),
        DesignDirection.tonal => FloatingActionButtonThemeData(shape: rounded(r.md + 4), extendedTextStyle: buttonLabel),
        _ => FloatingActionButtonThemeData(
            backgroundColor: s.surfaceContainerHigh,
            foregroundColor: s.primary,
            shape: rounded(r.md, BorderSide(color: s.primary, width: 1.5)),
            elevation: 0,
            focusElevation: 0,
            hoverElevation: 0,
            highlightElevation: 0,
            extendedTextStyle: buttonLabel,
          ),
      },
      inputDecorationTheme: InputDecorationThemeData(
        filled: tonal,
        fillColor: tonal ? s.surfaceContainerHighest : null,
        border: fieldBorder,
        enabledBorder: fieldBorder,
        focusedBorder: focused(tonal ? 2 : 1.5),
        errorBorder: fieldBorder.copyWith(borderSide: BorderSide(color: s.error)),
        focusedErrorBorder: fieldBorder.copyWith(borderSide: BorderSide(color: s.error, width: tonal ? 2 : 1.5)),
        labelStyle: text.bodyLarge!.copyWith(color: s.onSurfaceVariant),
        hintStyle: text.bodyLarge!.copyWith(color: s.onSurfaceVariant),
        helperStyle: text.bodySmall!.copyWith(color: s.onSurfaceVariant),
        isDense: console,
      ),
      searchBarTheme: SearchBarThemeData(
        elevation: const WidgetStatePropertyAll(0),
        backgroundColor: WidgetStatePropertyAll(console ? s.surfaceContainer : s.surfaceContainerHigh),
        surfaceTintColor: const WidgetStatePropertyAll(Colors.transparent),
        shape: WidgetStatePropertyAll(tonal ? const StadiumBorder() : rounded(r.md)),
        side: WidgetStatePropertyAll(edge),
        textStyle: WidgetStatePropertyAll(text.bodyLarge!.copyWith(color: s.onSurface)),
        hintStyle: WidgetStatePropertyAll(text.bodyLarge!.copyWith(color: s.onSurfaceVariant)),
      ),
      searchViewTheme: SearchViewThemeData(
        backgroundColor: s.surfaceContainerHigh,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        shape: rounded(r.xl),
        side: edge,
        dividerColor: s.outlineVariant,
      ),
      dividerTheme: DividerThemeData(color: s.outlineVariant, space: 1, thickness: 1),
      dialogTheme: DialogThemeData(
        shape: rounded(r.xl, edge),
        backgroundColor: flat ? (console ? s.surfaceContainer : s.surfaceContainerHigh) : null,
        surfaceTintColor: flat ? Colors.transparent : null,
        // Console titles are a size down: a console, not a poster.
        titleTextStyle: (console ? text.titleLarge : text.headlineSmall)!.copyWith(color: s.onSurface),
        contentTextStyle: text.bodyMedium!.copyWith(color: s.onSurfaceVariant),
      ),
      bottomSheetTheme: BottomSheetThemeData(
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(switch (d) { DesignDirection.rack => 24.0, DesignDirection.console => 16.0, _ => 28.0 })),
          side: edge,
        ),
        backgroundColor: flat ? s.surfaceContainerLow : null,
        modalBackgroundColor: flat ? s.surfaceContainerLow : null,
        surfaceTintColor: flat ? Colors.transparent : null,
        dragHandleColor: s.outline,
      ),
      snackBarTheme: SnackBarThemeData(
        behavior: SnackBarBehavior.floating,
        backgroundColor: s.inverseSurface,
        contentTextStyle: text.bodyMedium!.copyWith(color: s.onInverseSurface),
        // Rack and Console set the action in the inverse ink (the neutral
        // inverse surface doesn't match the accent's inverse tone); Tonal
        // keeps the M3 inverse primary.
        actionTextColor: tonal ? s.inversePrimary : s.onInverseSurface,
        shape: rounded(r.md),
        elevation: flat ? 2 : null,
      ),
      popupMenuTheme: PopupMenuThemeData(
        color: menuColor,
        surfaceTintColor: Colors.transparent,
        elevation: menuElevation,
        shape: menuShape,
        labelTextStyle: WidgetStatePropertyAll(text.bodyLarge!.copyWith(color: s.onSurface)),
      ),
      menuTheme: MenuThemeData(
        style: MenuStyle(
          backgroundColor: WidgetStatePropertyAll(menuColor),
          surfaceTintColor: const WidgetStatePropertyAll(Colors.transparent),
          elevation: WidgetStatePropertyAll(menuElevation),
          shape: WidgetStatePropertyAll(menuShape),
          side: WidgetStatePropertyAll(edge),
        ),
      ),
      menuButtonTheme: MenuButtonThemeData(
        style: MenuItemButton.styleFrom(textStyle: text.bodyLarge, foregroundColor: s.onSurface, iconColor: s.onSurfaceVariant),
      ),
      tabBarTheme: TabBarThemeData(
        labelColor: tonal ? s.primary : s.onSurface,
        unselectedLabelColor: s.onSurfaceVariant,
        labelStyle: text.titleSmall!.copyWith(fontWeight: tonal ? FontWeight.w700 : FontWeight.w600),
        unselectedLabelStyle: text.titleSmall!.copyWith(fontWeight: FontWeight.w500),
        dividerColor: s.outlineVariant,
        // Rack underlines in ink, Console in a square accent line, Tonal in
        // the M3 rounded accent.
        indicator: UnderlineTabIndicator(
          borderSide: BorderSide(color: rack ? s.onSurface : s.primary, width: tonal ? 3 : 2),
          borderRadius: tonal ? const BorderRadius.vertical(top: Radius.circular(3)) : BorderRadius.zero,
        ),
      ),
      tooltipTheme: TooltipThemeData(
        decoration: BoxDecoration(color: s.inverseSurface, borderRadius: BorderRadius.circular(r.xs)),
        textStyle: (console ? monoOf(text.labelSmall) : text.labelMedium!).copyWith(color: s.onInverseSurface),
      ),
      drawerTheme: DrawerThemeData(
        backgroundColor: s.surfaceContainerLow,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.horizontal(right: Radius.circular(r.lg))),
        endShape: RoundedRectangleBorder(borderRadius: BorderRadius.horizontal(left: Radius.circular(r.lg))),
      ),
      badgeTheme: BadgeThemeData(textStyle: (console ? monoOf(text.labelSmall) : text.labelSmall!).copyWith(fontFeatures: const [FontFeature.tabularFigures()])),
      scrollbarTheme: ScrollbarThemeData(
        thickness: WidgetStatePropertyAll(tonal ? 6 : 4),
        radius: Radius.circular(console ? 0 : 4),
        thumbColor: WidgetStatePropertyAll(s.onSurfaceVariant.withValues(alpha: .5)),
      ),
      textSelectionTheme: TextSelectionThemeData(
        cursorColor: s.primary,
        selectionColor: s.primary.withValues(alpha: .28),
        selectionHandleColor: s.primary,
      ),
      // Rack and Console cross-fade between pages (calm, no movement);
      // Tonal keeps the expressive predictive-back zoom.
      pageTransitionsTheme: PageTransitionsTheme(
        builders: {
          TargetPlatform.android: tonal ? const PredictiveBackPageTransitionsBuilder() : const FadeForwardsPageTransitionsBuilder(),
          TargetPlatform.iOS: const CupertinoPageTransitionsBuilder(),
          TargetPlatform.macOS: const CupertinoPageTransitionsBuilder(),
        },
      ),
    );
  }
}
