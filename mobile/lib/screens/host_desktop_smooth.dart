import 'dart:async';
import 'dart:convert';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:webview_flutter/webview_flutter.dart';

import '../services/api_client.dart';
import '../services/server_trust.dart';
import '../services/speedtest_service.dart' show isHomeNetworkHost;
import '../services/tailscale_service.dart';
import '../ui/ui.dart';
import '../services/rfb_client.dart';
import '../widgets/console_toolbar.dart' show consolePref, saveConsolePref;
import '../widgets/rfb_view.dart';

// Host desktop over WebRTC (the default, Auto): the web dashboard's viewer
// (`#/host-desktop/embed?mode=smooth`) in a WebView, inside the same
// session chrome as the native console. Shared contract:
// /root/nivaroos-wip/webrtc-contract.md (Embedding).

/// How the host desktop streams. Compatible is the native VNC console;
/// Auto (the default) tries WebRTC first and falls back to it; WebRTC only
/// (`smooth`, the saved name) doesn't fall back by itself.
enum HostStreamMode {
  auto('Auto', 'WebRTC video where it works, Compatible everywhere else'),
  smooth('WebRTC only', 'Video stream: smooth motion, needs your home network or Tailscale'),
  compatible('Compatible (VNC)', 'The classic remote screen. Works over any connection');

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
  return 'WebRTC video needs your home network or Tailscale, and this connection goes through the internet.';
}

/// [html] with the JWT set as `window.__nivaroosToken` first thing in
/// `<head>`, so it is there before any page script runs. Never in the URL.
/// [stream] (`{preset, adapt, fps, maxKbps}`) goes with it as `window.__nivaroosStream`,
/// for the page's first offer.
String injectNivaroosToken(String html, String token, {Map<String, Object>? stream}) {
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
  String encoder = '';

  /// The preset streaming now (the network adaptation may have stepped
  /// below the chosen one), from the page's stats.
  String? preset;
  Rect? _placed;

  @override
  String get displayInfo => [
        '$width × $height',
        if (encoderLabel(encoder).isNotEmpty) encoderLabel(encoder),
        if (fps != null) '$fps fps',
        if (rttMs != null) '$rttMs ms',
      ].join(' · ');

  @override
  bool get localCursor => true;

  @override
  RemoteCursor? remoteCursor;
  int _cursorShapes = 0;

  /// The page's {"t":"cursor"} (the host's shape: a base64 PNG, hotspot
  /// x / y; w 0 = hidden). A newer one while this decodes wins.
  Future<void> _cursorShape(Map<Object?, Object?> msg) async {
    final n = ++_cursorShapes;
    final png = msg['png'];
    ui.Image? image;
    if ((msg['w'] as num? ?? 0) > 0 && png is String && png.isNotEmpty) {
      try {
        final frame = await (await ui.instantiateImageCodec(base64Decode(png))).getNextFrame();
        image = frame.image;
      } catch (_) {
        return; // not a picture: the last shape stays
      }
    }
    if (n != _cursorShapes) return;
    final x = (msg['x'] as num? ?? 0).toDouble(), y = (msg['y'] as num? ?? 0).toDouble();
    remoteCursor = RemoteCursor(image, Offset(x, y));
    frameCount.value++;
  }

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

  void setStream(SmoothStream s) => _call('setStream(${jsonEncode(s.toJson())})');

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
        encoder = '${msg['encoder'] ?? ''}';
        preset = msg['preset'] is String ? msg['preset'] as String : null;
        frameCount.value++;
      case 'cursor':
        unawaited(_cursorShape(msg));
    }
  }
}

/// Frame rates and bitrates the page accepts (ui streamMode.js).
const smoothFps = [30, 60];
const smoothKbps = [4000, 8000, 12000, 20000, 40000];

/// "GPU · NVENC" / "CPU · x264" for the server's encoder (ui streamMode.js).
String encoderLabel(String enc) =>
    const {'nvh264enc': 'GPU · NVENC', 'vah264enc': 'GPU · VA-API', 'x264enc': 'CPU · x264'}[enc] ?? '';

/// The stream quality presets (the server owns their numbers per GPU / CPU
/// encoder); custom = the raw frame rate and bitrate.
enum SmoothPreset {
  low('Low bandwidth'),
  balanced('Balanced'),
  high('High quality'),
  custom('Custom');

  const SmoothPreset(this.label);
  final String label;

  static SmoothPreset? byName(Object? n) => values.asNameMap()[n];
}

/// What the page streams: a preset (adapting to the network or not), or
/// custom fps / bitrate.
typedef SmoothStream = ({SmoothPreset preset, bool adapt, int fps, int maxKbps});

extension on SmoothStream {
  Map<String, Object> toJson() => {'preset': preset.name, 'adapt': adapt, 'fps': fps, 'maxKbps': maxKbps};
}

/// The saved stream settings. Never chosen: Balanced, adapting. Before
/// presets (fps / bitrate only), anything but 30 fps / 12 Mbps is Custom.
SmoothStream smoothStreamFromPrefs({String? preset, String? adapt, String? fps, String? kbps}) {
  final f = int.tryParse(fps ?? '');
  final k = int.tryParse(kbps ?? '');
  final fv = smoothFps.contains(f) ? f! : 30;
  final kv = smoothKbps.contains(k) ? k! : 12000;
  final p = SmoothPreset.byName(preset) ?? (preset == null && (fv != 30 || kv != 12000) ? SmoothPreset.custom : SmoothPreset.balanced);
  return (preset: p, adapt: adapt != 'off', fps: fv, maxKbps: kv);
}

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

  /// Console prefs the stream settings are remembered under.
  static const presetPref = 'preset@smooth';
  static const adaptPref = 'adapt@smooth';
  static const fpsPref = 'fps@smooth';
  static const kbpsPref = 'kbps@smooth';

  /// The page falls back by itself within 4 s of an offer; this catches a
  /// server whose dashboard has no embed page (older NivaroOS) or a page
  /// that never says anything.
  static const startTimeout = Duration(seconds: 15);

  /// A short trip out of the app (a message, the clipboard) keeps the
  /// stream; away longer, the page is unloaded so the server's encoder
  /// stops, and coming back loads it again.
  static const backgroundGrace = Duration(seconds: 90);

  /// How often the token is checked; [ApiClient.freshToken] refreshes it
  /// 5 minutes before it expires.
  static const tokenCheck = Duration(minutes: 1);

  @override
  State<SmoothHostDesktop> createState() => _SmoothHostDesktopState();
}

class _SmoothHostDesktopState extends State<SmoothHostDesktop> with WidgetsBindingObserver {
  late final WebViewController _web;
  late final EmbedRfbClient _client;
  Timer? _startTimer, _tokenTimer, _backgroundTimer;
  String? _token;
  SmoothStream _stream = (preset: SmoothPreset.balanced, adapt: true, fps: 30, maxKbps: 12000);
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
        // The server's own certificate (HTTPS on your network): the WebView
        // doesn't know the pinned CA, so it proceeds only for the exact
        // certificate a pinned-CA-verified connection gets from the server.
        onSslAuthError: (e) async {
          final ok = await ServerTrust.instance.matchesServer(Uri.parse(_base), e.certificate?.data);
          await (ok ? e.proceed() : e.cancel());
        },
        onWebResourceError: (e) {
          if (e.isForMainFrame ?? false) _fallback("WebRTC video couldn't load from the server.");
        },
      ));
    _client = EmbedRfbClient(_web, load: _load, unload: _unload);
    _tokenTimer = Timer.periodic(SmoothHostDesktop.tokenCheck, (_) => _checkToken());
    _start();
  }

  Future<void> _start() async {
    _stream = smoothStreamFromPrefs(
      preset: await consolePref(SmoothHostDesktop.presetPref),
      adapt: await consolePref(SmoothHostDesktop.adaptPref),
      fps: await consolePref(SmoothHostDesktop.fpsPref),
      kbps: await consolePref(SmoothHostDesktop.kbpsPref),
    );
    if (mounted) unawaited(_client.connect());
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _startTimer?.cancel();
    _tokenTimer?.cancel();
    _backgroundTimer?.cancel();
    _done = true;
    _client.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.hidden || state == AppLifecycleState.paused) {
      _backgroundTimer ??= Timer(SmoothHostDesktop.backgroundGrace, _stop);
    } else if (state == AppLifecycleState.resumed) {
      _backgroundTimer?.cancel();
      _backgroundTimer = null;
      if (_stopped && !_done) unawaited(_client.connect());
    }
  }

  void _unload() {
    _startTimer?.cancel();
    unawaited(_web.loadHtmlString('<html></html>').catchError((_) {}));
  }

  Future<void> _load() async {
    _stopped = false;
    _startTimer?.cancel();
    _startTimer = Timer(SmoothHostDesktop.startTimeout, () => _fallback("WebRTC video didn't start in time."));
    try {
      final token = await ApiClient.instance.freshToken();
      if (token == null) {
        _fallback('Sign in again to use WebRTC video.');
        return;
      }
      final res = await http.get(Uri.parse('$_base/')).timeout(const Duration(seconds: 10));
      if (res.statusCode != 200) throw http.ClientException('HTTP ${res.statusCode}');
      if (!mounted || _stopped || _done) return;
      _token = token;
      await _web.loadHtmlString(
        injectNivaroosToken(utf8.decode(res.bodyBytes), token, stream: _stream.toJson()),
        baseUrl: '$_base${SmoothHostDesktop.embedPath}',
      );
    } catch (e) {
      debugPrint('[smooth] load failed: $e');
      _fallback("WebRTC video couldn't load from the server.");
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
    if (msg['t'] == 'adapt') return _adapted(msg);
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
        _fallback(reason.isEmpty ? "WebRTC video isn't available on the server." : reason);
      case 'error' when reason.toLowerCase().contains('session expired') && !_reauthed:
        // The token was refused: once, a fresh one and a reload.
        _reauthed = true;
        ApiClient.instance.refresh().then((r) {
          if (!mounted) return;
          r == RefreshResult.refreshed ? _client.connect() : _fallback('Sign in again to use WebRTC video.');
        });
      case 'error':
        _fallback(reason.isEmpty ? 'WebRTC video stopped.' : reason);
    }
  }

  /// The page's network adaptation stepped the preset down or back up.
  void _adapted(Map<Object?, Object?> msg) {
    final p = SmoothPreset.byName(msg['preset']);
    if (p == null) return;
    final text = msg['dir'] == 'down' ? 'The network is slow - switched to ${p.label}' : 'The network is stable again - back to ${p.label}';
    ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(text)));
  }

  void _setStream({SmoothPreset? preset, bool? adapt, int? fps, int? kbps}) {
    final s = _stream;
    setState(() => _stream = (preset: preset ?? s.preset, adapt: adapt ?? s.adapt, fps: fps ?? s.fps, maxKbps: kbps ?? s.maxKbps));
    unawaited(saveConsolePref(SmoothHostDesktop.presetPref, _stream.preset.name));
    unawaited(saveConsolePref(SmoothHostDesktop.adaptPref, _stream.adapt ? 'on' : 'off'));
    unawaited(saveConsolePref(SmoothHostDesktop.fpsPref, '${_stream.fps}'));
    unawaited(saveConsolePref(SmoothHostDesktop.kbpsPref, '${_stream.maxKbps}'));
    _client.setStream(_stream);
  }

  // In the Display menu where the VNC console has its picture qualities:
  // the presets, then (Custom) the raw frame rate and bitrate.
  List<Widget> _qualityItems(BuildContext context) {
    final s = _stream;
    return [
      for (final p in SmoothPreset.values)
        RadioMenuButton<SmoothPreset>(
          value: p,
          groupValue: s.preset,
          onChanged: (v) => _setStream(preset: v),
          // Kept current while the menu is open, like its header.
          child: ListenableBuilder(
            listenable: _client.frameCount,
            builder: (context, _) {
              final now = SmoothPreset.byName(_client.preset);
              return Text(p == s.preset && now != null && now != p && p != SmoothPreset.custom
                  ? '${p.label} · now ${now.label} (network)'
                  : p.label);
            },
          ),
        ),
      if (s.preset == SmoothPreset.custom) ...[
        for (final f in smoothFps)
          RadioMenuButton<int>(
            value: f,
            groupValue: s.fps,
            onChanged: (v) => _setStream(fps: v),
            child: Text(f == 60 ? '60 fps · smoothest' : '30 fps · lighter'),
          ),
        SubmenuButton(
          leadingIcon: const Icon(Icons.speed_outlined),
          menuChildren: [
            for (final k in smoothKbps)
              RadioMenuButton<int>(
                value: k,
                groupValue: s.maxKbps,
                onChanged: (v) => _setStream(kbps: v),
                child: Text('${k ~/ 1000} Mbps${k == 12000 ? ' · default' : ''}'),
              ),
          ],
          child: Text('Max bitrate · ${s.maxKbps ~/ 1000} Mbps'),
        ),
      ] else
        CheckboxMenuButton(
          value: s.adapt,
          onChanged: (v) => _setStream(adapt: v ?? true),
          child: const Text('Adapt to the network'),
        ),
    ];
  }

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
