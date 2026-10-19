import 'dart:async';

import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../models/cloud_account.dart';
import '../../services/api_client.dart';
import '../../services/cloud_signin.dart';
import '../../ui/ui.dart';

/// Starts the phone's redirect listener ([OAuthRedirectForwarder]) and
/// returns what stops it; swapped in tests.
typedef StartForwarder = Future<Future<void> Function()> Function(Uri relayBase, String provider);

Future<Future<void> Function()> _startForwarder(Uri relayBase, String provider) async {
  final f = await OAuthRedirectForwarder.start(relayBase);
  await keepAppAwake(provider);
  return () async {
    await keepAppAwake(null);
    await f.close();
  };
}

Future<bool> _openInBrowser(Uri url) async {
  // A Custom Tab: providers refuse sign-in inside an embedded WebView.
  if (await launchUrl(url, mode: LaunchMode.inAppBrowserView)) return true;
  return launchUrl(url, mode: LaunchMode.externalApplication);
}

typedef _Provider = ({String type, String label, String guide});

const _providers = <_Provider>[
  (type: 'drive', label: 'Google Drive', guide: 'google-drive'),
  (type: 'onedrive', label: 'OneDrive', guide: 'onedrive'),
  (type: 'dropbox', label: 'Dropbox', guide: 'dropbox'),
];

const _guide = 'https://github.com/F-e-n-y-x/NivaroOS/blob/master/docs/guides/cloud-own-app.md';

// What rclone takes as a client ID or secret (as the server checks).
final _safeKey = RegExp(r'^[A-Za-z0-9._~][A-Za-z0-9._~-]*$');

/// Files › Cloud drives › Add (no [account]) or Reconnect ([account]):
/// signs in to Google Drive, OneDrive or Dropbox in the browser, with
/// rclone's app or the user's own. The server runs the sign-in and keeps
/// the keys (cloud_signin.dart). Pops true once the drive is connected.
class CloudSignInScreen extends StatefulWidget {
  const CloudSignInScreen({
    super.key,
    this.account,
    this.startForwarder = _startForwarder,
    this.openBrowser = _openInBrowser,
    this.pollEvery = const Duration(seconds: 2),
  });

  final CloudAccount? account;
  final StartForwarder startForwarder;
  final Future<bool> Function(Uri url) openBrowser;
  final Duration pollEvery;

  @override
  State<CloudSignInScreen> createState() => _CloudSignInScreenState();
}

class _CloudSignInScreenState extends State<CloudSignInScreen> {
  final _label = TextEditingController();
  final _clientId = TextEditingController();
  final _secret = TextEditingController();
  String _type = 'drive';
  bool _ownApp = false;
  bool _showSecret = false;
  String? _keysError;

  bool _busy = false;
  String _phase = '';
  String? _error;
  Uri? _signInUrl;
  int _run = 0; // a new run (or Cancel) ends the one before

  CloudAccount? get _account => widget.account;
  String get _providerLabel => _account?.providerTitle ?? _providers.firstWhere((p) => p.type == _type).label;

  @override
  void dispose() {
    _run++;
    _label.dispose();
    _clientId.dispose();
    _secret.dispose();
    super.dispose();
  }

  Future<void> _signIn() async {
    final api = ApiClient.instance;
    final id = _clientId.text.trim(), secret = _secret.text.trim();
    if (_account == null && _ownApp) {
      final bad = !_safeKey.hasMatch(id) || !_safeKey.hasMatch(secret);
      setState(() => _keysError = id.isEmpty || secret.isEmpty
          ? 'Enter both the client ID and the client secret'
          : bad
              ? "That doesn't look like a client ID and secret"
              : null);
      if (_keysError != null) return;
    }
    final run = ++_run;
    bool live() => mounted && run == _run;
    setState(() {
      _busy = true;
      _error = null;
      _signInUrl = null;
      _phase = 'Starting the sign-in…';
    });

    final relay = api.buildUri('/v1/rclone-auth');
    Future<void> Function()? stopForwarder;
    try {
      try {
        stopForwarder = await widget.startForwarder(relay, _providerLabel);
      } catch (_) {
        throw Exception('Another app on this phone is using port $rcloneRedirectPort, which the sign-in needs. Close it and try again.');
      }
      final res = _account != null
          ? await api.post('/cloud/accounts/${Uri.encodeComponent(_account!.remote)}/authorize')
          : await api.post('/cloud/accounts/authorize', body: {
              'type': _type,
              'label': _label.text.trim(),
              if (_ownApp) ...{'client_id': id, 'client_secret': secret},
            });
      var v = _view(res);
      final name = _account?.remote ?? (v['name'] as String? ?? '');
      final path = v['path'] as String? ?? '';
      if (path.isEmpty || name.isEmpty) throw Exception(v['error'] as String? ?? "The server couldn't start the sign-in. Try again.");
      final url = await providerSignInUrl(relay, path);
      if (!live()) return;
      setState(() {
        _signInUrl = url;
        _phase = 'Sign in to $_providerLabel in the browser, then come back here.';
      });
      if (!await widget.openBrowser(url)) throw Exception("Couldn't open the browser.");

      while (v['running'] == true) {
        await Future<void>.delayed(widget.pollEvery);
        if (!live()) return;
        final data = (await api.get('/cloud/accounts/${Uri.encodeComponent(name)}/authorize'))['data'];
        if (data is! Map<String, dynamic>) throw Exception('The sign-in stopped on the server. Start it again.');
        v = data;
      }
      if (!live()) return;
      if (v['error'] is String && (v['error'] as String).isNotEmpty) throw Exception(v['error']);
      final token = v['token'] as String? ?? '';
      if (_account != null) {
        if (token.isEmpty) throw Exception('The sign-in ended without a token. Try again.');
        setState(() => _phase = 'Connecting $_providerLabel…');
        // Just the token: the account keeps its app.
        _view(await api.post('/cloud/accounts/${Uri.encodeComponent(name)}/reconnect', body: {
          'params': {'token': token},
        }, timeout: const Duration(minutes: 2)));
      } else if (v['saved'] != true) {
        throw Exception('The sign-in ended without saving the drive. Try again.');
      }
      if (!mounted || run != _run) return;
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text('$_providerLabel connected')));
      Navigator.of(context).pop(true);
    } catch (e) {
      if (live()) {
        setState(() {
          _busy = false;
          _error = e is ApiException ? e.message : e.toString().replaceFirst('Exception: ', '');
        });
      }
    } finally {
      await stopForwarder?.call();
    }
  }

  /// The envelope's data; throws its message when it says it failed.
  Map<String, dynamic> _view(Map<String, dynamic> res) {
    final code = res['success'];
    final data = res['data'];
    if (code is num && code != 200) {
      throw Exception(data is String && data.isNotEmpty ? data : (res['message']?.toString() ?? 'The server refused ($code).'));
    }
    return data is Map<String, dynamic> ? data : const {};
  }

  void _cancel() => setState(() {
        _run++;
        _busy = false;
        _signInUrl = null;
      });

  @override
  Widget build(BuildContext context) {
    final gutter = Space.gutter(context);
    Widget pad(Widget w) => Padding(padding: EdgeInsets.symmetric(horizontal: gutter), child: w);
    final a = _account;
    return AppScaffold(
      title: a == null ? 'Add cloud drive' : 'Reconnect',
      body: ListView(
        padding: const EdgeInsets.only(top: Space.sm, bottom: Space.xl),
        children: [
          if (a == null) ..._addForm(pad) else ..._reconnectInfo(a),
          if (_error != null) Notice(status: Status.error, message: _error!),
          const SizedBox(height: Space.md),
          if (_busy) _progress(pad) else pad(FilledButton(
            style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
            onPressed: _signIn,
            child: Text(a == null ? 'Sign in to $_providerLabel' : 'Sign in again'),
          )),
        ],
      ),
    );
  }

  List<Widget> _reconnectInfo(CloudAccount a) => [
        TileGroup(children: [
          ListTile(
            leading: const Icon(Icons.cloud_outlined),
            title: Text(a.displayName, maxLines: 2, overflow: TextOverflow.ellipsis),
            subtitle: Text(a.clientId.isEmpty
                ? '${a.providerTitle} · rclone\'s app'
                : '${a.providerTitle} · your own app (${a.clientId.length > 6 ? '${a.clientId.substring(0, 6)}…' : a.clientId})'),
          ),
        ]),
        if (a.problem.isNotEmpty) Notice(status: Status.warning, title: 'Sign-in expired', message: a.problem),
        Notice(
          status: Status.info,
          message: 'Sign in to ${a.providerTitle} again in the browser. The drive keeps its name, files and settings, and signs in with the same app as before.',
        ),
      ];

  List<Widget> _addForm(Widget Function(Widget) pad) {
    final enabled = !_busy;
    return [
      TileGroup(
        title: 'Service',
        footer: 'S3, WebDAV, SFTP, iCloud and the others are added in the web UI: Settings › Online Storage.',
        children: [
          for (final p in _providers)
            ListTile(
              leading: const Icon(Icons.cloud_outlined),
              title: Text(p.label),
              selected: _type == p.type,
              enabled: enabled,
              trailing: _type == p.type ? const Icon(Icons.check) : null,
              onTap: () => setState(() => _type = p.type),
            ),
        ],
      ),
      TileGroup(
        title: 'Sign in with',
        children: [
          ListTile(
            title: const Text("rclone's app"),
            subtitle: Text("Quickest to set up. Shared by every rclone user, so $_providerLabel slows it down - fine for small folders."),
            selected: !_ownApp,
            enabled: enabled,
            trailing: !_ownApp ? const Icon(Icons.check) : null,
            onTap: () => setState(() => _ownApp = false),
          ),
          ListTile(
            title: const Text('Your own app'),
            subtitle: Text('Its own speed limits - much faster for big backups like photo libraries. About 10 minutes to create, once.'),
            selected: _ownApp,
            enabled: enabled,
            trailing: _ownApp ? const Icon(Icons.check) : null,
            onTap: () => setState(() => _ownApp = true),
          ),
        ],
      ),
      if (_ownApp) ...[
        if (_type == 'drive')
          const Notice(
            status: Status.warning,
            title: 'Publish the app or it expires in 7 days',
            message: 'Google signs out an app left in Testing mode every 7 days. Publish it first: Google Cloud › OAuth consent screen › Publish app.',
          ),
        const SizedBox(height: Space.sm),
        pad(TextField(
          controller: _clientId,
          enabled: enabled,
          autocorrect: false,
          enableSuggestions: false,
          decoration: InputDecoration(labelText: 'Client ID', errorText: _keysError),
          onChanged: (_) => _keysError == null ? null : setState(() => _keysError = null),
        )),
        const SizedBox(height: Space.lg),
        pad(TextField(
          controller: _secret,
          enabled: enabled,
          obscureText: !_showSecret,
          autocorrect: false,
          enableSuggestions: false,
          decoration: InputDecoration(
            labelText: 'Client secret',
            helperText: 'Kept on the server; never shown again.',
            suffixIcon: IconButton(
              tooltip: _showSecret ? 'Hide secret' : 'Show secret',
              icon: Icon(_showSecret ? Icons.visibility_off_outlined : Icons.visibility_outlined),
              onPressed: () => setState(() => _showSecret = !_showSecret),
            ),
          ),
        )),
        Align(
          alignment: AlignmentDirectional.centerStart,
          child: Padding(
            padding: EdgeInsets.symmetric(horizontal: Space.gutter(context) - Space.md),
            child: TextButton.icon(
              icon: const Icon(Icons.open_in_new),
              label: const Text('How to create one'),
              onPressed: () => launchUrl(Uri.parse('$_guide#${_providers.firstWhere((p) => p.type == _type).guide}'), mode: LaunchMode.externalApplication),
            ),
          ),
        ),
      ],
      const SizedBox(height: Space.lg),
      pad(TextField(
        controller: _label,
        enabled: enabled,
        decoration: InputDecoration(labelText: 'Name', hintText: _providerLabel, helperText: 'How it shows in Files'),
      )),
    ];
  }

  Widget _progress(Widget Function(Widget) pad) {
    final theme = Theme.of(context);
    return pad(Semantics(
      liveRegion: true,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(children: [
            const SizedBox.square(dimension: 20, child: CircularProgressIndicator(strokeWidth: 2.5)),
            const SizedBox(width: Space.lg),
            Expanded(child: Text(_phase, style: theme.textTheme.bodyLarge)),
          ]),
          const SizedBox(height: Space.md),
          Wrap(spacing: Space.sm, children: [
            if (_signInUrl != null)
              OutlinedButton(onPressed: () => widget.openBrowser(_signInUrl!), child: const Text('Open sign-in page again')),
            TextButton(onPressed: _cancel, child: const Text('Cancel')),
          ]),
        ],
      ),
    ));
  }
}
