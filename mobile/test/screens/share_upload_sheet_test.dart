import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:nivaroos_mobile/models/server_profile.dart';
import 'package:nivaroos_mobile/screens/files/file_ops.dart';
import 'package:nivaroos_mobile/screens/share_upload_sheet.dart';
import 'package:nivaroos_mobile/services/share_upload.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';

import '../screenshots/harness.dart';

const _files = SharedFiles([
  SharedFile(uri: 'content://media/1', name: 'IMG_0001.jpg', size: 3 << 20, mime: 'image/jpeg'),
  SharedFile(uri: 'content://media/2', name: 'VID_0002.mp4', size: 40 << 20, mime: 'video/mp4'),
  SharedFile(uri: 'content://docs/3', name: 'lease.pdf', size: 200 << 10, mime: 'application/pdf'),
], rejected: 1);

/// Opens the sheet from a button, with a fake job scheduler and a store
/// in a temp folder; returns what the sheet popped with.
class _Host extends StatelessWidget {
  const _Host(this.open, this.done);
  final Future<ShareBatch?> Function(BuildContext) open;
  final void Function(ShareBatch?) done;

  @override
  Widget build(BuildContext context) => Scaffold(
        body: Builder(builder: (context) => Center(child: TextButton(onPressed: () async => done(await open(context)), child: const Text('open')))),
      );
}

void main() {
  late Directory dir;
  late ShareUploadStore store;
  late List<ShareBatch> started;
  late bool accept;

  setUp(() async {
    await signIn();
    dir = Directory.systemTemp.createTempSync('share_sheet');
    store = ShareUploadStore(dir);
    started = [];
    accept = true;
  });
  tearDown(() => dir.deleteSync(recursive: true));

  Future<bool> start(ShareBatch b) async {
    started.add(b);
    return accept;
  }

  Future<ShareBatch?> pumpSheet(WidgetTester tester, {SharedFiles? shared, ShareBatch? batch}) async {
    ShareBatch? result;
    var closed = false;
    await http.runWithClient(() async {
      await tester.pumpWidget(testApp(_Host(
        (c) => showShareUploadSheet(c, shared: shared, batch: batch, store: store, start: start, thumbnail: (_) async => null),
        (b) {
          result = b;
          closed = true;
        },
      )));
      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();
    }, () => FakeServer().client);
    expect(closed, isFalse);
    return result;
  }

  testWidgets('shows the files and uploads to a quick pick with the conflict choice', (tester) async {
    tester.view.physicalSize = const Size(412, 915) * 2;
    tester.view.devicePixelRatio = 2;
    addTearDown(tester.view.reset);
    ShareBatch? popped;
    await tester.pumpWidget(testApp(_Host(
      (c) => showShareUploadSheet(c, shared: _files, store: store, start: start, thumbnail: (_) async => null),
      (b) => popped = b,
    )));
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    expect(find.text('Upload to NivaroOS'), findsOneWidget);
    expect(find.text('3 files · 43.2 MB'), findsOneWidget);
    expect(find.text('IMG_0001.jpg'), findsOneWidget);
    expect(find.text('lease.pdf'), findsOneWidget);
    expect(find.textContaining("1 item couldn't be read"), findsOneWidget);
    // Last used: none yet, so Downloads.
    expect(find.text('/DATA/Downloads'), findsOneWidget);
    // One server: no picker.
    expect(find.text('Server'), findsNothing);

    await tester.tap(find.text('Gallery'));
    await tester.pumpAndSettle();
    expect(find.text('/DATA/Gallery'), findsOneWidget);
    await tester.tap(find.text('Keep both (number the new one)'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Replace it').last);
    await tester.pumpAndSettle();
    expect(find.textContaining('are overwritten'), findsOneWidget);

    await tester.ensureVisible(find.text('Upload 3'));
    await tester.tap(find.text('Upload 3'));
    await tester.pumpAndSettle();

    final b = started.single;
    expect(popped, same(b));
    expect(b.destDir, '/DATA/Gallery');
    expect(b.conflict, ConflictChoice.replace);
    expect(b.server, fakeServer);
    expect([for (final i in b.items) i.file.name], ['IMG_0001.jpg', 'VID_0002.mp4', 'lease.pdf']);
    expect(store.load(b.id)?.items, hasLength(3));
    expect(await StorageService.instance.getShareUploadDir(), '/DATA/Gallery');
  });

  testWidgets('remembers the last folder and offers the servers when there are several', (tester) async {
    await StorageService.instance.setShareUploadDir('/DATA/Media/Phone');
    await StorageService.instance.saveProfile(ServerProfile(id: 'a', name: 'atom', url: fakeServer, username: 'alex', accessToken: 't', refreshToken: 'r'));
    await StorageService.instance.saveProfile(ServerProfile(id: 'b', name: 'quark', url: 'http://quark.test', username: 'alex', accessToken: 't', refreshToken: 'r'));
    await pumpSheet(tester, shared: _files);
    expect(find.text('/DATA/Media/Phone'), findsOneWidget);
    expect(find.text('Server'), findsOneWidget);
    expect(find.text('atom'), findsOneWidget);
  });

  testWidgets('says so when Android refuses to start the upload', (tester) async {
    accept = false;
    await pumpSheet(tester, shared: _files);
    await tester.ensureVisible(find.text('Upload 3'));
    await tester.tap(find.text('Upload 3'));
    await tester.pumpAndSettle();
    expect(find.textContaining("Android didn't start the upload"), findsOneWidget);
    expect(find.text('Upload to NivaroOS'), findsOneWidget);
  });

  testWidgets('a finished batch shows each file, and Retry sends the failed ones again', (tester) async {
    final b = ShareBatch(
      id: 'done1',
      server: fakeServer,
      serverName: 'atom',
      destDir: '/DATA/Gallery',
      items: [
        ShareItem(_files.files[0], target: 'IMG_0001.jpg', state: ShareItemState.done),
        ShareItem(_files.files[1], target: 'VID_0002.mp4', state: ShareItemState.failed, error: 'Not enough space on the server.'),
        ShareItem(_files.files[2], target: 'lease.pdf', state: ShareItemState.skipped),
      ],
    );
    store.save(b);
    await pumpSheet(tester, batch: b);
    expect(find.text('Uploaded 1 of 2 files'), findsOneWidget);
    expect(find.text('Not enough space on the server.'), findsOneWidget);
    expect(find.text('Skipped: already there'), findsOneWidget);
    await tester.tap(find.text('Retry 1'));
    await tester.pumpAndSettle();
    expect(started.single.id, 'done1');
    expect(store.load('done1')!.items[1].state, ShareItemState.pending);
    expect(store.load('done1')!.items[0].state, ShareItemState.done);
  });
}
