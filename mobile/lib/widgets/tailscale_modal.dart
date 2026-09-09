import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:url_launcher/url_launcher.dart';
import '../theme.dart';
import '../services/tailscale_service.dart';
import '../screens/terminal_screen.dart';
import 'common.dart';

class TailscaleModal extends StatefulWidget {
  const TailscaleModal({super.key});

  static Future<void> show(BuildContext context) {
    return showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      builder: (_) => const TailscaleModal(),
    );
  }

  @override
  State<TailscaleModal> createState() => _TailscaleModalState();
}

class _TailscaleModalState extends State<TailscaleModal> {
  TailscaleStatus? _status;
  bool _loading = true;
  bool _toggling = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
    });
    try {
      final status = await TailscaleService.instance.getStatus();

      if (!mounted) return;
      setState(() {
        _status = status;
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _loading = false;
      });
    }
  }

  Future<void> _toggleState(bool target) async {
    setState(() => _toggling = true);
    try {
      await TailscaleService.instance.setState(target);
      await Future.delayed(const Duration(milliseconds: 1200));
      await _load();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Failed to update Tailscale: $e'), backgroundColor: NivaroColors.danger),
      );
    } finally {
      if (mounted) setState(() => _toggling = false);
    }
  }

  Future<void> _connectWithAuthKey() async {
    final ctrl = TextEditingController();
    final key = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceContainerHighest,
        title: const Text('Connect to Tailscale'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Enter your Tailscale Auth Key (tskey-auth-...) from admin.tailscale.com/keys:',
              style: TextStyle(color: NivaroColors.textMuted, fontSize: 13),
            ),
            const SizedBox(height: 14),
            TextField(
              controller: ctrl,
              autofocus: true,
              style: const TextStyle(fontFamily: 'monospace', fontSize: 13),
              decoration: const InputDecoration(
                hintText: 'tskey-auth-kXXXXX...',
                border: OutlineInputBorder(),
              ),
            ),
          ],
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.primary),
            onPressed: () => Navigator.pop(context, ctrl.text.trim()),
            child: const Text('Connect'),
          ),
        ],
      ),
    );

    if (key == null || key.isEmpty) return;

    setState(() => _toggling = true);
    try {
      await TailscaleService.instance.connectWithAuthKey(key);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Tailscale connection initiated.')),
        );
      }
      await Future.delayed(const Duration(seconds: 2));
      await _load();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Failed to connect: $e'), backgroundColor: NivaroColors.danger),
        );
      }
    } finally {
      if (mounted) setState(() => _toggling = false);
    }
  }

  Future<void> _openLoginUrl(String url) async {
    if (url.isNotEmpty) {
      final uri = Uri.tryParse(url);
      if (uri != null && await canLaunchUrl(uri)) {
        await launchUrl(uri, mode: LaunchMode.externalApplication);
      }
    }
  }

  void _copyToClipboard(String text, String label) {
    Clipboard.setData(ClipboardData(text: text));
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text('$label copied to clipboard'),
        behavior: SnackBarBehavior.floating,
        duration: const Duration(seconds: 2),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final status = _status;
    final isRunning = status?.isRunning ?? false;

    return Container(
      constraints: BoxConstraints(
        maxHeight: MediaQuery.of(context).size.height * 0.90,
      ),
      decoration: const BoxDecoration(
        color: NivaroColors.surfaceContainerLowest,
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      child: Column(
        children: [
          // Drag Handle
          Center(
            child: Container(
              margin: const EdgeInsets.only(top: 12, bottom: 8),
              width: 40,
              height: 4,
              decoration: BoxDecoration(
                color: NivaroColors.borderSubtle,
                borderRadius: BorderRadius.circular(2),
              ),
            ),
          ),

          // Header
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 10),
            child: Row(
              children: [
                Container(
                  width: 42,
                  height: 42,
                  decoration: BoxDecoration(
                    color: NivaroColors.primary.withOpacity(0.12),
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(color: NivaroColors.primary.withOpacity(0.3)),
                  ),
                  alignment: Alignment.center,
                  child: const Icon(Icons.vpn_lock_rounded, color: NivaroColors.primaryLight, size: 22),
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text(
                        'Tailscale VPN Manager',
                        style: TextStyle(fontWeight: FontWeight.w800, fontSize: 17, color: NivaroColors.textPrimary),
                      ),
                      const SizedBox(height: 2),
                      Row(
                        children: [
                          PulsingStatusDot(
                            color: isRunning ? NivaroColors.success : NivaroColors.textMuted,
                            size: 6.5,
                            animate: isRunning,
                          ),
                          const SizedBox(width: 6),
                          Text(
                            isRunning ? 'Tailnet Connected' : (status?.backendState ?? 'Offline / Standby'),
                            style: TextStyle(
                              color: isRunning ? NivaroColors.successLight : NivaroColors.textMuted,
                              fontSize: 12,
                              fontWeight: FontWeight.w600,
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
                RoundIconButton(
                  icon: Icons.refresh_rounded,
                  tooltip: 'Refresh Tailscale',
                  onPressed: _loading ? null : _load,
                ),
                const SizedBox(width: 6),
                RoundIconButton(
                  icon: Icons.close_rounded,
                  tooltip: 'Close',
                  onPressed: () => Navigator.pop(context),
                ),
              ],
            ),
          ),
          const Divider(height: 1, color: NivaroColors.borderSubtle),

          // Content
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : ListView(
                    padding: const EdgeInsets.fromLTRB(20, 16, 20, 32),
                    children: [
                      // Master Connect / Disconnect Card
                      DarkCard(
                        padding: const EdgeInsets.all(16),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Row(
                              children: [
                                Expanded(
                                  child: Column(
                                    crossAxisAlignment: CrossAxisAlignment.start,
                                    children: [
                                      const Text(
                                        'Tailscale Mesh Network',
                                        style: TextStyle(fontWeight: FontWeight.w700, fontSize: 15),
                                      ),
                                      const SizedBox(height: 4),
                                      Text(
                                        isRunning
                                            ? 'Server is accessible securely from any device in your tailnet.'
                                            : 'Connect this server to your Tailscale mesh network for zero-config remote access.',
                                        style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12.5),
                                      ),
                                    ],
                                  ),
                                ),
                                const SizedBox(width: 12),
                                _toggling
                                    ? const SizedBox(width: 28, height: 28, child: CircularProgressIndicator(strokeWidth: 2.5))
                                    : Switch(
                                        value: isRunning,
                                        activeColor: NivaroColors.primaryLight,
                                        onChanged: (val) => _toggleState(val),
                                      ),
                              ],
                            ),
                            if (!isRunning) ...[
                              const SizedBox(height: 14),
                              const Divider(height: 1, color: NivaroColors.borderSubtle),
                              const SizedBox(height: 12),
                              Row(
                                children: [
                                  Expanded(
                                    child: FilledButton.icon(
                                      onPressed: _toggling ? null : _connectWithAuthKey,
                                      style: FilledButton.styleFrom(
                                        backgroundColor: NivaroColors.primary,
                                        padding: const EdgeInsets.symmetric(vertical: 10),
                                        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
                                      ),
                                      icon: const Icon(Icons.key_rounded, size: 18),
                                      label: const Text('Connect with Auth Key', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 13)),
                                    ),
                                  ),
                                  const SizedBox(width: 8),
                                  OutlinedButton.icon(
                                    onPressed: () {
                                      Navigator.pop(context);
                                      Navigator.of(context).push(
                                        MaterialPageRoute(
                                          builder: (_) => const TerminalScreen(initCommand: 'tailscale up', title: 'Tailscale Connect'),
                                        ),
                                      );
                                    },
                                    style: OutlinedButton.styleFrom(
                                      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
                                    ),
                                    icon: const Icon(Icons.terminal_rounded, size: 18),
                                    label: const Text('Terminal Up'),
                                  ),
                                ],
                              ),
                              if (status?.authUrl.isNotEmpty == true) ...[
                                const SizedBox(height: 10),
                                OutlinedButton.icon(
                                  onPressed: () => _openLoginUrl(status!.authUrl),
                                  style: OutlinedButton.styleFrom(
                                    foregroundColor: NivaroColors.infoLight,
                                    minimumSize: const Size(double.infinity, 40),
                                    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
                                  ),
                                  icon: const Icon(Icons.open_in_browser_rounded, size: 18),
                                  label: const Text('Open Tailscale Auth Web Page'),
                                ),
                              ],
                            ],
                          ],
                        ),
                      ),
                      const SizedBox(height: 16),

                      // Server Node Details Card
                      if (status != null && (status.selfIp.isNotEmpty || isRunning)) ...[
                        DarkCard(
                          padding: const EdgeInsets.all(16),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Row(
                                children: [
                                  const Icon(Icons.computer_rounded, color: NivaroColors.primaryLight, size: 18),
                                  const SizedBox(width: 8),
                                  Text(
                                    status.hostName,
                                    style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14),
                                  ),
                                  const Spacer(),
                                  Container(
                                    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                                    decoration: BoxDecoration(
                                      color: NivaroColors.success.withOpacity(0.12),
                                      borderRadius: BorderRadius.circular(6),
                                    ),
                                    child: const Text('Host Node', style: TextStyle(color: NivaroColors.successLight, fontSize: 11, fontWeight: FontWeight.bold)),
                                  ),
                                ],
                              ),
                              const SizedBox(height: 14),
                              if (status.selfIp.isNotEmpty)
                                InkWell(
                                  onTap: () => _copyToClipboard(status.selfIp, 'Tailscale IP'),
                                  borderRadius: BorderRadius.circular(8),
                                  child: Container(
                                    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                                    decoration: BoxDecoration(
                                      color: NivaroColors.surfaceRaised,
                                      borderRadius: BorderRadius.circular(8),
                                      border: Border.all(color: NivaroColors.borderSubtle),
                                    ),
                                    child: Row(
                                      children: [
                                        const Text('Tailscale IP: ', style: TextStyle(color: NivaroColors.textMuted, fontSize: 12.5, fontWeight: FontWeight.w600)),
                                        Expanded(
                                          child: Text(status.selfIp, style: const TextStyle(fontFamily: 'monospace', fontWeight: FontWeight.bold, fontSize: 13, color: NivaroColors.primaryLight)),
                                        ),
                                        const Icon(Icons.copy_rounded, size: 16, color: NivaroColors.textFaint),
                                      ],
                                    ),
                                  ),
                                ),
                              if (status.magicDns.isNotEmpty) ...[
                                const SizedBox(height: 8),
                                InkWell(
                                  onTap: () => _copyToClipboard('${status.hostName}.${status.magicDns}', 'MagicDNS Domain'),
                                  borderRadius: BorderRadius.circular(8),
                                  child: Container(
                                    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                                    decoration: BoxDecoration(
                                      color: NivaroColors.surfaceRaised,
                                      borderRadius: BorderRadius.circular(8),
                                      border: Border.all(color: NivaroColors.borderSubtle),
                                    ),
                                    child: Row(
                                      children: [
                                        const Text('Domain: ', style: TextStyle(color: NivaroColors.textMuted, fontSize: 12.5, fontWeight: FontWeight.w600)),
                                        Expanded(
                                          child: Text('${status.hostName}.${status.magicDns}', style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 12.5, color: NivaroColors.textPrimary)),
                                        ),
                                        const Icon(Icons.copy_rounded, size: 16, color: NivaroColors.textFaint),
                                      ],
                                    ),
                                  ),
                                ),
                              ],
                            ],
                          ),
                        ),
                        const SizedBox(height: 20),
                      ],

                      // Tailnet Peers List
                      SectionHeader(
                        title: 'Tailnet Devices (${status?.peers.length ?? 0})',
                        subtitle: 'Connected nodes on this secure mesh network',
                      ),
                      if (status == null || status.peers.isEmpty)
                        const DarkCard(
                          padding: EdgeInsets.all(16),
                          child: Row(
                            children: [
                              Icon(Icons.devices_other_rounded, color: NivaroColors.textMuted, size: 20),
                              SizedBox(width: 12),
                              Expanded(
                                child: Text('No other tailnet devices connected yet.', style: TextStyle(color: NivaroColors.textMuted, fontSize: 12.5)),
                              ),
                            ],
                          ),
                        )
                      else
                        ...status.peers.map((peer) => Padding(
                              padding: const EdgeInsets.only(bottom: 8),
                              child: DarkCard(
                                padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
                                child: Row(
                                  children: [
                                    Icon(
                                      peer.os.toLowerCase().contains('android') || peer.os.toLowerCase().contains('ios')
                                          ? Icons.phone_android_rounded
                                          : Icons.laptop_mac_rounded,
                                      size: 20,
                                      color: peer.online ? NivaroColors.primaryLight : NivaroColors.textFaint,
                                    ),
                                    const SizedBox(width: 12),
                                    Expanded(
                                      child: Column(
                                        crossAxisAlignment: CrossAxisAlignment.start,
                                        children: [
                                          Text(
                                            peer.hostName,
                                            style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5),
                                            maxLines: 1,
                                            overflow: TextOverflow.ellipsis,
                                          ),
                                          const SizedBox(height: 2),
                                          Text(
                                            '${peer.ip.isNotEmpty ? peer.ip : "No IP"} · ${peer.os}',
                                            style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                                          ),
                                        ],
                                      ),
                                    ),
                                    Container(
                                      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                                      decoration: BoxDecoration(
                                        color: peer.online ? NivaroColors.success.withOpacity(0.12) : NivaroColors.surfaceMuted,
                                        borderRadius: BorderRadius.circular(6),
                                      ),
                                      child: Text(
                                        peer.online ? 'Online' : 'Offline',
                                        style: TextStyle(
                                          color: peer.online ? NivaroColors.successLight : NivaroColors.textFaint,
                                          fontSize: 11,
                                          fontWeight: FontWeight.bold,
                                        ),
                                      ),
                                    ),
                                  ],
                                ),
                              ),
                            )),
                    ],
                  ),
          ),
        ],
      ),
    );
  }
}
