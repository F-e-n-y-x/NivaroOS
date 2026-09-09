import 'package:flutter/material.dart';
import '../theme.dart';
import '../services/vm_client.dart';
import '../widgets/common.dart';

class VmFormScreen extends StatefulWidget {
  final VmClient client;
  final Vm? existing;

  const VmFormScreen({super.key, required this.client, this.existing});

  @override
  State<VmFormScreen> createState() => _VmFormScreenState();
}

class _VmFormScreenState extends State<VmFormScreen> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _nameCtrl;
  late final TextEditingController _bridgeCtrl;

  int _vcpus = 2;
  int _ramGb = 2;
  int _diskGb = 20;
  String _diskBus = 'virtio';
  bool _diskSsd = true;
  String _firmware = 'uefi';
  String _networkMode = 'bridge';
  String _nicModel = 'virtio';
  String? _selectedIso;
  int _displayWidth = 0;
  int _displayHeight = 0;
  bool _bootCdromFirst = true;

  List<VmIso> _isos = [];
  bool _loadingIsos = false;
  bool _saving = false;

  final _presets = [
    (name: 'Windows 11', vcpus: 4, ram: 4, disk: 60, uefi: true, bus: 'virtio', icon: Icons.window_rounded, color: const Color(0xFF0284C7)),
    (name: 'Windows 10', vcpus: 4, ram: 4, disk: 50, uefi: true, bus: 'virtio', icon: Icons.window_rounded, color: const Color(0xFF0284C7)),
    (name: 'Ubuntu 24.04', vcpus: 2, ram: 2, disk: 25, uefi: true, bus: 'virtio', icon: Icons.terminal_rounded, color: const Color(0xFFE95420)),
    (name: 'Debian 12', vcpus: 2, ram: 2, disk: 20, uefi: true, bus: 'virtio', icon: Icons.terminal_rounded, color: const Color(0xFFD70A53)),
    (name: 'Linux Mint', vcpus: 2, ram: 2, disk: 20, uefi: true, bus: 'virtio', icon: Icons.terminal_rounded, color: const Color(0xFF87CF3E)),
    (name: 'Arch Linux', vcpus: 2, ram: 2, disk: 20, uefi: true, bus: 'virtio', icon: Icons.terminal_rounded, color: const Color(0xFF1793D1)),
  ];

  final _resolutions = [
    (label: 'Auto / Default', width: 0, height: 0),
    (label: '1920 × 1080 (FHD)', width: 1920, height: 1080),
    (label: '1600 × 900 (HD+)', width: 1600, height: 900),
    (label: '1366 × 768 (WXGA)', width: 1366, height: 768),
    (label: '1280 × 720 (720p)', width: 1280, height: 720),
    (label: '1024 × 768 (XGA)', width: 1024, height: 768),
  ];

  @override
  void initState() {
    super.initState();
    final ex = widget.existing;
    _nameCtrl = TextEditingController(text: ex?.name ?? '');
    _bridgeCtrl = TextEditingController(text: (ex?.networks.isNotEmpty == true) ? (ex!.networks.first.bridgeName ?? 'br0') : 'br0');

    if (ex != null) {
      _vcpus = ex.vcpus > 0 ? ex.vcpus : 2;
      _ramGb = (ex.memoryMib / 1024).round().clamp(1, 64);
      _diskGb = ex.diskGib > 0 ? ex.diskGib : 20;
      _firmware = ex.firmware;
      _selectedIso = ex.isoPath;
      _displayWidth = ex.displayWidth;
      _displayHeight = ex.displayHeight;

      if (ex.disks.isNotEmpty) {
        _diskBus = ex.disks.first.bus;
        _diskSsd = ex.disks.first.ssd;
      }
      if (ex.networks.isNotEmpty) {
        _networkMode = ex.networks.first.mode;
        _nicModel = ex.networks.first.model;
      }
    }
    _loadIsos();
  }

  @override
  void dispose() {
    _nameCtrl.dispose();
    _bridgeCtrl.dispose();
    super.dispose();
  }

  Future<void> _loadIsos() async {
    setState(() => _loadingIsos = true);
    try {
      final list = await widget.client.listIsos();
      if (mounted) setState(() => _isos = list);
    } catch (_) {
    } finally {
      if (mounted) setState(() => _loadingIsos = false);
    }
  }

  void _applyPreset(dynamic p) {
    setState(() {
      if (_nameCtrl.text.isEmpty) _nameCtrl.text = p.name.toString().replaceAll(' ', '-').toLowerCase();
      _vcpus = p.vcpus;
      _ramGb = p.ram;
      _diskGb = p.disk;
      _diskBus = p.bus;
      _firmware = p.uefi ? 'uefi' : 'bios';
    });
  }

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate()) return;
    setState(() => _saving = true);

    final bootOrder = _bootCdromFirst && _selectedIso != null && _selectedIso!.isNotEmpty
        ? ['cdrom', 'vda', 'network']
        : ['vda', 'cdrom', 'network'];

    try {
      if (widget.existing != null) {
        await widget.client.updateVm(
          widget.existing!.name,
          vcpus: _vcpus,
          memoryMib: _ramGb * 1024,
          diskGib: _diskGb,
          diskBus: _diskBus,
          ssd: _diskSsd,
          isoPath: _selectedIso,
          networkMode: _networkMode,
          bridgeName: _networkMode == 'bridge' ? _bridgeCtrl.text.trim() : null,
          nicModel: _nicModel,
          firmware: _firmware,
          displayWidth: _displayWidth > 0 ? _displayWidth : null,
          displayHeight: _displayHeight > 0 ? _displayHeight : null,
          bootOrder: bootOrder,
        );
      } else {
        await widget.client.createVm(
          name: _nameCtrl.text.trim(),
          vcpus: _vcpus,
          memoryMib: _ramGb * 1024,
          diskGib: _diskGb,
          diskBus: _diskBus,
          ssd: _diskSsd,
          isoPath: _selectedIso,
          networkMode: _networkMode,
          bridgeName: _networkMode == 'bridge' ? _bridgeCtrl.text.trim() : null,
          nicModel: _nicModel,
          firmware: _firmware,
          displayWidth: _displayWidth > 0 ? _displayWidth : null,
          displayHeight: _displayHeight > 0 ? _displayHeight : null,
          bootOrder: bootOrder,
        );
      }
      if (mounted) Navigator.pop(context, true);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(e.toString().replaceFirst('Exception: ', '')), backgroundColor: NivaroColors.danger),
        );
      }
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final isEdit = widget.existing != null;

    return Scaffold(
      backgroundColor: NivaroColors.background,
      appBar: AppBar(
        title: Text(isEdit ? 'Configure: ${widget.existing!.name}' : 'New Virtual Machine', style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 17)),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh_rounded),
            tooltip: 'Reload ISOs',
            onPressed: _loadIsos,
          ),
        ],
      ),
      body: Form(
        key: _formKey,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 80),
          children: [
            if (!isEdit) ...[
              const SectionHeader(title: 'Quick OS Presets', subtitle: 'Auto-fill recommended specs'),
              SizedBox(
                height: 64,
                child: ListView.builder(
                  scrollDirection: Axis.horizontal,
                  itemCount: _presets.length,
                  itemBuilder: (context, i) {
                    final p = _presets[i];
                    return Padding(
                      padding: const EdgeInsets.only(right: 8),
                      child: InkWell(
                        onTap: () => _applyPreset(p),
                        borderRadius: BorderRadius.circular(14),
                        child: Container(
                          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                          decoration: BoxDecoration(
                            color: NivaroColors.surfaceContainerLow,
                            borderRadius: BorderRadius.circular(14),
                            border: Border.all(color: NivaroColors.borderSubtle),
                          ),
                          child: Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Container(
                                width: 32,
                                height: 32,
                                decoration: BoxDecoration(
                                  color: p.color.withOpacity(0.15),
                                  borderRadius: BorderRadius.circular(8),
                                ),
                                alignment: Alignment.center,
                                child: Icon(p.icon, color: p.color, size: 17),
                              ),
                              const SizedBox(width: 8),
                              Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                mainAxisAlignment: MainAxisAlignment.center,
                                children: [
                                  Text(p.name, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 12, color: Colors.white)),
                                  Text('${p.vcpus}C · ${p.ram}GB RAM', style: const TextStyle(color: NivaroColors.textMuted, fontSize: 10.5)),
                                ],
                              ),
                            ],
                          ),
                        ),
                      ),
                    );
                  },
                ),
              ),
              const SizedBox(height: 18),
            ],

            const SectionHeader(title: 'General Information'),
            DarkCard(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  TextFormField(
                    controller: _nameCtrl,
                    enabled: !isEdit,
                    style: const TextStyle(fontSize: 14),
                    decoration: const InputDecoration(
                      labelText: 'Virtual Machine Name',
                      hintText: 'e.g. windows-11, debian-server',
                      prefixIcon: Icon(Icons.computer_rounded, size: 19),
                    ),
                    validator: (v) {
                      if (v == null || v.trim().isEmpty) return 'Name is required';
                      if (!RegExp(r'^[a-zA-Z0-9_-]+$').hasMatch(v.trim())) {
                        return 'Only letters, numbers, hyphens and underscores allowed';
                      }
                      return null;
                    },
                  ),
                  const SizedBox(height: 16),
                  const Text('Firmware Boot Architecture', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 13, color: NivaroColors.textPrimary)),
                  const SizedBox(height: 8),
                  Row(
                    children: [
                      Expanded(
                        child: ChoiceChip(
                          label: const Center(child: Text('UEFI (OVMF)')),
                          selected: _firmware == 'uefi',
                          onSelected: (s) => setState(() => _firmware = 'uefi'),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: ChoiceChip(
                          label: const Center(child: Text('Legacy BIOS')),
                          selected: _firmware == 'bios',
                          onSelected: (s) => setState(() => _firmware = 'bios'),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 16),
                  const Text('Display Resolution Mode', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 13, color: NivaroColors.textPrimary)),
                  const SizedBox(height: 8),
                  DropdownButtonFormField<int>(
                    value: _resolutions.indexWhere((r) => r.width == _displayWidth && r.height == _displayHeight).clamp(0, _resolutions.length - 1),
                    isExpanded: true,
                    decoration: const InputDecoration(
                      prefixIcon: Icon(Icons.display_settings_rounded, size: 19),
                    ),
                    items: List.generate(_resolutions.length, (idx) {
                      final r = _resolutions[idx];
                      return DropdownMenuItem<int>(
                        value: idx,
                        child: Text(r.label, style: const TextStyle(fontSize: 13), overflow: TextOverflow.ellipsis),
                      );
                    }),
                    onChanged: (idx) {
                      if (idx != null) {
                        final r = _resolutions[idx];
                        setState(() {
                          _displayWidth = r.width;
                          _displayHeight = r.height;
                        });
                      }
                    },
                  ),
                ],
              ),
            ),
            const SizedBox(height: 18),

            const SectionHeader(title: 'Resource Allocation'),
            DarkCard(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      const Text('vCPU Cores', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5)),
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 3),
                        decoration: BoxDecoration(color: NivaroColors.primary.withOpacity(0.15), borderRadius: BorderRadius.circular(8)),
                        child: Text('$_vcpus Cores', style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 12, color: NivaroColors.primaryLight)),
                      ),
                    ],
                  ),
                  Slider(
                    value: _vcpus.toDouble(),
                    min: 1,
                    max: 16,
                    divisions: 15,
                    label: '$_vcpus Cores',
                    onChanged: (v) => setState(() => _vcpus = v.round()),
                  ),
                  Wrap(
                    spacing: 6,
                    runSpacing: 6,
                    alignment: WrapAlignment.center,
                    children: [1, 2, 4, 8, 12, 16].map((c) {
                      return ChoiceChip(
                        label: Text('${c}C', style: const TextStyle(fontSize: 11, fontWeight: FontWeight.bold)),
                        selected: _vcpus == c,
                        onSelected: (_) => setState(() => _vcpus = c),
                        visualDensity: VisualDensity.compact,
                      );
                    }).toList(),
                  ),
                  const Divider(height: 24),
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      const Text('Memory (RAM)', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5)),
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 3),
                        decoration: BoxDecoration(color: NivaroColors.purpleLight.withOpacity(0.15), borderRadius: BorderRadius.circular(8)),
                        child: Text('$_ramGb GB RAM', style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 12, color: NivaroColors.purpleLight)),
                      ),
                    ],
                  ),
                  Slider(
                    value: _ramGb.toDouble(),
                    min: 1,
                    max: 32,
                    divisions: 31,
                    label: '$_ramGb GB',
                    onChanged: (v) => setState(() => _ramGb = v.round()),
                  ),
                  Wrap(
                    spacing: 6,
                    runSpacing: 6,
                    alignment: WrapAlignment.center,
                    children: [1, 2, 4, 8, 16, 32].map((gb) {
                      return ChoiceChip(
                        label: Text('${gb}G', style: const TextStyle(fontSize: 11, fontWeight: FontWeight.bold)),
                        selected: _ramGb == gb,
                        onSelected: (_) => setState(() => _ramGb = gb),
                        visualDensity: VisualDensity.compact,
                      );
                    }).toList(),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 18),

            const SectionHeader(title: 'Virtual Storage & Bus'),
            DarkCard(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      const Text('Primary Virtual Disk', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5)),
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 3),
                        decoration: BoxDecoration(color: NivaroColors.success.withOpacity(0.15), borderRadius: BorderRadius.circular(8)),
                        child: Text('$_diskGb GB', style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 12, color: NivaroColors.successLight)),
                      ),
                    ],
                  ),
                  Slider(
                    value: _diskGb.toDouble().clamp(10, 500),
                    min: 10,
                    max: 300,
                    divisions: 29,
                    label: '$_diskGb GB',
                    onChanged: (v) => setState(() => _diskGb = v.round()),
                  ),
                  const SizedBox(height: 8),
                  const Text('Disk Controller Bus', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 13, color: NivaroColors.textPrimary)),
                  const SizedBox(height: 8),
                  Row(
                    children: [
                      Expanded(
                        child: ChoiceChip(
                          label: const Center(child: Text('VirtIO')),
                          selected: _diskBus == 'virtio',
                          onSelected: (s) => setState(() => _diskBus = 'virtio'),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: ChoiceChip(
                          label: const Center(child: Text('SATA')),
                          selected: _diskBus == 'sata',
                          onSelected: (s) => setState(() => _diskBus = 'sata'),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: ChoiceChip(
                          label: const Center(child: Text('IDE')),
                          selected: _diskBus == 'ide',
                          onSelected: (s) => setState(() => _diskBus = 'ide'),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 12),
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('SSD Emulation (TRIM / Discard)', style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600)),
                    subtitle: const Text('Allows guest OS to reclaim deleted blocks', style: TextStyle(fontSize: 11, color: NivaroColors.textMuted)),
                    value: _diskSsd,
                    onChanged: (v) => setState(() => _diskSsd = v),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 18),

            const SectionHeader(title: 'Virtual Network'),
            DarkCard(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: ChoiceChip(
                          label: const Center(child: Text('Bridged (LAN)')),
                          selected: _networkMode == 'bridge',
                          onSelected: (s) => setState(() => _networkMode = 'bridge'),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: ChoiceChip(
                          label: const Center(child: Text('NAT (Isolated)')),
                          selected: _networkMode == 'nat',
                          onSelected: (s) => setState(() => _networkMode = 'nat'),
                        ),
                      ),
                    ],
                  ),
                  if (_networkMode == 'bridge') ...[
                    const SizedBox(height: 14),
                    TextFormField(
                      controller: _bridgeCtrl,
                      style: const TextStyle(fontSize: 14),
                      decoration: const InputDecoration(
                        labelText: 'Bridge Interface Name',
                        hintText: 'e.g. br0, virbr0',
                        prefixIcon: Icon(Icons.settings_ethernet_rounded, size: 19),
                      ),
                      validator: (v) => _networkMode == 'bridge' && (v == null || v.trim().isEmpty) ? 'Bridge interface name is required' : null,
                    ),
                  ],
                  const SizedBox(height: 16),
                  const Text('NIC Device Model', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 13, color: NivaroColors.textPrimary)),
                  const SizedBox(height: 8),
                  Row(
                    children: [
                      Expanded(
                        child: ChoiceChip(
                          label: const Center(child: Text('VirtIO')),
                          selected: _nicModel == 'virtio',
                          onSelected: (s) => setState(() => _nicModel = 'virtio'),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: ChoiceChip(
                          label: const Center(child: Text('e1000')),
                          selected: _nicModel == 'e1000',
                          onSelected: (s) => setState(() => _nicModel = 'e1000'),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: ChoiceChip(
                          label: const Center(child: Text('RTL8139')),
                          selected: _nicModel == 'rtl8139',
                          onSelected: (s) => setState(() => _nicModel = 'rtl8139'),
                        ),
                      ),
                    ],
                  ),
                ],
              ),
            ),
            const SizedBox(height: 18),

            const SectionHeader(title: 'Boot Media / ISO'),
            DarkCard(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (_loadingIsos)
                    const Padding(
                      padding: EdgeInsets.symmetric(vertical: 8),
                      child: Center(child: CircularProgressIndicator(strokeWidth: 2)),
                    )
                  else
                    DropdownButtonFormField<String?>(
                      value: _selectedIso,
                      isExpanded: true,
                      decoration: const InputDecoration(
                        labelText: 'Select Boot ISO Image',
                        prefixIcon: Icon(Icons.album_rounded, size: 19),
                      ),
                      items: [
                        const DropdownMenuItem<String?>(
                          value: null,
                          child: Text('No ISO (Boot from Virtual Disk)', overflow: TextOverflow.ellipsis),
                        ),
                        ..._isos.map((iso) => DropdownMenuItem<String?>(
                              value: iso.path,
                              child: Text(iso.name, overflow: TextOverflow.ellipsis),
                            )),
                      ],
                      onChanged: (v) => setState(() => _selectedIso = v),
                    ),
                  if (_selectedIso != null && _selectedIso!.isNotEmpty) ...[
                    const SizedBox(height: 10),
                    SwitchListTile(
                      contentPadding: EdgeInsets.zero,
                      title: const Text('Prioritize CD-ROM in Boot Order', style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600)),
                      subtitle: const Text('Attempts to boot from selected ISO first', style: TextStyle(fontSize: 11, color: NivaroColors.textMuted)),
                      value: _bootCdromFirst,
                      onChanged: (v) => setState(() => _bootCdromFirst = v),
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(height: 28),

            FilledButton.icon(
              onPressed: _saving ? null : _submit,
              icon: _saving
                  ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                  : const Icon(Icons.check_rounded),
              label: Text(isEdit ? 'Save Changes' : 'Create Virtual Machine', style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 15)),
              style: FilledButton.styleFrom(
                backgroundColor: NivaroColors.primary,
                padding: const EdgeInsets.symmetric(vertical: 16),
                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
              ),
            ),
            const SizedBox(height: 24),
          ],
        ),
      ),
    );
  }
}
