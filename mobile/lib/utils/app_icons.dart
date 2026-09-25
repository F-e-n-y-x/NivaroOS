import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';
import '../services/api_client.dart';

class AppIconHelper {
  AppIconHelper._();

  static const Map<String, String> _builtInAssets = {
    'appstore': 'assets/app/appstore.png',
    'app store': 'assets/app/appstore.png',
    '/img/app/appstore.png': 'assets/app/appstore.png',
    '/img/app/appstore.svg': 'assets/app/appstore.svg',
    'files': 'assets/app/files.svg',
    '/img/app/files.svg': 'assets/app/files.svg',
    'settings': 'assets/app/settings.png',
    '/img/app/settings.png': 'assets/app/settings.png',
    'terminal': 'assets/app/terminal.png',
    '/img/app/terminal.png': 'assets/app/terminal.png',
    'vms': 'assets/app/vm-manager.png',
    'vm-manager': 'assets/app/vm-manager.png',
    '/img/app/vm-manager.png': 'assets/app/vm-manager.png',
    '/img/app/backup.png': 'assets/app/backup.png',
    '/img/app/default.svg': 'assets/app/default.svg',
  };

  /// Resolves the icon path/URL exactly as WebUI does.
  static String resolve(String rawIcon, String appName) {
    final trimmed = rawIcon.trim();
    if (trimmed.isEmpty) return '';

    // Check if it matches a built-in asset path
    final lower = trimmed.toLowerCase();
    if (_builtInAssets.containsKey(lower)) {
      return _builtInAssets[lower]!;
    }

    if (trimmed.startsWith('assets/')) {
      return trimmed;
    }

    if (trimmed.startsWith('data:image/')) {
      return trimmed;
    }

    if (trimmed.startsWith('http://') || trimmed.startsWith('https://')) {
      return trimmed;
    }

    final baseUrl = ApiClient.instance.baseUrl.replaceAll(RegExp(r'/+$'), '');
    if (trimmed.startsWith('/')) {
      // Check if this relative path has a bundled local asset equivalent
      if (_builtInAssets.containsKey(trimmed)) {
        return _builtInAssets[trimmed]!;
      }
      return '$baseUrl$trimmed';
    }

    return '$baseUrl/$trimmed';
  }

  /// Checks if the app is a known built-in system app
  static String? getBuiltInAsset(String appName) {
    final key = appName.toLowerCase().replaceAll(RegExp(r'[^a-z0-9]'), '');
    if (key == 'appstore' || key == 'appstore') {
      return 'assets/app/appstore.png';
    }
    if (key == 'files') return 'assets/app/files.svg';
    if (key == 'settings') return 'assets/app/settings.png';
    if (key == 'terminal') return 'assets/app/terminal.png';
    if (key == 'vms' || key == 'vmmanager') return 'assets/app/vm-manager.png';
    return null;
  }
}

/// An app's icon: the one set on the server (a bundled asset, a data URI,
/// a server path or a URL), on a neutral rounded square. Without an icon,
/// or while it loads or when it fails, it shows the app's initial in the
/// same tone - never colour-coded per app.
///
/// Decorative: the row next to it already names the app, so it is
/// excluded from semantics.
class NivaroAppIcon extends StatelessWidget {
  final String iconUrl;
  final String name;
  final String? imageName;
  final double size;
  final double? radius;
  final double? customRadiusPercent;

  /// No longer drawn (glows are banned); kept so older callers compile.
  final bool showRunningGlow;
  final bool isRunning;

  const NivaroAppIcon({
    super.key,
    required this.iconUrl,
    required this.name,
    this.imageName,
    this.size = 48,
    this.radius,
    this.customRadiusPercent,
    this.showRunningGlow = false,
    this.isRunning = false,
  });

  /// Tests only: draws network icons from bundled pictures instead of the
  /// internet (widget tests can't load them), so screenshots show real
  /// icons in the headers instead of placeholders.
  @visibleForTesting
  static Widget Function(String url, double size)? debugNetworkIcon;

  double get _effectiveRadius {
    if (customRadiusPercent != null && customRadiusPercent! > 0) {
      return (size * (customRadiusPercent! / 100)).clamp(0.0, size / 2);
    }
    return radius ?? (size * 0.25);
  }

  /// The letter shown when there is no picture: the first letter or digit
  /// of the name, upper case.
  static String monogram(String name) {
    final m = RegExp(r'[A-Za-z0-9]').firstMatch(name);
    return m == null ? '?' : m.group(0)!.toUpperCase();
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final builtIn = AppIconHelper.getBuiltInAsset(name);
    final resolved = builtIn ?? AppIconHelper.resolve(iconUrl, name);
    final fallback = _Monogram(letter: monogram(name), size: size);

    Widget content;
    if (resolved.isEmpty) {
      content = fallback;
    } else if (resolved.startsWith('assets/')) {
      content = resolved.endsWith('.svg')
          ? SvgPicture.asset(resolved, width: size, height: size, fit: BoxFit.contain, placeholderBuilder: (_) => fallback)
          : Image.asset(resolved, width: size, height: size, fit: BoxFit.cover, errorBuilder: (_, _, _) => fallback);
    } else if (debugNetworkIcon != null && resolved.startsWith('http')) {
      content = debugNetworkIcon!(resolved, size);
    } else if (resolved.startsWith('data:image/')) {
      content = _fromDataUri(resolved, fallback);
    } else if (resolved.toLowerCase().contains('.svg')) {
      content = SvgPicture.network(
        resolved,
        width: size,
        height: size,
        fit: BoxFit.contain,
        headers: _authHeadersFor(resolved),
        placeholderBuilder: (_) => fallback,
        errorBuilder: (_, _, _) => fallback,
      );
    } else {
      content = Image.network(
        resolved,
        width: size,
        height: size,
        fit: BoxFit.cover,
        headers: _authHeadersFor(resolved),
        errorBuilder: (_, _, _) => fallback,
        frameBuilder: (_, child, frame, sync) => sync || frame != null ? child : fallback,
      );
    }

    return ExcludeSemantics(
      child: Container(
        width: size,
        height: size,
        clipBehavior: Clip.antiAlias,
        decoration: BoxDecoration(
          color: scheme.surfaceContainerHighest,
          borderRadius: BorderRadius.circular(_effectiveRadius),
        ),
        child: content,
      ),
    );
  }

  Widget _fromDataUri(String uri, Widget fallback) {
    final comma = uri.indexOf(',');
    if (comma == -1) return fallback;
    try {
      final bytes = base64Decode(uri.substring(comma + 1));
      return uri.substring(0, comma).contains('svg')
          ? SvgPicture.memory(bytes, width: size, height: size, fit: BoxFit.contain, placeholderBuilder: (_) => fallback)
          : Image.memory(bytes, width: size, height: size, fit: BoxFit.cover, errorBuilder: (_, _, _) => fallback);
    } catch (_) {
      return fallback;
    }
  }

  // Icons served by this NivaroOS server need the same Authorization
  // header as every other call. The token goes only to this server's own
  // host, never to an external icon URL.
  static Map<String, String>? _authHeadersFor(String url) {
    final token = ApiClient.instance.accessToken;
    if (token == null || token.isEmpty) return null;
    try {
      final ownHost = Uri.parse(ApiClient.instance.baseUrl).host;
      final urlHost = Uri.parse(url).host;
      if (ownHost.isEmpty || urlHost != ownHost) return null;
    } catch (_) {
      return null;
    }
    return {'Authorization': token};
  }
}

class _Monogram extends StatelessWidget {
  const _Monogram({required this.letter, required this.size});

  final String letter;
  final double size;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    // The letter scales with the tile, not with the text setting: it is a
    // picture, and the app's name is always written next to it.
    final style = (size >= 48 ? theme.textTheme.headlineSmall : theme.textTheme.titleMedium)
        ?.copyWith(color: theme.colorScheme.onSurfaceVariant);
    return SizedBox(
      width: size,
      height: size,
      child: Center(
        child: MediaQuery.withNoTextScaling(child: Text(letter, style: style)),
      ),
    );
  }
}
