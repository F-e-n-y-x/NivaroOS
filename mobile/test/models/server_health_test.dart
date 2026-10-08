// Which checks the Server health page (and Home's header and "Needs
// attention") shows as a problem, as fine, or as not checked.
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/dashboard_stats.dart';

import '../screenshots/harness.dart' show fixture;

const _gb = 1 << 30;

DashboardStats _stats({double? temp = 48, int memTotal = 32 * _gb, int memAvailable = 20 * _gb, List<DiskUsage>? disks}) => DashboardStats(
      cpuPercent: 5,
      cpuPerCore: const [],
      cpuCores: 6,
      cpuModelName: '',
      cpuMhz: 0,
      cpuTemperature: temp,
      memTotal: memTotal,
      memUsed: memTotal - memAvailable,
      memFree: memAvailable,
      memAvailable: memAvailable,
      memUsedPercent: memTotal == 0 ? 0 : (memTotal - memAvailable) / memTotal * 100,
      netSamples: const [],
      disks: disks ?? [_disk('tank', 30, 100)],
    );

DiskUsage _disk(String label, int usedGb, int sizeGb) =>
    DiskUsage(mountPoint: '/DATA/$label', label: label, percent: '', sizeBytes: sizeGb * _gb, usedBytes: usedGb * _gb);

const _upToDate = UpdateSummary(serverUpdate: false, packages: 0);
const _apps = AppCounts(running: 3, stopped: []);
const _drives = [
  DriveHealth(name: 'sda', model: 'System', healthy: true, system: true),
  DriveHealth(name: 'sdb', model: 'WDC WD20EZAZ', healthy: true, type: 'HDD', temperature: 36),
];

/// Everything fine; each test changes one thing.
ServerHealth _health({
  DashboardStats? stats,
  bool noStats = false,
  UpdateSummary? updates = _upToDate,
  List<BackupJobBrief> backups = const [BackupJobBrief(name: 'Photos', health: 'ok')],
  bool? backupsInstalled = true,
  AppCounts? apps = _apps,
  List<DriveHealth>? drives = _drives,
  TailnetState? tailnet = TailnetState.connected,
  SharingBrief? sharing,
}) =>
    buildHealth(
      stats: noStats ? null : (stats ?? _stats()),
      updates: updates,
      backups: backups,
      backupsInstalled: backupsInstalled,
      apps: apps,
      drives: drives,
      tailnet: tailnet,
      tailnetIp: '100.64.0.1',
      sharing: sharing,
    );

Iterable<String> _titles(List<HealthCheck> l) => l.map((c) => c.title);

void main() {
  group('all good', () {
    final h = _health();

    test('nothing needs attention and every check passed', () {
      expect(h.allGood, isTrue);
      expect(h.worst, isNull);
      expect(h.verdict, 'All good');
      expect(h.unchecked, isEmpty);
      expect(_titles(h.fine), ['Processor temperature', 'Memory', 'tank', 'WDC WD20EZAZ', 'Photos', 'NivaroOS', 'System packages', 'Apps', 'Tailscale']);
    });

    test('the facts behind each check', () {
      String detail(String title) => h.fine.firstWhere((c) => c.title == title).detail;
      expect(detail('Processor temperature'), '48\u00A0°C · Normal');
      expect(detail('Memory'), '38% in use');
      expect(detail('tank'), '30% used · 70.0 GB free');
      expect(detail('WDC WD20EZAZ'), 'Healthy · HDD · 36\u00A0°C');
      expect(detail('Tailscale'), 'Connected · 100.64.0.1');
      expect(detail('Apps'), 'All 3 running');
    });

    test('the boot drive is not counted as a checked drive (the server does not read its health)', () {
      expect(_titles(h.fine), isNot(contains('sda')));
      expect(_titles(h.unchecked), isNot(contains('sda')));
    });
  });

  group('processor temperature', () {
    test('80 °C is a warning, 90 °C a problem', () {
      final warm = _health(stats: _stats(temp: 84)).attention.single;
      expect((warm.kind, warm.severity, warm.title), (AttentionKind.temperature, AttentionSeverity.warning, 'The processor is running hot'));
      final hot = _health(stats: _stats(temp: 93)).attention.single;
      expect((hot.severity, hot.title), (AttentionSeverity.error, 'The processor is very hot'));
      expect(hot.detail, startsWith('93\u00A0°C'));
    });

    test('no sensor is not checked, never "fine"', () {
      final h = _health(stats: _stats(temp: null));
      expect(h.allGood, isTrue);
      expect(_titles(h.fine), isNot(contains('Processor temperature')));
      expect(h.unchecked.firstWhere((c) => c.title == 'Processor temperature').detail, 'No temperature sensor found');
    });
  });

  group('memory', () {
    test('90% in use is a warning, 95% a problem (cache counts as free)', () {
      expect(_health(stats: _stats(memAvailable: 3 * _gb)).attention.single.severity, AttentionSeverity.warning);
      final full = _health(stats: _stats(memAvailable: _gb)).attention.single;
      expect((full.kind, full.severity, full.title), (AttentionKind.memory, AttentionSeverity.error, 'Memory is almost full'));
    });
  });

  group('drives', () {
    test('space: 80% is filling up, 90% almost full, the rest fine', () {
      final h = _health(stats: _stats(disks: [_disk('tank', 50, 100), _disk('blue', 85, 100), _disk('tower', 95, 100)]));
      expect(h.attention.map((a) => (a.title, a.severity)), [
        ('tower is almost full', AttentionSeverity.error),
        ('blue is filling up', AttentionSeverity.warning),
      ]);
      expect(_titles(h.fine), contains('tank'));
    });

    test('a failing drive is a problem; asleep or unknown is not checked', () {
      final h = _health(drives: const [
        DriveHealth(name: 'sdb', model: 'WDC WD20EZAZ', healthy: false),
        DriveHealth(name: 'sdc', model: 'TOSHIBA', sleeping: true),
        DriveHealth(name: 'sdd'),
      ]);
      final bad = h.attention.single;
      expect((bad.kind, bad.severity, bad.title), (AttentionKind.driveHealth, AttentionSeverity.error, 'WDC WD20EZAZ reports a problem'));
      expect(h.unchecked.map((c) => (c.title, c.detail)), containsAll([('TOSHIBA', 'Asleep · Health is checked when it wakes up'), ('sdd', 'Health unknown')]));
    });

    test("a mounted drive's health is told on its space row, under its own name", () {
      final blue = DiskUsage(mountPoint: '/DATA/blue', label: 'blue', percent: '', sizeBytes: 100 * _gb, usedBytes: 20 * _gb, model: 'WDC WD20EZAZ');
      final ok = _health(stats: _stats(disks: [blue]));
      expect(_titles(ok.fine), isNot(contains('WDC WD20EZAZ')));
      expect(ok.fine.firstWhere((c) => c.title == 'blue').detail, '20% used · 80.0 GB free · Healthy');
      final bad = _health(stats: _stats(disks: [blue]), drives: const [DriveHealth(name: 'sdb', model: 'WDC WD20EZAZ', healthy: false)]);
      expect(bad.attention.single.title, 'blue reports a problem');
      expect(bad.fine.firstWhere((c) => c.title == 'blue').detail, '20% used · 80.0 GB free');
    });

    test("an unreadable drive list says it couldn't check", () {
      expect(_titles(_health(drives: null).unchecked), contains('Drive health'));
    });

    test('reads GET /v1/disks', () {
      final drives = DriveHealth.fromDisksApi(fixture('v1/disks')['data']);
      expect(drives.map((d) => d.label), ['sda', 'WDC WD20EZAZ-00G', 'TOSHIBA MQ04ABF1', 'WDC WD40PURZ-85A']);
      expect(drives.first.system, isTrue);
      expect(drives[1].healthy, isTrue);
      expect(drives[1].temperature, 36);
      expect(drives[3].temperature, isNull, reason: '0 °C means not reported');
      expect(drives[3].sleeping, isTrue);
    });
  });

  group('backups', () {
    test('failed, paused and overdue jobs need attention; a finished one shows when it last ran', () {
      final at = DateTime.utc(2026, 9, 25, 3);
      final h = _health(backups: [
        const BackupJobBrief(name: 'A', health: 'problem', lastStatus: 'failed'),
        const BackupJobBrief(name: 'B', health: 'offline', destLabel: 'Sandisk'),
        const BackupJobBrief(name: 'C', health: 'warning'),
        BackupJobBrief(name: 'D', health: 'ok', lastStatus: 'success', lastEndedAt: at),
        const BackupJobBrief(name: 'E', health: 'disabled'),
      ]);
      expect(h.attention.map((a) => a.title), ["A didn't finish", 'B is paused', 'C needs a look']);
      final d = h.fine.firstWhere((c) => c.title == 'D');
      expect((d.detail, d.at), ('Last backup', at));
      expect(h.unchecked.firstWhere((c) => c.title == 'E').detail, 'Backup · Turned off');
    });

    test('not installed: no backup checks at all; unreachable: not checked', () {
      final none = _health(backups: const [], backupsInstalled: false);
      expect([...none.fine, ...none.unchecked].where((c) => c.area == HealthArea.backups), isEmpty);
      expect(_titles(_health(backupsInstalled: null).unchecked), contains('Backups'));
    });

    test('reads the last run time from GET /v1/backup/jobs', () {
      final jobs = (fixture('v1/backup/jobs')['data'] as List).map((e) => BackupJobBrief.fromJson(e as Map<String, dynamic>)).toList();
      expect(jobs[1].lastEndedAt, DateTime.parse('2026-09-25T03:12:40+05:30'));
    });
  });

  group('updates and apps', () {
    test('an update is worth a look; a security update is a warning', () {
      final h = _health(updates: const UpdateSummary(serverUpdate: true, serverVersion: '1.4.0', packages: 12, security: 2));
      expect(h.attention.map((a) => (a.title, a.severity)), [
        ('12 system updates', AttentionSeverity.warning),
        ('NivaroOS 1.4.0 is available', AttentionSeverity.info),
      ]);
      expect(h.verdict, '2 things need attention');
    });

    test('only "worth a look" items read as such', () {
      final h = _health(updates: const UpdateSummary(serverUpdate: true, packages: 0));
      expect(h.worst, AttentionSeverity.info);
      expect(h.verdict, '1 thing to look at');
    });

    test('a failed update check is not checked, not "up to date"', () {
      final h = _health(updates: const UpdateSummary());
      expect(_titles(h.fine), isNot(contains('NivaroOS')));
      expect(_titles(h.unchecked), containsAll(['NivaroOS updates', 'System updates']));
    });

    test('stopped apps are the owner\'s choice, not a problem', () {
      final h = _health(apps: const AppCounts(running: 1, stopped: ['searxng', 'comfyui', 'x']));
      expect(h.attention.where((a) => a.kind == AttentionKind.apps), isEmpty);
      expect(h.fine.singleWhere((c) => c.area == HealthArea.apps).detail, '1 running · 3 stopped');
    });

    test('apps that crashed or keep restarting need attention', () {
      final a = _health(apps: const AppCounts(running: 1, stopped: ['x'], failed: ['searxng', 'comfyui'])).attention.single;
      expect((a.kind, a.severity, a.title, a.detail), (AttentionKind.apps, AttentionSeverity.warning, '2 apps failed', 'searxng and comfyui'));
    });

    test('the app grid: failed, restarting and dead are failed; exited is stopped', () {
      final c = AppCounts.fromAppGrid([
        {'name': 'a', 'status': 'running'},
        {'name': 'b', 'status': 'exited', 'failed': false, 'exit_code': 137},
        {'name': 'c', 'status': 'exited', 'failed': true, 'exit_code': 1},
        {'name': 'd', 'status': 'restarting'},
        {'name': 'e', 'status': 'exited'}, // an older server: no failed field
      ]);
      expect(c.running, 1);
      expect(c.stopped, ['b', 'e']);
      expect(c.failed, ['c', 'd']);
    });
  });

  group('Tailscale and sharing', () {
    test('signed out needs attention; off or connecting is not checked; not installed is left out', () {
      expect(_health(tailnet: TailnetState.signedOut).attention.single.kind, AttentionKind.tailscale);
      expect(_health(tailnet: TailnetState.off).unchecked.single.detail, 'Switched off');
      expect(_health(tailnet: TailnetState.connecting).unchecked.single.detail, 'Connecting');
      final none = _health(tailnet: TailnetState.notInstalled);
      expect([...none.fine, ...none.unchecked].where((c) => c.area == HealthArea.tailscale), isEmpty);
    });

    test('sharing shows while it runs, and when Android stopped it', () {
      expect(_titles(_health(sharing: const SharingBrief(running: true)).fine), contains('Companion sharing'));
      expect(_health(sharing: const SharingBrief(running: false, stopReason: 'timeout')).attention.single.kind, AttentionKind.sharing);
      final byUser = _health(sharing: const SharingBrief(running: false, stopReason: 'stopped'));
      expect(byUser.allGood, isTrue);
      expect([...byUser.fine, ...byUser.unchecked].where((c) => c.area == HealthArea.sharing), isEmpty);
    });
  });

  test('before the first reading, the live checks are not checked', () {
    final h = _health(noStats: true);
    expect(_titles(h.unchecked), containsAll(['Processor temperature', 'Memory']));
    expect(h.fine.where((c) => c.area == HealthArea.temperature || c.area == HealthArea.memory), isEmpty);
  });

  test('worst first across every source', () {
    final h = _health(
      stats: _stats(temp: 85, disks: [_disk('tower', 95, 100)]),
      updates: const UpdateSummary(serverUpdate: true, packages: 0),
    );
    expect(h.attention.map((a) => a.severity), [AttentionSeverity.error, AttentionSeverity.warning, AttentionSeverity.info]);
    expect(h.worst, AttentionSeverity.error);
  });

  test('buildAttention is the same list Home shows', () {
    final updates = const UpdateSummary(serverUpdate: true, packages: 3);
    final disks = [_disk('blue', 85, 100)];
    expect(
      buildAttention(updates: updates, disks: disks).map((a) => a.title),
      buildHealth(stats: _stats(disks: disks), updates: updates, drives: const [], tailnet: TailnetState.notInstalled).attention.map((a) => a.title),
    );
  });

  test('a drive that failed to mount after a power cut needs attention, with Repair', () {
    final problems = MountProblem.fromFstabApi({
      'managed': [
        {'mount_point': '/DATA/tank', 'mounted': true},
        {
          'mount_point': '/DATA/tower',
          'mounted': false,
          'problem': {'reason': 'damaged', 'message': "tower couldn't be mounted: its file system is damaged", 'detail': r'$MFTMirr does not match $MFT (record 3).', 'repairable': true},
        },
        {'mount_point': '/DATA/usb', 'mounted': false, 'problem': {'reason': 'missing', 'message': "usb isn't connected"}},
        {'mount_point': '/DATA/blue', 'mounted': false, 'repair': {'running': true}},
      ],
    });
    expect(problems.map((p) => p.name), ['tower', 'usb', 'blue']);
    final attention = buildHealth(stats: _stats(), updates: _upToDate, apps: _apps, drives: const [], tailnet: TailnetState.notInstalled, mountProblems: problems).attention;
    final rows = attention.where((a) => a.kind == AttentionKind.driveMount).map((a) => (a.severity, a.title, a.detail)).toList();
    expect(rows, [
      (AttentionSeverity.error, "tower couldn't be mounted", 'Tap to repair it'),
      (AttentionSeverity.warning, "usb isn't connected", 'Apps that use it wait until it is back'),
      (AttentionSeverity.info, 'Repairing blue', 'Keep the drive connected'),
    ]);
    expect(problems.first.repairable && problems.first.detail.contains(r'$MFTMirr'), isTrue);
  });
}
