// The server's Trash (services/core route/v1/trash.go): list, restore,
// delete forever, empty. A delete in Files moves things there
// (`DELETE /v1/batch` answers with the trashed items' ids).
import '../../models/file_entry.dart';
import '../../services/api_client.dart';
import '../../utils/format.dart';
import 'file_ops.dart';

/// An item the server couldn't restore or delete, and why.
class TrashFailure {
  const TrashFailure({required this.id, required this.name, required this.error});
  final String id;
  final String name;
  final String error;

  static List<TrashFailure> listFrom(Object? raw) => [
        for (final f in (raw is List ? raw : const []))
          if (f is Map)
            TrashFailure(id: f['id']?.toString() ?? '', name: f['name']?.toString() ?? '', error: f['error']?.toString() ?? 'failed'),
      ];

  @override
  String toString() => '${name.isEmpty ? id : name}: $error';
}

/// What a restore did: where each item went back to, and what failed.
class TrashRestoreResult {
  const TrashRestoreResult({required this.restored, required this.failed});

  /// Item id → the path it is at now.
  final Map<String, String> restored;
  final List<TrashFailure> failed;

  /// Restored under a new name because the old one was taken again.
  Iterable<String> get renamed => restored.values.where((p) => _restoredSuffix.hasMatch(baseName(p)));

  static final _restoredSuffix = RegExp(r' \(restored( \d+)?\)(\.[^.]*)?$');

  /// The folders something came back to.
  Set<String> get folders => {for (final p in restored.values) parentOf(p)};
}

abstract final class TrashApi {
  static void _check(Map<String, dynamic> res) {
    final code = res['success'];
    if (code is num && code != 200) {
      throw Exception(res['message']?.toString() ?? 'The server refused ($code).');
    }
  }

  static Future<TrashListing> list() async {
    final res = await ApiClient.instance.get('/trash');
    _check(res);
    return TrashListing.fromJson(res['data']);
  }

  static Future<TrashRestoreResult> restore(List<String> ids) async {
    final res = await ApiClient.instance.post('/trash/restore', body: {'ids': ids});
    _check(res);
    final data = res['data'];
    final restored = <String, String>{};
    if (data is Map) {
      for (final r in (data['restored'] as List? ?? const [])) {
        if (r is Map && r['id'] != null) restored[r['id'].toString()] = r['path']?.toString() ?? '';
      }
    }
    return TrashRestoreResult(restored: restored, failed: TrashFailure.listFrom(data is Map ? data['failed'] : null));
  }

  static Future<List<TrashFailure>> deleteForever(List<String> ids) async {
    final res = await ApiClient.instance.deleteWithBody('/trash', {'ids': ids});
    _check(res);
    final data = res['data'];
    return TrashFailure.listFrom(data is Map ? data['failed'] : null);
  }

  static Future<List<TrashFailure>> empty() async {
    final res = await ApiClient.instance.delete('/trash/all');
    _check(res);
    final data = res['data'];
    return TrashFailure.listFrom(data is Map ? data['failed'] : null);
  }

  /// "3 items · 1.2 GB", or "Empty".
  static String summary(TrashListing t) =>
      t.isEmpty ? 'Empty' : '${formatCount(t.items.length, 'item')} · ${formatSize(t.bytes)}';

  /// The snackbar line after a restore that worked for at least one item.
  static String restoredMessage(TrashRestoreResult r, {required String Function(String path) folderLabel, String? singleName}) {
    final renamed = r.renamed.length;
    if (r.restored.length == 1) {
      final path = r.restored.values.first;
      final name = baseName(path);
      final to = folderLabel(parentOf(path));
      return renamed == 1 ? 'Restored as “$name” in $to' : 'Restored “${singleName ?? name}” to $to';
    }
    return [
      'Restored ${r.restored.length} items',
      if (renamed > 0) '${formatCount(renamed, 'renamed', 'renamed')} because the name was taken',
    ].join(' · ');
  }
}
