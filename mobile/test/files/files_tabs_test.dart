// Tabs in Files: the tab model (history, closing, what is saved), and the
// screen against a fake server - copy in one tab and paste in another
// sends the right batch job, Trash can be a tab (where paste is off), each
// tab keeps its own selection and scroll, and the tabs come back after a
// restart.
import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/screens/files/file_tabs.dart';
import 'package:nivaroos_mobile/screens/files/file_widgets.dart';
import 'package:nivaroos_mobile/screens/files/trash_screen.dart';
import 'package:nivaroos_mobile/screens/files_screen.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import '../screenshots/harness.dart';

const _documents = '/DATA/Documents';

/// The fixture server, with folder listings by path (Documents has the
/// fixture's files, every other folder is empty) and a log of requests.
class _Server {
  _Server([Map<String, Object> overrides = const {}]) : _fake = FakeServer(overrides: overrides);

  final FakeServer _fake;
  final calls = <(String, Object?)>[];
  final listed = <String>[];

  /// Folders listed from another fixture than the default.
  final contents = <String, String>{};

  /// Names gone from every listing (deleted elsewhere).
  final removed = <String>{};

  /// Listings held back until their completer completes.
  final hold = <String, Completer<void>>{};

  Iterable<Object?> bodiesOf(String key) => calls.where((c) => c.$1 == key).map((c) => c.$2);

  late final client = MockClient((req) async {
    final key = '${req.method} ${req.url.path}';
    calls.add((key, req.body.isEmpty ? null : jsonDecode(req.body)));
    if (key == 'GET /v1/folder') {
      final path = req.url.queryParameters['path'] ?? '';
      listed.add(path);
      final held = hold.remove(path);
      if (held != null) await held.future;
      final listing = fixture(contents[path] ?? (path == _documents ? 'files/documents' : 'files/empty'));
      final data = listing['data'] as Map<String, dynamic>;
      data['content'] = [for (final e in data['content'] as List) if (!removed.contains((e as Map)['name'])) e];
      return http.Response(jsonEncode(listing), 200, headers: {'content-type': 'application/json'});
    }
    final copy = http.Request(req.method, req.url)
      ..headers.addAll(req.headers)
      ..body = req.body;
    return http.Response.fromStream(await _fake.client.send(copy));
  });
}

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 8; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 200));
  }
}

Future<void> _run(WidgetTester tester, _Server server, Future<void> Function() body) async {
  tester.view.physicalSize = phone * 3;
  tester.view.devicePixelRatio = 3;
  addTearDown(tester.view.reset);
  await http.runWithClient(() async {
    await body();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => server.client);
}

Future<void> _pumpFiles(WidgetTester tester, {String? initialPath, Key? key, double textScale = 1}) async {
  await tester.pumpWidget(testApp(Scaffold(body: FilesScreen(key: key, initialPath: initialPath)), textScale: textScale));
  await _settle(tester);
}

Future<void> _menu(WidgetTester tester, String entry, String action) async {
  await tester.tap(find.byTooltip('More options for $entry'));
  await _settle(tester);
  await tester.tap(find.text(action));
  await _settle(tester);
}

Finder _chip(String title) => find.widgetWithText(InputChip, title);

ScrollPosition _listPosition(WidgetTester tester) => tester
    .state<ScrollableState>(find.byWidgetPredicate((w) => w is Scrollable && w.axisDirection == AxisDirection.down).first)
    .position;

void main() {
  group('model', () {
    test('back returns to where the tab was, scrolled as it was', () {
      final tab = FilesTab(0);
      tab.go(const FilesPlace.folder('/DATA'));
      tab.go(const FilesPlace.folder(_documents), offset: 120);
      tab.selected.add('$_documents/notes.md');
      tab.go(const FilesPlace.trash(), offset: 480);
      expect(tab.selected, isEmpty);
      expect(tab.offset, 0);
      expect(tab.back(), isTrue);
      expect(tab.place, const FilesPlace.folder(_documents));
      expect(tab.offset, 480);
      expect(tab.back(), isTrue);
      expect(tab.place, const FilesPlace.folder('/DATA'));
      expect(tab.offset, 120);
      expect(tab.back(), isTrue);
      expect(tab.place.isHome, isTrue);
      expect(tab.back(), isFalse);
    });

    test('going where the tab already is keeps its history', () {
      final tab = FilesTab(0, const FilesPlace.folder(_documents));
      tab.go(const FilesPlace.folder(_documents));
      expect(tab.history, isEmpty);
    });

    test('a folder on the phone is not the same place as on the server', () {
      expect(const FilesPlace.folder('/DATA', isLocal: true) == const FilesPlace.folder('/DATA'), isFalse);
    });

    test('new tabs open next to the one on screen; closing shows a neighbour', () {
      final tabs = FilesTabs(const FilesPlace.folder('/DATA'));
      final a = tabs.active;
      final c = tabs.open(const FilesPlace.trash());
      tabs.activate(a);
      final b = tabs.open(const FilesPlace.folder(_documents));
      expect(tabs.all, [a, b, c]);
      expect(tabs.active, b);

      expect(tabs.close(b), isTrue);
      expect(tabs.active, a, reason: 'the one before');
      expect(tabs.close(a), isTrue);
      expect(tabs.active, c, reason: 'the first shows the next');
      expect(tabs.close(c), isFalse, reason: 'the last tab stays');
      expect(tabs.length, 1);
    });

    test('closing a tab off screen keeps the one on screen', () {
      final tabs = FilesTabs();
      final a = tabs.active;
      final b = tabs.open(const FilesPlace.folder('/DATA'));
      final c = tabs.open(const FilesPlace.folder(_documents));
      tabs.close(a);
      expect(tabs.active, c);
      tabs.activate(b);
      tabs.close(c);
      expect(tabs.active, b);
    });

    test('places and the active tab are saved, not history or selection', () {
      final tabs = FilesTabs(const FilesPlace.folder(_documents));
      tabs.active.selected.add('$_documents/notes.md');
      tabs.open(const FilesPlace.folder('/storage/emulated/0/Download', isLocal: true));
      tabs.open(const FilesPlace.trash());
      tabs.open(const FilesPlace.home());
      tabs.activate(tabs.all[1]);
      final back = FilesTabs.decode(tabs.encode())!;
      expect([for (final t in back.all) t.place], [
        const FilesPlace.folder(_documents),
        const FilesPlace.folder('/storage/emulated/0/Download', isLocal: true),
        const FilesPlace.trash(),
        const FilesPlace.home(),
      ]);
      expect(back.activeIndex, 1);
      expect(back.all.first.selected, isEmpty);
      expect({for (final t in back.all) t.id}.length, 4, reason: 'ids stay unique');
    });

    test('nothing saved, or something unreadable, is no tabs', () {
      expect(FilesTabs.decode(null), isNull);
      expect(FilesTabs.decode(''), isNull);
      expect(FilesTabs.decode('{not json'), isNull);
      expect(FilesTabs.decode('{"tabs": []}'), isNull);
      final odd = FilesTabs.decode('{"tabs": [{"path": 3}, {"path": "/DATA"}], "active": 9}')!;
      expect(odd.all.first.place.isHome, isTrue);
      expect(odd.activeIndex, 0);
    });
  });

  group('paste bar', () {
    for (final (d, stacked) in [(DesignDirection.rack, false), (DesignDirection.tonal, false), (DesignDirection.console, true)]) {
      testWidgets('${d.name} at 412 dp: ${stacked ? 'wide buttons go under the words' : 'the words fit beside the buttons'}', (tester) async {
        await tester.runAsync(loadRealFonts);
        tester.view.physicalSize = phone * 2;
        tester.view.devicePixelRatio = 2;
        addTearDown(tester.view.reset);
        await tester.pumpWidget(testApp(
          Scaffold(
            body: Align(
              alignment: Alignment.bottomCenter,
              child: PasteBar(label: '“Budget 2026.xlsx” on clipboard', detail: 'Copying from Documents', actionLabel: 'Paste here', onPaste: () {}, onCancel: () {}),
            ),
          ),
          appearance: Appearance(direction: d),
        ));
        final words = tester.getRect(find.text('Copying from Documents'));
        final paste = tester.getRect(find.widgetWithText(FilledButton, 'Paste here'));
        expect(paste.top >= words.bottom, stacked);
        // Beside the buttons, the detail line still fits on one line.
        if (!stacked) expect(words.height, lessThan(24));
      });
    }
  });

  group('screen', () {
    setUp(() async {
      TrashScreen.clearCache();
      await signIn();
    });

    testWidgets('one tab looks as before: no strip, a tabs button', (tester) async {
      final server = _Server({'GET /v1/trash/support': fixture('files/trash_support')});
      await _run(tester, server, () async {
        await _pumpFiles(tester, initialPath: _documents);
        expect(find.byType(InputChip), findsNothing);
        expect(find.byTooltip('Tabs'), findsOneWidget);
      });
    });

    testWidgets('copy in tab A, paste in tab B sends one copy job to B', (tester) async {
      final server = _Server({
        'GET /v1/trash/support': fixture('files/trash_support'),
        'POST /v1/batch/task': {
          'success': 200,
          'message': 'ok',
          'data': {'id': 'job1'},
        },
        'GET /v1/batch/tasks': {
          'success': 200,
          'message': 'ok',
          'data': [
            {'id': 'job1', 'state': 'done', 'files_done': 1, 'files_total': 1, 'bytes_done': 10, 'bytes_total': 10},
          ],
        },
      });
      await _run(tester, server, () async {
        await _pumpFiles(tester, initialPath: _documents);

        // Tab A: copy notes.md.
        await _menu(tester, 'notes.md', 'Copy');
        expect(find.text('“notes.md” on clipboard'), findsOneWidget);
        expect(find.text('Copying from Documents'), findsOneWidget);

        // Tab B: Work, opened in a new tab from its menu.
        await _menu(tester, 'Work', 'Open in new tab');
        expect(_chip('Documents'), findsOneWidget);
        expect(_chip('Work'), findsOneWidget);
        expect(find.text('This folder is empty'), findsOneWidget);
        expect(find.text('“notes.md” on clipboard'), findsOneWidget, reason: 'the clipboard is shared');

        await tester.tap(find.widgetWithText(FilledButton, 'Paste here'));
        await _settle(tester);
        expect(server.bodiesOf('POST /v1/batch/task').single, {
          'type': 'copy',
          'item': [
            {'from': '$_documents/notes.md'},
          ],
          'to': '$_documents/Work',
          'style': 'rename',
        });
        expect(find.text('“notes.md” on clipboard'), findsNothing);

        // Back to tab A: still Documents, with its files.
        await tester.tap(_chip('Documents'));
        await _settle(tester);
        expect(find.text('notes.md'), findsOneWidget);
      });
    });

    testWidgets('Clear empties the clipboard in every tab', (tester) async {
      final server = _Server({'GET /v1/trash/support': fixture('files/trash_support')});
      await _run(tester, server, () async {
        await _pumpFiles(tester, initialPath: _documents);
        await _menu(tester, 'Holiday.jpg', 'Move');
        expect(find.text('Moving from Documents'), findsOneWidget);
        await _menu(tester, 'Invoices', 'Open in new tab');
        await tester.tap(find.text('Clear'));
        await _settle(tester);
        await tester.tap(_chip('Documents'));
        await _settle(tester);
        expect(find.text('“Holiday.jpg” on clipboard'), findsNothing);
      });
    });

    testWidgets('Trash as a tab: paste is off there, Up leaves it', (tester) async {
      final server = _Server({'GET /v1/trash/support': fixture('files/trash_support')});
      await _run(tester, server, () async {
        await _pumpFiles(tester, initialPath: _documents);
        await _menu(tester, 'notes.md', 'Copy');

        await tester.tap(find.byTooltip('Tabs'));
        await _settle(tester);
        await tester.tap(find.text('New tab'));
        await _settle(tester);
        expect(find.text('Open a folder to paste it'), findsOneWidget);
        await tester.tap(find.text('Trash'));
        await _settle(tester);
        expect(find.text('In Trash'), findsOneWidget);
        expect(_chip('Trash'), findsOneWidget);
        expect(find.text("Can't paste into Trash"), findsOneWidget);
        final paste = tester.widget<FilledButton>(find.widgetWithText(FilledButton, 'Paste here'));
        expect(paste.onPressed, isNull);

        await tester.tap(find.byTooltip('Up'));
        await _settle(tester);
        expect(find.text('Drives'), findsOneWidget);
        expect(_chip('Files'), findsOneWidget);
      });
    });

    testWidgets('each tab keeps its own selection, back and scroll', (tester) async {
      final server = _Server({'GET /v1/trash/support': fixture('files/trash_support')});
      await _run(tester, server, () async {
        await _pumpFiles(tester, initialPath: _documents);
        final state = tester.state<FilesScreenState>(find.byType(FilesScreen));
        await tester.tap(find.byTooltip('Tabs'));
        await _settle(tester);
        await tester.tap(find.text('New tab here'));
        await _settle(tester);
        expect(find.byType(InputChip), findsNWidgets(2));

        // Tab A: scrolled down, notes.md selected.
        await tester.tap(find.byType(InputChip).first);
        await _settle(tester);
        await tester.drag(find.text('notes.md'), const Offset(0, -200));
        await _settle(tester);
        final scrolled = _listPosition(tester).pixels;
        expect(scrolled, greaterThan(0));
        await tester.longPress(find.text('notes.md'));
        await _settle(tester);
        expect(find.text('1'), findsOneWidget);

        // Tab B: at the top, nothing selected.
        await tester.tap(find.byType(InputChip).last);
        await _settle(tester);
        expect(_listPosition(tester).pixels, 0);
        expect(find.text('1'), findsNothing);

        // Tab B goes into Work; back returns to Documents.
        await tester.tap(find.text('Work'));
        await _settle(tester);
        expect(find.text('This folder is empty'), findsOneWidget);
        expect(state.handleBack(), isTrue);
        await _settle(tester);
        expect(find.text('notes.md'), findsOneWidget);

        // Tab A: where it was, still selecting.
        await tester.tap(find.byType(InputChip).first);
        await _settle(tester);
        expect(_listPosition(tester).pixels, scrolled);
        expect(find.text('1'), findsOneWidget);
        expect(state.handleBack(), isTrue, reason: 'back clears the selection first');
        await _settle(tester);
        expect(find.text('1'), findsNothing);
      });
    });

    testWidgets('a selection lets go of items gone from the folder (deleted from another tab)', (tester) async {
      final server = _Server({'GET /v1/trash/support': fixture('files/trash_support')});
      await _run(tester, server, () async {
        await _pumpFiles(tester, initialPath: _documents);
        await tester.tap(find.byTooltip('Tabs'));
        await _settle(tester);
        await tester.tap(find.text('New tab here'));
        await _settle(tester);
        // Tab A: notes.md selected.
        await tester.tap(find.byType(InputChip).first);
        await _settle(tester);
        await tester.longPress(find.text('notes.md'));
        await _settle(tester);
        expect(find.text('1'), findsOneWidget);
        // Tab B, on the same folder, where notes.md goes away.
        await tester.tap(find.byType(InputChip).last);
        await _settle(tester);
        server.removed.add('notes.md');
        await tester.fling(find.text('Invoices'), const Offset(0, 400), 1000);
        await _settle(tester);
        expect(find.text('notes.md'), findsNothing);
        // Back to A: the fresh listing no longer has it, so neither does
        // the selection (no "1 selected" whose actions do nothing).
        await tester.tap(find.byType(InputChip).first);
        await _settle(tester);
        expect(find.text('notes.md'), findsNothing);
        expect(find.text('1'), findsNothing);
        expect(find.byTooltip('Clear selection'), findsNothing);
      });
    });

    testWidgets('the conflict sheet names the folder Paste was pressed in, even after a tab switch', (tester) async {
      final server = _Server({'GET /v1/trash/support': fixture('files/trash_support')});
      // Work already has a notes.md.
      server.contents['$_documents/Work'] = 'files/documents';
      await _run(tester, server, () async {
        await _pumpFiles(tester, initialPath: _documents);
        await _menu(tester, 'notes.md', 'Copy');
        await _menu(tester, 'Work', 'Open in new tab');
        expect(find.text('notes.md'), findsOneWidget);

        // Paste in Work, and switch to Documents while Work is listed again.
        final listing = server.hold['$_documents/Work'] = Completer<void>();
        await tester.tap(find.widgetWithText(FilledButton, 'Paste here'));
        await tester.pump();
        await tester.tap(_chip('Documents'));
        await _settle(tester);
        listing.complete();
        await _settle(tester);
        expect(find.text('“notes.md” is already in Work'), findsOneWidget);
      });
    });

    testWidgets('at 200% text the tab on screen and + stay clear of the edge, and the paste bar hides nothing', (tester) async {
      final server = _Server({'GET /v1/trash/support': fixture('files/trash_support')});
      // Real type: the test font's square glyphs would overflow at 200%.
      await tester.runAsync(loadRealFonts);
      await _run(tester, server, () async {
        await _pumpFiles(tester, initialPath: _documents, textScale: 2);
        await _menu(tester, 'Budget 2026.xlsx', 'Copy');
        await _menu(tester, 'Work', 'Open in new tab');

        final screen = tester.view.physicalSize / tester.view.devicePixelRatio;
        final gutter = Space.gutter(tester.element(find.byType(FilesScreen)));
        final chip = tester.getRect(_chip('Work'));
        expect(chip.left, greaterThanOrEqualTo(gutter - 0.5));
        expect(chip.right, lessThanOrEqualTo(screen.width - gutter + 0.5), reason: 'not flush against the edge');
        expect(tester.getRect(find.byTooltip('New tab')).right, lessThanOrEqualTo(screen.width), reason: 'the last tab brings + with it');

        // The empty folder keeps room for the paste bar under it, so its
        // button scrolls clear of the bar instead of staying under it.
        expect(find.text('“Budget 2026.xlsx” on clipboard'), findsOneWidget);
        await tester.drag(find.text('This folder is empty'), const Offset(0, -600));
        await _settle(tester);
        expect(find.text('Upload files').hitTestable(), findsOneWidget);
        expect(tester.getRect(find.text('Upload files')).bottom, lessThan(tester.getRect(find.byType(PasteBar)).top));
      });
    });

    testWidgets('open tabs and the active one come back after a restart', (tester) async {
      final server = _Server();
      await _run(tester, server, () async {
        await _pumpFiles(tester, key: const ValueKey(1));
        await tester.tap(find.text('DATA').first);
        await _settle(tester);
        expect(server.listed, contains('/DATA'));

        await tester.tap(find.byTooltip('Tabs'));
        await _settle(tester);
        await tester.tap(find.text('New tab'));
        await _settle(tester);
        await tester.tap(find.text('Trash'));
        await _settle(tester);
        await tester.tap(find.byTooltip('New tab'));
        await _settle(tester);
        // Close the third tab (on screen): the Trash shows again.
        await tester.tap(find.byTooltip('Close tab'));
        await _settle(tester);
        expect(find.byType(InputChip), findsNWidgets(2));
        expect(find.text('In Trash'), findsOneWidget);

        final saved = FilesTabs.decode(await StorageService.instance.getFilesTabs())!;
        expect([for (final t in saved.all) t.place], [const FilesPlace.folder('/DATA'), const FilesPlace.trash()]);
        expect(saved.activeIndex, 1);

        // A new Files screen, as after a restart.
        await tester.pumpWidget(const SizedBox.shrink());
        await _pumpFiles(tester, key: const ValueKey(2));
        expect(_chip('DATA'), findsOneWidget);
        expect(_chip('Trash'), findsOneWidget);
        expect(find.text('In Trash'), findsOneWidget);
        await tester.tap(_chip('DATA'));
        await _settle(tester);
        expect(find.text('This folder is empty'), findsOneWidget);
      });
    });

    testWidgets('a screen opened at a folder does not overwrite the saved tabs', (tester) async {
      final saved = FilesTabs(const FilesPlace.trash()).encode();
      await StorageService.instance.setFilesTabs(saved);
      final server = _Server({'GET /v1/trash/support': fixture('files/trash_support')});
      await _run(tester, server, () async {
        await _pumpFiles(tester, initialPath: _documents);
        await _menu(tester, 'Work', 'Open in new tab');
        expect(await StorageService.instance.getFilesTabs(), saved);
      });
    });
  });
}
