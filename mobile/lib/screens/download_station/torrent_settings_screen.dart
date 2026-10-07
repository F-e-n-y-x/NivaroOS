import 'package:flutter/material.dart';

import '../../services/api_client.dart';
import '../../services/download_station_api.dart';
import '../../ui/ui.dart';
import 'ds_folder_picker.dart';

/// Torrent settings, as on the web (Settings > Torrents), modelled on
/// qBittorrent's: the engine, folders, categories, speed limits with the
/// alternative schedule, queueing, seeding limits, connection and the
/// public trackers list. Each change is saved at once; the server checks
/// it (and every folder) and the screen shows what it kept.
class TorrentSettingsScreen extends StatefulWidget {
  const TorrentSettingsScreen({super.key, this.api});

  final DownloadStationApi? api;

  @override
  State<TorrentSettingsScreen> createState() => _TorrentSettingsScreenState();
}

class _TorrentSettingsScreenState extends State<TorrentSettingsScreen> {
  late final DownloadStationApi _api = widget.api ?? DownloadStationApi();
  Map<String, dynamic>? _s;
  DsTorrents? _info;
  Map<String, dynamic>? _trackers;
  Object? _error;
  bool _refreshing = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final s = await _api.torrentSettings();
      final info = await _api.torrents();
      final tl = await _api.trackerList();
      if (mounted) {
        setState(() {
          _s = s;
          _info = info;
          _trackers = tl;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _save(Map<String, Object?> changes) async {
    try {
      final s = await _api.saveTorrentSettings(changes);
      final info = changes.containsKey('engine') ? await _api.torrents() : _info;
      if (mounted) {
        setState(() {
          _s = s;
          _info = info;
        });
      }
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text("Couldn't save: $e")));
      await _load();
    }
  }

  Future<void> _refreshTrackers() async {
    setState(() => _refreshing = true);
    try {
      final tl = await _api.refreshTrackerList();
      if (mounted) setState(() => _trackers = tl);
    } catch (e) {
      if (mounted) ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text("Couldn't refresh: $e")));
    } finally {
      if (mounted) setState(() => _refreshing = false);
    }
  }

  /// A number typed in a dialog; [scale] converts what's shown to what's
  /// stored (1024 for KiB/s).
  Future<void> _editNumber(String key, String title, {String? unit, String? help, num scale = 1, bool decimal = false}) async {
    final cur = (_s![key] as num? ?? 0) / scale;
    final ctrl = TextEditingController(text: decimal ? cur.toString() : cur.round().toString());
    final v = await showDialog<num>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(title),
        content: TextField(
          controller: ctrl,
          autofocus: true,
          keyboardType: TextInputType.numberWithOptions(decimal: decimal),
          decoration: InputDecoration(suffixText: unit, helperText: help, helperMaxLines: 3),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
          TextButton(onPressed: () => Navigator.of(context).pop(num.tryParse(ctrl.text.replaceAll(',', '.'))), child: const Text('Save')),
        ],
      ),
    );
    ctrl.dispose();
    if (v == null || v < 0) return;
    await _save({key: decimal ? v * scale : (v * scale).round()});
  }

  Future<void> _editText(String key, String title, {String? hint, bool obscure = false}) async {
    final ctrl = TextEditingController(text: obscure ? '' : (_s![key]?.toString() ?? ''));
    final v = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(title),
        content: TextField(controller: ctrl, autofocus: true, obscureText: obscure, autocorrect: false, decoration: InputDecoration(hintText: hint)),
        actions: [
          TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
          TextButton(onPressed: () => Navigator.of(context).pop(ctrl.text.trim()), child: const Text('Save')),
        ],
      ),
    );
    ctrl.dispose();
    if (v == null) return;
    await _save({key: v});
  }

  Future<void> _pickTime(String key) async {
    final parts = (_s![key]?.toString() ?? '08:00').split(':');
    final t = await showTimePicker(context: context, initialTime: TimeOfDay(hour: int.tryParse(parts[0]) ?? 8, minute: int.tryParse(parts.last) ?? 0));
    if (t == null) return;
    await _save({key: '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}'});
  }

  Future<void> _pickFolder(String key) async {
    final p = await pickDownloadFolder(context, start: (_s![key]?.toString() ?? '').isEmpty ? _s!['save_dir']?.toString() : _s![key].toString(), api: _api);
    if (p != null) await _save({key: p});
  }

  List<Map<String, dynamic>> get _categories => [
        for (final c in _s!['categories'] is List ? _s!['categories'] as List : const [])
          if (c is Map) {'name': c['name']?.toString() ?? '', 'dir': c['dir']?.toString() ?? ''},
      ];

  Future<void> _addCategory() async {
    final ctrl = TextEditingController();
    final name = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('New category'),
        content: TextField(controller: ctrl, autofocus: true, maxLength: 64, decoration: const InputDecoration(hintText: 'e.g. Movies')),
        actions: [
          TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
          TextButton(onPressed: () => Navigator.of(context).pop(ctrl.text.trim()), child: const Text('Add')),
        ],
      ),
    );
    ctrl.dispose();
    if (name == null || name.isEmpty) return;
    await _save({'categories': [..._categories, {'name': name, 'dir': ''}]});
  }

  Future<void> _categoryActions(int i) async {
    final c = _categories[i];
    final pick = await showModalBottomSheet<String>(
      context: context,
      showDragHandle: true,
      builder: (context) => SafeArea(
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          ListTile(title: Text(c['name'] as String, style: Theme.of(context).textTheme.titleMedium)),
          if (_managed) ListTile(leading: const Icon(Icons.folder_outlined), title: const Text('Choose its folder'), onTap: () => Navigator.of(context).pop('folder')),
          ListTile(leading: const Icon(Icons.delete_outline), title: const Text('Remove category'), onTap: () => Navigator.of(context).pop('remove')),
        ]),
      ),
    );
    if (pick == null || !mounted) return;
    final cats = _categories;
    if (pick == 'remove') {
      cats.removeAt(i);
    } else {
      final p = await pickDownloadFolder(context, start: (c['dir'] as String).isEmpty ? _s!['save_dir']?.toString() : c['dir'] as String, api: _api);
      if (p == null) return;
      cats[i]['dir'] = p;
    }
    await _save({'categories': cats});
  }

  String get _engine => _info?.engine ?? (_s!['engine']?.toString() ?? '');
  bool get _managed => _engine != 'external';
  bool _can(String key) => _info?.supports(key) ?? true;

  String _kib(String key) {
    final v = (_s![key] as num? ?? 0) ~/ 1024;
    return v == 0 ? 'Unlimited' : '$v KiB/s';
  }

  String _off(String key, String unit) {
    final v = _s![key] as num? ?? 0;
    return v == 0 ? 'Off' : '$v $unit';
  }

  @override
  Widget build(BuildContext context) {
    final s = _s;
    final Widget body;
    if (s == null && _error != null) {
      final e = _error!;
      body = e is ApiException && e.isUnreachable ? ErrorState.offline(onRetry: _load) : ErrorState(title: "Couldn't load the settings", message: e.toString(), onRetry: _load);
    } else if (s == null) {
      body = const LoadingList(rows: 6);
    } else {
      body = ListView(padding: const EdgeInsets.only(bottom: Space.xl), children: _groups(s));
    }
    return AppScaffold(title: 'Torrent settings', onRefresh: _load, body: body);
  }

  List<Widget> _groups(Map<String, dynamic> s) {
    bool b(String k) => s[k] == true;
    String str(String k) => s[k]?.toString() ?? '';
    Widget toggle(String key, String title, {String? subtitle, IconData? icon, bool enabled = true}) => SwitchListTile(
          secondary: icon == null ? null : Icon(icon),
          title: Text(title),
          subtitle: subtitle == null ? (enabled ? null : const Text('Not available with this engine')) : Text(subtitle),
          value: b(key),
          onChanged: enabled ? (v) => _save({key: v}) : null,
        );
    Widget tile(IconData icon, String title, String value, VoidCallback onTap, {bool enabled = true}) => ListTile(
          leading: Icon(icon),
          title: Text(title),
          subtitle: Text(enabled ? value : 'Not available with this engine', maxLines: 2, overflow: TextOverflow.ellipsis),
          trailing: const Icon(Icons.chevron_right),
          enabled: enabled,
          onTap: enabled ? onTap : null,
        );
    final tl = _trackers;
    final updated = DateTime.tryParse(tl?['updated_at']?.toString() ?? '');
    final trackerCount = (tl?['trackers'] as List?)?.length ?? 0;
    return [
      TileGroup(
        title: 'Engine',
        footer: switch (_engine) {
          'qbittorrent' => 'libtorrent, the fastest engine. Runs only while a torrent is active. Torrents stay with the engine they were added to.',
          'external' => "Your own qBittorrent. Its speed, queue and connection settings stay in its own Web UI.",
          _ => 'A lighter engine inside Download Station, for small servers. Torrents stay with the engine they were added to.',
        },
        children: [
          for (final (id, label) in const [('qbittorrent', 'qBittorrent (recommended)'), ('external', 'My qBittorrent'), ('builtin', 'Built-in')])
            ListTile(
              leading: Icon(_engine == id ? Icons.radio_button_checked : Icons.radio_button_unchecked, color: _engine == id ? Theme.of(context).colorScheme.primary : null),
              title: Text(label),
              subtitle: id == 'qbittorrent' && _info?.qbittorrent == false ? const Text('qbittorrent-nox is not installed on the server') : null,
              enabled: !(id == 'qbittorrent' && _info?.qbittorrent == false),
              onTap: () => id == 'external' && str('external_url').isEmpty ? _editText('external_url', 'Address of your qBittorrent Web UI', hint: 'http://192.168.1.10:8080').then((_) => _save({'engine': 'external'})) : _save({'engine': id}),
            ),
          if (_engine == 'external') ...[
            tile(Icons.dns_outlined, 'Address', str('external_url'), () => _editText('external_url', 'Address of your qBittorrent Web UI', hint: 'http://192.168.1.10:8080')),
            tile(Icons.person_outline, 'Username', str('external_user').isEmpty ? 'Not set' : str('external_user'), () => _editText('external_user', 'Username')),
            tile(Icons.key_outlined, 'Password', b('external_password_set') ? 'Saved' : 'Not set', () => _editText('external_password', 'Password', obscure: true)),
          ],
        ],
      ),
      if (_managed)
        TileGroup(title: 'Folders', children: [
          tile(Icons.folder_outlined, 'Save folder', str('save_dir'), () => _pickFolder('save_dir')),
          ListTile(
            leading: const Icon(Icons.pending_outlined),
            title: const Text('Keep incomplete torrents in'),
            subtitle: Text(str('incomplete_dir').isEmpty ? 'Off - they download straight into the save folder' : str('incomplete_dir')),
            trailing: str('incomplete_dir').isEmpty ? const Icon(Icons.chevron_right) : IconButton(icon: const Icon(Icons.close), tooltip: 'Turn off', onPressed: () => _save({'incomplete_dir': ''})),
            onTap: () => _pickFolder('incomplete_dir'),
          ),
          ListTile(
            leading: const Icon(Icons.visibility_outlined),
            title: const Text('Watch folder'),
            subtitle: Text(str('watch_dir').isEmpty ? 'Off - .torrent files saved here are added automatically' : str('watch_dir')),
            trailing: str('watch_dir').isEmpty ? const Icon(Icons.chevron_right) : IconButton(icon: const Icon(Icons.close), tooltip: 'Turn off', onPressed: () => _save({'watch_dir': ''})),
            onTap: () => _pickFolder('watch_dir'),
          ),
        ]),
      TileGroup(title: 'Categories', children: [
        for (final (i, c) in _categories.indexed)
          ListTile(
            leading: const Icon(Icons.label_outline),
            title: Text(c['name'] as String),
            subtitle: _managed ? Text((c['dir'] as String).isEmpty ? '${str('save_dir')}/${c['name']}' : c['dir'] as String) : null,
            trailing: const Icon(Icons.more_vert),
            onTap: () => _categoryActions(i),
          ),
        ListTile(leading: const Icon(Icons.add), title: const Text('Add category'), onTap: _addCategory),
      ]),
      if (_managed) ...[
        TileGroup(title: 'Speed', footer: 'The alternative limits apply while switched on, or during the schedule.', children: [
          tile(Icons.download_outlined, 'Download limit', _kib('dl_limit'), () => _editNumber('dl_limit', 'Download limit', unit: 'KiB/s', help: '0 is unlimited.', scale: 1024)),
          tile(Icons.upload_outlined, 'Upload limit', _kib('up_limit'), () => _editNumber('up_limit', 'Upload limit', unit: 'KiB/s', help: '0 is unlimited.', scale: 1024)),
          tile(Icons.download_outlined, 'Alternative download limit', _kib('alt_dl_limit'), () => _editNumber('alt_dl_limit', 'Alternative download limit', unit: 'KiB/s', scale: 1024)),
          tile(Icons.upload_outlined, 'Alternative upload limit', _kib('alt_up_limit'), () => _editNumber('alt_up_limit', 'Alternative upload limit', unit: 'KiB/s', scale: 1024)),
          toggle('alt_speed', 'Use the alternative limits now', icon: Icons.speed),
          toggle('schedule', 'Schedule the alternative limits', icon: Icons.schedule),
          if (b('schedule')) ...[
            tile(Icons.play_circle_outline, 'From', str('schedule_from'), () => _pickTime('schedule_from')),
            tile(Icons.stop_circle_outlined, 'To', str('schedule_to'), () => _pickTime('schedule_to')),
            ListTile(
              leading: const Icon(Icons.date_range_outlined),
              title: const Text('Days'),
              trailing: DropdownButton<String>(
                value: str('schedule_days'),
                underline: const SizedBox.shrink(),
                items: const [
                  DropdownMenuItem(value: 'every', child: Text('Every day')),
                  DropdownMenuItem(value: 'weekdays', child: Text('Weekdays')),
                  DropdownMenuItem(value: 'weekends', child: Text('Weekends')),
                ],
                onChanged: (v) => _save({'schedule_days': v}),
              ),
            ),
          ],
        ]),
        TileGroup(title: 'Queue and seeding', children: [
          toggle('queueing', 'Torrent queueing', subtitle: 'At most this many active at once; the rest wait', icon: Icons.low_priority),
          if (b('queueing')) ...[
            tile(Icons.download_outlined, 'Active downloads', _off('max_active_downloads', ''), () => _editNumber('max_active_downloads', 'Active downloads', help: '0 is no limit.')),
            tile(Icons.upload_outlined, 'Active uploads', _off('max_active_uploads', ''), () => _editNumber('max_active_uploads', 'Active uploads', help: '0 is no limit.')),
            tile(Icons.swap_vert, 'Active torrents', _off('max_active_torrents', ''), () => _editNumber('max_active_torrents', 'Active torrents', help: '0 is no limit.')),
          ],
          tile(Icons.percent, 'Stop seeding at ratio', _off('ratio_limit', ''), () => _editNumber('ratio_limit', 'Stop seeding at ratio', help: '0 is off.', decimal: true)),
          tile(Icons.timer_outlined, 'Stop seeding after', _off('seed_time_limit', 'min'), () => _editNumber('seed_time_limit', 'Stop seeding after', unit: 'minutes', help: '0 is off.')),
          ListTile(
            leading: const Icon(Icons.flag_outlined),
            title: const Text('Then'),
            trailing: DropdownButton<String>(
              value: str('seed_limit_action'),
              underline: const SizedBox.shrink(),
              items: const [
                DropdownMenuItem(value: 'pause', child: Text('Pause it')),
                DropdownMenuItem(value: 'remove', child: Text('Remove it (files kept)')),
              ],
              onChanged: (v) => _save({'seed_limit_action': v}),
            ),
          ),
        ]),
        TileGroup(title: 'Connection', footer: 'The listening port is the only one torrents open to the internet.', children: [
          tile(Icons.settings_ethernet, 'Listening port', str('listen_port'), () => _editNumber('listen_port', 'Listening port', help: '1024 to 65535.')),
          toggle('upnp', 'Forward the port (UPnP / NAT-PMP)', icon: Icons.router_outlined),
          toggle('dht', 'DHT', subtitle: 'Find peers without trackers', icon: Icons.hub_outlined),
          toggle('pex', 'Peer exchange (PeX)', icon: Icons.people_outline),
          toggle('lsd', 'Local peer discovery', icon: Icons.lan_outlined, enabled: _can('lsd')),
          ListTile(
            leading: const Icon(Icons.lock_outline),
            title: const Text('Encryption'),
            trailing: DropdownButton<String>(
              value: str('encryption'),
              underline: const SizedBox.shrink(),
              items: const [
                DropdownMenuItem(value: 'allow', child: Text('Allow')),
                DropdownMenuItem(value: 'prefer', child: Text('Prefer')),
                DropdownMenuItem(value: 'require', child: Text('Require')),
              ],
              onChanged: (v) => _save({'encryption': v}),
            ),
          ),
          tile(Icons.device_hub, 'Maximum connections', _off('max_connections', ''), () => _editNumber('max_connections', 'Maximum connections', help: '0 is no limit.'), enabled: _can('max_connections')),
          tile(Icons.device_hub, 'Per torrent', _off('max_connections_per_torrent', ''), () => _editNumber('max_connections_per_torrent', 'Maximum connections per torrent', help: '0 is no limit.')),
          toggle('preallocate', 'Pre-allocate disk space', icon: Icons.storage_outlined, enabled: _can('preallocate')),
        ]),
      ],
      TileGroup(
        title: 'Public trackers',
        footer: 'Fetched from the list and added to public torrents only - never to private ones.',
        children: [
          toggle('trackers_auto', 'Add the best public trackers automatically', icon: Icons.radar),
          if (b('trackers_auto')) ...[
            tile(Icons.link, 'Trackers list URL', str('trackers_url'), () => _editText('trackers_url', 'Trackers list URL')),
            ListTile(
              leading: const Icon(Icons.update),
              title: const Text('Refresh'),
              trailing: DropdownButton<int>(
                value: (s['trackers_interval_hours'] as num?)?.toInt() ?? 24,
                underline: const SizedBox.shrink(),
                items: const [
                  DropdownMenuItem(value: 6, child: Text('Every 6 hours')),
                  DropdownMenuItem(value: 12, child: Text('Every 12 hours')),
                  DropdownMenuItem(value: 24, child: Text('Daily')),
                  DropdownMenuItem(value: 168, child: Text('Weekly')),
                ],
                onChanged: (v) => _save({'trackers_interval_hours': v}),
              ),
            ),
            ListTile(
              leading: const Icon(Icons.refresh),
              title: const Text('Refresh now'),
              subtitle: Text([
                if (updated == null || updated.year < 2000) 'Not fetched yet' else '$trackerCount trackers · updated ${MaterialLocalizations.of(context).formatShortDate(updated.toLocal())}',
                if ((tl?['error']?.toString() ?? '').isNotEmpty) tl!['error'].toString(),
              ].join('\n')),
              trailing: _refreshing ? const SizedBox.square(dimension: 20, child: CircularProgressIndicator(strokeWidth: 2)) : null,
              onTap: _refreshing ? null : _refreshTrackers,
            ),
          ],
        ],
      ),
    ];
  }
}
