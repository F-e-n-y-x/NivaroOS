import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/foundation.dart';

/// The phone half of file streams over the reverse tunnel (S-04).
///
/// When the server can't open a connection to this phone (away from home,
/// no Tailscale), it sends each file request - listing, download with
/// Range, upload, rename, delete, mkdir - over the tunnel WebSocket as a
/// stream. This replays it against the phone's own file server on loopback
/// (so every rule of [CompanionFileServer] - the secret, the shared-storage
/// policy, Range, atomic uploads - applies unchanged) and streams the
/// answer back. Wire format (see the server's companion_tunnel_mux.go):
///
///   server -> phone  text   {"action":"http","sid":N,"method","path","query","headers","body","window"}
///   phone  -> server text   {"type":"http_head","sid":N,"status":S,"headers":{...}}
///   binary frames           [kind u8][sid u32 BE][payload]
///     1 DATA (<= 64 KiB), 2 END, 3 CREDIT (u32), 4 RESET (utf-8 reason)
///
/// Flow control is credit based per stream and direction: this side sends
/// at most the server's window of body bytes ahead of the server's
/// CREDITs, and the server sends upload bytes only within the [window]
/// this side advertised, credited back once written to the local server.
/// Memory stays bounded by the windows, however slow either side is.
class TunnelStreams {
  TunnelStreams({
    required this.send,
    required this.localBase,
    required this.secret,
    HttpClient? client,
    this.window = defaultWindow,
    this.idleTimeout = const Duration(minutes: 5),
  }) : _client = client ?? (HttpClient()..autoUncompress = false);

  /// Sends one WebSocket message: a [String] (text) or [Uint8List] (binary).
  final void Function(Object message) send;

  /// The local file server, e.g. http://127.0.0.1:41234.
  final Uri? Function() localBase;

  /// The file-server secret (the tunnel itself is already the owner's
  /// authenticated session; the local server still wants it).
  final Future<String?> Function() secret;

  /// This side's receive window for upload bodies, per stream.
  final int window;

  /// A stream with no progress either way for this long is aborted.
  final Duration idleTimeout;

  final HttpClient _client;

  static const int frameData = 1;
  static const int frameEnd = 2;
  static const int frameCredit = 3;
  static const int frameReset = 4;
  static const int frameSize = 64 * 1024;
  static const int defaultWindow = 1 << 20;
  static const int maxStreams = 32;

  /// What the server may ask for: the file server's own endpoints only.
  static const Set<String> allowedPaths = {'/files', '/download', '/upload', '/delete', '/rename', '/mkdir', '/status'};
  static const Set<String> allowedMethods = {'GET', 'HEAD', 'POST', 'DELETE'};

  static const _forwardRequestHeaders = {'range', 'if-range', 'content-type'};
  static const _forwardResponseHeaders = {
    'content-type',
    'content-length',
    'content-range',
    'accept-ranges',
    'content-disposition',
    'etag',
    'last-modified',
  };

  final Map<int, _TunnelStream> _streams = {};

  @visibleForTesting
  int get activeStreams => _streams.length;

  static Uint8List frame(int kind, int sid, [List<int>? payload]) {
    final p = payload ?? const <int>[];
    final b = Uint8List(5 + p.length);
    b[0] = kind;
    ByteData.sublistView(b).setUint32(1, sid);
    b.setRange(5, 5 + p.length, p);
    return b;
  }

  static Uint8List _u32(int n) {
    final b = Uint8List(4);
    ByteData.sublistView(b).setUint32(0, n);
    return b;
  }

  /// The register message fields that turn streams on at the server.
  Map<String, Object> get registerFields => {'streams': 1, 'window': window};

  /// A text message from the server: returns true when it was a stream
  /// open (handled here).
  bool handleText(Map<String, dynamic> msg) {
    if (msg['action'] != 'http') return false;
    final sid = (msg['sid'] as num?)?.toInt();
    if (sid == null || sid <= 0) return true;
    final method = (msg['method'] as String? ?? 'GET').toUpperCase();
    final path = msg['path'] as String? ?? '';
    if (!allowedMethods.contains(method) || !allowedPaths.contains(path) || _streams.length >= maxStreams || _streams.containsKey(sid)) {
      send(frame(frameReset, sid, utf8.encode('refused')));
      return true;
    }
    final headers = <String, String>{};
    final rawHeaders = msg['headers'];
    if (rawHeaders is Map) {
      rawHeaders.forEach((k, v) => headers[k.toString().toLowerCase()] = v.toString());
    }
    final s = _TunnelStream(
      owner: this,
      sid: sid,
      method: method,
      path: path,
      query: msg['query'] as String? ?? '',
      headers: headers,
      hasBody: msg['body'] == true,
      credit: ((msg['window'] as num?)?.toInt() ?? defaultWindow).clamp(0, 64 << 20),
    );
    _streams[sid] = s;
    s.start();
    return true;
  }

  /// A binary frame from the server.
  void handleFrame(List<int> data) {
    if (data.length < 5) return;
    final bytes = data is Uint8List ? data : Uint8List.fromList(data);
    final kind = bytes[0];
    final sid = ByteData.sublistView(bytes).getUint32(1);
    final payload = Uint8List.sublistView(bytes, 5);
    final s = _streams[sid];
    if (s == null) return;
    switch (kind) {
      case frameData:
        s.onData(payload);
      case frameEnd:
        s.onEnd();
      case frameCredit:
        if (payload.length >= 4) s.onCredit(ByteData.sublistView(payload).getUint32(0));
      case frameReset:
        s.abort(null);
    }
  }

  /// The tunnel closed: abort everything.
  void closeAll() {
    for (final s in _streams.values.toList()) {
      s.abort(null);
    }
    _streams.clear();
  }

  void _remove(_TunnelStream s) {
    if (identical(_streams[s.sid], s)) _streams.remove(s.sid);
  }
}

class _TunnelStream {
  _TunnelStream({
    required this.owner,
    required this.sid,
    required this.method,
    required this.path,
    required this.query,
    required this.headers,
    required this.hasBody,
    required this.credit,
  });

  final TunnelStreams owner;
  final int sid;
  final String method;
  final String path;
  final String query;
  final Map<String, String> headers;
  final bool hasBody;

  /// How many response bytes we may still send.
  int credit;

  HttpClientRequest? _request;
  final Completer<HttpClientRequest> _requestReady = Completer();
  StreamSubscription<List<int>>? _sub;
  Uint8List? _pending; // response bytes waiting for credit
  bool _responseDone = false;
  bool _closed = false;
  Timer? _idle;

  /// Upload bytes are written in arrival order, one at a time.
  Future<void> _uploadChain = Future.value();

  void _touch() {
    _idle?.cancel();
    _idle = Timer(owner.idleTimeout, () => abort('the phone stopped (no progress)'));
  }

  Future<void> start() async {
    _touch();
    try {
      final base = owner.localBase();
      final secret = await owner.secret();
      if (base == null || secret == null || secret.isEmpty) {
        abort('the phone is not sharing files right now');
        return;
      }
      final uri = base.replace(path: path, query: query.isEmpty ? null : query);
      final req = await owner._client.openUrl(method, uri);
      if (_closed) {
        req.abort();
        return;
      }
      req.followRedirects = false;
      req.headers.set('X-Companion-Secret', secret);
      headers.forEach((k, v) {
        if (TunnelStreams._forwardRequestHeaders.contains(k)) req.headers.set(k, v);
      });
      final cl = int.tryParse(headers['content-length'] ?? '');
      if (hasBody) {
        req.contentLength = cl ?? -1;
        if (cl == null) req.bufferOutput = false;
      } else {
        req.contentLength = 0;
      }
      _request = req;
      _requestReady.complete(req);
      if (!hasBody) await _finishRequest();
    } catch (e) {
      abort('the phone could not open its file server');
    }
  }

  void onData(Uint8List chunk) {
    if (_closed || !hasBody) return;
    _touch();
    final copy = Uint8List.fromList(chunk);
    _uploadChain = _uploadChain.then((_) async {
      if (_closed) return;
      final req = await _requestReady.future;
      req.add(copy);
      await req.flush();
      if (_closed) return;
      owner.send(TunnelStreams.frame(TunnelStreams.frameCredit, sid, TunnelStreams._u32(copy.length)));
    }).catchError((Object _) => abort('the upload could not be written'));
  }

  void onEnd() {
    if (_closed || !hasBody) return;
    _uploadChain = _uploadChain.then((_) => _finishRequest()).catchError((Object _) => abort('the upload could not be finished'));
  }

  Future<void> _finishRequest() async {
    if (_closed) return;
    final req = await _requestReady.future;
    final HttpClientResponse resp;
    try {
      resp = await req.close();
    } catch (_) {
      abort('the phone could not answer');
      return;
    }
    if (_closed) return;
    final h = <String, String>{};
    resp.headers.forEach((name, values) {
      if (TunnelStreams._forwardResponseHeaders.contains(name.toLowerCase()) && values.isNotEmpty) h[name] = values.join(', ');
    });
    owner.send(jsonEncode({'type': 'http_head', 'sid': sid, 'status': resp.statusCode, 'headers': h}));
    _touch();
    _sub = resp.listen(
      (data) {
        _pending = _pending == null ? Uint8List.fromList(data) : (BytesBuilder(copy: false)..add(_pending!)..add(data)).takeBytes();
        _pump();
      },
      onDone: () {
        _responseDone = true;
        _pump();
      },
      onError: (Object _) => abort('reading the file failed'),
      cancelOnError: true,
    );
  }

  /// Sends pending response bytes as credit allows; pauses the source when
  /// out of credit.
  void _pump() {
    if (_closed) return;
    var p = _pending;
    while (p != null && p.isNotEmpty && credit > 0) {
      var n = p.length;
      if (n > TunnelStreams.frameSize) n = TunnelStreams.frameSize;
      if (n > credit) n = credit;
      owner.send(TunnelStreams.frame(TunnelStreams.frameData, sid, Uint8List.sublistView(p, 0, n)));
      credit -= n;
      p = n == p.length ? null : Uint8List.sublistView(p, n);
      _touch();
    }
    _pending = (p == null || p.isEmpty) ? null : p;
    final sub = _sub;
    if (_pending != null) {
      if (sub != null && !sub.isPaused) sub.pause();
      return;
    }
    if (_responseDone) {
      owner.send(TunnelStreams.frame(TunnelStreams.frameEnd, sid));
      _finish();
      return;
    }
    if (sub != null && sub.isPaused) sub.resume();
  }

  void onCredit(int n) {
    if (_closed) return;
    credit += n;
    _touch();
    _pump();
  }

  void _finish() {
    _closed = true;
    _idle?.cancel();
    owner._remove(this);
  }

  /// Ends the stream: [reason] non-null sends RESET to the server (a
  /// failure here), null means the server reset it (or the tunnel closed).
  void abort(String? reason) {
    if (_closed) return;
    _closed = true;
    _idle?.cancel();
    owner._remove(this);
    if (reason != null) {
      try {
        owner.send(TunnelStreams.frame(TunnelStreams.frameReset, sid, utf8.encode(reason)));
      } catch (_) {}
    }
    _sub?.cancel();
    _request?.abort();
  }
}
