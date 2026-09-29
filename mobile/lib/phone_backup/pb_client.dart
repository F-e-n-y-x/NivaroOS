// The phone's own backup client: every /v1/backup/devices/<id>/* call made
// with the device token (docs/specs/2026-09-30-phone-backup-device-api.md
// §1), never the owner's session. Token rotation (§1): any answer may
// carry X-NivaroOS-Device-Token; it is stored at once through
// [DeviceClient.onRotated]. A 401 means revoked or replaced: callers stop
// and ask to link the phone again, they never retry in a loop.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:http/http.dart' as http;

import 'pb_models.dart';

/// The phone's credential for one server: shown once at enrolment, kept in
/// Keystore-backed secure storage.
class DeviceCredential {
  const DeviceCredential({required this.server, required this.deviceId, required this.token, this.name = '', this.enrolledAt});

  /// The server's base URL ("http://nas.local").
  final String server;
  final String deviceId;

  /// `nvd_` + 43 base64url characters.
  final String token;
  final String name;
  final DateTime? enrolledAt;

  DeviceCredential withToken(String t) => DeviceCredential(server: server, deviceId: deviceId, token: t, name: name, enrolledAt: enrolledAt);

  Map<String, Object?> toJson() => {
        'server': server,
        'device_id': deviceId,
        'token': token,
        'name': name,
        if (enrolledAt != null) 'enrolled_at': enrolledAt!.toUtc().toIso8601String(),
      };

  static DeviceCredential? fromJson(Object? j) {
    if (j is! Map) return null;
    final server = j['server']?.toString() ?? '';
    final id = j['device_id']?.toString() ?? '';
    final token = j['token']?.toString() ?? '';
    if (server.isEmpty || id.isEmpty || token.isEmpty) return null;
    return DeviceCredential(
      server: server,
      deviceId: id,
      token: token,
      name: j['name']?.toString() ?? '',
      enrolledAt: DateTime.tryParse(j['enrolled_at']?.toString() ?? ''),
    );
  }

  static bool looksLikeToken(String t) => RegExp(r'^nvd_[A-Za-z0-9_-]{43}$').hasMatch(t);
}

/// A failed device call.
class PhoneBackupError implements Exception {
  PhoneBackupError(this.code, {this.status, this.offset, this.message = '', this.retryAfter});

  /// The service's error_code (dest_offline, session_closed, offset_mismatch,
  /// checksum_mismatch, rate_limited...), 'network' when the server wasn't
  /// reached, 'unauthorized' for a 401.
  final String code;
  final int? status;

  /// Upload-Offset of a tus error answer (offset_mismatch, io_error).
  final int? offset;
  final String message;
  final Duration? retryAfter;

  bool get revoked => status == 401 || code == 'unauthorized';
  bool get unreachable => code == 'network' || code == 'timeout';
  bool get driveMissing => code == 'dest_offline';
  bool get sessionClosed => code == 'session_closed';
  bool get rateLimited => status == 429 || code == 'rate_limited';

  @override
  String toString() => message.isNotEmpty ? '$code: $message' : code;
}

/// The answer to one PATCH.
class PatchResult {
  const PatchResult(this.offset, this.result);
  final int offset;

  /// X-NivaroOS-Upload-Result on the last chunk: stored | unchanged |
  /// no_new_items; '' before that.
  final String result;
}

/// Metadata of a new upload (tus Upload-Metadata, §5).
class UploadMeta {
  const UploadMeta({required this.session, required this.category, required this.sha256, this.path = '', this.name = '', this.mtime, this.takenAt, this.mediaId});

  final String session;
  final String category;
  final String sha256;
  final String path;
  final String name;
  final int? mtime;
  final int? takenAt;
  final int? mediaId;

  String encode() {
    String b(String s) => base64.encode(utf8.encode(s));
    return [
      'session ${b(session)}',
      'category ${b(category)}',
      if (path.isNotEmpty) 'path ${b(path)}',
      if (name.isNotEmpty) 'name ${b(name)}',
      'sha256 ${b(sha256)}',
      if (mtime != null) 'mtime ${b('$mtime')}',
      if (takenAt != null && takenAt! > 0) 'taken_at ${b('$takenAt')}',
      if (mediaId != null && mediaId! > 0) 'media_id ${b('$mediaId')}',
    ].join(',');
  }
}

/// A response header, whatever its case (a real server sends lower case,
/// a test double may not).
String? _h(http.BaseResponse res, String name) {
  final v = res.headers[name];
  if (v != null) return v;
  for (final e in res.headers.entries) {
    if (e.key.toLowerCase() == name) return e.value;
  }
  return null;
}

class DeviceClient {
  // ignore: prefer_initializing_formals
  DeviceClient(this._cred, {http.Client? client, this.onRotated, this.timeout = const Duration(seconds: 60)}) : _client = client;

  DeviceCredential _cred;
  final http.Client? _client;

  /// Called with the new credential when the server rotates the token.
  final FutureOr<void> Function(DeviceCredential next)? onRotated;
  final Duration timeout;

  DeviceCredential get credential => _cred;
  String get deviceId => _cred.deviceId;

  // Made on first use, so http.runWithClient (tests, screenshots) supplies
  // the fake one.
  http.Client? _owned;
  http.Client get _c => _client ?? (_owned ??= http.Client());

  /// Closes the client this made itself (not one passed in).
  void close() {
    _owned?.close();
    _owned = null;
  }

  String get _base {
    var b = _cred.server.trim();
    while (b.endsWith('/')) {
      b = b.substring(0, b.length - 1);
    }
    if (!b.startsWith('http://') && !b.startsWith('https://')) b = 'http://$b';
    return b;
  }

  /// A route under `/v1/backup/devices/<id>`.
  Uri deviceUri(String path, [Map<String, String>? query]) {
    final u = Uri.parse('$_base/v1/backup/devices/${Uri.encodeComponent(_cred.deviceId)}$path');
    return query == null || query.isEmpty ? u : u.replace(queryParameters: query);
  }

  /// A GET path under /v1/backup (a manifest's `content`).
  Uri backupUri(String path) => Uri.parse('$_base/v1/backup${path.startsWith('/') ? '' : '/'}$path');

  Map<String, String> get _auth => {'Authorization': 'Bearer ${_cred.token}'};

  Future<void> _rotation(http.BaseResponse res) async {
    final next = _h(res, 'x-nivaroos-device-token');
    if (next == null || next.isEmpty || next == _cred.token || !DeviceCredential.looksLikeToken(next)) return;
    _cred = _cred.withToken(next);
    await onRotated?.call(_cred);
  }

  Future<http.Response> _send(http.BaseRequest req) async {
    req.headers.addAll(_auth);
    http.StreamedResponse streamed;
    try {
      streamed = await _c.send(req).timeout(timeout);
    } on TimeoutException {
      throw PhoneBackupError('timeout', message: 'The server did not answer in time');
    } on SocketException catch (e) {
      throw PhoneBackupError('network', message: e.message);
    } on http.ClientException catch (e) {
      throw PhoneBackupError('network', message: e.message);
    } on HandshakeException catch (e) {
      throw PhoneBackupError('network', message: e.message);
    }
    final res = await http.Response.fromStream(streamed).timeout(timeout, onTimeout: () => throw PhoneBackupError('timeout'));
    await _rotation(res);
    return res;
  }

  static PhoneBackupError errorOf(http.Response res) {
    var code = '';
    var message = '';
    try {
      final j = jsonDecode(res.body);
      if (j is Map) {
        final data = j['data'];
        if (data is Map) code = data['error_code']?.toString() ?? '';
        message = j['message']?.toString() ?? '';
        if (code.isEmpty && RegExp(r'^[a-z_]+$').hasMatch(message)) code = message;
        if (data is Map && (data['detail']?.toString() ?? '').isNotEmpty) message = data['detail'].toString();
      }
    } catch (_) {}
    if (code.isEmpty) {
      code = switch (res.statusCode) {
        401 => 'unauthorized',
        404 => 'not_found',
        413 => 'too_large',
        422 => 'checksum_mismatch',
        429 => 'rate_limited',
        507 => 'no_space',
        _ => 'internal',
      };
    }
    final ra = int.tryParse(_h(res, 'retry-after') ?? '');
    return PhoneBackupError(code,
        status: res.statusCode, offset: int.tryParse(_h(res, 'upload-offset') ?? ''), message: message, retryAfter: ra == null ? null : Duration(seconds: ra));
  }

  Future<Object?> _json(String method, String path, {Object? body, Map<String, String>? query}) async {
    final req = http.Request(method, deviceUri(path, query));
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(body);
    }
    final res = await _send(req);
    if (res.statusCode >= 400) throw errorOf(res);
    if (res.body.isEmpty) return null;
    try {
      final j = jsonDecode(res.body);
      return j is Map ? j['data'] : j;
    } catch (_) {
      throw PhoneBackupError('internal', status: res.statusCode, message: 'Not a NivaroOS answer');
    }
  }

  // --- Setup and sessions -------------------------------------------------

  Future<PhoneDevice> ping() async => PhoneDevice.fromJson((await _json('GET', '/ping') as Map?)?['device']);

  Future<PhoneConfig> config() async => PhoneConfig.fromJson(await _json('GET', '/config'));

  Future<void> putPhoneSettings(Map<String, Object?> settings) => _json('PUT', '/phone-settings', body: settings);

  Future<PhoneSession> startSession(List<String> categories, {String reason = 'schedule', int expectBytes = 0}) async =>
      PhoneSession.fromJson(await _json('POST', '/sessions', body: {'categories': categories, 'reason': reason, 'expect_bytes': expectBytes}));

  Future<List<CheckAnswer>> check(String sid, String category, List<CheckItem> items) async {
    final d = await _json('POST', '/sessions/${Uri.encodeComponent(sid)}/check', body: {'category': category, 'items': [for (final i in items) i.toJson()]});
    final list = d is Map && d['items'] is List ? d['items'] as List : const [];
    return [for (final i in list) CheckAnswer.fromJson(i)];
  }

  /// The keys of [keys] the backup lacks.
  Future<Set<String>> checkItems(String sid, String category, List<String> keys) async {
    final d = await _json('POST', '/sessions/${Uri.encodeComponent(sid)}/check-items', body: {'category': category, 'keys': keys});
    final list = d is Map && d['new'] is List ? d['new'] as List : const [];
    return {for (final k in list) k.toString()};
  }

  Future<int> deleted(String sid, String category, List<String> paths) async {
    final d = await _json('POST', '/sessions/${Uri.encodeComponent(sid)}/deleted', body: {'category': category, 'paths': paths});
    return d is Map && d['marked'] is num ? (d['marked'] as num).toInt() : 0;
  }

  Future<PhoneFinishResult> finish(String sid, String status, List<Map<String, String>> errors) async =>
      PhoneFinishResult.fromJson(await _json('POST', '/sessions/${Uri.encodeComponent(sid)}/finish', body: {'status': status, 'errors': errors}));

  // --- tus uploads --------------------------------------------------------

  Future<UploadCreated> createUpload(int length, UploadMeta meta) async {
    final req = http.Request('POST', deviceUri('/uploads'))
      ..headers.addAll({'Tus-Resumable': '1.0.0', 'Upload-Length': '$length', 'Upload-Metadata': meta.encode()});
    final res = await _send(req);
    if (res.statusCode >= 400) throw errorOf(res);
    Object? data;
    try {
      final j = jsonDecode(res.body);
      data = j is Map ? j['data'] : null;
    } catch (_) {}
    final c = UploadCreated.fromJson(data);
    final headerOffset = int.tryParse(_h(res, 'upload-offset') ?? '');
    return UploadCreated(uploadId: c.uploadId, offset: headerOffset ?? c.offset, length: c.length == 0 ? length : c.length, status: c.status.isEmpty ? 'created' : c.status);
  }

  /// The confirmed offset, or null when the upload is gone (unknown,
  /// finished, expired, void after a location change).
  Future<int?> headUpload(String uid) async {
    final res = await _send(http.Request('HEAD', deviceUri('/uploads/${Uri.encodeComponent(uid)}'))..headers['Tus-Resumable'] = '1.0.0');
    if (res.statusCode == 404 || res.statusCode == 410) return null;
    if (res.statusCode >= 400) throw errorOf(res);
    return int.tryParse(_h(res, 'upload-offset') ?? '');
  }

  Future<PatchResult> patchUpload(String uid, int offset, Uint8List chunk) async {
    final req = http.Request('PATCH', deviceUri('/uploads/${Uri.encodeComponent(uid)}'))
      ..headers.addAll({'Tus-Resumable': '1.0.0', 'Content-Type': 'application/offset+octet-stream', 'Upload-Offset': '$offset'})
      ..bodyBytes = chunk;
    final res = await _send(req);
    if (res.statusCode >= 400) throw errorOf(res);
    final next = int.tryParse(_h(res, 'upload-offset') ?? '') ?? offset + chunk.length;
    return PatchResult(next, _h(res, 'x-nivaroos-upload-result') ?? '');
  }

  Future<void> deleteUpload(String uid) async {
    final res = await _send(http.Request('DELETE', deviceUri('/uploads/${Uri.encodeComponent(uid)}'))..headers['Tus-Resumable'] = '1.0.0');
    if (res.statusCode >= 400 && res.statusCode != 404) throw errorOf(res);
  }

  // --- Restore and browse -------------------------------------------------

  Future<List<PhoneSnapshot>> snapshots() async {
    final d = await _json('GET', '/snapshots');
    return [for (final s in d is List ? d : const []) PhoneSnapshot.fromJson(s)];
  }

  Future<PhoneBrowseResult> browse(String snapshot, String category, String path, {bool includeDeleted = true}) async => PhoneBrowseResult.fromJson(await _json(
        'GET',
        '/snapshots/${Uri.encodeComponent(snapshot)}/browse',
        query: {'category': category, 'path': path, if (includeDeleted) 'include_deleted': '1'},
      ));

  Future<RestoreManifestPage> restoreManifest({String snapshot = 'latest', String category = '', String after = '', int limit = 1000}) async =>
      RestoreManifestPage.fromJson(await _json('GET', '/restore-manifest', query: {
        'snapshot': snapshot,
        if (category.isNotEmpty) 'category': category,
        if (after.isNotEmpty) 'after': after,
        'limit': '$limit',
      }));

  Future<List<PhoneExport>> exports({String category = '', String snapshot = 'latest'}) async {
    final d = await _json('GET', '/exports', query: {if (category.isNotEmpty) 'category': category, 'snapshot': snapshot});
    return [for (final e in d is List ? d : const []) PhoneExport.fromJson(e)];
  }

  Future<Set<String>> excluded() async {
    final d = await _json('GET', '/excluded');
    return {for (final e in d is List ? d : const []) if (e is Map) e['sha256']?.toString() ?? ''}..remove('');
  }

  /// Downloads [uri] into [dest], resuming a partial file with Range.
  /// Returns the server's X-NivaroOS-SHA256 ('' when not sent).
  Future<String> download(Uri uri, File dest, {void Function(int received, int total)? onProgress}) async {
    var have = dest.existsSync() ? dest.lengthSync() : 0;
    final req = http.Request('GET', uri);
    if (have > 0) req.headers['Range'] = 'bytes=$have-';
    req.headers.addAll(_auth);
    http.StreamedResponse res;
    try {
      res = await _c.send(req).timeout(timeout);
    } on SocketException catch (e) {
      throw PhoneBackupError('network', message: e.message);
    } on http.ClientException catch (e) {
      throw PhoneBackupError('network', message: e.message);
    } on TimeoutException {
      throw PhoneBackupError('timeout');
    }
    await _rotation(res);
    if (res.statusCode == 416) {
      // Already complete.
      await res.stream.drain<void>();
      return _h(res, 'x-nivaroos-sha256') ?? '';
    }
    if (res.statusCode >= 400) throw errorOf(await http.Response.fromStream(res));
    if (res.statusCode == 200) have = 0; // the server ignored Range: start over
    final total = have + (res.contentLength ?? 0);
    final sink = dest.openWrite(mode: have > 0 ? FileMode.append : FileMode.write);
    var got = have;
    try {
      await for (final chunk in res.stream) {
        sink.add(chunk);
        got += chunk.length;
        onProgress?.call(got, total);
      }
    } on SocketException catch (e) {
      throw PhoneBackupError('network', message: e.message);
    } on http.ClientException catch (e) {
      throw PhoneBackupError('network', message: e.message);
    } finally {
      await sink.close();
    }
    return _h(res, 'x-nivaroos-sha256') ?? '';
  }
}
