import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';

import '../../backup/backup_api.dart';
import '../../backup/backup_models.dart';
import '../../backup/backup_state.dart';
import '../../backup/backup_strings.dart';
import '../../services/api_client.dart';
import '../../ui/ui.dart';
import '../dashboard_screen.dart' show StatusDisc;
import 'backup_decide_screen.dart';
import 'backup_job_form_screen.dart';
import 'backup_job_screen.dart';
import 'backup_restore_screen.dart';
import 'backup_widgets.dart';

/// Backup & Sync, the overview (backup plan WP1-1; the web's Overview and
/// Jobs sections): how the backups are doing, what needs the owner first,
/// what's running with its progress, every job with Back up now and
/// Pause / Resume, and what's coming up.
///
/// The module is optional: when its health route doesn't answer, the
/// screen says so and how to install it, instead of an error. It polls at
/// the phone's refresh setting while on screen and on pull to refresh.
class BackupScreen extends StatefulWidget {
  const BackupScreen({super.key, this.api});

  /// For tests; the app's session otherwise.
  final BackupApi? api;

  /// Forgets the last answer kept for the next visit (tests).
  static void clearCache() => _BackupScreenState._cache.clear();

  @override
  State<BackupScreen> createState() => _BackupScreenState();
}

class _BackupScreenState extends State<BackupScreen> with WidgetsBindingObserver, BackupPolling {
  late final BackupApi _api = widget.api ?? BackupApi();

  // Kept for the next visit, per server, so the screen opens on the last
  // answer while it refreshes.
  static final Map<String, ({List<BackupJob> jobs, DateTime at})> _cache = {};

  BackupHealth? _health;
  List<BackupJob>? _jobs;
  DateTime? _loadedAt;
  Object? _error;
  bool _loading = false;
  final Map<String, LiveStats?> _live = {};
  final Set<String> _busy = {};

  @override
  void initState() {
    super.initState();
    final cached = _cache[ApiClient.instance.baseUrl];
    if (cached != null) {
      _jobs = cached.jobs;
      _loadedAt = cached.at;
    }
    _load();
    startPolling();
  }

  @override
  void dispose() {
    stopPolling();
    super.dispose();
  }

  // Not installed: nothing to follow until "Check again".
  @override
  bool get wantsPolling => _health?.installed ?? true;

  @override
  Future<void> poll() => _load(quiet: true);

  Future<void> _load({bool quiet = false}) async {
    if (_loading) return;
    _loading = true;
    try {
      if (_health == null || !_health!.installed || !quiet) {
        final h = await _api.health();
        if (!mounted) return;
        setState(() => _health = h);
        if (!h.installed || !h.running) return;
      }
      final jobs = await _api.jobs();
      final live = <String, LiveStats?>{};
      await Future.wait([
        for (final j in jobs)
          if (j.activeRun?.status == 'running')
            () async {
              try {
                live[j.id] = (await _api.getRun(j.activeRun!.id)).live;
              } catch (_) {
                live[j.id] = _live[j.id];
              }
            }(),
      ]);
      if (!mounted) return;
      final now = clock.now();
      _cache[ApiClient.instance.baseUrl] = (jobs: jobs, at: now);
      setState(() {
        _jobs = jobs;
        _loadedAt = now;
        _error = null;
        _live
          ..clear()
          ..addAll(live);
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = e);
    } finally {
      _loading = false;
    }
  }

  Future<void> _refresh() async {
    _health = null;
    await _load();
  }

  Future<void> _open(Widget screen) async {
    await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => screen));
    if (mounted) unawaited(_load(quiet: true));
  }

  void _openJob(BackupJob job) => _open(BackupJobScreen(jobId: job.id, initial: job, api: widget.api));

  void _openAttention(BackupAttention a) {
    if (a.reason == 'waiting' && a.runId.isNotEmpty) {
      _open(BackupDecideScreen(runId: a.runId, jobId: a.job.id, api: widget.api));
    } else {
      _openJob(a.job);
    }
  }

  Future<void> _runNow(BackupJob job) async {
    setState(() => _busy.add(job.id));
    try {
      await _api.run(job.id);
      if (mounted) showBackupSnack(context, bt('backup.jobs.started', {'name': job.name}));
      await _load(quiet: true);
    } catch (e) {
      if (mounted) showBackupError(context, e);
    } finally {
      if (mounted) setState(() => _busy.remove(job.id));
    }
  }

  // Optimistic: the row flips at once and flips back if the server says no.
  Future<void> _toggle(BackupJob job) async {
    final jobs = _jobs;
    if (jobs == null) return;
    final next = !job.enabled;
    setState(() {
      _busy.add(job.id);
      _jobs = [for (final j in jobs) j.id == job.id ? j.copyWith(enabled: next) : j];
    });
    try {
      final saved = await _api.toggle(job.id, next);
      if (!mounted) return;
      setState(() => _jobs = [for (final j in _jobs!) j.id == saved.id ? saved : j]);
      showBackupSnack(context, bt(next ? 'backup.jobs.resumed' : 'backup.jobs.paused', {'name': job.name}));
    } catch (e) {
      if (!mounted) return;
      setState(() => _jobs = [for (final j in _jobs!) j.id == job.id ? job : j]);
      showBackupError(context, e);
    } finally {
      if (mounted) setState(() => _busy.remove(job.id));
    }
  }

  Future<void> _newJob() => _open(BackupJobFormScreen(api: widget.api));

  @override
  Widget build(BuildContext context) {
    final health = _health;
    final jobs = _jobs;
    final error = _error;
    final installed = health == null || (health.installed && health.running);
    final offline = error != null && BackupError.from(error).isUnreachable;
    final List<Widget> slivers;

    if (health != null && !health.installed) {
      slivers = [
        EmptyState(
          icon: Icons.backup_outlined,
          title: "Backup & Sync isn't installed",
          message: 'Backup & Sync is an optional part of NivaroOS. To add it, run the NivaroOS installer on the server again with --with-backup, then check again here.',
          actionLabel: 'Check again',
          onAction: _refresh,
          sliver: true,
        ),
      ];
    } else if (health != null && !health.running) {
      slivers = [
        ErrorState(
          icon: Icons.backup_outlined,
          title: bt('backup.app.unavailable.title'),
          message: '${bt('backup.app.unavailable.cause')} ${bt('backup.app.unavailable.fix')}',
          onRetry: _refresh,
          sliver: true,
        ),
      ];
    } else if (jobs == null && error != null) {
      slivers = [backupErrorState(error, title: "Couldn't load backups", onRetry: _refresh, sliver: true)];
    } else if (jobs == null) {
      slivers = const [SliverLoadingList(rows: 6)];
    } else if (jobs.isEmpty) {
      slivers = [
        EmptyState(
          icon: Icons.backup_outlined,
          title: bt('backup.empty.title'),
          message: bt('backup.empty.text'),
          actionLabel: bt('backup.jobs.new'),
          onAction: _newJob,
          sliver: true,
        ),
      ];
    } else {
      final attention = attentionItems(jobs);
      final running = runningJobs(jobs);
      final upcoming = sortedJobs(jobs).where((j) => j.enabled && j.nextRun != null && !j.isActive).toList()
        ..sort((a, b) => a.nextRun!.compareTo(b.nextRun!));
      final plugIns = jobs.where((j) => j.enabled && j.triggers.any((t) => t.kind == 'volume_mounted')).take(3).toList();
      slivers = [
        SliverList.list(children: [
          _OverviewHeader(overall: overallState(jobs)),
          if (attention.isNotEmpty)
            TileGroup(
              title: bt('backup.overview.attention_title', {'count': '${attention.length}'}),
              children: [for (final a in attention) _AttentionRow(item: a, onTap: () => _openAttention(a))],
            ),
          if (running.isNotEmpty)
            TileGroup(title: bt('backup.overview.running_title'), children: [
              for (final j in running) BackupJobTile(job: j, live: _live[j.id], onTap: () => _openJob(j)),
            ]),
          TileGroup(title: bt('backup.nav.jobs'), children: [
            for (final j in sortedJobs(jobs).where((j) => !running.contains(j)))
              BackupJobTile(
                job: j,
                busy: _busy.contains(j.id),
                onTap: () => _openJob(j),
                onRunNow: () => _runNow(j),
                onToggle: () => _toggle(j),
              ),
          ]),
          TileGroup(
            title: bt('backup.overview.upcoming_title'),
            footer: upcoming.isEmpty && plugIns.isEmpty ? bt('backup.overview.upcoming_none') : null,
            children: [
              for (final j in upcoming.take(5))
                ListTile(
                  leading: const Icon(Icons.event_outlined),
                  title: Text(j.name),
                  subtitle: Text(formatWhen(j.nextRun!)),
                  onTap: () => _openJob(j),
                ),
              for (final j in plugIns)
                ListTile(
                  leading: const Icon(Icons.usb_outlined),
                  title: Text(j.name),
                  subtitle: Text(bt('backup.overview.on_plug_in')),
                  onTap: () => _openJob(j),
                ),
            ],
          ),
          TileGroup(children: [
            ListTile(
              leading: const Icon(Icons.restore_outlined),
              title: Text(bt('backup.nav.restore')),
              subtitle: const Text('Get files back from a backup'),
              trailing: const Icon(Icons.chevron_right),
              onTap: () => _open(BackupRestoreScreen(jobs: jobs, api: widget.api)),
            ),
          ]),
          // Room for the FAB over the last row.
          SizedBox(height: 88 * MediaQuery.textScalerOf(context).scale(1)),
        ]),
      ];
    }

    return AppScaffold.slivers(
      title: bt('backup.app.title'),
      onRefresh: _refresh,
      banner: offline && jobs != null ? OfflineBanner(lastUpdated: _loadedAt, onRetry: _refresh) : null,
      floatingActionButton: installed && jobs != null && jobs.isNotEmpty
          ? FloatingActionButton.extended(onPressed: _newJob, icon: const Icon(Icons.add), label: Text(bt('backup.jobs.new')))
          : null,
      slivers: slivers,
    );
  }
}

/// The overview's one expressive moment: the verdict on a status disc,
/// how many jobs, how many need you, and the last success.
class _OverviewHeader extends StatelessWidget {
  const _OverviewHeader({required this.overall});

  final BackupOverall overall;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final t = DesignTokens.of(context);
    final (status, icon) = switch (overall.state) {
      'problem' => (Status.error, Icons.error_outline),
      'attention' => (Status.warning, Icons.warning_amber_outlined),
      _ => (Status.success, Icons.check_circle_outline),
    };
    final last = overall.lastSuccess;
    final facts = [
      btc('backup.overview.jobs_count', overall.jobs),
      if (overall.attention > 0) btc('backup.overview.attention_count', overall.attention),
      last == null ? bt('backup.overview.no_success_yet') : bt('backup.overview.last_success', {'when': _lower(formatRelative(last))}),
    ];
    final tone = StatusColors.toneOf(context, status);
    final dark = theme.brightness == Brightness.dark;
    final calm = status == Status.success || dark;
    final panel = !t.statusPanel
        ? null
        : status == Status.success
            ? scheme.surfaceContainerHighest
            : dark
                ? Color.alphaBlend(tone.color.withValues(alpha: .16), scheme.surfaceContainerHigh)
                : tone.container;
    final on = panel == null || calm ? scheme.onSurface : tone.onContainer;
    final muted = panel == null || calm ? scheme.onSurfaceVariant : tone.onContainer;
    final text = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(overall.title, style: theme.textTheme.headlineSmall?.copyWith(color: on)),
        const SizedBox(height: Space.xs),
        FactLine.plain(facts, style: theme.textTheme.bodyMedium?.copyWith(color: muted)),
      ],
    );
    final disc = StatusDisc(status: status, icon: icon, size: 56);
    final stacked = MediaQuery.textScalerOf(context).scale(10) > 15;
    final content = Semantics(
      container: true,
      liveRegion: true,
      label: [overall.title, ...facts].join('. '),
      child: ExcludeSemantics(
        child: stacked
            ? Column(crossAxisAlignment: CrossAxisAlignment.start, children: [disc, const SizedBox(height: Space.md), text])
            : Row(children: [disc, const SizedBox(width: Space.lg), Expanded(child: text)]),
      ),
    );
    final gutter = Space.gutter(context);
    if (panel == null) return Padding(padding: EdgeInsets.fromLTRB(gutter, Space.md, gutter, Space.lg), child: content);
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, t.gap),
      child: Material(
        color: panel,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(t.cardRadius)),
        child: Padding(padding: const EdgeInsets.all(Space.lg), child: content),
      ),
    );
  }

  static String _lower(String s) => s == 'Just now' || s == 'Yesterday' ? s.toLowerCase() : s;
}

/// A job that needs you: the severity glyph, the job, and why.
class _AttentionRow extends StatelessWidget {
  const _AttentionRow({required this.item, required this.onTap});

  final BackupAttention item;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final status = item.severity == BackupSeverity.problem ? Status.error : Status.warning;
    final icon = item.reason == 'waiting'
        ? Icons.back_hand_outlined
        : status == Status.error
            ? Icons.error_outline
            : Icons.warning_amber_outlined;
    return ListTile(
      leading: Icon(icon, color: StatusColors.toneOf(context, status).color),
      title: Text(item.job.name),
      subtitle: Text(item.title),
      trailing: const Icon(Icons.chevron_right),
      onTap: onTap,
    );
  }
}
