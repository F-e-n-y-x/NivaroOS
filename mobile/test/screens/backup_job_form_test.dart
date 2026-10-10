// The job form's options beyond the basics, and the job page's actions,
// against the fake server: each option lands in the Job JSON the server
// takes (jobs/model.go), the form's own checks and the server's field
// errors show next to their field, an encrypted job keeps its encryption
// when it moves, a copy of one asks for a new password, and Preview,
// Check backup, Delete and Test password call the right routes.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/backup/backup_models.dart';
import 'package:nivaroos_mobile/screens/backup/backup_decide_screen.dart';
import 'package:nivaroos_mobile/screens/backup/backup_job_form_screen.dart';
import 'package:nivaroos_mobile/screens/backup/backup_job_screen.dart';
import 'package:nivaroos_mobile/screens/backup/backup_run_screen.dart';
import 'package:nivaroos_mobile/screens/backup/backup_screen.dart';

import '../screenshots/backup_test.dart' show backupServer, photosJob;
import '../screenshots/harness.dart';

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 8; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 200));
  }
}

/// "PUT /v1/backup/jobs/bk_photos" (query included) -> JSON body.
typedef Sent = List<(String, Object?)>;

Future<Sent> _run(WidgetTester tester, Widget screen, Future<void> Function() body, {Map<String, Object> overrides = const {}}) async {
  await loadRealFonts();
  tester.view.physicalSize = const Size(412, 915) * 3;
  tester.view.devicePixelRatio = 3;
  addTearDown(tester.view.reset);
  final inner = FakeServer(overrides: {...backupServer, ...overrides}).client;
  final sent = <(String, Object?)>[];
  final client = MockClient((req) async {
    sent.add(('${req.method} ${req.url.path}${req.url.hasQuery ? '?${req.url.query}' : ''}', req.body.isEmpty ? null : jsonDecode(req.body)));
    final copy = http.Request(req.method, req.url)
      ..headers.addAll(req.headers)
      ..bodyBytes = req.bodyBytes;
    return http.Response.fromStream(await inner.send(copy));
  });
  await http.runWithClient(() async {
    await tester.pumpWidget(testApp(screen, pushed: true));
    await _settle(tester);
    await body();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => client);
  return sent;
}

Future<void> _tap(WidgetTester tester, Finder f) async {
  await tester.scrollUntilVisible(f, 200, scrollable: find.byType(Scrollable).first);
  await tester.ensureVisible(f);
  await tester.pump(const Duration(milliseconds: 300));
  await tester.tap(f);
  await tester.pump(const Duration(milliseconds: 300));
}

Future<void> _type(WidgetTester tester, String key, String text) async {
  final f = find.byKey(Key(key));
  await tester.scrollUntilVisible(f, 200, scrollable: find.byType(Scrollable).first);
  await tester.ensureVisible(f);
  await tester.pump(const Duration(milliseconds: 300));
  await tester.enterText(f, text);
  await tester.pump();
}

Map<String, Object?> _body(Sent sent, String route) => sent.lastWhere((s) => s.$1 == route).$2! as Map<String, Object?>;

Future<void> _save(WidgetTester tester) async {
  await tester.tap(find.text('Save'));
  await _settle(tester);
}

Map<String, Object?> _photosRaw() => Map<String, Object?>.from(fixture('backup/job_photos')['data'] as Map);

BackupJob _encryptedPhotos() => BackupJob.fromJson({
      ..._photosRaw(),
      'dest': {...(_photosRaw()['dest'] as Map), 'encryption': {'mode': 'folder', 'recovery_key': true}},
    });

final _saved = fixture('backup/job_photos');

void main() {
  setUp(() async {
    BackupScreen.clearCache();
    await signIn();
  });

  group('form options', () {
    testWidgets('an edit sends back what it does not show, and the defaults are the stored ones', (tester) async {
      final sent = await _run(tester, BackupJobFormScreen(job: photosJob()), () => _save(tester), overrides: {'PUT /v1/backup/jobs/bk_photos': _saved});
      final j = _body(sent, 'PUT /v1/backup/jobs/bk_photos');
      expect(j['revision'], 3);
      expect(j['filters'], {'exclude_presets': ['caches', 'trash'], 'exclude': []});
      expect(j['guards'], {'empty_source_pct': 50, 'delete_pct': 10, 'change_pct': 30, 'allow_empty_source': false});
      expect(j['options'], {'verify': false, 'preview_first': true, 'low_priority': true, 'max_duration_sec': 86400, 'copy_empty_dirs': false});
      expect(j['conditions'], {'dest_available': true, 'when_unmet': 'skip'});
      expect(j['retry'], {'max': 3, 'backoff_sec': [60, 600, 3600]});
      expect(j['notify'], {'on_success': false, 'on_failure': true, 'stale_after_hours': 336});
      expect(j['hooks'], isEmpty);
      expect(j['triggers'], [
        {'kind': 'schedule', 'cron': '0 3 * * 0', 'catch_up': true},
        {'kind': 'volume_mounted', 'min_gap_hours': 24, 'catch_up': true},
      ]);
      for (final k in ['health', 'stats', 'last_run', 'next_run', 'created_at']) {
        expect(j.containsKey(k), isFalse, reason: k);
      }
    });

    testWidgets('skip, time window, when unmet, retry, safety and advanced options go in the job', (tester) async {
      final sent = await _run(tester, BackupJobFormScreen(job: photosJob()), () async {
        await _tap(tester, find.text('Skip'));
        await _tap(tester, find.widgetWithText(FilterChip, 'Thumbnails'));
        await _tap(tester, find.widgetWithText(FilterChip, 'Trash and recycle bins'));
        await _tap(tester, find.text('Files larger than'));
        await _type(tester, 'max-size', '2.5');
        await _type(tester, 'exclude', '*.iso\n/Temp/**\n');

        await _tap(tester, find.text('Only run when…'));
        await _tap(tester, find.text('Only during these hours (server time)'));
        await _tap(tester, find.text('Fail and notify me'));
        await _tap(tester, find.byKey(const Key('retry')));
        await tester.pump(const Duration(seconds: 1));
        await tester.tap(find.text('Never').last);
        await tester.pump(const Duration(seconds: 1));
        await _type(tester, 'plug-gap', '0');

        await _tap(tester, find.text('Safety'));
        await _type(tester, 'delete-pct', '25');
        await _type(tester, 'empty-pct', '40');
        await _tap(tester, find.text('Check files after copying'));

        await _tap(tester, find.text('Advanced'));
        await _tap(tester, find.text('Low priority'));
        await _type(tester, 'max-hours', '6');
        await _type(tester, 'include', '*.jpg\n*.heic');
        await _tap(tester, find.text('Notify me when a run succeeds'));
        await _tap(tester, find.text('The source may be empty'));
        await _save(tester);
      }, overrides: {'PUT /v1/backup/jobs/bk_photos': _saved});
      final j = _body(sent, 'PUT /v1/backup/jobs/bk_photos');
      expect(j['filters'], {
        'exclude_presets': ['caches', 'thumbs'],
        'exclude': ['*.iso', '/Temp/**'],
        'include': ['*.jpg', '*.heic'],
        'max_size_bytes': 2500000000,
      });
      expect(j['conditions'], {'dest_available': true, 'when_unmet': 'fail', 'window': {'start': '22:00', 'end': '07:00'}});
      expect(j['retry'], {'max': 0, 'backoff_sec': [60, 600, 3600]});
      expect((j['triggers'] as List)[1], {'kind': 'volume_mounted', 'catch_up': true}, reason: '0 h = every time it is plugged in');
      expect(j['guards'], {'empty_source_pct': 40, 'delete_pct': 25, 'change_pct': 30, 'allow_empty_source': true});
      expect(j['options'], {'verify': true, 'preview_first': true, 'low_priority': false, 'max_duration_sec': 21600, 'copy_empty_dirs': false});
      expect((j['notify'] as Map)['on_success'], isTrue);
    });

    testWidgets('a limit out of range is caught before saving, next to the field, with its group opened', (tester) async {
      final sent = await _run(tester, BackupJobFormScreen(job: photosJob()), () async {
        await _tap(tester, find.text('Safety'));
        await _type(tester, 'delete-pct', '0');
        await _tap(tester, find.text('Safety')); // collapse it again
        await _save(tester);
        expect(find.text('This value is out of range.'), findsOneWidget);
        expect(find.byKey(const Key('delete-pct')), findsOneWidget, reason: 'the group opened to show it');
      });
      expect(sent.any((s) => s.$1.startsWith('PUT')), isFalse);
    });

    testWidgets("the server's field errors show at their fields, unknown ones in the notice", (tester) async {
      await _run(tester, BackupJobFormScreen(job: photosJob()), () async {
        await _save(tester);
        await tester.scrollUntilVisible(find.byKey(const Key('exclude')), 200, scrollable: find.byType(Scrollable).first);
        expect(find.text('Invalid pattern'), findsOneWidget, reason: 'the Skip group opened to show it');
        expect(find.text('This value is out of range.'), findsOneWidget, reason: 'notify.stale_after_hours has no field: the notice says it');
      }, overrides: {
        'PUT /v1/backup/jobs/bk_photos': const FakeResponse({
          'success': 400,
          'message': 'validation',
          'data': {
            'error_code': 'validation',
            'field_errors': {'filters.exclude[0]': 'invalid_filter', 'notify.stale_after_hours': 'out_of_range'},
          },
        }, status: 400),
      });
    });

    testWidgets('apps in the sources get a stop switch, sent as hook pairs', (tester) async {
      final immich = BackupJob.fromJson((fixture('backup/jobs')['data'] as List).firstWhere((j) => (j as Map)['id'] == 'bk_immich'));
      final overrides = {'PUT /v1/backup/jobs/bk_immich': _saved};
      final on = await _run(tester, BackupJobFormScreen(job: immich), () async {
        await _tap(tester, find.text('Keep apps consistent (1)'));
        expect(tester.widget<SwitchListTile>(find.widgetWithText(SwitchListTile, 'Stop immich during the backup')).value, isTrue);
        await _save(tester);
      }, overrides: overrides);
      expect(_body(on, 'PUT /v1/backup/jobs/bk_immich')['hooks'], [
        {'phase': 'pre', 'action': 'stop_apps', 'apps': ['immich'], 'app_mode': 'together', 'timeout_sec': 300, 'fail_policy': 'abort'},
        {'phase': 'post', 'action': 'start_apps', 'apps': ['immich'], 'app_mode': 'together', 'timeout_sec': 300, 'fail_policy': 'continue'},
      ]);
      final off = await _run(tester, BackupJobFormScreen(job: immich), () async {
        await _tap(tester, find.text('Keep apps consistent (1)'));
        await _tap(tester, find.text('Stop immich during the backup'));
        expect(find.textContaining('immich keeps running'), findsOneWidget);
        await _save(tester);
      }, overrides: overrides);
      expect(_body(off, 'PUT /v1/backup/jobs/bk_immich')['hooks'], isEmpty);
    });
  });

  group('encryption', () {
    testWidgets('an encrypted job moved to another destination keeps its encryption', (tester) async {
      final sent = await _run(tester, BackupJobFormScreen(job: _encryptedPhotos()), () async {
        expect(find.text('Encrypted folder'), findsOneWidget);
        await _tap(tester, find.text('Sandisk 128G'));
        await _settle(tester);
        await tester.tap(find.text('TeraBox'));
        await _settle(tester);
        await tester.tap(find.text('Use this location'));
        await _settle(tester);
        await _save(tester);
      }, overrides: {
        'PUT /v1/backup/jobs/bk_photos': _saved,
        'GET /v1/backup/locations/browse': {'success': 200, 'message': 'ok', 'data': {'path': '', 'entries': [], 'truncated': false}},
      });
      final dest = _body(sent, 'PUT /v1/backup/jobs/bk_photos')['dest'] as Map;
      expect(dest['kind'], 'cloud');
      expect(dest['encryption'], {'mode': 'folder', 'recovery_key': true});
      expect(dest['sub_path'], 'NivaroOS Backups/Photos to Sandisk', reason: "a new location gets the job's own folder");
    });

    testWidgets('a copy of an encrypted job needs a new password, then shows its recovery key once', (tester) async {
      final created = {
        ..._saved,
        'data': {...(_saved['data'] as Map), 'id': 'bk_new', 'recovery_key': 'ABCD-EFGH-IJKL'},
      };
      final sent = await _run(tester, BackupJobFormScreen(copyOf: _encryptedPhotos()), () async {
        await tester.tap(find.text('Create job'));
        await _settle(tester);
        await tester.scrollUntilVisible(find.byKey(const Key('password')), 200, scrollable: find.byType(Scrollable).first);
        expect(find.text('This field is required.'), findsOneWidget);
        await _type(tester, 'password', 'correct horse');
        await _type(tester, 'confirm', 'correct hors');
        await tester.tap(find.text('Create job'));
        await _settle(tester);
        expect(find.text("The two passwords don't match."), findsOneWidget);
        await _type(tester, 'confirm', 'correct horse');
        await tester.tap(find.text('Create job'));
        await _settle(tester);
        expect(find.text('Save your recovery key'), findsOneWidget);
        expect(find.text('ABCD-EFGH-IJKL'), findsOneWidget);
        await tester.tap(find.text("I've saved the recovery key"));
        await tester.pump();
        await tester.tap(find.text('Done'));
        await _settle(tester);
        expect(find.byType(BackupJobScreen), findsOneWidget);
      }, overrides: {'POST /v1/backup/jobs': created, 'GET /v1/backup/jobs/bk_new': _saved});
      final j = _body(sent, 'POST /v1/backup/jobs');
      expect(j['name'], 'Photos to Sandisk (copy)');
      expect(j.containsKey('id') || j.containsKey('revision'), isFalse);
      expect((j['dest'] as Map)['sub_path'], 'NivaroOS Backups/Photos to Sandisk (copy)', reason: 'a folder of its own');
      expect((j['dest'] as Map)['encryption'], {'mode': 'folder', 'recovery_key': false});
      expect(j['encryption_secret'], {'password': 'correct horse', 'make_recovery_key': true});
      expect(sent.any((s) => s.$1 == 'POST /v1/backup/jobs/bk_new/run'), isFalse, reason: 'a copy does not run by itself');
    });
  });

  group('job actions', () {
    Future<void> menu(WidgetTester tester, String item) async {
      await tester.tap(find.byTooltip('More actions'));
      for (var i = 0; i < 5; i++) {
        await tester.pump(const Duration(milliseconds: 200));
      }
      await tester.tap(find.text(item));
      await _settle(tester);
    }

    testWidgets('Preview a run starts a preview and opens the plan', (tester) async {
      final sent = await _run(tester, const BackupJobScreen(jobId: 'bk_photos'), () async {
        await menu(tester, 'Preview a run');
        expect(find.byType(BackupDecideScreen), findsOneWidget);
      }, overrides: {'GET /v1/backup/runs/run_photos_2': fixture('backup/run_phone_2')});
      expect(_body(sent, 'POST /v1/backup/jobs/bk_photos/run'), {'preview': true});
    });

    testWidgets('Check backup starts a verify run and opens it', (tester) async {
      final sent = await _run(tester, const BackupJobScreen(jobId: 'bk_photos'), () async {
        await menu(tester, 'Check backup');
        expect(find.byType(BackupRunScreen), findsOneWidget);
      });
      expect(_body(sent, 'POST /v1/backup/jobs/bk_photos/run'), {'preview': false, 'check': true});
    });

    testWidgets('Delete asks, and only purges the data when ticked', (tester) async {
      final sent = await _run(tester, const BackupJobScreen(jobId: 'bk_photos'), () async {
        await menu(tester, 'Delete');
        expect(find.text('Delete Photos to Sandisk?'), findsOneWidget);
        await tester.tap(find.text('Also delete the backup data on the destination'));
        await tester.pump();
        expect(find.textContaining("can't be undone"), findsOneWidget);
        await tester.tap(find.widgetWithText(TextButton, 'Delete'));
        await _settle(tester);
        expect(find.byType(BackupJobScreen), findsNothing);
      }, overrides: {'DELETE /v1/backup/jobs/bk_photos': {'success': 200, 'message': 'ok', 'data': {'purge_run_id': 'run_x'}}});
      expect(sent.any((s) => s.$1 == 'DELETE /v1/backup/jobs/bk_photos?purge_data=true'), isTrue);
    });

    testWidgets('Delete while running says to cancel first', (tester) async {
      await _run(tester, const BackupJobScreen(jobId: 'bk_photos'), () async {
        await menu(tester, 'Delete');
        await tester.tap(find.widgetWithText(TextButton, 'Delete'));
        await _settle(tester);
        expect(find.text('This job is running. Cancel the run first, then delete the job.'), findsOneWidget);
        expect(find.byType(BackupJobScreen), findsOneWidget);
      }, overrides: {
        'DELETE /v1/backup/jobs/bk_photos': const FakeResponse({'success': 409, 'message': 'invalid_state', 'data': {'error_code': 'invalid_state'}}, status: 409),
      });
    });

    testWidgets('Duplicate opens the form as a new job named "(copy)"', (tester) async {
      await _run(tester, const BackupJobScreen(jobId: 'bk_photos'), () async {
        await menu(tester, 'Duplicate');
        expect(find.byType(BackupJobFormScreen), findsOneWidget);
        await tester.scrollUntilVisible(find.text('Photos to Sandisk (copy)'), 300, scrollable: find.byType(Scrollable).first);
        expect(find.text('Photos to Sandisk (copy)'), findsOneWidget);
        expect(find.text('Create job'), findsOneWidget);
      });
    });

    testWidgets('an encrypted job tests a password against the server', (tester) async {
      final enc = {..._saved, 'data': _encryptedPhotos().raw};
      final sent = await _run(tester, const BackupJobScreen(jobId: 'bk_photos'), () async {
        await _tap(tester, find.widgetWithText(TextButton, 'Test'));
        await tester.enterText(find.byType(TextField), 'hunter22');
        await tester.tap(find.widgetWithText(FilledButton, 'Test'));
        await _settle(tester);
        expect(find.text('Correct: this unlocks the backup.'), findsOneWidget);
      }, overrides: {
        'GET /v1/backup/jobs/bk_photos': enc,
        'POST /v1/backup/jobs/bk_photos/encryption/test': {'success': 200, 'message': 'ok', 'data': {'ok': true}},
      });
      expect(_body(sent, 'POST /v1/backup/jobs/bk_photos/encryption/test'), {'password': 'hunter22'});
    });

    testWidgets('a changed destination folder offers Reconnect, which asks again for another job\'s folder', (tester) async {
      final changed = {..._saved, 'data': {...(_saved['data'] as Map), 'needs_attention': 'dest_changed'}};
      final sent = await _run(tester, const BackupJobScreen(jobId: 'bk_photos'), () async {
        await _tap(tester, find.text('Reconnect to this folder').first);
        await tester.tap(find.widgetWithText(FilledButton, 'Reconnect to this folder'));
        await _settle(tester);
        expect(find.text('Take the folder over'), findsOneWidget);
      }, overrides: {
        'GET /v1/backup/jobs/bk_photos': changed,
        'POST /v1/backup/jobs/bk_photos/reconnect-dest': const FakeResponse({'success': 409, 'message': 'dest_marker_mismatch', 'data': {'error_code': 'dest_marker_mismatch'}}, status: 409),
      });
      expect(_body(sent, 'POST /v1/backup/jobs/bk_photos/reconnect-dest'), {'adopt_other_job': false});
    });
  });
}
