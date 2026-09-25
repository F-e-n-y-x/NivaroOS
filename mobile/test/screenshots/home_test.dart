// Screenshots of the Home area: Home (dashboard) with its states, the
// processor/memory/storage/network detail screens, Updates and Logs.
// Same harness and rules as screens_test.dart (brief §7-§8); every shot
// runs strict.
//
//   flutter test test/screenshots/home_test.dart --update-goldens
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/dashboard_stats.dart';
import 'package:nivaroos_mobile/screens/dashboard_screen.dart';
import 'package:nivaroos_mobile/screens/system_logs_screen.dart';
import 'package:nivaroos_mobile/screens/system_updates_screen.dart';
import 'package:nivaroos_mobile/services/app_update_service.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/widgets/monitor_modals.dart';

import 'harness.dart';

/// Home after a couple of minutes on screen: two minutes of readings
/// behind the sparklines (the history is kept in memory from the 4 s
/// polls, so a fresh Home starts with none).
class _HistoryController extends HomeController {
  @override
  Future<void> refreshAll() async {
    await super.refreshAll();
    final last = live.value;
    if (last == null) return;
    // A second reading's traffic rate, as the next poll would give.
    live.value = LiveStats(stats: last.stats, rate: const NetRate(upBytesPerSec: 48300, downBytesPerSec: 2415000), updatedAt: last.updatedAt);
    history.cpu
      ..clear()
      ..addAll([4, 6, 5, 9, 14, 11, 7, 6, 8, 12, 18, 22, 16, 10, 8, 7, 9, 6, 5, 4, 6, 8, 5, 3, 2, 3, 2, 1, 2, 1]);
    history.memory
      ..clear()
      ..addAll([for (var i = 0; i < 30; i++) 40 + (i % 7) * 0.4 + (i > 20 ? 1.2 : 0)]);
    history.netDown
      ..clear()
      ..addAll([for (var i = 0; i < 30; i++) (i % 9 == 4 ? 3.9e6 : 1.2e6 + (i % 5) * 3e5)]);
    notifyListeners();
  }
}

/// Home that never finishes its first load: the skeleton.
class _LoadingController extends HomeController {
  @override
  Future<void> refreshAll() => Completer<void>().future;
  @override
  Future<void> refreshLive({bool forceDisks = false}) => Completer<void>().future;
}

/// Home with nothing loaded and the server unreachable.
class _OfflineController extends HomeController {
  @override
  Future<void> refreshAll() async {
    liveError = ApiException('Could not reach the server. Check your connection and the server address.');
    notifyListeners();
  }

  @override
  Future<void> refreshLive({bool forceDisks = false}) async {}
}

/// Home showing its last reading while the server is unreachable.
class _StaleController extends HomeController {
  @override
  Future<void> refreshAll() async {
    await super.refreshAll();
    live.value = live.value?.copyWith(stale: true);
    notifyListeners();
  }

  @override
  Future<void> refreshLive({bool forceDisks = false}) async {
    if (live.value == null) return super.refreshLive(forceDisks: forceDisks);
  }
}

LiveStats _live() {
  final util = fixture('v1/sys/utilization')['data'] as Map<String, dynamic>;
  final disks = (fixture('v1/sys/disks-usage')['data'] as List).map((e) => DiskUsage.fromJson(e as Map<String, dynamic>)).toList();
  return LiveStats(
    stats: DashboardStats.fromUtilization(util).withDisks(disks),
    rate: const NetRate(upBytesPerSec: 48300, downBytesPerSec: 2415000),
    updatedAt: shotTime,
  );
}

/// The fixture's package check, with the last check two hours before the shot.
Map<String, dynamic> _packages({int? count}) {
  final f = fixture('v1/sys/packages/check');
  final data = Map<String, dynamic>.from(f['data'] as Map);
  data['last_checked'] = shotTime.subtract(const Duration(hours: 2)).toIso8601String();
  if (count == 0) {
    data['count'] = 0;
    data['security_count'] = 0;
    data['packages'] = [];
  }
  return {...f, 'data': data};
}

Map<String, Object> get _speedtest => {
      'GET /v1/sys/speedtest/status': {
        'success': 200,
        'message': 'ok',
        'data': {
          'running': false,
          'phase': 'done',
          'live_mbps': 0,
          'result': {
            'ping_ms': 4.2,
            'jitter_ms': 0.8,
            'download_mbps': 289,
            'upload_mbps': 41.6,
            'server': 'Example ISP (Frankfurt)',
            'provider': 'speedtest.net',
            'timestamp': shotTime.subtract(const Duration(minutes: 30)).millisecondsSinceEpoch ~/ 1000,
          },
        },
      },
    };

const _releasesUrl = '$fakeServer/test/github/releases';

Map<String, Object> _releases({bool newer = true}) => {
      'GET /test/github/releases': [
        {
          'tag_name': newer ? 'mobile-v1.3.0' : 'mobile-v1.2.1',
          'name': 'NivaroOS for Android',
          'draft': false,
          'prerelease': false,
          'html_url': 'https://github.com/F-e-n-y-x/NivaroOS/releases/tag/mobile-v1.3.0',
          'body': '- Home shows what needs attention\n- Server and phone speed tests are labelled\n- Logs are searchable',
          'assets': [
            {'name': 'nivaroos-android.apk', 'size': 25270000, 'browser_download_url': '$fakeServer/test/nivaroos-android.apk'},
          ],
        },
      ],
    };

/// A shot with its variants.
class _Shot {
  const _Shot(this.build, {this.overrides = const {}, this.dense = false, this.tablet = false, this.tab = false, this.before});
  final Widget Function() build;
  final Map<String, Object> overrides;
  final Future<void> Function(WidgetTester tester)? before;

  /// Also at 360x740 and at 200% text.
  final bool dense;
  final bool tablet;

  /// One of HomeShell's tabs (Home), drawn on the shell's Scaffold.
  final bool tab;
}

final _allClear = <String, Object>{
  'GET /v1/sys/packages/check': _packages(count: 0),
  'GET /v1/backup/jobs': {'success': 200, 'message': 'ok', 'data': []},
  'GET /v2/app_management/web/appgrid': {
    'success': 200,
    'message': 'ok',
    'data': [
      {'name': 'jellyfin', 'title': {'en_us': 'Jellyfin'}, 'status': 'running'},
      {'name': 'nextcloud', 'title': {'en_us': 'Nextcloud'}, 'status': 'running'},
    ],
  },
};

final Map<String, _Shot> _shots = {
  'home': _Shot(() => DashboardScreen(controller: _HistoryController()), tab: true, dense: true, tablet: true),
  'home_all_clear': _Shot(() => DashboardScreen(controller: _HistoryController()), tab: true, overrides: _allClear),
  'home_loading': _Shot(() => DashboardScreen(controller: _LoadingController()), tab: true),
  'home_offline': _Shot(() => DashboardScreen(controller: _OfflineController()), tab: true),
  'home_stale': _Shot(() => DashboardScreen(controller: _StaleController()), tab: true),
  'home_error': _Shot(
    () => const DashboardScreen(),
    tab: true,
    overrides: {'GET /v1/sys/utilization': const FakeResponse({'success': 500, 'message': 'The system monitor is not running.'}, status: 500)},
  ),
  'home_cpu': _Shot(() => CpuDetailScreen(live: ValueNotifier(_live()), onRetry: () {}), dense: true),
  'home_memory': _Shot(() => MemoryDetailScreen(live: ValueNotifier(_live()), onRetry: () {}), dense: true),
  'home_storage': _Shot(() => StorageDetailScreen(live: ValueNotifier(_live()), onRetry: () {}, onOpenFiles: () {}), dense: true),
  'home_network': _Shot(() => NetworkDetailScreen(live: ValueNotifier(_live()), onRetry: () {}), overrides: _speedtest, dense: true),
  'updates': _Shot(() => const SystemUpdatesScreen(), overrides: {'GET /v1/sys/packages/check': _packages(), ..._releases()}, dense: true),
  // The server's Debian packages: their own page, apart from NivaroOS (plan §7.5).
  'updates_system_packages': _Shot(() => const SystemUpdatesScreen(page: UpdatesPage.packages), overrides: {'GET /v1/sys/packages/check': _packages(), ..._releases()}, dense: true),
  'updates_system_packages_up_to_date': _Shot(
    () => const SystemUpdatesScreen(page: UpdatesPage.packages),
    overrides: {'GET /v1/sys/packages/check': _packages(count: 0), ..._releases()},
  ),
  // The app's own update: the one sheet (download, checksum and signing
  // checks, install) that Settings also leads to.
  'updates_app_sheet': _Shot(
    () => const SystemUpdatesScreen(),
    overrides: {'GET /v1/sys/packages/check': _packages(), ..._releases()},
    before: (tester) async {
      final button = find.text('Update to 1.3.0');
      await tester.scrollUntilVisible(button, 300, scrollable: find.byType(Scrollable).first);
      await tester.tap(button);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
    },
  ),
  'updates_up_to_date': _Shot(
    () => const SystemUpdatesScreen(),
    overrides: {'GET /v1/sys/packages/check': _packages(count: 0), ..._releases(newer: false)},
  ),
  'updates_offline': _Shot(
    () => const SystemUpdatesScreen(),
    overrides: {
      'GET /v1/sys/version/check': const FakeResponse({'success': 500, 'message': 'Version service is not answering.'}, status: 500),
      'GET /v1/sys/packages/check': const FakeResponse({'success': 500, 'message': 'apt is busy.'}, status: 500),
    },
  ),
  'updates_packages': _Shot(
    () => PackagesScreen(
      packages: ((fixture('v1/sys/packages/check')['data'] as Map)['packages'] as List)
          .map((e) => PackageUpdate.fromJson(Map<String, dynamic>.from(e as Map)))
          .toList(),
    ),
    dense: true,
  ),
  'logs': _Shot(() => const SystemLogsScreen(), dense: true),
  'logs_error': _Shot(
    () => const SystemLogsScreen(),
    overrides: {'GET /v1/sys/logs': const FakeResponse({'success': 500, 'message': 'The log file could not be read.'}, status: 500)},
  ),
};

void main() {
  setUp(() async {
    await signIn();
    AppUpdateService.releasesApi = _releasesUrl;
  });

  for (final MapEntry(key: name, value: s) in _shots.entries) {
    Future<void> shot(WidgetTester tester, Brightness b, {Size size = phone, double textScale = 1}) => shoot(
          tester,
          name: name,
          dir: 'home',
          screen: s.build(),
          brightness: b,
          size: size,
          textScale: textScale,
          overrides: s.overrides,
          before: s.before,
          tab: s.tab,
          // Everything but Home itself is pushed, with a back arrow.
          pushed: !s.tab,
        );

    for (final b in Brightness.values) {
      testWidgets('$name ${b.name}', (tester) => shot(tester, b));
      if (s.dense) {
        testWidgets('$name small ${b.name}', (tester) => shot(tester, b, size: smallPhone));
        testWidgets('$name 200% text ${b.name}', (tester) => shot(tester, b, textScale: 2));
      }
    }
    if (s.tablet) testWidgets('$name tablet', (tester) => shot(tester, Brightness.light, size: tablet));
  }
}
