import 'package:flutter/material.dart';

import '../../backup/backup_strings.dart' show formatSeconds;
import '../../services/download_station_api.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import '../backup/backup_widgets.dart' show BackupPolling;
import '../files_screen.dart';
import 'torrents_screen.dart' show torrentStateLabel, torrentStatus, formatRatio;

const _priorities = {1: 'Normal', 6: 'High', 7: 'Maximum'};

const trackerStatusLabels = {
  'working': 'Working',
  'updating': 'Updating',
  'not_working': 'Not working',
  'not_contacted': 'Not contacted yet',
  'disabled': 'Disabled',
};

/// One torrent: its numbers, files (pick which to download and their
/// priority), trackers, sequential / first-last piece, and pause /
/// resume / recheck / remove - the web's torrent properties window.
class TorrentDetailScreen extends StatefulWidget {
  const TorrentDetailScreen({super.key, required this.hash, this.name = '', this.api});

  final String hash;
  final String name;
  final DownloadStationApi? api;

  @override
  State<TorrentDetailScreen> createState() => _TorrentDetailScreenState();
}

class _TorrentDetailScreenState extends State<TorrentDetailScreen> with WidgetsBindingObserver, BackupPolling {
  late final DownloadStationApi _api = widget.api ?? DownloadStationApi();
  DsTorrentDetail? _d;
  Object? _error;
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
  Future<void> poll() => _load();

  Future<void> _load() async {
    if (_loading) return;
    _loading = true;
    try {
      final d = await _api.torrent(widget.hash);
      if (mounted) {
        setState(() {
          _d = d;
          _error = null;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      _loading = false;
    }
  }

  Future<void> _run(Future<void> Function() action, String failure) async {
    try {
      await action();
    } catch (e) {
      if (mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text('$failure: $e')));
    }
    await _load();
  }

  Future<void> _remove(DsTorrent t) async {
    final deleteFiles = await showDialog<bool>(context: context, builder: (_) => RemoveTorrentDialog(name: t.name));
    if (deleteFiles == null || !mounted) return;
    try {
      await _api.removeTorrent(t.hash, deleteFiles: deleteFiles);
      if (mounted) Navigator.of(context).pop();
    } catch (e) {
      if (mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text("Couldn't remove ${t.name}: $e")));
    }
  }

  @override
  Widget build(BuildContext context) {
    final d = _d;
    final Widget body;
    if (d == null) {
      body = _error == null ? const LoadingList(rows: 6) : ErrorState(title: "Couldn't load the torrent", message: _error.toString(), onRetry: _load);
    } else {
      body = _content(d);
    }
    return AppScaffold(
      title: d?.torrent.name ?? (widget.name.isEmpty ? 'Torrent' : widget.name),
      onRefresh: _load,
      actions: [
        if (d != null)
          PopupMenuButton<String>(
            tooltip: 'More',
            onSelected: (v) {
              if (v == 'recheck') _run(() => _api.torrentAction(d.torrent.hash, 'recheck'), "Couldn't recheck");
              if (v == 'files' && d.torrent.dir.isNotEmpty) Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => FilesScreen(initialPath: d.torrent.dir)));
              if (v == 'remove') _remove(d.torrent);
            },
            itemBuilder: (_) => [
              PopupMenuItem(value: 'recheck', enabled: d.torrent.hasMetadata, child: const Text('Recheck')),
              PopupMenuItem(value: 'files', enabled: d.torrent.dir.isNotEmpty, child: const Text('Show in Files')),
              const PopupMenuItem(value: 'remove', child: Text('Remove')),
            ],
          ),
      ],
      body: body,
    );
  }

  Widget _content(DsTorrentDetail d) {
    final t = d.torrent;
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    Widget fact(String label, String value) => ListTile(
          dense: true,
          title: Text(label),
          trailing: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 220),
            child: Text(value, textAlign: TextAlign.end, maxLines: 2, overflow: TextOverflow.ellipsis, style: theme.textTheme.bodyMedium?.tabular),
          ),
        );
    return ListView(padding: const EdgeInsets.only(bottom: Space.xl), children: [
      Padding(
        padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.md),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Wrap(spacing: Space.sm, runSpacing: Space.xs, children: [
            StatusChip(label: torrentStateLabel(t), status: torrentStatus(t.state)),
            if (t.private == true) const StatusChip(label: 'Private', status: Status.warning, icon: Icons.lock_outline),
          ]),
          const SizedBox(height: Space.md),
          LinearProgressIndicator(value: t.state == 'metadata' ? null : t.progress),
          const SizedBox(height: Space.xs),
          Text('${(t.progress * 1000).floor() / 10}% of ${formatSize(t.size)}', style: theme.textTheme.bodySmall?.tabular),
          if (t.error.isNotEmpty) ...[const SizedBox(height: Space.sm), Text(t.error, style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.error))],
          const SizedBox(height: Space.md),
          Row(children: [
            Expanded(
              child: t.isPaused
                  ? FilledButton.icon(onPressed: () => _run(() => _api.torrentAction(t.hash, 'resume'), "Couldn't resume"), icon: const Icon(Icons.play_arrow), label: const Text('Resume'))
                  : OutlinedButton.icon(onPressed: () => _run(() => _api.torrentAction(t.hash, 'pause'), "Couldn't pause"), icon: const Icon(Icons.pause), label: const Text('Pause')),
            ),
          ]),
        ]),
      ),
      TileGroup(title: 'Transfer', children: [
        fact('Download speed', t.dlSpeed > 0 ? formatSpeed(t.dlSpeed) : '—'),
        fact('Upload speed', t.upSpeed > 0 ? formatSpeed(t.upSpeed) : '—'),
        fact('Downloaded / uploaded', '${formatSize(t.downloaded)} / ${formatSize(t.uploaded)}'),
        fact('Seeds', '${t.seeds} (${t.seedsTotal})'),
        fact('Peers', '${t.peers} (${t.peersTotal})'),
        fact('Share ratio', formatRatio(t.ratio)),
        fact('Time left', t.eta >= 0 && !t.isFinished ? formatSeconds(t.eta) : '—'),
        if (t.seedingTime > 0) fact('Seeding time', formatSeconds(t.seedingTime)),
        if (t.category.isNotEmpty) fact('Category', t.category),
        fact('Save folder', t.dir.isEmpty ? '—' : t.dir),
      ]),
      TileGroup(title: 'Options', children: [
        SwitchListTile(
          title: const Text('Download in sequential order'),
          subtitle: const Text('Pieces in order, so a video can be watched while it downloads'),
          value: t.sequential,
          onChanged: (v) => _run(() => _api.setTorrentOptions(t.hash, sequential: v, firstLast: t.firstLast), "Couldn't change it"),
        ),
        SwitchListTile(
          title: const Text('First and last pieces first'),
          value: t.firstLast,
          onChanged: (v) => _run(() => _api.setTorrentOptions(t.hash, sequential: t.sequential, firstLast: v), "Couldn't change it"),
        ),
      ]),
      TileGroup(
        title: 'Files',
        footer: d.files.isEmpty ? 'The file list appears once the metadata has arrived.' : null,
        children: [
          for (final f in d.files)
            CheckboxListTile(
              controlAffinity: ListTileControlAffinity.leading,
              value: f.priority > 0,
              onChanged: (v) => _run(() => _api.setTorrentFiles(t.hash, [f.index], v == true ? 1 : 0), "Couldn't change ${f.name}"),
              title: Text(f.name, maxLines: 2, overflow: TextOverflow.ellipsis),
              subtitle: Text('${formatSize(f.size)} · ${(f.progress * 1000).floor() / 10}%', style: theme.textTheme.bodySmall?.tabular),
              secondary: f.priority == 0
                  ? null
                  : PopupMenuButton<int>(
                      tooltip: 'Priority of ${f.name}',
                      initialValue: f.priority,
                      onSelected: (p) => _run(() => _api.setTorrentFiles(t.hash, [f.index], p), "Couldn't change ${f.name}"),
                      itemBuilder: (_) => [for (final e in _priorities.entries) PopupMenuItem(value: e.key, child: Text(e.value))],
                      child: Padding(
                        padding: const EdgeInsets.all(Space.sm),
                        child: Text(_priorities[f.priority] ?? 'Normal', style: theme.textTheme.labelLarge?.copyWith(color: theme.colorScheme.primary)),
                      ),
                    ),
            ),
        ],
      ),
      TileGroup(
        title: 'Trackers',
        footer: d.trackers.isEmpty ? 'No trackers - peers come from DHT and PeX.' : null,
        children: [
          for (final tr in d.trackers)
            ListTile(
              dense: true,
              leading: Icon(Icons.circle, size: 12, color: StatusColors.toneOf(context, _trackerStatus(tr.status)).color),
              title: Text(tr.url, maxLines: 1, overflow: TextOverflow.ellipsis),
              subtitle: FactLine.plain([trackerStatusLabels[tr.status] ?? tr.status, if (tr.peers > 0) '${tr.peers} peers', if (tr.message.isNotEmpty) tr.message], maxLines: 2),
            ),
        ],
      ),
    ]);
  }

  Status _trackerStatus(String s) => switch (s) {
        'working' => Status.success,
        'not_working' => Status.error,
        'updating' => Status.warning,
        _ => Status.neutral,
      };
}

/// Remove a torrent: off the list, and its files too if asked. Returns
/// whether to delete the files; null = cancel.
class RemoveTorrentDialog extends StatefulWidget {
  const RemoveTorrentDialog({super.key, required this.name});
  final String name;

  @override
  State<RemoveTorrentDialog> createState() => _RemoveTorrentDialogState();
}

class _RemoveTorrentDialogState extends State<RemoveTorrentDialog> {
  bool _deleteFiles = false;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return AlertDialog(
      title: const Text('Remove torrent'),
      content: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text('Remove ${widget.name} from the list?'),
        const SizedBox(height: Space.md),
        CheckboxListTile(
          contentPadding: EdgeInsets.zero,
          value: _deleteFiles,
          onChanged: (v) => setState(() => _deleteFiles = v ?? false),
          title: const Text('Also delete the downloaded files'),
          subtitle: Text(_deleteFiles ? 'The files are permanently deleted from disk.' : 'The downloaded files are kept.'),
        ),
      ]),
      actions: [
        TextButton(autofocus: true, onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton(
          style: FilledButton.styleFrom(backgroundColor: scheme.error, foregroundColor: scheme.onError, side: BorderSide.none),
          onPressed: () => Navigator.of(context).pop(_deleteFiles),
          child: const Text('Remove'),
        ),
      ],
    );
  }
}
