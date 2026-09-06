import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../services/api_client.dart';
import '../services/rfb_client.dart';
import '../services/vm_client.dart';
import '../models/vm_snapshot.dart';
import '../theme.dart';
import '../widgets/rfb_view.dart';
import '../widgets/common.dart';

class VmConsoleScreen extends StatefulWidget {
  final String vmName;
  const VmConsoleScreen({super.key, required this.vmName});

  @override
  State<VmConsoleScreen> createState() => _VmConsoleScreenState();
}

class _VmConsoleScreenState extends State<VmConsoleScreen> with WidgetsBindingObserver {
  late final RfbClient _client;
  late final VmClient _vmClient;
  Vm? _vm;
  bool _isFullscreen = false;
  bool _manualLandscape = false;
  bool _lastAppliedLandscape = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    // Allow dynamic sensor auto-rotation for seamless landscape switching
    SystemChrome.setPreferredOrientations([
      DeviceOrientation.portraitUp,
      DeviceOrientation.portraitDown,
      DeviceOrientation.landscapeLeft,
      DeviceOrientation.landscapeRight,
    ]);
    final host = Uri.parse(ApiClient.instance.baseUrl).host;
    _vmClient = VmClient(host);
    _client = RfbClient(host: host, port: 28641, vmName: widget.vmName);
    _loadVm();
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
    HapticFeedback.lightImpact();
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

  Future<void> _loadVm() async {
    try {
      final vm = await _vmClient.getVm(widget.vmName);
      if (mounted) setState(() => _vm = vm);
    } catch (_) {}
  }

  void _toggleFullscreen() {
    setState(() => _isFullscreen = !_isFullscreen);
    if (_isFullscreen) {
      SystemChrome.setEnabledSystemUIMode(SystemUiMode.immersiveSticky);
    } else {
      final isLandscape = MediaQuery.of(context).orientation == Orientation.landscape || _manualLandscape;
      if (!isLandscape) {
        SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
      }
    }
  }

  Future<void> _powerMenu() async {
    HapticFeedback.lightImpact();
    final action = await showModalBottomSheet<String>(
      context: context,
      backgroundColor: NivaroColors.surfaceContainerHighest,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(24))),
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Container(width: 36, height: 4, decoration: BoxDecoration(color: NivaroColors.borderHighlight, borderRadius: BorderRadius.circular(2))),
              const SizedBox(height: 12),
              ListTile(
                leading: const Icon(Icons.power_settings_new_rounded, color: NivaroColors.primaryLight),
                title: const Text('Graceful ACPI Shutdown', style: TextStyle(fontWeight: FontWeight.w600)),
                subtitle: const Text('Send ACPI power button signal to guest OS'),
                onTap: () => Navigator.pop(context, 'shutdown'),
              ),
              ListTile(
                leading: const Icon(Icons.restart_alt_rounded, color: NivaroColors.warningLight),
                title: const Text('Reset / Reboot', style: TextStyle(fontWeight: FontWeight.w600)),
                subtitle: const Text('Hard reset virtual machine processor'),
                onTap: () => Navigator.pop(context, 'reset'),
              ),
              ListTile(
                leading: const Icon(Icons.stop_circle_rounded, color: NivaroColors.dangerLight),
                title: const Text('Force Power Off', style: TextStyle(color: NivaroColors.dangerLight, fontWeight: FontWeight.w600)),
                subtitle: const Text('Instantly cut power to VM'),
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
          SnackBar(content: Text('Dispatched $action successfully.')),
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

  Future<void> _snapshotsMenu() async {
    HapticFeedback.lightImpact();
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      backgroundColor: NivaroColors.surfaceContainerHighest,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(24))),
      builder: (context) => _SnapshotsSheet(vmName: widget.vmName, vmClient: _vmClient),
    );
  }

  Future<void> _isoMenu() async {
    HapticFeedback.lightImpact();
    showModalBottomSheet(
      context: context,
      backgroundColor: NivaroColors.surfaceContainerHighest,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(24))),
      builder: (context) => _IsoSheet(vmName: widget.vmName, vmClient: _vmClient, onUpdated: _loadVm),
    );
  }

  @override
  Widget build(BuildContext context) {
    final isDeviceLandscape = MediaQuery.of(context).orientation == Orientation.landscape;
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
                  const PulsingStatusDot(color: NivaroColors.success, size: 7),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          widget.vmName,
                          style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 16),
                          overflow: TextOverflow.ellipsis,
                        ),
                        Text(
                          _vm != null ? '${_vm!.vcpus} vCPU · ${(_vm!.memoryMib / 1024).toStringAsFixed(1)} GB' : 'Virtual Machine Console',
                          style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11),
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
                  icon: Icons.camera_alt_rounded,
                  tooltip: 'Snapshots',
                  onPressed: _snapshotsMenu,
                ),
                const SizedBox(width: 6),
                RoundIconButton(
                  icon: Icons.album_rounded,
                  tooltip: 'ISO / CD-ROM',
                  onPressed: _isoMenu,
                ),
                const SizedBox(width: 6),
                RoundIconButton(
                  icon: Icons.power_settings_new_rounded,
                  tooltip: 'Power Menu',
                  color: NivaroColors.danger.withOpacity(0.15),
                  iconColor: NivaroColors.dangerLight,
                  onPressed: _powerMenu,
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
          vmName: widget.vmName,
          onPower: _powerMenu,
          onSnapshots: _snapshotsMenu,
          onIso: _isoMenu,
          isFullscreen: _isFullscreen,
          onToggleFullscreen: _toggleFullscreen,
          isLandscape: isLandscape,
          onToggleOrientation: _toggleOrientation,
        ),
      ),
    );
  }
}

class _SnapshotsSheet extends StatefulWidget {
  final String vmName;
  final VmClient vmClient;
  const _SnapshotsSheet({required this.vmName, required this.vmClient});

  @override
  State<_SnapshotsSheet> createState() => _SnapshotsSheetState();
}

class _SnapshotsSheetState extends State<_SnapshotsSheet> {
  List<VmSnapshot> _snapshots = [];
  bool _loading = true;
  String? _error;

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
      final list = await widget.vmClient.listSnapshots(widget.vmName);
      if (!mounted) return;
      setState(() {
        _snapshots = list;
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

  Future<void> _createSnapshot() async {
    final ctrl = TextEditingController(text: 'snap-${DateTime.now().millisecondsSinceEpoch ~/ 1000}');
    final name = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceContainerHighest,
        title: const Text('Create VM Snapshot'),
        content: TextField(
          controller: ctrl,
          decoration: const InputDecoration(labelText: 'Snapshot Name'),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.primary),
            onPressed: () => Navigator.pop(context, ctrl.text.trim()),
            child: const Text('Create'),
          ),
        ],
      ),
    );
    if (name == null || name.isEmpty) return;
    try {
      await widget.vmClient.createSnapshot(widget.vmName, snapName: name);
      _load();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Failed: $e'), backgroundColor: NivaroColors.danger));
      }
    }
  }

  Future<void> _restoreSnapshot(String name) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceContainerHighest,
        title: Text('Revert to $name?'),
        content: const Text('Current VM disk state will be replaced with this snapshot state.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.warning),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Revert State'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      await widget.vmClient.revertSnapshot(widget.vmName, name);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Reverted to snapshot $name')));
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Failed: $e'), backgroundColor: NivaroColors.danger));
      }
    }
  }

  Future<void> _deleteSnapshot(String name) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceContainerHighest,
        title: Text('Delete $name?'),
        content: const Text('This snapshot point will be deleted.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.danger),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      await widget.vmClient.deleteSnapshot(widget.vmName, name);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Deleted snapshot $name')));
      }
      _load();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Failed: $e'), backgroundColor: NivaroColors.danger));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 16, 20, 20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Center(child: Container(width: 36, height: 4, decoration: BoxDecoration(color: NivaroColors.borderHighlight, borderRadius: BorderRadius.circular(2)))),
            const SizedBox(height: 16),
            Row(
              children: [
                const Expanded(
                  child: Text('VM Snapshots', style: TextStyle(fontWeight: FontWeight.w800, fontSize: 18, color: Colors.white)),
                ),
                FilledButton.icon(
                  style: FilledButton.styleFrom(
                    backgroundColor: NivaroColors.primary,
                    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                  ),
                  icon: const Icon(Icons.add_a_photo_rounded, size: 16),
                  label: const Text('Create'),
                  onPressed: _createSnapshot,
                ),
              ],
            ),
            const SizedBox(height: 14),
            if (_loading)
              const Center(child: Padding(padding: EdgeInsets.all(20), child: CircularProgressIndicator()))
            else if (_error != null)
              Center(
                child: Padding(
                  padding: const EdgeInsets.all(12),
                  child: Text(_error!, style: const TextStyle(color: NivaroColors.dangerLight)),
                ),
              )
            else if (_snapshots.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 24),
                child: Center(
                  child: Text('No snapshots created yet.', style: TextStyle(color: NivaroColors.textMuted)),
                ),
              )
            else
              Flexible(
                child: ListView.separated(
                  shrinkWrap: true,
                  itemCount: _snapshots.length,
                  separatorBuilder: (_, __) => const Divider(height: 1, color: NivaroColors.borderSubtle),
                  itemBuilder: (context, index) {
                    final snap = _snapshots[index];
                    return ListTile(
                      contentPadding: EdgeInsets.zero,
                      leading: Container(
                        width: 38,
                        height: 38,
                        decoration: BoxDecoration(color: NivaroColors.primary.withOpacity(0.15), borderRadius: BorderRadius.circular(10)),
                        child: const Icon(Icons.camera_alt_rounded, color: NivaroColors.primaryLight, size: 20),
                      ),
                      title: Text(snap.name, style: const TextStyle(fontWeight: FontWeight.w700)),
                      subtitle: Text(snap.state, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
                      trailing: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          IconButton(
                            icon: const Icon(Icons.restore_rounded, color: NivaroColors.warningLight),
                            tooltip: 'Revert to snapshot',
                            onPressed: () {
                              Navigator.pop(context);
                              _restoreSnapshot(snap.name);
                            },
                          ),
                          IconButton(
                            icon: const Icon(Icons.delete_outline_rounded, color: NivaroColors.dangerLight),
                            tooltip: 'Delete snapshot',
                            onPressed: () => _deleteSnapshot(snap.name),
                          ),
                        ],
                      ),
                    );
                  },
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _IsoSheet extends StatefulWidget {
  final String vmName;
  final VmClient vmClient;
  final VoidCallback onUpdated;
  const _IsoSheet({required this.vmName, required this.vmClient, required this.onUpdated});

  @override
  State<_IsoSheet> createState() => _IsoSheetState();
}

class _IsoSheetState extends State<_IsoSheet> {
  List<VmIso> _isos = [];
  bool _loading = true;
  String? _error;

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
      final list = await widget.vmClient.listIsos();
      if (!mounted) return;
      setState(() {
        _isos = list;
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

  Future<void> _insertIso(String path) async {
    try {
      await widget.vmClient.insertCDROM(widget.vmName, path);
      if (mounted) {
        Navigator.pop(context);
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Inserted ISO into CD-ROM drive')));
      }
      widget.onUpdated();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Failed: $e'), backgroundColor: NivaroColors.danger));
      }
    }
  }

  Future<void> _ejectIso() async {
    try {
      await widget.vmClient.ejectCDROM(widget.vmName);
      if (mounted) {
        Navigator.pop(context);
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Ejected CD-ROM')));
      }
      widget.onUpdated();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Failed: $e'), backgroundColor: NivaroColors.danger));
      }
    }
  }

  Future<void> _insertVirtioWin() async {
    try {
      await widget.vmClient.insertVirtioWin(widget.vmName);
      if (mounted) {
        Navigator.pop(context);
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Inserted NivaroOS Guest Tools ISO')));
      }
      widget.onUpdated();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Failed: $e'), backgroundColor: NivaroColors.danger));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 16, 20, 20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Center(child: Container(width: 36, height: 4, decoration: BoxDecoration(color: NivaroColors.borderHighlight, borderRadius: BorderRadius.circular(2)))),
            const SizedBox(height: 16),
            const Text('Virtual CD-ROM / ISO', style: TextStyle(fontWeight: FontWeight.w800, fontSize: 18, color: Colors.white)),
            const SizedBox(height: 14),
            Row(
              children: [
                Expanded(
                  child: OutlinedButton.icon(
                    icon: const Icon(Icons.eject_rounded, size: 18),
                    label: const Text('Eject CD-ROM'),
                    onPressed: _ejectIso,
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: FilledButton.icon(
                    style: FilledButton.styleFrom(backgroundColor: NivaroColors.primary),
                    icon: const Icon(Icons.album_rounded, size: 18),
                    label: const Text('Guest Tools'),
                    onPressed: _insertVirtioWin,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 16),
            const Text('Available Host ISOs', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 14, color: NivaroColors.textSecondary)),
            const SizedBox(height: 8),
            if (_loading)
              const Center(child: Padding(padding: EdgeInsets.all(16), child: CircularProgressIndicator()))
            else if (_error != null)
              Text(_error!, style: const TextStyle(color: NivaroColors.dangerLight))
            else if (_isos.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 16),
                child: Text('No ISO images found in /DATA/ISOs', style: TextStyle(color: NivaroColors.textMuted)),
              )
            else
              Flexible(
                child: ListView.separated(
                  shrinkWrap: true,
                  itemCount: _isos.length,
                  separatorBuilder: (_, __) => const Divider(height: 1, color: NivaroColors.borderSubtle),
                  itemBuilder: (context, index) {
                    final iso = _isos[index];
                    return ListTile(
                      contentPadding: EdgeInsets.zero,
                      leading: const Icon(Icons.disc_full_rounded, color: NivaroColors.primaryLight),
                      title: Text(iso.name, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13.5)),
                      trailing: TextButton(
                        onPressed: () => _insertIso(iso.path),
                        child: const Text('Mount', style: TextStyle(fontWeight: FontWeight.bold)),
                      ),
                    );
                  },
                ),
              ),
          ],
        ),
      ),
    );
  }
}
