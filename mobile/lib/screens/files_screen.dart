import 'dart:io';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import 'package:open_filex/open_filex.dart';
import 'package:path_provider/path_provider.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../models/file_entry.dart';
import '../models/dashboard_stats.dart';
import '../utils/format.dart';
import '../widgets/common.dart';

enum FileSortOption { nameAsc, nameDesc, sizeAsc, sizeDesc, dateDesc }

/// Files explorer with modern Material 3 home (Favorites + Storage Drives)
/// and nested directory browser (Breadcrumbs, Search, Grid/List view, Multi-select).
class FilesScreen extends StatefulWidget {
  const FilesScreen({super.key});

  @override
  State<FilesScreen> createState() => FilesScreenState();
}

class FilesScreenState extends State<FilesScreen> {
  static const _rootPath = '/DATA';

  bool _atHome = true;
  String _path = _rootPath;

  List<FileEntry> _rootEntries = [];
  bool _rootLoading = true;

  List<DiskUsage> _disks = [];
  bool _disksLoading = true;

  List<FileEntry> _entries = [];
  bool _loading = true;
  String? _error;
  bool _gridView = true;
  bool _searching = false;
  FileSortOption _sortOption = FileSortOption.nameAsc;
  final _searchController = TextEditingController();
  final Set<String> _busyPaths = {};
  final Set<String> _selected = {};

  bool get _selecting => _selected.isNotEmpty;

  List<FileEntry> get _visibleEntries {
    final query = _searchController.text.trim().toLowerCase();
    var list = _entries;
    if (query.isNotEmpty) {
      list = list.where((e) => e.name.toLowerCase().contains(query)).toList();
    }

    final sorted = List<FileEntry>.from(list);
    sorted.sort((a, b) {
      if (a.isDir != b.isDir) return a.isDir ? -1 : 1;
      switch (_sortOption) {
        case FileSortOption.nameAsc:
          return a.name.toLowerCase().compareTo(b.name.toLowerCase());
        case FileSortOption.nameDesc:
          return b.name.toLowerCase().compareTo(a.name.toLowerCase());
        case FileSortOption.sizeAsc:
          return a.size.compareTo(b.size);
        case FileSortOption.sizeDesc:
          return b.size.compareTo(a.size);
        case FileSortOption.dateDesc:
          final aDate = a.modified ?? DateTime.fromMillisecondsSinceEpoch(0);
          final bDate = b.modified ?? DateTime.fromMillisecondsSinceEpoch(0);
          return bDate.compareTo(aDate);
      }
    });
    return sorted;
  }

  @override
  void initState() {
    super.initState();
    _loadRoot();
    _loadDisks();
    _searchController.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    _searchController.dispose();
    super.dispose();
  }

  List<String> get _segments {
    final parts = _path.split('/').where((p) => p.isNotEmpty).toList();
    return parts;
  }

  Future<void> _loadRoot() async {
    setState(() => _rootLoading = true);
    try {
      final res = await ApiClient.instance.get('/folder', query: {'path': _rootPath});
      final data = res['data'] as Map<String, dynamic>? ?? {};
      final content = (data['content'] as List<dynamic>? ?? []);
      final entries = content.map((e) => FileEntry.fromJson(e as Map<String, dynamic>)).where((e) => e.isDir).toList();
      entries.sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
      if (!mounted) return;
      setState(() {
        _rootEntries = entries;
        _rootLoading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _rootLoading = false);
    }
  }

  Future<void> _loadDisks() async {
    setState(() => _disksLoading = true);
    try {
      final res = await ApiClient.instance.get('/sys/disks-usage');
      final data = res['data'] as List<dynamic>? ?? [];
      final disks = data
          .map((e) => DiskUsage.fromJson(e as Map<String, dynamic>))
          .where((d) => d.mountPoint.isNotEmpty && !d.isSystemPartition)
          .toList();
      if (!mounted) return;
      setState(() {
        _disks = disks;
        _disksLoading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _disksLoading = false);
    }
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final res = await ApiClient.instance.get('/folder', query: {'path': _path});
      final data = res['data'] as Map<String, dynamic>? ?? {};
      final content = (data['content'] as List<dynamic>? ?? []);
      final entries = content.map((e) => FileEntry.fromJson(e as Map<String, dynamic>)).toList();
      if (!mounted) return;
      setState(() {
        _entries = entries;
        _loading = false;
        _selected.clear();
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString().replaceFirst('Exception: ', '');
        _loading = false;
      });
    }
  }

  void _enterFolder(String path) {
    HapticFeedback.lightImpact();
    setState(() {
      _atHome = false;
      _path = path;
      _searching = false;
      _searchController.clear();
    });
    _load();
  }

  void _goHome() {
    HapticFeedback.selectionClick();
    setState(() {
      _atHome = true;
      _path = _rootPath;
      _searching = false;
      _searchController.clear();
      _selected.clear();
    });
    _loadRoot();
    _loadDisks();
  }

  bool handleBack() {
    if (_searching) {
      setState(() {
        _searching = false;
        _searchController.clear();
      });
      return true;
    }
    if (_selected.isNotEmpty) {
      setState(_selected.clear);
      return true;
    }
    if (!_atHome) {
      if (_path != _rootPath) {
        final segments = _segments;
        if (segments.length > 1) {
          segments.removeLast();
          _goToSegment(segments.length - 1);
          return true;
        }
      }
      _goHome();
      return true;
    }
    return false;
  }

  void _open(FileEntry entry) {
    if (_selecting) {
      _toggleSelect(entry);
      return;
    }
    if (entry.isDir) {
      _enterFolder(entry.path);
    } else {
      _download(entry);
    }
  }

  void _toggleSelect(FileEntry entry) {
    HapticFeedback.selectionClick();
    setState(() {
      if (_selected.contains(entry.path)) {
        _selected.remove(entry.path);
      } else {
        _selected.add(entry.path);
      }
    });
  }

  void _goToSegment(int index) {
    HapticFeedback.lightImpact();
    final parts = _segments.sublist(0, index + 1);
    setState(() => _path = '/${parts.join('/')}');
    _load();
  }

  Future<void> _download(FileEntry entry) async {
    HapticFeedback.mediumImpact();
    setState(() => _busyPaths.add(entry.path));
    try {
      final res = await ApiClient.instance.getRaw('/file', query: {'path': entry.path});
      if (res.statusCode != 200) {
        throw Exception('Download failed (HTTP ${res.statusCode}).');
      }
      final dir = await getTemporaryDirectory();
      final localFile = File('${dir.path}/${entry.name}');
      await localFile.writeAsBytes(res.bodyBytes);
      await OpenFilex.open(localFile.path);
    } catch (e) {
      if (mounted) _showError(e);
    } finally {
      if (mounted) setState(() => _busyPaths.remove(entry.path));
    }
  }

  Future<void> _upload() async {
    final result = await FilePicker.platform.pickFiles(withData: true, allowMultiple: true);
    if (result == null || result.files.isEmpty) return;

    setState(() => _busyPaths.add('__upload__'));
    HapticFeedback.mediumImpact();
    try {
      for (final picked in result.files) {
        if (picked.bytes == null) continue;
        final uri = Uri.parse('${ApiClient.instance.baseUrl}/v1/file/upload');
        final request = http.MultipartRequest('POST', uri);
        request.headers.addAll({'Authorization': await ApiClient.instance.currentAuthHeader()});
        request.fields['path'] = _path;
        request.fields['filename'] = picked.name;
        request.fields['relativePath'] = picked.name;
        request.fields['totalChunks'] = '1';
        request.fields['chunkNumber'] = '1';
        request.files.add(http.MultipartFile.fromBytes('file', picked.bytes!, filename: picked.name));
        final streamed = await request.send();
        if (streamed.statusCode != 200) {
          throw Exception('Upload failed for ${picked.name} (HTTP ${streamed.statusCode}).');
        }
      }
      await _load();
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('${result.files.length} file(s) uploaded successfully.'),
            backgroundColor: NivaroColors.success,
          ),
        );
      }
    } catch (e) {
      if (mounted) _showError(e);
    } finally {
      if (mounted) setState(() => _busyPaths.remove('__upload__'));
    }
  }

  Future<void> _createFolder() async {
    final controller = TextEditingController();
    final name = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceRaised,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
        title: const Text('New Folder'),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: const InputDecoration(
            labelText: 'Folder name',
            hintText: 'e.g. Backups, Documents',
          ),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context), child: const Text('Cancel')),
          FilledButton(
            onPressed: () => Navigator.pop(context, controller.text.trim()),
            child: const Text('Create'),
          ),
        ],
      ),
    );
    if (name == null || name.isEmpty) return;
    try {
      await ApiClient.instance.post('/folder', body: {'path': '$_path/$name'});
      await _load();
    } catch (e) {
      if (mounted) _showError(e);
    }
  }

  Future<void> _rename(FileEntry entry) async {
    final controller = TextEditingController(text: entry.name);
    final newName = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceRaised,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
        title: const Text('Rename'),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: const InputDecoration(labelText: 'New name'),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context), child: const Text('Cancel')),
          FilledButton(
            onPressed: () => Navigator.pop(context, controller.text.trim()),
            child: const Text('Rename'),
          ),
        ],
      ),
    );
    if (newName == null || newName.isEmpty || newName == entry.name) return;
    final parentPath = entry.path.substring(0, entry.path.length - entry.name.length);
    setState(() => _busyPaths.add(entry.path));
    try {
      await ApiClient.instance.put('/file/name', body: {
        'old_path': entry.path,
        'new_path': '$parentPath$newName',
      });
      await _load();
    } catch (e) {
      if (mounted) _showError(e);
    } finally {
      if (mounted) setState(() => _busyPaths.remove(entry.path));
    }
  }

  Future<void> _delete(FileEntry entry) => _deletePaths([entry.path], entry.name);

  Future<void> _deleteSelected() => _deletePaths(_selected.toList(), '${_selected.length} items');

  Future<void> _deletePaths(List<String> paths, String label) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceRaised,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
        title: const Text('Delete permanently?'),
        content: Text('Delete "$label"? This cannot be recovered.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.danger),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;

    setState(() => _busyPaths.addAll(paths));
    try {
      await ApiClient.instance.deleteWithBody('/batch', paths);
      await _load();
    } catch (e) {
      if (mounted) _showError(e);
    } finally {
      if (mounted) setState(() => _busyPaths.removeAll(paths));
    }
  }

  void _showSortSheet() {
    HapticFeedback.lightImpact();
    showModalBottomSheet(
      context: context,
      backgroundColor: NivaroColors.surfaceRaised,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(24))),
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 20, vertical: 8),
                child: Text('Sort by', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 18)),
              ),
              const Divider(height: 12),
              RadioListTile<FileSortOption>(
                value: FileSortOption.nameAsc,
                groupValue: _sortOption,
                title: const Text('Name (A to Z)'),
                onChanged: (v) {
                  setState(() => _sortOption = v!);
                  Navigator.pop(context);
                },
              ),
              RadioListTile<FileSortOption>(
                value: FileSortOption.nameDesc,
                groupValue: _sortOption,
                title: const Text('Name (Z to A)'),
                onChanged: (v) {
                  setState(() => _sortOption = v!);
                  Navigator.pop(context);
                },
              ),
              RadioListTile<FileSortOption>(
                value: FileSortOption.sizeDesc,
                groupValue: _sortOption,
                title: const Text('Size (Largest first)'),
                onChanged: (v) {
                  setState(() => _sortOption = v!);
                  Navigator.pop(context);
                },
              ),
              RadioListTile<FileSortOption>(
                value: FileSortOption.sizeAsc,
                groupValue: _sortOption,
                title: const Text('Size (Smallest first)'),
                onChanged: (v) {
                  setState(() => _sortOption = v!);
                  Navigator.pop(context);
                },
              ),
              RadioListTile<FileSortOption>(
                value: FileSortOption.dateDesc,
                groupValue: _sortOption,
                title: const Text('Date Modified (Newest first)'),
                onChanged: (v) {
                  setState(() => _sortOption = v!);
                  Navigator.pop(context);
                },
              ),
            ],
          ),
        ),
      ),
    );
  }

  void _showNewActionSheet() {
    HapticFeedback.lightImpact();
    showModalBottomSheet(
      context: context,
      backgroundColor: NivaroColors.surfaceRaised,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(24))),
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              ListTile(
                leading: Container(
                  width: 40,
                  height: 40,
                  decoration: BoxDecoration(
                    color: NivaroColors.folderAccent.withValues(alpha: 0.15),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: const Icon(Icons.create_new_folder_rounded, color: NivaroColors.folderAccent),
                ),
                title: const Text('New Folder', style: TextStyle(fontWeight: FontWeight.w600)),
                subtitle: const Text('Create a subfolder here'),
                onTap: () {
                  Navigator.pop(context);
                  _createFolder();
                },
              ),
              ListTile(
                leading: Container(
                  width: 40,
                  height: 40,
                  decoration: BoxDecoration(
                    color: NivaroColors.primary.withValues(alpha: 0.15),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: const Icon(Icons.upload_file_rounded, color: NivaroColors.primary),
                ),
                title: const Text('Upload Files', style: TextStyle(fontWeight: FontWeight.w600)),
                subtitle: const Text('Select documents, photos or archives'),
                onTap: () {
                  Navigator.pop(context);
                  _upload();
                },
              ),
            ],
          ),
        ),
      ),
    );
  }

  void _showMenu(FileEntry entry) {
    HapticFeedback.lightImpact();
    showModalBottomSheet(
      context: context,
      backgroundColor: NivaroColors.surfaceRaised,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(24))),
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 12),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              ListTile(
                leading: Container(
                  width: 44,
                  height: 44,
                  decoration: BoxDecoration(
                    color: _styleFor(entry).color.withValues(alpha: 0.15),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Icon(_styleFor(entry).glyph, color: _styleFor(entry).color),
                ),
                title: Text(entry.name, style: const TextStyle(fontWeight: FontWeight.w700), maxLines: 1, overflow: TextOverflow.ellipsis),
                subtitle: Text(entry.isDir ? 'Folder' : formatBytes(entry.size)),
              ),
              const Divider(height: 16),
              if (!entry.isDir)
                ListTile(
                  leading: const Icon(Icons.download_rounded, color: NivaroColors.primaryLight),
                  title: const Text('Download & Open'),
                  onTap: () {
                    Navigator.pop(context);
                    _download(entry);
                  },
                ),
              ListTile(
                leading: const Icon(Icons.edit_note_rounded),
                title: const Text('Rename'),
                onTap: () {
                  Navigator.pop(context);
                  _rename(entry);
                },
              ),
              ListTile(
                leading: const Icon(Icons.check_circle_outline_rounded),
                title: const Text('Select'),
                onTap: () {
                  Navigator.pop(context);
                  _toggleSelect(entry);
                },
              ),
              ListTile(
                leading: const Icon(Icons.delete_outline_rounded, color: NivaroColors.danger),
                title: const Text('Delete', style: TextStyle(color: NivaroColors.danger)),
                onTap: () {
                  Navigator.pop(context);
                  _delete(entry);
                },
              ),
            ],
          ),
        ),
      ),
    );
  }

  void _showError(Object e) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(e.toString().replaceFirst('Exception: ', '')),
        backgroundColor: NivaroColors.danger,
        behavior: SnackBarBehavior.floating,
      ),
    );
  }

  ({Color color, IconData glyph}) _styleFor(FileEntry entry) {
    if (entry.isDir) {
      return (color: NivaroColors.folderAccent, glyph: Icons.folder_rounded);
    }
    switch (entry.extension.toLowerCase()) {
      case 'jpg':
      case 'jpeg':
      case 'png':
      case 'gif':
      case 'webp':
      case 'svg':
        return (color: const Color(0xFF38BDF8), glyph: Icons.image_rounded);
      case 'mp4':
      case 'mkv':
      case 'mov':
      case 'avi':
      case 'webm':
        return (color: const Color(0xFFF43F5E), glyph: Icons.movie_rounded);
      case 'mp3':
      case 'wav':
      case 'flac':
      case 'aac':
      case 'ogg':
        return (color: const Color(0xFFA855F7), glyph: Icons.audiotrack_rounded);
      case 'pdf':
        return (color: const Color(0xFFEF4444), glyph: Icons.picture_as_pdf_rounded);
      case 'zip':
      case 'tar':
      case 'gz':
      case '7z':
      case 'rar':
        return (color: const Color(0xFFF59E0B), glyph: Icons.folder_zip_rounded);
      case 'iso':
      case 'img':
        return (color: const Color(0xFF10B981), glyph: Icons.album_rounded);
      case 'yaml':
      case 'yml':
      case 'json':
      case 'sh':
      case 'py':
      case 'js':
      case 'ts':
        return (color: const Color(0xFF6366F1), glyph: Icons.code_rounded);
      default:
        return (color: NivaroColors.fileNeutral, glyph: Icons.insert_drive_file_rounded);
    }
  }

  @override
  Widget build(BuildContext context) {
    final uploading = _busyPaths.contains('__upload__');

    return SafeArea(
      bottom: false,
      child: Stack(
        children: [
          Column(
            children: [
              _buildHeader(),
              if (!_atHome && !_selecting) _buildBreadcrumb(),
              if (uploading)
                const LinearProgressIndicator(
                  minHeight: 2.5,
                  backgroundColor: Colors.transparent,
                  valueColor: AlwaysStoppedAnimation<Color>(NivaroColors.primary),
                ),
              Expanded(child: _atHome ? _buildHome() : _buildBrowser()),
            ],
          ),

          // Floating Bottom Action Bar for Multi-select
          if (_selecting)
            Positioned(
              left: 24,
              right: 24,
              bottom: 100,
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
                decoration: BoxDecoration(
                  color: NivaroColors.surfaceRaised,
                  borderRadius: BorderRadius.circular(NivaroShape.full),
                  border: Border.all(color: NivaroColors.primary.withValues(alpha: 0.3)),
                  boxShadow: [
                    BoxShadow(
                      color: Colors.black.withValues(alpha: 0.4),
                      blurRadius: 20,
                      offset: const Offset(0, 6),
                    ),
                  ],
                ),
                child: Row(
                  children: [
                    Text(
                      '${_selected.length} selected',
                      style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14),
                    ),
                    const Spacer(),
                    IconButton(
                      icon: const Icon(Icons.select_all_rounded, size: 20),
                      tooltip: 'Select all',
                      onPressed: () {
                        setState(() {
                          if (_selected.length == _entries.length) {
                            _selected.clear();
                          } else {
                            _selected.addAll(_entries.map((e) => e.path));
                          }
                        });
                      },
                    ),
                    IconButton(
                      icon: const Icon(Icons.delete_outline_rounded, color: NivaroColors.danger, size: 20),
                      tooltip: 'Delete selected',
                      onPressed: _deleteSelected,
                    ),
                    IconButton(
                      icon: const Icon(Icons.close_rounded, size: 20),
                      tooltip: 'Clear selection',
                      onPressed: () => setState(_selected.clear),
                    ),
                  ],
                ),
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildHeader() {
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 12, 20, 0),
      child: _selecting
          ? Row(
              children: [
                IconButton(
                  icon: const Icon(Icons.close_rounded),
                  onPressed: () => setState(_selected.clear),
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    '${_selected.length} Selected',
                    style: Theme.of(context).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w700),
                  ),
                ),
                IconButton(
                  icon: const Icon(Icons.delete_outline_rounded, color: NivaroColors.danger),
                  onPressed: _deleteSelected,
                ),
              ],
            )
          : _searching
              ? Row(
                  children: [
                    Expanded(
                      child: TextField(
                        controller: _searchController,
                        autofocus: true,
                        decoration: InputDecoration(
                          hintText: _atHome ? 'Search files & drives...' : 'Search in current folder...',
                          prefixIcon: const Icon(Icons.search_rounded, size: 20),
                          filled: true,
                          fillColor: NivaroColors.surfaceRaised,
                          contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
                          border: OutlineInputBorder(
                            borderRadius: BorderRadius.circular(NivaroShape.large),
                            borderSide: const BorderSide(color: NivaroColors.borderSubtle),
                          ),
                        ),
                      ),
                    ),
                    const SizedBox(width: 8),
                    IconButton(
                      icon: const Icon(Icons.close_rounded),
                      onPressed: () => setState(() {
                        _searching = false;
                        _searchController.clear();
                      }),
                    ),
                  ],
                )
              : Row(
                  children: [
                    if (!_atHome) ...[
                      IconButton(
                        icon: const Icon(Icons.arrow_back_rounded),
                        onPressed: handleBack,
                      ),
                      const SizedBox(width: 6),
                    ],
                    Expanded(
                      child: Text(
                        _atHome ? 'Files' : _segments.isNotEmpty ? _segments.last : 'Folder',
                        style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                              fontWeight: FontWeight.w800,
                              letterSpacing: -0.5,
                            ),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                    IconButton(
                      icon: const Icon(Icons.search_rounded, size: 22),
                      tooltip: 'Search',
                      onPressed: () => setState(() => _searching = true),
                    ),
                    if (!_atHome) ...[
                      IconButton(
                        icon: const Icon(Icons.sort_rounded, size: 22),
                        tooltip: 'Sort by',
                        onPressed: _showSortSheet,
                      ),
                      IconButton(
                        icon: Icon(_gridView ? Icons.view_list_rounded : Icons.grid_view_rounded, size: 22),
                        tooltip: _gridView ? 'List view' : 'Grid view',
                        onPressed: () => setState(() => _gridView = !_gridView),
                      ),
                      const SizedBox(width: 4),
                      FilledButton.icon(
                        onPressed: _showNewActionSheet,
                        icon: const Icon(Icons.add_rounded, size: 18),
                        label: const Text('Add'),
                        style: FilledButton.styleFrom(
                          backgroundColor: NivaroColors.primary,
                          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.medium)),
                        ),
                      ),
                    ],
                  ],
                ),
    );
  }

  Widget _buildBreadcrumb() {
    return Container(
      height: 44,
      padding: const EdgeInsets.symmetric(horizontal: 16),
      child: ListView(
        scrollDirection: Axis.horizontal,
        physics: const BouncingScrollPhysics(),
        children: [
          ActionChip(
            avatar: const Icon(Icons.home_rounded, size: 16, color: NivaroColors.textMuted),
            label: const Text('Home'),
            labelStyle: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
            backgroundColor: NivaroColors.surfaceRaised,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(NivaroShape.full),
              side: const BorderSide(color: NivaroColors.borderSubtle),
            ),
            onPressed: _goHome,
          ),
          for (var i = 0; i < _segments.length; i++) ...[
            const Padding(
              padding: EdgeInsets.symmetric(horizontal: 4),
              child: Icon(Icons.chevron_right_rounded, size: 16, color: NivaroColors.textFaint),
            ),
            ActionChip(
              label: Text(_segments[i]),
              labelStyle: TextStyle(
                fontSize: 12,
                fontWeight: i == _segments.length - 1 ? FontWeight.w700 : FontWeight.w500,
                color: i == _segments.length - 1 ? NivaroColors.primaryLight : NivaroColors.textPrimary,
              ),
              backgroundColor: i == _segments.length - 1 ? NivaroColors.primary.withValues(alpha: 0.15) : NivaroColors.surfaceRaised,
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(NivaroShape.full),
                side: BorderSide(
                  color: i == _segments.length - 1 ? NivaroColors.primary.withValues(alpha: 0.4) : NivaroColors.borderSubtle,
                ),
              ),
              onPressed: () => _goToSegment(i),
            ),
          ],
        ],
      ),
    );
  }

  Widget _buildHome() {
    final query = _searchController.text.trim().toLowerCase();
    final favorites = query.isEmpty ? _rootEntries : _rootEntries.where((e) => e.name.toLowerCase().contains(query)).toList();
    final drives = query.isEmpty
        ? _disks
        : _disks.where((d) => (d.label.isNotEmpty ? d.label : d.mountPoint).toLowerCase().contains(query)).toList();

    return RefreshIndicator(
      onRefresh: () async {
        await _loadRoot();
        await _loadDisks();
      },
      color: NivaroColors.primaryLight,
      backgroundColor: NivaroColors.surfaceRaised,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(20, 16, 20, 140),
        children: [
          Row(
            children: [
              const Expanded(child: SectionHeader(title: 'Top Folders')),
              Text('${favorites.length} items', style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
            ],
          ),
          const SizedBox(height: 10),
          if (_rootLoading)
            const Padding(padding: EdgeInsets.symmetric(vertical: 24), child: Center(child: CircularProgressIndicator()))
          else if (favorites.isEmpty)
            const DarkCard(
              child: Center(
                child: Padding(
                  padding: EdgeInsets.all(20),
                  child: Text('No directories found in /DATA', style: TextStyle(color: NivaroColors.textMuted)),
                ),
              ),
            )
          else
            GridView.count(
              crossAxisCount: 2,
              shrinkWrap: true,
              physics: const NeverScrollableScrollPhysics(),
              mainAxisSpacing: 12,
              crossAxisSpacing: 12,
              childAspectRatio: 2.5,
              children: favorites.map((entry) {
                final style = _styleFor(entry);
                return FavoriteCard(
                  name: entry.name,
                  color: style.color,
                  glyph: style.glyph,
                  onTap: () => _enterFolder(entry.path),
                );
              }).toList(),
            ),
          const SizedBox(height: 28),
          Row(
            children: [
              const Expanded(child: SectionHeader(title: 'Storage Drives')),
              Text('${drives.length} active', style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
            ],
          ),
          const SizedBox(height: 10),
          if (_disksLoading)
            const Padding(padding: EdgeInsets.symmetric(vertical: 24), child: Center(child: CircularProgressIndicator()))
          else if (drives.isEmpty)
            const DarkCard(
              child: Center(
                child: Padding(
                  padding: EdgeInsets.all(20),
                  child: Text('No storage partitions reported', style: TextStyle(color: NivaroColors.textMuted)),
                ),
              ),
            )
          else
            ...drives.map((d) => Padding(
                  padding: const EdgeInsets.only(bottom: 10),
                  child: DriveCard(
                    label: d.label.isNotEmpty ? d.label : d.mountPoint,
                    percentText: d.percent,
                    fraction: d.fraction,
                    usedBytes: d.usedBytes,
                    sizeBytes: d.sizeBytes,
                    onTap: () => _enterFolder(d.mountPoint),
                  ),
                )),
        ],
      ),
    );
  }

  Widget _buildBrowser() {
    final entries = _visibleEntries;
    return RefreshIndicator(
      onRefresh: _load,
      color: NivaroColors.primaryLight,
      backgroundColor: NivaroColors.surfaceRaised,
      child: _loading
          ? const Center(child: CircularProgressIndicator())
          : _error != null
              ? ListView(
                  children: [
                    Padding(
                      padding: const EdgeInsets.all(40),
                      child: Center(
                        child: Column(
                          children: [
                            const Icon(Icons.error_outline_rounded, color: NivaroColors.danger, size: 36),
                            const SizedBox(height: 12),
                            Text(_error!, style: const TextStyle(color: NivaroColors.textMuted), textAlign: TextAlign.center),
                            const SizedBox(height: 16),
                            OutlinedButton(onPressed: _load, child: const Text('Retry')),
                          ],
                        ),
                      ),
                    ),
                  ],
                )
              : entries.isEmpty
                  ? ListView(
                      children: [
                        Padding(
                          padding: const EdgeInsets.all(60),
                          child: Center(
                            child: Column(
                              children: [
                                const Icon(Icons.folder_open_rounded, size: 48, color: NivaroColors.textFaint),
                                const SizedBox(height: 14),
                                Text(
                                  _searchController.text.trim().isNotEmpty ? 'No matches found' : 'This folder is empty',
                                  style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 16),
                                ),
                                const SizedBox(height: 6),
                                const Text('Upload files or create folders using the Add button above.', style: TextStyle(color: NivaroColors.textMuted, fontSize: 13), textAlign: TextAlign.center),
                              ],
                            ),
                          ),
                        ),
                      ],
                    )
                  : _gridView
                      ? GridView.builder(
                          padding: const EdgeInsets.fromLTRB(20, 16, 20, 140),
                          gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                            crossAxisCount: 3,
                            mainAxisSpacing: 14,
                            crossAxisSpacing: 12,
                            childAspectRatio: 0.82,
                          ),
                          itemCount: entries.length,
                          itemBuilder: (context, i) {
                            final entry = entries[i];
                            final style = _styleFor(entry);
                            return FolderTile(
                              name: entry.name,
                              color: style.color,
                              glyph: style.glyph,
                              selected: _selected.contains(entry.path),
                              onTap: () => _open(entry),
                              onLongPress: () => _toggleSelect(entry),
                              onMore: () => _showMenu(entry),
                            );
                          },
                        )
                      : ListView.separated(
                          padding: const EdgeInsets.fromLTRB(16, 8, 16, 140),
                          itemCount: entries.length,
                          separatorBuilder: (_, __) => const SizedBox(height: 8),
                          itemBuilder: (context, i) {
                            final entry = entries[i];
                            final busy = _busyPaths.contains(entry.path);
                            final selected = _selected.contains(entry.path);
                            final style = _styleFor(entry);
                            return DarkCard(
                              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
                              onTap: busy ? null : () => _open(entry),
                              child: Row(
                                children: [
                                  selected
                                      ? Container(
                                          width: 42,
                                          height: 42,
                                          decoration: const BoxDecoration(
                                            color: NivaroColors.primary,
                                            shape: BoxShape.circle,
                                          ),
                                          child: const Icon(Icons.check_rounded, color: Colors.white, size: 22),
                                        )
                                      : Container(
                                          width: 42,
                                          height: 42,
                                          decoration: BoxDecoration(
                                            color: style.color.withValues(alpha: 0.16),
                                            borderRadius: BorderRadius.circular(NivaroShape.medium),
                                          ),
                                          child: Icon(style.glyph, color: style.color, size: 22),
                                        ),
                                  const SizedBox(width: 14),
                                  Expanded(
                                    child: Column(
                                      crossAxisAlignment: CrossAxisAlignment.start,
                                      children: [
                                        Text(
                                          entry.name,
                                          style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 14.5),
                                          maxLines: 1,
                                          overflow: TextOverflow.ellipsis,
                                        ),
                                        const SizedBox(height: 2),
                                        Text(
                                          entry.isDir ? 'Folder' : '${formatBytes(entry.size)} · ${entry.extension.toUpperCase()}',
                                          style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                                        ),
                                      ],
                                    ),
                                  ),
                                  if (busy)
                                    const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2))
                                  else
                                    IconButton(
                                      icon: const Icon(Icons.more_vert_rounded, color: NivaroColors.textMuted, size: 20),
                                      onPressed: () => _showMenu(entry),
                                    ),
                                ],
                              ),
                            );
                          },
                        ),
    );
  }
}
