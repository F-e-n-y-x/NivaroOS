import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:open_filex/open_filex.dart';
import 'package:path_provider/path_provider.dart';
import 'package:video_player/video_player.dart';
import 'package:chewie/chewie.dart';
import 'package:flutter_pdfview/flutter_pdfview.dart';
import 'package:flutter_svg/flutter_svg.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:url_launcher/url_launcher.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../models/file_entry.dart';
import '../utils/format.dart';
import '../widgets/common.dart';

class FileViewerScreen extends StatefulWidget {
  final FileEntry file;
  final String path;
  final bool isLocal;

  const FileViewerScreen({
    super.key,
    required this.file,
    required this.path,
    this.isLocal = false,
  });

  @override
  State<FileViewerScreen> createState() => _FileViewerScreenState();
}

class _FileViewerScreenState extends State<FileViewerScreen> with SingleTickerProviderStateMixin {
  Uint8List? _bytes;
  String? _text;
  bool _loading = true;
  double _downloadProgress = 0.0;
  int _downloadedBytes = 0;
  int _totalBytes = 0;
  String _downloadSpeed = '';
  String? _error;
  bool _savingToDevice = false;
  String? _cachedLocalPath;

  // Editor state
  bool _isEditing = false;
  bool _isSaving = false;
  final TextEditingController _textController = TextEditingController();

  // Mode toggles
  bool _wrapCode = true;
  bool _showFormattedJson = true;
  bool _showMarkdownPreview = true;
  bool _showCsvTable = true;
  String _codeSearchQuery = '';
  final TextEditingController _codeSearchController = TextEditingController();
  bool _isSearchingCode = false;

  // Image viewer state
  int _imageBgMode = 0; // 0: Dark Obsidian, 1: Pure Black, 2: Pure White, 3: Checkerboard
  final TransformationController _imageTransformController = TransformationController();

  // Video Player state
  VideoPlayerController? _videoPlayerController;
  ChewieController? _chewieController;
  bool _videoLoading = false;
  String? _videoError;

  // PDF Viewer state
  PDFViewController? _pdfViewController;
  int _pdfTotalPages = 0;
  int _pdfCurrentPage = 1;
  bool _pdfReady = false;
  String? _pdfError;

  // Audio Player state
  VideoPlayerController? _audioPlayerController;
  bool _audioPlaying = false;
  Duration _audioPosition = Duration.zero;
  Duration _audioDuration = Duration.zero;
  double _audioSpeed = 1.0;
  bool _audioLooping = false;
  late AnimationController _discAnimController;

  // CSV parsed data
  List<List<String>> _csvRows = [];
  String _csvFilter = '';
  final TextEditingController _csvFilterController = TextEditingController();

  @override
  void initState() {
    super.initState();
    _discAnimController = AnimationController(
      vsync: this,
      duration: const Duration(seconds: 4),
    );
    _loadFile();
  }

  @override
  void dispose() {
    _discAnimController.dispose();
    _textController.dispose();
    _codeSearchController.dispose();
    _csvFilterController.dispose();
    _imageTransformController.dispose();
    _chewieController?.dispose();
    _videoPlayerController?.dispose();
    _audioPlayerController?.dispose();
    super.dispose();
  }

  Future<void> _loadFile() async {
    setState(() {
      _loading = true;
      _downloadProgress = 0.0;
      _downloadedBytes = 0;
      _totalBytes = widget.file.size > 0 ? widget.file.size : 0;
      _downloadSpeed = '';
      _error = null;
    });

    try {
      String localPath;
      Uint8List? bytes;

      if (widget.isLocal) {
        final localFile = File(widget.path);
        if (!await localFile.exists()) {
          throw Exception('File not found at ${widget.path}');
        }
        localPath = widget.path;
        final len = await localFile.length();
        _totalBytes = len;
        if (len < 25 * 1024 * 1024) {
          bytes = await localFile.readAsBytes();
        }
      } else {
        // Stream download with progress to temporary cached file
        final tempDir = await getTemporaryDirectory();
        final safeName = widget.file.name.replaceAll(RegExp(r'[^a-zA-Z0-9._-]'), '_');
        final tempFile = File('${tempDir.path}/$safeName');

        // Check if valid cache exists matching size
        if (await tempFile.exists() && widget.file.size > 0 && await tempFile.length() == widget.file.size) {
          localPath = tempFile.path;
          if (widget.file.size < 25 * 1024 * 1024) {
            bytes = await tempFile.readAsBytes();
          }
        } else {
          await ApiClient.instance.downloadFileStream(
            widget.path,
            tempFile,
            onProgress: (received, total, speed) {
              if (mounted) {
                setState(() {
                  _downloadedBytes = received;
                  _totalBytes = total > 0 ? total : widget.file.size;
                  _downloadProgress = _totalBytes > 0 ? (received / _totalBytes).clamp(0.0, 1.0) : 0.0;
                  _downloadSpeed = speed > 1024 * 1024
                      ? '${(speed / (1024 * 1024)).toStringAsFixed(1)} MB/s'
                      : '${(speed / 1024).toStringAsFixed(0)} KB/s';
                });
              }
            },
          );
          localPath = tempFile.path;
          if (await tempFile.length() < 25 * 1024 * 1024) {
            bytes = await tempFile.readAsBytes();
          }
        }
      }

      String? text;
      if (widget.file.isText || widget.file.isMarkdown || widget.file.isJson || widget.file.isCsv || widget.file.isCode) {
        if (bytes != null) {
          try {
            text = utf8.decode(bytes);
          } catch (_) {
            try {
              text = latin1.decode(bytes);
            } catch (_) {}
          }
        } else {
          try {
            final f = File(localPath);
            text = await f.readAsString();
          } catch (_) {}
        }
      }

      if (text != null) {
        _textController.text = text;
        if (widget.file.isCsv) {
          _parseCsv(text);
        }
      }

      if (mounted) {
        setState(() {
          _bytes = bytes;
          _text = text;
          _cachedLocalPath = localPath;
          _loading = false;
        });

        // Initialize dedicated players
        if (widget.file.isVideo) {
          _initVideoPlayer(localPath);
        } else if (widget.file.isAudio) {
          _initAudioPlayer(localPath);
        }
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e.toString().replaceFirst('Exception: ', '');
          _loading = false;
        });
      }
    }
  }

  void _parseCsv(String content) {
    try {
      final lines = const LineSplitter().convert(content);
      final rows = <List<String>>[];
      for (final line in lines) {
        if (line.trim().isEmpty) continue;
        final row = <String>[];
        var insideQuotes = false;
        var current = StringBuffer();
        final delimiter = widget.file.extension == 'tsv' ? '	' : ',';

        for (int i = 0; i < line.length; i++) {
          final char = line[i];
          if (char == '"') {
            insideQuotes = !insideQuotes;
          } else if (char == delimiter && !insideQuotes) {
            row.add(current.toString().trim());
            current = StringBuffer();
          } else {
            current.write(char);
          }
        }
        row.add(current.toString().trim());
        rows.add(row);
      }
      _csvRows = rows;
    } catch (_) {
      _csvRows = [];
    }
  }

  Future<void> _initVideoPlayer(String filePath) async {
    try {
      setState(() {
        _videoLoading = true;
        _videoError = null;
      });

      final vController = VideoPlayerController.file(File(filePath));
      await vController.initialize();

      final double aspect = (vController.value.aspectRatio > 0 &&
              !vController.value.aspectRatio.isInfinite &&
              !vController.value.aspectRatio.isNaN)
          ? vController.value.aspectRatio
          : 16 / 9;

      final cController = ChewieController(
        videoPlayerController: vController,
        autoPlay: true,
        looping: false,
        aspectRatio: aspect,
        allowFullScreen: true,
        allowMuting: true,
        allowPlaybackSpeedChanging: true,
        playbackSpeeds: const [0.5, 0.75, 1.0, 1.25, 1.5, 2.0],
        materialProgressColors: ChewieProgressColors(
          playedColor: NivaroColors.primaryLight,
          handleColor: NivaroColors.primaryLight,
          backgroundColor: Colors.white24,
          bufferedColor: Colors.white38,
        ),
        errorBuilder: (context, errorMessage) {
          return Center(
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Icon(Icons.movie_filter_rounded, color: NivaroColors.dangerLight, size: 44),
                  const SizedBox(height: 12),
                  Text(
                    'Built-in video decoder notice: $errorMessage',
                    style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13),
                    textAlign: TextAlign.center,
                  ),
                  const SizedBox(height: 16),
                  FilledButton.icon(
                    onPressed: _openWithExternalApp,
                    style: FilledButton.styleFrom(backgroundColor: NivaroColors.primary),
                    icon: const Icon(Icons.open_in_new_rounded, size: 18),
                    label: const Text('Play with VLC / MX Player'),
                  ),
                ],
              ),
            ),
          );
        },
      );

      if (mounted) {
        setState(() {
          _videoPlayerController = vController;
          _chewieController = cController;
          _videoLoading = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _videoError = e.toString().replaceFirst('Exception: ', '');
          _videoLoading = false;
        });
      }
    }
  }

  Future<void> _initAudioPlayer(String filePath) async {
    try {
      final aController = VideoPlayerController.file(File(filePath));
      await aController.initialize();
      aController.addListener(() {
        if (mounted && _audioPlayerController != null) {
          final isPlaying = _audioPlayerController!.value.isPlaying;
          if (isPlaying && !_discAnimController.isAnimating) {
            _discAnimController.repeat();
          } else if (!isPlaying && _discAnimController.isAnimating) {
            _discAnimController.stop();
          }

          setState(() {
            _audioPosition = _audioPlayerController!.value.position;
            _audioDuration = _audioPlayerController!.value.duration;
            _audioPlaying = isPlaying;
          });
        }
      });

      if (mounted) {
        setState(() {
          _audioPlayerController = aController;
          _audioDuration = aController.value.duration;
        });
      }
    } catch (_) {}
  }

  Future<void> _saveFile() async {
    if (_isSaving) return;
    HapticFeedback.mediumImpact();
    setState(() => _isSaving = true);
    try {
      final newText = _textController.text;
      if (widget.isLocal) {
        final localFile = File(widget.path);
        await localFile.writeAsString(newText);
        if (mounted) {
          setState(() {
            _text = newText;
            _bytes = Uint8List.fromList(utf8.encode(newText));
            _isEditing = false;
            if (widget.file.isCsv) _parseCsv(newText);
          });
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(
              content: Text('Local file saved successfully!'),
              backgroundColor: NivaroColors.success,
            ),
          );
        }
      } else {
        await ApiClient.instance.put('/file', body: {
          'path': widget.path,
          'content': newText,
        });
        if (mounted) {
          setState(() {
            _text = newText;
            _bytes = Uint8List.fromList(utf8.encode(newText));
            _isEditing = false;
            if (widget.file.isCsv) _parseCsv(newText);
          });
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(
              content: Text('File saved successfully on server!'),
              backgroundColor: NivaroColors.success,
            ),
          );
        }
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Failed to save file: $e'), backgroundColor: NivaroColors.danger),
        );
      }
    } finally {
      if (mounted) setState(() => _isSaving = false);
    }
  }

  Future<void> _saveToDeviceDownloads() async {
    if (_savingToDevice) return;
    HapticFeedback.mediumImpact();
    setState(() => _savingToDevice = true);
    try {
      Directory? dir;
      if (Platform.isAndroid) {
        dir = Directory('/storage/emulated/0/Download');
        if (!await dir.exists()) {
          dir = await getExternalStorageDirectory();
        }
      } else {
        dir = await getApplicationDocumentsDirectory();
      }

      if (dir == null) throw Exception('Unable to access device storage');
      final targetFile = File('${dir.path}/${widget.file.name}');

      if (_cachedLocalPath != null) {
        await File(_cachedLocalPath!).copy(targetFile.path);
      } else if (_bytes != null) {
        await targetFile.writeAsBytes(_bytes!);
      } else {
        throw Exception('No file data to export');
      }

      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Saved "${widget.file.name}" to Downloads'),
            action: SnackBarAction(
              label: 'Open',
              textColor: NivaroColors.primaryLight,
              onPressed: () => OpenFilex.open(targetFile.path),
            ),
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Failed to save: $e'), backgroundColor: NivaroColors.danger),
        );
      }
    } finally {
      if (mounted) setState(() => _savingToDevice = false);
    }
  }

  Future<void> _openWithExternalApp() async {
    HapticFeedback.lightImpact();
    if (_cachedLocalPath != null) {
      final res = await OpenFilex.open(_cachedLocalPath!);
      if (res.type != ResultType.done && mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('External App: ${res.message}')),
        );
      }
    } else {
      await _saveToDeviceDownloads();
    }
  }

  void _showJumpToPageDialog() {
    if (_pdfTotalPages <= 1 || _pdfViewController == null) return;
    final textEdit = TextEditingController(text: '$_pdfCurrentPage');
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: NivaroColors.surfaceDim,
        title: const Text('Jump to Page', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text('Enter page number (1 - $_pdfTotalPages):', style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13)),
            const SizedBox(height: 12),
            TextField(
              controller: textEdit,
              keyboardType: TextInputType.number,
              autofocus: true,
              decoration: const InputDecoration(
                filled: true,
                fillColor: NivaroColors.surfaceContainerLowest,
                border: OutlineInputBorder(),
                isDense: true,
              ),
            ),
          ],
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('Cancel')),
          FilledButton(
            onPressed: () {
              final page = int.tryParse(textEdit.text.trim());
              if (page != null && page >= 1 && page <= _pdfTotalPages) {
                _pdfViewController?.setPage(page - 1);
              }
              Navigator.pop(ctx);
            },
            child: const Text('Go'),
          ),
        ],
      ),
    );
  }

  Color _getCategoryColor() {
    if (widget.file.isVideo) return NivaroColors.purple;
    if (widget.file.isAudio) return NivaroColors.cyan;
    if (widget.file.isPdf) return NivaroColors.danger;
    if (widget.file.isImage) return NivaroColors.info;
    if (widget.file.isMarkdown) return NivaroColors.primary;
    if (widget.file.isJson) return NivaroColors.warning;
    if (widget.file.isCsv) return NivaroColors.success;
    if (widget.file.isArchive) return const Color(0xFFF97316);
    if (widget.file.isOffice) return const Color(0xFF2563EB);
    return NivaroColors.primary;
  }

  IconData _getCategoryIcon() {
    if (widget.file.isVideo) return Icons.movie_rounded;
    if (widget.file.isAudio) return Icons.music_note_rounded;
    if (widget.file.isPdf) return Icons.picture_as_pdf_rounded;
    if (widget.file.isImage) return widget.file.isSvg ? Icons.polyline_rounded : Icons.image_rounded;
    if (widget.file.isMarkdown) return Icons.article_rounded;
    if (widget.file.isJson) return Icons.data_object_rounded;
    if (widget.file.isCsv) return Icons.table_chart_rounded;
    if (widget.file.isCode) return Icons.code_rounded;
    if (widget.file.isArchive) return Icons.folder_zip_rounded;
    if (widget.file.isOffice) return Icons.description_rounded;
    return Icons.insert_drive_file_rounded;
  }

  @override
  Widget build(BuildContext context) {
    final catColor = _getCategoryColor();

    return Scaffold(
      backgroundColor: NivaroColors.background,
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              widget.file.name,
              style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 15),
              overflow: TextOverflow.ellipsis,
            ),
            Text(
              '${widget.isLocal ? "Device Storage" : "NivaroOS Server"} · ${formatBytes(widget.file.size)} · ${widget.file.categoryLabel}',
              style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11),
            ),
          ],
        ),
        actions: [
          // Text / Code Edit Mode Toggle
          if (_text != null) ...[
            if (!_isEditing) ...[
              if (widget.file.isMarkdown)
                IconButton(
                  icon: Icon(_showMarkdownPreview ? Icons.code_rounded : Icons.preview_rounded, size: 22),
                  tooltip: _showMarkdownPreview ? 'View Raw Markdown' : 'Preview Rendered Markdown',
                  onPressed: () => setState(() => _showMarkdownPreview = !_showMarkdownPreview),
                ),
              if (widget.file.isJson)
                IconButton(
                  icon: Icon(_showFormattedJson ? Icons.raw_on_rounded : Icons.data_object_rounded, size: 22),
                  tooltip: _showFormattedJson ? 'View Raw JSON' : 'View Formatted JSON',
                  onPressed: () => setState(() => _showFormattedJson = !_showFormattedJson),
                ),
              if (widget.file.isCsv)
                IconButton(
                  icon: Icon(_showCsvTable ? Icons.notes_rounded : Icons.table_chart_rounded, size: 22),
                  tooltip: _showCsvTable ? 'View Raw Text' : 'View Data Table',
                  onPressed: () => setState(() => _showCsvTable = !_showCsvTable),
                ),
              if (widget.file.isCode || widget.file.isText) ...[
                IconButton(
                  icon: Icon(_isSearchingCode ? Icons.search_off_rounded : Icons.search_rounded, size: 22),
                  tooltip: _isSearchingCode ? 'Close search' : 'Search in code',
                  onPressed: () => setState(() {
                    _isSearchingCode = !_isSearchingCode;
                    if (!_isSearchingCode) {
                      _codeSearchController.clear();
                      _codeSearchQuery = '';
                    }
                  }),
                ),
                IconButton(
                  icon: Icon(_wrapCode ? Icons.wrap_text_rounded : Icons.menu_open_rounded, size: 22),
                  tooltip: _wrapCode ? 'Disable Word Wrap' : 'Enable Word Wrap',
                  onPressed: () => setState(() => _wrapCode = !_wrapCode),
                ),
              ],
              IconButton(
                icon: const Icon(Icons.edit_note_rounded, size: 24, color: NivaroColors.primaryLight),
                tooltip: 'Edit text file',
                onPressed: () => setState(() {
                  _isEditing = true;
                  _textController.text = _text ?? '';
                }),
              ),
            ] else ...[
              IconButton(
                icon: _isSaving
                    ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                    : const Icon(Icons.save_rounded, size: 22, color: NivaroColors.successLight),
                tooltip: 'Save changes',
                onPressed: _isSaving ? null : _saveFile,
              ),
              IconButton(
                icon: const Icon(Icons.close_rounded, size: 22),
                tooltip: 'Cancel editing',
                onPressed: () => setState(() {
                  _isEditing = false;
                  _textController.text = _text ?? '';
                }),
              ),
            ],
          ],

          // Image Background switcher
          if (widget.file.isImage)
            IconButton(
              icon: const Icon(Icons.palette_outlined, size: 22),
              tooltip: 'Switch background',
              onPressed: () => setState(() => _imageBgMode = (_imageBgMode + 1) % 4),
            ),

          // PDF Jump To Page
          if (widget.file.isPdf && _pdfTotalPages > 1)
            IconButton(
              icon: const Icon(Icons.find_in_page_rounded, size: 22),
              tooltip: 'Jump to Page',
              onPressed: _showJumpToPageDialog,
            ),

          // Save to device downloads
          if (!widget.isLocal)
            IconButton(
              icon: const Icon(Icons.download_rounded, size: 22),
              tooltip: 'Save to Downloads',
              onPressed: _savingToDevice ? null : _saveToDeviceDownloads,
            ),

          // Open with native app
          IconButton(
            icon: const Icon(Icons.open_in_new_rounded, size: 20),
            tooltip: 'Open with native app',
            onPressed: _openWithExternalApp,
          ),

          // Copy path
          IconButton(
            icon: const Icon(Icons.copy_rounded, size: 20),
            tooltip: 'Copy path',
            onPressed: () {
              Clipboard.setData(ClipboardData(text: widget.path));
              HapticFeedback.lightImpact();
              ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('File path copied to clipboard.')));
            },
          ),
        ],
      ),
      body: _loading
          ? _buildLoadingScreen(catColor)
          : _error != null
              ? _buildErrorScreen()
              : _buildViewerContent(),
    );
  }

  /// Modern animated loading screen with progress bar, download speed, and glowing badge
  Widget _buildLoadingScreen(Color catColor) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 420),
          child: DarkCard(
            padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 32),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                // Glowing Animated Icon Badge
                PulsingStatusDot(
                  color: catColor,
                  size: 14,
                ),
                const SizedBox(height: 16),
                Container(
                  width: 80,
                  height: 80,
                  decoration: BoxDecoration(
                    color: catColor.withOpacity(0.15),
                    shape: BoxShape.circle,
                    border: Border.all(color: catColor.withOpacity(0.3), width: 1.5),
                  ),
                  alignment: Alignment.center,
                  child: Icon(
                    _getCategoryIcon(),
                    color: catColor,
                    size: 40,
                  ),
                ),
                const SizedBox(height: 20),
                Text(
                  widget.file.name,
                  style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 16),
                  textAlign: TextAlign.center,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
                const SizedBox(height: 6),
                Text(
                  widget.isLocal
                      ? 'Reading from device storage...'
                      : 'Streaming from NivaroOS Server...',
                  style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12.5),
                ),
                const SizedBox(height: 24),

                // Progress Bar
                ClipRRect(
                  borderRadius: BorderRadius.circular(8),
                  child: _totalBytes > 0
                      ? LinearProgressIndicator(
                          value: _downloadProgress > 0 ? _downloadProgress : null,
                          minHeight: 8,
                          backgroundColor: Colors.white10,
                          valueColor: AlwaysStoppedAnimation<Color>(catColor),
                        )
                      : LinearProgressIndicator(
                          minHeight: 8,
                          backgroundColor: Colors.white10,
                          valueColor: AlwaysStoppedAnimation<Color>(catColor),
                        ),
                ),
                const SizedBox(height: 12),

                // Stats row
                Row(
                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                  children: [
                    Text(
                      _totalBytes > 0
                          ? '${formatBytes(_downloadedBytes)} / ${formatBytes(_totalBytes)}'
                          : formatBytes(_downloadedBytes),
                      style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12, fontWeight: FontWeight.w600),
                    ),
                    Text(
                      _downloadSpeed.isNotEmpty
                          ? '${(_downloadProgress * 100).toStringAsFixed(0)}% · $_downloadSpeed'
                          : '${(_downloadProgress * 100).toStringAsFixed(0)}%',
                      style: TextStyle(color: catColor, fontSize: 12, fontWeight: FontWeight.w700),
                    ),
                  ],
                ),
                const SizedBox(height: 20),

                // Cancel Button
                OutlinedButton.icon(
                  onPressed: () => Navigator.of(context).pop(),
                  style: OutlinedButton.styleFrom(
                    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
                  ),
                  icon: const Icon(Icons.close_rounded, size: 18),
                  label: const Text('Cancel'),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildErrorScreen() {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 440),
          child: DarkCard(
            padding: const EdgeInsets.all(28),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Icon(Icons.error_outline_rounded, color: NivaroColors.dangerLight, size: 48),
                const SizedBox(height: 16),
                Text(widget.file.name, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 16), textAlign: TextAlign.center),
                const SizedBox(height: 10),
                Text(
                  _error!,
                  style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13),
                  textAlign: TextAlign.center,
                ),
                const SizedBox(height: 24),
                Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    OutlinedButton(onPressed: _loadFile, child: const Text('Retry')),
                    const SizedBox(width: 12),
                    FilledButton.icon(
                      onPressed: _openWithExternalApp,
                      style: FilledButton.styleFrom(backgroundColor: NivaroColors.primary),
                      icon: const Icon(Icons.open_in_new_rounded, size: 18),
                      label: const Text('Open External App'),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildViewerContent() {
    // 1. Video Player
    if (widget.file.isVideo) {
      return _buildVideoPlayer();
    }

    // 2. PDF Viewer
    if (widget.file.isPdf) {
      return _buildPdfViewer();
    }

    // 3. Image Viewer
    if (widget.file.isImage) {
      return _buildImageViewer();
    }

    // 4. Audio Player
    if (widget.file.isAudio) {
      return _buildAudioPlayer();
    }

    // 5. Markdown Preview
    if (widget.file.isMarkdown && _showMarkdownPreview && !_isEditing && _text != null) {
      return _buildMarkdownViewer();
    }

    // 6. JSON Viewer
    if (widget.file.isJson && _showFormattedJson && !_isEditing && _text != null) {
      return _buildJsonViewer();
    }

    // 7. CSV / TSV Spreadsheet Viewer
    if (widget.file.isCsv && _showCsvTable && !_isEditing && _csvRows.isNotEmpty) {
      return _buildCsvViewer();
    }

    // 8. Text / Code Editor & Viewer
    if (_text != null) {
      return _buildCodeViewer();
    }

    // 9. Archive Inspector Card
    if (widget.file.isArchive) {
      return _buildArchiveCard();
    }

    // 10. Office / Document Card
    return _buildDocumentCard();
  }

  Widget _buildVideoPlayer() {
    if (_videoLoading) {
      return const Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            CircularProgressIndicator(),
            SizedBox(height: 12),
            Text('Initializing video player...', style: TextStyle(color: NivaroColors.textMuted)),
          ],
        ),
      );
    }

    if (_videoError != null || _chewieController == null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: DarkCard(
            padding: const EdgeInsets.all(24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Icon(Icons.movie_filter_rounded, color: NivaroColors.purpleLight, size: 48),
                const SizedBox(height: 14),
                Text(widget.file.name, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 16), textAlign: TextAlign.center),
                const SizedBox(height: 8),
                Text(
                  _videoError ?? 'Built-in video decoder does not support this stream.',
                  style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13),
                  textAlign: TextAlign.center,
                ),
                const SizedBox(height: 20),
                FilledButton.icon(
                  onPressed: _openWithExternalApp,
                  style: FilledButton.styleFrom(backgroundColor: NivaroColors.primary),
                  icon: const Icon(Icons.open_in_new_rounded, size: 20),
                  label: const Text('Open in VLC / MX Player'),
                ),
              ],
            ),
          ),
        ),
      );
    }

    return Container(
      color: Colors.black,
      child: SafeArea(
        child: Center(
          child: Chewie(controller: _chewieController!),
        ),
      ),
    );
  }

  Widget _buildPdfViewer() {
    if (_cachedLocalPath == null) {
      return const Center(
        child: CircularProgressIndicator(),
      );
    }

    if (_pdfError != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: DarkCard(
            padding: const EdgeInsets.all(24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Icon(Icons.picture_as_pdf_rounded, color: NivaroColors.dangerLight, size: 48),
                const SizedBox(height: 14),
                Text(widget.file.name, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 16), textAlign: TextAlign.center),
                const SizedBox(height: 8),
                Text('PDF error: $_pdfError', style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13), textAlign: TextAlign.center),
                const SizedBox(height: 20),
                FilledButton.icon(
                  onPressed: _openWithExternalApp,
                  style: FilledButton.styleFrom(backgroundColor: NivaroColors.primary),
                  icon: const Icon(Icons.open_in_new_rounded, size: 20),
                  label: const Text('Open in External PDF App'),
                ),
              ],
            ),
          ),
        ),
      );
    }

    return Stack(
      children: [
        PDFView(
          filePath: _cachedLocalPath!,
          enableSwipe: true,
          swipeHorizontal: false,
          autoSpacing: true,
          pageFling: true,
          pageSnap: true,
          defaultPage: 0,
          fitPolicy: FitPolicy.BOTH,
          preventLinkNavigation: false,
          backgroundColor: NivaroColors.background,
          onRender: (pages) {
            if (mounted) {
              setState(() {
                _pdfTotalPages = pages ?? 0;
                _pdfReady = true;
              });
            }
          },
          onError: (error) {
            if (mounted) setState(() => _pdfError = error.toString());
          },
          onPageError: (page, error) {
            if (mounted) setState(() => _pdfError = 'Page $page: ${error.toString()}');
          },
          onViewCreated: (PDFViewController controller) {
            _pdfViewController = controller;
          },
          onPageChanged: (int? page, int? total) {
            if (mounted) {
              setState(() {
                _pdfCurrentPage = (page ?? 0) + 1;
                if (total != null && total > 0) _pdfTotalPages = total;
              });
            }
          },
        ),
        if (_pdfReady && _pdfTotalPages > 0)
          Positioned(
            left: 0,
            right: 0,
            bottom: 24,
            child: Center(
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
                decoration: BoxDecoration(
                  color: Colors.black.withOpacity(0.82),
                  borderRadius: BorderRadius.circular(20),
                  border: Border.all(color: Colors.white12),
                  boxShadow: [
                    BoxShadow(color: Colors.black.withOpacity(0.4), blurRadius: 8, spreadRadius: 2),
                  ],
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    IconButton(
                      icon: const Icon(Icons.chevron_left_rounded, size: 20, color: Colors.white),
                      padding: EdgeInsets.zero,
                      constraints: const BoxConstraints(minWidth: 28, minHeight: 28),
                      tooltip: 'Previous Page',
                      onPressed: _pdfCurrentPage > 1
                          ? () => _pdfViewController?.setPage(_pdfCurrentPage - 2)
                          : null,
                    ),
                    const SizedBox(width: 8),
                    GestureDetector(
                      onTap: _showJumpToPageDialog,
                      child: Text(
                        '$_pdfCurrentPage / $_pdfTotalPages',
                        style: const TextStyle(color: Colors.white, fontWeight: FontWeight.bold, fontSize: 13),
                      ),
                    ),
                    const SizedBox(width: 8),
                    IconButton(
                      icon: const Icon(Icons.chevron_right_rounded, size: 20, color: Colors.white),
                      padding: EdgeInsets.zero,
                      constraints: const BoxConstraints(minWidth: 28, minHeight: 28),
                      tooltip: 'Next Page',
                      onPressed: _pdfCurrentPage < _pdfTotalPages
                          ? () => _pdfViewController?.setPage(_pdfCurrentPage)
                          : null,
                    ),
                  ],
                ),
              ),
            ),
          ),
      ],
    );
  }

  Widget _buildImageViewer() {
    Color bgColor;
    switch (_imageBgMode) {
      case 1:
        bgColor = Colors.black;
        break;
      case 2:
        bgColor = Colors.white;
        break;
      case 3:
        bgColor = const Color(0xFF1E2332);
        break;
      default:
        bgColor = NivaroColors.surfaceDim;
    }

    Widget imageWidget;
    if (widget.file.isSvg && _cachedLocalPath != null) {
      imageWidget = SvgPicture.file(
        File(_cachedLocalPath!),
        fit: BoxFit.contain,
        placeholderBuilder: (_) => const Center(child: CircularProgressIndicator()),
      );
    } else if (_cachedLocalPath != null) {
      imageWidget = Image.file(
        File(_cachedLocalPath!),
        fit: BoxFit.contain,
        errorBuilder: (context, error, stackTrace) {
          return Center(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Icon(Icons.broken_image_rounded, color: NivaroColors.dangerLight, size: 48),
                const SizedBox(height: 12),
                Text('Could not decode image format ($error)', style: const TextStyle(color: NivaroColors.textMuted)),
                const SizedBox(height: 16),
                FilledButton.icon(
                  onPressed: _openWithExternalApp,
                  icon: const Icon(Icons.open_in_new_rounded, size: 18),
                  label: const Text('Open in Gallery'),
                ),
              ],
            ),
          );
        },
      );
    } else if (_bytes != null) {
      imageWidget = Image.memory(_bytes!, fit: BoxFit.contain);
    } else {
      imageWidget = const Center(child: CircularProgressIndicator());
    }

    return Container(
      color: bgColor,
      alignment: Alignment.center,
      child: GestureDetector(
        onDoubleTap: () {
          if (_imageTransformController.value != Matrix4.identity()) {
            _imageTransformController.value = Matrix4.identity();
          } else {
            _imageTransformController.value = Matrix4.identity()..scale(2.5, 2.5);
          }
        },
        child: InteractiveViewer(
          transformationController: _imageTransformController,
          minScale: 0.1,
          maxScale: 10.0,
          child: Center(child: imageWidget),
        ),
      ),
    );
  }

  Widget _buildAudioPlayer() {
    final posStr = _formatDuration(_audioPosition);
    final durStr = _formatDuration(_audioDuration);
    final double progress = (_audioDuration.inMilliseconds > 0)
        ? (_audioPosition.inMilliseconds / _audioDuration.inMilliseconds).clamp(0.0, 1.0)
        : 0.0;

    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 480),
          child: DarkCard(
            padding: const EdgeInsets.all(28),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                // Animated Disc with Sound waves
                AnimatedBuilder(
                  animation: _discAnimController,
                  builder: (context, child) {
                    return Transform.rotate(
                      angle: _discAnimController.value * 2 * math.pi,
                      child: child,
                    );
                  },
                  child: Container(
                    width: 110,
                    height: 110,
                    decoration: BoxDecoration(
                      color: NivaroColors.cyan.withOpacity(0.15),
                      shape: BoxShape.circle,
                      border: Border.all(color: NivaroColors.cyan.withOpacity(0.4), width: 3),
                      boxShadow: _audioPlaying
                          ? [
                              BoxShadow(
                                color: NivaroColors.cyan.withOpacity(0.35),
                                blurRadius: 24,
                                spreadRadius: 6,
                              ),
                            ]
                          : null,
                    ),
                    alignment: Alignment.center,
                    child: Container(
                      width: 44,
                      height: 44,
                      decoration: const BoxDecoration(
                        color: NivaroColors.surfaceContainerHigh,
                        shape: BoxShape.circle,
                      ),
                      child: const Icon(Icons.music_note_rounded, color: NivaroColors.cyanLight, size: 24),
                    ),
                  ),
                ),
                const SizedBox(height: 24),
                Text(
                  widget.file.name,
                  style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 17),
                  textAlign: TextAlign.center,
                ),
                const SizedBox(height: 6),
                Text(
                  '${formatBytes(widget.file.size)} · Audio Stream',
                  style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12.5),
                ),
                const SizedBox(height: 24),

                // Audio Slider
                SliderTheme(
                  data: SliderTheme.of(context).copyWith(
                    activeTrackColor: NivaroColors.cyanLight,
                    inactiveTrackColor: Colors.white12,
                    thumbColor: NivaroColors.cyanLight,
                    thumbShape: const RoundSliderThumbShape(enabledThumbRadius: 6),
                    overlayShape: const RoundSliderOverlayShape(overlayRadius: 12),
                  ),
                  child: Slider(
                    value: progress,
                    onChanged: (val) {
                      if (_audioPlayerController != null && _audioDuration.inMilliseconds > 0) {
                        final targetMs = (val * _audioDuration.inMilliseconds).toInt();
                        _audioPlayerController!.seekTo(Duration(milliseconds: targetMs));
                      }
                    },
                  ),
                ),
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 16),
                  child: Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      Text(posStr, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
                      Text(durStr, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
                    ],
                  ),
                ),
                const SizedBox(height: 18),

                // Primary Controls
                Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    IconButton(
                      icon: const Icon(Icons.replay_10_rounded, size: 30),
                      tooltip: 'Seek back 10s',
                      onPressed: () {
                        if (_audioPlayerController != null) {
                          final newPos = _audioPosition - const Duration(seconds: 10);
                          _audioPlayerController!.seekTo(newPos < Duration.zero ? Duration.zero : newPos);
                        }
                      },
                    ),
                    const SizedBox(width: 20),
                    FloatingActionButton(
                      mini: false,
                      backgroundColor: NivaroColors.cyan,
                      foregroundColor: Colors.black,
                      elevation: 2,
                      onPressed: () {
                        if (_audioPlayerController != null) {
                          if (_audioPlaying) {
                            _audioPlayerController!.pause();
                          } else {
                            _audioPlayerController!.play();
                          }
                        }
                      },
                      child: Icon(_audioPlaying ? Icons.pause_rounded : Icons.play_arrow_rounded, size: 32),
                    ),
                    const SizedBox(width: 20),
                    IconButton(
                      icon: const Icon(Icons.forward_10_rounded, size: 30),
                      tooltip: 'Seek forward 10s',
                      onPressed: () {
                        if (_audioPlayerController != null) {
                          final newPos = _audioPosition + const Duration(seconds: 10);
                          _audioPlayerController!.seekTo(newPos > _audioDuration ? _audioDuration : newPos);
                        }
                      },
                    ),
                  ],
                ),
                const SizedBox(height: 16),

                // Secondary audio speed & loop options
                Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    ActionChip(
                      label: Text('${_audioSpeed}x', style: const TextStyle(fontSize: 12, fontWeight: FontWeight.bold)),
                      backgroundColor: NivaroColors.surfaceContainerLowest,
                      onPressed: () {
                        final speeds = [0.75, 1.0, 1.25, 1.5, 2.0];
                        final nextIndex = (speeds.indexOf(_audioSpeed) + 1) % speeds.length;
                        final newSpeed = speeds[nextIndex];
                        _audioPlayerController?.setPlaybackSpeed(newSpeed);
                        setState(() => _audioSpeed = newSpeed);
                      },
                    ),
                    const SizedBox(width: 12),
                    ActionChip(
                      avatar: Icon(_audioLooping ? Icons.repeat_one_rounded : Icons.repeat_rounded, size: 16, color: _audioLooping ? NivaroColors.cyanLight : Colors.white60),
                      label: Text(_audioLooping ? 'Looping' : 'Repeat', style: TextStyle(fontSize: 12, color: _audioLooping ? NivaroColors.cyanLight : Colors.white70)),
                      backgroundColor: NivaroColors.surfaceContainerLowest,
                      onPressed: () {
                        final newLoop = !_audioLooping;
                        _audioPlayerController?.setLooping(newLoop);
                        setState(() => _audioLooping = newLoop);
                      },
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildMarkdownViewer() {
    return Container(
      color: NivaroColors.surfaceDim,
      child: Markdown(
        data: _text!,
        selectable: true,
        styleSheet: MarkdownStyleSheet.fromTheme(Theme.of(context)).copyWith(
          p: const TextStyle(color: Color(0xFFE6EDF3), fontSize: 14, height: 1.5),
          h1: const TextStyle(color: Colors.white, fontSize: 22, fontWeight: FontWeight.w800),
          h2: const TextStyle(color: Colors.white, fontSize: 18, fontWeight: FontWeight.w700),
          h3: const TextStyle(color: NivaroColors.primaryLight, fontSize: 16, fontWeight: FontWeight.w600),
          code: const TextStyle(fontFamily: 'monospace', backgroundColor: Color(0xFF161B22), color: NivaroColors.cyanLight, fontSize: 12.5),
          codeblockDecoration: BoxDecoration(
            color: const Color(0xFF0D1117),
            borderRadius: BorderRadius.circular(10),
            border: Border.all(color: Colors.white12),
          ),
          blockquoteDecoration: BoxDecoration(
            color: NivaroColors.primary.withOpacity(0.08),
            borderRadius: BorderRadius.circular(8),
            border: const Border(left: BorderSide(color: NivaroColors.primary, width: 4)),
          ),
        ),
        onTapLink: (text, href, title) {
          if (href != null) launchUrl(Uri.parse(href), mode: LaunchMode.externalApplication);
        },
      ),
    );
  }

  Widget _buildJsonViewer() {
    String formatted;
    try {
      final parsed = jsonDecode(_text!);
      formatted = const JsonEncoder.withIndent('  ').convert(parsed);
    } catch (_) {
      formatted = _text!;
    }

    final lines = const LineSplitter().convert(formatted);

    return Container(
      color: const Color(0xFF0D1117),
      child: SelectionArea(
        child: ListView.builder(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 12),
          itemCount: lines.length,
          itemBuilder: (context, index) {
            final line = lines[index];
            return Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                SizedBox(
                  width: 36,
                  child: Text(
                    '${index + 1}',
                    style: const TextStyle(fontFamily: 'monospace', fontSize: 12, color: Colors.white24),
                    textAlign: TextAlign.right,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    line,
                    style: TextStyle(
                      fontFamily: 'monospace',
                      fontSize: 12.5,
                      color: _getJsonLineColor(line),
                      height: 1.4,
                    ),
                  ),
                ),
              ],
            );
          },
        ),
      ),
    );
  }

  Color _getJsonLineColor(String line) {
    final trimmed = line.trim();
    if (trimmed.startsWith('"') && trimmed.contains('":')) {
      return const Color(0xFF7EE787); // Key emerald
    }
    if (trimmed.endsWith(':') || trimmed == '{' || trimmed == '}' || trimmed == '[' || trimmed == ']') {
      return const Color(0xFF79C0FF); // Structure blue
    }
    if (trimmed.contains('true') || trimmed.contains('false') || trimmed.contains('null')) {
      return const Color(0xFFD2A8FF); // Keyword purple
    }
    return const Color(0xFFE6EDF3);
  }

  Widget _buildCsvViewer() {
    final filter = _csvFilter.toLowerCase();
    final header = _csvRows.isNotEmpty ? _csvRows.first : <String>[];
    final rows = _csvRows.length > 1
        ? _csvRows.skip(1).where((r) => filter.isEmpty || r.any((c) => c.toLowerCase().contains(filter))).toList()
        : <List<String>>[];

    return Column(
      children: [
        // Filter Bar & Row count
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
          color: NivaroColors.surfaceContainerLowest,
          child: Row(
            children: [
              Expanded(
                child: TextField(
                  controller: _csvFilterController,
                  onChanged: (val) => setState(() => _csvFilter = val),
                  decoration: InputDecoration(
                    hintText: 'Search in table...',
                    hintStyle: const TextStyle(fontSize: 13, color: Colors.white38),
                    prefixIcon: const Icon(Icons.search_rounded, size: 18),
                    suffixIcon: _csvFilter.isNotEmpty
                        ? IconButton(
                            icon: const Icon(Icons.clear_rounded, size: 16),
                            onPressed: () {
                              _csvFilterController.clear();
                              setState(() => _csvFilter = '');
                            },
                          )
                        : null,
                    isDense: true,
                    contentPadding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                    filled: true,
                    fillColor: NivaroColors.surfaceContainerLow,
                    border: OutlineInputBorder(borderRadius: BorderRadius.circular(10), borderSide: BorderSide.none),
                  ),
                ),
              ),
              const SizedBox(width: 12),
              Text(
                '${rows.length} of ${_csvRows.length - 1} rows · ${header.length} cols',
                style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11.5, fontWeight: FontWeight.bold),
              ),
            ],
          ),
        ),

        // Interactive Data Table
        Expanded(
          child: SelectionArea(
            child: SingleChildScrollView(
              scrollDirection: Axis.vertical,
              child: SingleChildScrollView(
                scrollDirection: Axis.horizontal,
                child: DataTable(
                  headingRowColor: WidgetStateProperty.all(NivaroColors.surfaceContainerHigh),
                  dataRowColor: WidgetStateProperty.resolveWith((states) {
                    return states.contains(WidgetState.hovered)
                        ? NivaroColors.primary.withOpacity(0.12)
                        : NivaroColors.surfaceDim;
                  }),
                  headingTextStyle: const TextStyle(fontWeight: FontWeight.bold, color: NivaroColors.cyanLight, fontSize: 13),
                  dataTextStyle: const TextStyle(color: Color(0xFFE6EDF3), fontSize: 12.5),
                  columns: [
                    const DataColumn(label: Text('#')),
                    for (final col in header) DataColumn(label: Text(col)),
                  ],
                  rows: [
                    for (int i = 0; i < rows.length; i++)
                      DataRow(
                        cells: [
                          DataCell(Text('${i + 1}', style: const TextStyle(color: Colors.white30, fontSize: 11))),
                          for (final cell in rows[i]) DataCell(Text(cell)),
                        ],
                      ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildCodeViewer() {
    if (_isEditing) {
      return Container(
        color: const Color(0xFF0D1117),
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
        child: TextField(
          controller: _textController,
          maxLines: null,
          keyboardType: TextInputType.multiline,
          autofocus: true,
          style: const TextStyle(
            fontFamily: 'monospace',
            fontSize: 13,
            color: Color(0xFFE6EDF3),
            height: 1.45,
          ),
          decoration: const InputDecoration(
            border: InputBorder.none,
            hintText: 'Enter text here...',
            hintStyle: TextStyle(color: Colors.white24),
          ),
        ),
      );
    }

    final lines = const LineSplitter().convert(_text!);

    return Column(
      children: [
        if (_isSearchingCode)
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 6),
            color: NivaroColors.surfaceContainerLowest,
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _codeSearchController,
                    autofocus: true,
                    onChanged: (val) => setState(() => _codeSearchQuery = val),
                    style: const TextStyle(fontSize: 13, color: Colors.white),
                    decoration: InputDecoration(
                      hintText: 'Find in text...',
                      hintStyle: const TextStyle(fontSize: 12.5, color: Colors.white30),
                      prefixIcon: const Icon(Icons.search_rounded, size: 16, color: NivaroColors.primaryLight),
                      suffixIcon: _codeSearchQuery.isNotEmpty
                          ? IconButton(
                              icon: const Icon(Icons.clear_rounded, size: 16),
                              onPressed: () {
                                _codeSearchController.clear();
                                setState(() => _codeSearchQuery = '');
                              },
                            )
                          : null,
                      isDense: true,
                      contentPadding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
                      filled: true,
                      fillColor: NivaroColors.surfaceContainerLow,
                      border: OutlineInputBorder(borderRadius: BorderRadius.circular(8), borderSide: BorderSide.none),
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                IconButton(
                  icon: const Icon(Icons.close_rounded, size: 18),
                  tooltip: 'Close search',
                  onPressed: () => setState(() {
                    _isSearchingCode = false;
                    _codeSearchController.clear();
                    _codeSearchQuery = '';
                  }),
                ),
              ],
            ),
          ),
        Expanded(
          child: Container(
            color: const Color(0xFF0D1117),
            child: SelectionArea(
              child: SingleChildScrollView(
                scrollDirection: _wrapCode ? Axis.vertical : Axis.horizontal,
                child: SingleChildScrollView(
                  scrollDirection: _wrapCode ? Axis.horizontal : Axis.vertical,
                  child: Padding(
                    padding: const EdgeInsets.all(12),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        for (int i = 0; i < lines.length; i++)
                          _buildCodeLine(i + 1, lines[i]),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildCodeLine(int lineNum, String lineText) {
    final query = _codeSearchQuery.toLowerCase();
    final isMatched = query.isNotEmpty && lineText.toLowerCase().contains(query);

    return Container(
      color: isMatched ? NivaroColors.warning.withOpacity(0.18) : Colors.transparent,
      padding: const EdgeInsets.symmetric(vertical: 1),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 38,
            child: Text(
              '$lineNum',
              style: TextStyle(
                fontFamily: 'monospace',
                fontSize: 12,
                color: isMatched ? NivaroColors.warningLight : Colors.white24,
                fontWeight: isMatched ? FontWeight.bold : FontWeight.normal,
              ),
              textAlign: TextAlign.right,
            ),
          ),
          const SizedBox(width: 14),
          Text(
            lineText.isEmpty ? ' ' : lineText,
            style: TextStyle(
              fontFamily: 'monospace',
              fontSize: 12.5,
              color: isMatched ? NivaroColors.warningLight : const Color(0xFFE6EDF3),
              height: 1.45,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildArchiveCard() {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 440),
          child: DarkCard(
            padding: const EdgeInsets.all(28),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Container(
                  width: 80,
                  height: 80,
                  decoration: BoxDecoration(
                    color: const Color(0xFFF97316).withOpacity(0.15),
                    shape: BoxShape.circle,
                  ),
                  alignment: Alignment.center,
                  child: const Icon(Icons.folder_zip_rounded, color: Color(0xFFFB923C), size: 42),
                ),
                const SizedBox(height: 18),
                Text(widget.file.name, style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 16), textAlign: TextAlign.center),
                const SizedBox(height: 6),
                Text(
                  '${formatBytes(widget.file.size)} · ${widget.file.extension.toUpperCase()} Compressed Archive',
                  style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12.5),
                ),
                const SizedBox(height: 24),
                FilledButton.icon(
                  onPressed: _openWithExternalApp,
                  style: FilledButton.styleFrom(
                    backgroundColor: NivaroColors.primary,
                    minimumSize: const Size(double.infinity, 46),
                    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
                  ),
                  icon: const Icon(Icons.open_in_new_rounded, size: 20),
                  label: const Text('Open with Native Archiver', style: TextStyle(fontWeight: FontWeight.bold)),
                ),
                if (!widget.isLocal) ...[
                  const SizedBox(height: 10),
                  OutlinedButton.icon(
                    onPressed: _savingToDevice ? null : _saveToDeviceDownloads,
                    style: OutlinedButton.styleFrom(
                      minimumSize: const Size(double.infinity, 44),
                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
                    ),
                    icon: const Icon(Icons.download_rounded, size: 20),
                    label: const Text('Save to Device Downloads'),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildDocumentCard() {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 440),
          child: DarkCard(
            padding: const EdgeInsets.all(28),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Container(
                  width: 80,
                  height: 80,
                  decoration: BoxDecoration(
                    color: NivaroColors.infoLight.withOpacity(0.15),
                    shape: BoxShape.circle,
                  ),
                  alignment: Alignment.center,
                  child: Icon(
                    _getCategoryIcon(),
                    color: NivaroColors.infoLight,
                    size: 42,
                  ),
                ),
                const SizedBox(height: 18),
                Text(
                  widget.file.name,
                  style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 16),
                  textAlign: TextAlign.center,
                ),
                const SizedBox(height: 6),
                Text(
                  '${formatBytes(widget.file.size)} · ${widget.file.categoryLabel}',
                  style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12.5),
                ),
                const SizedBox(height: 24),
                FilledButton.icon(
                  onPressed: _openWithExternalApp,
                  style: FilledButton.styleFrom(
                    backgroundColor: NivaroColors.primary,
                    minimumSize: const Size(double.infinity, 46),
                    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
                  ),
                  icon: const Icon(Icons.open_in_new_rounded, size: 20),
                  label: const Text('Open with Native App', style: TextStyle(fontWeight: FontWeight.bold)),
                ),
                if (!widget.isLocal) ...[
                  const SizedBox(height: 10),
                  OutlinedButton.icon(
                    onPressed: _savingToDevice ? null : _saveToDeviceDownloads,
                    style: OutlinedButton.styleFrom(
                      minimumSize: const Size(double.infinity, 44),
                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
                    ),
                    icon: const Icon(Icons.download_rounded, size: 20),
                    label: const Text('Save to Device Downloads'),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }

  String _formatDuration(Duration d) {
    final minutes = d.inMinutes.remainder(60).toString().padLeft(2, '0');
    final seconds = d.inSeconds.remainder(60).toString().padLeft(2, '0');
    if (d.inHours > 0) {
      final hours = d.inHours.toString().padLeft(2, '0');
      return '$hours:$minutes:$seconds';
    }
    return '$minutes:$seconds';
  }
}
