import 'dart:async';

import 'package:flutter/material.dart';

import '../../backup/backup_api.dart';
import '../../backup/backup_models.dart';
import '../../backup/backup_state.dart' show statusText;
import '../../backup/backup_strings.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import 'backup_run_screen.dart';
import 'backup_widgets.dart';

/// "Waiting for you" (the web's preview window): a run that stopped
/// before changing anything - it would delete or change more than the
/// job's limit, or the source looks empty - or a preview run that
/// finished. It shows why, what the run would add, update and delete
/// (searchable, a page at a time), and lets the owner continue (as shown,
/// or once as "Copy new files" when it would delete) or cancel it.
class BackupDecideScreen extends StatefulWidget {
  const BackupDecideScreen({super.key, required this.runId, this.jobId = '', this.api});

  final String runId;
  final String jobId;
  final BackupApi? api;

  @override
  State<BackupDecideScreen> createState() => _BackupDecideScreenState();
}

class _BackupDecideScreenState extends State<BackupDecideScreen> with WidgetsBindingObserver, BackupPolling {
  late final BackupApi _api = widget.api ?? BackupApi();
  static const _pageSize = 200;

  BackupRun? _run;
  BackupJob? _job;
  Object? _loadError;
  PreviewPage _counts = const PreviewPage();
  String _tab = 'add';
  bool _tabChosen = false;
  final List<PreviewItem> _items = [];
  int? _nextOffset = 0;
  bool _loadingItems = false;
  String? _listError;
  int _listSeq = 0;
  String _query = '';
  Timer? _searchTimer;

  // Nothing preselected when the run could go on either way (spec §12.6).
  String _mode = '';
  bool _deciding = false;
  String? _decisionError;

  final _scroll = ScrollController();

  bool get _waiting => _run?.status == 'waiting_user';
  bool get _working => _run?.status == 'queued' || _run?.status == 'running';
  bool get _previewDone => _run != null && _run!.kind == 'preview' && (_run!.status == 'success' || _run!.status == 'partial');
  String get _jobType => _job?.type ?? '';

  /// "Run it once as Copy new files" only when there's something to delete.
  bool get _offerCopyOnce => _jobType == 'mirror' && (_counts.delete > 0 || _run?.guard?.guard == 'delete');

  /// The decision's body, or null while one has to be picked.
  Map<String, Object?>? get _decision {
    if (!_offerCopyOnce) return {'proceed': true, 'mode': 'as_shown'};
    if (_mode != 'as_shown' && _mode != 'copy_once') return null;
    return {'proceed': true, 'mode': _mode};
  }

  @override
  void initState() {
    super.initState();
    _loadRun();
    final jobId = widget.jobId;
    if (jobId.isNotEmpty) {
      _api.job(jobId).then((j) {
        if (mounted) setState(() => _job = j);
      }, onError: (_) {});
    }
    _scroll.addListener(() {
      if (_scroll.position.pixels > _scroll.position.maxScrollExtent - 600) _loadMore();
    });
    startPolling();
  }

  @override
  void dispose() {
    stopPolling();
    _searchTimer?.cancel();
    _scroll.dispose();
    super.dispose();
  }

  @override
  bool get wantsPolling => _working;

  @override
  Future<void> poll() => _loadRun();

  Future<void> _loadRun() async {
    try {
      final wasWorking = _run == null || _working;
      final run = await _api.getRun(widget.runId);
      if (!mounted) return;
      setState(() {
        _run = run;
        _loadError = null;
      });
      if (_job == null && run.jobId.isNotEmpty && widget.jobId.isEmpty) {
        _api.job(run.jobId).then((j) {
          if (mounted) setState(() => _job = j);
        }, onError: (_) {});
      }
      if (wasWorking && (_waiting || _previewDone)) {
        if (!_tabChosen) {
          _tab = switch (run.guard?.guard) {
            'delete' => 'delete',
            'change' => 'update',
            _ => 'add',
          };
        }
        await _reloadItems();
      }
    } catch (e) {
      if (mounted) setState(() => _loadError = e);
    }
  }

  Future<void> _reloadItems() async {
    setState(() {
      _items.clear();
      _nextOffset = 0;
    });
    await _loadMore();
  }

  Future<void> _loadMore() async {
    final offset = _nextOffset;
    if (offset == null || _loadingItems) return;
    final seq = ++_listSeq;
    setState(() {
      _loadingItems = true;
      _listError = null;
    });
    try {
      final page = await _api.preview(widget.runId, op: _tab, q: _query.trim(), offset: offset, limit: _pageSize);
      if (!mounted || seq != _listSeq) return;
      setState(() {
        _counts = page;
        _items.addAll(page.items.where((i) => i.op == _tab));
        _nextOffset = page.nextOffset > 0 && page.items.isNotEmpty ? page.nextOffset : null;
      });
    } catch (e) {
      if (!mounted || seq != _listSeq) return;
      final err = BackupError.from(e);
      setState(() {
        _nextOffset = null;
        _listError = err.code == 'not_found' ? bt('backup.preview.no_plan') : err.title;
      });
    } finally {
      if (mounted && seq == _listSeq) setState(() => _loadingItems = false);
    }
  }

  void _setTab(String t) {
    if (t == _tab) return;
    setState(() {
      _tab = t;
      _tabChosen = true;
    });
    _reloadItems();
  }

  void _search(String q) {
    _query = q;
    _searchTimer?.cancel();
    _searchTimer = Timer(const Duration(milliseconds: 300), _reloadItems);
  }

  Future<void> _decide(bool proceed) async {
    if (_deciding) return;
    final body = proceed ? _decision : const {'proceed': false};
    if (body == null) return;
    setState(() {
      _deciding = true;
      _decisionError = null;
    });
    try {
      await _api.decide(widget.runId, proceed: proceed, mode: body['mode'] as String?);
      if (!mounted) return;
      if (proceed) {
        await Navigator.of(context).pushReplacement(MaterialPageRoute<void>(builder: (_) => BackupRunScreen(runId: widget.runId, api: widget.api)));
      } else {
        showBackupSnack(context, bt('backup.preview.cancelled'));
        Navigator.of(context).pop();
      }
    } catch (e) {
      if (!mounted) return;
      final err = BackupError.from(e);
      if (err.code == 'invalid_state') {
        // Decided elsewhere (the web, another phone).
        setState(() => _decisionError = bt('backup.preview.already_decided'));
        unawaited(_loadRun());
      } else {
        setState(() => _decisionError = err.title);
      }
    } finally {
      if (mounted) setState(() => _deciding = false);
    }
  }

  Future<void> _runNow() async {
    final jobId = _run?.jobId ?? '';
    if (jobId.isEmpty) return;
    setState(() => _deciding = true);
    try {
      final id = await _api.run(jobId);
      if (!mounted) return;
      await Navigator.of(context).pushReplacement(MaterialPageRoute<void>(builder: (_) => BackupRunScreen(runId: id, api: widget.api)));
    } catch (e) {
      if (mounted) showBackupError(context, e);
    } finally {
      if (mounted) setState(() => _deciding = false);
    }
  }

  String get _title {
    if (_waiting && _run?.guard != null) return bt('backup.preview.title_guard');
    return _jobType.isNotEmpty ? bt('backup.preview.title_$_jobType') : bt('backup.preview.title');
  }

  String? get _guardText {
    final g = _run?.guard;
    if (g == null) return null;
    final kind = const ['delete', 'change', 'empty_source'].contains(g.guard) ? g.guard : 'delete';
    return bt('backup.preview.guard.$kind', {'count': formatNumber(g.count), 'total': formatNumber(g.total), 'pct': formatNumber(g.pct), 'limit': '${g.limit}'});
  }

  @override
  Widget build(BuildContext context) {
    final run = _run;
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    final List<Widget> slivers;
    Widget? bottom;
    if (run == null && _loadError != null) {
      slivers = [backupErrorState(_loadError!, title: "Couldn't load this run", onRetry: _loadRun, sliver: true)];
    } else if (run == null) {
      slivers = const [SliverLoadingList(rows: 6)];
    } else if (_working) {
      slivers = [
        EmptyState(
          icon: Icons.hourglass_top_outlined,
          title: bt('backup.preview.working'),
          message: bt('backup.preview.working_hint'),
          actionLabel: bt('backup.preview.show_progress'),
          onAction: () => Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => BackupRunScreen(runId: run.id, api: widget.api))),
          sliver: true,
        ),
      ];
    } else if (!_waiting && !_previewDone) {
      // Over, one way or another: say how, as the web does.
      final code = run.errorCode.isNotEmpty ? run.errorCode : (run.status == 'cancelled' ? 'cancelled_by_user' : 'internal');
      final e = BackupError(code);
      slivers = [
        SliverList.list(children: [
          Notice(
              status: run.status == 'success' ? Status.success : Status.info,
              title: run.status == 'success' || run.status == 'queued' ? statusText(run.status) : e.title,
              message: [if (run.status != 'success') e.causeText, run.summary?.text ?? ''].where((s) => s.isNotEmpty).join(' '),
              actionLabel: bt('backup.run.open'),
              onAction: () => Navigator.of(context).pushReplacement(MaterialPageRoute<void>(builder: (_) => BackupRunScreen(runId: run.id, api: widget.api))),
            ),
          if (_decisionError != null) Padding(padding: EdgeInsets.fromLTRB(gutter, Space.md, gutter, 0), child: Text(_decisionError!)),
        ]),
      ];
    } else {
      final c = _counts;
      final guard = _guardText;
      final days = _job?.versionsDays ?? 0;
      slivers = [
        SliverList.list(children: [
          Padding(
            padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.sm),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(_title, style: theme.textTheme.headlineSmall),
              if (run.jobName.isNotEmpty) ...[
                const SizedBox(height: Space.xs),
                Text(run.jobName, style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
              ],
            ]),
          ),
          if (guard != null)
            Notice(status: Status.warning, icon: Icons.shield_outlined, message: guard)
          else if (c.delete > 0 && _jobType == 'mirror')
            Notice(
                status: Status.warning,
                message: days > 0
                    ? bt('backup.preview.delete_note', {'n': formatNumber(c.delete), 'days': '$days'})
                    : bt('backup.preview.delete_note_forever', {'n': formatNumber(c.delete)}))
          else if (c.add == 0 && c.update == 0 && c.delete == 0 && !_loadingItems)
            Notice(status: Status.success, message: bt('backup.preview.nothing')),
          if (_waiting && _offerCopyOnce)
            TileGroup(
              title: bt('backup.preview.decision'),
              children: [
                RadioGroup<String>(
                  groupValue: _mode,
                  onChanged: (v) => setState(() => _mode = v ?? ''),
                  child: Column(mainAxisSize: MainAxisSize.min, children: [
                    RadioListTile<String>(value: 'as_shown', title: Text(bt('backup.preview.mode_as_shown'))),
                    RadioListTile<String>(value: 'copy_once', title: Text(bt('backup.preview.mode_copy_once'))),
                  ]),
                ),
              ],
            ),
          SectionHeader(title: bt('backup.preview.counts_label')),
          Padding(
            padding: EdgeInsets.symmetric(horizontal: gutter),
            child: Semantics(
              label: bt('backup.preview.tabs_label'),
              child: SegmentedButton<String>(
                showSelectedIcon: false,
                segments: [
                  ButtonSegment(value: 'add', label: Text(bt('backup.preview.tab.add', {'n': formatNumber(c.add)}))),
                  ButtonSegment(value: 'update', label: Text(bt('backup.preview.tab.update', {'n': formatNumber(c.update)}))),
                  ButtonSegment(value: 'delete', label: Text(bt('backup.preview.tab.delete', {'n': formatNumber(c.delete)}))),
                ],
                selected: {_tab},
                onSelectionChanged: (s) => _setTab(s.first),
              ),
            ),
          ),
          if (_tab == 'add' && c.bytesAdd > 0)
            Padding(
              padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0),
              child: Text(bt('backup.preview.count_add', {'n': formatNumber(c.add), 'bytes': formatSize(c.bytesAdd)}),
                  style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant)),
            ),
          Padding(
            padding: EdgeInsets.fromLTRB(gutter, Space.md, gutter, Space.sm),
            child: SearchBar(
              hintText: bt('backup.preview.search'),
              leading: const Icon(Icons.search),
              elevation: const WidgetStatePropertyAll(0),
              onChanged: _search,
            ),
          ),
        ]),
        if (_listError != null)
          SliverToBoxAdapter(child: Padding(padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.lg), child: Text(_listError!)))
        else if (_items.isEmpty && !_loadingItems)
          SliverToBoxAdapter(
            child: Padding(
              padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.lg),
              child: Text(_query.isNotEmpty ? bt('backup.preview.no_match') : bt('backup.preview.empty_tab'),
                  style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
            ),
          )
        else
          SliverList.builder(
            itemCount: _items.length,
            itemBuilder: (context, i) {
              final it = _items[i];
              final (icon, label) = switch (it.op) {
                'add' => (Icons.add, bt('backup.preview.op.add')),
                'update' => (Icons.edit_outlined, bt('backup.preview.op.update')),
                _ => (Icons.remove, bt('backup.preview.op.delete')),
              };
              return ListTile(
                dense: true,
                leading: Icon(icon, size: 20, semanticLabel: label, color: it.op == 'delete' ? scheme.error : scheme.onSurfaceVariant),
                title: Text(it.path, maxLines: 2, overflow: TextOverflow.ellipsis),
                trailing: it.size > 0 ? Text(formatSize(it.size), style: theme.textTheme.bodySmall?.tabular) : null,
              );
            },
          ),
        if (_loadingItems) const SliverLoadingList(rows: 3, leading: SkeletonLeading.icon, subtitle: false),
        const SliverToBoxAdapter(child: SizedBox(height: Space.lg)),
      ];
      bottom = _bar(context);
    }
    final page = AppScaffold.slivers(
      title: _waiting ? bt('backup.action.review') : bt('backup.kind.preview'),
      controller: _scroll,
      onRefresh: _loadRun,
      slivers: slivers,
    );
    if (bottom == null) return page;
    // The decision stays in reach under a long list, as the web's footer.
    return Material(
      color: scheme.surface,
      child: Column(children: [
        Expanded(child: MediaQuery.removePadding(context: context, removeBottom: true, child: page)),
        bottom,
      ]),
    );
  }

  /// Cancel run / Continue while waiting; Run it now after a preview.
  Widget _bar(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final gutter = Space.gutter(context);
    final ready = _decision != null;
    return Material(
      color: DesignTokens.of(context).cardColor,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: EdgeInsets.fromLTRB(gutter, Space.md, gutter, Space.md),
          child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            if (_decisionError != null) ...[
              Text(_decisionError!, style: TextStyle(color: scheme.error)),
              const SizedBox(height: Space.sm),
            ] else if (_waiting && !ready) ...[
              Text(bt('backup.preview.mode_required'), style: Theme.of(context).textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant)),
              const SizedBox(height: Space.sm),
            ],
            Wrap(alignment: WrapAlignment.end, spacing: Space.sm, runSpacing: Space.sm, children: _waiting
                ? [
                    OutlinedButton(onPressed: _deciding ? null : () => _decide(false), child: Text(bt('backup.preview.cancel_run'))),
                    FilledButton(onPressed: _deciding || !ready ? null : () => _decide(true), child: Text(bt('backup.preview.continue'))),
                  ]
                : [
                    FilledButton.icon(onPressed: _deciding ? null : _runNow, icon: const Icon(Icons.play_arrow_outlined), label: Text(bt('backup.preview.run_now'))),
                  ]),
          ]),
        ),
      ),
    );
  }
}
