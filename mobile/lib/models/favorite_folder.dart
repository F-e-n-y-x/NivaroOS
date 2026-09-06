import 'package:flutter/material.dart';
import '../theme.dart';

/// Represents a folder in the WebUI Favorites section (synced with WebUI shortcuts).
class FavoriteFolder {
  final String name;
  final String path;
  final IconData icon;
  final Color color;
  final bool isCustom;
  final String? pack;

  const FavoriteFolder({
    required this.name,
    required this.path,
    required this.icon,
    this.color = NivaroColors.primaryLight,
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
      color: NivaroColors.primaryLight,
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

  static IconData resolveIcon(String iconStr, String name) {
    final lower = name.toLowerCase();
    if (lower.contains('download')) return Icons.download_rounded;
    if (lower.contains('doc') || lower.contains('file') || lower.contains('pdf')) return Icons.description_rounded;
    if (lower.contains('photo') || lower.contains('image') || lower.contains('gallery') || lower.contains('picture')) return Icons.photo_library_rounded;
    if (lower.contains('video') || lower.contains('movie') || lower.contains('media')) return Icons.movie_filter_rounded;
    if (lower.contains('music') || lower.contains('audio') || lower.contains('song')) return Icons.music_note_rounded;
    if (lower.contains('desktop')) return Icons.desktop_windows_rounded;
    if (lower.contains('backup') || lower.contains('archive')) return Icons.backup_rounded;
    if (lower.contains('code') || lower.contains('dev') || lower.contains('project')) return Icons.code_rounded;
    if (lower.contains('game')) return Icons.sports_esports_rounded;
    if (lower == 'root' || lower == '/') return Icons.dns_rounded;
    if (lower == 'data') return Icons.folder_special_rounded;
    return Icons.folder_rounded;
  }

  static Color resolveColor(String name, String path) {
    return NivaroColors.primaryLight;
  }

  /// Standard WebUI built-in favorite folders (FolderTree.vue parity)
  static List<FavoriteFolder> get defaultWebUiFavorites => const [
        FavoriteFolder(
          name: 'Root',
          path: '/',
          icon: Icons.dns_rounded,
          color: NivaroColors.primaryLight,
          isCustom: false,
        ),
        FavoriteFolder(
          name: 'DATA',
          path: '/DATA',
          icon: Icons.folder_special_rounded,
          color: NivaroColors.primaryLight,
          isCustom: false,
        ),
        FavoriteFolder(
          name: 'Desktop',
          path: '/DATA/Desktop',
          icon: Icons.desktop_windows_rounded,
          color: NivaroColors.primaryLight,
          isCustom: false,
        ),
        FavoriteFolder(
          name: 'Documents',
          path: '/DATA/Documents',
          icon: Icons.article_rounded,
          color: NivaroColors.primaryLight,
          isCustom: false,
        ),
        FavoriteFolder(
          name: 'Downloads',
          path: '/DATA/Downloads',
          icon: Icons.download_rounded,
          color: NivaroColors.primaryLight,
          isCustom: false,
        ),
        FavoriteFolder(
          name: 'Gallery',
          path: '/DATA/Gallery',
          icon: Icons.photo_library_rounded,
          color: NivaroColors.primaryLight,
          isCustom: false,
        ),
        FavoriteFolder(
          name: 'Media',
          path: '/DATA/Media',
          icon: Icons.movie_filter_rounded,
          color: NivaroColors.primaryLight,
          isCustom: false,
        ),
      ];
}
