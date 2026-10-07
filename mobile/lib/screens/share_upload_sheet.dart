import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../models/file_entry.dart';
import '../models/server_profile.dart';
import '../services/api_client.dart';
import '../services/permission_service.dart';
import '../services/session_service.dart';
import '../services/share_intent.dart';
import '../services/share_upload.dart';
import '../services/storage_service.dart';
import '../ui/ui.dart';
import '../utils/format.dart';
import 'download_station/ds_folder_picker.dart';
import 'files/file_ops.dart';
import 'files/file_widgets.dart' show fileKindIcon;

/// "Upload to NivaroOS": the files shared from another app, the server
/// and folder they go to, and what to do with names already there.
/// Upload saves the batch and hands it to Android's job scheduler; the
/// notification follows it from there. Returns the batch once started.
///
/// With [batch] instead of [shared] (an upload's notification was
/// tapped), it shows how each file went, with Retry for the ones that
/// didn't make it.
Future<ShareBatch?> showShareUploadSheet(
  BuildContext context, {
  SharedFiles? shared,
  ShareBatch? batch,
  ShareUploadStore? store,
  Future<bool> Function(ShareBatch b)? start,
  Future<Uint8List?> Function(String uri)? thumbnail,
  Future<List<String>> Function()? roots,
}) =>
    showModalBottomSheet<ShareBatch>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (_) => ShareUploadSheet(shared: shared, batch: batch, store: store, start: start, thumbnail: thumbnail, roots: roots),
    );

/// The server's disks, for the folder picker's top level.
Future<List<String>> serverStorageRoots() async {
  try {
    final res = await ApiClient.instance.get('/sys/disks-usage');
    List<dynamic> usb = const [];
    try {
      usb = (await ApiClient.instance.get('/disks/usb'))['data'] as List? ?? const [];
    } catch (_) {}
    final roots = [for (final l in storageLocations(res['data'] as List? ?? const [], usb: usb)) l.path];
    return roots.isEmpty ? const ['/DATA'] : roots;
  } on ApiException catch (e) {
    if (e.isUnreachable) rethrow;
    return const ['/DATA'];
  }
}

class ShareUploadSheet extends StatefulWidget {
  const ShareUploadSheet({super.key, this.shared, this.batch, this.store, this.start, this.thumbnail, this.roots}) : assert(shared != null || batch != null);

  final SharedFiles? shared;
  final ShareBatch? batch;
  final ShareUploadStore? store;
  final Future<bool> Function(ShareBatch b)? start;
  final Future<Uint8List?> Function(String uri)? thumbnail;
  final Future<List<String>> Function()? roots;

  @override
  State<ShareUploadSheet> createState() => _ShareUploadSheetState();
}

class _ShareUploadSheetState extends State<ShareUploadSheet> {
  static const _defaultDir = '/DATA/Downloads';

  /// Rows a new share lists before "and N more files".
  static const _shown = 4;

  ShareBatch? _batch;
  late final List<SharedFile> _files = widget.batch?.items.map((i) => i.file).toList() ?? widget.shared!.files;
  final _thumbs = <String, Future<Uint8List?>>{};

  List<ServerProfile> _servers = const [];
  String? _serverId;
  String _dest = _defaultDir;
  String _phoneName = 'Phone';
  ConflictChoice _conflict = ConflictChoice.keepBoth;
  bool _busy = false;
  String? _error;
  Timer? _poll;

  bool get _status => _batch != null;

  @override
  void initState() {
    super.initState();
    _batch = widget.batch;
    if (_status) {
      // Follow a batch that's still running.
      _poll = Timer.periodic(const Duration(seconds: 1), (_) => _reload());
    } else {
      _loadServers();
      _loadDest();
      StorageService.instance.getCompanionDeviceName().then((n) {
        if (n != null && n.trim().isNotEmpty && mounted) setState(() => _phoneName = n.trim());
      }).ignore();
    }
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<ShareUploadStore> get _store async => widget.store ?? await ShareUploadStore.open();

  Future<void> _reload() async {
    final b = _batch;
    if (b == null || !b.hasPending) return;
    final fresh = (await _store).load(b.id);
    if (fresh != null && mounted) setState(() => _batch = fresh);
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

  Future<void> _loadDest() async {
    final last = await StorageService.instance.getShareUploadDir();
    if (mounted) setState(() => _dest = last ?? _defaultDir);
  }

  Future<void> _switchServer(String? id) async {
    final p = _servers.where((s) => s.id == id).firstOrNull;
    if (p == null || id == _serverId) return;
    setState(() => _serverId = id);
    await SessionService.switchTo(p);
    await _loadDest();
  }

  String get _serverName {
    final p = _servers.where((s) => s.id == _serverId).firstOrNull;
    return p?.displayName ?? ApiClient.displayHost(ApiClient.instance.baseUrl);
  }

  List<({String label, String path})> get _quickPicks => [
        (label: 'Downloads', path: '/DATA/Downloads'),
        (label: 'Gallery', path: '/DATA/Gallery'),
        (label: 'Documents', path: '/DATA/Documents'),
        (label: '$_phoneName uploads', path: '/DATA/Backup/$_phoneName uploads'),
      ];

  Future<void> _pickFolder() async {
    final picked = await pickServerFolder(context, start: _dest, roots: widget.roots ?? serverStorageRoots, confirmLabel: 'Upload here');
    if (picked != null && mounted) setState(() => _dest = picked);
  }

  Future<bool> _start(ShareBatch b) => (widget.start ?? ShareIntent.startUpload)(b);

  Future<void> _upload() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    // The progress shows in a notification: ask once, if never asked.
    await PermissionService.requestNotifications();
    final b = ShareBatch(
      id: ShareBatch.newId(),
      server: ApiClient.instance.baseUrl,
      serverName: _serverName,
      destDir: _dest,
      conflict: _conflict,
      items: [for (final f in _files) ShareItem(f)],
    );
    await _go(b);
    StorageService.instance.setShareUploadDir(_dest).ignore();
  }

  Future<void> _retry() async {
    final b = _batch!;
    setState(() {
      _busy = true;
      _error = null;
    });
    b.retry();
    await _go(b);
  }

  Future<void> _go(ShareBatch b) async {
    try {
      (await _store).save(b);
      if (await _start(b)) {
        if (mounted) Navigator.of(context).pop(b);
        return;
      }
      _error = "Android didn't start the upload. Try again.";
    } catch (e) {
      _error = "Couldn't start the upload: $e";
    }
    if (mounted) setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    final b = _batch;
    final rejected = widget.shared?.rejected ?? 0;
    final total = _files.fold(0, (s, f) => s + f.size);
    final String title;
    final String subtitle;
    if (b == null) {
      title = 'Upload to NivaroOS';
      subtitle = '${formatCount(_files.length, 'file')} · ${formatSize(total)}';
    } else {
      final end = b.count(ShareItemState.cancelled) > 0 && b.count(ShareItemState.failed) == 0 ? ShareRunEnd.cancelled : ShareRunEnd.finished;
      title = b.hasPending ? 'Uploading to ${b.serverName}' : shareFinalNotice(b, end).title;
      subtitle = 'To ${b.destDir} on ${b.serverName}';
    }
    final failed = b == null ? 0 : b.count(ShareItemState.failed) + b.count(ShareItemState.cancelled);
    return SafeArea(
      child: SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.lg),
        child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
          Text(title, style: theme.textTheme.titleLarge),
          const SizedBox(height: Space.xs),
          Text(subtitle, style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
          const SizedBox(height: Space.md),
          if (rejected > 0)
            Padding(
              padding: const EdgeInsets.only(bottom: Space.md),
              child: Notice(
                status: Status.warning,
                message: rejected == 1 ? "1 item couldn't be read and is left out." : "$rejected items couldn't be read and are left out.",
              ),
            ),
          if (_files.isEmpty)
            Text('Nothing here can be uploaded.', style: theme.textTheme.bodyMedium)
          else if (b == null) ...[
            // A share's first few; a big batch says how many more.
            for (final f in _files.take(_shown)) _row(context, f, null),
            if (_files.length > _shown)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: Space.sm),
                child: Text(
                  'and ${formatCount(_files.length - _shown, 'more file')} · ${formatSize(_files.skip(_shown).fold(0, (s, f) => s + f.size))}',
                  style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
                ),
              ),
          ] else
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 360),
              child: ListView.builder(
                shrinkWrap: true,
                itemCount: _files.length,
                itemBuilder: (context, n) => _row(context, _files[n], b.items[n]),
              ),
            ),
          if (b == null) ..._choices(context),
          if (_error != null) ...[
            const SizedBox(height: Space.sm),
            Text(_error!, style: theme.textTheme.bodyMedium?.copyWith(color: scheme.error)),
          ],
          const SizedBox(height: Space.lg),
          Row(children: [
            Expanded(
              child: OutlinedButton(onPressed: _busy ? null : () => Navigator.of(context).pop(), child: Text(b == null ? 'Cancel' : 'Close')),
            ),
            if (b == null || (failed > 0 && !b.hasPending)) ...[
              const SizedBox(width: Space.md),
              Expanded(
                child: FilledButton(
                  onPressed: _busy || _files.isEmpty ? null : (b == null ? _upload : _retry),
                  child: Text(b == null ? (_files.length == 1 ? 'Upload' : 'Upload ${_files.length}') : 'Retry $failed'),
                ),
              ),
            ],
          ]),
        ]),
      ),
    );
  }

  List<Widget> _choices(BuildContext context) {
    final theme = Theme.of(context);
    return [
      const SizedBox(height: Space.md),
      if (_servers.length > 1) ...[
        DropdownButtonFormField<String>(
          initialValue: _serverId,
          isExpanded: true,
          decoration: const InputDecoration(labelText: 'Server'),
          items: [for (final s in _servers) DropdownMenuItem(value: s.id, child: Text(s.displayName, overflow: TextOverflow.ellipsis))],
          onChanged: _busy ? null : _switchServer,
        ),
        const SizedBox(height: Space.sm),
      ],
      ListTile(
        contentPadding: EdgeInsets.zero,
        leading: const Icon(Icons.folder_outlined),
        title: const Text('Upload to'),
        subtitle: Text(_dest, maxLines: 2, overflow: TextOverflow.ellipsis),
        trailing: TextButton(onPressed: _busy ? null : _pickFolder, child: const Text('Change')),
      ),
      Wrap(spacing: Space.sm, runSpacing: Space.sm, children: [
        for (final q in _quickPicks)
          ChoiceChip(
            label: Text(q.label),
            selected: _dest == q.path,
            onSelected: _busy ? null : (_) => setState(() => _dest = q.path),
          ),
      ]),
      const SizedBox(height: Space.lg),
      DropdownButtonFormField<ConflictChoice>(
        initialValue: _conflict,
        isExpanded: true,
        decoration: const InputDecoration(labelText: 'If a file with the same name is there'),
        items: const [
          DropdownMenuItem(value: ConflictChoice.keepBoth, child: Text('Keep both (number the new one)', overflow: TextOverflow.ellipsis)),
          DropdownMenuItem(value: ConflictChoice.replace, child: Text('Replace it', overflow: TextOverflow.ellipsis)),
          DropdownMenuItem(value: ConflictChoice.skip, child: Text('Skip the new one', overflow: TextOverflow.ellipsis)),
        ],
        onChanged: _busy ? null : (v) => setState(() => _conflict = v ?? ConflictChoice.keepBoth),
      ),
      if (_conflict == ConflictChoice.replace)
        Padding(
          padding: const EdgeInsets.only(top: Space.xs),
          child: Text('Files already there with these names are overwritten.', style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.error)),
        ),
    ];
  }

  Widget _row(BuildContext context, SharedFile f, ShareItem? item) {
    final scheme = Theme.of(context).colorScheme;
    final (String text, Color? color, IconData? icon) = switch (item?.state) {
      null => (formatSize(f.size), null, null),
      ShareItemState.pending => ('${formatSize(f.size)} · Waiting', null, Icons.schedule),
      ShareItemState.done => ('Uploaded${item!.target != f.name ? ' as ${item.target}' : ''}', null, Icons.check_circle_outline),
      ShareItemState.skipped => ('Skipped: already there', null, Icons.skip_next_outlined),
      ShareItemState.cancelled => ('Cancelled', null, Icons.block),
      ShareItemState.failed => (item!.error ?? 'Failed', scheme.error, Icons.error_outline),
    };
    return ListTile(
      contentPadding: EdgeInsets.zero,
      leading: SizedBox.square(dimension: 40, child: _thumb(context, f)),
      title: Text(f.name, maxLines: 1, overflow: TextOverflow.ellipsis),
      subtitle: Text(text, maxLines: 3, overflow: TextOverflow.ellipsis, style: color == null ? null : TextStyle(color: color)),
      trailing: icon == null ? null : Icon(icon, color: color ?? scheme.onSurfaceVariant),
    );
  }

  Widget _thumb(BuildContext context, SharedFile f) {
    final scheme = Theme.of(context).colorScheme;
    var kind = FileEntry(name: f.name, path: '', isDir: false, size: f.size).kind;
    if (kind == FileKind.other && f.isImage) kind = FileKind.image;
    if (kind == FileKind.other && f.isVideo) kind = FileKind.video;
    final icon = Center(child: Icon(fileKindIcon(kind), color: scheme.onSurfaceVariant));
    if (!f.isImage && !f.isVideo) return icon;
    final future = _thumbs[f.uri] ??= (widget.thumbnail ?? ShareIntent.thumbnail)(f.uri);
    return FutureBuilder<Uint8List?>(
      future: future,
      builder: (context, snap) {
        final bytes = snap.data;
        if (bytes == null || bytes.isEmpty) return icon;
        return ClipRRect(
          borderRadius: BorderRadius.circular(DesignTokens.of(context).radii.sm),
          child: Image.memory(bytes, fit: BoxFit.cover, gaplessPlayback: true, errorBuilder: (_, _, _) => icon),
        );
      },
    );
  }
}
