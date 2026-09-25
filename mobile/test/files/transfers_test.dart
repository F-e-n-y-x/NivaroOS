import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/models/file_entry.dart';
import 'package:nivaroos_mobile/screens/files/file_ops.dart';
import 'package:nivaroos_mobile/screens/files/resumable_upload.dart';
import 'package:nivaroos_mobile/screens/files/transfers.dart';
import 'package:nivaroos_mobile/services/api_client.dart';

/// The multipart form fields of a captured request.
Map<String, String> formFields(http.Request req) {
  final body = latin1.decode(req.bodyBytes);
  final out = <String, String>{};
  for (final m in RegExp(r'name="([^"]+)"\r\n\r\n([^\r]*)\r\n').allMatches(body)) {
    out[m.group(1)!] = m.group(2)!;
  }
  return out;
}

ResumableUploader uploader({int chunk = 8, Future<bool> Function()? refresh}) => ResumableUploader(
      chunkSize: chunk,
      retryDelay: Duration.zero,
      authHeader: () async => 'token',
      refreshToken: refresh ?? () async => true,
      endpoint: (q) => Uri.parse('http://nas.test/v2/casaos/file/upload').replace(queryParameters: q.isEmpty ? null : q),
    );

void main() {
  late Directory tmp;
  setUp(() => tmp = Directory.systemTemp.createTempSync('nivaro_transfer'));
  tearDown(() => tmp.deleteSync(recursive: true));

  File fileOf(int bytes, [String name = 'photo.jpg']) =>
      File('${tmp.path}/$name')..writeAsBytesSync(List.generate(bytes, (i) => i % 251));

  group('resumable upload (plan M-12)', () {
    test('chunk fields follow simple-uploader.js', () {
      final f = uploadChunkFields(destDir: '/DATA', relativePath: 'a/b c.jpg', totalSize: 20, chunkNumber: 3, chunkSize: 8);
      expect(f, {
        'path': '/DATA',
        'relativePath': 'a/b c.jpg',
        'filename': 'b c.jpg',
        'identifier': '20-abcjpg',
        'chunkNumber': '3',
        'chunkSize': '8',
        'currentChunkSize': '4',
        'totalChunks': '3',
        'totalSize': '20',
      });
      expect(uploadTotalChunks(0, 8), 1);
      expect(uploadTotalChunks(16, 8), 2);
      expect(uploadChunkFields(destDir: '/', relativePath: 'e', totalSize: 0, chunkNumber: 1, chunkSize: 8)['currentChunkSize'], '0');
    });

    test('8 MiB chunks by default', () => expect(uploadChunkSize, 8 * 1024 * 1024));

    test('skips chunks the server has, sends the rest in order, finishes on complete', () async {
      final file = fileOf(20);
      final posted = <Map<String, String>>[];
      final checked = <String>[];
      final progress = <int>[];
      await http.runWithClient(() async {
        await uploader().upload(
          file: file,
          destDir: '/DATA/Gallery',
          relativePath: 'photo.jpg',
          onProgress: (sent, total) => progress.add(sent),
        );
      }, () => MockClient((req) async {
            expect(req.headers['Authorization'], 'token');
            if (req.method == 'GET') {
              final n = req.url.queryParameters['chunkNumber']!;
              checked.add(n);
              return http.Response('', n == '2' ? 200 : 204);
            }
            final fields = formFields(req);
            posted.add(fields);
            return http.Response(jsonEncode({'success': 200, 'complete': fields['chunkNumber'] == '3'}), 200);
          }));
      expect(checked, ['1', '2', '3']);
      expect(posted.map((f) => f['chunkNumber']), ['1', '3']);
      expect(posted.first['currentChunkSize'], '8');
      expect(posted.last['currentChunkSize'], '4');
      expect(posted.first['path'], '/DATA/Gallery');
      expect(progress.last, 20);
    });

    test('the chunk bytes are the file’s bytes at the chunk offset', () async {
      final file = fileOf(12);
      final bodies = <String>[];
      await http.runWithClient(() => uploader().upload(file: file, destDir: '/DATA', relativePath: 'photo.jpg'),
          () => MockClient((req) async {
                if (req.method == 'GET') return http.Response('', 204);
                bodies.add(latin1.decode(req.bodyBytes));
                return http.Response(jsonEncode({'complete': formFields(req)['chunkNumber'] == '2'}), 200);
              }));
      final second = latin1.decode(List.generate(4, (i) => (8 + i) % 251));
      expect(bodies.last, contains('\r\n\r\n$second\r\n'));
    });

    test('a 401 refreshes the token once and retries', () async {
      final file = fileOf(4);
      var refreshed = 0;
      var posts = 0;
      await http.runWithClient(
        () => uploader(refresh: () async {
          refreshed++;
          return true;
        }).upload(file: file, destDir: '/DATA', relativePath: 'photo.jpg'),
        () => MockClient((req) async {
          if (req.method == 'GET') return http.Response('', 204);
          posts++;
          return posts == 1 ? http.Response('{}', 401) : http.Response(jsonEncode({'complete': true}), 200);
        }),
      );
      expect(refreshed, 1);
      expect(posts, 2);
    });

    test('a refused refresh stops with a sign-in message', () async {
      final file = fileOf(4);
      await expectLater(
        http.runWithClient(
          () => uploader(refresh: () async => false).upload(file: file, destDir: '/DATA', relativePath: 'photo.jpg'),
          () => MockClient((req) async => req.method == 'GET' ? http.Response('', 204) : http.Response('{}', 401)),
        ),
        throwsA(isA<UploadException>().having((e) => e.statusCode, 'status', 401)),
      );
    });

    test('server errors are retried, permanent ones are not', () async {
      final file = fileOf(4);
      var posts = 0;
      await http.runWithClient(
        () => uploader().upload(file: file, destDir: '/DATA', relativePath: 'photo.jpg'),
        () => MockClient((req) async {
          if (req.method == 'GET') return http.Response('', 204);
          posts++;
          return posts < 3 ? http.Response('{"message":"busy"}', 500) : http.Response(jsonEncode({'complete': true}), 200);
        }),
      );
      expect(posts, 3);

      posts = 0;
      await expectLater(
        http.runWithClient(
          () => uploader().upload(file: file, destDir: '/DATA', relativePath: 'photo.jpg'),
          () => MockClient((req) async {
            if (req.method == 'GET') return http.Response('', 204);
            posts++;
            return http.Response(jsonEncode({'message': 'a folder named "photo.jpg" already exists here'}), 400);
          }),
        ),
        throwsA(isA<UploadException>().having((e) => e.message, 'message', contains('already exists'))),
      );
      expect(posts, 1);
    });

    test('an upload the server never confirms is an error', () async {
      final file = fileOf(4);
      await expectLater(
        http.runWithClient(
          () => uploader().upload(file: file, destDir: '/DATA', relativePath: 'photo.jpg'),
          () => MockClient((req) async => req.method == 'GET' ? http.Response('', 204) : http.Response('{"complete":false}', 200)),
        ),
        throwsA(isA<UploadException>()),
      );
    });

    test('cancel stops before the next chunk', () async {
      final file = fileOf(20);
      final cancel = TransferCancel();
      var posts = 0;
      await expectLater(
        http.runWithClient(
          () => uploader().upload(file: file, destDir: '/DATA', relativePath: 'photo.jpg', cancel: cancel),
          () => MockClient((req) async {
            if (req.method == 'GET') return http.Response('', 204);
            posts++;
            cancel.cancel();
            return http.Response('{"complete":false}', 200);
          }),
        ),
        throwsA(isA<TransferCancelled>()),
      );
      expect(posts, 1);
    });
  });

  group('server to phone (plan M-11)', () {
    FileEntry remote(String name, int size, {bool dir = false}) =>
        FileEntry(name: name, path: '/DATA/Photos/$name', isDir: dir, size: size);

    Future<TransferOutcome> run(DownloadTask task) =>
        task.run((_) {}, TransferCancel());

    test('move deletes the originals only after every size matched', () async {
      final deleted = <List<String>>[];
      final outcome = await run(DownloadTask(
        items: [DownloadItem(remote('a.jpg', 3), 'a.jpg'), DownloadItem(remote('album', 0, dir: true), 'album')],
        destDir: tmp.path,
        move: true,
        listFolder: (path) async => RemoteListing([FileEntry(name: 'b.jpg', path: '$path/b.jpg', isDir: false, size: 2)]),
        download: (remote, target, onProgress) async {
          await target.writeAsBytes(remote.endsWith('a.jpg') ? [1, 2, 3] : [1, 2]);
          onProgress(3);
        },
        deleteRemote: (paths) async => deleted.add(paths),
      ));
      expect(outcome.failed, isFalse);
      expect(outcome.message, 'Moved 2 items to this phone');
      expect(File('${tmp.path}/a.jpg').lengthSync(), 3);
      expect(File('${tmp.path}/album/b.jpg').lengthSync(), 2);
      expect(deleted, [
        ['/DATA/Photos/a.jpg', '/DATA/Photos/album'],
      ]);
      // No temporary files left behind.
      expect(tmp.listSync(recursive: true).where((e) => e.path.contains('.nvpart')), isEmpty);
    });

    test('a move keeps server folders that hold hidden entries (review 4)', () async {
      // GET /v1/folder hides `.temp` folders and in-flight copies but
      // counts them in `total`; those were never downloaded, so the folder
      // must not be deleted wholesale.
      final deleted = <List<String>>[];
      final outcome = await run(DownloadTask(
        items: [DownloadItem(remote('album', 0, dir: true), 'album')],
        destDir: tmp.path,
        move: true,
        listFolder: (path) async => switch (path) {
              '/DATA/Photos/album' => RemoteListing([
                  FileEntry(name: 'b.jpg', path: '$path/b.jpg', isDir: false, size: 2),
                  FileEntry(name: 'clean', path: '$path/clean', isDir: true, size: 0),
                  FileEntry(name: 'dirty', path: '$path/dirty', isDir: true, size: 0),
                ]),
              '/DATA/Photos/album/dirty' => RemoteListing(
                  [FileEntry(name: 'c.jpg', path: '$path/c.jpg', isDir: false, size: 2)],
                  hidden: 1,
                ),
              _ => RemoteListing([FileEntry(name: 'd.jpg', path: '$path/d.jpg', isDir: false, size: 2)]),
            },
        download: (remote, target, onProgress) async => target.writeAsBytes([1, 2]),
        deleteRemote: (paths) async => deleted.add(paths),
      ));
      expect(outcome.failed, isFalse);
      expect(outcome.message, contains('stays on the server'));
      expect(deleted, [
        ['/DATA/Photos/album/b.jpg', '/DATA/Photos/album/clean', '/DATA/Photos/album/dirty/c.jpg'],
      ]);
    });

    test('RemoteListing counts what the listing hides', () {
      final l = RemoteListing.fromResponse({
        'content': [
          {'name': 'a', 'path': '/a', 'is_dir': false, 'size': 1},
        ],
        'total': 3,
      });
      expect(l.entries, hasLength(1));
      expect(l.hidden, 2);
      expect(RemoteListing.fromResponse({'content': []}).hidden, 0);
    });

    test('a short download keeps the originals and says so', () async {
      final deleted = <List<String>>[];
      final outcome = await run(DownloadTask(
        items: [DownloadItem(remote('a.jpg', 10), 'a.jpg')],
        destDir: tmp.path,
        move: true,
        download: (remote, target, onProgress) async => target.writeAsBytes([1, 2]),
        deleteRemote: (paths) async => deleted.add(paths),
      ));
      expect(outcome.failed, isTrue);
      expect(outcome.message, contains('nothing was removed from the server'));
      expect(deleted, isEmpty);
      expect(File('${tmp.path}/a.jpg').existsSync(), isFalse);
    });

    test('a failed delete is reported, not swallowed', () async {
      final outcome = await run(DownloadTask(
        items: [DownloadItem(remote('a.jpg', 1), 'a.jpg')],
        destDir: tmp.path,
        move: true,
        download: (remote, target, onProgress) async => target.writeAsBytes([1]),
        deleteRemote: (paths) async => throw ApiException('Permission denied'),
      ));
      expect(outcome.failed, isTrue);
      expect(outcome.message, contains('couldn’t remove the originals'));
      expect(outcome.message, contains('Permission denied'));
      expect(File('${tmp.path}/a.jpg').existsSync(), isTrue);
    });

    test('copy never deletes', () async {
      var deletes = 0;
      final outcome = await run(DownloadTask(
        items: [DownloadItem(remote('a.jpg', 1), 'a (2).jpg')],
        destDir: tmp.path,
        download: (remote, target, onProgress) async => target.writeAsBytes([1]),
        deleteRemote: (paths) async => deletes++,
      ));
      expect(outcome.message, 'Downloaded 1 item to this phone');
      expect(File('${tmp.path}/a (2).jpg').existsSync(), isTrue);
      expect(deletes, 0);
    });
  });

  group('phone to phone', () {
    test('copy and move with a new name', () async {
      final src = Directory('${tmp.path}/src')..createSync();
      final dest = Directory('${tmp.path}/dest')..createSync();
      File('${src.path}/a.txt').writeAsStringSync('a');
      Directory('${src.path}/d').createSync();
      File('${src.path}/d/b.txt').writeAsStringSync('b');

      await LocalTransferTask(items: [UploadItem('${src.path}/a.txt', 'a (2).txt'), UploadItem('${src.path}/d', 'd')], destDir: dest.path)
          .run((_) {}, TransferCancel());
      expect(File('${dest.path}/a (2).txt').readAsStringSync(), 'a');
      expect(File('${dest.path}/d/b.txt').readAsStringSync(), 'b');
      expect(File('${src.path}/a.txt').existsSync(), isTrue);

      await LocalTransferTask(items: [UploadItem('${src.path}/a.txt', 'a.txt')], destDir: dest.path, move: true).run((_) {}, TransferCancel());
      expect(File('${src.path}/a.txt').existsSync(), isFalse);
      expect(File('${dest.path}/a.txt').existsSync(), isTrue);
    });
  });

  group('server to server (plan M-29)', () {
    setUp(() {
      FlutterSecureStorage.setMockInitialValues({});
      ApiClient.instance.setBaseUrl('http://nas.test');
      ApiClient.instance.setSession('t', 'r');
    });

    test('one job per conflict style, polled to the end', () async {
      final submitted = <Map<String, dynamic>>[];
      var polls = 0;
      final outcome = await http.runWithClient(
        () => ServerTransferTask(
          kind: TransferKind.copy,
          destDir: '/DATA/Backup',
          pollInterval: Duration.zero,
          batches: planTransfer(['/DATA/a', '/DATA/b'], {'/DATA/b': ConflictChoice.replace}),
        ).run((_) {}, TransferCancel()),
        () => MockClient((req) async {
          if (req.method == 'POST' && req.url.path == '/v1/batch/task') {
            final body = jsonDecode(req.body) as Map<String, dynamic>;
            submitted.add(body);
            return http.Response(jsonEncode({'success': 200, 'data': {'id': 'job${submitted.length}'}}), 200);
          }
          if (req.url.path == '/v1/batch/tasks') {
            polls++;
            final state = polls < 2 ? 'running' : 'done';
            return http.Response(
              jsonEncode({
                'success': 200,
                'data': [
                  {'id': 'job1', 'state': state, 'bytes_total': 10, 'bytes_done': 5, 'files_total': 1, 'files_done': 1},
                  {'id': 'job2', 'state': state, 'bytes_total': 10, 'bytes_done': 5, 'files_total': 1, 'files_done': 1, 'files_skipped': 0},
                  {'id': 'someone-else', 'state': 'running'},
                ],
              }),
              200,
            );
          }
          return http.Response('{}', 404);
        }),
      );
      expect(submitted.map((b) => b['style']), ['rename', 'overwrite']);
      expect(submitted.first['item'], [
        {'from': '/DATA/a'},
      ]);
      expect(submitted.first['type'], 'copy');
      expect(submitted.first['to'], '/DATA/Backup');
      expect(outcome.failed, isFalse);
      expect(outcome.message, 'Copied 2 items to Backup');
      expect(outcome.affectedDirs, contains('/DATA/Backup'));
    });

    test('failures are counted from the job', () {
      final snap = ServerJobsSnapshot([
        {
          'state': 'done_with_errors',
          'files_failed': 2,
          'failures': [
            {'path': '/x', 'error': 'no space left on device'},
          ],
        },
      ]);
      final o = snap.outcome(TransferKind.move, '/DATA/x', ['/DATA/a']);
      expect(o.failed, isTrue);
      expect(o.message, '2 files couldn’t be moved: no space left on device');
    });

    test('an unknown total is an indeterminate bar', () {
      expect(ServerJobsSnapshot([{'state': 'scanning', 'bytes_total': -1}]).bytesTotal, isNull);
      expect(const TransferProgress(title: 't').fraction, isNull);
      expect(const TransferProgress(title: 't', doneBytes: 5, totalBytes: 10).fraction, 0.5);
    });
  });

  test('the queue runs tasks in order and reports each outcome', () async {
    final outcomes = <String>[];
    final queue = TransferQueue(onFinished: (o) => outcomes.add(o.message));
    queue.add(ServerCallTask(title: 'one', call: () async => const TransferOutcome(message: '1')));
    queue.add(ServerCallTask(title: 'two', call: () async => throw Exception('boom')));
    await Future<void>.delayed(const Duration(milliseconds: 10));
    expect(outcomes, ['1', 'boom']);
    expect(queue.current.value, isNull);
    queue.dispose();
  });
}
