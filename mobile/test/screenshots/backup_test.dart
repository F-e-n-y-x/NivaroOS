// Backup & Sync screens in every style (Rack, Tonal, Console), light and
// true black, at 412 and 360 dp and at 200% text, against the fake server
// with the contract-shaped fixtures in fixtures/backup/ (see the README).
//
//   flutter test test/screenshots/backup_test.dart --update-goldens
//
// PNGs: goldens/backup/<style>/<screen>_<mode>_<size>[_text2x].png.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/backup/backup_models.dart';
import 'package:nivaroos_mobile/screens/backup/backup_decide_screen.dart';
import 'package:nivaroos_mobile/screens/backup/backup_job_screen.dart';
import 'package:nivaroos_mobile/screens/backup/backup_restore_screen.dart';
import 'package:nivaroos_mobile/screens/backup/backup_run_screen.dart';
import 'package:nivaroos_mobile/screens/backup/backup_screen.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'harness.dart';

/// The fake server's Backup & Sync: four jobs (fine, failed, running,
/// waiting for a decision), a job's detail, runs, versions and a version's
/// files, a running run with its log, a waiting run with its plan, and
/// the locations.
final Map<String, Object> backupServer = {
  'GET /v1/backup/jobs': fixture('backup/jobs'),
  'GET /v1/backup/jobs/bk_photos': fixture('backup/job_photos'),
  'GET /v1/backup/jobs/bk_docs': fixture('backup/job_docs'),
  'GET /v1/backup/jobs/bk_phone': fixture('backup/job_phone'),
  'GET /v1/backup/runs': fixture('backup/runs_photos'),
  'GET /v1/backup/runs/run_photos_1': fixture('backup/run_photos_1'),
  'GET /v1/backup/runs/run_docs_2': fixture('backup/run_docs_2'),
  'GET /v1/backup/runs/run_docs_2/log': fixture('backup/log_docs_2'),
  'GET /v1/backup/runs/run_phone_2': fixture('backup/run_phone_2'),
  'GET /v1/backup/runs/run_phone_2/preview': fixture('backup/preview_phone_2'),
  'GET /v1/backup/jobs/bk_photos/versions': fixture('backup/versions_photos'),
  'GET /v1/backup/jobs/bk_docs/versions': {'success': 200, 'message': 'ok', 'data': []},
  'GET /v1/backup/jobs/bk_phone/versions': {'success': 200, 'message': 'ok', 'data': []},
  'GET /v1/backup/jobs/bk_photos/versions/current/browse': fixture('backup/browse_photos'),
  'GET /v1/backup/locations': fixture('backup/locations'),
  'POST /v1/backup/jobs/bk_photos/run': fixture('backup/run_started'),
  'POST /v1/backup/jobs/bk_photos/restore': fixture('backup/restore_started'),
};

/// The module isn't installed: its health route isn't there.
final Map<String, Object> backupMissing = {
  'GET /v1/backup/health': const FakeResponse({'success': 404, 'message': 'not found'}, status: 404),
};

BackupJob photosJob() => BackupJob.fromJson(fixture('backup/job_photos')['data']);

class _Shot {
  const _Shot(this.build, {this.overrides = const {}, this.full = true});
  final Widget Function() build;
  final Map<String, Object> overrides;

  /// Every mode x size x text scale; otherwise light and black at 412.
  final bool full;
}

final Map<String, _Shot> _shots = {
  'backup_overview': _Shot(() => const BackupScreen()),
  'backup_job': _Shot(() => const BackupJobScreen(jobId: 'bk_photos')),
  'backup_restore': _Shot(() => BackupRestoreScreen(jobs: [photosJob()], job: photosJob())),
  'backup_run': _Shot(() => const BackupRunScreen(runId: 'run_docs_2'), full: false),
  'backup_decide': _Shot(() => const BackupDecideScreen(runId: 'run_phone_2', jobId: 'bk_phone'), full: false),
  'backup_not_installed': _Shot(() => const BackupScreen(), overrides: backupMissing, full: false),
};

const _styles = [DesignDirection.rack, DesignDirection.tonal, DesignDirection.console];

void main() {
  setUp(() async {
    BackupScreen.clearCache();
    await signIn();
  });

  for (final d in _styles) {
    for (final MapEntry(key: name, value: s) in _shots.entries) {
      for (final m in const [AppThemeMode.light, AppThemeMode.black]) {
        final sizes = s.full ? const [(phone, 1.0), (smallPhone, 1.0), (phone, 2.0)] : const [(phone, 1.0)];
        for (final (size, scale) in sizes) {
          testWidgets('${d.name} $name ${m.name} ${size.width.toInt()} ${scale}x', (tester) {
            final a = Appearance(mode: m, direction: d);
            return shoot(
              tester,
              dir: 'backup/${d.name}',
              name: name,
              screen: s.build(),
              brightness: m == AppThemeMode.light ? Brightness.light : Brightness.dark,
              themeName: m.name,
              appearance: a,
              size: size,
              textScale: scale,
              pushed: true,
              overrides: {...backupServer, ...s.overrides},
            );
          });
        }
      }
    }
  }
}
