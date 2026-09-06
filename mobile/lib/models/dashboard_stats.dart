int _safeInt(dynamic val) {
  if (val == null) return 0;
  if (val is int) return val;
  if (val is double) return val.round();
  if (val is num) return val.toInt();
  if (val is String) {
    final direct = int.tryParse(val);
    if (direct != null) return direct;
    final d = double.tryParse(val);
    if (d != null) return d.round();
    // String like '1024' or '1023410176'
    final digits = val.replaceAll(RegExp(r'[^0-9]'), '');
    if (digits.isNotEmpty) {
      return int.tryParse(digits) ?? 0;
    }
  }
  return 0;
}

double _safeDouble(dynamic val) {
  if (val == null) return 0.0;
  if (val is double) return val;
  if (val is int) return val.toDouble();
  if (val is num) return val.toDouble();
  if (val is String) {
    final cleaned = val.replaceAll('%', '').trim();
    final d = double.tryParse(cleaned);
    if (d != null) return d;
  }
  if (val is Map) {
    if (val.containsKey('value')) return _safeDouble(val['value']);
    if (val.containsKey('percent')) return _safeDouble(val['percent']);
  }
  return 0.0;
}

class DiskUsage {
  final String mountPoint;
  final String label;
  final String percent;
  final int sizeBytes;
  final int usedBytes;
  final String filesystem;
  final bool isUsb;

  DiskUsage({
    required this.mountPoint,
    required this.label,
    required this.percent,
    required this.sizeBytes,
    required this.usedBytes,
    this.filesystem = '',
    this.isUsb = false,
  });

  factory DiskUsage.fromJson(Map<String, dynamic> j) {
    final mp = j['mount_point']?.toString() ?? j['mountpoint']?.toString() ?? j['path']?.toString() ?? '';
    final size = _safeInt(j['size_bytes'] ?? j['size'] ?? j['total_bytes'] ?? j['total']);
    final avail = _safeInt(j['avail_bytes'] ?? j['avail'] ?? j['free_bytes'] ?? j['free']);
    final used = _safeInt(j['used_bytes'] ?? j['used'] ?? (size > avail && avail > 0 ? size - avail : 0));
    final pct = j['percent']?.toString() ?? j['used_percent']?.toString() ?? (size > 0 ? '${((used / size) * 100).toStringAsFixed(1)}%' : '0%');
    final rawLabel = j['label']?.toString() ?? j['name']?.toString() ?? j['disk_name']?.toString() ?? '';
    final label = rawLabel.isNotEmpty ? rawLabel : (mp.split('/').where((s) => s.isNotEmpty).lastOrNull ?? mp);
    final type = j['filesystem']?.toString() ?? j['fstype']?.toString() ?? j['type']?.toString() ?? '';
    final isUsb = j['is_usb'] == true || type.toLowerCase() == 'usb' || (j['model']?.toString().toLowerCase().contains('usb') ?? false);

    return DiskUsage(
      mountPoint: mp,
      label: label.isNotEmpty ? label : (isUsb ? 'USB Storage' : 'Local Drive'),
      percent: pct,
      sizeBytes: size,
      usedBytes: used,
      filesystem: type,
      isUsb: isUsb,
    );
  }

  static List<DiskUsage> fromStorageApi(dynamic raw) {
    final List<DiskUsage> results = [];
    if (raw is! List) return results;

    for (final item in raw) {
      if (item is! Map) continue;
      final map = Map<String, dynamic>.from(item);
      final diskName = map['disk_name']?.toString() ?? map['name']?.toString() ?? map['model']?.toString() ?? map['path']?.toString() ?? 'Disk';
      final diskType = map['fstype']?.toString() ?? map['type']?.toString() ?? '';
      final isDiskUsb = map['is_usb'] == true || diskType.toLowerCase() == 'usb' || (map['model']?.toString().toLowerCase().contains('usb') ?? false);

      final children = map['children'] as List<dynamic>?;
      if (children != null && children.isNotEmpty) {
        for (final child in children) {
          if (child is Map) {
            final cmap = Map<String, dynamic>.from(child);
            final mp = cmap['mount_point']?.toString() ?? cmap['mountpoint']?.toString() ?? cmap['path']?.toString() ?? '';
            if (mp.isEmpty) continue;
            final size = _safeInt(cmap['size_bytes'] ?? cmap['size'] ?? cmap['total_bytes'] ?? cmap['total']);
            final avail = _safeInt(cmap['avail_bytes'] ?? cmap['avail'] ?? cmap['free_bytes'] ?? cmap['free']);
            final used = _safeInt(cmap['used_bytes'] ?? cmap['used'] ?? (size > avail && avail > 0 ? size - avail : 0));
            final rawLabel = cmap['label']?.toString() ?? cmap['name']?.toString() ?? '';
            final label = rawLabel.isNotEmpty ? rawLabel : (mp.split('/').where((s) => s.isNotEmpty).lastOrNull ?? diskName);
            final pct = cmap['percent']?.toString() ?? (size > 0 ? '${((used / size) * 100).toStringAsFixed(1)}%' : '0%');

            final du = DiskUsage(
              mountPoint: mp,
              label: label.isNotEmpty ? label : (isDiskUsb ? 'USB Drive' : diskName),
              percent: pct,
              sizeBytes: size,
              usedBytes: used,
              filesystem: cmap['fstype']?.toString() ?? cmap['type']?.toString() ?? cmap['filesystem']?.toString() ?? diskType,
              isUsb: isDiskUsb,
            );
            if (!du.isSystemPartition) {
              results.add(du);
            }
          }
        }
      } else {
        final mp = map['mount_point']?.toString() ?? map['mountpoint']?.toString() ?? map['path']?.toString() ?? '';
        if (mp.isNotEmpty) {
          final size = _safeInt(map['size_bytes'] ?? map['size'] ?? map['total_bytes'] ?? map['total']);
          final avail = _safeInt(map['avail_bytes'] ?? map['avail'] ?? map['free_bytes'] ?? map['free']);
          final used = _safeInt(map['used_bytes'] ?? map['used'] ?? (size > avail && avail > 0 ? size - avail : 0));
          final rawLabel = map['label']?.toString() ?? map['name']?.toString() ?? '';
          final label = rawLabel.isNotEmpty ? rawLabel : (mp.split('/').where((s) => s.isNotEmpty).lastOrNull ?? diskName);
          final pct = map['percent']?.toString() ?? (size > 0 ? '${((used / size) * 100).toStringAsFixed(1)}%' : '0%');

          final du = DiskUsage(
            mountPoint: mp,
            label: label.isNotEmpty ? label : (isDiskUsb ? 'USB Storage' : 'Local Drive'),
            percent: pct,
            sizeBytes: size,
            usedBytes: used,
            filesystem: diskType,
            isUsb: isDiskUsb,
          );
          if (!du.isSystemPartition) {
            results.add(du);
          }
        }
      }
    }
    return results;
  }

  double get fraction {
    final cleaned = percent.replaceAll('%', '').trim();
    final v = double.tryParse(cleaned) ?? 0;
    return (v / 100).clamp(0, 1);
  }

  int get freeBytes => (sizeBytes - usedBytes).clamp(0, sizeBytes);

  bool get isSystemPartition {
    final l = label.toLowerCase();
    final m = mountPoint.toLowerCase();
    if (m == '/boot/efi' || m.startsWith('/boot/efi') || m == '/boot') return true;
    if (l.contains('efi') && sizeBytes < 600 * 1024 * 1024) return true;
    return false;
  }
}

class NetSample {
  final String name;
  final int bytesSent;
  final int bytesRecv;
  final String state;
  final int time;

  NetSample({
    required this.name,
    required this.bytesSent,
    required this.bytesRecv,
    this.state = 'up',
    this.time = 0,
  });

  factory NetSample.fromJson(Map<String, dynamic> j) => NetSample(
        name: j['name']?.toString() ?? '',
        bytesSent: _safeInt(j['bytesSent'] ?? j['bytes_sent']),
        bytesRecv: _safeInt(j['bytesRecv'] ?? j['bytes_recv']),
        state: j['state']?.toString() ?? 'up',
        time: _safeInt(j['time'] ?? j['timestamp']),
      );
}

class DashboardStats {
  final double cpuPercent;
  final List<double> cpuPerCore;
  final int cpuCores;
  final String cpuModelName;
  final double cpuMhz;
  final double? cpuTemperature;
  final double? cpuPower;

  final int memTotal;
  final int memUsed;
  final int memFree;
  final int memAvailable;
  final double memUsedPercent;

  final List<NetSample> netSamples;
  final List<DiskUsage> disks;

  DashboardStats({
    required this.cpuPercent,
    required this.cpuPerCore,
    required this.cpuCores,
    required this.cpuModelName,
    required this.cpuMhz,
    required this.cpuTemperature,
    required this.cpuPower,
    required this.memTotal,
    required this.memUsed,
    required this.memFree,
    required this.memAvailable,
    required this.memUsedPercent,
    required this.netSamples,
    required this.disks,
  });

  NetSample? get primaryNet {
    for (final s in netSamples) {
      if (s.name != 'lo' &&
          !s.name.startsWith('veth') &&
          !s.name.startsWith('docker') &&
          !s.name.startsWith('br-')) {
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
    if (tempRaw != null) temperature = _safeDouble(tempRaw);

    double? power;
    final powerRaw = cpu['power'];
    if (powerRaw != null) power = _safeDouble(powerRaw);

    final perCoreRaw = cpu['percpu'] as List<dynamic>? ?? [];
    final perCore = perCoreRaw.map((e) => _safeDouble(e)).toList();

    return DashboardStats(
      cpuPercent: _safeDouble(cpu['percent']),
      cpuPerCore: perCore,
      cpuCores: _safeInt(cpu['num']) > 0 ? _safeInt(cpu['num']) : (perCore.isNotEmpty ? perCore.length : 1),
      cpuModelName: cpu['model_name']?.toString() ?? cpu['model']?.toString() ?? 'Nivaro Multi-Core Processor',
      cpuMhz: _safeDouble(cpu['mhz']),
      cpuTemperature: temperature,
      cpuPower: power,
      memTotal: _safeInt(mem['total']),
      memUsed: _safeInt(mem['used']),
      memFree: _safeInt(mem['free']),
      memAvailable: _safeInt(mem['available']),
      memUsedPercent: _safeDouble(mem['usedPercent'] ?? mem['used_percent']),
      netSamples: netRaw.map((e) => NetSample.fromJson(Map<String, dynamic>.from(e as Map))).toList(),
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
        cpuPerCore: cpuPerCore,
        cpuCores: cpuCores,
        cpuModelName: cpuModelName,
        cpuMhz: cpuMhz,
        cpuTemperature: cpuTemperature,
        cpuPower: cpuPower,
        memTotal: memTotal,
        memUsed: memUsed,
        memFree: memFree,
        memAvailable: memAvailable,
        memUsedPercent: memUsedPercent,
        netSamples: netSamples,
        disks: disks,
      );
}
