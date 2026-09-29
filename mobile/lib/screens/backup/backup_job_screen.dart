import 'dart:async';

import 'package:flutter/material.dart';

import '../../backup/backup_api.dart';
import '../../backup/backup_models.dart';
import '../../backup/backup_state.dart';
import '../../backup/backup_strings.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import 'backup_decide_screen.dart';
import 'backup_job_form_screen.dart';
import 'backup_restore_screen.dart';
import 'backup_run_screen.dart';
import 'backup_widgets.dart';

/// One job (the web's job details pane): what it is and how it's doing,
/// Back up now / Cancel, Pause / Resume, why it needs you, what it does in
/// plain sentences, its versions (each one a way into Restore) and its run
/// history (each one opens the run with its log).
class BackupJobScreen extends StatefulWidget {
  const BackupJobScreen({super.key, required this.jobId, this.initial, this.api});

  final String jobId;

  /// The job as the list had it, shown while the details load.
  final BackupJob? initial;
  final BackupApi? api;

  @override
  State<BackupJobScreen> createState() => _BackupJobScreenState();
}

class _BackupJobScreenState extends State<BackupJobScreen> with WidgetsBindingObserver, BackupPolling {
  late final BackupApi _api = widget.api ?? BackupApi();
  static const _page = 20;

  late BackupJob? _job = widget.initial;
  List<BackupRun>? _runs;
  String _nextBefore = '';
  List<BackupVersion>? _versions;
  LiveStats? _live;
  Object? _error;
  bool _busy = false;
  bool _loadingMore = false;
  bool _loading = false;

  @override
  void initState() {
    super.initState();
    _load();
    startPolling();
  }

  @override
  void dispose() {
    stopPolling();
    super.dispose();
  }

  @override
  Future<void> poll() => _load(quiet: true);

  Future<void> _load({bool quiet = false}) async {
    if (_loading) return;
    _loading = true;
    try {
      final job = await _api.job(widget.jobId);
      // A poll re-reads the history only when a run started or ended.
      final prev = _job;
      final changed = !quiet ||
          _runs == null ||
          prev == null ||
          prev.lastRun?.id != job.lastRun?.id ||
          prev.activeRun?.id != job.activeRun?.id ||
          prev.activeRun?.status != job.activeRun?.status;
      final results = await Future.wait<Object?>([
        Future.value(job),
        if (changed) _api.runs(jobId: widget.jobId, limit: _page) else Future.value(null),
        if (changed || _versions == null) _api.versions(widget.jobId).then<Object?>((v) => v, onError: (_) => null) else Future.value(_versions),
      ]);
      final runs = results[1] as RunList?;
      LiveStats? live;
      if (job.activeRun?.status == 'running') {
        try {
          live = (await _api.getRun(job.activeRun!.id)).live;
        } catch (_) {
          live = _live;
        }
      }
      if (!mounted) return;
      setState(() {
        _job = job;
        if (runs != null) {
          // Keep the pages loaded past the first while polling.
          final older = (_runs ?? const <BackupRun>[]).skip(_page).where((r) => !runs.runs.any((n) => n.id == r.id));
          _runs = [...runs.runs, ...older];
          if (older.isEmpty) _nextBefore = runs.nextBefore;
        }
        _versions = results[2] as List<BackupVersion>? ?? _versions;
        _live = live;
        _error = null;
      });
    } catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      _loading = false;
    }
  }

  Future<void> _more() async {
    if (_loadingMore || _nextBefore.isEmpty) return;
    setState(() => _loadingMore = true);
    try {
      final page = await _api.runs(jobId: widget.jobId, limit: _page, before: _nextBefore);
      if (!mounted) return;
      setState(() {
        _runs = [...?_runs, ...page.runs];
        _nextBefore = page.nextBefore;
      });
    } catch (e) {
      if (mounted) showBackupError(context, e);
    } finally {
      if (mounted) setState(() => _loadingMore = false);
    }
  }

  Future<void> _open(Widget screen) async {
    await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => screen));
    if (mounted) unawaited(_load(quiet: true));
  }

  Future<void> _runNow() async {
    final job = _job;
    if (job == null) return;
    setState(() => _busy = true);
    try {
      final runId = await _api.run(job.id);
      if (!mounted) return;
      showBackupSnack(context, bt('backup.jobs.started', {'name': job.name}));
      await _load(quiet: true);
      if (mounted && runId.isNotEmpty) unawaited(_open(BackupRunScreen(runId: runId, api: widget.api)));
    } catch (e) {
      if (mounted) showBackupError(context, e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _cancel() async {
    final job = _job;
    final run = job?.activeRun;
    if (job == null || run == null) return;
    final ok = await ConfirmDialog.destructive(
      context,
      title: bt('backup.run.cancel_title', {'name': job.name}),
      message: bt(run.kind == 'restore' ? 'backup.run.cancel_confirm_restore' : 'backup.run.cancel_confirm'),
      confirmLabel: bt('backup.run.cancel'),
      permanent: false,
    );
    if (!ok || !mounted) return;
    setState(() => _busy = true);
    try {
      await _api.cancel(run.id);
    } catch (e) {
      // Already over: just show how it ended.
      if (mounted && BackupError.from(e).code != 'invalid_state') showBackupError(context, e);
    } finally {
      if (mounted) setState(() => _busy = false);
      await _load(quiet: true);
    }
  }

  Future<void> _toggle() async {
    final job = _job;
    if (job == null) return;
    final next = !job.enabled;
    setState(() {
      _busy = true;
      _job = job.copyWith(enabled: next);
    });
    try {
      final saved = await _api.toggle(job.id, next);
      if (!mounted) return;
      setState(() => _job = saved);
      showBackupSnack(context, bt(next ? 'backup.jobs.resumed' : 'backup.jobs.paused', {'name': job.name}));
    } catch (e) {
      if (!mounted) return;
      setState(() => _job = job);
      showBackupError(context, e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _openAttention(BackupAttention a) {
    if (a.reason == 'waiting' && a.runId.isNotEmpty) {
      _open(BackupDecideScreen(runId: a.runId, jobId: a.job.id, api: widget.api));
    } else if (a.runId.isNotEmpty) {
      _open(BackupRunScreen(runId: a.runId, api: widget.api));
    }
  }

  @override
  Widget build(BuildContext context) {
    final job = _job;
    final error = _error;
    final List<Widget> slivers;
    if (job == null && error != null) {
      slivers = [backupErrorState(error, title: "Couldn't load this job", onRetry: _load, sliver: true)];
    } else if (job == null) {
      slivers = const [SliverLoadingList(rows: 8)];
    } else {
      final attention = attentionFor(job);
      final runs = _runs;
      final versions = _versions;
      slivers = [
        SliverList.list(children: [
          _JobHeader(job: job, live: _live, busy: _busy, onRunNow: _runNow, onCancel: _cancel, onToggle: _toggle,
              onOpenRun: job.activeRun == null ? null : () => _open(BackupRunScreen(runId: job.activeRun!.id, api: widget.api))),
          if (attention != null)
            Notice(
                title: attention.title,
                message: attention.cause,
                status: attention.severity == BackupSeverity.problem ? Status.error : Status.warning,
                actionLabel: attention.reason == 'waiting'
                    ? bt('backup.action.review')
                    : attention.runId.isNotEmpty
                        ? bt('backup.action.view_log')
                        : null,
                onAction: attention.runId.isNotEmpty ? () => _openAttention(attention) : null,
              ),
          TileGroup(title: bt('backup.jobs.tab.summary'), children: [
            ListTile(
              leading: const Icon(Icons.folder_outlined),
              title: Text(bt('backup.jobs.fact.what')),
              subtitle: Text(job.sources.map((s) => s.display).join('\n')),
            ),
            ListTile(
              leading: Icon(job.destOnline ? Icons.save_outlined : Icons.power_off_outlined),
              title: Text(bt('backup.jobs.fact.where')),
              subtitle: Text(job.destOnline ? job.dest.display : '${job.dest.display} · ${bt('backup.health.offline')}'),
            ),
            ListTile(
              leading: Icon(jobTypeIcon(job.type)),
              title: Text(bt('backup.jobs.fact.how')),
              subtitle: Text('${bt('backup.type.${job.type}.label')} · ${bt('backup.type.${job.type}.deleted')}'),
            ),
            ListTile(
              leading: const Icon(Icons.schedule_outlined),
              title: Text(bt('backup.jobs.fact.when')),
              subtitle: Text(_whenText(job)),
            ),
            if (retentionText(job) != null)
              ListTile(
                leading: const Icon(Icons.history_outlined),
                title: Text(bt('backup.jobs.fact.keep')),
                subtitle: Text(retentionText(job)!),
              ),
            if (job.type != 'archive')
              ListTile(
                leading: const Icon(Icons.shield_outlined),
                title: Text(bt('backup.jobs.fact.safety')),
                subtitle: Text(job.type == 'mirror'
                    ? bt('backup.jobs.safety', {'delete': '${job.deletePct}', 'change': '${job.changePct}'})
                    : bt('backup.summary.guard_change', {'change': '${job.changePct}'})),
              ),
            ListTile(
              leading: const Icon(Icons.data_usage_outlined),
              title: Text(bt('backup.jobs.fact.size')),
              subtitle: Text([
                (job.sizeBytes ?? 0) > 0 ? formatSize(job.sizeBytes!) : bt('backup.jobs.size_unknown'),
                if ((job.versionsCount ?? 0) > 0) btc('backup.jobs.versions_count', job.versionsCount!),
              ].join(' · ')),
            ),
          ]),
          _versionsGroup(job, versions),
          TileGroup(
            title: bt('backup.jobs.tab.history'),
            footer: runs != null && runs.isEmpty ? bt('backup.jobs.no_history') : null,
            children: [
              if (runs == null) ...SkeletonRow.group(rows: 3),
              for (final r in runs ?? const <BackupRun>[]) _RunRow(run: r, onTap: () => _open(BackupRunScreen(runId: r.id, api: widget.api))),
              if (_nextBefore.isNotEmpty)
                ListTile(
                  leading: _loadingMore ? const SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2)) : const Icon(Icons.expand_more),
                  title: const Text('Show older runs'),
                  onTap: _loadingMore ? null : _more,
                ),
            ],
          ),
          const SizedBox(height: Space.lg),
        ]),
      ];
    }
    return AppScaffold.slivers(
      title: bt('backup.app.title'),
      onRefresh: () => _load(),
      banner: job != null && error != null && BackupError.from(error).isUnreachable ? OfflineBanner(onRetry: _load) : null,
      actions: [
        if (job != null)
          IconButton(
            tooltip: bt('backup.jobs.menu.edit'),
            icon: const Icon(Icons.edit_outlined),
            onPressed: () => _open(BackupJobFormScreen(job: job, api: widget.api)),
          ),
      ],
      slivers: slivers,
    );
  }

  Widget _versionsGroup(BackupJob job, List<BackupVersion>? versions) {
    final shown = (versions ?? const <BackupVersion>[]).take(6).toList();
    return TileGroup(
      title: bt('backup.jobs.tab.versions'),
      footer: versions != null && versions.isEmpty
          ? bt('backup.restore.no_versions')
          : job.type == 'copy'
              ? bt('backup.restore.copy_note')
              : null,
      children: [
        if (versions == null) ...SkeletonRow.group(rows: 2),
        for (final v in shown)
          ListTile(
            leading: Icon(v.kind == 'current' ? Icons.folder_copy_outlined : (v.kind == 'archive' ? Icons.inventory_2_outlined : Icons.history_outlined)),
            title: Text(v.label(currentAt: job.lastSuccess)),
            subtitle: v.meta.isEmpty ? null : Text(v.meta),
            trailing: const Icon(Icons.chevron_right),
            onTap: () => _open(BackupRestoreScreen(jobs: [job], job: job, versionId: v.id, api: widget.api)),
          ),
        if (versions != null && versions.isNotEmpty)
          ListTile(
            leading: const Icon(Icons.restore_outlined),
            title: const Text('Restore files…'),
            subtitle: Text(bt('backup.restore.browse_hint')),
            onTap: () => _open(BackupRestoreScreen(jobs: [job], job: job, api: widget.api)),
          ),
      ],
    );
  }

  static String _whenText(BackupJob job) {
    final parts = [
      for (final t in job.triggers)
        if (t.kind == 'schedule' && t.cron.isNotEmpty) cronText(t.cron) else if (t.kind == 'volume_mounted') bt('backup.jobs.on_plug_in'),
    ];
    if (parts.isEmpty) parts.add(bt('backup.jobs.manual_only'));
    final next = nextRunText(job);
    if (next.isNotEmpty) parts.add(next);
    if (!job.enabled) parts.add(bt('backup.health.disabled'));
    return parts.join(' · ');
  }
}

/// The job's header: type, name, state, the live progress when running,
/// and its actions - Back up now (or Cancel run) and Pause / Resume.
class _JobHeader extends StatelessWidget {
  const _JobHeader({
    required this.job,
    required this.live,
    required this.busy,
    required this.onRunNow,
    required this.onCancel,
    required this.onToggle,
    this.onOpenRun,
  });

  final BackupJob job;
  final LiveStats? live;
  final bool busy;
  final VoidCallback onRunNow;
  final VoidCallback onCancel;
  final VoidCallback onToggle;
  final VoidCallback? onOpenRun;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final t = DesignTokens.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    final active = job.isActive ? job.activeRun : null;
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, t.gap),
      child: Material(
        color: t.cardColor,
        shape: t.cardBorder == null
            ? RoundedRectangleBorder(borderRadius: BorderRadius.circular(t.cardRadius))
            : RoundedRectangleBorder(borderRadius: BorderRadius.circular(t.cardRadius), side: BorderSide(color: t.cardBorder!)),
        child: Padding(
          padding: const EdgeInsets.all(Space.lg),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(children: [
                Icon(jobTypeIcon(job.type), size: 20, color: scheme.onSurfaceVariant),
                const SizedBox(width: Space.sm),
                Expanded(child: Text(bt('backup.type.${job.type}.label'), style: theme.textTheme.labelLarge?.copyWith(color: scheme.onSurfaceVariant))),
              ]),
              const SizedBox(height: Space.sm),
              Text(job.name, style: theme.textTheme.headlineSmall),
              const SizedBox(height: Space.sm),
              Wrap(spacing: Space.sm, runSpacing: Space.sm, children: [
                JobStateChip(job: job, live: live),
                if (job.isImported) StatusChip(label: bt('backup.jobs.imported_badge'), status: Status.info, icon: Icons.input_outlined),
              ]),
              const SizedBox(height: Space.md),
              if (active != null && active.status == 'running')
                InkWell(onTap: onOpenRun, child: RunProgress(live: live))
              else if (active != null && active.status == 'queued')
                Text(bt('backup.run.queued_note'), style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant))
              else
                Text(lastRunText(job), style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
              const SizedBox(height: Space.lg),
              Wrap(spacing: Space.sm, runSpacing: Space.sm, children: [
                if (active == null)
                  FilledButton.icon(onPressed: busy ? null : onRunNow, icon: const Icon(Icons.play_arrow_outlined), label: const Text('Back up now'))
                else ...[
                  if (active.status != 'waiting_user')
                    OutlinedButton.icon(onPressed: onOpenRun, icon: const Icon(Icons.open_in_full_outlined), label: Text(bt('backup.jobs.open_progress'))),
                  OutlinedButton.icon(
                    onPressed: busy ? null : onCancel,
                    style: OutlinedButton.styleFrom(foregroundColor: scheme.error),
                    icon: const Icon(Icons.stop_circle_outlined),
                    label: Text(bt('backup.run.cancel')),
                  ),
                ],
                OutlinedButton.icon(
                  onPressed: busy ? null : onToggle,
                  icon: Icon(job.enabled ? Icons.pause_outlined : Icons.play_circle_outline),
                  label: Text(job.enabled ? bt('backup.jobs.menu.pause') : bt('backup.jobs.menu.resume')),
                ),
              ]),
            ],
          ),
        ),
      ),
    );
  }
}

/// One run in the history: its status, kind and when, how long it took
/// and what it moved.
class _RunRow extends StatelessWidget {
  const _RunRow({required this.run, required this.onTap});

  final BackupRun run;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final (tone, icon) = runStatusView(run.status);
    final color = tone == Status.neutral ? Theme.of(context).colorScheme.onSurfaceVariant : StatusColors.toneOf(context, tone).color;
    final at = run.at;
    final d = run.duration;
    final facts = [
      if (at != null) formatWhen(at),
      if (d != null) formatSeconds(d.inSeconds),
      if (run.counts.bytesTransferred > 0) formatSize(run.counts.bytesTransferred),
    ];
    return ListTile(
      leading: Icon(icon, color: color),
      title: Text('${statusText(run.status)} · ${bt('backup.kind.${run.kind}')}'),
      subtitle: Text(facts.join(' · ')),
      trailing: const Icon(Icons.chevron_right),
      onTap: onTap,
    );
  }
}
