import 'package:flutter/material.dart';

/// A folder in the web UI's Favorites (the sidebar shortcuts), kept in sync
/// with the server's `users/current/custom/shortcut` list.
class FavoriteFolder {
  final String name;
  final String path;
  final IconData icon;

  /// Kept for older callers; the design system colours folder icons from
  /// the theme, so this is never used to draw anything.
  final Color? color;
  final bool isCustom;
  final String? pack;

  const FavoriteFolder({
    required this.name,
    required this.path,
    required this.icon,
    this.color,
    this.isCustom = false,
    this.pack,
  });

  factory FavoriteFolder.fromCustomJson(Map<String, dynamic> json) {
    final name = json['name'] as String? ?? 'Shortcut';
    final path = json['path'] as String? ?? '/DATA';
    final iconStr = json['icon'] as String? ?? 'folder-outline';
    return FavoriteFolder(
      name: name,
      path: path,
      icon: resolveIcon(iconStr, name),
      isCustom: true,
      pack: json['pack'] as String? ?? 'casa',
    );
  }

  Map<String, dynamic> toJson() => {
        'name': name,
        'path': path,
        'icon': 'folder-outline',
        'pack': pack ?? 'casa',
        'visible': true,
        'selected': true,
        'extensions': null,
      };

  /// An outlined glyph for a folder, from its name. Every favourite is a
  /// folder, so anything without a better match is the folder icon.
  static IconData resolveIcon(String iconStr, String name) {
    final lower = name.toLowerCase();
    if (lower.contains('download')) return Icons.download_outlined;
    if (lower.contains('doc') || lower.contains('pdf')) return Icons.description_outlined;
    if (lower.contains('photo') || lower.contains('image') || lower.contains('gallery') || lower.contains('picture')) {
      return Icons.photo_library_outlined;
    }
    if (lower.contains('video') || lower.contains('movie') || lower.contains('media')) return Icons.video_library_outlined;
    if (lower.contains('music') || lower.contains('audio') || lower.contains('song')) return Icons.music_note_outlined;
    if (lower.contains('desktop')) return Icons.desktop_windows_outlined;
    if (lower.contains('backup') || lower.contains('archive')) return Icons.backup_outlined;
    if (lower.contains('code') || lower.contains('dev') || lower.contains('project')) return Icons.code_outlined;
    if (lower == 'root' || lower == '/') return Icons.dns_outlined;
    return Icons.folder_outlined;
  }

  /// Kept for older callers (the shortcuts service); colours come from the
  /// theme now.
  static Color? resolveColor(String name, String path) => null;

  /// The web UI's built-in favourites (FolderTree.vue parity).
  static List<FavoriteFolder> get defaultWebUiFavorites => const [
        FavoriteFolder(name: 'Root', path: '/', icon: Icons.dns_outlined),
        FavoriteFolder(name: 'DATA', path: '/DATA', icon: Icons.folder_special_outlined),
        FavoriteFolder(name: 'Desktop', path: '/DATA/Desktop', icon: Icons.desktop_windows_outlined),
        FavoriteFolder(name: 'Documents', path: '/DATA/Documents', icon: Icons.description_outlined),
        FavoriteFolder(name: 'Downloads', path: '/DATA/Downloads', icon: Icons.download_outlined),
        FavoriteFolder(name: 'Gallery', path: '/DATA/Gallery', icon: Icons.photo_library_outlined),
        FavoriteFolder(name: 'Media', path: '/DATA/Media', icon: Icons.video_library_outlined),
      ];
}
