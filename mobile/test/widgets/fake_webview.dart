// A WebView platform for widget tests: records what the app loads and
// runs, lets the test post NivaroosBridge messages, and draws a stand-in
// "remote screen" where the platform view would be.
import 'package:flutter/material.dart';
import 'package:webview_flutter_platform_interface/webview_flutter_platform_interface.dart';

class FakeWebViewPlatform extends WebViewPlatform {
  final controllers = <FakeWebViewController>[];

  /// The one the screen made last.
  FakeWebViewController get last => controllers.last;

  static FakeWebViewPlatform install() {
    final p = FakeWebViewPlatform();
    WebViewPlatform.instance = p;
    return p;
  }

  @override
  PlatformWebViewController createPlatformWebViewController(PlatformWebViewControllerCreationParams params) {
    final c = FakeWebViewController(params);
    controllers.add(c);
    return c;
  }

  @override
  PlatformNavigationDelegate createPlatformNavigationDelegate(PlatformNavigationDelegateCreationParams params) =>
      FakeNavigationDelegate(params);

  @override
  PlatformWebViewWidget createPlatformWebViewWidget(PlatformWebViewWidgetCreationParams params) =>
      _FakeWebViewWidget(params);
}

class FakeWebViewController extends PlatformWebViewController {
  FakeWebViewController(super.params) : super.implementation();

  final loads = <({String html, String? baseUrl})>[];
  final scripts = <String>[];
  final channels = <String, void Function(JavaScriptMessage)>{};
  FakeNavigationDelegate? delegate;

  /// The page posts [json] on NivaroosBridge.
  void post(String json) => channels['NivaroosBridge']!(JavaScriptMessage(message: json));

  @override
  Future<void> loadHtmlString(String html, {String? baseUrl}) async => loads.add((html: html, baseUrl: baseUrl));

  @override
  Future<void> runJavaScript(String javaScript) async => scripts.add(javaScript);

  @override
  Future<void> addJavaScriptChannel(JavaScriptChannelParams params) async =>
      channels[params.name] = params.onMessageReceived;

  @override
  Future<void> setPlatformNavigationDelegate(PlatformNavigationDelegate handler) async =>
      delegate = handler as FakeNavigationDelegate;

  @override
  Future<void> setJavaScriptMode(JavaScriptMode javaScriptMode) async {}

  @override
  Future<void> setBackgroundColor(Color color) async {}
}

class FakeNavigationDelegate extends PlatformNavigationDelegate {
  FakeNavigationDelegate(super.params) : super.implementation();

  NavigationRequestCallback? onNavigationRequest;
  WebResourceErrorCallback? onWebResourceError;
  SslAuthErrorCallback? onSslAuthError;

  @override
  Future<void> setOnSSlAuthError(SslAuthErrorCallback onSslAuthError) async => this.onSslAuthError = onSslAuthError;

  @override
  Future<void> setOnNavigationRequest(NavigationRequestCallback onNavigationRequest) async =>
      this.onNavigationRequest = onNavigationRequest;

  @override
  Future<void> setOnWebResourceError(WebResourceErrorCallback onWebResourceError) async =>
      this.onWebResourceError = onWebResourceError;
}

class _FakeWebViewWidget extends PlatformWebViewWidget {
  _FakeWebViewWidget(super.params) : super.implementation();

  // A desktop-ish picture: a wallpaper and one window.
  @override
  Widget build(BuildContext context) => ColoredBox(
        color: const Color(0xFF2F3B35),
        child: Center(
          child: FractionallySizedBox(
            widthFactor: 0.56,
            heightFactor: 0.5,
            child: Container(
              color: const Color(0xFFF3F3F1),
              alignment: Alignment.topLeft,
              padding: const EdgeInsets.all(16),
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                for (final w in const [0.8, 0.6, 0.7, 0.4])
                  Padding(
                    padding: const EdgeInsets.only(bottom: 10),
                    child: FractionallySizedBox(widthFactor: w, child: Container(height: 10, color: const Color(0xFF9AA09B))),
                  ),
              ]),
            ),
          ),
        ),
      );
}
