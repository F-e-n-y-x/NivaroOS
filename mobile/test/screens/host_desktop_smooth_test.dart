// Host desktop Smooth (beta): when the app opens the WebRTC embed, what it
// hands the page (the token, before any script, never in the URL), and
// every way back to the native console.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/screens/host_desktop_screen.dart';
import 'package:nivaroos_mobile/screens/host_desktop_smooth.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/rfb_client.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/services/vm_client.dart';
import 'package:nivaroos_mobile/widgets/rfb_view.dart';
import 'package:webview_flutter_platform_interface/webview_flutter_platform_interface.dart' show NavigationDecision, NavigationRequest;

import '../widgets/fake_webview.dart';

const _lan = 'http://192.168.1.20';
const _index = '<!doctype html><html><head><meta charset="utf-8"><script type="module" src="/assets/index.js"></script></head><body></body></html>';

class _Vm extends VmClient {
  _Vm({this.webrtc = true, this.reason = ''});
  final bool webrtc;
  final String reason;
  int streamCalls = 0;
  @override
  Future<bool> hostDesktopInstalled() async => true;
  @override
  Future<({bool webrtc, String reason})> hostStream() async {
    streamCalls++;
    return (webrtc: webrtc, reason: reason);
  }

  @override
  Future<HostDisplay> getHostDisplay() async => throw VmException('no display');
  @override
  Future<HostDesktopStatus> hostDesktopStatus() async => throw VmException('no status');
}

class _Rfb extends RfbClient {
  _Rfb() : super(channelFactory: (uri, headers) async => throw UnimplementedError());
  int connects = 0;
  @override
  Future<void> connect() async {
    connects++;
    status.value = RfbStatus.connecting;
  }
}

void main() {
  late FakeWebViewPlatform web;
  setUp(() async {
    web = FakeWebViewPlatform.install();
    FlutterSecureStorage.setMockInitialValues({});
    await StorageService.instance.init();
    await StorageService.instance.setConsolePref('bar@host', 'shown');
    ApiClient.instance.setBaseUrl(_lan);
    ApiClient.instance.setSession('jwt-1', 'refresh-1');
  });

  test('the token goes first in <head>, escaped; a page without <head> gets it first', () {
    final html = injectNivaroosToken(_index, 'a.b</script>c');
    // A "</script>" in the value can't end the tag early.
    expect(html, contains(r'<head><script>window.__nivaroosToken="a.b\u003c/script>c";</script><meta'));
    expect(injectNivaroosToken('<head>', 't', stream: {'fps': 60, 'maxKbps': 8000}),
        '<head><script>window.__nivaroosToken="t";window.__nivaroosStream={"fps":60,"maxKbps":8000};</script>');
    expect(html.indexOf('__nivaroosToken'), lessThan(html.indexOf('index.js')));
    expect(injectNivaroosToken('<p>x</p>', 't'), startsWith('<script>window.__nivaroosToken="t";</script><p>'));
  });

  test('Smooth is tried on the home network and Tailscale, never through the internet', () {
    expect(smoothBlockedByRoute('http://192.168.1.20'), isNull);
    expect(smoothBlockedByRoute('http://10.0.0.5:8080'), isNull);
    expect(smoothBlockedByRoute('http://nivaro.local'), isNull);
    expect(smoothBlockedByRoute('http://100.101.3.4'), isNull);
    expect(smoothBlockedByRoute('https://atom.tail1234.ts.net'), isNull);
    expect(smoothBlockedByRoute('https://nas.example.com'), contains('internet'));
    expect(smoothBlockedByRoute('https://abc.trycloudflare.com'), contains('internet'));
  });

  Future<(_Vm, _Rfb)> open(WidgetTester tester, {String mode = 'smooth', bool webrtc = true, String reason = ''}) async {
    await StorageService.instance.setConsolePref(HostStreamMode.pref, mode);
    final vm = _Vm(webrtc: webrtc, reason: reason);
    final rfb = _Rfb();
    await tester.pumpWidget(MaterialApp(home: HostDesktopScreen(client: vm, rfb: rfb, history: RemoteClipboardHistory())));
    for (var i = 0; i < 5; i++) {
      await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
      await tester.pump();
    }
    return (vm, rfb);
  }

  Future<void> withServer(Future<void> Function() body) =>
      http.runWithClient(
          body,
          () => MockClient((req) async => switch (req.url.path) {
                '/' => http.Response(_index, 200),
                '/v1/users/refresh' => http.Response(jsonEncode({
                    'data': {'access_token': 'jwt-3', 'refresh_token': 'refresh-3'}
                  }), 200),
                _ => http.Response('', 404),
              }));

  testWidgets('Smooth loads the embed with the token set before page scripts, never in the URL', (tester) async {
    await withServer(() async {
      final (_, rfb) = await open(tester);
      expect(rfb.connects, 0);
      final load = web.last.loads.single;
      expect(load.baseUrl, '$_lan/#/host-desktop/embed?mode=smooth');
      expect(load.baseUrl, isNot(contains('jwt')));
      expect(load.html, contains('<head><script>window.__nivaroosToken="jwt-1";window.__nivaroosStream={"fps":30,"maxKbps":12000};</script>'));
      expect(find.text('Connecting to Host desktop'), findsOneWidget);
      expect(find.byTooltip('Pin toolbar'), findsNothing);

      // Live: the native console's toolbar, every button of it.
      web.last.post('{"state":"live","reason":""}');
      await tester.pump();
      expect(find.text('Connecting to Host desktop'), findsNothing);
      for (final t in [
        'Show keyboard',
        'Input: touch. Switch to mouse pointer',
        'Display',
        'Extra keys',
        'Clipboard',
        'More options',
        'Pin toolbar',
        'Hide toolbar',
        'Disconnect',
      ]) {
        expect(find.byTooltip(t), findsOneWidget, reason: t);
      }
      await tester.tap(find.byTooltip('Pin toolbar'));
      await tester.pump();
      expect(await StorageService.instance.getConsolePref('bar@host'), 'pinned');

      // Off-site navigation stays out of the WebView.
      final nav = web.last.delegate!.onNavigationRequest!;
      expect(await nav(const NavigationRequest(url: '$_lan/#/files', isMainFrame: true)), NavigationDecision.navigate);
      expect(await nav(const NavigationRequest(url: 'https://evil.example/', isMainFrame: true)), NavigationDecision.prevent);
      await tester.pumpWidget(const SizedBox());
    });
  });

  testWidgets('the page falling back opens the native console and says why', (tester) async {
    await withServer(() async {
      final (_, rfb) = await open(tester);
      web.last.post(jsonEncode({'state': 'fallback', 'reason': 'No video frame arrived within 4 seconds'}));
      await tester.pump();
      await tester.pump();
      expect(rfb.connects, 1);
      expect(find.textContaining('Using Compatible mode. No video frame arrived within 4 seconds'), findsOneWidget);
      // Only once, whatever the page says next.
      await tester.pumpWidget(const SizedBox());
    });
  });

  testWidgets('a page that never reports in (an older dashboard) falls back too', (tester) async {
    await withServer(() async {
      final (_, rfb) = await open(tester);
      await tester.pump(SmoothHostDesktop.startTimeout);
      await tester.pump();
      expect(rfb.connects, 1);
      expect(find.textContaining("didn't start in time"), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
    });
  });

  testWidgets('a server without WebRTC goes straight to the native console with its reason', (tester) async {
    await withServer(() async {
      final (vm, rfb) = await open(tester, mode: 'auto', webrtc: false, reason: 'No H.264 encoder found on the server');
      expect(vm.streamCalls, 1);
      expect(web.controllers, isEmpty);
      expect(rfb.connects, 1);
      expect(find.textContaining('Using Compatible mode. No H.264 encoder found on the server'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
    });
  });

  testWidgets('through a tunnel it does not even ask the server', (tester) async {
    ApiClient.instance.setBaseUrl('https://nas.example.com');
    await withServer(() async {
      final (vm, rfb) = await open(tester);
      expect(vm.streamCalls, 0);
      expect(web.controllers, isEmpty);
      expect(rfb.connects, 1);
      expect(find.textContaining('home network or Tailscale'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
    });
  });

  testWidgets('Compatible (the default) never opens the WebView', (tester) async {
    await withServer(() async {
      final (vm, rfb) = await open(tester, mode: '');
      expect(vm.streamCalls, 0);
      expect(web.controllers, isEmpty);
      expect(rfb.connects, 1);
      await tester.pumpWidget(const SizedBox());
    });
  });

  testWidgets('a refreshed token is handed to the page; a refused one reloads with a fresh one', (tester) async {
    await withServer(() async {
      await open(tester);
      final c = web.last;
      c.post('{"state":"live"}');
      await tester.pump();
      ApiClient.instance.setSession('jwt-2', 'refresh-2');
      await tester.pump(SmoothHostDesktop.tokenCheck);
      await tester.pump();
      expect(c.scripts.where((s) => s.contains('window.__nivaroosToken="jwt-2"')), hasLength(1));
      // Unchanged: nothing more.
      await tester.pump(SmoothHostDesktop.tokenCheck);
      expect(c.scripts.where((s) => s.contains('__nivaroosToken')), hasLength(1));

      // The page's offer got a 401: refresh once and load again.
      await StorageService.instance.setSession(accessToken: 'jwt-2', refreshToken: 'refresh-2', username: 'alex');
      c.post('{"state":"error","reason":"Your session expired - sign in again"}');
      for (var i = 0; i < 5; i++) {
        await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
        await tester.pump();
      }
      expect(c.loads, hasLength(2));
      expect(c.loads.last.html, contains('window.__nivaroosToken="jwt-3"'));
      await tester.pumpWidget(const SizedBox());
    });
  });

  testWidgets('in the background the page is unloaded; back in the app it loads again', (tester) async {
    await withServer(() async {
      await open(tester);
      final c = web.last;
      web.last.post('{"state":"live"}');
      await tester.pump();
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
      await tester.pump();
      expect(c.loads, hasLength(2));
      expect(c.loads.last.html, isNot(contains('__nivaroosToken')));
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      for (var i = 0; i < 3; i++) {
        await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
        await tester.pump();
      }
      expect(c.loads, hasLength(3));
      expect(c.loads.last.html, contains('__nivaroosToken'));
      expect(find.text('Connecting to Host desktop'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
    });
  });

  testWidgets('the soft keyboard types into the page', (tester) async {
    await withServer(() async {
      await open(tester);
      web.last.post('{"state":"live"}');
      await tester.pump();
      await tester.tap(find.byTooltip('Show keyboard'));
      await tester.pump();
      await tester.enterText(find.byType(TextField), '​​a');
      expect(web.last.scripts.reversed.take(2).toList().reversed, [
        'window.nivaroosEmbed&&nivaroosEmbed.key(97,true);',
        'window.nivaroosEmbed&&nivaroosEmbed.key(97,false);',
      ]);
      await tester.pumpWidget(const SizedBox());
    });
  });

  // What the app sends the page: the calls after [from].
  List<String> calls(FakeWebViewController c, int from) =>
      [for (final s in c.scripts.skip(from)) s.replaceFirst('window.nivaroosEmbed&&nivaroosEmbed.', '').replaceFirst(RegExp(r';$'), '')];

  Future<FakeWebViewController> live(WidgetTester tester) async {
    await open(tester);
    final c = web.last;
    c.post('{"t":"size","w":1920,"h":1080}');
    c.post('{"state":"live"}');
    await tester.pump();
    return c;
  }

  testWidgets("the native console's gestures drive the page; it only draws the video where the app says", (tester) async {
    await withServer(() async {
      final c = await live(tester);
      // 800 x 600 test screen, 1920 x 1080 fitted: 800 x 450 from y 75.
      expect(c.scripts, contains('window.nivaroosEmbed&&nivaroosEmbed.view({x:0.0,y:75.0,w:800.0,h:450.0});'));

      // Touch mode. Tap: click there.
      var n = c.scripts.length;
      await tester.tapAt(const Offset(400, 300));
      await tester.pump(const Duration(milliseconds: 600));
      expect(calls(c, n), ['pointer(960,540,0)', 'pointer(960,540,1)', 'pointer(960,540,0)']);

      // Hold: right-click.
      n = c.scripts.length;
      final hold = await tester.startGesture(const Offset(200, 175));
      await tester.pump(const Duration(milliseconds: 600));
      await hold.up();
      expect(calls(c, n), ['pointer(480,240,0)', 'pointer(480,240,4)', 'pointer(480,240,0)']);

      // Two fingers up: the wheel, natural scrolling.
      n = c.scripts.length;
      final a = await tester.startGesture(const Offset(380, 400), pointer: 7);
      final b = await tester.startGesture(const Offset(420, 400), pointer: 8);
      await tester.pump(const Duration(milliseconds: 300));
      for (var i = 0; i < 6; i++) {
        await a.moveBy(const Offset(0, -10));
        await b.moveBy(const Offset(0, -10));
      }
      await a.up();
      await b.up();
      expect(calls(c, n).where((s) => s.endsWith(',16)')), hasLength(greaterThanOrEqualTo(2)));
      expect(calls(c, n).where((s) => s.startsWith('pointer(')).every((s) => s.endsWith(',16)') || s.endsWith(',0)')), isTrue);

      // Pinch: the app zooms, the page moves the video.
      n = c.scripts.length;
      final p = await tester.startGesture(const Offset(350, 300), pointer: 9);
      final q = await tester.startGesture(const Offset(450, 300), pointer: 10);
      for (var i = 0; i < 5; i++) {
        await p.moveBy(const Offset(-20, 0));
        await q.moveBy(const Offset(20, 0));
        await tester.pump();
      }
      await p.up();
      await q.up();
      await tester.pump(const Duration(milliseconds: 400));
      final views = calls(c, n).where((s) => s.startsWith('view(')).toList();
      expect(views, isNotEmpty);
      final w = double.parse(RegExp(r'w:([\d.]+)').firstMatch(views.last)!.group(1)!);
      expect(w, greaterThan(1600));
      expect(calls(c, n).where((s) => s.startsWith('pointer(')), isEmpty);
      await tester.pumpWidget(const SizedBox());
    });
  });

  testWidgets('Display: frame rate and bitrate go to the page and are remembered; stats in the header', (tester) async {
    await withServer(() async {
      final c = await live(tester);
      c.post('{"t":"stats","encoder":"nvh264enc","fps":59,"rttMs":4}');
      await tester.tap(find.byTooltip('Display'));
      await tester.pumpAndSettle();
      expect(find.text('1920 × 1080 · 59 fps · 4 ms'), findsOneWidget);
      for (final t in ['Fit to screen', 'Fill screen', '1:1 pixels', 'Match this phone', 'Stay in landscape', '30 fps · lighter']) {
        expect(find.text(t), findsOneWidget, reason: t);
      }
      // No VNC picture qualities: they don't apply to a video stream.
      expect(find.text('Best picture'), findsNothing);
      await tester.tap(find.text('60 fps · smoothest'));
      await tester.pumpAndSettle();
      expect(c.scripts.last, 'window.nivaroosEmbed&&nivaroosEmbed.setStream({fps:60,maxKbps:12000});');
      await tester.tap(find.byTooltip('Display'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Max bitrate · 12 Mbps'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('40 Mbps'));
      await tester.pumpAndSettle();
      expect(c.scripts.last, 'window.nivaroosEmbed&&nivaroosEmbed.setStream({fps:60,maxKbps:40000});');
      expect(await StorageService.instance.getConsolePref(SmoothHostDesktop.fpsPref), '60');
      expect(await StorageService.instance.getConsolePref(SmoothHostDesktop.kbpsPref), '40000');

      // Next time the page starts with them.
      await tester.tap(find.byTooltip('More options'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Reconnect'));
      for (var i = 0; i < 5; i++) {
        await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
        await tester.pump();
      }
      expect(c.loads.last.html, contains('window.__nivaroosStream={"fps":60,"maxKbps":40000}'));
      await tester.pumpWidget(const SizedBox());
    });
  });

  testWidgets('clipboard both ways, extra keys with latches and Ctrl+Alt+Del', (tester) async {
    await withServer(() async {
      final c = await live(tester);
      await tester.tap(find.byTooltip('Extra keys'));
      await tester.pump();
      var n = c.scripts.length;
      await tester.tap(find.text('Ctrl'));
      await tester.pump();
      await tester.tap(find.text('Esc'));
      await tester.pump();
      expect(calls(c, n), ['key(65507,true)', 'key(65307,true)', 'key(65307,false)', 'key(65507,false)']);
      n = c.scripts.length;
      final keyRow = find.descendant(
          of: find.byWidgetPredicate((w) => w is ListView && w.scrollDirection == Axis.horizontal), matching: find.byType(Scrollable));
      await tester.scrollUntilVisible(find.text('Ctrl+Alt+Del'), 150, scrollable: keyRow);
      await tester.pumpAndSettle();
      await tester.tap(find.text('Ctrl+Alt+Del'));
      await tester.pump();
      expect(calls(c, n), [
        'key(65507,true)', 'key(65513,true)', 'key(65535,true)',
        'key(65535,false)', 'key(65513,false)', 'key(65507,false)',
      ]);


      // Phone -> host: the clipboard sheet, lossless.
      await tester.tap(find.byTooltip('Clipboard'));
      await tester.pumpAndSettle();
      await tester.enterText(find.widgetWithText(TextField, 'Or type the text to send'), 'héllo "x" </script>');
      await tester.pump();
      await tester.tap(find.text('Send to clipboard'));
      await tester.pumpAndSettle();
      expect(c.scripts.last, r'window.nivaroosEmbed&&nivaroosEmbed.clip("héllo \"x\" \u003c/script>");');
      expect(find.text('Sent to the clipboard on the server'), findsOneWidget);

      await tester.pump(const Duration(seconds: 10));
      await tester.pumpAndSettle();

      // Host -> phone: the console's own "copied" note.
      c.post(jsonEncode({'t': 'clip', 'text': 'from host'}));
      await tester.pump();
      await tester.pump();
      expect(find.text('The server copied text'), findsOneWidget);
      expect(find.text('Copy here'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
    });
  });
}
