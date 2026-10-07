// Download Station and share links in every style (Rack, Tonal, Console),
// light and true black, at 412 and 360 dp and at 200% text, against the
// fake server: fixtures/ds (the sidecar's real DownloadView shape) and
// fixtures/v1/quickshare.json.
//
//   flutter test test/screenshots/download_station_test.dart --update-goldens
//
// PNGs: goldens/download_station/<style>/<screen>_<mode>_<size>[_text2x].png.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/file_entry.dart';
import 'package:nivaroos_mobile/screens/download_station/download_station_screen.dart';
import 'package:nivaroos_mobile/screens/download_station/ds_add_sheet.dart';
import 'package:nivaroos_mobile/screens/download_station/ds_settings_screen.dart';
import 'package:nivaroos_mobile/screens/files/share_link.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'harness.dart';

const _ds = '/v1/download-station';

final Map<String, Object> _dsServer = {
  'GET $_ds/downloads': jsonDecode(File('test/screenshots/fixtures/ds/downloads.json').readAsStringSync()) as Object,
  'GET $_ds/settings': fixture('ds/settings'),
  'GET $_ds/storage/roots': fixture('ds/roots'),
};

/// Opens a bottom sheet over a blank page once, so the sheet is shot as
/// it shows in the app.
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
  Widget build(BuildContext context) => const AppScaffold(title: 'Files', body: SizedBox.expand());
}

const _report = FileEntry(name: 'tax-return-2025.pdf', path: '/DATA/Documents/tax-return-2025.pdf', isDir: false, size: 1 << 20);

final Map<String, Object?> _created = {
  'success': 200,
  'message': 'ok',
  'data': (fixture('v1/quickshare')['data'] as List)[1],
};

class _Shot {
  const _Shot(this.build, {this.overrides = const {}, this.full = false, this.before});
  final Widget Function() build;
  final Map<String, Object> overrides;
  final bool full;
  final Future<void> Function(WidgetTester tester)? before;
}

Future<void> _tapAndSettle(WidgetTester tester, Finder f) async {
  await tester.tap(f);
  for (var i = 0; i < 6; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 200));
  }
}

final Map<String, _Shot> _shots = {
  'ds_list': _Shot(() => const DownloadStationScreen(), overrides: _dsServer, full: true),
  'ds_empty': _Shot(() => const DownloadStationScreen(), overrides: {..._dsServer, 'GET $_ds/downloads': <Object>[]}),
  'ds_unavailable': _Shot(() => const DownloadStationScreen(), overrides: {'GET $_ds/downloads': const FakeResponse({'error': 'download station unavailable'}, status: 502)}),
  'ds_add': _Shot(() => _SheetHost((c) => showAddDownloadSheet(c)), overrides: _dsServer, full: true),
  'ds_share': _Shot(
    () => _SheetHost((c) => showAddDownloadSheet(c, text: 'Debian 13 https://cdimage.debian.org/debian-cd/current/amd64/iso-cd/debian-13.1.0-amd64-netinst.iso', pickServer: true)),
    overrides: _dsServer,
    full: true,
  ),
  'ds_settings': _Shot(() => const DsSettingsScreen(), overrides: _dsServer),
  'share_link_sheet': _Shot(() => _SheetHost((c) => showShareLinkSheet(c, _report)), full: true),
  'share_link_created': _Shot(
    () => _SheetHost((c) => showShareLinkSheet(c, _report)),
    overrides: {'POST /v1/quickshare': _created},
    before: (tester) async {
      await _tapAndSettle(tester, find.text('Create link'));
      await _tapAndSettle(tester, find.text('QR code'));
    },
  ),
  'shared_links': _Shot(() => const SharedLinksScreen(), full: true),
};

const _styles = [DesignDirection.rack, DesignDirection.tonal, DesignDirection.console];

void main() {
  setUp(signIn);

  for (final d in _styles) {
    for (final MapEntry(key: name, value: s) in _shots.entries) {
      for (final m in const [AppThemeMode.light, AppThemeMode.black]) {
        final sizes = s.full ? const [(phone, 1.0), (smallPhone, 1.0), (phone, 2.0)] : const [(phone, 1.0)];
        for (final (size, scale) in sizes) {
          testWidgets('${d.name} $name ${m.name} ${size.width.toInt()} ${scale}x', (tester) {
            return shoot(
              tester,
              dir: 'download_station/${d.name}',
              name: name,
              screen: s.build(),
              brightness: m == AppThemeMode.light ? Brightness.light : Brightness.dark,
              themeName: m.name,
              appearance: Appearance(mode: m, direction: d),
              size: size,
              textScale: scale,
              pushed: true,
              overrides: s.overrides,
              before: s.before,
            );
          });
        }
      }
    }
  }
}
