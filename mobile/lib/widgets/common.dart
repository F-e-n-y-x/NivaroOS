import 'dart:ui';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme.dart';

/// Circular icon button with subtle border, surface elevation, and haptic feedback.
class RoundIconButton extends StatelessWidget {
  final IconData icon;
  final VoidCallback? onPressed;
  final double size;
  final String? tooltip;
  final Color? color;
  final Color? iconColor;

  const RoundIconButton({
    super.key,
    required this.icon,
    this.onPressed,
    this.size = 40,
    this.tooltip,
    this.color,
    this.iconColor,
  });

  @override
  Widget build(BuildContext context) {
    Widget button = SizedBox(
      width: size,
      height: size,
      child: Material(
        color: color ?? NivaroColors.surfaceRaised,
        shape: const CircleBorder(side: BorderSide(color: NivaroColors.border)),
        child: InkWell(
          customBorder: const CircleBorder(),
          onTap: onPressed == null
              ? null
              : () {
                  HapticFeedback.lightImpact();
                  onPressed!();
                },
          child: Icon(
            icon,
            size: size * 0.48,
            color: iconColor ?? (onPressed == null ? NivaroColors.textFaint : NivaroColors.textPrimary),
          ),
        ),
      ),
    );

    if (tooltip != null) {
      button = Tooltip(message: tooltip!, child: button);
    }
    return button;
  }
}

/// A connection pill badge (LAN / Tailscale / Cloud) with a pulsing status dot.
class LanBadge extends StatelessWidget {
  final String address;
  final String label;
  final bool isOnline;

  const LanBadge({
    super.key,
    required this.address,
    this.label = 'LAN',
    this.isOnline = true,
  });

  @override
  Widget build(BuildContext context) {
    final statusColor = isOnline ? NivaroColors.success : NivaroColors.warning;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      decoration: BoxDecoration(
        color: NivaroColors.surface,
        borderRadius: BorderRadius.circular(NivaroShape.large),
        border: Border.all(color: NivaroColors.border),
      ),
      child: Row(
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: BoxDecoration(
              color: statusColor,
              shape: BoxShape.circle,
              boxShadow: [
                BoxShadow(
                  color: statusColor.withOpacity(0.4),
                  blurRadius: 6,
                  spreadRadius: 1,
                ),
              ],
            ),
          ),
          const SizedBox(width: 8),
          Text(
            label,
            style: TextStyle(
              color: statusColor,
              fontWeight: FontWeight.w700,
              fontSize: 12,
              letterSpacing: 0.5,
            ),
          ),
          const SizedBox(width: 10),
          Container(width: 1, height: 12, color: NivaroColors.border),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              address,
              style: const TextStyle(
                color: NivaroColors.textMuted,
                fontSize: 13,
                fontWeight: FontWeight.w500,
              ),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          Icon(
            isOnline ? Icons.wifi_rounded : Icons.wifi_off_rounded,
            color: statusColor,
            size: 16,
          ),
        ],
      ),
    );
  }
}

/// Generic elevated dark card container with rounded squircle and soft shadow.
class DarkCard extends StatelessWidget {
  final Widget child;
  final EdgeInsetsGeometry padding;
  final VoidCallback? onTap;
  final Color? color;
  final BorderSide? border;

  const DarkCard({
    super.key,
    required this.child,
    this.padding = const EdgeInsets.all(16),
    this.onTap,
    this.color,
    this.border,
  });

  @override
  Widget build(BuildContext context) {
    final card = Container(
      padding: padding,
      decoration: BoxDecoration(
        color: color ?? NivaroColors.surface,
        borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
        border: Border.fromBorderSide(border ?? const BorderSide(color: NivaroColors.border)),
        boxShadow: const [
          BoxShadow(
            color: Color(0x33000000),
            blurRadius: 16,
            offset: Offset(0, 4),
          ),
        ],
      ),
      child: child,
    );

    if (onTap != null) {
      return Material(
        color: Colors.transparent,
        child: InkWell(
          borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
          onTap: () {
            HapticFeedback.lightImpact();
            onTap!();
          },
          child: card,
        ),
      );
    }

    return card;
  }
}

/// Hardware monitor tile for CPU, Memory, Temp, and Network.
class MonitorCard extends StatelessWidget {
  final String label;
  final IconData icon;
  final Color color;
  final Widget value;
  final double? percent;
  final String? subtitle;

  const MonitorCard({
    super.key,
    required this.label,
    required this.icon,
    required this.color,
    required this.value,
    this.percent,
    this.subtitle,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: NivaroColors.surface,
        borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
        border: Border.all(color: NivaroColors.border),
        boxShadow: const [
          BoxShadow(
            color: Color(0x33000000),
            blurRadius: 12,
            offset: Offset(0, 4),
          ),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          Row(
            children: [
              Container(
                width: 32,
                height: 32,
                decoration: BoxDecoration(
                  color: color.withOpacity(0.14),
                  borderRadius: BorderRadius.circular(NivaroShape.medium),
                ),
                alignment: Alignment.center,
                child: Icon(icon, color: color, size: 17),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  label,
                  style: const TextStyle(
                    color: NivaroColors.textMuted,
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          value,
          if (percent != null) ...[
            const SizedBox(height: 6),
            StorageBar(fraction: percent!, color: color),
          ],
          if (subtitle != null) ...[
            const SizedBox(height: 4),
            Text(
              subtitle!,
              style: const TextStyle(color: NivaroColors.textFaint, fontSize: 11),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          ],
        ],
      ),
    );
  }
}

/// Rounded animated progress bar for metrics and drive usage.
class StorageBar extends StatelessWidget {
  final double fraction;
  final Color color;
  final double height;

  const StorageBar({
    super.key,
    required this.fraction,
    this.color = NivaroColors.primaryLight,
    this.height = 6,
  });

  @override
  Widget build(BuildContext context) {
    final clamped = fraction.clamp(0.0, 1.0);
    return ClipRRect(
      borderRadius: BorderRadius.circular(height / 2),
      child: LinearProgressIndicator(
        value: clamped,
        minHeight: height,
        backgroundColor: NivaroColors.surfaceMuted,
        valueColor: AlwaysStoppedAnimation(color),
      ),
    );
  }
}

/// Interactive storage drive card showing mount point, percentage, and usage numbers.
class DriveCard extends StatelessWidget {
  final String label;
  final String percentText;
  final double fraction;
  final int usedBytes;
  final int sizeBytes;
  final VoidCallback? onTap;

  const DriveCard({
    super.key,
    required this.label,
    required this.percentText,
    required this.fraction,
    required this.usedBytes,
    required this.sizeBytes,
    this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final color = fraction > 0.9
        ? NivaroColors.danger
        : fraction > 0.75
            ? NivaroColors.warning
            : NivaroColors.info;

    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
        onTap: onTap == null
            ? null
            : () {
                HapticFeedback.lightImpact();
                onTap!();
              },
        child: Container(
          padding: const EdgeInsets.all(16),
          decoration: BoxDecoration(
            color: NivaroColors.surface,
            borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
            border: Border.all(color: NivaroColors.border),
            boxShadow: const [
              BoxShadow(
                color: Color(0x2B000000),
                blurRadius: 12,
                offset: Offset(0, 4),
              ),
            ],
          ),
          child: Row(
            children: [
              Container(
                width: 44,
                height: 44,
                decoration: BoxDecoration(
                  color: color.withOpacity(0.14),
                  borderRadius: BorderRadius.circular(NivaroShape.medium),
                ),
                alignment: Alignment.center,
                child: Icon(Icons.storage_rounded, color: color, size: 22),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            label,
                            style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14.5),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                        Container(
                          padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                          decoration: BoxDecoration(
                            color: color.withOpacity(0.14),
                            borderRadius: BorderRadius.circular(NivaroShape.small),
                          ),
                          child: Text(
                            percentText,
                            style: TextStyle(color: color, fontSize: 11.5, fontWeight: FontWeight.w700),
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 8),
                    StorageBar(fraction: fraction, color: color, height: 7),
                    const SizedBox(height: 6),
                    Text(
                      '${_fmt(usedBytes)} used of ${_fmt(sizeBytes)}',
                      style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                    ),
                  ],
                ),
              ),
              if (onTap != null) ...[
                const SizedBox(width: 8),
                const Icon(Icons.chevron_right_rounded, color: NivaroColors.textFaint, size: 20),
              ],
            ],
          ),
        ),
      ),
    );
  }

  static String _fmt(num bytes) {
    if (bytes <= 0) return '0 B';
    const suffixes = ['B', 'KB', 'MB', 'GB', 'TB'];
    var value = bytes.toDouble();
    var i = 0;
    while (value >= 1024 && i < suffixes.length - 1) {
      value /= 1024;
      i++;
    }
    return '${value.toStringAsFixed(i == 0 ? 0 : 1)} ${suffixes[i]}';
  }
}

/// Compact favorite folder card for the Files Screen.
class FavoriteCard extends StatelessWidget {
  final String name;
  final Color color;
  final IconData glyph;
  final VoidCallback onTap;

  const FavoriteCard({
    super.key,
    required this.name,
    required this.color,
    required this.glyph,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
        onTap: () {
          HapticFeedback.lightImpact();
          onTap();
        },
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
          decoration: BoxDecoration(
            color: NivaroColors.surface,
            borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
            border: Border.all(color: NivaroColors.border),
            boxShadow: const [
              BoxShadow(color: Color(0x2B000000), blurRadius: 10, offset: Offset(0, 3)),
            ],
          ),
          child: Row(
            children: [
              Container(
                width: 38,
                height: 38,
                decoration: BoxDecoration(
                  color: color.withOpacity(0.16),
                  borderRadius: BorderRadius.circular(NivaroShape.medium),
                ),
                alignment: Alignment.center,
                child: Icon(glyph, color: color, size: 20),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  name,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13.5),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Status badge pill with glowing dot for VMs and Containers.
class StatusPill extends StatelessWidget {
  final String label;
  final String state;

  const StatusPill({
    super.key,
    required this.label,
    required this.state,
  });

  @override
  Widget build(BuildContext context) {
    final isRunning = state.toLowerCase() == 'running' || state.toLowerCase() == 'up';
    final isPaused = state.toLowerCase() == 'paused';
    final color = isRunning
        ? NivaroColors.success
        : isPaused
            ? NivaroColors.warning
            : NivaroColors.textFaint;

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: color.withOpacity(0.14),
        borderRadius: BorderRadius.circular(NivaroShape.round),
        border: Border.all(color: color.withOpacity(0.3)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 6,
            height: 6,
            decoration: BoxDecoration(
              color: color,
              shape: BoxShape.circle,
              boxShadow: isRunning
                  ? [BoxShadow(color: color.withOpacity(0.5), blurRadius: 4, spreadRadius: 1)]
                  : null,
            ),
          ),
          const SizedBox(width: 5),
          Text(
            label,
            style: TextStyle(color: color, fontSize: 11, fontWeight: FontWeight.w700),
          ),
        ],
      ),
    );
  }
}

/// Modern floating navigation bar with blur effect and animated selection.
class FloatingNavBar extends StatelessWidget {
  final int currentIndex;
  final ValueChanged<int> onTap;
  final VoidCallback onAvatarTap;
  final String avatarInitial;

  static const _items = [
    (icon: Icons.dashboard_outlined, active: Icons.dashboard_rounded, label: 'Home'),
    (icon: Icons.folder_outlined, active: Icons.folder_rounded, label: 'Files'),
    (icon: Icons.monitor_outlined, active: Icons.monitor_rounded, label: 'VMs'),
    (icon: Icons.grid_view_outlined, active: Icons.grid_view_rounded, label: 'Apps'),
  ];

  const FloatingNavBar({
    super.key,
    required this.currentIndex,
    required this.onTap,
    required this.onAvatarTap,
    required this.avatarInitial,
  });

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      minimum: const EdgeInsets.only(bottom: 12, left: 16, right: 16),
      child: Row(
        children: [
          Expanded(
            child: Container(
              height: 62,
              decoration: BoxDecoration(
                borderRadius: BorderRadius.circular(31),
                boxShadow: const [
                  BoxShadow(
                    color: Color(0x55000000),
                    blurRadius: 24,
                    offset: Offset(0, 8),
                  ),
                ],
              ),
              child: ClipRRect(
                borderRadius: BorderRadius.circular(31),
                child: BackdropFilter(
                  filter: ImageFilter.blur(sigmaX: 20, sigmaY: 20),
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 8),
                    decoration: BoxDecoration(
                      color: NivaroColors.surfaceContainerHigh.withOpacity(0.85),
                      borderRadius: BorderRadius.circular(31),
                      border: Border.all(color: NivaroColors.border),
                    ),
                    child: Row(
                      mainAxisAlignment: MainAxisAlignment.spaceAround,
                      children: List.generate(_items.length, (i) {
                        final selected = i == currentIndex;
                        final item = _items[i];
                        return _NavTabButton(
                          icon: selected ? item.active : item.icon,
                          label: item.label,
                          selected: selected,
                          onTap: () {
                            HapticFeedback.selectionClick();
                            onTap(i);
                          },
                        );
                      }),
                    ),
                  ),
                ),
              ),
            ),
          ),
          const SizedBox(width: 10),
          GestureDetector(
            onTap: () {
              HapticFeedback.lightImpact();
              onAvatarTap();
            },
            child: Container(
              width: 56,
              height: 56,
              decoration: BoxDecoration(
                gradient: const LinearGradient(
                  begin: Alignment.topLeft,
                  end: Alignment.bottomRight,
                  colors: [NivaroColors.primaryLight, NivaroColors.primaryDark],
                ),
                shape: BoxShape.circle,
                border: Border.all(color: NivaroColors.borderHighlight, width: 1.5),
                boxShadow: const [
                  BoxShadow(
                    color: Color(0x40000000),
                    blurRadius: 16,
                    offset: Offset(0, 6),
                  ),
                ],
              ),
              alignment: Alignment.center,
              child: Text(
                avatarInitial,
                style: const TextStyle(
                  color: Colors.white,
                  fontWeight: FontWeight.w800,
                  fontSize: 19,
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _NavTabButton extends StatelessWidget {
  final IconData icon;
  final String label;
  final bool selected;
  final VoidCallback onTap;

  const _NavTabButton({
    required this.icon,
    required this.label,
    required this.selected,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      borderRadius: BorderRadius.circular(20),
      onTap: onTap,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 200),
        curve: Curves.easeOutCubic,
        padding: EdgeInsets.symmetric(horizontal: selected ? 14 : 8, vertical: 8),
        decoration: BoxDecoration(
          color: selected ? NivaroColors.primary.withOpacity(0.2) : Colors.transparent,
          borderRadius: BorderRadius.circular(20),
          border: selected ? Border.all(color: NivaroColors.primary.withOpacity(0.35)) : null,
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              icon,
              color: selected ? NivaroColors.primaryLight : NivaroColors.textMuted,
              size: 22,
            ),
            if (selected) ...[
              const SizedBox(width: 6),
              Text(
                label,
                style: const TextStyle(
                  color: NivaroColors.primaryLight,
                  fontWeight: FontWeight.w700,
                  fontSize: 12.5,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// Grid tile for Folders and Files in the Files screen.
class FolderTile extends StatelessWidget {
  final String name;
  final Color color;
  final IconData glyph;
  final VoidCallback onTap;
  final VoidCallback? onMore;
  final VoidCallback? onLongPress;
  final bool selected;

  const FolderTile({
    super.key,
    required this.name,
    required this.color,
    required this.glyph,
    required this.onTap,
    this.onMore,
    this.onLongPress,
    this.selected = false,
  });

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
        onTap: () {
          HapticFeedback.lightImpact();
          onTap();
        },
        onLongPress: onLongPress == null
            ? null
            : () {
                HapticFeedback.mediumImpact();
                onLongPress!();
              },
        child: Container(
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: NivaroColors.surface,
            borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
            border: Border.all(
              color: selected ? NivaroColors.primaryLight : NivaroColors.border,
              width: selected ? 2 : 1,
            ),
            boxShadow: const [
              BoxShadow(color: Color(0x2B000000), blurRadius: 8, offset: Offset(0, 3)),
            ],
          ),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Stack(
                alignment: Alignment.center,
                clipBehavior: Clip.none,
                children: [
                  Container(
                    width: 48,
                    height: 48,
                    decoration: BoxDecoration(
                      color: color.withOpacity(0.15),
                      borderRadius: BorderRadius.circular(NivaroShape.medium),
                    ),
                    alignment: Alignment.center,
                    child: selected
                        ? const Icon(Icons.check_rounded, color: NivaroColors.primaryLight, size: 24)
                        : Icon(glyph, color: color, size: 24),
                  ),
                  if (onMore != null && !selected)
                    Positioned(
                      top: -10,
                      right: -16,
                      child: IconButton(
                        icon: const Icon(Icons.more_vert, size: 16, color: NivaroColors.textMuted),
                        onPressed: onMore,
                        constraints: const BoxConstraints(minWidth: 28, minHeight: 28),
                        padding: EdgeInsets.zero,
                      ),
                    ),
                ],
              ),
              const SizedBox(height: 10),
              Text(
                name,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                textAlign: TextAlign.center,
                style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 12.5),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Curated monogram palette for app launchers.
const List<Color> _monogramPalette = [
  Color(0xFF2563EB),
  Color(0xFF7C3AED),
  Color(0xFFDB2777),
  Color(0xFFEA580C),
  Color(0xFF10B981),
  Color(0xFF0EA5E9),
  Color(0xFFEAB308),
  Color(0xFF6366F1),
];

Color monogramColorFor(String seed) {
  if (seed.isEmpty) return _monogramPalette.first;
  final hash = seed.codeUnits.fold<int>(0, (acc, c) => acc + c);
  return _monogramPalette[hash % _monogramPalette.length];
}

/// App launcher icon tile with status dot and update badge.
class AppIconTile extends StatelessWidget {
  final String name;
  final String iconUrl;
  final bool running;
  final bool showStatusDot;
  final VoidCallback onTap;
  final VoidCallback? onLongPress;
  final Widget? badge;

  const AppIconTile({
    super.key,
    required this.name,
    required this.iconUrl,
    required this.onTap,
    this.onLongPress,
    this.running = false,
    this.showStatusDot = true,
    this.badge,
  });

  @override
  Widget build(BuildContext context) {
    final color = monogramColorFor(name);
    return InkWell(
      borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
      onTap: () {
        HapticFeedback.lightImpact();
        onTap();
      },
      onLongPress: onLongPress == null
          ? null
          : () {
              HapticFeedback.mediumImpact();
              onLongPress!();
            },
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          AspectRatio(
            aspectRatio: 1,
            child: Stack(
              clipBehavior: Clip.none,
              children: [
                Positioned.fill(
                  child: Container(
                    decoration: BoxDecoration(
                      borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
                      border: Border.all(color: NivaroColors.border),
                      boxShadow: const [
                        BoxShadow(
                          color: Color(0x33000000),
                          blurRadius: 8,
                          offset: Offset(0, 3),
                        ),
                      ],
                    ),
                    child: ClipRRect(
                      borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
                      child: iconUrl.isNotEmpty
                          ? Image.network(
                              iconUrl,
                              fit: BoxFit.cover,
                              errorBuilder: (_, __, ___) => _Monogram(name: name, color: color),
                            )
                          : _Monogram(name: name, color: color),
                    ),
                  ),
                ),
                if (showStatusDot)
                  Positioned(
                    right: 2,
                    bottom: 2,
                    child: Container(
                      width: 12,
                      height: 12,
                      decoration: BoxDecoration(
                        color: running ? NivaroColors.success : NivaroColors.textFaint,
                        shape: BoxShape.circle,
                        border: Border.all(color: NivaroColors.background, width: 2),
                      ),
                    ),
                  ),
                if (badge != null) Positioned(top: -4, right: -4, child: badge!),
              ],
            ),
          ),
          const SizedBox(height: 8),
          Text(
            name,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            textAlign: TextAlign.center,
            style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 12),
          ),
        ],
      ),
    );
  }
}

class _Monogram extends StatelessWidget {
  final String name;
  final Color color;
  const _Monogram({required this.name, required this.color});

  @override
  Widget build(BuildContext context) {
    final letter = name.trim().isNotEmpty ? name.trim()[0].toUpperCase() : '?';
    return Container(
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [color, Color.lerp(color, Colors.black, 0.45)!],
        ),
      ),
      alignment: Alignment.center,
      child: Text(
        letter,
        style: const TextStyle(
          color: Colors.white,
          fontWeight: FontWeight.w800,
          fontSize: 24,
        ),
      ),
    );
  }
}

/// Section header with title and optional trailing widget (e.g. view all, refresh, counter).
class SectionHeader extends StatelessWidget {
  final String title;
  final Widget? trailing;
  final EdgeInsetsGeometry padding;

  const SectionHeader({
    super.key,
    required this.title,
    this.trailing,
    this.padding = const EdgeInsets.only(bottom: 12),
  });

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: padding,
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          Text(title, style: nivaroSectionLabelStyle),
          if (trailing != null) trailing!,
        ],
      ),
    );
  }
}
