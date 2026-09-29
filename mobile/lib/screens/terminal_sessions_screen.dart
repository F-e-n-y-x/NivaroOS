import 'dart:async';

import 'package:flutter/material.dart';

import '../services/api_client.dart';
import '../services/terminal_sessions.dart';
import '../ui/ui.dart';
import 'terminal_screen.dart';

/// Every terminal running on the server - shells on the server itself and
/// in app containers - to go back to, rename or end, and a way to start a
/// new one. Terminals keep running when their screen closes, so this is
/// where they are found again (More > Terminal).
class TerminalSessionsScreen extends StatefulWidget {
  const TerminalSessionsScreen({super.key, this.connector});

  /// Passed on to the terminals it opens; tests pass a fake.
  final TerminalConnector? connector;

  @override
  State<TerminalSessionsScreen> createState() => _TerminalSessionsScreenState();
}

class _TerminalSessionsScreenState extends State<TerminalSessionsScreen> {
  TerminalSessionList? _list;
  String? _error;
  bool _loading = true;
  Timer? _refresh;

  TerminalSessionsApi get _api => TerminalSessionsApi.instance;

  @override
  void initState() {
    super.initState();
    _load();
    // What each shell is doing (vim, a build) and who is attached change
    // on their own.
    _refresh = Timer.periodic(const Duration(seconds: 15), (_) => _load(quiet: true));
  }

  @override
  void dispose() {
    _refresh?.cancel();
    super.dispose();
  }

  Future<void> _load({bool quiet = false}) async {
    if (!quiet && _list == null) setState(() => _loading = true);
    try {
      final list = await _api.list();
      if (!mounted) return;
      setState(() {
        _list = list;
        _error = list.failed ? list.errors.values.first : null;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e is ApiException ? e.message : "Couldn't load the terminals";
        _loading = false;
      });
    }
  }

  Future<void> _open(TerminalSession s) async {
    await Navigator.of(context).push(MaterialPageRoute(builder: (_) => TerminalScreen(session: s, connector: widget.connector)));
    if (mounted) unawaited(_load(quiet: true));
  }

  Future<void> _new() async {
    await Navigator.of(context).push(MaterialPageRoute(builder: (_) => TerminalScreen(connector: widget.connector)));
    if (mounted) unawaited(_load(quiet: true));
  }

  Future<void> _rename(TerminalSession s) async {
    final messenger = ScaffoldMessenger.of(context);
    final name = await showTerminalRenameDialog(context, s);
    if (name == null) return;
    try {
      await _api.rename(s, name);
      await _load(quiet: true);
    } catch (e) {
      messenger.showSnackBar(SnackBar(content: Text("Couldn't rename it. ${e is ApiException ? e.message : ''}".trim())));
    }
  }

  Future<void> _end(TerminalSession s) async {
    final messenger = ScaffoldMessenger.of(context);
    if (s.running) {
      final ok = await ConfirmDialog.destructive(
        context,
        title: 'End “${s.title}”?',
        message: 'The shell stops, with anything still running in it.',
        confirmLabel: 'End',
        permanent: false,
      );
      if (!ok) return;
    }
    // Gone from the list at once; put back if the server says no.
    final before = _list;
    if (before != null) {
      setState(() => _list = TerminalSessionList(
            sessions: [for (final o in before.sessions) if (o != s) o],
            unsupported: before.unsupported,
            errors: before.errors,
            maxSessions: before.maxSessions,
            detachedTimeout: before.detachedTimeout,
          ));
    }
    try {
      await _api.end(s);
      if (s.running) messenger.showSnackBar(SnackBar(content: Text('Ended “${s.title}”')));
    } catch (e) {
      if (mounted) setState(() => _list = before);
      messenger.showSnackBar(SnackBar(content: Text("Couldn't end it. ${e is ApiException ? e.message : ''}".trim())));
    }
    unawaited(_load(quiet: true));
  }

  @override
  Widget build(BuildContext context) {
    final list = _list;
    final hostOld = list?.unsupported.contains(TerminalFamily.host) ?? false;
    final running = list?.running ?? const [];
    final host = [for (final s in running) if (!s.isContainer) s];
    final apps = [for (final s in running) if (s.isContainer) s];
    final ended = list?.ended ?? const [];
    final atLimit = list?.maxSessions != null && host.length >= list!.maxSessions!;

    return AppScaffold.slivers(
      title: 'Terminal',
      onRefresh: _load,
      // Not over the empty state, which has the same button.
      floatingActionButton: (_loading && list == null) || (list != null && list.sessions.isEmpty && !hostOld && list.errors.isEmpty)
          ? null
          : FloatingActionButton.extended(
              onPressed: atLimit ? null : _new,
              icon: const Icon(Icons.add),
              label: const Text('New terminal'),
            ),
      slivers: [
        if (_loading && list == null)
          const SliverLoadingList(rows: 3)
        else if (list == null || (_error != null && list.sessions.isEmpty))
          ErrorState(
            sliver: true,
            title: "Couldn't load the terminals",
            message: _error ?? "The server didn't answer.",
            onRetry: _load,
          )
        else if (list.sessions.isEmpty && !hostOld && list.errors.isEmpty)
          EmptyState(
            sliver: true,
            icon: Icons.terminal_outlined,
            title: 'No terminals running',
            message: 'A terminal keeps running on the server when you leave it, so you can come back to it here - from this phone or the web.',
            actionLabel: 'New terminal',
            onAction: _new,
          )
        else
          SliverList.list(children: [
            if (hostOld)
              const Padding(
                padding: EdgeInsets.fromLTRB(Space.lg, Space.sm, Space.lg, 0),
                child: Notice(
                  status: Status.info,
                  message: 'This server ends a terminal when you close it. Update NivaroOS to keep terminals running and come back to them.',
                ),
              ),
            for (final MapEntry(key: family, value: why) in list.errors.entries)
              Padding(
                padding: const EdgeInsets.fromLTRB(Space.lg, Space.sm, Space.lg, 0),
                child: Notice(
                  status: Status.warning,
                  message: "Couldn't load the ${family == TerminalFamily.host ? 'server' : 'app'} terminals. $why",
                  actionLabel: 'Retry',
                  onAction: _load,
                ),
              ),
            if (atLimit)
              Padding(
                padding: const EdgeInsets.fromLTRB(Space.lg, Space.sm, Space.lg, 0),
                child: Notice(
                  status: Status.warning,
                  message: '${host.length} terminals are running, the most the server allows. End one to start another.',
                ),
              ),
            if (host.isNotEmpty)
              TileGroup(
                title: 'On the server',
                footer: apps.isEmpty ? _footer(list) : null,
                children: [for (final s in host) _row(s)],
              ),
            if (apps.isNotEmpty)
              TileGroup(
                title: 'In apps',
                footer: _footer(list),
                children: [for (final s in apps) _row(s)],
              ),
            if (host.isEmpty && apps.isEmpty && !hostOld)
              const Padding(
                padding: EdgeInsets.all(Space.lg),
                child: Text('No terminals running.'),
              ),
            if (ended.isNotEmpty)
              TileGroup(
                title: 'Ended recently',
                footer: 'Open one to read what it printed last.',
                children: [for (final s in ended) _row(s)],
              ),
            // Room for the FAB over the last row.
            const SizedBox(height: 88),
          ]),
      ],
    );
  }

  String _footer(TerminalSessionList list) {
    final t = list.detachedTimeout;
    final idle = t == null
        ? ''
        : ' One with nothing attached ends after ${t.inHours >= 24 && t.inHours % 24 == 0 ? (t.inHours == 24 ? 'a day' : '${t.inHours ~/ 24} days') : t.inHours >= 1 ? '${t.inHours} h' : '${t.inMinutes} min'} without activity.';
    return 'Terminals keep running when you leave them.$idle They end when the server restarts.';
  }

  Widget _row(TerminalSession s) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final small = theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant);
    final what = s.activity;
    final first = [
      if (s.isContainer) s.container ?? 'App container',
      if (what.isNotEmpty) what else if (!s.isContainer && s.user != null) s.user!,
    ].join(' · ');
    final when = s.running ? (s.lastActivityAt ?? s.createdAt) : (s.exitedAt ?? s.lastActivityAt);
    return MergeSemantics(
      child: ListTile(
        leading: Icon(
          s.isContainer ? Icons.widgets_outlined : Icons.terminal_outlined,
          color: s.running ? null : scheme.onSurfaceVariant,
        ),
        title: Text(s.title, maxLines: 1, overflow: TextOverflow.ellipsis),
        subtitle: Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisSize: MainAxisSize.min, children: [
          if (s.running && first.isNotEmpty)
            Text(first, maxLines: 1, overflow: TextOverflow.ellipsis, style: DesignTokens.of(context).mono(theme.textTheme.bodyMedium).copyWith(color: scheme.onSurfaceVariant)),
          if (!s.running) Text(s.endedText),
          Wrap(spacing: Space.sm, crossAxisAlignment: WrapCrossAlignment.center, children: [
            if (when != null) (s.running ? Text(activeAgo(when), style: small) : RelativeTime(when, style: small)),
            if (s.running && s.clients > 0)
              Text(s.clients == 1 ? 'Open on another device' : 'Open on ${s.clients} devices', style: small),
          ]),
        ]),
        isThreeLine: s.running && first.isNotEmpty,
        onTap: () => _open(s),
        trailing: s.running
            ? PopupMenuButton<String>(
                tooltip: 'Options for ${s.title}',
                onSelected: (v) => switch (v) {
                  'rename' => _rename(s),
                  'end' => _end(s),
                  _ => null,
                },
                itemBuilder: (_) => [
                  const PopupMenuItem(value: 'rename', child: Text('Rename')),
                  const PopupMenuItem(value: 'end', child: Text('End')),
                ],
              )
            : IconButton(
                tooltip: 'Remove from the list',
                icon: const Icon(Icons.close),
                onPressed: () => _end(s),
              ),
      ),
    );
  }
}

/// "Active 5 min ago", "Active just now", "Active yesterday".
String activeAgo(DateTime t, {bool lower = false}) {
  var r = formatRelative(t);
  if (r == 'Just now' || r == 'Yesterday') r = r.toLowerCase();
  return '${lower ? 'active' : 'Active'} $r';
}

/// Asks for a new name for [s]; null when cancelled or unchanged.
Future<String?> showTerminalRenameDialog(BuildContext context, TerminalSession s) {
  return showDialog<String>(context: context, builder: (_) => _RenameDialog(initial: s.title));
}

class _RenameDialog extends StatefulWidget {
  const _RenameDialog({required this.initial});
  final String initial;

  @override
  State<_RenameDialog> createState() => _RenameDialogState();
}

class _RenameDialogState extends State<_RenameDialog> {
  late final _text = TextEditingController(text: widget.initial)..selection = TextSelection(baseOffset: 0, extentOffset: widget.initial.length);

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  void _save() {
    final v = _text.text.trim();
    if (v.isEmpty) return;
    Navigator.of(context).pop(v == widget.initial ? null : v);
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Rename terminal'),
      content: TextField(
        controller: _text,
        autofocus: true,
        maxLength: 80,
        textInputAction: TextInputAction.done,
        decoration: const InputDecoration(labelText: 'Name'),
        onChanged: (_) => setState(() {}),
        onSubmitted: (_) => _save(),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        TextButton(onPressed: _text.text.trim().isEmpty ? null : _save, child: const Text('Rename')),
      ],
    );
  }
}

/// Opens a shell in [container] (an app's main container): straight into a
/// new one, or - when shells are already running there - a sheet to go
/// back to one of them or start another.
Future<void> openContainerTerminal(BuildContext context, {required String container, required String title, TerminalConnector? connector}) async {
  final nav = Navigator.of(context);
  List<TerminalSession> running = const [];
  try {
    final list = await TerminalSessionsApi.instance.list(container: container);
    running = list.running;
  } catch (_) {}
  void openNew() => nav.push(MaterialPageRoute(builder: (_) => TerminalScreen(container: container, title: title, connector: connector)));
  if (running.isEmpty || !context.mounted) {
    openNew();
    return;
  }
  final picked = await showModalBottomSheet<Object>(
    context: context,
    showDragHandle: true,
    builder: (context) {
      final theme = Theme.of(context);
      return SafeArea(
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(Space.xl, 0, Space.xl, Space.sm),
            child: Text('Shells in $title', style: theme.textTheme.titleLarge),
          ),
          for (final s in running)
            ListTile(
              leading: const Icon(Icons.terminal_outlined),
              title: Text(s.title),
              subtitle: Text([if (s.activity.isNotEmpty) s.activity, if (s.lastActivityAt != null) activeAgo(s.lastActivityAt!, lower: true)].join(' · ')),
              onTap: () => Navigator.of(context).pop(s),
            ),
          ListTile(
            leading: const Icon(Icons.add),
            title: const Text('New shell'),
            onTap: () => Navigator.of(context).pop(_newShell),
          ),
          const SizedBox(height: Space.sm),
        ]),
      );
    },
  );
  if (!context.mounted) return;
  if (picked is TerminalSession) {
    nav.push(MaterialPageRoute(builder: (_) => TerminalScreen(session: picked, title: title, connector: connector)));
  } else if (picked == _newShell) {
    openNew();
  }
  // Dismissed: nothing.
}

const _newShell = 'new';
