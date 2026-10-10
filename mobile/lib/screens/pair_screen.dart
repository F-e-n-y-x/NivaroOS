import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../models/server_profile.dart';
import '../services/api_client.dart';
import '../services/device_sync_service.dart';
import '../services/session_service.dart';
import '../ui/ui.dart';
import '../utils/pair_link.dart';
import 'home_shell.dart';

/// Add a server with the one-time code the web shows under Settings >
/// Companion Devices > Pair a phone: scan its QR code (the server's
/// addresses + the code) or type the address and code. The code becomes a
/// normal session of the account that showed it - no password typed here.
///
/// By default the phone then uses that server ([DiscoveryScreen]). With
/// [returnProfile] it pops the new [ServerProfile] instead (Servers > Add).
class PairScreen extends StatefulWidget {
  const PairScreen({super.key, this.initialLink, this.returnProfile = false});

  /// A scanned code to pair with right away.
  final PairLink? initialLink;
  final bool returnProfile;

  @override
  State<PairScreen> createState() => _PairScreenState();
}

class _PairScreenState extends State<PairScreen> {
  final _address = TextEditingController();
  final _code = TextEditingController();

  /// The scanned addresses, tried in order (while the address field still
  /// shows the first of them).
  List<String> _scanned = const [];
  bool _busy = false;
  String? _addressError;
  String? _codeError;
  String? _error;

  @override
  void initState() {
    super.initState();
    final link = widget.initialLink;
    if (link != null) {
      _use(link);
      WidgetsBinding.instance.addPostFrameCallback((_) => _pair());
    }
  }

  @override
  void dispose() {
    _address.dispose();
    _code.dispose();
    super.dispose();
  }

  void _use(PairLink link) {
    _scanned = link.urls;
    _address.text = link.urls.isEmpty ? '' : link.urls.first;
    _code.text = PairLink.formatCode(link.code);
    _addressError = _codeError = _error = null;
  }

  Future<void> _scan() async {
    final link = await Navigator.of(context).push<PairLink>(MaterialPageRoute(builder: (_) => const PairScanScreen()));
    if (link == null || !mounted) return;
    setState(() => _use(link));
    await _pair();
  }

  Future<String> _deviceName() async {
    try {
      final d = await DeviceSyncService.instance.getLocalDeviceInfo();
      return d.name.isNotEmpty ? d.name : d.model;
    } catch (_) {
      return '';
    }
  }

  Future<void> _pair() async {
    if (_busy) return;
    final typed = _address.text.trim();
    final candidates = _scanned.isNotEmpty && typed == _scanned.first ? _scanned : [if (typed.isNotEmpty) typed];
    setState(() {
      _addressError = candidates.isEmpty ? 'Enter the server address.' : null;
      _codeError = PairLink.isCode(_code.text) ? null : 'Enter the 8-character code shown on the web.';
      _error = null;
    });
    if (_addressError != null || _codeError != null) return;
    setState(() => _busy = true);
    try {
      // The first address that answers as NivaroOS (home Wi-Fi, Tailscale,
      // a public name...).
      String? url;
      ApiException? first;
      for (final c in candidates) {
        try {
          url = (await ApiClient.probe(c, timeout: const Duration(seconds: 5))).url;
          break;
        } on ApiException catch (e) {
          first ??= e;
        }
      }
      if (!mounted) return;
      if (url == null) {
        setState(() => _addressError = first?.message ?? 'Couldn’t reach the server.');
        return;
      }
      final t = await ApiClient.redeemPairCode(url, PairLink.normalizeCode(_code.text), device: await _deviceName());
      if (!mounted) return;
      if (widget.returnProfile) {
        Navigator.of(context).pop(ServerProfile(
          id: 'srv_${clock.now().millisecondsSinceEpoch}',
          name: ServerProfile.defaultName(url),
          url: url,
          username: t.username,
          accessToken: t.accessToken,
          refreshToken: t.refreshToken,
        ));
        return;
      }
      await SessionService.useServer(url);
      await SessionService.signedIn(accessToken: t.accessToken, refreshToken: t.refreshToken, username: t.username);
      if (!mounted) return;
      Navigator.of(context).pushAndRemoveUntil(MaterialPageRoute(builder: (_) => const HomeShell()), (_) => false);
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => e.kind == ApiErrorKind.auth || e.kind == ApiErrorKind.rateLimited ? _codeError = e.message : _error = e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    return AppScaffold(
      title: 'Pair with a code',
      body: ListView(
        padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.xl),
        children: [
          Text(
            'On the web, open Settings › Companion Devices › Pair a phone. Scan the QR code it shows, or type the address and code.',
            style: theme.textTheme.bodyLarge?.copyWith(color: scheme.onSurfaceVariant),
          ),
          const SizedBox(height: Space.lg),
          FilledButton.tonalIcon(
            style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)).merge(tonalButtonStyle(context)),
            onPressed: _busy ? null : _scan,
            icon: const Icon(Icons.qr_code_scanner_outlined),
            label: const Text('Scan QR code'),
          ),
          const SizedBox(height: Space.xl),
          TextField(
            controller: _address,
            enabled: !_busy,
            keyboardType: TextInputType.url,
            autocorrect: false,
            enableSuggestions: false,
            textInputAction: TextInputAction.next,
            onChanged: (_) {
              if (_addressError != null) setState(() => _addressError = null);
            },
            decoration: InputDecoration(
              labelText: 'Server address',
              hintText: '192.168.1.20 or nas.example.com',
              helperText: _scanned.length > 1 && _address.text.trim() == _scanned.first ? 'From the QR code, with ${_scanned.length - 1} more to try' : null,
              errorText: _addressError,
              errorMaxLines: 5,
              prefixIcon: const Icon(Icons.link_outlined),
            ),
          ),
          const SizedBox(height: Space.lg),
          TextField(
            controller: _code,
            enabled: !_busy,
            autocorrect: false,
            enableSuggestions: false,
            textCapitalization: TextCapitalization.characters,
            maxLength: 9,
            style: const TextStyle(fontFamily: 'GeistMono', letterSpacing: 2),
            textInputAction: TextInputAction.done,
            onSubmitted: (_) => _pair(),
            onChanged: (_) {
              if (_codeError != null) setState(() => _codeError = null);
            },
            decoration: InputDecoration(
              labelText: 'Pairing code',
              hintText: 'ABCD-EFGH',
              counterText: '',
              helperText: _codeError == null ? 'Works once, for 5 minutes' : null,
              errorText: _codeError,
              errorMaxLines: 4,
              prefixIcon: const Icon(Icons.password_outlined),
            ),
          ),
          if (_error != null) ...[
            const SizedBox(height: Space.lg),
            Semantics(
              liveRegion: true,
              child: Card.filled(
                color: scheme.errorContainer,
                child: Padding(
                  padding: const EdgeInsets.all(Space.md),
                  child: Text(_error!, style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onErrorContainer)),
                ),
              ),
            ),
          ],
          const SizedBox(height: Space.xl),
          FilledButton(
            style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
            onPressed: _busy ? null : _pair,
            child: _busy
                ? Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2, color: scheme.onSurface.withValues(alpha: 0.38))),
                      const SizedBox(width: Space.md),
                      const Text('Pairing…'),
                    ],
                  )
                : const Text('Pair'),
          ),
        ],
      ),
    );
  }
}

/// The camera, looking for a pairing QR code; pops the [PairLink].
class PairScanScreen extends StatefulWidget {
  const PairScanScreen({super.key});

  @override
  State<PairScanScreen> createState() => _PairScanScreenState();
}

class _PairScanScreenState extends State<PairScanScreen> {
  bool _done = false;
  bool _foreign = false;

  void _detect(BarcodeCapture capture) {
    if (_done) return;
    for (final b in capture.barcodes) {
      final link = PairLink.parse(b.rawValue ?? '');
      if (link != null) {
        _done = true;
        Navigator.of(context).pop(link);
        return;
      }
    }
    if (!_foreign && mounted) setState(() => _foreign = true);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return AppScaffold(
      title: 'Scan the QR code',
      body: Column(
        children: [
          Expanded(
            child: MobileScanner(
              onDetect: _detect,
              errorBuilder: (context, error) => Center(
                child: Padding(
                  padding: const EdgeInsets.all(Space.xl),
                  child: Text(
                    error.errorCode == MobileScannerErrorCode.permissionDenied
                        ? 'The app may not use the camera. Allow it in the phone’s settings, or go back and type the code.'
                        : 'The camera didn’t start. Go back and type the code instead.',
                    textAlign: TextAlign.center,
                    style: theme.textTheme.bodyLarge,
                  ),
                ),
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.all(Space.lg),
            child: Semantics(
              liveRegion: true,
              child: Text(
                _foreign ? 'That QR code isn’t a NivaroOS pairing code.' : 'Point the camera at the code in Settings › Companion Devices › Pair a phone.',
                textAlign: TextAlign.center,
                style: theme.textTheme.bodyMedium?.copyWith(color: _foreign ? scheme.error : scheme.onSurfaceVariant),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
