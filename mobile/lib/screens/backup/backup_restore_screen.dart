import 'package:clock/clock.dart';
import 'package:flutter/material.dart';

import '../../backup/backup_api.dart';
import '../../backup/backup_models.dart';
import '../../backup/backup_strings.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import 'backup_folder_picker.dart';
import 'backup_run_screen.dart';
import 'backup_widgets.dart';

/// Restore (the web's Restore section, browse window and restore window,
/// on one page): which job, from when (the current copy, a recycle folder
/// or an archive), what (everything, or files picked by browsing the
/// version), where to (back where they came from, or another folder),
/// what to do when a file is already there - and, as the API allows, a
/// check first that changes nothing. It starts a restore run and opens it.
class BackupRestoreScreen extends StatefulWidget {
  const BackupRestoreScreen({super.key, required this.jobs, this.job, this.versionId, this.api});

  /// The jobs to choose from (the overview passes all of them).
  final List<BackupJob> jobs;

  /// The job to start with.
  final BackupJob? job;

  /// The version to start with (a job's Versions list).
  final String? versionId;
  final BackupApi? api;

  @override
  State<BackupRestoreScreen> createState() => BackupRestoreScreenState();
}

@visibleForTesting
class BackupRestoreScreenState extends State<BackupRestoreScreen> {
  late final BackupApi _api = widget.api ?? BackupApi();

  BackupJob? _job;
  List<BackupVersion>? _versions;
  Object? _versionsError;
  String? _versionId;
  List<String> _paths = const [];
  String _mode = 'original';
  BackupEndpoint? _other;
  String _conflict = 'keep_both';
  bool _dryRun = false;
  bool _submitting = false;
  BackupError? _error;

  static const conflicts = ['keep_both', 'overwrite', 'skip'];

  @override
  void initState() {
    super.initState();
    final jobs = [...widget.jobs]..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
    _job = widget.job ?? (jobs.isEmpty ? null : jobs.first);
    _versionId = widget.versionId;
    _loadVersions();
  }

  Future<void> _loadVersions() async {
    final job = _job;
    if (job == null) return;
    setState(() {
      _versions = null;
      _versionsError = null;
    });
    try {
      final v = await _api.versions(job.id);
      if (!mounted || _job?.id != job.id) return;
      setState(() {
        _versions = v;
        if (_versionId == null || !v.any((x) => x.id == _versionId)) _versionId = v.isEmpty ? null : v.first.id;
      });
    } catch (e) {
      if (mounted) setState(() => _versionsError = e);
    }
  }

  Future<void> _pickJob() async {
    final jobs = [...widget.jobs]..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
    final picked = await showDialog<BackupJob>(
      context: context,
      builder: (context) => SimpleDialog(
        title: Text(bt('backup.restore.step_job')),
        children: [
          RadioGroup<String>(
            groupValue: _job?.id,
            onChanged: (id) => Navigator.of(context).pop(jobs.firstWhere((j) => j.id == id)),
            child: Column(mainAxisSize: MainAxisSize.min, children: [
              for (final j in jobs) RadioListTile<String>(value: j.id, title: Text(j.name), subtitle: Text(j.dest.display)),
            ]),
          ),
        ],
      ),
    );
    if (picked == null || picked.id == _job?.id) return;
    setState(() {
      _job = picked;
      _versionId = null;
      _paths = const [];
      _error = null;
    });
    await _loadVersions();
  }

  BackupVersion? get _version => _versions?.where((v) => v.id == _versionId).firstOrNull;

  Future<void> _browse() async {
    final job = _job, version = _version;
    if (job == null || version == null) return;
    final picked = await Navigator.of(context).push<List<String>>(MaterialPageRoute(
      builder: (_) => BackupBrowseScreen(job: job, version: version, selected: _paths, api: widget.api),
    ));
    if (picked != null && mounted) setState(() => _paths = picked);
  }

  Future<void> _chooseFolder() async {
    final picked = await pickBackupFolder(context, role: 'dest', api: widget.api, title: bt('backup.restore.choose_folder').replaceAll('…', ''));
    if (picked != null && mounted) {
      setState(() {
        _other = picked;
        _mode = 'other';
        _error = null;
      });
    }
  }

  /// Why Restore can't start yet; null when it can.
  @visibleForTesting
  String? get blocker {
    if (_job == null) return bt('backup.restore.no_jobs');
    if (_versionId == null) return bt('backup.restore.no_versions');
    if (_mode == 'other' && _other == null) return bt('backup.restore.no_folder_yet');
    return null;
  }

  Future<void> _submit() async {
    final job = _job, versionId = _versionId;
    if (_submitting || blocker != null || job == null || versionId == null) return;
    setState(() {
      _submitting = true;
      _error = null;
    });
    try {
      final runId = await _api.restore(job.id,
          versionId: versionId, paths: _paths, target: _mode == 'other' ? _other : null, conflict: _conflict, dryRun: _dryRun);
      if (!mounted) return;
      await Navigator.of(context).pushReplacement(MaterialPageRoute<void>(builder: (_) => BackupRunScreen(runId: runId, api: widget.api)));
    } catch (e) {
      if (mounted) setState(() => _error = BackupError.from(e));
    } finally {
      if (mounted) setState(() => _submitting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    final job = _job;
    if (job == null) {
      return AppScaffold(
        title: bt('backup.nav.restore'),
        body: EmptyState(icon: Icons.restore_outlined, title: bt('backup.nav.restore'), message: bt('backup.restore.no_jobs')),
      );
    }
    final versions = _versions;
    final version = _version;
    final targetError = _error?.fieldError('target');
    final blocked = blocker;
    // "photo (restored 2026-09-30).jpg": the date the engine uses.
    final now = clock.now();
    final today = '${now.year}-${now.month.toString().padLeft(2, '0')}-${now.day.toString().padLeft(2, '0')}';

    return AppScaffold(
      title: bt('backup.nav.restore'),
      body: ListView(
        padding: EdgeInsets.only(bottom: Space.xl + MediaQuery.paddingOf(context).bottom),
        children: [
          if (widget.jobs.length > 1 || widget.job == null)
            TileGroup(title: bt('backup.restore.step_job'), children: [
              ListTile(
                leading: const Icon(Icons.backup_outlined),
                title: Text(job.name),
                subtitle: Text(job.dest.display),
                trailing: widget.jobs.length > 1 ? const Icon(Icons.unfold_more) : null,
                onTap: widget.jobs.length > 1 ? _pickJob : null,
              ),
            ]),
          TileGroup(
            title: bt('backup.restore.step_when'),
            footer: versions != null && versions.isEmpty
                ? bt('backup.restore.no_versions')
                : job.type == 'copy'
                    ? bt('backup.restore.copy_note')
                    : null,
            children: [
              if (versions == null && _versionsError == null) ...SkeletonRow.group(rows: 2),
              if (_versionsError != null)
                ListTile(
                  leading: Icon(Icons.error_outline, color: scheme.error),
                  title: Text(BackupError.from(_versionsError!).title),
                  trailing: TextButton(onPressed: _loadVersions, child: Text(bt('backup.retry'))),
                ),
              if (versions != null && versions.isNotEmpty)
                RadioGroup<String>(
                  groupValue: _versionId,
                  onChanged: (v) => setState(() {
                    _versionId = v;
                    _paths = const [];
                  }),
                  child: Column(mainAxisSize: MainAxisSize.min, children: [
                    for (final v in versions)
                      RadioListTile<String>(
                        value: v.id,
                        title: Text(v.label(currentAt: job.lastSuccess)),
                        subtitle: v.meta.isEmpty ? null : Text(v.meta),
                      ),
                  ]),
                ),
            ],
          ),
          TileGroup(title: bt('backup.restore.what'), footer: bt('backup.restore.browse_hint'), children: [
            ListTile(
              leading: Icon(_paths.isEmpty ? Icons.select_all_outlined : Icons.checklist_outlined),
              title: Text(_paths.isEmpty ? bt('backup.restore.what_all') : btc('backup.restore.what_n', _paths.length)),
              subtitle: _paths.isEmpty ? null : Text(_paths.take(5).join('\n') + (_paths.length > 5 ? '\n…' : '')),
              trailing: Text(bt('backup.restore.browse')),
              enabled: version != null,
              onTap: version == null ? null : _browse,
            ),
          ]),
          TileGroup(title: bt('backup.restore.where'), children: [
            RadioGroup<String>(
              groupValue: _mode,
              onChanged: (m) => setState(() {
                _mode = m ?? 'original';
                _error = null;
              }),
              child: Column(mainAxisSize: MainAxisSize.min, children: [
                RadioListTile<String>(
                  value: 'original',
                  title: Text(bt('backup.restore.to_original')),
                  subtitle: Text(formatList([for (final s in job.sources) s.display])),
                ),
                RadioListTile<String>(
                  value: 'other',
                  title: Text(bt('backup.restore.to_other')),
                  subtitle: Text(
                    targetError ?? (_other?.display ?? bt('backup.restore.no_folder_yet')),
                    style: targetError != null || (_mode == 'other' && _other == null) ? TextStyle(color: scheme.error) : null,
                  ),
                ),
              ]),
            ),
            if (_mode == 'other')
              ListTile(
                leading: const Icon(Icons.folder_open_outlined),
                title: Text(_other == null ? bt('backup.restore.choose_folder') : bt('backup.restore.change_folder')),
                onTap: _chooseFolder,
              ),
          ]),
          TileGroup(title: bt('backup.restore.conflict'), children: [
            RadioGroup<String>(
              groupValue: _conflict,
              onChanged: (c) => setState(() => _conflict = c ?? 'keep_both'),
              child: Column(mainAxisSize: MainAxisSize.min, children: [
                for (final c in conflicts)
                  RadioListTile<String>(
                    value: c,
                    title: Text(bt('backup.restore.conflict_$c')),
                    subtitle: Text(bt('backup.restore.conflict_${c}_hint', {'date': today})),
                  ),
              ]),
            ),
          ]),
          if (_conflict == 'overwrite')
            Notice(status: Status.warning, message: bt('backup.restore.overwrite_warning')),
          TileGroup(children: [
            SwitchListTile(
              value: _dryRun,
              onChanged: (v) => setState(() => _dryRun = v),
              title: const Text('Check first'),
              subtitle: const Text('Shows what the restore would do, without changing any file.'),
            ),
          ]),
          if (_error != null && targetError == null)
            Notice(status: Status.error, title: _error!.title, message: _error!.fix.isNotEmpty ? _error!.fix : _error!.causeText),
          Padding(
            padding: EdgeInsets.fromLTRB(gutter, Space.lg, gutter, 0),
            child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
              FilledButton.icon(
                onPressed: _submitting || blocked != null ? null : _submit,
                icon: const Icon(Icons.restore_outlined),
                label: Text(_submitting ? bt('backup.restore.starting') : (_dryRun ? 'Check the restore' : bt('backup.restore.start'))),
              ),
              if (blocked != null && versions != null) ...[
                const SizedBox(height: Space.sm),
                Text(blocked, style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant), textAlign: TextAlign.center),
              ],
            ]),
          ),
        ],
      ),
    );
  }
}

/// Browses one version of a job, read-only, and picks what to restore:
/// folders open, boxes tick, and Done hands the picked paths back.
class BackupBrowseScreen extends StatefulWidget {
  const BackupBrowseScreen({super.key, required this.job, required this.version, this.selected = const [], this.api});

  final BackupJob job;
  final BackupVersion version;
  final List<String> selected;
  final BackupApi? api;

  @override
  State<BackupBrowseScreen> createState() => _BackupBrowseScreenState();
}

class _BackupBrowseScreenState extends State<BackupBrowseScreen> {
  late final BackupApi _api = widget.api ?? BackupApi();
  String _path = '';
  BrowseResult? _result;
  Object? _error;
  late final Set<String> _selected = {...widget.selected};

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final path = _path;
    setState(() {
      _result = null;
      _error = null;
    });
    try {
      final r = await _api.browseVersion(widget.job.id, widget.version.id, path);
      if (mounted && path == _path) setState(() => _result = r);
    } catch (e) {
      if (mounted && path == _path) setState(() => _error = e);
    }
  }

  void _go(String path) {
    _path = path;
    _load();
  }

  String _join(String name) => _path.isEmpty ? name : '$_path/$name';

  String get _parent {
    final parts = _path.split('/').where((p) => p.isNotEmpty).toList();
    if (parts.isNotEmpty) parts.removeLast();
    return parts.join('/');
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final r = _result;
    final entries = r?.entries ?? const <BrowseEntry>[];
    final all = entries.isNotEmpty && entries.every((e) => _selected.contains(_join(e.name)));
    final Widget body;
    if (r == null && _error != null) {
      body = backupErrorState(_error!, title: "Couldn't open this folder", onRetry: _load);
    } else if (r == null) {
      body = const LoadingList(rows: 8);
    } else if (entries.isEmpty) {
      body = EmptyState(icon: Icons.folder_open_outlined, title: bt('backup.browse.empty'), message: '');
    } else {
      body = ListView(
        padding: EdgeInsets.only(bottom: Space.xl + MediaQuery.paddingOf(context).bottom),
        children: [
          CheckboxListTile(
            value: all ? true : (entries.any((e) => _selected.contains(_join(e.name))) ? null : false),
            tristate: true,
            title: Text(bt('backup.browse.select_all')),
            onChanged: (_) => setState(() {
              for (final e in entries) {
                all ? _selected.remove(_join(e.name)) : _selected.add(_join(e.name));
              }
            }),
          ),
          const Divider(height: 1),
          for (final e in entries)
            ListTile(
              leading: Checkbox(
                value: _selected.contains(_join(e.name)),
                semanticLabel: bt('backup.browse.select_named', {'name': e.name}),
                onChanged: (v) => setState(() => v == true ? _selected.add(_join(e.name)) : _selected.remove(_join(e.name))),
              ),
              title: Text(e.name),
              subtitle: Text([
                if (!e.dir) formatSize(e.size),
                if (e.mtime != null) formatWhen(e.mtime!),
              ].join(' · ')),
              trailing: e.dir ? Icon(Icons.folder_outlined, color: scheme.primary) : const Icon(Icons.insert_drive_file_outlined),
              onTap: e.dir
                  ? () => _go(_join(e.name))
                  : () => setState(() => _selected.contains(_join(e.name)) ? _selected.remove(_join(e.name)) : _selected.add(_join(e.name))),
            ),
          if (r.truncated) Padding(padding: const EdgeInsets.all(Space.lg), child: Text(bt('backup.browse.truncated'))),
        ],
      );
    }
    final count = _selected.length;
    return PopScope(
      canPop: _path.isEmpty,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _go(_parent);
      },
      child: AppScaffold(
        title: _path.isEmpty ? widget.version.label(currentAt: widget.job.lastSuccess) : _path.split('/').last,
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(40),
          child: Align(
            alignment: AlignmentDirectional.centerStart,
            child: Padding(
              padding: EdgeInsets.fromLTRB(Space.gutter(context), 0, Space.gutter(context), Space.sm),
              child: Text(
                [
                  _path.isEmpty ? widget.job.name : '${widget.job.name} › ${_path.replaceAll('/', ' › ')}',
                  count == 0 ? bt('backup.browse.none_selected') : btc('backup.browse.n_selected', count),
                ].join(' · '),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
              ),
            ),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(_selected.toList()..sort()),
            child: const Text('Done'),
          ),
        ],
        body: body,
      ),
    );
  }
}
