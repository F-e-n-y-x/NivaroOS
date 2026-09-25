import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:url_launcher/url_launcher.dart';

import '../services/api_client.dart';
import '../services/tailscale_service.dart';
import '../ui/ui.dart';

/// Entry point kept for the screens that open Tailscale
/// (`TailscaleModal.show(context)`); it is a full page now, because status,
/// addresses and the device list don't fit a sheet.
abstract final class TailscaleModal {
  static Future<void> show(BuildContext context) =>
      Navigator.of(context).push(MaterialPageRoute(builder: (_) => const TailscaleScreen()));
}

/// The server's Tailscale (plan M-10, WP1-12): connect or disconnect, sign
/// the server in with Tailscale's own login link (opened in the browser,
/// then the page waits for the approval), its tailnet address and name,
/// and the other devices on the tailnet. No auth keys are typed or stored.
class TailscaleScreen extends StatefulWidget {
  const TailscaleScreen({super.key});

  @override
  State<TailscaleScreen> createState() => _TailscaleScreenState();
}

class _TailscaleScreenState extends State<TailscaleScreen> {
  TailscaleStatus? _status;
  ApiException? _error;
  bool _busy = false;
  bool _waitingForLogin = false;
  String? _loginUrl;
  bool _disposed = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final status = await TailscaleService.instance.getStatus();
      if (!mounted) return;
      setState(() {
        _status = status;
        _error = null;
        if (status.authUrl.isNotEmpty) _loginUrl = status.authUrl;
        if (status.isRunning) _loginUrl = null;
      });
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  void _snack(String text) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(text)));
  }

  Future<void> _connect() async {
    setState(() => _busy = true);
    try {
      final result = await TailscaleService.instance.connect();
      if (result.needsLogin) {
        setState(() => _loginUrl = result.loginUrl);
        await _openLogin();
      } else {
        await TailscaleService.instance.waitFor((s) => s.isRunning, timeout: const Duration(seconds: 20), cancelled: () => _disposed);
      }
      await _load();
    } on ApiException catch (e) {
      _snack("Couldn't connect Tailscale. ${e.message}");
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _openLogin() async {
    final url = _loginUrl;
    if (url == null) return;
    try {
      await launchUrl(Uri.parse(url), mode: LaunchMode.externalApplication);
    } catch (_) {
      _snack("Couldn't open the browser. Copy the link and open it on any device.");
    }
    if (!mounted || _waitingForLogin) return;
    setState(() => _waitingForLogin = true);
    final status = await TailscaleService.instance.waitFor((s) => s.isRunning, cancelled: () => _disposed);
    if (!mounted) return;
    setState(() {
      _waitingForLogin = false;
      if (status != null) _status = status;
      if (status?.isRunning ?? false) _loginUrl = null;
    });
    if (status?.isRunning ?? false) _snack('The server is on your tailnet');
  }

  Future<void> _disconnect() async {
    final viaTailscale = TailscaleService.isTailscaleAddress(ApiClient.instance.baseUrl);
    final ok = await ConfirmDialog.destructive(
      context,
      title: 'Disconnect Tailscale?',
      message: viaTailscale
          ? 'This phone reaches the server through Tailscale right now, so the app loses its connection until Tailscale is back on.'
          : "Devices that reach the server through Tailscale can't until you connect again.",
      confirmLabel: 'Disconnect',
      permanent: false,
    );
    if (!ok) return;
    setState(() => _busy = true);
    try {
      await TailscaleService.instance.disconnect();
      await _load();
    } on ApiException catch (e) {
      _snack("Couldn't disconnect Tailscale. ${e.message}");
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _install() async {
    final ok = await ConfirmDialog.confirm(
      context,
      title: 'Install Tailscale on the server?',
      message: "The server downloads Tailscale's official install script and runs it. It takes a minute or two.",
      confirmLabel: 'Install',
    );
    if (!ok) return;
    setState(() => _busy = true);
    try {
      final result = await TailscaleService.instance.install();
      await _load();
      if (result.needsLogin) {
        setState(() => _loginUrl = result.loginUrl);
        await _openLogin();
      }
    } on ApiException catch (e) {
      _snack("Couldn't install Tailscale. ${e.message}");
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _copy(String text, String what) {
    Clipboard.setData(ClipboardData(text: text));
    _snack('$what copied');
  }

  @override
  Widget build(BuildContext context) {
    final status = _status;
    final error = _error;

    return AppScaffold.slivers(
      title: 'Tailscale',
      onRefresh: _load,
      banner: error != null && status != null && error.isUnreachable ? OfflineBanner(onRetry: _load) : null,
      slivers: [
        if (status == null && error != null)
          error.isUnreachable
              ? ErrorState.offline(sliver: true, onRetry: _load, details: error.details)
              : ErrorState(sliver: true, title: "Couldn't load Tailscale", message: error.message, onRetry: _load, details: error.details)
        else if (status == null)
          const SliverLoadingList(rows: 5)
        else if (status.state == TailscaleState.notInstalled)
          EmptyState(
            sliver: true,
            icon: Icons.vpn_key_outlined,
            title: "Tailscale isn't on the server",
            message: 'Tailscale lets your devices reach the server from anywhere, without opening ports on your router.',
            actionLabel: _busy ? null : 'Install Tailscale',
            onAction: _busy ? null : _install,
          )
        else
          SliverList.list(children: _content(context, status)),
      ],
    );
  }

  List<Widget> _content(BuildContext context, TailscaleStatus s) {
    final theme = Theme.of(context);
    final state = s.state;
    final (title, subtitle, chip) = switch (state) {
      TailscaleState.running => ('Connected', s.magicDns.isEmpty ? 'On your tailnet' : s.magicDns, Status.success),
      TailscaleState.starting => ('Connecting', 'Tailscale is starting', Status.info),
      TailscaleState.needsLogin => ('Needs sign-in', 'Sign the server in to your tailnet', Status.warning),
      TailscaleState.stopped => ('Off', 'Signed in, but disconnected', Status.neutral),
      TailscaleState.noDaemon => ('Off', "Tailscale's service isn't running on the server", Status.neutral),
      TailscaleState.notInstalled => ('Not installed', '', Status.neutral),
    };
    final on = state == TailscaleState.running || state == TailscaleState.starting;
    final needsLogin = state == TailscaleState.needsLogin || (_loginUrl != null && !s.isRunning);

    return [
      TileGroup(
        footer: 'To use it from this phone, install the Tailscale app and sign in to the same tailnet.',
        children: [
          ListTile(
            leading: const Icon(Icons.vpn_key_outlined),
            title: Text('Tailscale on the server', style: theme.textTheme.bodyLarge),
            subtitle: Padding(
              padding: const EdgeInsets.only(top: Space.xs),
              child: Align(alignment: AlignmentDirectional.centerStart, child: StatusChip(label: title, status: chip)),
            ),
            // Signing in is the only way forward here, so it is a button,
            // not an off switch next to "Needs sign-in" (is it running or
            // not?). Otherwise the switch says and changes on/off.
            trailing: _busy
                ? const SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2.5))
                : needsLogin
                    ? null
                    : Semantics(
                        label: on ? 'Disconnect Tailscale' : 'Connect Tailscale',
                        child: Switch(
                          value: on,
                          onChanged: (v) => v ? _connect() : _disconnect(),
                        ),
                      ),
            onTap: _busy || needsLogin ? null : () => on ? _disconnect() : _connect(),
          ),
          if (subtitle.isNotEmpty && state != TailscaleState.running && !needsLogin)
            ListTile(leading: const Icon(Icons.info_outline), title: Text(subtitle)),
          if (needsLogin)
            Padding(
              padding: const EdgeInsets.all(Space.lg),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text(
                    _waitingForLogin
                        ? 'Approve the server in the browser; this page updates by itself.'
                        : 'Sign the server in to your tailnet. Tailscale’s sign-in page opens in the browser.',
                    style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                  ),
                  const SizedBox(height: Space.md),
                  FilledButton.icon(
                    style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
                    onPressed: _busy || _waitingForLogin ? null : (_loginUrl != null ? _openLogin : _connect),
                    onLongPress: _loginUrl == null ? null : () => _copy(_loginUrl!, 'Sign-in link'),
                    icon: const Icon(Icons.login_outlined),
                    label: Text(_waitingForLogin ? 'Waiting for the approval…' : 'Sign in to Tailscale'),
                  ),
                ],
              ),
            ),
          ListTile(
            leading: const Icon(Icons.phone_android_outlined),
            title: const Text('Get Tailscale for this phone'),
            trailing: const Icon(Icons.open_in_new_outlined),
            onTap: () => launchUrl(Uri.parse('https://tailscale.com/download/android'), mode: LaunchMode.externalApplication),
          ),
        ],
      ),
      if (s.selfIp.isNotEmpty || s.dnsName.isNotEmpty)
        TileGroup(title: 'This server', children: [
          if (s.selfIp.isNotEmpty)
            ListTile(
              leading: const Icon(Icons.lan_outlined),
              title: const Text('Tailscale address'),
              subtitle: Text(s.selfIp, style: theme.textTheme.bodyMedium?.tabular),
              trailing: IconButton(tooltip: 'Copy address', icon: const Icon(Icons.content_copy_outlined), onPressed: () => _copy(s.selfIp, 'Address')),
            ),
          if (s.dnsName.isNotEmpty)
            ListTile(
              leading: const Icon(Icons.dns_outlined),
              title: const Text('Name'),
              subtitle: Text(s.dnsName),
              trailing: IconButton(tooltip: 'Copy name', icon: const Icon(Icons.content_copy_outlined), onPressed: () => _copy(s.dnsName, 'Name')),
            ),
          if (s.primaryRoutes.isNotEmpty)
            ListTile(
              leading: const Icon(Icons.alt_route_outlined),
              title: const Text('Shares these networks'),
              subtitle: Text(s.primaryRoutes.join(', ')),
            ),
          if (s.hasExitNodeOption)
            const ListTile(
              leading: Icon(Icons.public_outlined),
              title: Text('Exit node'),
              subtitle: Text('Other devices can send their internet traffic through this server'),
            ),
        ]),
      TileGroup(
        title: s.peers.isEmpty ? 'Devices on your tailnet' : 'Devices on your tailnet (${s.peers.length})',
        children: s.peers.isEmpty
            ? [
                const ListTile(
                  leading: Icon(Icons.devices_other_outlined),
                  title: Text('No other devices yet'),
                  subtitle: Text('Devices signed in to the same tailnet show up here.'),
                ),
              ]
            : [
                for (final p in s.peers)
                  ListTile(
                    leading: Icon(p.isMobile ? Icons.phone_android_outlined : Icons.computer_outlined),
                    title: Text(p.hostName.isEmpty ? p.dnsName : p.hostName, maxLines: 1, overflow: TextOverflow.ellipsis),
                    subtitle: Text([
                      p.online ? 'Online' : (p.lastSeen == null ? 'Offline' : 'Offline · seen ${seenPhrase(p.lastSeen!)}'),
                      if (p.ip.isNotEmpty) p.ip,
                    ].join(' · ')),
                    onLongPress: p.ip.isEmpty ? null : () => _copy(p.ip, 'Address'),
                  ),
              ],
      ),
    ];
  }
}

/// "2 h ago", "yesterday", "Sep 5": a relative time for use mid-sentence.
String seenPhrase(DateTime t) {
  final r = formatRelative(t);
  return r == 'Just now' || r == 'Yesterday' ? r.toLowerCase() : r;
}
