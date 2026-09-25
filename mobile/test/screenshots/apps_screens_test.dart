// Screenshots of the Apps area: the Apps tab and its states, an app's
// page, logs, the app store, a store app's page, custom install and the
// terminal. Same harness as screens_test.dart; PNGs in goldens/apps/.
import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/container_entry.dart';
import 'package:nivaroos_mobile/screens/app_store_screen.dart';
import 'package:nivaroos_mobile/screens/apps_screen.dart';
import 'package:nivaroos_mobile/screens/container_logs_screen.dart';
import 'package:nivaroos_mobile/screens/custom_install_screen.dart';
import 'package:nivaroos_mobile/screens/terminal_screen.dart';

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

/// A terminal session that prints a prompt and some output, with a
/// multi-byte character split across two frames (plan M-07).
class FakeTerminal implements TerminalTransport {
  final _frames = StreamController<dynamic>();
  final sent = <Object>[];

  FakeTerminal() {
    final line = utf8.encode('\x1b[1;32malex@atom\x1b[0m:\x1b[1;34m~\x1b[0m\$ docker ps --format "{{.Names}}\\t{{.Status}}"\r\n');
    final out = utf8.encode('jellyfin        Up 10 hours\r\nnextcloud       Up 10 hours\r\nhomeassistant   Up 3 days\r\n'
        'Grüße aus Köln, café crème\r\n\x1b[1;32malex@atom\x1b[0m:\x1b[1;34m~\x1b[0m\$ ');
    _frames.add(line);
    // Split inside the 'ü' (C3 BC): the screen must not show U+FFFD.
    final cut = out.indexOf(0xBC);
    _frames.add(out.sublist(0, cut));
    _frames.add(out.sublist(cut));
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

enum Size2 { phone, small, text2x }

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

  // The terminal (dark in both themes).
  shots('terminal', () => TerminalScreen(connector: (uri, headers) async => FakeTerminal()), extra: {Size2.small}, fixedDark: true);
  shots('terminal_closed', () => TerminalScreen(connector: (uri, headers) async => throw Exception('refused')), fixedDark: true);
}
