import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../backup/backup_api.dart';
import '../../backup/backup_models.dart';
import '../../backup/backup_state.dart';
import '../../backup/backup_strings.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import 'backup_decide_screen.dart';
import 'backup_widgets.dart';

/// One run (the web's run window): its state and live progress, what it
/// did, the files that failed, its steps, the details log, and Cancel
/// while it runs or Run again once it's over. It follows the run at the
/// phone's refresh setting until it ends (the web polls every 3 s when
/// its socket is down).
class BackupRunScreen extends StatefulWidget {
  const BackupRunScreen({super.key, required this.runId, this.api});

  final String runId;
  final BackupApi? api;

  @override
  State<BackupRunScreen> createState() => _BackupRunScreenState();
}

class _BackupRunScreenState extends State<BackupRunScreen> with WidgetsBindingObserver, BackupPolling {
  late final BackupApi _api = widget.api ?? BackupApi();

  BackupRun? _run;
  Object? _error;
  bool _busy = false;

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
  bool get wantsPolling => !(_run?.isFinal ?? false);

  @override
  Future<void> poll() => _load();

  Future<void> _load() async {
    try {
      final run = await _api.getRun(widget.runId);
      if (mounted) {
        setState(() {
          _run = run;
          _error = null;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _cancel() async {
    final run = _run;
    if (run == null) return;
    final ok = await ConfirmDialog.destructive(
      context,
      title: bt('backup.run.cancel_title', {'name': run.jobName}),
      message: bt(run.kind == 'restore' ? 'backup.run.cancel_confirm_restore' : 'backup.run.cancel_confirm'),
      confirmLabel: bt('backup.run.cancel'),
      permanent: false,
    );
    if (!ok || !mounted) return;
    setState(() => _busy = true);
    try {
      final r = await _api.cancel(run.id);
      if (mounted) setState(() => _run = r);
    } catch (e) {
      if (mounted && BackupError.from(e).code != 'invalid_state') showBackupError(context, e);
    } finally {
      if (mounted) setState(() => _busy = false);
      await _load();
    }
  }

  Future<void> _runAgain() async {
    final run = _run;
    if (run == null) return;
    setState(() => _busy = true);
    try {
      final id = await _api.run(run.jobId);
      if (!mounted) return;
      showBackupSnack(context, bt('backup.jobs.started', {'name': run.jobName}));
      if (id.isNotEmpty && id != run.id) {
        await Navigator.of(context).pushReplacement(MaterialPageRoute<void>(builder: (_) => BackupRunScreen(runId: id, api: widget.api)));
      }
    } catch (e) {
      if (mounted) showBackupError(context, e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _review() async {
    final run = _run;
    if (run == null) return;
    await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => BackupDecideScreen(runId: run.id, jobId: run.jobId, api: widget.api)));
    if (mounted) unawaited(_load());
  }

  @override
  Widget build(BuildContext context) {
    final run = _run;
    final error = _error;
    final List<Widget> slivers;
    if (run == null && error != null) {
      slivers = [backupErrorState(error, title: "Couldn't load this run", onRetry: _load, sliver: true)];
    } else if (run == null) {
      slivers = const [SliverLoadingList(rows: 6)];
    } else {
      final summary = run.summary?.text ?? '';
      slivers = [
        SliverList.list(children: [
          _RunHeader(run: run),
          if (run.restore?.dryRun ?? false)
            const Notice(status: Status.info, message: 'This was a check: no files were changed. It shows what a restore would do.'),
          if (run.status == 'waiting_user')
            Notice(
              status: Status.warning,
              icon: Icons.back_hand_outlined,
              title: bt('backup.attention.waiting.title'),
              message: summary.isNotEmpty ? summary : bt('backup.attention.waiting.cause'),
              actionLabel: bt('backup.action.review'),
              onAction: _review,
            ),
          if (run.isFinal) _result(context, run),
          if (run.fileErrors.isNotEmpty)
            TileGroup(title: btc('backup.run.file_errors', run.fileErrors.length), children: [
              for (final e in run.fileErrors.take(50))
                ListTile(
                  leading: Icon(Icons.insert_drive_file_outlined, color: Theme.of(context).colorScheme.error),
                  title: Text(e.path, style: DesignTokens.of(context).mono(Theme.of(context).textTheme.bodyMedium)),
                  subtitle: Text(BackupError.fieldText(e.code)),
                ),
            ]),
          if (run.steps.isNotEmpty)
            TileGroup(title: bt('backup.run.steps'), children: [for (final s in run.steps) _StepRow(step: s)]),
          if (run.hasLog)
            TileGroup(children: [
              ListTile(
                leading: const Icon(Icons.receipt_long_outlined),
                title: Text(run.isFinal ? bt('backup.run.details') : bt('backup.run.details_live')),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => BackupLogScreen(run: run, api: widget.api))),
              ),
            ]),
          _actions(context, run),
          const SizedBox(height: Space.lg),
        ]),
      ];
    }
    return AppScaffold.slivers(
      title: run?.jobName.isNotEmpty ?? false ? run!.jobName : bt('backup.kind.backup'),
      onRefresh: _load,
      banner: run != null && error != null && BackupError.from(error).isUnreachable ? OfflineBanner(onRetry: _load) : null,
      slivers: slivers,
    );
  }

  Widget _result(BuildContext context, BackupRun run) {
    final c = run.counts;
    final d = run.duration;
    final summary = run.summary?.text ?? '';
    final cause = run.errorCode.isNotEmpty && run.status != 'success' ? BackupError(run.errorCode).causeText : '';
    return TileGroup(
      title: statusText(run.status),
      footer: [summary, cause].where((s) => s.isNotEmpty).join(' '),
      children: [
        MetricRow(icon: Icons.add_circle_outline, label: bt('backup.run.count.added'), value: formatNumber(c.added)),
        MetricRow(icon: Icons.change_circle_outlined, label: bt('backup.run.count.changed'), value: formatNumber(c.changed)),
        if (c.deleted > 0)
          MetricRow(icon: Icons.remove_circle_outline, label: bt(run.kind == 'backup' ? 'backup.run.count.recycled' : 'backup.run.count.deleted'), value: formatNumber(c.deleted)),
        if (c.skipped > 0) MetricRow(icon: Icons.redo_outlined, label: bt('backup.run.count.skipped'), value: formatNumber(c.skipped)),
        if (c.errored > 0) MetricRow(icon: Icons.error_outline, label: bt('backup.run.count.errored'), value: formatNumber(c.errored)),
        MetricRow(icon: Icons.data_usage_outlined, label: bt('backup.run.count.data'), value: formatSize(c.bytesTransferred)),
        if (d != null) MetricRow(icon: Icons.timer_outlined, label: bt('backup.run.count.duration'), value: formatSeconds(d.inSeconds)),
      ],
    );
  }

  Widget _actions(BuildContext context, BackupRun run) {
    final scheme = Theme.of(context).colorScheme;
    final buttons = <Widget>[
      if (run.status == 'waiting_user') FilledButton(onPressed: _busy ? null : _review, child: Text(bt('backup.action.review'))),
      if (run.isFinal && run.kind == 'backup' && run.jobId.isNotEmpty)
        FilledButton.icon(onPressed: _busy ? null : _runAgain, icon: const Icon(Icons.replay_outlined), label: Text(bt('backup.action.retry'))),
      if (!run.isFinal)
        OutlinedButton.icon(
          onPressed: _busy ? null : _cancel,
          style: OutlinedButton.styleFrom(foregroundColor: scheme.error),
          icon: const Icon(Icons.stop_circle_outlined),
          label: Text(bt('backup.run.cancel')),
        ),
    ];
    if (buttons.isEmpty) return const SizedBox.shrink();
    return Padding(
      padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.lg, Space.gutter(context), 0),
      child: Wrap(spacing: Space.sm, runSpacing: Space.sm, children: buttons),
    );
  }
}

/// The run's status, what kind of run it is and why it ran, and - while
/// it runs - its progress and the file it's on.
class _RunHeader extends StatelessWidget {
  const _RunHeader({required this.run});

  final BackupRun run;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final t = DesignTokens.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    final sub = [
      bt('backup.kind.${run.kind}'),
      if (btHas('backup.trigger.${run.trigger}')) bt('backup.trigger.${run.trigger}'),
      if (run.startedAt != null)
        bt('backup.run.started', {'when': formatWhen(run.startedAt!)})
      else if (run.queuedAt != null)
        bt('backup.run.queued_at', {'when': formatWhen(run.queuedAt!)}),
    ];
    final live = run.live;
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, t.gap),
      child: Material(
        color: t.cardColor,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(t.cardRadius),
          side: t.cardBorder == null ? BorderSide.none : BorderSide(color: t.cardBorder!),
        ),
        child: Padding(
          padding: const EdgeInsets.all(Space.lg),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              RunStatusChip(status: run.status, percent: run.status == 'running' && live?.ratio != null ? (live!.ratio! * 100).floor() : null),
              const SizedBox(height: Space.md),
              FactLine.plain(sub, style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
              if (run.status == 'running') ...[
                const SizedBox(height: Space.lg),
                RunProgress(live: live, phase: run.phase),
                if (live != null && live.currentFile.isNotEmpty) ...[
                  const SizedBox(height: Space.sm),
                  Text('${bt('backup.run.now')} ${live.currentFile}',
                      style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant), maxLines: 2, overflow: TextOverflow.ellipsis),
                ],
              ] else if (run.status == 'queued') ...[
                const SizedBox(height: Space.md),
                Text(bt('backup.run.queued_note'), style: theme.textTheme.bodyMedium),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// One step: its state's glyph and what it does.
class _StepRow extends StatelessWidget {
  const _StepRow({required this.step});

  final RunStep step;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final (icon, color) = switch (step.state) {
      'done' => (Icons.check_circle_outline, StatusColors.toneOf(context, Status.success).color),
      'active' => (Icons.pending_outlined, StatusColors.toneOf(context, Status.info).color),
      'failed' => (Icons.cancel_outlined, scheme.error),
      _ => (Icons.radio_button_unchecked, scheme.onSurfaceVariant),
    };
    final text = step.message?.text ?? '';
    final state = btHas('backup.run.step_state.${step.state}') ? bt('backup.run.step_state.${step.state}') : step.state;
    return ListTile(
      leading: Icon(icon, color: color),
      title: Text(text.isEmpty ? (btHas('backup.phase.${step.phase}') ? bt('backup.phase.${step.phase}') : step.phase) : text),
      subtitle: Text(state),
    );
  }
}

/// A run's details log: line by line with its level, newest at the end,
/// read in pages (GET /runs/:id/log?after=) and followed live while the
/// run is going. Copy puts it on the clipboard.
class BackupLogScreen extends StatefulWidget {
  const BackupLogScreen({super.key, required this.run, this.api});

  final BackupRun run;
  final BackupApi? api;

  @override
  State<BackupLogScreen> createState() => _BackupLogScreenState();
}

class _BackupLogScreenState extends State<BackupLogScreen> with WidgetsBindingObserver, BackupPolling {
  late final BackupApi _api = widget.api ?? BackupApi();
  final List<LogLine> _lines = [];
  int _offset = 0;
  bool _done = false;
  bool _loading = false;
  bool _raw = false;
  Object? _error;

  // A log can be long; the phone keeps the last lines.
  static const _keep = 5000;

  @override
  void initState() {
    super.initState();
    _more();
    startPolling();
  }

  @override
  void dispose() {
    stopPolling();
    super.dispose();
  }

  @override
  bool get wantsPolling => !_done;

  @override
  Future<void> poll() => _more();

  Future<void> _more() async {
    if (_loading) return;
    _loading = true;
    try {
      // Read until caught up, a page at a time.
      for (var i = 0; i < 10; i++) {
        final page = await _api.log(widget.run.id, after: _offset);
        if (!mounted) return;
        setState(() {
          _lines.addAll(page.lines);
          if (_lines.length > _keep) _lines.removeRange(0, _lines.length - _keep);
          _offset = page.nextOffset;
          _done = page.done;
          _error = null;
        });
        if (page.lines.isEmpty || page.done) break;
      }
    } catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      _loading = false;
    }
  }

  Future<void> _copy() async {
    final text = _lines.map((l) => [if (l.time != null) formatExact(l.time!), l.level.toUpperCase(), _raw && l.raw.isNotEmpty ? l.raw : l.text].join('  ')).join('\n');
    await Clipboard.setData(ClipboardData(text: text));
    if (mounted) showBackupSnack(context, bt('backup.log.copied_toast'));
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final mono = DesignTokens.of(context).mono(theme.textTheme.bodySmall);
    final Widget body;
    if (_lines.isEmpty && _error != null) {
      body = backupErrorState(_error!, title: "Couldn't load the log", onRetry: _more);
    } else if (_lines.isEmpty && !_done) {
      body = const LoadingList(rows: 8, leading: SkeletonLeading.none);
    } else if (_lines.isEmpty) {
      body = EmptyState(icon: Icons.receipt_long_outlined, title: bt('backup.log.empty'), message: '');
    } else {
      body = ListView.builder(
        padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.sm, Space.gutter(context), Space.xl + MediaQuery.paddingOf(context).bottom),
        itemCount: _lines.length,
        itemBuilder: (context, i) {
          final l = _lines[i];
          final (icon, color) = switch (l.level) {
            'error' => (Icons.error_outline, scheme.error),
            'warn' => (Icons.warning_amber_outlined, StatusColors.toneOf(context, Status.warning).color),
            _ => (Icons.circle_outlined, scheme.onSurfaceVariant),
          };
          final text = _raw && l.raw.isNotEmpty ? l.raw : l.text;
          return Padding(
            padding: const EdgeInsets.symmetric(vertical: Space.xs),
            child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Padding(
                padding: const EdgeInsets.only(top: 2, right: Space.sm),
                child: Icon(icon, size: l.level == 'info' ? 10 : 16, color: color, semanticLabel: bt('backup.log.lvl_${l.level == 'warn' ? 'warn' : l.level == 'error' ? 'error' : 'info'}')),
              ),
              Expanded(
                child: Text.rich(TextSpan(children: [
                  if (l.time != null) TextSpan(text: '${_hms(l.time!)}  ', style: mono.copyWith(color: scheme.onSurfaceVariant)),
                  TextSpan(text: text, style: _raw ? mono : theme.textTheme.bodyMedium),
                ])),
              ),
            ]),
          );
        },
      );
    }
    return AppScaffold(
      title: bt('backup.log.label'),
      onRefresh: _more,
      actions: [
        IconButton(
          tooltip: bt('backup.log.show_raw'),
          isSelected: _raw,
          icon: const Icon(Icons.code_outlined),
          onPressed: () => setState(() => _raw = !_raw),
        ),
        IconButton(tooltip: bt('backup.log.copy'), icon: const Icon(Icons.copy_outlined), onPressed: _lines.isEmpty ? null : _copy),
      ],
      body: body,
    );
  }

  static String _hms(DateTime t) {
    final l = t.toLocal();
    String two(int n) => n.toString().padLeft(2, '0');
    return '${two(l.hour)}:${two(l.minute)}:${two(l.second)}';
  }
}
