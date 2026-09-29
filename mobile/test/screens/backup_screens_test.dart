// Backup & Sync against the fake server: Back up now from the overview's
// menu and from a job, the restore wizard's checks and the body it sends,
// the decision on a waiting run, and the state when the module isn't
// installed.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/screens/backup/backup_decide_screen.dart';
import 'package:nivaroos_mobile/screens/backup/backup_job_screen.dart';
import 'package:nivaroos_mobile/screens/backup/backup_restore_screen.dart';
import 'package:nivaroos_mobile/screens/backup/backup_run_screen.dart';
import 'package:nivaroos_mobile/screens/backup/backup_screen.dart';
import 'package:nivaroos_mobile/screens/dashboard_screen.dart';

import '../screenshots/backup_test.dart' show backupMissing, backupServer, photosJob;
import '../screenshots/harness.dart';

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 8; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 200));
  }
}

/// Requests with their JSON bodies, "POST /v1/backup/jobs/x/run" -> body.
typedef Sent = List<(String, Object?)>;

Future<Sent> _run(WidgetTester tester, Widget screen, Future<void> Function() body, {Map<String, Object> overrides = const {}}) async {
  await loadRealFonts();
  tester.view.physicalSize = const Size(412, 915) * 3;
  tester.view.devicePixelRatio = 3;
  addTearDown(tester.view.reset);
  final server = FakeServer(overrides: {...backupServer, ...overrides});
  final inner = server.client;
  final sent = <(String, Object?)>[];
  final client = MockClient((req) async {
    sent.add(('${req.method} ${req.url.path}', req.body.isEmpty ? null : jsonDecode(req.body)));
    final copy = http.Request(req.method, req.url)
      ..headers.addAll(req.headers)
      ..bodyBytes = req.bodyBytes;
    return http.Response.fromStream(await inner.send(copy));
  });
  await http.runWithClient(() async {
    await tester.pumpWidget(testApp(screen));
    await _settle(tester);
    await body();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => client);
  return sent;
}

/// Opens the Photos job's menu (its button can sit under New job).
Future<void> _openMenu(WidgetTester tester) async {
  final menu = find.byWidgetPredicate((w) => w is PopupMenuButton && w.tooltip == 'More actions for Photos to Sandisk');
  await tester.scrollUntilVisible(menu, 200, scrollable: find.byType(Scrollable).first);
  tester.state<PopupMenuButtonState<String>>(menu).showButtonMenu();
}

/// Scrolls [f] into view and taps it.
Future<void> _tap(WidgetTester tester, Finder f) async {
  await tester.ensureVisible(f);
  await tester.pump(const Duration(milliseconds: 300));
  await tester.tap(f);
  await tester.pump();
}

bool _posted(Sent sent, String route) => sent.any((s) => s.$1 == route);

void main() {
  setUp(() async {
    BackupScreen.clearCache();
    await signIn();
  });

  testWidgets("Home's backup row opens the job in the app, not the web interface", (tester) async {
    await _run(tester, const Scaffold(body: DashboardScreen()), () async {
      final row = find.text("Photos to Sandisk didn't finish");
      await tester.scrollUntilVisible(row, 200, scrollable: find.byType(Scrollable).first);
      await _tap(tester, row);
      await _settle(tester);
      expect(find.byType(BackupJobScreen), findsOneWidget);
    }, overrides: {
      // Home's own fixture list (fixtures/v1/backup/jobs.json), not the rich one.
      'GET /v1/backup/jobs': fixture('v1/backup/jobs'),
    });
  });

  group('not installed', () {
    testWidgets('no health route: says so and how to add it, no error', (tester) async {
      final sent = await _run(tester, const BackupScreen(), () async {
        expect(find.text("Backup & Sync isn't installed"), findsOneWidget);
        expect(find.textContaining('--with-backup'), findsOneWidget);
        expect(find.text('Check again'), findsOneWidget);
        expect(find.text('New job'), findsNothing);
      }, overrides: backupMissing);
      expect(_posted(sent, 'GET /v1/backup/jobs'), isFalse, reason: 'the jobs are not asked for when the module is absent');
    });

    testWidgets('installed:false reads the same', (tester) async {
      await _run(tester, const BackupScreen(), () async {
        expect(find.text("Backup & Sync isn't installed"), findsOneWidget);
      }, overrides: {'GET /v1/backup/health': {'installed': false, 'running': false, 'service': 'backup', 'version': ''}});
    });

    testWidgets('installed but stopped: the web texts for a service that is not answering', (tester) async {
      await _run(tester, const BackupScreen(), () async {
        expect(find.text("Backup & Sync isn't answering"), findsOneWidget);
        expect(find.textContaining('sudo systemctl start nivaroos-backup'), findsOneWidget);
      }, overrides: {'GET /v1/backup/health': {'installed': true, 'running': false, 'service': 'backup', 'version': '1.0.0'}});
    });
  });

  group('overview', () {
    testWidgets('needs attention first, the running job with its progress, every job', (tester) async {
      await _run(tester, const BackupScreen(), () async {
        expect(find.text('Problem'), findsWidgets);
        expect(find.text('Needs attention (2)'), findsOneWidget);
        expect(find.text('Waiting for your review'), findsOneWidget);
        expect(find.text('Running now'), findsOneWidget);
        expect(find.textContaining('3,210 of 4,700 files'), findsOneWidget);
      });
    });

    testWidgets('Back up now from a job\'s menu starts a run', (tester) async {
      final sent = await _run(tester, const BackupScreen(), () async {
        await _openMenu(tester);
        await _settle(tester);
        await tester.tap(find.text('Back up now'));
        await _settle(tester);
        expect(find.text('Photos to Sandisk started.'), findsOneWidget);
      });
      expect(sent.where((s) => s.$1 == 'POST /v1/backup/jobs/bk_photos/run').single.$2, {'preview': false});
    });

    testWidgets('Pause from the menu flips the job at once and posts the toggle', (tester) async {
      final sent = await _run(tester, const BackupScreen(), () async {
        await _openMenu(tester);
        await _settle(tester);
        await tester.tap(find.text('Pause schedule'));
        await _settle(tester);
      }, overrides: {'POST /v1/backup/jobs/bk_photos/toggle': {'success': 200, 'message': 'ok', 'data': {...(backupServer['GET /v1/backup/jobs/bk_photos'] as Map)['data'] as Map, 'enabled': false, 'health': 'disabled'}}});
      expect(sent.where((s) => s.$1 == 'POST /v1/backup/jobs/bk_photos/toggle').single.$2, {'enabled': false});
    });
  });

  group('job', () {
    testWidgets('Back up now posts the run and opens it', (tester) async {
      final sent = await _run(tester, const BackupJobScreen(jobId: 'bk_photos'), () async {
        expect(find.text('Photos to Sandisk'), findsOneWidget);
        await tester.tap(find.text('Back up now'));
        await _settle(tester);
        expect(find.byType(BackupRunScreen), findsOneWidget);
      });
      expect(_posted(sent, 'POST /v1/backup/jobs/bk_photos/run'), isTrue);
    });

    testWidgets('a running job offers Cancel run, which asks first', (tester) async {
      final sent = await _run(tester, const BackupJobScreen(jobId: 'bk_docs'), () async {
        await tester.tap(find.text('Cancel run'));
        await _settle(tester);
        expect(find.text('Cancel Documents to tank?'), findsOneWidget);
        await tester.tap(find.text('Cancel run').last);
        await _settle(tester);
      }, overrides: {'POST /v1/backup/runs/run_docs_2/cancel': backupServer['GET /v1/backup/runs/run_docs_2']!});
      expect(_posted(sent, 'POST /v1/backup/runs/run_docs_2/cancel'), isTrue);
    });

    testWidgets('history: the runs with status, duration and data', (tester) async {
      await _run(tester, const BackupJobScreen(jobId: 'bk_photos'), () async {
        await tester.scrollUntilVisible(find.text('History'), 300, scrollable: find.byType(Scrollable).first);
        expect(find.text('Succeeded · Backup'), findsWidgets);
        expect(find.textContaining('13 min · 10.4 GB'), findsOneWidget);
      });
    });
  });

  group('restore wizard', () {
    Widget screen() => BackupRestoreScreen(jobs: [photosJob()], job: photosJob());

    final button = find.byWidgetPredicate((w) => w is FilledButton);
    Future<FilledButton> restoreButton(WidgetTester tester) async {
      await tester.scrollUntilVisible(button, 300, scrollable: find.byType(Scrollable).first);
      return tester.widget<FilledButton>(button);
    }

    testWidgets('another folder needs a folder before Restore can start', (tester) async {
      final sent = await _run(tester, screen(), () async {
        expect((await restoreButton(tester)).onPressed, isNotNull, reason: 'back where they came from needs nothing more');
        await tester.scrollUntilVisible(find.text('Another folder'), -300, scrollable: find.byType(Scrollable).first);
        await tester.tap(find.text('Another folder'));
        await tester.pump();
        expect(find.text('No folder chosen yet'), findsWidgets);
        expect((await restoreButton(tester)).onPressed, isNull);
        await tester.tap(button, warnIfMissed: false);
        await tester.pump();
      });
      expect(_posted(sent, 'POST /v1/backup/jobs/bk_photos/restore'), isFalse);
    });

    testWidgets('a picked folder is sent as the target', (tester) async {
      final sent = await _run(tester, screen(), () async {
        await tester.scrollUntilVisible(find.text('Another folder'), 300, scrollable: find.byType(Scrollable).first);
        await tester.tap(find.text('Another folder'));
        await tester.pump();
        await tester.scrollUntilVisible(find.text('Choose a folder…'), 300, scrollable: find.byType(Scrollable).first);
        await tester.tap(find.text('Choose a folder…'));
        await _settle(tester);
        await tester.tap(find.text('tower'));
        await _settle(tester);
        await tester.tap(find.text('Use this location'));
        await _settle(tester);
        expect(find.text('tower'), findsOneWidget);
        await restoreButton(tester);
        await tester.tap(button);
        await _settle(tester);
      }, overrides: {'GET /v1/backup/locations/browse': {'success': 200, 'message': 'ok', 'data': {'path': '', 'entries': [], 'truncated': false}}});
      final body = sent.where((s) => s.$1 == 'POST /v1/backup/jobs/bk_photos/restore').single.$2 as Map;
      expect((body['target'] as Map)['mode'], 'other');
      expect(((body['target'] as Map)['endpoint'] as Map)['ref_id'], '6c1e2f0a-9b1d-4c1e-8f3a-2b7d9e0c4a11');
    });

    testWidgets('the version, picked files, conflict mode and check-first go in the request', (tester) async {
      final sent = await _run(tester, screen(), () async {
        // The older version, two files from it.
        await tester.tap(find.textContaining('Fri, Sep 18'));
        await tester.pump();
        await tester.tap(find.text('Everything in this version'));
        await _settle(tester);
        await tester.tap(find.text('notes.txt'));
        await tester.tap(find.text('wedding.mp4'));
        await tester.pump();
        expect(find.textContaining('2 items selected'), findsOneWidget);
        await tester.tap(find.text('Done'));
        await _settle(tester);
        expect(find.text('2 items'), findsOneWidget);
        await tester.scrollUntilVisible(find.text('Skip it'), 300, scrollable: find.byType(Scrollable).first);
        await _tap(tester, find.text('Skip it'));
        await tester.scrollUntilVisible(find.text('Check first'), 300, scrollable: find.byType(Scrollable).first);
        await _tap(tester, find.text('Check first'));
        await tester.scrollUntilVisible(find.text('Check the restore'), 300, scrollable: find.byType(Scrollable).first);
        await _tap(tester, find.text('Check the restore'));
        await _settle(tester);
        expect(find.byType(BackupRunScreen), findsOneWidget);
      }, overrides: {'GET /v1/backup/jobs/bk_photos/versions/v_20260917T213000Z/browse': backupServer['GET /v1/backup/jobs/bk_photos/versions/current/browse']!});
      final body = sent.where((s) => s.$1 == 'POST /v1/backup/jobs/bk_photos/restore').single.$2;
      expect(body, {
        'version_id': 'v_20260917T213000Z',
        'paths': ['notes.txt', 'wedding.mp4'],
        'target': {'mode': 'original'},
        'conflict': 'skip',
        'dry_run': true,
      });
    });

    testWidgets('a server field error lands on the target', (tester) async {
      await _run(tester, screen(), () async {
        await restoreButton(tester);
        await tester.tap(button);
        await _settle(tester);
        await tester.scrollUntilVisible(find.text('Folder not allowed'), -300, scrollable: find.byType(Scrollable).first);
        expect(find.text('Folder not allowed'), findsOneWidget);
      }, overrides: {
        'POST /v1/backup/jobs/bk_photos/restore': const FakeResponse({
          'success': 400,
          'message': 'validation',
          'data': {'error_code': 'validation', 'field_errors': {'target.endpoint.sub_path': 'path_not_allowed'}},
        }, status: 400),
      });
    });
  });

  group('decide', () {
    testWidgets('a run that would delete: nothing is preselected, then Continue sends the choice', (tester) async {
      final sent = await _run(tester, const BackupDecideScreen(runId: 'run_phone_2', jobId: 'bk_phone'), () async {
        expect(find.text('This run is paused for your review'), findsOneWidget);
        expect(find.textContaining('24,090 of 48,190'), findsOneWidget);
        FilledButton cont() => tester.widget<FilledButton>(find.ancestor(of: find.text('Continue'), matching: find.byType(FilledButton)));
        expect(cont().onPressed, isNull);
        expect(find.text('Choose how this run should continue.'), findsOneWidget);
        await tester.tap(find.textContaining('nothing is deleted'));
        await tester.pump();
        expect(cont().onPressed, isNotNull);
        await tester.tap(find.text('Continue'));
        await _settle(tester);
      }, overrides: {'POST /v1/backup/runs/run_phone_2/decide': backupServer['GET /v1/backup/runs/run_phone_2']!});
      expect(sent.where((s) => s.$1 == 'POST /v1/backup/runs/run_phone_2/decide').single.$2, {'proceed': true, 'mode': 'copy_once'});
      expect(sent.any((s) => s.$1 == 'GET /v1/backup/runs/run_phone_2/preview'), isTrue);
    });

    testWidgets('decided elsewhere: says so', (tester) async {
      await _run(tester, const BackupDecideScreen(runId: 'run_phone_2', jobId: 'bk_phone'), () async {
        await tester.tap(find.text('Cancel run'));
        await _settle(tester);
        expect(find.text('This run was already decided somewhere else.'), findsOneWidget);
      }, overrides: {
        'POST /v1/backup/runs/run_phone_2/decide': const FakeResponse({
          'success': 409,
          'message': 'invalid_state',
          'data': {'error_code': 'invalid_state'},
        }, status: 409),
      });
    });
  });
}
