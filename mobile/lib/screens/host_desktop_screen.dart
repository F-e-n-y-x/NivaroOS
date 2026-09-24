import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../services/api_client.dart';
import '../services/rfb_client.dart';
import '../services/vm_client.dart';
import '../theme.dart';
import '../widgets/rfb_view.dart';
import '../widgets/common.dart';

/// Remote-controls the NivaroOS server's OWN desktop session (x11vnc on
/// display :0) - a different console from any per-VM one. Deliberately
/// mirrors VmConsoleScreen's structure (fullscreen/rotation handling, the
/// same RfbClient/RfbView stack) since it's the same underlying RFB
/// protocol either way, just pointed at /host/console instead of
/// /vms/{name}/console (see RfbClient's vmName == null case) - the only
/// host-specific feature is changing the host's actual display resolution,
/// which has no per-VM equivalent.
class HostDesktopScreen extends StatefulWidget {
  const HostDesktopScreen({super.key});

  @override
  State<HostDesktopScreen> createState() => _HostDesktopScreenState();
}

class _HostDesktopScreenState extends State<HostDesktopScreen>
    with WidgetsBindingObserver {
  late final RfbClient _client;
  late final VmClient _vmClient;
  bool _isFullscreen = false;
  bool _manualLandscape = false;
  bool _lastAppliedLandscape = false;
  HostDisplay? _display;
  bool _resizing = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    SystemChrome.setPreferredOrientations([
      DeviceOrientation.portraitUp,
      DeviceOrientation.portraitDown,
      DeviceOrientation.landscapeLeft,
      DeviceOrientation.landscapeRight,
    ]);
    final host = Uri.parse(ApiClient.instance.baseUrl).host;
    _vmClient = VmClient(host);
    _client = RfbClient(host: host, port: 28641);
    _loadDisplay();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _client.close();
    SystemChrome.setPreferredOrientations([
      DeviceOrientation.portraitUp,
      DeviceOrientation.portraitDown,
      DeviceOrientation.landscapeLeft,
      DeviceOrientation.landscapeRight,
    ]);
    SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
    super.dispose();
  }

  void _syncSystemUI(bool isLandscape) {
    if (isLandscape != _lastAppliedLandscape || _isFullscreen) {
      _lastAppliedLandscape = isLandscape;
      if (isLandscape || _isFullscreen) {
        SystemChrome.setEnabledSystemUIMode(SystemUiMode.immersiveSticky);
      } else {
        SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
      }
    }
  }

  void _toggleOrientation() {
    setState(() => _manualLandscape = !_manualLandscape);
    if (_manualLandscape) {
      SystemChrome.setPreferredOrientations([
        DeviceOrientation.landscapeLeft,
        DeviceOrientation.landscapeRight,
      ]);
      SystemChrome.setEnabledSystemUIMode(SystemUiMode.immersiveSticky);
    } else {
      SystemChrome.setPreferredOrientations([
        DeviceOrientation.portraitUp,
        DeviceOrientation.portraitDown,
        DeviceOrientation.landscapeLeft,
        DeviceOrientation.landscapeRight,
      ]);
      SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
    }
  }

  void _toggleFullscreen() {
    setState(() => _isFullscreen = !_isFullscreen);
    if (_isFullscreen) {
      SystemChrome.setEnabledSystemUIMode(SystemUiMode.immersiveSticky);
    } else {
      final isLandscape =
          MediaQuery.of(context).orientation == Orientation.landscape ||
              _manualLandscape;
      if (!isLandscape) {
        SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
      }
    }
  }

  Future<void> _loadDisplay() async {
    try {
      final d = await _vmClient.getHostDisplay();
      if (mounted) setState(() => _display = d);
    } catch (_) {}
  }

  Future<void> _resolutionMenu() async {
    final resolutions = _display?.resolutions ?? [];
    final choice = await showModalBottomSheet<DisplayResolution>(
      context: context,
      backgroundColor: NivaroColors.surfaceContainerHighest,
      shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(24))),
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 16, 20, 20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Center(
                  child: Container(
                      width: 36,
                      height: 4,
                      decoration: BoxDecoration(
                          color: NivaroColors.borderHighlight,
                          borderRadius: BorderRadius.circular(2)))),
              const SizedBox(height: 16),
              Text('Host Display Resolution',
                  style: TextStyle(
                      fontWeight: FontWeight.w800,
                      fontSize: 18,
                      color: NivaroColors.textPrimary)),
              if (_display != null) ...[
                const SizedBox(height: 4),
                Text('Current: ${_display!.current}',
                    style: TextStyle(
                        color: NivaroColors.textMuted, fontSize: 12)),
              ],
              const SizedBox(height: 14),
              if (resolutions.isEmpty)
                Padding(
                  padding: EdgeInsets.symmetric(vertical: 16),
                  child: Text(
                      'No alternate resolutions detected for this display.',
                      style: TextStyle(color: NivaroColors.textMuted)),
                )
              else
                Flexible(
                  child: ListView.separated(
                    shrinkWrap: true,
                    itemCount: resolutions.length,
                    separatorBuilder: (_, _) => Divider(
                        height: 1, color: NivaroColors.borderSubtle),
                    itemBuilder: (context, index) {
                      final r = resolutions[index];
                      final isCurrent = _display != null &&
                          r.width == _display!.width &&
                          r.height == _display!.height;
                      return ListTile(
                        contentPadding: EdgeInsets.zero,
                        leading: Icon(Icons.desktop_windows_rounded,
                            color: isCurrent
                                ? NivaroColors.primaryLight
                                : NivaroColors.textMuted),
                        title: Text(
                            r.label.isNotEmpty
                                ? r.label
                                : '${r.width}x${r.height}',
                            style:
                                const TextStyle(fontWeight: FontWeight.w600)),
                        trailing: isCurrent
                            ? Icon(Icons.check_circle_rounded,
                                color: NivaroColors.successLight)
                            : null,
                        onTap:
                            isCurrent ? null : () => Navigator.pop(context, r),
                      );
                    },
                  ),
                ),
            ],
          ),
        ),
      ),
    );
    if (choice == null || _resizing) return;
    setState(() => _resizing = true);
    try {
      final d = await _vmClient.setHostDisplay(choice.width, choice.height);
      if (mounted) {
        setState(() => _display = d);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
              content: Text(
                  'Host display resolution: ${choice.width}x${choice.height}')),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
              content: Text(e.toString().replaceFirst('Exception: ', '')),
              backgroundColor: NivaroColors.danger),
        );
      }
    } finally {
      if (mounted) setState(() => _resizing = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final isDeviceLandscape =
        MediaQuery.of(context).orientation == Orientation.landscape;
    final isLandscape = isDeviceLandscape || _manualLandscape;
    final hideAppBar = _isFullscreen || isLandscape;

    _syncSystemUI(isLandscape);

    return Scaffold(
      backgroundColor: Colors.black,
      appBar: hideAppBar
          ? null
          : AppBar(
              backgroundColor: NivaroColors.surface,
              foregroundColor: Colors.white,
              elevation: 0,
              title: Row(
                children: [
                  PulsingStatusDot(color: NivaroColors.success, size: 7),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        const Text('Host Desktop',
                            style: TextStyle(
                                fontWeight: FontWeight.w800, fontSize: 16)),
                        Text(
                          _display != null
                              ? 'NivaroOS Server · ${_display!.current}'
                              : 'NivaroOS Server Display',
                          style: TextStyle(
                              color: NivaroColors.textMuted, fontSize: 11),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
              actions: [
                RoundIconButton(
                  icon: Icons.stay_current_landscape_rounded,
                  tooltip: 'Switch to Landscape',
                  onPressed: _toggleOrientation,
                ),
                const SizedBox(width: 6),
                RoundIconButton(
                  icon: Icons.aspect_ratio_rounded,
                  tooltip: 'Display Resolution',
                  onPressed: _resizing ? null : _resolutionMenu,
                ),
                const SizedBox(width: 12),
              ],
            ),
      body: SafeArea(
        top: !hideAppBar,
        bottom: false,
        left: false,
        right: false,
        child: RfbView(
          client: _client,
          vmClient: _vmClient,
          vmName: '',
          isFullscreen: _isFullscreen,
          onToggleFullscreen: _toggleFullscreen,
          isLandscape: isLandscape,
          onToggleOrientation: _toggleOrientation,
        ),
      ),
    );
  }
}
