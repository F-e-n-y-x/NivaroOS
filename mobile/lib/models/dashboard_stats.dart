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
}

class DashboardStats {
  final double cpuPercent;
  final int memTotal;
  final int memUsed;
  final double memUsedPercent;
  final List<DiskUsage> disks;

  DashboardStats({
    required this.cpuPercent,
    required this.memTotal,
    required this.memUsed,
    required this.memUsedPercent,
    required this.disks,
  });

  factory DashboardStats.fromUtilization(Map<String, dynamic> data) {
    final cpu = data['cpu'] as Map<String, dynamic>? ?? {};
    final mem = data['mem'] as Map<String, dynamic>? ?? {};
    return DashboardStats(
      cpuPercent: (cpu['percent'] as num?)?.toDouble() ?? 0,
      memTotal: (mem['total'] as num?)?.toInt() ?? 0,
      memUsed: (mem['used'] as num?)?.toInt() ?? 0,
      memUsedPercent: (mem['usedPercent'] as num?)?.toDouble() ?? 0,
      disks: const [],
    );
  }

  DashboardStats withDisks(List<DiskUsage> disks) => DashboardStats(
        cpuPercent: cpuPercent,
        memTotal: memTotal,
        memUsed: memUsed,
        memUsedPercent: memUsedPercent,
        disks: disks,
      );
}
