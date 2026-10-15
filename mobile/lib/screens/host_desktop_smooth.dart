import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import 'package:webview_flutter/webview_flutter.dart';

import '../services/api_client.dart';
import '../services/speedtest_service.dart' show isHomeNetworkHost;
import '../services/tailscale_service.dart';
import '../ui/ui.dart';
import '../widgets/rfb_view.dart';

// Host desktop "Smooth (beta)": the web dashboard's WebRTC viewer
// (`#/host-desktop/embed?mode=smooth`) in a WebView, inside the same
// session chrome as the native console. Shared contract:
// /root/nivaroos-wip/webrtc-contract.md (Embedding).

/// How the host desktop streams. Compatible is the native VNC console;
/// Auto and Smooth try WebRTC first and fall back to it.
enum HostStreamMode {
  auto('Auto', 'Smooth where it works, Compatible everywhere else'),
  smooth('Smooth (beta)', 'Video stream: smoother motion, needs your home network or Tailscale'),
  compatible('Compatible', 'The classic remote screen. Works over any connection');

  const HostStreamMode(this.label, this.detail);
  final String label;
  final String detail;

  /// The console pref it is remembered under.
  static const pref = 'stream@host';
}

/// The stream mode chooser, for the host desktop's More menu.
Future<HostStreamMode?> pickHostStreamMode(BuildContext context, HostStreamMode current) =>
    showModalBottomSheet<HostStreamMode>(
      context: context,
      showDragHandle: true,
      useSafeArea: true,
      builder: (context) => SingleChildScrollView(
        padding: const EdgeInsets.only(bottom: Space.lg),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: EdgeInsets.symmetric(horizontal: Space.gutter(context)),
              child: Semantics(
                header: true,
                child: Text('Stream mode', style: Theme.of(context).textTheme.titleLarge),
              ),
            ),
            const SizedBox(height: Space.sm),
            RadioGroup<HostStreamMode>(
              groupValue: current,
              onChanged: (m) => Navigator.of(context).pop(m),
              child: Column(children: [
                for (final m in HostStreamMode.values)
                  RadioListTile<HostStreamMode>(value: m, title: Text(m.label), subtitle: Text(m.detail)),
              ]),
            ),
          ],
        ),
      ),
    );

/// Why Smooth can't work over [baseUrl], or null: the video is UDP to the
/// server's home-network and Tailscale addresses only, so a server reached
/// through a Cloudflare tunnel or any other internet path can't get it.
String? smoothBlockedByRoute(String baseUrl) {
  final host = Uri.tryParse(baseUrl)?.host ?? '';
  if (host == 'localhost' || host == '127.0.0.1' || isHomeNetworkHost(host) || TailscaleService.isTailscaleAddress(baseUrl)) {
    return null;
  }
  return 'Smooth mode needs your home network or Tailscale, and this connection goes through the internet.';
}

/// [html] with the JWT set as `window.__nivaroosToken` first thing in
/// `<head>`, so it is there before any page script runs. Never in the URL.
String injectNivaroosToken(String html, String token) {
  final tag = '<script>window.__nivaroosToken=${_js(token)};</script>';
  final head = RegExp(r'<head[^>]*>', caseSensitive: false).firstMatch(html);
  return head == null ? '$tag$html' : html.replaceRange(head.end, head.end, tag);
}

// A JS string literal that can't close the <script> it sits in.
String _js(String s) => jsonEncode(s).replaceAll('<', r'<');

/// The Smooth session: the embed page full screen and immersive, with the
/// console's toolbar (keyboard, More, Pin, Hide, Disconnect). The page
/// reports its state on the `NivaroosBridge` channel; `fallback`, `error`,
/// a page that doesn't load or never goes live hand over to [onFallback]
/// with a sentence saying why. It stops streaming while the app is in the
/// background and keeps the page's token fresh.
class SmoothHostDesktop extends StatefulWidget {
  const SmoothHostDesktop({super.key, required this.onFallback, this.menuItems = const []});

  final ValueChanged<String> onFallback;
  final List<ConsoleMenuItem> menuItems;

  static const embedPath = '/#/host-desktop/embed?mode=smooth';

  /// The page falls back by itself within 4 s of an offer; this catches a
  /// server whose dashboard has no embed page (older NivaroOS) or a page
  /// that never says anything.
  static const startTimeout = Duration(seconds: 15);

  /// How often the token is checked; [ApiClient.freshToken] refreshes it
  /// 5 minutes before it expires.
  static const tokenCheck = Duration(minutes: 1);

  @override
  State<SmoothHostDesktop> createState() => _SmoothHostDesktopState();
}

class _SmoothHostDesktopState extends State<SmoothHostDesktop> with WidgetsBindingObserver {
  final _bar = GlobalKey<ConsoleSessionToolbarState>();
  final _sessionFocus = FocusNode(debugLabel: 'smooth session');
  final _keyboardFocus = FocusNode(debugLabel: 'smooth keyboard');
  late final WebViewController _web;
  Timer? _startTimer, _tokenTimer;
  String? _token;
  bool _live = false;
  bool _stopped = false;
  bool _done = false;
  bool _reauthed = false;

  String get _base => ApiClient.instance.baseUrl;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _keyboardFocus.addListener(() => setState(() {}));
    _web = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setBackgroundColor(Colors.black)
      ..addJavaScriptChannel('NivaroosBridge', onMessageReceived: (m) => _onBridge(m.message))
      ..setNavigationDelegate(NavigationDelegate(
        onNavigationRequest: _onNavigation,
        onWebResourceError: (e) {
          if (e.isForMainFrame ?? false) _fallback("Smooth mode couldn't load from the server.");
        },
      ));
    _tokenTimer = Timer.periodic(SmoothHostDesktop.tokenCheck, (_) => _checkToken());
    SystemChrome.setEnabledSystemUIMode(SystemUiMode.immersiveSticky);
    _load();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _startTimer?.cancel();
    _tokenTimer?.cancel();
    _sessionFocus.dispose();
    _keyboardFocus.dispose();
    // Unload the page so its peer closes now, not when the WebView is collected.
    unawaited(_web.loadHtmlString('<html></html>').catchError((_) {}));
    SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.hidden || state == AppLifecycleState.paused) {
      _stop();
    } else if (state == AppLifecycleState.resumed && _stopped) {
      _load();
    }
  }

  Future<void> _load() async {
    _stopped = false;
    if (_live) setState(() => _live = false);
    _startTimer?.cancel();
    _startTimer = Timer(SmoothHostDesktop.startTimeout, () => _fallback("Smooth mode didn't start in time."));
    try {
      final token = await ApiClient.instance.freshToken();
      if (token == null) {
        _fallback('Sign in again to use Smooth mode.');
        return;
      }
      final res = await http.get(Uri.parse('$_base/')).timeout(const Duration(seconds: 10));
      if (res.statusCode != 200) throw http.ClientException('HTTP ${res.statusCode}');
      if (!mounted || _stopped || _done) return;
      _token = token;
      await _web.loadHtmlString(injectNivaroosToken(utf8.decode(res.bodyBytes), token), baseUrl: '$_base${SmoothHostDesktop.embedPath}');
    } catch (e) {
      debugPrint('[smooth] load failed: $e');
      _fallback("Smooth mode couldn't load from the server.");
    }
  }

  /// Backgrounded: no stream, no encoder on the server.
  void _stop() {
    if (_stopped || _done) return;
    _stopped = true;
    _startTimer?.cancel();
    unawaited(_web.loadHtmlString('<html></html>').catchError((_) {}));
  }

  void _fallback(String reason) {
    if (_done || !mounted) return;
    _done = true;
    _startTimer?.cancel();
    _tokenTimer?.cancel();
    widget.onFallback(reason);
  }

  Future<void> _checkToken() async {
    final token = await ApiClient.instance.freshToken();
    if (token == null || token == _token || _stopped || _done || !mounted) return;
    _token = token;
    await _web.runJavaScript(
        'window.__nivaroosToken=${_js(token)};window.dispatchEvent(new Event("nivaroos-token"));');
  }

  NavigationDecision _onNavigation(NavigationRequest r) {
    final u = Uri.tryParse(r.url);
    final base = Uri.tryParse(_base);
    final same = u != null && base != null && u.host == base.host && u.port == base.port;
    return same || r.url.startsWith('about:') || r.url.startsWith('data:')
        ? NavigationDecision.navigate
        : NavigationDecision.prevent;
  }

  void _onBridge(String raw) {
    if (!mounted || _done) return;
    final Object? msg;
    try {
      msg = jsonDecode(raw);
    } catch (_) {
      return;
    }
    if (msg is! Map) return;
    final reason = '${msg['reason'] ?? ''}'.trim();
    if (msg['t'] == 'clip' && msg['text'] is String) return _copied(msg['text'] as String);
    switch (msg['state']) {
      case 'live':
        _startTimer?.cancel();
        _reauthed = false;
        setState(() => _live = true);
        _bar.currentState?.poke();
      case 'fallback':
        _fallback(reason.isEmpty ? "Smooth mode isn't available on the server." : reason);
      case 'error' when reason.toLowerCase().contains('session expired') && !_reauthed:
        // The token was refused: once, a fresh one and a reload.
        _reauthed = true;
        ApiClient.instance.refresh().then((r) {
          if (!mounted) return;
          r == RefreshResult.refreshed ? _load() : _fallback('Sign in again to use Smooth mode.');
        });
      case 'error':
        _fallback(reason.isEmpty ? 'Smooth mode stopped.' : reason);
    }
  }

  void _copied(String text) {
    if (!RemoteClipboardHistory.instance.record(text, ClipDirection.copied, RemoteClipboardHistory.hostTarget)) return;
    Clipboard.setData(ClipboardData(text: text));
    ScaffoldMessenger.maybeOf(context)?.showSnackBar(const SnackBar(content: Text('The server copied text to this phone')));
  }

  // The soft keyboard: keysyms into the page's input channel.
  void _key(int keysym) => unawaited(_web
      .runJavaScript('window.nivaroosEmbed&&(nivaroosEmbed.key($keysym,true),nivaroosEmbed.key($keysym,false));')
      .catchError((_) {}));

  void _disconnect() => Navigator.of(context).pop();

  @override
  Widget build(BuildContext context) {
    return Theme(
      data: RemoteConsoleFrameState.consoleTheme(context),
      child: Builder(builder: (context) {
        return PopScope(
          canPop: !_live,
          onPopInvokedWithResult: (didPop, _) async {
            if (!didPop && await confirmConsoleDisconnect(context, RemoteClipboardHistory.hostTarget) && mounted) {
              _disconnect();
            }
          },
          child: AnnotatedRegion<SystemUiOverlayStyle>(
            value: AppTheme.systemBarsStyle(Brightness.dark),
            child: Scaffold(
              backgroundColor: Colors.black,
              resizeToAvoidBottomInset: false,
              body: Focus(
                focusNode: _sessionFocus,
                autofocus: true,
                child: Stack(fit: StackFit.expand, children: [
                  Semantics(label: "The server's screen", child: WebViewWidget(controller: _web)),
                  RemoteKeyboardField(focusNode: _keyboardFocus, onKey: _key),
                  if (!_live)
                    ColoredBox(
                      color: Colors.black,
                      child: SafeArea(
                        child: ConsoleSessionMessage(
                          heading: 'Connecting to Host desktop',
                          detail: 'Smooth mode (beta)',
                          busy: true,
                          onClose: _disconnect,
                        ),
                      ),
                    ),
                  ConsoleSessionToolbar(
                    key: _bar,
                    kind: 'host',
                    visible: _live,
                    onDisconnect: _disconnect,
                    buttons: [
                      IconButton(
                        tooltip: _keyboardFocus.hasFocus ? 'Hide keyboard' : 'Show keyboard',
                        isSelected: _keyboardFocus.hasFocus,
                        icon: const Icon(Icons.keyboard_outlined),
                        selectedIcon: const Icon(Icons.keyboard_hide_outlined),
                        onPressed: () {
                          RemoteKeyboardField.toggle(context, _keyboardFocus, _sessionFocus);
                          _bar.currentState?.poke();
                        },
                      ),
                      MenuAnchor(
                        onOpen: () => _bar.currentState?.menuOpened(),
                        onClose: () => _bar.currentState?.menuClosed(),
                        builder: (context, controller, _) => IconButton(
                          tooltip: 'More options',
                          icon: const Icon(Icons.more_vert),
                          onPressed: () => controller.isOpen ? controller.close() : controller.open(),
                        ),
                        menuChildren: [
                          for (final item in widget.menuItems)
                            MenuItemButton(
                              leadingIcon: Icon(item.icon),
                              onPressed: item.onPressed == null ? null : () => item.onPressed!(context),
                              child: Text(item.label),
                            ),
                          if (widget.menuItems.isNotEmpty) const Divider(),
                          MenuItemButton(leadingIcon: const Icon(Icons.refresh), onPressed: _load, child: const Text('Reconnect')),
                        ],
                      ),
                    ],
                  ),
                ]),
              ),
            ),
          ),
        );
      }),
    );
  }
}
