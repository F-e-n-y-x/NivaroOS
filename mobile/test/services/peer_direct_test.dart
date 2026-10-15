import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/models/file_entry.dart';
import 'package:nivaroos_mobile/screens/files/resumable_upload.dart';
import 'package:nivaroos_mobile/screens/files/transfers.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/device_sync_service.dart';
import 'package:nivaroos_mobile/services/peer_direct.dart';

final phoneA = CompanionDevice(id: 'A', name: 'A', model: '', osVersion: '', appVersion: '', platform: 'android', ipAddress: '', storagePath: '/DATA/Companion/A');

/// Phone A on the tailnet ("100.101.1.2:8765") and the server, as one mock:
/// [direct] answers A's /download (null: A doesn't answer).
MockClient world({required http.StreamedResponse? Function(http.BaseRequest r)? direct, List<String> heads = const []}) =>
    MockClient.streaming((req, body) async {
      if (req.url.host == '100.101.1.2') {
        if (req.headers['X-Peer-Ticket'] != 'T' || req.headers.containsKey('Authorization')) return http.StreamedResponse(const Stream.empty(), 401);
        final r = direct?.call(req);
        if (r == null) throw const SocketException('no route');
        return r;
      }
      if (req.url.host == '100.101.9.9') throw const SocketException('no route');
      if (req.url.path == '/v1/file') {
        return http.StreamedResponse(Stream.value(utf8.encode('via-server')), 200, contentLength: 10);
      }
      return http.StreamedResponse(const Stream.empty(), 404);
    });

PeerDirect peers({bool tailnet = true, bool ticketOk = true}) => PeerDirect(
      tailnetIps: () async => tailnet ? ['100.64.0.9'] : [],
      devices: () async => [phoneA],
      ticket: (id, body) async {
        if (!ticketOk) throw ApiException('no direct route', statusCode: 409);
        expect(body, {'path': '/DATA/Companion/A/DCIM/x.mp4', 'op': 'read'});
        return {
          'data': {
            'ticket': 'T',
            'path': '/storage/emulated/0/DCIM/x.mp4',
            'candidates': ['100.101.9.9:8765', '100.101.1.2:8765'],
            'expires_at': DateTime.now().add(const Duration(hours: 2)).toUtc().toIso8601String(),
          },
        };
      },
    );

void main() {
  setUp(() {
    FlutterSecureStorage.setMockInitialValues({});
    ApiClient.instance.setBaseUrl('http://nas.test');
    ApiClient.instance.setSession('t', 'r');
  });

  http.StreamedResponse ok(String s) => http.StreamedResponse(Stream.value(utf8.encode(s)), 200, contentLength: s.length);

  test('the candidate that answers wins; the ticket is a header, not in the URL', () async {
    final link = await http.runWithClient(() => peers().resolve('/DATA/Companion/A/DCIM/x.mp4'), () => world(direct: (_) => ok('')));
    expect(link?.uri.toString(), 'http://100.101.1.2:8765/download?path=%2Fstorage%2Femulated%2F0%2FDCIM%2Fx.mp4');
    expect(link?.headers, {'X-Peer-Ticket': 'T'});
    expect(link!.uri.toString(), isNot(contains('T&')));
  });

  test('the server when this phone has no Tailscale, the server says no, or A is not answering', () async {
    final w = world(direct: (_) => ok(''));
    expect(await http.runWithClient(() => peers(tailnet: false).resolve('/DATA/Companion/A/DCIM/x.mp4'), () => w), isNull);
    expect(await http.runWithClient(() => peers(ticketOk: false).resolve('/DATA/Companion/A/DCIM/x.mp4'), () => w), isNull);
    expect(await http.runWithClient(() => peers().resolve('/DATA/Media/x.mp4'), () => w), isNull, reason: 'not a phone');
    expect(await http.runWithClient(() => peers().resolve('/DATA/Companion/A/DCIM/x.mp4'), () => world(direct: null)), isNull);
  });

  test('a pull comes straight from the phone, and through the server once the phone drops', () async {
    final dir = await Directory.systemTemp.createTemp('peer');
    addTearDown(() => dir.delete(recursive: true));
    const entry = FileEntry(name: 'x.mp4', path: '/DATA/Companion/A/DCIM/x.mp4', isDir: false, size: 10);
    var drop = false;
    final routes = <String?>[];
    final p = peers();
    Future<TransferOutcome> pull() => http.runWithClient(
          () => DownloadTask(items: const [DownloadItem(entry, 'x.mp4')], destDir: dir.path, peers: p).run((pr) => routes.add(pr.route), TransferCancel()),
          () => world(direct: (req) {
            if (req.method == 'HEAD') return ok('');
            if (!drop) return ok('from-phone');
            return http.StreamedResponse(Stream.fromFutures([Future.value(utf8.encode('from')), Future.error(const SocketException('reset'))]), 200, contentLength: 10);
          }),
        );

    expect((await pull()).failed, isFalse);
    expect(File('${dir.path}/x.mp4').readAsStringSync(), 'from-phone');
    expect(routes, contains('Direct · Tailscale'));

    drop = true;
    routes.clear();
    File('${dir.path}/x.mp4').deleteSync();
    expect((await pull()).failed, isFalse);
    expect(File('${dir.path}/x.mp4').readAsStringSync(), 'via-server');
    expect(routes.last, '');
  });
}
