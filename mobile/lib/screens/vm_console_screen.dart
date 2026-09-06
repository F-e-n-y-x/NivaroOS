import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../services/api_client.dart';
import '../services/rfb_client.dart';
import '../services/vm_client.dart';
import '../theme.dart';
import '../widgets/rfb_view.dart';

/// Native touch-first VM console screen connecting directly to the sidecar's
/// RFB websocket bridge.
class VmConsoleScreen extends StatefulWidget {
  final String vmName;
  const VmConsoleScreen({super.key, required this.vmName});

  @override
  State<VmConsoleScreen> createState() => _VmConsoleScreenState();
}

class _VmConsoleScreenState extends State<VmConsoleScreen> {
  late final RfbClient _client;
  late final VmClient _vmClient;
  String? _connectError;

  @override
  void initState() {
    super.initState();
    final host = Uri.parse(ApiClient.instance.baseUrl).host;
    _vmClient = VmClient(host);
    _client = RfbClient(host: host, port: 28641, vmName: widget.vmName);
    _client.connect().catchError((e) {
      if (mounted) setState(() => _connectError = e.toString());
    });
  }

  @override
  void dispose() {
    _client.close();
    super.dispose();
  }

  Future<void> _power() async {
    HapticFeedback.lightImpact();
    final action = await showModalBottomSheet<String>(
      context: context,
      backgroundColor: NivaroColors.surfaceRaised,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(24))),
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              ListTile(
                leading: const Icon(Icons.power_settings_new_rounded, color: NivaroColors.primaryLight),
                title: const Text('Graceful Shutdown', style: TextStyle(fontWeight: FontWeight.w600)),
                subtitle: const Text('Send ACPI shutdown signal'),
                onTap: () => Navigator.pop(context, 'shutdown'),
              ),
              ListTile(
                leading: const Icon(Icons.restart_alt_rounded, color: NivaroColors.warning),
                title: const Text('Reset / Reboot', style: TextStyle(fontWeight: FontWeight.w600)),
                subtitle: const Text('Hard reset the virtual machine'),
                onTap: () => Navigator.pop(context, 'reset'),
              ),
              ListTile(
                leading: const Icon(Icons.stop_circle_rounded, color: NivaroColors.danger),
                title: const Text('Force Power Off', style: TextStyle(color: NivaroColors.danger, fontWeight: FontWeight.w600)),
                subtitle: const Text('Instantly kill power to VM'),
                onTap: () => Navigator.pop(context, 'force-off'),
              ),
            ],
          ),
        ),
      ),
    );
    if (action == null) return;
    try {
      HapticFeedback.mediumImpact();
      switch (action) {
        case 'shutdown':
          await _vmClient.shutdown(widget.vmName);
          break;
        case 'force-off':
          await _vmClient.forceOff(widget.vmName);
          break;
        case 'reset':
          await _vmClient.reset(widget.vmName);
          break;
      }
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Action $action dispatched successfully.')),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(e.toString().replaceFirst('Exception: ', '')), backgroundColor: NivaroColors.danger),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: NivaroColors.background,
      appBar: AppBar(
        backgroundColor: NivaroColors.surface,
        foregroundColor: Colors.white,
        elevation: 0,
        title: Row(
          children: [
            Container(
              width: 8,
              height: 8,
              decoration: const BoxDecoration(
                color: NivaroColors.success,
                shape: BoxShape.circle,
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    widget.vmName,
                    style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 16),
                    overflow: TextOverflow.ellipsis,
                  ),
                  Text(
                    'Native RFB Console',
                    style: TextStyle(color: Colors.white.withValues(alpha: 0.6), fontSize: 11),
                  ),
                ],
              ),
            ),
          ],
        ),
        actions: [
          IconButton(
            icon: const Icon(Icons.power_settings_new_rounded, color: NivaroColors.danger),
            tooltip: 'Power Menu',
            onPressed: _power,
          ),
        ],
      ),
      body: _connectError != null
          ? Center(
              child: Padding(
                padding: const EdgeInsets.all(32),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Icon(Icons.error_outline_rounded, color: NivaroColors.danger, size: 48),
                    const SizedBox(height: 16),
                    Text(
                      'Connection Failed',
                      style: Theme.of(context).textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w700),
                    ),
                    const SizedBox(height: 8),
                    Text(
                      _connectError!,
                      style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13),
                      textAlign: TextAlign.center,
                    ),
                    const SizedBox(height: 20),
                    FilledButton.icon(
                      onPressed: () {
                        setState(() => _connectError = null);
                        _client.connect().catchError((e) {
                          if (mounted) setState(() => _connectError = e.toString());
                        });
                      },
                      icon: const Icon(Icons.refresh_rounded, size: 18),
                      label: const Text('Retry Connection'),
                    ),
                  ],
                ),
              ),
            )
          : RfbView(client: _client, onPower: _power),
    );
  }
}

