import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/screens/files/file_ops.dart';
import 'package:nivaroos_mobile/screens/files/resumable_upload.dart';
import 'package:nivaroos_mobile/services/share_intent.dart';
import 'package:nivaroos_mobile/services/share_upload.dart';

/// A server with the v2 upload protocol: keeps chunks per identifier,
/// says which it has, and completes a file when all are in. [failPost]
/// lets a test break the network for some chunks.
class FakeUploadServer {
  final chunks = <String, Map<int, List<int>>>{};
  final files = <String, List<int>>{};
  final posted = <String>[];
  bool Function(Map<String, String> fields)? failPost;
  String? refuse;

  static Map<String, String> fields(http.Request req) {
    final body = latin1.decode(req.bodyBytes);
    return {for (final m in RegExp(r'name="([^"]+)"\r\n\r\n([^\r]*)\r\n').allMatches(body)) m.group(1)!: m.group(2)!};
  }

  static List<int> fileBytes(http.Request req) {
    final body = req.bodyBytes;
    final s = latin1.decode(body);
    final start = s.indexOf('\r\n\r\n', s.indexOf('name="file"')) + 4;
    final end = s.lastIndexOf('\r\n--');
    return body.sublist(start, end);
  }

  MockClient get client => MockClient((req) async {
        if (req.method == 'GET') {
          final q = req.url.queryParameters;
          final has = chunks['${q['path']}/${q['relativePath']}']?.containsKey(int.parse(q['chunkNumber']!)) ?? false;
          return http.Response('', has ? 200 : 204);
        }
        final f = fields(req);
        if (failPost?.call(f) ?? false) throw http.ClientException('network is down');
        if (refuse != null) return http.Response(jsonEncode({'success': 500, 'message': refuse}), 500);
        final key = '${f['path']}/${f['relativePath']}';
        final n = int.parse(f['chunkNumber']!);
        posted.add('$key#$n');
        final got = chunks.putIfAbsent(key, () => {})..[n] = fileBytes(req);
        final complete = got.length == int.parse(f['totalChunks']!);
        if (complete) {
          files[key] = [for (var i = 1; i <= got.length; i++) ...got[i]!];
          chunks.remove(key);
        }
        return http.Response(jsonEncode({'success': 200, 'complete': complete}), 200);
      });
}

ResumableUploader _uploader() => ResumableUploader(
      chunkSize: 4,
      maxRetries: 1,
      retryDelay: Duration.zero,
      authHeader: () async => 'token',
      refreshToken: () async => true,
      endpoint: (q) => Uri.parse('http://nas.test/v2/casaos/file/upload').replace(queryParameters: q.isEmpty ? null : q),
    );

SharedFile _file(String name, int size, {String mime = 'image/jpeg'}) => SharedFile(uri: 'content://x/$name/$size', name: name, size: size, mime: mime);

List<int> _bytesOf(String uri) {
  final size = int.parse(uri.split('/').last);
  return List.generate(size, (i) => (i * 7 + uri.length) % 256);
}

ShareBatch _batch(List<SharedFile> files, {ConflictChoice conflict = ConflictChoice.keepBoth}) =>
    ShareBatch(id: 'b1', server: 'http://nas.test', serverName: 'atom', destDir: '/DATA/Gallery', conflict: conflict, items: [for (final f in files) ShareItem(f)]);

void main() {
  group('share payload', () {
    setUp(() {
      ShareIntent.pending.value = null;
      ShareIntent.pendingTorrent.value = null;
      ShareIntent.pendingFiles.value = null;
      ShareIntent.pendingOpen.value = null;
    });

    test('a single file', () {
      ShareIntent.apply({
        'files': [
          {'uri': 'content://media/external/images/media/12', 'name': 'IMG_0012.jpg', 'size': 2048, 'mime': 'image/jpeg'},
        ],
        'rejected': 0,
      });
      final f = ShareIntent.pendingFiles.value!;
      expect(f.files.single.name, 'IMG_0012.jpg');
      expect(f.files.single.isImage, isTrue);
      expect(f.totalBytes, 2048);
      expect(ShareIntent.pending.value, isNull);
    });

    test('several files', () {
      ShareIntent.apply({
        'files': [
          for (var i = 0; i < 40; i++) {'uri': 'content://p/$i', 'name': 'f$i.mp4', 'size': i * 10, 'mime': 'video/mp4'},
        ],
      });
      expect(ShareIntent.pendingFiles.value!.files, hasLength(40));
      expect(ShareIntent.pendingFiles.value!.files.every((f) => f.isVideo), isTrue);
    });

    test('mixed kinds, a bad entry and unreadable ones', () {
      ShareIntent.apply({
        'files': [
          {'uri': 'content://a/1', 'name': 'photo.heic', 'size': 10, 'mime': 'image/heic'},
          {'uri': 'content://a/2', 'name': 'report.pdf', 'size': 20, 'mime': 'application/pdf'},
          {'uri': 'content://a/3', 'name': '', 'size': 0, 'mime': ''},
          {'uri': '', 'name': 'broken', 'size': 1},
          'nonsense',
        ],
        'rejected': 2,
      });
      final f = ShareIntent.pendingFiles.value!;
      expect([for (final x in f.files) x.name], ['photo.heic', 'report.pdf', 'file']);
      expect(f.rejected, 4);
    });

    test('text still goes to Download on server; the notification opens a batch', () {
      ShareIntent.apply('https://example.com/a.iso');
      expect(ShareIntent.pending.value, 'https://example.com/a.iso');
      expect(ShareIntent.pendingFiles.value, isNull);
      ShareIntent.apply({'open': 'b1', 'folder': '/DATA/Gallery'});
      expect(ShareIntent.pendingOpen.value, (batch: 'b1', folder: '/DATA/Gallery'));
      ShareIntent.apply({'open': 'b2', 'folder': null});
      expect(ShareIntent.pendingOpen.value?.folder, isNull);
      expect(ShareIntent.pendingFiles.value, isNull);
    });
  });

  group('store', () {
    test('saves and loads a batch', () {
      final dir = Directory.systemTemp.createTempSync('share_store');
      addTearDown(() => dir.deleteSync(recursive: true));
      final store = ShareUploadStore(dir);
      final b = _batch([_file('a.jpg', 3), _file('b.pdf', 5, mime: 'application/pdf')], conflict: ConflictChoice.replace)
        ..items[0].target = 'a (2).jpg'
        ..items[0].state = ShareItemState.done
        ..items[1].state = ShareItemState.failed
        ..items[1].error = 'Not enough space on the server.'
        ..waits = 2;
      store.save(b);
      final back = store.load('b1')!;
      expect(back.conflict, ConflictChoice.replace);
      expect(back.waits, 2);
      expect(back.items[0].target, 'a (2).jpg');
      expect(back.items[1].state, ShareItemState.failed);
      expect(back.items[1].error, 'Not enough space on the server.');
      expect(store.load('nope'), isNull);
      back.retry();
      expect(back.items[1].state, ShareItemState.pending);
      expect(back.items[0].state, ShareItemState.done);
    });
  });

  group('runner', () {
    late FakeUploadServer server;
    late List<String> saves;

    setUp(() {
      server = FakeUploadServer();
      saves = [];
    });

    ShareUploader runner({Set<String> there = const {}, Future<List<int>> Function(String, int, int)? read}) => ShareUploader(
          uploader: _uploader(),
          read: read ?? (uri, offset, length) async => _bytesOf(uri).sublist(offset, offset + length),
          listNames: (_) async => there,
          save: (b) => saves.add(jsonEncode(b.toJson())),
        );

    test('uploads every file under its name, in chunks', () async {
      final b = _batch([_file('a.jpg', 10), _file('b.pdf', 3, mime: 'application/pdf'), _file('empty.txt', 0, mime: 'text/plain')]);
      final end = await http.runWithClient(() => runner().run(b, TransferCancel()), () => server.client);
      expect(end, ShareRunEnd.finished);
      expect(b.items.every((i) => i.state == ShareItemState.done), isTrue);
      expect(server.files['/DATA/Gallery/a.jpg'], _bytesOf(b.items[0].file.uri));
      expect(server.files['/DATA/Gallery/b.pdf'], _bytesOf(b.items[1].file.uri));
      expect(server.files['/DATA/Gallery/empty.txt'], isEmpty);
      expect(saves, isNotEmpty);
      final n = shareFinalNotice(b, end);
      expect(n.title, 'Uploaded 3 files');
      expect(n.folder, '/DATA/Gallery');
    });

    test('names already there: keep both, replace, skip; two shared with one name', () async {
      for (final (choice, want) in [
        (ConflictChoice.keepBoth, ['a (2).jpg', 'c.jpg', 'c (2).jpg']),
        (ConflictChoice.replace, ['a.jpg', 'c.jpg', 'c (2).jpg']),
        (ConflictChoice.skip, [null, 'c.jpg', 'c (2).jpg']),
      ]) {
        final b = _batch([_file('a.jpg', 2), _file('c.jpg', 2), _file('c.jpg', 3)], conflict: choice);
        await http.runWithClient(() => runner(there: {'a.jpg'}).run(b, TransferCancel()), () => server.client);
        expect([for (final i in b.items) i.target], want, reason: choice.name);
        expect(b.items[0].state, choice == ConflictChoice.skip ? ShareItemState.skipped : ShareItemState.done);
      }
      expect(serverSafeName('a/b\\c.jpg'), 'a_b_c.jpg');
      expect(serverSafeName('..'), 'file');
    });

    test('a lost network pauses the batch; the next run resumes the file', () async {
      final b = _batch([_file('big.mp4', 13, mime: 'video/mp4'), _file('next.jpg', 2)]);
      server.failPost = (f) => f['chunkNumber'] == '3';
      final first = await http.runWithClient(() => runner().run(b, TransferCancel()), () => server.client);
      expect(first, ShareRunEnd.waitNetwork);
      expect(b.items[0].state, ShareItemState.pending);
      expect(b.items[0].target, 'big.mp4');
      expect(shareFinalNotice(b, first).title, 'Upload paused');

      // Back online, from what was saved (another engine, another run).
      final saved = ShareBatch.fromJson(jsonDecode(saves.last))!;
      server.failPost = null;
      server.posted.clear();
      final reads = <int>[];
      final second = await http.runWithClient(
        () => runner(there: {'big.mp4'}, read: (uri, offset, length) async {
          reads.add(offset);
          return _bytesOf(uri).sublist(offset, offset + length);
        }).run(saved, TransferCancel()),
        () => server.client,
      );
      expect(second, ShareRunEnd.finished);
      // Same name (not "big (2).mp4"); chunks 1-2 weren't sent again.
      expect(saved.items[0].target, 'big.mp4');
      expect(server.posted.where((p) => p.startsWith('/DATA/Gallery/big.mp4')), ['/DATA/Gallery/big.mp4#3', '/DATA/Gallery/big.mp4#4']);
      expect(reads.take(2), [8, 12]);
      expect(server.files['/DATA/Gallery/big.mp4'], _bytesOf(saved.items[0].file.uri));
      expect(saved.items.every((i) => i.state == ShareItemState.done), isTrue);
    });

    test('per-file errors: the rest still upload, with words for each', () async {
      final b = _batch([_file('gone.jpg', 2), _file('ok.jpg', 2)]);
      final end = await http.runWithClient(
        () => runner(read: (uri, offset, length) async {
          if (uri.contains('gone')) throw PlatformException(code: 'PERMISSION', message: 'denied');
          return _bytesOf(uri).sublist(offset, offset + length);
        }).run(b, TransferCancel()),
        () => server.client,
      );
      expect(end, ShareRunEnd.finished);
      expect(b.items[0].state, ShareItemState.failed);
      expect(b.items[0].error, contains('Share it again'));
      expect(b.items[1].state, ShareItemState.done);
      final n = shareFinalNotice(b, end);
      expect(n.title, 'Uploaded 1 of 2 files');
      expect(n.folder, isNull);

      server.refuse = 'write /DATA/Gallery/.x: no space left on device';
      final full = _batch([_file('x.jpg', 2)]);
      await http.runWithClient(() => runner().run(full, TransferCancel()), () => server.client);
      expect(full.items.single.error, 'Not enough space on the server.');
      expect(shareFinalNotice(full, ShareRunEnd.finished).title, "Couldn't upload 1 file");
      expect(shareErrorText(UploadException('open /DATA/x: permission denied')), "The server can't write to this folder.");
    });

    test('retry after a failure uploads only what failed', () async {
      final b = _batch([_file('a.jpg', 2), _file('b.jpg', 2)]);
      server.refuse = 'no space left on device';
      await http.runWithClient(() => runner().run(b, TransferCancel()), () => server.client);
      expect(b.count(ShareItemState.failed), 2);
      server.refuse = null;
      b.items[0].state = ShareItemState.done; // as if it had made it
      b.retry();
      await http.runWithClient(() => runner().run(b, TransferCancel()), () => server.client);
      expect(server.files.keys, ['/DATA/Gallery/b.jpg']);
      expect(b.items.every((i) => i.state == ShareItemState.done), isTrue);
    });

    test('cancel stops the batch and marks the rest', () async {
      final b = _batch([_file('a.jpg', 9), _file('b.jpg', 2)]);
      final cancel = TransferCancel();
      final end = await http.runWithClient(
        () => runner(read: (uri, offset, length) async {
          if (offset >= 4) cancel.cancel();
          return _bytesOf(uri).sublist(offset, offset + length);
        }).run(b, cancel),
        () => server.client,
      );
      expect(end, ShareRunEnd.cancelled);
      expect(b.items.map((i) => i.state), everyElement(ShareItemState.cancelled));
      expect(shareFinalNotice(b, end).title, 'Upload cancelled');
    });

    test('progress notice reads "Uploading 3 files to atom · 45%"', () {
      final b = _batch([_file('a.jpg', 1), _file('b.jpg', 1), _file('c.jpg', 1)]);
      final n = shareProgressNotice(b, const ShareProgress(doneBytes: 45, totalBytes: 100, index: 1, count: 3, current: 'b.jpg'));
      expect(n.title, 'Uploading 3 files to atom · 45%');
      expect(n.text, 'b.jpg · 2 of 3');
    });
  });

  test('uploadFrom sends what read returns (Files uses the same path)', () async {
    final server = FakeUploadServer();
    final data = Uint8List.fromList(List.generate(9, (i) => i));
    await http.runWithClient(
      () => _uploader().uploadFrom(size: 9, read: (o, l) async => data.sublist(o, o + l), destDir: '/DATA', relativePath: 'n.bin'),
      () => server.client,
    );
    expect(server.files['/DATA/n.bin'], data);
  });
}
