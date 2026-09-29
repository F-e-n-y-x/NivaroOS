// When the phone backs itself up: daily or weekly at a time, or only when
// asked. The job that runs it is one-off and rescheduled after every run
// (android PhoneBackupJobService), so the rules live here, in plain Dart,
// where they are tested.

enum ScheduleKind { manual, daily, weekly }

class BackupSchedule {
  const BackupSchedule({this.kind = ScheduleKind.daily, this.hour = 2, this.minute = 0, this.weekday = DateTime.sunday});

  final ScheduleKind kind;
  final int hour;
  final int minute;

  /// DateTime.monday..DateTime.sunday (weekly only).
  final int weekday;

  static const manual = BackupSchedule(kind: ScheduleKind.manual);

  /// How long a waiting backup (drive not connected, no network) waits
  /// before it tries again, at most (never sooner than the next slot).
  static const retryWait = Duration(hours: 1);

  Duration get period => kind == ScheduleKind.weekly ? const Duration(days: 7) : const Duration(days: 1);

  /// The first slot strictly after [t] (local time); null for manual.
  DateTime? slotAfter(DateTime t) {
    if (kind == ScheduleKind.manual) return null;
    var d = DateTime(t.year, t.month, t.day, hour, minute);
    if (kind == ScheduleKind.weekly) {
      d = DateTime(d.year, d.month, d.day + (weekday - d.weekday) % 7, hour, minute);
      while (!d.isAfter(t)) {
        d = DateTime(d.year, d.month, d.day + 7, hour, minute);
      }
    } else {
      while (!d.isAfter(t)) {
        d = DateTime(d.year, d.month, d.day + 1, hour, minute);
      }
    }
    return d;
  }

  /// When the next backup should run. The first slot after the last
  /// completed backup; if that is already past (the phone was off, or the
  /// conditions weren't met), now. With no backup yet, the next slot.
  DateTime? nextRun({required DateTime now, DateTime? lastBackup}) {
    if (kind == ScheduleKind.manual) return null;
    if (lastBackup == null) return slotAfter(now);
    final due = slotAfter(lastBackup)!;
    return due.isAfter(now) ? due : now;
  }

  /// After a run that could not happen (the drive is missing, the server
  /// can't be reached): try again in [retryWait], or at the next slot if
  /// that comes first. Manual backups also retry, so "Back up now" isn't
  /// lost when the drive is plugged back in.
  DateTime retryAt({required DateTime now}) {
    final retry = now.add(retryWait);
    final slot = slotAfter(now);
    return slot != null && slot.isBefore(retry) ? slot : retry;
  }

  /// "daily 02:00", "weekly sun 02:00", "manual" (phone-settings, display
  /// only on the server).
  String get wire {
    final t = '${hour.toString().padLeft(2, '0')}:${minute.toString().padLeft(2, '0')}';
    return switch (kind) {
      ScheduleKind.manual => 'manual',
      ScheduleKind.daily => 'daily $t',
      ScheduleKind.weekly => 'weekly ${_days[weekday - 1]} $t',
    };
  }

  static const _days = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'];
  static const _dayNames = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];

  static BackupSchedule parse(String s) {
    final p = s.trim().toLowerCase().split(RegExp(r'\s+'));
    (int, int)? time(String t) {
      final m = RegExp(r'^(\d{1,2}):(\d{2})$').firstMatch(t);
      if (m == null) return null;
      final h = int.parse(m[1]!), mi = int.parse(m[2]!);
      return h < 24 && mi < 60 ? (h, mi) : null;
    }

    if (p.length == 2 && p[0] == 'daily') {
      final t = time(p[1]);
      if (t != null) return BackupSchedule(hour: t.$1, minute: t.$2);
    }
    if (p.length == 3 && p[0] == 'weekly') {
      final d = _days.indexOf(p[1]);
      final t = time(p[2]);
      if (d >= 0 && t != null) return BackupSchedule(kind: ScheduleKind.weekly, weekday: d + 1, hour: t.$1, minute: t.$2);
    }
    return manual;
  }

  /// "Every day at 2:00", "Every Sunday at 2:00", "Only when you tap Back up now".
  String get label {
    final t = '$hour:${minute.toString().padLeft(2, '0')}';
    return switch (kind) {
      ScheduleKind.manual => 'Only when you tap Back up now',
      ScheduleKind.daily => 'Every day at $t',
      ScheduleKind.weekly => 'Every ${_dayNames[weekday - 1]} at $t',
    };
  }

  static String dayName(int weekday) => _dayNames[weekday - 1];

  @override
  bool operator ==(Object other) => other is BackupSchedule && other.wire == wire;

  @override
  int get hashCode => wire.hashCode;
}

/// What the phone must be doing for a scheduled backup to start.
class RunConditions {
  const RunConditions({this.wifiOnly = true, this.chargingOnly = false});

  final bool wifiOnly;
  final bool chargingOnly;

  List<String> get wire => [if (wifiOnly) 'wifi', if (chargingOnly) 'charging'];

  /// Whether a backup may start now.
  bool allows({required bool onUnmetered, required bool charging}) => (!wifiOnly || onUnmetered) && (!chargingOnly || charging);
}
