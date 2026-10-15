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
    expect(html, contains(r'<head><script>window.__nivaroosToken="a.b</script>c";</script><meta'));
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
      expect(load.html, contains('<head><script>window.__nivaroosToken="jwt-1";</script>'));
      expect(find.text('Connecting to Host desktop'), findsOneWidget);
      expect(find.byTooltip('Pin toolbar'), findsNothing);

      // Live: the console's own toolbar, Pin and Hide and all.
      web.last.post('{"state":"live","reason":""}');
      await tester.pump();
      expect(find.text('Connecting to Host desktop'), findsNothing);
      for (final t in ['Show keyboard', 'More options', 'Pin toolbar', 'Hide toolbar', 'Disconnect']) {
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
      expect(web.last.scripts.last, contains('nivaroosEmbed.key(97,true),nivaroosEmbed.key(97,false)'));
      await tester.pumpWidget(const SizedBox());
    });
  });
}
