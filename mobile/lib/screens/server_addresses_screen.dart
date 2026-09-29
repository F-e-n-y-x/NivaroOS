import 'dart:async';

import 'package:flutter/material.dart';

import '../models/server_profile.dart';
import '../services/api_client.dart';
import '../services/server_routes.dart';
import '../ui/ui.dart';
import '../widgets/connection_banner.dart';

/// The ways this phone reaches the server in use: its home-network
/// address, its Tailscale address or name, a public URL. The app uses the
/// first one in this list that answers, checking again when the app comes
/// back and when the phone's network changes, so it works at home and
/// away without changing anything. The same sign-in works on all of them.
class ServerAddressesScreen extends StatefulWidget {
  const ServerAddressesScreen({super.key, this.routes});

  /// The routes to show and change; the app's by default.
  final ServerRoutes? routes;

  @override
  State<ServerAddressesScreen> createState() => _ServerAddressesScreenState();
}

class _ServerAddressesScreenState extends State<ServerAddressesScreen> {
  ServerRoutes get _routes => widget.routes ?? ServerRoutes.instance;

  List<ServerAddress> _addresses = [];
  final Map<String, RouteProbe> _probes = {};
  bool _loading = true;
  bool _checking = false;
  bool _learning = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    await _routes.load();
    if (!mounted) return;
    setState(() {
      _addresses = List.of(_routes.profile?.effectiveAddresses ?? [for (final c in _routes.candidates) ServerAddress(c)]);
      _loading = false;
    });
    unawaited(_check());
  }

  /// Probes every address (for the per-row answer) and picks the one to
  /// use.
  Future<void> _check() async {
    if (_checking || _addresses.isEmpty) return;
    setState(() => _checking = true);
    try {
      final results = await _routes.probeAll();
      await _routes.select();
      if (!mounted) return;
      setState(() {
        _probes
          ..clear()
          ..addEntries(results.map((p) => MapEntry(p.url, p)));
      });
    } finally {
      if (mounted) setState(() => _checking = false);
    }
  }

  Future<void> _learn() async {
    setState(() => _learning = true);
    try {
      await _routes.learn(force: true);
      if (!mounted) return;
      final before = _addresses.length;
      setState(() => _addresses = List.of(_routes.profile?.effectiveAddresses ?? _addresses));
      final added = _addresses.length - before;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(
        content: Text(added > 0 ? 'Added ${added == 1 ? 'an address' : '$added addresses'} from the server' : 'No new addresses on the server'),
      ));
      unawaited(_check());
    } finally {
      if (mounted) setState(() => _learning = false);
    }
  }

  Future<void> _save(List<ServerAddress> next, {List<String> ignored = const []}) async {
    setState(() => _addresses = next);
    await _routes.saveAddresses(next, ignored: ignored);
    unawaited(_check());
  }

  void _move(int from, int to) {
    if (to < 0 || to >= _addresses.length) return;
    final next = List.of(_addresses);
    final a = next.removeAt(from);
    next.insert(to, a);
    _save(next);
  }

  Future<void> _remove(ServerAddress a) async {
    if (_addresses.length < 2) return;
    final ok = await ConfirmDialog.destructive(
      context,
      title: 'Remove ${ApiClient.displayHost(a.url)}?',
      message: a.learned
          ? 'The app stops using this address and won’t add it again from the server.'
          : 'The app stops using this address. Nothing changes on the server.',
      confirmLabel: 'Remove',
    );
    if (!ok) return;
    await _save([for (final x in _addresses) if (x.url != a.url) x], ignored: a.learned ? [a.url] : const []);
  }

  Future<void> _edit([ServerAddress? existing]) async {
    final url = await showDialog<String>(
      context: context,
      builder: (_) => _AddressDialog(initial: existing?.url),
    );
    if (url == null || !mounted) return;
    if (_addresses.any((a) => RouteRules.same(a.url, url) && a.url != existing?.url)) {
      ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('That address is already in the list')));
      return;
    }
    final typed = ServerAddress(url);
    if (existing == null) {
      // In its kind's place (home, then Tailscale, then internet); the
      // user can move it after.
      final at = _addresses.lastIndexWhere((a) => RouteRules.rank(a.url) <= RouteRules.rank(url)) + 1;
      await _save(List.of(_addresses)..insert(at, typed));
    } else {
      await _save([for (final a in _addresses) a.url == existing.url ? typed : a]);
    }
  }

  String _answer(String url) {
    final p = _probes[url];
    if (p == null) return _checking ? 'Checking…' : '';
    return p.ok ? (p.ms == null ? 'Answers' : '${p.ms} ms') : 'No answer';
  }

  Widget _connection(RouteState s) {
    final String title;
    final String subtitle;
    switch (s.status) {
      case RouteStatus.connected:
        title = s.kind?.label ?? 'Connected';
        subtitle = 'Using ${ApiClient.displayHost(s.url ?? '')}';
      case RouteStatus.unreachable:
        title = "Can't reach the server";
        subtitle = 'None of its addresses answered';
      case RouteStatus.checking:
        title = 'Checking…';
        subtitle = s.url == null ? '' : 'Using ${ApiClient.displayHost(s.url!)}';
      case RouteStatus.idle:
        title = s.kind?.label ?? 'Not checked yet';
        subtitle = s.url == null ? '' : 'Using ${ApiClient.displayHost(s.url!)}';
    }
    return ListTile(
      leading: Icon(s.status == RouteStatus.unreachable ? Icons.cloud_off_outlined : routeIcon(s.kind)),
      title: Text(title),
      subtitle: subtitle.isEmpty ? null : Text(subtitle),
      trailing: _checking
          ? const SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2.5))
          : TextButton(onPressed: _check, child: const Text('Check')),
    );
  }

  Widget _row(int i, RouteState s) {
    final a = _addresses[i];
    final kind = RouteRules.kindOf(a.url);
    final inUse = s.url != null && RouteRules.same(s.url!, a.url) && s.status != RouteStatus.unreachable;
    final answer = _answer(a.url);
    final parts = [
      kind.label,
      if (a.learned) 'From the server',
      if (answer.isNotEmpty) answer,
    ];
    final host = ApiClient.displayHost(a.url);
    final shown = a.url.startsWith('https://') ? 'https://$host' : host;
    return MergeSemantics(
      child: ListTile(
        leading: Icon(routeIcon(kind)),
        title: Row(
          children: [
            Flexible(child: Text(shown, maxLines: 1, overflow: TextOverflow.ellipsis)),
            if (inUse) ...[
              const SizedBox(width: Space.sm),
              const StatusChip(label: 'In use', status: Status.success),
            ],
          ],
        ),
        subtitle: Text(parts.join(' · ')),
        trailing: PopupMenuButton<String>(
          tooltip: 'Options for $host',
          onSelected: (v) => switch (v) {
            'up' => _move(i, i - 1),
            'down' => _move(i, i + 1),
            'edit' => _edit(a),
            'remove' => _remove(a),
            _ => null,
          },
          itemBuilder: (context) => [
            if (i > 0) const PopupMenuItem(value: 'up', child: Text('Move up')),
            if (i < _addresses.length - 1) const PopupMenuItem(value: 'down', child: Text('Move down')),
            const PopupMenuItem(value: 'edit', child: Text('Edit')),
            if (_addresses.length > 1) const PopupMenuItem(value: 'remove', child: Text('Remove')),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return AppScaffold.slivers(
      title: 'Server addresses',
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _loading ? null : () => _edit(),
        icon: const Icon(Icons.add_outlined),
        label: const Text('Add address'),
      ),
      slivers: [
        if (_loading)
          const SliverLoadingList(rows: 4)
        else
          ValueListenableBuilder<RouteState>(
            valueListenable: _routes.state,
            builder: (context, s, _) {
              final problem = connectionProblem(s);
              return SliverList.list(children: [
                TileGroup(title: 'Connection', children: [_connection(s)]),
                if (problem != null && problem.openTailscale)
                  Notice(
                    status: Status.warning,
                    icon: Icons.vpn_lock_outlined,
                    message: 'Turn on Tailscale on this phone to reach the server away from home.',
                    actionLabel: 'Open Tailscale',
                    onAction: () => ServerRoutes.openTailscale(),
                  ),
                TileGroup(
                  title: 'Addresses',
                  footer: 'The app uses the first address that answers, from the top, and checks again when you open it or '
                      'the phone changes network. Your sign-in works on all of them.',
                  children: [for (var i = 0; i < _addresses.length; i++) _row(i, s)],
                ),
                TileGroup(children: [
                  ListTile(
                    leading: const Icon(Icons.sync_outlined),
                    title: const Text('Get addresses from the server'),
                    subtitle: const Text('Its Tailscale address and home-network addresses'),
                    enabled: !_learning,
                    trailing: _learning ? const SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2.5)) : null,
                    onTap: _learning ? null : _learn,
                  ),
                ]),
                // Room for the FAB over the last row.
                const SizedBox(height: 88),
              ]);
            },
          ),
      ],
    );
  }
}

/// Asks for an address: a home-network IP, a Tailscale IP or name, or a
/// public URL. Returns it normalised.
class _AddressDialog extends StatefulWidget {
  const _AddressDialog({this.initial});

  final String? initial;

  @override
  State<_AddressDialog> createState() => _AddressDialogState();
}

class _AddressDialogState extends State<_AddressDialog> {
  late final _text = TextEditingController(text: widget.initial ?? '');
  String? _error;

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  void _done() {
    final url = ApiClient.normalizeServerUrl(_text.text);
    final host = Uri.tryParse(url)?.host ?? '';
    if (url.isEmpty || host.isEmpty) {
      setState(() => _error = 'Enter an address, like 100.64.0.1 or https://nas.example.com');
      return;
    }
    Navigator.of(context).pop(url);
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(widget.initial == null ? 'Add address' : 'Edit address'),
      content: TextField(
        controller: _text,
        autofocus: true,
        keyboardType: TextInputType.url,
        autocorrect: false,
        enableSuggestions: false,
        textInputAction: TextInputAction.done,
        onSubmitted: (_) => _done(),
        decoration: InputDecoration(
          labelText: 'Address',
          helperText: 'A Tailscale address (100.x.y.z), a home-network address, or https://… for a public one',
          helperMaxLines: 3,
          errorText: _error,
          errorMaxLines: 3,
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        TextButton(onPressed: _done, child: Text(widget.initial == null ? 'Add' : 'Save')),
      ],
    );
  }
}
