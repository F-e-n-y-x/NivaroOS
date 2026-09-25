import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/services/speedtest_service.dart';

import '../screenshots/harness.dart';

http.Response _json(Object body, [int status = 200]) => http.Response(jsonEncode(body), status, headers: {'content-type': 'application/json'});

Map<String, Object> _status({required bool running, required String phase, double live = 0, Map<String, Object>? result, String? error}) => {
      'success': 200,
      'message': 'ok',
      'data': {'running': running, 'phase': phase, 'live_mbps': live, 'result': ?result, 'error': ?error},
    };

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() async {
    await signIn();
    SpeedtestService.serverPollInterval = Duration.zero;
  });

  test('server test: starts on the server, follows its phases, returns its result', () async {
    final requests = <String>[];
    final answers = [
      _status(running: true, phase: 'ping'),
      _status(running: true, phase: 'download', live: 250, result: {'ping_ms': 3.1, 'jitter_ms': 0.4}),
      _status(running: true, phase: 'upload', live: 40, result: {'ping_ms': 3.1, 'download_mbps': 289}),
      _status(running: false, phase: 'done', result: {
        'ping_ms': 3.1,
        'jitter_ms': 0.4,
        'download_mbps': 289,
        'upload_mbps': 41.6,
        'server': 'Example ISP',
        'timestamp': 1790330000,
      }),
    ];
    final progress = <SpeedtestProgress>[];
    final r = await http.runWithClient(
      () => SpeedtestService.instance.runServerSpeedtest(onProgress: progress.add),
      () => MockClient((req) async {
        requests.add('${req.method} ${req.url.path}');
        if (req.method == 'POST') return _json({'success': 200, 'message': 'ok', 'data': {}});
        return _json(answers.removeAt(0));
      }),
    );
    expect(requests.first, 'POST /v1/sys/speedtest');
    expect(requests.skip(1), everyElement('GET /v1/sys/speedtest/status'));
    expect(r.downloadMbps, 289);
    expect(r.uploadMbps, 41.6);
    expect(r.server, 'Example ISP');
    expect(r.testedAt, DateTime.fromMillisecondsSinceEpoch(1790330000 * 1000));
    expect(progress.map((p) => p.phase), containsAllInOrder([SpeedtestPhase.connecting, SpeedtestPhase.ping, SpeedtestPhase.download, SpeedtestPhase.upload]));
    expect(progress.firstWhere((p) => p.phase == SpeedtestPhase.download).liveMbps, 250);
  });

  test('server test: a test already running (409) is followed, not an error', () async {
    final answers = [
      _status(running: false, phase: 'done', result: {'download_mbps': 100, 'upload_mbps': 20, 'ping_ms': 5}),
    ];
    final r = await http.runWithClient(
      () => SpeedtestService.instance.runServerSpeedtest(),
      () => MockClient((req) async => req.method == 'POST'
          ? _json({'success': 500, 'message': 'a speed test is already running'}, 409)
          : _json(answers.removeAt(0))),
    );
    expect(r.downloadMbps, 100);
  });

  test("server test: the server's error is reported as a sentence", () async {
    await expectLater(
      http.runWithClient(
        () => SpeedtestService.instance.runServerSpeedtest(),
        () => MockClient((req) async => req.method == 'POST'
            ? _json({'success': 200, 'message': 'ok'})
            : _json(_status(running: false, phase: 'error', error: 'cannot reach speedtest.net'))),
      ),
      throwsA(isA<SpeedtestException>().having((e) => e.message, 'message', 'Cannot reach speedtest.net.')),
    );
  });

  test('last server result: only a finished test counts', () async {
    Future<SpeedResult?> last(Map<String, Object> status) =>
        http.runWithClient(() => SpeedtestService.instance.lastServerResult(), () => MockClient((_) async => _json(status)));
    expect(await last(_status(running: true, phase: 'download')), isNull);
    expect((await last(_status(running: false, phase: 'done', result: {'download_mbps': 9.5})))!.downloadMbps, 9.5);
  });

  test('result parsing keeps unknown values null', () {
    final r = SpeedResult.fromServerJson({'download_mbps': 10})!;
    expect(r.uploadMbps, isNull);
    expect(r.pingMs, isNull);
    expect(r.server, isNull);
    expect(r.testedAt, isNull);
  });
}
