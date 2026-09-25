// A style styles the whole app (owner requests, 2026-09-26): every stock
// component takes the style's shape, edges and type from the theme; true
// black keeps each style's character; Monochrome is ink with no hue.
// Contrast of every pair lives in theme_contrast_test.dart.
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/theme/app_theme.dart';
import 'package:nivaroos_mobile/ui/theme/appearance.dart';
import 'package:nivaroos_mobile/ui/theme/design_tokens.dart';

import 'theme_contrast_test.dart' show contrast;

const _modes = [('light', Brightness.light, false), ('dark', Brightness.dark, false), ('black', Brightness.dark, true)];

ThemeData _theme(DesignDirection d, String mode, {AccentColor accent = AccentColor.blue}) {
  final (_, b, black) = _modes.firstWhere((m) => m.$1 == mode);
  return AppTheme.build(brightness: b, black: black, accent: accent, direction: d);
}

double _radius(ShapeBorder? shape) => switch (shape) {
      RoundedRectangleBorder(:final borderRadius) => (borderRadius as BorderRadius).topLeft.x,
      StadiumBorder() => double.infinity,
      _ => throw StateError('unexpected shape $shape'),
    };

/// How far a colour is from grey: the spread of its channels, 0-1.
double _hue(Color c) => [c.r, c.g, c.b].reduce(math.max) - [c.r, c.g, c.b].reduce(math.min);

void main() {
  group('every style owns the stock components', () {
    for (final d in DesignDirection.selectable) {
      for (final (mode, _, _) in _modes) {
        test('${d.name} $mode', () {
          final t = _theme(d, mode);
          final family = t.textTheme.bodyMedium!.fontFamily;
          expect(family, isNotNull);
          // The style's face everywhere the components set their own type.
          for (final style in [
            t.appBarTheme.titleTextStyle,
            t.listTileTheme.titleTextStyle,
            t.dialogTheme.contentTextStyle,
            t.snackBarTheme.contentTextStyle,
            t.inputDecorationTheme.labelStyle,
            t.navigationBarTheme.labelTextStyle!.resolve({}),
            t.chipTheme.labelStyle,
          ]) {
            expect(style!.fontFamily, family);
          }
          // Each component theme is set, not left to the Material default.
          expect(t.filledButtonTheme.style, isNotNull);
          expect(t.outlinedButtonTheme.style, isNotNull);
          expect(t.textButtonTheme.style, isNotNull);
          expect(t.iconButtonTheme.style, isNotNull);
          expect(t.segmentedButtonTheme.style, isNotNull);
          expect(t.navigationBarTheme.indicatorShape, isNotNull);
          expect(t.navigationRailTheme.indicatorShape, isNotNull);
          expect(t.chipTheme.shape, isNotNull);
          expect(t.checkboxTheme.shape, isNotNull);
          expect(t.radioTheme.fillColor, isNotNull);
          expect(t.sliderTheme.activeTrackColor, isNotNull);
          expect(t.progressIndicatorTheme.color, isNotNull);
          expect(t.inputDecorationTheme.enabledBorder, isNotNull);
          expect(t.dialogTheme.shape, isNotNull);
          expect(t.bottomSheetTheme.shape, isNotNull);
          expect(t.snackBarTheme.shape, isNotNull);
          expect(t.popupMenuTheme.shape, isNotNull);
          expect(t.menuTheme.style?.shape, isNotNull);
          expect(t.searchBarTheme.shape, isNotNull);
          expect(t.tabBarTheme.indicator, isNotNull);
          expect(t.tooltipTheme.decoration, isNotNull);
          expect(t.cardTheme.shape, isNotNull);
          expect(t.drawerTheme.shape, isNotNull);
          expect(t.dividerTheme.color, t.colorScheme.outlineVariant);
          expect(t.floatingActionButtonTheme.shape, isNotNull);
          // Radii come from the style's scale, which the tokens carry too.
          expect(_radius(t.dialogTheme.shape), t.extension<DesignTokens>()!.radii.xl);
          expect(_radius(t.chipTheme.shape), t.extension<DesignTokens>()!.radii.sm);
        });
      }
    }

    test('the three styles differ in shape, density and edges', () {
      final rack = _theme(DesignDirection.rack, 'light');
      final tonal = _theme(DesignDirection.tonal, 'light');
      final console = _theme(DesignDirection.console, 'light');
      final dialogs = {for (final t in [rack, tonal, console]) _radius(t.dialogTheme.shape)};
      expect(dialogs, hasLength(3));
      final chips = {for (final t in [rack, tonal, console]) _radius(t.chipTheme.shape)};
      expect(chips, hasLength(3));
      expect(tonal.filledButtonTheme.style!.shape, isNull, reason: 'Tonal keeps the M3 stadium');
      expect(tonal.outlinedButtonTheme.style!.shape!.resolve({}), isA<StadiumBorder>());
      expect(console.visualDensity.vertical, lessThan(rack.visualDensity.vertical), reason: 'Console is a step denser');
      // Console rules the app bar off; the others don't.
      expect(console.appBarTheme.shape, isA<Border>());
      expect(rack.appBarTheme.shape, isNull);
      // Rack and Console edge their floating surfaces; Tonal doesn't.
      expect((rack.dialogTheme.shape! as RoundedRectangleBorder).side, isNot(BorderSide.none));
      expect((console.dialogTheme.shape! as RoundedRectangleBorder).side, isNot(BorderSide.none));
      expect((tonal.dialogTheme.shape! as RoundedRectangleBorder).side, BorderSide.none);
      // Tonal's text fields are filled; the others outlined.
      expect(tonal.inputDecorationTheme.filled, isTrue);
      expect(rack.inputDecorationTheme.border, isA<OutlineInputBorder>());
      // Console's actions are in its mono face.
      expect(console.filledButtonTheme.style!.textStyle!.resolve({})!.fontFamily, 'IBMPlexMono');
    });

    test('v2 keeps the Material defaults it shipped with', () {
      final t = _theme(DesignDirection.v2, 'light');
      expect(t.filledButtonTheme.style, isNull);
      expect(t.chipTheme.shape, isNull);
      expect(t.switchTheme.trackColor, isNull);
    });
  });

  group('true black keeps each style', () {
    for (final d in DesignDirection.selectable) {
      test('${d.name}: black is the dark style on #000, same shapes and type', () {
        final dark = _theme(d, 'dark');
        final black = _theme(d, 'black');
        expect(black.colorScheme.surface, Colors.black);
        expect(black.scaffoldBackgroundColor, Colors.black);
        expect(black.textTheme.bodyMedium!.fontFamily, dark.textTheme.bodyMedium!.fontFamily);
        expect(black.dialogTheme.shape, isA<RoundedRectangleBorder>());
        expect(_radius(black.dialogTheme.shape), _radius(dark.dialogTheme.shape));
        expect(black.filledButtonTheme.style!.shape?.resolve({}), dark.filledButtonTheme.style!.shape?.resolve({}));
        expect(black.visualDensity, dark.visualDensity);
        final bt = black.extension<DesignTokens>()!, dt = dark.extension<DesignTokens>()!;
        expect(bt.button, dt.button);
        expect(bt.ruledGroups, dt.ruledGroups);
        expect(bt.chart.inkLine, dt.chart.inkLine);
        expect(bt.cardRadius, dt.cardRadius);
        // No scroll-under tint: on black it would turn navy.
        expect(black.appBarTheme.surfaceTintColor, Colors.transparent);
      });
    }

    test('Rack: graphite cards with hairline edges, warm like its dark', () {
      final t = _theme(DesignDirection.rack, 'black');
      final tk = t.extension<DesignTokens>()!;
      expect(tk.cardBorder, isNotNull);
      expect(tk.cardColor, isNot(Colors.black));
      for (final c in [t.colorScheme.surfaceContainer, t.colorScheme.surfaceContainerHigh, t.colorScheme.outlineVariant]) {
        expect(c.r, greaterThanOrEqualTo(c.b), reason: 'graphite stays warm: $c');
      }
    });

    test('Tonal: tonal cards with no edge, dimmed rather than flattened', () {
      for (final a in [AccentColor.blue, AccentColor.violet, AccentColor.teal]) {
        final dark = _theme(DesignDirection.tonal, 'dark', accent: a);
        final t = _theme(DesignDirection.tonal, 'black', accent: a);
        final tk = t.extension<DesignTokens>()!;
        expect(tk.cardBorder, isNull, reason: 'Tonal separates by tone, not lines');
        expect(tk.segmentBorder, isNull);
        expect(contrast(tk.cardColor, Colors.black), greaterThanOrEqualTo(1.2), reason: 'cards still show on black');
        // Dimmed: darker than dark's card, but the accent's tint survives.
        final darkCard = dark.extension<DesignTokens>()!.cardColor;
        expect(tk.cardColor.computeLuminance(), lessThan(darkCard.computeLuminance()));
        expect(_hue(tk.cardColor), greaterThan(0), reason: '${a.name}: the card keeps a tint');
        // The loud moment stays: the emphasised card is still the primary container.
        expect(tk.emphasisCard, t.colorScheme.primaryContainer);
      }
    });

    test('Console: the terminal look - #000 panels ruled by hairlines', () {
      final t = _theme(DesignDirection.console, 'black');
      final tk = t.extension<DesignTokens>()!;
      expect(tk.cardColor, Colors.black);
      expect(tk.cardBorder, isNotNull);
      expect(contrast(tk.cardBorder!, Colors.black), greaterThanOrEqualTo(1.3), reason: 'the rule has to show on its own');
      expect(tk.chart.grid, isTrue);
      expect(t.appBarTheme.shape, isA<Border>());
      expect(t.navigationBarTheme.backgroundColor, Colors.black);
    });
  });

  group('Monochrome', () {
    test('is the first colour', () {
      expect(AccentColor.values.first, AccentColor.mono);
      expect(AccentColor.mono.label, 'Monochrome');
    });

    test('its swatch is ink: dark on light, light on dark', () {
      expect(AccentColor.mono.swatch(Brightness.light).computeLuminance(), lessThan(.05));
      expect(AccentColor.mono.swatch(Brightness.dark).computeLuminance(), greaterThan(.7));
      expect(AccentColor.teal.swatch(Brightness.dark), AccentColor.teal.seed);
    });

    for (final d in DesignDirection.selectable) {
      for (final (mode, _, _) in _modes) {
        test('${d.name} $mode: the accent is the ink, with no hue', () {
          final t = _theme(d, mode, accent: AccentColor.mono);
          final s = t.colorScheme;
          expect(s.primary, s.onSurface);
          expect(s.onPrimary, s.surface);
          if (mode == 'light') {
            expect(s.primary.computeLuminance(), lessThan(.05), reason: 'near-black on light');
          } else {
            expect(s.primary.computeLuminance(), greaterThan(.6), reason: 'near-white on dark and black');
          }
          // Rack's paper and Console's graphite keep their slight warmth or
          // coolness; nothing carries an accent's hue.
          for (final (role, c) in [
            ('primary', s.primary),
            ('primaryContainer', s.primaryContainer),
            ('secondary', s.secondary),
            ('secondaryContainer', s.secondaryContainer),
            ('tertiary', s.tertiary),
            ('tertiaryContainer', s.tertiaryContainer),
          ]) {
            expect(_hue(c), lessThan(.08), reason: '$role $c has a hue');
          }
          // Selection shows: where a style marks it with a fill (Rack's ink,
          // Console's primary container), that fill stands clear of the
          // page, not one grey on another. Tonal adds a check to its tonal
          // fill, as M3 does.
          if (d != DesignDirection.tonal) {
            final fill = t.chipTheme.color!.resolve({WidgetState.selected})!;
            expect(contrast(fill, s.surface), greaterThanOrEqualTo(3), reason: 'selected chip on the page');
            final seg = t.segmentedButtonTheme.style!.backgroundColor!.resolve({WidgetState.selected})!;
            expect(contrast(seg, s.surface), greaterThanOrEqualTo(3), reason: 'selected segment on the page');
          }
          expect(t.chipTheme.showCheckmark ?? true, isTrue);
          // The status colours keep theirs: they carry meaning.
          expect(_hue(s.error), greaterThan(.3));
        });
      }
    }
  });

  group('stock components build in every style, mode and accent', () {
    for (final d in DesignDirection.selectable) {
      for (final (mode, _, _) in _modes) {
        for (final a in [AccentColor.mono, AccentColor.blue]) {
          testWidgets('${d.name} $mode ${a.name}', (tester) async {
            await tester.pumpWidget(MaterialApp(theme: _theme(d, mode, accent: a), home: const _Gallery()));
            expect(tester.takeException(), isNull);
            // The selected switch draws in the accent, which for Monochrome
            // is the ink: never invisible against the page.
            final theme = Theme.of(tester.element(find.byType(Switch).first));
            final track = theme.switchTheme.trackColor?.resolve({WidgetState.selected}) ?? theme.colorScheme.primary;
            expect(contrast(track, theme.colorScheme.surface), greaterThanOrEqualTo(3));
          });
        }
      }
    }
  });
}

class _Gallery extends StatelessWidget {
  const _Gallery();

  @override
  Widget build(BuildContext context) {
    return DefaultTabController(
      length: 2,
      child: Scaffold(
        appBar: AppBar(title: const Text('Gallery'), bottom: const TabBar(tabs: [Tab(text: 'One'), Tab(text: 'Two')])),
        floatingActionButton: FloatingActionButton(onPressed: () {}, child: const Icon(Icons.add)),
        bottomNavigationBar: NavigationBar(destinations: const [
          NavigationDestination(icon: Icon(Icons.home_outlined), label: 'Home'),
          NavigationDestination(icon: Icon(Icons.apps_outlined), label: 'Apps'),
        ]),
        body: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            const ListTile(leading: Icon(Icons.dns_outlined), title: Text('Server'), subtitle: Text('192.168.1.20'), trailing: Text('12 GB')),
            SwitchListTile(value: true, onChanged: (_) {}, title: const Text('On')),
            SwitchListTile(value: false, onChanged: (_) {}, title: const Text('Off')),
            CheckboxListTile(value: true, onChanged: (_) {}, title: const Text('Checked')),
            RadioGroup<int>(groupValue: 1, onChanged: (_) {}, child: const Row(children: [Radio<int>(value: 1), Radio<int>(value: 2)])),
            Slider(value: .4, onChanged: (_) {}),
            SegmentedButton<int>(segments: const [ButtonSegment(value: 1, label: Text('2 s')), ButtonSegment(value: 2, label: Text('4 s'))], selected: const {1}, onSelectionChanged: (_) {}),
            Wrap(spacing: 8, children: [
              FilterChip(label: const Text('Running'), selected: true, onSelected: (_) {}),
              ChoiceChip(label: const Text('Stopped'), selected: false, onSelected: (_) {}),
              ActionChip(label: const Text('Refresh'), onPressed: () {}),
            ]),
            Wrap(spacing: 8, children: [
              FilledButton(onPressed: () {}, child: const Text('Save')),
              FilledButton.tonal(onPressed: () {}, style: tonalButtonStyle(context), child: const Text('Tonal')),
              OutlinedButton(onPressed: () {}, child: const Text('Cancel')),
              TextButton(onPressed: () {}, child: const Text('Retry')),
              IconButton(onPressed: () {}, icon: const Icon(Icons.more_vert)),
              const Tooltip(message: 'Help', child: Icon(Icons.help_outline)),
            ]),
            const TextField(decoration: InputDecoration(labelText: 'Host', hintText: 'nas.example.com')),
            const SearchBar(hintText: 'Search'),
            const LinearProgressIndicator(value: .5),
            const CircularProgressIndicator(value: .5),
            const Divider(),
            const Card(child: Padding(padding: EdgeInsets.all(16), child: Text('Card'))),
            AlertDialog(title: const Text('Delete?'), content: const Text('It goes to the Trash.'), actions: [TextButton(onPressed: () {}, child: const Text('Delete'))]),
            MenuAnchor(menuChildren: [MenuItemButton(onPressed: () {}, child: const Text('Open'))], builder: (_, c, _) => TextButton(onPressed: c.open, child: const Text('Menu'))),
          ],
        ),
      ),
    );
  }
}
