// What Home shows about the server: live utilization, drives, and the list
// of things that need attention. Parsing and the attention rules live here,
// away from the widgets, so they can be unit tested
// (test/models/dashboard_stats_test.dart).
//
// Unknown values stay unknown: a field the server didn't send is null (or
// 0 where the old API needs an int), never a plausible-looking default.

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

double? _maybeDouble(dynamic val) {
  if (val == null) return null;
  if (val is num) return val.toDouble();
  if (val is String) return double.tryParse(val.replaceAll('%', '').trim());
  if (val is Map && val.containsKey('value')) return _maybeDouble(val['value']);
  return null;
}

double _safeDouble(dynamic val) => _maybeDouble(val) ?? 0.0;

class DiskUsage {
  final String mountPoint;
  final String label;
  final String percent;
  final int sizeBytes;
  final int usedBytes;
  final String filesystem;
  final bool isUsb;

  /// The drive the server boots from ("/").
  final bool isSystem;

  /// 'ssd', 'hdd', 'usb', 'nvme' … as the server reports it; '' if unknown.
  final String kind;

  /// The drive's model name ("WDC WD20EZAZ-00G"); '' if unknown.
  final String model;

  DiskUsage({
    required this.mountPoint,
    required this.label,
    required this.percent,
    required this.sizeBytes,
    required this.usedBytes,
    this.filesystem = '',
    this.isUsb = false,
    this.isSystem = false,
    this.kind = '',
    this.model = '',
  });

  factory DiskUsage.fromJson(Map<String, dynamic> j) {
    final mp = j['mount_point']?.toString() ?? j['mountpoint']?.toString() ?? j['path']?.toString() ?? '';
    final size = _safeInt(j['size_bytes'] ?? j['size'] ?? j['total_bytes'] ?? j['total']);
    final avail = _safeInt(j['avail_bytes'] ?? j['avail'] ?? j['free_bytes'] ?? j['free']);
    final used = _safeInt(j['used_bytes'] ?? j['used'] ?? (size > avail && avail > 0 ? size - avail : 0));
    final pct = j['percent']?.toString() ?? j['used_percent']?.toString() ?? (size > 0 ? '${((used / size) * 100).toStringAsFixed(1)}%' : '');
    final rawLabel = j['label']?.toString() ?? j['name']?.toString() ?? j['disk_name']?.toString() ?? '';
    final type = j['filesystem']?.toString() ?? j['fstype']?.toString() ?? j['type']?.toString() ?? '';
    final isUsb = j['is_usb'] == true || type.toLowerCase() == 'usb' || (j['model']?.toString().toLowerCase().contains('usb') ?? false);
    final isSystem = j['is_system'] == true || mp == '/';
    final fromPath = mp.split('/').where((s) => s.isNotEmpty).lastOrNull;
    final label = rawLabel.isNotEmpty
        ? rawLabel
        : isSystem
            ? 'System drive'
            : (fromPath ?? (isUsb ? 'USB drive' : 'Local drive'));

    return DiskUsage(
      mountPoint: mp,
      label: label,
      percent: pct,
      sizeBytes: size,
      usedBytes: used,
      filesystem: type,
      isUsb: isUsb,
      isSystem: isSystem,
      kind: j['kind']?.toString() ?? '',
      model: j['model']?.toString().trim() ?? '',
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
            final pct = cmap['percent']?.toString() ?? (size > 0 ? '${((used / size) * 100).toStringAsFixed(1)}%' : '');

            final du = DiskUsage(
              mountPoint: mp,
              label: label.isNotEmpty ? label : (isDiskUsb ? 'USB drive' : diskName),
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
          final pct = map['percent']?.toString() ?? (size > 0 ? '${((used / size) * 100).toStringAsFixed(1)}%' : '');

          final du = DiskUsage(
            mountPoint: mp,
            label: label.isNotEmpty ? label : (isDiskUsb ? 'USB drive' : 'Local drive'),
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

  /// How full the drive is, 0..1. From the byte counts when the server sent
  /// them (the percent text is rounded), else from the percent text; 0 when
  /// neither is known - check [sizeKnown] before showing it.
  double get fraction {
    if (sizeBytes > 0) return (usedBytes / sizeBytes).clamp(0.0, 1.0);
    final v = double.tryParse(percent.replaceAll('%', '').trim()) ?? 0;
    return (v / 100).clamp(0.0, 1.0);
  }

  bool get sizeKnown => sizeBytes > 0;

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
    this.state = '',
    this.time = 0,
  });

  factory NetSample.fromJson(Map<String, dynamic> j) => NetSample(
        name: j['name']?.toString() ?? '',
        bytesSent: _safeInt(j['bytesSent'] ?? j['bytes_sent']),
        bytesRecv: _safeInt(j['bytesRecv'] ?? j['bytes_recv']),
        state: j['state']?.toString() ?? '',
        time: _safeInt(j['time'] ?? j['timestamp']),
      );
}

/// Network throughput between two utilization samples, in bytes per second.
class NetRate {
  const NetRate({required this.upBytesPerSec, required this.downBytesPerSec});

  final double upBytesPerSec;
  final double downBytesPerSec;

  /// The rate from [prev] (taken at [prevAt]) to [cur] (at [curAt]), or
  /// null when it can't be known: a different interface, no time passed,
  /// or a counter that went backwards (the server restarted or the
  /// interface was reset). A reset used to show as 0 B/s, which reads as
  /// "idle" - null shows as "—" instead.
  static NetRate? between(NetSample? prev, DateTime? prevAt, NetSample? cur, DateTime curAt) {
    if (prev == null || prevAt == null || cur == null || prev.name != cur.name) return null;
    final seconds = curAt.difference(prevAt).inMilliseconds / 1000;
    if (seconds <= 0) return null;
    final up = cur.bytesSent - prev.bytesSent;
    final down = cur.bytesRecv - prev.bytesRecv;
    if (up < 0 || down < 0) return null;
    return NetRate(upBytesPerSec: up / seconds, downBytesPerSec: down / seconds);
  }
}

/// One memory module, from `mem.dimms`.
class MemoryModule {
  const MemoryModule({required this.locator, required this.size, required this.type, required this.speed, required this.partNumber});

  final String locator;
  final String size;
  final String type;
  final String speed;
  final String partNumber;

  factory MemoryModule.fromJson(Map<String, dynamic> j) => MemoryModule(
        locator: j['locator']?.toString() ?? '',
        size: j['size']?.toString() ?? '',
        type: j['type']?.toString() ?? '',
        speed: j['speed']?.toString() ?? '',
        partNumber: _known(j['part_number']?.toString()),
      );

  static String _known(String? v) {
    final t = (v ?? '').trim();
    return t.toLowerCase() == 'unknown' ? '' : t;
  }
}

class DashboardStats {
  final double cpuPercent;
  final List<double> cpuPerCore;

  /// Physical cores as the server reports them; 0 if unknown.
  final int cpuCores;

  /// '' when the server didn't say.
  final String cpuModelName;
  final double cpuMhz;
  final double? cpuTemperature;

  final int memTotal;
  final int memUsed;
  final int memFree;
  final int memAvailable;
  final double memUsedPercent;
  final List<MemoryModule> memModules;

  /// Swap size and how much of it is in use; 0 when the server has none
  /// or doesn't report it.
  final int swapTotal;
  final int swapUsed;

  final List<NetSample> netSamples;
  final List<DiskUsage> disks;

  DashboardStats({
    required this.cpuPercent,
    required this.cpuPerCore,
    required this.cpuCores,
    required this.cpuModelName,
    required this.cpuMhz,
    required this.cpuTemperature,
    required this.memTotal,
    required this.memUsed,
    required this.memFree,
    required this.memAvailable,
    required this.memUsedPercent,
    this.memModules = const [],
    this.swapTotal = 0,
    this.swapUsed = 0,
    required this.netSamples,
    required this.disks,
  });

  /// The interface that carries the server's traffic: the first that isn't
  /// loopback, a container bridge or a veth pair.
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

  /// Hardware threads (the per-core list), which can be twice [cpuCores].
  int get cpuThreads => cpuPerCore.length;

  /// Memory the kernel uses for file cache and buffers. It is given back
  /// when apps need it, so it doesn't count as used.
  int get memCache {
    final c = memTotal - memUsed - memFree;
    return c > 0 ? c : 0;
  }

  factory DashboardStats.fromUtilization(Map<String, dynamic> data) {
    final cpu = data['cpu'] as Map<String, dynamic>? ?? {};
    final mem = data['mem'] as Map<String, dynamic>? ?? {};
    final netRaw = data['net'] as List<dynamic>? ?? [];

    final perCoreRaw = cpu['percpu'] as List<dynamic>? ?? [];
    final perCore = perCoreRaw.map((e) => _safeDouble(e)).toList();
    final temp = _maybeDouble(cpu['temperature']);
    final memTotal = _safeInt(mem['total']);
    final memUsed = _safeInt(mem['used']);
    final pctRaw = _maybeDouble(mem['usedPercent'] ?? mem['used_percent']);

    return DashboardStats(
      cpuPercent: _safeDouble(cpu['percent']),
      cpuPerCore: perCore,
      cpuCores: _safeInt(cpu['num']),
      cpuModelName: (cpu['model_name']?.toString() ?? '').trim(),
      cpuMhz: _safeDouble(cpu['mhz']),
      // 0 °C means "no sensor" on this server, not a frozen CPU.
      cpuTemperature: temp == null || temp <= 0 ? null : temp,
      memTotal: memTotal,
      memUsed: memUsed,
      memFree: _safeInt(mem['free']),
      memAvailable: _safeInt(mem['available']),
      memUsedPercent: pctRaw ?? (memTotal > 0 ? memUsed / memTotal * 100 : 0),
      memModules: (mem['dimms'] as List<dynamic>? ?? const [])
          .whereType<Map>()
          .map((e) => MemoryModule.fromJson(Map<String, dynamic>.from(e)))
          .toList(),
      swapTotal: _safeInt(mem['swapTotal']),
      swapUsed: _safeInt(mem['swapUsed']),
      netSamples: netRaw.map((e) => NetSample.fromJson(Map<String, dynamic>.from(e as Map))).toList(),
      disks: const [],
    );
  }

  /// Data drives: every mounted drive except boot partitions.
  List<DiskUsage> get dataDisks => disks.where((d) => !d.isSystemPartition).toList();

  int get storageUsed => dataDisks.fold(0, (sum, d) => sum + d.usedBytes);
  int get storageTotal => dataDisks.fold(0, (sum, d) => sum + d.sizeBytes);
  double get storageFraction {
    final total = storageTotal;
    if (total <= 0) return 0.0;
    return (storageUsed / total).clamp(0.0, 1.0);
  }

  String get storagePercentText {
    final total = storageTotal;
    if (total <= 0) return '—';
    return '${(storageFraction * 100).toStringAsFixed(0)}%';
  }

  DashboardStats withDisks(List<DiskUsage> disks) => DashboardStats(
        cpuPercent: cpuPercent,
        cpuPerCore: cpuPerCore,
        cpuCores: cpuCores,
        cpuModelName: cpuModelName,
        cpuMhz: cpuMhz,
        cpuTemperature: cpuTemperature,
        memTotal: memTotal,
        memUsed: memUsed,
        memFree: memFree,
        memAvailable: memAvailable,
        memUsedPercent: memUsedPercent,
        memModules: memModules,
        swapTotal: swapTotal,
        swapUsed: swapUsed,
        netSamples: netSamples,
        disks: disks,
      );
}

/// The latest live reading Home and the detail screens share.
class LiveStats {
  const LiveStats({required this.stats, required this.rate, required this.updatedAt, this.stale = false});

  final DashboardStats stats;

  /// Null until two samples exist (or after a counter reset).
  final NetRate? rate;
  final DateTime updatedAt;

  /// True while the server can't be reached and this is the last reading.
  final bool stale;

  LiveStats copyWith({bool? stale}) => LiveStats(stats: stats, rate: rate, updatedAt: updatedAt, stale: stale ?? this.stale);
}

/// `GET /v1/sys/hardware`: facts about the machine. Empty strings when
/// the server couldn't tell.
class HostInfo {
  const HostInfo({
    required this.hostname,
    required this.osName,
    required this.kernel,
    required this.arch,
    required this.uptime,
    required this.dockerVersion,
  });

  final String hostname;
  final String osName;
  final String kernel;
  final String arch;

  /// "4 days, 23 hours" (the "up " of `uptime -p` removed).
  final String uptime;
  final String dockerVersion;

  factory HostInfo.fromJson(Map<String, dynamic> j) {
    String s(String k) => j[k]?.toString().trim() ?? '';
    var up = s('uptime');
    if (up.startsWith('up ')) up = up.substring(3);
    // "4 days, 23 hours, 4 minutes" - the minutes are noise at a glance.
    final parts = up.split(', ');
    if (parts.length > 2) up = parts.take(2).join(', ');
    return HostInfo(
      hostname: s('hostname'),
      osName: s('os_name'),
      kernel: s('kernel'),
      arch: s('arch'),
      uptime: up,
      dockerVersion: s('docker_version'),
    );
  }
}

/// Pending updates, from `/sys/version/check` and `/sys/packages/check`.
/// Either half is null when its check failed.
class UpdateSummary {
  const UpdateSummary({this.serverUpdate, this.serverVersion, this.packages, this.security = 0});

  /// True when a newer NivaroOS is available; null if the check failed.
  final bool? serverUpdate;

  /// The version on offer when [serverUpdate] is true.
  final String? serverVersion;

  /// Upgradable system packages; null if the check failed.
  final int? packages;
  final int security;
}

/// One backup job as Home needs it (`GET /v1/backup/jobs`).
class BackupJobBrief {
  const BackupJobBrief({required this.name, required this.health, this.lastStatus, this.destLabel = '', this.destOnline = true});

  final String name;

  /// problem | offline | warning | ok | disabled
  final String health;
  final String? lastStatus;
  final String destLabel;
  final bool destOnline;

  factory BackupJobBrief.fromJson(Map<String, dynamic> j) => BackupJobBrief(
        name: j['name']?.toString() ?? '',
        health: j['health']?.toString() ?? '',
        lastStatus: (j['last_run'] as Map?)?['status']?.toString(),
        destLabel: (j['dest'] as Map?)?['label']?.toString() ?? '',
        destOnline: j['dest_online'] != false,
      );
}

/// Installed apps by state, from the app grid.
class AppCounts {
  const AppCounts({required this.running, required this.stopped});

  final int running;

  /// Names of apps that aren't running.
  final List<String> stopped;

  int get total => running + stopped.length;

  factory AppCounts.fromAppGrid(List<dynamic> grid) {
    var running = 0;
    final stopped = <String>[];
    for (final item in grid.whereType<Map>()) {
      final status = item['status']?.toString().toLowerCase() ?? '';
      if (status == 'running') {
        running++;
      } else {
        final title = item['title'];
        final name = (title is Map ? title['en_us']?.toString() : null) ?? item['name']?.toString() ?? '';
        stopped.add(name);
      }
    }
    return AppCounts(running: running, stopped: stopped);
  }
}

enum AttentionSeverity { error, warning, info }

enum AttentionKind { serverUpdate, packages, disk, backup, apps }

/// One row of Home's "Needs attention" list.
class AttentionItem {
  const AttentionItem({required this.kind, required this.severity, required this.title, required this.detail, this.disk});

  final AttentionKind kind;
  final AttentionSeverity severity;
  final String title;
  final String detail;

  /// For [AttentionKind.disk]: the drive.
  final DiskUsage? disk;
}

/// Drive fill levels that need attention (the same thresholds UsageBar
/// colours at).
const diskWarnAt = 0.8;
const diskCriticalAt = 0.9;

/// Everything that needs the owner, worst first. Each input is optional: a
/// check that failed or a module that isn't installed adds nothing.
List<AttentionItem> buildAttention({
  UpdateSummary? updates,
  List<DiskUsage> disks = const [],
  List<BackupJobBrief> backups = const [],
  AppCounts? apps,
}) {
  final items = <AttentionItem>[];

  for (final d in disks) {
    if (d.isSystemPartition || !d.sizeKnown) continue;
    final f = d.fraction;
    if (f < diskWarnAt) continue;
    final pct = (f * 100).round();
    items.add(AttentionItem(
      kind: AttentionKind.disk,
      severity: f >= diskCriticalAt ? AttentionSeverity.error : AttentionSeverity.warning,
      title: f >= diskCriticalAt ? '${d.label} is almost full' : '${d.label} is filling up',
      detail: '$pct% used',
      disk: d,
    ));
  }

  for (final b in backups) {
    switch (b.health) {
      case 'problem':
        items.add(AttentionItem(
          kind: AttentionKind.backup,
          severity: AttentionSeverity.error,
          title: b.lastStatus == 'waiting_user' ? '${b.name} is waiting for you' : "${b.name} didn't finish",
          detail: b.lastStatus == 'waiting_user' ? 'Backup · A decision is needed' : 'Backup · The last run failed',
        ));
      case 'offline':
        items.add(AttentionItem(
          kind: AttentionKind.backup,
          severity: AttentionSeverity.warning,
          title: '${b.name} is paused',
          detail: b.destLabel.isEmpty ? 'Backup · The destination is not connected' : 'Backup · ${b.destLabel} is not connected',
        ));
      case 'warning':
        items.add(AttentionItem(
          kind: AttentionKind.backup,
          severity: AttentionSeverity.warning,
          title: '${b.name} needs a look',
          detail: 'Backup · Partly done or overdue',
        ));
    }
  }

  if (updates?.serverUpdate == true) {
    final v = updates!.serverVersion;
    items.add(AttentionItem(
      kind: AttentionKind.serverUpdate,
      severity: AttentionSeverity.info,
      title: v == null || v.isEmpty ? 'NivaroOS update available' : 'NivaroOS $v is available',
      detail: 'Server update',
    ));
  }
  final pkgs = updates?.packages ?? 0;
  if (pkgs > 0) {
    final sec = updates!.security;
    items.add(AttentionItem(
      kind: AttentionKind.packages,
      severity: sec > 0 ? AttentionSeverity.warning : AttentionSeverity.info,
      title: pkgs == 1 ? '1 system update' : '$pkgs system updates',
      detail: sec > 0 ? '$sec security' : 'Debian packages',
    ));
  }

  final stopped = apps?.stopped ?? const [];
  if (stopped.isNotEmpty) {
    items.add(AttentionItem(
      kind: AttentionKind.apps,
      severity: AttentionSeverity.info,
      title: stopped.length == 1 ? '1 app is stopped' : '${stopped.length} apps are stopped',
      detail: _nameList(stopped),
    ));
  }

  items.sort((a, b) => a.severity.index.compareTo(b.severity.index));
  return items;
}

/// "searxng, comfyui and 6 more".
String _nameList(List<String> names) {
  final shown = names.where((n) => n.isNotEmpty).take(2).toList();
  final rest = names.length - shown.length;
  if (rest <= 0) return shown.join(' and ');
  return '${shown.join(', ')} and $rest more';
}
