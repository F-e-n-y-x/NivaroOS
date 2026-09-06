import 'package:flutter/material.dart';
import 'package:percent_indicator/percent_indicator.dart';
import '../theme.dart';

/// Small circular dark icon button used for header actions (search, menu,
/// refresh) - the reference app never uses a flat AppBar action icon, only
/// these floating circular ones.
class RoundIconButton extends StatelessWidget {
  final IconData icon;
  final VoidCallback? onPressed;
  final double size;
  const RoundIconButton({super.key, required this.icon, this.onPressed, this.size = 40});

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: size,
      height: size,
      child: Material(
        color: NivaroColors.surface,
        shape: const CircleBorder(),
        child: InkWell(
          customBorder: const CircleBorder(),
          onTap: onPressed,
          child: Icon(icon, size: size * 0.46, color: NivaroColors.textPrimary),
        ),
      ),
    );
  }
}

/// A green wifi glyph + "LAN" + the server's address, in a full-height pill -
/// appears on the discovery, login and home screens in the reference app.
class LanBadge extends StatelessWidget {
  final String address;
  const LanBadge({super.key, required this.address});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 14),
      decoration: BoxDecoration(
        color: NivaroColors.surface,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: NivaroColors.border),
      ),
      child: Row(
        children: [
          const Icon(Icons.wifi_rounded, color: NivaroColors.success, size: 18),
          const SizedBox(width: 6),
          const Text('LAN', style: TextStyle(color: NivaroColors.success, fontWeight: FontWeight.w700, fontSize: 13)),
          const Spacer(),
          Text(address, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13.5)),
        ],
      ),
    );
  }
}

/// The device/server identity card reused across discovery, login and home -
/// a big icon tile on a dark card, mirroring the reference app's "ZimaOS"
/// device tile exactly.
class ServerCard extends StatelessWidget {
  final String title;
  final Widget icon;
  const ServerCard({super.key, required this.title, required this.icon});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: NivaroColors.surface,
        borderRadius: BorderRadius.circular(22),
        border: Border.all(color: NivaroColors.border),
      ),
      child: Row(
        children: [
          Expanded(
            child: Text(title, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 18, color: NivaroColors.textPrimary)),
          ),
          icon,
        ],
      ),
    );
  }
}

/// One "Monitor" tile: a label, a dotted percent ring, and a value - the 2x2
/// grid on the reference app's home screen.
class StatRingCard extends StatelessWidget {
  final String label;
  final double percent;
  final String value;
  final Color color;
  const StatRingCard({super.key, required this.label, required this.percent, required this.value, required this.color});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: NivaroColors.surface,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: NivaroColors.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13, fontWeight: FontWeight.w600)),
          const SizedBox(height: 14),
          Align(
            alignment: Alignment.centerRight,
            child: CircularPercentIndicator(
              radius: 38,
              lineWidth: 6,
              percent: percent.clamp(0, 1),
              center: Text(value, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14, color: NivaroColors.textPrimary)),
              progressColor: color,
              backgroundColor: NivaroColors.surfaceMuted,
              circularStrokeCap: CircularStrokeCap.round,
              animation: true,
              animationDuration: 500,
            ),
          ),
        ],
      ),
    );
  }
}

/// A plain-value "Monitor" tile (temperature, network) without a ring -
/// still dark-card-styled to match its ring-based neighbors in the grid.
class StatValueCard extends StatelessWidget {
  final String label;
  final Widget child;
  const StatValueCard({super.key, required this.label, required this.child});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: NivaroColors.surface,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: NivaroColors.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13, fontWeight: FontWeight.w600)),
          const SizedBox(height: 14),
          child,
        ],
      ),
    );
  }
}

/// The segmented-dot storage bar from the reference app's Storage cards -
/// a row of small dots, a filled prefix in [color] and the rest muted,
/// rather than a plain linear progress bar.
class DotStorageBar extends StatelessWidget {
  final double fraction;
  final Color color;
  final int dots;
  const DotStorageBar({super.key, required this.fraction, this.color = NivaroColors.success, this.dots = 22});

  @override
  Widget build(BuildContext context) {
    final filled = (fraction.clamp(0, 1) * dots).round();
    return LayoutBuilder(
      builder: (context, constraints) {
        return Wrap(
          spacing: 4,
          runSpacing: 4,
          children: List.generate(dots, (i) {
            return Container(
              width: 6,
              height: 6,
              decoration: BoxDecoration(
                shape: BoxShape.circle,
                color: i < filled ? color : NivaroColors.surfaceMuted,
              ),
            );
          }),
        );
      },
    );
  }
}

/// A generic dark stat/storage card shell used by Home's Storage section.
class DarkCard extends StatelessWidget {
  final Widget child;
  final EdgeInsetsGeometry padding;
  const DarkCard({super.key, required this.child, this.padding = const EdgeInsets.all(16)});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: padding,
      decoration: BoxDecoration(
        color: NivaroColors.surface,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: NivaroColors.border),
      ),
      child: child,
    );
  }
}

/// The floating navigation from the reference app: a compact rounded pill
/// with icon-only tabs, and a separate circular avatar button beside it
/// (which opens Settings) - not a stock full-width Material bottom bar.
class FloatingNavBar extends StatelessWidget {
  final int currentIndex;
  final ValueChanged<int> onTap;
  final VoidCallback onAvatarTap;
  final String avatarInitial;

  static const _items = [
    (icon: Icons.home_outlined, active: Icons.home_rounded),
    (icon: Icons.folder_outlined, active: Icons.folder_rounded),
    (icon: Icons.dns_outlined, active: Icons.dns_rounded),
    (icon: Icons.apps_outlined, active: Icons.apps_rounded),
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
              height: 60,
              padding: const EdgeInsets.symmetric(horizontal: 6),
              decoration: BoxDecoration(
                color: NivaroColors.surfaceRaised,
                borderRadius: BorderRadius.circular(30),
                border: Border.all(color: NivaroColors.border),
                boxShadow: const [BoxShadow(color: Colors.black38, blurRadius: 16, offset: Offset(0, 6))],
              ),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                children: List.generate(_items.length, (i) {
                  final selected = i == currentIndex;
                  final item = _items[i];
                  return _NavIcon(
                    icon: selected ? item.active : item.icon,
                    selected: selected,
                    onTap: () => onTap(i),
                  );
                }),
              ),
            ),
          ),
          const SizedBox(width: 10),
          GestureDetector(
            onTap: onAvatarTap,
            child: Container(
              width: 60,
              height: 60,
              decoration: BoxDecoration(
                color: NivaroColors.primary,
                shape: BoxShape.circle,
                boxShadow: const [BoxShadow(color: Colors.black38, blurRadius: 16, offset: Offset(0, 6))],
              ),
              alignment: Alignment.center,
              child: Text(
                avatarInitial,
                style: const TextStyle(color: Colors.white, fontWeight: FontWeight.w800, fontSize: 20),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _NavIcon extends StatelessWidget {
  final IconData icon;
  final bool selected;
  final VoidCallback onTap;
  const _NavIcon({required this.icon, required this.selected, required this.onTap});

  @override
  Widget build(BuildContext context) {
    return InkWell(
      customBorder: const CircleBorder(),
      onTap: onTap,
      child: Container(
        width: 44,
        height: 44,
        decoration: BoxDecoration(
          color: selected ? NivaroColors.primary.withOpacity(0.18) : Colors.transparent,
          shape: BoxShape.circle,
        ),
        alignment: Alignment.center,
        child: Icon(icon, color: selected ? NivaroColors.primary : NivaroColors.textMuted, size: 24),
      ),
    );
  }
}

/// The colored gradient folder tile from the reference app's Files screen -
/// each well-known folder kind gets its own accent + glyph instead of every
/// folder looking identical.
class FolderTile extends StatelessWidget {
  final String name;
  final Color color;
  final IconData glyph;
  final VoidCallback onTap;
  final VoidCallback? onMore;
  const FolderTile({super.key, required this.name, required this.color, required this.glyph, required this.onTap, this.onMore});

  @override
  Widget build(BuildContext context) {
    return InkWell(
      borderRadius: BorderRadius.circular(18),
      onTap: onTap,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          AspectRatio(
            aspectRatio: 1.15,
            child: Stack(
              children: [
                Container(
                  decoration: BoxDecoration(
                    gradient: LinearGradient(
                      begin: Alignment.topLeft,
                      end: Alignment.bottomRight,
                      colors: [color, Color.lerp(color, Colors.black, 0.25)!],
                    ),
                    borderRadius: BorderRadius.circular(18),
                  ),
                  alignment: Alignment.center,
                  child: Icon(glyph, color: Colors.white.withOpacity(0.9), size: 34),
                ),
                if (onMore != null)
                  Positioned(
                    top: 2,
                    right: 2,
                    child: IconButton(
                      icon: const Icon(Icons.more_vert, size: 18, color: Colors.white),
                      onPressed: onMore,
                      constraints: const BoxConstraints(minWidth: 30, minHeight: 30),
                      padding: EdgeInsets.zero,
                    ),
                  ),
              ],
            ),
          ),
          const SizedBox(height: 6),
          Text(name, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13.5)),
        ],
      ),
    );
  }
}
