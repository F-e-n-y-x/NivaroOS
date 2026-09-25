import 'package:flutter/material.dart';

import '../services/api_client.dart';
import '../services/discovery_service.dart';
import '../services/session_service.dart';
import '../services/storage_service.dart';
import '../ui/ui.dart';
import 'home_shell.dart';
import 'login_screen.dart';

/// First run: find the server. NivaroOS servers on this Wi-Fi announce
/// themselves over mDNS and are listed as they answer; any other server
/// (a reverse proxy, a tunnel, Tailscale) is typed in. Every address is
/// checked before moving on, so a wrong port, a web page that isn't
/// NivaroOS or an untrusted certificate is reported here, in words,
/// instead of as a failed sign-in.
class DiscoveryScreen extends StatefulWidget {
  const DiscoveryScreen({super.key});

  @override
  State<DiscoveryScreen> createState() => _DiscoveryScreenState();
}

class _DiscoveryScreenState extends State<DiscoveryScreen> {
  final _service = DiscoveryService();
  final _addressController = TextEditingController();
  final List<DiscoveredServer> _found = [];
  bool _scanning = true;

  /// The address being checked (a found server's URL, or the typed one).
  String? _checking;
  String? _addressError;
  String? _foundError;

  @override
  void initState() {
    super.initState();
    _scan();
  }

  @override
  void dispose() {
    _addressController.dispose();
    super.dispose();
  }

  Future<void> _scan() async {
    setState(() {
      _scanning = true;
      _found.clear();
      _foundError = null;
    });
    try {
      await for (final server in _service.discover()) {
        if (!mounted) return;
        setState(() {
          if (!_found.any((s) => s.host == server.host && s.port == server.port)) _found.add(server);
        });
      }
    } catch (_) {}
    if (!mounted) return;
    setState(() => _scanning = false);
  }

  /// Checks [input], then goes to sign-in (or straight in, for a saved
  /// server that still has a session).
  Future<void> _connect(String input, {required bool typed}) async {
    if (_checking != null) return;
    if (input.trim().isEmpty) {
      setState(() => _addressError = 'Enter the server address.');
      return;
    }
    setState(() {
      _checking = input;
      _addressError = null;
      _foundError = null;
    });
    try {
      final probe = await ApiClient.probe(input);
      if (!mounted) return;
      final saved = (await StorageService.instance.getProfiles())
          .where((p) => ApiClient.normalizeServerUrl(p.url) == probe.url && p.hasSession)
          .firstOrNull;
      if (saved != null) {
        await SessionService.switchTo(saved);
        if (!mounted) return;
        Navigator.of(context).pushAndRemoveUntil(MaterialPageRoute(builder: (_) => const HomeShell()), (_) => false);
        return;
      }
      await SessionService.useServer(probe.url);
      if (!mounted) return;
      Navigator.of(context).push(MaterialPageRoute(builder: (_) => LoginScreen(serverInitialized: probe.initialized)));
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => typed ? _addressError = e.message : _foundError = e.message);
    } finally {
      if (mounted) setState(() => _checking = null);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    final busy = _checking != null;

    return AppScaffold.slivers(
      title: 'Connect to a server',
      // The first screen anyone sees: the NivaroOS mark over the title.
      leading: Navigator.canPop(context) ? null : const BrandMark(),
      collapsingTitle: true,
      onRefresh: _scan,
      slivers: [
        SliverToBoxAdapter(
          child: Padding(
            padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.sm),
            child: Text(
              'Pick your NivaroOS server, or type its address if it is somewhere else.',
              style: theme.textTheme.bodyLarge?.copyWith(color: scheme.onSurfaceVariant),
            ),
          ),
        ),
        SliverToBoxAdapter(
          child: SectionHeader(
            title: 'On this Wi-Fi',
            actionLabel: _scanning ? null : 'Scan again',
            onAction: _scanning || busy ? null : _scan,
          ),
        ),
        if (_found.isEmpty && _scanning)
          const SliverLoadingList(rows: 2)
        else if (_found.isEmpty)
          SliverToBoxAdapter(
            child: ListTile(
              leading: Icon(Icons.search_off_outlined, color: scheme.onSurfaceVariant),
              title: const Text('No servers found'),
              subtitle: const Text('Check that this phone is on the same Wi-Fi as the server. Some networks block discovery; type the address below instead.'),
              isThreeLine: true,
            ),
          )
        else
          SliverList.list(children: [
            for (final s in _found)
              ListTile(
                leading: const Icon(Icons.dns_outlined),
                title: Text(s.name.isNotEmpty ? s.name : s.host),
                subtitle: Text(s.port == 80 ? s.host : '${s.host}:${s.port}'),
                trailing: _checking == s.url
                    ? const SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2.5))
                    : const Icon(Icons.chevron_right),
                enabled: !busy || _checking == s.url,
                onTap: () => _connect(s.url, typed: false),
              ),
            if (_scanning)
              Padding(
                padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.sm),
                child: Semantics(
                  liveRegion: true,
                  label: 'Looking for more servers',
                  child: ExcludeSemantics(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('Looking for more servers…', style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant)),
                        const SizedBox(height: Space.sm),
                        const LinearProgressIndicator(),
                      ],
                    ),
                  ),
                ),
              ),
          ]),
        if (_foundError != null)
          SliverToBoxAdapter(child: _InlineError(message: _foundError!)),
        const SliverToBoxAdapter(child: SectionHeader(title: 'Or enter an address')),
        SliverToBoxAdapter(
          child: Padding(
            padding: EdgeInsets.symmetric(horizontal: gutter),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                TextField(
                  controller: _addressController,
                  keyboardType: TextInputType.url,
                  autocorrect: false,
                  enableSuggestions: false,
                  textInputAction: TextInputAction.go,
                  enabled: !busy || _checking == _addressController.text,
                  onSubmitted: (v) => _connect(v, typed: true),
                  onChanged: (_) {
                    if (_addressError != null) setState(() => _addressError = null);
                  },
                  decoration: InputDecoration(
                    labelText: 'Server address',
                    hintText: '192.168.1.20 or nas.example.com',
                    helperText: _addressError == null ? 'An IP address, a name, or a full https:// link' : null,
                    helperMaxLines: 3,
                    errorText: _addressError,
                    errorMaxLines: 5,
                    prefixIcon: const Icon(Icons.link_outlined),
                  ),
                ),
                const SizedBox(height: Space.lg),
                // With servers found, picking one is the main path: the
                // typed-address button steps back to tonal.
                ValueListenableBuilder(
                  valueListenable: _addressController,
                  builder: (context, value, _) {
                    final onPressed = busy ? null : () => _connect(_addressController.text, typed: true);
                    final style = FilledButton.styleFrom(minimumSize: const Size.fromHeight(48));
                    final child = _checking != null && _checking == value.text
                        ? const _ButtonProgress(label: 'Checking…')
                        : const Text('Continue');
                    return _found.isNotEmpty && value.text.trim().isEmpty
                        ? FilledButton.tonal(style: style.merge(tonalButtonStyle(context)), onPressed: onPressed, child: child)
                        : FilledButton(style: style, onPressed: onPressed, child: child);
                  },
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

class _InlineError extends StatelessWidget {
  const _InlineError({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Semantics(
      liveRegion: true,
      child: Padding(
        padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.sm, Space.gutter(context), 0),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(Icons.error_outline, size: 20, color: scheme.error),
            const SizedBox(width: Space.md),
            Expanded(child: Text(message, style: Theme.of(context).textTheme.bodyMedium?.copyWith(color: scheme.error))),
          ],
        ),
      ),
    );
  }
}

/// A small spinner and label inside a busy button.
class _ButtonProgress extends StatelessWidget {
  const _ButtonProgress({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    final color = DefaultTextStyle.of(context).style.color;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2, color: color)),
        const SizedBox(width: Space.md),
        Text(label),
      ],
    );
  }
}
