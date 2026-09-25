// Screenshots of the v2 shared widgets (lib/ui/widgets), light and dark:
// one gallery page of the building blocks, plus the full-screen states and
// the dialogs and sheets. These run strict: any overflow fails.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'harness.dart';

Widget _gallery() => AppScaffold.slivers(
  title: 'Components',
  actions: [IconButton(tooltip: 'Search', onPressed: () {}, icon: const Icon(Icons.search))],
  banner: OfflineBanner(lastUpdated: shotTime.subtract(const Duration(minutes: 5)), onRetry: () {}),
  slivers: [
    SliverList.list(
      children: [
        SectionHeader(title: 'Status chips', actionLabel: 'See all', onAction: () {}),
        const Padding(
          padding: EdgeInsets.symmetric(horizontal: Space.lg),
          child: Wrap(
            spacing: Space.sm,
            runSpacing: Space.sm,
            children: [
              StatusChip(label: 'Running', status: Status.success),
              StatusChip(label: 'Update available', status: Status.warning),
              StatusChip(label: 'Failed', status: Status.error),
              StatusChip(label: 'Syncing', status: Status.info),
              StatusChip(label: 'Stopped', status: Status.neutral, icon: Icons.stop_circle_outlined),
            ],
          ),
        ),
        const SectionHeader(title: 'Usage bars'),
        const Padding(
          padding: EdgeInsets.symmetric(horizontal: Space.lg),
          child: Column(
            children: [
              UsageBar(value: 230, max: 422, label: 'System disk', detail: '230 GB of 422 GB'),
              SizedBox(height: Space.lg),
              UsageBar(value: 1.52, max: 1.8, label: 'blue', detail: '1.52 TB of 1.8 TB'),
              SizedBox(height: Space.lg),
              UsageBar(value: 3.9, max: 4, label: 'tank', detail: '3.9 TB of 4 TB'),
              SizedBox(height: Space.lg),
              UsageBar(value: 0.3, max: 32, label: 'Memory (nearly idle)', detail: '0.3 GB of 32 GB'),
              SizedBox(height: Space.lg),
              UsageBar(value: 0, max: 0, label: 'USB drive', detail: 'Size unknown'),
            ],
          ),
        ),
        // The accent in the places it shows most: buttons, a segmented
        // choice and the navigation indicator (compare the fidelity shots).
        const SectionHeader(title: 'Buttons and navigation'),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: Space.lg),
          child: Wrap(
            spacing: Space.sm,
            runSpacing: Space.sm,
            children: [
              FilledButton(onPressed: () {}, child: const Text('Install')),
              FilledButton.tonal(onPressed: () {}, child: const Text('Open')),
              OutlinedButton(onPressed: () {}, child: const Text('Details')),
              TextButton(onPressed: () {}, child: const Text('Cancel')),
            ],
          ),
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(Space.lg, Space.md, Space.lg, 0),
          child: SegmentedButton<int>(
            segments: const [
              ButtonSegment(value: 0, label: Text('List')),
              ButtonSegment(value: 1, label: Text('Grid')),
            ],
            selected: const {0},
            onSelectionChanged: (_) {},
          ),
        ),
        const SizedBox(height: Space.md),
        // Without the bottom inset it gets as the app's real bar.
        Builder(
          builder: (context) => MediaQuery.removePadding(
            context: context,
            removeBottom: true,
            child: NavigationBar(
              selectedIndex: 0,
              destinations: const [
                NavigationDestination(icon: Icon(Icons.home_outlined), selectedIcon: Icon(Icons.home), label: 'Home'),
                NavigationDestination(
                  icon: Icon(Icons.folder_outlined),
                  selectedIcon: Icon(Icons.folder),
                  label: 'Files',
                ),
                NavigationDestination(icon: Icon(Icons.apps_outlined), selectedIcon: Icon(Icons.apps), label: 'Apps'),
              ],
            ),
          ),
        ),
        const TileGroup(
          title: 'Metric rows',
          children: [
            MetricRow(
              icon: Icons.memory_outlined,
              label: 'CPU',
              value: '12',
              unit: '%',
              supporting: 'AMD Ryzen 5 3600 · 6 cores',
            ),
            MetricRow(icon: Icons.thermostat_outlined, label: 'Temperature', value: '48', unit: '°C'),
            MetricRow(icon: Icons.upload_outlined, label: 'Upload', value: '1.2', unit: 'MB/s'),
            MetricRow(
              icon: Icons.bolt_outlined,
              label: 'Power',
              value: '—',
              supporting: 'This CPU does not report power',
            ),
          ],
        ),
        TileGroup(
          title: 'Settings group',
          footer: 'Applies to this phone only.',
          children: [
            AppearanceTile(controller: ThemeController()),
            ListTile(
              leading: const Icon(Icons.schedule_outlined),
              title: const Text('Last backup'),
              subtitle: RelativeTime(shotTime.subtract(const Duration(minutes: 2)), prefix: 'Finished '),
            ),
            SwitchListTile(
              secondary: const Icon(Icons.sync_outlined),
              title: const Text('Keep running in background'),
              value: true,
              onChanged: (_) {},
            ),
          ],
        ),
        const SectionHeader(title: 'Loading'),
      ],
    ),
    const SliverLoadingList(rows: 3, leading: SkeletonLeading.avatar, trailing: true),
  ],
);

/// Installed apps, as a screen with data cached from an earlier visit.
Widget _cachedApps() => ListView(
  children: const [
    ListTile(leading: Icon(Icons.movie_outlined), title: Text('Jellyfin'), subtitle: Text('Running')),
    ListTile(leading: Icon(Icons.photo_library_outlined), title: Text('Immich'), subtitle: Text('Running')),
    ListTile(leading: Icon(Icons.cloud_outlined), title: Text('Nextcloud'), subtitle: Text('Stopped')),
  ],
);

final Map<String, Widget Function()> _states = {
  'state_loading': () => const AppScaffold(
    title: 'Apps',
    body: LoadingList(leading: SkeletonLeading.thumbnail),
  ),
  'state_empty': () => AppScaffold(
    title: 'Apps',
    body: EmptyState(
      icon: Icons.apps_outlined,
      title: 'No apps yet',
      message: 'Apps you install on the server appear here.',
      actionLabel: 'Open app store',
      onAction: () {},
    ),
  ),
  'state_error': () => AppScaffold(
    title: 'Logs',
    body: ErrorState(
      title: "Couldn't load the logs",
      message: 'The server answered with an error. Try again in a moment.',
      onRetry: () {},
      details: 'GET /v1/sys/logs → 500',
    ),
  ),
  // The server is unreachable but there is cached data: the banner over
  // the last list, which stays usable.
  'state_offline': () => AppScaffold(
    title: 'Apps',
    banner: OfflineBanner(lastUpdated: shotTime.subtract(const Duration(minutes: 12)), onRetry: () {}),
    body: _cachedApps(),
  ),
  // Unreachable with nothing cached: the full-screen state instead.
  'state_unreachable': () => AppScaffold(
    title: 'Apps',
    body: ErrorState.offline(onRetry: () {}),
  ),
  // The sliver forms, as a top-level tab puts them.
  'state_sliver_loading': () => const AppScaffold.slivers(
    title: 'Apps',
    slivers: [SliverLoadingList(leading: SkeletonLeading.thumbnail)],
  ),
  'state_sliver_empty': () => AppScaffold.slivers(
    title: 'Apps',
    slivers: [
      EmptyState(
        icon: Icons.apps_outlined,
        title: 'No apps yet',
        message: 'Apps you install on the server appear here.',
        actionLabel: 'Open app store',
        onAction: () {},
        sliver: true,
      ),
    ],
  ),
};

Future<void> _openButton(WidgetTester tester) async {
  await tester.tap(find.text('Open'));
  await tester.pumpAndSettle();
}

Widget _launcher(void Function(BuildContext) open) => Scaffold(
  body: Builder(
    builder: (context) => Center(
      child: FilledButton(onPressed: () => open(context), child: const Text('Open')),
    ),
  ),
);

/// Canvas sizes that fit the gallery's content, so the shots have no
/// blank tail.
const _gallerySize = Size(412, 1880);
const _gallery2xSize = Size(412, 2680);

void main() {
  setUp(signIn);

  for (final b in Brightness.values) {
    testWidgets('gallery ${b.name}', (tester) async {
      await shoot(tester, dir: 'widgets', name: 'gallery', screen: _gallery(), brightness: b, size: _gallerySize);
    });
    testWidgets('gallery 200% text ${b.name}', (tester) async {
      await shoot(
        tester,
        dir: 'widgets',
        name: 'gallery',
        screen: _gallery(),
        brightness: b,
        textScale: 2,
        size: _gallery2xSize,
      );
    });
    // The same page with the scheme's fidelity variant, which keeps the raw
    // NivaroOS blue as primary: for the owner to compare against the
    // default tonal-spot scheme (brief §8, Decisions).
    testWidgets('gallery fidelity ${b.name}', (tester) async {
      await shoot(
        tester,
        dir: 'widgets',
        name: 'gallery_fidelity',
        screen: Theme(data: AppTheme.withVariant(b, DynamicSchemeVariant.fidelity), child: _gallery()),
        brightness: b,
        size: _gallerySize,
      );
    });
    for (final e in _states.entries) {
      testWidgets('${e.key} ${b.name}', (tester) async {
        await shoot(tester, dir: 'widgets', name: e.key, screen: e.value(), brightness: b);
      });
    }
    testWidgets('confirm_destructive ${b.name}', (tester) async {
      await shoot(
        tester,
        dir: 'widgets',
        name: 'confirm_destructive',
        brightness: b,
        screen: _launcher(
          (c) => ConfirmDialog.destructive(
            c,
            title: 'Delete “mint”?',
            message: 'The VM and its 60 GB disk are deleted.',
            confirmLabel: 'Delete',
          ),
        ),
        before: _openButton,
      );
    });
    testWidgets('appearance ${b.name}', (tester) async {
      await shoot(
        tester,
        dir: 'widgets',
        name: 'appearance',
        brightness: b,
        pushed: true,
        screen: AppearanceScreen(controller: ThemeController(Appearance(mode: b == Brightness.dark ? AppThemeMode.dark : AppThemeMode.light))),
      );
    });
  }
}
