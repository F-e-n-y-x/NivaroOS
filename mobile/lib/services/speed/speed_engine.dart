// Speed measurement for the phone's own tests (phone <-> server, and the
// phone's internet), measured the way the server's own test and
// speedtest.net do it:
//
// - Latency: one kept-alive connection, a warm-up request (it pays for DNS,
//   TCP and TLS), then the time to first byte of tiny requests. Ping = the
//   lowest, jitter = the mean change between consecutive samples. Opening a
//   new connection per probe - what the app used to do - measured two or
//   three round trips per "ping".
// - Throughput: several parallel streams for a fixed time, sampled in
//   slices; the result drops the slowest 30% of slices (TCP ramp-up,
//   stalls) and the fastest 10% (bursts) and averages the rest. Downloads
//   count bytes as they arrive; uploads stream a request body and count
//   bytes as the socket takes them, so there's no per-request overhead or
//   waiting for replies (the old 2 MB POSTs spent most of a gigabit link
//   waiting). Nothing is capped: streams re-request until time is up.
//
// Plain dart:io, no Flutter, so tool/speedbench.dart can run it on a
// desktop against a real server.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;
import 'dart:typed_data';

typedef LiveRate = void Function(double mbps, double fraction);

/// speedtest.net answers 403 to Dart's default user agent ("Dart/x
/// (dart:io)"), so every request says who it is.
const speedUserAgent = 'NivaroOS-speedtest/1.0';

class LatencyResult {
  const LatencyResult(this.pingMs, this.jitterMs, this.samples);
  final double pingMs;
  final double jitterMs;
  final int samples;
}

class SpeedEngine {
  SpeedEngine({this.streams = 8, this.duration = const Duration(seconds: 8), this.slice = const Duration(milliseconds: 200)});

  final int streams;
  final Duration duration;
  final Duration slice;

  HttpClient _client() => HttpClient()
    ..userAgent = speedUserAgent
    ..autoUncompress = false
    ..maxConnectionsPerHost = streams + 2
    ..idleTimeout = const Duration(seconds: 15)
    ..connectionTimeout = const Duration(seconds: 5);

  /// Time to first byte of [samples] tiny GETs to [url] on one connection.
  Future<LatencyResult?> latency(Uri url, {int samples = 15, Duration timeout = const Duration(seconds: 3)}) async {
    final client = _client()..maxConnectionsPerHost = 1;
    final times = <double>[];
    try {
      for (var i = 0; i <= samples; i++) {
        final sw = Stopwatch();
        try {
          final req = await client.getUrl(_noCache(url)).timeout(timeout);
          req.headers.set(HttpHeaders.cacheControlHeader, 'no-cache');
          // A server that redirects (e.g. http -> https) isn't timed or
          // picked: uploads can't follow a redirect.
          req.followRedirects = false;
          sw.start();
          final res = await req.close().timeout(timeout);
          sw.stop();
          await res.drain<void>();
          if (i == 0) continue; // the warm-up pays for DNS + TCP + TLS
          if (res.statusCode >= 200 && res.statusCode < 300) times.add(sw.elapsedMicroseconds / 1000.0);
        } catch (_) {
          // A lost probe isn't counted.
        }
      }
    } finally {
      client.close(force: true);
    }
    if (times.isEmpty) return null;
    var diffs = 0.0;
    for (var i = 1; i < times.length; i++) {
      diffs += (times[i] - times[i - 1]).abs();
    }
    final sorted = [...times]..sort();
    return LatencyResult(sorted.first, times.length > 1 ? diffs / (times.length - 1) : 0, times.length);
  }

  /// Download throughput in Mbps; [urlFor] gives stream n its URL (so
  /// streams can spread over mirrors). Null when nothing arrived.
  Future<double?> download(Uri Function(int stream) urlFor, {LiveRate? onLive}) {
    return _run(onLive, (client, count, stop) async {
      final id = count.id;
      while (!stop()) {
        try {
          final req = await client.getUrl(_noCache(urlFor(id))).timeout(const Duration(seconds: 5));
          req.headers.set(HttpHeaders.cacheControlHeader, 'no-cache');
          final res = await req.close().timeout(const Duration(seconds: 5));
          if (res.statusCode != 200 && res.statusCode != 206) {
            await res.drain<void>();
            count.lastStatus = res.statusCode;
            await Future<void>.delayed(const Duration(milliseconds: 100));
            continue;
          }
          final done = Completer<void>();
          late StreamSubscription<List<int>> sub;
          sub = res.listen(
            (chunk) {
              count.bytes += chunk.length;
              if (stop()) {
                sub.cancel();
                if (!done.isCompleted) done.complete();
              }
            },
            onDone: () {
              if (!done.isCompleted) done.complete();
            },
            onError: (Object _) {
              if (!done.isCompleted) done.complete();
            },
            cancelOnError: true,
          );
          await done.future;
        } catch (_) {
          await Future<void>.delayed(const Duration(milliseconds: 100));
        }
      }
    });
  }

  /// Upload throughput in Mbps: each stream sends one long request body
  /// (re-sent if the server ends it) and counts what the socket accepted.
  Future<double?> upload(Uri Function(int stream) urlFor, {int bodyBytes = 200 * 1000 * 1000, LiveRate? onLive}) {
    final chunk = Uint8List(256 * 1024);
    final rnd = math.Random(7);
    for (var i = 0; i < chunk.length; i++) {
      chunk[i] = rnd.nextInt(256); // incompressible
    }
    return _run(onLive, (client, count, stop) async {
      final id = count.id;
      while (!stop()) {
        HttpClientRequest? req;
        try {
          req = await client.postUrl(_noCache(urlFor(id))).timeout(const Duration(seconds: 5));
          req.headers.contentType = ContentType.binary;
          req.contentLength = bodyBytes;
          var sent = 0;
          while (sent < bodyBytes && !stop()) {
            final n = math.min(chunk.length, bodyBytes - sent);
            req.add(n == chunk.length ? chunk : Uint8List.sublistView(chunk, 0, n));
            // flush() completes once the socket took the data - that's the
            // back-pressure that makes the count honest.
            await req.flush();
            sent += n;
            count.bytes += n;
          }
          if (stop()) {
            req.abort();
            break;
          }
          final res = await req.close().timeout(const Duration(seconds: 10));
          await res.drain<void>();
          if (res.statusCode >= 400) {
            count.lastStatus = res.statusCode;
            await Future<void>.delayed(const Duration(milliseconds: 100));
          }
        } catch (_) {
          req?.abort();
          await Future<void>.delayed(const Duration(milliseconds: 100));
        }
      }
    });
  }

  Future<double?> _run(LiveRate? onLive, Future<void> Function(HttpClient client, _Count count, bool Function() stop) worker) async {
    final client = _client();
    final counts = List.generate(streams, _Count.new);
    final watch = Stopwatch()..start();
    var stopped = false;
    bool stop() => stopped || watch.elapsed >= duration;
    int total() => counts.fold(0, (s, c) => s + c.bytes);

    final slices = <double>[];
    var lastBytes = 0;
    var lastMicros = 0;
    final ticker = Timer.periodic(slice, (_) {
      final now = watch.elapsedMicroseconds;
      final bytes = total();
      final dt = now - lastMicros;
      if (dt > 0) slices.add((bytes - lastBytes) * 8 / dt); // bits per microsecond = Mbps
      lastBytes = bytes;
      lastMicros = now;
      onLive?.call(trimmedMean(slices), math.min(1, now / duration.inMicroseconds));
    });
    try {
      await Future.wait([for (final c in counts) worker(client, c, stop)])
          .timeout(duration + const Duration(seconds: 3), onTimeout: () => const []);
    } finally {
      stopped = true;
      ticker.cancel();
      watch.stop();
      client.close(force: true);
    }
    if (total() == 0) return null;
    if (slices.length < 3) return total() * 8 / watch.elapsedMicroseconds;
    return trimmedMean(slices);
  }

  static Uri _noCache(Uri u) => u.replace(queryParameters: {...u.queryParameters, 'nocache': '${DateTime.now().microsecondsSinceEpoch}'});
}

/// Mean of [samples] without the slowest 30% and the fastest 10%.
double trimmedMean(List<double> samples) {
  if (samples.isEmpty) return 0;
  final s = [...samples]..sort();
  var lo = s.length * 3 ~/ 10;
  var hi = s.length - s.length ~/ 10;
  if (hi <= lo) {
    lo = 0;
    hi = s.length;
  }
  var sum = 0.0;
  for (var i = lo; i < hi; i++) {
    sum += s[i];
  }
  return sum / (hi - lo);
}

class _Count {
  _Count(this.id);
  final int id;
  int bytes = 0;
  int? lastStatus;
}

/// A speedtest.net server an internet test runs against.
class SpeedTarget {
  const SpeedTarget({required this.name, required this.ping, required this.download, required this.upload});
  final String name;
  final Uri ping;
  final Uri Function() download;
  final Uri upload;

  /// [scheme] is https for the JSON list's hosts, http for the XML list's
  /// host:8080 entries.
  static SpeedTarget ookla(String host, String name, {String scheme = 'https'}) => SpeedTarget(
        name: name,
        ping: Uri.parse('$scheme://$host/hello'),
        // 25 MB per request; the streams re-request until time is up.
        download: () => Uri.parse('$scheme://$host/download?size=25000000'),
        upload: Uri.parse('$scheme://$host/upload'),
      );
}

/// speedtest.net's servers nearest this device's public IP (its JSON API,
/// else its older XML list - the same sources as the server's own test),
/// the one with the lowest latency, or null when none answers. Cloudflare
/// is deliberately not used: its numbers were well off real line speed.
Future<({SpeedTarget target, LatencyResult latency})?> nearestSpeedTarget({int candidates = 5}) async {
  final client = HttpClient()
    ..userAgent = speedUserAgent
    ..connectionTimeout = const Duration(seconds: 5);
  Future<String?> get(String url) async {
    try {
      final req = await client.getUrl(Uri.parse(url)).timeout(const Duration(seconds: 6));
      final res = await req.close().timeout(const Duration(seconds: 6));
      final body = await res.transform(utf8.decoder).join();
      return res.statusCode == 200 ? body : null;
    } catch (_) {
      return null;
    }
  }

  var list = <SpeedTarget>[];
  try {
    final json = await get('https://www.speedtest.net/api/js/servers?engine=js&https_functional=true&limit=10');
    if (json != null) list = parseOoklaJson(json);
    if (list.isEmpty) {
      final xml = await get('https://www.speedtest.net/speedtest-servers-static.php');
      if (xml != null) list = parseOoklaXml(xml);
    }
  } finally {
    client.close(force: true);
  }

  final probe = SpeedEngine(streams: 1);
  SpeedTarget? best;
  LatencyResult? bestLat;
  await Future.wait([
    for (final t in list.take(candidates))
      probe.latency(t.ping, samples: 4, timeout: const Duration(seconds: 2)).then((l) {
        if (l != null && (bestLat == null || l.pingMs < bestLat!.pingMs)) {
          best = t;
          bestLat = l;
        }
      }),
  ]);
  if (best == null || bestLat == null) return null;
  return (target: best!, latency: bestLat!);
}

String _label(String sponsor, String city) => city.isEmpty ? sponsor : (sponsor.isEmpty ? city : '$sponsor ($city)');

/// speedtest.net's `/api/js/servers` list, nearest first.
List<SpeedTarget> parseOoklaJson(String body) {
  try {
    final v = jsonDecode(body);
    if (v is! List) return const [];
    return [
      for (final e in v)
        if (e is Map && (e['host']?.toString() ?? '').isNotEmpty)
          SpeedTarget.ookla(e['host'].toString(), _label(e['sponsor']?.toString() ?? '', e['name']?.toString() ?? '')),
    ];
  } catch (_) {
    return const [];
  }
}

/// speedtest.net's `speedtest-servers-static.php` XML (`<server host="h:8080"
/// name="City" sponsor="ISP" .../>`), nearest first.
List<SpeedTarget> parseOoklaXml(String body) {
  String? attr(String tag, String name) => RegExp('\\b$name="([^"]*)"').firstMatch(tag)?.group(1);
  return [
    for (final m in RegExp(r'<server\b[^>]*>').allMatches(body))
      if ((attr(m.group(0)!, 'host') ?? '').isNotEmpty)
        SpeedTarget.ookla(attr(m.group(0)!, 'host')!, _label(attr(m.group(0)!, 'sponsor') ?? '', attr(m.group(0)!, 'name') ?? ''), scheme: 'http'),
  ];
}
