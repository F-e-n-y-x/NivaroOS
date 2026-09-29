// Screenshots of the Apps area: the Apps tab and its states, an app's
// page, logs, the app store, a store app's page, custom install and the
// terminal. Same harness as screens_test.dart; PNGs in goldens/apps/.
// ignore_for_file: implementation_imports
import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/container_entry.dart';
import 'package:nivaroos_mobile/screens/app_store_screen.dart';
import 'package:nivaroos_mobile/screens/apps_screen.dart';
import 'package:nivaroos_mobile/screens/container_logs_screen.dart';
import 'package:nivaroos_mobile/screens/custom_install_screen.dart';
import 'package:nivaroos_mobile/screens/terminal_screen.dart';
import 'package:nivaroos_mobile/screens/terminal_sessions_screen.dart';
import 'package:nivaroos_mobile/services/terminal_sessions.dart';
import 'package:nivaroos_mobile/widgets/terminal_surface.dart';
import 'package:xterm/src/ui/render.dart';
import 'package:xterm/xterm.dart' show CellOffset;

import 'harness.dart';

/// A running container app, as the Apps tab builds it from the fixtures.
const jellyfin = InstalledApp(
  id: 'jellyfin',
  title: 'Jellyfin',
  kind: AppKind.compose,
  status: 'running',
  image: 'linuxserver/jellyfin:10.11.10',
  scheme: 'http',
  port: '8097',
  index: '/',
  storeAppId: 'jellyfin',
  icon: 'https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/Jellyfin/icon.svg',
  statusText: 'Up 10 hours',
  hasUpdate: true,
);

const searxng = InstalledApp(
  id: 'searxng',
  title: 'searxng',
  kind: AppKind.container,
  status: 'exited',
  image: 'searxng/searxng:latest',
  statusText: 'Exited (0) 2 hours ago',
  autoUpdate: true,
);

const jellyfinContainer = InstalledApp(id: 'jellyfin', title: 'jellyfin', kind: AppKind.container, status: 'running');

/// The "build box" session from fixtures/v1/sys/terminal-sessions.json.
final buildBox = TerminalSession.fromJson(
  (fixture('v1/sys/terminal-sessions')['data']['sessions'] as List).firstWhere((s) => s['title'] == 'build box') as Map<String, dynamic>,
);

/// A terminal session that reattaches: hello, the replayed output (a
/// build, a prompt, a multi-byte character split across two frames - plan
/// M-07), then live.
class FakeTerminal implements TerminalTransport {
  final _frames = StreamController<dynamic>();
  final sent = <Object>[];

  FakeTerminal() {
    _frames.add('\u0000${jsonEncode({'type': 'hello', 'session': fixture('v1/sys/terminal-sessions')['data']['sessions'][1], 'replay_bytes': 900})}');
    final line = utf8.encode('\x1b[1;32malex@atom\x1b[0m:\x1b[1;34m~/src/nivaroos\x1b[0m\$ docker ps --format "{{.Names}}\\t{{.Status}}"\r\n');
    final out = utf8.encode('jellyfin        Up 10 hours\r\nnextcloud       Up 10 hours\r\nhomeassistant   Up 3 days\r\n'
        'Grüße aus Köln, café crème\r\n'
        '\x1b[1;32malex@atom\x1b[0m:\x1b[1;34m~/src/nivaroos\x1b[0m\$ make -j8\r\n'
        'go build -o build/nivaroos ./services/core\r\n'
        'go build -o build/nivaroos-app-management ./services/app-management\r\n'
        'pnpm --dir ui build\r\n'
        '\x1b[32m✓\x1b[0m 1843 modules transformed.\r\n'
        'build/sysroot/var/lib/nivaroos/www/index.html   \x1b[2m2.31 kB\x1b[0m\r\n'
        '\x1b[33mwarning\x1b[0m: chunk vendor.js is larger than 500 kB\r\n'
        '\x1b[1;32malex@atom\x1b[0m:\x1b[1;34m~/src/nivaroos\x1b[0m\$ ');
    _frames.add(line);
    // Split inside the 'ü' (C3 BC): the screen must not show U+FFFD.
    final cut = out.indexOf(0xBC);
    _frames.add(out.sublist(0, cut));
    _frames.add(out.sublist(cut));
    _frames.add('\u0000{"type":"live"}');
  }

  @override
  Stream<dynamic> get stream => _frames.stream;

  @override
  void add(Object frame) => sent.add(frame);

  @override
  Future<void> close() async => _frames.close();

  @override
  int? get closeCode => null;

  @override
  String? get closeReason => null;
}

/// Long-presses "nextcloud" in the terminal, for the selection shot.
Future<void> selectInTerminal(WidgetTester tester) async {
  final r = tester.allRenderObjects.whereType<RenderTerminal>().first;
  final buffer = tester.widget<TerminalSurface>(find.byType(TerminalSurface)).terminal.buffer;
  final y = [for (var i = 0; i < buffer.height; i++) buffer.lines[i].getText()].indexWhere((l) => l.startsWith('nextcloud'));
  final at = r.localToGlobal(r.getOffset(CellOffset(3, y)) + Offset(r.cellSize.width / 2, r.cellSize.height / 2));
  await tester.longPressAt(at);
  await tester.pump(const Duration(seconds: 1));
}

enum Size2 { phone, small, text2x }

const _noSessions = {
  'success': 200,
  'message': 'OK',
  'data': {'sessions': [], 'running': 0, 'limits': {'max_sessions': 12, 'detached_timeout_seconds': 86400}},
};

void main() {
  setUp(() async {
    AppsScreen.clearCache();
    await signIn();
  });
  tearDown(() => StoreInstaller.instance.debugSet('jellyfin', null));

  /// Light and dark at 412x915, plus [extra].
  void shots(
    String name,
    Widget Function() build, {
    Set<Size2> extra = const {},
    Map<String, Object> overrides = const {},
    bool tab = false,
    Future<void> Function(WidgetTester)? before,
    bool fixedDark = false,
  }) {
    // A screen that is always dark (the terminal) is shot once, named for it.
    for (final b in fixedDark ? const [Brightness.light] : Brightness.values) {
      for (final s in {Size2.phone, ...extra}) {
        final label = switch (s) { Size2.phone => '', Size2.small => ' small', Size2.text2x => ' 200% text' };
        testWidgets('$name$label ${b.name}', (tester) async {
          await shoot(
            tester,
            name: name,
            dir: 'apps',
            screen: build(),
            brightness: b,
            size: s == Size2.small ? smallPhone : phone,
            textScale: s == Size2.text2x ? 2 : 1,
            overrides: overrides,
            tab: tab,
            before: before,
            pushed: !tab,
            themeName: fixedDark ? 'fixed_dark' : null,
          );
        });
      }
    }
  }

  const all = {Size2.small, Size2.text2x};

  // The Apps tab.
  shots('apps', () => const AppsScreen(), tab: true, extra: all);
  shots('apps_empty', () => const AppsScreen(), tab: true, overrides: {
    'GET /v2/app_management/web/appgrid': {'data': []},
    'GET /v1/users/current/custom/link': {'data': []},
  });
  shots('apps_error', () => const AppsScreen(), tab: true, overrides: {
    'GET /v2/app_management/web/appgrid': const FakeResponse({'message': 'docker daemon is not running'}, status: 500),
  });
  shots('apps_search_none', () => const AppsScreen(), tab: true, before: (tester) async {
    await tester.enterText(find.byType(SearchBar), 'plex');
    await tester.pump();
  });

  shots('apps_updates', () => const AppsScreen(), tab: true, extra: {Size2.small}, before: (tester) async {
    final chip = find.widgetWithText(ChoiceChip, '1 update');
    // Scroll only the chip row (ensureVisible would also move the page).
    await tester.drag(find.ancestor(of: chip, matching: find.byType(SingleChildScrollView)).first, const Offset(-300, 0));
    await tester.pump(const Duration(milliseconds: 400));
    await tester.tap(chip);
    await tester.pump(const Duration(milliseconds: 400));
  });

  // An app's page.
  shots('app_detail', () => const AppDetailScreen(app: jellyfin), extra: all, overrides: {
    'GET /v2/app_management/compose/jellyfin': {
      'data': {
        'status': 'running',
        'store_info': {
          'category': 'Media',
          'tips': {
            'before_install': {'en_us': 'Sign in with the account you create on first start. Media folders are under /DATA/Media.'},
          },
        },
      },
    },
  });
  shots('app_detail_stopped', () => const AppDetailScreen(app: searxng), extra: {Size2.small});

  // Logs.
  shots('container_logs', () => ContainerLogsScreen.forApp(jellyfinContainer), extra: all);
  shots('container_logs_empty', () => ContainerLogsScreen.forApp(jellyfinContainer), overrides: {
    'GET /v1/container/jellyfin/logs': {'data': ''},
  });

  // The store.
  shots('app_store', () => const AppStoreScreen(), extra: all);
  shots('app_store_error', () => const AppStoreScreen(), overrides: {
    'GET /v2/app_management/apps': const FakeResponse({'message': 'The app store sources could not be read.'}, status: 500),
  });
  shots(
    'store_app_detail',
    () => const StoreAppDetailScreen(app: StoreApp(id: 'jellyfin', title: 'Jellyfin'), arch: 'amd64'),
    extra: all,
  );
  shots(
    'store_app_installing',
    () {
      StoreInstaller.instance.debugSet('jellyfin', const InstallProgress(percent: 42));
      return const StoreAppDetailScreen(app: StoreApp(id: 'jellyfin', title: 'Jellyfin'), arch: 'amd64');
    },
  );
  shots(
    'store_app_other_cpu',
    () => const StoreAppDetailScreen(app: StoreApp(id: 'jellyfin', title: 'Jellyfin'), arch: 'riscv64'),
  );

  // Custom install.
  shots('custom_install', () => const CustomInstallScreen(), extra: {Size2.text2x});
  shots('custom_install_template', () => CustomInstallScreen(initialYaml: CustomInstallScreen.templates[2].yaml));

  // The terminal (dark in both themes): reattached to a running session,
  // with a selection, a lost connection, and a session the server no
  // longer has.
  shots('terminal', () => TerminalScreen(session: buildBox, connector: (uri, headers) async => FakeTerminal()), extra: {Size2.small}, fixedDark: true);
  shots('terminal_selection', () => TerminalScreen(session: buildBox, connector: (uri, headers) async => FakeTerminal()), fixedDark: true, before: selectInTerminal);
  shots('terminal_reconnecting', () => TerminalScreen(session: buildBox, connector: (uri, headers) async => throw const SocketException('unreachable')), fixedDark: true);
  shots('terminal_closed', () => TerminalScreen(session: buildBox, connector: (uri, headers) async => throw const TerminalHandshakeException(404)), fixedDark: true);

  // More > Terminal: the running terminals, and none.
  shots('terminal_sessions', () => const TerminalSessionsScreen(), extra: {Size2.small, Size2.text2x});
  shots('terminal_sessions_empty', () => const TerminalSessionsScreen(), overrides: {
    'GET /v1/sys/terminal-sessions': _noSessions,
    'GET /v1/container/terminal-sessions': _noSessions,
  });
}
