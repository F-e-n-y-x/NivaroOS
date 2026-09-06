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
      request.headers.addAll({'Authorization': await _authHeader()});
      request.fields['path'] = _path;
      request.fields['filename'] = picked.name;
      request.fields['relativePath'] = picked.name;
      request.fields['totalChunks'] = '1';
      request.fields['chunkNumber'] = '1';
      request.files.add(http.MultipartFile.fromBytes(
        'file',
        picked.bytes!,
        filename: picked.name,
      ));
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

  Future<String> _authHeader() async {
    // ApiClient keeps the token in memory only - this mirrors the same
    // value it would send itself, needed here since MultipartRequest
    // can't go through ApiClient's own get/post helpers.
    return ApiClient.instance.currentAuthHeader();
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
      // DELETE /v1/batch expects a raw JSON array body (a list of paths),
      // not query params - a distinct enough shape from every other call
      // in this app that ApiClient exposes it as its own method.
      await ApiClient.instance.deleteWithBody('/batch', [entry.path]);
      await _load();
    } catch (e) {
      if (mounted) _showError(e);
    } finally {
      if (mounted) setState(() => _busyPaths.remove(entry.path));
    }
  }

  void _showError(Object e) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(e.toString().replaceFirst('Exception: ', '')), backgroundColor: NivaroColors.danger),
    );
  }

  IconData _iconFor(FileEntry entry) {
    if (entry.isDir) return Icons.folder;
    switch (entry.extension) {
      case 'jpg':
      case 'jpeg':
      case 'png':
      case 'gif':
      case 'webp':
        return Icons.image_outlined;
      case 'mp4':
      case 'mkv':
      case 'mov':
        return Icons.movie_outlined;
      case 'mp3':
      case 'wav':
      case 'flac':
        return Icons.audiotrack_outlined;
      case 'pdf':
        return Icons.picture_as_pdf_outlined;
      case 'zip':
      case 'tar':
      case 'gz':
        return Icons.folder_zip_outlined;
      default:
        return Icons.insert_drive_file_outlined;
    }
  }

  @override
  Widget build(BuildContext context) {
    final uploading = _busyPaths.contains('__upload__');
    return Scaffold(
      appBar: AppBar(
        title: const Text('Files'),
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(40),
          child: SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.symmetric(horizontal: 12),
            child: Row(
              children: [
                _crumb('DATA', () {
                  setState(() => _path = '/DATA');
                  _load();
                }),
                for (var i = 0; i < _segments.length; i++) ...[
                  if (i > 0 || _segments[i] != 'DATA') ...[
                    const Icon(Icons.chevron_right, size: 16, color: NivaroColors.textMuted),
                    _crumb(_segments[i], () => _goToSegment(i)),
                  ],
                ],
              ],
            ),
          ),
        ),
      ),
      floatingActionButton: FloatingActionButton(
        onPressed: uploading ? null : _upload,
        child: uploading
            ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
            : const Icon(Icons.upload_outlined),
      ),
      body: RefreshIndicator(
        onRefresh: _load,
        child: _loading
            ? const Center(child: CircularProgressIndicator())
            : _error != null
                ? Center(
                    child: Padding(
                      padding: const EdgeInsets.all(24),
                      child: Text(_error!, style: const TextStyle(color: NivaroColors.textMuted), textAlign: TextAlign.center),
                    ),
                  )
                : _entries.isEmpty
                    ? const Center(child: Text('This folder is empty.', style: TextStyle(color: NivaroColors.textMuted)))
                    : ListView.separated(
                        itemCount: _entries.length,
                        separatorBuilder: (_, __) => const Divider(height: 1),
                        itemBuilder: (context, i) {
                          final entry = _entries[i];
                          final busy = _busyPaths.contains(entry.path);
                          return ListTile(
                            leading: Icon(_iconFor(entry), color: entry.isDir ? NivaroColors.primary : NivaroColors.textMuted),
                            title: Text(entry.name, maxLines: 1, overflow: TextOverflow.ellipsis),
                            subtitle: entry.isDir ? null : Text(formatBytes(entry.size)),
                            trailing: busy
                                ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2))
                                : PopupMenuButton<String>(
                                    onSelected: (value) {
                                      if (value == 'delete') _delete(entry);
                                      if (value == 'download' && !entry.isDir) _download(entry);
                                    },
                                    itemBuilder: (context) => [
                                      if (!entry.isDir) const PopupMenuItem(value: 'download', child: Text('Download')),
                                      const PopupMenuItem(value: 'delete', child: Text('Delete')),
                                    ],
                                  ),
                            onTap: busy ? null : () => _open(entry),
                          );
                        },
                      ),
      ),
    );
  }

  Widget _crumb(String label, VoidCallback onTap) {
    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 8),
        child: Text(label, style: const TextStyle(fontWeight: FontWeight.w500)),
      ),
    );
  }
}
