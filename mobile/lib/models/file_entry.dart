class FileEntry {
  final String name;
  final String path;
  final bool isDir;
  final int size;
  final DateTime? modified;

  FileEntry({
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
      mod = DateTime.fromMillisecondsSinceEpoch(modRaw.toInt() * 1000);
    }
    return FileEntry(
      name: j['name'] as String? ?? '',
      path: j['path'] as String? ?? '',
      isDir: j['is_dir'] as bool? ?? false,
      size: (j['size'] as num?)?.toInt() ?? 0,
      modified: mod,
    );
  }

  String get extension {
    final dot = name.lastIndexOf('.');
    if (dot <= 0 || dot == name.length - 1) return '';
    return name.substring(dot + 1).toLowerCase();
  }

  bool get isImage => const [
        'png', 'jpg', 'jpeg', 'webp', 'gif', 'bmp', 'svg', 'heic', 'heif', 'ico', 'tiff', 'tif', 'avif'
      ].contains(extension);

  bool get isSvg => extension == 'svg';

  bool get isVideo => const [
        'mp4', 'mkv', 'webm', 'mov', 'avi', 'flv', 'ts', 'm4v', '3gp', 'wmv', 'mpg', 'mpeg', 'ogv',
        'vob', 'm2ts', 'mts', 'f4v', 'divx', 'asf', 'rmvb', 'm3u8'
      ].contains(extension);

  bool get isAudio => const [
        'mp3', 'flac', 'wav', 'aac', 'ogg', 'm4a', 'opus', 'wma', 'aiff', 'alac', 'mid', 'midi', 'amr'
      ].contains(extension);

  bool get isPdf => extension == 'pdf';

  bool get isMarkdown => const ['md', 'markdown', 'mdown', 'mkdn', 'mkd'].contains(extension);

  bool get isJson => extension == 'json' || extension == 'jsonc' || extension == 'json5';

  bool get isCsv => extension == 'csv' || extension == 'tsv';

  bool get isArchive => const [
        'zip', 'tar', 'gz', 'tgz', 'bz2', 'tbz2', 'xz', 'txz', '7z', 'rar', 'apk', 'deb', 'rpm', 'iso', 'img', 'zst'
      ].contains(extension);

  bool get isOffice => const [
        'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'odt', 'ods', 'odp', 'rtf', 'epub', 'pages', 'numbers', 'key'
      ].contains(extension);

  bool get isCode => const [
        'dart', 'js', 'ts', 'jsx', 'tsx', 'py', 'go', 'rs', 'c', 'cpp', 'h', 'hpp', 'cs', 'java',
        'kt', 'kts', 'scala', 'rb', 'php', 'sh', 'bash', 'zsh', 'fish', 'ps1', 'sql', 'html', 'htm',
        'css', 'scss', 'sass', 'less', 'xml', 'yaml', 'yml', 'toml', 'ini', 'conf', 'cfg', 'env',
        'log', 'txt', 'dockerfile', 'gitignore', 'gitattributes', 'properties', 'gradle', 'vue',
        'svelte', 'lua', 'swift', 'r', 'pl', 'pm', 'asm', 's', 'vim', 'lock', 'cmake', 'makefile'
      ].contains(extension);

  bool get isText =>
      isCode ||
      isMarkdown ||
      isJson ||
      isCsv ||
      const [
        'txt', 'text', 'log', 'out', 'err', 'diff', 'patch', 'spec', 'info', 'license', 'readme', 'authors'
      ].contains(extension) ||
      name.startsWith('.') ||
      name.toLowerCase() == 'dockerfile' ||
      name.toLowerCase() == 'makefile' ||
      name.toLowerCase() == 'license' ||
      name.toLowerCase() == 'readme';

  String get categoryLabel {
    if (isVideo) return 'Video';
    if (isAudio) return 'Audio';
    if (isPdf) return 'PDF Document';
    if (isImage) return isSvg ? 'Vector SVG' : 'Image';
    if (isMarkdown) return 'Markdown';
    if (isJson) return 'JSON Data';
    if (isCsv) return extension == 'tsv' ? 'TSV Spreadsheet' : 'CSV Spreadsheet';
    if (isCode) return 'Source Code';
    if (isText) return 'Text File';
    if (isArchive) return 'Archive';
    if (isOffice) return 'Office Document';
    return extension.isNotEmpty ? '${extension.toUpperCase()} File' : 'Binary File';
  }
}
