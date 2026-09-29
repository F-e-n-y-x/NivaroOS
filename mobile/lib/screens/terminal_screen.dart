import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:xterm/xterm.dart';

import '../services/api_client.dart';
import '../services/storage_service.dart';
import '../services/terminal_protocol.dart';
import '../services/terminal_sessions.dart';
import '../ui/ui.dart';
import '../widgets/terminal_surface.dart';
import 'terminal_sessions_screen.dart';

export '../services/terminal_protocol.dart';

/// The ANSI palette, from a dark theme's roles and status colours, so the
/// terminal matches the app's style (Rack, Tonal, Console, true black)
/// instead of carrying its own hex values.
TerminalTheme terminalThemeFor(ColorScheme s, StatusColors st) => TerminalTheme(
      cursor: s.primary,
      selection: s.primary.withValues(alpha: 0.35),
      foreground: s.onSurface,
      background: s.surfaceContainerLowest,
      black: s.surfaceContainerHighest,
      red: s.error,
      green: st.success.color,
      yellow: st.warning.color,
      blue: s.primary,
      magenta: s.tertiary,
      cyan: st.info.color,
      white: s.onSurfaceVariant,
      brightBlack: s.outline,
      brightRed: s.onErrorContainer,
      brightGreen: st.success.onContainer,
      brightYellow: st.warning.onContainer,
      brightBlue: s.onPrimaryContainer,
      brightMagenta: s.onTertiaryContainer,
      brightCyan: st.info.onContainer,
      brightWhite: s.onSurface,
      searchHitBackground: st.warning.container,
      searchHitBackgroundCurrent: st.success.container,
      searchHitForeground: s.onSurface,
    );

/// The dark theme the terminal is drawn in: the app's own when it is dark
/// (so true black stays black), otherwise the dark theme of the same style
/// and accent.
ThemeData terminalAppTheme(BuildContext context) {
  final theme = Theme.of(context);
  if (theme.brightness == Brightness.dark) return theme;
  final direction = theme.extension<DesignTokens>()?.direction ?? DesignDirection.defaultDirection;
  return AppTheme.build(brightness: Brightness.dark, accent: ThemeController.instance.value.accent, direction: direction);
}

enum _Phase {
  /// Creating the session on the server.
  starting,

  /// Opening the WebSocket.
  connecting,

  /// Replaying the scrollback (between `hello` and `live`).
  restoring,
  live,

  /// The link dropped; the session is still on the server. Retrying.
  reconnecting,

  /// The shell ended (`exit`), or the old-style session closed.
  ended,

  /// The server doesn't know the session (it ended, or the server
  /// restarted).
  gone,

  /// Something else went wrong; [_message] says what.
  failed,
}

/// A shell on the server, or in an app's container, that keeps running on
/// the server when this screen closes (docs/specs/2026-09-29-terminal-sessions.md).
///
/// - With [session]: reattaches to it; the server replays its recent
///   output, then streams live.
/// - Otherwise starts a new one: on the server, or in [container].
///
/// A dropped connection reattaches by itself. On a server without
/// sessions (an older NivaroOS) it falls back to the old plain-connect
/// route, where the shell ends with the screen.
class TerminalScreen extends StatefulWidget {
  const TerminalScreen({
    super.key,
    this.session,
    this.container,
    this.title,
    this.subtitle,
    this.initCommand,
    this.connector,
  });

  /// The session to reattach to.
  final TerminalSession? session;

  /// A new shell in this container (name or id). Ignored with [session].
  final String? container;

  /// Shown until the server names the session (the app's name for a
  /// container shell).
  final String? title;

  /// Under the title while connected; otherwise where the shell runs.
  final String? subtitle;

  /// Typed into a new shell once it is live.
  final String? initCommand;

  /// Opens the WebSocket; tests pass a fake.
  final TerminalConnector? connector;

  @override
  State<TerminalScreen> createState() => _TerminalScreenState();
}

class _TerminalScreenState extends State<TerminalScreen> with WidgetsBindingObserver {
  late Terminal _terminal = _newTerminal();
  final _controller = TerminalController();
  final _scroll = ScrollController();
  final _focus = FocusNode();
  final _surface = GlobalKey<TerminalSurfaceState>();

  TerminalSession? _session;
  _Phase _phase = _Phase.starting;
  String? _message;

  /// On an older server: the plain-connect route, no session.
  bool _legacy = false;

  TerminalTransport? _ws;
  StreamSubscription<dynamic>? _sub;
  TerminalOutputDecoder? _decoder;
  int _gen = 0;
  bool _exited = false;
  bool _sentInit = false;
  bool _everLive = false;

  Timer? _retryTimer;
  int _retries = 0;
  int _retryIn = 0;
  Timer? _countdown;

  Timer? _resizeTimer;
  bool _ctrl = false;
  bool _alt = false;
  double _fontSize = TerminalFontSize.fallback;

  ScaffoldMessengerState? _messenger;

  TerminalSessionsApi get _api => TerminalSessionsApi.instance;
  TerminalFamily get _family => widget.session?.family ?? (widget.container == null ? TerminalFamily.host : TerminalFamily.container);

  bool get _connected => _phase == _Phase.live || _phase == _Phase.restoring;

  @override
  void initState() {
    super.initState();
    _session = widget.session;
    WidgetsBinding.instance.addObserver(this);
    StorageService.instance.getTerminalFontSize().then((v) {
      if (v != null && mounted) setState(() => _fontSize = TerminalFontSize.clamp(v));
    });
    // After the first layout, so the terminal knows its size and the shell
    // starts at it.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _start();
    });
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _messenger = ScaffoldMessenger.maybeOf(context);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _gen++;
    _retryTimer?.cancel();
    _countdown?.cancel();
    _resizeTimer?.cancel();
    _sub?.cancel();
    // Detach: the session keeps running on the server.
    _ws?.close();
    _focus.dispose();
    _controller.dispose();
    _scroll.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // Back from the background: the phone may have dropped the socket.
    if (state == AppLifecycleState.resumed && _phase == _Phase.reconnecting) _retryNow();
  }

  Terminal _newTerminal({int? cols, int? rows}) {
    final t = Terminal(maxLines: 10000);
    t.onOutput = _onInput;
    t.onResize = (cols, rows, _, _) => _queueResize();
    if (cols != null && rows != null && cols > 0 && rows > 0) t.resize(cols, rows);
    return t;
  }

  int get _cols => _terminal.viewWidth > 0 ? _terminal.viewWidth : 80;
  int get _rows => _terminal.viewHeight > 0 ? _terminal.viewHeight : 24;

  void _queueResize() {
    _resizeTimer?.cancel();
    _resizeTimer = Timer(const Duration(milliseconds: 80), _sendResize);
  }

  void _sendResize() {
    if (_connected && _terminal.viewWidth > 0 && _terminal.viewHeight > 0) {
      _ws?.add(Wsterm.resize(_terminal.viewWidth, _terminal.viewHeight));
    }
  }

  // --- connecting ---------------------------------------------------------

  Future<void> _start() async {
    final s = _session;
    if (s != null) return _attach(s);
    return _create();
  }

  Future<void> _create() async {
    final gen = ++_gen;
    setState(() {
      _phase = _Phase.starting;
      _message = null;
    });
    if (ApiClient.instance.baseUrl.isEmpty) return _fail(gen, 'No server is set up');
    try {
      final s = await _api.create(_family, cols: _cols, rows: _rows, container: widget.container);
      if (!mounted || gen != _gen) return;
      _session = s;
      await _attach(s);
    } on TerminalSessionsUnsupported {
      if (!mounted || gen != _gen) return;
      _legacy = true;
      await _connectLegacy();
    } on ApiException catch (e) {
      if (!mounted || gen != _gen) return;
      if (e.statusCode == 429) {
        setState(() {
          _phase = _Phase.failed;
          _message = 'You have the most terminals the server allows running. End one to start another.';
        });
      } else if (e.statusCode == 409) {
        _fail(gen, "${widget.title ?? 'The app'} isn't running, so there is no shell to open. Start it first.");
      } else {
        _fail(gen, e.message);
      }
    } catch (e) {
      _fail(gen, "Couldn't start the terminal");
    }
  }

  Future<void> _attach(TerminalSession s, {bool reconnect = false}) async {
    final gen = ++_gen;
    _retryTimer?.cancel();
    _countdown?.cancel();
    await _drop();
    if (!mounted || gen != _gen) return;
    setState(() {
      _phase = reconnect || _everLive ? _Phase.reconnecting : _Phase.connecting;
      _retryIn = 0;
      _message = null;
    });
    try {
      final headers = await ApiClient.instance.authHeaders();
      final uri = ApiClient.instance.webSocketUri(s.attachPath, {'cols': '$_cols', 'rows': '$_rows'});
      final ws = await (widget.connector ?? IoTerminalTransport.connect)(uri, headers);
      if (!mounted || gen != _gen) {
        unawaited(ws.close());
        return;
      }
      _listen(gen, ws);
    } on TerminalHandshakeException catch (e) {
      if (!mounted || gen != _gen) return;
      if (e.statusCode == 404) {
        _goneNow();
      } else if (e.statusCode == 401 || e.statusCode == 403) {
        _fail(gen, 'The server refused the terminal. Sign in again and retry.');
      } else {
        _scheduleRetry(gen);
      }
    } catch (e) {
      if (!mounted || gen != _gen) return;
      _scheduleRetry(gen);
    }
  }

  /// The old plain-connect route: `/v1/sys/wsterm` or the container's
  /// `/terminal`. The shell ends with the connection.
  Future<void> _connectLegacy() async {
    final gen = ++_gen;
    await _drop();
    setState(() => _phase = _Phase.connecting);
    final container = widget.container;
    final path = container == null ? TerminalFamily.host.legacyPath! : '/v1/container/${Uri.encodeComponent(container)}/terminal';
    try {
      final headers = await ApiClient.instance.authHeaders();
      final uri = ApiClient.instance.webSocketUri(path, {'cols': '$_cols', 'rows': '$_rows'});
      final ws = await (widget.connector ?? IoTerminalTransport.connect)(uri, headers);
      if (!mounted || gen != _gen) {
        unawaited(ws.close());
        return;
      }
      _listen(gen, ws);
      // No hello on this route: it is live at once.
      _goLive();
    } catch (e) {
      _fail(gen, e is ApiException ? e.message : "Couldn't connect to the server");
    }
  }

  void _listen(int gen, TerminalTransport ws) {
    _ws = ws;
    _decoder = TerminalOutputDecoder();
    _exited = false;
    _sub = ws.stream.listen(
      (frame) {
        if (gen != _gen) return;
        if (frame is String) {
          final c = Wsterm.control(frame);
          if (c == null) {
            _terminal.write(frame);
          } else {
            _onControl(c);
          }
        } else if (frame is List<int>) {
          _terminal.write(_decoder!.add(frame));
        }
      },
      onError: (Object e) => _closed(gen, ws, error: true),
      onDone: () => _closed(gen, ws),
      cancelOnError: true,
    );
  }

  void _onControl(TerminalControl c) {
    switch (c) {
      case HelloControl(:final session):
        // Start from a clean screen, so a reattach doesn't print the
        // history twice.
        _controller.clearSelection();
        final t = _newTerminal(cols: _terminal.viewWidth, rows: _terminal.viewHeight);
        setState(() {
          _terminal = t;
          _phase = _Phase.restoring;
          if (session != null) _session = session;
        });
      case LiveControl():
        _goLive();
      case SessionControl(:final session):
        if (session != null) setState(() => _session = session);
      case ExitControl(:final code, :final reason):
        _exited = true;
        setState(() {
          _phase = _Phase.ended;
          _message = _exitText(code, reason);
        });
      case UnknownControl():
        break;
    }
  }

  String _exitText(int? code, String reason) => switch (reason) {
        'killed' => 'This terminal was ended.',
        'timeout' => 'This terminal was closed after a day with nothing attached.',
        'evicted' => 'This terminal was closed to make room for a new one.',
        _ => code == null || code < 0 ? 'The shell ended.' : 'The shell ended (exit code $code).',
      };

  void _goLive() {
    if (!mounted) return;
    final first = !_everLive;
    _everLive = true;
    _retries = 0;
    if (!_exited) setState(() => _phase = _Phase.live);
    // The replay is in: show the latest output.
    WidgetsBinding.instance.addPostFrameCallback((_) => _surface.currentState?.scrollToBottom());
    _sendResize();
    final cmd = widget.initCommand;
    if (!_sentInit && cmd != null && cmd.isNotEmpty && widget.session == null) {
      _sentInit = true;
      _send('$cmd\r');
    }
    if (first && !_exited) _focus.requestFocus();
  }

  void _closed(int gen, TerminalTransport ws, {bool error = false}) {
    if (!mounted || gen != _gen) return;
    final rest = _decoder?.close() ?? '';
    if (rest.isNotEmpty) _terminal.write(rest);
    _decoder = null;
    _ws = null;
    _sub = null;
    if (_exited) {
      setState(() => _phase = _Phase.ended);
      return;
    }
    if (_legacy) {
      setState(() {
        _phase = _Phase.ended;
        _message = error ? 'The connection was lost' : Wsterm.closeMessage(ws.closeCode, ws.closeReason);
      });
      return;
    }
    final code = ws.closeCode;
    if (code == Wsterm.tooSlow) {
      // Fell behind: reattach at once; the replay catches up.
      _attach(_session!, reconnect: true);
      return;
    }
    if (code == 1008 || code == 4001 || code == 4003) {
      _fail(gen, Wsterm.closeMessage(code, ws.closeReason));
      return;
    }
    // Lost, or closed by the server without an exit: the shell may still
    // be there. Reattach; a 404 then says it is gone.
    _scheduleRetry(gen, immediate: _retries == 0);
  }

  void _scheduleRetry(int gen, {bool immediate = false}) {
    final s = _session;
    if (s == null) return _fail(gen, "Couldn't connect to the server");
    _retries++;
    // 0 s, then 2, 4, 8, 15, 30 s.
    final wait = immediate ? 0 : const [2, 4, 8, 15, 30][(_retries - 1).clamp(0, 4)];
    setState(() {
      _phase = _Phase.reconnecting;
      _retryIn = wait;
    });
    _retryTimer?.cancel();
    _countdown?.cancel();
    if (wait == 0) {
      _retryTimer = Timer(const Duration(milliseconds: 300), () => _attach(s, reconnect: true));
      return;
    }
    _countdown = Timer.periodic(const Duration(seconds: 1), (t) {
      if (!mounted) return t.cancel();
      setState(() => _retryIn = (_retryIn - 1).clamp(0, 60));
    });
    _retryTimer = Timer(Duration(seconds: wait), () => _attach(s, reconnect: true));
  }

  void _retryNow() {
    final s = _session;
    if (s == null) return;
    _retryTimer?.cancel();
    _countdown?.cancel();
    _attach(s, reconnect: true);
  }

  void _goneNow() {
    _retryTimer?.cancel();
    _countdown?.cancel();
    setState(() {
      _phase = _Phase.gone;
      _message = 'This terminal is no longer on the server. It ended, or the server restarted.';
    });
    unawaited(_api.refreshCount());
  }

  void _fail(int gen, String message) {
    if (!mounted || gen != _gen) return;
    setState(() {
      _phase = _Phase.failed;
      _message = message;
    });
  }

  Future<void> _drop() async {
    final sub = _sub;
    final ws = _ws;
    _sub = null;
    _ws = null;
    await sub?.cancel();
    if (ws != null) unawaited(ws.close());
  }

  // --- input --------------------------------------------------------------

  void _send(String data) {
    if (_connected) _ws?.add(Wsterm.input(data));
  }

  // Everything the keyboard (soft or hardware) types comes through here.
  // A latched Ctrl or Alt from the key row applies to the next key.
  void _onInput(String data) {
    if (_ctrl && data.length == 1) {
      final c = data.toUpperCase().codeUnitAt(0);
      setState(() => _ctrl = false);
      if (c >= 64 && c <= 95) {
        _send(String.fromCharCode(c - 64));
        return;
      }
    }
    if (_alt && data.isNotEmpty) {
      setState(() => _alt = false);
      _send('\x1b$data');
      return;
    }
    _send(data);
  }

  Future<void> _paste() async {
    final clip = await Clipboard.getData(Clipboard.kTextPlain);
    if (!mounted) return;
    final text = clip?.text;
    if (text == null || text.isEmpty) {
      _messenger?.showSnackBar(const SnackBar(content: Text('The clipboard is empty')));
      return;
    }
    // Several lines into a shell that doesn't take bracketed paste run one
    // by one as they arrive: ask first.
    final lines = '\n'.allMatches(text.trimRight()).length + 1;
    if (lines > 1 && !_terminal.bracketedPasteMode && !await _confirmPaste(text, lines)) return;
    // A terminal's Enter is CR.
    _terminal.paste(text.replaceAll('\r\n', '\r').replaceAll('\n', '\r'));
    _surface.currentState?.scrollToBottom();
  }

  Future<bool> _confirmPaste(String text, int lines) async {
    final preview = text.trimRight().split('\n').take(8).join('\n');
    final ok = await showDialog<bool>(
      context: context,
      builder: (context) {
        final theme = Theme.of(context);
        final tokens = DesignTokens.of(context);
        return AlertDialog(
          title: Text('Paste $lines lines?'),
          content: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
            const Text('Each line runs as a command as soon as it is pasted.'),
            const SizedBox(height: Space.md),
            Container(
              width: double.infinity,
              constraints: const BoxConstraints(maxHeight: 160),
              padding: const EdgeInsets.all(Space.sm),
              decoration: BoxDecoration(
                color: theme.colorScheme.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(tokens.radii.sm),
              ),
              child: SingleChildScrollView(
                child: Text(
                  lines > 8 ? '$preview\n…' : preview,
                  style: tokens.mono(theme.textTheme.bodySmall),
                ),
              ),
            ),
          ]),
          actions: [
            TextButton(autofocus: true, onPressed: () => Navigator.of(context).pop(false), child: const Text('Cancel')),
            TextButton(onPressed: () => Navigator.of(context).pop(true), child: const Text('Paste')),
          ],
        );
      },
    );
    return ok == true;
  }

  void _copied(String text) {
    Clipboard.setData(ClipboardData(text: text));
    HapticFeedback.lightImpact();
    final lines = '\n'.allMatches(text).length + 1;
    _messenger?.hideCurrentSnackBar();
    _messenger?.showSnackBar(SnackBar(content: Text(lines > 1 ? 'Copied $lines lines' : 'Copied'), duration: const Duration(seconds: 2)));
  }

  void _setFontSize(double size, {bool save = false}) {
    final v = TerminalFontSize.clamp(size);
    if (v != _fontSize) setState(() => _fontSize = v);
    if (save) unawaited(StorageService.instance.setTerminalFontSize(v));
  }

  // --- session actions ----------------------------------------------------

  Future<void> _rename() async {
    final s = _session;
    if (s == null) return;
    final name = await showTerminalRenameDialog(context, s);
    if (name == null || !mounted) return;
    try {
      final updated = await _api.rename(s, name);
      if (mounted) setState(() => _session = updated);
    } catch (e) {
      _messenger?.showSnackBar(SnackBar(content: Text("Couldn't rename it. ${e is ApiException ? e.message : ''}".trim())));
    }
  }

  Future<void> _end() async {
    final s = _session;
    if (s == null) return;
    final ok = await ConfirmDialog.destructive(
      context,
      title: 'End “${s.title}”?',
      message: 'The shell stops, with anything still running in it.',
      confirmLabel: 'End',
      permanent: false,
    );
    if (!ok || !mounted) return;
    final nav = Navigator.of(context);
    try {
      _gen++;
      await _drop();
      await _api.end(s);
      _session = null;
      if (mounted) nav.pop();
    } catch (e) {
      _messenger?.showSnackBar(SnackBar(content: Text("Couldn't end it. ${e is ApiException ? e.message : ''}".trim())));
      _retryNow();
    }
  }

  void _newSession() {
    Navigator.of(context).pushReplacement(MaterialPageRoute(
      builder: (_) => TerminalScreen(
        container: _family == TerminalFamily.container ? (_session?.container ?? widget.container) : null,
        title: _family == TerminalFamily.container ? widget.title : null,
        connector: widget.connector,
      ),
    ));
  }

  void _allTerminals() {
    Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => TerminalSessionsScreen(connector: widget.connector)));
  }

  /// After the screen closes: the shell is still there; say so, with a way
  /// to end it.
  void _toldStillRunning() {
    final s = _session;
    final messenger = _messenger;
    if (s == null || _legacy || messenger == null) return;
    if (_phase == _Phase.ended || _phase == _Phase.gone || _phase == _Phase.failed) return;
    final api = _api;
    messenger.hideCurrentSnackBar();
    messenger.showSnackBar(SnackBar(
      content: Text('“${s.title}” keeps running on the server'),
      action: SnackBarAction(
        label: 'End it',
        onPressed: () => api.end(s).catchError((Object _) {}),
      ),
    ));
    unawaited(api.refreshCount());
  }

  Future<bool> _confirmLeaveLegacy() => ConfirmDialog.confirm(
        context,
        title: 'Close the terminal?',
        message: 'This server ends the shell when the terminal closes, and anything still running in it stops. '
            'Update NivaroOS to keep terminals running.',
        confirmLabel: 'Close',
      );

  // --- build --------------------------------------------------------------

  String get _title => _session?.title ?? widget.title ?? (_family == TerminalFamily.container ? 'Container shell' : 'Terminal');

  String get _status {
    final s = _session;
    return switch (_phase) {
      _Phase.starting => 'Starting…',
      _Phase.connecting => 'Connecting…',
      _Phase.restoring => 'Restoring recent output…',
      _Phase.reconnecting => _retryIn > 0 ? 'Reconnecting in $_retryIn s…' : 'Reconnecting…',
      _Phase.ended => 'Ended',
      _Phase.gone => 'Not on the server',
      _Phase.failed => 'Not connected',
      _Phase.live => [
          widget.subtitle ??
              (s?.isContainer == true
                  ? 'In ${s!.container ?? 'the container'}'
                  : ApiClient.displayHost(ApiClient.instance.baseUrl)),
          if (s != null && s.clients > 1) s.clients == 2 ? 'also open elsewhere' : 'open in ${s.clients - 1} other places',
        ].join(' · '),
    };
  }

  @override
  Widget build(BuildContext context) {
    final dark = terminalAppTheme(context);
    return Theme(
      data: dark,
      child: Builder(builder: (context) {
        final theme = Theme.of(context);
        final scheme = theme.colorScheme;
        final hasSelection = _controller.selection != null;
        final running = _session?.running ?? true;
        return PopScope(
          canPop: !(_legacy && _connected),
          onPopInvokedWithResult: (didPop, _) async {
            if (didPop) {
              _toldStillRunning();
              return;
            }
            final nav = Navigator.of(context);
            if (await _confirmLeaveLegacy() && mounted) {
              _gen++;
              await _drop();
              setState(() => _phase = _Phase.ended);
              nav.pop();
            }
          },
          child: Scaffold(
            backgroundColor: scheme.surfaceContainerLowest,
            appBar: AppBar(
              backgroundColor: scheme.surfaceContainer,
              systemOverlayStyle: AppTheme.systemBarsStyle(Brightness.dark),
              title: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(_title, maxLines: 1, overflow: TextOverflow.ellipsis),
                  Semantics(
                    liveRegion: true,
                    child: Text(
                      _status,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
                    ),
                  ),
                ],
              ),
              bottom: _phase == _Phase.restoring || _phase == _Phase.starting || _phase == _Phase.connecting
                  ? const PreferredSize(preferredSize: Size.fromHeight(2), child: LinearProgressIndicator(minHeight: 2))
                  : null,
              actions: [
                if (_connected)
                  IconButton(
                    tooltip: _focus.hasFocus ? 'Hide keyboard' : 'Show keyboard',
                    icon: const Icon(Icons.keyboard_outlined),
                    onPressed: () => setState(() {
                      if (_focus.hasFocus) {
                        _focus.unfocus();
                      } else {
                        _surface.currentState?.requestKeyboard();
                      }
                    }),
                  ),
                PopupMenuButton<String>(
                  tooltip: 'More options',
                  onSelected: (v) {
                    switch (v) {
                      case 'copy':
                        _surface.currentState?.copySelection();
                      case 'paste':
                        _paste();
                      case 'selectall':
                        _surface.currentState?.selectAll();
                      case 'clear':
                        // Screen and history here at once; the shell
                        // redraws its prompt on Ctrl+L.
                        _terminal.write('\x1b[H\x1b[2J\x1b[3J');
                        _send('\x0c');
                      case 'bigger':
                        _setFontSize(_fontSize + 1, save: true);
                      case 'smaller':
                        _setFontSize(_fontSize - 1, save: true);
                      case 'rename':
                        _rename();
                      case 'new':
                        _newSession();
                      case 'all':
                        _allTerminals();
                      case 'end':
                        _end();
                    }
                  },
                  itemBuilder: (_) => [
                    if (hasSelection) const PopupMenuItem(value: 'copy', child: ListTile(leading: Icon(Icons.copy_outlined), title: Text('Copy'))),
                    if (_connected) const PopupMenuItem(value: 'paste', child: ListTile(leading: Icon(Icons.content_paste_outlined), title: Text('Paste'))),
                    const PopupMenuItem(value: 'selectall', child: ListTile(leading: Icon(Icons.select_all_outlined), title: Text('Select all'))),
                    if (_connected) const PopupMenuItem(value: 'clear', child: ListTile(leading: Icon(Icons.clear_all_outlined), title: Text('Clear screen'))),
                    const PopupMenuDivider(),
                    const PopupMenuItem(value: 'bigger', child: ListTile(leading: Icon(Icons.text_increase_outlined), title: Text('Larger text'))),
                    const PopupMenuItem(value: 'smaller', child: ListTile(leading: Icon(Icons.text_decrease_outlined), title: Text('Smaller text'))),
                    const PopupMenuDivider(),
                    if (_session != null && running && !_legacy)
                      const PopupMenuItem(value: 'rename', child: ListTile(leading: Icon(Icons.edit_outlined), title: Text('Rename'))),
                    if (!_legacy) const PopupMenuItem(value: 'all', child: ListTile(leading: Icon(Icons.layers_outlined), title: Text('All terminals'))),
                    PopupMenuItem(
                      value: 'new',
                      child: ListTile(leading: const Icon(Icons.add), title: Text(_family == TerminalFamily.container ? 'New shell' : 'New terminal')),
                    ),
                    if (_session != null && running && !_legacy && _phase != _Phase.ended && _phase != _Phase.gone)
                      PopupMenuItem(
                        value: 'end',
                        child: ListTile(
                          leading: Icon(Icons.power_settings_new, color: scheme.error),
                          title: Text('End session', style: TextStyle(color: scheme.error)),
                        ),
                      ),
                  ],
                ),
              ],
            ),
            // Edge to edge: the key row takes the bottom inset itself, so its
            // colour runs under the gesture bar.
            body: SafeArea(
              top: false,
              bottom: false,
              child: Column(children: [
                Expanded(
                  child: Semantics(
                    label: 'Terminal output',
                    child: TerminalSurface(
                      key: _surface,
                      terminal: _terminal,
                      controller: _controller,
                      scrollController: _scroll,
                      focusNode: _focus,
                      theme: terminalThemeFor(scheme, StatusColors.of(context)),
                      fontSize: _fontSize,
                      readOnly: !_connected && _phase != _Phase.reconnecting,
                      onFontSizeChanged: _setFontSize,
                      onFontSizeChangeEnd: (v) => _setFontSize(v, save: true),
                      onCopy: _copied,
                      onPaste: _paste,
                    ),
                  ),
                ),
                ?_banner(context),
                if (_phase != _Phase.ended && _phase != _Phase.gone && !(_phase == _Phase.failed && _session == null))
                  _KeyBar(
                    ctrl: _ctrl,
                    alt: _alt,
                    onPaste: _paste,
                    onKey: (k) {
                      HapticFeedback.selectionClick();
                      switch (k) {
                        case 'ctrl':
                          setState(() => _ctrl = !_ctrl);
                        case 'alt':
                          setState(() => _alt = !_alt);
                        default:
                          final key = _KeyBarState.terminalKeys[k];
                          if (key != null) {
                            // A latched Ctrl/Alt applies to this key (Ctrl+Left
                            // jumps a word) and is used up by it.
                            final ctrl = _ctrl, alt = _alt;
                            if (ctrl || alt) setState(() => _ctrl = _alt = false);
                            _terminal.keyInput(key, ctrl: ctrl, alt: alt);
                          } else {
                            _onInput(k);
                          }
                      }
                    },
                  ),
              ]),
            ),
          ),
        );
      }),
    );
  }

  /// What went wrong, and what to do about it, above the key row.
  Widget? _banner(BuildContext context) {
    final (IconData?, String?, List<Widget>) parts = switch (_phase) {
      _Phase.reconnecting => (
          Icons.cloud_off_outlined,
          _everLive ? 'Connection lost. The terminal keeps running on the server.' : "Can't reach the server. Trying again…",
          [TextButton(onPressed: _retryNow, child: const Text('Retry now'))],
        ),
      _Phase.ended => (
          Icons.check_circle_outline,
          _message ?? 'The shell ended.',
          [
            if (_legacy) TextButton(onPressed: _connectLegacy, child: const Text('Reconnect')) else TextButton(onPressed: _newSession, child: const Text('New terminal')),
          ],
        ),
      _Phase.gone => (
          Icons.link_off_outlined,
          _message ?? 'This terminal is no longer on the server.',
          [TextButton(onPressed: _newSession, child: const Text('New terminal'))],
        ),
      _Phase.failed => (
          Icons.error_outline,
          _message ?? "Couldn't connect",
          [
            if (_message?.contains('End one') == true)
              TextButton(onPressed: _allTerminals, child: const Text('See terminals'))
            else
              TextButton(onPressed: _start, child: const Text('Try again')),
          ],
        ),
      _ => (null, null, const <Widget>[]),
    };
    final (icon, text, actions) = parts;
    if (icon == null || text == null) return null;
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Material(
      color: scheme.surfaceContainerHigh,
      child: SafeArea(
        top: false,
        // Without the key row below, the banner takes the bottom inset.
        bottom: _phase == _Phase.ended || _phase == _Phase.gone || (_phase == _Phase.failed && _session == null),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(Space.lg, Space.xs, Space.sm, Space.xs),
          child: Row(children: [
            Icon(icon, size: 20, color: scheme.onSurfaceVariant),
            const SizedBox(width: Space.md),
            Expanded(child: Semantics(liveRegion: true, child: Text(text, style: theme.textTheme.bodyMedium))),
            ...actions,
          ]),
        ),
      ),
    );
  }
}

/// The keys a phone keyboard lacks: Esc, Tab, latching Ctrl and Alt,
/// arrows (they repeat while held), Home/End, PgUp/PgDn and a few symbols,
/// with Paste kept at the end of the row. Each is a 48dp target.
class _KeyBar extends StatefulWidget {
  const _KeyBar({required this.ctrl, required this.alt, required this.onKey, required this.onPaste});

  final bool ctrl;
  final bool alt;
  final void Function(String key) onKey;
  final VoidCallback onPaste;

  @override
  State<_KeyBar> createState() => _KeyBarState();
}

class _KeyBarState extends State<_KeyBar> {
  final _scroll = ScrollController();

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  static const terminalKeys = {
    'esc': TerminalKey.escape,
    'tab': TerminalKey.tab,
    'up': TerminalKey.arrowUp,
    'down': TerminalKey.arrowDown,
    'left': TerminalKey.arrowLeft,
    'right': TerminalKey.arrowRight,
    'home': TerminalKey.home,
    'end': TerminalKey.end,
    'pgup': TerminalKey.pageUp,
    'pgdn': TerminalKey.pageDown,
  };

  static const _keys = <(String, String, String?)>[
    // (value sent to onKey, label, spoken label); arrows are icons.
    // Tab and the four arrows come first: all five fit on a 360dp phone
    // without scrolling the row (Up is history, Right accepts a suggestion).
    ('tab', 'Tab', 'Tab'),
    ('left', '', 'Left'),
    ('up', '', 'Up'),
    ('down', '', 'Down'),
    ('right', '', 'Right'),
    ('esc', 'Esc', 'Escape'),
    ('ctrl', 'Ctrl', 'Control'),
    ('alt', 'Alt', 'Alt'),
    ('|', '|', 'Pipe'),
    ('~', '~', 'Tilde'),
    ('/', '/', 'Slash'),
    ('-', '-', 'Dash'),
    ('home', 'Home', 'Home'),
    ('end', 'End', 'End'),
    ('pgup', 'PgUp', 'Page up'),
    ('pgdn', 'PgDn', 'Page down'),
  ];

  static const _repeating = {'left', 'down', 'up', 'right', 'pgup', 'pgdn'};

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final bottom = MediaQuery.paddingOf(context).bottom;
    return Material(
      color: scheme.surfaceContainer,
      child: Padding(
        padding: EdgeInsets.only(bottom: bottom),
        child: Row(children: [
          Expanded(
            // Faded ends say "more keys this way" instead of a key cut in half.
            child: FadingEdges(
              controller: _scroll,
              child: SingleChildScrollView(
                controller: _scroll,
                scrollDirection: Axis.horizontal,
                padding: const EdgeInsets.all(Space.xs),
                child: Row(
                  children: [
                    for (final (value, label, spoken) in _keys)
                      _Key(
                        label: label,
                        icon: switch (value) {
                          'left' => Icons.keyboard_arrow_left,
                          'down' => Icons.keyboard_arrow_down,
                          'up' => Icons.keyboard_arrow_up,
                          'right' => Icons.keyboard_arrow_right,
                          _ => null,
                        },
                        semanticsLabel: spoken ?? label,
                        toggled: value == 'ctrl' ? widget.ctrl : (value == 'alt' ? widget.alt : null),
                        repeat: _repeating.contains(value),
                        onTap: () => widget.onKey(value),
                      ),
                  ],
                ),
              ),
            ),
          ),
          // Paste stays in reach, outside the scrolling row.
          DecoratedBox(
            decoration: BoxDecoration(border: Border(left: BorderSide(color: scheme.outlineVariant))),
            child: Padding(
              padding: const EdgeInsets.all(Space.xs),
              child: _Key(label: '', icon: Icons.content_paste_outlined, semanticsLabel: 'Paste', onTap: widget.onPaste),
            ),
          ),
        ]),
      ),
    );
  }
}

class _Key extends StatefulWidget {
  const _Key({required this.label, required this.semanticsLabel, required this.onTap, this.toggled, this.icon, this.repeat = false});

  final String label;
  final IconData? icon;
  final String semanticsLabel;
  final VoidCallback onTap;

  /// Null for a plain key; true/false for Ctrl and Alt.
  final bool? toggled;

  /// Held down, the key repeats (arrows, page keys).
  final bool repeat;

  @override
  State<_Key> createState() => _KeyState();
}

class _KeyState extends State<_Key> {
  Timer? _repeat;

  void _stop() {
    _repeat?.cancel();
    _repeat = null;
  }

  @override
  void dispose() {
    _stop();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final tokens = DesignTokens.of(context);
    final on = widget.toggled == true;
    return Semantics(
      button: true,
      toggled: widget.toggled,
      label: widget.semanticsLabel,
      excludeSemantics: true,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: Space.xs / 2),
        child: Listener(
          onPointerUp: (_) => _stop(),
          onPointerCancel: (_) => _stop(),
          child: Material(
            // Keys read as keys: a tonal cap, like a keyboard's.
            color: on ? scheme.secondaryContainer : scheme.surfaceContainerHighest,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(tokens.radii.sm),
              side: tokens.cardBorder == null ? BorderSide.none : BorderSide(color: scheme.outlineVariant),
            ),
            clipBehavior: Clip.antiAlias,
            child: InkWell(
              onTap: widget.onTap,
              onLongPress: widget.repeat
                  ? () {
                      widget.onTap();
                      _repeat = Timer.periodic(const Duration(milliseconds: 70), (_) => widget.onTap());
                    }
                  : null,
              child: ConstrainedBox(
                constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
                child: Padding(
                  padding: const EdgeInsets.symmetric(horizontal: Space.sm),
                  child: Center(
                    child: widget.icon != null
                        ? Icon(widget.icon, color: scheme.onSurface)
                        : MediaQuery.withClampedTextScaling(
                            maxScaleFactor: 1.3,
                            child: Text(
                              widget.label,
                              style: tokens.mono(theme.textTheme.labelLarge).copyWith(color: on ? scheme.onSecondaryContainer : scheme.onSurface),
                            ),
                          ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
