// Pieces the Backup & Sync screens share: status in colour + icon + word,
// the run progress bar, the job row, and polling at the phone's refresh
// setting (Settings > Home > Refresh widgets) while a screen is on top.
import 'dart:async';

import 'package:flutter/material.dart';

import '../../backup/backup_api.dart';
import '../../backup/backup_models.dart';
import '../../backup/backup_state.dart';
import '../../backup/backup_strings.dart';
import '../../services/widget_refresh.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';

/// A run status as the app's status tone and a glyph (state.js
/// STATUS_VIEW): never colour alone.
(Status, IconData) runStatusView(String status) => switch (status) {
      'queued' => (Status.info, Icons.schedule_outlined),
      'running' => (Status.info, Icons.cloud_upload_outlined),
      'waiting_user' => (Status.warning, Icons.back_hand_outlined),
      'success' => (Status.success, Icons.check_circle_outline),
      'partial' => (Status.warning, Icons.warning_amber_outlined),
      'failed' => (Status.error, Icons.cancel_outlined),
      'interrupted' => (Status.warning, Icons.restart_alt_outlined),
      _ => (Status.neutral, Icons.remove_circle_outline), // cancelled, skipped
    };

/// A job's health as tone and glyph (state.js HEALTH_VIEW).
(Status, IconData) healthView(String health) => switch (health) {
      'problem' => (Status.error, Icons.error_outline),
      'offline' => (Status.warning, Icons.power_off_outlined),
      'warning' => (Status.warning, Icons.warning_amber_outlined),
      'ok' => (Status.success, Icons.check_circle_outline),
      _ => (Status.neutral, Icons.pause_circle_outline),
    };

/// The glyph of a job type.
IconData jobTypeIcon(String type) => switch (type) {
      'mirror' => Icons.sync_outlined,
      'archive' => Icons.inventory_2_outlined,
      _ => Icons.file_copy_outlined,
    };

/// A run status chip ("Running 68 %", "Failed").
class RunStatusChip extends StatelessWidget {
  const RunStatusChip({super.key, required this.status, this.percent});

  final String status;
  final int? percent;

  @override
  Widget build(BuildContext context) {
    final (tone, icon) = runStatusView(status);
    final label = percent == null ? statusText(status) : '${statusText(status)} $percent %';
    return StatusChip(label: label, status: tone, icon: icon);
  }
}

/// A job's state as a chip: the active run when there is one, else its
/// health when it isn't fine.
class JobStateChip extends StatelessWidget {
  const JobStateChip({super.key, required this.job, this.live});

  final BackupJob job;
  final LiveStats? live;

  @override
  Widget build(BuildContext context) {
    final active = job.activeRun;
    if (job.isActive) {
      final r = live?.ratio;
      return RunStatusChip(status: active!.status, percent: active.status == 'running' && r != null ? (r * 100).floor() : null);
    }
    final (tone, icon) = healthView(job.health);
    return StatusChip(label: healthText(job.health), status: tone, icon: icon);
  }
}

/// A run's progress: the bar (indeterminate until the total is known),
/// then the phase, files and bytes, and - unless [compact] - speed and
/// time left (the web's RunProgress).
class RunProgress extends StatelessWidget {
  const RunProgress({super.key, required this.live, this.phase = '', this.compact = false});

  final LiveStats? live;
  final String phase;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final l = live;
    final ratio = l?.ratio;
    final counts = <String>[
      if (phase.isNotEmpty && btHas('backup.phase.$phase')) bt('backup.phase.$phase'),
      if (l != null && l.totalFiles > 0) bt('backup.run.files_of', {'done': formatNumber(l.files), 'total': formatNumber(l.totalFiles)}),
      if (l != null && l.totalBytes > 0)
        bt('backup.run.bytes_of', {'done': formatSize(l.bytes), 'total': formatSize(l.totalBytes)})
      else if (l != null && l.bytes > 0)
        formatSize(l.bytes),
    ];
    final rate = <String>[
      if (l != null && l.speedBps > 0) formatSpeed(l.speedBps),
      if (l != null && (l.etaSec ?? 0) > 0) bt('backup.run.eta', {'time': formatSeconds(l.etaSec!)}),
    ];
    final percent = ratio == null ? null : (ratio * 100).floor();
    final muted = theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant);
    return Semantics(
      label: bt('backup.phase.transfer'),
      value: percent == null ? bt('backup.run.progress_unknown') : '$percent %, ${counts.join(', ')}',
      child: ExcludeSemantics(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          mainAxisSize: MainAxisSize.min,
          children: [
            LinearProgressIndicator(value: ratio),
            const SizedBox(height: Space.sm),
            Text.rich(
              TextSpan(children: [
                if (percent != null) TextSpan(text: '$percent %', style: theme.textTheme.titleSmall?.tabular),
                if (percent != null && counts.isNotEmpty) const TextSpan(text: '  '),
                if (counts.isNotEmpty) TextSpan(text: counts.join(' · '), style: muted?.tabular),
              ]),
            ),
            if (!compact && rate.isNotEmpty) ...[
              const SizedBox(height: Space.xs),
              Text(rate.join(' · '), style: muted?.tabular),
            ],
          ],
        ),
      ),
    );
  }
}

/// "Succeeded 7 h ago: 4,120 added, …" or "Hasn't run yet".
String lastRunText(BackupJob job) {
  final r = job.lastRun;
  if (r == null) return bt('backup.jobs.never_run');
  final when = r.endedAt == null ? '' : _lowerRelative(formatRelative(r.endedAt!));
  var summary = r.summary?.text ?? '';
  final status = statusText(r.status);
  // "Failed 3 h ago: Cloud sign-in expired", not "…: Failed: Cloud …".
  if (summary.startsWith('$status: ')) summary = summary.substring(status.length + 2);
  return summary.isEmpty
      ? bt('backup.jobs.last_run', {'status': status, 'when': when})
      : bt('backup.jobs.last_run_summary', {'status': status, 'when': when, 'summary': summary});
}

String _lowerRelative(String s) => s == 'Just now' || s == 'Yesterday' ? s.toLowerCase() : s;

/// "next: Tomorrow 3:00 AM"; '' when paused, running or not scheduled.
String nextRunText(BackupJob job) {
  final n = job.nextRun;
  if (!job.enabled || n == null || job.isActive) return '';
  return bt('backup.jobs.next_run', {'when': formatWhen(n)});
}

/// One job in a list: its type glyph, name, what it does and how the last
/// run went (or the live progress), its state, and a menu with Back up
/// now and Pause / Resume.
class BackupJobTile extends StatelessWidget {
  const BackupJobTile({super.key, required this.job, required this.onTap, this.live, this.onRunNow, this.onToggle, this.busy = false});

  final BackupJob job;
  final VoidCallback onTap;
  final LiveStats? live;
  final VoidCallback? onRunNow;
  final VoidCallback? onToggle;
  final bool busy;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final running = job.activeRun?.status == 'running';
    final next = nextRunText(job);
    final showChip = !running && (job.isActive || job.health != 'ok');
    return ListTile(
      leading: Icon(jobTypeIcon(job.type), color: job.enabled ? null : scheme.onSurfaceVariant),
      title: Text(job.name),
      isThreeLine: true,
      subtitle: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(jobDescribeLine(job)),
          if (running) ...[
            const SizedBox(height: Space.sm),
            RunProgress(live: live, compact: true),
          ] else
            Text([lastRunText(job), if (next.isNotEmpty) next].join(' · ')),
          if (showChip) ...[
            const SizedBox(height: Space.sm),
            JobStateChip(job: job, live: live),
          ],
        ],
      ),
      trailing: onRunNow == null && onToggle == null
          ? null
          : PopupMenuButton<String>(
              tooltip: bt('backup.jobs.more_named', {'name': job.name}),
              enabled: !busy,
              onSelected: (v) => v == 'run' ? onRunNow?.call() : onToggle?.call(),
              itemBuilder: (context) => [
                if (onRunNow != null && !job.isActive) const PopupMenuItem(value: 'run', child: Text('Back up now')),
                if (onToggle != null) PopupMenuItem(value: 'toggle', child: Text(job.enabled ? bt('backup.jobs.menu.pause') : bt('backup.jobs.menu.resume'))),
              ],
            ),
      onTap: onTap,
    );
  }
}

/// Polls while its screen is what the user sees: the app is in front,
/// the route is on top and the widget is visible. The pace is the
/// phone's Refresh widgets setting, followed live; "only when I pull to
/// refresh" polls nothing.
mixin BackupPolling<T extends StatefulWidget> on State<T>, WidgetsBindingObserver {
  Timer? _pollTimer;

  /// Called on every tick while on screen.
  Future<void> poll();

  /// False to pause (nothing is running, say).
  bool get wantsPolling => true;

  /// The fastest this screen asks: the setting, but no faster than
  /// [minEvery].
  Duration get minEvery => const Duration(seconds: 2);

  void startPolling() {
    WidgetsBinding.instance.addObserver(this);
    WidgetRefreshController.instance.addListener(_retime);
    _retime();
  }

  void stopPolling() {
    WidgetsBinding.instance.removeObserver(this);
    WidgetRefreshController.instance.removeListener(_retime);
    _pollTimer?.cancel();
  }

  void _retime() {
    _pollTimer?.cancel();
    final every = WidgetRefreshController.instance.value.every;
    if (every == null) return;
    _pollTimer = Timer.periodic(every < minEvery ? minEvery : every, (_) {
      if (onScreen && wantsPolling) poll();
    });
  }

  bool get onScreen {
    if (!mounted) return false;
    final lifecycle = WidgetsBinding.instance.lifecycleState;
    if (lifecycle != null && lifecycle != AppLifecycleState.resumed) return false;
    if (!(ModalRoute.of(context)?.isCurrent ?? true)) return false;
    return Visibility.of(context);
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed && onScreen) poll();
  }
}

/// Shows a failed write the way the web does: the code's title (and its
/// fix when there is one).
void showBackupError(BuildContext context, Object error, {String? prefix}) {
  final e = BackupError.from(error);
  final text = [?prefix, e.title, if (e.code == 'forbidden') e.causeText].join(' ');
  ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(text)));
}

/// A one-line snack bar.
void showBackupSnack(BuildContext context, String text) => ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(text)));

/// The standard load failure: offline, or the code's title and cause.
Widget backupErrorState(Object error, {required String title, required VoidCallback onRetry, bool sliver = false}) {
  final e = BackupError.from(error);
  if (e.isUnreachable) return ErrorState.offline(onRetry: onRetry, sliver: sliver);
  return ErrorState(
    title: title,
    message: e.code.isEmpty ? e.title : '${e.title}. ${e.causeText}',
    onRetry: onRetry,
    details: e.code.isEmpty ? null : 'error_code: ${e.code}',
    sliver: sliver,
  );
}
