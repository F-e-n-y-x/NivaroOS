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
import 'drive_report.dart';

export 'drive_report.dart' show DriveVerdict;

enum AttentionSeverity { error, warning, info }

enum AttentionKind { serverUpdate, packages, disk, driveHealth, driveMount, backup, apps, temperature, memory, tailscale, sharing, downloads }

/// One row of "Needs attention" (Home and the Server health page).
class AttentionItem {
  const AttentionItem({required this.kind, required this.severity, required this.title, required this.detail, this.disk, this.drive, this.mount, this.backupJobId = '', this.backupRunId = ''});

  final AttentionKind kind;
  final AttentionSeverity severity;
  final String title;

  /// One line: why it needs attention.
  final String detail;

  /// For [AttentionKind.disk]: the drive.
  final DiskUsage? disk;

  /// For [AttentionKind.driveHealth]: the drive (opens its health page).
  final DriveHealth? drive;

  /// For [AttentionKind.driveMount]: the drive that isn't mounted.
  final MountProblem? mount;

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
/// server; a sleeping drive isn't woken for it). The drive's page
/// (`GET /v1/disks/health`) has the numbers behind it.
class DriveHealth {
  const DriveHealth({
    required this.name,
    this.path = '',
    this.model = '',
    this.healthy,
    this.verdict = DriveVerdict.unknown,
    this.summary = '',
    this.sleeping = false,
    this.temperature,
    this.type = '',
    this.system = false,
  });

  /// The kernel's name ("sda").
  final String name;

  /// "/dev/sda", for the drive's health page.
  final String path;
  final String model;

  /// True healthy (good or watch), false failing, null unknown (never read
  /// while awake, or no SMART).
  final bool? healthy;

  /// good / watch / failing, and why; unknown from a server older than
  /// drive health (it only says healthy or not).
  final DriveVerdict verdict;
  final String summary;
  final bool sleeping;

  /// °C; null when the drive doesn't report it.
  final int? temperature;

  /// "HDD", "SSD", "USB", "MMC".
  final String type;

  /// The boot drive. Servers older than drive health don't read its SMART
  /// (they always say "true"), so then it isn't counted as checked.
  final bool system;

  /// Its health was read: by a server with drive health, or any non-system
  /// drive.
  bool get checked => !system || hasVerdict;

  bool get hasVerdict => verdict != DriveVerdict.unknown || summary.isNotEmpty;

  String get label => system ? 'System drive' : (model.isNotEmpty ? model : name);

  factory DriveHealth.fromJson(Map<String, dynamic> j) {
    final h = j['health']?.toString();
    final t = j['temperature'];
    final model = j['model']?.toString().trim() ?? '';
    final verdict = driveVerdictOf(j['health_verdict']);
    return DriveHealth(
      name: j['name']?.toString() ?? '',
      path: j['path']?.toString() ?? '',
      model: model,
      healthy: switch (verdict) {
        DriveVerdict.good || DriveVerdict.watch => true,
        DriveVerdict.failing => false,
        DriveVerdict.unknown => h == 'true' ? true : (h == 'false' ? false : null),
      },
      verdict: verdict,
      summary: j['health_summary']?.toString() ?? '',
      sleeping: j['sleeping'] == true,
      temperature: t is num && t > 0 ? t.toInt() : null,
      type: j['disk_type']?.toString() ?? '',
      system: j['system'] == true || model == 'System',
    );
  }

  /// The `disks` list of `GET /v1/disks`' data.
  static List<DriveHealth> fromDisksApi(Object? data) {
    final list = data is Map ? data['disks'] : null;
    if (list is! List) return const [];
    return list.whereType<Map>().map((e) => DriveHealth.fromJson(Map<String, dynamic>.from(e))).toList();
  }
}

/// A drive set to mount at boot that isn't mounted (a power cut left it
/// dirty or damaged, it isn't connected), or one being repaired: the
/// `managed` entries of `GET /v1/storage/fstab` with a `problem` or a
/// `repair`.
class MountProblem {
  const MountProblem({
    required this.mountPoint,
    this.reason = '',
    this.message = '',
    this.detail = '',
    this.repairable = false,
    this.repairRunning = false,
    this.repairMessage = '',
    this.repairOk = false,
  });

  final String mountPoint;

  /// missing | dirty | damaged | failed; '' when only a repair is shown.
  final String reason;
  final String message;
  final String detail;
  final bool repairable;
  final bool repairRunning;
  final String repairMessage;
  final bool repairOk;

  /// "tower" for /DATA/tower.
  String get name => mountPoint.split('/').lastWhere((p) => p.isNotEmpty, orElse: () => mountPoint);

  bool get hasProblem => reason.isNotEmpty;

  static List<MountProblem> fromFstabApi(Object? data) {
    final list = data is Map ? data['managed'] : null;
    if (list is! List) return const [];
    final out = <MountProblem>[];
    for (final e in list.whereType<Map>()) {
      final p = e['problem'], r = e['repair'];
      if (p is! Map && !(r is Map && r['running'] == true)) continue;
      out.add(MountProblem(
        mountPoint: e['mount_point']?.toString() ?? '',
        reason: p is Map ? p['reason']?.toString() ?? 'failed' : '',
        message: p is Map ? p['message']?.toString() ?? '' : '',
        detail: p is Map ? p['detail']?.toString() ?? '' : '',
        repairable: p is Map && p['repairable'] == true,
        repairRunning: r is Map && r['running'] == true,
        repairMessage: r is Map ? r['message']?.toString() ?? '' : '',
        repairOk: r is Map && r['ok'] == true,
      ));
    }
    return out;
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
  int failedDownloads = 0,
  List<MountProblem> mountProblems = const [],
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
  final smart = [for (final d in drives ?? const <DriveHealth>[]) if (d.checked) d];
  final named = <DriveHealth, String>{};

  // Space on each drive, with its health when that is good.
  final disks = stats?.disks ?? const <DiskUsage>[];
  for (final d in disks) {
    if (d.isSystemPartition || !d.sizeKnown) continue;
    // the root filesystem's row carries the system drive's health
    final drive = d.isSystem
        ? smart.where((h) => h.system && !named.containsKey(h)).firstOrNull
        : d.model.isEmpty
            ? null
            : smart.where((h) => !h.system && h.model == d.model && !named.containsKey(h)).firstOrNull;
    if (drive != null) named[drive] = d.label;
    final f = d.fraction;
    final pct = (f * 100).round();
    if (f < diskWarnAt) {
      fine.add(HealthCheck(
        area: HealthArea.storage,
        title: d.label,
        detail: ['$pct% used', '${formatBytes(d.freeBytes)} free', if (drive != null && drive.healthy == true && drive.verdict != DriveVerdict.watch) 'Healthy'].join(' · '),
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
      final verdict = d.hasVerdict ? d.verdict : (d.healthy == false ? DriveVerdict.failing : (d.healthy == true ? DriveVerdict.good : DriveVerdict.unknown));
      switch (verdict) {
        case DriveVerdict.failing:
          items.add(AttentionItem(
            kind: AttentionKind.driveHealth,
            severity: AttentionSeverity.error,
            title: '${name ?? d.label} may be failing',
            detail: d.summary.isNotEmpty ? d.summary : 'Its health check failed · Back up what is on it',
            drive: d,
          ));
        case DriveVerdict.watch:
          items.add(AttentionItem(
            kind: AttentionKind.driveHealth,
            severity: AttentionSeverity.warning,
            title: '${name ?? d.label} needs watching',
            detail: d.summary,
            drive: d,
          ));
        case DriveVerdict.good:
          // A mounted drive's health is on its space row already.
          if (name == null) fine.add(HealthCheck(area: HealthArea.driveHealth, title: d.label, detail: ['Healthy', ...facts].join(' · ')));
        case DriveVerdict.unknown:
          notChecked(HealthArea.driveHealth, name ?? d.label,
              d.sleeping ? 'Asleep · Health is checked when it wakes up' : (d.summary.isNotEmpty ? d.summary : 'Health unknown'));
      }
    }
  }

  // Drives that should be mounted but aren't: apps that keep files on
  // them are held until they are.
  for (final m in mountProblems) {
    items.add(AttentionItem(
      kind: AttentionKind.driveMount,
      severity: m.repairRunning ? AttentionSeverity.info : (m.reason == 'missing' ? AttentionSeverity.warning : AttentionSeverity.error),
      title: m.repairRunning
          ? 'Repairing ${m.name}'
          : m.reason == 'missing'
              ? "${m.name} isn't connected"
              : "${m.name} couldn't be mounted",
      detail: m.repairRunning ? 'Keep the drive connected' : (m.repairable ? 'Tap to repair it' : 'Apps that use it wait until it is back'),
      mount: m,
    ));
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
  } else if (apps.failed.isNotEmpty) {
    // Only apps that crashed or keep restarting need attention - a
    // container the owner stopped is their choice, not a problem.
    final failed = apps.failed;
    items.add(AttentionItem(
      kind: AttentionKind.apps,
      severity: AttentionSeverity.warning,
      title: failed.length == 1 ? '1 app failed' : '${failed.length} apps failed',
      detail: _nameList(failed),
    ));
  } else if (apps.total > 0) {
    final stopped = apps.stopped.length;
    fine.add(HealthCheck(
      area: HealthArea.apps,
      title: 'Apps',
      detail: stopped == 0
          ? (apps.total == 1 ? 'The app is running' : 'All ${apps.total} running')
          : '${apps.running} running · $stopped stopped',
    ));
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

  // Download Station: only failures are worth a line (it is optional, and
  // running downloads are not a problem).
  if (failedDownloads > 0) {
    items.add(AttentionItem(
      kind: AttentionKind.downloads,
      severity: AttentionSeverity.warning,
      title: failedDownloads == 1 ? '1 download failed' : '$failedDownloads downloads failed',
      detail: 'Download Station · Retry or remove them',
    ));
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
