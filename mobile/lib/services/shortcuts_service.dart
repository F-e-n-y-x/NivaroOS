import 'dart:convert';
import '../models/favorite_folder.dart';
import 'api_client.dart';

/// Service for managing WebUI synced folder shortcuts and favorites.
class ShortcutsService {
  ShortcutsService._();
  static final ShortcutsService instance = ShortcutsService._();

  /// Fetches custom user shortcuts from WebUI backend (/v1/users/current/custom/shortcut).
  Future<List<FavoriteFolder>> getCustomShortcuts() async {
    try {
      final res = await ApiClient.instance.get('/users/current/custom/shortcut');
      dynamic data = res['data'];
      if (data is String && data.isNotEmpty) {
        try {
          data = jsonDecode(data);
        } catch (_) {}
      }
      if (data is List) {
        return data
            .whereType<Map<String, dynamic>>()
            .map((e) => FavoriteFolder.fromCustomJson(e))
            .toList();
      }
    } catch (_) {}
    return [];
  }

  /// Fetches all favorites: WebUI default folders + user custom shortcuts.
  Future<List<FavoriteFolder>> getAllFavorites() async {
    final custom = await getCustomShortcuts();
    final defaults = FavoriteFolder.defaultWebUiFavorites;
    final defaultPaths = defaults.map((d) => d.path).toSet();
    final uniqueCustom = custom.where((c) => !defaultPaths.contains(c.path)).toList();
    return [...defaults, ...uniqueCustom];
  }

  /// Saves the custom user shortcuts to the backend.
  Future<bool> saveCustomShortcuts(List<FavoriteFolder> customShortcuts) async {
    try {
      final body = customShortcuts.map((s) => s.toJson()).toList();
      await ApiClient.instance.post('/users/current/custom/shortcut', body: body);
      return true;
    } catch (_) {
      return false;
    }
  }

  /// Toggles whether a path is in the custom shortcuts list.
  Future<bool> toggleFavorite(String name, String path) async {
    final current = await getCustomShortcuts();
    final exists = current.any((s) => s.path == path);
    List<FavoriteFolder> updated;
    if (exists) {
      updated = current.where((s) => s.path != path).toList();
    } else {
      updated = [
        ...current,
        FavoriteFolder(
          name: name,
          path: path,
          icon: FavoriteFolder.resolveIcon('folder-outline', name),
          color: FavoriteFolder.resolveColor(name, path),
          isCustom: true,
        ),
      ];
    }
    return saveCustomShortcuts(updated);
  }

  /// Checks if a given path is favorited (either default or custom).
  Future<bool> isFavorited(String path) async {
    if (FavoriteFolder.defaultWebUiFavorites.any((d) => d.path == path)) {
      return true;
    }
    final custom = await getCustomShortcuts();
    return custom.any((c) => c.path == path);
  }
}
