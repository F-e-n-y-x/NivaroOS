import 'dart:async';
import 'dart:io';

import 'package:clock/clock.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:open_filex/open_filex.dart';

import '../models/cloud_account.dart';
import '../models/favorite_folder.dart';
import '../models/file_entry.dart';
import '../services/api_client.dart';
import '../services/device_sync_service.dart';
import '../services/permission_service.dart';
import '../services/shortcuts_service.dart';
import '../ui/ui.dart';
import '../utils/format.dart';
import 'file_viewer_screen.dart';
import 'files/file_ops.dart';
import 'files/file_sheets.dart';
import 'files/file_widgets.dart';
import 'files/transfers.dart';

/// Where this phone's own files start.
const phoneStorageRoot = '/storage/emulated/0';

/// One folder's listing and when it was fetched.
class _Listing {
  const _Listing(this.entries, this.fetched);
  final List<FileEntry> entries;
  final DateTime fetched;
}

/// What is on the Files clipboard: items picked with Copy or Move, waiting
/// for "Copy here" / "Move here" in another folder.
class _Clip {
  const _Clip(this.entries, this.isLocal, this.kind);
  final List<FileEntry> entries;
  final bool isLocal;
  final TransferKind kind;
}

/// The Files tab (design brief §1a, plan WP1-15/WP1-26), modelled on Google
/// Files: a home page of locations (server storage, favourites, phones,
/// cloud drives), then folders with the current folder as the title, a
/// breadcrumb trail, sort and view controls, long-press multi-select with
/// a contextual bar, and copy / move by picking the items and then the
/// destination. Copies and moves with name conflicts ask first.
class FilesScreen extends StatefulWidget {
  const FilesScreen({super.key, this.initialPath, this.initialIsLocal = false});

  /// Opens straight into this folder instead of the locations page
  /// (screenshots, deep links).
  final String? initialPath;
  final bool initialIsLocal;

  @override
  State<FilesScreen> createState() => FilesScreenState();
}

class FilesScreenState extends State<FilesScreen> {
  // Where we are; null path = the locations page.
  String? _path;
  bool _isLocal = false;

  final _cache = <String, _Listing>{};
  bool _loading = false;
  Object? _error;

  /// The last refresh of the folder on screen failed to reach the server;
  /// what's shown is the cached listing.
  bool _stale = false;

  // View
  FileSort _sort = FileSort.name;
  bool _ascending = true;
  bool _grid = false;
  bool _showHidden = false;
  bool _searching = false;
  final _search = TextEditingController();

  final _selected = <String>{};
  _Clip? _clip;

  // The locations page
  List<FileLocation> _storage = const [];
  bool _storageLoading = true;
  Object? _storageError;
  List<FileLocation> _phones = const [];
  FileLocation? _thisPhone;
  bool _phonesLoading = true;
  List<FileLocation> _cloud = const [];
  bool _cloudLoading = true;
  Object? _cloudError;
  List<FileLocation> _favorites = const [];
  Set<String> _customFavorites = const {};
  bool _favoritesLoading = true;
  DateTime? _homeUpdated;
  bool _homeStale = false;

  late final TransferQueue _transfers = TransferQueue(onFinished: _onTransferFinished);

  String _key(String path, bool isLocal) => '${isLocal ? 'phone' : 'server'}:$path';
  _Listing? get _listing => _path == null ? null : _cache[_key(_path!, _isLocal)];

  List<FileLocation> get _allLocations => [..._storage, ?_thisPhone, ..._phones, ..._cloud];

  FileLocation? get _location => _path == null ? null : locationFor(_path!, _allLocations, isLocal: _isLocal);

  @override
  void initState() {
    super.initState();
    _search.addListener(() => setState(() {}));
    _loadHome();
    final initial = widget.initialPath;
    if (initial != null) {
      _path = initial;
      _isLocal = widget.initialIsLocal;
      _load();
    }
  }

  @override
  void dispose() {
    _search.dispose();
    _transfers.dispose();
    super.dispose();
  }

  /// Back steps out of selection, then search, then up a folder, then to
  /// the locations page. False when there is nothing left to step out of
  /// (the shell then goes to Home).
  bool handleBack() {
    if (_selected.isNotEmpty) {
      setState(_selected.clear);
      return true;
    }
    if (_searching) {
      _closeSearch();
      return true;
    }
    if (_path != null) {
      _up();
      return true;
    }
    return false;
  }

  // -------------------------------------------------------------------------
  // Loading

  Future<void> _loadHome() async {
    await Future.wait([_loadStorage(), _loadPhones(), _loadCloud(), _loadFavorites()]);
    if (!mounted) return;
    final offline = _storageError is ApiException && (_storageError as ApiException).isUnreachable;
    setState(() {
      if (offline && _storage.isNotEmpty) {
        _homeStale = true;
      } else if (_storageError == null) {
        _homeStale = false;
        _homeUpdated = clock.now();
      }
    });
  }

  Future<void> _loadStorage() async {
    List<dynamic>? disks;
    Object? error;
    try {
      final res = await ApiClient.instance.get('/sys/disks-usage');
      disks = res['data'] as List? ?? const [];
    } catch (e) {
      error = e;
      // Older servers: the storage manager's list has the same children.
      try {
        final res = await ApiClient.instance.get('/storage');
        disks = [
          for (final d in (res['data'] as List? ?? const []).whereType<Map>())
            for (final c in (d['children'] as List? ?? const []).whereType<Map>())
              {...c, 'is_system': c['mount_point'] == '/DATA'},
        ];
        error = null;
      } catch (_) {}
    }
    List<dynamic> usb = const [];
    if (disks != null) {
      try {
        final res = await ApiClient.instance.get('/disks/usb');
        usb = res['data'] as List? ?? const [];
      } catch (_) {}
    }
    if (!mounted) return;
    setState(() {
      _storageLoading = false;
      _storageError = error;
      if (disks != null) {
        final list = storageLocations(disks, usb: usb);
        if (!list.any((l) => l.path == '/DATA')) {
          list.insert(0, const FileLocation(label: 'DATA', path: '/DATA', kind: LocationKind.storage));
        }
        _storage = list;
      }
    });
  }

  Future<void> _loadPhones() async {
    List<FileLocation> phones = const [];
    FileLocation? me;
    try {
      final devices = await DeviceSyncService.instance.listCompanionDevices();
      phones = [
        for (final d in devices)
          if (!d.isCurrentDevice)
            FileLocation(
              label: d.name,
              path: d.storagePath.isNotEmpty ? d.storagePath : '/DATA/Companion/${d.name}',
              kind: LocationKind.phone,
              detail: d.model.isEmpty ? null : d.model,
              online: d.isOnline,
            ),
      ];
      final current = devices.where((d) => d.isCurrentDevice).firstOrNull;
      me = FileLocation(
        label: 'This phone',
        path: phoneStorageRoot,
        kind: LocationKind.thisPhone,
        usedBytes: current != null && current.totalStorageBytes > 0 ? current.usedStorageBytes : null,
        totalBytes: current != null && current.totalStorageBytes > 0 ? current.totalStorageBytes : null,
      );
    } catch (_) {
      me = const FileLocation(label: 'This phone', path: phoneStorageRoot, kind: LocationKind.thisPhone);
    }
    if (!mounted) return;
    setState(() {
      _phones = phones;
      _thisPhone = me;
      _phonesLoading = false;
    });
  }

  Future<void> _loadCloud() async {
    try {
      final res = await ApiClient.instance.get('/cloud');
      final accounts = [
        for (final e in (res['data'] as List? ?? const []))
          if (e is Map<String, dynamic>) CloudAccount.fromJson(e),
      ];
      if (!mounted) return;
      setState(() {
        _cloudLoading = false;
        _cloudError = null;
        _cloud = [
          for (final a in accounts)
            FileLocation(
              label: a.displayName,
              path: a.mountPoint.isNotEmpty ? a.mountPoint : '/DATA/Cloud/${a.displayName}',
              kind: LocationKind.cloud,
              detail: a.isMounted ? a.providerTitle : '${a.providerTitle} · Not connected',
              online: a.isMounted,
            ),
        ];
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _cloudLoading = false;
        _cloudError = e;
      });
    }
  }

  Future<void> _loadFavorites() async {
    List<FavoriteFolder> all;
    Set<String> custom = const {};
    try {
      all = await ShortcutsService.instance.getAllFavorites();
      custom = {for (final f in await ShortcutsService.instance.getCustomShortcuts()) f.path};
    } catch (_) {
      all = FavoriteFolder.defaultWebUiFavorites;
    }
    if (!mounted) return;
    setState(() {
      _favoritesLoading = false;
      _customFavorites = custom;
      // DATA is already under storage; the whole-disk root is the least
      // likely place to want, so it goes last.
      _favorites = [
        for (final f in all)
          if (f.path != '/DATA' && f.path != '/') FileLocation(label: f.name, path: f.path, kind: LocationKind.favorite),
        for (final f in all)
          if (f.path == '/') FileLocation(label: f.name, path: f.path, kind: LocationKind.favorite),
      ];
    });
  }

  Future<List<FileEntry>> _listServer(String path) async {
    // Thumbnails load with the token as it is (Image.network can't retry a
    // 401), so make sure it won't run out while they do (plan M-16).
    await ApiClient.instance.freshToken();
    final res = await ApiClient.instance.get('/folder', query: {'path': path});
    final data = res['data'];
    final content = data is Map ? (data['content'] as List? ?? const []) : const [];
    return [for (final e in content) if (e is Map<String, dynamic>) FileEntry.fromJson(e)];
  }

  Future<List<FileEntry>> _listLocal(String path) async {
    final entries = <FileEntry>[];
    await for (final entity in Directory(path).list(followLinks: false)) {
      try {
        final stat = await entity.stat();
        final isDir = stat.type == FileSystemEntityType.directory;
        entries.add(FileEntry(
          name: baseName(entity.path),
          path: entity.path,
          isDir: isDir,
          size: isDir ? 0 : stat.size,
          modified: stat.modified,
        ));
      } catch (_) {}
    }
    return entries;
  }

  /// Loads the folder on screen: the cached listing shows at once, then
  /// the fresh one replaces it.
  Future<void> _load() async {
    final path = _path;
    if (path == null) return;
    final isLocal = _isLocal;
    final key = _key(path, isLocal);
    setState(() {
      _loading = _cache[key] == null;
      _error = null;
    });
    try {
      final entries = isLocal ? await _listLocal(path) : await _listServer(path);
      if (!mounted) return;
      setState(() {
        _cache[key] = _Listing(entries, clock.now());
        if (_path == path && _isLocal == isLocal) {
          _loading = false;
          _stale = false;
        }
      });
    } catch (e) {
      if (!mounted || _path != path || _isLocal != isLocal) return;
      setState(() {
        _loading = false;
        final unreachable = e is ApiException && e.isUnreachable;
        if (_cache[key] != null && unreachable) {
          _stale = true;
        } else {
          _cache.remove(key);
          _error = e;
          _stale = false;
        }
      });
    }
  }

  Future<void> _refresh() async {
    if (_path == null) {
      await _loadHome();
    } else {
      await _load();
    }
  }

  // -------------------------------------------------------------------------
  // Navigation

  void _open(String path, {required bool isLocal}) {
    setState(() {
      _path = path;
      _isLocal = isLocal;
      _selected.clear();
      _searching = false;
      _search.clear();
      _error = null;
      _stale = false;
    });
    _load();
  }

  Future<void> _openLocation(FileLocation l) async {
    if (l.isLocal) {
      final granted = await PermissionService.requestManageStorage();
      if (!granted && mounted) {
        _snack('Allow NivaroOS to access all files to browse this phone.');
      }
    }
    if (!mounted) return;
    _open(l.path, isLocal: l.isLocal);
  }

  void _goHome() {
    setState(() {
      _path = null;
      _selected.clear();
      _searching = false;
      _search.clear();
      _error = null;
    });
  }

  void _up() {
    final path = _path;
    if (path == null) return;
    final root = _location?.path;
    if (path == root || path == '/' || (root == null && parentOf(path) == path)) {
      _goHome();
      return;
    }
    _open(parentOf(path), isLocal: _isLocal);
  }

  void _closeSearch() {
    setState(() {
      _searching = false;
      _search.clear();
    });
  }

  // -------------------------------------------------------------------------
  // Entries

  List<FileEntry> get _allEntries => _listing?.entries ?? const [];

  List<FileEntry> get _visible => sortEntries(
        visibleEntries(_allEntries, showHidden: _showHidden, query: _searching ? _search.text : ''),
        _sort,
        ascending: _ascending,
      );

  void _toggle(FileEntry e) {
    setState(() {
      if (!_selected.remove(e.path)) _selected.add(e.path);
    });
  }

  void _onTap(FileEntry e) {
    if (_selected.isNotEmpty) {
      _toggle(e);
      return;
    }
    if (e.isDir) {
      _open(e.path, isLocal: _isLocal);
      return;
    }
    final gallery = e.hasThumbnail ? _visible.where((x) => x.hasThumbnail).toList() : null;
    Navigator.of(context)
        .push<bool>(MaterialPageRoute(
          builder: (_) => FileViewerScreen(file: e, path: e.path, isLocal: _isLocal, gallery: gallery),
        ))
        .then((changed) {
      if (changed == true && mounted) _load();
    });
  }

  List<FileEntry> get _selectedEntries => [for (final e in _allEntries) if (_selected.contains(e.path)) e];

  Set<String> get _names => {for (final e in _allEntries) e.name};

  // -------------------------------------------------------------------------
  // Actions

  void _snack(String message, {SnackBarAction? action, bool error = false}) {
    final messenger = ScaffoldMessenger.maybeOf(context);
    messenger?.hideCurrentSnackBar();
    messenger?.showSnackBar(SnackBar(
      content: Text(message),
      action: action,
      // An Undo stays long enough to reach, then goes by itself.
      persist: action == null ? null : false,
      duration: action == null ? const Duration(seconds: 4) : const Duration(seconds: 8),
    ));
  }

  String _plain(Object e) => e.toString().replaceFirst('Exception: ', '');

  /// The server's envelope says whether it worked even on HTTP 200.
  void _checkEnvelope(Map<String, dynamic> res) {
    final code = res['success'];
    if (code is num && code != 200) {
      final data = res['data'];
      throw Exception(data is String && data.isNotEmpty ? data : (res['message']?.toString() ?? 'The server refused ($code).'));
    }
  }

  void _clipSelected(TransferKind kind, [List<FileEntry>? items]) {
    final entries = items ?? _selectedEntries;
    if (entries.isEmpty) return;
    setState(() {
      _clip = _Clip(entries, _isLocal, kind);
      _selected.clear();
    });
  }

  Future<void> _paste() async {
    final clip = _clip;
    final dest = _path;
    if (clip == null || dest == null) return;
    final destLocal = _isLocal;
    final sameSide = clip.isLocal == destLocal;
    final sources = [for (final e in clip.entries) e.path];

    if (sameSide && clip.kind == TransferKind.move && sources.every((s) => parentOf(s) == dest)) {
      _snack('Already in this folder. Pick another folder to move to.');
      return;
    }
    if (sameSide && sources.any((s) => isWithin(dest, s))) {
      _snack("A folder can't go inside itself.");
      return;
    }

    // What's already here, freshly listed.
    Set<String> names;
    try {
      names = {for (final e in destLocal ? await _listLocal(dest) : await _listServer(dest)) e.name};
    } catch (e) {
      _snack("Couldn't check this folder: ${_plain(e)}", error: true);
      return;
    }
    if (!mounted) return;
    final conflicts = [
      for (final s in sources)
        if ((!sameSide || parentOf(s) != dest) && names.contains(baseName(s))) s,
    ];
    var choices = <String, ConflictChoice>{};
    if (conflicts.isNotEmpty) {
      final answer = await showConflictSheet(
        context,
        conflicts: conflicts,
        destName: _title,
        kind: clip.kind,
      );
      if (answer == null || !mounted) return;
      choices = answer;
    }

    final move = clip.kind == TransferKind.move;
    TransferTask task;
    if (!clip.isLocal && !destLocal) {
      final batches = planTransfer(sources, choices);
      if (batches.isEmpty) return _skippedAll();
      task = ServerTransferTask(kind: clip.kind, batches: batches, destDir: dest);
    } else {
      final taken = {...names};
      final uploads = <UploadItem>[];
      final downloads = <DownloadItem>[];
      for (final e in clip.entries) {
        final choice = choices[e.path];
        String? name;
        if (sameSide && parentOf(e.path) == dest) {
          name = uniqueName(e.name, taken, isDir: e.isDir); // a copy next to itself
        } else {
          name = resolveLocalName(e.name, taken, choice, isDir: e.isDir);
        }
        if (name == null) continue;
        taken.add(name);
        if (clip.isLocal) {
          uploads.add(UploadItem(e.path, name));
        } else {
          downloads.add(DownloadItem(e, name, replace: choice == ConflictChoice.replace));
        }
      }
      if (uploads.isEmpty && downloads.isEmpty) return _skippedAll();
      if (clip.isLocal && destLocal) {
        task = LocalTransferTask(items: uploads, destDir: dest, move: move);
      } else if (clip.isLocal) {
        task = UploadTask(items: uploads, destDir: dest, move: move);
      } else {
        task = DownloadTask(items: downloads, destDir: dest, move: move);
      }
    }
    setState(() => _clip = null);
    _transfers.add(task);
  }

  void _skippedAll() {
    setState(() => _clip = null);
    _snack('Nothing to do: every item was skipped.');
  }

  void _onTransferFinished(TransferOutcome outcome) {
    if (!mounted) return;
    for (final dir in outcome.affectedDirs) {
      // Invalidate both sides; a path only exists on one.
      if (_path != dir) {
        _cache.remove(_key(dir, true));
        _cache.remove(_key(dir, false));
      }
    }
    if (_path != null && outcome.affectedDirs.contains(_path)) _load();
    _loadStorage();
    _snack(outcome.message, error: outcome.failed);
    setState(() {});
  }

  Future<void> _upload() async {
    final dest = _path;
    if (dest == null || _isLocal) return;
    List<String> files;
    try {
      final picked = await FilePicker.pickFiles(dialogTitle: 'Upload to $_title');
      files = picked.map((f) => f.path).whereType<String>().toList();
    } catch (e) {
      _snack("Couldn't open the file picker: ${_plain(e)}", error: true);
      return;
    }
    if (files.isEmpty || !mounted) return;
    final names = _names;
    final conflicts = [for (final f in files) if (names.contains(baseName(f))) f];
    var choices = <String, ConflictChoice>{};
    if (conflicts.isNotEmpty) {
      final answer = await showConflictSheet(context, conflicts: conflicts, destName: _title, kind: TransferKind.copy);
      if (answer == null || !mounted) return;
      choices = answer;
    }
    final taken = {...names};
    final items = <UploadItem>[];
    for (final f in files) {
      final name = resolveLocalName(baseName(f), taken, choices[f]);
      if (name == null) continue;
      taken.add(name);
      items.add(UploadItem(f, name));
    }
    if (items.isEmpty) return _skippedAll();
    _transfers.add(UploadTask(items: items, destDir: dest));
  }

  Future<void> _newFolder() async {
    final dest = _path;
    if (dest == null) return;
    final name = await showNameDialog(
      context,
      title: 'New folder',
      confirmLabel: 'Create',
      initial: uniqueName('New folder', _names, isDir: true),
      taken: _names,
    );
    if (name == null) return;
    final path = joinPath(dest, name);
    try {
      if (_isLocal) {
        await Directory(path).create();
      } else {
        _checkEnvelope(await ApiClient.instance.post('/folder', body: {'path': path}));
      }
      await _load();
    } catch (e) {
      _snack("Couldn't create “$name”: ${_plain(e)}", error: true);
    }
  }

  Future<void> _rename(FileEntry e) async {
    final taken = _names..remove(e.name);
    final name = await showNameDialog(
      context,
      title: e.isDir ? 'Rename folder' : 'Rename file',
      confirmLabel: 'Rename',
      initial: e.name,
      taken: taken,
      selectStem: !e.isDir,
    );
    if (name == null || name == e.name) return;
    final target = joinPath(parentOf(e.path), name);
    try {
      if (_isLocal) {
        e.isDir ? await Directory(e.path).rename(target) : await File(e.path).rename(target);
      } else {
        _checkEnvelope(await ApiClient.instance.put('/file/name', body: {'old_path': e.path, 'new_path': target}));
      }
      setState(_selected.clear);
      await _load();
    } catch (err) {
      _snack("Couldn't rename “${e.name}”: ${_plain(err)}", error: true);
    }
  }

  Future<void> _delete(List<FileEntry> items) async {
    if (items.isEmpty) return;
    final dir = _path!;
    var toTrash = false;
    if (!_isLocal) {
      try {
        final res = await ApiClient.instance.get('/trash/support', query: {'path': dir});
        final data = res['data'];
        toTrash = data is Map && data['supported'] == true;
      } catch (_) {}
    }
    if (!mounted) return;
    final what = items.length == 1 ? '“${items.first.name}”' : '${items.length} items';
    final confirmed = toTrash
        ? await ConfirmDialog.destructive(
            context,
            title: 'Move $what to trash?',
            message: 'You can undo this right away, or restore ${items.length == 1 ? 'it' : 'them'} later from the trash in the web UI.',
            confirmLabel: 'Move to trash',
            permanent: false,
          )
        : await ConfirmDialog.destructive(
            context,
            title: 'Delete $what?',
            message: _isLocal
                ? '${items.length == 1 ? 'It is' : 'They are'} deleted from this phone.'
                : '${_location?.label ?? 'This location'} has no trash, so ${items.length == 1 ? 'it is' : 'they are'} deleted for good.',
            confirmLabel: 'Delete',
          );
    if (!confirmed || !mounted) return;
    try {
      if (_isLocal) {
        for (final e in items) {
          e.isDir ? await Directory(e.path).delete(recursive: true) : await File(e.path).delete();
        }
        setState(_selected.clear);
        await _load();
        _snack('Deleted $what');
        return;
      }
      final res = await ApiClient.instance.deleteWithBody('/batch', [for (final e in items) {'path': e.path}]);
      _checkEnvelope(res);
      final data = res['data'];
      final trashed = data is Map ? (data['trashed'] as List? ?? const []) : const [];
      final ids = [for (final t in trashed) if (t is Map && t['id'] != null) t['id'].toString()];
      final protected = data is Map ? (data['protected'] as List? ?? const []) : const [];
      setState(_selected.clear);
      await _load();
      if (protected.isNotEmpty) {
        _snack(res['message']?.toString() ?? 'Some items are drives or system folders and were left alone.');
      } else if (ids.isNotEmpty) {
        _snack('Moved $what to trash', action: SnackBarAction(label: 'Undo', onPressed: () => _restore(ids)));
      } else {
        _snack('Deleted $what');
      }
    } catch (e) {
      _snack("Couldn't delete $what: ${_plain(e)}", error: true);
      await _load();
    }
  }

  Future<void> _restore(List<String> ids) async {
    try {
      await ApiClient.instance.post('/trash/restore', body: {'ids': ids});
      await _load();
      _snack('Restored');
    } catch (e) {
      _snack("Couldn't restore: ${_plain(e)}", error: true);
    }
  }

  void _compress(List<FileEntry> items) {
    final dir = _path;
    if (dir == null || items.isEmpty) return;
    final name = defaultArchiveName([for (final e in items) e.path], _names);
    final dest = joinPath(dir, name);
    setState(_selected.clear);
    _transfers.add(ServerCallTask(
      title: 'Compressing ${describeItems([for (final e in items) e.path])}',
      call: () async {
        try {
          _checkEnvelope(await ApiClient.instance.post('/file/archive', body: {
            'files': [for (final e in items) e.path],
            'destination': dest,
          }));
          return TransferOutcome(message: 'Made “$name”', affectedDirs: {dir});
        } catch (e) {
          return TransferOutcome(message: "Couldn't compress: ${_plain(e)}", failed: true, affectedDirs: {dir});
        }
      },
    ));
  }

  void _extract(FileEntry archive) {
    final dir = _path;
    if (dir == null) return;
    final folder = defaultExtractFolder(archive.name, _names);
    final dest = joinPath(dir, folder);
    setState(_selected.clear);
    _transfers.add(ServerCallTask(
      title: 'Extracting “${archive.name}”',
      call: () async {
        try {
          _checkEnvelope(await ApiClient.instance.post('/file/unarchive', body: {'path': archive.path, 'destination': dest}));
          return TransferOutcome(message: 'Extracted to “$folder”', affectedDirs: {dir});
        } catch (e) {
          return TransferOutcome(message: "Couldn't extract “${archive.name}”: ${_plain(e)}", failed: true, affectedDirs: {dir});
        }
      },
    ));
  }

  Future<void> _toggleFavorite(FileEntry folder) async {
    try {
      await ShortcutsService.instance.toggleFavorite(folder.name, folder.path);
      final wasFavorite = _customFavorites.contains(folder.path);
      await _loadFavorites();
      _snack(wasFavorite ? 'Removed “${folder.name}” from favorites' : 'Added “${folder.name}” to favorites');
    } catch (e) {
      _snack("Couldn't update favorites: ${_plain(e)}", error: true);
    }
  }

  Future<void> _openWith(FileEntry e) async {
    if (_isLocal) {
      final res = await OpenFilex.open(e.path);
      if (res.type != ResultType.done) _snack(res.message.isEmpty ? 'No app on this phone opens this file.' : res.message);
      return;
    }
    // Opens the viewer, which downloads it and offers "Open with".
    _onTap(e);
  }

  bool _isFavoriteFolder(FileEntry e) => _favorites.any((f) => f.path == e.path);

  Future<void> _showActions(FileEntry e) async {
    final server = !_isLocal;
    final actions = <EntryAction>{
      EntryAction.select,
      if (!e.isDir && _isLocal) EntryAction.openWith,
      EntryAction.copy,
      EntryAction.move,
      EntryAction.rename,
      if (server) EntryAction.compress,
      if (server && e.isExtractable) EntryAction.extract,
      if (server && e.isDir && !_isFavoriteFolder(e)) EntryAction.favorite,
      if (server && e.isDir && _customFavorites.contains(e.path)) EntryAction.unfavorite,
      EntryAction.info,
      EntryAction.delete,
    };
    final action = await showEntryActions(context, entry: e, isLocal: _isLocal, actions: actions, deleteIsPermanent: _isLocal);
    if (action == null || !mounted) return;
    switch (action) {
      case EntryAction.open:
        _onTap(e);
      case EntryAction.select:
        _toggle(e);
      case EntryAction.copy:
        _clipSelected(TransferKind.copy, [e]);
      case EntryAction.move:
        _clipSelected(TransferKind.move, [e]);
      case EntryAction.rename:
        await _rename(e);
      case EntryAction.compress:
        _compress([e]);
      case EntryAction.extract:
        _extract(e);
      case EntryAction.favorite || EntryAction.unfavorite:
        await _toggleFavorite(e);
      case EntryAction.info:
        await showInfoSheet(context, entry: e, isLocal: _isLocal, locationLabel: _location?.label);
      case EntryAction.openWith:
        await _openWith(e);
      case EntryAction.delete:
        await _delete([e]);
    }
  }

  Future<void> _showAdd() async {
    final action = await showAddSheet(context, canUpload: !_isLocal);
    if (!mounted) return;
    switch (action) {
      case AddAction.upload:
        await _upload();
      case AddAction.newFolder:
        await _newFolder();
      case null:
        break;
    }
  }

  Future<void> _showSort() async {
    final result = await showSortSheet(context, sort: _sort, ascending: _ascending);
    if (result == null) return;
    setState(() {
      _sort = result.$1;
      _ascending = result.$2;
    });
  }

  Future<void> _showLocations() async {
    final l = await showLocationsSheet(context, locations: [..._allLocations, ..._favorites], current: _location);
    if (l != null) await _openLocation(l);
  }

  // -------------------------------------------------------------------------
  // Building

  /// The folder's name, or its location's for a location root.
  String get _title {
    final path = _path;
    if (path == null) return 'Files';
    final l = _location;
    if (l != null && l.path == path) return l.label;
    if (path == '/') return 'Root';
    return baseName(path);
  }

  @override
  Widget build(BuildContext context) {
    final banner = Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (_path == null ? _homeStale : _stale)
          OfflineBanner(lastUpdated: _path == null ? _homeUpdated : _listing?.fetched, onRetry: _refresh),
        TransferStrip(progress: _transfers.current, onCancel: _transfers.cancelCurrent, queued: _transfers.queued),
      ],
    );
    return _path == null ? _buildHome(banner) : _buildFolder(banner);
  }

  Widget? _pasteBar() {
    final clip = _clip;
    if (clip == null) return null;
    final move = clip.kind == TransferKind.move;
    final what = clip.entries.length == 1 ? '“${clip.entries.first.name}”' : '${clip.entries.length} items';
    final canPaste = _path != null && _error == null && !_loading;
    return PasteBar(
      label: _path == null ? '${move ? 'Moving' : 'Copying'} $what. Open a folder to put ${clip.entries.length == 1 ? 'it' : 'them'} in.' : '${move ? 'Moving' : 'Copying'} $what',
      actionLabel: move ? 'Move here' : 'Copy here',
      onPaste: canPaste ? _paste : null,
      onCancel: () => setState(() => _clip = null),
    );
  }

  // The locations page -------------------------------------------------------

  Widget _buildHome(Widget banner) {
    final offline = _storageError is ApiException && (_storageError as ApiException).isUnreachable;
    final nothing = _storage.isEmpty && !_storageLoading;
    return AppScaffold.slivers(
      title: 'Files',
      banner: banner,
      onRefresh: _loadHome,
      floatingActionButton: _pasteBar(),
      floatingActionButtonLocation: FloatingActionButtonLocation.centerFloat,
      slivers: [
        if (nothing && offline)
          ErrorState.offline(onRetry: _loadHome, sliver: true)
        else
          SliverList.list(children: [
            ?_summaryPanel(),
            _storageGroup(),
            _phonesGroup(),
            _cloudGroup(),
            _favoritesGroup(),
            if (_clip != null) const SizedBox(height: 96),
          ]),
      ],
    );
  }

  Widget _skeletonRows(int n) => SkeletonPulse(
        child: ExcludeSemantics(
          child: Column(
            children: [
              for (var i = 0; i < n; i++)
                SizedBox(
                  height: 72,
                  child: Padding(
                    padding: const EdgeInsets.symmetric(horizontal: Space.lg),
                    child: Row(
                      children: [
                        const SkeletonBox(width: 24, height: 24),
                        const SizedBox(width: Space.lg),
                        Expanded(
                          child: Column(
                            mainAxisAlignment: MainAxisAlignment.center,
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              FractionallySizedBox(widthFactor: i.isEven ? 0.4 : 0.55, child: const SkeletonBox(height: 14)),
                              const SizedBox(height: Space.sm),
                              const SkeletonBox(height: 8),
                            ],
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
            ],
          ),
        ),
      );

  Widget _usageTile(FileLocation l, {VoidCallback? onTap}) {
    final used = l.usedBytes, total = l.totalBytes;
    final hasUsage = used != null && total != null && total > 0;
    return ListTile(
      leading: Icon(locationIcon(l.kind)),
      title: hasUsage
          ? UsageBar(
              value: used.toDouble(),
              max: total.toDouble(),
              label: l.label,
              detail: [
                if (l.detail != null) l.detail!,
                '${formatSize(l.availableBytes ?? 0)} free of ${formatSize(total)}',
              ].join(' · '),
            )
          : Text(l.label),
      subtitle: hasUsage ? null : Text(locationSubtitle(l)),
      contentPadding: const EdgeInsetsDirectional.only(start: Space.lg, end: Space.lg, top: Space.xs, bottom: Space.xs),
      onTap: onTap ?? () => _openLocation(l),
    );
  }

  /// The page's one expressive moment: how much room the server has left,
  /// in all its drives together.
  Widget? _summaryPanel() {
    final drives = _storage.where((l) => (l.totalBytes ?? 0) > 0).toList();
    if (drives.isEmpty) return null;
    final total = drives.fold<int>(0, (s, l) => s + l.totalBytes!);
    final used = drives.fold<int>(0, (s, l) => s + (l.usedBytes ?? 0));
    final free = drives.fold<int>(0, (s, l) => s + (l.availableBytes ?? 0));
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    final (value, unit) = _splitSize(formatSize(free));
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0),
      child: Card.filled(
        color: scheme.surfaceContainerHigh,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Corners.extraLarge)),
        child: Padding(
          padding: const EdgeInsets.all(Space.lg),
          child: Semantics(
            container: true,
            label: 'Server storage: ${formatSize(free)} free of ${formatSize(total)} on ${formatCount(drives.length, 'drive')}',
            excludeSemantics: true,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text('Server storage', style: theme.textTheme.labelLarge?.copyWith(color: scheme.onSurfaceVariant)),
                const SizedBox(height: Space.xs),
                Text.rich(
                  TextSpan(children: [
                    TextSpan(text: value, style: theme.textTheme.headlineMedium?.emphasized.tabular),
                    TextSpan(text: ' $unit free', style: theme.textTheme.titleMedium?.copyWith(color: scheme.onSurfaceVariant)),
                  ]),
                ),
                Text(
                  'of ${formatSize(total)} on ${formatCount(drives.length, 'drive')}',
                  style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
                ),
                const SizedBox(height: Space.md),
                UsageBar(value: used.toDouble(), max: total.toDouble(), label: 'Used', warnAt: 0.85, criticalAt: 0.95),
              ],
            ),
          ),
        ),
      ),
    );
  }

  static (String, String) _splitSize(String formatted) {
    final i = formatted.lastIndexOf(' ');
    return i <= 0 ? (formatted, '') : (formatted.substring(0, i), formatted.substring(i + 1));
  }

  Widget _storageGroup() {
    if (_storageLoading && _storage.isEmpty) {
      return TileGroup(title: 'Server storage', children: [_skeletonRows(2)]);
    }
    if (_storage.isEmpty) {
      return TileGroup(title: 'Server storage', children: [
        ListTile(
          leading: Icon(Icons.error_outline, color: Theme.of(context).colorScheme.error),
          title: const Text("Couldn't load storage"),
          subtitle: Text(_storageError == null ? 'The server listed no drives.' : _plain(_storageError!)),
          trailing: TextButton(onPressed: _loadHome, child: const Text('Retry')),
        ),
      ]);
    }
    return TileGroup(title: 'Drives', children: [for (final l in _storage) _usageTile(l)]);
  }

  Widget _favoritesGroup() {
    if (_favoritesLoading) return TileGroup(title: 'Favorites', children: [_skeletonRows(2)]);
    if (_favorites.isEmpty) return const SizedBox.shrink();
    return TileGroup(
      title: 'Favorites',
      children: [
        for (final f in _favorites)
          ListTile(
            leading: Icon(FavoriteFolder.resolveIcon('', f.label)),
            title: Text(f.label, maxLines: 1, overflow: TextOverflow.ellipsis),
            subtitle: Text(f.path, maxLines: 1, overflow: TextOverflow.ellipsis),
            onTap: () => _openLocation(f),
          ),
      ],
    );
  }

  Widget _phonesGroup() {
    final me = _thisPhone;
    if (_phonesLoading && me == null) return TileGroup(title: 'Phones', children: [_skeletonRows(1)]);
    return TileGroup(
      title: 'Phones',
      footer: _phones.isEmpty ? 'Other phones with the NivaroOS app appear here when they share their storage.' : null,
      children: [
        if (me != null) _usageTile(me),
        for (final p in _phones)
          ListTile(
            leading: const Icon(Icons.smartphone_outlined),
            title: Text(p.label, maxLines: 1, overflow: TextOverflow.ellipsis),
            subtitle: Text(locationSubtitle(p).isEmpty ? 'Connected' : [if (p.detail != null) p.detail!, p.online ? 'Connected' : 'Offline'].join(' · ')),
            enabled: p.online,
            onTap: () => _openLocation(p),
          ),
      ],
    );
  }

  Widget _cloudGroup() {
    if (_cloudLoading) return TileGroup(title: 'Cloud drives', children: [_skeletonRows(1)]);
    if (_cloudError != null && _cloud.isEmpty) return const SizedBox.shrink();
    if (_cloud.isEmpty) {
      return const TileGroup(
        title: 'Cloud drives',
        children: [
          ListTile(
            leading: Icon(Icons.cloud_outlined),
            title: Text('No cloud drives'),
            subtitle: Text('Connect Google Drive, OneDrive, Dropbox and others in the web UI.'),
          ),
        ],
      );
    }
    return TileGroup(
      title: 'Cloud drives',
      children: [
        for (final c in _cloud)
          ListTile(
            leading: const Icon(Icons.cloud_outlined),
            title: Text(c.label, maxLines: 1, overflow: TextOverflow.ellipsis),
            subtitle: Text(c.detail ?? c.path, maxLines: 1, overflow: TextOverflow.ellipsis),
            enabled: c.online,
            onTap: () => _openLocation(c),
          ),
      ],
    );
  }

  // A folder -----------------------------------------------------------------

  PreferredSizeWidget _folderBottom(BuildContext context) {
    if (_searching) {
      final h = (MediaQuery.textScalerOf(context).scale(24) + 40).clamp(64.0, 112.0);
      return PreferredSize(
        preferredSize: Size.fromHeight(h),
        child: Padding(
          padding: EdgeInsets.fromLTRB(Space.gutter(context), 0, Space.gutter(context), Space.sm),
          child: TextField(
            controller: _search,
            autofocus: true,
            textInputAction: TextInputAction.search,
            decoration: InputDecoration(
              hintText: 'Search in $_title',
              prefixIcon: const Icon(Icons.search),
              suffixIcon: IconButton(icon: const Icon(Icons.close), tooltip: 'Close search', onPressed: _closeSearch),
            ),
          ),
        ),
      );
    }
    final l = _location;
    final crumbs = breadcrumbs(_path!, root: l?.path ?? '/', rootLabel: l?.label ?? 'Root');
    return BreadcrumbBar(
      // The folders above this one: the current folder is the title
      // already, so the trail doesn't repeat it.
      crumbs: crumbs.length > 1 ? crumbs.sublist(0, crumbs.length - 1) : crumbs,
      locationIcon: locationIcon(l?.kind ?? LocationKind.root),
      height: BreadcrumbBar.heightFor(context),
      onLocations: _showLocations,
      onCrumb: (c) => _open(c.path, isLocal: _isLocal),
    );
  }

  PreferredSizeWidget? _selectionBar() {
    if (_selected.isEmpty) return null;
    final items = _selectedEntries;
    final single = items.length == 1 ? items.first : null;
    final server = !_isLocal;
    return AppBar(
      leading: IconButton(icon: const Icon(Icons.close), tooltip: 'Clear selection', onPressed: () => setState(_selected.clear)),
      // Just the count, as in Google Photos: "12 selected" doesn't fit next
      // to four actions at 360dp.
      title: Semantics(
        liveRegion: true,
        label: '${_selected.length} selected',
        excludeSemantics: true,
        child: Text('${_selected.length}'),
      ),
      backgroundColor: Theme.of(context).colorScheme.surfaceContainer,
      actions: [
        IconButton(icon: const Icon(Icons.content_copy_outlined), tooltip: 'Copy', onPressed: () => _clipSelected(TransferKind.copy)),
        IconButton(icon: const Icon(Icons.drive_file_move_outlined), tooltip: 'Move', onPressed: () => _clipSelected(TransferKind.move)),
        IconButton(
          icon: const Icon(Icons.delete_outline),
          tooltip: _isLocal ? 'Delete' : 'Move to trash',
          onPressed: () => _delete(items),
        ),
        PopupMenuButton<String>(
          tooltip: 'More actions',
          onSelected: (v) {
            switch (v) {
              case 'all':
                setState(() => _selected.addAll(_visible.map((e) => e.path)));
              case 'rename':
                if (single != null) _rename(single);
              case 'compress':
                _compress(items);
              case 'extract':
                if (single != null) _extract(single);
              case 'info':
                if (single != null) showInfoSheet(context, entry: single, isLocal: _isLocal, locationLabel: _location?.label);
            }
          },
          itemBuilder: (context) => [
            const PopupMenuItem(value: 'all', child: Text('Select all')),
            if (single != null) const PopupMenuItem(value: 'rename', child: Text('Rename')),
            if (server) const PopupMenuItem(value: 'compress', child: Text('Compress to ZIP')),
            if (server && single != null && single.isExtractable) const PopupMenuItem(value: 'extract', child: Text('Extract here')),
            if (single != null) const PopupMenuItem(value: 'info', child: Text('Details')),
          ],
        ),
      ],
    );
  }

  List<Widget> _folderActions() {
    final path = _path!;
    final isFav = _favorites.any((f) => f.path == path);
    final canUnfav = _customFavorites.contains(path);
    return [
      IconButton(icon: const Icon(Icons.search), tooltip: 'Search in this folder', onPressed: () => setState(() => _searching = true)),
      PopupMenuButton<String>(
        tooltip: 'More options',
        onSelected: (v) {
          switch (v) {
            case 'hidden':
              setState(() => _showHidden = !_showHidden);
            case 'fav':
              _toggleFavorite(FileEntry(name: _title, path: path, isDir: true, size: 0));
            case 'folder':
              _newFolder();
            case 'refresh':
              _load();
          }
        },
        itemBuilder: (context) => [
          const PopupMenuItem(value: 'folder', child: Text('New folder')),
          CheckedPopupMenuItem(value: 'hidden', checked: _showHidden, child: const Text('Show hidden files')),
          if (!_isLocal && (!isFav || canUnfav))
            PopupMenuItem(value: 'fav', child: Text(isFav ? 'Remove from favorites' : 'Add to favorites')),
          const PopupMenuItem(value: 'refresh', child: Text('Refresh')),
        ],
      ),
    ];
  }

  Widget _buildFolder(Widget banner) {
    final entries = _visible;
    final selecting = _selected.isNotEmpty;
    Widget? fab;
    if (_clip != null) {
      fab = _pasteBar();
    } else if (!selecting && _error == null && !_loading) {
      // Labelled: a bare "+" doesn't say whether it uploads or makes a
      // folder.
      fab = _isLocal
          ? FloatingActionButton.extended(onPressed: _showAdd, icon: const Icon(Icons.create_new_folder_outlined), label: const Text('New folder'))
          : FloatingActionButton.extended(onPressed: _showAdd, icon: const Icon(Icons.add), label: const Text('New'), tooltip: 'Upload or new folder');
    }
    return AppScaffold.slivers(
      title: _title,
      collapsingTitle: false,
      leading: IconButton(icon: const Icon(Icons.arrow_back), tooltip: 'Up', onPressed: _up),
      actions: _folderActions(),
      bottom: _folderBottom(context),
      appBar: _selectionBar(),
      banner: banner,
      onRefresh: _load,
      maxContentWidth: _grid ? null : Space.readingMaxWidth,
      floatingActionButton: fab,
      floatingActionButtonLocation: _clip != null ? FloatingActionButtonLocation.centerFloat : null,
      slivers: _folderSlivers(entries, selecting),
    );
  }

  List<Widget> _folderSlivers(List<FileEntry> entries, bool selecting) {
    if (_loading && _listing == null) return [const FolderSkeleton()];
    final error = _error;
    if (error != null) {
      if (error is ApiException && error.isUnreachable) return [ErrorState.offline(onRetry: _load, sliver: true, details: error.details)];
      return [
        ErrorState(
          title: "Couldn't open “$_title”",
          message: _errorMessage(error),
          onRetry: _load,
          details: error is ApiException ? error.details : error.toString(),
          sliver: true,
        ),
      ];
    }
    final all = _allEntries;
    if (entries.isEmpty) {
      if (_searching && _search.text.trim().isNotEmpty) {
        return [
          EmptyState(
            icon: Icons.search_off,
            title: 'No matches',
            message: 'Nothing in $_title matches “${_search.text.trim()}”.',
            actionLabel: 'Clear search',
            onAction: _search.clear,
            sliver: true,
          ),
        ];
      }
      if (all.isNotEmpty && !_showHidden) {
        return [
          EmptyState(
            icon: Icons.visibility_off_outlined,
            title: 'Only hidden files here',
            message: 'Everything in this folder starts with a dot.',
            actionLabel: 'Show hidden files',
            onAction: () => setState(() => _showHidden = true),
            sliver: true,
          ),
        ];
      }
      return [
        EmptyState(
          icon: Icons.folder_open_outlined,
          title: 'This folder is empty',
          message: _isLocal ? 'Copy files here from the server, or make a folder.' : 'Upload files from this phone, or make a folder.',
          actionLabel: _isLocal ? 'New folder' : 'Upload files',
          onAction: _isLocal ? _newFolder : _upload,
          sliver: true,
        ),
      ];
    }
    final folders = entries.where((e) => e.isDir).length;
    final files = entries.length - folders;
    final free = _location?.availableBytes;
    final count = [
      [if (folders > 0) formatCount(folders, 'folder'), if (files > 0) formatCount(files, 'file')].join(', '),
      // Where it all lives, and how much room is left there.
      if (free != null) '${formatSize(free)} free',
    ].where((e) => e.isNotEmpty).join(' · ');
    return [
      SliverToBoxAdapter(
        child: SortHeader(
          sort: _sort,
          ascending: _ascending,
          grid: _grid,
          count: count,
          onSort: _showSort,
          onToggleView: () => setState(() => _grid = !_grid),
        ),
      ),
      if (_grid)
        SliverPadding(
          padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.sm, Space.gutter(context), 0),
          sliver: SliverGrid.builder(
            gridDelegate: SliverGridDelegateWithMaxCrossAxisExtent(
              maxCrossAxisExtent: 200,
              mainAxisExtent: FileGridTile.extentFor(context),
              crossAxisSpacing: Space.sm,
              mainAxisSpacing: Space.sm,
            ),
            itemCount: entries.length,
            itemBuilder: (context, i) {
              final e = entries[i];
              return FileGridTile(
                key: ValueKey(e.path),
                entry: e,
                isLocal: _isLocal,
                selected: _selected.contains(e.path),
                selecting: selecting,
                onTap: () => _onTap(e),
                onLongPress: () => selecting ? _toggle(e) : _showActionsOrSelect(e),
              );
            },
          ),
        )
      else
        SliverList.builder(
          itemCount: entries.length,
          itemBuilder: (context, i) {
            final e = entries[i];
            return FileRow(
              key: ValueKey(e.path),
              entry: e,
              isLocal: _isLocal,
              selected: _selected.contains(e.path),
              selecting: selecting,
              onTap: () => _onTap(e),
              onLongPress: () => _toggle(e),
              onMore: () => _showActions(e),
            );
          },
        ),
      // Room for the FAB or the paste bar over the last row (it grows
      // with the text), so the last row's menu can scroll clear of it.
      SliverToBoxAdapter(child: SizedBox(height: Space.xl + MediaQuery.textScalerOf(context).scale(56))),
    ];
  }

  /// Long-press in the grid starts selection, like in the list.
  void _showActionsOrSelect(FileEntry e) => _toggle(e);

  String _errorMessage(Object e) {
    if (e is FileSystemException) {
      return _isLocal ? "This phone didn't allow reading this folder. Check that NivaroOS may access all files." : e.message;
    }
    if (e is PathNotFoundException) return 'This folder no longer exists.';
    return _plain(e);
  }
}
