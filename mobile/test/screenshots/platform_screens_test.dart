// Screenshots of the onboarding, account and platform screens (discovery,
// sign-in, servers, settings, companion devices, Tailscale, More), in
// light and dark at 412x915, 360x740 and 200% text, plus their error and
// in-between states. See harness.dart, and brief §7-§8.
import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/server_profile.dart';
import 'package:nivaroos_mobile/screens/companion_devices_screen.dart';
import 'package:nivaroos_mobile/screens/discovery_screen.dart';
import 'package:nivaroos_mobile/screens/login_screen.dart';
import 'package:nivaroos_mobile/screens/more_screen.dart';
import 'package:nivaroos_mobile/screens/server_profiles_screen.dart';
import 'package:nivaroos_mobile/screens/settings_screen.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/app_update_service.dart';
import 'package:nivaroos_mobile/services/background_service.dart';
import 'package:nivaroos_mobile/services/device_sync_service.dart';
import 'package:nivaroos_mobile/services/discovery_service.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/widgets/tailscale_modal.dart';

import 'harness.dart';

/// Saved state for the shots: signed in to the fake server as alex, with a
/// second saved server, a recent update check, and this phone named.
Map<String, String> _storage({bool updateAvailable = false}) => {
      'server_url': fakeServer,
      'access_token': 'test-token',
      'refresh_token': 'test-refresh',
      'username': 'alex',
      'active_profile_id': 'srv_home',
      'companion_device_name': "Alex's Pixel",
      'saved_server_profiles': jsonEncode([
        ServerProfile(id: 'srv_home', name: 'Home', url: fakeServer, username: 'alex', accessToken: 't', refreshToken: 'r').toJson(),
        ServerProfile(id: 'srv_office', name: 'Office', url: 'https://nas.example.com', username: 'alex', accessToken: 't', refreshToken: 'r').toJson(),
        ServerProfile(id: 'srv_lab', name: '', url: 'http://192.168.1.40:8080', username: 'admin').toJson(),
      ]),
      'app_update_check': jsonEncode({
        'checked_at': shotTime.subtract(const Duration(hours: 2)).toIso8601String(),
        'latest': updateAvailable
            ? const AppRelease(
                version: '1.3.0',
                tag: 'mobile-v1.3.0',
                notes: '- Sign-in keeps working through proxy errors\n- Share storage for a set time',
                apkUrl: 'https://github.com/x/nivaroos-android.apk',
                apkSize: 25270000,
                pageUrl: 'https://github.com/x',
              ).toJson()
            : null,
      }),
    };

Future<void> _signIn({bool updateAvailable = false}) async {
  stubPlatformChannels();
  StorageService.instance.resetForTest();
  FlutterSecureStorage.setMockInitialValues(_storage(updateAvailable: updateAvailable));
  await StorageService.instance.init();
  ApiClient.instance.setBaseUrl(fakeServer);
  ApiClient.instance.setSession('test-token', 'test-refresh');
  DeviceSyncService.instance.registrationProblem.value = null;
  BackgroundService.debugIsAndroid = true;
  DeviceSyncService.deviceInfoOverride = ('Google', 'Pixel 8', 'Android 16 (SDK 36)');
}

/// The native sharing channel: sharing on until 15:03, or off.
void _stubSharing({bool running = false, bool forever = false, String? lastStopReason}) {
  final messenger = TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  messenger.setMockMethodCallHandler(const MethodChannel('com.fenyx.nivaroos/companion_share'), (call) async {
    if (call.method == 'status') {
      return {
        'running': running,
        'endsAt': running && !forever ? shotTime.add(const Duration(hours: 1)).millisecondsSinceEpoch : 0,
        'lastStopReason': lastStopReason,
      };
    }
    return true;
  });
}

/// Types into the field labelled [label], then taps [button].
Future<void> Function(WidgetTester) _submit(String label, String text, String button, {String? passwordLabel, String? password}) {
  return (tester) async {
    await tester.enterText(find.widgetWithText(TextField, label), text);
    if (passwordLabel != null) await tester.enterText(find.widgetWithText(TextField, passwordLabel), password ?? '');
    await tester.pump();
    final target = find.widgetWithText(FilledButton, button);
    await tester.ensureVisible(target);
    await tester.tap(target);
    for (var i = 0; i < 10; i++) {
      await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 20)));
      await tester.pump(const Duration(milliseconds: 100));
    }
    // Let the text field lose its cursor blink so the shot is stable.
    FocusManager.instance.primaryFocus?.unfocus();
    await tester.pump(const Duration(seconds: 1));
  };
}

class _Shot {
  const _Shot(this.build, {this.overrides = const {}, this.before, this.setup, this.dense = true, this.tab = false, this.tablet = false, this.root = false});

  /// A first screen (discovery, sign-in): the only route, no back arrow.
  /// Everything else is pushed.
  final bool root;

  final Widget Function() build;
  final Map<String, Object> overrides;
  final Future<void> Function(WidgetTester tester)? before;
  final Future<void> Function()? setup;

  /// Also at 360x740 and 200% text.
  final bool dense;
  final bool tab;
  final bool tablet;
}

final _found = [
  DiscoveredServer(name: 'atom', host: '192.168.1.20', port: 80),
  DiscoveredServer(name: 'backup-box', host: '192.168.1.31', port: 8080),
];

final Map<String, _Shot> _shots = {
  'discovery': _Shot(() => const DiscoveryScreen(), root: true),
  'discovery_found': _Shot(
    () => const DiscoveryScreen(),
    root: true,
    setup: () async => DiscoveryService.debugDiscover = () => Stream.fromIterable(_found),
  ),
  // Still scanning after the first answer: a thin progress bar under it.
  'discovery_scanning': _Shot(
    () => const DiscoveryScreen(),
    root: true,
    dense: false,
    setup: () async => DiscoveryService.debugDiscover = () {
      final c = StreamController<DiscoveredServer>()..add(_found.first);
      return c.stream;
    },
  ),
  'discovery_not_nivaro': _Shot(
    () => const DiscoveryScreen(),
    root: true,
    dense: false,
    before: _submit('Server address', 'nas.example.com', 'Continue'),
  ),
  'login': _Shot(() => const LoginScreen(), root: true),
  'login_wrong_password': _Shot(
    () => const LoginScreen(),
    root: true,
    dense: false,
    overrides: {'POST /v1/users/login': const FakeResponse({'success': 10006, 'message': 'user does not exist or password is invalid'}, status: 400)},
    before: _submit('Username', 'alex', 'Sign in', passwordLabel: 'Password', password: 'hunter2'),
  ),
  'login_unreachable': _Shot(
    () => const LoginScreen(),
    root: true,
    dense: false,
    overrides: {'POST /v1/users/login': const FakeResponse('<html><body>502 Bad Gateway</body></html>', status: 502)},
    before: _submit('Username', 'alex', 'Sign in', passwordLabel: 'Password', password: 'hunter2'),
  ),
  'login_reauth': _Shot(() => const LoginScreen(isReauth: true, initialUsername: 'alex'), dense: false),
  'login_new_server': _Shot(() => const LoginScreen(serverInitialized: false), dense: false, root: true),
  'server_profiles': _Shot(() => const ServerProfilesScreen()),
  'server_profile_form': _Shot(() => const ServerProfileForm(), dense: false),
  'settings': _Shot(() => const SettingsScreen(), tablet: true),
  // Settings > Home > Refresh widgets: the interval choices.
  'settings_refresh_sheet': _Shot(
    () => const SettingsScreen(),
    dense: false,
    before: (tester) async {
      await tester.tap(find.text('Refresh widgets'));
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
    },
  ),
  'settings_update': _Shot(() => const SettingsScreen(), dense: false, setup: () => _signIn(updateAvailable: true)),
  'companion_devices': _Shot(() => const CompanionDevicesScreen()),
  'companion_devices_loading': _Shot(
    () => const CompanionDevicesScreen(),
    dense: false,
    setup: () async => DeviceSyncService.debugFetchDevices = () => Completer<List<CompanionDevice>>().future,
  ),
  'companion_devices_sharing': _Shot(() => const CompanionDevicesScreen(), setup: () async => _stubSharing(running: true)),
  // Sharing set to Never: on, with no end time.
  'companion_devices_sharing_never': _Shot(() => const CompanionDevicesScreen(), dense: false, setup: () async => _stubSharing(running: true, forever: true)),
  // The share sheet with its time limits, Never chosen.
  'companion_share_sheet': _Shot(
    () => const CompanionDevicesScreen(),
    setup: () async => _stubSharing(),
    before: (tester) async {
      await tester.tap(find.text('Share storage with the server'));
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      await tester.tap(find.text('Never'));
      // Let the chips' check-mark animation finish.
      for (var i = 0; i < 10; i++) {
        await tester.pump(const Duration(milliseconds: 100));
      }
    },
  ),
  'companion_devices_stopped': _Shot(() => const CompanionDevicesScreen(), dense: false, setup: () async => _stubSharing(lastStopReason: 'timeout')),
  'companion_devices_offline': _Shot(
    () => const CompanionDevicesScreen(),
    dense: false,
    overrides: {'GET /v1/companion/devices': const FakeResponse('<html>502</html>', status: 502)},
  ),
  'companion_devices_error': _Shot(
    () => const CompanionDevicesScreen(),
    dense: false,
    overrides: {'GET /v1/companion/devices': const FakeResponse({'success': 500, 'message': 'companion registry is unavailable'}, status: 500)},
  ),
  'tailscale': _Shot(() => const TailscaleScreen()),
  'tailscale_needs_login': _Shot(
    () => const TailscaleScreen(),
    dense: false,
    overrides: {
      'GET /v1/tailscale/status': const FakeResponse({
        'success': 200,
        'data': {'BackendState': 'NeedsLogin', 'AuthURL': 'https://login.tailscale.com/a/abc123', 'Peer': {}},
      }),
    },
  ),
  'tailscale_not_installed': _Shot(
    () => const TailscaleScreen(),
    dense: false,
    overrides: {
      'GET /v1/tailscale/status': const FakeResponse({'success': 200, 'data': {'BackendState': 'NoDaemon'}}),
      'GET /v1/tailscale/installed': const FakeResponse({'success': 200, 'data': {'installed': false}}),
    },
  ),
  'more': _Shot(() => const MoreScreen(), tab: true, tablet: true),
};

void main() {
  setUp(() async {
    // Touch, as on a phone: no keyboard focus rings in the shots.
    FocusManager.instance.highlightStrategy = FocusHighlightStrategy.alwaysTouch;
    await _signIn();
    _stubSharing();
  });
  tearDown(() {
    BackgroundService.debugIsAndroid = null;
    DiscoveryService.debugDiscover = null;
    DeviceSyncService.debugFetchDevices = null;
  });

  for (final MapEntry(key: name, value: s) in _shots.entries) {
    // Real elevation shadows rather than flutter_test's solid outlines (the
    // FAB, menus), reset before the test ends as flutter_test requires.
    Future<void> shot(WidgetTester tester, Brightness brightness, {Size size = phone, double textScale = 1}) async {
      if (s.setup != null) await tester.runAsync(s.setup!);
      debugDisableShadows = false;
      try {
        await shoot(
          tester,
          name: name,
          dir: 'platform',
          screen: s.build(),
          brightness: brightness,
          size: size,
          textScale: textScale,
          overrides: s.overrides,
          before: s.before,
          tab: s.tab,
          pushed: !s.tab && !s.root,
        );
      } finally {
        debugDisableShadows = true;
      }
    }

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
