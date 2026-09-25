import 'package:flutter/material.dart';

import '../theme/app_theme.dart';
import '../theme/appearance.dart';
import '../theme/design_tokens.dart';
import '../theme/spacing.dart';
import '../theme/theme_controller.dart';
import '../theme/motion.dart';
import 'metric_card.dart';
import 'app_scaffold.dart';
import 'live_chart.dart';
import 'section_header.dart';

/// The "Appearance" setting on More: the current choice in one line
/// ("Dark · Teal"), opening [AppearanceScreen].
class AppearanceTile extends StatelessWidget {
  const AppearanceTile({super.key, this.controller});

  /// Defaults to [ThemeController.instance]; tests pass their own.
  final ThemeController? controller;

  @override
  Widget build(BuildContext context) {
    final c = controller ?? ThemeController.instance;
    return ListenableBuilder(
      listenable: Listenable.merge([c, c.wallpaperSeed]),
      builder: (context, _) => ListTile(
        leading: const Icon(Icons.palette_outlined),
        title: const Text('Appearance'),
        subtitle: Text(c.value.summary(wallpaperAvailable: c.wallpaperSeed.value != null)),
        onTap: () => Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => AppearanceScreen(controller: c))),
      ),
    );
  }
}

/// Design preview, mode and colour, each shown as what it looks like
/// rather than as a word: a live preview of Home's cards at the top that
/// redraws with every choice, the design directions as samples of
/// themselves, the modes as small pictures of the app in that mode, the
/// accents as labelled swatches. Every change applies at once (the app
/// re-themes behind the page) and is saved.
class AppearanceScreen extends StatelessWidget {
  const AppearanceScreen({super.key, this.controller});

  final ThemeController? controller;

  @override
  Widget build(BuildContext context) {
    final c = controller ?? ThemeController.instance;
    return ListenableBuilder(
      listenable: Listenable.merge([c, c.wallpaperSeed]),
      builder: (context, _) {
        final a = c.value;
        final seed = c.wallpaperSeed.value;
        final gutter = Space.gutter(context);
        final wallpaperOn = a.wallpaper && seed != null;
        final theme = Theme.of(context);
        final note = theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant);
        return AppScaffold.slivers(
          title: 'Appearance',
          slivers: [
            SliverList.list(children: [
              Padding(padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0), child: const _Preview()),
              Padding(padding: EdgeInsets.fromLTRB(gutter, Space.md, gutter, 0), child: const _Controls()),
              const SectionHeader(title: 'Design preview'),
              Padding(
                padding: EdgeInsets.symmetric(horizontal: gutter),
                child: _StyleRow(appearance: a, seed: seed, onPick: (d) => c.set(a.copyWith(direction: d))),
              ),
              Padding(
                padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0),
                child: Text(a.direction.description, style: note),
              ),
              const SectionHeader(title: 'Mode'),
              Padding(
                padding: EdgeInsets.symmetric(horizontal: gutter),
                child: _ModeRow(appearance: a, seed: seed, onPick: (m) => c.set(a.copyWith(mode: m))),
              ),
              Padding(
                padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0),
                child: Text(
                  switch (a.mode) {
                    AppThemeMode.system => "Light or dark, following this phone's dark theme setting.",
                    AppThemeMode.black => 'Pure black behind everything, for OLED screens. ${_blackNote(a.direction)}',
                    _ => 'Always ${a.mode.label.toLowerCase()}, whatever the phone is set to.',
                  },
                  style: note,
                ),
              ),
              const SectionHeader(title: 'Colour'),
              if (seed != null)
                SwitchListTile(
                  secondary: _Swatch(label: 'Wallpaper colours', colors: _wallpaperColors(seed, theme.brightness), selected: false, size: 36),
                  title: const Text('Match wallpaper'),
                  subtitle: const Text("Colours from this phone's wallpaper"),
                  value: wallpaperOn,
                  onChanged: (on) => c.set(a.copyWith(wallpaper: on)),
                ),
              Padding(
                padding: EdgeInsets.fromLTRB(gutter - Space.xs, Space.xs, gutter - Space.xs, Space.xl),
                child: AnimatedOpacity(
                  opacity: wallpaperOn ? .5 : 1,
                  duration: Motion.of(context).short,
                  child: _SwatchGrid(
                    selected: wallpaperOn ? null : a.accent,
                    onPick: (accent) => c.set(a.copyWith(accent: accent, wallpaper: false)),
                  ),
                ),
              ),
            ]),
          ],
        );
      },
    );
  }

  /// What true black keeps of each style: it is that style on #000, not
  /// a generic dark theme.
  static String _blackNote(DesignDirection d) => switch (d) {
        DesignDirection.tonal => 'Cards keep their tonal colour, dimmed.',
        DesignDirection.console => 'Panels are ruled off by hairlines, like a terminal.',
        _ => 'Cards keep their graphite fill and hairline edge.',
      };

  static List<Color> _wallpaperColors(Color seed, Brightness b) {
    final s = ColorScheme.fromSeed(seedColor: seed, brightness: b);
    return [s.primary, s.secondary, s.tertiary];
  }
}

/// Two Home cards drawn in the current appearance, so every choice below
/// shows on the real card at once. A fixed line shape, labelled as a
/// preview for TalkBack, never passed off as a reading.
class _Preview extends StatelessWidget {
  const _Preview();

  static const _cpu = <double>[8, 12, 10, 18, 14, 22, 30, 26, 19, 24, 34, 28, 22, 26, 31, 27, 24, 29, 36, 33];
  static const _mem = <double>[41, 41, 42, 42, 42, 43, 42, 42, 43, 44, 44, 43, 43, 44, 44, 45, 44, 44, 45, 45];

  @override
  Widget build(BuildContext context) {
    final gap = DesignTokens.of(context).gap;
    final inset = DesignTokens.of(context).cardPadding.left;
    Widget card(String label, IconData icon, String value, MetricLevel? level, String detail, List<double> v, {bool emphasized = false}) => MetricCard(
          icon: icon,
          label: label,
          value: value,
          unit: '%',
          level: level,
          detail: detail,
          emphasized: emphasized,
          semanticLabel: label,
          body: LiveChart(series: [ChartSeries(v)], capacity: v.length, max: 100, window: '2 min', bleed: true, labelInset: inset),
        );
    final scale = MediaQuery.textScalerOf(context).scale(10) / 10;
    return Semantics(
      label: 'Preview of the Home cards in this appearance',
      excludeSemantics: true,
      child: SizedBox(
        height: 196 * scale.clamp(1.0, 2.0),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Expanded(child: card('Processor', Icons.memory_outlined, '33', const MetricLevel('Light'), '4.2 GHz', _cpu)),
            SizedBox(width: gap),
            Expanded(child: card('Memory', Icons.developer_board_outlined, '45', null, '14 of 31 GB', _mem, emphasized: true)),
          ],
        ),
      ),
    );
  }
}

/// The stock controls under the preview cards - the primary and a quiet
/// button, a selected chip, a switch that is on - so a style or colour
/// shows on the whole app at once, not only on Home's cards (Monochrome's
/// ink switch and chip included). A picture, not controls: TalkBack skips
/// it and taps pass through.
class _Controls extends StatelessWidget {
  const _Controls();

  @override
  Widget build(BuildContext context) {
    void nothing([Object? _]) {}
    return ExcludeSemantics(
      child: IgnorePointer(
        child: Wrap(
          spacing: Space.sm,
          runSpacing: Space.sm,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            FilledButton(onPressed: nothing, child: const Text('Start')),
            OutlinedButton(onPressed: nothing, child: const Text('Logs')),
            FilterChip(label: const Text('Running'), selected: true, onSelected: nothing),
            Switch(value: true, onChanged: nothing),
          ],
        ),
      ),
    );
  }
}

/// The directions side by side, each drawn in its own theme: a small card
/// with its type, number and line. Selection is an outline plus a check.
class _StyleRow extends StatelessWidget {
  const _StyleRow({required this.appearance, required this.seed, required this.onPick});

  final Appearance appearance;
  final Color? seed;
  final ValueChanged<DesignDirection> onPick;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final d in DesignDirection.selectable) ...[
          if (d != DesignDirection.selectable.first) const SizedBox(width: Space.sm),
          Expanded(
            child: Semantics(
              button: true,
              selected: appearance.direction == d,
              label: '${d.label} design. ${d.description}${d == DesignDirection.defaultDirection ? '. Recommended' : ''}',
              excludeSemantics: true,
              child: InkWell(
                onTap: () => onPick(d),
                borderRadius: BorderRadius.circular(DesignTokens.of(context).radii.lg),
                child: Padding(
                  padding: const EdgeInsets.only(bottom: Space.xs),
                  child: Column(
                    children: [
                      _StyleSample(direction: d, appearance: appearance, seed: seed, selected: appearance.direction == d),
                      const SizedBox(height: Space.sm),
                      Row(
                        mainAxisAlignment: MainAxisAlignment.center,
                        children: [
                          if (appearance.direction == d) ...[Icon(Icons.check, size: 16, color: scheme.primary), const SizedBox(width: 2)],
                          Flexible(
                            child: Text(
                              d.label,
                              style: text.labelLarge?.copyWith(color: appearance.direction == d ? scheme.primary : scheme.onSurface),
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                        ],
                      ),
                      if (d == DesignDirection.defaultDirection)
                        Text('Recommended', style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant), maxLines: 1, overflow: TextOverflow.ellipsis),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ],
      ],
    );
  }
}

/// The accents - Monochrome first, then the eight colours - as a 3 × 3
/// grid of 48dp swatches, each named under it, so none is left alone on a
/// row and each name has room at large text. A swatch shows the accent as
/// it draws in this theme (Monochrome is dark ink on light, light on dark).
class _SwatchGrid extends StatelessWidget {
  const _SwatchGrid({required this.selected, required this.onPick});

  final AccentColor? selected;
  final ValueChanged<AccentColor> onPick;

  @override
  Widget build(BuildContext context) {
    final text = Theme.of(context).textTheme;
    final scheme = Theme.of(context).colorScheme;
    const perRow = 3;
    final brightness = Theme.of(context).brightness;
    final rows = [for (var i = 0; i < AccentColor.values.length; i += perRow) AccentColor.values.sublist(i, (i + perRow).clamp(0, AccentColor.values.length))];
    return Column(
      children: [
        for (final row in rows)
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              for (final accent in row)
                Expanded(
                  child: Column(
                    children: [
                      _Swatch(label: accent.label, colors: [accent.swatch(brightness)], selected: selected == accent, onTap: () => onPick(accent)),
                      ExcludeSemantics(
                        child: Text(
                          accent.label,
                          textAlign: TextAlign.center,
                          maxLines: 2,
                          style: text.labelSmall?.copyWith(color: selected == accent ? scheme.onSurface : scheme.onSurfaceVariant, fontWeight: selected == accent ? FontWeight.w600 : null),
                        ),
                      ),
                      const SizedBox(height: Space.sm),
                    ],
                  ),
                ),
            ],
          ),
      ],
    );
  }
}

/// The four modes as small pictures of the app, each drawn with that
/// mode's real colours. Selection is an outline plus a check, not colour.
class _ModeRow extends StatelessWidget {
  const _ModeRow({required this.appearance, required this.seed, required this.onPick});

  final Appearance appearance;
  final Color? seed;
  final ValueChanged<AppThemeMode> onPick;

  @override
  Widget build(BuildContext context) {
    ThemeData theme(AppThemeMode m, Brightness b) => AppTheme.forAppearance(appearance.copyWith(mode: m), b, wallpaperSeed: seed);
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final m in AppThemeMode.values) ...[
          if (m != AppThemeMode.values.first) const SizedBox(width: Space.sm),
          Expanded(
            child: _ModeTile(
              mode: m,
              // System follows the phone, so it shows both, whatever the
              // phone is set to now: light and dark split on the diagonal.
              theme: theme(m, m == AppThemeMode.light || m == AppThemeMode.system ? Brightness.light : Brightness.dark),
              darkTheme: m == AppThemeMode.system ? theme(m, Brightness.dark) : null,
              selected: appearance.mode == m,
              onTap: () => onPick(m),
            ),
          ),
        ],
      ],
    );
  }
}

class _ModeTile extends StatelessWidget {
  const _ModeTile({required this.mode, required this.theme, this.darkTheme, required this.selected, required this.onTap});

  final AppThemeMode mode;
  final ThemeData theme;

  /// With it, the picture is [theme] above the diagonal and this below.
  final ThemeData? darkTheme;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Semantics(
      button: true,
      selected: selected,
      label: '${mode.label} mode',
      excludeSemantics: true,
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(DesignTokens.of(context).radii.lg),
        child: Padding(
          padding: const EdgeInsets.only(bottom: Space.xs),
          child: Column(
            children: [
              DecoratedBox(
                position: DecorationPosition.foreground,
                decoration: BoxDecoration(
                  borderRadius: BorderRadius.circular(DesignTokens.of(context).radii.lg),
                  border: Border.all(color: selected ? scheme.primary : scheme.outlineVariant, width: selected ? 2.5 : 1),
                ),
                child: ClipRRect(
                  borderRadius: BorderRadius.circular(DesignTokens.of(context).radii.lg),
                  child: AspectRatio(
                    aspectRatio: .62,
                    child: darkTheme == null
                        ? _MiniApp(theme: theme)
                        : Stack(fit: StackFit.expand, children: [
                            _MiniApp(theme: theme),
                            ClipPath(clipper: const _LowerRightHalf(), child: _MiniApp(theme: darkTheme!)),
                          ]),
                  ),
                ),
              ),
              const SizedBox(height: Space.sm),
              Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  if (selected) ...[Icon(Icons.check, size: 16, color: scheme.primary), const SizedBox(width: 2)],
                  Flexible(
                    child: Text(
                      mode == AppThemeMode.system ? 'System' : mode.label,
                      style: text.labelLarge?.copyWith(color: selected ? scheme.primary : scheme.onSurface),
                      textAlign: TextAlign.center,
                      maxLines: 2,
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// The triangle below the diagonal from the top-right to the bottom-left
/// corner.
class _LowerRightHalf extends CustomClipper<Path> {
  const _LowerRightHalf();

  @override
  Path getClip(Size size) => Path()
    ..moveTo(size.width, 0)
    ..lineTo(size.width, size.height)
    ..lineTo(0, size.height)
    ..close();

  @override
  bool shouldReclip(_LowerRightHalf oldClipper) => false;
}

/// A picture of Home in [theme]: a status strip, a metric card with its
/// line, two list rows and the navigation bar. Shapes only - no numbers,
/// so it can't be mistaken for real readings.
class _MiniApp extends StatelessWidget {
  const _MiniApp({required this.theme});

  final ThemeData theme;

  // A decorative line shape for the picture, not data.
  static const _shape = <double>[3, 5, 4, 7, 6, 9, 7, 5, 6, 8, 12, 9, 7, 8, 6];

  @override
  Widget build(BuildContext context) {
    final s = theme.colorScheme;
    final t = theme.extension<DesignTokens>() ?? DesignTokens.fallback(theme);
    Widget bar(double w, Color c, [double h = 5]) => FractionallySizedBox(
          widthFactor: w,
          alignment: Alignment.centerLeft,
          child: Container(height: h, decoration: BoxDecoration(color: c, borderRadius: BorderRadius.circular(h / 2))),
        );
    // The navigation indicator's corner, scaled down: a pill in Tonal (and
    // v2), the style's small corner elsewhere.
    final indicator = t.direction == DesignDirection.tonal || t.direction == DesignDirection.v2 ? 3.5 : t.radii.sm / 3;
    final card = BoxDecoration(
      color: t.cardColor,
      borderRadius: BorderRadius.circular(t.cardRadius / 2.5),
      border: t.cardBorder == null ? null : Border.all(color: t.cardBorder!, width: .75),
    );
    return Theme(
      data: theme,
      child: ColoredBox(
        color: s.surface,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const SizedBox(height: 10),
            Padding(padding: const EdgeInsets.symmetric(horizontal: 8), child: bar(.45, s.onSurface, 6)),
            const SizedBox(height: 8),
            Container(
              margin: const EdgeInsets.symmetric(horizontal: 6),
              padding: const EdgeInsets.fromLTRB(6, 6, 6, 4),
              decoration: card,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  bar(.4, s.onSurfaceVariant.withValues(alpha: .6), 3),
                  const SizedBox(height: 4),
                  bar(.3, s.onSurface, 7),
                  const SizedBox(height: 4),
                  LiveChart(series: const [ChartSeries(_shape)], capacity: _shape.length, max: 14, height: 18),
                ],
              ),
            ),
            const SizedBox(height: 5),
            for (final w in const [.7, .55])
              Container(
                margin: const EdgeInsets.fromLTRB(6, 0, 6, 3),
                padding: const EdgeInsets.all(6),
                decoration: card.copyWith(color: s.surfaceContainer),
                child: bar(w, s.onSurfaceVariant.withValues(alpha: .5), 3),
              ),
            const Spacer(),
            Container(
              height: 18,
              color: theme.navigationBarTheme.backgroundColor ?? s.surfaceContainer,
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                children: [
                  Container(width: 14, height: 7, decoration: BoxDecoration(color: s.secondaryContainer, borderRadius: BorderRadius.circular(indicator))),
                  for (var i = 0; i < 3; i++)
                    Container(width: 5, height: 5, decoration: BoxDecoration(color: s.onSurfaceVariant, shape: BoxShape.circle)),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// A 48dp colour swatch; the selected one carries a check. Without
/// [onTap] it is a picture only (the wallpaper switch's icon).
class _Swatch extends StatelessWidget {
  const _Swatch({required this.label, required this.colors, required this.selected, this.onTap, this.size = 44});

  final String label;
  final List<Color> colors;
  final bool selected;
  final VoidCallback? onTap;
  final double size;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final main = colors.first;
    final check = ThemeData.estimateBrightnessForColor(main) == Brightness.dark ? Colors.white : Colors.black;
    final dot = Container(
      width: size,
      height: size,
      padding: const EdgeInsets.all(3),
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        border: Border.all(color: selected ? scheme.onSurface : Colors.transparent, width: 2),
      ),
      child: ClipOval(
        child: Stack(fit: StackFit.expand, children: [
          if (colors.length == 1)
            ColoredBox(color: main)
          else
            Row(children: [
              Expanded(child: ColoredBox(color: colors[0])),
              Expanded(
                child: Column(children: [
                  Expanded(child: ColoredBox(color: colors[1])),
                  Expanded(child: ColoredBox(color: colors[2])),
                ]),
              ),
            ]),
          if (selected) Icon(Icons.check, size: 20, color: check),
        ]),
      ),
    );
    if (onTap == null) return ExcludeSemantics(child: dot);
    return Tooltip(
      message: label,
      child: Semantics(
        button: true,
        selected: selected,
        label: '$label${colors.length > 1 ? '' : ' accent'}',
        excludeSemantics: true,
        child: InkResponse(
          onTap: onTap,
          radius: 28,
          child: SizedBox(width: 52, height: 52, child: Center(child: dot)),
        ),
      ),
    );
  }
}

/// A style as a sample of itself, in its own theme: its card, corner and
/// edge, its name type, its number type and its line.
class _StyleSample extends StatelessWidget {
  const _StyleSample({required this.direction, required this.appearance, required this.seed, required this.selected});

  final DesignDirection direction;
  final Appearance appearance;
  final Color? seed;
  final bool selected;

  @override
  Widget build(BuildContext context) {
    final theme = AppTheme.forAppearance(appearance.copyWith(direction: direction), Theme.of(context).brightness, wallpaperSeed: seed);
    final t = theme.extension<DesignTokens>()!;
    final s = theme.colorScheme;
    final outer = Theme.of(context).colorScheme;
    final scale = MediaQuery.textScalerOf(context).scale(10) / 10;
    return ExcludeSemantics(
      child: Theme(
        data: theme,
        child: Container(
          height: 112 * scale.clamp(1.0, 1.6),
          padding: const EdgeInsets.all(4),
          decoration: BoxDecoration(
            color: s.surface,
            borderRadius: BorderRadius.circular(DesignTokens.of(context).radii.lg),
            border: Border.all(color: selected ? outer.primary : outer.outlineVariant, width: selected ? 2.5 : 1),
          ),
          child: Material(
            color: t.cardColor,
            shape: t.cardShape(t.cardRadius.clamp(0, 14)),
            clipBehavior: Clip.antiAlias,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(10, 8, 10, 0),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text('Processor', style: t.cardLabel.copyWith(fontSize: 11), maxLines: 1, overflow: TextOverflow.ellipsis),
                      MetricValue(value: '42', unit: '%', style: t.heroValue.copyWith(fontSize: 28, letterSpacing: -0.5)),
                    ],
                  ),
                ),
                Expanded(
                  child: LiveChart(series: const [ChartSeries(_MiniApp._shape)], capacity: _MiniApp._shape.length, max: 14, bleed: true, labelInset: 10),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
