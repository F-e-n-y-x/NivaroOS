import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:chewie/chewie.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_pdfview/flutter_pdfview.dart';
import 'package:flutter_svg/flutter_svg.dart';
import 'package:http/http.dart' as http;
import 'package:open_filex/open_filex.dart';
import 'package:path_provider/path_provider.dart';
import 'package:url_launcher/url_launcher.dart';
import 'package:video_player/video_player.dart';

import '../models/file_entry.dart';
import '../services/api_client.dart';
import '../ui/ui.dart';
import '../utils/format.dart';
import 'files/file_ops.dart';
import 'files/file_sheets.dart';
import 'files/file_widgets.dart';

/// Text files larger than this open in another app rather than here.
const _maxTextPreview = 5 * 1024 * 1024;

/// Opens one file: pictures (with swiping through the folder's other
/// pictures and pinch zoom), video and audio (streamed, not downloaded
/// first), PDF, Markdown, text and code (with find and editing), CSV
/// tables. Anything else gets "Open with another app" and "Save to phone".
///
/// Pops with `true` when the file was edited and saved, so the folder
/// reloads.
class FileViewerScreen extends StatefulWidget {
  const FileViewerScreen({
    super.key,
    required this.file,
    required this.path,
    this.isLocal = false,
    this.gallery,
  });

  final FileEntry file;
  final String path;

  /// On this phone rather than on the server.
  final bool isLocal;

  /// The folder's pictures, in the folder's order, to swipe through when
  /// [file] is a picture.
  final List<FileEntry>? gallery;

  @override
  State<FileViewerScreen> createState() => _FileViewerScreenState();
}

enum _Mode { image, video, audio, pdf, markdown, text, csv, none }

_Mode _modeFor(FileEntry f) {
  if (f.isImage) return _Mode.image;
  if (f.isVideo) return _Mode.video;
  if (f.isAudio) return _Mode.audio;
  if (f.isPdf) return _Mode.pdf;
  if (f.isMarkdown) return _Mode.markdown;
  if (f.isCsv) return _Mode.csv;
  if (f.isText) return _Mode.text;
  return _Mode.none;
}

class _FileViewerScreenState extends State<FileViewerScreen> {
  late final _Mode _mode = _modeFor(widget.file);

  // Download (PDF, text) state
  bool _downloading = false;
  int _received = 0;
  int _total = 0;
  Object? _error;
  String? _localPath;
  http.Client? _client;

  // Text
  String? _text;
  bool _tooLarge = false;
  bool _showSource = false;
  bool _wrap = true;
  bool _finding = false;
  final _find = TextEditingController();

  // Editing
  bool _editing = false;
  bool _saving = false;
  bool _changed = false;
  final _editor = TextEditingController();

  // Busy with "Open with" / "Save to phone"
  bool _exporting = false;

  @override
  void initState() {
    super.initState();
    _find.addListener(() => setState(() {}));
    if (_mode == _Mode.pdf || _mode == _Mode.markdown || _mode == _Mode.text || _mode == _Mode.csv) _prepare();
  }

  @override
  void dispose() {
    _client?.close();
    _find.dispose();
    _editor.dispose();
    super.dispose();
  }

  // -------------------------------------------------------------------------
  // Getting the file

  Future<String> _localCopy({void Function(int received, int total)? onProgress}) => downloadToCache(
        widget.file,
        isLocal: widget.isLocal,
        onProgress: onProgress,
        onClient: (c) => _client = c,
      );

  Future<void> _prepare() async {
    setState(() {
      _downloading = true;
      _received = 0;
      _total = widget.file.size;
      _error = null;
    });
    try {
      if (_mode != _Mode.pdf && widget.file.size > _maxTextPreview) {
        setState(() {
          _tooLarge = true;
          _downloading = false;
        });
        return;
      }
      final path = await _localCopy(onProgress: (r, t) {
        if (mounted) {
          setState(() {
            _received = r;
            _total = t;
          });
        }
      });
      String? text;
      if (_mode != _Mode.pdf) {
        final bytes = await File(path).readAsBytes();
        if (bytes.length > _maxTextPreview) {
          _tooLarge = true;
        } else {
          try {
            text = utf8.decode(bytes);
          } on FormatException {
            text = latin1.decode(bytes);
          }
          if (widget.file.isJson) text = _prettyJson(text);
        }
      }
      if (!mounted) return;
      setState(() {
        _localPath = path;
        _text = text;
        _downloading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e;
        _downloading = false;
      });
    }
  }

  static String _prettyJson(String text) {
    try {
      return const JsonEncoder.withIndent('  ').convert(jsonDecode(text));
    } catch (_) {
      return text;
    }
  }

  // -------------------------------------------------------------------------
  // Actions

  void _snack(String message) {
    final m = ScaffoldMessenger.maybeOf(context);
    m?.hideCurrentSnackBar();
    m?.showSnackBar(SnackBar(content: Text(message)));
  }

  String _plain(Object e) => e.toString().replaceFirst('Exception: ', '');

  Future<void> _openWith() async {
    if (_exporting) return;
    setState(() => _exporting = true);
    await openWithAnotherApp(context, widget.file, isLocal: widget.isLocal, cached: _localPath);
    if (mounted) setState(() => _exporting = false);
  }

  Future<void> _saveToPhone() async {
    if (_exporting) return;
    setState(() => _exporting = true);
    await saveToPhone(context, widget.file, cached: _localPath);
    if (mounted) setState(() => _exporting = false);
  }

  Future<void> _copyPath() async {
    await Clipboard.setData(ClipboardData(text: widget.path));
    _snack('Path copied');
  }

  void _details([FileEntry? entry]) => showInfoSheet(context, entry: entry ?? widget.file, isLocal: widget.isLocal);

  Future<void> _save() async {
    if (_saving) return;
    setState(() => _saving = true);
    final text = _editor.text;
    try {
      if (widget.isLocal) {
        await File(widget.path).writeAsString(text);
      } else {
        final res = await ApiClient.instance.put('/file', body: {'path': widget.path, 'content': text});
        final code = res['success'];
        if (code is num && code != 200) throw Exception(res['message'] ?? 'The server refused ($code).');
        // The cached copy is stale now.
        final cached = _localPath;
        if (cached != null && cached != widget.path) await File(cached).writeAsString(text);
      }
      if (!mounted) return;
      setState(() {
        _text = text;
        _editing = false;
        _changed = true;
      });
      _snack('Saved');
    } catch (e) {
      _snack("Couldn't save: ${_plain(e)}");
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  bool get _dirty => _editing && _editor.text != (_text ?? '');

  Future<void> _stopEditing() async {
    if (_dirty) {
      final discard = await ConfirmDialog.destructive(
        context,
        title: 'Discard your changes?',
        message: 'What you typed since the last save is lost.',
        confirmLabel: 'Discard',
        permanent: false,
      );
      if (!discard || !mounted) return;
    }
    setState(() => _editing = false);
  }

  // -------------------------------------------------------------------------
  // Building

  @override
  Widget build(BuildContext context) {
    if (_mode == _Mode.image) {
      return _ImageGallery(
        files: widget.gallery?.any((g) => g.path == widget.file.path) == true ? widget.gallery! : [widget.file],
        initial: widget.file,
        isLocal: widget.isLocal,
        onDetails: _details,
      );
    }
    if (_mode == _Mode.video || _mode == _Mode.audio) {
      return _MediaPlayer(
        file: widget.file,
        isLocal: widget.isLocal,
        audio: _mode == _Mode.audio,
        onOpenWith: _openWith,
        onSave: widget.isLocal ? null : _saveToPhone,
        onDetails: _details,
      );
    }
    return PopScope(
      canPop: !_editing,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _stopEditing();
      },
      child: _buildDocument(context),
    );
  }

  Widget _buildDocument(BuildContext context) {
    final isText = _mode == _Mode.text || _mode == _Mode.markdown || _mode == _Mode.csv;
    final ready = _text != null && !_downloading && _error == null;
    return AppScaffold(
      title: widget.file.name,
      maxContentWidth: null,
      leading: _editing
          ? IconButton(icon: const Icon(Icons.close), tooltip: 'Stop editing', onPressed: _stopEditing)
          : BackButton(onPressed: () => Navigator.of(context).pop(_changed)),
      actions: _editing
          ? [
              Padding(
                padding: const EdgeInsets.only(right: Space.sm),
                child: _saving
                    ? const Padding(
                        padding: EdgeInsets.all(Space.md),
                        child: SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 3)),
                      )
                    : TextButton(onPressed: _save, child: const Text('Save')),
              ),
            ]
          : [
              if (isText && ready && _mode != _Mode.text)
                IconButton(
                  icon: Icon(_showSource ? (_mode == _Mode.csv ? Icons.table_chart_outlined : Icons.visibility_outlined) : Icons.code),
                  tooltip: _showSource ? (_mode == _Mode.csv ? 'Show as table' : 'Show preview') : 'Show source',
                  onPressed: () => setState(() => _showSource = !_showSource),
                ),
              if (isText && ready && _showsLines)
                IconButton(
                  icon: const Icon(Icons.search),
                  tooltip: 'Find in file',
                  onPressed: () => setState(() => _finding = !_finding),
                ),
              _overflowMenu(isText && ready),
            ],
      bottom: _finding && _showsLines && !_editing ? _findBar(context) : null,
      body: _documentBody(context),
    );
  }

  bool get _showsLines => _mode == _Mode.text || _showSource;

  Widget _overflowMenu(bool editable) {
    return PopupMenuButton<String>(
      tooltip: 'More options',
      onSelected: (v) {
        switch (v) {
          case 'edit':
            _editor.text = _text ?? '';
            setState(() {
              _editing = true;
              _finding = false;
            });
          case 'wrap':
            setState(() => _wrap = !_wrap);
          case 'open':
            _openWith();
          case 'save':
            _saveToPhone();
          case 'path':
            _copyPath();
          case 'info':
            _details();
        }
      },
      itemBuilder: (context) => [
        if (editable) const PopupMenuItem(value: 'edit', child: Text('Edit')),
        if (editable && _showsLines) CheckedPopupMenuItem(value: 'wrap', checked: _wrap, child: const Text('Wrap lines')),
        const PopupMenuItem(value: 'open', child: Text('Open with another app')),
        if (!widget.isLocal) const PopupMenuItem(value: 'save', child: Text('Save to phone')),
        const PopupMenuItem(value: 'path', child: Text('Copy path')),
        const PopupMenuItem(value: 'info', child: Text('Details')),
      ],
    );
  }

  PreferredSizeWidget _findBar(BuildContext context) {
    final h = (MediaQuery.textScalerOf(context).scale(24) + 40).clamp(64.0, 112.0);
    final query = _find.text;
    final matches = query.isEmpty ? 0 : _lines.where((l) => l.toLowerCase().contains(query.toLowerCase())).length;
    return PreferredSize(
      preferredSize: Size.fromHeight(h),
      child: Padding(
        padding: EdgeInsets.fromLTRB(Space.gutter(context), 0, Space.gutter(context), Space.sm),
        child: TextField(
          controller: _find,
          autofocus: true,
          decoration: InputDecoration(
            hintText: 'Find in file',
            prefixIcon: const Icon(Icons.search),
            suffixText: query.isEmpty ? null : formatCount(matches, 'line'),
            suffixIcon: IconButton(
              icon: const Icon(Icons.close),
              tooltip: 'Close find',
              onPressed: () => setState(() {
                _finding = false;
                _find.clear();
              }),
            ),
          ),
        ),
      ),
    );
  }

  List<String>? _linesCache;
  String? _linesFor;
  List<String> get _lines {
    final text = _text ?? '';
    if (_linesFor != text) {
      _linesCache = const LineSplitter().convert(text);
      _linesFor = text;
    }
    return _linesCache!;
  }

  Widget _documentBody(BuildContext context) {
    final error = _error;
    if (error != null) {
      if (error is ApiException && error.isUnreachable) return ErrorState.offline(onRetry: _prepare, details: error.details);
      return ErrorState(
        title: "Couldn't open “${widget.file.name}”",
        message: _plain(error),
        onRetry: _prepare,
        details: error is ApiException ? error.details : null,
      );
    }
    if (_downloading) {
      return _DownloadProgress(file: widget.file, isLocal: widget.isLocal, received: _received, total: _total, onCancel: () => Navigator.of(context).pop());
    }
    if (_mode == _Mode.none || _tooLarge) {
      return _NoPreview(
        file: widget.file,
        isLocal: widget.isLocal,
        reason: _tooLarge ? 'This file is too large to show here.' : null,
        busy: _exporting,
        onOpenWith: _openWith,
        onSave: widget.isLocal ? null : _saveToPhone,
      );
    }
    if (_mode == _Mode.pdf) return _PdfView(path: _localPath!, onOpenWith: _openWith);
    if (_editing) return _Editor(controller: _editor);
    final text = _text ?? '';
    if (text.isEmpty) {
      return EmptyState(
        icon: fileKindIcon(widget.file.kind),
        title: 'This file is empty',
        message: 'There is nothing in “${widget.file.name}” yet.',
        actionLabel: 'Edit',
        onAction: () => setState(() {
          _editor.text = '';
          _editing = true;
        }),
      );
    }
    if (_mode == _Mode.markdown && !_showSource) return _MarkdownView(text: text);
    if (_mode == _Mode.csv && !_showSource) return _CsvView(text: text, tsv: widget.file.extension == 'tsv');
    return _LinesView(lines: _lines, wrap: _wrap, query: _finding ? _find.text : '');
  }
}

// ---------------------------------------------------------------------------
// Local copies, "Open with another app" and "Save to phone"

/// A local copy of [file]: the file itself on this phone, otherwise a
/// download into the app's cache (reused while its size matches). The
/// client doing the download goes to [onClient], so closing it cancels.
Future<String> downloadToCache(
  FileEntry file, {
  required bool isLocal,
  void Function(int received, int total)? onProgress,
  void Function(http.Client client)? onClient,
}) async {
  if (isLocal) return file.path;
  final dir = Directory('${(await getTemporaryDirectory()).path}/viewer');
  await dir.create(recursive: true);
  final safe = file.name.replaceAll(RegExp(r'[^A-Za-z0-9._ ()-]'), '_');
  final target = File('${dir.path}/${file.path.hashCode.toUnsigned(32)}_$safe');
  if (await target.exists() && await target.length() == file.size) return target.path;
  final part = File('${target.path}.part');
  final client = http.Client();
  onClient?.call(client);
  IOSink? sink;
  try {
    final uri = ApiClient.instance.buildUri('/file', {'path': file.path});
    final res = await ApiClient.instance.send(() => http.Request('GET', uri), client: client);
    if (res.statusCode != 200) {
      await res.stream.drain<void>();
      throw ApiException(
        res.statusCode == 404
            ? "It isn't on the server any more. It may have been moved or deleted."
            : "The server couldn't send this file.",
        statusCode: res.statusCode,
        details: 'GET /v1/file → HTTP ${res.statusCode}',
      );
    }
    final total = res.contentLength ?? file.size;
    sink = part.openWrite();
    var received = 0;
    await for (final chunk in res.stream) {
      sink.add(chunk);
      received += chunk.length;
      onProgress?.call(received, total);
    }
    await sink.close();
    sink = null;
    if (file.size > 0 && received != file.size) {
      throw ApiException('The download stopped early (${formatSize(received)} of ${formatSize(file.size)}). Try again.');
    }
    await part.rename(target.path);
    return target.path;
  } finally {
    await sink?.close();
    client.close();
  }
}

/// Downloads [file] with a progress dialog (Cancel stops it); null when it
/// was cancelled or failed (the failure is shown in a snackbar).
Future<String?> _fetchWithDialog(BuildContext context, FileEntry file, {required bool isLocal, String? cached}) async {
  if (cached != null) return cached;
  if (isLocal) return file.path;
  final progress = ValueNotifier<(int, int)>((0, file.size));
  http.Client? client;
  var cancelled = false;
  final navigator = Navigator.of(context, rootNavigator: true);
  final messenger = ScaffoldMessenger.maybeOf(context);
  final dialog = showDialog<void>(
    context: context,
    barrierDismissible: false,
    builder: (context) => _DownloadDialog(
      name: file.name,
      progress: progress,
      onCancel: () {
        cancelled = true;
        client?.close();
        Navigator.of(context).pop();
      },
    ),
  );
  try {
    final path = await downloadToCache(file, isLocal: false, onProgress: (r, t) => progress.value = (r, t), onClient: (c) => client = c);
    if (!cancelled) navigator.pop();
    await dialog;
    return cancelled ? null : path;
  } catch (e) {
    if (!cancelled) {
      navigator.pop();
      messenger?.showSnackBar(SnackBar(content: Text("Couldn't download “${file.name}”: ${e.toString().replaceFirst('Exception: ', '')}")));
    }
    await dialog;
    return null;
  } finally {
    progress.dispose();
  }
}

/// Hands [file] to another app on the phone (downloading it first when it
/// is on the server).
Future<void> openWithAnotherApp(BuildContext context, FileEntry file, {required bool isLocal, String? cached}) async {
  final messenger = ScaffoldMessenger.maybeOf(context);
  final path = await _fetchWithDialog(context, file, isLocal: isLocal, cached: cached);
  if (path == null) return;
  final res = await OpenFilex.open(path);
  if (res.type != ResultType.done) {
    messenger?.showSnackBar(SnackBar(
      content: Text(res.type == ResultType.noAppToOpen ? 'No app on this phone opens this kind of file.' : res.message),
    ));
  }
}

/// Copies [file] from the server into the phone's Downloads folder, under
/// a new name when one is taken.
Future<void> saveToPhone(BuildContext context, FileEntry file, {String? cached}) async {
  final messenger = ScaffoldMessenger.maybeOf(context);
  final path = await _fetchWithDialog(context, file, isLocal: false, cached: cached);
  if (path == null) return;
  try {
    var dir = Directory('/storage/emulated/0/Download');
    if (!await dir.exists()) dir = await getApplicationDocumentsDirectory();
    final taken = {for (final e in dir.listSync()) baseName(e.path)};
    final name = uniqueName(file.name, taken);
    final target = await File(path).copy('${dir.path}/$name');
    messenger?.showSnackBar(SnackBar(
      content: Text('Saved “$name” to Downloads'),
      persist: false,
      action: SnackBarAction(label: 'Open', onPressed: () => OpenFilex.open(target.path)),
    ));
  } catch (e) {
    messenger?.showSnackBar(SnackBar(content: Text("Couldn't save to this phone: ${e.toString().replaceFirst('Exception: ', '')}")));
  }
}

// ---------------------------------------------------------------------------
// Download progress and "no preview"

class _DownloadProgress extends StatelessWidget {
  const _DownloadProgress({required this.file, required this.isLocal, required this.received, required this.total, required this.onCancel});

  final FileEntry file;
  final bool isLocal;
  final int received;
  final int total;
  final VoidCallback onCancel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final fraction = total > 0 ? (received / total).clamp(0.0, 1.0) : null;
    return Center(
      child: SingleChildScrollView(
        padding: EdgeInsets.all(Space.gutter(context)),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 360),
          child: Semantics(
            liveRegion: true,
            label: 'Downloading ${file.name}',
            value: fraction == null ? null : '${(fraction * 100).round()}%',
            child: ExcludeSemantics(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(fileKindIcon(file.kind), size: 48, color: scheme.onSurfaceVariant),
                  const SizedBox(height: Space.lg),
                  Text(file.name, style: theme.textTheme.titleMedium, textAlign: TextAlign.center, maxLines: 2, overflow: TextOverflow.ellipsis),
                  const SizedBox(height: Space.lg),
                  LinearProgressIndicator(value: fraction),
                  const SizedBox(height: Space.sm),
                  Text(
                    total > 0 ? '${formatSize(received)} of ${formatSize(total)}' : formatSize(received),
                    style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant).tabular,
                  ),
                  const SizedBox(height: Space.xl),
                  TextButton(onPressed: onCancel, child: const Text('Cancel')),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _NoPreview extends StatelessWidget {
  const _NoPreview({required this.file, required this.isLocal, required this.onOpenWith, this.onSave, this.reason, this.busy = false});

  final FileEntry file;
  final bool isLocal;
  final String? reason;
  final bool busy;
  final VoidCallback onOpenWith;
  final VoidCallback? onSave;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final message = reason ??
        (file.isArchive
            ? 'Archives can’t be opened here. Extract it in Files, or open it with another app.'
            : 'NivaroOS can’t show ${file.categoryLabel.toLowerCase().startsWith(RegExp('[aeiou]')) ? 'an' : 'a'} ${file.categoryLabel.toLowerCase()} here.');
    return LayoutBuilder(
      builder: (context, c) => SingleChildScrollView(
        padding: EdgeInsets.symmetric(horizontal: Space.gutter(context), vertical: Space.xl),
        child: ConstrainedBox(
          constraints: BoxConstraints(minHeight: (c.maxHeight - Space.xl * 2).clamp(0, double.infinity)),
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 360),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(fileKindIcon(file.kind), size: 48, color: scheme.onSurfaceVariant),
                  const SizedBox(height: Space.lg),
                  Semantics(
                    header: true,
                    child: Text(file.name, style: theme.textTheme.titleLarge, textAlign: TextAlign.center),
                  ),
                  const SizedBox(height: Space.xs),
                  Text(
                    '${file.categoryLabel} · ${formatSize(file.size)}',
                    style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
                  ),
                  const SizedBox(height: Space.lg),
                  Text(message, textAlign: TextAlign.center, style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
                  const SizedBox(height: Space.xl),
                  Wrap(
                    alignment: WrapAlignment.center,
                    spacing: Space.sm,
                    runSpacing: Space.sm,
                    children: [
                      FilledButton.icon(
                        onPressed: busy ? null : onOpenWith,
                        icon: const Icon(Icons.open_in_new_outlined),
                        label: const Text('Open with another app'),
                      ),
                      if (onSave != null)
                        OutlinedButton.icon(
                          onPressed: busy ? null : onSave,
                          icon: const Icon(Icons.download_outlined),
                          label: const Text('Save to phone'),
                        ),
                    ],
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _DownloadDialog extends StatelessWidget {
  const _DownloadDialog({required this.name, required this.progress, required this.onCancel});

  final String name;
  final ValueListenable<(int, int)> progress;
  final VoidCallback onCancel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AlertDialog(
      title: const Text('Downloading'),
      content: ValueListenableBuilder<(int, int)>(
        valueListenable: progress,
        builder: (context, p, _) {
          final (received, total) = p;
          return Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(name, maxLines: 2, overflow: TextOverflow.ellipsis),
              const SizedBox(height: Space.lg),
              LinearProgressIndicator(value: total > 0 ? (received / total).clamp(0.0, 1.0) : null),
              const SizedBox(height: Space.sm),
              Text(
                total > 0 ? '${formatSize(received)} of ${formatSize(total)}' : formatSize(received),
                style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant).tabular,
              ),
            ],
          );
        },
      ),
      actions: [TextButton(onPressed: onCancel, child: const Text('Cancel'))],
    );
  }
}

// ---------------------------------------------------------------------------
// Text

class _LinesView extends StatelessWidget {
  const _LinesView({required this.lines, required this.wrap, required this.query});

  final List<String> lines;
  final bool wrap;
  final String query;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final style = DesignTokens.of(context).mono(theme.textTheme.bodyMedium).copyWith(color: scheme.onSurface);
    final numberStyle = style.copyWith(color: scheme.onSurfaceVariant);
    final q = query.toLowerCase();
    final digits = '${lines.length}'.length;
    final painter = TextPainter(text: TextSpan(text: '0' * digits, style: numberStyle), textDirection: TextDirection.ltr, textScaler: MediaQuery.textScalerOf(context))..layout();
    final numberWidth = painter.width;
    final gutter = Space.gutter(context);

    Widget line(int i) {
      final matched = q.isNotEmpty && lines[i].toLowerCase().contains(q);
      return Container(
        color: matched ? scheme.tertiaryContainer : null,
        padding: EdgeInsets.fromLTRB(gutter, 1, gutter, 1),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SizedBox(
              width: numberWidth,
              child: ExcludeSemantics(child: Text('${i + 1}', style: numberStyle, textAlign: TextAlign.right)),
            ),
            const SizedBox(width: Space.lg),
            if (wrap)
              Expanded(child: Text(lines[i].isEmpty ? ' ' : lines[i], style: matched ? style.copyWith(color: scheme.onTertiaryContainer) : style))
            else
              Text(lines[i].isEmpty ? ' ' : lines[i], style: matched ? style.copyWith(color: scheme.onTertiaryContainer) : style, softWrap: false),
          ],
        ),
      );
    }

    final list = SelectionArea(
      child: ListView.builder(
        padding: const EdgeInsets.symmetric(vertical: Space.sm),
        itemCount: lines.length,
        itemBuilder: (context, i) => line(i),
      ),
    );
    if (wrap) return ColoredBox(color: scheme.surfaceContainerLowest, child: list);
    // Unwrapped: as wide as the longest line, scrolling sideways.
    final longest = lines.fold<int>(0, (m, l) => l.length > m ? l.length : m);
    final charPainter = TextPainter(text: TextSpan(text: 'M', style: style), textDirection: TextDirection.ltr, textScaler: MediaQuery.textScalerOf(context))..layout();
    return ColoredBox(
      color: scheme.surfaceContainerLowest,
      child: LayoutBuilder(
        builder: (context, c) => SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          child: SizedBox(
            width: (gutter * 2 + numberWidth + Space.lg + charPainter.width * (longest + 1)).clamp(c.maxWidth, double.infinity),
            child: list,
          ),
        ),
      ),
    );
  }
}

class _Editor extends StatelessWidget {
  const _Editor({required this.controller});
  final TextEditingController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ColoredBox(
      color: theme.colorScheme.surfaceContainerLowest,
      child: TextField(
        controller: controller,
        maxLines: null,
        expands: true,
        autofocus: true,
        keyboardType: TextInputType.multiline,
        textAlignVertical: TextAlignVertical.top,
        style: DesignTokens.of(context).mono(theme.textTheme.bodyMedium),
        decoration: InputDecoration(
          border: InputBorder.none,
          enabledBorder: InputBorder.none,
          focusedBorder: InputBorder.none,
          contentPadding: EdgeInsets.all(Space.gutter(context)),
          hintText: 'Start typing',
        ),
      ),
    );
  }
}

class _MarkdownView extends StatelessWidget {
  const _MarkdownView({required this.text});
  final String text;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final base = MarkdownStyleSheet.fromTheme(theme);
    return Align(
      alignment: Alignment.topCenter,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: Space.readingMaxWidth),
        child: Markdown(
          data: text,
          selectable: true,
          padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.lg, Space.gutter(context), Space.xxl),
          styleSheet: base.copyWith(
            code: DesignTokens.of(context).mono(theme.textTheme.bodyMedium).copyWith(backgroundColor: scheme.surfaceContainerHighest),
            codeblockDecoration: BoxDecoration(
              color: scheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(DesignTokens.of(context).radii.sm),
            ),
            codeblockPadding: const EdgeInsets.all(Space.md),
            blockquoteDecoration: BoxDecoration(
              color: scheme.surfaceContainer,
              border: Border(left: BorderSide(color: scheme.outlineVariant, width: 4)),
            ),
            blockSpacing: Space.md,
          ),
          onTapLink: (text, href, title) {
            final uri = href == null ? null : Uri.tryParse(href);
            if (uri != null && (uri.scheme == 'http' || uri.scheme == 'https')) {
              launchUrl(uri, mode: LaunchMode.externalApplication);
            }
          },
        ),
      ),
    );
  }
}

/// Parses CSV/TSV with quoted fields (commas and doubled quotes inside
/// quotes). Newlines inside quotes are kept within the field.
List<List<String>> parseDelimited(String text, {String delimiter = ','}) {
  final rows = <List<String>>[];
  var row = <String>[];
  final field = StringBuffer();
  var quoted = false;
  for (var i = 0; i < text.length; i++) {
    final c = text[i];
    if (quoted) {
      if (c == '"') {
        if (i + 1 < text.length && text[i + 1] == '"') {
          field.write('"');
          i++;
        } else {
          quoted = false;
        }
      } else {
        field.write(c);
      }
    } else if (c == '"') {
      quoted = true;
    } else if (c == delimiter) {
      row.add(field.toString());
      field.clear();
    } else if (c == '\n' || c == '\r') {
      if (c == '\r' && i + 1 < text.length && text[i + 1] == '\n') i++;
      row.add(field.toString());
      field.clear();
      if (row.any((f) => f.isNotEmpty) || row.length > 1) rows.add(row);
      row = <String>[];
    } else {
      field.write(c);
    }
  }
  if (field.isNotEmpty || row.isNotEmpty) {
    row.add(field.toString());
    rows.add(row);
  }
  return rows;
}

class _CsvView extends StatelessWidget {
  const _CsvView({required this.text, required this.tsv});
  final String text;
  final bool tsv;

  static const _maxRows = 500;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final rows = parseDelimited(text, delimiter: tsv ? '\t' : ',');
    if (rows.isEmpty) {
      return const EmptyState(icon: Icons.table_chart_outlined, title: 'No rows', message: 'This table has nothing in it.');
    }
    final header = rows.first;
    final body = rows.skip(1).take(_maxRows).toList();
    final width = rows.fold<int>(0, (m, r) => r.length > m ? r.length : m);
    String cell(List<String> r, int i) => i < r.length ? r[i] : '';
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.sm, Space.gutter(context), Space.sm),
          child: Text(
            '${formatCount(rows.length - 1, 'row')} · ${formatCount(width, 'column')}${rows.length - 1 > _maxRows ? ' · showing the first $_maxRows' : ''}',
            style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant).tabular,
          ),
        ),
        Expanded(
          child: SelectionArea(
            child: SingleChildScrollView(
              child: SingleChildScrollView(
                scrollDirection: Axis.horizontal,
                padding: EdgeInsets.symmetric(horizontal: Space.gutter(context) - Space.sm),
                child: DataTable(
                  headingRowColor: WidgetStatePropertyAll(scheme.surfaceContainer),
                  columns: [for (var i = 0; i < width; i++) DataColumn(label: Text(cell(header, i)))],
                  rows: [
                    for (final r in body) DataRow(cells: [for (var i = 0; i < width; i++) DataCell(Text(cell(r, i)))]),
                  ],
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// PDF

class _PdfView extends StatefulWidget {
  const _PdfView({required this.path, required this.onOpenWith});
  final String path;
  final VoidCallback onOpenWith;

  @override
  State<_PdfView> createState() => _PdfViewState();
}

class _PdfViewState extends State<_PdfView> {
  PDFViewController? _controller;
  int _pages = 0;
  int _page = 0;
  String? _error;

  Future<void> _jump() async {
    final controller = TextEditingController(text: '${_page + 1}');
    final page = await showDialog<int>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Go to page'),
        content: TextField(
          controller: controller,
          autofocus: true,
          keyboardType: TextInputType.number,
          decoration: InputDecoration(labelText: 'Page (1 to $_pages)'),
          onSubmitted: (v) => Navigator.of(context).pop(int.tryParse(v.trim())),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
          TextButton(onPressed: () => Navigator.of(context).pop(int.tryParse(controller.text.trim())), child: const Text('Go')),
        ],
      ),
    );
    controller.dispose();
    if (page != null && page >= 1 && page <= _pages) await _controller?.setPage(page - 1);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    if (_error != null) {
      return EmptyState(
        icon: Icons.picture_as_pdf_outlined,
        title: "Couldn't show this PDF",
        message: _error!,
        actionLabel: 'Open with another app',
        onAction: widget.onOpenWith,
      );
    }
    return Stack(
      children: [
        PDFView(
          filePath: widget.path,
          autoSpacing: true,
          pageSnap: false,
          pageFling: false,
          fitPolicy: FitPolicy.WIDTH,
          backgroundColor: scheme.surfaceContainerHighest,
          onRender: (pages) => setState(() => _pages = pages ?? 0),
          onViewCreated: (c) => _controller = c,
          onPageChanged: (page, total) => setState(() {
            _page = page ?? 0;
            if (total != null && total > 0) _pages = total;
          }),
          onError: (e) => setState(() => _error = e.toString()),
          onPageError: (page, e) => setState(() => _error = 'Page ${(page ?? 0) + 1}: $e'),
        ),
        if (_pages > 1)
          Positioned(
            left: 0,
            right: 0,
            bottom: Space.lg + MediaQuery.paddingOf(context).bottom,
            child: Center(
              child: Material(
                color: scheme.surfaceContainerHigh,
                shape: RoundedRectangleBorder(
                  borderRadius: BorderRadius.circular(DesignTokens.of(context).radii.xl),
                  side: DesignTokens.of(context).cardBorder == null ? BorderSide.none : BorderSide(color: DesignTokens.of(context).cardBorder!),
                ),
                elevation: 2,
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    IconButton(
                      icon: const Icon(Icons.chevron_left),
                      tooltip: 'Previous page',
                      onPressed: _page > 0 ? () => _controller?.setPage(_page - 1) : null,
                    ),
                    Semantics(
                      button: true,
                      label: 'Page ${_page + 1} of $_pages. Go to page',
                      excludeSemantics: true,
                      child: TextButton(
                        onPressed: _jump,
                        child: Text('${_page + 1} of $_pages', style: theme.textTheme.labelLarge?.tabular),
                      ),
                    ),
                    IconButton(
                      icon: const Icon(Icons.chevron_right),
                      tooltip: 'Next page',
                      onPressed: _page < _pages - 1 ? () => _controller?.setPage(_page + 1) : null,
                    ),
                  ],
                ),
              ),
            ),
          ),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// Pictures

/// Full-screen pictures on a dark page (media is judged on dark, whatever
/// the app's theme), swiping through the folder's pictures, pinch and
/// double-tap zoom.
class _ImageGallery extends StatefulWidget {
  const _ImageGallery({required this.files, required this.initial, required this.isLocal, required this.onDetails});

  final List<FileEntry> files;
  final FileEntry initial;
  final bool isLocal;
  final void Function(FileEntry) onDetails;

  @override
  State<_ImageGallery> createState() => _ImageGalleryState();
}

class _ImageGalleryState extends State<_ImageGallery> {
  late int _index = widget.files.indexWhere((f) => f.path == widget.initial.path).clamp(0, widget.files.length - 1);
  late final _pages = PageController(initialPage: _index);
  bool _zoomed = false;
  String? _token = ApiClient.instance.accessToken;
  int _retry = 0;

  @override
  void initState() {
    super.initState();
    // Pictures load with a header, not through ApiClient, so start with a
    // token that won't expire mid-swipe.
    ApiClient.instance.freshToken().then((t) {
      if (mounted && t != _token) setState(() => _token = t);
    });
  }

  @override
  void dispose() {
    _pages.dispose();
    super.dispose();
  }

  FileEntry get _current => widget.files[_index];

  Future<void> _reload() async {
    final t = await ApiClient.instance.freshToken();
    if (!mounted) return;
    PaintingBinding.instance.imageCache.clear();
    setState(() {
      _token = t;
      _retry++;
    });
  }

  @override
  Widget build(BuildContext context) {
    final dark = AppTheme.dark();
    final multiple = widget.files.length > 1;
    return Theme(
      data: dark,
      child: Builder(
        builder: (context) {
          final scheme = Theme.of(context).colorScheme;
          return Scaffold(
            backgroundColor: scheme.surfaceContainerLowest,
            extendBodyBehindAppBar: true,
            appBar: AppBar(
              backgroundColor: scheme.surfaceContainerLowest.withValues(alpha: 0.7),
              systemOverlayStyle: AppTheme.systemBarsStyle(Brightness.dark),
              title: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(_current.name, maxLines: 1, overflow: TextOverflow.ellipsis),
                  if (multiple)
                    Text(
                      '${_index + 1} of ${widget.files.length}',
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant).tabular,
                    ),
                ],
              ),
              actions: [
                IconButton(icon: const Icon(Icons.info_outline), tooltip: 'Details', onPressed: () => widget.onDetails(_current)),
                PopupMenuButton<String>(
                  tooltip: 'More options',
                  onSelected: (v) {
                    if (v == 'open') openWithAnotherApp(context, _current, isLocal: widget.isLocal);
                    if (v == 'save') saveToPhone(context, _current);
                    if (v == 'path') {
                      Clipboard.setData(ClipboardData(text: _current.path));
                      ScaffoldMessenger.maybeOf(context)?.showSnackBar(const SnackBar(content: Text('Path copied')));
                    }
                  },
                  itemBuilder: (context) => [
                    const PopupMenuItem(value: 'open', child: Text('Open with another app')),
                    if (!widget.isLocal) const PopupMenuItem(value: 'save', child: Text('Save to phone')),
                    const PopupMenuItem(value: 'path', child: Text('Copy path')),
                  ],
                ),
              ],
            ),
            body: PageView.builder(
              controller: _pages,
              physics: _zoomed ? const NeverScrollableScrollPhysics() : const PageScrollPhysics(),
              itemCount: widget.files.length,
              onPageChanged: (i) => setState(() {
                _index = i;
                _zoomed = false;
              }),
              itemBuilder: (context, i) => _ZoomableImage(
                key: ValueKey('${widget.files[i].path}#$_retry'),
                file: widget.files[i],
                isLocal: widget.isLocal,
                token: _token,
                onZoomChanged: (z) {
                  if (z != _zoomed) setState(() => _zoomed = z);
                },
                onRetry: _reload,
              ),
            ),
          );
        },
      ),
    );
  }
}

class _ZoomableImage extends StatefulWidget {
  const _ZoomableImage({super.key, required this.file, required this.isLocal, required this.token, required this.onZoomChanged, required this.onRetry});

  final FileEntry file;
  final bool isLocal;
  final String? token;
  final ValueChanged<bool> onZoomChanged;
  final VoidCallback onRetry;

  @override
  State<_ZoomableImage> createState() => _ZoomableImageState();
}

class _ZoomableImageState extends State<_ZoomableImage> with SingleTickerProviderStateMixin {
  final _transform = TransformationController();
  late final _animation = AnimationController(vsync: this, duration: Motion.medium);
  Offset _tap = Offset.zero;

  @override
  void dispose() {
    _transform.dispose();
    _animation.dispose();
    super.dispose();
  }

  void _animateTo(Matrix4 target) {
    final tween = Matrix4Tween(begin: _transform.value, end: target);
    final curved = CurvedAnimation(parent: _animation, curve: Motion.standard);
    void tick() => _transform.value = tween.evaluate(curved);
    _animation
      ..duration = Motion.of(context).medium
      ..reset();
    _animation.addListener(tick);
    _animation.forward().whenCompleteOrCancel(() => _animation.removeListener(tick));
  }

  void _doubleTap() {
    HapticFeedback.selectionClick();
    final zoomed = _transform.value.getMaxScaleOnAxis() > 1.01;
    if (zoomed) {
      _animateTo(Matrix4.identity());
      widget.onZoomChanged(false);
    } else {
      const s = 2.5;
      _animateTo(Matrix4.identity()
        ..translateByDouble(-_tap.dx * (s - 1), -_tap.dy * (s - 1), 0, 1)
        ..scaleByDouble(s, s, 1, 1));
      widget.onZoomChanged(true);
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final f = widget.file;
    Widget error(BuildContext context, Object e, StackTrace? _) => _ImageError(file: f, onRetry: widget.isLocal ? null : widget.onRetry);
    Widget loading() => Center(child: CircularProgressIndicator(color: scheme.onSurfaceVariant));
    final Widget image;
    if (f.isSvg) {
      image = widget.isLocal
          ? SvgPicture.file(File(f.path), fit: BoxFit.contain, placeholderBuilder: (_) => loading())
          : SvgPicture.network(
              ApiClient.instance.buildUri('/file', {'path': f.path}).toString(),
              headers: {if (widget.token != null) 'Authorization': widget.token!},
              fit: BoxFit.contain,
              placeholderBuilder: (_) => loading(),
              errorBuilder: error,
            );
    } else if (widget.isLocal) {
      image = Image.file(File(f.path), fit: BoxFit.contain, errorBuilder: error);
    } else {
      image = Image.network(
        ApiClient.instance.buildUri('/file', {'path': f.path}).toString(),
        headers: {if (widget.token != null) 'Authorization': widget.token!},
        fit: BoxFit.contain,
        errorBuilder: error,
        loadingBuilder: (context, child, p) {
          if (p == null) return child;
          final total = p.expectedTotalBytes;
          return Center(
            child: CircularProgressIndicator(
              value: total == null ? null : p.cumulativeBytesLoaded / total,
              color: scheme.onSurfaceVariant,
            ),
          );
        },
      );
    }
    return Semantics(
      image: true,
      label: f.name,
      child: GestureDetector(
        onDoubleTapDown: (d) => _tap = d.localPosition,
        onDoubleTap: _doubleTap,
        child: InteractiveViewer(
          transformationController: _transform,
          minScale: 1,
          maxScale: 8,
          onInteractionEnd: (_) => widget.onZoomChanged(_transform.value.getMaxScaleOnAxis() > 1.01),
          child: SizedBox.expand(child: image),
        ),
      ),
    );
  }
}

class _ImageError extends StatelessWidget {
  const _ImageError({required this.file, this.onRetry});
  final FileEntry file;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) {
    final decodable = file.hasThumbnail || file.isSvg;
    return EmptyState(
      icon: Icons.broken_image_outlined,
      title: decodable ? "Couldn't load this picture" : 'This picture can’t be shown here',
      message: decodable
          ? 'Check the connection to the server and try again.'
          : '${file.extension.toUpperCase()} pictures open in another app. Use More options.',
      actionLabel: decodable && onRetry != null ? 'Retry' : null,
      onAction: onRetry,
    );
  }
}

// ---------------------------------------------------------------------------
// Video and audio

/// Plays straight from the server (the file route supports ranges, so it
/// streams and seeks) or from this phone. When the phone can't decode the
/// file, it offers the server's compatible stream (`/v1/file/stream`
/// remuxes MKV and friends) and another app.
class _MediaPlayer extends StatefulWidget {
  const _MediaPlayer({
    required this.file,
    required this.isLocal,
    required this.audio,
    required this.onOpenWith,
    required this.onDetails,
    this.onSave,
  });

  final FileEntry file;
  final bool isLocal;
  final bool audio;
  final VoidCallback onOpenWith;
  final VoidCallback? onSave;
  final VoidCallback onDetails;

  @override
  State<_MediaPlayer> createState() => _MediaPlayerState();
}

class _MediaPlayerState extends State<_MediaPlayer> {
  VideoPlayerController? _video;
  ChewieController? _chewie;
  Object? _error;
  bool _compatible = false;

  @override
  void initState() {
    super.initState();
    _start();
  }

  Future<void> _start() async {
    _chewie?.dispose();
    await _video?.dispose();
    setState(() {
      _video = null;
      _chewie = null;
      _error = null;
    });
    try {
      final VideoPlayerController controller;
      if (widget.isLocal) {
        controller = VideoPlayerController.file(File(widget.file.path));
      } else {
        final token = await ApiClient.instance.freshToken();
        final uri = ApiClient.instance.buildUri(_compatible ? '/file/stream' : '/file', {'path': widget.file.path});
        controller = VideoPlayerController.networkUrl(uri, httpHeaders: {'Authorization': ?token});
      }
      await controller.initialize();
      if (!mounted) {
        await controller.dispose();
        return;
      }
      controller.addListener(_onTick);
      final scheme = AppTheme.dark().colorScheme;
      final aspect = controller.value.aspectRatio;
      setState(() {
        _video = controller;
        if (!widget.audio) {
          _chewie = ChewieController(
            videoPlayerController: controller,
            autoPlay: true,
            aspectRatio: aspect.isFinite && aspect > 0 ? aspect : 16 / 9,
            allowPlaybackSpeedChanging: true,
            materialProgressColors: ChewieProgressColors(
              playedColor: scheme.primary,
              handleColor: scheme.primary,
              bufferedColor: scheme.onSurfaceVariant,
              backgroundColor: scheme.surfaceContainerHighest,
            ),
          );
        }
      });
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  void _onTick() {
    if (mounted && widget.audio) setState(() {});
  }

  @override
  void dispose() {
    _video?.removeListener(_onTick);
    _chewie?.dispose();
    _video?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final page = widget.audio ? Theme.of(context) : AppTheme.dark();
    return Theme(
      data: page,
      child: Builder(builder: (context) {
        final scheme = Theme.of(context).colorScheme;
        return Scaffold(
          backgroundColor: widget.audio ? scheme.surface : scheme.surfaceContainerLowest,
          appBar: AppBar(
            backgroundColor: widget.audio ? null : scheme.surfaceContainerLowest,
            systemOverlayStyle: widget.audio ? null : AppTheme.systemBarsStyle(Brightness.dark),
            title: Text(widget.file.name, maxLines: 1, overflow: TextOverflow.ellipsis),
            actions: [
              IconButton(icon: const Icon(Icons.info_outline), tooltip: 'Details', onPressed: widget.onDetails),
              PopupMenuButton<String>(
                tooltip: 'More options',
                onSelected: (v) {
                  if (v == 'open') widget.onOpenWith();
                  if (v == 'save') widget.onSave?.call();
                },
                itemBuilder: (context) => [
                  const PopupMenuItem(value: 'open', child: Text('Open with another app')),
                  if (widget.onSave != null) const PopupMenuItem(value: 'save', child: Text('Save to phone')),
                ],
              ),
            ],
          ),
          body: SafeArea(top: false, child: _body(context)),
        );
      }),
    );
  }

  Widget _body(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    if (_error != null) {
      final canRemux = !widget.isLocal && !widget.audio && !_compatible;
      return LayoutBuilder(
        builder: (context, c) => SingleChildScrollView(
          padding: EdgeInsets.symmetric(horizontal: Space.gutter(context), vertical: Space.xl),
          child: ConstrainedBox(
            constraints: BoxConstraints(minHeight: (c.maxHeight - Space.xl * 2).clamp(0, double.infinity)),
            child: Center(
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 360),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(widget.audio ? Icons.audio_file_outlined : Icons.movie_outlined, size: 48, color: scheme.onSurfaceVariant),
                    const SizedBox(height: Space.lg),
                    Text("This phone can't play it", style: Theme.of(context).textTheme.titleLarge, textAlign: TextAlign.center),
                    const SizedBox(height: Space.sm),
                    Text(
                      canRemux
                          ? 'The server can convert it to a format this phone plays. It may take a moment to start.'
                          : 'Try another app, like VLC.',
                      style: Theme.of(context).textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
                      textAlign: TextAlign.center,
                    ),
                    const SizedBox(height: Space.xl),
                    Wrap(
                      alignment: WrapAlignment.center,
                      spacing: Space.sm,
                      runSpacing: Space.sm,
                      children: [
                        if (canRemux)
                          FilledButton(
                            onPressed: () {
                              _compatible = true;
                              _start();
                            },
                            child: const Text('Play converted'),
                          ),
                        OutlinedButton.icon(
                          onPressed: widget.onOpenWith,
                          icon: const Icon(Icons.open_in_new_outlined),
                          label: const Text('Open with another app'),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      );
    }
    final video = _video;
    if (video == null) {
      return Semantics(
        label: 'Loading',
        liveRegion: true,
        child: Center(child: CircularProgressIndicator(color: scheme.onSurfaceVariant)),
      );
    }
    if (!widget.audio) return Center(child: Chewie(controller: _chewie!));
    return _AudioControls(file: widget.file, controller: video);
  }
}

class _AudioControls extends StatelessWidget {
  const _AudioControls({required this.file, required this.controller});
  final FileEntry file;
  final VideoPlayerController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final v = controller.value;
    final duration = v.duration;
    final position = v.position > duration ? duration : v.position;
    void seek(Duration d) => controller.seekTo(d < Duration.zero ? Duration.zero : (d > duration ? duration : d));
    return Center(
      child: SingleChildScrollView(
        padding: EdgeInsets.all(Space.gutter(context)),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 480),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              ExcludeSemantics(
                child: Container(
                  width: 160,
                  height: 160,
                  decoration: BoxDecoration(color: scheme.surfaceContainerHigh, borderRadius: BorderRadius.circular(DesignTokens.of(context).radii.xl)),
                  child: Icon(Icons.music_note_outlined, size: 64, color: scheme.onSurfaceVariant),
                ),
              ),
              const SizedBox(height: Space.xl),
              Text(file.name, style: theme.textTheme.titleLarge, textAlign: TextAlign.center),
              const SizedBox(height: Space.xs),
              Text(formatSize(file.size), style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
              const SizedBox(height: Space.xl),
              Slider(
                value: duration.inMilliseconds > 0 ? position.inMilliseconds / duration.inMilliseconds : 0,
                onChanged: duration.inMilliseconds > 0 ? (x) => seek(duration * x) : null,
                semanticFormatterCallback: (_) => '${formatDuration(position)} of ${formatDuration(duration)}',
              ),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: Space.xl),
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                  children: [
                    Text(formatDuration(position), style: theme.textTheme.bodySmall?.tabular),
                    Text(formatDuration(duration), style: theme.textTheme.bodySmall?.tabular),
                  ],
                ),
              ),
              const SizedBox(height: Space.lg),
              Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  IconButton(icon: const Icon(Icons.replay_10), tooltip: 'Back 10 seconds', onPressed: () => seek(position - const Duration(seconds: 10))),
                  const SizedBox(width: Space.lg),
                  IconButton.filled(
                    iconSize: 40,
                    padding: const EdgeInsets.all(Space.md),
                    icon: Icon(v.isPlaying ? Icons.pause : Icons.play_arrow),
                    tooltip: v.isPlaying ? 'Pause' : 'Play',
                    onPressed: () => v.isPlaying ? controller.pause() : controller.play(),
                  ),
                  const SizedBox(width: Space.lg),
                  IconButton(icon: const Icon(Icons.forward_10), tooltip: 'Forward 10 seconds', onPressed: () => seek(position + const Duration(seconds: 10))),
                ],
              ),
              const SizedBox(height: Space.sm),
              TextButton(
                onPressed: () {
                  const speeds = [1.0, 1.25, 1.5, 2.0, 0.75];
                  final i = speeds.indexOf(v.playbackSpeed);
                  controller.setPlaybackSpeed(speeds[(i + 1) % speeds.length]);
                },
                child: Text('Speed ${v.playbackSpeed == v.playbackSpeed.roundToDouble() ? v.playbackSpeed.toStringAsFixed(0) : v.playbackSpeed}×'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
