// Resumable uploads (tus 1.0 subset, device API §5). The server always
// keeps exactly the bytes it confirmed, so after a lost answer, a network
// change, the app being killed or the phone rebooting, the next attempt
// asks where to continue (HEAD, or the create/check answer's offset) and
// sends only the rest.
import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'pb_client.dart';

/// Bytes to send: a file, or a content:// document read through the
/// platform.
abstract class ByteSource {
  int get length;
  Future<Uint8List> read(int offset, int length);
}

class FileByteSource implements ByteSource {
  FileByteSource(this.file) : length = file.lengthSync();
  final File file;
  @override
  final int length;

  @override
  Future<Uint8List> read(int offset, int len) async {
    final raf = await file.open();
    try {
      await raf.setPosition(offset);
      return await raf.read(len);
    } finally {
      await raf.close();
    }
  }
}

class MemoryByteSource implements ByteSource {
  MemoryByteSource(this.bytes);
  final Uint8List bytes;
  @override
  int get length => bytes.length;
  @override
  Future<Uint8List> read(int offset, int len) async => Uint8List.sublistView(bytes, offset, (offset + len).clamp(0, bytes.length));
}

/// How an upload ended.
class UploadOutcome {
  const UploadOutcome(this.result, {this.bytesSent = 0});

  /// stored | unchanged | no_new_items (from the last PATCH), or have |
  /// excluded (create said nothing is to be sent).
  final String result;
  final int bytesSent;

  bool get sent => result == 'stored' || result == 'unchanged' || result == 'no_new_items';
}

/// The bytes on the phone don't match the SHA-256 the upload was created
/// with (the file changed while it was read): hash it again.
class ChecksumMismatch implements Exception {
  const ChecksumMismatch();
}

/// The run was stopped (budget over, cancelled): the upload stays
/// resumable on the server.
class UploadStopped implements Exception {
  const UploadStopped();
}

class TusUploader {
  TusUploader(this.client, {this.chunkSize = 8 << 20, this.maxRetries = 5, this.retryDelay = const Duration(seconds: 2), Future<void> Function(Duration)? sleep})
      : _sleep = sleep ?? Future<void>.delayed;

  final DeviceClient client;

  /// The largest PATCH body (config limits.chunk_size).
  final int chunkSize;
  final int maxRetries;
  final Duration retryDelay;
  final Future<void> Function(Duration) _sleep;

  /// Sends [source]. [uploadId]/[offset] come from a check answer when an
  /// unfinished upload of exactly this file exists. [shouldStop] is asked
  /// before every chunk; [onProgress] gets the confirmed offset.
  Future<UploadOutcome> upload(
    ByteSource source,
    UploadMeta meta, {
    String uploadId = '',
    int? offset,
    bool Function()? shouldStop,
    void Function(int confirmed, int total)? onProgress,
  }) =>
      _upload(source, meta, uploadId: uploadId, offset: offset, shouldStop: shouldStop, onProgress: onProgress, restarts: 0);

  Future<UploadOutcome> _upload(
    ByteSource source,
    UploadMeta meta, {
    String uploadId = '',
    int? offset,
    bool Function()? shouldStop,
    void Function(int confirmed, int total)? onProgress,
    required int restarts,
  }) async {
    Future<UploadOutcome> restart() {
      // Gone on the server (expired, or void after a location change):
      // create it again, a few times at most.
      if (restarts >= 3) throw PhoneBackupError('internal', message: 'The upload kept disappearing on the server');
      return _upload(source, meta, shouldStop: shouldStop, onProgress: onProgress, restarts: restarts + 1);
    }

    final length = source.length;
    var uid = uploadId;
    int? at = offset;
    if (uid.isNotEmpty && at == null) at = await _retrying(() => client.headUpload(uid));
    if (uid.isEmpty || at == null) {
      final c = await _retrying(() => client.createUpload(length, meta));
      switch (c.status) {
        case 'have':
        case 'excluded':
          return UploadOutcome(c.status);
        case 'stored':
          return const UploadOutcome('stored');
      }
      uid = c.uploadId;
      at = c.offset;
    }
    var pos = at;
    final start = pos;
    onProgress?.call(pos, length);
    var failures = 0;
    var result = '';
    while (pos < length) {
      if (shouldStop?.call() ?? false) throw const UploadStopped();
      final n = (length - pos) < chunkSize ? length - pos : chunkSize;
      final chunk = await source.read(pos, n);
      try {
        final r = await client.patchUpload(uid, pos, chunk);
        pos = r.offset;
        result = r.result;
        failures = 0;
        onProgress?.call(pos, length);
      } on PhoneBackupError catch (e) {
        switch (e.code) {
          case 'offset_mismatch':
          case 'io_error':
            // The server says where it is; continue from there.
            final o = e.offset ?? await client.headUpload(uid);
            if (o == null) return restart();
            pos = o;
            continue;
          case 'checksum_mismatch':
            throw const ChecksumMismatch();
          case 'not_found':
            // Expired, or void after a location change: start over.
            return restart();
          case 'invalid_state':
            // Another PATCH of this upload is still running (a lost answer).
            await _sleep(retryDelay);
            final o = await client.headUpload(uid);
            if (o == null) return restart();
            pos = o;
            continue;
        }
        if (e.unreachable && failures < maxRetries) {
          failures++;
          await _sleep(retryDelay * failures);
          final o = await _retrying(() => client.headUpload(uid));
          if (o == null) return restart();
          pos = o;
          continue;
        }
        rethrow;
      }
    }
    return UploadOutcome(result.isEmpty ? 'stored' : result, bytesSent: pos - start);
  }

  Future<T> _retrying<T>(Future<T> Function() call) async {
    for (var i = 0;; i++) {
      try {
        return await call();
      } on PhoneBackupError catch (e) {
        if (!e.unreachable || i >= maxRetries) rethrow;
        await _sleep(retryDelay * (i + 1));
      }
    }
  }
}
