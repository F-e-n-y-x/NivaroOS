// Design system v3: the same screens drawn in each design direction, for
// the owner to choose from (design brief "Design system v3 - directions").
// One code path; only the theme differs.
//
//   flutter test test/screenshots/directions_test.dart --update-goldens
//
// PNGs: goldens/directions/<direction>/<screen>_<mode>_<size>.png, and -
// with NVOS_COMPARE=1 - one side-by-side comparison per screen, mode and
// size in goldens/directions/compare/ (the 1.3 look first for reference,
// then Rack, Tonal, Console).
import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/container_entry.dart';
import 'package:nivaroos_mobile/models/dashboard_stats.dart';
import 'package:nivaroos_mobile/screens/apps_screen.dart';
import 'package:nivaroos_mobile/screens/dashboard_screen.dart';
import 'package:nivaroos_mobile/screens/files/trash_screen.dart';
import 'package:nivaroos_mobile/screens/files_screen.dart';
import 'package:nivaroos_mobile/screens/settings_screen.dart';
import 'package:nivaroos_mobile/ui/ui.dart';
import 'package:nivaroos_mobile/widgets/monitor_modals.dart';

import 'files_screens_test.dart' show copyThenWorkTab, docsAndWork;
import 'harness.dart';
import 'home_test.dart' show HistoryController, fillHistory, openFreeMemory, runningVms, scrollToVms;

const _jellyfin = InstalledApp(
  id: 'jellyfin',
  title: 'Jellyfin',
  kind: AppKind.compose,
  status: 'running',
  image: 'linuxserver/jellyfin:10.11.10',
  scheme: 'http',
  port: '8097',
  index: '/',
  storeAppId: 'jellyfin',
  icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Jellyfin/icon.svg',
  statusText: 'Up 10 hours',
  hasUpdate: true,
);

/// Each direction's signature accent, for one extra Home shot.
const _signature = {
  DesignDirection.v2: (AccentColor.teal, AppThemeMode.light),
  DesignDirection.rack: (AccentColor.ember, AppThemeMode.light),
  DesignDirection.tonal: (AccentColor.violet, AppThemeMode.light),
  DesignDirection.console: (AccentColor.lime, AppThemeMode.dark),
};

LiveStats _live() {
  final util = fixture('v1/sys/utilization')['data'] as Map<String, dynamic>;
  final disks = (fixture('v1/sys/disks-usage')['data'] as List).map((e) => DiskUsage.fromJson(e as Map<String, dynamic>)).toList();
  return LiveStats(
    stats: DashboardStats.fromUtilization(util).withDisks(disks),
    rate: const NetRate(upBytesPerSec: 48300, downBytesPerSec: 2415000),
    updatedAt: shotTime,
  );
}

class _Screen {
  const _Screen(this.build,
      {this.modes = const [AppThemeMode.light], this.extra = const [], this.tab = false, this.pushed = false, this.overrides = const {}, this.before});
  final Widget Function() build;
  final List<AppThemeMode> modes;

  /// More sizes and text scales, shot in the first mode only.
  final List<(Size, double)> extra;

  final bool tab;
  final bool pushed;
  final Map<String, Object> overrides;
  final Future<void> Function(WidgetTester)? before;
}

final Map<String, _Screen> _screens = {
  'home': _Screen(() => DashboardScreen(controller: HistoryController()),
      tab: true,
      overrides: runningVms,
      modes: const [AppThemeMode.light, AppThemeMode.dark, AppThemeMode.black],
      extra: const [(smallPhone, 1), (phone, 2)]),
  'home_more': _Screen(
    () => DashboardScreen(controller: HistoryController()),
    tab: true,
    overrides: runningVms,
    modes: const [AppThemeMode.light, AppThemeMode.dark],
    before: (tester) async {
      await tester.drag(find.byType(Scrollable).first, const Offset(0, -760));
      await tester.pump(const Duration(seconds: 1));
    },
  ),
  // The one running VM's console preview, in every mode.
  'home_vm_preview': _Screen(
    () => DashboardScreen(controller: HistoryController()),
    tab: true,
    overrides: runningVms,
    modes: const [AppThemeMode.light, AppThemeMode.dark, AppThemeMode.black],
    before: scrollToVms,
  ),
  'cpu': _Screen(() {
    final h = LiveHistory();
    fillHistory(h);
    return CpuDetailScreen(live: ValueNotifier(_live()), onRetry: () {}, history: h);
  }, pushed: true, modes: const [AppThemeMode.light, AppThemeMode.dark]),
  // Memory > Free up memory: the confirm sheet in every style and mode.
  'memory_free': _Screen(() {
    final h = LiveHistory();
    fillHistory(h);
    return MemoryDetailScreen(live: ValueNotifier(_live()), onRetry: () {}, history: h);
  }, pushed: true, modes: const [AppThemeMode.light, AppThemeMode.dark, AppThemeMode.black], before: openFreeMemory),
  'files': _Screen(() => const FilesScreen(), tab: true, modes: const [AppThemeMode.light, AppThemeMode.dark]),
  // Two tabs sharing a clipboard: the strip and the paste bar.
  'files_tabs': _Screen(() => const FilesScreen(initialPath: '/DATA/Documents'),
      tab: true, overrides: docsAndWork, modes: const [AppThemeMode.light, AppThemeMode.dark, AppThemeMode.black], before: copyThenWorkTab),
  // Settings > Home > Refresh widgets.
  'settings_refresh': _Screen(() => const SettingsScreen(), pushed: true, modes: const [AppThemeMode.light, AppThemeMode.black], before: (tester) async {
    await tester.tap(find.text('Refresh widgets'));
    await tester.pump();
    await tester.pump(const Duration(seconds: 1));
  }),
  'trash': _Screen(() => const TrashScreen(), pushed: true, modes: const [AppThemeMode.light, AppThemeMode.dark], extra: const [(smallPhone, 1)], overrides: {
    'GET /v1/trash/support': fixture('files/trash_support'),
  }),
  'trash_item': _Screen(() => const TrashScreen(), pushed: true, modes: const [AppThemeMode.light, AppThemeMode.dark], overrides: {
    'GET /v1/trash/support': fixture('files/trash_support'),
  }, before: (tester) async {
    await tester.scrollUntilVisible(find.text('Old invoices'), 200, scrollable: find.byType(Scrollable).first);
    await tester.pump(const Duration(seconds: 1));
    await tester.tap(find.text('Old invoices'));
    await tester.pump(const Duration(seconds: 1));
  }),
  'appearance': _Screen(() => const SizedBox(), pushed: true, modes: const [AppThemeMode.light, AppThemeMode.dark, AppThemeMode.black], extra: const [(phone, 2)]),
  'app_info': _Screen(() => const AppDetailScreen(app: _jellyfin), pushed: true, modes: const [AppThemeMode.light, AppThemeMode.dark], overrides: {
    'GET /v2/app_management/compose/jellyfin': {
      'data': {
        'status': 'running',
        'store_info': {'category': 'Media'},
      },
    },
  }),
};

String _file(DesignDirection d, String screen, AppThemeMode m, {String suffix = '', Size size = phone, double textScale = 1}) =>
    'goldens/directions/${d.name}/$screen${suffix}_${m.name}_${size.width.toInt()}x${size.height.toInt()}${textScale == 1 ? '' : '_text${textScale.toInt()}x'}.png';

void main() {
  setUp(() async {
    TrashScreen.clearCache();
    await signIn();
  });

  for (final d in DesignDirection.values) {
    for (final MapEntry(key: name, value: s) in _screens.entries) {
      for (final m in s.modes) {
        final a = Appearance(mode: m, direction: d);
        testWidgets('${d.name} $name ${m.name}', (tester) => _shoot(tester, d, name, s, a));
      }
      for (final (size, scale) in s.extra) {
        final a = Appearance(mode: s.modes.first, direction: d);
        testWidgets('${d.name} $name ${s.modes.first.name} ${size.width.toInt()} ${scale}x', (tester) => _shoot(tester, d, name, s, a, size: size, textScale: scale));
      }
    }
    final (accent, mode) = _signature[d]!;
    testWidgets('${d.name} home accent', (tester) => _shoot(tester, d, 'home', _screens['home']!, Appearance(mode: mode, accent: accent, direction: d), suffix: '_${accent.name}'));
    // Monochrome: the style's own ink as the accent.
    for (final m in const [AppThemeMode.light, AppThemeMode.black]) {
      testWidgets('${d.name} home mono ${m.name}', (tester) => _shoot(tester, d, 'home', _screens['home']!, Appearance(mode: m, accent: AccentColor.mono, direction: d), suffix: '_mono'));
    }
  }

  // Side by side: every direction's shot of one screen in one mode.
  final compare = <(String, AppThemeMode, Size, double)>[
    for (final MapEntry(key: name, value: s) in _screens.entries) ...[
      for (final m in s.modes) (name, m, phone, 1.0),
      for (final (size, scale) in s.extra) (name, s.modes.first, size, scale),
    ],
  ];
  for (final (name, mode, size, scale) in compare) {
    final tag = size == phone && scale == 1 ? '' : '_${size.width.toInt()}${scale == 1 ? '' : '_text${scale.toInt()}x'}';
    testWidgets('compare $name ${mode.name}$tag', skip: !_compareSheets, (tester) => _compare(
          tester,
          [for (final d in DesignDirection.values) (d == DesignDirection.v2 ? '1.3 theme (reference)' : d.label, 'test/screenshots/${_file(d, name, mode, size: size, textScale: scale)}')],
          'goldens/directions/compare/${name}_${mode.name}$tag.png',
          size: size,
        ));
  }
  testWidgets('compare home signature accents', skip: !_compareSheets, (tester) => _compare(
        tester,
        [
          for (final d in DesignDirection.values)
            ('${d.label} · ${_signature[d]!.$1.label}', 'test/screenshots/${_file(d, 'home', _signature[d]!.$2, suffix: '_${_signature[d]!.$1.name}')}'),
        ],
        'goldens/directions/compare/home_accents.png',
      ));
}

Future<void> _shoot(WidgetTester tester, DesignDirection d, String name, _Screen s, Appearance a, {String suffix = '', Size size = phone, double textScale = 1}) {
  final dark = a.mode != AppThemeMode.light;
  final screen = name == 'appearance' ? AppearanceScreen(controller: ThemeController(a)) : s.build();
  return shoot(
    tester,
    dir: 'directions/${d.name}',
    name: '$name$suffix',
    screen: screen,
    brightness: dark ? Brightness.dark : Brightness.light,
    themeName: a.mode.name,
    appearance: a,
    size: size,
    textScale: textScale,
    tab: s.tab,
    pushed: s.pushed,
    overrides: s.overrides,
    before: s.before,
  );
}

/// Draws the PNGs in [shots] next to each other under their names and
/// compares the result with [golden].
// The side-by-side sheets are review aids, built on request
// (NVOS_COMPARE=1 flutter test test/screenshots/directions_test.dart
// --update-goldens) and not kept in git - at ~13 MB per refresh they would
// bloat the repository.
final _compareSheets = Platform.environment['NVOS_COMPARE'] == '1';

Future<void> _compare(WidgetTester tester, List<(String, String)> shots, String golden, {Size size = phone}) async {
  await loadRealFonts();
  final images = <ui.Image?>[];
  await tester.runAsync(() async {
    for (final (_, path) in shots) {
      final f = File(path);
      if (!f.existsSync()) {
        images.add(null);
        continue;
      }
      final codec = await ui.instantiateImageCodec(f.readAsBytesSync());
      images.add((await codec.getNextFrame()).image);
    }
  });
  final w = size.width, h = size.height;
  const gap = 24.0, head = 56.0;
  tester.view.physicalSize = Size(shots.length * w + (shots.length + 1) * gap, h + head + gap) * 1.5;
  tester.view.devicePixelRatio = 1.5;
  addTearDown(tester.view.reset);
  final theme = AppTheme.light();
  await tester.pumpWidget(MaterialApp(
    debugShowCheckedModeBanner: false,
    theme: theme,
    home: ColoredBox(
      color: const Color(0xFFD9D9D6),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(gap, 0, 0, gap),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            for (var i = 0; i < shots.length; i++)
              Padding(
                padding: const EdgeInsets.only(right: gap),
                child: SizedBox(
                  width: w,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      SizedBox(
                        height: head,
                        child: Align(
                          alignment: Alignment.centerLeft,
                          child: Text(shots[i].$1, style: theme.textTheme.titleLarge?.copyWith(color: const Color(0xFF1B1B1F))),
                        ),
                      ),
                      SizedBox(
                        width: w,
                        height: h,
                        child: images[i] == null
                            ? const Center(child: Text('Not rendered yet'))
                            : RawImage(image: images[i], fit: BoxFit.contain, filterQuality: FilterQuality.medium),
                      ),
                    ],
                  ),
                ),
              ),
          ],
        ),
      ),
    ),
  ));
  await expectLater(find.byType(MaterialApp), matchesGoldenFile(golden));
  for (final i in images) {
    i?.dispose();
  }
}
