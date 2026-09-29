// End to end with the real server code: the Go test
// TestCompanionTunnelDartPhone (services/core/route/v1) starts the server's
// tunnel endpoint and runs this with NIVAROOS_TUNNEL_WS set; this plays the
// phone - a local file server plus [TunnelStreams] on a real WebSocket -
// until the server closes the tunnel. Skipped when run on its own.
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/companion_tunnel_streams.dart';

void main() {
  final wsUrl = Platform.environment['NIVAROOS_TUNNEL_WS'];
  final mb = int.tryParse(Platform.environment['NIVAROOS_TUNNEL_MB'] ?? '') ?? 64;

  test('phone half against the server over a real WebSocket', () async {
    final blob = Uint8List(mb << 20);
    for (var i = 0; i < blob.length; i++) {
      blob[i] = (i * 7 + i ~/ 251) & 0xff;
    }
    var uploaded = 0;
    final local = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    local.listen((req) async {
      if (req.headers.value('x-companion-secret') != 's3cret') {
        req.response.statusCode = 401;
        await req.response.close();
        return;
      }
      if (req.uri.path == '/upload') {
        await for (final c in req) {
          uploaded += c.length;
        }
        req.response.write('{"success":true}');
        await req.response.close();
        return;
      }
      if (req.uri.path == '/files') {
        req.response.write('{"success":true,"files":[{"name":"DCIM","is_dir":true}]}');
        await req.response.close();
        return;
      }
      var start = 0, end = blob.length - 1;
      final range = req.headers.value('range');
      if (range != null) {
        final p = range.substring(6).split('-');
        start = int.parse(p[0]);
        if (p[1].isNotEmpty) end = int.parse(p[1]);
        req.response.statusCode = 206;
        req.response.headers.set('Content-Range', 'bytes $start-$end/${blob.length}');
      }
      req.response.contentLength = end - start + 1;
      try {
        await req.response.addStream(Stream.fromIterable([
          for (var i = start; i <= end; i += 64 * 1024) Uint8List.sublistView(blob, i, (i + 64 * 1024).clamp(0, end + 1)),
        ]));
        await req.response.close();
      } catch (_) {}
    });

    final ws = await WebSocket.connect(wsUrl!);
    final streams = TunnelStreams(
      send: ws.add,
      localBase: () => Uri(scheme: 'http', host: '127.0.0.1', port: local.port),
      secret: () async => 's3cret',
    );
    ws.add(jsonEncode({'action': 'register', 'port': 8765, ...streams.registerFields}));
    await for (final m in ws) {
      if (m is String) {
        streams.handleText(jsonDecode(m) as Map<String, dynamic>);
      } else {
        streams.handleFrame(m as List<int>);
      }
    }
    streams.closeAll();
    await local.close(force: true);
    // ignore: avoid_print
    print('phone: uploaded $uploaded bytes');
  }, skip: wsUrl == null ? 'run by the Go test TestCompanionTunnelDartPhone' : false, timeout: const Timeout(Duration(minutes: 5)));
}
