// The Files screen's rows, tiles and bars, on the v2 design system.
import 'dart:io';

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import '../../models/file_entry.dart';
import '../../services/api_client.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import 'file_ops.dart';
import 'transfers.dart';

/// The one glyph for each kind of file (outlined, per the brief).
IconData fileKindIcon(FileKind kind) => switch (kind) {
      FileKind.folder => Icons.folder_outlined,
      FileKind.image => Icons.image_outlined,
      FileKind.video => Icons.movie_outlined,
      FileKind.audio => Icons.audio_file_outlined,
      FileKind.pdf => Icons.picture_as_pdf_outlined,
      FileKind.document => Icons.description_outlined,
      FileKind.spreadsheet => Icons.table_chart_outlined,
      FileKind.presentation => Icons.slideshow_outlined,
      FileKind.archive => Icons.folder_zip_outlined,
      FileKind.code => Icons.code_outlined,
      FileKind.text => Icons.article_outlined,
      FileKind.other => Icons.insert_drive_file_outlined,
    };

/// The glyph for a location.
IconData locationIcon(LocationKind kind) => switch (kind) {
      LocationKind.storage => Icons.storage_outlined,
      LocationKind.usb => Icons.usb_outlined,
      LocationKind.cloud => Icons.cloud_outlined,
      LocationKind.thisPhone => Icons.smartphone_outlined,
      LocationKind.phone => Icons.smartphone_outlined,
      LocationKind.favorite => Icons.folder_outlined,
      LocationKind.root => Icons.dns_outlined,
      LocationKind.trash => Icons.delete_outline,
    };

/// "2.4 MB · 5 min ago" for a file, "Folder · Yesterday" for a folder.
String entrySubtitle(FileEntry e) {
  final parts = <String>[
    e.isDir ? 'Folder' : formatSize(e.size),
    if (e.modified != null && e.modified!.year > 1971) formatRelative(e.modified!, now: clock.now()),
  ];
  return parts.join(' · ');
}

/// The leading square of a row or tile: a picture's thumbnail when there
/// is one, otherwise the kind's icon. Folders are tinted primary (the one
/// accent); files use the neutral variant colour.
class FileThumbnail extends StatelessWidget {
  const FileThumbnail({super.key, required this.entry, required this.isLocal, this.size = 40, this.iconSize = 24});

  /// Fills the space it gets (the grid's preview area).
  const FileThumbnail.fill({super.key, required this.entry, required this.isLocal, this.iconSize = 40}) : size = null;

  final FileEntry entry;
  final bool isLocal;

  /// The square's side; null to fill.
  final double? size;
  final double iconSize;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final icon = Icon(
      fileKindIcon(entry.kind),
      size: iconSize,
      color: entry.isDir ? scheme.primary : scheme.onSurfaceVariant,
    );
    Widget child = Center(child: icon);
    if (entry.hasThumbnail) {
      final px = ((size ?? 200) * MediaQuery.devicePixelRatioOf(context)).round();
      Widget fallback(BuildContext _, Object _, StackTrace? _) => Center(child: icon);
      // The picture sits on a tile only once it has decoded; until then,
      // and when it fails, the row shows the plain icon like other files.
      Widget framed(BuildContext context, Widget child, int? frame, bool sync) => sync || frame != null
          ? ClipRRect(
              borderRadius: BorderRadius.circular(DesignTokens.of(context).radii.sm),
              child: ColoredBox(color: scheme.surfaceContainerHighest, child: SizedBox.expand(child: child)),
            )
          : Center(child: icon);
      child = isLocal
          ? Image.file(File(entry.path), fit: BoxFit.cover, cacheWidth: px, errorBuilder: fallback, frameBuilder: framed)
          : Image.network(
              ApiClient.instance.buildUri('/image', {'path': entry.path, 'type': 'thumbnail'}).toString(),
              headers: {'Authorization': ?ApiClient.instance.accessToken},
              fit: BoxFit.cover,
              cacheWidth: px,
              errorBuilder: fallback,
              frameBuilder: framed,
            );
    }
    return ExcludeSemantics(child: size == null ? SizedBox.expand(child: child) : SizedBox.square(dimension: size, child: child));
  }
}

/// A file or folder in the list view.
class FileRow extends StatelessWidget {
  const FileRow({
    super.key,
    required this.entry,
    required this.isLocal,
    required this.selected,
    required this.selecting,
    required this.onTap,
    required this.onLongPress,
    required this.onMore,
  });

  final FileEntry entry;
  final bool isLocal;
  final bool selected;

  /// Selection mode is on: taps toggle, the menu button is hidden.
  final bool selecting;
  final VoidCallback onTap;
  final VoidCallback onLongPress;
  final VoidCallback onMore;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Semantics(
      selected: selecting ? selected : null,
      child: ListTile(
        selected: selected,
        selectedTileColor: scheme.secondaryContainer,
        selectedColor: scheme.onSecondaryContainer,
        leading: selected
            ? ExcludeSemantics(
                child: SizedBox.square(dimension: 40, child: Icon(Icons.check_circle, color: scheme.primary, size: 28)),
              )
            : FileThumbnail(entry: entry, isLocal: isLocal),
        // Two lines for long names at large text sizes, where one line
        // leaves only a few characters.
        title: Text(entry.name, maxLines: MediaQuery.textScalerOf(context).scale(1) > 1.3 ? 2 : 1, overflow: TextOverflow.ellipsis),
        subtitle: Text(entrySubtitle(entry), maxLines: 1, overflow: TextOverflow.ellipsis),
        trailing: selecting
            ? null
            : IconButton(
                icon: const Icon(Icons.more_vert),
                tooltip: 'More options for ${entry.name}',
                onPressed: onMore,
              ),
        onTap: onTap,
        onLongPress: onLongPress,
      ),
    );
  }
}

/// A file or folder in the grid view: a large preview with the name under
/// it. Long-press or the menu button for actions.
class FileGridTile extends StatelessWidget {
  const FileGridTile({
    super.key,
    required this.entry,
    required this.isLocal,
    required this.selected,
    required this.selecting,
    required this.onTap,
    required this.onLongPress,
  });

  final FileEntry entry;
  final bool isLocal;
  final bool selected;
  final bool selecting;
  final VoidCallback onTap;
  final VoidCallback onLongPress;

  /// Height of the preview area.
  static const double previewHeight = 112;

  /// The tile's height for the current text size.
  static double extentFor(BuildContext context) {
    final scaler = MediaQuery.textScalerOf(context);
    final text = Theme.of(context).textTheme;
    final name = scaler.scale(text.bodyMedium?.fontSize ?? 14) * (text.bodyMedium?.height ?? 1.43);
    final meta = scaler.scale(text.bodySmall?.fontSize ?? 12) * (text.bodySmall?.height ?? 1.33);
    // Rounded up: the text lines' fractional heights must never overflow.
    return (previewHeight + Space.sm + name + meta + Space.md).ceilToDouble() + 1;
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Semantics(
      selected: selecting ? selected : null,
      button: true,
      label: '${entry.name}, ${entrySubtitle(entry)}',
      excludeSemantics: true,
      child: Card.filled(
        color: selected ? scheme.secondaryContainer : scheme.surfaceContainer,
        child: InkWell(
          onTap: onTap,
          onLongPress: onLongPress,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(Space.sm, Space.sm, Space.sm, Space.md),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                SizedBox(
                  height: previewHeight - Space.sm,
                  child: Stack(
                    fit: StackFit.expand,
                    children: [
                      entry.hasThumbnail
                          ? FileThumbnail.fill(entry: entry, isLocal: isLocal)
                          : Center(child: FileThumbnail(entry: entry, isLocal: isLocal, size: 56, iconSize: 40)),
                      if (selecting)
                        Positioned(
                          top: Space.xs,
                          left: Space.xs,
                          child: Icon(
                            selected ? Icons.check_circle : Icons.radio_button_unchecked,
                            color: selected ? scheme.primary : scheme.onSurfaceVariant,
                          ),
                        ),
                    ],
                  ),
                ),
                const SizedBox(height: Space.sm),
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: Space.xs),
                  child: Text(entry.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: theme.textTheme.bodyMedium),
                ),
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: Space.xs),
                  child: Text(
                    entrySubtitle(entry),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Skeleton rows shaped like [FileRow] (a 40dp square, two lines).
class FolderSkeleton extends StatelessWidget {
  const FolderSkeleton({super.key, this.rows = 8});

  final int rows;

  static const _widths = [0.55, 0.4, 0.68, 0.48, 0.6, 0.36, 0.52, 0.44];

  @override
  Widget build(BuildContext context) {
    final scaler = MediaQuery.textScalerOf(context);
    final text = Theme.of(context).textTheme;
    final titleLine = scaler.scale(text.bodyLarge?.fontSize ?? 16) * (text.bodyLarge?.height ?? 1.5);
    final subLine = scaler.scale(text.bodyMedium?.fontSize ?? 14) * (text.bodyMedium?.height ?? 1.43);
    return SliverSemantics(
      label: 'Loading',
      liveRegion: true,
      sliver: SkeletonPulse.sliver(
        child: SliverList.builder(
          itemCount: rows,
          itemBuilder: (context, i) => ExcludeSemantics(
            child: ConstrainedBox(
              constraints: const BoxConstraints(minHeight: 72),
              child: Padding(
                padding: EdgeInsets.symmetric(horizontal: Space.gutter(context), vertical: Space.sm),
                child: Row(
                  children: [
                    const SkeletonBox(width: 40, height: 40, corner: Corner.sm),
                    const SizedBox(width: Space.lg),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          SizedBox(
                            height: titleLine,
                            child: Align(
                              alignment: AlignmentDirectional.centerStart,
                              child: FractionallySizedBox(
                                widthFactor: _widths[i % _widths.length],
                                child: SkeletonBox(height: scaler.scale(14)),
                              ),
                            ),
                          ),
                          SizedBox(
                            height: subLine,
                            child: Align(
                              alignment: AlignmentDirectional.centerStart,
                              child: FractionallySizedBox(
                                widthFactor: 0.3,
                                child: SkeletonBox(height: scaler.scale(12)),
                              ),
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// The path from the location to the current folder, under the title.
/// The first step names the location and opens the Locations sheet; the
/// others go to that folder. It scrolls so the current folder is always
/// in view.
class BreadcrumbBar extends StatefulWidget implements PreferredSizeWidget {
  const BreadcrumbBar({
    super.key,
    required this.crumbs,
    required this.locationIcon,
    required this.onCrumb,
    required this.onLocations,
    required this.height,
  });

  final List<Crumb> crumbs;
  final IconData locationIcon;
  final ValueChanged<Crumb> onCrumb;
  final VoidCallback onLocations;
  final double height;

  /// The bar's height for the current text size.
  static double heightFor(BuildContext context) =>
      (MediaQuery.textScalerOf(context).scale(20) + 28).clamp(48.0, 96.0);

  @override
  Size get preferredSize => Size.fromHeight(height);

  @override
  State<BreadcrumbBar> createState() => _BreadcrumbBarState();
}

class _BreadcrumbBarState extends State<BreadcrumbBar> {
  final _scroll = ScrollController();

  @override
  void initState() {
    super.initState();
    _jumpToEnd();
  }

  @override
  void didUpdateWidget(BreadcrumbBar old) {
    super.didUpdateWidget(old);
    if (old.crumbs.last != widget.crumbs.last) _jumpToEnd();
  }

  void _jumpToEnd() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scroll.hasClients) _scroll.jumpTo(_scroll.position.maxScrollExtent);
    });
  }

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    final crumbs = widget.crumbs;
    return SizedBox(
      height: widget.height,
      child: Semantics(
        container: true,
        label: 'Folder path',
        child: ListView.separated(
          controller: _scroll,
          scrollDirection: Axis.horizontal,
          padding: EdgeInsets.symmetric(horizontal: gutter - Space.sm),
          itemCount: crumbs.length,
          separatorBuilder: (_, _) =>
              ExcludeSemantics(child: Icon(Icons.chevron_right, size: 20, color: scheme.onSurfaceVariant)),
          itemBuilder: (context, i) {
            final crumb = crumbs[i];
            final isLast = i == crumbs.length - 1;
            if (i == 0) {
              return Center(
                child: TextButton.icon(
                  onPressed: widget.onLocations,
                  icon: Icon(widget.locationIcon, size: 18),
                  label: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(crumb.label),
                      const Icon(Icons.arrow_drop_down, size: 20),
                    ],
                  ),
                  style: TextButton.styleFrom(
                    foregroundColor: isLast ? scheme.onSurface : scheme.onSurfaceVariant,
                    minimumSize: const Size(48, 48),
                    // The icon lands on the screen gutter, like row icons.
                    padding: const EdgeInsetsDirectional.only(start: Space.sm, end: Space.xs),
                  ),
                ).withSemantics('${crumb.label}, change location'),
              );
            }
            return Center(
              child: TextButton(
                onPressed: isLast ? null : () => widget.onCrumb(crumb),
                style: TextButton.styleFrom(
                  foregroundColor: scheme.onSurfaceVariant,
                  disabledForegroundColor: scheme.onSurface,
                  minimumSize: const Size(48, 48),
                  // Tighter than a stand-alone button, so the chevrons
                  // read as joining the steps of one path.
                  padding: const EdgeInsets.symmetric(horizontal: Space.sm),
                  textStyle: theme.textTheme.labelLarge,
                ),
                child: Text(crumb.label),
              ),
            );
          },
        ),
      ),
    );
  }
}

extension on Widget {
  Widget withSemantics(String label) => Semantics(label: label, button: true, excludeSemantics: true, child: this);
}

/// Sort order and view switch, above the list.
class SortHeader extends StatelessWidget {
  const SortHeader({
    super.key,
    required this.sort,
    required this.ascending,
    required this.grid,
    required this.onSort,
    required this.onToggleView,
    this.count,
  });

  final FileSort sort;
  final bool ascending;
  final bool grid;
  final VoidCallback onSort;
  final VoidCallback onToggleView;

  /// "12 items", shown when known.
  final String? count;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final gutter = Space.gutter(context);
    final dir = ascending ? 'ascending' : 'descending';
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter - Space.md, Space.xs, gutter - Space.md, 0),
      child: Row(
        children: [
          Semantics(
            label: 'Sort by ${sort.label.toLowerCase()}, $dir',
            button: true,
            excludeSemantics: true,
            child: TextButton.icon(
              onPressed: onSort,
              icon: Icon(ascending ? Icons.arrow_upward : Icons.arrow_downward, size: 18),
              label: Text(sort.label),
              style: TextButton.styleFrom(foregroundColor: scheme.onSurfaceVariant, minimumSize: const Size(48, 48)),
            ),
          ),
          if (count != null) ...[
            const SizedBox(width: Space.xs),
            Expanded(
              child: Text(
                count!,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant).tabular,
              ),
            ),
          ] else
            const Spacer(),
          IconButton(
            onPressed: onToggleView,
            icon: Icon(grid ? Icons.view_list_outlined : Icons.grid_view_outlined),
            tooltip: grid ? 'Show as list' : 'Show as grid',
          ),
        ],
      ),
    );
  }
}

/// The running transfer: what, how far, and Stop. Rebuilds on its own as
/// progress ticks, so the list underneath doesn't.
class TransferStrip extends StatelessWidget {
  const TransferStrip({super.key, required this.progress, required this.onCancel, this.queued = 0});

  final ValueListenable<TransferProgress?> progress;
  final VoidCallback onCancel;
  final int queued;

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<TransferProgress?>(
      valueListenable: progress,
      builder: (context, p, _) {
        if (p == null) return const SizedBox(width: double.infinity);
        final theme = Theme.of(context);
        final scheme = theme.colorScheme;
        final fraction = p.fraction;
        // The file name goes last: it is the part that can be cut short.
        final detail = <String>[
          if (p.totalBytes != null && p.totalBytes! > 0) '${formatSize(p.doneBytes)} of ${formatSize(p.totalBytes!)}',
          if (queued > 0) '$queued more waiting',
          if (p.current != null) p.current!,
        ].join(' · ');
        return Semantics(
          container: true,
          liveRegion: true,
          label: p.title,
          value: fraction == null ? null : '${(fraction * 100).round()}%',
          child: Material(
            color: scheme.surfaceContainerHigh,
            child: Padding(
              padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.sm, Space.sm, Space.sm),
              child: Row(
                children: [
                  Expanded(
                    child: ExcludeSemantics(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Text(p.title, maxLines: 1, overflow: TextOverflow.ellipsis, style: theme.textTheme.bodyMedium),
                          const SizedBox(height: Space.xs),
                          LinearProgressIndicator(value: fraction),
                          if (detail.isNotEmpty) ...[
                            const SizedBox(height: Space.xs),
                            Text(
                              detail,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant).tabular,
                            ),
                          ],
                        ],
                      ),
                    ),
                  ),
                  const SizedBox(width: Space.sm),
                  TextButton(onPressed: onCancel, child: const Text('Stop')),
                ],
              ),
            ),
          ),
        );
      },
    );
  }
}

/// While something is on the Files clipboard: "3 items on clipboard",
/// where they came from, Clear and "Paste here". A floating toolbar over
/// the list, so the user can move between folders (and tabs) to pick the
/// destination.
class PasteBar extends StatelessWidget {
  const PasteBar({
    super.key,
    required this.label,
    this.detail,
    required this.actionLabel,
    required this.onPaste,
    required this.onCancel,
  });

  final String label;

  /// A second, quieter line: what paste will do, or why it can't here.
  final String? detail;
  final String actionLabel;

  /// Null while pasting here isn't possible (the items' own folder for a
  /// move, or a location that isn't a folder).
  final VoidCallback? onPaste;

  /// Clears the clipboard.
  final VoidCallback onCancel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Padding(
      padding: EdgeInsets.symmetric(horizontal: Space.gutter(context)),
      child: Material(
        color: scheme.surfaceContainerHighest,
        elevation: 3,
        shadowColor: scheme.shadow,
        surfaceTintColor: Colors.transparent,
        borderRadius: BorderRadius.circular(DesignTokens.of(context).radii.lg),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(Space.lg, Space.sm, Space.sm, Space.sm),
          child: LayoutBuilder(builder: (context, constraints) {
            final text = Semantics(
              liveRegion: true,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(label, maxLines: 2, overflow: TextOverflow.ellipsis, style: theme.textTheme.bodyMedium),
                  if (detail != null)
                    Text(
                      detail!,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
                    ),
                ],
              ),
            );
            final buttons = [
              TextButton(onPressed: onCancel, child: const Text('Clear')),
              const SizedBox(width: Space.xs),
              FilledButton(onPressed: onPaste, child: Text(actionLabel)),
            ];
            // Too narrow for the words beside the buttons (a small phone,
            // large text): the buttons go under them.
            if (constraints.maxWidth < MediaQuery.textScalerOf(context).scale(340)) {
              return Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Padding(padding: const EdgeInsets.only(top: Space.xs, right: Space.sm), child: text),
                  Row(mainAxisAlignment: MainAxisAlignment.end, children: buttons),
                ],
              );
            }
            return Row(
              children: [
                Expanded(child: text),
                const SizedBox(width: Space.sm),
                ...buttons,
              ],
            );
          }),
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// Tabs

/// What a tab shows in the strip and the tabs sheet.
class FileTabInfo {
  const FileTabInfo({required this.id, required this.title, required this.icon, this.subtitle});
  final int id;
  final String title;
  final IconData icon;

  /// Where it is in full (the folder's path), for the sheet and tooltips.
  final String? subtitle;
}

/// The tabs button in the Files app bar: the number of open tabs in a
/// square, as browsers draw it. Opens the tabs sheet.
class TabsButton extends StatelessWidget {
  const TabsButton({super.key, required this.count, required this.onPressed});

  final int count;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      value: count == 1 ? '1 tab open' : '$count tabs open',
      child: IconButton(
        tooltip: 'Tabs',
        onPressed: onPressed,
        icon: Builder(builder: (context) {
          final icon = IconTheme.of(context);
          final size = icon.size ?? 24;
          final color = icon.color ?? Theme.of(context).colorScheme.onSurfaceVariant;
          return ExcludeSemantics(
            child: SizedBox.square(
              dimension: size,
              child: Center(
                child: Container(
                  width: size * 0.8,
                  height: size * 0.8,
                  alignment: Alignment.center,
                  decoration: BoxDecoration(
                    border: Border.all(color: color, width: size / 12),
                    borderRadius: BorderRadius.circular(size / 6),
                  ),
                  // Sized from the icon, which already follows the text size.
                  child: Text(
                    count > 99 ? '∞' : '$count',
                    textScaler: TextScaler.noScaling,
                    style: TextStyle(color: color, fontSize: size * (count > 9 ? 0.36 : 0.46), fontWeight: FontWeight.w700, height: 1),
                  ),
                ),
              ),
            ),
          );
        }),
      ),
    );
  }
}

/// The open tabs as a row of chips under the app bar, shown while there is
/// more than one: tap to switch, × on the one on screen to close it, + for
/// a new tab. The chips are the direction's own (ChipTheme).
class FileTabStrip extends StatefulWidget {
  const FileTabStrip({
    super.key,
    required this.tabs,
    required this.activeId,
    required this.onSelect,
    required this.onClose,
    required this.onNew,
  });

  final List<FileTabInfo> tabs;
  final int activeId;
  final ValueChanged<int> onSelect;
  final ValueChanged<int> onClose;
  final VoidCallback onNew;

  @override
  State<FileTabStrip> createState() => _FileTabStripState();
}

class _FileTabStripState extends State<FileTabStrip> {
  final _scroll = ScrollController();
  final _keys = <int, GlobalKey>{};

  @override
  void initState() {
    super.initState();
    _revealActive();
  }

  @override
  void didUpdateWidget(FileTabStrip old) {
    super.didUpdateWidget(old);
    if (old.activeId != widget.activeId || old.tabs.length != widget.tabs.length) _revealActive();
  }

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  /// Scrolls the tab on screen into view (a new one opens off the edge).
  void _revealActive() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      final context = _keys[widget.activeId]?.currentContext;
      if (!mounted || context == null) return;
      Scrollable.ensureVisible(context, alignmentPolicy: ScrollPositionAlignmentPolicy.keepVisibleAtEnd);
      Scrollable.ensureVisible(context, alignmentPolicy: ScrollPositionAlignmentPolicy.keepVisibleAtStart);
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    final ids = {for (final t in widget.tabs) t.id};
    _keys.removeWhere((id, _) => !ids.contains(id));
    return SizedBox(
      width: double.infinity,
      child: Material(
        // Opaque, as the bar above it: the list scrolls underneath.
        color: theme.appBarTheme.backgroundColor ?? theme.colorScheme.surface,
        child: SingleChildScrollView(
          controller: _scroll,
          scrollDirection: Axis.horizontal,
          padding: EdgeInsets.fromLTRB(gutter, Space.xs, gutter - Space.sm, Space.xs),
          child: Row(
            children: [
              for (final (i, t) in widget.tabs.indexed) ...[
                if (i > 0) const SizedBox(width: Space.sm),
                _chip(t, i),
              ],
              IconButton(icon: const Icon(Icons.add), tooltip: 'New tab', onPressed: widget.onNew),
            ],
          ),
        ),
      ),
    );
  }

  Widget _chip(FileTabInfo t, int i) {
    final active = t.id == widget.activeId;
    return Semantics(
      key: _keys.putIfAbsent(t.id, GlobalKey.new),
      hint: 'Tab ${i + 1} of ${widget.tabs.length}',
      child: InputChip(
        avatar: Icon(t.icon),
        label: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 160),
          child: Text(t.title, maxLines: 1, overflow: TextOverflow.ellipsis),
        ),
        tooltip: t.subtitle,
        selected: active,
        showCheckmark: false,
        onPressed: () => widget.onSelect(t.id),
        onDeleted: active ? () => widget.onClose(t.id) : null,
        deleteButtonTooltipMessage: 'Close tab',
      ),
    );
  }
}
