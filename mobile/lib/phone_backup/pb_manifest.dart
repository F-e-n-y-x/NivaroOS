// The phone's own list of what a files category held at the last backup
// (path, size, mtime, SHA-256). It saves re-hashing: a file whose size and
// mtime are unchanged is offered to the check with its known hash, and
// the server answers `have` without the phone reading it again. It also
// tells which files disappeared since, for the deleted report.
import 'dart:convert';

import 'pb_models.dart';

/// A file found on the phone by a scan.
class ScannedFile {
  const ScannedFile({required this.path, required this.size, required this.mtime, required this.source, this.takenAt = 0, this.mediaId = 0});

  /// The backup path ("DCIM/Camera/PXL_1.jpg").
  final String path;
  final int size;

  /// Unix milliseconds.
  final int mtime;

  /// Where to read it: a content:// URI or a file path.
  final String source;
  final int takenAt;
  final int mediaId;
}

class ManifestEntry {
  const ManifestEntry(this.size, this.mtime, this.sha256);
  final int size;
  final int mtime;
  final String sha256;

  List<Object> toJson() => [size, mtime, sha256];

  static ManifestEntry? fromJson(Object? j) {
    if (j is! List || j.length < 3) return null;
    final s = j[0], m = j[1], h = j[2];
    if (s is! num || m is! num || h is! String) return null;
    return ManifestEntry(s.toInt(), m.toInt(), h);
  }
}

class LocalManifest {
  LocalManifest([Map<String, ManifestEntry>? entries]) : entries = entries ?? {};

  final Map<String, ManifestEntry> entries;
  bool dirty = false;

  /// The known hash of [f], if the file is unchanged since it was hashed.
  String knownHash(ScannedFile f) {
    final e = entries[f.path];
    return e != null && e.size == f.size && e.mtime == f.mtime ? e.sha256 : '';
  }

  CheckItem checkItem(ScannedFile f, {String? sha256}) => CheckItem(path: f.path, size: f.size, mtime: f.mtime, sha256: sha256 ?? knownHash(f));

  void record(ScannedFile f, String sha256) {
    final e = entries[f.path];
    if (e != null && e.size == f.size && e.mtime == f.mtime && e.sha256 == sha256) return;
    entries[f.path] = ManifestEntry(f.size, f.mtime, sha256);
    dirty = true;
  }

  /// Paths known here that [present] (this scan) no longer has, sorted.
  List<String> gone(Iterable<String> present) {
    final now = present is Set<String> ? present : present.toSet();
    return entries.keys.where((p) => !now.contains(p)).toList()..sort();
  }

  void forget(Iterable<String> paths) {
    for (final p in paths) {
      if (entries.remove(p) != null) dirty = true;
    }
  }

  String encode() => jsonEncode({for (final e in entries.entries) e.key: e.value.toJson()});

  static LocalManifest decode(String s) {
    try {
      final j = jsonDecode(s);
      if (j is! Map) return LocalManifest();
      final out = <String, ManifestEntry>{};
      j.forEach((k, v) {
        final e = ManifestEntry.fromJson(v);
        if (e != null) out[k.toString()] = e;
      });
      return LocalManifest(out);
    } catch (_) {
      return LocalManifest();
    }
  }
}

/// A backup path is valid as the server checks it (§3): relative,
/// '/'-separated UTF-8, at most 4096 bytes, segments at most 255 bytes, no
/// empty/./.. segment, no NUL, backslash or control character.
bool validBackupPath(String p) {
  if (p.isEmpty || p.startsWith('/')) return false;
  if (utf8.encode(p).length > 4096) return false;
  for (final r in p.runes) {
    if (r < 0x20 || r == 0x7F || r == 0x5C) return false;
  }
  for (final seg in p.split('/')) {
    if (seg.isEmpty || seg == '.' || seg == '..') return false;
    if (utf8.encode(seg).length > 255) return false;
  }
  return true;
}

/// A segment for a backup path from a free name (a SAF folder's label, a
/// calendar name): no slashes or control characters, not empty.
String safeSegment(String s) {
  var t = s.replaceAll(RegExp(r'[\x00-\x1f\x7f/\\]'), '_').trim();
  if (t.isEmpty || t == '.' || t == '..') t = '_';
  while (utf8.encode(t).length > 255) {
    t = t.substring(0, t.length - 1);
  }
  return t;
}
