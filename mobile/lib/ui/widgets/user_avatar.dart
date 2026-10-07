import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';

/// An account's picture, or its initials on the direction's primary
/// container (Rack, Tonal and Console each tint it; true black keeps it
/// off the black). Decorative: the name is always written next to it.
class UserAvatar extends StatelessWidget {
  const UserAvatar({super.key, required this.username, this.picture, this.radius = 20});

  final String username;
  final Uint8List? picture;
  final double radius;

  static String initials(String name) {
    final parts = name.trim().split(RegExp(r'[\s._-]+')).where((p) => p.isNotEmpty).toList();
    if (parts.isEmpty) return '';
    final first = parts[0].characters.first;
    final second = parts.length > 1 ? parts[1].characters.first : '';
    return (first + second).toUpperCase();
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final letters = initials(username);
    final pic = picture;
    return ExcludeSemantics(
      child: CircleAvatar(
        radius: radius,
        backgroundColor: scheme.primaryContainer,
        foregroundColor: scheme.onPrimaryContainer,
        foregroundImage: pic == null ? null : MemoryImage(pic),
        child: letters.isEmpty
            ? Icon(Icons.person_outline, size: radius)
            : Text(letters, style: TextStyle(fontSize: radius * (letters.length > 1 ? 0.7 : 0.8))),
      ),
    );
  }
}
