// Fakes for the terminal tests: a WebSocket the test drives frame by frame,
// and a terminal-sessions server behind package:http.
import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/services/terminal_protocol.dart';

/// A session object as the server sends it.
Map<String, dynamic> sessionJson(
  String id, {
  String title = 'Terminal 1',
  String kind = 'host',
  String state = 'running',
  int clients = 0,
  String? container,
  String? command,
  String? cwd,
  String lastActivity = '2026-09-25T13:58:00Z',
  int? exitCode,
  String exitReason = '',
}) {
  final base = kind == 'host' ? '/v1/sys/terminal-sessions' : '/v1/container/terminal-sessions';
  return {
    'id': id,
    'title': title,
    'kind': kind,
    if (kind == 'host') 'user': 'alex',
    'container': ?container,
    'shell': kind == 'host' ? '/bin/bash' : 'bash',
    'state': state,
    'exit_code': exitCode,
    'exit_reason': exitReason,
    'exited_at': state == 'exited' ? lastActivity : null,
    'created_at': '2026-09-25T12:00:00Z',
    'last_activity_at': lastActivity,
    'clients': clients,
    'cols': 80,
    'rows': 24,
    'cwd': ?cwd,
    'command': ?command,
    'scrollback_bytes': 1200,
    'legacy': false,
    'attach_path': '$base/$id/attach',
  };
}

/// A terminal WebSocket the test plays the server's side of.
class FakeTerminalSocket implements TerminalTransport {
  FakeTerminalSocket(this.uri, this.headers);

  final Uri uri;
  final Map<String, String> headers;
  final _frames = StreamController<dynamic>();
  final sent = <Object>[];
  bool closed = false;
  int? code;
  String? reason;

  void hello(Map<String, dynamic> session, {int replay = 0}) =>
      _frames.add('\u0000${jsonEncode({'type': 'hello', 'session': session, 'replay_bytes': replay})}');
  void live() => _frames.add('\u0000{"type":"live"}');
  void session(Map<String, dynamic> session) => _frames.add('\u0000${jsonEncode({'type': 'session', 'session': session})}');
  void exit(int code, String reason) => _frames.add('\u0000${jsonEncode({'type': 'exit', 'code': code, 'reason': reason})}');
  void out(String text) => _frames.add(utf8.encode(text));

  /// The server closes the socket with [code].
  Future<void> drop([int? code]) async {
    this.code = code;
    await _frames.close();
  }

  /// Bytes the screen sent as input, decoded.
  String get input => sent.whereType<List<int>>().map(utf8.decode).join();

  /// Resize control frames sent, as (cols, rows).
  List<(int, int)> get resizes => [
        for (final f in sent.whereType<String>())
          if (f.startsWith('\u0000'))
            if (jsonDecode(f.substring(1)) case {'type': 'resize', 'cols': final int c, 'rows': final int r}) (c, r),
      ];

  @override
  Stream<dynamic> get stream => _frames.stream;

  @override
  void add(Object frame) => sent.add(frame);

  @override
  Future<void> close() async {
    closed = true;
  }

  @override
  int? get closeCode => code;

  @override
  String? get closeReason => reason;
}

/// Hands out a [FakeTerminalSocket] per connect and remembers them; a
/// queued [TerminalHandshakeException] or error is thrown instead.
class FakeConnector {
  final sockets = <FakeTerminalSocket>[];
  final failures = <Object>[];

  FakeTerminalSocket get last => sockets.last;

  Future<TerminalTransport> call(Uri uri, Map<String, String> headers) async {
    if (failures.isNotEmpty) throw failures.removeAt(0);
    final s = FakeTerminalSocket(uri, headers);
    sockets.add(s);
    return s;
  }
}

/// The session routes of both services, in memory.
class FakeSessionServer {
  FakeSessionServer({this.hostSupported = true});

  bool hostSupported;
  final host = <Map<String, dynamic>>[];
  final containers = <Map<String, dynamic>>[];
  final requests = <String>[];
  int _next = 1;

  /// The next POST answers with this status and message instead.
  (int, String)? failCreate;

  http.Client get client => MockClient(_handle);

  static http.Response _json(Object body, [int status = 200]) =>
      http.Response(jsonEncode(body), status, headers: {'content-type': 'application/json'});

  Future<http.Response> _handle(http.Request req) async {
    final path = req.url.path;
    requests.add('${req.method} $path${req.url.hasQuery ? '?${req.url.query}' : ''}');
    final List<Map<String, dynamic>> family;
    final String kind;
    if (path.startsWith('/v1/sys/terminal-sessions')) {
      if (!hostSupported) return http.Response('404 page not found', 404);
      family = host;
      kind = 'host';
    } else if (path.startsWith('/v1/container/terminal-sessions')) {
      family = containers;
      kind = 'container';
    } else {
      return _json({'success': 404, 'message': 'not found'}, 404);
    }
    final parts = path.split('/').where((p) => p.isNotEmpty).toList();
    final id = parts.length > 3 ? parts[3] : null;
    Map<String, dynamic>? find() => family.where((s) => s['id'] == id).firstOrNull;
    switch (req.method) {
      case 'GET' when id == null:
        final c = req.url.queryParameters['container'];
        final list = [for (final s in family) if (c == null || s['container'] == c) s];
        return _json({
          'success': 200,
          'message': 'OK',
          'data': {
            'sessions': list,
            'running': list.where((s) => s['state'] == 'running').length,
            'limits': {'max_sessions': 12, 'scrollback_bytes': 2097152, 'detached_timeout_seconds': 86400, 'exited_retention_seconds': 600},
          },
        });
      case 'POST':
        final fail = failCreate;
        if (fail != null) {
          failCreate = null;
          return _json({'success': fail.$1, 'message': fail.$2}, fail.$1);
        }
        final body = jsonDecode(req.body) as Map<String, dynamic>;
        final n = _next++;
        final s = sessionJson('s$n', title: kind == 'host' ? 'Terminal $n' : '${body['container']} (bash)', kind: kind, container: body['container'] as String?);
        family.insert(0, s);
        return _json({'success': 201, 'message': 'Created', 'data': s}, 201);
      case 'PUT':
        final s = find();
        if (s == null) return _json({'success': 404, 'message': 'not found'}, 404);
        s['title'] = (jsonDecode(req.body) as Map)['title'];
        return _json({'success': 200, 'message': 'OK', 'data': s});
      case 'DELETE':
        final s = find();
        if (s == null) return _json({'success': 404, 'message': 'not found'}, 404);
        family.remove(s);
        return _json({'success': 200, 'message': 'OK'});
      case 'GET':
        final s = find();
        return s == null ? _json({'success': 404, 'message': 'not found'}, 404) : _json({'success': 200, 'message': 'OK', 'data': s});
    }
    return _json({'success': 405, 'message': 'method'}, 405);
  }
}
