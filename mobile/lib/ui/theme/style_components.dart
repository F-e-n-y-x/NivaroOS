import 'package:flutter/cupertino.dart' show CupertinoPageTransitionsBuilder;
import 'package:flutter/material.dart';

import 'appearance.dart';
import 'design_tokens.dart';

/// How a style draws the stock Material components (owner request,
/// 2026-09-26: "a style styles the whole app"). `AppTheme` hands every
/// Rack, Tonal, Console, Soft, Terminal and Bold theme through [StyleComponents.apply], so an
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
/// - **Soft**: grouped settings. Large corners (14-26), no edges, white
///   surfaces on grey, filled accent buttons and selections, grey-filled
///   quiet buttons, chips and fields, the iOS-style page slide.
/// - **Terminal**: mono and square (0-4), the page ruled off by lines
///   (the app bar included), inverse video for what is pressed or
///   selected - an accent block with page-coloured text - one step denser.
/// - **Bold**: Swiss. Small corners (2-12), solid fills with no edges,
///   accent buttons with heavy labels, ink selections and a 2dp ink rule
///   under the app bar, underlined fields, thick tracks and meters.
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
        // sm is chips (a 32dp chip at 16 is a capsule) and thumbnails; md
        // is buttons, fields and menus.
        DesignDirection.soft => const Radii(xs: 6, sm: 16, md: 14, lg: 22, xl: 26),
        DesignDirection.terminal => const Radii(xs: 0, sm: 2, md: 2, lg: 4, xl: 4),
        DesignDirection.bold => const Radii(xs: 2, sm: 4, md: 6, lg: 8, xl: 12),
      };

  /// The floating navigation bar of [d] on [s] (see [NavBarTokens]).
  /// Light themes float on a soft shadow (not Console, which is drawn by
  /// lines alone); dark ones on their edge or a tonal step, where a shadow
  /// wouldn't show. True black draws the style's edge in every style: the
  /// bar is a near-black panel on the #000 page, and Tonal's cards (the
  /// same tonal step as its bar) scroll under it.
  static NavBarTokens navBar(DesignDirection d, ColorScheme s, {required bool black}) {
    final light = s.brightness == Brightness.light;
    return switch (d) {
      // v2 floats too, as the M3 pill: one shell for every style.
      DesignDirection.v2 => NavBarTokens(
          height: 68,
          radius: 28,
          color: light ? s.surfaceContainer : (black ? s.surfaceContainerLow : s.surfaceContainerHigh),
          edge: black ? s.outlineVariant : null,
          elevation: light ? 3 : 0,
        ),
      // Paper or graphite, like the cards, with their hairline.
      DesignDirection.rack => NavBarTokens(height: 64, radius: radii(d).lg, color: s.surfaceContainer, edge: s.outlineVariant, elevation: light ? 2 : 0),
      // A soft tonal pill: a step above the cards in dark, so it still
      // reads over them.
      DesignDirection.tonal => NavBarTokens(
          height: 68,
          radius: 34,
          color: light ? s.surfaceContainer : (black ? s.surfaceContainerHigh : s.surfaceContainerHighest),
          edge: black ? s.outlineVariant : null,
          elevation: light ? 3 : 0,
        ),
      // A ruled panel: black on true black, like Console's cards.
      DesignDirection.console => NavBarTokens(height: 60, radius: radii(d).md, color: black ? s.surface : s.surfaceContainer, edge: s.outlineVariant, elevation: 0),
      // A white pill on a soft shadow, like the cards; a grey one in dark.
      DesignDirection.soft => NavBarTokens(
          height: 68,
          radius: 34,
          color: light ? s.surfaceContainer : (black ? s.surfaceContainerHigh : s.surfaceContainerHighest),
          edge: black ? s.outlineVariant : null,
          elevation: light ? 3 : 0,
        ),
      // A ruled strip the page colour, in every mode.
      DesignDirection.terminal => NavBarTokens(height: 60, radius: radii(d).lg, color: s.surface, edge: s.outlineVariant, elevation: 0),
      // A solid slab: ink in light, the top grey step in dark.
      DesignDirection.bold => NavBarTokens(
          height: 64,
          radius: radii(d).lg,
          color: light ? s.inverseSurface : s.surfaceContainerHighest,
          edge: black ? s.outlineVariant : null,
          elevation: 0,
        ),
    };
  }

  /// The floating bar's selected indicator in [d]: concentric with the
  /// bar ([FloatingNavigationBar.indicatorInset] in from its corners), so
  /// Rack's is precise, Tonal's a pill and Console's tight and ruled by the
  /// accent. v2 keeps the M3 pill.
  static ShapeBorder navBarIndicator(DesignDirection d, ColorScheme s) {
    const inset = 6.0; // FloatingNavigationBar.indicatorInset
    final bar = navBar(d, s, black: false);
    final outer = bar.radius.clamp(0.0, bar.height / 2);
    if (d == DesignDirection.v2 || outer >= bar.height / 2) return const StadiumBorder();
    final radius = BorderRadius.circular((outer - inset).clamp(2.0, outer));
    return d == DesignDirection.console
        ? RoundedRectangleBorder(borderRadius: radius, side: BorderSide(color: s.primary))
        : RoundedRectangleBorder(borderRadius: radius);
  }

  /// The floating bar's indicator fill in [d]: the style's selected fill
  /// (ink in Rack, the primary container in Console, the accent in
  /// Terminal), and in Tonal and v2 the secondary container - except in
  /// light, where that container is a step off the bar's own tone (1.1:1)
  /// and the indicator all but vanished, so there it is the secondary
  /// colour at 24% over the bar (about 1.4:1), still a soft tonal pill.
  /// Soft tints the bar with the accent; Bold's is the opposite tone of
  /// its slab (paper on the ink bar, white on the grey one).
  static Color navBarIndicatorColor(DesignDirection d, ColorScheme s, {required bool black}) => navBarColors(d, s, black: black).indicator;

  /// The floating bar's indicator fill, the selected icon and label on
  /// it, and the idle icons and labels on the bar, in [d].
  static ({Color indicator, Color selected, Color idle}) navBarColors(DesignDirection d, ColorScheme s, {required bool black}) {
    final light = s.brightness == Brightness.light;
    final bar = navBar(d, s, black: black).color;
    return switch (d) {
      DesignDirection.rack => (indicator: s.onSurface, selected: s.surface, idle: s.onSurfaceVariant),
      DesignDirection.console => (indicator: s.primaryContainer, selected: s.onPrimaryContainer, idle: s.onSurfaceVariant),
      DesignDirection.terminal => (indicator: s.primary, selected: s.onPrimary, idle: s.onSurfaceVariant),
      // An accent-tinted pill holding ink (the accent itself would sit
      // under 4.5:1 on its own tint).
      DesignDirection.soft => (indicator: Color.alphaBlend(s.primary.withValues(alpha: .2), bar), selected: s.onSurface, idle: s.onSurfaceVariant),
      DesignDirection.bold => light
          ? (indicator: s.surface, selected: s.onSurface, idle: s.onInverseSurface)
          : (indicator: s.onSurface, selected: s.surface, idle: s.onSurfaceVariant),
      DesignDirection.v2 || DesignDirection.tonal => (
          indicator: light ? Color.alphaBlend(s.secondary.withValues(alpha: .24), bar) : s.secondaryContainer,
          selected: s.onSecondaryContainer,
          idle: s.onSurfaceVariant,
        ),
    };
  }

  /// The component themes of [d] on [base], whose [ThemeData.textTheme]
  /// carries the style's faces; [text] is that theme with real sizes.
  static ThemeData apply(ThemeData base, DesignDirection d, TextTheme text, {required bool black, required String? mono}) {
    assert(d != DesignDirection.v2, 'v2 keeps the Material defaults');
    final s = base.colorScheme;
    final r = radii(d);
    final rack = d == DesignDirection.rack;
    final tonal = d == DesignDirection.tonal;
    final console = d == DesignDirection.console;
    final soft = d == DesignDirection.soft;
    final terminal = d == DesignDirection.terminal;
    final bold = d == DesignDirection.bold;
    // Console and Terminal: dense, ruled, the app bar ruled off.
    final ruled = console || terminal;
    // Only Tonal tints by elevation; every other style is flat (no
    // scroll-under tint, no elevation tint), and so is every style on true
    // black, where a tint turns navy.
    final flat = !tonal || black;

    RoundedRectangleBorder rounded(double radius, [BorderSide side = BorderSide.none]) =>
        RoundedRectangleBorder(borderRadius: BorderRadius.circular(radius), side: side);
    final hairline = BorderSide(color: s.outlineVariant);
    // Floating surfaces (dialogs, sheets, menus, snack bars) get a hairline
    // in Rack, Console and Terminal; Tonal, Soft and Bold separate them by
    // tone (and in light a shadow) alone.
    final edgeless = tonal || soft || bold;
    final edge = edgeless ? BorderSide.none : hairline;
    TextStyle monoOf(TextStyle? t) => t!.copyWith(fontFamily: mono, fontFamilyFallback: const ['Roboto']);

    final OutlinedBorder buttonShape = tonal ? const StadiumBorder() : rounded(r.md);
    // Button labels: Console's actions are mono, Tonal's and Bold's
    // emphasised (Terminal's face is mono already).
    final buttonLabel = console
        ? monoOf(text.labelLarge).copyWith(fontWeight: FontWeight.w500, letterSpacing: .2)
        : text.labelLarge!.copyWith(fontWeight: tonal || bold ? FontWeight.w600 : FontWeight.w500);

    // The primary button is part of each style's identity: ink in Rack,
    // tonal in Tonal, an accent outline with a mono label in Console, the
    // accent fill in Soft, Terminal (inverse video, square) and Bold (a
    // roomier slab with a heavy label).
    // Monochrome's containers are greys that sit next to the secondary
    // container (in dark they are the same grey), so there Tonal's primary
    // is ink on the page instead, to stay a step above a tonal button.
    final monoAccent = s.primary == s.onSurface;
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
          backgroundColor: monoAccent ? s.primary : s.primaryContainer,
          foregroundColor: monoAccent ? s.onPrimary : s.onPrimaryContainer,
          textStyle: buttonLabel,
        ),
      DesignDirection.console || DesignDirection.v2 => FilledButton.styleFrom(
          backgroundColor: Colors.transparent,
          foregroundColor: s.primary,
          side: BorderSide(color: s.primary, width: 1.5),
          textStyle: buttonLabel,
          shape: buttonShape,
        ),
      DesignDirection.soft || DesignDirection.terminal || DesignDirection.bold => FilledButton.styleFrom(
          backgroundColor: s.primary,
          foregroundColor: s.onPrimary,
          textStyle: buttonLabel,
          shape: buttonShape,
          padding: bold ? const EdgeInsets.symmetric(horizontal: 22) : null,
          minimumSize: bold ? const Size(64, 44) : null,
        ),
    };
    // Outlined buttons are the quiet alternative: ink on a hairline in Rack,
    // Console and Terminal (the accent outline is Console's primary), the
    // M3 accent label in Tonal, ink on a 2dp ink edge in Bold, and in Soft
    // a grey fill with the accent label and no edge (a grouped-settings
    // "grey" button).
    final outlined = OutlinedButton.styleFrom(
      foregroundColor: tonal ? null : (soft ? s.primary : s.onSurface),
      backgroundColor: soft ? s.surfaceContainerHighest : null,
      side: tonal ? null : (soft ? BorderSide.none : (bold ? BorderSide(color: s.onSurface, width: 2) : BorderSide(color: s.outline))),
      textStyle: buttonLabel,
      shape: buttonShape,
      padding: bold ? const EdgeInsets.symmetric(horizontal: 22) : null,
      minimumSize: bold ? const Size(64, 44) : null,
    );
    final textButton = TextButton.styleFrom(textStyle: buttonLabel, shape: buttonShape);
    final elevated = ElevatedButton.styleFrom(
      textStyle: buttonLabel,
      shape: buttonShape,
      elevation: flat ? 0 : null,
      backgroundColor: flat ? s.surfaceContainerHigh : null,
      side: flat && !edgeless ? hairline : null,
    );
    final iconButton = IconButton.styleFrom(shape: ruled || bold ? rounded(r.sm) : null);

    // Selected chips and segments: ink in Rack and Bold (a pressed key),
    // the secondary container in Tonal, the primary container in Console,
    // the accent in Soft and Terminal (inverse video). The label and check
    // follow the fill; unselected ones stay on the page (Soft's sit on a
    // grey fill).
    final (Color selectedFill, Color onSelected) = switch (d) {
      DesignDirection.rack || DesignDirection.bold => (s.onSurface, s.surface),
      DesignDirection.console => (s.primaryContainer, s.onPrimaryContainer),
      DesignDirection.soft || DesignDirection.terminal => (s.primary, s.onPrimary),
      DesignDirection.v2 || DesignDirection.tonal => (s.secondaryContainer, s.onSecondaryContainer),
    };
    Color selectable(Set<WidgetState> st, Color unselected) => st.contains(WidgetState.selected) ? onSelected : unselected;

    // Toggles are the accent's: on is the primary colour with an onPrimary
    // thumb in every style (Monochrome makes that ink). Off differs: Rack
    // a paper track with an outline, Console a graphite track with a
    // hairline, Tonal the M3 default.
    final switchTheme = tonal
        ? SwitchThemeData(
            // The check in the primary on the onPrimary thumb: M3 draws it
            // in onPrimaryContainer, which Monochrome leaves near-white on
            // a white thumb in light.
            thumbIcon: WidgetStateProperty.resolveWith((st) => st.contains(WidgetState.selected) ? Icon(Icons.check, color: s.primary) : null),
          )
        : SwitchThemeData(
            thumbColor: WidgetStateProperty.resolveWith((st) {
              if (st.contains(WidgetState.disabled)) return s.onSurface.withValues(alpha: .38);
              return st.contains(WidgetState.selected) ? s.onPrimary : (rack ? s.outline : s.onSurfaceVariant);
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

    final navLabel = (ruled ? text.labelSmall! : text.labelMedium!);
    final navIndicator = tonal || soft ? const StadiumBorder() : rounded(r.sm);
    // The rail (tablets, landscape) keeps its edge-to-edge fill; the bar
    // on phones floats on its own surface.
    final navBackground = switch (d) {
      DesignDirection.tonal => black ? s.surfaceContainer : null,
      DesignDirection.console => black ? s.surface : s.surfaceContainerLow,
      DesignDirection.terminal => s.surface,
      DesignDirection.soft || DesignDirection.bold => s.surfaceContainer,
      DesignDirection.v2 || DesignDirection.rack => s.surfaceContainerLow,
    };
    final bar = navBar(d, s, black: black);
    final navColors = navBarColors(d, s, black: black);

    final InputBorder fieldBorder = switch (d) {
      // Expressive filled fields: a tonal fill with rounded top corners
      // and the M3 active indicator, which gives the 3:1 edge.
      DesignDirection.tonal =>
        UnderlineInputBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(r.sm)), borderSide: BorderSide(color: s.onSurfaceVariant)),
      // Editorial: a bare ink rule under the text, no box.
      DesignDirection.bold => UnderlineInputBorder(borderRadius: BorderRadius.zero, borderSide: BorderSide(color: s.onSurfaceVariant, width: 1.5)),
      // A white (dark: card grey) field with the button's corners.
      DesignDirection.soft => OutlineInputBorder(borderRadius: BorderRadius.circular(r.md), borderSide: BorderSide(color: s.outline)),
      DesignDirection.v2 || DesignDirection.rack || DesignDirection.console || DesignDirection.terminal =>
        OutlineInputBorder(borderRadius: BorderRadius.circular(r.sm), borderSide: BorderSide(color: s.outline)),
    };
    InputBorder focused(double width) => fieldBorder.copyWith(borderSide: BorderSide(color: s.primary, width: width));

    final menuShape = rounded(r.md, edge);
    final menuColor = ruled ? s.surfaceContainer : s.surfaceContainerHigh;
    final menuElevation = ruled ? 0.0 : (rack || bold ? 2.0 : 3.0);

    return base.copyWith(
      visualDensity: ruled ? const VisualDensity(horizontal: 0, vertical: -1) : VisualDensity.standard,
      scaffoldBackgroundColor: s.surface,
      appBarTheme: base.appBarTheme.copyWith(
        backgroundColor: flat ? s.surface : null,
        surfaceTintColor: flat ? Colors.transparent : null,
        scrolledUnderElevation: flat ? 0 : null,
        foregroundColor: s.onSurface,
        titleTextStyle: text.titleLarge!.copyWith(color: s.onSurface),
        // Console and Terminal rule the bar off from the page, like a
        // terminal's title bar; Bold sets a 2dp ink rule under it, like a
        // masthead; the others let content scroll under a clean edge.
        shape: ruled ? Border(bottom: hairline) : (bold ? Border(bottom: BorderSide(color: s.onSurface, width: 2)) : null),
      ),
      // The floating bar (FloatingNavigationBar): one indicator behind the
      // selected icon and its label, in the style's selected fill (ink in
      // Rack, the secondary container in Tonal, the primary container in
      // Console, where the accent also rules it as a line), with corners
      // concentric with the bar's.
      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: bar.color,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        height: bar.height,
        indicatorShape: navBarIndicator(d, s),
        indicatorColor: navColors.indicator,
        labelTextStyle: WidgetStateProperty.resolveWith((st) => navLabel.copyWith(
              color: st.contains(WidgetState.selected) ? navColors.selected : navColors.idle,
              fontWeight: st.contains(WidgetState.selected) ? (tonal ? FontWeight.w700 : FontWeight.w600) : FontWeight.w500,
            )),
        iconTheme: WidgetStateProperty.resolveWith(
            (st) => IconThemeData(size: 24, color: st.contains(WidgetState.selected) ? navColors.selected : navColors.idle)),
      ),
      // The rail keeps the M3 layout (the label under the indicator, on the
      // rail), in the same selected fill.
      navigationRailTheme: NavigationRailThemeData(
        backgroundColor: navBackground,
        indicatorShape: navIndicator,
        indicatorColor: selectedFill,
        selectedLabelTextStyle: navLabel.copyWith(color: s.onSurface, fontWeight: tonal ? FontWeight.w700 : FontWeight.w600),
        unselectedLabelTextStyle: navLabel.copyWith(color: s.onSurfaceVariant, fontWeight: FontWeight.w500),
        selectedIconTheme: IconThemeData(color: onSelected),
        unselectedIconTheme: IconThemeData(color: s.onSurfaceVariant),
      ),
      // Symmetric padding so trailing values line up with the 16dp phone
      // gutter (AppScaffold and TileGroup adjust it). Trailing values are
      // tabular everywhere and mono in Console.
      listTileTheme: ListTileThemeData(
        contentPadding: const EdgeInsets.symmetric(horizontal: 16),
        iconColor: s.onSurfaceVariant,
        titleTextStyle: text.bodyLarge!.copyWith(color: s.onSurface, fontWeight: tonal || bold ? FontWeight.w500 : null),
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
        // Soft's and Bold's cards are their white or grey slabs; Terminal's
        // the page, ruled.
        color: soft || bold ? s.surfaceContainer : (terminal ? s.surface : null),
        elevation: flat ? 0 : null,
        surfaceTintColor: flat ? Colors.transparent : null,
        shape: rounded(r.lg, edge),
      ),
      switchTheme: switchTheme,
      checkboxTheme: CheckboxThemeData(
        fillColor: WidgetStateProperty.resolveWith(toggleFill),
        checkColor: WidgetStatePropertyAll(s.onPrimary),
        shape: rounded(switch (d) {
          DesignDirection.terminal => 1.0,
          DesignDirection.console || DesignDirection.bold => 2.0,
          DesignDirection.rack => 3.0,
          DesignDirection.v2 || DesignDirection.tonal || DesignDirection.soft => 6.0,
        }),
        side: WidgetStateBorderSide.resolveWith((st) => st.contains(WidgetState.selected)
            ? const BorderSide(color: Colors.transparent, width: 0)
            : BorderSide(color: st.contains(WidgetState.disabled) ? s.onSurface.withValues(alpha: .38) : s.onSurfaceVariant, width: rack || ruled ? 1.5 : 2)),
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
        trackHeight: switch (d) {
          DesignDirection.rack => 6.0,
          DesignDirection.console || DesignDirection.terminal => 4.0,
          DesignDirection.bold => 8.0,
          DesignDirection.v2 || DesignDirection.tonal || DesignDirection.soft => null,
        },
        activeTrackColor: s.primary,
        inactiveTrackColor: tonal ? null : s.surfaceContainerHighest,
        thumbColor: s.primary,
        valueIndicatorColor: s.inverseSurface,
        valueIndicatorTextStyle: (ruled ? monoOf(text.labelMedium) : text.labelMedium!).copyWith(color: s.onInverseSurface),
      ),
      progressIndicatorTheme: ProgressIndicatorThemeData(
        // ignore: deprecated_member_use
        year2023: false,
        color: s.primary,
        linearTrackColor: tonal ? null : s.surfaceContainerHighest,
        circularTrackColor: tonal ? null : s.surfaceContainerHighest,
        linearMinHeight: switch (d) {
          DesignDirection.rack => 3.0,
          DesignDirection.console || DesignDirection.terminal => 2.0,
          DesignDirection.bold => 6.0,
          DesignDirection.v2 || DesignDirection.tonal || DesignDirection.soft => null,
        },
        strokeWidth: switch (d) {
          DesignDirection.rack => 3.0,
          DesignDirection.console || DesignDirection.terminal => 2.0,
          DesignDirection.bold => 5.0,
          DesignDirection.v2 || DesignDirection.tonal || DesignDirection.soft => null,
        },
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
        // Soft's chips are grey capsules with no edge; Bold's unselected
        // ones carry a heavier edge.
        side: WidgetStateBorderSide.resolveWith((st) {
          if (st.contains(WidgetState.selected)) return BorderSide(color: selectedFill);
          if (soft) return BorderSide(color: s.surfaceContainerHighest);
          if (bold) return BorderSide(color: s.outline, width: 1.5);
          return BorderSide(color: tonal ? s.outlineVariant : s.outline.withValues(alpha: .55));
        }),
        color: WidgetStateProperty.resolveWith((st) {
          if (st.contains(WidgetState.selected)) return st.contains(WidgetState.disabled) ? s.onSurface.withValues(alpha: .12) : selectedFill;
          return tonal ? s.surfaceContainerLow : (soft ? s.surfaceContainerHighest : Colors.transparent);
        }),
        checkmarkColor: onSelected,
        labelStyle: text.labelLarge!.copyWith(
          fontWeight: tonal || bold ? FontWeight.w600 : FontWeight.w500,
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
        // The primary button's accent fill: lifted and rounded in Soft,
        // flat and square-cornered in Terminal and Bold.
        DesignDirection.soft || DesignDirection.terminal || DesignDirection.bold => FloatingActionButtonThemeData(
            backgroundColor: s.primary,
            foregroundColor: s.onPrimary,
            shape: rounded(soft ? r.lg : r.md),
            elevation: soft ? 3 : 0,
            focusElevation: soft ? null : 0,
            hoverElevation: soft ? null : 0,
            highlightElevation: soft ? null : 0,
            extendedTextStyle: buttonLabel,
          ),
        DesignDirection.console || DesignDirection.v2 => FloatingActionButtonThemeData(
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
        filled: tonal || soft,
        fillColor: tonal ? s.surfaceContainerHighest : (soft ? s.surfaceContainer : null),
        border: fieldBorder,
        enabledBorder: fieldBorder,
        focusedBorder: focused(tonal || bold ? 2 : 1.5),
        errorBorder: fieldBorder.copyWith(borderSide: BorderSide(color: s.error)),
        focusedErrorBorder: fieldBorder.copyWith(borderSide: BorderSide(color: s.error, width: tonal ? 2 : 1.5)),
        labelStyle: text.bodyLarge!.copyWith(color: s.onSurfaceVariant),
        hintStyle: text.bodyLarge!.copyWith(color: s.onSurfaceVariant),
        helperStyle: text.bodySmall!.copyWith(color: s.onSurfaceVariant),
        isDense: ruled,
      ),
      searchBarTheme: SearchBarThemeData(
        elevation: const WidgetStatePropertyAll(0),
        backgroundColor: WidgetStatePropertyAll(ruled ? s.surfaceContainer : (soft ? s.surfaceContainerHighest : s.surfaceContainerHigh)),
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
        backgroundColor: flat ? (ruled ? s.surfaceContainer : s.surfaceContainerHigh) : null,
        surfaceTintColor: flat ? Colors.transparent : null,
        // Console and Terminal titles are a size down: a console, not a
        // poster.
        titleTextStyle: (ruled ? text.titleLarge : text.headlineSmall)!.copyWith(color: s.onSurface),
        contentTextStyle: text.bodyMedium!.copyWith(color: s.onSurfaceVariant),
      ),
      bottomSheetTheme: BottomSheetThemeData(
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(switch (d) {
            DesignDirection.rack => 24.0,
            DesignDirection.console => 16.0,
            DesignDirection.terminal || DesignDirection.bold => r.xl,
            DesignDirection.v2 || DesignDirection.tonal || DesignDirection.soft => 28.0,
          })),
          side: edge,
        ),
        // Soft's sheet is a white card on the grey page.
        backgroundColor: flat ? (soft ? s.surfaceContainer : s.surfaceContainerLow) : null,
        modalBackgroundColor: flat ? (soft ? s.surfaceContainer : s.surfaceContainerLow) : null,
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
        labelStyle: text.titleSmall!.copyWith(fontWeight: tonal ? FontWeight.w700 : (terminal ? FontWeight.w500 : FontWeight.w600)),
        unselectedLabelStyle: text.titleSmall!.copyWith(fontWeight: terminal ? FontWeight.w400 : FontWeight.w500),
        dividerColor: s.outlineVariant,
        // Rack underlines in ink, Bold in a heavy ink bar, Console and
        // Terminal in a square accent line, Tonal and Soft in the M3
        // rounded accent.
        indicator: UnderlineTabIndicator(
          borderSide: BorderSide(color: rack || bold ? s.onSurface : s.primary, width: tonal || soft || bold ? 3 : 2),
          borderRadius: tonal || soft ? const BorderRadius.vertical(top: Radius.circular(3)) : BorderRadius.zero,
        ),
      ),
      tooltipTheme: TooltipThemeData(
        decoration: BoxDecoration(color: s.inverseSurface, borderRadius: BorderRadius.circular(r.xs)),
        textStyle: (ruled ? monoOf(text.labelSmall) : text.labelMedium!).copyWith(color: s.onInverseSurface),
      ),
      drawerTheme: DrawerThemeData(
        backgroundColor: s.surfaceContainerLow,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.horizontal(right: Radius.circular(r.lg))),
        endShape: RoundedRectangleBorder(borderRadius: BorderRadius.horizontal(left: Radius.circular(r.lg))),
      ),
      badgeTheme: BadgeThemeData(textStyle: (ruled ? monoOf(text.labelSmall) : text.labelSmall!).copyWith(fontFeatures: const [FontFeature.tabularFigures()])),
      scrollbarTheme: ScrollbarThemeData(
        thickness: WidgetStatePropertyAll(tonal || soft ? 6 : 4),
        radius: Radius.circular(ruled ? 0 : 4),
        thumbColor: WidgetStatePropertyAll(s.onSurfaceVariant.withValues(alpha: .5)),
      ),
      textSelectionTheme: TextSelectionThemeData(
        cursorColor: s.primary,
        selectionColor: s.primary.withValues(alpha: .28),
        selectionHandleColor: s.primary,
      ),
      // Rack, Console, Terminal and Bold cross-fade between pages (calm, no
      // movement); Tonal keeps the expressive predictive-back zoom; Soft
      // slides like grouped settings do.
      pageTransitionsTheme: PageTransitionsTheme(
        builders: {
          TargetPlatform.android: switch (d) {
            DesignDirection.tonal => const PredictiveBackPageTransitionsBuilder(),
            DesignDirection.soft => const CupertinoPageTransitionsBuilder(),
            _ => const FadeForwardsPageTransitionsBuilder(),
          },
          TargetPlatform.iOS: const CupertinoPageTransitionsBuilder(),
          TargetPlatform.macOS: const CupertinoPageTransitionsBuilder(),
        },
      ),
    );
  }
}
