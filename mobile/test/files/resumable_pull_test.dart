import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/models/file_entry.dart';
import 'package:nivaroos_mobile/screens/files/resumable_pull.dart';
import 'package:nivaroos_mobile/screens/files/resumable_upload.dart';
import 'package:nivaroos_mobile/screens/files/transfers.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/device_sync_service.dart';
import 'package:nivaroos_mobile/services/peer_direct.dart';
import 'package:nivaroos_mobile/services/pull_job.dart';

const remote = '/DATA/Companion/A/DCIM/big.mp4';
final blob = Uint8List.fromList(List.generate(4 << 20, (i) => (i * 31 + i ~/ 977) & 0xff));

/// Phone A's file over both routes: directly (host 100.101.1.2, with the
/// ticket) and through the server (/v1/file). Both serve Range with the
/// phone's ETag and If-Range. [dropDirect]/[dropRelay]: the route breaks
/// after that many bytes of a response (null: never).
class TwoRoutes {
  TwoRoutes({this.dropDirect, this.dropRelay});
  int? dropDirect, dropRelay;
  String etag = '"4194304-abc"';
  Uint8List data = blob;
  final requests = <String>[]; // "direct bytes=123-" / "relay full"

  MockClient get client => MockClient.streaming((req, _) async {
        final direct = req.url.host == '100.101.1.2';
        if (!direct && req.url.path != '/v1/file') return http.StreamedResponse(const Stream.empty(), 404);
        if (direct && req.headers['X-Peer-Ticket'] != 'T') return http.StreamedResponse(const Stream.empty(), 401);
        if (req.method == 'HEAD') return http.StreamedResponse(const Stream.empty(), 200);
        final range = req.headers['Range'];
        final ifRange = req.headers['If-Range'];
        var start = 0;
        if (range != null && (ifRange == null || ifRange == etag)) start = int.parse(RegExp(r'bytes=(\d+)-').firstMatch(range)!.group(1)!);
        requests.add('${direct ? 'direct' : 'relay'} ${start > 0 ? 'from $start' : 'full'}');
        final drop = direct ? dropDirect : dropRelay;
        // Each route breaks once.
        if (direct) {
          dropDirect = null;
        } else {
          dropRelay = null;
        }
        final body = data.sublist(start);
        Stream<List<int>> stream() async* {
          for (var i = 0; i < body.length; i += 64 << 10) {
            if (drop != null && i >= drop) throw const SocketException('Connection reset by peer');
            yield body.sublist(i, (i + (64 << 10)).clamp(0, body.length));
          }
        }

        return http.StreamedResponse(stream(), start > 0 ? 206 : 200, contentLength: body.length, headers: {
          'etag': etag,
          if (start > 0) 'content-range': 'bytes $start-${data.length - 1}/${data.length}',
        });
      });
}

PeerDirect peers({bool tailnet = true}) => PeerDirect(
      tailnetIps: () async => tailnet ? ['100.64.0.9'] : [],
      devices: () async => [CompanionDevice(id: 'A', name: 'A', model: '', osVersion: '', appVersion: '', platform: '', ipAddress: '', storagePath: '/DATA/Companion/A')],
      ticket: (id, body) async => {
        'data': {
          'ticket': 'T',
          'path': '/storage/emulated/0/DCIM/big.mp4',
          'candidates': ['100.101.1.2:8765'],
          'expires_at': DateTime.now().add(const Duration(hours: 2)).toUtc().toIso8601String(),
        },
      },
    );

void main() {
  late Directory dir;
  setUp(() async {
    FlutterSecureStorage.setMockInitialValues({});
    ApiClient.instance.setBaseUrl('http://nas.test');
    ApiClient.instance.setSession('t', 'r');
    dir = await Directory.systemTemp.createTemp('pull');
  });
  tearDown(() => dir.delete(recursive: true));

  Future<TransferOutcome> pull(TwoRoutes w, {PeerDirect? p, int attempts = 6}) => http.runWithClient(
        () => DownloadTask(
          items: [DownloadItem(FileEntry(name: 'big.mp4', path: remote, isDir: false, size: blob.length), 'big.mp4')],
          destDir: dir.path,
          puller: ResumablePull(peers: p ?? peers(), attempts: attempts, backoff: (_) => Duration.zero),
        ).run((_) {}, TransferCancel()),
        () => w.client,
      );

  void sameAsBlob() {
    expect(File('${dir.path}/big.mp4').readAsBytesSync(), blob);
    expect(dir.listSync().map((e) => e.path.split('/').last), ['big.mp4'], reason: 'no part or validator left');
  }

  test('a direct pull that drops goes on through the server from the byte it reached', () async {
    final w = TwoRoutes(dropDirect: 1 << 20);
    expect((await pull(w)).failed, isFalse);
    sameAsBlob();
    expect(w.requests, ['direct full', 'relay from ${1 << 20}']);
  });

  test('a pull through the server that drops resumes directly once the phone is reachable', () async {
    final w = TwoRoutes(dropRelay: 2 << 20);
    expect((await pull(w, p: peers(tailnet: false))).failed, isFalse);
    sameAsBlob();
    expect(w.requests, ['relay full', 'relay from ${2 << 20}']);

    // Interrupted for good (no retries left), then pulled again later on
    // the tailnet: the next run resumes the part file directly.
    File('${dir.path}/big.mp4').deleteSync();
    final w2 = TwoRoutes(dropRelay: 3 << 20);
    await expectLater(pull(w2, p: peers(tailnet: false), attempts: 1), throwsA(isA<SocketException>()));
    expect(w2.requests, ['relay full']);
    expect((await pull(w2)).failed, isFalse);
    sameAsBlob();
    expect(w2.requests, ['relay full', 'direct from ${3 << 20}']);
  });

  test('a file that changed in between is pulled whole, not glued onto the old bytes', () async {
    final w = TwoRoutes(dropRelay: 1 << 20);
    await expectLater(pull(w, p: peers(tailnet: false), attempts: 1), throwsA(isA<SocketException>()));
    w.etag = '"4194304-new"';
    w.data = Uint8List.fromList(blob.reversed.toList());
    expect((await pull(w, p: peers(tailnet: false))).failed, isFalse);
    expect(File('${dir.path}/big.mp4').readAsBytesSync(), w.data);
    expect(w.requests.last, 'relay full');
  });

  group('large pulls run as an Android job', () {
    const big = FileEntry(name: 'film.mkv', path: '/DATA/Media/film.mkv', isDir: false, size: PullJob.minBytes);
    Future<(TransferOutcome, List<PullBatch>)> start({bool move = false, bool accepted = true}) async {
      final handed = <PullBatch>[];
      final outcome = await DownloadTask(
        items: const [DownloadItem(big, 'film.mkv')],
        destDir: dir.path,
        move: move,
        download: (remote, target, onProgress) async => target.writeAsStringSync('x'),
        background: (b) async {
          handed.add(b);
          return accepted;
        },
      ).run((_) {}, TransferCancel());
      return (outcome, handed);
    }

    test('a copy of 512 MB or more goes to the job', () async {
      final (outcome, handed) = await start();
      expect(outcome.failed, isFalse);
      expect(outcome.message, contains('in the background'));
      expect(handed.single.files.single.toJson(), {'remote': '/DATA/Media/film.mkv', 'size': PullJob.minBytes, 'target': '${dir.path}/film.mkv', 'state': 'pending', 'error': null});
    });

    test('a move, or a job Android refused, runs here', () async {
      expect((await start(move: true)).$2, isEmpty);
      final (outcome, handed) = await start(accepted: false);
      expect(handed, hasLength(1));
      expect(outcome.message, contains('arrive complete'), reason: 'it was pulled here (the fake download is short)');
    });

    test('a run cut off by the network waits, and the next run resumes the file', () async {
      final b = PullBatch(id: 'b1', server: 'http://nas.test', destDir: dir.path, files: [PullFile(remote: remote, size: blob.length, target: '${dir.path}/big.mp4')]);
      final saved = <String>[];
      final w = TwoRoutes(dropRelay: 1 << 20);
      Future<PullRunEnd> run() => http.runWithClient(
            () => runPull(b, ResumablePull(peers: peers(tailnet: false), attempts: 1, backoff: (_) => Duration.zero), TransferCancel(),
                save: (b) => saved.add(b.files.single.state.name)),
            () => w.client,
          );
      expect(await run(), PullRunEnd.waitNetwork);
      expect(b.files.single.state, PullState.pending);
      expect(await run(), PullRunEnd.finished);
      expect(b.files.single.state, PullState.done);
      expect(saved.last, 'done');
      sameAsBlob();
      expect(w.requests, ['relay full', 'relay from ${1 << 20}']);
      expect(pullFinalNotice(b, PullRunEnd.finished).title, 'Downloaded 1 file');
    });
  });
}
