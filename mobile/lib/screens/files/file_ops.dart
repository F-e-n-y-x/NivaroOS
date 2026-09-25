// Pure logic behind the Files screen: paths, sorting, breadcrumbs,
// locations, and how a copy or move with name conflicts turns into server
// jobs. No widgets and no I/O here, so all of it is unit-tested
// (test/files/file_ops_test.dart).
import '../../models/file_entry.dart';

/// The last segment of [path] ("/DATA/a/b.txt" → "b.txt"). Works for
/// server and phone paths (both use "/").
String baseName(String path) {
  final trimmed = path.replaceAll(RegExp(r'/+$'), '');
  if (trimmed.isEmpty) return '/';
  return trimmed.split('/').last;
}

/// The folder [path] is in ("/DATA/a/b.txt" → "/DATA/a"; "/DATA" → "/").
String parentOf(String path) {
  final trimmed = path.replaceAll(RegExp(r'/+$'), '');
  final i = trimmed.lastIndexOf('/');
  if (i <= 0) return '/';
  return trimmed.substring(0, i);
}

/// [dir] + "/" + [name], without doubling the slash at the root.
String joinPath(String dir, String name) => dir.endsWith('/') ? '$dir$name' : '$dir/$name';

/// Whether [path] is [root] or inside it.
bool isWithin(String path, String root) {
  if (root == '/') return path.startsWith('/');
  return path == root || path.startsWith('$root/');
}

/// A name that isn't in [taken], the way the server's "keep both" names
/// it: "photo.jpg" → "photo (2).jpg" → "photo (3).jpg". Folders and
/// extensionless names get the number at the end.
String uniqueName(String name, Set<String> taken, {bool isDir = false}) {
  if (!taken.contains(name)) return name;
  final dot = isDir ? -1 : name.lastIndexOf('.');
  final hasExt = dot > 0 && dot < name.length - 1;
  final stem = hasExt ? name.substring(0, dot) : name;
  final ext = hasExt ? name.substring(dot) : '';
  for (var n = 2;; n++) {
    final candidate = '$stem ($n)$ext';
    if (!taken.contains(candidate)) return candidate;
  }
}

/// The name the server gives a restored item whose old name has been
/// reused since (services/core service/trash `restoredName`): "photo.jpg"
/// → "photo (restored).jpg", then "photo (restored 2).jpg" if [taken]
/// has that too. Like Go's filepath.Ext, the extension is from the last
/// dot, for folders as well.
String restoredName(String name, [Set<String> taken = const {}]) {
  final dot = name.lastIndexOf('.');
  final stem = dot >= 0 ? name.substring(0, dot) : name;
  final ext = dot >= 0 ? name.substring(dot) : '';
  for (var i = 1;; i++) {
    final candidate = '$stem ${i == 1 ? '(restored)' : '(restored $i)'}$ext';
    if (!taken.contains(candidate)) return candidate;
  }
}

/// Why a proposed file or folder name can't be used, or null if it can.
String? validateName(String name, {Set<String> taken = const {}, String? current}) {
  final trimmed = name.trim();
  if (trimmed.isEmpty) return 'Enter a name';
  if (trimmed == '.' || trimmed == '..') return "That name isn't allowed";
  if (trimmed.contains('/') || trimmed.contains('\u0000')) return 'Names can’t contain “/”';
  if (trimmed.length > 255) return 'That name is too long';
  if (trimmed != current && taken.contains(trimmed)) return 'Something with that name is already here';
  return null;
}

// ---------------------------------------------------------------------------
// Sorting

enum FileSort {
  name('Name'),
  modified('Date modified'),
  size('Size'),
  type('Type');

  const FileSort(this.label);
  final String label;
}

/// Folders first, then files, each ordered by [sort]. Names compare
/// case-insensitively with numbers in number order ("2.jpg" before
/// "10.jpg"), like Android's own file manager. Ties fall back to the name
/// so the order is stable.
List<FileEntry> sortEntries(List<FileEntry> entries, FileSort sort, {bool ascending = true}) {
  int byName(FileEntry a, FileEntry b) => naturalCompare(a.name, b.name);
  final list = List<FileEntry>.of(entries);
  list.sort((a, b) {
    if (a.isDir != b.isDir) return a.isDir ? -1 : 1;
    final cmp = switch (sort) {
      FileSort.name => byName(a, b),
      FileSort.modified => (a.modified ?? DateTime(0)).compareTo(b.modified ?? DateTime(0)),
      FileSort.size => a.isDir ? 0 : a.size.compareTo(b.size),
      FileSort.type => a.extension.compareTo(b.extension),
    };
    final ordered = ascending ? cmp : -cmp;
    return ordered != 0 ? ordered : byName(a, b);
  });
  return list;
}

/// The default direction for [sort]: A→Z for names and types, newest and
/// largest first for dates and sizes (what people look for).
bool defaultAscending(FileSort sort) => sort == FileSort.name || sort == FileSort.type;

/// Case-insensitive comparison that orders runs of digits by value.
int naturalCompare(String a, String b) {
  final re = RegExp(r'(\d+)|(\D+)');
  final pa = re.allMatches(a.toLowerCase()).map((m) => m.group(0)!).toList();
  final pb = re.allMatches(b.toLowerCase()).map((m) => m.group(0)!).toList();
  for (var i = 0; i < pa.length && i < pb.length; i++) {
    final x = pa[i], y = pb[i];
    final nx = int.tryParse(x), ny = int.tryParse(y);
    final c = (nx != null && ny != null && x.length < 19 && y.length < 19) ? nx.compareTo(ny) : x.compareTo(y);
    if (c != 0) return c;
  }
  final c = pa.length.compareTo(pb.length);
  return c != 0 ? c : a.compareTo(b);
}

/// [entries] without hidden files (unless [showHidden]) and matching
/// [query] (case-insensitive, anywhere in the name).
List<FileEntry> visibleEntries(List<FileEntry> entries, {bool showHidden = false, String query = ''}) {
  final q = query.trim().toLowerCase();
  return [
    for (final e in entries)
      if ((showHidden || !e.isHidden) && (q.isEmpty || e.name.toLowerCase().contains(q))) e,
  ];
}

// ---------------------------------------------------------------------------
// Locations and breadcrumbs

/// [trash] is the server's Trash, which opens its own screen.
enum LocationKind { storage, usb, cloud, thisPhone, phone, favorite, root, trash }

/// A place files live: a server disk, a USB drive, a cloud mount, a
/// phone's folder on the server, or this phone's own storage.
class FileLocation {
  const FileLocation({
    required this.label,
    required this.path,
    required this.kind,
    this.detail,
    this.usedBytes,
    this.totalBytes,
    this.freeBytes,
    this.online = true,
  });

  final String label;
  final String path;
  final LocationKind kind;

  /// A short second line ("SanDisk Ultra", "Google Drive", "Offline").
  final String? detail;
  final int? usedBytes;
  final int? totalBytes;

  /// What can still be written, when the server says (it is less than
  /// total minus used: ext4 keeps a reserve for root).
  final int? freeBytes;

  /// Space left: [freeBytes], or total minus used.
  int? get availableBytes {
    if (freeBytes != null) return freeBytes;
    final u = usedBytes, t = totalBytes;
    return u != null && t != null ? (t - u).clamp(0, t) : null;
  }

  /// False for a companion phone that isn't connected.
  final bool online;

  /// On this phone rather than on the server.
  bool get isLocal => kind == LocationKind.thisPhone;

  @override
  String toString() => 'FileLocation($label, $path)';
}

/// The location [path] belongs to: the one with the longest root that
/// contains it, among those on the same side (phone or server).
FileLocation? locationFor(String path, Iterable<FileLocation> locations, {required bool isLocal}) {
  FileLocation? best;
  for (final l in locations) {
    if (l.isLocal != isLocal || l.kind == LocationKind.favorite) continue;
    if (!isWithin(path, l.path)) continue;
    if (best == null || l.path.length > best.path.length) best = l;
  }
  return best;
}

/// One step of the breadcrumb trail.
class Crumb {
  const Crumb(this.label, this.path);
  final String label;
  final String path;

  @override
  bool operator ==(Object other) => other is Crumb && other.label == label && other.path == path;

  @override
  int get hashCode => Object.hash(label, path);

  @override
  String toString() => 'Crumb($label, $path)';
}

/// The trail from [root] (labelled [rootLabel]) down to [path]. Folders
/// above the root are not shown; a path outside it starts at "/".
List<Crumb> breadcrumbs(String path, {required String root, required String rootLabel}) {
  if (!isWithin(path, root)) {
    root = '/';
    rootLabel = 'Root';
  }
  final crumbs = [Crumb(rootLabel, root)];
  final rest = path.length > root.length ? path.substring(root == '/' ? 1 : root.length + 1) : '';
  var current = root;
  for (final part in rest.split('/').where((s) => s.isNotEmpty)) {
    current = joinPath(current, part);
    crumbs.add(Crumb(part, current));
  }
  return crumbs;
}

/// Mount points that aren't places to browse: boot partitions, the VM
/// Manager's pass-through shares (`/DATA/VMs/share`, and the old
/// `/data/vm-shares`), and phone storage, which has its own section.
bool isHiddenMount(String mountPoint) {
  final mp = mountPoint.toLowerCase();
  return mp.isEmpty ||
      mp.startsWith('/boot') ||
      mp == '/data/vms/share' ||
      mp.startsWith('/data/vms/share/') ||
      mp.startsWith('/data/vm-shares') ||
      mp.startsWith('/storage/emulated') ||
      mp == '[swap]';
}

/// Server storage from `GET /v1/sys/disks-usage` (and `GET /v1/disks/usb`
/// for USB drives): the system disk becomes "DATA" at /DATA, where its
/// user data lives; other data disks and USB drives appear under their
/// labels at their mount points.
List<FileLocation> storageLocations(List<dynamic> disksUsage, {List<dynamic> usb = const []}) {
  final out = <FileLocation>[];
  final seen = <String>{};
  int? toInt(Object? v) => v is num ? v.toInt() : (v is String ? int.tryParse(v) : null);

  void add(FileLocation l) {
    if (seen.add(l.path)) out.add(l);
  }

  for (final d in disksUsage.whereType<Map>()) {
    final mp = d['mount_point']?.toString() ?? '';
    final size = toInt(d['size_bytes']);
    final used = toInt(d['used_bytes']);
    if (d['is_system'] == true || mp == '/') {
      add(FileLocation(label: 'DATA', path: '/DATA', kind: LocationKind.storage, usedBytes: used, totalBytes: size, freeBytes: toInt(d['avail_bytes'])));
      continue;
    }
    if (isHiddenMount(mp)) continue;
    final label = (d['label']?.toString().isNotEmpty ?? false) ? d['label'].toString() : baseName(mp);
    final isUsb = d['is_usb'] == true;
    add(FileLocation(
      label: label,
      path: mp,
      kind: isUsb ? LocationKind.usb : LocationKind.storage,
      detail: isUsb ? _usbDetail(d) : null,
      usedBytes: used,
      totalBytes: size,
      freeBytes: toInt(d['avail_bytes']),
    ));
  }
  for (final disk in usb.whereType<Map>()) {
    final children = disk['children'];
    final parts = children is List && children.isNotEmpty ? children.whereType<Map>() : [disk];
    for (final c in parts) {
      final mp = c['mount_point']?.toString() ?? '';
      if (isHiddenMount(mp)) continue;
      final label = (c['label']?.toString().isNotEmpty ?? false) ? c['label'].toString() : baseName(mp);
      add(FileLocation(
        label: label,
        path: mp,
        kind: LocationKind.usb,
        detail: _usbDetail(disk),
        usedBytes: toInt(c['used_bytes']),
        totalBytes: toInt(c['size_bytes']),
        freeBytes: toInt(c['avail_bytes']),
      ));
    }
  }
  return out;
}

String? _usbDetail(Map d) {
  final model = (d['name'] ?? d['model'])?.toString().trim() ?? '';
  return model.isEmpty ? null : model;
}

// ---------------------------------------------------------------------------
// Copy and move

enum TransferKind { copy, move }

/// What to do with an item whose name already exists at the destination.
enum ConflictChoice {
  keepBoth('rename'),
  replace('overwrite'),
  skip('skip');

  const ConflictChoice(this.style);

  /// The server's conflict style (`POST /v1/batch/task` `style`).
  final String style;
}

/// The source paths among [sources] whose name is already taken in
/// [destDir] ([existingNames] is the destination's listing). Copying an
/// item into its own folder isn't a conflict: the server keeps both.
List<String> findConflicts(List<String> sources, String destDir, Set<String> existingNames) => [
      for (final s in sources)
        if (parentOf(s) != destDir && existingNames.contains(baseName(s))) s,
    ];

/// One server job: these sources, with this conflict style.
class TransferBatch {
  const TransferBatch(this.style, this.sources);
  final String style;
  final List<String> sources;

  @override
  bool operator ==(Object other) =>
      other is TransferBatch && other.style == style && _listEquals(other.sources, sources);

  @override
  int get hashCode => Object.hash(style, Object.hashAll(sources));

  @override
  String toString() => 'TransferBatch($style, $sources)';
}

bool _listEquals(List<String> a, List<String> b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

/// Splits a copy or move into the fewest server jobs that honour every
/// answer in [choices] (keyed by source path; sources not in it had no
/// conflict). The server takes one conflict style per job, so replaced
/// items go in a job of their own; skipped items are left out. Items
/// without a conflict go with the kept-both ones, under "keep both", so
/// a name that appeared since the check is still never overwritten.
List<TransferBatch> planTransfer(List<String> sources, Map<String, ConflictChoice> choices) {
  final keep = <String>[];
  final replace = <String>[];
  for (final s in sources) {
    switch (choices[s]) {
      case null || ConflictChoice.keepBoth:
        keep.add(s);
      case ConflictChoice.replace:
        replace.add(s);
      case ConflictChoice.skip:
        break;
    }
  }
  return [
    if (keep.isNotEmpty) TransferBatch(ConflictChoice.keepBoth.style, keep),
    if (replace.isNotEmpty) TransferBatch(ConflictChoice.replace.style, replace),
  ];
}

/// The name a transferred item gets on a phone (or anywhere the app
/// resolves conflicts itself) for [choice]: its own name, a numbered one,
/// or null to skip it.
String? resolveLocalName(String name, Set<String> taken, ConflictChoice? choice, {bool isDir = false}) {
  if (!taken.contains(name) || choice == null) return name;
  return switch (choice) {
    ConflictChoice.skip => null,
    ConflictChoice.replace => name,
    ConflictChoice.keepBoth => uniqueName(name, taken, isDir: isDir),
  };
}

/// The name for a new archive of [sources]: "item.zip" for one item,
/// "Archive.zip" for several, made unique in [taken].
String defaultArchiveName(List<String> sources, Set<String> taken) {
  final base = sources.length == 1 ? '${_stripExt(baseName(sources.first))}.zip' : 'Archive.zip';
  return uniqueName(base, taken);
}

/// The folder an archive extracts into: its name without the archive
/// extension(s), made unique ("photos.tar.gz" → "photos").
String defaultExtractFolder(String archiveName, Set<String> taken) {
  var stem = archiveName;
  for (final ext in const ['.tar.gz', '.tar.bz2', '.tar.xz', '.tar.zst']) {
    if (stem.toLowerCase().endsWith(ext)) {
      stem = stem.substring(0, stem.length - ext.length);
      return uniqueName(stem, taken, isDir: true);
    }
  }
  return uniqueName(_stripExt(stem), taken, isDir: true);
}

String _stripExt(String name) {
  final dot = name.lastIndexOf('.');
  return dot > 0 ? name.substring(0, dot) : name;
}
