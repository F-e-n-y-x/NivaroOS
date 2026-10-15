// Resumable pulls from the server, or straight from another phone (design
// 2026-10, phase 2).
//
// A file is written to a hidden part file next to its target. When the
// connection drops, the next attempt asks only for the rest:
// `Range: bytes=<what we have>-` with `If-Range: <validator>` (the ETag the
// first answer carried, or its Last-Modified). A 206 appends, a 200 means
// the file changed (or the source can't resume): start over. Each attempt
// picks its route again, so a pull that started directly over Tailscale
// carries on through the server - from the byte it reached - and the other
// way round. The part file and its validator (`<part>.v`) stay on disk, so
// a later run (the background job, or the user's retry) resumes too.
import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;

import '../../services/api_client.dart';
import '../../services/peer_direct.dart';
import 'file_ops.dart';
import 'resumable_upload.dart';

class ResumablePull {
  ResumablePull({
    PeerDirect? peers,
    this.attempts = 6,
    this.idleTimeout = const Duration(seconds: 45),
    Duration Function(int attempt)? backoff,
  })  : peers = peers ?? PeerDirect.instance,
        backoff = backoff ?? ((n) => Duration(seconds: 1 << n.clamp(0, 4)));

  final PeerDirect peers;
  final int attempts;

  /// A connection that sends nothing for this long is treated as dropped
  /// (Wi-Fi gone without a reset).
  final Duration idleTimeout;
  final Duration Function(int attempt) backoff;

  /// Whether the last bytes came straight from another phone.
  bool lastDirect = false;

  /// The hidden part file [target] is pulled into: next to it, short (a
  /// 255-byte name still fits), and the same for the same source and
  /// target, so another run finds it and resumes.
  static String partPath(String target, String remote, int size) =>
      joinPath(parentOf(target), '.nvpart-${sha1.convert(utf8.encode('$remote\n$size\n$target')).toString().substring(0, 16)}');

  /// Removes a part file and its validator.
  static Future<void> discard(File part) async {
    for (final f in [part, File('${part.path}.v')]) {
      try {
        if (await f.exists()) await f.delete();
      } catch (_) {}
    }
  }

  /// Pulls [remote] (a server path) into [part] until it holds [size]
  /// bytes or the attempts run out. [onProgress] gets the bytes in [part]
  /// so far (resumed ones included). Network failures are retried; a
  /// refusal (4xx) is not.
  Future<void> fetch(String remote, File part, int size, void Function(int have) onProgress, TransferCancel cancel) async {
    Object? last;
    for (var n = 0; n < attempts; n++) {
      cancel.throwIfCancelled();
      if (n > 0) {
        await _sleep(backoff(n - 1), cancel);
      }
      final direct = await peers.resolve(remote);
      try {
        if (await _attempt(remote, part, size, direct, onProgress, cancel)) return;
        last = null;
      } on TransferCancelled {
        rethrow;
      } catch (e) {
        cancel.throwIfCancelled();
        last = e;
        if (direct != null) {
          // The other phone stopped answering directly: the next attempt
          // goes through the server, from the same byte.
          peers.failed(direct);
          continue;
        }
        if (e is ApiException && !_retryable(e.statusCode)) rethrow;
      }
    }
    throw last ?? ApiException("Couldn't download “${baseName(remote)}”");
  }

  static bool _retryable(int? status) => status == null || status == 408 || status == 429 || status >= 500;

  static Future<void> _sleep(Duration d, TransferCancel cancel) async {
    final done = Completer<void>();
    final t = Timer(d, done.complete);
    final detach = cancel.onCancel(() {
      t.cancel();
      if (!done.isCompleted) done.complete();
    });
    await done.future;
    detach();
    cancel.throwIfCancelled();
  }

  /// One request. True when [part] is complete.
  Future<bool> _attempt(String remote, File part, int size, PeerLink? direct, void Function(int) onProgress, TransferCancel cancel) async {
    final vFile = File('${part.path}.v');
    var have = await part.exists() ? await part.length() : 0;
    final validator = await vFile.exists() ? (await vFile.readAsString()).trim() : '';
    if (have > size || (have > 0 && validator.isEmpty)) {
      // Nothing to check the bytes against: start over.
      await discard(part);
      have = 0;
    }
    if (have == size && size > 0) return true;
    lastDirect = direct != null;
    final uri = direct?.uri ?? ApiClient.instance.buildUri('/file', {'path': remote});
    final headers = {
      ...?direct?.headers,
      if (have > 0) 'Range': 'bytes=$have-',
      if (have > 0) 'If-Range': validator,
    };
    // Our own client, so Cancel can close it mid-file.
    final client = http.Client();
    final detach = cancel.onCancel(client.close);
    IOSink? sink;
    try {
      http.BaseRequest build() => http.Request('GET', uri)..headers.addAll(headers);
      final res = await (direct == null ? ApiClient.instance.send(build, client: client) : client.send(build())).timeout(idleTimeout);
      final append = res.statusCode == 206 && _rangeStart(res.headers['content-range']) == have;
      if (res.statusCode == 416 && have == size) {
        await res.stream.drain<void>();
        return true;
      }
      if (res.statusCode != 200 && !append) {
        await res.stream.drain<void>();
        throw ApiException("Couldn't download “${baseName(remote)}” (HTTP ${res.statusCode})", statusCode: res.statusCode);
      }
      if (!append) {
        have = 0;
        final v = res.headers['etag'];
        final lm = res.headers['last-modified'];
        // Weak ETags can't be used with If-Range.
        final next = v != null && !v.startsWith('W/') ? v : (lm ?? '');
        await discard(part);
        if (next.isNotEmpty) await vFile.writeAsString(next, flush: true);
      }
      sink = part.openWrite(mode: append ? FileMode.append : FileMode.write);
      await for (final chunk in res.stream.timeout(idleTimeout)) {
        sink.add(chunk);
        have += chunk.length;
        onProgress(have);
      }
      await sink.flush();
      return true;
    } catch (_) {
      cancel.throwIfCancelled();
      rethrow;
    } finally {
      await sink?.close();
      detach();
      client.close();
    }
  }

  static int? _rangeStart(String? contentRange) {
    final m = RegExp(r'^bytes (\d+)-').firstMatch(contentRange ?? '');
    return m == null ? null : int.parse(m.group(1)!);
  }
}
