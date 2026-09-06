import 'package:flutter/material.dart';
import 'package:webview_flutter/webview_flutter.dart';
import '../services/api_client.dart';
import '../services/storage_service.dart';

/// The one deliberate exception to "this app is native, not a website
/// wrapper": a VM's live remote display. NivaroOS's own console view
/// already renders this correctly (a real, working VNC-over-websocket
/// bridge) - reimplementing an RFB/VNC client natively here, blind,
/// without a real device to verify it against, would be a worse and
/// riskier outcome than reusing the one already-proven implementation for
/// just this one specialized, inherently-visual screen. Everything else in
/// this app (discovery, login, dashboard, files, VM list/power control,
/// settings) is genuinely native - see home_shell.dart.
///
/// The embedded page is its own separate browser context with no
/// knowledge of this app's already-authenticated session, so the current
/// tokens are handed off via one-time query params the web app itself
/// reads and clears on load (see ui/src/main.js's handleMobileAppTokenHandoff).
class VmConsoleScreen extends StatefulWidget {
  final String vmName;
  const VmConsoleScreen({super.key, required this.vmName});

  @override
  State<VmConsoleScreen> createState() => _VmConsoleScreenState();
}

class _VmConsoleScreenState extends State<VmConsoleScreen> {
  late final WebViewController _controller;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setBackgroundColor(const Color(0xFF1E1E1E))
      ..setNavigationDelegate(NavigationDelegate(
        onPageFinished: (_) {
          if (mounted) setState(() => _loading = false);
        },
      ));
    _load();
  }

  Future<void> _load() async {
    final token = await StorageService.instance.getAccessToken();
    final refresh = await StorageService.instance.getRefreshToken();
    final base = ApiClient.instance.baseUrl;
    final url = Uri.parse('$base/vm-console/${Uri.encodeComponent(widget.vmName)}').replace(
      queryParameters: {
        if (token != null) 'token': token,
        if (refresh != null) 'refresh': refresh,
      },
    );
    _controller.loadRequest(url);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: const Color(0xFF1E1E1E),
      appBar: AppBar(
        backgroundColor: const Color(0xFF262626),
        foregroundColor: Colors.white,
        title: Text(widget.vmName),
      ),
      body: Stack(
        children: [
          WebViewWidget(controller: _controller),
          if (_loading) const Center(child: CircularProgressIndicator()),
        ],
      ),
    );
  }
}
