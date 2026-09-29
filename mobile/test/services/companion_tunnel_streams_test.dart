// The phone half of file streams over the reverse tunnel (S-04): replays
// the server's requests against the local file server, streams answers back
// under the server's credit, takes uploads within its own window, and stops
// on RESET.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/companion_file_server.dart';
import 'package:nivaroos_mobile/services/companion_tunnel_streams.dart';

Uint8List pattern(int n) => Uint8List.fromList(List.generate(n, (i) => (i * 7 + i ~/ 251) & 0xff));

/// A stand-in for the phone's file server on loopback.
class FakeFileServer {
  late HttpServer server;
  final files = <String, Uint8List>{};
  final uploads = <String, Uint8List>{};
  int requests = 0;

  Future<void> start() async {
    server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    server.listen((req) async {
      requests++;
      if (req.headers.value('x-companion-secret') != 's3cret') {
        req.response.statusCode = 401;
        await req.response.close();
        return;
      }
      final p = req.uri.queryParameters['path'] ?? '';
      if (req.uri.path == '/upload') {
        final b = BytesBuilder();
        await for (final c in req) {
          b.add(c);
        }
        uploads[p] = b.takeBytes();
        req.response.headers.contentType = ContentType.json;
        req.response.write('{"success":true}');
        await req.response.close();
        return;
      }
      final data = files[p];
      if (data == null) {
        req.response.statusCode = 404;
        await req.response.close();
        return;
      }
      var start = 0, end = data.length - 1;
      final range = req.headers.value('range');
      if (range != null && range.startsWith('bytes=')) {
        final parts = range.substring(6).split('-');
        start = int.parse(parts[0]);
        if (parts[1].isNotEmpty) end = int.parse(parts[1]);
        req.response.statusCode = 206;
        req.response.headers.set('Content-Range', 'bytes $start-$end/${data.length}');
      }
      req.response.headers.set('Content-Type', 'video/mp4');
      req.response.headers.set('X-Private', 'not forwarded');
      req.response.contentLength = end - start + 1;
      try {
        // Big chunks, like File.openRead: the stream must still be sent in
        // frames and paused when out of credit.
        for (var i = start; i <= end; i += 256 * 1024) {
          req.response.add(Uint8List.sublistView(data, i, (i + 256 * 1024).clamp(0, end + 1)));
          await req.response.flush();
        }
        await req.response.close();
      } catch (_) {}
    });
  }
}

/// The server side of the wire, recording what the phone sent.
class FakeServerSide {
  final heads = <int, Map<String, dynamic>>{};
  final bodies = <int, BytesBuilder>{};
  final ended = <int>{};
  final resets = <int, String>{};
  final credits = <int, int>{};
  final changed = StreamController<void>.broadcast();

  void receive(Object m) {
    if (m is String) {
      final j = jsonDecode(m) as Map<String, dynamic>;
      heads[j['sid'] as int] = j;
    } else {
      final b = m as Uint8List;
      final sid = ByteData.sublistView(b).getUint32(1);
      final payload = Uint8List.sublistView(b, 5);
      switch (b[0]) {
        case TunnelStreams.frameData:
          expect(payload.length, lessThanOrEqualTo(TunnelStreams.frameSize));
          (bodies[sid] ??= BytesBuilder()).add(payload);
        case TunnelStreams.frameEnd:
          ended.add(sid);
        case TunnelStreams.frameReset:
          resets[sid] = utf8.decode(payload);
        case TunnelStreams.frameCredit:
          credits[sid] = (credits[sid] ?? 0) + ByteData.sublistView(payload).getUint32(0);
      }
    }
    changed.add(null);
  }

  int received(int sid) => bodies[sid]?.length ?? 0;

  Future<void> until(bool Function() cond) async {
    final deadline = DateTime.now().add(const Duration(seconds: 5));
    while (!cond()) {
      if (DateTime.now().isAfter(deadline)) fail('timed out');
      await Future<void>.delayed(const Duration(milliseconds: 5));
    }
  }
}

void main() {
  late FakeFileServer local;
  late FakeServerSide server;
  late TunnelStreams streams;

  setUp(() async {
    local = FakeFileServer();
    await local.start();
    server = FakeServerSide();
    streams = TunnelStreams(
      send: server.receive,
      localBase: () => Uri(scheme: 'http', host: '127.0.0.1', port: local.server.port),
      secret: () async => 's3cret',
      window: 128 * 1024,
    );
  });

  tearDown(() async {
    streams.closeAll();
    await local.server.close(force: true);
  });

  void open(int sid, {String method = 'GET', String path = '/download', String query = '', Map<String, String>? headers, bool body = false, int window = 1 << 20}) {
    expect(streams.handleText({'action': 'http', 'sid': sid, 'method': method, 'path': path, 'query': query, 'headers': headers ?? {}, 'body': body, 'window': window}), isTrue);
  }

  void credit(int sid, int n) {
    final c = Uint8List(4);
    ByteData.sublistView(c).setUint32(0, n);
    streams.handleFrame(TunnelStreams.frame(TunnelStreams.frameCredit, sid, c));
  }

  test('download: head, body in frames, end; only media headers forwarded', () async {
    local.files['/v.mp4'] = pattern(3 * 1024 * 1024 + 11);
    open(1, query: 'path=%2Fv.mp4', window: 8 << 20);
    await server.until(() => server.ended.contains(1));
    expect(server.heads[1]!['status'], 200);
    final h = server.heads[1]!['headers'] as Map;
    expect(h['content-type'], 'video/mp4');
    expect(h['content-length'], '${3 * 1024 * 1024 + 11}');
    expect(h.containsKey('x-private'), isFalse);
    expect(server.bodies[1]!.takeBytes(), local.files['/v.mp4']);
    expect(streams.activeStreams, 0);
  });

  test('range request (video seeking)', () async {
    local.files['/v.mp4'] = pattern(500000);
    open(2, query: 'path=%2Fv.mp4', headers: {'Range': 'bytes=1000-1999'});
    await server.until(() => server.ended.contains(2));
    expect(server.heads[2]!['status'], 206);
    expect((server.heads[2]!['headers'] as Map)['content-range'], 'bytes 1000-1999/500000');
    expect(server.bodies[2]!.takeBytes(), local.files['/v.mp4']!.sublist(1000, 2000));
  });

  test('flow control: never more than the credit in flight', () async {
    local.files['/big'] = pattern(4 * 1024 * 1024);
    const window = 200 * 1024;
    open(3, query: 'path=%2Fbig', window: window);
    await server.until(() => server.received(3) == window);
    await Future<void>.delayed(const Duration(milliseconds: 200));
    expect(server.received(3), window, reason: 'sent past the window with no credit');
    // The consumer reads: credit flows back in pieces, the rest arrives,
    // never beyond what was credited.
    var credited = window;
    while (!server.ended.contains(3)) {
      credit(3, 100 * 1024);
      credited += 100 * 1024;
      await Future<void>.delayed(const Duration(milliseconds: 1));
      expect(server.received(3), lessThanOrEqualTo(credited));
    }
    expect(server.bodies[3]!.takeBytes(), local.files['/big']);
  });

  test('upload streamed to the local server, credited as written', () async {
    final data = pattern(700 * 1024 + 3);
    open(4, method: 'POST', path: '/upload', query: 'path=%2Fup.bin', headers: {'Content-Length': '${data.length}'}, body: true);
    var sent = 0;
    for (var i = 0; i < data.length; i += TunnelStreams.frameSize) {
      final end = (i + TunnelStreams.frameSize).clamp(0, data.length);
      // Stay within the phone's advertised window, as the server does.
      await server.until(() => sent - (server.credits[4] ?? 0) + (end - i) <= streams.window);
      streams.handleFrame(TunnelStreams.frame(TunnelStreams.frameData, 4, Uint8List.sublistView(data, i, end)));
      sent += end - i;
    }
    streams.handleFrame(TunnelStreams.frame(TunnelStreams.frameEnd, 4));
    await server.until(() => server.ended.contains(4));
    expect(server.heads[4]!['status'], 200);
    expect(local.uploads['/up.bin'], data);
    expect(server.credits[4], data.length);
  });

  test('RESET from the server stops the transfer; refused paths are reset', () async {
    local.files['/big'] = pattern(8 * 1024 * 1024);
    open(5, query: 'path=%2Fbig', window: 64 * 1024);
    await server.until(() => server.received(5) == 64 * 1024);
    streams.handleFrame(TunnelStreams.frame(TunnelStreams.frameReset, 5, utf8.encode('cancelled')));
    expect(streams.activeStreams, 0);
    credit(5, 1 << 20); // late credit for a finished stream: ignored
    await Future<void>.delayed(const Duration(milliseconds: 100));
    expect(server.received(5), 64 * 1024);

    open(6, path: '/../../data/secret');
    open(7, method: 'PUT', path: '/upload');
    expect(server.resets.keys, containsAll([6, 7]));
    expect(streams.activeStreams, 0);
  });

  test('many concurrent streams', () async {
    for (var i = 0; i < 20; i++) {
      local.files['/f$i'] = pattern(100000 + i * 991);
    }
    for (var i = 0; i < 20; i++) {
      open(100 + i, query: 'path=%2Ff$i');
    }
    await server.until(() => List.generate(20, (i) => server.ended.contains(100 + i)).every((e) => e));
    for (var i = 0; i < 20; i++) {
      expect(server.bodies[100 + i]!.takeBytes(), local.files['/f$i']);
    }
  });

  test('the tunnel closing aborts open streams', () async {
    local.files['/big'] = pattern(2 * 1024 * 1024);
    open(8, query: 'path=%2Fbig', window: 64 * 1024);
    await server.until(() => server.received(8) > 0);
    streams.closeAll();
    expect(streams.activeStreams, 0);
  });

  test('hello proof matches the server (companionHelloProof)', () {
    expect(CompanionFileServer.helloProof('s3cret', 'dev_1', '00112233445566778899aabbccddeeff'),
        '380ca98629f8f6814373f1f8af97796d9ab6e657731df29f18fcdd99dd4942c3');
  });

  test('Tailscale addresses', () {
    for (final ip in ['100.64.0.1', '100.100.10.1', '100.127.255.254', 'fd7a:115c:a1e0::1']) {
      expect(CompanionFileServer.isTailnetAddress(ip), isTrue, reason: ip);
    }
    for (final ip in ['100.63.255.255', '100.128.0.1', '192.168.1.5', '10.0.0.1', 'fd7a:115c:a1e1::1', 'nonsense']) {
      expect(CompanionFileServer.isTailnetAddress(ip), isFalse, reason: ip);
    }
  });
}
