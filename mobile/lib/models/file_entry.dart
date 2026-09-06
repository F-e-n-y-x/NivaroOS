class FileEntry {
  final String name;
  final String path;
  final bool isDir;
  final int size;

  FileEntry({required this.name, required this.path, required this.isDir, required this.size});

  factory FileEntry.fromJson(Map<String, dynamic> j) => FileEntry(
        name: j['name'] as String? ?? '',
        path: j['path'] as String? ?? '',
        isDir: j['is_dir'] as bool? ?? false,
        size: (j['size'] as num?)?.toInt() ?? 0,
      );

  String get extension {
    final dot = name.lastIndexOf('.');
    if (dot <= 0 || dot == name.length - 1) return '';
    return name.substring(dot + 1).toLowerCase();
  }
}
