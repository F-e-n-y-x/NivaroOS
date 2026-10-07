import 'package:flutter/material.dart';

import '../../models/server_profile.dart';
import '../../services/api_client.dart';
import '../../services/download_station_api.dart';
import '../../services/session_service.dart';
import '../../services/storage_service.dart';
import '../../ui/ui.dart';
import 'ds_add_torrent_sheet.dart';
import 'ds_folder_picker.dart';

/// "Add download" (and, from Android's share sheet, "Download on server"):
/// links, where to save them, and an optional name for a single link.
/// With [pickServer], the servers this phone is signed in to are offered
/// when there are several; picking another switches the app to it.
/// Returns how many downloads were added; null when cancelled.
Future<int?> showAddDownloadSheet(BuildContext context, {String text = '', bool pickServer = false, DownloadStationApi? api}) =>
    showModalBottomSheet<int>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (_) => AddDownloadSheet(text: text, pickServer: pickServer, api: api),
    );

class AddDownloadSheet extends StatefulWidget {
  const AddDownloadSheet({super.key, this.text = '', this.pickServer = false, this.api});

  final String text;
  final bool pickServer;
  final DownloadStationApi? api;

  @override
  State<AddDownloadSheet> createState() => _AddDownloadSheetState();
}

class _AddDownloadSheetState extends State<AddDownloadSheet> {
  late final DownloadStationApi _api = widget.api ?? DownloadStationApi();
  late final _links = TextEditingController(text: extractLinks(widget.text).join('\n'));
  final _name = TextEditingController();

  /// '' = the server's default folder ([_defaultDir]).
  String _dir = '';
  String? _defaultDir;
  List<ServerProfile> _servers = const [];
  String? _serverId;
  bool _busy = false;
  String? _error;

  List<String> get _urls => extractLinks(_links.text);

  bool get _onlyTorrent => _urls.isEmpty && RegExp(r'magnet:|\.torrent\b', caseSensitive: false).hasMatch(widget.text);

  @override
  void initState() {
    super.initState();
    _loadDefault();
    if (widget.pickServer) _loadServers();
  }

  @override
  void dispose() {
    _links.dispose();
    _name.dispose();
    super.dispose();
  }

  Future<void> _loadServers() async {
    try {
      final all = (await StorageService.instance.getProfiles()).where((p) => p.hasSession).toList();
      final current = ApiClient.instance.baseUrl;
      if (!mounted) return;
      setState(() {
        _servers = all;
        _serverId = all.where((p) => p.url == current).firstOrNull?.id ?? all.firstOrNull?.id;
      });
    } catch (_) {}
  }

  Future<void> _loadDefault() async {
    try {
      final s = await _api.settings();
      if (mounted) setState(() => _defaultDir = s.defaultDir);
    } catch (e) {
      if (mounted) setState(() => _defaultDir = '');
    }
  }

  Future<void> _switchServer(String? id) async {
    final p = _servers.where((s) => s.id == id).firstOrNull;
    if (p == null || id == _serverId) return;
    setState(() {
      _serverId = id;
      _dir = '';
      _defaultDir = null;
    });
    await SessionService.switchTo(p);
    await _loadDefault();
  }

  Future<void> _pickFolder() async {
    final picked = await pickDownloadFolder(context, start: _dir.isEmpty ? _defaultDir : _dir, api: _api);
    if (picked != null && mounted) setState(() => _dir = picked);
  }

  Future<void> _submit({required bool start}) async {
    final urls = _urls;
    if (urls.isEmpty) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    var added = 0;
    final failed = <String>[];
    String? reason;
    for (final u in urls) {
      try {
        await _api.add(u, dir: _dir, filename: urls.length == 1 ? _name.text.trim() : '', start: start);
        added++;
      } catch (e) {
        failed.add(u);
        reason ??= e.toString();
      }
    }
    if (!mounted) return;
    if (failed.isEmpty) {
      Navigator.of(context).pop(added);
      return;
    }
    // Keep only the ones that failed, so trying again never adds a link twice.
    setState(() {
      _busy = false;
      _links.text = failed.join('\n');
      _error = added == 0 ? reason : '$added added. ${failed.length} failed: $reason';
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    final urls = _urls;
    final folder = _dir.isNotEmpty ? _dir : (_defaultDir == null ? 'Loading…' : (_defaultDir!.isEmpty ? 'Default folder' : _defaultDir!));
    return SafeArea(
      child: Padding(
        padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
        child: SingleChildScrollView(
          padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.lg),
          child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
            Text(widget.pickServer ? 'Download on server' : 'Add download', style: theme.textTheme.titleLarge),
            const SizedBox(height: Space.lg),
            if (_servers.length > 1) ...[
              DropdownButtonFormField<String>(
                initialValue: _serverId,
                decoration: const InputDecoration(labelText: 'Server'),
                items: [for (final s in _servers) DropdownMenuItem(value: s.id, child: Text(s.displayName, overflow: TextOverflow.ellipsis))],
                onChanged: _busy ? null : _switchServer,
              ),
              const SizedBox(height: Space.md),
            ],
            if (_onlyTorrent)
              Padding(
                padding: const EdgeInsets.only(bottom: Space.md),
                child: Notice(
                  status: Status.info,
                  message: 'Magnet links and .torrent files are added as torrents.',
                  actionLabel: 'Add as torrent',
                  onAction: () {
                    final nav = Navigator.of(context);
                    nav.pop();
                    showAddTorrentSheet(nav.context, text: widget.text, api: widget.api);
                  },
                ),
              ),
            TextField(
              controller: _links,
              minLines: 1,
              maxLines: 5,
              keyboardType: TextInputType.url,
              autocorrect: false,
              onChanged: (_) => setState(() {}),
              decoration: InputDecoration(
                labelText: urls.length > 1 ? 'Links (one per line)' : 'Link',
                hintText: 'https://example.com/file.zip',
              ),
            ),
            if (urls.length <= 1) ...[
              const SizedBox(height: Space.md),
              TextField(
                controller: _name,
                autocorrect: false,
                decoration: const InputDecoration(labelText: 'Save as (optional)', hintText: 'The name the site gives it'),
              ),
            ],
            const SizedBox(height: Space.sm),
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.folder_outlined),
              title: const Text('Save to'),
              subtitle: Text(folder, maxLines: 2, overflow: TextOverflow.ellipsis),
              trailing: TextButton(onPressed: _busy || _defaultDir == null ? null : _pickFolder, child: const Text('Change')),
            ),
            if (_error != null) ...[
              const SizedBox(height: Space.sm),
              Text(_error!, style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.error)),
            ],
            const SizedBox(height: Space.lg),
            Row(children: [
              Expanded(
                child: OutlinedButton(onPressed: _busy || urls.isEmpty ? null : () => _submit(start: false), child: const Text('Download later')),
              ),
              const SizedBox(width: Space.md),
              Expanded(
                child: FilledButton(
                  onPressed: _busy || urls.isEmpty ? null : () => _submit(start: true),
                  child: Text(urls.length > 1 ? 'Download ${urls.length}' : 'Download'),
                ),
              ),
            ]),
          ]),
        ),
      ),
    );
  }
}
