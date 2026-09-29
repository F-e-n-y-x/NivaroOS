import 'package:flutter/material.dart';

import '../../backup/backup_api.dart';
import '../../backup/backup_models.dart';
import '../../backup/backup_strings.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import 'backup_widgets.dart';

/// Picks a folder for a backup: a location first (a drive, a USB stick,
/// merged storage, a network share, a cloud account - GET /locations),
/// then a folder inside it (GET /locations/browse). The same pickers the
/// web's wizard and restore use, so what can be picked is what the
/// service can reach. [role] "source" also offers the server's shared
/// folders and app data as shortcuts; "dest" hides read-only places.
Future<BackupEndpoint?> pickBackupFolder(BuildContext context, {required String role, String? title, BackupApi? api}) {
  return Navigator.of(context).push<BackupEndpoint>(MaterialPageRoute(
    builder: (_) => BackupLocationPickerScreen(role: role, title: title, api: api),
  ));
}

class BackupLocationPickerScreen extends StatefulWidget {
  const BackupLocationPickerScreen({super.key, required this.role, this.title, this.api});

  final String role;
  final String? title;
  final BackupApi? api;

  @override
  State<BackupLocationPickerScreen> createState() => _BackupLocationPickerScreenState();
}

class _BackupLocationPickerScreenState extends State<BackupLocationPickerScreen> {
  late final BackupApi _api = widget.api ?? BackupApi();
  List<BackupLocation>? _locations;
  Object? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _locations = null;
      _error = null;
    });
    try {
      final l = await _api.locations(widget.role);
      if (mounted) setState(() => _locations = l);
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _open(BackupLocation l) async {
    final picked = await Navigator.of(context).push<BackupEndpoint>(MaterialPageRoute(
      builder: (_) => BackupFolderBrowserScreen(location: l, api: widget.api),
    ));
    if (picked != null && mounted) Navigator.of(context).pop(picked);
  }

  static IconData _icon(BackupLocation l) => switch (l.kind) {
        'usb' => Icons.usb_outlined,
        'smb' => Icons.lan_outlined,
        'cloud' => Icons.cloud_outlined,
        'merge' => Icons.join_full_outlined,
        _ => Icons.storage_outlined,
      };

  static String _status(BackupLocation l) {
    if (!l.online) {
      return l.lastSeen != null
          ? bt('backup.loc.status.not_connected_seen', {'at': formatRelative(l.lastSeen!)})
          : bt(l.kind == 'cloud' ? 'backup.loc.status.cloud_offline' : 'backup.loc.status.not_connected');
    }
    final free = l.free, total = l.total;
    final space = free == null
        ? bt('backup.loc.space_unknown')
        : total != null
            ? bt('backup.loc.free_of', {'free': formatSize(free), 'total': formatSize(total)})
            : bt('backup.loc.free', {'free': formatSize(free)});
    return [
      if (l.kind == 'cloud' && l.provider.isNotEmpty) bt('backup.loc.cloud_provider', {'provider': l.provider}),
      if (l.systemDisk) bt('backup.loc.system_disk'),
      space,
    ].join(' · ');
  }

  @override
  Widget build(BuildContext context) {
    final locations = _locations;
    final title = widget.title ?? bt(widget.role == 'source' ? 'backup.loc.title_source' : 'backup.loc.title_dest');
    final Widget body;
    if (locations == null && _error != null) {
      body = backupErrorState(_error!, title: "Couldn't load storage", onRetry: _load);
    } else if (locations == null) {
      body = const LoadingList(rows: 6);
    } else if (locations.isEmpty) {
      body = EmptyState(icon: Icons.storage_outlined, title: title, message: bt('backup.loc.none'));
    } else {
      final groups = <String, List<BackupLocation>>{
        'internal': locations.where((l) => l.kind == 'volume' || l.kind == 'merge').toList(),
        'usb': locations.where((l) => l.kind == 'usb').toList(),
        'network': locations.where((l) => l.kind == 'smb').toList(),
        'cloud': locations.where((l) => l.kind == 'cloud').toList(),
      };
      final presets = widget.role == 'source'
          ? [for (final l in locations) for (final p in l.presets) (l, p)]
          : const <(BackupLocation, FolderPreset)>[];
      final folders = presets.where((e) => e.$2.id.startsWith('data:')).toList();
      final apps = presets.where((e) => e.$2.id.startsWith('appdata:')).toList();
      body = ListView(
        padding: EdgeInsets.only(bottom: Space.xl + MediaQuery.paddingOf(context).bottom),
        children: [
          if (folders.isNotEmpty)
            TileGroup(title: 'Shared folders', children: [
              for (final (l, p) in folders)
                ListTile(
                  leading: const Icon(Icons.folder_outlined),
                  title: Text(p.label),
                  subtitle: Text('${l.label} › ${p.subPath}'),
                  onTap: () => Navigator.of(context).pop(BackupEndpoint(kind: l.kind, refId: l.refId, subPath: p.subPath, label: l.label, preset: p.id, match: l.match)),
                ),
            ]),
          if (apps.isNotEmpty)
            TileGroup(title: "Apps' data", children: [
              for (final (l, p) in apps)
                ListTile(
                  leading: const Icon(Icons.apps_outlined),
                  title: Text(p.label),
                  subtitle: Text('${l.label} › ${p.subPath}'),
                  onTap: () => Navigator.of(context).pop(BackupEndpoint(kind: l.kind, refId: l.refId, subPath: p.subPath, label: l.label, preset: p.id, match: l.match)),
                ),
            ]),
          for (final MapEntry(key: group, value: list) in groups.entries)
            if (list.isNotEmpty)
              TileGroup(title: bt('backup.loc.group.$group'), children: [
                for (final l in list)
                  ListTile(
                    leading: Icon(_icon(l)),
                    title: Text(l.label.isNotEmpty ? l.label : l.refId),
                    subtitle: Text(_status(l)),
                    enabled: l.online,
                    trailing: const Icon(Icons.chevron_right),
                    onTap: l.online ? () => _open(l) : null,
                  ),
              ]),
        ],
      );
    }
    return AppScaffold(title: title, body: body);
  }
}

/// The folders in a location; tap to go in, "Use this folder" to pick
/// the one on screen.
class BackupFolderBrowserScreen extends StatefulWidget {
  const BackupFolderBrowserScreen({super.key, required this.location, this.api});

  final BackupLocation location;
  final BackupApi? api;

  @override
  State<BackupFolderBrowserScreen> createState() => _BackupFolderBrowserScreenState();
}

class _BackupFolderBrowserScreenState extends State<BackupFolderBrowserScreen> {
  late final BackupApi _api = widget.api ?? BackupApi();
  String _path = '';
  BrowseResult? _result;
  Object? _error;

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
      final r = await _api.browseLocation(widget.location.endpoint(), path);
      if (mounted && path == _path) setState(() => _result = r);
    } catch (e) {
      if (mounted && path == _path) setState(() => _error = e);
    }
  }

  void _go(String path) {
    _path = path;
    _load();
  }

  String get _parent {
    final parts = _path.split('/').where((p) => p.isNotEmpty).toList();
    if (parts.isNotEmpty) parts.removeLast();
    return parts.join('/');
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final r = _result;
    final dirs = (r?.entries ?? const <BrowseEntry>[]).where((e) => e.dir).toList();
    final label = widget.location.label.isNotEmpty ? widget.location.label : widget.location.refId;
    final Widget list;
    if (r == null && _error != null) {
      list = backupErrorState(_error!, title: "Couldn't open this folder", onRetry: _load);
    } else if (r == null) {
      list = const LoadingList(rows: 8);
    } else if (dirs.isEmpty) {
      list = EmptyState(icon: Icons.folder_open_outlined, title: 'No folders here', message: 'Use this folder, or go back and choose another.');
    } else {
      list = ListView(
        children: [
          for (final d in dirs)
            ListTile(
              leading: Icon(Icons.folder_outlined, color: theme.colorScheme.primary),
              title: Text(d.name),
              trailing: const Icon(Icons.chevron_right),
              onTap: () => _go(_path.isEmpty ? d.name : '$_path/${d.name}'),
            ),
          if (r.truncated) Padding(padding: const EdgeInsets.all(Space.lg), child: Text(bt('backup.browse.truncated'))),
        ],
      );
    }
    return PopScope(
      canPop: _path.isEmpty,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _go(_parent);
      },
      child: AppScaffold(
        title: _path.isEmpty ? label : _path.split('/').last,
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(32),
          child: Align(
            alignment: AlignmentDirectional.centerStart,
            child: Padding(
              padding: EdgeInsets.fromLTRB(Space.gutter(context), 0, Space.gutter(context), Space.sm),
              child: Text(_path.isEmpty ? label : '$label › ${_path.replaceAll('/', ' › ')}',
                  maxLines: 1, overflow: TextOverflow.ellipsis, style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
            ),
          ),
        ),
        body: Column(children: [
          Expanded(child: list),
          Material(
            color: DesignTokens.of(context).cardColor,
            child: SafeArea(
              top: false,
              child: Padding(
                padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.md, Space.gutter(context), Space.md),
                child: SizedBox(
                  width: double.infinity,
                  child: FilledButton(
                    onPressed: r == null ? null : () => Navigator.of(context).pop(widget.location.endpoint(_path)),
                    child: Text(_path.isEmpty ? bt('backup.loc.use_location') : bt('backup.loc.choose_folder')),
                  ),
                ),
              ),
            ),
          ),
        ]),
      ),
    );
  }
}
