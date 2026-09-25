/// One reading from the GPU sidecar (`GET /v1/gpu/gpu-stats`, the same
/// route the web UI's GPU widget polls). The sidecar answers an error, or
/// no name, on a machine without a dedicated GPU and working driver; then
/// there is no reading and Home shows no GPU card.
class GpuStats {
  const GpuStats({
    required this.name,
    this.driverVersion = '',
    this.utilizationPercent = 0,
    this.memoryUsedMib = 0,
    this.memoryTotalMib = 0,
    this.temperatureC,
    this.powerDrawW,
    this.powerLimitW,
    this.processes = const [],
  });

  final String name;
  final String driverVersion;
  final double utilizationPercent;
  final int memoryUsedMib;
  final int memoryTotalMib;
  final double? temperatureC;
  final double? powerDrawW;
  final double? powerLimitW;
  final List<GpuProcess> processes;

  int get memoryUsedBytes => memoryUsedMib * 1024 * 1024;
  int get memoryTotalBytes => memoryTotalMib * 1024 * 1024;

  /// Null when [json] is not a reading (an error, or no GPU).
  static GpuStats? tryParse(Object? json) {
    if (json is! Map) return null;
    final name = json['name']?.toString() ?? '';
    if (json['error'] != null || name.isEmpty) return null;
    double? positive(Object? v) => v is num && v > 0 ? v.toDouble() : null;
    return GpuStats(
      name: name,
      driverVersion: json['driver_version']?.toString() ?? '',
      utilizationPercent: (json['utilization_percent'] as num?)?.toDouble() ?? 0,
      memoryUsedMib: (json['memory_used_mib'] as num?)?.toInt() ?? 0,
      memoryTotalMib: (json['memory_total_mib'] as num?)?.toInt() ?? 0,
      temperatureC: positive(json['temperature_c']),
      powerDrawW: positive(json['power_draw_w']),
      powerLimitW: positive(json['power_limit_w']),
      processes: [
        for (final p in (json['processes'] as List<dynamic>? ?? const []))
          if (p is Map)
            GpuProcess(
              pid: (p['pid'] as num?)?.toInt() ?? 0,
              command: p['command']?.toString() ?? '',
              utilizationPercent: (p['utilization_percent'] as num?)?.toDouble() ?? 0,
            ),
      ]..sort((a, b) => b.utilizationPercent.compareTo(a.utilizationPercent)),
    );
  }
}

class GpuProcess {
  const GpuProcess({required this.pid, required this.command, required this.utilizationPercent});

  final int pid;
  final String command;
  final double utilizationPercent;
}
