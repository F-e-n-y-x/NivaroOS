import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';

import 'terminal_sessions.dart';

// ---------------------------------------------------------------------------
// Protocol: wsterm framing v2 (services/common/utils/wsterm/wsterm.go) plus
// the session control frames (docs/specs/2026-09-29-terminal-sessions.md).
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

  /// Close code for a viewer that fell too far behind: reattach.
  static const tooSlow = 1013;

  /// The control message in a server TEXT [frame], or null when the frame
  /// is a plain status line to print.
  static TerminalControl? control(String frame) {
    if (!frame.startsWith(controlPrefix)) return null;
    Object? json;
    try {
      json = jsonDecode(frame.substring(1));
    } catch (_) {
      return const UnknownControl();
    }
    if (json is! Map<String, dynamic>) return const UnknownControl();
    TerminalSession? session() => json is Map<String, dynamic> && json['session'] is Map<String, dynamic> ? TerminalSession.fromJson(json['session'] as Map<String, dynamic>) : null;
    return switch (json['type']) {
      'hello' => HelloControl(session(), (json['replay_bytes'] as num?)?.toInt() ?? 0),
      'live' => const LiveControl(),
      'session' => SessionControl(session()),
      'exit' => ExitControl((json['code'] as num?)?.toInt(), json['reason']?.toString() ?? ''),
      _ => const UnknownControl(),
    };
  }

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

/// A control message from the server.
sealed class TerminalControl {
  const TerminalControl();
}

/// First frame after attaching; the replay follows.
class HelloControl extends TerminalControl {
  const HelloControl(this.session, this.replayBytes);
  final TerminalSession? session;
  final int replayBytes;
}

/// The replay is done; what follows is live.
class LiveControl extends TerminalControl {
  const LiveControl();
}

/// The session was renamed, or its viewers changed.
class SessionControl extends TerminalControl {
  const SessionControl(this.session);
  final TerminalSession? session;
}

/// The session ended; a close frame follows.
class ExitControl extends TerminalControl {
  const ExitControl(this.code, this.reason);
  final int? code;
  final String reason;
}

class UnknownControl extends TerminalControl {
  const UnknownControl();
}

/// Decodes terminal output that arrives in arbitrary chunks. The server
/// cuts output with no regard for UTF-8, so a character can start in one
/// frame and end in the next; one decoder per connection keeps the partial
/// bytes until the rest arrives instead of showing U+FFFD (plan M-07).
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
  /// Frames from the server: `List<int>` for output, `String` for control
  /// messages (0x00 + JSON) and status lines to print as they are.
  Stream<dynamic> get stream;

  /// Sends a frame: `List<int>` goes as BINARY, `String` as TEXT.
  void add(Object frame);

  Future<void> close();
  int? get closeCode;
  String? get closeReason;
}

/// The server answered the WebSocket handshake with a plain HTTP status
/// instead of upgrading: 404 means the session is unknown (it ended, or
/// the server restarted).
class TerminalHandshakeException implements Exception {
  const TerminalHandshakeException(this.statusCode);
  final int statusCode;

  @override
  String toString() => 'The server answered HTTP $statusCode';
}

/// Opens a transport to [uri] with the handshake [headers].
typedef TerminalConnector = Future<TerminalTransport> Function(Uri uri, Map<String, String> headers);

class IoTerminalTransport implements TerminalTransport {
  IoTerminalTransport(this._ws);

  final WebSocket _ws;

  static Future<TerminalTransport> connect(Uri uri, Map<String, String> headers) async {
    try {
      final ws = await WebSocket.connect(uri.toString(), headers: headers).timeout(const Duration(seconds: 15));
      // The server pings every 25 s; ping back too, so a dead link is
      // noticed from this side as well.
      ws.pingInterval = const Duration(seconds: 30);
      return IoTerminalTransport(ws);
    } on WebSocketException catch (e) {
      final status = e.httpStatusCode;
      if (status != null) throw TerminalHandshakeException(status);
      rethrow;
    }
  }

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
