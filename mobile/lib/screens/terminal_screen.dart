import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:xterm/xterm.dart';

import '../services/api_client.dart';
import '../ui/ui.dart';

// ---------------------------------------------------------------------------
// Protocol: wsterm framing v2 (services/common/utils/wsterm/wsterm.go)
// ---------------------------------------------------------------------------

/// The frames of NivaroOS's terminal WebSocket protocol, version 2: BINARY
/// frames are raw input (never parsed by the server), a TEXT frame that
/// starts with 0x00 is a JSON control message. Plan M-06.
abstract final class Wsterm {
  /// The first byte of a control TEXT frame.
  static const controlPrefix = '\u0000';

  /// A resize control frame, sent as TEXT.
  static String resize(int cols, int rows) => '$controlPrefix${jsonEncode({'type': 'resize', 'cols': cols, 'rows': rows})}';

  /// Terminal input as a BINARY frame's bytes.
  static Uint8List input(String text) => utf8.encode(text);

  /// Close code the server uses when the session failed on its side.
  static const serverFailure = 1011;

  /// What to tell the user when a session ends with [code] and [reason].
  static String closeMessage(int? code, String? reason) {
    final why = (reason ?? '').trim();
    return switch (code) {
      null || 1000 || 1005 => 'Session ended',
      serverFailure => why.isEmpty ? 'The server stopped the session' : 'The server stopped the session: $why',
      1006 => 'The connection was lost',
      1008 || 4001 || 4003 => 'The server refused the session. Sign in again and retry.',
      _ => why.isEmpty ? 'Disconnected (code $code)' : 'Disconnected: $why',
    };
  }
}

/// Decodes terminal output that arrives in arbitrary chunks. The server
/// cuts output every 8 KiB with no regard for UTF-8, so a character can
/// start in one frame and end in the next; one decoder per session keeps
/// the partial bytes until the rest arrives instead of showing U+FFFD
/// (plan M-07).
class TerminalOutputDecoder {
  TerminalOutputDecoder() {
    // A sink that collects as it goes (StringConversionSink.withCallback
    // would only report on close).
    _input = const Utf8Decoder(allowMalformed: true).startChunkedConversion(_Collect(_out));
  }

  final _out = StringBuffer();
  late final ByteConversionSink _input;

  /// Decodes [bytes] and returns every complete character so far.
  String add(List<int> bytes) {
    _input.add(bytes);
    final s = _out.toString();
    _out.clear();
    return s;
  }

  /// Flushes what is left (an incomplete character becomes U+FFFD).
  String close() {
    _input.close();
    final s = _out.toString();
    _out.clear();
    return s;
  }
}

class _Collect implements Sink<String> {
  _Collect(this._out);

  final StringBuffer _out;

  @override
  void add(String data) => _out.write(data);

  @override
  void close() {}
}

/// A terminal WebSocket, as the screen needs it. [IoTerminalTransport] is
/// the real one; tests pass their own.
abstract class TerminalTransport {
  /// Frames from the server: `List<int>` for output, `String` for status
  /// lines to print as they are.
  Stream<dynamic> get stream;

  /// Sends a frame: `List<int>` goes as BINARY, `String` as TEXT.
  void add(Object frame);

  Future<void> close();
  int? get closeCode;
  String? get closeReason;
}

/// Opens a transport to [uri] with the handshake [headers].
typedef TerminalConnector = Future<TerminalTransport> Function(Uri uri, Map<String, String> headers);

class IoTerminalTransport implements TerminalTransport {
  IoTerminalTransport(this._ws);

  final WebSocket _ws;

  static Future<TerminalTransport> connect(Uri uri, Map<String, String> headers) async =>
      IoTerminalTransport(await WebSocket.connect(uri.toString(), headers: headers).timeout(const Duration(seconds: 15)));

  @override
  Stream<dynamic> get stream => _ws;

  @override
  void add(Object frame) => _ws.add(frame);

  @override
  Future<void> close() => _ws.close();

  @override
  int? get closeCode => _ws.closeCode;

  @override
  String? get closeReason => _ws.closeReason;
}

/// The ANSI palette, from the dark theme's roles and status colours, so the
/// terminal matches the app instead of carrying its own hex values.
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

enum _Conn { connecting, connected, closed }

/// A shell on the server (`/v1/sys/wsterm`) or in an app's container
/// (`/v1/container/{id}/terminal`), with a row of the keys a phone
/// keyboard lacks. Always dark, whatever the app theme.
class TerminalScreen extends StatefulWidget {
  const TerminalScreen({
    super.key,
    this.initCommand,
    this.title = 'Terminal',
    this.subtitle,
    this.path = '/v1/sys/wsterm',
    this.connector,
  });

  /// Typed into the shell once it is connected.
  final String? initCommand;
  final String title;

  /// Under the title; the server's address when null.
  final String? subtitle;

  /// The WebSocket route.
  final String path;

  /// Opens the WebSocket; tests pass a fake.
  final TerminalConnector? connector;

  @override
  State<TerminalScreen> createState() => _TerminalScreenState();
}

class _TerminalScreenState extends State<TerminalScreen> {
  final _terminal = Terminal(maxLines: 10000);
  final _controller = TerminalController();
  final _focus = FocusNode();

  TerminalTransport? _ws;
  StreamSubscription<dynamic>? _sub;
  TerminalOutputDecoder? _decoder;
  _Conn _conn = _Conn.connecting;
  String? _closedMessage;
  bool _ctrl = false;
  bool _alt = false;
  double _fontScale = 1;
  int _session = 0;

  @override
  void initState() {
    super.initState();
    _terminal.onOutput = _onInput;
    _terminal.onResize = (cols, rows, _, _) {
      if (_conn == _Conn.connected) _ws?.add(Wsterm.resize(cols, rows));
    };
    _connect();
  }

  @override
  void dispose() {
    _session++;
    _sub?.cancel();
    _ws?.close();
    _focus.dispose();
    _controller.dispose();
    super.dispose();
  }

  Future<void> _connect() async {
    final session = ++_session;
    await _sub?.cancel();
    _sub = null;
    unawaited(_ws?.close());
    _ws = null;
    setState(() {
      _conn = _Conn.connecting;
      _closedMessage = null;
    });

    if (ApiClient.instance.baseUrl.isEmpty) {
      _closed(session, 'No server is set up');
      return;
    }
    final cols = _terminal.viewWidth > 0 ? _terminal.viewWidth : 80;
    final rows = _terminal.viewHeight > 0 ? _terminal.viewHeight : 24;
    try {
      // The token goes in the handshake header only, refreshed first when
      // it is about to expire (plan M-07, M-16).
      final headers = await ApiClient.instance.authHeaders();
      final uri = ApiClient.instance.webSocketUri(widget.path, {'cols': '$cols', 'rows': '$rows'});
      final ws = await (widget.connector ?? IoTerminalTransport.connect)(uri, headers);
      if (!mounted || session != _session) {
        unawaited(ws.close());
        return;
      }
      _ws = ws;
      _decoder = TerminalOutputDecoder();
      setState(() => _conn = _Conn.connected);
      _sub = ws.stream.listen(
        (frame) {
          if (frame is String) {
            _terminal.write(frame);
          } else if (frame is List<int>) {
            _terminal.write(_decoder!.add(frame));
          }
        },
        onError: (Object e) => _closed(session, 'The connection was lost'),
        onDone: () => _closed(session, Wsterm.closeMessage(ws.closeCode, ws.closeReason)),
        cancelOnError: true,
      );
      if (_terminal.viewWidth > 0 && _terminal.viewHeight > 0) ws.add(Wsterm.resize(_terminal.viewWidth, _terminal.viewHeight));
      final cmd = widget.initCommand;
      if (cmd != null && cmd.isNotEmpty) _send('$cmd\r');
      _focus.requestFocus();
    } catch (e) {
      _closed(session, e is ApiException ? e.message : "Couldn't connect to the server");
    }
  }

  void _closed(int session, String message) {
    if (!mounted || session != _session) return;
    final rest = _decoder?.close() ?? '';
    if (rest.isNotEmpty) _terminal.write(rest);
    _decoder = null;
    setState(() {
      _conn = _Conn.closed;
      _closedMessage = message;
    });
  }

  void _send(String data) {
    if (_conn == _Conn.connected) _ws?.add(Wsterm.input(data));
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
    final text = clip?.text;
    if (text != null && text.isNotEmpty) _terminal.paste(text);
  }

  void _copy() {
    final sel = _controller.selection;
    if (sel == null) return;
    Clipboard.setData(ClipboardData(text: _terminal.buffer.getText(sel)));
    _controller.clearSelection();
    ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Copied')));
  }

  Future<bool> _confirmLeave() async {
    if (_conn != _Conn.connected) return true;
    return ConfirmDialog.confirm(
      context,
      title: 'Close the terminal?',
      message: 'The shell session ends, and anything still running in it stops.',
      confirmLabel: 'Close',
    );
  }

  @override
  Widget build(BuildContext context) {
    // The terminal is dark in both app themes; every colour below comes
    // from the dark theme.
    final dark = AppTheme.dark();
    return Theme(
      data: dark,
      child: Builder(builder: (context) {
        final theme = Theme.of(context);
        final scheme = theme.colorScheme;
        final base = theme.textTheme.bodyMedium?.fontSize ?? 14;
        final status = switch (_conn) {
          _Conn.connecting => 'Connecting…',
          _Conn.connected => widget.subtitle ?? ApiClient.displayHost(ApiClient.instance.baseUrl),
          _Conn.closed => 'Disconnected',
        };
        return PopScope(
          canPop: _conn != _Conn.connected,
          onPopInvokedWithResult: (didPop, _) async {
            if (didPop) return;
            final nav = Navigator.of(context);
            if (await _confirmLeave() && mounted) {
              setState(() => _conn = _Conn.closed);
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
                  Text(widget.title, maxLines: 1, overflow: TextOverflow.ellipsis),
                  Semantics(
                    liveRegion: true,
                    child: Text(
                      status,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
                    ),
                  ),
                ],
              ),
              actions: [
                IconButton(
                  tooltip: _focus.hasFocus ? 'Hide keyboard' : 'Show keyboard',
                  icon: const Icon(Icons.keyboard_outlined),
                  onPressed: () => setState(() => _focus.hasFocus ? _focus.unfocus() : _focus.requestFocus()),
                ),
                PopupMenuButton<String>(
                  tooltip: 'More options',
                  onSelected: (v) {
                    switch (v) {
                      case 'copy':
                        _copy();
                      case 'paste':
                        _paste();
                      case 'clear':
                        _terminal.write('\x1b[2J\x1b[H');
                        _send('\x0c');
                      case 'bigger':
                        setState(() => _fontScale = (_fontScale + 0.15).clamp(0.7, 2.0));
                      case 'smaller':
                        setState(() => _fontScale = (_fontScale - 0.15).clamp(0.7, 2.0));
                      case 'reconnect':
                        _connect();
                    }
                  },
                  itemBuilder: (_) => [
                    if (_controller.selection != null) const PopupMenuItem(value: 'copy', child: Text('Copy')),
                    const PopupMenuItem(value: 'paste', child: Text('Paste')),
                    const PopupMenuItem(value: 'clear', child: Text('Clear screen')),
                    const PopupMenuDivider(),
                    const PopupMenuItem(value: 'bigger', child: Text('Larger text')),
                    const PopupMenuItem(value: 'smaller', child: Text('Smaller text')),
                    const PopupMenuDivider(),
                    PopupMenuItem(value: 'reconnect', child: Text(_conn == _Conn.connected ? 'Start a new session' : 'Reconnect')),
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
                    child: TerminalView(
                      _terminal,
                      controller: _controller,
                      focusNode: _focus,
                      autofocus: true,
                      theme: terminalThemeFor(scheme, StatusColors.of(context)),
                      // The terminal has its own text size (Larger/Smaller
                      // text); the phone's text scale would make a
                      // 40-column shell unusable.
                      textScaler: TextScaler.noScaling,
                      textStyle: TerminalStyle(fontSize: base * _fontScale, fontFamily: 'monospace'),
                      padding: const EdgeInsets.all(Space.xs),
                      deleteDetection: true,
                      keyboardType: TextInputType.visiblePassword,
                      keyboardAppearance: Brightness.dark,
                    ),
                  ),
                ),
                if (_conn == _Conn.closed)
                  Material(
                    color: scheme.surfaceContainerHigh,
                    child: Padding(
                      padding: const EdgeInsets.fromLTRB(Space.lg, Space.xs, Space.sm, Space.xs),
                      child: Row(children: [
                        Icon(Icons.link_off_outlined, size: 20, color: scheme.onSurfaceVariant),
                        const SizedBox(width: Space.md),
                        Expanded(child: Semantics(liveRegion: true, child: Text(_closedMessage ?? 'Disconnected', style: theme.textTheme.bodyMedium))),
                        TextButton(onPressed: _connect, child: const Text('Reconnect')),
                      ]),
                    ),
                  ),
                _KeyBar(
                  ctrl: _ctrl,
                  alt: _alt,
                  onKey: (k) {
                    HapticFeedback.selectionClick();
                    switch (k) {
                      case 'ctrl':
                        setState(() => _ctrl = !_ctrl);
                      case 'alt':
                        setState(() => _alt = !_alt);
                      case 'esc':
                        _terminal.keyInput(TerminalKey.escape);
                      case 'tab':
                        _terminal.keyInput(TerminalKey.tab);
                      case 'up':
                        _terminal.keyInput(TerminalKey.arrowUp);
                      case 'down':
                        _terminal.keyInput(TerminalKey.arrowDown);
                      case 'left':
                        _terminal.keyInput(TerminalKey.arrowLeft);
                      case 'right':
                        _terminal.keyInput(TerminalKey.arrowRight);
                      case 'home':
                        _terminal.keyInput(TerminalKey.home);
                      case 'end':
                        _terminal.keyInput(TerminalKey.end);
                      case 'pgup':
                        _terminal.keyInput(TerminalKey.pageUp);
                      case 'pgdn':
                        _terminal.keyInput(TerminalKey.pageDown);
                      default:
                        _onInput(k);
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
}

/// The keys a phone keyboard lacks: Esc, Tab, latching Ctrl and Alt,
/// arrows, Home/End, PgUp/PgDn and a few symbols. Each is a 48dp target.
class _KeyBar extends StatefulWidget {
  const _KeyBar({required this.ctrl, required this.alt, required this.onKey});

  final bool ctrl;
  final bool alt;
  final void Function(String key) onKey;

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

  static const _keys = <(String, String, String?)>[
    // (value sent to onKey, label, spoken label); arrows are icons.
    ('esc', 'Esc', 'Escape'),
    ('tab', 'Tab', 'Tab'),
    ('ctrl', 'Ctrl', 'Control'),
    ('alt', 'Alt', 'Alt'),
    ('left', '', 'Left'),
    ('down', '', 'Down'),
    ('up', '', 'Up'),
    ('right', '', 'Right'),
    ('home', 'Home', 'Home'),
    ('end', 'End', 'End'),
    ('pgup', 'PgUp', 'Page up'),
    ('pgdn', 'PgDn', 'Page down'),
    ('|', '|', 'Pipe'),
    ('/', '/', 'Slash'),
    ('-', '-', 'Dash'),
    ('~', '~', 'Tilde'),
  ];

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final ctrl = widget.ctrl;
    final alt = widget.alt;
    final onKey = widget.onKey;
    return Material(
      color: scheme.surfaceContainer,
      // Faded ends say "more keys this way" instead of a key cut in half.
      child: FadingEdges(
        controller: _scroll,
        child: SingleChildScrollView(
        controller: _scroll,
        scrollDirection: Axis.horizontal,
        padding: EdgeInsets.fromLTRB(Space.xs, Space.xs, Space.xs, Space.xs + MediaQuery.paddingOf(context).bottom),
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
                toggled: value == 'ctrl' ? ctrl : (value == 'alt' ? alt : null),
                onTap: () => onKey(value),
              ),
          ],
        ),
        ),
      ),
    );
  }
}

class _Key extends StatelessWidget {
  const _Key({required this.label, required this.semanticsLabel, required this.onTap, this.toggled, this.icon});

  final String label;
  final IconData? icon;
  final String semanticsLabel;
  final VoidCallback onTap;

  /// Null for a plain key; true/false for Ctrl and Alt.
  final bool? toggled;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final on = toggled == true;
    return Semantics(
      button: true,
      toggled: toggled,
      label: semanticsLabel,
      excludeSemantics: true,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: Space.xs / 2),
        child: Material(
          // Keys read as keys: a tonal cap, like a keyboard's.
          color: on ? scheme.secondaryContainer : scheme.surfaceContainerHighest,
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Corners.small)),
          clipBehavior: Clip.antiAlias,
          child: InkWell(
            onTap: onTap,
            child: ConstrainedBox(
              constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: Space.sm),
                child: Center(
                  child: icon != null
                      ? Icon(icon, color: scheme.onSurface)
                      : MediaQuery.withClampedTextScaling(
                    maxScaleFactor: 1.3,
                    child: Text(
                      label,
                      style: theme.textTheme.labelLarge?.copyWith(
                        fontFamily: 'monospace',
                        color: on ? scheme.onSecondaryContainer : scheme.onSurface,
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
