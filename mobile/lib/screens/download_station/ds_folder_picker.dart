import 'package:flutter/material.dart';

import '../../models/file_entry.dart';
import '../../services/api_client.dart';
import '../../services/download_station_api.dart';
import '../../ui/ui.dart';
import '../files/file_ops.dart';
import '../files/file_sheets.dart';

/// Picks where a download is saved: one of Download Station's storage
/// roots (GET /storage/roots - the only places it may write), then a
/// folder inside it from the server's file listing, like the web's folder
/// picker. Returns the absolute server path.
Future<String?> pickDownloadFolder(BuildContext context, {String? start, DownloadStationApi? api}) =>
    Navigator.of(context).push<String>(MaterialPageRoute(builder: (_) => DsFolderPickerScreen(start: start, api: api)));

class DsFolderPickerScreen extends StatefulWidget {
  const DsFolderPickerScreen({super.key, this.start, this.api});

  final String? start;
  final DownloadStationApi? api;

  @override
  State<DsFolderPickerScreen> createState() => _DsFolderPickerScreenState();
}

class _DsFolderPickerScreenState extends State<DsFolderPickerScreen> {
  late final DownloadStationApi _api = widget.api ?? DownloadStationApi();
  List<String>? _roots;

  /// '' = the list of roots.
  String _path = '';
  List<FileEntry>? _dirs;
  Object? _error;

  @override
  void initState() {
    super.initState();
    _loadRoots();
  }

  String? _rootOf(String p) => _roots?.where((r) => p == r || p.startsWith(r.endsWith('/') ? r : '$r/')).firstOrNull;

  Future<void> _loadRoots() async {
    setState(() => _error = null);
    try {
      final roots = await _api.roots();
      if (!mounted) return;
      _roots = roots;
      final start = widget.start ?? '';
      _go(_rootOf(start) != null ? start : '');
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _go(String path) async {
    setState(() {
      _path = path;
      _dirs = null;
      _error = null;
    });
    if (path.isEmpty) return;
    try {
      final res = await ApiClient.instance.get('/folder', query: {'path': path});
      final data = res['data'];
      final content = data is Map ? (data['content'] as List? ?? const []) : const [];
      final dirs = [
        for (final e in content)
          if (e is Map<String, dynamic>) FileEntry.fromJson(e),
      ].where((e) => e.isDir && !e.name.startsWith('.')).toList()
        ..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
      if (mounted && path == _path) setState(() => _dirs = dirs);
    } catch (e) {
      if (mounted && path == _path) setState(() => _error = e);
    }
  }

  void _up() {
    final root = _rootOf(_path);
    _go(_path == root ? '' : parentOf(_path));
  }

  Future<void> _newFolder() async {
    final name = await showNameDialog(context, title: 'New folder', confirmLabel: 'Create', taken: {for (final d in _dirs ?? const <FileEntry>[]) d.name});
    if (name == null || !mounted) return;
    try {
      final created = await _api.createFolder(_path, name);
      if (mounted) _go(created);
    } catch (e) {
      if (mounted) ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text("Couldn't create the folder: $e")));
    }
  }

  @override
  Widget build(BuildContext context) {
    final roots = _roots;
    final dirs = _dirs;
    final Widget list;
    if (_error != null) {
      final e = _error!;
      list = e is ApiException && e.isUnreachable
          ? ErrorState.offline(onRetry: roots == null ? _loadRoots : () => _go(_path))
          : ErrorState(title: "Couldn't open this folder", message: e.toString(), onRetry: roots == null ? _loadRoots : () => _go(_path));
    } else if (roots == null || (_path.isNotEmpty && dirs == null)) {
      list = const LoadingList(rows: 8);
    } else if (_path.isEmpty) {
      list = ListView(children: [
        for (final r in roots)
          ListTile(leading: const Icon(Icons.storage_outlined), title: Text(r), trailing: const Icon(Icons.chevron_right), onTap: () => _go(r)),
      ]);
    } else if (dirs!.isEmpty) {
      list = const EmptyState(icon: Icons.folder_open_outlined, title: 'No folders here', message: 'Save here, or make a new folder.');
    } else {
      final color = Theme.of(context).colorScheme.primary;
      list = ListView(children: [
        for (final d in dirs)
          ListTile(leading: Icon(Icons.folder_outlined, color: color), title: Text(d.name), trailing: const Icon(Icons.chevron_right), onTap: () => _go(d.path)),
      ]);
    }
    return PopScope(
      canPop: _path.isEmpty,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _up();
      },
      child: AppScaffold(
        title: _path.isEmpty ? 'Save to' : baseName(_path),
        actions: [
          if (_path.isNotEmpty) IconButton(icon: const Icon(Icons.create_new_folder_outlined), tooltip: 'New folder', onPressed: _newFolder),
        ],
        body: Column(children: [
          Expanded(child: list),
          if (_path.isNotEmpty)
            Material(
              color: DesignTokens.of(context).cardColor,
              child: SafeArea(
                top: false,
                child: Padding(
                  padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.md, Space.gutter(context), Space.md),
                  child: SizedBox(
                    width: double.infinity,
                    child: FilledButton(onPressed: () => Navigator.of(context).pop(_path), child: const Text('Save here')),
                  ),
                ),
              ),
            ),
        ]),
      ),
    );
  }
}
