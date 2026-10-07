// Backup & Sync parsing and wording, against the frozen REST contract
// (docs/specs/backup-api.json: every example there is decoded strictly
// into the Go types by services/backup/jobs/contract_test.go, so these are
// real response shapes) and the web app's rules (ui/src/apps/backup).
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/backup/backup_api.dart';
import 'package:nivaroos_mobile/backup/backup_models.dart';
import 'package:nivaroos_mobile/backup/backup_state.dart';
import 'package:nivaroos_mobile/backup/backup_strings.dart';
import 'package:nivaroos_mobile/services/api_client.dart';

final Map<String, dynamic> _contract = jsonDecode(File('../docs/specs/backup-api.json').readAsStringSync()) as Map<String, dynamic>;

Object? _example(String id) => (_contract['endpoints'] as List).cast<Map<String, dynamic>>().firstWhere((e) => e['id'] == id)['response'];

void main() {
  group('the contract examples parse', () {
    test('an encrypted destination is shown and sent back as it came', () {
      final job = BackupJob.fromJson(_example('jobs_create'));
      expect(job.dest.encryptionMode, 'folder');
      expect(job.dest.toJson()['encryption'], {'mode': 'folder', 'recovery_key': true});
      expect(job.dest.withSubPath('x').encryptionMode, 'folder');
      expect(bt('backup.encrypt.mode_folder'), 'Encrypted folder');
      final plain = BackupJob.fromJson((_example('jobs_list') as List).first);
      expect((plain.dest.encryptionMode, plain.dest.toJson().containsKey('encryption')), ('', false));
    });

    test('health', () {
      final h = BackupHealth.fromJson(_example('health'));
      expect((h.installed, h.running, h.version), (true, true, '1.0.0'));
    });

    test('GET /jobs: jobs with their last run, triggers and retention', () {
      final jobs = [for (final j in _example('jobs_list') as List) BackupJob.fromJson(j)];
      expect(jobs, hasLength(2));
      final photos = jobs[0], immich = jobs[1];
      expect((photos.id, photos.type, photos.health, photos.enabled, photos.revision), ('bk_1a2b3c4d5e6f', 'mirror', 'ok', true, 3));
      expect(photos.sources.single.display, 'tower › photos');
      expect(photos.dest.display, 'Sandisk 128G › NivaroOS Backups/Photos');
      expect(photos.dest.match, {'serial': '4C530001230914117281', 'size_bytes': 128035676160});
      expect([for (final t in photos.triggers) t.kind], ['volume_mounted', 'schedule']);
      expect(photos.triggers[1].cron, '0 3 * * 0');
      expect((photos.versionsDays, photos.deletePct, photos.changePct, photos.previewFirst), (30, 10, 30, true));
      expect(photos.lastRun!.status, 'success');
      expect(photos.lastRun!.endedAt, DateTime.parse('2026-09-24T03:12:40+02:00'));
      expect(photos.nextRun, DateTime.parse('2026-09-28T03:00:00+02:00'));
      expect(photos.activeRun, isNull);
      expect(photos.sizeBytes, isNull, reason: 'the list has no stats');
      expect(immich.keepLast, 8);
      expect((immich.windowStart, immich.windowEnd, immich.whenUnmet), ('22:00', '07:00', 'wait'));
      expect(immich.isImported, isTrue);
      expect(immich.lastRun!.summary!.errorCode, 'cloud_auth');
    });

    test('GET /jobs/:id adds the stats', () {
      final j = BackupJob.fromJson(_example('jobs_get'));
      expect((j.sizeBytes, j.versionsCount, j.lastSuccess), (210000000000, 6, DateTime.parse('2026-09-24T03:12:40+02:00')));
    });

    test('GET /runs: counts, steps, guard, file errors, paging', () {
      final list = RunList.fromJson(_example('runs_list'));
      expect(list.nextBefore, 'run_01J8Z3Q4K5M6N7P8Q9R0S1T2V1');
      final ok = list.runs[0], waiting = list.runs[1];
      expect((ok.status, ok.kind, ok.trigger, ok.isFinal), ('success', 'backup', 'schedule', true));
      expect(ok.counts.bytesTransferred, 11200000000);
      expect(ok.duration, const Duration(minutes: 12, seconds: 40));
      expect(ok.steps.map((s) => s.message!.text), ['Check the destination (Sandisk 128G)', 'Copy files', 'Clean up versions older than 30 days']);
      expect(ok.fileErrors.single.path, 'photos/locked.db');
      expect(ok.hasLog, isTrue);
      expect(waiting.isFinal, isFalse);
      expect((waiting.guard!.guard, waiting.guard!.pct, waiting.guard!.limit, waiting.guard!.count), ('delete', 50.0, 10, 24090));
      expect(waiting.errorCode, 'delete_guard');
    });

    test('GET /runs/:id: live progress while running', () {
      final r = BackupRun.fromJson(_example('runs_get'));
      final live = r.live!;
      expect((live.bytes, live.totalBytes, live.files, live.totalFiles, live.speedBps, live.etaSec), (8100000000, 11900000000, 3210, 4700, 12300000, 360));
      expect(live.ratio, closeTo(0.68, 0.01));
      expect(live.currentFile, 'photos/2024/IMG_2231.HEIC');
      expect(r.steps[1].state, 'active');
    });

    test('GET /runs/:id/log: message lines, raw lines, the next offset', () {
      final page = LogPage.fromJson(_example('runs_log'));
      expect((page.nextOffset, page.done), (4096, false));
      expect(page.lines[0].text, 'Copied photos/2024/IMG_2231.HEIC');
      expect(page.lines[1].level, 'error');
      expect(page.lines[1].text, "Couldn't copy photos/locked.db: Read or write error");
      expect(page.lines[2].text, 'INFO  : Transferred: 8.1 GiB / 11.9 GiB, 68%');
    });

    test('GET /runs/:id/preview', () {
      final p = PreviewPage.fromJson(_example('runs_preview'));
      expect((p.add, p.update, p.delete, p.bytesAdd, p.total, p.nextOffset), (4120, 12, 318, 11200000000, 4450, 200));
      expect([for (final i in p.items) i.op], ['add', 'update', 'delete']);
    });

    test('versions, browse, locations, cron preview, run started', () {
      final v = [for (final x in _example('jobs_versions') as List) BackupVersion.fromJson(x)];
      expect(v.map((x) => x.kind), ['current', 'recycle']);
      expect(v[0].label(), 'Current copy');
      expect(v[1].label(), startsWith('Deleted or replaced on '));
      expect(v[1].meta, '14 files · 76.3 MB');
      final b = BrowseResult.fromJson(_example('jobs_versions_browse'));
      expect((b.path, b.entries.length, b.entries.first.dir, b.truncated), ('old-phone', 2, true, false));
      final locs = [for (final l in _example('locations') as List) BackupLocation.fromJson(l)];
      expect(locs.map((l) => l.kind).toSet(), {'volume', 'usb', 'merge', 'cloud'});
      final sys = locs.firstWhere((l) => l.systemDisk);
      expect(sys.presets.map((p) => p.id), ['data:Documents', 'appdata:immich']);
      final tera = locs.firstWhere((l) => l.provider == 'terabox');
      expect(tera.free, isNull, reason: 'unknown free space stays unknown');
      final c = CronPreview.fromJson(_example('cron_preview'));
      expect((c.valid, c.message!.text, c.next.length), (true, 'Every day at 03:00', 5));
      expect((_example('jobs_run') as Map)['run_id'], 'run_01J8Z3Q4K5M6N7P8Q9R0S1T2V3');
    });

    test('an endpoint goes back to the server as it came', () {
      final j = BackupJob.fromJson(_example('jobs_list') is List ? (_example('jobs_list') as List).first : null);
      expect(j.dest.toJson(), (((_example('jobs_list') as List).first as Map)['dest'] as Map));
    });
  });

  group('wording, as the web renders the server keys', () {
    test('a run summary with sizes, counts and nested keys', () {
      expect(const BackupMessage('backup.run.summary.ok', {'added': 4120, 'changed': 12, 'deleted': 318, 'bytes': 11200000000}).text,
          '4,120 added, 12 changed, 318 moved to recycle · 10.4 GB');
      expect(const BackupMessage('backup.run.summary.failed', {'reason_key': 'backup.err.cloud_auth.title'}).text, startsWith('Failed: '));
      expect(const BackupMessage('backup.run.summary.failed', {'reason_key': 'backup.err.cloud_auth.title'}).text, isNot(contains('backup.err')));
    });

    test('plurals pick their form', () {
      expect(btc('backup.overview.jobs_count', 1), '1 job');
      expect(btc('backup.overview.jobs_count', 3), '3 jobs');
      expect(btc('backup.ver.files', 1200), '1,200 files');
    });

    test('an unknown key comes back as itself', () {
      expect(bt('backup.nope'), 'backup.nope');
    });

    test('schedules in words, like the server says them', () {
      expect(cronText('0 3 * * *'), 'Every day at 03:00');
      expect(cronText('0 3 * * 0'), 'Every Sunday at 03:00');
      expect(cronText('30 1 * * 1-5'), 'On Mon, Tue, Wed, Thu and Fri at 01:30');
      expect(cronText('*/15 * * * *'), 'Every 15 minutes');
      expect(cronText('5 */6 * * *'), 'Every 6 hours at minute 5');
      expect(cronText('0 4 1 * *'), 'On day 1 of every month at 04:00');
      expect(cronText('@daily'), 'Every day at 00:00');
      expect(cronText('0 3 * 1 *'), 'Custom schedule: 0 3 * 1 *');
    });
  });

  group('what needs attention (state.js)', () {
    BackupJob job(Map<String, Object?> patch) => BackupJob.fromJson({...((_example('jobs_list') as List).first as Map).cast<String, Object?>(), ...patch});

    test('a waiting run comes first and opens the decision', () {
      final waiting = job({'name': 'Z', 'active_run': {'id': 'run_w', 'kind': 'backup', 'status': 'waiting_user'}});
      final failed = job({'name': 'A', 'health': 'problem', 'last_run': {'id': 'r', 'kind': 'backup', 'status': 'failed', 'summary': {'key': 'backup.run.summary.failed', 'args': {'reason_key': 'backup.err.no_space.title'}}}});
      final fine = job({'name': 'B'});
      final items = attentionItems([fine, failed, waiting]);
      expect(items.map((i) => (i.job.name, i.reason)), [('Z', 'waiting'), ('A', 'error')]);
      expect(items[0].runId, 'run_w');
      expect(items[1].code, 'no_space');
      expect(overallState([fine, failed, waiting]).state, 'problem');
      expect(overallState([fine]).state, 'ok');
      expect(overallState([]).state, 'empty');
    });

    test('offline and partial are warnings, not problems', () {
      final off = job({'health': 'offline'});
      final partial = job({'health': 'warning', 'last_run': {'id': 'r', 'kind': 'backup', 'status': 'partial'}});
      expect(attentionFor(off)!.code, 'dest_offline');
      expect(attentionFor(partial)!.reason, 'partial');
      expect(overallState([off, partial]).state, 'attention');
    });
  });

  group('errors', () {
    test('an ErrorBody becomes the web text for its code', () {
      final e = BackupError.from(ApiException('forbidden', statusCode: 403, data: {'error_code': 'forbidden'}));
      expect((e.code, e.title), ('forbidden', 'Administrators only'));
      final v = BackupError.from(ApiException('validation', statusCode: 400, data: {
        'error_code': 'validation',
        'field_errors': {'dest.sub_path': 'path_not_allowed', 'triggers[1].cron': 'invalid_cron'},
      }));
      expect(v.fieldError('triggers'), 'This schedule isn\'t valid.');
      expect(v.fieldError('dest'), isNotNull);
      expect(v.fieldError('sources'), isNull);
    });

    test('an unreachable server says so, not "something went wrong"', () {
      final e = BackupError.from(ApiException("Couldn't reach nivaro.test.", kind: ApiErrorKind.network));
      expect(e.isUnreachable, isTrue);
      expect(e.title, "Couldn't reach nivaro.test.");
    });
  });
}
