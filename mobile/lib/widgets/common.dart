import 'package:flutter/material.dart';
import '../theme.dart';
import '../utils/app_icons.dart';

/// Pulsing status dot indicating live state.
class PulsingStatusDot extends StatefulWidget {
  /// Defaults to the theme's success colour.
  final Color? color;
  final double size;
  final bool animate;
  const PulsingStatusDot({
    super.key,
    this.color,
    this.size = 7,
    this.animate = true,
  });

  @override
  State<PulsingStatusDot> createState() => _PulsingStatusDotState();
}

class _PulsingStatusDotState extends State<PulsingStatusDot>
    with SingleTickerProviderStateMixin {
  late final AnimationController _ctrl = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1400),
  )..repeat(reverse: true);

  late final Animation<double> _anim =
      Tween<double>(begin: 0.45, end: 1.0).animate(
    CurvedAnimation(parent: _ctrl, curve: Curves.easeInOut),
  );

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final color = widget.color ?? NivaroColors.success;
    if (!widget.animate) {
      return Container(
        width: widget.size,
        height: widget.size,
        decoration: BoxDecoration(
          color: color,
          shape: BoxShape.circle,
        ),
      );
    }
    return AnimatedBuilder(
      animation: _anim,
      builder: (context, _) => Container(
        width: widget.size,
        height: widget.size,
        decoration: BoxDecoration(
          color: color.withValues(alpha: _anim.value),
          shape: BoxShape.circle,
          boxShadow: [
            BoxShadow(
              color: color.withValues(alpha: 0.45 * _anim.value),
              blurRadius: 6,
              spreadRadius: 1,
            ),
          ],
        ),
      ),
    );
  }
}

/// Circular action button with unified dark surface styling.
class RoundIconButton extends StatelessWidget {
  final IconData icon;
  final VoidCallback? onPressed;
  final String? tooltip;
  final double size;
  final Color? color;
  final Color? iconColor;

  const RoundIconButton({
    super.key,
    required this.icon,
    this.onPressed,
    this.tooltip,
    this.size = 40,
    this.color,
    this.iconColor,
  });

  @override
  Widget build(BuildContext context) {
    final btn = Material(
      color: Colors.transparent,
      child: InkWell(
        onTap: onPressed == null
            ? null
            : () {
                onPressed!();
              },
        customBorder: const CircleBorder(),
        child: Container(
          width: size,
          height: size,
          decoration: BoxDecoration(
            color: color ?? NivaroColors.surfaceRaised,
            shape: BoxShape.circle,
            border: Border.all(color: NivaroColors.borderSubtle),
          ),
          alignment: Alignment.center,
          child: Icon(icon,
              size: size * 0.5, color: iconColor ?? NivaroColors.textPrimary),
        ),
      ),
    );

    if (tooltip != null) {
      return Tooltip(message: tooltip!, child: btn);
    }
    return btn;
  }
}

/// Deprecated: the v1 section header. New code uses `SectionHeader` from
/// lib/ui/widgets/section_header.dart.
class LegacySectionHeader extends StatelessWidget {
  final String title;
  final String? subtitle;
  final Widget? trailing;

  const LegacySectionHeader({
    super.key,
    required this.title,
    this.subtitle,
    this.trailing,
  });

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 12, top: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  title,
                  style: TextStyle(
                    fontWeight: FontWeight.w800,
                    fontSize: 16,
                    letterSpacing: -0.2,
                    color: NivaroColors.textPrimary,
                  ),
                ),
                if (subtitle != null) ...[
                  const SizedBox(height: 2),
                  Text(
                    subtitle!,
                    style: TextStyle(
                      color: NivaroColors.textMuted,
                      fontSize: 12,
                      fontWeight: FontWeight.w400,
                    ),
                  ),
                ],
              ],
            ),
          ),
          ?trailing,
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
        border: Border.all(color: NivaroColors.borderSubtle),
        boxShadow: const [
          BoxShadow(
              color: Color(0x2B000000), blurRadius: 12, offset: Offset(0, 4)),
        ],
      ),
      // ListTiles paint their ink on the nearest Material; without one here
      // the card's own colour hides every ripple (and trips a debug assert).
      child: Material(type: MaterialType.transparency, child: child),
    );

    if (onTap != null) {
      return Material(
        color: Colors.transparent,
        child: InkWell(
          borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
          onTap: () {
            onTap!();
          },
          child: card,
        ),
      );
    }
    return card;
  }
}

/// Status badge showing server connection address, ping, and status dot.
class LanBadge extends StatelessWidget {
  final String address;
  final String label;
  final int? pingMs;
  final bool isOnline;
  final VoidCallback? onTap;

  const LanBadge({
    super.key,
    required this.address,
    this.label = 'LAN',
    this.pingMs,
    this.isOnline = true,
    this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final statusColor =
        isOnline ? NivaroColors.successLight : NivaroColors.dangerLight;
    return Material(
      color: Colors.transparent,
      child: InkWell(
        onTap: onTap != null
            ? () {
                onTap!();
              }
            : null,
        borderRadius: BorderRadius.circular(NivaroShape.full),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
          decoration: BoxDecoration(
            color: NivaroColors.surfaceRaised,
            borderRadius: BorderRadius.circular(NivaroShape.full),
            border: Border.all(color: NivaroColors.borderSubtle),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              PulsingStatusDot(color: statusColor, size: 7),
              const SizedBox(width: 8),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                decoration: BoxDecoration(
                  color: NivaroColors.primary.withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(4),
                ),
                child: Text(
                  label,
                  style: TextStyle(
                    color: NivaroColors.primaryLight,
                    fontSize: 10.5,
                    fontWeight: FontWeight.w800,
                  ),
                ),
              ),
              if (pingMs != null) ...[
                const SizedBox(width: 8),
                Text(
                  '${pingMs}ms',
                  style: TextStyle(
                      color: NivaroColors.textMuted,
                      fontSize: 11,
                      fontWeight: FontWeight.w600),
                ),
              ],
              const SizedBox(width: 10),
              Container(width: 1, height: 12, color: NivaroColors.borderSubtle),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  address,
                  style: TextStyle(
                    color: NivaroColors.textMuted,
                    fontSize: 12.5,
                    fontWeight: FontWeight.w500,
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              if (onTap != null)
                Icon(Icons.speed_rounded,
                    size: 16, color: NivaroColors.primaryLight),
            ],
          ),
        ),
      ),
    );
  }
}

/// Real-time hardware system monitor card with unified dark styling.
class MonitorCard extends StatelessWidget {
  final String label;
  final IconData icon;
  final Color? color;
  final double? percent;
  final Widget value;
  final String? subtitle;
  final VoidCallback? onTap;

  const MonitorCard({
    super.key,
    required this.label,
    required this.icon,
    this.color,
    this.percent,
    required this.value,
    this.subtitle,
    this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final barColor = color ?? NivaroColors.primaryLight;
    final card = Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: NivaroColors.surface,
        borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
        border: Border.all(color: NivaroColors.borderSubtle),
        boxShadow: const [
          BoxShadow(
              color: Color(0x2B000000), blurRadius: 10, offset: Offset(0, 3)),
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
                  color: NivaroColors.primary.withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(8),
                ),
                alignment: Alignment.center,
                child: Icon(icon, color: NivaroColors.primaryLight, size: 17),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  label,
                  style: TextStyle(
                    color: NivaroColors.textSecondary,
                    fontSize: 12.5,
                    fontWeight: FontWeight.w600,
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              if (onTap != null)
                Icon(Icons.chevron_right_rounded,
                    size: 16, color: NivaroColors.textFaint),
            ],
          ),
          const SizedBox(height: 8),
          value,
          if (percent != null) ...[
            const SizedBox(height: 8),
            ClipRRect(
              borderRadius: BorderRadius.circular(3),
              child: LinearProgressIndicator(
                value: percent!.clamp(0.0, 1.0),
                backgroundColor: NivaroColors.surfaceRaised,
                valueColor: AlwaysStoppedAnimation<Color>(barColor),
                minHeight: 4.5,
              ),
            ),
          ],
          if (subtitle != null) ...[
            const SizedBox(height: 4),
            Text(
              subtitle!,
              style:
                  TextStyle(color: NivaroColors.textMuted, fontSize: 11),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          ],
        ],
      ),
    );

    if (onTap != null) {
      return Material(
        color: Colors.transparent,
        child: InkWell(
          borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
          onTap: () {
            onTap!();
          },
          child: card,
        ),
      );
    }
    return card;
  }
}

/// Card showing physical storage drive or mounted pool.
class DriveCard extends StatelessWidget {
  final String label;
  final String percentText;
  final double fraction;
  final int usedBytes;
  final int sizeBytes;
  final bool isUsb;
  final VoidCallback? onTap;

  const DriveCard({
    super.key,
    required this.label,
    required this.percentText,
    required this.fraction,
    required this.usedBytes,
    required this.sizeBytes,
    this.isUsb = false,
    this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return DarkCard(
      onTap: onTap,
      padding: const EdgeInsets.all(14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 36,
                height: 36,
                decoration: BoxDecoration(
                  color: isUsb
                      ? NivaroColors.accent.withValues(alpha: 0.12)
                      : NivaroColors.primary.withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(NivaroShape.medium),
                ),
                alignment: Alignment.center,
                child: Icon(
                  isUsb ? Icons.usb_rounded : Icons.storage_rounded,
                  color: isUsb
                      ? NivaroColors.accentLight
                      : NivaroColors.primaryLight,
                  size: 19,
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      label,
                      style: TextStyle(
                          fontWeight: FontWeight.w700,
                          fontSize: 13.5,
                          color: NivaroColors.textPrimary),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                    const SizedBox(height: 2),
                    Text(
                      '${_fmt(usedBytes)} of ${_fmt(sizeBytes)} used',
                      style: TextStyle(
                        color: NivaroColors.textMuted,
                        fontSize: 11.5,
                      ),
                    ),
                  ],
                ),
              ),
              Text(
                percentText,
                style: TextStyle(
                  fontWeight: FontWeight.w800,
                  fontSize: 14,
                  color: NivaroColors.textPrimary,
                ),
              ),
              if (onTap != null) ...[
                const SizedBox(width: 6),
                Icon(Icons.chevron_right_rounded,
                    size: 18, color: NivaroColors.textFaint),
              ],
            ],
          ),
          const SizedBox(height: 10),
          ClipRRect(
            borderRadius: BorderRadius.circular(3),
            child: LinearProgressIndicator(
              value: fraction.clamp(0.0, 1.0),
              backgroundColor: NivaroColors.surfaceRaised,
              valueColor: AlwaysStoppedAnimation<Color>(
                fraction > 0.90
                    ? NivaroColors.dangerLight
                    : (fraction > 0.80
                        ? NivaroColors.warningLight
                        : NivaroColors.primaryLight),
              ),
              minHeight: 5,
            ),
          ),
        ],
      ),
    );
  }

  static String _fmt(int bytes) {
    const suffixes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
    var value = bytes.toDouble();
    var i = 0;
    while (value >= 1024 && i < suffixes.length - 1) {
      value /= 1024;
      i++;
    }
    return '${value.toStringAsFixed(i == 0 ? 0 : 1)} ${suffixes[i]}';
  }
}

/// Card showing a connected online cloud storage account.
class CloudAccountCard extends StatelessWidget {
  final String name;
  final String providerTitle;
  final String mountPoint;
  final String type;
  final double? uploadMbps;
  final double? downloadMbps;
  final VoidCallback onTap;

  const CloudAccountCard({
    super.key,
    required this.name,
    required this.providerTitle,
    required this.mountPoint,
    required this.type,
    this.uploadMbps,
    this.downloadMbps,
    required this.onTap,
  });

  IconData get _icon {
    switch (type.toLowerCase()) {
      case 'drive':
        return Icons.add_to_drive_rounded;
      case 'onedrive':
        return Icons.cloud_queue_rounded;
      case 'dropbox':
        return Icons.inventory_2_rounded;
      case 'icloud':
        return Icons.cloud_done_rounded;
      case 's3':
        return Icons.dns_rounded;
      case 'webdav':
        return Icons.folder_shared_rounded;
      default:
        return Icons.cloud_rounded;
    }
  }

  @override
  Widget build(BuildContext context) {
    return DarkCard(
      onTap: onTap,
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      child: Row(
        children: [
          Container(
            width: 38,
            height: 38,
            decoration: BoxDecoration(
              color: NivaroColors.primary.withValues(alpha: 0.12),
              borderRadius: BorderRadius.circular(NivaroShape.medium),
            ),
            alignment: Alignment.center,
            child: Icon(_icon, color: NivaroColors.primaryLight, size: 20),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        name,
                        style: TextStyle(
                            fontWeight: FontWeight.w700,
                            fontSize: 13.5,
                            color: NivaroColors.textPrimary),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                    Container(
                      padding: const EdgeInsets.symmetric(
                          horizontal: 6, vertical: 2),
                      decoration: BoxDecoration(
                        color: NivaroColors.success.withValues(alpha: 0.12),
                        borderRadius: BorderRadius.circular(4),
                      ),
                      child: Text('Connected',
                          style: TextStyle(
                              color: NivaroColors.successLight,
                              fontSize: 10,
                              fontWeight: FontWeight.bold)),
                    ),
                  ],
                ),
                const SizedBox(height: 2),
                Text(
                  '$providerTitle · $mountPoint',
                  style: TextStyle(
                      color: NivaroColors.textMuted, fontSize: 11.5),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ],
            ),
          ),
          const SizedBox(width: 8),
          Icon(Icons.chevron_right_rounded,
              size: 18, color: NivaroColors.textFaint),
        ],
      ),
    );
  }
}

/// Compact favorite folder card for Files Screen (synced with WebUI).
class FavoriteCard extends StatelessWidget {
  final String name;
  final String? path;
  final Color? color;
  final IconData glyph;
  final bool isCustom;
  final VoidCallback onTap;
  final VoidCallback? onLongPress;

  const FavoriteCard({
    super.key,
    required this.name,
    this.path,
    this.color,
    required this.glyph,
    this.isCustom = false,
    required this.onTap,
    this.onLongPress,
  });

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
        onTap: () {
          onTap();
        },
        onLongPress: onLongPress != null
            ? () {
                onLongPress!();
              }
            : null,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 13, vertical: 11),
          decoration: BoxDecoration(
            color: NivaroColors.surface,
            borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
            border: Border.all(color: NivaroColors.borderSubtle),
            boxShadow: const [
              BoxShadow(
                  color: Color(0x2B000000),
                  blurRadius: 10,
                  offset: Offset(0, 3)),
            ],
          ),
          child: Row(
            children: [
              Container(
                width: 36,
                height: 36,
                decoration: BoxDecoration(
                  color: NivaroColors.primary.withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(NivaroShape.medium),
                ),
                alignment: Alignment.center,
                child: Icon(glyph, color: NivaroColors.primaryLight, size: 19),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            name,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(
                                fontWeight: FontWeight.w700,
                                fontSize: 13.5,
                                color: NivaroColors.textPrimary),
                          ),
                        ),
                        if (isCustom)
                          Container(
                            padding: const EdgeInsets.symmetric(
                                horizontal: 5, vertical: 1),
                            decoration: BoxDecoration(
                              color: NivaroColors.primary.withValues(alpha: 0.12),
                              borderRadius: BorderRadius.circular(4),
                            ),
                            child: Text('Pinned',
                                style: TextStyle(
                                    color: NivaroColors.primaryLight,
                                    fontSize: 9.5,
                                    fontWeight: FontWeight.bold)),
                          ),
                      ],
                    ),
                    if (path != null) ...[
                      const SizedBox(height: 2),
                      Text(
                        path!,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                            color: NivaroColors.textMuted, fontSize: 11),
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Status Pill Badge
class StatusPill extends StatelessWidget {
  final String label;
  final String? state;
  final Color? color;

  const StatusPill({
    super.key,
    required this.label,
    this.state,
    this.color,
  });

  @override
  Widget build(BuildContext context) {
    final c = color ??
        (state == 'running'
            ? NivaroColors.successLight
            : NivaroColors.textMuted);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: c.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(6),
        border: Border.all(color: c.withValues(alpha: 0.3)),
      ),
      child: Text(
        label,
        style: TextStyle(color: c, fontSize: 10.5, fontWeight: FontWeight.bold),
      ),
    );
  }
}

/// Application Grid Tile matching WebUI icon display.
class AppTile extends StatelessWidget {
  final String name;
  final String iconUrl;
  final String? imageName;
  final bool running;
  final VoidCallback? onTap;
  final VoidCallback? onLongPress;
  final Widget? badge;

  final double? customRadiusPercent;

  const AppTile({
    super.key,
    required this.name,
    required this.iconUrl,
    this.imageName,
    this.running = true,
    this.customRadiusPercent,
    this.onTap,
    this.onLongPress,
    this.badge,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      borderRadius: BorderRadius.circular(16),
      onTap: () {
        if (onTap != null) onTap!();
      },
      onLongPress: () {
        if (onLongPress != null) onLongPress!();
      },
      // GridView.builder forces every item into a tight cellWidth x
      // cellHeight box (that's what childAspectRatio actually sizes) - this
      // tile's own content (icon + label) is shorter than that, and Column's
      // default mainAxisAlignment.start left it pinned to the top of the
      // cell with dead space below, instead of centered in it. Center here
      // (Column's mainAxisSize.min alone doesn't help once the parent gives
      // it a tight constraint - it still fills the cell, just top-aligns
      // its children within that space) fixes every grid density (3-9
      // columns) at once rather than hand-tuning padding per breakpoint.
      child: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Stack(
              clipBehavior: Clip.none,
              children: [
                NivaroAppIcon(
                  iconUrl: iconUrl,
                  name: name,
                  imageName: imageName,
                  size: 58,
                  radius: 15,
                  customRadiusPercent: customRadiusPercent,
                ),
                Positioned(
                  right: -2,
                  bottom: -2,
                  child: Container(
                    width: 12,
                    height: 12,
                    decoration: BoxDecoration(
                      color: running
                          ? NivaroColors.success
                          : NivaroColors.textFaint,
                      shape: BoxShape.circle,
                      border: Border.all(color: NivaroColors.surface, width: 2),
                    ),
                  ),
                ),
                if (badge != null)
                  Positioned(
                    top: -4,
                    right: -4,
                    child: badge!,
                  ),
              ],
            ),
            const SizedBox(height: 7),
            Text(
              name,
              style: TextStyle(
                  fontWeight: FontWeight.w600,
                  fontSize: 12,
                  color: NivaroColors.textPrimary),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              textAlign: TextAlign.center,
            ),
          ],
        ),
      ),
    );
  }
}
