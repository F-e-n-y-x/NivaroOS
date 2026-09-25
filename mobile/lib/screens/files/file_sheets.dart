// Bottom sheets and dialogs of the Files screen: conflicts, item actions,
// sort, names, info, locations and tabs.
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../models/file_entry.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import 'file_ops.dart';
import 'file_widgets.dart';

/// A sheet's title row, in the sheet's own gutter.
class _SheetTitle extends StatelessWidget {
  const _SheetTitle(this.title, {this.subtitle});
  final String title;
  final String? subtitle;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(Space.xl, 0, Space.xl, Space.sm),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Semantics(header: true, child: Text(title, style: theme.textTheme.titleLarge)),
          if (subtitle != null) ...[
            const SizedBox(height: Space.xs),
            Text(subtitle!, style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
          ],
        ],
      ),
    );
  }
}

Future<T?> _showSheet<T>(BuildContext context, WidgetBuilder builder) => showModalBottomSheet<T>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (context) => ListTileTheme.merge(
        contentPadding: const EdgeInsets.symmetric(horizontal: Space.xl),
        child: SafeArea(top: false, child: builder(context)),
      ),
    );

// ---------------------------------------------------------------------------
// Conflicts

/// Asks what to do about [conflicts] (names already in [destName]) before a
/// copy or move: keep both, replace or skip, for all of them at once
/// ("Apply to all", on by default) or one at a time. Returns the answer per
/// source path, or null for Cancel.
///
/// A restore from the Trash passes [restore]: the server never overwrites
/// there, so the choices are Keep both (the restored one gets
/// "(restored)" in its name) and Skip.
Future<Map<String, ConflictChoice>?> showConflictSheet(
  BuildContext context, {
  required List<String> conflicts,
  required String destName,
  TransferKind kind = TransferKind.copy,
  bool restore = false,
}) {
  return _showSheet<Map<String, ConflictChoice>>(
    context,
    (context) => ConflictSheet(conflicts: conflicts, destName: destName, kind: kind, restore: restore),
  );
}

class ConflictSheet extends StatefulWidget {
  const ConflictSheet({super.key, required this.conflicts, required this.destName, this.kind = TransferKind.copy, this.restore = false});

  final List<String> conflicts;
  final String destName;
  final TransferKind kind;

  /// Restoring from the Trash: no Replace, and the kept copy is renamed
  /// the server's way.
  final bool restore;

  @override
  State<ConflictSheet> createState() => _ConflictSheetState();
}

class _ConflictSheetState extends State<ConflictSheet> {
  bool _applyToAll = true;
  int _index = 0;
  final _answers = <String, ConflictChoice>{};

  void _choose(ConflictChoice choice) {
    HapticFeedback.selectionClick();
    final remaining = widget.conflicts.sublist(_index);
    if (_applyToAll || widget.conflicts.length == 1) {
      for (final c in remaining) {
        _answers[c] = choice;
      }
      Navigator.of(context).pop(_answers);
      return;
    }
    _answers[widget.conflicts[_index]] = choice;
    if (_index == widget.conflicts.length - 1) {
      Navigator.of(context).pop(_answers);
    } else {
      setState(() => _index++);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final many = widget.conflicts.length > 1;
    final one = !many || !_applyToAll;
    final name = baseName(widget.conflicts[_index]);
    final title = one
        ? '“$name” is already in ${widget.destName}'
        : '${widget.conflicts.length} items are already in ${widget.destName}';
    final shown = widget.conflicts.take(4).map(baseName).toList();
    final more = widget.conflicts.length - shown.length;
    final verb = widget.restore ? 'restored' : (widget.kind == TransferKind.move ? 'moved' : 'copied');
    final keepBoth = widget.restore
        ? (many && _applyToAll ? 'The restored ones get “(restored)” added, like “photo (restored).jpg”' : 'The restored one becomes “${restoredName(name)}”')
        : (many && _applyToAll ? 'The new ones get a number, like “photo (2).jpg”' : 'The new one becomes “${uniqueName(name, {name})}”');

    return SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _SheetTitle(
            title,
            subtitle: many && !_applyToAll
                ? 'Item ${_index + 1} of ${widget.conflicts.length}. Everything else is $verb as usual.'
                : 'Everything else is $verb as usual.',
          ),
          if (many && _applyToAll)
            Padding(
              padding: const EdgeInsets.fromLTRB(Space.xl, 0, Space.xl, Space.sm),
              child: Text(
                [...shown, if (more > 0) 'and $more more'].join('\n'),
                style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
              ),
            ),
          ListTile(
            leading: const Icon(Icons.file_copy_outlined),
            title: const Text('Keep both'),
            subtitle: Text(keepBoth),
            onTap: () => _choose(ConflictChoice.keepBoth),
          ),
          if (!widget.restore)
            ListTile(
              leading: Icon(Icons.swap_horiz, color: scheme.error),
              title: Text('Replace', style: TextStyle(color: scheme.error)),
              subtitle: const Text('The ones already there are overwritten'),
              onTap: () => _choose(ConflictChoice.replace),
            ),
          ListTile(
            leading: const Icon(Icons.skip_next_outlined),
            title: const Text('Skip'),
            subtitle: Text(many && _applyToAll ? 'Leave these out' : 'Leave this one out'),
            onTap: () => _choose(ConflictChoice.skip),
          ),
          if (many && _index == 0)
            CheckboxListTile(
              value: _applyToAll,
              onChanged: (v) => setState(() => _applyToAll = v ?? true),
              title: Text('Same answer for all ${widget.conflicts.length}'),
              controlAffinity: ListTileControlAffinity.leading,
              // The 18dp box sits in a 40dp slot: pull it 8dp out so it
              // centres on the 24dp icons above, and close the gap so the
              // label starts on their text edge.
              contentPadding: const EdgeInsetsDirectional.only(start: Space.xl - Space.sm, end: Space.xl),
              horizontalTitleGap: Space.sm,
            ),
          Padding(
            padding: const EdgeInsets.fromLTRB(Space.lg, Space.sm, Space.lg, Space.sm),
            child: Align(
              alignment: AlignmentDirectional.centerEnd,
              child: TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
            ),
          ),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// Item actions

/// What the actions sheet can do; the screen decides which apply.
enum EntryAction { open, openInNewTab, select, copy, move, rename, compress, extract, favorite, unfavorite, info, openWith, delete }

/// The menu for one item: its name and size at the top, then actions.
Future<EntryAction?> showEntryActions(
  BuildContext context, {
  required FileEntry entry,
  required bool isLocal,
  required Set<EntryAction> actions,
  bool deleteIsPermanent = false,
}) {
  final scheme = Theme.of(context).colorScheme;
  Widget tile(EntryAction a, IconData icon, String label, {bool danger = false}) => ListTile(
        leading: Icon(icon, color: danger ? scheme.error : null),
        title: Text(label, style: danger ? TextStyle(color: scheme.error) : null),
        onTap: () => Navigator.of(context).pop(a),
      );
  return _showSheet<EntryAction>(
    context,
    (context) => SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          ListTile(
            // A 24dp icon, so the name lines up with the actions below.
            leading: ExcludeSemantics(child: Icon(fileKindIcon(entry.kind), color: entry.isDir ? scheme.primary : null)),
            title: Text(entry.name, maxLines: 2, overflow: TextOverflow.ellipsis, style: Theme.of(context).textTheme.titleMedium),
            subtitle: Text(entrySubtitle(entry)),
          ),
          const Divider(indent: Space.xl, endIndent: Space.xl),
          if (actions.contains(EntryAction.select)) tile(EntryAction.select, Icons.check_circle_outline, 'Select'),
          if (actions.contains(EntryAction.openInNewTab)) tile(EntryAction.openInNewTab, Icons.tab_outlined, 'Open in new tab'),
          if (actions.contains(EntryAction.openWith)) tile(EntryAction.openWith, Icons.open_in_new_outlined, 'Open with another app'),
          if (actions.contains(EntryAction.copy)) tile(EntryAction.copy, Icons.content_copy_outlined, 'Copy'),
          if (actions.contains(EntryAction.move)) tile(EntryAction.move, Icons.drive_file_move_outlined, 'Move'),
          if (actions.contains(EntryAction.rename)) tile(EntryAction.rename, Icons.drive_file_rename_outline, 'Rename'),
          if (actions.contains(EntryAction.compress)) tile(EntryAction.compress, Icons.archive_outlined, 'Compress to ZIP'),
          if (actions.contains(EntryAction.extract)) tile(EntryAction.extract, Icons.unarchive_outlined, 'Extract here'),
          if (actions.contains(EntryAction.favorite)) tile(EntryAction.favorite, Icons.star_outline, 'Add to favorites'),
          if (actions.contains(EntryAction.unfavorite)) tile(EntryAction.unfavorite, Icons.star_outlined, 'Remove from favorites'),
          if (actions.contains(EntryAction.info)) tile(EntryAction.info, Icons.info_outline, 'Details'),
          if (actions.contains(EntryAction.delete))
            tile(EntryAction.delete, Icons.delete_outline, deleteIsPermanent ? 'Delete' : 'Move to Trash', danger: true),
          const SizedBox(height: Space.sm),
        ],
      ),
    ),
  );
}

// ---------------------------------------------------------------------------
// Sort

/// Sort by, and the direction; tapping the current order flips it.
Future<(FileSort, bool)?> showSortSheet(BuildContext context, {required FileSort sort, required bool ascending}) {
  return _showSheet<(FileSort, bool)>(
    context,
    (context) {
      String dirLabel(FileSort s, bool asc) => switch (s) {
            FileSort.name || FileSort.type => asc ? 'A to Z' : 'Z to A',
            FileSort.modified => asc ? 'Oldest first' : 'Newest first',
            FileSort.size => asc ? 'Smallest first' : 'Largest first',
          };
      return SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const _SheetTitle('Sort by'),
            RadioGroup<FileSort>(
              groupValue: sort,
              onChanged: (s) {
                if (s == null) return;
                Navigator.of(context).pop((s, s == sort ? !ascending : defaultAscending(s)));
              },
              child: Column(
                children: [
                  for (final s in FileSort.values)
                    RadioListTile<FileSort>(
                      value: s,
                      title: Text(s.label),
                      subtitle: Text(dirLabel(s, s == sort ? ascending : defaultAscending(s))),
                      secondary: s == sort
                          ? IconButton(
                              icon: Icon(ascending ? Icons.arrow_upward : Icons.arrow_downward),
                              tooltip: 'Reverse the order',
                              onPressed: () => Navigator.of(context).pop((s, !ascending)),
                            )
                          : null,
                    ),
                ],
              ),
            ),
            const SizedBox(height: Space.sm),
          ],
        ),
      );
    },
  );
}

// ---------------------------------------------------------------------------
// Names

/// A dialog asking for a name (new folder, rename). The text before the
/// extension starts selected when renaming a file.
Future<String?> showNameDialog(
  BuildContext context, {
  required String title,
  required String confirmLabel,
  String initial = '',
  Set<String> taken = const {},
  bool selectStem = false,
}) {
  return showDialog<String>(
    context: context,
    builder: (context) => _NameDialog(
      title: title,
      confirmLabel: confirmLabel,
      initial: initial,
      taken: taken,
      selectStem: selectStem,
    ),
  );
}

class _NameDialog extends StatefulWidget {
  const _NameDialog({
    required this.title,
    required this.confirmLabel,
    required this.initial,
    required this.taken,
    required this.selectStem,
  });

  final String title;
  final String confirmLabel;
  final String initial;
  final Set<String> taken;
  final bool selectStem;

  @override
  State<_NameDialog> createState() => _NameDialogState();
}

class _NameDialogState extends State<_NameDialog> {
  late final _controller = TextEditingController(text: widget.initial);
  String? _error;

  @override
  void initState() {
    super.initState();
    final dot = widget.initial.lastIndexOf('.');
    final end = widget.selectStem && dot > 0 ? dot : widget.initial.length;
    _controller.selection = TextSelection(baseOffset: 0, extentOffset: end);
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _submit() {
    final name = _controller.text.trim();
    final error = validateName(name, taken: widget.taken, current: widget.initial.isEmpty ? null : widget.initial);
    if (error != null) {
      setState(() => _error = error);
      return;
    }
    Navigator.of(context).pop(name);
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(widget.title),
      content: TextField(
        controller: _controller,
        autofocus: true,
        textInputAction: TextInputAction.done,
        decoration: InputDecoration(labelText: 'Name', errorText: _error),
        onChanged: (_) {
          if (_error != null) setState(() => _error = null);
        },
        onSubmitted: (_) => _submit(),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        TextButton(onPressed: _submit, child: Text(widget.confirmLabel)),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// Info

/// Name, kind, size, dates and where it is. The path can be copied.
Future<void> showInfoSheet(BuildContext context, {required FileEntry entry, required bool isLocal, String? locationLabel}) {
  return _showSheet<void>(context, (context) {
    final modified = entry.modified;
    return SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _SheetTitle(entry.name),
          ListTile(leading: Icon(fileKindIcon(entry.kind)), title: const Text('Type'), subtitle: Text(entry.categoryLabel)),
          if (!entry.isDir)
            ListTile(
              leading: const Icon(Icons.data_usage_outlined),
              title: const Text('Size'),
              subtitle: Text('${formatSize(entry.size)} (${_grouped(entry.size)} bytes)'),
            ),
          if (modified != null && modified.year > 1971)
            ListTile(
              leading: const Icon(Icons.schedule_outlined),
              title: const Text('Modified'),
              subtitle: Text(formatExact(modified)),
            ),
          ListTile(
            leading: Icon(isLocal ? Icons.smartphone_outlined : Icons.dns_outlined),
            title: Text(isLocal ? 'On this phone' : (locationLabel == null ? 'On the server' : 'On the server · $locationLabel')),
            subtitle: Text(entry.path),
            trailing: IconButton(
              icon: const Icon(Icons.content_copy_outlined),
              tooltip: 'Copy path',
              onPressed: () async {
                await Clipboard.setData(ClipboardData(text: entry.path));
                if (context.mounted) {
                  ScaffoldMessenger.maybeOf(context)?.showSnackBar(const SnackBar(content: Text('Path copied')));
                }
              },
            ),
          ),
          const SizedBox(height: Space.sm),
        ],
      ),
    );
  });
}

String _grouped(int n) {
  final s = n.toString();
  final b = StringBuffer();
  for (var i = 0; i < s.length; i++) {
    if (i > 0 && (s.length - i) % 3 == 0) b.write(',');
    b.write(s[i]);
  }
  return b.toString();
}

// ---------------------------------------------------------------------------
// Add

enum AddAction { upload, newFolder }

/// The FAB's menu: upload from this phone (server folders only) and new
/// folder.
Future<AddAction?> showAddSheet(BuildContext context, {required bool canUpload}) {
  return _showSheet<AddAction>(
    context,
    (context) => Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (canUpload)
          ListTile(
            leading: const Icon(Icons.upload_file_outlined),
            title: const Text('Upload from this phone'),
            subtitle: const Text('Pick files to send to this folder'),
            onTap: () => Navigator.of(context).pop(AddAction.upload),
          ),
        ListTile(
          leading: const Icon(Icons.create_new_folder_outlined),
          title: const Text('New folder'),
          onTap: () => Navigator.of(context).pop(AddAction.newFolder),
        ),
        const SizedBox(height: Space.sm),
      ],
    ),
  );
}

// ---------------------------------------------------------------------------
// Locations

/// Every place to browse, grouped like the Files home, with the current
/// one marked. Returns the location picked.
Future<FileLocation?> showLocationsSheet(
  BuildContext context, {
  required List<FileLocation> locations,
  FileLocation? current,
}) {
  return _showSheet<FileLocation>(context, (context) {
    final groups = <String, List<FileLocation>>{
      // The Trash spans the server's drives, so it closes their group.
      'Server storage': [
        for (final l in locations) if (l.kind == LocationKind.storage || l.kind == LocationKind.usb) l,
        for (final l in locations) if (l.kind == LocationKind.trash) l,
      ],
      'Phones': [for (final l in locations) if (l.kind == LocationKind.thisPhone || l.kind == LocationKind.phone) l],
      'Cloud': [for (final l in locations) if (l.kind == LocationKind.cloud) l],
      'Favorites': [for (final l in locations) if (l.kind == LocationKind.favorite) l],
    };
    final scheme = Theme.of(context).colorScheme;
    return DraggableScrollableSheet(
      expand: false,
      initialChildSize: 0.6,
      minChildSize: 0.3,
      maxChildSize: 0.95,
      builder: (context, scroll) => ListView(
        controller: scroll,
        children: [
          const _SheetTitle('Locations'),
          for (final MapEntry(key: title, value: items) in groups.entries)
            if (items.isNotEmpty) ...[
              Padding(
                padding: const EdgeInsets.fromLTRB(Space.xl, Space.lg, Space.xl, Space.sm),
                child: Semantics(
                  header: true,
                  child: Text(title, style: Theme.of(context).textTheme.titleSmall?.copyWith(color: scheme.primary)),
                ),
              ),
              for (final l in items)
                ListTile(
                  leading: Icon(locationIcon(l.kind)),
                  title: Text(l.label, maxLines: 1, overflow: TextOverflow.ellipsis),
                  subtitle: Text(locationSubtitle(l), maxLines: 1, overflow: TextOverflow.ellipsis),
                  enabled: l.online,
                  selected: current != null && current.path == l.path && current.kind == l.kind,
                  onTap: () => Navigator.of(context).pop(l),
                ),
            ],
          const SizedBox(height: Space.lg),
        ],
      ),
    );
  });
}

/// The second line of a location row.
String locationSubtitle(FileLocation l) {
  final total = l.totalBytes;
  final free = l.availableBytes;
  final space = free != null && total != null && total > 0 ? '${formatSize(free)} free of ${formatSize(total)}' : null;
  return [
    if (l.detail != null) l.detail!,
    if (!l.online) 'Offline',
    ?space,
    if (l.detail == null && space == null && l.online) l.path,
  ].join(' · ');
}

// ---------------------------------------------------------------------------
// Tabs

/// What was picked in the tabs sheet.
enum TabsChoice { select, newTab, newTabHere }

/// The open tabs, like a browser's tab switcher: tap one to show it, ×
/// to close it (right away, the sheet stays open), and New tab (at the
/// locations page) or, with [hereTitle], a new tab at the place on screen.
/// Returns the choice and, for select, the tab's id.
Future<(TabsChoice, int?)?> showTabsSheet(
  BuildContext context, {
  required List<FileTabInfo> tabs,
  required int activeId,
  required ValueChanged<int> onClose,
  String? hereTitle,
}) {
  return _showSheet<(TabsChoice, int?)>(
    context,
    (context) => _TabsSheet(tabs: tabs, activeId: activeId, onClose: onClose, hereTitle: hereTitle),
  );
}

class _TabsSheet extends StatefulWidget {
  const _TabsSheet({required this.tabs, required this.activeId, required this.onClose, this.hereTitle});
  final List<FileTabInfo> tabs;
  final int activeId;
  final ValueChanged<int> onClose;
  final String? hereTitle;

  @override
  State<_TabsSheet> createState() => _TabsSheetState();
}

class _TabsSheetState extends State<_TabsSheet> {
  late final _tabs = [...widget.tabs];

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final here = widget.hereTitle;
    return SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Align(
            alignment: AlignmentDirectional.centerStart,
            child: _SheetTitle('Tabs', subtitle: '${formatCount(_tabs.length, 'tab')} open'),
          ),
          for (final t in _tabs)
            ListTile(
              leading: Icon(t.icon),
              title: Text(t.title, maxLines: 1, overflow: TextOverflow.ellipsis),
              subtitle: t.subtitle == null ? null : Text(t.subtitle!, maxLines: 1, overflow: TextOverflow.ellipsis),
              selected: t.id == widget.activeId,
              onTap: () => Navigator.of(context).pop((TabsChoice.select, t.id)),
              trailing: _tabs.length > 1
                  ? IconButton(
                      icon: const Icon(Icons.close),
                      tooltip: 'Close “${t.title}”',
                      onPressed: () {
                        widget.onClose(t.id);
                        setState(() => _tabs.remove(t));
                      },
                    )
                  : null,
            ),
          const Divider(indent: Space.xl, endIndent: Space.xl),
          ListTile(
            leading: Icon(Icons.add, color: scheme.primary),
            title: const Text('New tab'),
            subtitle: const Text('Starts at the locations'),
            onTap: () => Navigator.of(context).pop((TabsChoice.newTab, null)),
          ),
          if (here != null)
            ListTile(
              leading: Icon(Icons.tab_outlined, color: scheme.primary),
              title: const Text('New tab here'),
              subtitle: Text('Opens “$here” in another tab', maxLines: 1, overflow: TextOverflow.ellipsis),
              onTap: () => Navigator.of(context).pop((TabsChoice.newTabHere, null)),
            ),
          const SizedBox(height: Space.sm),
        ],
      ),
    );
  }
}
