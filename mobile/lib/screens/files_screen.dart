import 'dart:io';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:open_filex/open_filex.dart';
import 'package:path_provider/path_provider.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../models/file_entry.dart';
import '../utils/format.dart';
import '../widgets/common.dart';

class FilesScreen extends StatefulWidget {
  const FilesScreen({super.key});

  @override
  State<FilesScreen> createState() => _FilesScreenState();
}

class _FilesScreenState extends State<FilesScreen> {
  String _path = '/DATA';
  List<FileEntry> _entries = [];
  bool _loading = true;
  String? _error;
  bool _gridView = true;
  final Set<String> _busyPaths = {};

  @override
  void initState() {
    super.initState();
    _load();
  }

  List<String> get _segments {
    final parts = _path.split('/').where((p) => p.isNotEmpty).toList();
    return parts;
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
      entries.sort((a, b) {
        if (a.isDir != b.isDir) return a.isDir ? -1 : 1;
        return a.name.toLowerCase().compareTo(b.name.toLowerCase());
      });
      if (!mounted) return;
      setState(() {
        _entries = entries;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString().replaceFirst('Exception: ', '');
        _loading = false;
      });
    }
  }

  void _open(FileEntry entry) {
    if (entry.isDir) {
      setState(() => _path = entry.path);
      _load();
    } else {
      _download(entry);
    }
  }

  void _goToSegment(int index) {
    final parts = _segments.sublist(0, index + 1);
    setState(() => _path = '/${parts.join('/')}');
    _load();
  }

  Future<void> _download(FileEntry entry) async {
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
    final result = await FilePicker.platform.pickFiles(withData: true);
    if (result == null || result.files.isEmpty) return;
    final picked = result.files.single;
    if (picked.bytes == null) return;

    setState(() => _busyPaths.add('__upload__'));
    try {
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
        throw Exception('Upload failed (HTTP ${streamed.statusCode}).');
      }
      await _load();
    } catch (e) {
      if (mounted) _showError(e);
    } finally {
      if (mounted) setState(() => _busyPaths.remove('__upload__'));
    }
  }

  Future<void> _delete(FileEntry entry) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Delete'),
        content: Text('Delete "${entry.name}"? This cannot be undone.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete', style: TextStyle(color: NivaroColors.danger)),
          ),
        ],
      ),
    );
    if (confirmed != true) return;

    setState(() => _busyPaths.add(entry.path));
    try {
      await ApiClient.instance.deleteWithBody('/batch', [entry.path]);
      await _load();
    } catch (e) {
      if (mounted) _showError(e);
    } finally {
      if (mounted) setState(() => _busyPaths.remove(entry.path));
    }
  }

  void _showMenu(FileEntry entry) {
    showModalBottomSheet(
      context: context,
      backgroundColor: NivaroColors.surfaceRaised,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(20))),
      builder: (context) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              title: Text(entry.name, style: const TextStyle(fontWeight: FontWeight.w700)),
              subtitle: Text(entry.isDir ? 'Folder' : formatBytes(entry.size)),
            ),
            const Divider(height: 1),
            if (!entry.isDir)
              ListTile(
                leading: const Icon(Icons.download_outlined),
                title: const Text('Download'),
                onTap: () {
                  Navigator.pop(context);
                  _download(entry);
                },
              ),
            ListTile(
              leading: const Icon(Icons.delete_outline, color: NivaroColors.danger),
              title: const Text('Delete', style: TextStyle(color: NivaroColors.danger)),
              onTap: () {
                Navigator.pop(context);
                _delete(entry);
              },
            ),
          ],
        ),
      ),
    );
  }

  void _showError(Object e) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(e.toString().replaceFirst('Exception: ', '')), backgroundColor: NivaroColors.danger),
    );
  }

  ({Color color, IconData glyph}) _styleFor(FileEntry entry) {
    if (entry.isDir) {
      final key = entry.name.toLowerCase();
      return (color: folderAccents[key] ?? folderDefaultAccent, glyph: Icons.folder_rounded);
    }
    switch (entry.extension) {
      case 'jpg':
      case 'jpeg':
      case 'png':
      case 'gif':
      case 'webp':
        return (color: const Color(0xFF8B5CF6), glyph: Icons.image_rounded);
      case 'mp4':
      case 'mkv':
      case 'mov':
        return (color: const Color(0xFFE84D8A), glyph: Icons.movie_rounded);
      case 'mp3':
      case 'wav':
      case 'flac':
        return (color: NivaroColors.info, glyph: Icons.audiotrack_rounded);
      case 'pdf':
        return (color: NivaroColors.danger, glyph: Icons.picture_as_pdf_rounded);
      case 'zip':
      case 'tar':
      case 'gz':
        return (color: NivaroColors.warning, glyph: Icons.folder_zip_rounded);
      default:
        return (color: const Color(0xFF6B7280), glyph: Icons.insert_drive_file_rounded);
    }
  }

  @override
  Widget build(BuildContext context) {
    final uploading = _busyPaths.contains('__upload__');
    return SafeArea(
      bottom: false,
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 12, 20, 0),
            child: Row(
              children: [
                const Expanded(child: Text('Files', style: nivaroTitleStyle)),
                RoundIconButton(
                  icon: _gridView ? Icons.view_list_rounded : Icons.grid_view_rounded,
                  onPressed: () => setState(() => _gridView = !_gridView),
                ),
                const SizedBox(width: 8),
                RoundIconButton(
                  icon: uploading ? Icons.hourglass_top_rounded : Icons.upload_rounded,
                  onPressed: uploading ? null : _upload,
                ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 14, 16, 0),
            child: SizedBox(
              height: 32,
              child: ListView(
                scrollDirection: Axis.horizontal,
                children: [
                  _crumb('DATA', () {
                    setState(() => _path = '/DATA');
                    _load();
                  }),
                  for (var i = 0; i < _segments.length; i++)
                    if (i > 0 || _segments[i] != 'DATA') ...[
                      const Padding(
                        padding: EdgeInsets.symmetric(horizontal: 2),
                        child: Icon(Icons.chevron_right_rounded, size: 16, color: NivaroColors.textFaint),
                      ),
                      _crumb(_segments[i], () => _goToSegment(i)),
                    ],
                ],
              ),
            ),
          ),
          Expanded(
            child: RefreshIndicator(
              onRefresh: _load,
              child: _loading
                  ? const Center(child: CircularProgressIndicator())
                  : _error != null
                      ? ListView(
                          children: [
                            Padding(
                              padding: const EdgeInsets.all(40),
                              child: Text(_error!, style: const TextStyle(color: NivaroColors.textMuted), textAlign: TextAlign.center),
                            ),
                          ],
                        )
                      : _entries.isEmpty
                          ? ListView(
                              children: const [
                                Padding(
                                  padding: EdgeInsets.all(40),
                                  child: Center(child: Text('This folder is empty.', style: TextStyle(color: NivaroColors.textMuted))),
                                ),
                              ],
                            )
                          : _gridView
                              ? GridView.builder(
                                  padding: const EdgeInsets.fromLTRB(20, 16, 20, 140),
                                  gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                                    crossAxisCount: 3,
                                    mainAxisSpacing: 16,
                                    crossAxisSpacing: 14,
                                    childAspectRatio: 0.82,
                                  ),
                                  itemCount: _entries.length,
                                  itemBuilder: (context, i) {
                                    final entry = _entries[i];
                                    final style = _styleFor(entry);
                                    return FolderTile(
                                      name: entry.name,
                                      color: style.color,
                                      glyph: style.glyph,
                                      onTap: () => _open(entry),
                                      onMore: () => _showMenu(entry),
                                    );
                                  },
                                )
                              : ListView.separated(
                                  padding: const EdgeInsets.fromLTRB(16, 8, 16, 140),
                                  itemCount: _entries.length,
                                  separatorBuilder: (_, __) => const SizedBox(height: 8),
                                  itemBuilder: (context, i) {
                                    final entry = _entries[i];
                                    final busy = _busyPaths.contains(entry.path);
                                    final style = _styleFor(entry);
                                    return DarkCard(
                                      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 6),
                                      child: ListTile(
                                        contentPadding: EdgeInsets.zero,
                                        leading: Container(
                                          width: 40,
                                          height: 40,
                                          decoration: BoxDecoration(color: style.color.withOpacity(0.18), borderRadius: BorderRadius.circular(12)),
                                          child: Icon(style.glyph, color: style.color, size: 20),
                                        ),
                                        title: Text(entry.name, maxLines: 1, overflow: TextOverflow.ellipsis),
                                        subtitle: entry.isDir ? null : Text(formatBytes(entry.size), style: const TextStyle(color: NivaroColors.textMuted)),
                                        trailing: busy
                                            ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2))
                                            : IconButton(icon: const Icon(Icons.more_vert, color: NivaroColors.textMuted), onPressed: () => _showMenu(entry)),
                                        onTap: busy ? null : () => _open(entry),
                                      ),
                                    );
                                  },
                                ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _crumb(String label, VoidCallback onTap) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(8),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 6),
        child: Text(label, style: const TextStyle(fontWeight: FontWeight.w600, color: NivaroColors.textMuted)),
      ),
    );
  }
}
