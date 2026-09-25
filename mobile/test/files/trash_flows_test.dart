// The Trash against a fake server: restore (with the name-taken question),
// delete forever and empty only after a destructive confirm, and Undo on
// "Moved to Trash" in Files restores exactly what was trashed.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/models/file_entry.dart';
import 'package:nivaroos_mobile/screens/files/file_ops.dart';
import 'package:nivaroos_mobile/screens/files/trash_api.dart';
import 'package:nivaroos_mobile/screens/files/trash_screen.dart';
import 'package:nivaroos_mobile/screens/files_screen.dart';

import '../screenshots/harness.dart';

/// The fixture server, plus a log of every request with its JSON body.
class _Server {
  _Server(Map<String, Object> overrides) : _fake = FakeServer(overrides: overrides);

  final FakeServer _fake;
  final calls = <(String, Object?)>[];

  Iterable<Object?> bodiesOf(String key) => calls.where((c) => c.$1 == key).map((c) => c.$2);
  bool called(String key) => calls.any((c) => c.$1 == key);

  late final client = MockClient((req) async {
    calls.add(('${req.method} ${req.url.path}', req.body.isEmpty ? null : jsonDecode(req.body)));
    final copy = http.Request(req.method, req.url)
      ..headers.addAll(req.headers)
      ..body = req.body;
    return http.Response.fromStream(await _fake.client.send(copy));
  });
}

const _ok = {'success': 200, 'message': 'ok'};

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 8; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 200));
  }
}

Future<void> _run(WidgetTester tester, _Server server, Widget screen, Future<void> Function() body) async {
  tester.view.physicalSize = phone * 3;
  tester.view.devicePixelRatio = 3;
  addTearDown(tester.view.reset);
  await http.runWithClient(() async {
    await tester.pumpWidget(testApp(screen));
    await _settle(tester);
    await body();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => server.client);
}

void main() {
  setUp(() async {
    TrashScreen.clearCache();
    await signIn();
  });

  group('models', () {
    test('an empty Trash sends items: null', () {
      final t = TrashListing.fromJson(fixture('files/trash_empty')['data']);
      expect(t.isEmpty, isTrue);
      expect(t.retentionDays, 30);
    });

    test('items are newest first, with where they came from', () {
      final t = TrashListing.fromJson(fixture('v1/trash')['data']);
      expect(t.items.first.name, 'Camera uploads 2025');
      expect(t.items.first.measuring, isTrue);
      final invoices = t.items.firstWhere((i) => i.name == 'Old invoices');
      expect(invoices.originalFolder, '/DATA/Documents/Invoices');
      expect(invoices.isDir, isTrue);
      expect(invoices.items, 37);
      expect(invoices.daysLeft(30, DateTime(2026, 9, 25, 14, 3)), 29);
      expect(trashFolderLabel('/DATA/Documents/Invoices'), 'Documents/Invoices');
      expect(trashFolderLabel('/media/SANDISK'), '/media/SANDISK');
    });

    test('restored names follow the server', () {
      expect(restoredName('notes.md'), 'notes (restored).md');
      expect(restoredName('notes.md', {'notes (restored).md'}), 'notes (restored 2).md');
      expect(restoredName('Invoices'), 'Invoices (restored)');
      const r = TrashRestoreResult(restored: {'a': '/DATA/Documents/notes (restored).md', 'b': '/DATA/x.jpg'}, failed: []);
      expect(r.renamed.toList(), ['/DATA/Documents/notes (restored).md']);
      expect(r.folders, {'/DATA/Documents', '/DATA'});
    });
  });

  testWidgets('restore sends the item id and says where it went', (tester) async {
    final server = _Server({
      'GET /v1/folder': fixture('files/documents'),
      'POST /v1/trash/restore': {
        ..._ok,
        'data': {
          'restored': [
            {'id': '1f3a9c20d4e5', 'path': '/DATA/Documents/Holiday draft.jpg'},
          ],
          'failed': [],
        },
      },
    });
    Set<String>? restoredInto;
    await _run(tester, server, TrashScreen(onRestored: (f) => restoredInto = f), () async {
      await tester.tap(find.text('Holiday draft.jpg'));
      await _settle(tester);
      await tester.tap(find.text('Restore'));
      await _settle(tester);
      expect(find.textContaining('is already in'), findsNothing);
      expect(server.bodiesOf('POST /v1/trash/restore').single, {
        'ids': ['1f3a9c20d4e5'],
      });
      expect(find.text('Restored “Holiday draft.jpg” to Documents'), findsOneWidget);
      expect(restoredInto, {'/DATA/Documents'});
    });
  });

  testWidgets('a taken name asks first: Skip restores nothing, Keep both restores', (tester) async {
    final server = _Server({
      'GET /v1/folder': fixture('files/documents'),
      'POST /v1/trash/restore': {
        ..._ok,
        'data': {
          'restored': [
            {'id': '2b7e41f0a9c3', 'path': '/DATA/Documents/notes (restored).md'},
          ],
          'failed': [],
        },
      },
    });
    await _run(tester, server, const TrashScreen(), () async {
      Future<void> restoreNotes() async {
        await tester.tap(find.text('notes.md'));
        await _settle(tester);
        await tester.tap(find.text('Restore'));
        await _settle(tester);
        expect(find.text('“notes.md” is already in Documents'), findsOneWidget);
        expect(find.text('Replace'), findsNothing);
      }

      await restoreNotes();
      await tester.tap(find.text('Skip'));
      await _settle(tester);
      expect(server.called('POST /v1/trash/restore'), isFalse);
      expect(find.text('Nothing restored: every item was skipped.'), findsOneWidget);

      await restoreNotes();
      await tester.tap(find.text('Keep both'));
      await _settle(tester);
      expect(server.bodiesOf('POST /v1/trash/restore').single, {
        'ids': ['2b7e41f0a9c3'],
      });
      expect(find.text('Restored as “notes (restored).md” in Documents'), findsOneWidget);
    });
  });

  testWidgets('delete forever needs the destructive confirm and sends the selection', (tester) async {
    final server = _Server({
      'DELETE /v1/trash': {
        ..._ok,
        'data': {'failed': []},
      },
    });
    await _run(tester, server, const TrashScreen(), () async {
      await tester.longPress(find.text('notes.md'));
      await _settle(tester);
      await tester.tap(find.text('Holiday draft.jpg'));
      await _settle(tester);
      expect(find.text('2'), findsOneWidget);

      await tester.tap(find.byTooltip('Delete forever'));
      await _settle(tester);
      expect(find.text('Delete 2 items forever?'), findsOneWidget);
      await tester.tap(find.text('Cancel'));
      await _settle(tester);
      expect(server.called('DELETE /v1/trash'), isFalse);

      await tester.tap(find.byTooltip('Delete forever'));
      await _settle(tester);
      await tester.tap(find.widgetWithText(FilledButton, 'Delete forever'));
      await _settle(tester);
      final body = server.bodiesOf('DELETE /v1/trash').single as Map;
      expect((body['ids'] as List).toSet(), {'1f3a9c20d4e5', '2b7e41f0a9c3'});
      expect(find.text('Deleted 2 items forever'), findsOneWidget);
    });
  });

  testWidgets('Empty Trash confirms, then deletes everything', (tester) async {
    final server = _Server({
      'DELETE /v1/trash/all': {
        ..._ok,
        'data': {'failed': []},
      },
    });
    await _run(tester, server, const TrashScreen(), () async {
      await tester.tap(find.text('Empty Trash'));
      await _settle(tester);
      expect(find.text('Empty Trash?'), findsOneWidget);
      expect(server.called('DELETE /v1/trash/all'), isFalse);
      await tester.tap(find.widgetWithText(FilledButton, 'Empty Trash'));
      await _settle(tester);
      expect(server.called('DELETE /v1/trash/all'), isTrue);
      expect(find.text('Trash emptied'), findsOneWidget);
    });
  });

  testWidgets('an empty Trash says so', (tester) async {
    final server = _Server({'GET /v1/trash': fixture('files/trash_empty')});
    await _run(tester, server, const TrashScreen(), () async {
      expect(find.text('Trash is empty'), findsOneWidget);
      expect(find.text('Empty Trash'), findsNothing);
    });
  });

  testWidgets('deleting in Files offers Undo, which restores what was trashed', (tester) async {
    final server = _Server({
      'GET /v1/folder': fixture('files/documents'),
      'GET /v1/trash/support': fixture('files/trash_support'),
      'DELETE /v1/batch': {
        ..._ok,
        'data': {
          'trashed': [
            {'id': 'abc123', 'name': 'notes.md', 'original_path': '/DATA/Documents/notes.md'},
          ],
        },
      },
      'POST /v1/trash/restore': {
        ..._ok,
        'data': {
          'restored': [
            {'id': 'abc123', 'path': '/DATA/Documents/notes.md'},
          ],
          'failed': [],
        },
      },
    });
    await _run(tester, server, const Scaffold(body: FilesScreen(initialPath: '/DATA/Documents')), () async {
      await tester.tap(find.byTooltip('More options for notes.md'));
      await _settle(tester);
      await tester.tap(find.text('Move to Trash'));
      await _settle(tester);
      expect(find.text('Move “notes.md” to Trash?'), findsOneWidget);
      await tester.tap(find.widgetWithText(FilledButton, 'Move to Trash'));
      await _settle(tester);
      expect(server.bodiesOf('DELETE /v1/batch').single, [
        {'path': '/DATA/Documents/notes.md'},
      ]);
      expect(find.text('Moved “notes.md” to Trash'), findsOneWidget);
      await tester.tap(find.text('Undo'));
      await _settle(tester);
      expect(server.bodiesOf('POST /v1/trash/restore').single, {
        'ids': ['abc123'],
      });
      expect(find.text('Restored “notes.md”'), findsOneWidget);
    });
  });

  testWidgets('the Files home has a Trash row that opens the Trash', (tester) async {
    final server = _Server({});
    await _run(tester, server, const Scaffold(body: FilesScreen()), () async {
      expect(find.text('Trash'), findsOneWidget);
      expect(find.text('7 items · 1.5 GB'), findsOneWidget);
      await tester.tap(find.text('Trash'));
      await _settle(tester);
      expect(find.text('In Trash'), findsOneWidget);
      expect(find.text('Camera uploads 2025'), findsOneWidget);
    });
  });
}
