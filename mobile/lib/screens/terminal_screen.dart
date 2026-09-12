import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:xterm/xterm.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../services/storage_service.dart';

/// Authentic native interactive terminal for NivaroOS:
/// - True VT100/ANSI terminal emulation powered by xterm.dart
/// - Directly connected to NivaroOS server PTY (/v1/sys/wsterm)
/// - Full support for interactive TUIs (htop, nano, vim, docker exec -it, etc.)
/// - Soft and hardware keyboard input with IME & backspace detection
/// - Termux-style latchable Extra Keys Bar (ESC, TAB, CTRL, ALT, Arrows, ~, /, |, -)
/// - Font size scaling, copy/paste, and 3-dot overflow menu
class TerminalScreen extends StatefulWidget {
  final String? initCommand;
  final String title;

  const TerminalScreen({
    super.key,
    this.initCommand,
    this.title = 'Terminal',
  });

  @override
  State<TerminalScreen> createState() => _TerminalScreenState();
}

class _TerminalScreenState extends State<TerminalScreen> {
  late final Terminal _terminal;
  final TerminalController _terminalController = TerminalController();
  final FocusNode _focusNode = FocusNode();

  WebSocket? _ws;
  bool _connected = false;
  bool _connecting = true;
  double _fontSize = 12.0;

  bool _ctrlActive = false;
  bool _altActive = false;

  final List<String> _quickMacros = [
    'ls -la',
    'htop',
    'docker ps',
    'df -h',
    'free -m',
    'tailscale status',
    'uptime',
    'uname -a',
  ];

  static const _terminalTheme = TerminalTheme(
    cursor: Color(0xFF38BDF8),
    selection: Color(0x4438BDF8),
    foreground: Color(0xFFF1F5F9),
    background: Color(0xFF0A0D14),
    black: Color(0xFF0F172A),
    red: Color(0xFFEF4444),
    green: Color(0xFF22C55E),
    yellow: Color(0xFFEAB308),
    blue: Color(0xFF3B82F6),
    magenta: Color(0xFFA855F7),
    cyan: Color(0xFF06B6D4),
    white: Color(0xFFF8FAFC),
    brightBlack: Color(0xFF64748B),
    brightRed: Color(0xFFF87171),
    brightGreen: Color(0xFF4ADE80),
    brightYellow: Color(0xFFFDE047),
    brightBlue: Color(0xFF60A5FA),
    brightMagenta: Color(0xFFC084FC),
    brightCyan: Color(0xFF22D3EE),
    brightWhite: Color(0xFFFFFFFF),
    searchHitBackground: Color(0xFFFDE047),
    searchHitBackgroundCurrent: Color(0xFF22C55E),
    searchHitForeground: Color(0xFF000000),
  );

  @override
  void initState() {
    super.initState();
    _terminal = Terminal(
      maxLines: 10000,
    );

    _terminal.onOutput = (data) {
      _send(data);
    };

    _terminal.onResize = (width, height, pixelWidth, pixelHeight) {
      if (_connected && _ws != null) {
        _ws!.add(jsonEncode({
          'type': 'resize',
          'cols': width,
          'rows': height,
        }));
      }
    };

    _connect();

    WidgetsBinding.instance.addPostFrameCallback((_) {
      _focusTerminal();
    });
  }

  @override
  void dispose() {
    _ws?.close();
    _ws = null;
    _focusNode.dispose();
    _terminalController.dispose();
    super.dispose();
  }

  void _focusTerminal() {
    if (!_focusNode.hasFocus) {
      _focusNode.requestFocus();
    }
  }

  Future<void> _connect() async {
    setState(() {
      _connecting = true;
      _connected = false;
    });

    _terminal.write('\x1b[36m[Connecting to NivaroOS PTY (/v1/sys/wsterm)...]\x1b[0m\r\n');

    try {
      await _ws?.close();
      _ws = null;

      final baseUrl = ApiClient.instance.baseUrl;
      if (baseUrl.isEmpty) {
        _terminal.write('\x1b[31m[No server configured. Please connect to a server first.]\x1b[0m\r\n');
        if (mounted) {
          setState(() {
            _connecting = false;
            _connected = false;
          });
        }
        return;
      }

      final uri = Uri.parse(baseUrl);
      final wsScheme = uri.scheme == 'https' ? 'wss' : 'ws';
      final token = ApiClient.instance.accessToken ?? (await StorageService.instance.getAccessToken()) ?? '';

      final cols = _terminal.viewWidth > 0 ? _terminal.viewWidth : 80;
      final rows = _terminal.viewHeight > 0 ? _terminal.viewHeight : 24;

      final wsUri = uri.replace(
        scheme: wsScheme,
        path: '/v1/sys/wsterm',
        queryParameters: {
          'token': token,
          'cols': '$cols',
          'rows': '$rows',
        },
      );

      _ws = await WebSocket.connect(
        wsUri.toString(),
        headers: token.isNotEmpty ? {'Authorization': token} : null,
      ).timeout(const Duration(seconds: 10));

      if (mounted) {
        setState(() {
          _connected = true;
          _connecting = false;
        });
      }

      _ws!.listen(
        (data) {
          if (data is String) {
            _terminal.write(data);
          } else if (data is List<int>) {
            _terminal.write(utf8.decode(data, allowMalformed: true));
          }
        },
        onError: (err) {
          _terminal.write('\r\n\x1b[31m[Connection error: $err]\x1b[0m\r\n');
          if (mounted) {
            setState(() => _connected = false);
          }
        },
        onDone: () {
          _terminal.write('\r\n\x1b[33m[Terminal session disconnected]\x1b[0m\r\n');
          if (mounted) {
            setState(() => _connected = false);
          }
        },
      );

      // Initial resize sync
      if (_terminal.viewWidth > 0 && _terminal.viewHeight > 0) {
        _ws!.add(jsonEncode({
          'type': 'resize',
          'cols': _terminal.viewWidth,
          'rows': _terminal.viewHeight,
        }));
      }

      if (widget.initCommand != null && widget.initCommand!.isNotEmpty) {
        Future.delayed(const Duration(milliseconds: 350), () {
          _send('${widget.initCommand}\r');
        });
      }
    } catch (e) {
      _terminal.write('\r\n\x1b[31m[Failed to connect: $e]\x1b[0m\r\n');
      if (mounted) {
        setState(() {
          _connecting = false;
          _connected = false;
        });
      }
    }
  }

  void _send(String data) {
    if (_ws != null && _connected) {
      _ws!.add(utf8.encode(data));
    }
  }

  KeyEventResult _handleKeyEvent(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent) return KeyEventResult.ignored;

    if (_ctrlActive) {
      final char = event.character;
      if (char != null && char.isNotEmpty) {
        setState(() => _ctrlActive = false);
        final code = char.toUpperCase().codeUnitAt(0);
        if (code >= 64 && code <= 95) {
          _send(String.fromCharCode(code - 64));
          return KeyEventResult.handled;
        }
      }
    }

    if (_altActive) {
      final char = event.character;
      if (char != null && char.isNotEmpty) {
        setState(() => _altActive = false);
        _send('\x1b$char');
        return KeyEventResult.handled;
      }
    }

    return KeyEventResult.ignored;
  }

  void _sendSpecialKey(String key) {
    switch (key) {
      case 'ESC':
        _terminal.keyInput(TerminalKey.escape);
        break;
      case 'TAB':
        _terminal.keyInput(TerminalKey.tab);
        break;
      case 'CTRL':
        setState(() => _ctrlActive = !_ctrlActive);
        break;
      case 'ALT':
        setState(() => _altActive = !_altActive);
        break;
      case 'CTRL+C':
        _send('\x03');
        break;
      case 'CTRL+Z':
        _send('\x1a');
        break;
      case 'CTRL+D':
        _send('\x04');
        break;
      case 'CTRL+L':
        _send('\x0c');
        break;
      case 'UP':
        _terminal.keyInput(TerminalKey.arrowUp);
        break;
      case 'DOWN':
        _terminal.keyInput(TerminalKey.arrowDown);
        break;
      case 'LEFT':
        _terminal.keyInput(TerminalKey.arrowLeft);
        break;
      case 'RIGHT':
        _terminal.keyInput(TerminalKey.arrowRight);
        break;
      default:
        _send(key);
    }
  }

  Future<void> _pasteFromClipboard() async {
    final clip = await Clipboard.getData(Clipboard.kTextPlain);
    if (clip != null && clip.text != null && clip.text!.isNotEmpty) {
      _terminal.paste(clip.text!);
    }
  }

  void _clearScreen() {
    _terminal.write('\x1b[2J\x1b[H');
    _send('\x0c');
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: const Color(0xFF0A0D14),
      appBar: AppBar(
        backgroundColor: const Color(0xFF0F131D),
        elevation: 0,
        titleSpacing: 12,
        title: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.terminal_rounded, size: 20, color: Color(0xFF22C55E)),
            const SizedBox(width: 8),
            Flexible(
              child: Text(
                widget.title,
                style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 16),
                overflow: TextOverflow.ellipsis,
              ),
            ),
            const SizedBox(width: 8),
            // Clean, non-intrusive status dot
            Tooltip(
              message: _connected ? 'Connected' : (_connecting ? 'Connecting...' : 'Offline'),
              child: Container(
                width: 8,
                height: 8,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: _connected
                      ? const Color(0xFF22C55E)
                      : (_connecting ? const Color(0xFFF59E0B) : const Color(0xFFEF4444)),
                  boxShadow: [
                    BoxShadow(
                      color: (_connected ? const Color(0xFF22C55E) : const Color(0xFFEF4444)).withOpacity(0.4),
                      blurRadius: 4,
                      spreadRadius: 1,
                    ),
                  ],
                ),
              ),
            ),
          ],
        ),
        actions: [
          IconButton(
            icon: const Icon(Icons.keyboard_rounded, size: 21, color: NivaroColors.primaryLight),
            tooltip: 'Toggle Soft Keyboard',
            onPressed: () {
              if (_focusNode.hasFocus) {
                _focusNode.unfocus();
              } else {
                _focusTerminal();
              }
            },
          ),
          PopupMenuButton<String>(
            icon: const Icon(Icons.more_vert_rounded, size: 22, color: NivaroColors.textSecondary),
            color: NivaroColors.surfaceRaised,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(12),
              side: const BorderSide(color: NivaroColors.borderSubtle),
            ),
            tooltip: 'Terminal Options',
            onSelected: (val) {
              switch (val) {
                case 'paste':
                  _pasteFromClipboard();
                  break;
                case 'font_plus':
                  setState(() => _fontSize = (_fontSize + 1).clamp(9.0, 22.0));
                  break;
                case 'font_minus':
                  setState(() => _fontSize = (_fontSize - 1).clamp(9.0, 22.0));
                  break;
                case 'font_reset':
                  setState(() => _fontSize = 12.0);
                  break;
                case 'clear':
                  _clearScreen();
                  break;
                case 'reconnect':
                  _connect();
                  break;
              }
            },
            itemBuilder: (context) => [
              PopupMenuItem(
                value: 'reconnect',
                child: Row(
                  children: [
                    Icon(
                      _connected ? Icons.refresh_rounded : Icons.sync_problem_rounded,
                      size: 18,
                      color: _connected ? NivaroColors.primaryLight : NivaroColors.warning,
                    ),
                    const SizedBox(width: 10),
                    Text(
                      _connected ? 'Reconnect Session' : 'Connect Now',
                      style: const TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600),
                    ),
                  ],
                ),
              ),
              const PopupMenuDivider(),
              const PopupMenuItem(
                value: 'paste',
                child: Row(
                  children: [
                    Icon(Icons.content_paste_rounded, size: 18, color: NivaroColors.textSecondary),
                    SizedBox(width: 10),
                    Text('Paste from Clipboard', style: TextStyle(fontSize: 13.5)),
                  ],
                ),
              ),
              const PopupMenuItem(
                value: 'clear',
                child: Row(
                  children: [
                    Icon(Icons.clear_all_rounded, size: 18, color: NivaroColors.textSecondary),
                    SizedBox(width: 10),
                    Text('Clear Screen', style: TextStyle(fontSize: 13.5)),
                  ],
                ),
              ),
              const PopupMenuDivider(),
              PopupMenuItem(
                value: 'font_plus',
                child: Row(
                  children: [
                    const Icon(Icons.text_increase_rounded, size: 18, color: NivaroColors.textSecondary),
                    const SizedBox(width: 10),
                    Text('Increase Font (${_fontSize.toInt()}pt)', style: const TextStyle(fontSize: 13.5)),
                  ],
                ),
              ),
              PopupMenuItem(
                value: 'font_minus',
                child: Row(
                  children: [
                    const Icon(Icons.text_decrease_rounded, size: 18, color: NivaroColors.textSecondary),
                    const SizedBox(width: 10),
                    Text('Decrease Font (${_fontSize.toInt()}pt)', style: const TextStyle(fontSize: 13.5)),
                  ],
                ),
              ),
              const PopupMenuItem(
                value: 'font_reset',
                child: Row(
                  children: [
                    Icon(Icons.restart_alt_rounded, size: 18, color: NivaroColors.textMuted),
                    SizedBox(width: 10),
                    Text('Reset Font Size', style: TextStyle(fontSize: 13.5)),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(width: 4),
        ],
      ),
      body: SafeArea(
        child: Column(
          children: [
            // Quick Macro Chips Bar
            Container(
              height: 36,
              color: const Color(0xFF111522),
              padding: const EdgeInsets.symmetric(horizontal: 8),
              child: ListView.separated(
                scrollDirection: Axis.horizontal,
                itemCount: _quickMacros.length,
                separatorBuilder: (_, __) => const SizedBox(width: 6),
                itemBuilder: (context, i) {
                  final macro = _quickMacros[i];
                  return Center(
                    child: InkWell(
                      borderRadius: BorderRadius.circular(6),
                      onTap: () {
                        _send('$macro\r');
                      },
                      child: Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3.5),
                        decoration: BoxDecoration(
                          color: const Color(0xFF1E2436),
                          borderRadius: BorderRadius.circular(6),
                          border: Border.all(color: NivaroColors.borderSubtle),
                        ),
                        child: Text(
                          macro,
                          style: const TextStyle(fontFamily: 'monospace', fontSize: 11, color: NivaroColors.textSecondary),
                        ),
                      ),
                    ),
                  );
                },
              ),
            ),

            // Native Terminal Canvas
            Expanded(
              child: Container(
                color: const Color(0xFF0A0D14),
                padding: const EdgeInsets.only(top: 2, left: 2, right: 2),
                child: TerminalView(
                  _terminal,
                  controller: _terminalController,
                  focusNode: _focusNode,
                  autofocus: true,
                  theme: _terminalTheme,
                  textStyle: TerminalStyle(
                    fontSize: _fontSize,
                    fontFamily: 'monospace',
                  ),
                  deleteDetection: true,
                  keyboardType: TextInputType.text,
                  keyboardAppearance: Brightness.dark,
                  onKeyEvent: _handleKeyEvent,
                ),
              ),
            ),

            // Termux-Style Extra Keys Bar
            Container(
              color: const Color(0xFF111420),
              padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 4),
              child: SingleChildScrollView(
                scrollDirection: Axis.horizontal,
                child: Row(
                  children: [
                    _KeyButton(label: 'ESC', onTap: () => _sendSpecialKey('ESC')),
                    _KeyButton(label: 'TAB', onTap: () => _sendSpecialKey('TAB')),
                    _KeyButton(
                      label: 'CTRL',
                      active: _ctrlActive,
                      color: const Color(0xFF38BDF8),
                      onTap: () => _sendSpecialKey('CTRL'),
                    ),
                    _KeyButton(
                      label: 'ALT',
                      active: _altActive,
                      color: const Color(0xFFFBBF24),
                      onTap: () => _sendSpecialKey('ALT'),
                    ),
                    _KeyButton(label: 'CTRL+C', color: NivaroColors.dangerLight, onTap: () => _sendSpecialKey('CTRL+C')),
                    _KeyButton(label: 'CTRL+Z', onTap: () => _sendSpecialKey('CTRL+Z')),
                    _KeyButton(label: 'CTRL+D', onTap: () => _sendSpecialKey('CTRL+D')),
                    _KeyButton(label: 'CTRL+L', onTap: () => _sendSpecialKey('CTRL+L')),
                    _KeyButton(label: '↑', onTap: () => _sendSpecialKey('UP')),
                    _KeyButton(label: '↓', onTap: () => _sendSpecialKey('DOWN')),
                    _KeyButton(label: '←', onTap: () => _sendSpecialKey('LEFT')),
                    _KeyButton(label: '→', onTap: () => _sendSpecialKey('RIGHT')),
                    _KeyButton(label: '~', onTap: () => _send('~')),
                    _KeyButton(label: '/', onTap: () => _send('/')),
                    _KeyButton(label: '|', onTap: () => _send('|')),
                    _KeyButton(label: '-', onTap: () => _send('-')),
                    _KeyButton(label: ':', onTap: () => _send(':')),
                    _KeyButton(label: 'sudo', onTap: () => _send('sudo ')),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _KeyButton extends StatelessWidget {
  final String label;
  final VoidCallback onTap;
  final Color? color;
  final bool active;

  const _KeyButton({
    required this.label,
    required this.onTap,
    this.color,
    this.active = false,
  });

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 2.5),
      child: InkWell(
        borderRadius: BorderRadius.circular(5),
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
          decoration: BoxDecoration(
            color: active ? (color ?? NivaroColors.primary).withOpacity(0.25) : const Color(0xFF1E2436),
            borderRadius: BorderRadius.circular(5),
            border: Border.all(
              color: active ? (color ?? NivaroColors.primaryLight) : NivaroColors.borderSubtle,
            ),
          ),
          child: Text(
            label,
            style: TextStyle(
              fontFamily: 'monospace',
              fontSize: 11.5,
              fontWeight: FontWeight.bold,
              color: active ? (color ?? NivaroColors.primaryLight) : (color ?? NivaroColors.textPrimary),
            ),
          ),
        ),
      ),
    );
  }
}
