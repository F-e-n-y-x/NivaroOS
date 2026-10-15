import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:webview_flutter/webview_flutter.dart';

import '../services/api_client.dart';
import '../services/speedtest_service.dart' show isHomeNetworkHost;
import '../services/tailscale_service.dart';
import '../ui/ui.dart';
import '../services/rfb_client.dart';
import '../widgets/console_toolbar.dart' show consolePref, saveConsolePref;
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
/// [stream] (`{fps, maxKbps}`) goes with it as `window.__nivaroosStream`,
/// for the page's first offer.
String injectNivaroosToken(String html, String token, {Map<String, int>? stream}) {
  final tag = '<script>window.__nivaroosToken=${_js(token)};'
      '${stream == null ? '' : 'window.__nivaroosStream=${jsonEncode(stream)};'}</script>';
  final head = RegExp(r'<head[^>]*>', caseSensitive: false).firstMatch(html);
  return head == null ? '$tag$html' : html.replaceRange(head.end, head.end, tag);
}

// A JS string literal that can't close the <script> it sits in.
String _js(String s) => jsonEncode(s).replaceAll('<', r'\u003c');

/// The Smooth stream as an [RfbClient], so the native console
/// ([RemoteConsoleFrame]) runs it unchanged: its gestures, keys, latches,
/// clipboard and menus go to the page through `window.nivaroosEmbed`, and
/// the page only draws the video, where [place] says. Input is the app's
/// own rather than the page's touch handling so both lanes behave the
/// same (tap, hold to right-click, two-finger scroll, pinch, the mouse
/// pointer mode).
class EmbedRfbClient extends RfbClient {
  EmbedRfbClient(this.web, {required this.load, required this.unload});

  final WebViewController web;

  /// Loads the page (a new session); [unload] blanks it (the peer closes).
  final VoidCallback load;
  final VoidCallback unload;

  int? fps;
  int? rttMs;
  Rect? _placed;

  @override
  String get displayInfo => [
        '$width × $height',
        if (fps != null) '$fps fps',
        if (rttMs != null) '$rttMs ms',
      ].join(' · ');

  @override
  bool get localCursor => true;

  void _call(String js) => unawaited(web.runJavaScript('window.nivaroosEmbed&&nivaroosEmbed.$js;').catchError((_) {}));

  @override
  Future<void> connect() async {
    status.value = RfbStatus.connecting;
    load();
  }

  @override
  void close() {
    unload();
    status.value = RfbStatus.idle;
  }

  @override
  void sendPointer(int x, int y, int buttonMask) {
    if (!isConnected || width <= 0 || height <= 0) return;
    _call('pointer(${x.clamp(0, width - 1)},${y.clamp(0, height - 1)},$buttonMask)');
  }

  @override
  void sendKey(int keysym, bool down) {
    if (isConnected) _call('key($keysym,$down)');
  }

  /// Lossless: the page's data channel carries UTF-8.
  @override
  bool sendClipboard(String text) {
    if (!isConnected) return false;
    _call('clip(${_js(text)})');
    return true;
  }

  void setStream(int fps, int maxKbps) => _call('setStream({fps:$fps,maxKbps:$maxKbps})');

  /// The console's fit, zoom and pan: where the page draws the video, in
  /// its CSS pixels (= logical pixels; the WebView fills the same box).
  void place(Rect r) {
    _placed = r;
    _call('view({x:${r.left.toStringAsFixed(1)},y:${r.top.toStringAsFixed(1)},'
        'w:${r.width.toStringAsFixed(1)},h:${r.height.toStringAsFixed(1)}})');
  }

  /// The page (re)loaded: it needs the placement again.
  void replace() {
    final r = _placed;
    if (r != null) place(r);
  }

  /// A NivaroosBridge message that isn't a state. Size and stats bump
  /// [frameCount], which the console redraws on.
  void bridge(Map<Object?, Object?> msg) {
    switch (msg['t']) {
      case 'clip' when msg['text'] is String:
        remoteCopied(msg['text'] as String);
      case 'size' when msg['w'] is int && msg['h'] is int:
        width = msg['w'] as int;
        height = msg['h'] as int;
        frameCount.value++;
      case 'stats':
        fps = (msg['fps'] as num?)?.round();
        rttMs = (msg['rttMs'] as num?)?.round();
        frameCount.value++;
    }
  }
}

/// Frame rates and bitrates the page accepts (ui streamMode.js).
const smoothFps = [30, 60];
const smoothKbps = [4000, 8000, 12000, 20000, 40000];

/// The Smooth session: the native console ([RemoteConsoleFrame]) over the
/// embed page, with the frame rate and bitrate in its Display menu where
/// the VNC qualities would be. The page reports its state on the
/// `NivaroosBridge` channel; `fallback`, `error`, a page that doesn't load
/// or never goes live hand over to [onFallback] with a sentence saying
/// why. It stops streaming while the app is in the background and keeps
/// the page's token fresh.
class SmoothHostDesktop extends StatefulWidget {
  const SmoothHostDesktop({
    super.key,
    required this.onFallback,
    this.menuItems = const [],
    this.onMatchPhone,
    this.history,
  });

  final ValueChanged<String> onFallback;
  final List<ConsoleMenuItem> menuItems;
  final Future<void> Function(BuildContext context)? onMatchPhone;
  final RemoteClipboardHistory? history;

  static const embedPath = '/#/host-desktop/embed?mode=smooth';

  /// Console prefs the frame rate and bitrate are remembered under.
  static const fpsPref = 'fps@smooth';
  static const kbpsPref = 'kbps@smooth';

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
  late final WebViewController _web;
  late final EmbedRfbClient _client;
  Timer? _startTimer, _tokenTimer;
  String? _token;
  int _fps = 30;
  int _kbps = 12000;
  bool _stopped = false;
  bool _done = false;
  bool _reauthed = false;

  String get _base => ApiClient.instance.baseUrl;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
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
    _client = EmbedRfbClient(_web, load: _load, unload: _unload);
    _tokenTimer = Timer.periodic(SmoothHostDesktop.tokenCheck, (_) => _checkToken());
    _start();
  }

  Future<void> _start() async {
    final fps = int.tryParse(await consolePref(SmoothHostDesktop.fpsPref) ?? '');
    final kbps = int.tryParse(await consolePref(SmoothHostDesktop.kbpsPref) ?? '');
    if (smoothFps.contains(fps)) _fps = fps!;
    if (smoothKbps.contains(kbps)) _kbps = kbps!;
    if (mounted) unawaited(_client.connect());
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _startTimer?.cancel();
    _tokenTimer?.cancel();
    _done = true;
    _client.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.hidden || state == AppLifecycleState.paused) {
      _stop();
    } else if (state == AppLifecycleState.resumed && _stopped && !_done) {
      unawaited(_client.connect());
    }
  }

  void _unload() {
    _startTimer?.cancel();
    unawaited(_web.loadHtmlString('<html></html>').catchError((_) {}));
  }

  Future<void> _load() async {
    _stopped = false;
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
      await _web.loadHtmlString(
        injectNivaroosToken(utf8.decode(res.bodyBytes), token, stream: {'fps': _fps, 'maxKbps': _kbps}),
        baseUrl: '$_base${SmoothHostDesktop.embedPath}',
      );
    } catch (e) {
      debugPrint('[smooth] load failed: $e');
      _fallback("Smooth mode couldn't load from the server.");
    }
  }

  /// Backgrounded: no stream, no encoder on the server.
  void _stop() {
    if (_stopped || _done) return;
    _stopped = true;
    _client.close();
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
    if (msg['t'] != null) return _client.bridge(msg);
    final reason = '${msg['reason'] ?? ''}'.trim();
    switch (msg['state']) {
      case 'live':
        _startTimer?.cancel();
        _reauthed = false;
        _client.replace();
        _client.status.value = RfbStatus.connected;
      case 'connecting' when _client.isConnected:
        // A live stream dropped; the page offers again by itself.
        _client.status.value = RfbStatus.connecting;
      case 'fallback':
        _fallback(reason.isEmpty ? "Smooth mode isn't available on the server." : reason);
      case 'error' when reason.toLowerCase().contains('session expired') && !_reauthed:
        // The token was refused: once, a fresh one and a reload.
        _reauthed = true;
        ApiClient.instance.refresh().then((r) {
          if (!mounted) return;
          r == RefreshResult.refreshed ? _client.connect() : _fallback('Sign in again to use Smooth mode.');
        });
      case 'error':
        _fallback(reason.isEmpty ? 'Smooth mode stopped.' : reason);
    }
  }

  void _setStream({int? fps, int? kbps}) {
    setState(() {
      _fps = fps ?? _fps;
      _kbps = kbps ?? _kbps;
    });
    unawaited(saveConsolePref(SmoothHostDesktop.fpsPref, '$_fps'));
    unawaited(saveConsolePref(SmoothHostDesktop.kbpsPref, '$_kbps'));
    _client.setStream(_fps, _kbps);
  }

  // In the Display menu where the VNC console has its picture qualities.
  List<Widget> _qualityItems(BuildContext context) => [
        for (final f in smoothFps)
          RadioMenuButton<int>(
            value: f,
            groupValue: _fps,
            onChanged: (v) => _setStream(fps: v),
            child: Text(f == 60 ? '60 fps · smoothest' : '30 fps · lighter'),
          ),
        SubmenuButton(
          leadingIcon: const Icon(Icons.speed_outlined),
          menuChildren: [
            for (final k in smoothKbps)
              RadioMenuButton<int>(
                value: k,
                groupValue: _kbps,
                onChanged: (v) => _setStream(kbps: v),
                child: Text('${k ~/ 1000} Mbps${k == 12000 ? ' · default' : ''}'),
              ),
          ],
          child: Text('Max bitrate · ${_kbps ~/ 1000} Mbps'),
        ),
      ];

  @override
  Widget build(BuildContext context) => RemoteConsoleFrame(
        client: _client,
        title: 'Host desktop',
        screenLabel: "The server's screen",
        clipboardTarget: RemoteClipboardHistory.hostTarget,
        history: widget.history,
        menuItems: widget.menuItems,
        onMatchPhone: widget.onMatchPhone,
        underlay: ExcludeSemantics(child: WebViewWidget(controller: _web)),
        onPicture: _client.place,
        qualityItems: _qualityItems,
      );
}
