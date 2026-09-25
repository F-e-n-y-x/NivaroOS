// Screenshots of the Files tab and the file viewer (design brief §7-§8):
// every state in light and dark, plus 360x740 and 200% text where rows are
// dense. Goldens land in goldens/files/.
//
//   flutter test test/screenshots/files_screens_test.dart --update-goldens
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/file_entry.dart';
import 'package:nivaroos_mobile/screens/file_viewer_screen.dart';
import 'package:nivaroos_mobile/screens/files/file_ops.dart';
import 'package:nivaroos_mobile/screens/files/file_sheets.dart';
import 'package:nivaroos_mobile/screens/files/file_widgets.dart';
import 'package:nivaroos_mobile/screens/files/transfers.dart';
import 'package:nivaroos_mobile/screens/files/trash_screen.dart';
import 'package:nivaroos_mobile/screens/files_screen.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'harness.dart';

const _documents = '/DATA/Documents';

final _docs = {'GET /v1/folder': fixture('files/documents'), 'GET /v1/trash/support': fixture('files/trash_support')};

/// Local files for the viewer to open.
final _dir = Directory.systemTemp.createTempSync('nivaro_files_shots');
File _local(String name, String content) => File('${_dir.path}/$name')..writeAsStringSync(content);

final _readme = _local(
  'notes.md',
  '# Notes\n\nThings to do on the server this week:\n\n'
      '- Move the photo library to **tank**\n- Update Jellyfin\n- Check the backup of `/DATA/Documents`\n\n'
      '> Snapshots run nightly at 02:00.\n\n```\nzfs list -t snapshot\n```\n',
);
final _compose = _local(
  'docker-compose.yml',
  'services:\n  jellyfin:\n    image: jellyfin/jellyfin:10.10\n    container_name: jellyfin\n    network_mode: host\n'
      '    volumes:\n      - /DATA/AppData/jellyfin/config:/config\n      - /DATA/Media:/media:ro\n'
      '    restart: unless-stopped\n    environment:\n      - JELLYFIN_PublishedServerUrl=http://192.168.1.20:8096\n',
);
final _csv = _local('budget.csv', 'Month,Rent,Power,Internet\nJuly,1200,84,40\nAugust,1200,91,40\n"September, est.",1200,77,40\n');
final _archive = _local('backup-2026-09.tar.gz', 'not really an archive');

FileEntry _entry(File f) => FileEntry(name: baseName(f.path), path: f.path, isDir: false, size: f.lengthSync());

/// Shows a sheet from inside the app once the first frame is up.
class _SheetHost extends StatefulWidget {
  const _SheetHost(this.show);
  final void Function(BuildContext context) show;

  @override
  State<_SheetHost> createState() => _SheetHostState();
}

class _SheetHostState extends State<_SheetHost> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => widget.show(context));
  }

  @override
  Widget build(BuildContext context) => const AppScaffold(title: 'Documents', body: SizedBox.expand());
}

/// The parts that are hard to catch mid-flight on a screen: the transfer
/// strip, the paste bar and the loading skeleton.
class _Parts extends StatelessWidget {
  const _Parts();

  @override
  Widget build(BuildContext context) {
    final progress = ValueNotifier<TransferProgress?>(const TransferProgress(
      title: 'Uploading 12 items to Gallery',
      current: 'IMG_2044.jpg',
      doneBytes: 734003200,
      totalBytes: 1288490188,
      totalItems: 12,
      doneItems: 5,
    ));
    return AppScaffold.slivers(
      title: 'Gallery',
      collapsingTitle: false,
      banner: TransferStrip(progress: progress, onCancel: () {}, queued: 1),
      floatingActionButton: PasteBar(label: 'Moving 3 items', actionLabel: 'Move here', onPaste: () {}, onCancel: () {}),
      floatingActionButtonLocation: FloatingActionButtonLocation.centerFloat,
      slivers: const [FolderSkeleton(rows: 6)],
    );
  }
}

Future<void> _wait(WidgetTester tester) async {
  for (var i = 0; i < 6; i++) {
    await tester.pump(const Duration(milliseconds: 100));
  }
}

/// One Files shot: name, screen and extras.
class _Shot {
  const _Shot(this.build, {this.overrides = const {}, this.before, this.small = false, this.text2x = false, this.tablet = false, this.tab = false, this.dark = true});
  final Widget Function() build;
  final Map<String, Object> overrides;
  final Future<void> Function(WidgetTester tester)? before;
  final bool small;
  final bool text2x;
  final bool tablet;
  final bool tab;
  final bool dark;
}

final Map<String, _Shot> _shots = {
  'files_home': _Shot(() => const FilesScreen(), tab: true, small: true, text2x: true, tablet: true),
  'files_folder': _Shot(() => const FilesScreen(initialPath: _documents), overrides: _docs, tab: true, small: true, text2x: true, tablet: true),
  'files_folder_grid': _Shot(
    () => const FilesScreen(initialPath: _documents),
    overrides: _docs,
    tab: true,
    small: true,
    text2x: true,
    before: (t) async {
      await t.tap(find.byTooltip('Show as grid'));
      await _wait(t);
    },
  ),
  'files_folder_empty': _Shot(
    () => const FilesScreen(initialPath: '$_documents/Work'),
    overrides: {'GET /v1/folder': fixture('files/empty')},
    tab: true,
  ),
  'files_folder_error': _Shot(
    () => const FilesScreen(initialPath: '$_documents/Work'),
    overrides: {'GET /v1/folder': const FakeResponse({'success': 500, 'message': 'Permission denied: /DATA/Documents/Work'}, status: 500)},
    tab: true,
  ),
  'files_search_none': _Shot(
    () => const FilesScreen(initialPath: _documents),
    overrides: _docs,
    tab: true,
    before: (t) async {
      await t.tap(find.byTooltip('Search in this folder'));
      await _wait(t);
      await t.enterText(find.byType(TextField), 'invoice 2019');
      await _wait(t);
    },
  ),
  'files_selection': _Shot(
    () => const FilesScreen(initialPath: _documents),
    overrides: _docs,
    tab: true,
    small: true,
    before: (t) async {
      await t.longPress(find.text('Budget 2026.xlsx'));
      await _wait(t);
      await t.tap(find.text('notes.md'));
      await _wait(t);
    },
  ),
  'files_actions': _Shot(
    () => const FilesScreen(initialPath: _documents),
    overrides: _docs,
    tab: true,
    text2x: true,
    before: (t) async {
      await t.tap(find.byTooltip('More options for backup-2026-09.tar.gz'));
      await _wait(t);
    },
  ),
  'files_paste': _Shot(
    () => const FilesScreen(initialPath: _documents),
    overrides: _docs,
    tab: true,
    small: true,
    before: (t) async {
      await t.tap(find.byTooltip('More options for Budget 2026.xlsx'));
      await _wait(t);
      await t.tap(find.text('Move'));
      await _wait(t);
    },
  ),
  'files_sort': _Shot(
    () => const FilesScreen(initialPath: _documents),
    overrides: _docs,
    tab: true,
    dark: false,
    before: (t) async {
      await t.tap(find.bySemanticsLabel('Sort by name, ascending'));
      await _wait(t);
    },
  ),
  'files_locations': _Shot(
    () => const FilesScreen(initialPath: '$_documents/Work'),
    overrides: {'GET /v1/folder': fixture('files/empty')},
    tab: true,
    before: (t) async {
      await t.tap(find.bySemanticsLabel('DATA, change location'));
      await _wait(t);
    },
  ),
  'files_conflict': _Shot(
    () => _SheetHost((context) => showConflictSheet(
          context,
          conflicts: ['/DATA/Downloads/Holiday.jpg', '/DATA/Downloads/notes.md', '/DATA/Downloads/Invoices'],
          destName: 'Documents',
          kind: TransferKind.copy,
        )),
    text2x: true,
    small: true,
  ),
  'files_conflict_one': _Shot(
    () => _SheetHost((context) => showConflictSheet(
          context,
          conflicts: ['/DATA/Downloads/Holiday.jpg'],
          destName: 'Documents',
          kind: TransferKind.move,
        )),
  ),
  'files_rename': _Shot(
    () => _SheetHost((context) => showNameDialog(
          context,
          title: 'Rename file',
          confirmLabel: 'Rename',
          initial: 'Contract - flat lease.pdf',
          selectStem: true,
        )),
  ),
  'files_parts': _Shot(() => const _Parts(), small: true),
  'trash': _Shot(() => const TrashScreen(), small: true, text2x: true, tablet: true),
  'trash_empty': _Shot(() => const TrashScreen(), overrides: {'GET /v1/trash': fixture('files/trash_empty')}),
  'trash_error': _Shot(
    () => const TrashScreen(),
    overrides: {'GET /v1/trash': const FakeResponse({'success': 500, 'message': 'Trash index is unreadable'}, status: 500)},
  ),
  'trash_selection': _Shot(
    () => const TrashScreen(),
    small: true,
    before: (t) async {
      await t.longPress(find.text('notes.md'));
      await _wait(t);
      await t.tap(find.text('Old invoices'));
      await _wait(t);
    },
  ),
  'trash_item': _Shot(
    () => const TrashScreen(),
    text2x: true,
    before: (t) async {
      await t.scrollUntilVisible(find.text('Old invoices'), 200, scrollable: find.byType(Scrollable).first);
      await _wait(t);
      await t.tap(find.text('Old invoices'));
      await _wait(t);
    },
  ),
  'trash_restore_conflict': _Shot(
    () => const TrashScreen(),
    overrides: {'GET /v1/folder': fixture('files/documents')},
    before: (t) async {
      await t.tap(find.text('notes.md'));
      await _wait(t);
      await t.tap(find.text('Restore'));
      await _wait(t);
    },
  ),
  'trash_empty_confirm': _Shot(
    () => const TrashScreen(),
    before: (t) async {
      await t.tap(find.text('Empty Trash'));
      await _wait(t);
    },
  ),
  'file_viewer_markdown': _Shot(
    () => FileViewerScreen(file: _entry(_readme), path: _readme.path, isLocal: true),
    small: true,
    text2x: true,
  ),
  'file_viewer_text': _Shot(() => FileViewerScreen(file: _entry(_compose), path: _compose.path, isLocal: true), small: true),
  'file_viewer_csv': _Shot(() => FileViewerScreen(file: _entry(_csv), path: _csv.path, isLocal: true)),
  'file_viewer_no_preview': _Shot(() => FileViewerScreen(file: _entry(_archive), path: _archive.path, isLocal: true)),
  'file_viewer_error': _Shot(
    () => const FileViewerScreen(
      file: FileEntry(name: 'Contract - flat lease.pdf', path: '$_documents/Contract - flat lease.pdf', isDir: false, size: 1843220),
      path: '$_documents/Contract - flat lease.pdf',
    ),
    overrides: {'GET /v1/file': const FakeResponse({'success': 404, 'message': 'not found'}, status: 404)},
  ),
};

void main() {
  setUp(() async {
    TrashScreen.clearCache();
    await signIn();
  });

  for (final MapEntry(key: name, value: s) in _shots.entries) {
    Future<void> shootOne(WidgetTester tester, Brightness b, {Size size = phone, double textScale = 1}) => shoot(
          tester,
          name: name,
          dir: 'files',
          screen: s.build(),
          brightness: b,
          size: size,
          textScale: textScale,
          overrides: s.overrides,
          before: s.before,
          tab: s.tab,
          pushed: name.startsWith('file_viewer') || name.startsWith('trash'),
        );

    // Real elevation shadows: flutter_test draws them as solid black
    // outlines by default, which would put a black ring round the FAB.
    // (The flag must be back to its default when the test body ends.)
    Future<void> shot(WidgetTester tester, Brightness b, {Size size = phone, double textScale = 1}) async {
      debugDisableShadows = false;
      try {
        await shootOne(tester, b, size: size, textScale: textScale);
      } finally {
        debugDisableShadows = true;
      }
    }


    for (final b in [Brightness.light, if (s.dark) Brightness.dark]) {
      testWidgets('$name ${b.name}', (t) => shot(t, b));
      if (s.small) testWidgets('$name small ${b.name}', (t) => shot(t, b, size: smallPhone));
      if (s.text2x) testWidgets('$name 200% text ${b.name}', (t) => shot(t, b, textScale: 2));
    }
    if (s.tablet) testWidgets('$name tablet', (t) => shot(t, Brightness.light, size: tablet));
  }
}
