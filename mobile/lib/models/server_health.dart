// Server health: every check the app can make with what it already reads
// (live utilization, drives, updates, backups, apps, Tailscale, this
// phone's sharing), sorted into what needs the owner, what is fine, and
// what couldn't be checked. Home's status header, its "Needs attention"
// group and the Server health page all read this one model, so they can't
// disagree (test/models/server_health_test.dart).
//
// A check whose data is missing never passes: it goes under "Not checked"
// with the reason, or is left out when the feature isn't there at all
// (Backup & Sync not installed, Tailscale not installed).

import '../utils/format.dart';
import 'dashboard_stats.dart';

enum AttentionSeverity { error, warning, info }

enum AttentionKind { serverUpdate, packages, disk, driveHealth, backup, apps, temperature, memory, tailscale, sharing }

/// One row of "Needs attention" (Home and the Server health page).
class AttentionItem {
  const AttentionItem({required this.kind, required this.severity, required this.title, required this.detail, this.disk, this.backupJobId = '', this.backupRunId = ''});

  final AttentionKind kind;
  final AttentionSeverity severity;
  final String title;

  /// One line: why it needs attention.
  final String detail;

  /// For [AttentionKind.disk]: the drive.
  final DiskUsage? disk;

  /// For [AttentionKind.backup]: the job, and the run waiting for a
  /// decision when one is.
  final String backupJobId;
  final String backupRunId;
}

/// What a check is about; picks its icon.
enum HealthArea { temperature, memory, storage, driveHealth, backups, updates, apps, tailscale, sharing }

/// A check that passed, or one that couldn't be made (then [detail] says
/// why).
class HealthCheck {
  const HealthCheck({required this.area, required this.title, required this.detail, this.at});

  final HealthArea area;
  final String title;
  final String detail;

  /// A time that goes with [detail] ("Last backup" · 3 h ago).
  final DateTime? at;
}

/// A drive's own health, from `GET /v1/disks` (SMART, cached by the
/// server; a sleeping drive isn't woken for it).
class DriveHealth {
  const DriveHealth({required this.name, this.model = '', this.healthy, this.sleeping = false, this.temperature, this.type = '', this.system = false});

  /// The kernel's name ("sda").
  final String name;
  final String model;

  /// True healthy, false failing, null unknown (never read while awake).
  final bool? healthy;
  final bool sleeping;

  /// °C; null when the drive doesn't report it.
  final int? temperature;

  /// "HDD", "SSD", "USB", "MMC".
  final String type;

  /// The boot drive. The server doesn't read its SMART health (it always
  /// says "true"), so it isn't counted as checked.
  final bool system;

  String get label => model.isNotEmpty && !system ? model : name;

  factory DriveHealth.fromJson(Map<String, dynamic> j) {
    final h = j['health']?.toString();
    final t = j['temperature'];
    final model = j['model']?.toString().trim() ?? '';
    return DriveHealth(
      name: j['name']?.toString() ?? '',
      model: model,
      healthy: h == 'true' ? true : (h == 'false' ? false : null),
      sleeping: j['sleeping'] == true,
      temperature: t is num && t > 0 ? t.toInt() : null,
      type: j['disk_type']?.toString() ?? '',
      system: model == 'System',
    );
  }

  /// The `disks` list of `GET /v1/disks`' data.
  static List<DriveHealth> fromDisksApi(Object? data) {
    final list = data is Map ? data['disks'] : null;
    if (list is! List) return const [];
    return list.whereType<Map>().map((e) => DriveHealth.fromJson(Map<String, dynamic>.from(e))).toList();
  }
}

/// Where the server's Tailscale stands, as far as health goes.
enum TailnetState { connected, connecting, signedOut, off, notInstalled }

/// This phone's companion storage sharing.
class SharingBrief {
  const SharingBrief({required this.running, this.stopReason});

  final bool running;

  /// Why the last session ended (BackgroundService's lastStopReason).
  final String? stopReason;
}

/// Drive fill levels that need attention (the same thresholds UsageBar
/// colours at).
const diskWarnAt = 0.8;
const diskCriticalAt = 0.9;

/// Processor temperatures that need attention, °C.
const cpuHotAt = 80.0;
const cpuCriticalAt = 90.0;

/// Memory in use (of the total, cache not counted) that needs attention.
const memoryWarnAt = 0.9;
const memoryCriticalAt = 0.95;

/// The whole picture: what needs the owner (worst first), what is fine,
/// and what couldn't be checked.
class ServerHealth {
  const ServerHealth({this.attention = const [], this.fine = const [], this.unchecked = const []});

  final List<AttentionItem> attention;
  final List<HealthCheck> fine;
  final List<HealthCheck> unchecked;

  bool get allGood => attention.isEmpty;

  /// The worst severity; null when all is good.
  AttentionSeverity? get worst => attention.isEmpty ? null : attention.first.severity;

  /// The one-line answer, the same on Home and the Server health page.
  String get verdict {
    final n = attention.length;
    return switch (worst) {
      null => 'All good',
      AttentionSeverity.info => n == 1 ? '1 thing to look at' : '$n things to look at',
      _ => n == 1 ? '1 thing needs attention' : '$n things need attention',
    };
  }
}

/// Every check, from what the app has loaded. Each input is optional:
/// null means that check's data couldn't be read (it is listed as not
/// checked), an empty list that there is nothing to check.
///
/// [backupsInstalled]: false when Backup & Sync isn't installed (no backup
/// checks), null when it couldn't be asked. [stats] null: no live reading
/// yet. [drives] null: the drive list couldn't be read.
ServerHealth buildHealth({
  DashboardStats? stats,
  UpdateSummary? updates,
  List<BackupJobBrief> backups = const [],
  bool? backupsInstalled = true,
  AppCounts? apps,
  List<DriveHealth>? drives,
  TailnetState? tailnet,
  String tailnetIp = '',
  SharingBrief? sharing,
}) {
  final items = <AttentionItem>[];
  final fine = <HealthCheck>[];
  final unchecked = <HealthCheck>[];

  void notChecked(HealthArea area, String title, String why) => unchecked.add(HealthCheck(area: area, title: title, detail: why));

  // Processor temperature and memory.
  if (stats == null) {
    notChecked(HealthArea.temperature, 'Processor temperature', 'Waiting for the server');
    notChecked(HealthArea.memory, 'Memory', 'Waiting for the server');
  } else {
    final t = stats.cpuTemperature;
    if (t == null) {
      notChecked(HealthArea.temperature, 'Processor temperature', 'No temperature sensor found');
    } else {
      final deg = '${t.toStringAsFixed(0)}\u00A0°C';
      if (t >= cpuHotAt) {
        items.add(AttentionItem(
          kind: AttentionKind.temperature,
          severity: t >= cpuCriticalAt ? AttentionSeverity.error : AttentionSeverity.warning,
          title: t >= cpuCriticalAt ? 'The processor is very hot' : 'The processor is running hot',
          detail: '$deg · Check the fans and the airflow',
        ));
      } else {
        fine.add(HealthCheck(area: HealthArea.temperature, title: 'Processor temperature', detail: '$deg · Normal'));
      }
    }

    if (stats.memTotal > 0) {
      // The same figure as Home's Memory card.
      final used = stats.memUsedPercent / 100;
      final pct = (used * 100).round();
      if (used >= memoryWarnAt) {
        items.add(AttentionItem(
          kind: AttentionKind.memory,
          severity: used >= memoryCriticalAt ? AttentionSeverity.error : AttentionSeverity.warning,
          title: used >= memoryCriticalAt ? 'Memory is almost full' : 'Memory is running low',
          detail: '$pct% in use · Apps may slow down or be stopped',
        ));
      } else {
        fine.add(HealthCheck(area: HealthArea.memory, title: 'Memory', detail: '$pct% in use'));
      }
    } else {
      notChecked(HealthArea.memory, 'Memory', "The server didn't report it");
    }
  }

  // Each drive's own health (SMART), matched by model so a mounted
  // drive's is told under its own name ("blue", not "WDC WD20EZAZ-00G").
  final smart = [for (final d in drives ?? const <DriveHealth>[]) if (!d.system) d];
  final named = <DriveHealth, String>{};

  // Space on each drive, with its health when that is good.
  final disks = stats?.disks ?? const <DiskUsage>[];
  for (final d in disks) {
    if (d.isSystemPartition || !d.sizeKnown) continue;
    final drive = d.model.isEmpty ? null : smart.where((h) => h.model == d.model && !named.containsKey(h)).firstOrNull;
    if (drive != null) named[drive] = d.label;
    final f = d.fraction;
    final pct = (f * 100).round();
    if (f < diskWarnAt) {
      fine.add(HealthCheck(
        area: HealthArea.storage,
        title: d.label,
        detail: ['$pct% used', '${formatBytes(d.freeBytes)} free', if (drive?.healthy == true) 'Healthy'].join(' · '),
      ));
      continue;
    }
    items.add(AttentionItem(
      kind: AttentionKind.disk,
      severity: f >= diskCriticalAt ? AttentionSeverity.error : AttentionSeverity.warning,
      title: f >= diskCriticalAt ? '${d.label} is almost full' : '${d.label} is filling up',
      detail: '$pct% used',
      disk: d,
    ));
  }
  if (stats != null && disks.isEmpty) notChecked(HealthArea.storage, 'Storage space', "Couldn't read the drives");

  // Each drive's own health (SMART).
  if (drives == null) {
    notChecked(HealthArea.driveHealth, 'Drive health', "Couldn't ask the server");
  } else {
    for (final d in smart) {
      final name = named[d];
      final facts = [if (d.type.isNotEmpty) d.type, if (d.temperature != null) '${d.temperature}\u00A0°C'];
      switch (d.healthy) {
        case false:
          items.add(AttentionItem(
            kind: AttentionKind.driveHealth,
            severity: AttentionSeverity.error,
            title: '${name ?? d.label} reports a problem',
            detail: 'Its health check failed · Back up what is on it',
          ));
        case true:
          // A mounted drive's health is on its space row already.
          if (name == null) fine.add(HealthCheck(area: HealthArea.driveHealth, title: d.label, detail: ['Healthy', ...facts].join(' · ')));
        case null:
          notChecked(HealthArea.driveHealth, name ?? d.label, d.sleeping ? 'Asleep · Health is checked when it wakes up' : 'Health unknown');
      }
    }
  }

  // Backups.
  if (backupsInstalled == null) {
    notChecked(HealthArea.backups, 'Backups', "Couldn't reach Backup & Sync");
  } else if (backupsInstalled) {
    for (final b in backups) {
      switch (b.health) {
        case 'problem':
          items.add(AttentionItem(
            kind: AttentionKind.backup,
            severity: AttentionSeverity.error,
            title: b.lastStatus == 'waiting_user' || b.waitingRunId.isNotEmpty ? '${b.name} is waiting for you' : "${b.name} didn't finish",
            detail: b.lastStatus == 'waiting_user' || b.waitingRunId.isNotEmpty ? 'Backup · A decision is needed' : 'Backup · The last run failed',
            backupJobId: b.id,
            backupRunId: b.waitingRunId,
          ));
        case 'offline':
          items.add(AttentionItem(
            kind: AttentionKind.backup,
            severity: AttentionSeverity.warning,
            title: '${b.name} is paused',
            detail: b.destLabel.isEmpty ? 'Backup · The destination is not connected' : 'Backup · ${b.destLabel} is not connected',
            backupJobId: b.id,
          ));
        case 'warning':
          items.add(AttentionItem(
            kind: AttentionKind.backup,
            severity: AttentionSeverity.warning,
            title: '${b.name} needs a look',
            detail: 'Backup · Partly done or overdue',
            backupJobId: b.id,
          ));
        case 'ok':
          final done = b.lastStatus == 'success' ? b.lastEndedAt : null;
          fine.add(HealthCheck(area: HealthArea.backups, title: b.name, detail: done == null ? 'Backup · Ready' : 'Last backup', at: done));
        case 'disabled':
          notChecked(HealthArea.backups, b.name, 'Backup · Turned off');
      }
    }
  }

  // Updates.
  if (updates == null) {
    notChecked(HealthArea.updates, 'Updates', 'Not checked yet');
  } else {
    switch (updates.serverUpdate) {
      case true:
        final v = updates.serverVersion;
        items.add(AttentionItem(
          kind: AttentionKind.serverUpdate,
          severity: AttentionSeverity.info,
          title: v == null || v.isEmpty ? 'NivaroOS update available' : 'NivaroOS $v is available',
          detail: 'Server update',
        ));
      case false:
        fine.add(const HealthCheck(area: HealthArea.updates, title: 'NivaroOS', detail: 'Up to date'));
      case null:
        notChecked(HealthArea.updates, 'NivaroOS updates', "Couldn't check for updates");
    }
    final pkgs = updates.packages;
    if (pkgs == null) {
      notChecked(HealthArea.updates, 'System updates', "Couldn't check for updates");
    } else if (pkgs == 0) {
      fine.add(const HealthCheck(area: HealthArea.updates, title: 'System packages', detail: 'Up to date'));
    } else {
      final sec = updates.security;
      items.add(AttentionItem(
        kind: AttentionKind.packages,
        severity: sec > 0 ? AttentionSeverity.warning : AttentionSeverity.info,
        title: pkgs == 1 ? '1 system update' : '$pkgs system updates',
        detail: sec > 0 ? '$sec security' : 'Debian packages',
      ));
    }
  }

  // Apps.
  if (apps == null) {
    notChecked(HealthArea.apps, 'Apps', "Couldn't read the apps");
  } else if (apps.stopped.isNotEmpty) {
    final stopped = apps.stopped;
    items.add(AttentionItem(
      kind: AttentionKind.apps,
      severity: AttentionSeverity.info,
      title: stopped.length == 1 ? '1 app is stopped' : '${stopped.length} apps are stopped',
      detail: _nameList(stopped),
    ));
  } else if (apps.total > 0) {
    fine.add(HealthCheck(area: HealthArea.apps, title: 'Apps', detail: apps.total == 1 ? 'The app is running' : 'All ${apps.total} running'));
  }

  // Tailscale.
  switch (tailnet) {
    case null:
      notChecked(HealthArea.tailscale, 'Tailscale', "Couldn't ask the server");
    case TailnetState.notInstalled:
      break;
    case TailnetState.connected:
      fine.add(HealthCheck(area: HealthArea.tailscale, title: 'Tailscale', detail: tailnetIp.isEmpty ? 'Connected' : 'Connected · $tailnetIp'));
    case TailnetState.connecting:
      notChecked(HealthArea.tailscale, 'Tailscale', 'Connecting');
    case TailnetState.signedOut:
      items.add(const AttentionItem(
        kind: AttentionKind.tailscale,
        severity: AttentionSeverity.info,
        title: 'Tailscale needs you to sign in',
        detail: "Remote access doesn't work until you do",
      ));
    case TailnetState.off:
      notChecked(HealthArea.tailscale, 'Tailscale', 'Switched off');
  }

  // This phone's storage sharing: only worth a line while it runs or when
  // Android stopped it.
  if (sharing != null) {
    if (sharing.running) {
      fine.add(const HealthCheck(area: HealthArea.sharing, title: 'Companion sharing', detail: "Sharing this phone's storage"));
    } else if (sharing.stopReason == 'timeout' || sharing.stopReason == 'not_allowed') {
      items.add(AttentionItem(
        kind: AttentionKind.sharing,
        severity: AttentionSeverity.info,
        title: 'Companion sharing stopped',
        detail: sharing.stopReason == 'timeout' ? "Android's daily limit for it was reached" : "Android didn't let it start",
      ));
    }
  }

  items.sort((a, b) => a.severity.index.compareTo(b.severity.index));
  return ServerHealth(attention: items, fine: fine, unchecked: unchecked);
}

/// Everything that needs the owner, worst first: [buildHealth]'s
/// attention list.
List<AttentionItem> buildAttention({
  UpdateSummary? updates,
  List<DiskUsage> disks = const [],
  List<BackupJobBrief> backups = const [],
  AppCounts? apps,
}) =>
    buildHealth(
      stats: DashboardStats.fromUtilization(const {}).withDisks(disks),
      updates: updates,
      backups: backups,
      apps: apps,
      drives: const [],
      tailnet: TailnetState.notInstalled,
    ).attention;

/// "searxng, comfyui and 6 more".
String _nameList(List<String> names) {
  final shown = names.where((n) => n.isNotEmpty).take(2).toList();
  final rest = names.length - shown.length;
  if (rest <= 0) return shown.join(' and ');
  return '${shown.join(', ')} and $rest more';
}
