import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/dashboard_stats.dart';

import '../screenshots/harness.dart' show fixture;

DiskUsage _disk(String label, int used, int size) =>
    DiskUsage(mountPoint: '/DATA/$label', label: label, percent: '', sizeBytes: size, usedBytes: used);

void main() {
  group('DashboardStats.fromUtilization', () {
    final stats = DashboardStats.fromUtilization(fixture('v1/sys/utilization')['data'] as Map<String, dynamic>);

    test('reads the real server answer', () {
      expect(stats.cpuCores, 6);
      expect(stats.cpuThreads, 12);
      expect(stats.cpuModelName, 'AMD Ryzen 5 3600 6-Core Processor');
      expect(stats.cpuTemperature, 48);
      expect(stats.memUsedPercent, 42);
      expect(stats.memModules, hasLength(2));
      expect(stats.memModules.first.partNumber, 'F4-3200C16-16GVK');
      expect(stats.memModules.first.size, '16 GB');
      expect(stats.primaryNet?.name, 'enp7s0');
    });

    test('cache is what is neither used nor free', () {
      expect(stats.memCache, stats.memTotal - stats.memUsed - stats.memFree);
    });

    test('invents nothing when fields are missing', () {
      final empty = DashboardStats.fromUtilization(const {});
      expect(empty.cpuModelName, '');
      expect(empty.cpuCores, 0);
      expect(empty.cpuTemperature, isNull);
      expect(empty.storagePercentText, '—');
    });

    test('0 °C means no sensor', () {
      final s = DashboardStats.fromUtilization(const {
        'cpu': {'temperature': 0},
      });
      expect(s.cpuTemperature, isNull);
    });
  });

  group('DiskUsage', () {
    final disks = (fixture('v1/sys/disks-usage')['data'] as List).map((e) => DiskUsage.fromJson(e as Map<String, dynamic>)).toList();

    test('names the boot drive and leaves out EFI', () {
      final root = disks.firstWhere((d) => d.mountPoint == '/');
      expect(root.isSystem, isTrue);
      expect(root.label, 'System drive');
      expect(root.kind, 'ssd');
      expect(disks.firstWhere((d) => d.mountPoint == '/boot/efi').isSystemPartition, isTrue);
    });

    test('fraction comes from the bytes, not the rounded text', () {
      final blue = disks.firstWhere((d) => d.label == 'blue');
      expect(blue.fraction, closeTo(340616548352 / 2000396742656, 1e-9));
    });

    test('storage totals skip boot partitions', () {
      final s = DashboardStats.fromUtilization(const {}).withDisks(disks);
      expect(s.dataDisks, hasLength(4));
      expect(s.storageTotal, disks.where((d) => !d.isSystemPartition).fold<int>(0, (a, d) => a + d.sizeBytes));
    });
  });

  group('NetRate.between', () {
    final t0 = DateTime(2026, 9, 25, 14);
    NetSample n(int sent, int recv, [String name = 'eth0']) => NetSample(name: name, bytesSent: sent, bytesRecv: recv);

    test('bytes per second over the elapsed time', () {
      final r = NetRate.between(n(0, 0), t0, n(4000, 8000), t0.add(const Duration(seconds: 4)))!;
      expect(r.upBytesPerSec, 1000);
      expect(r.downBytesPerSec, 2000);
    });

    test('unknown, not zero, after a counter reset', () {
      expect(NetRate.between(n(5000, 5000), t0, n(10, 10), t0.add(const Duration(seconds: 4))), isNull);
    });

    test('unknown for the first sample or another interface', () {
      expect(NetRate.between(null, null, n(1, 1), t0), isNull);
      expect(NetRate.between(n(0, 0), t0, n(1, 1, 'wlan0'), t0.add(const Duration(seconds: 1))), isNull);
    });
  });

  group('HostInfo', () {
    test('trims uptime to two parts', () {
      final h = HostInfo.fromJson(fixture('v1/sys/hardware')['data'] as Map<String, dynamic>);
      expect(h.uptime, '4 days, 23 hours');
      expect(h.hostname, 'nivaro');
    });
  });

  group('AppCounts', () {
    test('counts running and names the rest', () {
      final a = AppCounts.fromAppGrid(fixture('v2/app_management/web/appgrid')['data'] as List);
      expect(a.stopped, contains('searxng'));
      expect(a.running + a.stopped.length, a.total);
    });
  });

  group('buildAttention', () {
    test('nothing to report is an empty list', () {
      expect(buildAttention(updates: const UpdateSummary(serverUpdate: false, packages: 0), disks: [_disk('a', 1, 10)]), isEmpty);
    });

    test('worst first: a full disk and a failed backup before updates', () {
      final items = buildAttention(
        updates: const UpdateSummary(serverUpdate: false, packages: 12, security: 2),
        disks: [_disk('tank', 95, 100), _disk('blue', 85, 100), _disk('ok', 10, 100)],
        backups: const [BackupJobBrief(name: 'Photos', health: 'problem', lastStatus: 'failed'), BackupJobBrief(name: 'Docs', health: 'ok')],
        apps: const AppCounts(running: 3, stopped: ['a', 'b', 'c', 'd']),
      );
      expect(items.map((e) => e.title), [
        'tank is almost full',
        "Photos didn't finish",
        'blue is filling up',
        '12 system updates',
        '4 apps are stopped',
      ]);
      expect(items.first.severity, AttentionSeverity.error);
      expect(items[2].severity, AttentionSeverity.warning);
      expect(items[3].severity, AttentionSeverity.warning, reason: 'security updates are a warning');
      expect(items.last.detail, 'a, b and 2 more');
    });

    test('a disk of unknown size is not reported as full', () {
      expect(buildAttention(disks: [DiskUsage(mountPoint: '/x', label: 'x', percent: '99%', sizeBytes: 0, usedBytes: 0)]), isEmpty);
    });

    test('offline backup destination is a warning that names it', () {
      final items = buildAttention(backups: const [BackupJobBrief(name: 'Photos', health: 'offline', destLabel: 'Sandisk')]);
      expect(items.single.severity, AttentionSeverity.warning);
      expect(items.single.detail, contains('Sandisk'));
    });

    test('a NivaroOS update names its version', () {
      final items = buildAttention(updates: const UpdateSummary(serverUpdate: true, serverVersion: '1.1.0'));
      expect(items.single.title, 'NivaroOS 1.1.0 is available');
    });
  });
}
