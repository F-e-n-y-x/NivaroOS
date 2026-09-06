class DiskUsage {
  final String mountPoint;
  final String label;
  final String percent;
  final int sizeBytes;
  final int usedBytes;

  DiskUsage({
    required this.mountPoint,
    required this.label,
    required this.percent,
    required this.sizeBytes,
    required this.usedBytes,
  });

  factory DiskUsage.fromJson(Map<String, dynamic> j) => DiskUsage(
        mountPoint: j['mount_point'] as String? ?? '',
        label: j['label'] as String? ?? '',
        percent: (j['percent'] as String?) ?? '0%',
        sizeBytes: (j['size_bytes'] as num?)?.toInt() ?? 0,
        usedBytes: (j['used_bytes'] as num?)?.toInt() ?? 0,
      );

  double get fraction {
    final cleaned = percent.replaceAll('%', '').trim();
    final v = double.tryParse(cleaned) ?? 0;
    return (v / 100).clamp(0, 1);
  }

  /// EFI system partitions are real disk entries the sys API reports, but
  /// they're not a place a user ever wants to browse or think of as "a
  /// drive" - hide them from Storage/Drives sections everywhere.
  bool get isSystemPartition => label.toLowerCase().contains('efi') || mountPoint.toLowerCase().contains('efi');
}

/// A single network interface's cumulative counters, as reported by
/// `/v1/sys/utilization`'s `net` array - these are running totals since
/// boot, not a rate, so DashboardScreen diffs two samples itself to show a
/// throughput number.
class NetSample {
  final String name;
  final int bytesSent;
  final int bytesRecv;
  NetSample({required this.name, required this.bytesSent, required this.bytesRecv});

  factory NetSample.fromJson(Map<String, dynamic> j) => NetSample(
        name: j['name'] as String? ?? '',
        bytesSent: (j['bytesSent'] as num?)?.toInt() ?? 0,
        bytesRecv: (j['bytesRecv'] as num?)?.toInt() ?? 0,
      );
}

class DashboardStats {
  final double cpuPercent;
  final double? cpuTemperature;
  final int memTotal;
  final int memUsed;
  final double memUsedPercent;
  final List<NetSample> netSamples;
  final List<DiskUsage> disks;

  DashboardStats({
    required this.cpuPercent,
    required this.cpuTemperature,
    required this.memTotal,
    required this.memUsed,
    required this.memUsedPercent,
    required this.netSamples,
    required this.disks,
  });

  /// The main, non-loopback interface - the one worth showing a single
  /// throughput number for, mirroring the reference app's single "Network"
  /// card (it doesn't enumerate every interface either).
  NetSample? get primaryNet {
    for (final s in netSamples) {
      if (s.name != 'lo' && !s.name.startsWith('veth') && !s.name.startsWith('docker') && !s.name.startsWith('br-')) {
        return s;
      }
    }
    return netSamples.isEmpty ? null : netSamples.first;
  }

  factory DashboardStats.fromUtilization(Map<String, dynamic> data) {
    final cpu = data['cpu'] as Map<String, dynamic>? ?? {};
    final mem = data['mem'] as Map<String, dynamic>? ?? {};
    final netRaw = data['net'] as List<dynamic>? ?? [];
    double? temperature;
    final tempRaw = cpu['temperature'];
    if (tempRaw is num) temperature = tempRaw.toDouble();
    return DashboardStats(
      cpuPercent: (cpu['percent'] as num?)?.toDouble() ?? 0,
      cpuTemperature: temperature,
      memTotal: (mem['total'] as num?)?.toInt() ?? 0,
      memUsed: (mem['used'] as num?)?.toInt() ?? 0,
      memUsedPercent: (mem['usedPercent'] as num?)?.toDouble() ?? 0,
      netSamples: netRaw.map((e) => NetSample.fromJson(e as Map<String, dynamic>)).toList(),
      disks: const [],
    );
  }

  int get storageUsed => disks.where((d) => !d.isSystemPartition).fold(0, (sum, d) => sum + d.usedBytes);
  int get storageTotal => disks.where((d) => !d.isSystemPartition).fold(0, (sum, d) => sum + d.sizeBytes);
  double get storageFraction {
    final total = storageTotal;
    if (total <= 0) return 0.0;
    return (storageUsed / total).clamp(0.0, 1.0);
  }
  String get storagePercentText {
    final total = storageTotal;
    if (total <= 0) return '0%';
    return '${(storageFraction * 100).toStringAsFixed(0)}%';
  }

  DashboardStats withDisks(List<DiskUsage> disks) => DashboardStats(
        cpuPercent: cpuPercent,
        cpuTemperature: cpuTemperature,
        memTotal: memTotal,
        memUsed: memUsed,
        memUsedPercent: memUsedPercent,
        netSamples: netSamples,
        disks: disks,
      );
}
