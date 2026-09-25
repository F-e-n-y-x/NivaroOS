import 'package:clock/clock.dart';
import 'package:flutter/material.dart';

import '../models/server_profile.dart';
import '../services/api_client.dart';
import '../services/session_service.dart';
import '../services/storage_service.dart';
import '../ui/ui.dart';
import 'home_shell.dart';
import 'login_screen.dart';

/// Saved servers: the one in use, and the others to switch to. Switching
/// stops everything the phone does in the background for the old server
/// (storage sharing, the heartbeat) before the new one takes over, and
/// restarts the app's screens on it (plan M-17).
class ServerProfilesScreen extends StatefulWidget {
  const ServerProfilesScreen({super.key});

  @override
  State<ServerProfilesScreen> createState() => _ServerProfilesScreenState();
}

class _ServerProfilesScreenState extends State<ServerProfilesScreen> {
  List<ServerProfile> _profiles = [];
  String? _activeUrl;
  String? _activeId;
  bool _loading = true;
  String? _switching;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final profiles = await StorageService.instance.getProfiles();
    final activeUrl = await StorageService.instance.getServerUrl();
    final activeId = await StorageService.instance.getActiveProfileId();
    if (!mounted) return;
    setState(() {
      _profiles = profiles;
      _activeUrl = activeUrl;
      _activeId = activeId;
      _loading = false;
    });
  }

  bool _isActive(ServerProfile p) {
    if (_activeUrl == null) return false;
    final same = ApiClient.normalizeServerUrl(p.url) == ApiClient.normalizeServerUrl(_activeUrl!);
    if (!same) return false;
    // Two profiles for one server: the active id decides.
    final twins = _profiles.where((o) => ApiClient.normalizeServerUrl(o.url) == ApiClient.normalizeServerUrl(_activeUrl!)).length;
    return twins < 2 || p.id == _activeId;
  }

  Future<void> _switchTo(ServerProfile profile) async {
    if (_switching != null) return;
    setState(() => _switching = profile.id);
    try {
      final hasSession = await SessionService.switchTo(profile);
      if (!mounted) return;
      final nav = Navigator.of(context);
      if (!hasSession) {
        // Replace the whole stack: the Home under it still shows the old
        // server's data, and back must not lead there (review finding 12).
        nav.pushAndRemoveUntil(MaterialPageRoute(builder: (_) => LoginScreen(initialUsername: profile.username)), (_) => false);
        return;
      }
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Switched to ${profile.displayName}')));
      nav.pushAndRemoveUntil(MaterialPageRoute(builder: (_) => const HomeShell()), (_) => false);
    } finally {
      if (mounted) setState(() => _switching = null);
    }
  }

  Future<void> _signInAgain(ServerProfile profile) async {
    if (_isActive(profile)) {
      final ok = await Navigator.of(context).push<bool>(
        MaterialPageRoute(builder: (_) => LoginScreen(isReauth: true, initialUsername: profile.username)),
      );
      if (ok == true) await _load();
      return;
    }
    await _switchTo(profile.withoutTokens());
  }

  Future<void> _edit([ServerProfile? existing]) async {
    final saved = await Navigator.of(context).push<ServerProfile>(
      MaterialPageRoute(fullscreenDialog: true, builder: (_) => ServerProfileForm(existing: existing)),
    );
    if (saved == null || !mounted) return;
    await StorageService.instance.saveProfile(saved);
    if (existing != null && _isActive(existing) &&
        (saved.url != existing.url || saved.accessToken != existing.accessToken || saved.username != existing.username)) {
      // The server in use changed its address or account: start over on it.
      await _switchTo(saved);
      return;
    }
    await _load();
  }

  Future<void> _remove(ServerProfile profile) async {
    final ok = await ConfirmDialog.destructive(
      context,
      title: 'Remove “${profile.displayName}”?',
      message: 'Its address and sign-in are removed from this phone. Nothing changes on the server.',
      confirmLabel: 'Remove',
    );
    if (!ok) return;
    await StorageService.instance.deleteProfile(profile.id);
    await _load();
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Removed ${profile.displayName}')));
  }

  Widget _row(ServerProfile p, {required bool active}) {
    final host = ApiClient.displayHost(p.url);
    // The name is often the host itself; don't say it twice.
    final where = p.displayName == host ? null : host;
    final parts = [
      if (p.username.isNotEmpty) p.username,
      ?where,
      if (!active && !p.hasSession) 'Signed out',
    ];
    final subtitle = parts.isEmpty ? host : parts.join(' · ');
    return MergeSemantics(
      child: ListTile(
        // Outlined like every other row icon; the "In use" group already says
        // which server this is.
        leading: const Icon(Icons.dns_outlined),
        title: Text(p.displayName, maxLines: 1, overflow: TextOverflow.ellipsis),
        subtitle: Text(subtitle),
        onTap: active || _switching != null ? null : () => _switchTo(p),
        trailing: _switching == p.id
            ? const SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2.5))
            : PopupMenuButton<String>(
                tooltip: 'Options for ${p.displayName}',
                onSelected: (v) => switch (v) {
                  'switch' => _switchTo(p),
                  'signin' => _signInAgain(p),
                  'edit' => _edit(p),
                  'remove' => _remove(p),
                  _ => null,
                },
                itemBuilder: (context) => [
                  if (!active) const PopupMenuItem(value: 'switch', child: Text('Switch to this server')),
                  PopupMenuItem(value: 'signin', child: Text(active || p.hasSession ? 'Sign in again' : 'Sign in')),
                  const PopupMenuItem(value: 'edit', child: Text('Edit')),
                  if (!active) const PopupMenuItem(value: 'remove', child: Text('Remove')),
                ],
              ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final active = _profiles.where(_isActive).toList();
    final others = _profiles.where((p) => !_isActive(p)).toList();

    return AppScaffold.slivers(
      title: 'Servers',
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _edit(),
        icon: const Icon(Icons.add_outlined),
        label: const Text('Add server'),
      ),
      slivers: [
        if (_loading)
          const SliverLoadingList(rows: 3)
        else if (_profiles.isEmpty)
          EmptyState(
            sliver: true,
            icon: Icons.dns_outlined,
            title: 'No servers saved',
            message: 'Add a NivaroOS server to switch between servers without typing their address again.',
            actionLabel: 'Add server',
            onAction: () => _edit(),
          )
        else
          SliverList.list(children: [
            if (active.isNotEmpty) TileGroup(title: 'In use', children: [for (final p in active) _row(p, active: true)]),
            if (others.isNotEmpty)
              TileGroup(
                title: 'Other servers',
                footer: 'Tap a server to switch to it. Storage sharing and check-ins with the current server stop first.',
                children: [for (final p in others) _row(p, active: false)],
              ),
            // Room for the FAB over the last row.
            const SizedBox(height: 88),
          ]),
      ],
    );
  }
}

/// Add or edit a saved server: a full-screen form (more than three
/// fields). The address is checked before saving; with a password it also
/// signs in, so switching to the server later needs no sign-in.
class ServerProfileForm extends StatefulWidget {
  const ServerProfileForm({super.key, this.existing});

  final ServerProfile? existing;

  @override
  State<ServerProfileForm> createState() => _ServerProfileFormState();
}

class _ServerProfileFormState extends State<ServerProfileForm> {
  late final _name = TextEditingController(text: widget.existing?.name ?? '');
  late final _url = TextEditingController(text: widget.existing?.url ?? '');
  late final _user = TextEditingController(text: widget.existing?.username ?? '');
  final _pass = TextEditingController();
  bool _saving = false;
  bool _obscure = true;
  String? _urlError;
  String? _passError;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _url.dispose();
    _user.dispose();
    _pass.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    setState(() {
      _urlError = _url.text.trim().isEmpty ? 'Enter the server address' : null;
      _passError = null;
      _error = null;
    });
    if (_urlError != null) return;
    setState(() => _saving = true);
    try {
      final String url;
      try {
        url = (await ApiClient.probe(_url.text)).url;
      } on ApiException catch (e) {
        setState(() => _urlError = e.message);
        return;
      }
      final existing = widget.existing;
      String? access;
      String? refresh;
      final username = _user.text.trim();
      if (_pass.text.isNotEmpty) {
        try {
          final t = await ApiClient.requestTokens(url, username, _pass.text);
          access = t.accessToken;
          refresh = t.refreshToken;
        } on ApiException catch (e) {
          setState(() => e.kind == ApiErrorKind.auth ? _passError = e.message : _error = e.message);
          return;
        }
      } else if (existing != null && existing.url == url && existing.username == username) {
        access = existing.accessToken;
        refresh = existing.refreshToken;
      }
      final name = _name.text.trim();
      final profile = ServerProfile(
        id: existing?.id ?? 'srv_${clock.now().millisecondsSinceEpoch}',
        name: name.isEmpty ? ServerProfile.defaultName(url) : name,
        url: url,
        username: username,
        accessToken: access,
        refreshToken: refresh,
        lastConnected: existing?.lastConnected,
      );
      if (mounted) Navigator.of(context).pop(profile);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final editing = widget.existing != null;
    final gutter = Space.gutter(context);
    final scheme = Theme.of(context).colorScheme;
    return AppScaffold(
      title: editing ? 'Edit server' : 'Add server',
      leading: IconButton(
        tooltip: 'Close',
        icon: const Icon(Icons.close_outlined),
        onPressed: _saving ? null : () => Navigator.of(context).pop(),
      ),
      actions: [
        Padding(
          padding: const EdgeInsets.only(right: Space.sm),
          child: TextButton(onPressed: _saving ? null : _save, child: Text(_saving ? 'Checking…' : 'Save')),
        ),
      ],
      body: AutofillGroup(
        child: ListView(
          padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.xl),
          children: [
            if (_saving) const Padding(padding: EdgeInsets.only(bottom: Space.lg), child: LinearProgressIndicator()),
            TextField(
              controller: _url,
              enabled: !_saving,
              keyboardType: TextInputType.url,
              autocorrect: false,
              enableSuggestions: false,
              textInputAction: TextInputAction.next,
              decoration: InputDecoration(
                labelText: 'Address',
                helperText: 'For example 192.168.1.20 or https://nas.example.com',
                helperMaxLines: 2,
                errorText: _urlError,
                errorMaxLines: 5,
              ),
            ),
            const SizedBox(height: Space.lg),
            TextField(
              controller: _name,
              enabled: !_saving,
              textCapitalization: TextCapitalization.sentences,
              textInputAction: TextInputAction.next,
              decoration: const InputDecoration(labelText: 'Name (optional)', helperText: 'Shown in the server list, like “Home” or “Office”'),
            ),
            const SizedBox(height: Space.xl),
            TextField(
              controller: _user,
              enabled: !_saving,
              autofillHints: const [AutofillHints.username],
              autocorrect: false,
              textInputAction: TextInputAction.next,
              decoration: const InputDecoration(labelText: 'Username'),
            ),
            const SizedBox(height: Space.lg),
            TextField(
              controller: _pass,
              enabled: !_saving,
              obscureText: _obscure,
              autofillHints: const [AutofillHints.password],
              textInputAction: TextInputAction.done,
              onSubmitted: (_) => _save(),
              decoration: InputDecoration(
                labelText: 'Password (optional)',
                helperText: editing ? 'Leave empty to keep the current sign-in' : 'With a password, switching to this server needs no sign-in',
                helperMaxLines: 2,
                errorText: _passError,
                suffixIcon: IconButton(
                  tooltip: _obscure ? 'Show password' : 'Hide password',
                  icon: Icon(_obscure ? Icons.visibility_outlined : Icons.visibility_off_outlined),
                  onPressed: () => setState(() => _obscure = !_obscure),
                ),
              ),
            ),
            if (_error != null) ...[
              const SizedBox(height: Space.lg),
              Semantics(
                liveRegion: true,
                child: Text(_error!, style: Theme.of(context).textTheme.bodyMedium?.copyWith(color: scheme.error)),
              ),
            ],
          ],
        ),
      ),
    );
  }
}
