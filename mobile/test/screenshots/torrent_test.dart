// Torrents in Download Station in every style (Rack, Tonal, Console),
// light and true black, against the fake server: fixtures/ds/torrents.json
// and torrent.json (the sidecar's real TorrentInfo / TorrentDetail shape).
//
//   flutter test test/screenshots/torrent_test.dart --update-goldens
//
// PNGs: goldens/torrents/<style>/<screen>_<mode>_<size>[_text2x].png.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/download_station/ds_add_torrent_sheet.dart';
import 'package:nivaroos_mobile/screens/download_station/torrent_detail_screen.dart';
import 'package:nivaroos_mobile/screens/download_station/torrent_settings_screen.dart';
import 'package:nivaroos_mobile/screens/download_station/torrents_screen.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'harness.dart';

const _ds = '/v1/download-station';
const _debian = '7acf8fb590b2060dd9c3146ef770169d593433b0';

final Map<String, Object> _server = {
  'GET $_ds/torrents': fixture('ds/torrents'),
  'GET $_ds/torrents/$_debian': fixture('ds/torrent'),
  'GET $_ds/torrents/trackers': fixture('ds/torrent_trackers'),
  'GET $_ds/settings': fixture('ds/settings'),
  'GET $_ds/storage/roots': fixture('ds/roots'),
};

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
  Widget build(BuildContext context) => const AppScaffold(title: 'Torrents', body: SizedBox.expand());
}

class _Shot {
  const _Shot(this.build, {this.overrides = const {}, this.full = false});
  final Widget Function() build;
  final Map<String, Object> overrides;
  final bool full;
}

final Map<String, _Shot> _shots = {
  'torrents_list': _Shot(() => const TorrentsScreen(), overrides: _server, full: true),
  'torrents_empty': _Shot(() => const TorrentsScreen(), overrides: {
    ..._server,
    'GET $_ds/torrents': {'engine': 'qbittorrent', 'running': false, 'qbittorrent': true, 'unsupported': <Object>[], 'torrents': <Object>[]},
  }),
  'torrent_detail': _Shot(() => const TorrentDetailScreen(hash: _debian), overrides: _server, full: true),
  'torrent_add': _Shot(
    () => _SheetHost((c) => showAddTorrentSheet(c, text: 'magnet:?xt=urn:btih:$_debian&dn=debian-13.7.0-amd64-netinst.iso')),
    overrides: _server,
  ),
  'torrent_settings': _Shot(() => const TorrentSettingsScreen(), overrides: _server, full: true),
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
              dir: 'torrents/${d.name}',
              name: name,
              screen: s.build(),
              brightness: m == AppThemeMode.light ? Brightness.light : Brightness.dark,
              themeName: m.name,
              appearance: Appearance(mode: m, direction: d),
              size: size,
              textScale: scale,
              pushed: true,
              overrides: s.overrides,
            );
          });
        }
      }
    }
  }
}
