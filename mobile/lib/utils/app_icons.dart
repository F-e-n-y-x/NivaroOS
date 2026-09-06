import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';
import '../services/api_client.dart';
import '../theme.dart';

/// WebUI Default App Icon SVG (The official NivaroOS cube/box icon when no icon is set)
const String kWebUiDefaultAppSvg = '<svg width="64" height="64" viewBox="0 0 64 64" fill="none" xmlns="http://www.w3.org/2000/svg"><rect width="64" height="64" rx="12" fill="url(#paint0_linear)"/><path d="M32 50.1882C31.5747 50.1882 31.1494 50.0782 30.7683 49.8582L17.2317 42.0428C16.4695 41.6028 16 40.7896 16 39.9095V24.2787C16 23.8407 16.1163 23.4192 16.3271 23.0521L32 32.0941V50.1882Z" fill="url(#paint1_linear)"/><path d="M47.6729 23.0521C47.8837 23.4192 48 23.8407 48 24.2787V39.9095C48 40.7896 47.5305 41.6028 46.7683 42.0428L33.2317 49.8582C32.8506 50.0782 32.4253 50.1882 32 50.1882V32.0941L47.6729 23.0521Z" fill="url(#paint2_linear)"/><path d="M47.6729 23.0521C47.4601 22.6816 47.1511 22.3664 46.7683 22.1454L33.2317 14.33C32.4695 13.89 31.5305 13.89 30.7683 14.33L17.2317 22.1454C16.8489 22.3664 16.5399 22.6816 16.3271 23.0521L32 32.0941L47.6729 23.0521Z" fill="url(#paint3_linear)"/><path d="M30 8.1547C31.2376 7.44017 32.7624 7.44017 34 8.1547L51.6506 18.3453C52.8882 19.0598 53.6506 20.3803 53.6506 21.8094V42.1906C53.6506 43.6197 52.8882 44.9402 51.6506 45.6547L34 55.8453C32.7624 56.5598 31.2376 56.5598 30 55.8453L12.3494 45.6547C11.1118 44.9402 10.3494 43.6197 10.3494 42.1906V21.8094C10.3494 20.3803 11.1118 19.0598 12.3494 18.3453L30 8.1547Z" stroke="#389FFA" stroke-width="1.5"/><defs><linearGradient id="paint0_linear" x1="32" y1="0" x2="32" y2="64" gradientUnits="userSpaceOnUse"><stop stop-color="#F7FAFC"/><stop offset="0.88" stop-color="#E7ECF2"/><stop offset="1" stop-color="#DADFE6"/></linearGradient><linearGradient id="paint1_linear" x1="24" y1="27" x2="23" y2="44" gradientUnits="userSpaceOnUse"><stop stop-color="#299BFF"/><stop offset="1" stop-color="#1A94FF"/></linearGradient><linearGradient id="paint2_linear" x1="39" y1="27.5" x2="41" y2="45.5" gradientUnits="userSpaceOnUse"><stop stop-color="#2486F9"/><stop offset="1" stop-color="#047FF1"/></linearGradient><linearGradient id="paint3_linear" x1="32" y1="14" x2="31.4" y2="32" gradientUnits="userSpaceOnUse"><stop stop-color="#80CAFF"/><stop offset="1" stop-color="#3DABFF"/></linearGradient></defs></svg>';

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
    if (key == 'appstore' || key == 'appstore') return 'assets/app/appstore.png';
    if (key == 'files') return 'assets/app/files.svg';
    if (key == 'settings') return 'assets/app/settings.png';
    if (key == 'terminal') return 'assets/app/terminal.png';
    if (key == 'vms' || key == 'vmmanager') return 'assets/app/vm-manager.png';
    return null;
  }
}

/// Official NivaroOS WebUI App Icon Widget
/// Displays:
/// 1. The exact set icon from WebUI (Custom Base64, URL, Server Path, or Built-in Asset).
/// 2. If no icon is set or loading fails, displays the official WebUI "not set" default SVG icon.
class NivaroAppIcon extends StatelessWidget {
  final String iconUrl;
  final String name;
  final String? imageName;
  final double size;
  final double? radius;
  final double? customRadiusPercent;
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

  double get _effectiveRadius {
    if (customRadiusPercent != null && customRadiusPercent! > 0) {
      return (size * (customRadiusPercent! / 100)).clamp(0.0, size / 2);
    }
    return radius ?? (size * 0.24);
  }

  @override
  Widget build(BuildContext context) {
    // 1. Check direct built-in match
    final builtIn = AppIconHelper.getBuiltInAsset(name);
    final resolved = builtIn ?? AppIconHelper.resolve(iconUrl, name);
    final effectiveR = _effectiveRadius;

    Widget content;
    if (resolved.isEmpty) {
      content = _defaultWebUiIcon();
    } else if (resolved.startsWith('assets/')) {
      if (resolved.endsWith('.svg')) {
        content = SvgPicture.asset(
          resolved,
          width: size,
          height: size,
          fit: BoxFit.contain,
          placeholderBuilder: (_) => _defaultWebUiIcon(),
        );
      } else {
        content = Image.asset(
          resolved,
          width: size,
          height: size,
          fit: BoxFit.cover,
          errorBuilder: (_, __, ___) => _defaultWebUiIcon(),
        );
      }
    } else if (resolved.startsWith('data:image/')) {
      final comma = resolved.indexOf(',');
      if (comma != -1) {
        try {
          final bytes = base64Decode(resolved.substring(comma + 1));
          if (resolved.contains('svg')) {
            content = SvgPicture.memory(
              bytes,
              width: size,
              height: size,
              fit: BoxFit.contain,
              placeholderBuilder: (_) => _defaultWebUiIcon(),
            );
          } else {
            content = Image.memory(
              bytes,
              width: size,
              height: size,
              fit: BoxFit.cover,
              errorBuilder: (_, __, ___) => _defaultWebUiIcon(),
            );
          }
        } catch (_) {
          content = _defaultWebUiIcon();
        }
      } else {
        content = _defaultWebUiIcon();
      }
    } else if (resolved.toLowerCase().contains('.svg') || resolved.toLowerCase().endsWith('.svg')) {
      content = SvgPicture.network(
        resolved,
        width: size,
        height: size,
        fit: BoxFit.contain,
        placeholderBuilder: (_) => _defaultWebUiIcon(),
      );
    } else {
      content = Image.network(
        resolved,
        width: size,
        height: size,
        fit: BoxFit.cover,
        errorBuilder: (_, __, ___) => _defaultWebUiIcon(),
      );
    }

    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: NivaroColors.surfaceRaised,
        borderRadius: BorderRadius.circular(effectiveR),
        border: Border.all(
          color: isRunning && showRunningGlow
              ? NivaroColors.success.withOpacity(0.4)
              : NivaroColors.borderSubtle,
          width: 1,
        ),
        boxShadow: isRunning && showRunningGlow
            ? [
                BoxShadow(
                  color: NivaroColors.success.withOpacity(0.2),
                  blurRadius: 10,
                  spreadRadius: 1,
                ),
              ]
            : const [
                BoxShadow(
                  color: Color(0x33000000),
                  blurRadius: 8,
                  offset: Offset(0, 3),
                ),
              ],
      ),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(effectiveR > 0 ? effectiveR - 1 : 0),
        child: content,
      ),
    );
  }

  Widget _defaultWebUiIcon() {
    return SvgPicture.string(
      kWebUiDefaultAppSvg,
      width: size,
      height: size,
      fit: BoxFit.contain,
    );
  }
}
