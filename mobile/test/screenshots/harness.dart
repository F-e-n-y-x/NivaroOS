// Screenshot harness: renders real screens against a fake NivaroOS server
// so the UI can be reviewed (and regressions caught) without a phone.
//
//   flutter test test/screenshots --update-goldens   # write PNGs
//   flutter test test/screenshots                    # compare
//
// PNGs land in test/screenshots/goldens/. Roboto and the Material icon font
// come from the Flutter SDK running the tests, Roboto Mono (for the
// terminal and logs) from test/screenshots/fonts/, so text renders as on a
// device instead of as the test font's boxes.
//
// The fake server answers from test/screenshots/fixtures/, laid out by
// URL: GET /v1/sys/utilization reads fixtures/v1/sys/utilization.json,
// the VM sidecar (port 28641) reads fixtures/vm/<path>.json, a .txt
// fixture is served as a plain body and a .png one as an image. See fixtures/README.md.
//
// Every shot runs at the same frozen time ([shotTime], through
// package:clock), so "12 min ago" and friends render identically on every
// run and goldens are compared pixel for pixel, with no tolerance.
import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:multicast_dns/multicast_dns.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/device_sync_service.dart';
import 'package:nivaroos_mobile/services/discovery_service.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/ui/theme/app_theme.dart';
import 'package:nivaroos_mobile/ui/theme/appearance.dart';
import 'package:nivaroos_mobile/ui/theme/scaled_icons.dart';
import 'package:nivaroos_mobile/utils/app_icons.dart';

const fakeServer = 'http://nivaro.test';

/// The VM sidecar's port (VmClient talks to it directly, not via /v1).
const vmSidecarPort = 28641;

/// The time every shot is taken at (`clock.now()` inside [shoot]).
final shotTime = DateTime(2026, 9, 25, 14, 3);

/// Phone sizes (logical pixels) the screens are rendered at.
const phone = Size(412, 915);
const smallPhone = Size(360, 740);

/// A tablet in landscape, for the NavigationRail layout.
const tablet = Size(1024, 768);

const _fixtures = 'test/screenshots/fixtures';

bool _fontsLoaded = false;

/// The Flutter SDK running this test: FLUTTER_ROOT when the tool sets it,
/// otherwise found from the test runner binary, which lives in
/// `<sdk>/bin/cache/artifacts/engine/<platform>/`.
Directory _flutterRoot() {
  final env = Platform.environment['FLUTTER_ROOT'];
  if (env != null && env.isNotEmpty) return Directory(env);
  var dir = File(Platform.resolvedExecutable).parent;
  while (dir.path != dir.parent.path) {
    if (Directory('${dir.path}/bin/cache/artifacts/material_fonts').existsSync()) return dir;
    dir = dir.parent;
  }
  throw StateError('Cannot find the Flutter SDK from ${Platform.resolvedExecutable}; set FLUTTER_ROOT.');
}

Future<void> _loadFamily(String family, List<File> files) async {
  final loader = FontLoader(family);
  for (final f in files) {
    loader.addFont(Future.value(ByteData.view(f.readAsBytesSync().buffer)));
  }
  await loader.load();
}

Future<void> loadRealFonts() async {
  if (_fontsLoaded) return;
  final dir = Directory('${_flutterRoot().path}/bin/cache/artifacts/material_fonts');
  final roboto = dir
      .listSync()
      .whereType<File>()
      .where((f) => RegExp(r'/Roboto-[A-Za-z]+\.ttf$').hasMatch(f.path))
      .toList();
  if (roboto.isEmpty) throw StateError('No Roboto fonts in ${dir.path}');
  await _loadFamily('Roboto', roboto);
  await _loadFamily('MaterialIcons', [File('${dir.path}/MaterialIcons-Regular.otf')]);
  // The design directions' typefaces, bundled in assets/fonts/ and
  // declared in pubspec.yaml (tests don't load pubspec fonts by themselves).
  final bundled = <String, List<File>>{};
  for (final f in Directory('assets/fonts').listSync().whereType<File>().where((f) => f.path.endsWith('.ttf'))) {
    final family = f.uri.pathSegments.last.split('-').first;
    (bundled[family] ??= []).add(f);
  }
  for (final e in bundled.entries) {
    await _loadFamily(e.key, e.value);
  }
  final mono = [File('test/screenshots/fonts/RobotoMono-Regular.ttf'), File('test/screenshots/fonts/RobotoMono-Bold.ttf')];
  // The app asks for 'monospace'; xterm asks for its own list first.
  for (final family in ['monospace', 'RobotoMono', 'Roboto Mono']) {
    await _loadFamily(family, mono);
  }
  _fontsLoaded = true;
}

/// Fixture JSON by URL path, e.g. 'v1/sys/utilization'.
Map<String, dynamic> fixture(String path) {
  final f = File('$_fixtures/$path.json');
  return jsonDecode(f.readAsStringSync()) as Map<String, dynamic>;
}

/// A canned response for [FakeServer.overrides].
class FakeResponse {
  const FakeResponse(this.body, {this.status = 200});

  /// JSON-encodable data, or a String sent as-is.
  final Object? body;
  final int status;
}

/// The fake NivaroOS server. Every request is answered from the fixture
/// tree unless [overrides] has an entry for it, keyed "GET /v1/sys/logs"
/// (method, space, path; for the VM sidecar the path starts with /vm). A
/// key with the query as well ("GET /v1/folder?path=/DATA/Documents/Work",
/// decoded) wins over the one without, for a screen that lists two folders.
/// A request with no fixture gets a 404 and is recorded in [misses], so a
/// screen that calls something new shows up in the test output.
class FakeServer {
  FakeServer({this.overrides = const {}});

  final Map<String, Object> overrides;
  final List<String> misses = [];
  final List<String> requests = [];

  MockClient get client => MockClient(_handle);

  Future<http.Response> _handle(http.Request req) async {
    // App icons and other pictures come from the internet (a CDN, the app
    // store's icon URLs). Serve a neutral placeholder so they render as
    // "no picture yet" instead of failing.
    if (req.url.host != Uri.parse(fakeServer).host) {
      requests.add('${req.method} ${req.url}');
      return req.url.path.toLowerCase().endsWith('.svg')
          ? http.Response(_placeholderSvg, 200, headers: {'content-type': 'image/svg+xml'})
          : http.Response.bytes(_transparentPng, 200, headers: {'content-type': 'image/png'});
    }
    final path = req.url.port == vmSidecarPort ? '/vm${req.url.path}' : req.url.path;
    final key = '${req.method} $path';
    requests.add(key);
    final override = (req.url.hasQuery ? overrides['$key?${Uri.decodeQueryComponent(req.url.query)}'] : null) ?? overrides[key];
    if (override != null) {
      final r = override is FakeResponse ? override : FakeResponse(override);
      return _json(r.body, r.status);
    }
    if (req.method == 'GET') {
      final json = File('$_fixtures$path.json');
      if (json.existsSync()) {
        return http.Response.bytes(json.readAsBytesSync(), 200, headers: {'content-type': 'application/json'});
      }
      final text = File('$_fixtures$path.txt');
      if (text.existsSync()) return http.Response(text.readAsStringSync(), 200);
      final png = File('$_fixtures$path.png');
      if (png.existsSync()) return http.Response.bytes(png.readAsBytesSync(), 200, headers: {'content-type': 'image/png'});
    }
    misses.add(key);
    return _json({'success': 404, 'message': 'not found'}, 404);
  }

  static const _placeholderSvg =
      '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48"><rect width="48" height="48" rx="12" fill="#9e9e9e" fill-opacity="0.35"/></svg>';

  // A 1x1 transparent PNG.
  static final _transparentPng = base64Decode(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=');

  static http.Response _json(Object? body, int status) => http.Response(
        body is String ? body : jsonEncode(body),
        status,
        headers: {'content-type': 'application/json'},
      );
}

/// Stubs every plugin channel the app touches so screens run in tests:
/// battery, device and package info, permissions (granted), the background
/// service (stopped), path_provider (a temp dir), url_launcher, and the
/// app's own com.fenyx.nivaroos channels - plus mDNS and this phone's
/// storage reading, so nothing depends on the machine running the tests.
void stubPlatformChannels() {
  // mDNS discovery: a network with multicast blocked, so the discovery
  // screen always shows its "nothing found" state.
  DiscoveryService.clientFactory = () => MDnsClient(
        rawDatagramSocketFactory: (host, int port, {bool reuseAddress = true, bool reusePort = true, int ttl = 1}) async =>
            throw const SocketException('multicast is off in tests'),
      );
  // This phone's storage: a fixed 422 GB with 252 GB used, not the disk of
  // the machine running the tests.
  DeviceSyncService.storageMetricsOverride = const {'total': 422 << 30, 'used': 252 << 30, 'free': 170 << 30};
  final messenger = TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  final tmp = Directory.systemTemp.path;

  void stub(String name, Object? Function(MethodCall call) answer) {
    messenger.setMockMethodCallHandler(MethodChannel(name), (call) async => answer(call));
  }

  stub('plugins.flutter.io/path_provider', (call) => tmp);
  stub('dev.fluttercommunity.plus/battery', (call) => switch (call.method) {
        'getBatteryLevel' => 76,
        'isInBatterySaveMode' => false,
        _ => null,
      });
  stub('dev.fluttercommunity.plus/package_info', (call) => {
        'appName': 'NivaroOS',
        'packageName': 'com.fenyx.nivaroos_mobile',
        'version': '1.2.1',
        'buildNumber': '6',
      });
  stub('dev.fluttercommunity.plus/device_info', (call) => null);
  stub('flutter.baseflow.com/permissions/methods', (call) => switch (call.method) {
        'checkPermissionStatus' => 1,
        'requestPermissions' => {for (final p in (call.arguments as List)) p: 1},
        'shouldShowRequestPermissionRationale' => false,
        _ => null,
      });
  stub('id.flutter/background_service', (call) => call.method == 'isServiceRunning' ? false : null);
  stub('id.flutter/background_service_android', (call) => null);
  stub('plugins.flutter.io/url_launcher', (call) => false);
  stub('com.fenyx.nivaroos/device_info', (call) => switch (call.method) {
        'getBatteryLevel' => 76,
        'getHardwareId' => 'test-hardware-id',
        _ => null,
      });
  stub('com.fenyx.nivaroos/background_service', (call) => switch (call.method) {
        'isIgnoringBatteryOptimizations' => true,
        'isAutoStartOnBoot' => true,
        'isSamsungDevice' => false,
        _ => true,
      });
}

/// Signs the app in to the fake server.
Future<void> signIn() async {
  stubPlatformChannels();
  FlutterSecureStorage.setMockInitialValues({
    'server_url': fakeServer,
    'access_token': 'test-token',
    'refresh_token': 'test-refresh',
    'username': 'alex',
  });
  await StorageService.instance.init();
  // The storage cache outlives the mock above: no Files tabs from the
  // test before.
  await StorageService.instance.setFilesTabs(null);
  ApiClient.instance.setBaseUrl(fakeServer);
  ApiClient.instance.setSession('test-token', 'test-refresh');
}

/// The golden file name for a shot: `name_light|dark_WxH[_text2x].png`.
/// [themeName] replaces the brightness for screens that draw their own
/// fixed theme (the always-dark consoles: `fixed_dark`).
String goldenName(String name, Brightness brightness, Size size, double textScale, {String? themeName}) {
  final scale = textScale == 1 ? '' : '_text${textScale.toStringAsFixed(textScale % 1 == 0 ? 0 : 1)}x';
  return '${name}_${themeName ?? brightness.name}_${size.width.toInt()}x${size.height.toInt()}$scale';
}

/// Bundled stand-ins for app icons from the internet, one per app (picked
/// by the URL), so headers and lists show a real picture.
final List<Uint8List> _appIcons = [
  for (var i = 0; i < 8; i++) File('$_fixtures/icons/app_$i.png').readAsBytesSync(),
];

Widget _fakeNetworkIcon(String url, double size) {
  final i = url.codeUnits.fold<int>(0, (h, c) => (h * 31 + c) & 0x7fffffff) % _appIcons.length;
  return Image.memory(_appIcons[i], width: size, height: size, fit: BoxFit.cover, gaplessPlayback: true);
}

/// Pushes [child] over a blank first route once, so it renders the way it
/// does in the app - as a pushed screen, with a back arrow.
class _PushedHost extends StatefulWidget {
  const _PushedHost({required this.child});
  final Widget child;

  @override
  State<_PushedHost> createState() => _PushedHostState();
}

class _PushedHostState extends State<_PushedHost> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) Navigator.of(context).push(PageRouteBuilder<void>(pageBuilder: (_, _, _) => widget.child));
    });
  }

  @override
  Widget build(BuildContext context) => const Scaffold();
}

/// Wraps [child] the way `NivaroApp` does: both themes, system bar
/// styling, icons scaled with the text, and an optional text scale. With
/// [pushed], [child] is pushed over a blank first route, as a detail
/// screen is in the app. [appearance] picks the direction, accent and
/// true black (with [brightness] dark); the default is the app's default.
Widget testApp(Widget child, {Brightness brightness = Brightness.light, double textScale = 1, bool pushed = false, Appearance appearance = const Appearance()}) {
  return MaterialApp(
    debugShowCheckedModeBanner: false,
    theme: AppTheme.forAppearance(appearance, Brightness.light),
    darkTheme: AppTheme.forAppearance(appearance, Brightness.dark),
    themeMode: brightness == Brightness.dark ? ThemeMode.dark : ThemeMode.light,
    builder: (context, app) => AnnotatedRegion<SystemUiOverlayStyle>(
      value: AppTheme.systemBarsStyle(Theme.of(context).brightness),
      child: MediaQuery(
        data: MediaQuery.of(context).copyWith(textScaler: TextScaler.linear(textScale)),
        child: ScaledIcons(child: app!),
      ),
    ),
    home: pushed ? _PushedHost(child: child) : child,
  );
}

/// Pumps [screen] at [size] in [brightness], serves all HTTP from a
/// [FakeServer], lets it load, and compares with `goldens/<dir>/<name>.png`.
///
/// With [tab], the screen is one of HomeShell's tabs, which draw on the
/// shell's Scaffold rather than their own; it is rendered on a Scaffold the
/// same way, so it shows on the page colour instead of a bare canvas.
///
/// Periodic refresh timers are left running while the screen loads
/// (pumpAndSettle would never return with them) and cancelled when the
/// tree is torn down at the end.
///
/// Pictures loaded with dart:io (Image.network) always fail in tests; those
/// errors are ignored so the shot shows the screen's fallback. With
/// [strict] false, known problems of the pre-v2 screens - layout
/// overflows, ListTiles on coloured boxes, and uncaught async errors (the
/// consoles' WebSockets cannot connect here) - are printed as findings
/// instead of failing the test. A migrated screen's shots run strict.
Future<void> shoot(
  WidgetTester tester, {
  required String name,
  required Widget screen,
  String dir = 'screens',
  Brightness brightness = Brightness.light,
  Size size = phone,
  double textScale = 1,
  Map<String, Object> overrides = const {},
  Duration settle = const Duration(seconds: 2),
  Future<void> Function(WidgetTester tester)? before,
  bool strict = true,
  bool tab = false,
  bool pushed = false,
  String? themeName,
  Appearance appearance = const Appearance(),
}) async {
  await loadRealFonts();
  final findings = <String>[];
  final testHandler = FlutterError.onError;
  FlutterError.onError = (details) {
    if (details.library == 'image resource service') return;
    final text = details.exceptionAsString();
    if (!strict && (text.contains('overflowed') || text.contains('ListTile background color or ink splashes'))) {
      findings.add(text.split('\n').first);
      return;
    }
    testHandler?.call(details);
  };
  final uncaught = <Object>[];
  addTearDown(() => FlutterError.onError = testHandler);
  stubPlatformChannels();
  NivaroAppIcon.debugNetworkIcon = _fakeNetworkIcon;
  // 2x: sharp enough to review, and half the bytes of 3x in git.
  tester.view.physicalSize = size * 2;
  tester.view.devicePixelRatio = 2;
  // A phone's status bar and gesture bar, so edge-to-edge insets show.
  tester.view.padding = const FakeViewPadding(top: 24 * 3, bottom: 24 * 3);
  tester.view.viewPadding = const FakeViewPadding(top: 24 * 3, bottom: 24 * 3);
  addTearDown(tester.view.reset);

  final server = FakeServer(overrides: overrides);
  final done = Completer<void>();
  final page = tab ? Scaffold(body: screen) : screen;
  runZonedGuarded(() => withClock(Clock.fixed(shotTime), () => http.runWithClient(() async {
    await tester.pumpWidget(testApp(page, brightness: brightness, textScale: textScale, pushed: pushed, appearance: appearance));
    // Real file IO (fixtures, temp files) only completes outside the fake
    // clock, so alternate a little real time with fake frames.
    for (var i = 0; i < 10; i++) {
      await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 20)));
      await tester.pump(settle ~/ 10);
    }
    if (before != null) await before(tester);
    await expectLater(
      find.byType(MaterialApp),
      matchesGoldenFile('goldens/$dir/${goldenName(name, brightness, size, textScale, themeName: themeName)}.png'),
    );
    // Dispose the tree so periodic timers are cancelled, then run out any
    // one-shot timers the screen left behind.
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => server.client)).then(done.complete, onError: done.completeError), (error, stack) => uncaught.add(error));
  await done.future;

  FlutterError.onError = testHandler;
  if (uncaught.isNotEmpty) {
    if (strict) throw TestFailure('Uncaught errors in $name: ${uncaught.join('; ')}');
    findings.addAll(uncaught.map((e) => 'uncaught ${e.runtimeType}: ${e.toString().split('\n').first}'));
  }
  if (findings.isNotEmpty) {
    debugPrint('[screenshots] $name: ${findings.toSet().join('; ')}');
  }
  if (server.misses.isNotEmpty) {
    debugPrint('[screenshots] $name: no fixture for ${server.misses.toSet().join(', ')}');
  }
}
