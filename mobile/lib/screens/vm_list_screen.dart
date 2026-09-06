import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../services/vm_client.dart';
import '../widgets/common.dart';
import 'vm_console_screen.dart';
import 'vm_form_screen.dart';

/// Virtual Machine management screen with real-time state, specs,
/// direct VNC console launching, and power controls.
class VmListScreen extends StatefulWidget {
  const VmListScreen({super.key});

  @override
  State<VmListScreen> createState() => _VmListScreenState();
}

class _VmListScreenState extends State<VmListScreen> {
  List<Vm> _vms = [];
  bool _loading = true;
  String? _error;
  final Set<String> _busy = {};

  VmClient get _client {
    final uri = Uri.parse(ApiClient.instance.baseUrl);
    return VmClient(uri.host);
  }

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final vms = await _client.listVms();
      if (!mounted) return;
      setState(() {
        _vms = vms;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString().replaceFirst('Exception: ', '');
        _loading = false;
      });
    }
  }

  Future<void> _act(Vm vm, Future<void> Function(String) action) async {
    HapticFeedback.mediumImpact();
    setState(() => _busy.add(vm.name));
    try {
      await action(vm.name);
      await Future.delayed(const Duration(milliseconds: 400));
      await _load();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(e.toString().replaceFirst('Exception: ', '')),
            backgroundColor: NivaroColors.danger,
            behavior: SnackBarBehavior.floating,
          ),
        );
      }
    } finally {
      if (mounted) setState(() => _busy.remove(vm.name));
    }
  }

  Future<void> _createVm() async {
    HapticFeedback.selectionClick();
    final created = await Navigator.of(context).push<bool>(
      MaterialPageRoute(builder: (_) => VmFormScreen(client: _client)),
    );
    if (created == true) _load();
  }

  Future<void> _editVm(Vm vm) async {
    HapticFeedback.selectionClick();
    final saved = await Navigator.of(context).push<bool>(
      MaterialPageRoute(builder: (_) => VmFormScreen(client: _client, existing: vm)),
    );
    if (saved == true) _load();
  }

  Future<void> _deleteVm(Vm vm) async {
    HapticFeedback.mediumImpact();
    final wipe = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceRaised,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
        title: const Text('Delete Virtual Machine'),
        content: Text('Are you sure you want to delete "${vm.name}"?'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context), child: const Text('Cancel')),
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Keep Disk'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.danger),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete & Wipe Disk'),
          ),
        ],
      ),
    );
    if (wipe == null) return;
    setState(() => _busy.add(vm.name));
    try {
      await _client.deleteVm(vm.name, wipeDisk: wipe);
      await _load();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(e.toString().replaceFirst('Exception: ', '')),
            backgroundColor: NivaroColors.danger,
            behavior: SnackBarBehavior.floating,
          ),
        );
      }
    } finally {
      if (mounted) setState(() => _busy.remove(vm.name));
    }
  }

  void _showMenu(Vm vm) {
    final busy = _busy.contains(vm.name);
    HapticFeedback.lightImpact();
    showModalBottomSheet(
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
                leading: Container(
                  width: 44,
                  height: 44,
                  decoration: BoxDecoration(
                    color: _osColor(vm.name).withValues(alpha: 0.15),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Icon(_osIcon(vm.name), color: _osColor(vm.name)),
                ),
                title: Text(vm.name, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 16)),
                subtitle: Text('${vm.vcpus} vCPUs · ${(vm.memoryMib / 1024).toStringAsFixed(1)} GB RAM'),
                trailing: StatusPill(label: vm.isRunning ? 'Running' : 'Stopped', state: vm.state),
              ),
              const Divider(height: 20),
              ListTile(
                leading: const Icon(Icons.edit_note_rounded),
                title: const Text('Configure Specs'),
                onTap: () {
                  Navigator.pop(context);
                  _editVm(vm);
                },
              ),
              if (vm.isRunning) ...[
                ListTile(
                  leading: const Icon(Icons.restart_alt_rounded, color: NivaroColors.primaryLight),
                  title: const Text('Reset / Restart'),
                  onTap: () {
                    Navigator.pop(context);
                    _act(vm, _client.reset);
                  },
                ),
                ListTile(
                  leading: const Icon(Icons.power_settings_new_rounded, color: NivaroColors.warning),
                  title: const Text('Force Power Off', style: TextStyle(color: NivaroColors.warning)),
                  onTap: () {
                    Navigator.pop(context);
                    _act(vm, _client.forceOff);
                  },
                ),
              ],
              ListTile(
                enabled: !busy,
                leading: const Icon(Icons.delete_outline_rounded, color: NivaroColors.danger),
                title: const Text('Delete VM', style: TextStyle(color: NivaroColors.danger)),
                onTap: () {
                  Navigator.pop(context);
                  _deleteVm(vm);
                },
              ),
            ],
          ),
        ),
      ),
    );
  }

  IconData _osIcon(String name) {
    final lower = name.toLowerCase();
    if (lower.contains('win')) return Icons.window_rounded;
    if (lower.contains('ubuntu') || lower.contains('debian') || lower.contains('arch') || lower.contains('linux')) {
      return Icons.terminal_rounded;
    }
    if (lower.contains('mac') || lower.contains('darwin')) return Icons.laptop_mac_rounded;
    return Icons.developer_board_rounded;
  }

  Color _osColor(String name) {
    final lower = name.toLowerCase();
    if (lower.contains('win')) return const Color(0xFF0284C7);
    if (lower.contains('ubuntu')) return const Color(0xFFE95420);
    if (lower.contains('debian')) return const Color(0xFFD70A53);
    if (lower.contains('arch')) return const Color(0xFF1793D1);
    if (lower.contains('alpine')) return const Color(0xFF0D597F);
    return NivaroColors.primary;
  }

  @override
  Widget build(BuildContext context) {
    final runningCount = _vms.where((v) => v.isRunning).length;

    return SafeArea(
      bottom: false,
      child: Stack(
        children: [
          RefreshIndicator(
            onRefresh: _load,
            color: NivaroColors.primaryLight,
            backgroundColor: NivaroColors.surfaceRaised,
            child: ListView(
              padding: const EdgeInsets.fromLTRB(20, 14, 20, 140),
              children: [
                // Header
                Row(
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            'Virtual Machines',
                            style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                                  fontWeight: FontWeight.w800,
                                  letterSpacing: -0.5,
                                ),
                          ),
                          const SizedBox(height: 2),
                          Text(
                            '${_vms.length} VMs configured · $runningCount active',
                            style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13),
                          ),
                        ],
                      ),
                    ),
                    IconButton(
                      icon: const Icon(Icons.refresh_rounded, size: 22),
                      tooltip: 'Refresh',
                      onPressed: _load,
                    ),
                  ],
                ),
                const SizedBox(height: 20),

                if (_loading && _vms.isEmpty)
                  const Center(
                    child: Padding(
                      padding: EdgeInsets.all(60),
                      child: CircularProgressIndicator(),
                    ),
                  )
                else if (_error != null && _vms.isEmpty)
                  DarkCard(
                    padding: const EdgeInsets.all(28),
                    child: Center(
                      child: Column(
                        children: [
                          const Icon(Icons.error_outline_rounded, color: NivaroColors.danger, size: 36),
                          const SizedBox(height: 12),
                          Text(_error!, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13), textAlign: TextAlign.center),
                          const SizedBox(height: 14),
                          OutlinedButton(onPressed: _load, child: const Text('Retry')),
                        ],
                      ),
                    ),
                  )
                else if (_vms.isEmpty)
                  DarkCard(
                    padding: const EdgeInsets.all(36),
                    child: Center(
                      child: Column(
                        children: [
                          Container(
                            width: 64,
                            height: 64,
                            decoration: BoxDecoration(
                              color: NivaroColors.primary.withValues(alpha: 0.15),
                              shape: BoxShape.circle,
                            ),
                            child: const Icon(Icons.developer_board_rounded, size: 32, color: NivaroColors.primaryLight),
                          ),
                          const SizedBox(height: 16),
                          const Text('No Virtual Machines', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 16)),
                          const SizedBox(height: 6),
                          const Text(
                            'Run full operating systems with KVM hardware acceleration.',
                            style: TextStyle(color: NivaroColors.textMuted, fontSize: 13),
                            textAlign: TextAlign.center,
                          ),
                          const SizedBox(height: 20),
                          ElevatedButton.icon(
                            onPressed: _createVm,
                            icon: const Icon(Icons.add_rounded, size: 18),
                            label: const Text('Create Virtual Machine'),
                          ),
                        ],
                      ),
                    ),
                  )
                else
                  ..._vms.map((vm) {
                    final busy = _busy.contains(vm.name);
                    final osColor = _osColor(vm.name);
                    final osIcon = _osIcon(vm.name);

                    return Padding(
                      padding: const EdgeInsets.only(bottom: 12),
                      child: DarkCard(
                        onTap: () => _showMenu(vm),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Row(
                              children: [
                                Container(
                                  width: 42,
                                  height: 42,
                                  decoration: BoxDecoration(
                                    color: osColor.withValues(alpha: 0.15),
                                    borderRadius: BorderRadius.circular(NivaroShape.medium),
                                    border: Border.all(color: osColor.withValues(alpha: 0.25)),
                                  ),
                                  child: Icon(osIcon, color: osColor, size: 22),
                                ),
                                const SizedBox(width: 14),
                                Expanded(
                                  child: Column(
                                    crossAxisAlignment: CrossAxisAlignment.start,
                                    children: [
                                      Text(
                                        vm.name,
                                        style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 15.5),
                                        overflow: TextOverflow.ellipsis,
                                      ),
                                      const SizedBox(height: 2),
                                      Text(
                                        '${vm.vcpus} vCPU · ${(vm.memoryMib / 1024).toStringAsFixed(1)} GB RAM · ${vm.diskGib} GB disk',
                                        style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                                      ),
                                    ],
                                  ),
                                ),
                                StatusPill(label: vm.isRunning ? 'Running' : 'Stopped', state: vm.state),
                                const SizedBox(width: 4),
                                IconButton(
                                  icon: const Icon(Icons.more_vert_rounded, color: NivaroColors.textMuted, size: 20),
                                  onPressed: busy ? null : () => _showMenu(vm),
                                ),
                              ],
                            ),
                            const SizedBox(height: 14),

                            // Action Buttons Row
                            Row(
                              children: [
                                if (vm.isRunning) ...[
                                  Expanded(
                                    child: FilledButton.icon(
                                      onPressed: busy
                                          ? null
                                          : () {
                                              HapticFeedback.lightImpact();
                                              Navigator.of(context).push(
                                                MaterialPageRoute(builder: (_) => VmConsoleScreen(vmName: vm.name)),
                                              );
                                            },
                                      icon: const Icon(Icons.monitor_rounded, size: 18),
                                      label: const Text('Open Console'),
                                      style: FilledButton.styleFrom(
                                        backgroundColor: NivaroColors.primary,
                                        padding: const EdgeInsets.symmetric(vertical: 10),
                                        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.medium)),
                                      ),
                                    ),
                                  ),
                                  const SizedBox(width: 10),
                                  OutlinedButton.icon(
                                    onPressed: busy ? null : () => _act(vm, _client.shutdown),
                                    icon: const Icon(Icons.power_settings_new_rounded, size: 18, color: NivaroColors.danger),
                                    label: const Text('Stop', style: TextStyle(color: NivaroColors.danger)),
                                    style: OutlinedButton.styleFrom(
                                      side: const BorderSide(color: NivaroColors.borderSubtle),
                                      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
                                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.medium)),
                                    ),
                                  ),
                                ] else
                                  Expanded(
                                    child: FilledButton.icon(
                                      onPressed: busy ? null : () => _act(vm, _client.start),
                                      icon: busy
                                          ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                                          : const Icon(Icons.play_arrow_rounded, size: 18),
                                      label: const Text('Start VM'),
                                      style: FilledButton.styleFrom(
                                        backgroundColor: NivaroColors.success,
                                        padding: const EdgeInsets.symmetric(vertical: 10),
                                        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.medium)),
                                      ),
                                    ),
                                  ),
                              ],
                            ),
                          ],
                        ),
                      ),
                    );
                  }),
              ],
            ),
          ),

          // Floating Action Button
          Positioned(
            right: 20,
            bottom: 110,
            child: FloatingActionButton.extended(
              onPressed: _createVm,
              backgroundColor: NivaroColors.primary,
              foregroundColor: Colors.white,
              elevation: 4,
              icon: const Icon(Icons.add_rounded),
              label: const Text('New VM', style: TextStyle(fontWeight: FontWeight.w600)),
            ),
          ),
        ],
      ),
    );
  }
}

