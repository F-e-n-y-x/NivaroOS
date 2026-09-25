import 'dart:async';
import 'dart:math';
import 'dart:typed_data';

import 'package:clock/clock.dart';
import 'package:http/http.dart' as http;

import 'api_client.dart';

/// Three different measurements, never mixed up (plan M-32):
///
/// - [SpeedtestKind.server]: the server's own internet connection, measured
///   by the server (`POST /v1/sys/speedtest`, then `GET …/status`), the
///   same test as the web UI's Network widget.
/// - [SpeedtestKind.phoneToServer]: the link between this phone and the
///   server, against the gateway's `/speedtest/{ping,download,upload}`.
/// - [SpeedtestKind.phoneInternet]: this phone's internet connection,
///   against public test servers. It has nothing to do with the server.
enum SpeedtestKind { server, phoneToServer, phoneInternet }

enum SpeedtestPhase {
  idle,
  connecting,
  ping,
  download,
  upload,
  completed,
  error,
}

/// A running test's progress. [liveMbps] is the rate of the phase in
/// progress; the finished parts are in [partial].
class SpeedtestProgress {
  const SpeedtestProgress({required this.phase, this.liveMbps, this.fraction, this.partial = const SpeedResult()});

  final SpeedtestPhase phase;
  final double? liveMbps;

  /// 0..1 when the test knows how far along it is; null otherwise.
  final double? fraction;
  final SpeedResult partial;
}

typedef SpeedtestProgressCallback = void Function(SpeedtestProgress progress);

/// A result. Any value is null until it has actually been measured.
class SpeedResult {
  const SpeedResult({this.downloadMbps, this.uploadMbps, this.pingMs, this.jitterMs, this.server, this.testedAt});

  final double? downloadMbps;
  final double? uploadMbps;
  final double? pingMs;
  final double? jitterMs;

  /// Who the test ran against ("Example ISP (Frankfurt)"), when known.
  final String? server;
  final DateTime? testedAt;

  SpeedResult copyWith({double? downloadMbps, double? uploadMbps, double? pingMs, double? jitterMs, String? server, DateTime? testedAt}) =>
      SpeedResult(
        downloadMbps: downloadMbps ?? this.downloadMbps,
        uploadMbps: uploadMbps ?? this.uploadMbps,
        pingMs: pingMs ?? this.pingMs,
        jitterMs: jitterMs ?? this.jitterMs,
        server: server ?? this.server,
        testedAt: testedAt ?? this.testedAt,
      );

  /// The `result` object of `/v1/sys/speedtest/status`.
  static SpeedResult? fromServerJson(Object? raw) {
    if (raw is! Map) return null;
    double? n(Object? v) => v is num ? v.toDouble() : null;
    final ts = raw['timestamp'];
    return SpeedResult(
      downloadMbps: n(raw['download_mbps']),
      uploadMbps: n(raw['upload_mbps']),
      pingMs: n(raw['ping_ms']),
      jitterMs: n(raw['jitter_ms']),
      server: (raw['server']?.toString().isNotEmpty ?? false) ? raw['server'].toString() : null,
      testedAt: ts is num && ts > 0 ? DateTime.fromMillisecondsSinceEpoch(ts.toInt() * 1000) : null,
    );
  }
}

class SpeedtestException implements Exception {
  SpeedtestException(this.message);
  final String message;
  @override
  String toString() => message;
}

class SpeedtestService {
  SpeedtestService._();
  static final SpeedtestService instance = SpeedtestService._();

  /// How often the server test's status is polled. Tests shorten it.
  static Duration serverPollInterval = const Duration(milliseconds: 700);

  /// The server gives up after a minute; stop waiting a little later.
  static Duration serverTimeout = const Duration(seconds: 75);

  // Public download files for the phone's internet test.
  static const List<String> _cdnEndpoints = [
    'https://proof.ovh.net/files/100Mb.dat',
    'https://cachefly.cachefly.net/100mb.test',
    'https://speedtest.selectel.ru/100MB',
    'https://ash-speed.hetzner.com/100MB.bin',
    'https://fsn1-speed.hetzner.com/100MB.bin',
  ];

  static SpeedtestPhase _serverPhase(String? p) => switch (p) {
        'ping' => SpeedtestPhase.ping,
        'download' => SpeedtestPhase.download,
        'upload' => SpeedtestPhase.upload,
        'done' => SpeedtestPhase.completed,
        'error' => SpeedtestPhase.error,
        _ => SpeedtestPhase.connecting,
      };

  /// The server's last finished test, if it has one (it keeps it in memory
  /// until it restarts). Null when there is none or the server can't say.
  Future<SpeedResult?> lastServerResult() async {
    final res = await ApiClient.instance.get('/sys/speedtest/status');
    final data = res['data'];
    if (data is! Map || data['phase'] != 'done') return null;
    return SpeedResult.fromServerJson(data['result']);
  }

  /// Runs the server's internet speed test and reports its progress. If a
  /// test is already running (started from the web UI, say), follows that
  /// one instead of failing.
  Future<SpeedResult> runServerSpeedtest({SpeedtestProgressCallback? onProgress}) async {
    onProgress?.call(const SpeedtestProgress(phase: SpeedtestPhase.connecting));
    try {
      await ApiClient.instance.post('/sys/speedtest');
    } on ApiException catch (e) {
      // 409: one is already running - watch it.
      if (e.statusCode != 409) rethrow;
    }
    final deadline = clock.now().add(serverTimeout);
    while (true) {
      await Future<void>.delayed(serverPollInterval);
      final res = await ApiClient.instance.get('/sys/speedtest/status');
      final data = res['data'];
      if (data is! Map) throw SpeedtestException('The server sent an unexpected answer.');
      final phase = _serverPhase(data['phase']?.toString());
      final partial = SpeedResult.fromServerJson(data['result']) ?? const SpeedResult();
      final running = data['running'] == true;
      if (!running) {
        if (phase == SpeedtestPhase.error) {
          final msg = data['error']?.toString() ?? '';
          throw SpeedtestException(msg.isEmpty ? 'The server could not finish the test.' : _sentence(msg));
        }
        if (phase == SpeedtestPhase.completed && partial.downloadMbps != null) return partial;
        throw SpeedtestException('The server stopped the test before it finished.');
      }
      final live = data['live_mbps'];
      onProgress?.call(SpeedtestProgress(
        phase: phase,
        liveMbps: live is num && live > 0 ? live.toDouble() : null,
        partial: partial,
      ));
      if (clock.now().isAfter(deadline)) {
        throw SpeedtestException('The server took too long to answer.');
      }
    }
  }

  static String _sentence(String s) => s.isEmpty ? s : '${s[0].toUpperCase()}${s.substring(1)}${s.endsWith('.') ? '' : '.'}';

  /// Measures this phone's internet connection against public test
  /// servers. Uses mobile data when the phone is not on Wi-Fi.
  Future<SpeedResult> runPhoneInternetTest({SpeedtestProgressCallback? onProgress}) async {
    onProgress?.call(const SpeedtestProgress(phase: SpeedtestPhase.connecting, fraction: 0.02));

    String? location;
    try {
      final traceRes = await http.get(Uri.parse('https://1.1.1.1/cdn-cgi/trace')).timeout(const Duration(seconds: 3));
      if (traceRes.statusCode == 200) {
        for (final line in traceRes.body.split('\n')) {
          if (line.startsWith('colo=')) location = 'Cloudflare ${line.substring(5).trim()}';
        }
      }
    } catch (_) {
      // Only a label; the test itself decides whether the internet works.
    }

    final pings = <int>[];
    for (var i = 0; i < 8; i++) {
      final sw = Stopwatch()..start();
      try {
        final res = await http.head(Uri.parse('https://1.1.1.1')).timeout(const Duration(seconds: 2));
        sw.stop();
        if (res.statusCode < 400) pings.add(max(1, sw.elapsedMilliseconds));
      } catch (_) {
        // A failed probe is not counted.
      }
      onProgress?.call(SpeedtestProgress(
        phase: SpeedtestPhase.ping,
        fraction: 0.02 + (i + 1) / 8 * 0.1,
        partial: SpeedResult(pingMs: pings.isEmpty ? null : _avg(pings)),
      ));
    }
    if (pings.isEmpty) {
      throw SpeedtestException("This phone can't reach the internet. Check its connection and try again.");
    }
    final ping = _avg(pings);
    final jitter = _jitter(pings, ping);
    var partial = SpeedResult(pingMs: ping, jitterMs: jitter, server: location);

    final down = await _measure(
      durationMs: 5500,
      workers: 8,
      phase: SpeedtestPhase.download,
      fractionStart: 0.12,
      fractionSpan: 0.48,
      partial: partial,
      onProgress: onProgress,
      work: (client, count, stop) => _downloadLoop(client, Uri.parse(_cdnEndpoints[count.id % _cdnEndpoints.length]), count, stop),
    );
    if (down == null) throw SpeedtestException('The download test failed: no data arrived.');
    partial = partial.copyWith(downloadMbps: down);

    final payload = _payload(2 * 1024 * 1024, 31);
    final up = await _measure(
      durationMs: 4500,
      workers: 4,
      phase: SpeedtestPhase.upload,
      fractionStart: 0.6,
      fractionSpan: 0.38,
      partial: partial,
      onProgress: onProgress,
      work: (client, count, stop) => _uploadLoop(client, Uri.parse('https://speed.cloudflare.com/__up'), payload, count, stop),
    );
    if (up == null) throw SpeedtestException('The upload test failed: no data could be sent.');
    return partial.copyWith(uploadMbps: up, testedAt: clock.now());
  }

  /// Measures the link between this phone and the server through the
  /// gateway's speed-test routes (no internet involved).
  Future<SpeedResult> runPhoneToServerTest({SpeedtestProgressCallback? onProgress}) async {
    onProgress?.call(const SpeedtestProgress(phase: SpeedtestPhase.connecting, fraction: 0.02));
    final api = ApiClient.instance;
    final pingUrl = api.buildUri('/speedtest/ping');
    final downUrl = api.buildUri('/speedtest/download');
    final upUrl = api.buildUri('/speedtest/upload');

    final pings = <int>[];
    for (var i = 0; i < 15; i++) {
      final sw = Stopwatch()..start();
      try {
        final res = await http.get(pingUrl).timeout(const Duration(milliseconds: 1500));
        sw.stop();
        if (res.statusCode == 200) pings.add(max(1, sw.elapsedMilliseconds));
      } catch (_) {
        // A failed probe is not counted.
      }
      onProgress?.call(SpeedtestProgress(
        phase: SpeedtestPhase.ping,
        fraction: 0.02 + (i + 1) / 15 * 0.1,
        partial: SpeedResult(pingMs: pings.isEmpty ? null : _avg(pings)),
      ));
    }
    if (pings.isEmpty) {
      throw SpeedtestException("Couldn't reach the server's speed test. Check the connection and try again.");
    }
    final ping = _avg(pings);
    var partial = SpeedResult(pingMs: ping, jitterMs: _jitter(pings, ping), server: api.buildUri('/').host);

    final down = await _measure(
      durationMs: 5000,
      workers: 8,
      phase: SpeedtestPhase.download,
      fractionStart: 0.12,
      fractionSpan: 0.48,
      partial: partial,
      onProgress: onProgress,
      work: (client, count, stop) => _downloadLoop(client, downUrl, count, stop),
    );
    if (down == null) throw SpeedtestException('The download test failed: no data arrived from the server.');
    partial = partial.copyWith(downloadMbps: down);

    final payload = _payload(2 * 1024 * 1024, 17);
    final up = await _measure(
      durationMs: 4500,
      workers: 6,
      phase: SpeedtestPhase.upload,
      fractionStart: 0.6,
      fractionSpan: 0.38,
      partial: partial,
      onProgress: onProgress,
      work: (client, count, stop) => _uploadLoop(client, upUrl, payload, count, stop),
    );
    if (up == null) throw SpeedtestException('The upload test failed: no data could be sent to the server.');
    return partial.copyWith(uploadMbps: up, testedAt: clock.now());
  }

  static double _avg(List<int> v) => v.reduce((a, b) => a + b) / v.length;
  static double _jitter(List<int> v, double avg) => v.map((p) => (p - avg).abs()).reduce((a, b) => a + b) / v.length;
  static Uint8List _payload(int size, int mul) => Uint8List.fromList(List<int>.generate(size, (i) => (i * mul) & 0xFF));

  /// Runs [workers] copies of [work] for [durationMs] and returns the
  /// steady rate in Mbps (the 85th percentile of a smoothed rate, so the
  /// TCP ramp-up doesn't pull it down), or null when nothing moved.
  Future<double?> _measure({
    required int durationMs,
    required int workers,
    required SpeedtestPhase phase,
    required double fractionStart,
    required double fractionSpan,
    required SpeedResult partial,
    required SpeedtestProgressCallback? onProgress,
    required Future<void> Function(http.Client client, _ByteCount count, bool Function() stop) work,
  }) async {
    final watch = Stopwatch()..start();
    final counts = List.generate(workers, _ByteCount.new);
    int total() => counts.fold(0, (s, c) => s + c.bytes);
    var smoothed = 0.0;
    final samples = <double>[];
    var lastT = 0;
    var lastBytes = 0;
    var stopped = false;
    bool stop() => stopped || watch.elapsedMilliseconds >= durationMs;

    final ticker = Timer.periodic(const Duration(milliseconds: 100), (_) {
      final now = watch.elapsedMilliseconds;
      final dt = now - lastT;
      if (dt <= 0) return;
      final bytes = total();
      final instant = (bytes - lastBytes) * 8.0 / (dt * 1000.0);
      lastT = now;
      lastBytes = bytes;
      if (instant > 0) {
        smoothed = smoothed == 0 ? instant : 0.35 * instant + 0.65 * smoothed;
        samples.add(smoothed);
      }
      onProgress?.call(SpeedtestProgress(
        phase: phase,
        liveMbps: smoothed > 0 ? smoothed : null,
        fraction: (fractionStart + now / durationMs * fractionSpan).clamp(fractionStart, fractionStart + fractionSpan),
        partial: partial,
      ));
    });

    final client = http.Client();
    try {
      await Future.wait([for (final c in counts) work(client, c, stop)])
          .timeout(Duration(milliseconds: durationMs + 800), onTimeout: () => const []);
    } finally {
      stopped = true;
      ticker.cancel();
      watch.stop();
      client.close();
    }

    if (samples.isNotEmpty) {
      samples.sort();
      return samples[(samples.length * 0.85).floor().clamp(0, samples.length - 1)];
    }
    final bytes = total();
    if (bytes > 0 && watch.elapsedMilliseconds > 0) return bytes * 8.0 / (watch.elapsedMilliseconds * 1000.0);
    return null;
  }

  static Future<void> _downloadLoop(http.Client client, Uri url, _ByteCount count, bool Function() stop) async {
    while (!stop()) {
      try {
        final req = http.Request('GET', url.replace(queryParameters: {...url.queryParameters, '_t': '${clock.now().microsecondsSinceEpoch}'}))
          ..headers['Cache-Control'] = 'no-cache';
        final res = await client.send(req).timeout(const Duration(seconds: 4));
        if (res.statusCode == 200 || res.statusCode == 206) {
          await for (final chunk in res.stream) {
            count.bytes += chunk.length;
            if (stop()) break;
          }
        } else {
          await Future<void>.delayed(const Duration(milliseconds: 50));
        }
      } catch (_) {
        await Future<void>.delayed(const Duration(milliseconds: 50));
      }
    }
  }

  static Future<void> _uploadLoop(http.Client client, Uri url, Uint8List payload, _ByteCount count, bool Function() stop) async {
    while (!stop()) {
      try {
        final res = await client
            .post(url, body: payload, headers: {'Content-Type': 'application/octet-stream'})
            .timeout(const Duration(seconds: 4));
        if (res.statusCode == 200 || res.statusCode == 204) {
          count.bytes += payload.length;
        } else {
          await Future<void>.delayed(const Duration(milliseconds: 50));
        }
      } catch (_) {
        await Future<void>.delayed(const Duration(milliseconds: 50));
      }
    }
  }
}

class _ByteCount {
  _ByteCount(this.id);
  final int id;
  int bytes = 0;
}
