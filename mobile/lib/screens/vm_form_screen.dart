import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../services/vm_client.dart';
import '../theme.dart';
import '../widgets/common.dart';

/// Create or edit a VM with OS preset quick-selection, resource sliders,
/// ISO boot picker, and firmware/network configuration.
class VmFormScreen extends StatefulWidget {
  final VmClient client;
  final Vm? existing;
  const VmFormScreen({super.key, required this.client, this.existing});

  @override
  State<VmFormScreen> createState() => _VmFormScreenState();
}

class _VmFormScreenState extends State<VmFormScreen> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _name;
  late final TextEditingController _vcpus;
  late final TextEditingController _memoryGb;
  late final TextEditingController _diskGb;
  late final TextEditingController _bridgeName;
  String _networkMode = 'nat';
  String _firmware = 'bios';
  String? _isoPath;
  List<VmIso> _isos = [];
  bool _loadingIsos = true;
  bool _saving = false;
  String? _error;

  bool get _isEdit => widget.existing != null;

  @override
  void initState() {
    super.initState();
    final e = widget.existing;
    _name = TextEditingController(text: e?.name ?? '');
    _vcpus = TextEditingController(text: (e?.vcpus ?? 2).toString());
    _memoryGb = TextEditingController(text: e == null ? '2.0' : (e.memoryMib / 1024).toStringAsFixed(1));
    _diskGb = TextEditingController(text: (e?.diskGib ?? 20).toString());
    _bridgeName = TextEditingController(text: e?.networks.isNotEmpty == true ? (e!.networks.first.bridgeName ?? '') : '');
    _networkMode = e?.networkMode.isNotEmpty == true ? e!.networkMode : 'nat';
    _firmware = e?.firmware ?? 'bios';
    _isoPath = e?.isoPath;
    _loadIsos();
  }

  Future<void> _loadIsos() async {
    try {
      final isos = await widget.client.listIsos();
      if (!mounted) return;
      setState(() {
        _isos = isos;
        _loadingIsos = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _loadingIsos = false);
    }
  }

  @override
  void dispose() {
    _name.dispose();
    _vcpus.dispose();
    _memoryGb.dispose();
    _diskGb.dispose();
    _bridgeName.dispose();
    super.dispose();
  }

  void _applyPreset(String name, int vcpus, double memGb, int diskGb, String fw) {
    HapticFeedback.selectionClick();
    setState(() {
      if (!_isEdit && _name.text.isEmpty) {
        _name.text = name.toLowerCase().replaceAll(' ', '-');
      }
      _vcpus.text = vcpus.toString();
      _memoryGb.text = memGb.toStringAsFixed(1);
      _diskGb.text = diskGb.toString();
      _firmware = fw;
    });
  }

  Future<void> _save() async {
    if (!_formKey.currentState!.validate()) return;
    HapticFeedback.mediumImpact();
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final memoryMib = (double.parse(_memoryGb.text) * 1024).round();
      if (_isEdit) {
        await widget.client.updateVm(
          widget.existing!.name,
          vcpus: int.parse(_vcpus.text),
          memoryMib: memoryMib,
          firmware: _firmware,
        );
      } else {
        await widget.client.createVm(
          name: _name.text.trim(),
          vcpus: int.parse(_vcpus.text),
          memoryMib: memoryMib,
          diskGib: int.tryParse(_diskGb.text),
          isoPath: _isoPath,
          networkMode: _networkMode,
          bridgeName: _networkMode == 'bridge' ? _bridgeName.text.trim() : null,
          firmware: _firmware,
        );
      }
      if (mounted) Navigator.of(context).pop(true);
    } catch (e) {
      setState(() => _error = e.toString().replaceFirst('Exception: ', ''));
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Column(
          children: [
            // Top Bar
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 20, 12),
              child: Row(
                children: [
                  IconButton(
                    icon: const Icon(Icons.arrow_back_rounded),
                    onPressed: () => Navigator.of(context).pop(),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      _isEdit ? 'Edit Virtual Machine' : 'Create Virtual Machine',
                      style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                            fontWeight: FontWeight.w800,
                            letterSpacing: -0.5,
                          ),
                    ),
                  ),
                ],
              ),
            ),
            const Divider(height: 1),

            // Form Fields
            Expanded(
              child: Form(
                key: _formKey,
                child: ListView(
                  padding: const EdgeInsets.fromLTRB(20, 16, 20, 40),
                  children: [
                    // Presets Bar (on create only)
                    if (!_isEdit) ...[
                      const SectionHeader(title: 'Quick OS Presets'),
                      const SizedBox(height: 10),
                      SingleChildScrollView(
                        scrollDirection: Axis.horizontal,
                        child: Row(
                          children: [
                            _presetChip('Ubuntu / Debian', Icons.terminal_rounded, () => _applyPreset('ubuntu', 2, 2.0, 25, 'bios')),
                            const SizedBox(width: 8),
                            _presetChip('Windows 11', Icons.window_rounded, () => _applyPreset('win11', 4, 4.0, 64, 'uefi')),
                            const SizedBox(width: 8),
                            _presetChip('Alpine Linux', Icons.speed_rounded, () => _applyPreset('alpine', 1, 1.0, 10, 'bios')),
                            const SizedBox(width: 8),
                            _presetChip('Docker Host', Icons.layers_rounded, () => _applyPreset('docker-vm', 4, 8.0, 40, 'bios')),
                          ],
                        ),
                      ),
                      const SizedBox(height: 20),
                    ],

                    // General Card
                    const SectionHeader(title: 'General Details'),
                    const SizedBox(height: 10),
                    DarkCard(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          TextFormField(
                            controller: _name,
                            enabled: !_isEdit,
                            decoration: const InputDecoration(
                              labelText: 'VM Name',
                              hintText: 'e.g. ubuntu-server, windows11',
                              prefixIcon: Icon(Icons.badge_rounded, size: 20),
                              filled: true,
                              fillColor: NivaroColors.surfaceRaised,
                            ),
                            validator: (v) {
                              if (v == null || v.trim().isEmpty) return 'Name is required';
                              if (!RegExp(r'^[a-zA-Z0-9_-]+$').hasMatch(v.trim())) {
                                return 'Letters, numbers, - and _ only';
                              }
                              return null;
                            },
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(height: 20),

                    // Compute & Memory Card
                    const SectionHeader(title: 'Compute & Memory'),
                    const SizedBox(height: 10),
                    DarkCard(
                      child: Column(
                        children: [
                          Row(
                            children: [
                              Expanded(
                                child: TextFormField(
                                  controller: _vcpus,
                                  keyboardType: TextInputType.number,
                                  decoration: const InputDecoration(
                                    labelText: 'vCPU Cores',
                                    prefixIcon: Icon(Icons.memory_rounded, size: 20),
                                    filled: true,
                                    fillColor: NivaroColors.surfaceRaised,
                                  ),
                                  validator: (v) {
                                    final val = int.tryParse(v ?? '');
                                    if (val == null || val <= 0) return 'Valid CPU count required';
                                    return null;
                                  },
                                ),
                              ),
                              const SizedBox(width: 14),
                              Expanded(
                                child: TextFormField(
                                  controller: _memoryGb,
                                  keyboardType: const TextInputType.numberWithOptions(decimal: true),
                                  decoration: const InputDecoration(
                                    labelText: 'RAM (GB)',
                                    prefixIcon: Icon(Icons.speed_rounded, size: 20),
                                    filled: true,
                                    fillColor: NivaroColors.surfaceRaised,
                                  ),
                                  validator: (v) {
                                    final val = double.tryParse(v ?? '');
                                    if (val == null || val <= 0) return 'Valid RAM required';
                                    return null;
                                  },
                                ),
                              ),
                            ],
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(height: 20),

                    // Storage & Media Card
                    if (!_isEdit) ...[
                      const SectionHeader(title: 'Storage & Boot Media'),
                      const SizedBox(height: 10),
                      DarkCard(
                        child: Column(
                          children: [
                            TextFormField(
                              controller: _diskGb,
                              keyboardType: TextInputType.number,
                              decoration: const InputDecoration(
                                labelText: 'Main Disk Size (GB)',
                                prefixIcon: Icon(Icons.storage_rounded, size: 20),
                                filled: true,
                                fillColor: NivaroColors.surfaceRaised,
                              ),
                              validator: (v) {
                                final val = int.tryParse(v ?? '');
                                if (val == null || val <= 0) return 'Valid disk size required';
                                return null;
                              },
                            ),
                            const SizedBox(height: 14),
                            if (_loadingIsos)
                              const LinearProgressIndicator()
                            else
                              DropdownButtonFormField<String?>(
                                dropdownColor: NivaroColors.surfaceRaised,
                                value: _isoPath,
                                decoration: const InputDecoration(
                                  labelText: 'Boot ISO Image (optional)',
                                  prefixIcon: Icon(Icons.album_rounded, size: 20),
                                  filled: true,
                                  fillColor: NivaroColors.surfaceRaised,
                                ),
                                items: [
                                  const DropdownMenuItem(value: null, child: Text('None (Network / Existing Disk)')),
                                  ..._isos.map(
                                    (iso) => DropdownMenuItem(
                                      value: iso.path,
                                      child: Text(iso.name, overflow: TextOverflow.ellipsis),
                                    ),
                                  ),
                                ],
                                onChanged: (v) => setState(() => _isoPath = v),
                              ),
                          ],
                        ),
                      ),
                      const SizedBox(height: 20),
                    ],

                    // Firmware & Network Card
                    const SectionHeader(title: 'Firmware & Network'),
                    const SizedBox(height: 10),
                    DarkCard(
                      child: Column(
                        children: [
                          DropdownButtonFormField<String>(
                            dropdownColor: NivaroColors.surfaceRaised,
                            value: _firmware,
                            decoration: const InputDecoration(
                              labelText: 'Boot Firmware',
                              prefixIcon: Icon(Icons.developer_mode_rounded, size: 20),
                              filled: true,
                              fillColor: NivaroColors.surfaceRaised,
                            ),
                            items: const [
                              DropdownMenuItem(value: 'bios', child: Text('Legacy BIOS')),
                              DropdownMenuItem(value: 'uefi', child: Text('UEFI (Windows 11 / Modern OS)')),
                            ],
                            onChanged: _isEdit ? null : (v) => setState(() => _firmware = v ?? 'bios'),
                          ),
                          if (!_isEdit) ...[
                            const SizedBox(height: 14),
                            DropdownButtonFormField<String>(
                              dropdownColor: NivaroColors.surfaceRaised,
                              value: _networkMode,
                              decoration: const InputDecoration(
                                labelText: 'Network Mode',
                                prefixIcon: Icon(Icons.lan_rounded, size: 20),
                                filled: true,
                                fillColor: NivaroColors.surfaceRaised,
                              ),
                              items: const [
                                DropdownMenuItem(value: 'nat', child: Text('NAT (Shared Host Network)')),
                                DropdownMenuItem(value: 'bridge', child: Text('Bridged (Direct LAN IP)')),
                              ],
                              onChanged: (v) => setState(() => _networkMode = v ?? 'nat'),
                            ),
                            if (_networkMode == 'bridge') ...[
                              const SizedBox(height: 14),
                              TextFormField(
                                controller: _bridgeName,
                                decoration: const InputDecoration(
                                  labelText: 'Bridge Interface Name',
                                  hintText: 'e.g. br0, virbr0',
                                  prefixIcon: Icon(Icons.router_rounded, size: 20),
                                  filled: true,
                                  fillColor: NivaroColors.surfaceRaised,
                                ),
                              ),
                            ],
                          ],
                        ],
                      ),
                    ),

                    if (_error != null) ...[
                      const SizedBox(height: 16),
                      DarkCard(
                        color: NivaroColors.danger.withValues(alpha: 0.15),
                        child: Row(
                          children: [
                            const Icon(Icons.error_outline_rounded, color: NivaroColors.danger),
                            const SizedBox(width: 12),
                            Expanded(child: Text(_error!, style: const TextStyle(color: NivaroColors.danger, fontSize: 13))),
                          ],
                        ),
                      ),
                    ],

                    const SizedBox(height: 28),
                    SizedBox(
                      width: double.infinity,
                      height: 50,
                      child: FilledButton(
                        onPressed: _saving ? null : _save,
                        style: FilledButton.styleFrom(
                          backgroundColor: NivaroColors.primary,
                          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
                        ),
                        child: _saving
                            ? const SizedBox(width: 22, height: 22, child: CircularProgressIndicator(strokeWidth: 2.5, color: Colors.white))
                            : Text(_isEdit ? 'Save Changes' : 'Create & Provision VM', style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 15)),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _presetChip(String label, IconData icon, VoidCallback onTap) {
    return ActionChip(
      avatar: Icon(icon, size: 16, color: NivaroColors.primaryLight),
      label: Text(label),
      labelStyle: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
      backgroundColor: NivaroColors.surfaceRaised,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(NivaroShape.full),
        side: const BorderSide(color: NivaroColors.borderSubtle),
      ),
      onPressed: onTap,
    );
  }
}

