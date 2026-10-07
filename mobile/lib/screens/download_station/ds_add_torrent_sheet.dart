import 'dart:io';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';

import '../../services/download_station_api.dart';
import '../../ui/ui.dart';
import 'ds_folder_picker.dart';

/// "Add torrent": magnet links and links to .torrent files (one per line)
/// and/or .torrent files, where to save them, a category, and the start
/// options. Returns how many were added; null when cancelled.
Future<int?> showAddTorrentSheet(BuildContext context, {String text = '', List<TorrentFile> files = const [], DownloadStationApi? api}) => showModalBottomSheet<int>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (_) => AddTorrentSheet(text: text, files: files, api: api),
    );

class AddTorrentSheet extends StatefulWidget {
  const AddTorrentSheet({super.key, this.text = '', this.files = const [], this.api});

  final String text;
  final List<TorrentFile> files;
  final DownloadStationApi? api;

  @override
  State<AddTorrentSheet> createState() => _AddTorrentSheetState();
}

class _AddTorrentSheetState extends State<AddTorrentSheet> {
  late final DownloadStationApi _api = widget.api ?? DownloadStationApi();
  late final _links = TextEditingController(text: extractTorrentSources(widget.text).join('\n'));
  late final List<TorrentFile> _files = [...widget.files];

  /// '' = the default folder (the category's, else the save folder).
  String _dir = '';
  String? _saveDir;
  List<({String name, String dir})> _categories = const [];
  String _category = '';
  bool _external = false;
  bool _paused = false;
  bool _sequential = false;
  bool _firstLast = false;
  bool _busy = false;
  String? _error;

  List<String> get _sources => extractTorrentSources(_links.text);
  int get _count => _sources.length + _files.length;

  @override
  void initState() {
    super.initState();
    _loadSettings();
  }

  @override
  void dispose() {
    _links.dispose();
    super.dispose();
  }

  Future<void> _loadSettings() async {
    try {
      final s = await _api.torrentSettings();
      final info = await _api.torrents();
      if (!mounted) return;
      setState(() {
        _saveDir = s['save_dir']?.toString() ?? '';
        _categories = [
          for (final c in s['categories'] is List ? s['categories'] as List : const [])
            if (c is Map) (name: c['name']?.toString() ?? '', dir: c['dir']?.toString() ?? ''),
        ];
        _external = info.engine == 'external';
      });
    } catch (_) {
      if (mounted) setState(() => _saveDir = '');
    }
  }

  String get _defaultDir {
    final c = _categories.where((c) => c.name == _category).firstOrNull;
    if (c != null) return c.dir.isNotEmpty ? c.dir : '$_saveDir/${c.name}';
    return _saveDir ?? '';
  }

  Future<void> _pickFiles() async {
    try {
      final picked = await FilePicker.pickFiles(dialogTitle: 'Choose .torrent files');
      final add = <TorrentFile>[];
      for (final f in picked) {
        final path = f.path;
        if (path == null || !f.name.toLowerCase().endsWith('.torrent')) continue;
        add.add(TorrentFile(f.name, await File(path).readAsBytes()));
      }
      if (!mounted) return;
      setState(() {
        _files.addAll(add);
        if (add.length < picked.length) _error = 'Only .torrent files can be added.';
      });
    } catch (e) {
      if (mounted) setState(() => _error = "Couldn't open the file picker: $e");
    }
  }

  Future<void> _pickFolder() async {
    final picked = await pickDownloadFolder(context, start: _dir.isEmpty ? _defaultDir : _dir, api: _api);
    if (picked != null && mounted) setState(() => _dir = picked);
  }

  Future<void> _submit() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    var added = 0;
    final failedLinks = <String>[];
    final failedFiles = <TorrentFile>[];
    String? reason;
    Future<bool> add({String? source, List<int>? file}) async {
      try {
        await _api.addTorrent(source: source, file: file, dir: _dir, category: _category, paused: _paused, sequential: _sequential, firstLast: _firstLast);
        added++;
        return true;
      } catch (e) {
        reason ??= e.toString();
        return false;
      }
    }

    for (final s in _sources) {
      if (!await add(source: s)) failedLinks.add(s);
    }
    for (final f in _files) {
      if (!await add(file: f.bytes)) failedFiles.add(f);
    }
    if (!mounted) return;
    if (failedLinks.isEmpty && failedFiles.isEmpty) {
      Navigator.of(context).pop(added);
      return;
    }
    // Only the failed ones stay, so trying again never adds one twice.
    setState(() {
      _busy = false;
      _links.text = failedLinks.join('\n');
      _files
        ..clear()
        ..addAll(failedFiles);
      _error = added == 0 ? reason : '$added added. ${failedLinks.length + failedFiles.length} failed: $reason';
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    final folder = _dir.isNotEmpty ? _dir : (_saveDir == null ? 'Loading…' : (_defaultDir.isEmpty ? 'Default folder' : _defaultDir));
    return SafeArea(
      child: Padding(
        padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
        child: SingleChildScrollView(
          padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.lg),
          child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
            Text('Add torrent', style: theme.textTheme.titleLarge),
            const SizedBox(height: Space.lg),
            TextField(
              controller: _links,
              minLines: 1,
              maxLines: 5,
              keyboardType: TextInputType.url,
              autocorrect: false,
              onChanged: (_) => setState(() {}),
              decoration: const InputDecoration(labelText: 'Magnet links or links to .torrent files', hintText: 'magnet:?xt=urn:btih:…'),
            ),
            const SizedBox(height: Space.sm),
            for (final f in _files)
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.description_outlined),
                title: Text(f.name, maxLines: 1, overflow: TextOverflow.ellipsis),
                trailing: IconButton(icon: const Icon(Icons.close), tooltip: 'Remove ${f.name}', onPressed: _busy ? null : () => setState(() => _files.remove(f))),
              ),
            Align(
              alignment: AlignmentDirectional.centerStart,
              child: TextButton.icon(onPressed: _busy ? null : _pickFiles, icon: const Icon(Icons.upload_file_outlined), label: const Text('Choose .torrent files')),
            ),
            if (_categories.isNotEmpty) ...[
              const SizedBox(height: Space.sm),
              DropdownButtonFormField<String>(
                initialValue: _category,
                decoration: const InputDecoration(labelText: 'Category'),
                items: [
                  const DropdownMenuItem(value: '', child: Text('None')),
                  for (final c in _categories) DropdownMenuItem(value: c.name, child: Text(c.name, overflow: TextOverflow.ellipsis)),
                ],
                onChanged: _busy ? null : (v) => setState(() => _category = v ?? ''),
              ),
            ],
            if (!_external)
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.folder_outlined),
                title: const Text('Save to'),
                subtitle: Text(folder, maxLines: 2, overflow: TextOverflow.ellipsis),
                trailing: TextButton(onPressed: _busy || _saveDir == null ? null : _pickFolder, child: const Text('Change')),
              ),
            SwitchListTile(contentPadding: EdgeInsets.zero, title: const Text('Start paused'), value: _paused, onChanged: _busy ? null : (v) => setState(() => _paused = v)),
            SwitchListTile(contentPadding: EdgeInsets.zero, title: const Text('Download in sequential order'), value: _sequential, onChanged: _busy ? null : (v) => setState(() => _sequential = v)),
            SwitchListTile(contentPadding: EdgeInsets.zero, title: const Text('First and last pieces first'), value: _firstLast, onChanged: _busy ? null : (v) => setState(() => _firstLast = v)),
            if (_error != null) ...[
              const SizedBox(height: Space.sm),
              Text(_error!, style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.error)),
            ],
            const SizedBox(height: Space.lg),
            FilledButton(onPressed: _busy || _count == 0 ? null : _submit, child: Text(_count > 1 ? 'Add $_count torrents' : 'Add torrent')),
          ]),
        ),
      ),
    );
  }
}
