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
}
