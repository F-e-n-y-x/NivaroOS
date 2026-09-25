import 'dart:async';

import 'package:flutter/material.dart';

import '../models/vm_snapshot.dart';
import '../services/rfb_client.dart';
import '../services/vm_client.dart';
import '../ui/ui.dart';
import '../widgets/rfb_view.dart';

/// A VM's screen (VNC through the gateway), with power, snapshots, the
/// disc drive and the clipboard. A VM that is off shows that, with Start,
/// instead of a failing connection.
class VmConsoleScreen extends StatefulWidget {
  const VmConsoleScreen({super.key, required this.vmName, this.client, this.rfb, this.history});

  final String vmName;

  /// For tests; the app uses the session's gateway clients.
  final VmClient? client;
  final RfbClient? rfb;
  final RemoteClipboardHistory? history;

  @override
  State<VmConsoleScreen> createState() => _VmConsoleScreenState();
}

class _VmConsoleScreenState extends State<VmConsoleScreen> {
  late final VmClient _client = widget.client ?? VmClient();
  late final RfbClient _rfb = widget.rfb ?? RfbClient(vmName: widget.vmName);
  Vm? _vm;
  VmException? _loadError;
  bool _loading = true;
  bool _starting = false;

  @override
  void initState() {
    super.initState();
    _rfb.status.addListener(_onStatus);
    _load();
  }

  @override
  void dispose() {
    _rfb.status.removeListener(_onStatus);
    if (widget.rfb == null) _rfb.dispose();
    if (widget.client == null) _client.close();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _loadError = null;
    });
    try {
      final vm = await _client.getVm(widget.vmName);
      if (!mounted) return;
      setState(() {
        _vm = vm;
        _loading = false;
      });
      if (vm.isActive) unawaited(_rfb.connect());
    } on VmException catch (e) {
      if (!mounted) return;
      setState(() {
        _loadError = e;
        _loading = false;
      });
    }
  }

  // When the connection ends, find out whether the VM simply went off
  // (shut down from inside, or from here) - that is "off", not an error.
  Future<void> _onStatus() async {
    final s = _rfb.status.value;
    if (s != RfbStatus.disconnected && s != RfbStatus.failed) return;
    try {
      final vm = await _client.getVm(widget.vmName);
      if (mounted) setState(() => _vm = vm);
    } on VmException {
      // Keep showing the connection error.
    }
  }

  void _snack(String text) => ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(text)));

  Future<void> _start() async {
    setState(() => _starting = true);
    try {
      await _client.start(widget.vmName);
      final vm = await _client.waitForState(widget.vmName, (s) => s == VmPowerState.running);
      if (!mounted) return;
      setState(() => _vm = vm ?? _vm);
      if (vm?.isRunning ?? false) {
        await _rfb.connect();
      } else {
        _snack("${widget.vmName} didn't start. Check it on the web dashboard.");
      }
    } on VmException catch (e) {
      if (mounted) _snack(e.message);
    } finally {
      if (mounted) setState(() => _starting = false);
    }
  }

  Future<void> _power(BuildContext sheetContext) async {
    final vm = _vm;
    if (vm == null) return;
    final action = await showModalBottomSheet<_Power>(
      context: sheetContext,
      showDragHandle: true,
      useSafeArea: true,
      builder: (context) => _PowerSheet(vm: vm),
    );
    if (action == null || !mounted || !sheetContext.mounted) return;
    final name = widget.vmName;
    switch (action) {
      case _Power.shutdown:
        await _do(() => _client.shutdown(name), 'Asked $name to shut down');
      case _Power.pause:
        await _do(() => _client.pause(name), 'Paused $name');
      case _Power.resume:
        await _do(() => _client.resume(name), 'Resumed $name');
      case _Power.reset:
        if (await ConfirmDialog.destructive(sheetContext,
            title: 'Reset “$name”?',
            message: 'It restarts at once, without shutting down. Anything not saved in the VM is lost.',
            confirmLabel: 'Reset',
            permanent: false)) {
          await _do(() => _client.reset(name), 'Reset $name');
        }
      case _Power.forceStop:
        if (await ConfirmDialog.destructive(sheetContext,
            title: 'Force stop “$name”?',
            message: 'This cuts the power, like pulling the plug. Anything not saved in the VM is lost.',
            confirmLabel: 'Force stop',
            permanent: false)) {
          await _do(() => _client.forceOff(name), 'Stopped $name');
        }
    }
    try {
      final fresh = await _client.getVm(name);
      if (mounted) setState(() => _vm = fresh);
    } on VmException {
      // The next status change reads it again.
    }
  }

  Future<void> _do(Future<void> Function() action, String done) async {
    try {
      await action();
      if (mounted) _snack(done);
    } on VmException catch (e) {
      if (mounted) _snack(e.message);
    }
  }

  Future<void> _snapshots(BuildContext sheetContext) => showModalBottomSheet<void>(
        context: sheetContext,
        showDragHandle: true,
        useSafeArea: true,
        isScrollControlled: true,
        builder: (_) => SnapshotsSheet(client: _client, vmName: widget.vmName),
      );

  Future<void> _disc(BuildContext sheetContext) async {
    final vm = _vm;
    if (vm == null) return;
    await showModalBottomSheet<void>(
      context: sheetContext,
      showDragHandle: true,
      useSafeArea: true,
      isScrollControlled: true,
      builder: (_) => DiscSheet(client: _client, vm: vm),
    );
    try {
      final fresh = await _client.getVm(widget.vmName);
      if (mounted) setState(() => _vm = fresh);
    } on VmException {
      // Shown next time.
    }
  }

  Widget? _placeholder() {
    if (_loading && _vm == null) {
      return const ConsolePlaceholder(icon: Icons.desktop_windows_outlined, title: '', message: '', loading: true);
    }
    final error = _loadError;
    if (error != null && _vm == null) {
      return ConsolePlaceholder(
        icon: error.kind == VmErrorKind.offline ? Icons.cloud_off_outlined : Icons.error_outline,
        title: error.kind == VmErrorKind.offline ? "Can't reach the server" : "Couldn't open ${widget.vmName}",
        message: error.message,
        actionLabel: 'Retry',
        onAction: _load,
      );
    }
    final vm = _vm;
    if (vm != null && !vm.isActive && _rfb.status.value != RfbStatus.connecting) {
      final crashed = vm.powerState == VmPowerState.crashed;
      return ConsolePlaceholder(
        icon: Icons.power_settings_new_outlined,
        title: crashed ? '${vm.name} crashed' : '${vm.name} is off',
        message: 'Start it to use its screen here.',
        actionLabel: 'Start',
        busy: _starting,
        onAction: _start,
      );
    }
    return null;
  }

  @override
  Widget build(BuildContext context) {
    final vm = _vm;
    final active = vm?.isActive ?? false;
    return RemoteConsoleFrame(
      client: _rfb,
      title: widget.vmName,
      clipboardTarget: widget.vmName,
      history: widget.history,
      clipboardHint: vm == null
          ? null
          : vm.clipboardChannel
              ? 'Copy and paste needs NivaroOS Guest Tools installed in the VM. If nothing arrives, use Type it.'
              : 'Clipboard sharing turns on the next time this VM starts. Until then, use Type it.',
      placeholder: _placeholder(),
      placeholderStatus: vm == null
          ? null
          : vm.powerState == VmPowerState.crashed
              ? 'Crashed'
              : vm.isActive
                  ? null
                  : 'Off',
      menuItems: [
        if (active) ConsoleMenuItem(icon: Icons.power_settings_new_outlined, label: 'Power', onPressed: _power),
        if (vm != null) ConsoleMenuItem(icon: Icons.history_outlined, label: 'Snapshots', onPressed: _snapshots),
        if (vm != null) ConsoleMenuItem(icon: Icons.album_outlined, label: 'Disc drive', onPressed: _disc),
      ],
    );
  }
}

enum _Power { shutdown, pause, resume, reset, forceStop }

class _PowerSheet extends StatelessWidget {
  const _PowerSheet({required this.vm});

  final Vm vm;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final paused = vm.powerState == VmPowerState.paused;
    void pick(_Power p) => Navigator.of(context).pop(p);
    return ListView(
      shrinkWrap: true,
      padding: const EdgeInsets.only(bottom: Space.lg),
      children: [
        Padding(
          padding: EdgeInsets.fromLTRB(Space.gutter(context), 0, Space.gutter(context), Space.sm),
          child: Semantics(header: true, child: Text('Power', style: theme.textTheme.titleLarge)),
        ),
        if (!paused) ...[
          ListTile(
            leading: const Icon(Icons.power_settings_new_outlined),
            title: const Text('Shut down'),
            subtitle: const Text('Asks the VM to shut down, like pressing its power button'),
            onTap: () => pick(_Power.shutdown),
          ),
          ListTile(
            leading: const Icon(Icons.pause_circle_outline),
            title: const Text('Pause'),
            subtitle: const Text('Freezes it where it is, until you resume it'),
            onTap: () => pick(_Power.pause),
          ),
          ListTile(
            leading: const Icon(Icons.restart_alt_outlined),
            title: const Text('Reset'),
            subtitle: const Text('Restarts it at once, without shutting down'),
            onTap: () => pick(_Power.reset),
          ),
        ] else
          ListTile(
            leading: const Icon(Icons.play_arrow_outlined),
            title: const Text('Resume'),
            onTap: () => pick(_Power.resume),
          ),
        ListTile(
          leading: const Icon(Icons.power_off_outlined),
          title: const Text('Force stop'),
          subtitle: const Text('Cuts the power. Unsaved work in the VM is lost'),
          onTap: () => pick(_Power.forceStop),
        ),
      ],
    );
  }
}

/// A few skeleton rows for a sheet that is loading.
class _SheetSkeleton extends StatelessWidget {
  const _SheetSkeleton();

  static const rows = 3;

  @override
  Widget build(BuildContext context) {
    final gutter = Space.gutter(context);
    final scaler = MediaQuery.textScalerOf(context);
    return Semantics(
      label: 'Loading',
      child: ExcludeSemantics(
        child: SkeletonPulse(
          child: Column(children: [
            for (var i = 0; i < rows; i++)
              Padding(
                padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.md),
                child: Row(children: [
                  const SkeletonBox(width: 24, height: 24, radius: 12),
                  const SizedBox(width: Space.lg),
                  Expanded(
                    child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                      FractionallySizedBox(widthFactor: [0.6, 0.45, 0.7][i % 3], child: SkeletonBox(height: scaler.scale(16))),
                      const SizedBox(height: Space.sm),
                      FractionallySizedBox(widthFactor: 0.35, child: SkeletonBox(height: scaler.scale(12))),
                    ]),
                  ),
                ]),
              ),
          ]),
        ),
      ),
    );
  }
}

/// A short message with an optional Retry, inside a sheet.
class _SheetMessage extends StatelessWidget {
  const _SheetMessage({required this.text, this.onRetry, this.error = false});

  final String text;
  final VoidCallback? onRetry;
  final bool error;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter - Space.md, Space.sm),
      child: Row(children: [
        Expanded(
          child: Text(text,
              style: theme.textTheme.bodyMedium
                  ?.copyWith(color: error ? theme.colorScheme.error : theme.colorScheme.onSurfaceVariant)),
        ),
        if (onRetry != null) TextButton(onPressed: onRetry, child: const Text('Retry')),
      ]),
    );
  }
}

/// A VM's snapshots: take one, go back to one, delete one.
class SnapshotsSheet extends StatefulWidget {
  const SnapshotsSheet({super.key, required this.client, required this.vmName});

  final VmClient client;
  final String vmName;

  @override
  State<SnapshotsSheet> createState() => _SnapshotsSheetState();
}

class _SnapshotsSheetState extends State<SnapshotsSheet> {
  List<VmSnapshot>? _snapshots;
  VmException? _error;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final list = await widget.client.listSnapshots(widget.vmName);
      list.sort((a, b) => (b.creationTime ?? DateTime(0)).compareTo(a.creationTime ?? DateTime(0)));
      if (mounted) setState(() => _snapshots = list);
    } on VmException catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  void _snack(String text) => ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(text)));

  Future<void> _run(Future<void> Function() action, String done) async {
    setState(() => _busy = true);
    try {
      await action();
      if (mounted) _snack(done);
      await _load();
    } on VmException catch (e) {
      if (mounted) _snack(e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _take() async {
    final name = await showDialog<String>(context: context, builder: (_) => const _SnapshotNameDialog());
    if (name == null || !mounted) return;
    await _run(() => widget.client.createSnapshot(widget.vmName, snapName: name), 'Snapshot taken');
  }

  Future<void> _revert(VmSnapshot s) async {
    final ok = await ConfirmDialog.destructive(
      context,
      title: 'Go back to “${s.name}”?',
      message: '${widget.vmName} returns to how it was when this snapshot was taken. Changes made since then are lost.',
      confirmLabel: 'Go back',
    );
    if (ok && mounted) await _run(() => widget.client.revertSnapshot(widget.vmName, s.name), 'Went back to ${s.name}');
  }

  Future<void> _delete(VmSnapshot s) async {
    final ok = await ConfirmDialog.destructive(
      context,
      title: 'Delete snapshot “${s.name}”?',
      message: "The VM itself doesn't change; you just can't go back to this point any more.",
      confirmLabel: 'Delete',
    );
    if (ok && mounted) await _run(() => widget.client.deleteSnapshot(widget.vmName, s.name), 'Deleted ${s.name}');
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    final list = _snapshots;
    return ListView(
      shrinkWrap: true,
      padding: const EdgeInsets.only(bottom: Space.lg),
      children: [
        Padding(
          padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.sm),
          child: Row(children: [
            Expanded(child: Semantics(header: true, child: Text('Snapshots', style: theme.textTheme.titleLarge))),
            const SizedBox(width: Space.md),
            FilledButton.tonalIcon(style: tonalButtonStyle(context), 
              onPressed: _busy || list == null ? null : _take,
              icon: const Icon(Icons.add),
              label: const Text('Take snapshot'),
            ),
          ]),
        ),
        if (_busy) const LinearProgressIndicator(),
        if (_error != null)
          _SheetMessage(text: "Couldn't load snapshots. ${_error!.message}", onRetry: _load, error: true)
        else if (list == null)
          const _SheetSkeleton()
        else if (list.isEmpty)
          const _SheetMessage(text: 'No snapshots yet. Take one before a risky change, so you can go back to it.')
        else
          for (final s in list)
            ListTile(
              leading: const Icon(Icons.history_outlined),
              title: Text(s.name, maxLines: 1, overflow: TextOverflow.ellipsis),
              subtitle: Text([
                s.creationTime == null ? 'Date unknown' : formatExact(s.creationTime!),
                s.includesMemory ? 'with memory' : 'disks only',
                if (s.current) 'current',
              ].join(' · ')),
              trailing: MenuAnchor(
                builder: (context, controller, _) => IconButton(
                  tooltip: 'Actions for ${s.name}',
                  icon: const Icon(Icons.more_vert),
                  onPressed: _busy ? null : () => controller.isOpen ? controller.close() : controller.open(),
                ),
                menuChildren: [
                  MenuItemButton(
                    leadingIcon: const Icon(Icons.restore_outlined),
                    onPressed: () => _revert(s),
                    child: const Text('Go back to this'),
                  ),
                  MenuItemButton(
                    leadingIcon: Icon(Icons.delete_outline, color: theme.colorScheme.error),
                    onPressed: () => _delete(s),
                    child: Text('Delete', style: TextStyle(color: theme.colorScheme.error)),
                  ),
                ],
              ),
            ),
      ],
    );
  }
}

class _SnapshotNameDialog extends StatefulWidget {
  const _SnapshotNameDialog();

  @override
  State<_SnapshotNameDialog> createState() => _SnapshotNameDialogState();
}

class _SnapshotNameDialogState extends State<_SnapshotNameDialog> {
  final _name = TextEditingController();
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  void _submit() {
    final name = _name.text.trim();
    if (name.contains('/')) {
      setState(() => _error = 'A name can’t contain “/”');
      return;
    }
    Navigator.of(context).pop(name);
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Take a snapshot'),
      content: TextField(
        controller: _name,
        autofocus: true,
        textInputAction: TextInputAction.done,
        onSubmitted: (_) => _submit(),
        decoration: InputDecoration(
          labelText: 'Name',
          helperText: 'Leave it empty to name it by date and time',
          errorText: _error,
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        TextButton(onPressed: _submit, child: const Text('Take snapshot')),
      ],
    );
  }
}

/// The VM's virtual disc drive: what's in it, eject, and insert an ISO.
class DiscSheet extends StatefulWidget {
  const DiscSheet({super.key, required this.client, required this.vm});

  final VmClient client;
  final Vm vm;

  @override
  State<DiscSheet> createState() => _DiscSheetState();
}

class _DiscSheetState extends State<DiscSheet> {
  List<VmIso>? _isos;
  VmException? _error;
  late String? _inserted = widget.vm.isoPath;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final isos = await widget.client.listIsos();
      if (mounted) setState(() => _isos = isos);
    } on VmException catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _run(Future<void> Function() action, String? inserted, String done) async {
    setState(() => _busy = true);
    try {
      await action();
      if (!mounted) return;
      setState(() => _inserted = inserted);
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(done)));
    } on VmException catch (e) {
      if (mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(e.message)));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  static String _size(int mib) => mib >= 1024 ? '${(mib / 1024).toStringAsFixed(1)} GB' : '$mib MB';

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    final name = widget.vm.name;
    final inserted = _inserted;
    final isos = _isos;
    return ListView(
      shrinkWrap: true,
      padding: const EdgeInsets.only(bottom: Space.lg),
      children: [
        Padding(
          padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.sm),
          child: Semantics(header: true, child: Text('Disc drive', style: theme.textTheme.titleLarge)),
        ),
        if (_busy) const LinearProgressIndicator(),
        ListTile(
          leading: const Icon(Icons.album_outlined),
          title: Text(inserted == null ? 'Empty' : inserted.split('/').last, maxLines: 2, overflow: TextOverflow.ellipsis),
          subtitle: Text(inserted == null ? 'No disc inserted' : 'In the drive now'),
          trailing: inserted == null
              ? null
              : TextButton(
                  onPressed: _busy ? null : () => _run(() => widget.client.ejectCDROM(name), null, 'Ejected the disc'),
                  child: const Text('Eject'),
                ),
        ),
        const SectionHeader(title: 'Insert a disc'),
        if (_error != null)
          _SheetMessage(text: "Couldn't load the ISO list. ${_error!.message}", onRetry: _load, error: true)
        else if (isos == null)
          const _SheetSkeleton()
        else if (isos.isEmpty)
          const _SheetMessage(text: 'No ISO files on the server yet. Upload them in the web dashboard, to ${VmIso.folder}.')
        else
          for (final iso in isos)
            ListTile(
              enabled: !_busy,
              leading: const Icon(Icons.album_outlined),
              title: Text(iso.name, maxLines: 2, overflow: TextOverflow.ellipsis),
              subtitle: iso.sizeMib > 0 ? Text(_size(iso.sizeMib)) : null,
              selected: iso.path == inserted,
              trailing: iso.path == inserted ? const Icon(Icons.check, semanticLabel: 'In the drive') : null,
              onTap: iso.path == inserted
                  ? null
                  : () => _run(() => widget.client.insertCDROM(name, iso.path), iso.path, 'Inserted ${iso.name}'),
            ),
      ],
    );
  }
}
