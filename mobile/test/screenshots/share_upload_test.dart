// "Upload to NivaroOS" (files shared from another app) in every style
// (Rack, Tonal, Console), light and true black, at 412 and 360 dp and at
// 200% text: the sheet as a share opens it, and a finished batch with a
// failed file and Retry.
//
//   flutter test test/screenshots/share_upload_test.dart --update-goldens
//
// PNGs: goldens/share_upload/<style>/<screen>_<mode>_<size>[_text2x].png.
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/share_upload_sheet.dart';
import 'package:nivaroos_mobile/services/share_upload.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'harness.dart';

/// Opens the sheet over a blank page once, as a share does.
class _SheetHost extends StatefulWidget {
  const _SheetHost(this.open);
  final void Function(BuildContext context) open;

  @override
  State<_SheetHost> createState() => _SheetHostState();
}

class _SheetHostState extends State<_SheetHost> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) widget.open(context);
    });
  }

  @override
  Widget build(BuildContext context) => const AppScaffold(title: 'Home', body: SizedBox.expand());
}

const _shared = SharedFiles([
  SharedFile(uri: 'content://media/1', name: 'IMG_20261004_181502.jpg', size: 4 << 20, mime: 'image/jpeg'),
  SharedFile(uri: 'content://media/2', name: 'VID_20261004_182210.mp4', size: 212 << 20, mime: 'video/mp4'),
  SharedFile(uri: 'content://media/3', name: 'IMG_20261005_090144.jpg', size: 3 << 20, mime: 'image/jpeg'),
  SharedFile(uri: 'content://docs/4', name: 'Lease agreement 2026.pdf', size: 640 << 10, mime: 'application/pdf'),
  SharedFile(uri: 'content://docs/5', name: 'boarding-pass.pkpass', size: 90 << 10, mime: 'application/octet-stream'),
]);

final _thumbs = [for (var i = 0; i < 3; i++) File('test/screenshots/fixtures/icons/app_$i.png').readAsBytesSync()];

Future<Uint8List?> _thumb(String uri) async => uri.startsWith('content://media/') ? _thumbs[int.parse(uri.split('/').last) - 1] : null;

ShareBatch _finished() => ShareBatch(
      id: 'shot',
      server: fakeServer,
      serverName: 'atom',
      destDir: '/DATA/Gallery',
      items: [
        ShareItem(_shared.files[0], target: _shared.files[0].name, state: ShareItemState.done),
        ShareItem(_shared.files[1], target: _shared.files[1].name, state: ShareItemState.failed, error: 'Not enough space on the server.'),
        ShareItem(_shared.files[2], target: 'IMG_20261005_090144 (2).jpg', state: ShareItemState.done),
        ShareItem(_shared.files[3], target: _shared.files[3].name, state: ShareItemState.skipped),
      ],
    );

final _store = ShareUploadStore(Directory.systemTemp.createTempSync('share_shots'));

final Map<String, Widget Function()> _shots = {
  'share_upload': () => _SheetHost((c) => showShareUploadSheet(c, shared: _shared, store: _store, start: (_) async => true, thumbnail: _thumb)),
  'share_upload_status': () => _SheetHost((c) => showShareUploadSheet(c, batch: _finished(), store: _store, start: (_) async => true, thumbnail: _thumb)),
};

const _styles = [DesignDirection.rack, DesignDirection.tonal, DesignDirection.console];

void main() {
  setUp(signIn);

  for (final d in _styles) {
    for (final MapEntry(key: name, value: build) in _shots.entries) {
      for (final m in const [AppThemeMode.light, AppThemeMode.black]) {
        for (final (size, scale) in const [(phone, 1.0), (smallPhone, 1.0), (phone, 2.0)]) {
          testWidgets('${d.name} $name ${m.name} ${size.width.toInt()} ${scale}x', (tester) {
            return shoot(
              tester,
              dir: 'share_upload/${d.name}',
              name: name,
              screen: build(),
              brightness: m == AppThemeMode.light ? Brightness.light : Brightness.dark,
              themeName: m.name,
              appearance: Appearance(mode: m, direction: d),
              size: size,
              textScale: scale,
            );
          });
        }
      }
    }
  }
}
