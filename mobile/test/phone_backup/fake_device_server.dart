// An in-memory NivaroOS backup service speaking the device API
// (docs/specs/2026-09-30-phone-backup-device-api.md), for the engine and
// tus tests: sessions, check, check-items, deleted, finish, tus uploads
// with confirmed offsets, token rotation, a missing drive and revocation.
import 'dart:convert';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

class FakeUpload {
  FakeUpload(this.id, this.length, this.meta);
  final String id;
  final int length;
  final Map<String, String> meta;
  final BytesBuilder data = BytesBuilder();
  bool done = false;
  int get offset => data.length;
}

const tokenA = 'nvd_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
const tokenB = 'nvd_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb';

class FakeDeviceServer {
  FakeDeviceServer({this.deviceId = 'dev_1', this.token = tokenA});

  final String deviceId;
  String token;

  /// Tokens that still work (the old one during a rotation).
  late final Set<String> valid = {token};

  // Knobs.
  String destError = '';
  bool revoked = false;

  /// Answer the next N PATCHes with a dropped connection after storing
  /// half the chunk (as a cut-short body does).
  int cutPatches = 0;

  /// Rotate the token at the next request.
  String? rotateTo;

  /// Fail the next PATCH with 409 offset_mismatch.
  bool mismatchNext = false;

  // What the server holds.
  final Map<String, Map<String, ({int size, int mtime, String sha})>> files = {};
  final Map<String, Set<String>> itemKeys = {};
  final Map<String, FakeUpload> uploads = {};
  final List<Map<String, Object?>> exports = [];
  final Map<String, Set<String>> deletedMarks = {};
  final List<Map<String, Object?>> finished = [];
  final List<String> log = [];
  String? openSession;
  List<String> openCategories = const [];
  int _n = 0;

  MockClient get client => MockClient(handle);

  static String sha(List<int> b) => sha256.convert(b).toString();

  http.Response _env(Object? data, {int status = 200, Map<String, String> headers = const {}}) =>
      http.Response(jsonEncode({'success': status, 'message': 'ok', 'data': data}), status, headers: {'content-type': 'application/json', ...headers});

  http.Response _err(int status, String code, {Map<String, String> headers = const {}}) =>
      http.Response(jsonEncode({'success': status, 'message': code, 'data': {'error_code': code}}), status, headers: {'content-type': 'application/json', ...headers});

  Map<String, String> _metaOf(String header) {
    final out = <String, String>{};
    for (final pair in header.split(',')) {
      final p = pair.trim().split(' ');
      if (p.length == 2) out[p[0]] = utf8.decode(base64.decode(p[1]));
    }
    return out;
  }

  Future<http.Response> handle(http.Request req) async {
    final path = req.url.path;
    log.add('${req.method} $path');
    final auth = req.headers['Authorization'] ?? req.headers['authorization'] ?? '';
    final tok = auth.startsWith('Bearer ') ? auth.substring(7) : '';
    if (revoked || !valid.contains(tok)) return _err(401, 'unauthorized');
    final prefix = '/v1/backup/devices/$deviceId';
    if (!path.startsWith(prefix)) return _err(401, 'unauthorized');
    final rest = path.substring(prefix.length);
    final extra = <String, String>{};
    if (rotateTo != null) {
      valid.add(rotateTo!);
      token = rotateTo!;
      rotateTo = null;
    }
    if (tok != token) extra['X-NivaroOS-Device-Token'] = token;
    final res = await _route(req, rest);
    return http.Response.bytes(res.bodyBytes, res.statusCode, headers: {...res.headers, ...extra});
  }

  Map<String, dynamic> _body(http.Request r) => r.body.isEmpty ? {} : jsonDecode(r.body) as Map<String, dynamic>;

  Future<http.Response> _route(http.Request req, String rest) async {
    final m = req.method;
    if (m == 'GET' && rest == '/config') {
      return _env({
        'device': {'id': deviceId, 'name': 'Pixel 8'},
        'destination': {'path': '/DATA/Backup/Pixel 8', 'online': destError.isEmpty, if (destError.isNotEmpty) 'error_code': destError},
        'settings': {'keep_last': 10, 'keep_days': 30},
        'limits': {'chunk_size': 16, 'check_batch': 3, 'deleted_batch': 2, 'item_keys_batch': 2, 'max_finish_errors': 200},
        'categories': [
          for (final c in ['media', 'files', 'contacts', 'calendar', 'sms', 'calllog', 'apps', 'apks', 'settings']) {'category': c},
        ],
      });
    }
    if (m == 'PUT' && rest == '/phone-settings') return _env(_body(req));
    if (m == 'POST' && rest == '/sessions') {
      if (destError.isNotEmpty) return _err(409, destError);
      final cats = [for (final c in _body(req)['categories'] as List) c.toString()]..sort();
      if (openSession != null && openCategories.join(',') == cats.join(',')) {
        return _env({'id': openSession, 'categories': cats, 'status': 'open', 'resumed': true});
      }
      openSession = 'ses_${++_n}';
      openCategories = cats;
      return _env({'id': openSession, 'categories': cats, 'status': 'open', 'resumed': false}, status: 201);
    }
    final ses = RegExp(r'^/sessions/([^/]+)/(check|check-items|deleted|finish)$').firstMatch(rest);
    if (ses != null) {
      if (ses[1] != openSession) return _err(409, 'session_closed');
      final b = _body(req);
      final cat = b['category']?.toString() ?? '';
      switch (ses[2]) {
        case 'check':
          final have = files[cat] ?? {};
          return _env({
            'items': [
              for (final i in b['items'] as List)
                () {
                  final p = i['path'] as String;
                  final h = have[p];
                  final s = i['sha256'] as String;
                  if (h != null && h.size == i['size'] && h.mtime == i['mtime'] && (s.isEmpty || s == h.sha)) return {'path': p, 'status': 'have'};
                  if (s.isEmpty) return {'path': p, 'status': 'need_hash'};
                  for (final u in uploads.values) {
                    if (!u.done && u.meta['path'] == p && u.meta['sha256'] == s) return {'path': p, 'status': 'need', 'upload_id': u.id, 'offset': u.offset};
                  }
                  return {'path': p, 'status': 'need'};
                }(),
            ],
          });
        case 'check-items':
          final known = itemKeys[cat] ?? {};
          return _env({'new': [for (final k in b['keys'] as List) if (!known.contains(k)) k]});
        case 'deleted':
          final paths = [for (final p in b['paths'] as List) p.toString()];
          (deletedMarks[cat] ??= {}).addAll(paths);
          return _env({'marked': paths.length, 'unknown': 0});
        case 'finish':
          finished.add(b);
          final st = b['status'];
          openSession = null;
          return _env({
            'session': {'id': ses[1], 'status': 'finished'},
            if (st == 'success' || st == 'partial') 'snapshot': {'id': 'snap_$_n', 'status': st},
            'pending_uploads': uploads.values.where((u) => !u.done).length,
          });
      }
    }
    if (m == 'POST' && rest == '/uploads') {
      final length = int.parse(req.headers['Upload-Length'] ?? req.headers['upload-length']!);
      final meta = _metaOf(req.headers['Upload-Metadata'] ?? req.headers['upload-metadata']!);
      if (meta['session'] != openSession) return _err(409, 'session_closed');
      if (destError.isNotEmpty) return _err(409, destError);
      final cat = meta['category']!;
      final path = meta['path'] ?? '';
      if (path.isNotEmpty && files[cat]?[path]?.sha == meta['sha256']) return _env({'status': 'have', 'offset': 0, 'length': length});
      for (final u in uploads.values) {
        if (!u.done && u.length == length && u.meta['sha256'] == meta['sha256'] && u.meta['category'] == cat && (u.meta['path'] ?? '') == path) {
          return _env({'upload_id': u.id, 'status': 'resumed', 'offset': u.offset, 'length': length}, headers: {'Upload-Offset': '${u.offset}'});
        }
      }
      if (length == 0) {
        _place(FakeUpload('', 0, meta));
        return _env({'status': 'stored', 'offset': 0, 'length': 0}, status: 201);
      }
      final id = 'up_${++_n}';
      uploads[id] = FakeUpload(id, length, meta);
      return _env({'upload_id': id, 'status': 'created', 'offset': 0, 'length': length}, status: 201, headers: {'Location': '/x/$id', 'Upload-Offset': '0'});
    }
    final up = RegExp(r'^/uploads/([^/]+)$').firstMatch(rest);
    if (up != null) {
      final u = uploads[up[1]];
      if (u == null || u.done) return http.Response('', 404);
      if (m == 'HEAD') return http.Response('', 200, headers: {'Upload-Offset': '${u.offset}', 'Upload-Length': '${u.length}'});
      if (m == 'DELETE') {
        uploads.remove(u.id);
        return http.Response('', 204);
      }
      if (m == 'PATCH') {
        if (destError.isNotEmpty) return _err(409, destError);
        final at = int.parse(req.headers['Upload-Offset'] ?? req.headers['upload-offset']!);
        if (mismatchNext || at != u.offset) {
          mismatchNext = false;
          return _err(409, 'offset_mismatch', headers: {'Upload-Offset': '${u.offset}'});
        }
        final body = req.bodyBytes;
        if (body.length > 16 || u.offset + body.length > u.length) return _err(413, 'too_large');
        if (cutPatches > 0) {
          cutPatches--;
          u.data.add(body.sublist(0, body.length ~/ 2));
          throw http.ClientException('Connection reset');
        }
        u.data.add(body);
        final headers = {'Upload-Offset': '${u.offset}'};
        if (u.offset == u.length) {
          final bytes = u.data.toBytes();
          if (sha(bytes) != u.meta['sha256']) {
            uploads.remove(u.id);
            return _err(422, 'checksum_mismatch');
          }
          u.done = true;
          headers['X-NivaroOS-Upload-Result'] = _place(u, bytes);
        }
        return http.Response('', 204, headers: headers);
      }
    }
    return _err(404, 'not_found');
  }

  final Map<String, Uint8List> stored = {};

  String _place(FakeUpload u, [Uint8List? bytes]) {
    final cat = u.meta['category']!;
    final path = u.meta['path'] ?? '';
    final b = bytes ?? Uint8List(0);
    if (path.isNotEmpty) {
      (files[cat] ??= {})[path] = (size: u.length, mtime: int.tryParse(u.meta['mtime'] ?? '') ?? 0, sha: u.meta['sha256']!);
      stored['$cat/$path'] = b;
      return 'stored';
    }
    if (cat == 'sms' || cat == 'calllog') {
      final keys = serverKeys(utf8.decode(b));
      final known = itemKeys[cat] ??= {};
      if (keys.every(known.contains)) return 'no_new_items';
      known.addAll(keys);
      exports.add({'category': cat, 'size': b.length, 'items': keys.length});
      stored['$cat/${exports.length}'] = b;
      return 'stored';
    }
    final same = exports.where((e) => e['category'] == cat && e['name'] == (u.meta['name'] ?? '')).lastOrNull;
    if (same != null && same['sha'] == u.meta['sha256']) return 'unchanged';
    exports.add({'category': cat, 'name': u.meta['name'] ?? '', 'sha': u.meta['sha256'], 'size': b.length});
    stored['$cat/${u.meta['name'] ?? ''}/${exports.length}'] = b;
    return 'stored';
  }

  /// The item keys of an SMS Backup & Restore file, computed the way the
  /// Go service does (jobs.itemKey): from the unescaped attributes of each
  /// top-level sms, mms or call element.
  static List<String> serverKeys(String xml) {
    String un(String v) => v
        .replaceAll('&lt;', '<')
        .replaceAll('&gt;', '>')
        .replaceAll('&quot;', '"')
        .replaceAll('&apos;', "'")
        .replaceAll('&#10;', '\n')
        .replaceAll('&#13;', '\r')
        .replaceAll('&#9;', '\t')
        .replaceAll('&amp;', '&');
    final out = <String>[];
    for (final el in RegExp(r'^  <(sms|mms|call) ([^>]*?)/?>', multiLine: true).allMatches(xml)) {
      final a = {for (final m in RegExp(r'(\w+)="([^"]*)"').allMatches(el[2]!)) m[1]!: un(m[2]!)};
      final s = switch (el[1]) {
        'sms' => 'sms|${a['address']}|${a['date']}|${a['type']}|${a['body']}',
        'mms' => 'mms|${a['address']}|${a['date']}|${a['msg_box']}|${a['m_id']}',
        _ => 'call|${a['number']}|${a['date']}|${a['duration']}',
      };
      out.add(sha(utf8.encode(s)));
    }
    return out;
  }
}
