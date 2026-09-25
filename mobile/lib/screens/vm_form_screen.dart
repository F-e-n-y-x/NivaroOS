import 'package:flutter/material.dart';

import '../services/vm_client.dart';
import '../ui/ui.dart';
import 'vm_list_screen.dart' show formatMemoryMib;

/// Create a VM, or edit one (a full-screen dialog: close, title, Save).
///
/// Editing loads the VM fresh from the server and saves it back complete,
/// changing only what was edited (plan M-04, [VmClient.buildUpdateBody]):
/// existing disks keep their path and can only grow, adapters keep their
/// MAC, passthrough devices and the shared folder stay as they are, and a
/// new disk is its own explicit action ("Add a disk"), never a side effect
/// of saving.
class VmFormScreen extends StatefulWidget {
  const VmFormScreen({super.key, required this.client, this.existing, this.takenNames = const []});

  final VmClient client;

  /// The VM to edit; null to create one.
  final Vm? existing;

  /// Names already in use, so a new VM's name can be checked as it's typed.
  final List<String> takenNames;

  @override
  State<VmFormScreen> createState() => _VmFormScreenState();
}

/// A starting point for a new VM; picking one only fills in the fields.
class _Template {
  const _Template(this.label, this.vcpus, this.memoryMib, this.diskGib, this.firmware, this.bus, this.nic);
  final String label;
  final int vcpus;
  final int memoryMib;
  final int diskGib;
  final String firmware;
  final String bus;
  final String nic;
}

// Windows gets a SATA disk and an e1000e adapter because its installer has
// drivers for both; VirtIO needs the drivers disc first. Linux has VirtIO
// built in, and it's the fastest.
const _templates = [
  _Template('Linux', 2, 2048, 20, 'uefi', 'virtio', 'virtio'),
  _Template('Windows', 4, 4096, 64, 'uefi', 'sata', 'e1000e'),
  _Template('Other', 2, 2048, 20, 'bios', 'sata', 'e1000e'),
];

const _memorySteps = [512, 1024, 2048, 3072, 4096, 6144, 8192, 12288, 16384, 24576, 32768, 49152, 65536];
const _diskSteps = [8, 16, 20, 32, 40, 48, 64, 80, 100, 128, 160, 200, 256, 320, 400, 500, 640, 800, 1000, 1500, 2000];
const _maxCpus = 32;

const _resolutions = [
  (w: 0, h: 0, label: 'Let the guest decide'),
  (w: 1920, h: 1080, label: '1920 × 1080'),
  (w: 1600, h: 900, label: '1600 × 900'),
  (w: 1366, h: 768, label: '1366 × 768'),
  (w: 1280, h: 720, label: '1280 × 720'),
  (w: 1024, h: 768, label: '1024 × 768'),
  (w: 800, h: 600, label: '800 × 600'),
];

const _nicModels = {
  'virtio': 'VirtIO (fastest, needs drivers on Windows)',
  'e1000e': 'Intel e1000e (works everywhere)',
  'e1000': 'Intel e1000 (older systems)',
  'rtl8139': 'Realtek RTL8139 (very old systems)',
};

const _buses = {'virtio': 'VirtIO', 'sata': 'SATA', 'ide': 'IDE'};

final _nameRe = RegExp(r'^[a-zA-Z0-9_-]+$');

int _next(List<int> steps, int value) => steps.firstWhere((s) => s > value, orElse: () => value);
int _previous(List<int> steps, int value) => steps.lastWhere((s) => s < value, orElse: () => value);

class _VmFormScreenState extends State<VmFormScreen> {
  final _formKey = GlobalKey<FormState>();
  final _name = TextEditingController();
  final _bridgeText = TextEditingController();

  bool get _isEdit => widget.existing != null;

  // Loading.
  bool _loading = true;
  VmException? _loadError;
  Vm? _current;
  List<VmIso>? _isos;
  bool _isoError = false;
  List<VmHostNetwork> _networks = const [];

  // The form.
  int _template = 0;
  int _vcpus = 2;
  int _memoryMib = 2048;
  String _firmware = 'uefi';
  int _diskGib = 20;
  String _bus = 'virtio';
  bool _ssd = true;
  String _iso = '';
  String _netMode = 'bridge';
  String _bridge = '';
  String _nicModel = 'virtio';
  int _displayW = 0;
  int _displayH = 0;
  final Map<int, int> _diskSizes = {};
  final Map<int, VmNetwork> _nicEdits = {};

  bool _dirty = false;
  bool _saving = false;
  String? _saveError;

  @override
  void initState() {
    super.initState();
    if (!_isEdit) _applyTemplate(0, markDirty: false);
    _load();
  }

  @override
  void dispose() {
    _name.dispose();
    _bridgeText.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _loadError = null;
    });
    final isos = widget.client.listIsos().then<List<VmIso>?>((v) => v).catchError((_) => null);
    final nets = widget.client.listNetworks().catchError((_) => <VmHostNetwork>[]);
    try {
      // Always the server's current settings, never the list's copy:
      // saving a stale copy could undo a change made elsewhere.
      final vm = _isEdit ? await widget.client.getVm(widget.existing!.name) : null;
      final loadedIsos = await isos;
      final loadedNets = await nets;
      if (!mounted) return;
      setState(() {
        _current = vm;
        _isos = loadedIsos;
        _isoError = loadedIsos == null;
        _networks = loadedNets;
        if (vm != null) _fillFrom(vm);
        if (vm == null && _bridge.isEmpty) {
          final bridges = _bridgeNames;
          _bridge = bridges.isNotEmpty ? bridges.first : '';
          if (bridges.isEmpty && _networks.any((n) => n.mode == 'nat')) _netMode = 'nat';
        }
        _loading = false;
      });
    } on VmException catch (e) {
      if (!mounted) return;
      setState(() {
        _loadError = e;
        _loading = false;
      });
    }
  }

  Future<void> _reloadIsos() async {
    setState(() => _isoError = false);
    try {
      final isos = await widget.client.listIsos();
      if (mounted) setState(() => _isos = isos);
    } on VmException {
      if (mounted) setState(() => _isoError = true);
    }
  }

  void _fillFrom(Vm vm) {
    _vcpus = vm.vcpus;
    _memoryMib = vm.memoryMib;
    _firmware = vm.firmware;
    _iso = vm.isoPath ?? '';
    _displayW = vm.displayWidth;
    _displayH = vm.displayHeight;
    _diskSizes.clear();
    _nicEdits.clear();
  }

  List<String> get _bridgeNames => [for (final n in _networks) if (n.mode == 'bridge') n.name];

  void _applyTemplate(int i, {bool markDirty = true}) {
    final t = _templates[i];
    setState(() {
      _template = i;
      _vcpus = t.vcpus;
      _memoryMib = t.memoryMib;
      _diskGib = t.diskGib;
      _firmware = t.firmware;
      _bus = t.bus;
      _nicModel = t.nic;
      if (markDirty) _dirty = true;
    });
  }

  void _edit(VoidCallback change) {
    setState(() {
      change();
      _dirty = true;
      _saveError = null;
    });
  }

  bool get _locked => _current?.isActive ?? false;

  VmEdits _edits() {
    final vm = _current!;
    return VmEdits(
      vcpus: _vcpus != vm.vcpus ? _vcpus : null,
      memoryMib: _memoryMib != vm.memoryMib ? _memoryMib : null,
      firmware: _firmware != vm.firmware ? _firmware : null,
      isoPath: _iso != (vm.isoPath ?? '') ? _iso : null,
      displayWidth: _displayW,
      displayHeight: _displayH,
      diskSizes: Map.of(_diskSizes),
      networks: Map.of(_nicEdits),
    );
  }

  Future<void> _save() async {
    if (_saving) return;
    FocusScope.of(context).unfocus();
    if (!(_formKey.currentState?.validate() ?? false)) {
      setState(() => _saveError = 'Check the fields marked in red.');
      return;
    }
    setState(() {
      _saving = true;
      _saveError = null;
    });
    final messenger = ScaffoldMessenger.maybeOf(context);
    try {
      if (_isEdit) {
        final loaded = _current!;
        // Read it again right before saving: started from the console or
        // changed on the web since this opened?
        final fresh = await widget.client.getVm(loaded.name);
        if (fresh.disks.length != loaded.disks.length ||
            fresh.networks.map((n) => n.mac).join() != loaded.networks.map((n) => n.mac).join()) {
          throw VmException('${loaded.name} changed on the server since you opened this. Close it and open it again.');
        }
        if (fresh.isActive && !loaded.isActive && _changesNeedingStop(loaded)) {
          throw VmException('${loaded.name} is running now. Shut it down, then save again.');
        }
        await widget.client.updateVm(fresh, _edits());
      } else {
        final vm = await widget.client.createVm(VmCreateSpec(
          name: _name.text.trim(),
          vcpus: _vcpus,
          memoryMib: _memoryMib,
          firmware: _firmware,
          diskGib: _diskGib,
          diskBus: _bus,
          diskSsd: _ssd,
          isoPath: _iso.isEmpty ? null : _iso,
          networkMode: _netMode,
          bridgeName: _netMode == 'bridge' ? (_bridgeNames.isEmpty ? _bridgeText.text.trim() : _bridge) : null,
          nicModel: _nicModel,
          displayWidth: _displayW,
          displayHeight: _displayH,
        ));
        final warning = vm.warning;
        if (warning != null && warning.isNotEmpty) {
          messenger?.showSnackBar(SnackBar(content: Text(warning), duration: const Duration(seconds: 8)));
        }
      }
      if (mounted) Navigator.of(context).pop(true);
    } on VmException catch (e) {
      if (mounted) setState(() => _saveError = e.message);
    } on ArgumentError catch (e) {
      if (mounted) setState(() => _saveError = '${e.message}');
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  bool _changesNeedingStop(Vm vm) =>
      _vcpus != vm.vcpus ||
      _memoryMib != vm.memoryMib ||
      _firmware != vm.firmware ||
      _diskSizes.isNotEmpty ||
      _displayW != vm.displayWidth ||
      _displayH != vm.displayHeight;

  Future<void> _onPop(bool didPop, Object? result) async {
    if (didPop) return;
    final discard = await ConfirmDialog.destructive(
      context,
      title: 'Discard changes?',
      message: _isEdit ? 'Your changes to ${widget.existing!.name} are not saved.' : 'This VM is not created yet.',
      confirmLabel: 'Discard',
      permanent: false,
    );
    if (discard && mounted) Navigator.of(context).pop(false);
  }

  Future<void> _addDisk() async {
    final vm = _current!;
    final spec = await showDialog<({int gib, String bus})>(
      context: context,
      builder: (_) => _AddDiskDialog(vmName: vm.name, running: vm.isActive),
    );
    if (spec == null || !mounted) return;
    setState(() => _saving = true);
    try {
      await widget.client.addDisk(vm.name, gib: spec.gib, bus: spec.bus);
      final fresh = await widget.client.getVm(vm.name);
      if (!mounted) return;
      setState(() => _current = fresh);
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text('Added a ${spec.gib} GB disk to ${vm.name}')));
    } on VmException catch (e) {
      if (mounted) setState(() => _saveError = e.message);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final title = _isEdit ? 'Edit ${widget.existing!.name}' : 'New VM';
    final ready = !_loading && _loadError == null;
    return PopScope(
      canPop: !_dirty || _saving,
      onPopInvokedWithResult: _onPop,
      child: AppScaffold(
        title: title,
        leading: IconButton(
          tooltip: 'Close',
          icon: const Icon(Icons.close),
          onPressed: () => Navigator.of(context).maybePop(),
        ),
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: Space.sm),
            child: _saving
                ? Semantics(
                    label: 'Saving',
                    child: const Padding(
                      padding: EdgeInsets.all(Space.md),
                      child: SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2.5)),
                    ),
                  )
                : TextButton(onPressed: ready ? _save : null, child: Text(_isEdit ? 'Save' : 'Create')),
          ),
        ],
        body: _body(context),
      ),
    );
  }

  Widget _body(BuildContext context) {
    if (_loading) return const LoadingList(rows: 8, leading: SkeletonLeading.none);
    final error = _loadError;
    if (error != null) {
      return error.kind == VmErrorKind.offline
          ? ErrorState.offline(onRetry: _load, details: error.details)
          : ErrorState(
              title: "Couldn't load the settings of ${widget.existing?.name ?? 'the VM'}",
              message: error.message,
              details: error.details,
              onRetry: _load,
            );
    }
    final gutter = Space.gutter(context);
    final vm = _current;
    return Form(
      key: _formKey,
      child: ListView(
        padding: const EdgeInsets.only(bottom: Space.xxl),
        children: [
          // One slot for both notices, always present, so the rows below
          // keep their place (and their state) when a notice appears.
          AnimatedSize(
            duration: Motion.of(context).short,
            alignment: Alignment.topCenter,
            child: Column(mainAxisSize: MainAxisSize.min, children: [
              if (_saveError != null) _Notice(text: _saveError!, status: Status.error),
              if (_locked)
                _Notice(
                  text: '${vm!.name} is ${vm.powerState == VmPowerState.paused ? 'paused' : 'running'}. '
                      'Shut it down to change CPU, memory, firmware, disk sizes or the display. '
                      'The network and install media can change now.',
                  status: Status.info,
                ),
            ]),
          ),
          if (!_isEdit) ..._createHeader(context),
          const SectionHeader(title: 'Hardware'),
          _StepperRow(
            label: 'CPU cores',
            value: '$_vcpus',
            lessTooltip: 'Fewer CPU cores',
            moreTooltip: 'More CPU cores',
            enabled: !_locked,
            onMinus: _vcpus > 1 ? () => _edit(() => _vcpus--) : null,
            onPlus: _vcpus < _maxCpus ? () => _edit(() => _vcpus++) : null,
          ),
          _StepperRow(
            label: 'Memory',
            value: formatMemoryMib(_memoryMib),
            lessTooltip: 'Less memory',
            moreTooltip: 'More memory',
            helper: !_isEdit && _templates[_template].label == 'Windows' ? 'Windows needs at least 4 GB' : null,
            enabled: !_locked,
            onMinus: _previous(_memorySteps, _memoryMib) != _memoryMib
                ? () => _edit(() => _memoryMib = _previous(_memorySteps, _memoryMib))
                : null,
            onPlus: _next(_memorySteps, _memoryMib) != _memoryMib
                ? () => _edit(() => _memoryMib = _next(_memorySteps, _memoryMib))
                : null,
          ),
          _Labeled(
            label: 'Firmware',
            enabled: !_locked,
            helper: 'Windows 11 needs UEFI. Older systems may need BIOS.',
            child: SegmentedButton<String>(
              segments: const [
                ButtonSegment(value: 'uefi', label: Text('UEFI')),
                ButtonSegment(value: 'bios', label: Text('BIOS')),
              ],
              selected: {_firmware},
              onSelectionChanged: _locked ? null : (s) => _edit(() => _firmware = s.first),
            ),
          ),
          const SectionHeader(title: 'Storage'),
          if (!_isEdit) ..._newDisk(context) else ..._existingDisks(context, vm!),
          const SectionHeader(title: 'Install media'),
          _isoField(context),
          const SectionHeader(title: 'Network'),
          if (!_isEdit) ..._newNetwork(context) else ..._existingNetworks(context, vm!),
          const SectionHeader(title: 'Display'),
          _Labeled(
            label: 'Resolution',
            enabled: !_locked,
            helper: 'The size the guest prefers. The console scales it to your screen.',
            child: _resolutionField(context),
          ),
          if (vm != null) ..._passthrough(context, vm),
          SizedBox(height: gutter),
        ],
      ),
    );
  }

  List<Widget> _createHeader(BuildContext context) {
    final gutter = Space.gutter(context);
    return [
      const SectionHeader(title: 'Start from'),
      Padding(
        padding: EdgeInsets.symmetric(horizontal: gutter),
        // Three short, exclusive choices: a segmented button, like
        // Firmware and Disk type below (the app's rule: segments for 2-3
        // exclusive options, chips for longer or scrolling sets).
        child: SegmentedButton<int>(
          segments: [
            for (var i = 0; i < _templates.length; i++) ButtonSegment(value: i, label: Text(_templates[i].label)),
          ],
          selected: {_template},
          onSelectionChanged: (v) => _applyTemplate(v.first),
        ),
      ),
      Padding(
        padding: EdgeInsets.fromLTRB(gutter, Space.lg, gutter, 0),
        child: TextFormField(
          controller: _name,
          autofocus: false,
          textInputAction: TextInputAction.done,
          maxLength: 64,
          decoration: const InputDecoration(
            labelText: 'Name',
            helperText: 'Letters, numbers, - and _. It names the VM folder on the server.',
            counterText: '',
            helperMaxLines: 3,
          ),
          onChanged: (_) => _edit(() {}),
          autovalidateMode: AutovalidateMode.onUserInteraction,
          validator: (v) {
            final name = (v ?? '').trim();
            if (name.isEmpty) return 'Give the VM a name';
            if (!_nameRe.hasMatch(name)) return 'Use only letters, numbers, - and _';
            if (widget.takenNames.any((n) => n.toLowerCase() == name.toLowerCase())) {
              return 'A VM called “$name” already exists';
            }
            return null;
          },
        ),
      ),
    ];
  }

  List<Widget> _newDisk(BuildContext context) => [
        _StepperRow(
          label: 'Disk size',
          value: '$_diskGib GB',
          lessTooltip: 'Smaller disk',
          moreTooltip: 'Larger disk',
          helper: 'It only takes the space the VM actually uses.',
          onMinus: _previous(_diskSteps, _diskGib) != _diskGib
              ? () => _edit(() => _diskGib = _previous(_diskSteps, _diskGib))
              : null,
          onPlus: _next(_diskSteps, _diskGib) != _diskGib ? () => _edit(() => _diskGib = _next(_diskSteps, _diskGib)) : null,
        ),
        _Labeled(
          label: 'Disk type',
          helper: _bus == 'virtio' ? 'Fastest. Windows needs the drivers disc to see it.' : 'Works without extra drivers.',
          child: SegmentedButton<String>(
            segments: [for (final e in _buses.entries) ButtonSegment(value: e.key, label: Text(e.value))],
            selected: {_bus},
            onSelectionChanged: (s) => _edit(() => _bus = s.first),
          ),
        ),
        SwitchListTile(
          title: const Text('SSD'),
          subtitle: const Text('Lets the guest free up space it no longer uses'),
          value: _ssd,
          onChanged: (v) => _edit(() => _ssd = v),
        ),
      ];

  List<Widget> _existingDisks(BuildContext context, Vm vm) {
    final gutter = Space.gutter(context);
    final theme = Theme.of(context);
    return [
      if (vm.disks.isEmpty)
        Padding(
          padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.sm),
          child: Text('This VM has no disk.',
              style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
        ),
      for (var i = 0; i < vm.disks.length; i++)
        Builder(builder: (context) {
          final d = vm.disks[i];
          final size = _diskSizes[i] ?? d.gib;
          final kind = [_buses[d.bus] ?? d.bus.toUpperCase(), if (d.ssd) 'SSD', if (d.target != null) d.target!].join(' · ');
          return _StepperRow(
            label: vm.disks.length == 1 ? 'Disk' : 'Disk ${i + 1}',
            helper: kind,
            value: '$size GB',
            enabled: !_locked,
            lessTooltip: size <= d.gib ? "Disks can't shrink" : 'Smaller',
            moreTooltip: 'Larger',
            onMinus: size > d.gib
                ? () => _edit(() {
                      final smaller = _previous(_diskSteps, size);
                      final v = smaller < d.gib ? d.gib : smaller;
                      if (v == d.gib) {
                        _diskSizes.remove(i);
                      } else {
                        _diskSizes[i] = v;
                      }
                    })
                : null,
            onPlus: _next(_diskSteps, size) != size ? () => _edit(() => _diskSizes[i] = _next(_diskSteps, size)) : null,
          );
        }),
      Padding(
        padding: EdgeInsets.fromLTRB(gutter, Space.xs, gutter, 0),
        child: Text(
          "Disks can grow but not shrink. Growing one doesn't change the partitions inside the VM.",
          style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
        ),
      ),
      Padding(
        padding: EdgeInsets.fromLTRB(gutter, Space.md, gutter, 0),
        child: Align(
          alignment: AlignmentDirectional.centerStart,
          child: OutlinedButton.icon(
            onPressed: _saving ? null : _addDisk,
            icon: const Icon(Icons.add),
            label: const Text('Add a disk'),
          ),
        ),
      ),
    ];
  }

  Widget _isoField(BuildContext context) {
    final gutter = Space.gutter(context);
    final isos = _isos;
    final entries = <DropdownMenuEntry<String>>[
      const DropdownMenuEntry(value: '', label: 'None (start from the disk)'),
      for (final iso in isos ?? const <VmIso>[]) DropdownMenuEntry(value: iso.path, label: iso.name),
    ];
    // Keep the VM's own ISO selectable even when it isn't in the folder.
    if (_iso.isNotEmpty && !entries.any((e) => e.value == _iso)) {
      entries.add(DropdownMenuEntry(value: _iso, label: _iso.split('/').last));
    }
    return Padding(
      padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.sm),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          LayoutBuilder(
            builder: (context, c) => DropdownMenu<String>(
              key: ValueKey('iso-${entries.length}'),
              width: c.maxWidth,
              initialSelection: _iso,
              label: const Text('Disc image (ISO)'),
              requestFocusOnTap: false,
              dropdownMenuEntries: entries,
              onSelected: (v) => _edit(() => _iso = v ?? ''),
            ),
          ),
          Padding(
            padding: const EdgeInsets.only(top: Space.xs, left: Space.lg, right: Space.lg),
            child: Text('An installer to start from. ISOs live in ${VmIso.folder} on the server.',
                style: Theme.of(context).textTheme.bodySmall?.copyWith(color: Theme.of(context).colorScheme.onSurfaceVariant)),
          ),
          if (_isoError)
            Padding(
              padding: const EdgeInsets.only(top: Space.xs),
              child: Row(children: [
                Expanded(
                  child: Text("Couldn't load the ISO list.",
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(color: Theme.of(context).colorScheme.error)),
                ),
                TextButton(onPressed: _reloadIsos, child: const Text('Retry')),
              ]),
            ),
        ],
      ),
    );
  }

  List<Widget> _newNetwork(BuildContext context) => [
        _Labeled(
          label: 'Connection',
          helper: _netMode == 'bridge'
              ? 'The VM joins your home network and gets its own address.'
              : 'The VM shares the server’s address and is reachable only from the server.',
          child: SegmentedButton<String>(
            segments: const [
              ButtonSegment(value: 'bridge', label: Text('Home network')),
              ButtonSegment(value: 'nat', label: Text('Private (NAT)')),
            ],
            selected: {_netMode},
            onSelectionChanged: (s) => _edit(() => _netMode = s.first),
          ),
        ),
        if (_netMode == 'bridge') _bridgeField(context, _bridge, (v) => _edit(() => _bridge = v)),
        _modelField(context, _nicModel, (v) => _edit(() => _nicModel = v)),
      ];

  List<Widget> _existingNetworks(BuildContext context, Vm vm) {
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    if (vm.networks.isEmpty) {
      return [
        Padding(
          padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.sm),
          child: Text('This VM has no network adapter. Add one in the web dashboard.',
              style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
        ),
      ];
    }
    return [
      for (var i = 0; i < vm.networks.length; i++) ...() {
        final n = _nicEdits[i] ?? vm.networks[i];
        void update(VmNetwork v) => _edit(() => _nicEdits[i] = v);
        return [
          if (vm.networks.length > 1)
            Padding(
              padding: EdgeInsets.fromLTRB(gutter, Space.md, gutter, 0),
              child: Text('Adapter ${i + 1}', style: theme.textTheme.titleSmall),
            ),
          _Labeled(
            label: 'Connection',
            helper: n.mac == null ? null : 'Address ${n.mac} stays the same.',
            child: SegmentedButton<String>(
              segments: const [
                ButtonSegment(value: 'bridge', label: Text('Home network')),
                ButtonSegment(value: 'nat', label: Text('Private (NAT)')),
              ],
              selected: {n.mode},
              onSelectionChanged: (s) => update(n.copyWith(mode: s.first)),
            ),
          ),
          if (n.mode == 'bridge') _bridgeField(context, n.bridgeName ?? '', (v) => update(n.copyWith(bridgeName: v))),
          _modelField(context, n.model, (v) => update(n.copyWith(model: v))),
        ];
      }(),
    ];
  }

  Widget _bridgeField(BuildContext context, String value, ValueChanged<String> onChanged) {
    final gutter = Space.gutter(context);
    final bridges = _bridgeNames;
    if (bridges.isEmpty) {
      if (_bridgeText.text.isEmpty && value.isNotEmpty) _bridgeText.text = value;
      return Padding(
        padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.sm),
        child: TextFormField(
          controller: _bridgeText,
          decoration: const InputDecoration(labelText: 'Bridge', helperText: 'The server’s network bridge, like br0'),
          onChanged: onChanged,
          validator: (v) => (v ?? '').trim().isEmpty ? 'Name the bridge to join' : null,
        ),
      );
    }
    final options = {...bridges, if (value.isNotEmpty) value}.toList();
    return Padding(
      padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.sm),
      child: LayoutBuilder(
        builder: (context, c) => DropdownMenu<String>(
          width: c.maxWidth,
          initialSelection: value.isEmpty ? options.first : value,
          label: const Text('Bridge'),
          requestFocusOnTap: false,
          dropdownMenuEntries: [
            for (final b in options)
              DropdownMenuEntry(
                value: b,
                label: b,
                trailingIcon: () {
                  final nic = _networks.where((n) => n.name == b).firstOrNull?.hostNic;
                  return nic == null ? null : Text(nic);
                }(),
              ),
          ],
          onSelected: (v) {
            if (v != null) onChanged(v);
          },
        ),
      ),
    );
  }

  Widget _modelField(BuildContext context, String value, ValueChanged<String> onChanged) {
    final gutter = Space.gutter(context);
    final options = {..._nicModels, if (!_nicModels.containsKey(value)) value: value};
    return Padding(
      padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.sm),
      child: LayoutBuilder(
        builder: (context, c) => DropdownMenu<String>(
          width: c.maxWidth,
          initialSelection: value,
          label: const Text('Adapter type'),
          requestFocusOnTap: false,
          dropdownMenuEntries: [for (final e in options.entries) DropdownMenuEntry(value: e.key, label: e.value)],
          onSelected: (v) {
            if (v != null) onChanged(v);
          },
        ),
      ),
    );
  }

  Widget _resolutionField(BuildContext context) {
    final options = [
      ..._resolutions,
      if (!_resolutions.any((r) => r.w == _displayW && r.h == _displayH))
        (w: _displayW, h: _displayH, label: '$_displayW × $_displayH'),
    ];
    final selected = options.indexWhere((r) => r.w == _displayW && r.h == _displayH);
    return LayoutBuilder(
      builder: (context, c) => DropdownMenu<int>(
        width: c.maxWidth,
        enabled: !_locked,
        initialSelection: selected,
        requestFocusOnTap: false,
        dropdownMenuEntries: [
          for (var i = 0; i < options.length; i++) DropdownMenuEntry(value: i, label: options[i].label),
        ],
        onSelected: (i) {
          if (i == null) return;
          _edit(() {
            _displayW = options[i].w;
            _displayH = options[i].h;
          });
        },
      ),
    );
  }

  List<Widget> _passthrough(BuildContext context, Vm vm) {
    final rows = <Widget>[
      for (final u in vm.usbDevices)
        ListTile(
          leading: const Icon(Icons.usb_outlined),
          title: const Text('USB device'),
          subtitle: Text('${u.vendorId}:${u.productId}'),
        ),
      for (final p in vm.pciDevices)
        ListTile(
          leading: const Icon(Icons.memory_outlined),
          title: const Text('PCI device'),
          subtitle: Text(p.address),
        ),
      for (final s in vm.sharedFolders)
        ListTile(
          leading: const Icon(Icons.folder_shared_outlined),
          title: const Text('Shared folder'),
          subtitle: Text('${s.sourceDir}${s.readOnly ? ' · read only' : ''}'),
        ),
    ];
    if (rows.isEmpty) return const [];
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    return [
      const SectionHeader(title: 'Devices and sharing'),
      ...rows,
      Padding(
        padding: EdgeInsets.fromLTRB(gutter, Space.xs, gutter, 0),
        child: Text(
          'Change these in the web dashboard. Saving here keeps them as they are.',
          style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
        ),
      ),
    ];
  }
}

/// A labelled number with − and + (CPU cores, memory, disk size).
class _StepperRow extends StatelessWidget {
  const _StepperRow({
    required this.label,
    required this.value,
    required this.onMinus,
    required this.onPlus,
    required this.lessTooltip,
    required this.moreTooltip,
    this.helper,
    this.enabled = true,
  });

  final String label;
  final String value;
  final String? helper;
  final VoidCallback? onMinus;
  final VoidCallback? onPlus;
  final bool enabled;
  final String lessTooltip;
  final String moreTooltip;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    final muted = enabled ? null : scheme.onSurface.withValues(alpha: 0.38);
    // With large text the label and the controls get a line each.
    final stacked = MediaQuery.textScalerOf(context).scale(16) > 24;
    final labels = ExcludeSemantics(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: theme.textTheme.bodyLarge?.copyWith(color: muted)),
          if (helper != null)
            Text(helper!, style: theme.textTheme.bodySmall?.copyWith(color: muted ?? scheme.onSurfaceVariant)),
        ],
      ),
    );
    final controls = Row(mainAxisSize: MainAxisSize.min, children: [
      IconButton.outlined(tooltip: lessTooltip, onPressed: enabled ? onMinus : null, icon: const Icon(Icons.remove)),
      ConstrainedBox(
        constraints: const BoxConstraints(minWidth: 72),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: Space.sm),
          child: ExcludeSemantics(
            child: Text(value, textAlign: TextAlign.center, style: theme.textTheme.titleMedium?.tabular.copyWith(color: muted)),
          ),
        ),
      ),
      IconButton.outlined(tooltip: moreTooltip, onPressed: enabled ? onPlus : null, icon: const Icon(Icons.add)),
    ]);
    return Semantics(
      label: label,
      value: value,
      hint: helper,
      enabled: enabled,
      child: Padding(
        padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter - Space.xs, Space.sm),
        child: stacked
            ? Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                labels,
                const SizedBox(height: Space.sm),
                Align(alignment: AlignmentDirectional.centerStart, child: controls),
              ])
            : Row(children: [Expanded(child: labels), const SizedBox(width: Space.sm), controls]),
      ),
    );
  }
}

/// A field with its label above and a helper line below.
class _Labeled extends StatelessWidget {
  const _Labeled({required this.label, required this.child, this.helper, this.enabled = true});

  final String label;
  final bool enabled;
  final String? helper;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    return Padding(
      padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.sm),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(label,
              style: theme.textTheme.bodyLarge
                  ?.copyWith(color: enabled ? null : theme.colorScheme.onSurface.withValues(alpha: 0.38))),
          const SizedBox(height: Space.sm),
          child,
          if (helper != null) ...[
            const SizedBox(height: Space.xs),
            // Dimmed with the control it explains, so a locked field reads
            // as locked all the way down.
            Text(helper!,
                style: theme.textTheme.bodySmall?.copyWith(
                    color: enabled ? theme.colorScheme.onSurfaceVariant : theme.colorScheme.onSurface.withValues(alpha: 0.38))),
          ],
        ],
      ),
    );
  }
}

/// A tonal notice at the top of the form: why fields are locked, or why
/// saving failed.
class _Notice extends StatelessWidget {
  const _Notice({required this.text, required this.status});

  final String text;
  final Status status;

  @override
  Widget build(BuildContext context) {
    final tone = StatusColors.toneOf(context, status);
    final gutter = Space.gutter(context);
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.sm),
      child: Semantics(
        liveRegion: status == Status.error,
        child: DecoratedBox(
          decoration: BoxDecoration(color: tone.container, borderRadius: BorderRadius.circular(Corners.medium)),
          child: Padding(
            padding: const EdgeInsets.all(Space.lg),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(StatusChip.defaultIcon(status), color: tone.onContainer, size: 20),
                const SizedBox(width: Space.md),
                Expanded(
                  child: Text(text, style: Theme.of(context).textTheme.bodyMedium?.copyWith(color: tone.onContainer)),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Size and type of a new disk; it is created at once, not on Save.
class _AddDiskDialog extends StatefulWidget {
  const _AddDiskDialog({required this.vmName, required this.running});

  final String vmName;
  final bool running;

  @override
  State<_AddDiskDialog> createState() => _AddDiskDialogState();
}

class _AddDiskDialogState extends State<_AddDiskDialog> {
  int _gib = 32;
  String _bus = 'virtio';

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AlertDialog(
      title: Text('Add a disk to ${widget.vmName}?'),
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text('The disk is created now, not when you save.', style: theme.textTheme.bodyMedium),
            const SizedBox(height: Space.lg),
            Row(children: [
              Expanded(child: Text('Size', style: theme.textTheme.bodyLarge)),
              IconButton.outlined(
                tooltip: 'Smaller',
                onPressed: _previous(_diskSteps, _gib) != _gib ? () => setState(() => _gib = _previous(_diskSteps, _gib)) : null,
                icon: const Icon(Icons.remove),
              ),
              ConstrainedBox(
                constraints: const BoxConstraints(minWidth: 72),
                child: Text('$_gib GB', textAlign: TextAlign.center, style: theme.textTheme.titleMedium?.tabular),
              ),
              IconButton.outlined(
                tooltip: 'Larger',
                onPressed: _next(_diskSteps, _gib) != _gib ? () => setState(() => _gib = _next(_diskSteps, _gib)) : null,
                icon: const Icon(Icons.add),
              ),
            ]),
            const SizedBox(height: Space.lg),
            SegmentedButton<String>(
              segments: [
                for (final e in _buses.entries)
                  ButtonSegment(value: e.key, label: Text(e.value), enabled: !widget.running || e.key == 'virtio'),
              ],
              selected: {_bus},
              onSelectionChanged: (s) => setState(() => _bus = s.first),
            ),
            if (widget.running) ...[
              const SizedBox(height: Space.sm),
              Text('While it runs, only a VirtIO disk can be added.',
                  style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(autofocus: true, onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        TextButton(onPressed: () => Navigator.of(context).pop((gib: _gib, bus: _bus)), child: const Text('Add disk')),
      ],
    );
  }
}
