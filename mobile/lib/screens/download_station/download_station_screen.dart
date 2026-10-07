/// Download Station (plan WP1-2): the server's downloads with live
/// progress, filters and search, pause / resume / retry / remove, adding
/// links, and its simple settings. Wording follows the web app
/// (ui/src/apps/download-station). Polls at the Refresh widgets pace while
/// on screen.
library;

import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../backup/backup_strings.dart' show formatSeconds;
import '../../models/file_entry.dart';
import '../../services/api_client.dart';
import '../../services/download_station_api.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import '../backup/backup_widgets.dart' show BackupPolling;
import '../file_viewer_screen.dart';
import '../files_screen.dart';
import 'ds_add_sheet.dart';
import 'ds_settings_screen.dart';
import 'torrents_screen.dart';

enum DsFilter { all, active, completed, failed }

extension on DsFilter {
  String get label => switch (this) {
        DsFilter.all => 'All',
        DsFilter.active => 'Active',
        DsFilter.completed => 'Completed',
        DsFilter.failed => 'Failed',
      };

  bool matches(DsDownload d) => switch (this) {
        DsFilter.all => true,
        DsFilter.active => d.state != DsState.completed && d.state != DsState.failed,
        DsFilter.completed => d.state == DsState.completed,
        DsFilter.failed => d.state == DsState.failed,
      };
}

/// "45.2%", "Queued", "Paused" - the web's state badge.
String dsStateLabel(DsDownload d) => switch (d.state) {
      DsState.downloading => d.sizeKnown ? '${((d.fraction ?? 0) * 1000).floor() / 10}%' : 'Downloading',
      DsState.queued => 'Queued',
      DsState.paused => 'Paused',
      DsState.completed => 'Completed',
      DsState.failed => 'Failed',
    };

/// "12 MB / 1.4 GB · 3.1 MB/s · 7 min left"; a failed one's reason.
String dsMetaLine(DsDownload d) {
  if (d.state == DsState.failed) return d.error.isEmpty ? 'Failed' : d.error;
  final size = d.state == DsState.completed
      ? formatSize(d.size)
      : d.sizeKnown
          ? '${formatSize(d.downloaded)} / ${formatSize(d.size)}'
          : (d.downloaded > 0 ? formatSize(d.downloaded) : 'Size unknown');
  return [
    size,
    if (d.state == DsState.downloading && d.speed > 0) formatSpeed(d.speed),
    if (d.state == DsState.downloading && d.eta >= 0) '${formatSeconds(d.eta)} left',
    if (d.state == DsState.completed) d.dir,
  ].join(' · ');
}

Status _status(DsState s) => switch (s) {
      DsState.downloading => Status.info,
      DsState.queued || DsState.paused => Status.neutral,
      DsState.completed => Status.success,
      DsState.failed => Status.error,
    };

class DownloadStationScreen extends StatefulWidget {
  const DownloadStationScreen({super.key, this.api, this.initialFilter = DsFilter.all});

  final DownloadStationApi? api;
  final DsFilter initialFilter;

  @override
  State<DownloadStationScreen> createState() => _DownloadStationScreenState();
}

class _DownloadStationScreenState extends State<DownloadStationScreen> with WidgetsBindingObserver, BackupPolling {
  late final DownloadStationApi _api = widget.api ?? DownloadStationApi();
  late DsFilter _filter = widget.initialFilter;
  final _search = TextEditingController();
  List<DsDownload>? _downloads;
  Object? _error;
  DateTime? _loadedAt;
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
    _search.dispose();
    super.dispose();
  }

  @override
  Future<void> poll() => _load();

  Future<void> _load() async {
    if (_loading) return;
    _loading = true;
    try {
      final list = await _api.downloads();
      if (!mounted) return;
      setState(() {
        _downloads = list;
        _error = null;
        _loadedAt = clock.now();
      });
    } catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      _loading = false;
    }
  }

  void _snack(String text) => ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(text)));

  Future<void> _run(Future<void> Function() action, String failure) async {
    try {
      await action();
    } catch (e) {
      _snack('$failure: $e');
    }
    await _load();
  }

  Future<void> _add() async {
    final added = await showAddDownloadSheet(context, api: _api);
    if (added != null && mounted) await _load();
  }

  Future<void> _remove(DsDownload d) async {
    final done = d.state == DsState.completed;
    if (!done) {
      final ok = await ConfirmDialog.destructive(context,
          title: 'Remove download', message: 'Cancel and remove ${d.filename}? The partially downloaded data is deleted', confirmLabel: 'Remove');
      if (ok) await _run(() => _api.remove(d.id), "Couldn't remove ${d.filename}");
      return;
    }
    final deleteFile = await showDialog<bool>(
      context: context,
      builder: (context) => _RemoveDialog(name: d.filename),
    );
    if (deleteFile == null) return;
    await _run(() => _api.remove(d.id, deleteFile: deleteFile), "Couldn't remove ${d.filename}");
  }

  void _openFile(DsDownload d) => Navigator.of(context).push(MaterialPageRoute<void>(
        builder: (_) => FileViewerScreen(file: FileEntry(name: d.filename, path: d.path, isDir: false, size: d.size), path: d.path),
      ));

  void _showInFiles(DsDownload d) => Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => FilesScreen(initialPath: d.dir)));

  Future<void> _actions(DsDownload d) async {
    final scheme = Theme.of(context).colorScheme;
    Widget tile(String id, IconData icon, String label, {bool danger = false}) => ListTile(
          leading: Icon(icon, color: danger ? scheme.error : null),
          title: Text(label, style: danger ? TextStyle(color: scheme.error) : null),
          onTap: () => Navigator.of(context).pop(id),
        );
    final pick = await showModalBottomSheet<String>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (context) => SafeArea(
        child: SingleChildScrollView(
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            ListTile(
              title: Text(d.filename, maxLines: 2, overflow: TextOverflow.ellipsis, style: Theme.of(context).textTheme.titleMedium),
              subtitle: Text(d.url, maxLines: 2, overflow: TextOverflow.ellipsis),
            ),
            const Divider(indent: Space.xl, endIndent: Space.xl),
            if (d.isRunning) tile('pause', Icons.pause, 'Pause'),
            if (d.state == DsState.paused) tile('resume', Icons.play_arrow_outlined, 'Resume'),
            if (d.state == DsState.failed) tile('resume', Icons.restart_alt, 'Retry'),
            if (d.state == DsState.completed) tile('open', Icons.open_in_new_outlined, 'Open'),
            if (d.state == DsState.completed) tile('files', Icons.folder_open_outlined, 'Show in Files'),
            tile('redownload', Icons.replay, 'Download again'),
            tile('copy', Icons.link, 'Copy link'),
            tile('remove', Icons.close, 'Remove', danger: true),
            const SizedBox(height: Space.sm),
          ]),
        ),
      ),
    );
    if (pick == null || !mounted) return;
    switch (pick) {
      case 'pause':
        await _run(() => _api.pause(d.id), "Couldn't pause ${d.filename}");
      case 'resume':
        await _run(() => _api.resume(d.id), "Couldn't resume ${d.filename}");
      case 'redownload':
        await _run(() => _api.redownload(d.id), "Couldn't restart ${d.filename}");
      case 'open':
        _openFile(d);
      case 'files':
        _showInFiles(d);
      case 'copy':
        await Clipboard.setData(ClipboardData(text: d.url));
        _snack('Link copied');
      case 'remove':
        await _remove(d);
    }
  }

  void _openTorrents() => Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => TorrentsScreen(api: _api)));

  void _openSettings() => Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => DsSettingsScreen(api: _api)));

  @override
  Widget build(BuildContext context) {
    final list = _downloads;
    final hasActive = list?.any((d) => d.isRunning) ?? false;
    final hasResumable = list?.any((d) => d.state == DsState.paused || d.state == DsState.failed) ?? false;
    final hasCompleted = list?.any((d) => d.state == DsState.completed) ?? false;
    return AppScaffold.slivers(
      title: 'Download Station',
      onRefresh: _load,
      banner: _error != null && list != null ? OfflineBanner(lastUpdated: _loadedAt, onRetry: _load) : null,
      actions: [
        if (hasActive)
          IconButton(icon: const Icon(Icons.pause), tooltip: 'Pause all', onPressed: () => _run(_api.pauseAll, "Couldn't pause the downloads"))
        else if (hasResumable)
          IconButton(icon: const Icon(Icons.play_arrow_outlined), tooltip: 'Resume all', onPressed: () => _run(_api.resumeAll, "Couldn't resume the downloads")),
        PopupMenuButton<String>(
          tooltip: 'More',
          onSelected: (v) async {
            if (v == 'clear') await _run(_api.clearCompleted, "Couldn't clear the completed downloads");
            if (v == 'settings') _openSettings();
            if (v == 'torrents') _openTorrents();
          },
          itemBuilder: (_) => [
            const PopupMenuItem(value: 'torrents', child: Text('Torrents')),
            PopupMenuItem(value: 'clear', enabled: hasCompleted, child: const Text('Clear completed from list')),
            const PopupMenuItem(value: 'settings', child: Text('Settings')),
          ],
        ),
      ],
      floatingActionButton: list == null ? null : FloatingActionButton.extended(onPressed: _add, icon: const Icon(Icons.add), label: const Text('Add download')),
      slivers: _body(list),
    );
  }

  List<Widget> _body(List<DsDownload>? list) {
    if (list == null) {
      final e = _error;
      if (e == null) return const [SliverLoadingList(rows: 6, trailing: true)];
      if (DownloadStationApi.isUnavailable(e)) {
        return const [
          EmptyState(
            sliver: true,
            icon: Icons.download_outlined,
            title: "Download Station isn't available",
            message: "This server doesn't run Download Station, or it isn't answering. Install it from the App Store in the web dashboard.",
          ),
        ];
      }
      return [
        e is ApiException && e.isUnreachable
            ? ErrorState.offline(onRetry: _load, sliver: true)
            : ErrorState(title: "Couldn't load the downloads", message: e.toString(), onRetry: _load, details: 'GET ${DownloadStationApi.base}/downloads: $e', sliver: true),
      ];
    }
    if (list.isEmpty) {
      return [
        EmptyState(
          sliver: true,
          icon: Icons.download_outlined,
          title: 'No downloads yet',
          message: 'Add a link here, or share one to NivaroOS from your browser.',
          actionLabel: 'Add download',
          onAction: _add,
        ),
      ];
    }
    final q = _search.text.trim().toLowerCase();
    final counts = {for (final f in DsFilter.values) f: list.where(f.matches).length};
    final visible = list.where((d) => _filter.matches(d) && (q.isEmpty || d.filename.toLowerCase().contains(q) || d.url.toLowerCase().contains(q))).toList();
    final gutter = Space.gutter(context);
    return [
      SliverToBoxAdapter(
        child: Padding(
          padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.sm),
          child: TextField(
            controller: _search,
            onChanged: (_) => setState(() {}),
            decoration: InputDecoration(
              prefixIcon: const Icon(Icons.search),
              hintText: 'Search downloads',
              isDense: true,
              suffixIcon: q.isEmpty ? null : IconButton(icon: const Icon(Icons.close), tooltip: 'Clear search', onPressed: () => setState(_search.clear)),
            ),
          ),
        ),
      ),
      SliverToBoxAdapter(
        child: SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          padding: EdgeInsets.symmetric(horizontal: gutter),
          child: Row(children: [
            for (final f in DsFilter.values)
              Padding(
                padding: const EdgeInsetsDirectional.only(end: Space.sm),
                child: ChoiceChip(
                  label: Text(counts[f]! > 0 ? '${f.label} ${counts[f]}' : f.label),
                  selected: _filter == f,
                  onSelected: (_) => setState(() => _filter = f),
                ),
              ),
          ]),
        ),
      ),
      const SliverToBoxAdapter(child: SizedBox(height: Space.md)),
      if (visible.isEmpty)
        const SliverToBoxAdapter(child: Padding(padding: EdgeInsets.all(Space.xl), child: Center(child: Text('Nothing matches this filter.'))))
      else
        SliverList.list(children: [
          TileGroup(children: [
            for (final d in visible) _DownloadTile(download: d, onTap: () => _actions(d), onAction: () => _primary(d)),
          ]),
          const SizedBox(height: 88),
        ]),
    ];
  }

  void _primary(DsDownload d) {
    switch (d.state) {
      case DsState.downloading || DsState.queued:
        _run(() => _api.pause(d.id), "Couldn't pause ${d.filename}");
      case DsState.paused || DsState.failed:
        _run(() => _api.resume(d.id), "Couldn't resume ${d.filename}");
      case DsState.completed:
        _openFile(d);
    }
  }
}

class _DownloadTile extends StatelessWidget {
  const _DownloadTile({required this.download, required this.onTap, required this.onAction});

  final DsDownload download;
  final VoidCallback onTap;
  final VoidCallback onAction;

  @override
  Widget build(BuildContext context) {
    final d = download;
    final theme = Theme.of(context);
    final status = _status(d.state);
    final tone = StatusColors.toneOf(context, status);
    final (IconData icon, String tip) = switch (d.state) {
      DsState.downloading || DsState.queued => (Icons.pause, 'Pause ${d.filename}'),
      DsState.paused => (Icons.play_arrow_outlined, 'Resume ${d.filename}'),
      DsState.failed => (Icons.restart_alt, 'Retry ${d.filename}'),
      DsState.completed => (Icons.open_in_new_outlined, 'Open ${d.filename}'),
    };
    final muted = theme.textTheme.bodySmall?.copyWith(color: d.state == DsState.failed ? theme.colorScheme.error : theme.colorScheme.onSurfaceVariant);
    return ListTile(
      onTap: onTap,
      title: Text(d.filename, maxLines: 2, overflow: TextOverflow.ellipsis),
      subtitle: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          const SizedBox(height: Space.xs),
          Wrap(spacing: Space.sm, runSpacing: Space.xs, crossAxisAlignment: WrapCrossAlignment.center, children: [
            StatusChip(label: dsStateLabel(d), status: status),
          ]),
          if (d.state != DsState.completed) ...[
            const SizedBox(height: Space.sm),
            LinearProgressIndicator(
              value: d.state == DsState.downloading ? d.fraction : (d.fraction ?? 0),
              color: d.state == DsState.downloading ? null : tone.color,
            ),
          ],
          const SizedBox(height: Space.xs),
          Text(dsMetaLine(d), maxLines: 2, overflow: TextOverflow.ellipsis, style: muted?.tabular),
        ],
      ),
      trailing: IconButton(icon: Icon(icon), tooltip: tip, onPressed: onAction),
    );
  }
}

/// Removing a finished download: off the list, and the file too if asked
/// (the web's checkbox). Returns whether to delete the file; null = cancel.
class _RemoveDialog extends StatefulWidget {
  const _RemoveDialog({required this.name});
  final String name;

  @override
  State<_RemoveDialog> createState() => _RemoveDialogState();
}

class _RemoveDialogState extends State<_RemoveDialog> {
  bool _deleteFile = false;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return AlertDialog(
      title: const Text('Remove download'),
      content: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text('Remove ${widget.name} from the list?'),
        const SizedBox(height: Space.md),
        CheckboxListTile(
          contentPadding: EdgeInsets.zero,
          value: _deleteFile,
          onChanged: (v) => setState(() => _deleteFile = v ?? false),
          title: const Text('Also delete the downloaded file'),
          subtitle: Text(_deleteFile ? 'The file is permanently deleted from disk.' : 'The downloaded file is kept.'),
        ),
      ]),
      actions: [
        TextButton(autofocus: true, onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton(
          style: FilledButton.styleFrom(backgroundColor: scheme.error, foregroundColor: scheme.onError, side: BorderSide.none),
          onPressed: () => Navigator.of(context).pop(_deleteFile),
          child: const Text('Remove'),
        ),
      ],
    );
  }
}
