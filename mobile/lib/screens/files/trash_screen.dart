// The server's Trash, opened from the Files home or the Locations sheet
// (like the web UI's Files → Trash, ui/src/apps/files/TrashView.vue):
// what was deleted, from where and when, newest first. Restore puts items
// back (asking first when the name has been taken again), Delete forever
// and Empty Trash remove them for good.
import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:intl/intl.dart';

import '../../models/file_entry.dart';
import '../../services/api_client.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import 'file_ops.dart';
import 'file_sheets.dart';
import 'file_widgets.dart';
import 'trash_api.dart';

/// A folder the way the Trash names it: under /DATA without the prefix
/// ("Documents/Work"), elsewhere the full path.
String trashFolderLabel(String folder) {
  if (folder == '/') return 'Root';
  if (folder == '/DATA') return 'DATA';
  if (folder.startsWith('/DATA/')) return folder.substring(6);
  return folder;
}

/// The folder's own name, for short messages ("Restored to Work").
String _shortFolder(String folder) => folder == '/' ? 'Root' : baseName(folder);

class TrashScreen extends StatefulWidget {
  const TrashScreen({super.key, this.onRestored, this.onShowFolder});

  /// Folders something was restored into (so Files can reload them).
  final void Function(Set<String> folders)? onRestored;

  /// "Show" on the restore snackbar: close the Trash and open this folder.
  final void Function(String folder)? onShowFolder;

  /// The last listing, shown at once the next time the Trash opens.
  static TrashListing? _cached;

  @visibleForTesting
  static void clearCache() => _cached = null;

  @override
  State<TrashScreen> createState() => _TrashScreenState();
}

enum _Busy { restore, delete, empty }

class _TrashScreenState extends State<TrashScreen> {
  TrashListing? _listing = TrashScreen._cached;
  DateTime? _fetched;
  bool _loading = false;
  Object? _error;
  bool _stale = false;
  _Busy? _busy;
  final _selected = <String>{};
  Timer? _recount;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _recount?.cancel();
    super.dispose();
  }

  List<TrashItem> get _items => _listing?.items ?? const [];

  /// [quiet]: a background refresh while folders are still being counted.
  Future<void> _load({bool quiet = false}) async {
    _recount?.cancel();
    if (!quiet) {
      setState(() {
        _loading = _listing == null;
        _error = null;
      });
    }
    try {
      final listing = await TrashApi.list();
      if (!mounted) return;
      TrashScreen._cached = listing;
      setState(() {
        _listing = listing;
        _fetched = clock.now();
        _loading = false;
        _stale = false;
        _error = null;
        _selected.retainAll({for (final i in listing.items) i.id});
      });
      if (listing.measuring) _recount = Timer(const Duration(seconds: 3), () => _load(quiet: true));
    } catch (e) {
      if (!mounted || quiet) return;
      setState(() {
        _loading = false;
        if (_listing != null && e is ApiException && e.isUnreachable) {
          _stale = true;
        } else {
          _listing = null;
          _error = e;
        }
      });
    }
  }

  void _snack(String message, {SnackBarAction? action}) {
    final messenger = ScaffoldMessenger.maybeOf(context);
    messenger?.hideCurrentSnackBar();
    messenger?.showSnackBar(SnackBar(
      content: Text(message),
      action: action,
      persist: action == null ? null : false,
      duration: Duration(seconds: action == null ? 4 : 6),
    ));
  }

  String _plain(Object e) => e is ApiException ? e.message : e.toString().replaceFirst('Exception: ', '');

  String _failures(List<TrashFailure> failed) =>
      failed.length == 1 ? '“${failed.first.name.isEmpty ? 'An item' : failed.first.name}”: ${failed.first.error}' : '${failed.length} items failed: ${failed.first.error}';

  void _toggle(TrashItem i) => setState(() {
        if (!_selected.remove(i.id)) _selected.add(i.id);
      });

  List<TrashItem> get _selectedItems => [for (final i in _items) if (_selected.contains(i.id)) i];

  // -------------------------------------------------------------------------
  // Restore

  /// Which of [items] to restore, after asking about names that are taken
  /// again in their folders (or twice among the items). Null for Cancel.
  Future<List<TrashItem>?> _resolveConflicts(List<TrashItem> items) async {
    final folders = {for (final i in items) i.originalFolder};
    final taken = <String, Set<String>>{};
    await Future.wait([
      for (final f in folders)
        () async {
          try {
            final res = await ApiClient.instance.get('/folder', query: {'path': f});
            final data = res['data'];
            final content = data is Map ? (data['content'] as List? ?? const []) : const [];
            taken[f] = {for (final e in content) if (e is Map && e['name'] != null) e['name'].toString()};
          } catch (_) {
            // Gone or unreadable: the server makes the folder again, and
            // it never overwrites, so there is nothing to ask about.
            taken[f] = {};
          }
        }(),
    ]);
    final seen = <String>{};
    final conflicts = <String>[];
    for (final i in items) {
      final clash = taken[i.originalFolder]!.contains(i.name) || !seen.add(i.originalPath);
      if (clash && !conflicts.contains(i.originalPath)) conflicts.add(i.originalPath);
    }
    if (conflicts.isEmpty || !mounted) return items;
    final conflictFolders = {for (final c in conflicts) parentOf(c)};
    final answer = await showConflictSheet(
      context,
      conflicts: conflicts,
      destName: conflictFolders.length == 1 ? _shortFolder(conflictFolders.first) : 'their folders',
      restore: true,
    );
    if (answer == null) return null;
    return [for (final i in items) if (answer[i.originalPath] != ConflictChoice.skip) i];
  }

  Future<void> _restore(List<TrashItem> items) async {
    if (items.isEmpty || _busy != null) return;
    final chosen = await _resolveConflicts(items);
    if (chosen == null || !mounted) return;
    if (chosen.isEmpty) {
      _snack('Nothing restored: every item was skipped.');
      return;
    }
    setState(() => _busy = _Busy.restore);
    try {
      final r = await TrashApi.restore([for (final i in chosen) i.id]);
      if (!mounted) return;
      if (r.restored.isNotEmpty) widget.onRestored?.call(r.folders);
      final single = chosen.length == 1 ? chosen.first.name : null;
      if (r.restored.isNotEmpty && r.failed.isEmpty) {
        final folders = r.folders;
        _snack(
          TrashApi.restoredMessage(r, folderLabel: _shortFolder, singleName: single),
          action: folders.length == 1 && widget.onShowFolder != null
              ? SnackBarAction(
                  label: 'Show',
                  onPressed: () {
                    final folder = folders.first;
                    if (mounted) Navigator.of(context).maybePop();
                    widget.onShowFolder?.call(folder);
                  },
                )
              : null,
        );
      } else if (r.restored.isNotEmpty) {
        _snack('Restored ${r.restored.length} of ${chosen.length}. ${_failures(r.failed)}');
      } else {
        _snack("Couldn't restore ${_failures(r.failed)}");
      }
    } catch (e) {
      if (mounted) _snack("Couldn't restore: ${_plain(e)}");
    } finally {
      if (mounted) {
        setState(() {
          _busy = null;
          _selected.clear();
        });
        await _load(quiet: true);
      }
    }
  }

  // -------------------------------------------------------------------------
  // Delete forever / empty

  Future<void> _deleteForever(List<TrashItem> items) async {
    if (items.isEmpty || _busy != null) return;
    final what = items.length == 1 ? '“${items.first.name}”' : '${items.length} items';
    final ok = await ConfirmDialog.destructive(
      context,
      title: 'Delete $what forever?',
      message: items.length == 1 ? 'It is removed from the server and can’t be restored.' : 'They are removed from the server and can’t be restored.',
      confirmLabel: 'Delete forever',
    );
    if (!ok || !mounted) return;
    setState(() => _busy = _Busy.delete);
    try {
      final failed = await TrashApi.deleteForever([for (final i in items) i.id]);
      if (!mounted) return;
      _snack(failed.isEmpty ? 'Deleted $what forever' : "Couldn't delete ${_failures(failed)}");
    } catch (e) {
      if (mounted) _snack("Couldn't delete $what: ${_plain(e)}");
    } finally {
      if (mounted) {
        setState(() {
          _busy = null;
          _selected.clear();
        });
        await _load(quiet: true);
      }
    }
  }

  Future<void> _empty() async {
    final listing = _listing;
    if (listing == null || listing.isEmpty || _busy != null) return;
    final ok = await ConfirmDialog.destructive(
      context,
      title: 'Empty Trash?',
      message: 'All ${formatCount(listing.items.length, 'item')} (${formatSize(listing.bytes)}) are deleted for good.',
      confirmLabel: 'Empty Trash',
    );
    if (!ok || !mounted) return;
    setState(() => _busy = _Busy.empty);
    try {
      final failed = await TrashApi.empty();
      if (!mounted) return;
      _snack(failed.isEmpty ? 'Trash emptied' : "Some items couldn't be deleted. ${_failures(failed)}");
    } catch (e) {
      if (mounted) _snack("Couldn't empty the Trash: ${_plain(e)}");
    } finally {
      if (mounted) {
        setState(() {
          _busy = null;
          _selected.clear();
        });
        await _load(quiet: true);
      }
    }
  }

  // -------------------------------------------------------------------------
  // Item sheet

  Future<void> _showItem(TrashItem item) async {
    final retention = _listing?.retentionDays ?? 30;
    final action = await showModalBottomSheet<_Busy>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (context) => ListTileTheme.merge(
        contentPadding: const EdgeInsets.symmetric(horizontal: Space.xl),
        child: SafeArea(top: false, child: _TrashItemSheet(item: item, retentionDays: retention)),
      ),
    );
    if (!mounted) return;
    switch (action) {
      case _Busy.restore:
        await _restore([item]);
      case _Busy.delete:
        await _deleteForever([item]);
      case _Busy.empty || null:
        break;
    }
  }

  // -------------------------------------------------------------------------
  // Building

  PreferredSizeWidget? _selectionBar() {
    if (_selected.isEmpty) return null;
    final items = _selectedItems;
    final idle = _busy == null;
    return AppBar(
      leading: IconButton(icon: const Icon(Icons.close), tooltip: 'Clear selection', onPressed: () => setState(_selected.clear)),
      title: Semantics(
        liveRegion: true,
        label: '${_selected.length} selected',
        excludeSemantics: true,
        child: Text('${_selected.length}'),
      ),
      backgroundColor: Theme.of(context).colorScheme.surfaceContainer,
      actions: [
        IconButton(icon: const Icon(Icons.restore), tooltip: 'Restore', onPressed: idle ? () => _restore(items) : null),
        IconButton(icon: const Icon(Icons.delete_forever_outlined), tooltip: 'Delete forever', onPressed: idle ? () => _deleteForever(items) : null),
        IconButton(
          icon: const Icon(Icons.select_all),
          tooltip: 'Select all',
          onPressed: _selected.length == _items.length ? null : () => setState(() => _selected.addAll(_items.map((i) => i.id))),
        ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final listing = _listing;
    final banner = Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (_stale) OfflineBanner(lastUpdated: _fetched, onRetry: _load),
        if (_busy != null) const LinearProgressIndicator(minHeight: 2),
      ],
    );
    return AppScaffold.slivers(
      title: 'Trash',
      appBar: _selectionBar(),
      banner: banner,
      onRefresh: _load,
      maxContentWidth: Space.readingMaxWidth,
      actions: [
        if (listing != null && !listing.isEmpty)
          PopupMenuButton<String>(
            tooltip: 'More options',
            onSelected: (v) {
              switch (v) {
                case 'select':
                  setState(() => _selected.addAll(_items.map((i) => i.id)));
                case 'empty':
                  _empty();
              }
            },
            itemBuilder: (context) => [
              const PopupMenuItem(value: 'select', child: Text('Select all')),
              PopupMenuItem(value: 'empty', enabled: _busy == null, child: const Text('Empty Trash')),
            ],
          ),
      ],
      slivers: _slivers(),
    );
  }

  List<Widget> _slivers() {
    final listing = _listing;
    if (_loading && listing == null) return [const FolderSkeleton(rows: 6)];
    final error = _error;
    if (error != null && listing == null) {
      if (error is ApiException && error.isUnreachable) return [ErrorState.offline(onRetry: _load, sliver: true, details: error.details)];
      return [
        ErrorState(
          title: "Couldn't load the Trash",
          message: _plain(error),
          onRetry: _load,
          details: error is ApiException ? error.details : error.toString(),
          sliver: true,
        ),
      ];
    }
    if (listing == null || listing.isEmpty) {
      return [
        EmptyState(
          icon: Icons.delete_outline,
          title: 'Trash is empty',
          message: 'Files and folders you delete on the server stay here for ${listing?.retentionDays ?? 30} days, so you can restore them.',
          sliver: true,
        ),
      ];
    }
    final now = clock.now();
    final groups = <String, List<TrashItem>>{};
    for (final i in listing.items) {
      groups.putIfAbsent(_groupOf(i.deletedAt, now), () => []).add(i);
    }
    final selecting = _selected.isNotEmpty;
    return [
      SliverToBoxAdapter(child: _SummaryPanel(listing: listing, busy: _busy, onEmpty: _empty)),
      for (final MapEntry(key: title, value: items) in groups.entries) ...[
        SliverToBoxAdapter(child: SectionHeader(title: title)),
        SliverList.builder(
          itemCount: items.length,
          itemBuilder: (context, n) {
            final i = items[n];
            return TrashRow(
              key: ValueKey(i.id),
              item: i,
              selected: _selected.contains(i.id),
              selecting: selecting,
              enabled: _busy == null,
              onTap: () => selecting ? _toggle(i) : _showItem(i),
              onLongPress: () {
                HapticFeedback.selectionClick();
                _toggle(i);
              },
            );
          },
        ),
      ],
      const SliverToBoxAdapter(child: SizedBox(height: Space.xl)),
    ];
  }

  static String _groupOf(DateTime? at, DateTime now) {
    if (at == null) return 'Earlier';
    final local = at.toLocal();
    final today = DateTime(now.year, now.month, now.day);
    final days = today.difference(DateTime(local.year, local.month, local.day)).inDays;
    if (days <= 0) return 'Today';
    if (days == 1) return 'Yesterday';
    if (days < 7) return 'This week';
    return 'Earlier';
  }
}

/// The page's one expressive moment: how much the Trash holds, how long
/// the server keeps it, and the way to clear it.
class _SummaryPanel extends StatelessWidget {
  const _SummaryPanel({required this.listing, required this.busy, required this.onEmpty});

  final TrashListing listing;
  final _Busy? busy;
  final VoidCallback onEmpty;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    final size = formatSize(listing.bytes);
    final i = size.lastIndexOf(' ');
    final value = i > 0 ? size.substring(0, i) : size;
    final unit = i > 0 ? size.substring(i + 1) : '';
    final count = formatCount(listing.items.length, 'item');
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.sm),
      child: Card.filled(
        color: DesignTokens.of(context).cardColor,
        shape: DesignTokens.of(context).cardShape(),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(Space.lg, Space.lg, Space.lg, Space.sm),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Semantics(
                container: true,
                label: 'Trash: $count, $size. Deleted for good after ${listing.retentionDays} days.',
                excludeSemantics: true,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('In Trash', style: theme.textTheme.labelLarge?.copyWith(color: scheme.onSurfaceVariant)),
                    const SizedBox(height: Space.xs),
                    Text.rich(TextSpan(children: [
                      TextSpan(text: value, style: theme.textTheme.headlineMedium?.emphasized.tabular),
                      TextSpan(text: ' $unit · $count', style: theme.textTheme.titleMedium?.copyWith(color: scheme.onSurfaceVariant)),
                    ])),
                    const SizedBox(height: Space.xs),
                    Text(
                      'Items are deleted for good after ${listing.retentionDays} days.',
                      style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: Space.sm),
              Align(
                alignment: AlignmentDirectional.centerEnd,
                child: TextButton.icon(
                  onPressed: busy == null ? onEmpty : null,
                  style: TextButton.styleFrom(foregroundColor: scheme.error),
                  icon: const Icon(Icons.delete_sweep_outlined),
                  label: const Text('Empty Trash'),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// One thing in the Trash: its name, the folder it came from and when it
/// was deleted, and its size.
class TrashRow extends StatelessWidget {
  const TrashRow({
    super.key,
    required this.item,
    required this.selected,
    required this.selecting,
    required this.onTap,
    required this.onLongPress,
    this.enabled = true,
  });

  final TrashItem item;
  final bool selected;
  final bool selecting;
  final bool enabled;
  final VoidCallback onTap;
  final VoidCallback onLongPress;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final entry = item.entry;
    final big = MediaQuery.textScalerOf(context).scale(1) > 1.3;
    final at = item.deletedAt;
    final size = item.measuring ? 'Counting…' : formatSize(item.size);
    return Semantics(
      selected: selecting ? selected : null,
      child: ListTile(
        enabled: enabled,
        selected: selected,
        selectedTileColor: scheme.secondaryContainer,
        selectedColor: scheme.onSecondaryContainer,
        leading: ExcludeSemantics(
          child: SizedBox.square(
            dimension: 40,
            child: selected
                ? Icon(Icons.check_circle, color: scheme.primary, size: 28)
                : Icon(fileKindIcon(entry.kind), color: item.isDir ? scheme.primary : scheme.onSurfaceVariant),
          ),
        ),
        title: Text(item.name, maxLines: big ? 2 : 1, overflow: TextOverflow.ellipsis),
        // When first, so a long folder path is what gets cut off; at large
        // text the size moves down here too.
        subtitle: Text(
          _capitalized([
            if (at != null) formatRelative(at),
            'from ${trashFolderLabel(item.originalFolder)}',
            if (big) size,
          ].join(' · ')),
          maxLines: big ? 2 : 1,
          overflow: TextOverflow.ellipsis,
        ),
        trailing: big
            ? null
            : Text(
                item.isDir && !item.measuring ? '${formatCount(item.items, 'item')}\n$size' : size,
                textAlign: TextAlign.end,
                style: theme.textTheme.bodySmall?.tabular.copyWith(color: selected ? null : scheme.onSurfaceVariant),
              ),
        onTap: onTap,
        onLongPress: onLongPress,
      ),
    );
  }
}

String _capitalized(String s) => s.isEmpty ? s : '${s[0].toUpperCase()}${s.substring(1)}';

/// What one item is and what can be done with it.
class _TrashItemSheet extends StatelessWidget {
  const _TrashItemSheet({required this.item, required this.retentionDays});

  final TrashItem item;
  final int retentionDays;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final at = item.deletedAt;
    final left = item.daysLeft(retentionDays, clock.now());
    final facts = <(String, String)>[
      ('From', item.originalFolder),
      if (at != null) ('Deleted', formatExact(at)),
      ('Size', item.measuring ? 'Counting…' : [if (item.isDir) formatCount(item.items, 'item'), formatSize(item.size)].join(' · ')),
      if (left != null)
        ('Kept until', '${DateFormat.yMMMd().format(item.expiresAt(retentionDays)!.toLocal())} · ${left == 0 ? 'deleted for good today' : '${formatCount(left, 'day')} left'}'),
    ];
    return SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(Space.xl, 0, Space.xl, Space.sm),
            child: Row(
              children: [
                Icon(fileKindIcon(item.entry.kind), color: item.isDir ? scheme.primary : scheme.onSurfaceVariant),
                const SizedBox(width: Space.lg),
                Expanded(
                  child: Semantics(
                    header: true,
                    child: Text(item.name, style: theme.textTheme.titleLarge, maxLines: 2, overflow: TextOverflow.ellipsis),
                  ),
                ),
              ],
            ),
          ),
          for (final (label, value) in facts)
            Padding(
              padding: const EdgeInsets.fromLTRB(Space.xl, Space.xs, Space.xl, Space.xs),
              child: MergeSemantics(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(label, style: theme.textTheme.labelMedium?.copyWith(color: scheme.onSurfaceVariant)),
                    Text(value, style: theme.textTheme.bodyMedium),
                  ],
                ),
              ),
            ),
          const SizedBox(height: Space.sm),
          ListTile(
            leading: const Icon(Icons.restore),
            title: const Text('Restore'),
            subtitle: Text('Back to ${trashFolderLabel(item.originalFolder)}', maxLines: 1, overflow: TextOverflow.ellipsis),
            onTap: () => Navigator.of(context).pop(_Busy.restore),
          ),
          ListTile(
            leading: Icon(Icons.delete_forever_outlined, color: scheme.error),
            title: Text('Delete forever', style: TextStyle(color: scheme.error)),
            onTap: () => Navigator.of(context).pop(_Busy.delete),
          ),
          const SizedBox(height: Space.sm),
        ],
      ),
    );
  }
}
