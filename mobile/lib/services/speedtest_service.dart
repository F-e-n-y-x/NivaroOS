import 'dart:async';

import 'package:clock/clock.dart';

import 'api_client.dart';
import 'speed/speed_engine.dart';

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

  /// This phone's internet connection, against the speedtest.net server
  /// nearest the phone (lowest ping of the ones speedtest.net suggests for
  /// its IP) - the same method as the server's own test. It has nothing to
  /// do with the NivaroOS server.
  Future<SpeedResult> runPhoneInternetTest({SpeedtestProgressCallback? onProgress}) async {
    onProgress?.call(const SpeedtestProgress(phase: SpeedtestPhase.connecting, fraction: 0.02));
    final pick = await nearestSpeedTarget();
    if (pick == null) {
      throw SpeedtestException("Couldn't reach a speedtest.net server. Check this phone's internet connection and try again.");
    }
    final t = pick.target;
    return _runPhoneTest(
      name: t.name,
      ping: t.ping,
      download: (_) => t.download(),
      upload: (_) => t.upload,
      // Test servers end each request at their own limit; 25 MB bodies are
      // re-sent until time is up.
      uploadBodyBytes: 25 * 1000 * 1000,
      unreachable: "Couldn't reach an internet speed test server. Check this phone's internet connection.",
      onProgress: onProgress,
    );
  }

  /// The link between this phone and the NivaroOS server, against the
  /// gateway's `/speedtest/{ping,download,upload}` (no size limit there).
  Future<SpeedResult> runPhoneToServerTest({SpeedtestProgressCallback? onProgress}) async {
    onProgress?.call(const SpeedtestProgress(phase: SpeedtestPhase.connecting, fraction: 0.02));
    final api = ApiClient.instance;
    return _runPhoneTest(
      name: api.buildUri('/').host,
      ping: api.buildUri('/speedtest/ping'),
      download: (_) => api.buildUri('/speedtest/download'),
      upload: (_) => api.buildUri('/speedtest/upload'),
      uploadBodyBytes: 200 * 1000 * 1000,
      unreachable: "Couldn't reach the server's speed test. Check the connection and try again.",
      onProgress: onProgress,
    );
  }

  /// Latency, then download, then upload with [SpeedEngine] (see there for
  /// how each is measured), reporting progress as it goes.
  Future<SpeedResult> _runPhoneTest({
    required String name,
    required Uri ping,
    required Uri Function(int stream) download,
    required Uri Function(int stream) upload,
    required int uploadBodyBytes,
    required String unreachable,
    SpeedtestProgressCallback? onProgress,
  }) async {
    final engine = SpeedEngine();
    onProgress?.call(SpeedtestProgress(phase: SpeedtestPhase.ping, fraction: 0.05, partial: SpeedResult(server: name)));
    final lat = await engine.latency(ping);
    if (lat == null) throw SpeedtestException(unreachable);
    var partial = SpeedResult(pingMs: _round(lat.pingMs), jitterMs: _round(lat.jitterMs), server: name);
    onProgress?.call(SpeedtestProgress(phase: SpeedtestPhase.ping, fraction: 0.12, partial: partial));

    final down = await engine.download(download,
        onLive: (mbps, f) => onProgress?.call(SpeedtestProgress(
              phase: SpeedtestPhase.download,
              liveMbps: mbps > 0 ? mbps : null,
              fraction: 0.12 + f * 0.46,
              partial: partial,
            )));
    if (down == null) throw SpeedtestException('The download test failed: no data arrived.');
    partial = partial.copyWith(downloadMbps: _round(down));

    final up = await engine.upload(upload,
        bodyBytes: uploadBodyBytes,
        onLive: (mbps, f) => onProgress?.call(SpeedtestProgress(
              phase: SpeedtestPhase.upload,
              liveMbps: mbps > 0 ? mbps : null,
              fraction: 0.58 + f * 0.4,
              partial: partial,
            )));
    if (up == null) throw SpeedtestException('The upload test failed: no data could be sent.');
    return partial.copyWith(uploadMbps: _round(up), testedAt: clock.now());
  }

  static double _round(double v) => (v * 10).roundToDouble() / 10;
}
