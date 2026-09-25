// Uploads from the phone to the server with core's resumable, size-checked
// protocol (plan M-12): `/v2/casaos/file/upload`, the same one the web UI's
// upload tray uses (ui/src/apps/files/UploadTray.vue, simple-uploader.js;
// server services/core/route/v2/file.go and service/file_upload.go).
//
// For each 8 MiB chunk:
//   GET  ...?chunkNumber=n&...  200 = the server already has it (resume),
//                               204 = send it;
//   POST multipart (the same fields + "file"), 200 {"complete": bool}.
// The chunk that completes the file makes the server fsync it, check its
// size and only then rename it into place, so a half-sent file never
// appears under its real name.
//
// The token is read before every request and a 401 refreshes it once
// (plan M-16), so a long upload survives the 3 h token lifetime.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'package:http/http.dart' as http;

import '../../services/api_client.dart';

/// 8 MiB: every request stays under common proxy body limits (Cloudflare's
/// 100 MB, nginx's client_max_body_size), and a failed chunk is cheap to
/// resend. The web UI uses the same size.
const int uploadChunkSize = 8 * 1024 * 1024;

/// An upload that can't succeed by retrying (a bad path, a folder where
/// the file should go, no permission), or that ran out of retries.
class UploadException implements Exception {
  UploadException(this.message, {this.statusCode});
  final String message;
  final int? statusCode;

  @override
  String toString() => message;
}

/// Thrown when the user cancelled.
class TransferCancelled implements Exception {
  const TransferCancelled();
  @override
  String toString() => 'Cancelled';
}

/// Cancels a running transfer: checked between pieces, and it closes the
/// HTTP client of a request in flight so that stops at once.
class TransferCancel {
  bool _cancelled = false;
  final _listeners = <void Function()>[];

  bool get isCancelled => _cancelled;

  void cancel() {
    if (_cancelled) return;
    _cancelled = true;
    for (final l in List.of(_listeners)) {
      l();
    }
  }

  void Function() onCancel(void Function() listener) {
    _listeners.add(listener);
    return () => _listeners.remove(listener);
  }

  void throwIfCancelled() {
    if (_cancelled) throw const TransferCancelled();
  }
}

/// The chunk fields both requests carry, in simple-uploader.js's names.
Map<String, String> uploadChunkFields({
  required String destDir,
  required String relativePath,
  required int totalSize,
  required int chunkNumber,
  required int chunkSize,
}) {
  final chunks = uploadTotalChunks(totalSize, chunkSize);
  final start = (chunkNumber - 1) * chunkSize;
  final current = chunks == 1 ? totalSize : math.min(chunkSize, totalSize - start);
  final name = relativePath.split('/').last;
  return {
    'path': destDir,
    'relativePath': relativePath,
    'filename': name,
    'identifier': uploadIdentifier(totalSize, relativePath),
    'chunkNumber': '$chunkNumber',
    'chunkSize': '$chunkSize',
    'currentChunkSize': '$current',
    'totalChunks': '$chunks',
    'totalSize': '$totalSize',
  };
}

/// Fixed-size chunks with a shorter last one; an empty file is one empty
/// chunk.
int uploadTotalChunks(int totalSize, int chunkSize) => math.max(1, (totalSize / chunkSize).ceil());

/// simple-uploader.js's default identifier (size + the path with anything
/// but letters, digits, "_" and "-" removed). The server keys an upload by
/// destination + relative path + identifier + size, so a retry of the same
/// file resumes and two different files never mix.
String uploadIdentifier(int totalSize, String relativePath) =>
    '$totalSize-${relativePath.replaceAll(RegExp(r'[^0-9A-Za-z_-]'), '')}';

/// Uploads one local file. Create one per transfer; it holds no state
/// between files.
class ResumableUploader {
  ResumableUploader({
    this.chunkSize = uploadChunkSize,
    this.maxRetries = 4,
    this.retryDelay = const Duration(milliseconds: 1500),
    Future<String> Function()? authHeader,
    Future<bool> Function()? refreshToken,
    Uri Function(Map<String, String> query)? endpoint,
  })  : _authHeader = authHeader ?? ApiClient.instance.currentAuthHeader,
        _refresh = refreshToken ?? (() async => await ApiClient.instance.refresh() == RefreshResult.refreshed),
        _endpoint = endpoint ?? ((q) => ApiClient.instance.buildUri('/v2/casaos/file/upload', q.isEmpty ? null : q));

  final int chunkSize;
  final int maxRetries;
  final Duration retryDelay;
  final Future<String> Function() _authHeader;
  final Future<bool> Function() _refresh;
  final Uri Function(Map<String, String> query) _endpoint;

  /// Statuses that retrying won't fix (simple-uploader's permanentErrors).
  static const permanentStatuses = {400, 404, 409, 415, 501};

  /// Sends [file] to `destDir/relativePath` on the server. [onProgress]
  /// gets the bytes of this file the server has (resumed chunks count at
  /// once). Throws [UploadException], or [TransferCancelled].
  Future<void> upload({
    required File file,
    required String destDir,
    required String relativePath,
    void Function(int sent, int total)? onProgress,
    TransferCancel? cancel,
  }) async {
    final total = await file.length();
    final chunks = uploadTotalChunks(total, chunkSize);
    final raf = await file.open();
    var done = 0; // bytes in completed chunks
    var complete = false;
    try {
      for (var n = 1; n <= chunks; n++) {
        cancel?.throwIfCancelled();
        final fields = uploadChunkFields(
          destDir: destDir,
          relativePath: relativePath,
          totalSize: total,
          chunkNumber: n,
          chunkSize: chunkSize,
        );
        final length = int.parse(fields['currentChunkSize']!);
        if (await _hasChunk(fields, cancel)) {
          done += length;
          onProgress?.call(done, total);
          continue;
        }
        await raf.setPosition((n - 1) * chunkSize);
        final bytes = await raf.read(length);
        if (bytes.length != length) {
          throw UploadException('The file changed while it was being uploaded. Try again.');
        }
        if (await _sendChunk(fields, bytes, cancel, (sent) => onProgress?.call(done + sent, total))) complete = true;
        done += length;
        onProgress?.call(done, total);
      }
    } finally {
      await raf.close();
    }
    // The server finalizes (and forgets) an upload on the chunk that
    // completes it, so one of this run's chunks must have said so.
    if (!complete) {
      throw UploadException("The server didn't confirm the upload. Try again.");
    }
  }

  Future<bool> _hasChunk(Map<String, String> fields, TransferCancel? cancel) async {
    for (var attempt = 0; attempt < 2; attempt++) {
      final client = http.Client();
      final detach = cancel?.onCancel(client.close);
      try {
        final res = await client.get(_endpoint(fields), headers: await _headers());
        if (res.statusCode == 401 && attempt == 0 && await _refresh()) continue;
        return res.statusCode == 200;
      } catch (_) {
        cancel?.throwIfCancelled();
        // The check is only an optimisation; send the chunk instead.
        return false;
      } finally {
        detach?.call();
        client.close();
      }
    }
    return false;
  }

  /// Sends one chunk, retrying transient failures. Returns whether it
  /// completed the file.
  Future<bool> _sendChunk(
    Map<String, String> fields,
    List<int> bytes,
    TransferCancel? cancel,
    void Function(int sent) onSent,
  ) async {
    var refreshed = false;
    Object? lastError;
    for (var attempt = 0; attempt <= maxRetries; attempt++) {
      cancel?.throwIfCancelled();
      if (attempt > 0) await Future<void>.delayed(retryDelay * attempt);
      cancel?.throwIfCancelled();
      final client = http.Client();
      final detach = cancel?.onCancel(client.close);
      try {
        final res = await http.Response.fromStream(await client.send(await _multipart(fields, bytes, onSent)));
        if (res.statusCode == 401 && !refreshed) {
          refreshed = true;
          if (await _refresh()) {
            attempt--; // a fresh token isn't a retry
            continue;
          }
          throw UploadException('Your session ended. Sign in again, then retry the upload.', statusCode: 401);
        }
        if (res.statusCode == 200) {
          final body = _decode(res.body);
          return body['complete'] == true;
        }
        final message = _decode(res.body)['message']?.toString();
        if (permanentStatuses.contains(res.statusCode) || res.statusCode == 401) {
          throw UploadException(message ?? 'The server refused the upload (HTTP ${res.statusCode}).', statusCode: res.statusCode);
        }
        lastError = UploadException(message ?? 'The server had a problem (HTTP ${res.statusCode}).', statusCode: res.statusCode);
      } on UploadException {
        rethrow;
      } catch (e) {
        cancel?.throwIfCancelled();
        lastError = e;
      } finally {
        detach?.call();
        client.close();
      }
    }
    if (lastError is UploadException) throw lastError;
    throw UploadException("Couldn't reach the server. Check the connection and try again.");
  }

  Future<Map<String, String>> _headers() async {
    final auth = await _authHeader();
    return {if (auth.isNotEmpty) 'Authorization': auth};
  }

  /// A multipart POST whose body is streamed in 64 KiB pieces, so progress
  /// moves while a chunk is on its way rather than jumping 8 MiB at a time.
  Future<http.BaseRequest> _multipart(Map<String, String> fields, List<int> bytes, void Function(int) onSent) async {
    final uri = _endpoint(const {});
    final form = http.MultipartRequest('POST', uri)
      ..fields.addAll(fields)
      ..files.add(http.MultipartFile('file', _pieces(bytes, onSent), bytes.length, filename: fields['filename']));
    form.headers.addAll(await _headers());
    return form;
  }

  static Stream<List<int>> _pieces(List<int> bytes, void Function(int) onSent) async* {
    const piece = 64 * 1024;
    for (var i = 0; i < bytes.length; i += piece) {
      final end = math.min(i + piece, bytes.length);
      yield bytes.sublist(i, end);
      onSent(end);
    }
  }

  static Map<String, dynamic> _decode(String body) {
    try {
      final v = jsonDecode(body);
      return v is Map<String, dynamic> ? v : const {};
    } catch (_) {
      return const {};
    }
  }
}
