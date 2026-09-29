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
    this.stale = false,
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

  /// The sidecar's query failed for a moment and these are its last good
  /// numbers (an idle card in its lowest power state can do that).
  final bool stale;

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
      stale: json['stale'] == true,
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

/// Whether Home shows a GPU card, from one poll's outcome at a time. One
/// failed or empty reply used to hide the card for good (until
/// pull-to-refresh); an idle GPU can fail a reading now and then. So:
/// once a GPU has been seen, failures keep the last reading and polling
/// goes on; before that, only [missesBeforeNone] failures in a row mean
/// "this server has no GPU".
class GpuPresence {
  GpuPresence({this.missesBeforeNone = 3});

  final int missesBeforeNone;
  bool _seen = false;
  int _misses = 0;

  /// False once the server has shown it has no GPU: stop polling it.
  bool get worthPolling => _seen || _misses < missesBeforeNone;

  /// Records a poll: [reading] null for an error or no GPU. Returns what
  /// the card should show - the new reading, [previous] to keep the last
  /// one, or null for no card.
  GpuStats? record(GpuStats? reading, GpuStats? previous) {
    if (reading != null) {
      _seen = true;
      _misses = 0;
      return reading;
    }
    if (_seen) return previous;
    _misses++;
    return null;
  }

  /// Pull-to-refresh: ask again from scratch.
  void reset() {
    _seen = false;
    _misses = 0;
  }
}
