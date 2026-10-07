/// Download Station's torrents: the list with live progress, speeds,
/// seeds/peers, ratio and time left, whichever engine the server runs
/// (services/download-sidecar/torrent.go), with filters, search,
/// pause / resume and the alternative speed switch. Wording follows the
/// web's Torrents section (ui/src/apps/download-station/torrent).
library;

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';

import '../../backup/backup_strings.dart' show formatSeconds;
import '../../services/api_client.dart';
import '../../services/download_station_api.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import '../backup/backup_widgets.dart' show BackupPolling;
import 'ds_add_torrent_sheet.dart';
import 'torrent_detail_screen.dart';
import 'torrent_settings_screen.dart';

enum TorrentFilter { all, downloading, seeding, paused, completed }

extension TorrentFilterX on TorrentFilter {
  String get label => switch (this) {
        TorrentFilter.all => 'All',
        TorrentFilter.downloading => 'Downloading',
        TorrentFilter.seeding => 'Seeding',
        TorrentFilter.paused => 'Paused',
        TorrentFilter.completed => 'Completed',
      };

  bool matches(DsTorrent t) => switch (this) {
        TorrentFilter.all => true,
        TorrentFilter.downloading => const {'downloading', 'stalled', 'metadata', 'queued', 'checking'}.contains(t.state) && !t.isFinished,
        TorrentFilter.seeding => t.state == 'seeding',
        TorrentFilter.paused => t.state == 'paused' || t.state == 'error',
        TorrentFilter.completed => t.isFinished || t.state == 'completed',
      };
}

const engineNames = {'qbittorrent': 'qBittorrent', 'external': 'My qBittorrent', 'builtin': 'Built-in engine'};

/// The state badge: "41.2%" while downloading, else the state's name.
String torrentStateLabel(DsTorrent t) => switch (t.state) {
      'downloading' => '${(t.progress * 1000).floor() / 10}%',
      'stalled' => 'Stalled',
      'metadata' => 'Getting metadata',
      'seeding' => 'Seeding',
      'queued' => 'Queued',
      'checking' => 'Checking',
      'moving' => 'Moving',
      'paused' => 'Paused',
      'completed' => 'Completed',
      _ => 'Error',
    };

Status torrentStatus(String state) => switch (state) {
      'seeding' || 'completed' => Status.success,
      'error' => Status.error,
      'paused' || 'queued' => Status.neutral,
      'stalled' => Status.warning,
      _ => Status.info,
    };

String formatRatio(double r) => r < 0 ? '—' : (r >= 100 ? r.toStringAsFixed(0) : r.toStringAsFixed(2));

/// "312 MB / 756 MB · 6.2 MB/s down · 310 KB/s up · 1 min left · 24/212 seeds · ratio 0.04"
List<String> torrentFacts(DsTorrent t) {
  if (t.state == 'error') return [t.error.isEmpty ? 'Error' : t.error];
  return [
    t.isFinished ? formatSize(t.size) : (t.size > 0 ? '${formatSize(t.done)} / ${formatSize(t.size)}' : 'Size not known yet'),
    if (t.dlSpeed > 0) '${formatSpeed(t.dlSpeed)} down',
    if (t.upSpeed > 0) '${formatSpeed(t.upSpeed)} up',
    if (t.eta >= 0 && !t.isFinished) '${formatSeconds(t.eta)} left',
    if (!t.isPaused) '${t.seeds}/${t.seedsTotal} seeds · ${t.peers} peers',
    'ratio ${formatRatio(t.ratio)}',
  ];
}

class TorrentsScreen extends StatefulWidget {
  const TorrentsScreen({super.key, this.api});

  final DownloadStationApi? api;

  @override
  State<TorrentsScreen> createState() => _TorrentsScreenState();
}

class _TorrentsScreenState extends State<TorrentsScreen> with WidgetsBindingObserver, BackupPolling {
  late final DownloadStationApi _api = widget.api ?? DownloadStationApi();
  TorrentFilter _filter = TorrentFilter.all;
  final _search = TextEditingController();
  DsTorrents? _data;
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
      final d = await _api.torrents();
      if (!mounted) return;
      setState(() {
        _data = d;
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
    final added = await showAddTorrentSheet(context, api: _api);
    if (added != null && mounted) await _load();
  }

  Future<void> _toggleAlt() => _run(() async {
        final s = await _api.torrentSettings();
        await _api.saveTorrentSettings({'alt_speed': s['alt_speed'] != true});
      }, "Couldn't switch the speed limits");

  void _open(DsTorrent t) => Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => TorrentDetailScreen(hash: t.hash, name: t.name, api: _api))).then((_) => _load());

  @override
  Widget build(BuildContext context) {
    final d = _data;
    return AppScaffold.slivers(
      title: 'Torrents',
      onRefresh: _load,
      banner: _error != null && d != null ? OfflineBanner(lastUpdated: _loadedAt, onRetry: _load) : null,
      actions: [
        if (d != null)
          IconButton(
            icon: Icon(d.altSpeedActive ? Icons.speed : Icons.speed_outlined, color: d.altSpeedActive ? Theme.of(context).colorScheme.primary : null),
            tooltip: d.altSpeedActive ? 'Alternative speed limits are on' : 'Use alternative speed limits',
            isSelected: d.altSpeedActive,
            onPressed: _toggleAlt,
          ),
        IconButton(
          icon: const Icon(Icons.settings_outlined),
          tooltip: 'Torrent settings',
          onPressed: () => Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => TorrentSettingsScreen(api: _api))).then((_) => _load()),
        ),
      ],
      floatingActionButton: d == null ? null : FloatingActionButton.extended(onPressed: _add, icon: const Icon(Icons.add_link), label: const Text('Add torrent')),
      slivers: _body(d),
    );
  }

  List<Widget> _body(DsTorrents? d) {
    if (d == null) {
      final e = _error;
      if (e == null) return const [SliverLoadingList(rows: 6, trailing: true)];
      if (DownloadStationApi.isUnavailable(e)) {
        return const [
          EmptyState(
            sliver: true,
            icon: Icons.download_outlined,
            title: "Torrents aren't available",
            message: "This server's Download Station doesn't have torrents yet, or it isn't answering. Update NivaroOS on the server.",
          ),
        ];
      }
      return [
        e is ApiException && e.isUnreachable
            ? ErrorState.offline(onRetry: _load, sliver: true)
            : ErrorState(title: "Couldn't load the torrents", message: e.toString(), onRetry: _load, details: 'GET ${DownloadStationApi.base}/torrents: $e', sliver: true),
      ];
    }
    final gutter = Space.gutter(context);
    final theme = Theme.of(context);
    final engineLine = Padding(
      padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.sm),
      child: FactLine.plain([
        '${engineNames[d.engine] ?? d.engine} · ${d.running ? 'running' : 'idle'}',
        if (d.dlSpeed > 0) '${formatSpeed(d.dlSpeed)} down',
        if (d.upSpeed > 0) '${formatSpeed(d.upSpeed)} up',
      ], style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant).tabular),
    );
    if (d.torrents.isEmpty) {
      return [
        SliverToBoxAdapter(child: engineLine),
        EmptyState(
          sliver: true,
          icon: Icons.add_link,
          title: 'No torrents yet',
          message: 'Add a magnet link or a .torrent file here, or open one with NivaroOS from another app.',
          actionLabel: 'Add torrent',
          onAction: _add,
        ),
      ];
    }
    final q = _search.text.trim().toLowerCase();
    final counts = {for (final f in TorrentFilter.values) f: d.torrents.where(f.matches).length};
    final visible = d.torrents.where((t) => _filter.matches(t) && (q.isEmpty || t.name.toLowerCase().contains(q) || t.category.toLowerCase().contains(q))).toList();
    return [
      SliverToBoxAdapter(child: engineLine),
      SliverToBoxAdapter(
        child: Padding(
          padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.sm),
          child: TextField(
            controller: _search,
            onChanged: (_) => setState(() {}),
            decoration: InputDecoration(
              prefixIcon: const Icon(Icons.search),
              hintText: 'Search torrents',
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
            for (final f in TorrentFilter.values)
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
            for (final t in visible)
              TorrentTile(
                torrent: t,
                onTap: () => _open(t),
                onToggle: () => _run(() => _api.torrentAction(t.hash, t.isPaused ? 'resume' : 'pause'), "Couldn't ${t.isPaused ? 'resume' : 'pause'} ${t.name}"),
              ),
          ]),
          const SizedBox(height: 88),
        ]),
    ];
  }
}

class TorrentTile extends StatelessWidget {
  const TorrentTile({super.key, required this.torrent, required this.onTap, required this.onToggle});

  final DsTorrent torrent;
  final VoidCallback onTap;
  final VoidCallback onToggle;

  @override
  Widget build(BuildContext context) {
    final t = torrent;
    final theme = Theme.of(context);
    final status = torrentStatus(t.state);
    final tone = StatusColors.toneOf(context, status);
    final muted = theme.textTheme.bodySmall?.copyWith(color: t.state == 'error' ? theme.colorScheme.error : theme.colorScheme.onSurfaceVariant);
    return ListTile(
      onTap: onTap,
      title: Text(t.name, maxLines: 2, overflow: TextOverflow.ellipsis),
      subtitle: Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisSize: MainAxisSize.min, children: [
        const SizedBox(height: Space.xs),
        Wrap(spacing: Space.sm, runSpacing: Space.xs, crossAxisAlignment: WrapCrossAlignment.center, children: [
          StatusChip(label: torrentStateLabel(t), status: status),
          if (t.category.isNotEmpty) StatusChip(label: t.category, status: Status.neutral, icon: Icons.label_outline),
          if (t.private == true) const StatusChip(label: 'Private', status: Status.warning, icon: Icons.lock_outline),
        ]),
        if (!t.isFinished) ...[
          const SizedBox(height: Space.sm),
          LinearProgressIndicator(value: t.state == 'metadata' ? null : t.progress, color: t.state == 'downloading' ? null : tone.color),
        ],
        const SizedBox(height: Space.xs),
        FactLine.plain(torrentFacts(t), maxLines: 3, style: muted?.tabular),
      ]),
      trailing: IconButton(
        icon: Icon(t.isPaused ? Icons.play_arrow_outlined : Icons.pause),
        tooltip: '${t.isPaused ? 'Resume' : 'Pause'} ${t.name}',
        onPressed: onToggle,
      ),
    );
  }
}
