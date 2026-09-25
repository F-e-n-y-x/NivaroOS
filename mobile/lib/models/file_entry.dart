/// What a file is, for its icon, its label and which viewer opens it.
///
/// One kind, one glyph (see `fileKindIcon` in screens/files/file_widgets.dart): the
/// list, the grid, the viewer and the info sheet all show the same icon for
/// the same kind.
enum FileKind {
  folder,
  image,
  video,
  audio,
  pdf,
  document,
  spreadsheet,
  presentation,
  archive,
  code,
  text,
  other,
}

/// A file or folder, from a server listing (`GET /v1/folder`) or from this
/// phone's own storage.
class FileEntry {
  final String name;
  final String path;
  final bool isDir;
  final int size;
  final DateTime? modified;

  const FileEntry({
    required this.name,
    required this.path,
    required this.isDir,
    required this.size,
    this.modified,
  });

  factory FileEntry.fromJson(Map<String, dynamic> j) {
    DateTime? mod;
    final modRaw = j['modified'] ?? j['updated_at'] ?? j['date'];
    if (modRaw is String) {
      mod = DateTime.tryParse(modRaw);
    } else if (modRaw is num) {
      // Seconds since the epoch; a value that large is already in ms.
      final v = modRaw.toInt();
      mod = DateTime.fromMillisecondsSinceEpoch(v > 100000000000 ? v : v * 1000);
    }
    final path = j['path'] as String? ?? '';
    var name = j['name'] as String? ?? '';
    if (name.isEmpty && path.isNotEmpty) {
      name = path.split('/').where((s) => s.isNotEmpty).lastOrNull ?? path;
    }
    return FileEntry(
      name: name,
      path: path,
      isDir: j['is_dir'] as bool? ?? false,
      size: (j['size'] as num?)?.toInt() ?? 0,
      modified: mod,
    );
  }

  FileEntry copyWith({String? name, String? path, bool? isDir, int? size, DateTime? modified}) => FileEntry(
        name: name ?? this.name,
        path: path ?? this.path,
        isDir: isDir ?? this.isDir,
        size: size ?? this.size,
        modified: modified ?? this.modified,
      );

  /// Dot files (".git", ".env"), hidden by default like on the server.
  bool get isHidden => name.startsWith('.');

  /// The lower-case extension without the dot, or '' when there is none
  /// (".bashrc" has none: the dot starts the name).
  String get extension {
    final dot = name.lastIndexOf('.');
    if (dot <= 0 || dot == name.length - 1) return '';
    return name.substring(dot + 1).toLowerCase();
  }

  /// The name without its extension ("report.pdf" → "report").
  String get stem {
    final ext = extension;
    return ext.isEmpty ? name : name.substring(0, name.length - ext.length - 1);
  }

  static const _images = {
    'png', 'jpg', 'jpeg', 'webp', 'gif', 'bmp', 'svg', 'heic', 'heif', 'ico', 'tiff', 'tif', 'avif',
  };
  static const _videos = {
    'mp4', 'mkv', 'webm', 'mov', 'avi', 'flv', 'ts', 'm4v', '3gp', 'wmv', 'mpg', 'mpeg', 'ogv',
    'vob', 'm2ts', 'mts', 'f4v', 'divx', 'asf', 'rmvb', 'm3u8',
  };
  static const _audio = {
    'mp3', 'flac', 'wav', 'aac', 'ogg', 'm4a', 'opus', 'wma', 'aiff', 'alac', 'mid', 'midi', 'amr',
  };
  static const _archives = {
    'zip', 'tar', 'gz', 'tgz', 'bz2', 'tbz2', 'xz', 'txz', '7z', 'rar', 'apk', 'deb', 'rpm', 'iso', 'img', 'zst',
  };

  /// Archives the server can extract (`POST /v1/file/unarchive`, which uses
  /// mholt/archiver: zip, tar and compressed tars, rar, 7z).
  static const _extractable = {'zip', 'tar', 'gz', 'tgz', 'bz2', 'tbz2', 'xz', 'txz', '7z', 'rar', 'zst'};
  static const _documents = {'doc', 'docx', 'odt', 'rtf', 'epub', 'pages'};
  static const _spreadsheets = {'xls', 'xlsx', 'ods', 'numbers'};
  static const _presentations = {'ppt', 'pptx', 'odp', 'key'};
  static const _code = {
    'dart', 'js', 'ts', 'jsx', 'tsx', 'py', 'go', 'rs', 'c', 'cpp', 'h', 'hpp', 'cs', 'java',
    'kt', 'kts', 'scala', 'rb', 'php', 'sh', 'bash', 'zsh', 'fish', 'ps1', 'sql', 'html', 'htm',
    'css', 'scss', 'sass', 'less', 'xml', 'yaml', 'yml', 'toml', 'ini', 'conf', 'cfg', 'env',
    'dockerfile', 'gitignore', 'gitattributes', 'properties', 'gradle', 'vue',
    'svelte', 'lua', 'swift', 'r', 'pl', 'pm', 'asm', 's', 'vim', 'lock', 'cmake', 'makefile',
  };
  static const _plainText = {
    'txt', 'text', 'log', 'out', 'err', 'diff', 'patch', 'spec', 'info', 'license', 'readme', 'authors', 'nfo',
  };
  static const _textNames = {'dockerfile', 'makefile', 'license', 'readme', 'changelog', 'authors'};

  bool get isImage => !isDir && _images.contains(extension);
  bool get isSvg => !isDir && extension == 'svg';
  bool get isVideo => !isDir && _videos.contains(extension);
  bool get isAudio => !isDir && _audio.contains(extension);
  bool get isPdf => !isDir && extension == 'pdf';
  bool get isMarkdown => !isDir && const {'md', 'markdown', 'mdown', 'mkdn', 'mkd'}.contains(extension);
  bool get isJson => !isDir && const {'json', 'jsonc', 'json5'}.contains(extension);
  bool get isCsv => !isDir && (extension == 'csv' || extension == 'tsv');
  bool get isArchive => !isDir && _archives.contains(extension);
  bool get isExtractable => !isDir && _extractable.contains(extension);
  bool get isOffice =>
      !isDir && (_documents.contains(extension) || _spreadsheets.contains(extension) || _presentations.contains(extension));
  bool get isCode => !isDir && _code.contains(extension);

  bool get isText =>
      !isDir &&
      (isCode ||
          isMarkdown ||
          isJson ||
          isCsv ||
          _plainText.contains(extension) ||
          (isHidden && extension.isEmpty) ||
          _textNames.contains(name.toLowerCase()));

  /// Pictures Flutter itself can decode, so a thumbnail or preview is
  /// worth asking for. (HEIC, TIFF and friends open in another app.)
  bool get hasThumbnail => !isDir && const {'png', 'jpg', 'jpeg', 'webp', 'gif', 'bmp'}.contains(extension);

  FileKind get kind {
    if (isDir) return FileKind.folder;
    if (isImage) return FileKind.image;
    if (isVideo) return FileKind.video;
    if (isAudio) return FileKind.audio;
    if (isPdf) return FileKind.pdf;
    if (_spreadsheets.contains(extension) || isCsv) return FileKind.spreadsheet;
    if (_presentations.contains(extension)) return FileKind.presentation;
    if (_documents.contains(extension)) return FileKind.document;
    if (isArchive) return FileKind.archive;
    if (isCode || isJson) return FileKind.code;
    if (isText) return FileKind.text;
    return FileKind.other;
  }

  /// A short, sentence-case description: "Image", "PDF document",
  /// "ZIP archive", "Folder".
  String get categoryLabel {
    final lower = name.toLowerCase();
    final compoundTar = const ['.tar.gz', '.tar.bz2', '.tar.xz', '.tar.zst'].where(lower.endsWith).firstOrNull;
    final ext = (compoundTar?.substring(1) ?? extension).toUpperCase();
    return switch (kind) {
      FileKind.folder => 'Folder',
      FileKind.image => isSvg ? 'SVG image' : 'Image',
      FileKind.video => 'Video',
      FileKind.audio => 'Audio',
      FileKind.pdf => 'PDF document',
      FileKind.spreadsheet => isCsv ? '$ext table' : 'Spreadsheet',
      FileKind.presentation => 'Presentation',
      FileKind.document => 'Document',
      FileKind.archive => '$ext archive',
      FileKind.code => isJson ? 'JSON' : 'Code',
      FileKind.text => isMarkdown ? 'Markdown' : 'Text',
      FileKind.other => ext.isEmpty ? 'File' : '$ext file',
    };
  }

  @override
  bool operator ==(Object other) =>
      other is FileEntry &&
      other.path == path &&
      other.name == name &&
      other.isDir == isDir &&
      other.size == size &&
      other.modified == modified;

  @override
  int get hashCode => Object.hash(path, name, isDir, size, modified);

  @override
  String toString() => 'FileEntry($path${isDir ? '/' : ''}, $size)';
}
