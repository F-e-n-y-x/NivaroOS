import 'package:flutter/material.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../services/vm_client.dart';
import 'vm_console_screen.dart';

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
    setState(() => _busy.add(vm.name));
    try {
      await action(vm.name);
      await Future.delayed(const Duration(milliseconds: 400));
      await _load();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(e.toString().replaceFirst('Exception: ', '')), backgroundColor: NivaroColors.danger),
        );
      }
    } finally {
      if (mounted) setState(() => _busy.remove(vm.name));
    }
  }

  Color _stateColor(String state) {
    switch (state) {
      case 'running':
        return NivaroColors.success;
      case 'paused':
        return NivaroColors.warning;
      default:
        return NivaroColors.textMuted;
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Virtual Machines'),
        actions: [IconButton(onPressed: _load, icon: const Icon(Icons.refresh))],
      ),
      body: RefreshIndicator(
        onRefresh: _load,
        child: _loading
            ? const Center(child: CircularProgressIndicator())
            : _error != null
                ? Center(
                    child: Padding(
                      padding: const EdgeInsets.all(24),
                      child: Text(_error!, style: const TextStyle(color: NivaroColors.textMuted), textAlign: TextAlign.center),
                    ),
                  )
                : _vms.isEmpty
                    ? const Center(child: Text('No virtual machines yet.', style: TextStyle(color: NivaroColors.textMuted)))
                    : ListView.builder(
                        padding: const EdgeInsets.all(12),
                        itemCount: _vms.length,
                        itemBuilder: (context, i) {
                          final vm = _vms[i];
                          final busy = _busy.contains(vm.name);
                          return Card(
                            margin: const EdgeInsets.only(bottom: 10),
                            child: Padding(
                              padding: const EdgeInsets.all(14),
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Row(
                                    children: [
                                      Container(width: 8, height: 8, decoration: BoxDecoration(color: _stateColor(vm.state), shape: BoxShape.circle)),
                                      const SizedBox(width: 8),
                                      Expanded(child: Text(vm.name, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 16))),
                                      Text(vm.state, style: TextStyle(color: _stateColor(vm.state), fontSize: 12, fontWeight: FontWeight.w600)),
                                    ],
                                  ),
                                  const SizedBox(height: 6),
                                  Text(
                                    '${vm.vcpus} vCPU · ${(vm.memoryMib / 1024).toStringAsFixed(1)} GB RAM · ${vm.diskGib} GB disk',
                                    style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12.5),
                                  ),
                                  const SizedBox(height: 12),
                                  Row(
                                    children: [
                                      if (vm.isRunning) ...[
                                        Expanded(
                                          child: OutlinedButton.icon(
                                            onPressed: busy
                                                ? null
                                                : () => Navigator.of(context).push(
                                                      MaterialPageRoute(builder: (_) => VmConsoleScreen(vmName: vm.name)),
                                                    ),
                                            icon: const Icon(Icons.monitor_outlined, size: 18),
                                            label: const Text('Console'),
                                          ),
                                        ),
                                        const SizedBox(width: 8),
                                        Expanded(
                                          child: OutlinedButton.icon(
                                            onPressed: busy ? null : () => _act(vm, _client.shutdown),
                                            icon: const Icon(Icons.power_settings_new, size: 18, color: NivaroColors.danger),
                                            label: const Text('Stop', style: TextStyle(color: NivaroColors.danger)),
                                          ),
                                        ),
                                      ] else
                                        Expanded(
                                          child: ElevatedButton.icon(
                                            onPressed: busy ? null : () => _act(vm, _client.start),
                                            icon: busy
                                                ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                                                : const Icon(Icons.play_arrow, size: 18),
                                            label: const Text('Start'),
                                          ),
                                        ),
                                    ],
                                  ),
                                ],
                              ),
                            ),
                          );
                        },
                      ),
      ),
    );
  }
}
