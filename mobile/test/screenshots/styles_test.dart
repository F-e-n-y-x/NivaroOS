// A style styles the whole app (owner request, 2026-09-26): one gallery of
// the stock Material components - app bar, tabs, list rows, toggles,
// slider, segmented choice, chips, every button, fields, search, progress,
// a card, a dialog, a snack bar, an open menu and the navigation bar -
// drawn in every style × mode × accent that shows its character: Monochrome,
// the default blue and the style's signature accent, in light, dark and
// true black.
//
//   flutter test test/screenshots/styles_test.dart --update-goldens
//
// PNGs: goldens/styles/<style>/components_<accent>_<mode>_412x1400.png.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'harness.dart';

const _size = Size(412, 1400);

/// Each style's signature accent (design brief §10).
const _signature = {
  DesignDirection.rack: AccentColor.ember,
  DesignDirection.tonal: AccentColor.violet,
  DesignDirection.console: AccentColor.lime,
};

class _Components extends StatefulWidget {
  const _Components();

  @override
  State<_Components> createState() => _ComponentsState();
}

class _ComponentsState extends State<_Components> {
  final _menu = MenuController();

  @override
  Widget build(BuildContext context) {
    final gutter = Space.gutter(context);
    Widget row(List<Widget> children) => Padding(
          padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.xs),
          child: Wrap(spacing: Space.sm, runSpacing: Space.sm, crossAxisAlignment: WrapCrossAlignment.center, children: children),
        );
    return DefaultTabController(
      length: 3,
      child: Scaffold(
        appBar: AppBar(
          title: const Text('Components'),
          actions: [
            IconButton(tooltip: 'Search', onPressed: () {}, icon: const Icon(Icons.search)),
            MenuAnchor(
              controller: _menu,
              menuChildren: [
                MenuItemButton(leadingIcon: const Icon(Icons.refresh), onPressed: () {}, child: const Text('Refresh')),
                MenuItemButton(leadingIcon: const Icon(Icons.settings_outlined), onPressed: () {}, child: const Text('Settings')),
              ],
              builder: (context, c, _) => IconButton(tooltip: 'More', onPressed: c.open, icon: const Icon(Icons.more_vert)),
            ),
          ],
          bottom: const TabBar(tabs: [Tab(text: 'Overview'), Tab(text: 'Logs'), Tab(text: 'Settings')]),
        ),
        floatingActionButton: FloatingActionButton.extended(onPressed: () {}, icon: const Icon(Icons.add), label: const Text('New VM')),
        bottomNavigationBar: NavigationBar(selectedIndex: 0, destinations: const [
          NavigationDestination(icon: Icon(Icons.dashboard_outlined), selectedIcon: Icon(Icons.dashboard), label: 'Home'),
          NavigationDestination(icon: Icon(Icons.apps_outlined), label: 'Apps'),
          NavigationDestination(icon: Icon(Icons.folder_outlined), label: 'Files'),
          NavigationDestination(icon: Icon(Icons.more_horiz), label: 'More'),
        ]),
        body: ListView(
          padding: const EdgeInsets.only(bottom: 96),
          children: [
            TileGroup(title: 'List rows and toggles', children: [
              const ListTile(leading: Icon(Icons.dns_outlined), title: Text('nas'), subtitle: Text('192.168.1.20 · Ubuntu 24.04'), trailing: Text('12.4 GB')),
              SwitchListTile(value: true, onChanged: (_) {}, title: const Text('Notifications')),
              SwitchListTile(value: false, onChanged: (_) {}, title: const Text('Share storage')),
              CheckboxListTile(value: true, onChanged: (_) {}, title: const Text('Keep the disks')),
              RadioGroup<int>(
                groupValue: 1,
                onChanged: (_) {},
                child: const Column(children: [
                  RadioListTile<int>(value: 1, title: Text('Every 4 s')),
                  RadioListTile<int>(value: 2, title: Text('Every 10 s')),
                ]),
              ),
            ]),
            const SectionHeader(title: 'Choices'),
            Padding(
              padding: EdgeInsets.symmetric(horizontal: gutter),
              child: SegmentedButton<int>(
                segments: const [
                  ButtonSegment(value: 1, label: Text('2 s')),
                  ButtonSegment(value: 2, label: Text('4 s')),
                  ButtonSegment(value: 3, label: Text('10 s')),
                ],
                selected: const {2},
                onSelectionChanged: (_) {},
              ),
            ),
            row([
              FilterChip(label: const Text('Running'), selected: true, onSelected: (_) {}),
              FilterChip(label: const Text('Stopped'), selected: false, onSelected: (_) {}),
              ActionChip(avatar: const Icon(Icons.refresh), label: const Text('Refresh'), onPressed: () {}),
            ]),
            Padding(padding: EdgeInsets.symmetric(horizontal: gutter - Space.sm), child: Slider(value: .62, onChanged: (_) {})),
            const SectionHeader(title: 'Buttons'),
            row([
              FilledButton(onPressed: () {}, child: const Text('Install')),
              FilledButton.tonal(onPressed: () {}, style: tonalButtonStyle(context), child: const Text('Open')),
              OutlinedButton(onPressed: () {}, child: const Text('Details')),
              TextButton(onPressed: () {}, child: const Text('Retry')),
              IconButton.filledTonal(onPressed: () {}, icon: const Icon(Icons.terminal)),
            ]),
            const SectionHeader(title: 'Fields and progress'),
            Padding(
              padding: EdgeInsets.symmetric(horizontal: gutter),
              child: const Column(children: [
                TextField(decoration: InputDecoration(labelText: 'Server address', hintText: 'nas.example.com', helperText: 'A name or an IP address')),
                SizedBox(height: Space.md),
                SearchBar(hintText: 'Search files', leading: Icon(Icons.search)),
                SizedBox(height: Space.lg),
                LinearProgressIndicator(value: .42),
                SizedBox(height: Space.lg),
                Row(children: [CircularProgressIndicator(value: .7), SizedBox(width: Space.lg), Expanded(child: Divider())]),
              ]),
            ),
            const SectionHeader(title: 'Card and dialog'),
            Padding(
              padding: EdgeInsets.symmetric(horizontal: gutter),
              child: const Card(child: ListTile(leading: Icon(Icons.backup_outlined), title: Text('Backup finished'), subtitle: Text('2 min ago · 1.2 GB'))),
            ),
            const SizedBox(height: Space.md),
            AlertDialog(
              title: const Text('Shut down nas?'),
              content: const Text('Apps and VMs stop until it starts again.'),
              actions: [TextButton(onPressed: () {}, child: const Text('Cancel')), TextButton(onPressed: () {}, child: const Text('Shut down'))],
            ),
          ],
        ),
      ),
    );
  }
}

void main() {
  for (final d in DesignDirection.selectable) {
    for (final accent in {AccentColor.mono, AccentColor.blue, _signature[d]!}) {
      for (final mode in [AppThemeMode.light, AppThemeMode.dark, AppThemeMode.black]) {
        testWidgets('${d.name} components ${accent.name} ${mode.name}', (tester) => shoot(
              tester,
              dir: 'styles/${d.name}',
              name: 'components_${accent.name}',
              screen: const _Components(),
              brightness: mode == AppThemeMode.light ? Brightness.light : Brightness.dark,
              themeName: mode.name,
              appearance: Appearance(mode: mode, accent: accent, direction: d),
              size: _size,
              before: (tester) async {
                // A snack bar and the open menu, so they show in the shot.
                ScaffoldMessenger.of(tester.element(find.byType(ListView))).showSnackBar(
                  SnackBar(content: const Text('Moved to the Trash'), action: SnackBarAction(label: 'Undo', onPressed: () {}), duration: const Duration(minutes: 1)),
                );
                await tester.tap(find.byIcon(Icons.more_vert));
                await tester.pump(const Duration(seconds: 1));
              },
            ));
      }
    }
  }
}
