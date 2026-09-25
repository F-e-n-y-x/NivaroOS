import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../services/api_client.dart';
import '../services/vm_client.dart';
import '../ui/ui.dart';
import 'vm_console_screen.dart';
import 'vm_form_screen.dart';

/// "2 GB", "1.5 GB", "512 MB".
String formatMemoryMib(int mib) {
  if (mib <= 0) return '—';
  // A no-break space keeps number and unit on one line.
  if (mib < 1024) return '$mib\u00a0MB';
  final gb = mib / 1024;
  return gb == gb.roundToDouble() ? '${gb.round()}\u00a0GB' : '${gb.toStringAsFixed(1)}\u00a0GB';
}

/// "4 CPUs · 4 GB memory · 60 GB disk" - what a VM has, in one line.
String vmSummary(Vm vm) {
  final cpus = vm.vcpus == 1 ? '1\u00a0CPU' : '${vm.vcpus}\u00a0CPUs';
  final disk = vm.disks.isEmpty && vm.diskGib == 0
      ? 'no disk'
      : vm.disks.length > 1
          ? '${vm.disks.length} disks, ${vm.totalDiskGib}\u00a0GB'
          : '${vm.totalDiskGib}\u00a0GB disk';
  return '$cpus · ${formatMemoryMib(vm.memoryMib)} memory · $disk';
}

/// A VM's state as a chip: icon and word, never colour alone.
class VmStateChip extends StatelessWidget {
  const VmStateChip({super.key, required this.state, this.busyLabel});

  final VmPowerState state;

  /// While an action is under way ("Starting"), shown instead of the state.
  final String? busyLabel;

  static (String, Status, IconData) describe(VmPowerState state) => switch (state) {
        VmPowerState.running => ('Running', Status.success, Icons.play_circle_outline),
        VmPowerState.paused => ('Paused', Status.warning, Icons.pause_circle_outline),
        VmPowerState.stopped => ('Off', Status.neutral, Icons.stop_circle_outlined),
        VmPowerState.crashed => ('Crashed', Status.error, Icons.error_outline),
        VmPowerState.suspended => ('Sleeping', Status.info, Icons.bedtime_outlined),
        VmPowerState.unknown => ('Unknown', Status.neutral, Icons.help_outline),
      };

  @override
  Widget build(BuildContext context) {
    final busy = busyLabel;
    if (busy != null) return StatusChip(label: busy, status: Status.info, icon: Icons.hourglass_top_outlined);
    final (label, status, icon) = describe(state);
    return StatusChip(label: label, status: status, icon: icon);
  }
}

/// The Virtual machines tab: every VM with its state, a live picture of
/// the running ones, and the next thing to do with each (Start, Console,
/// Shut down); everything else in its menu.
class VmListScreen extends StatefulWidget {
  const VmListScreen({super.key, this.client, this.refreshEvery = const Duration(seconds: 5)});

  /// For tests; the app uses the session's gateway client.
  final VmClient? client;

  /// How often states and pictures refresh while the tab is on screen.
  final Duration refreshEvery;

  /// Forgets the lists and pictures kept for the next visit (tests).
  @visibleForTesting
  static void clearCache() {
    _cache.clear();
    _thumbnails.clear();
  }

  @override
  State<VmListScreen> createState() => _VmListScreenState();
}

/// The last list per server, shown at once on the next visit and while
/// offline.
final Map<String, ({List<Vm> vms, DateTime at})> _cache = {};

class _VmListScreenState extends State<VmListScreen> with WidgetsBindingObserver {
  late final VmClient _client = widget.client ?? VmClient();
  List<Vm>? _vms;
  DateTime? _loadedAt;
  VmException? _error;
  bool _loading = false;
  bool _notSetUp = false;
  int _tick = 0;
  Timer? _timer;

  /// VM name -> what is being done to it ("Starting").
  final Map<String, String> _busy = {};

  String get _cacheKey => ApiClient.instance.baseUrl;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    final cached = _cache[_cacheKey];
    if (cached != null) {
      _vms = cached.vms;
      _loadedAt = cached.at;
    }
    _load();
    _timer = Timer.periodic(widget.refreshEvery, (_) => _poll());
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _timer?.cancel();
    if (widget.client == null) _client.close();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed && _onScreen) _load(quiet: true);
  }

  // The tab stays alive in the shell's IndexedStack, and the console is
  // pushed over it: only poll while this list is what the user sees.
  bool get _onScreen {
    if (!mounted) return false;
    final lifecycle = WidgetsBinding.instance.lifecycleState;
    if (lifecycle != null && lifecycle != AppLifecycleState.resumed) return false;
    if (!(ModalRoute.of(context)?.isCurrent ?? true)) return false;
    return Visibility.of(context);
  }

  void _poll() {
    if (!_onScreen || _loading || _notSetUp) return;
    _load(quiet: true);
  }

  Future<void> _load({bool quiet = false}) async {
    if (_loading) return;
    _loading = true;
    if (!quiet) setState(() {});
    try {
      final vms = await _client.listVms();
      if (!mounted) return;
      final now = clock.now();
      _cache[_cacheKey] = (vms: vms, at: now);
      setState(() {
        _vms = vms;
        _loadedAt = now;
        _error = null;
        _notSetUp = false;
        _tick++;
      });
    } on VmException catch (e) {
      var notSetUp = false;
      if (e.kind != VmErrorKind.offline && e.kind != VmErrorKind.unauthorized) {
        try {
          notSetUp = !(await _client.setupStatus()).ready;
        } on VmException {
          // Keep the original error.
        }
      }
      if (!mounted) return;
      setState(() {
        _error = e;
        _notSetUp = notSetUp;
      });
    } finally {
      _loading = false;
      if (mounted && !quiet) setState(() {});
    }
  }

  void _snack(String message) =>
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(message)));

  /// Runs [action] on [vm], shows [label] on its chip until the VM reaches
  /// a state [done] accepts, and says so if it doesn't.
  Future<void> _run(
    Vm vm,
    String label,
    Future<void> Function(String) action, {
    bool Function(VmPowerState)? done,
    Duration wait = const Duration(seconds: 20),
    String? stillMessage,
  }) async {
    setState(() => _busy[vm.name] = label);
    try {
      await action(vm.name);
      if (done != null) {
        final after = await _client.waitForState(vm.name, done, timeout: wait);
        if (after != null && !done(after.powerState) && stillMessage != null && mounted) _snack(stillMessage);
      }
    } on VmException catch (e) {
      if (mounted) _snack(e.message);
    } finally {
      if (mounted) setState(() => _busy.remove(vm.name));
      await _load(quiet: true);
    }
  }

  void _start(Vm vm) => _run(vm, 'Starting', _client.start, done: (s) => s == VmPowerState.running);

  void _resume(Vm vm) => _run(vm, 'Resuming', _client.resume, done: (s) => s == VmPowerState.running);

  void _pause(Vm vm) => _run(vm, 'Pausing', _client.pause, done: (s) => s == VmPowerState.paused);

  void _shutdown(Vm vm) => _run(
        vm,
        'Shutting down',
        _client.shutdown,
        done: (s) => s == VmPowerState.stopped,
        wait: const Duration(seconds: 60),
        stillMessage:
            "${vm.name} is still running. It may be waiting for someone to confirm, or it ignores the power button. Use Force stop to turn it off.",
      );

  Future<void> _reset(Vm vm) async {
    final ok = await ConfirmDialog.destructive(
      context,
      title: 'Reset “${vm.name}”?',
      message: 'It restarts at once, without shutting down. Anything not saved in the VM is lost.',
      confirmLabel: 'Reset',
      permanent: false,
    );
    if (ok && mounted) await _run(vm, 'Resetting', _client.reset);
  }

  Future<void> _forceStop(Vm vm) async {
    final ok = await ConfirmDialog.destructive(
      context,
      title: 'Force stop “${vm.name}”?',
      message: 'This cuts the power, like pulling the plug. Anything not saved in the VM is lost.',
      confirmLabel: 'Force stop',
      permanent: false,
    );
    if (ok && mounted) await _run(vm, 'Stopping', _client.forceOff, done: (s) => s == VmPowerState.stopped);
  }

  Future<void> _delete(Vm vm) async {
    final wipe = await DeleteVmDialog.show(context, vm);
    if (wipe == null || !mounted) return;
    setState(() => _busy[vm.name] = 'Deleting');
    try {
      await _client.deleteVm(vm.name, wipeDisk: wipe);
      if (mounted) _snack(wipe ? 'Deleted ${vm.name} and its disks' : 'Deleted ${vm.name}. Its disks are still on the server.');
    } on VmException catch (e) {
      if (mounted) _snack(e.message);
    } finally {
      if (mounted) setState(() => _busy.remove(vm.name));
      await _load(quiet: true);
    }
  }

  Future<void> _openForm([Vm? vm]) async {
    final saved = await Navigator.of(context).push<bool>(MaterialPageRoute(
      fullscreenDialog: true,
      builder: (_) => VmFormScreen(
        client: _client,
        existing: vm,
        takenNames: [for (final v in _vms ?? const <Vm>[]) v.name],
      ),
    ));
    if (saved == true && mounted) {
      _snack(vm == null ? 'Virtual machine created' : 'Saved ${vm.name}');
      await _load(quiet: true);
    }
  }

  Future<void> _openConsole(Vm vm) async {
    await Navigator.of(context).push(MaterialPageRoute(
      builder: (_) => VmConsoleScreen(vmName: vm.name, client: widget.client),
    ));
    if (mounted) _load(quiet: true);
  }

  Future<void> _showActions(Vm vm) async {
    final action = await showModalBottomSheet<void Function()>(
      context: context,
      showDragHandle: true,
      useSafeArea: true,
      isScrollControlled: true,
      builder: (context) => _VmActionsSheet(vm: vm, onPick: (a) => Navigator.of(context).pop(a), state: this),
    );
    action?.call();
  }

  @override
  Widget build(BuildContext context) {
    final vms = _vms;
    final error = _error;
    final showBanner = error != null && vms != null && (error.kind == VmErrorKind.offline || error.kind == VmErrorKind.unavailable);
    return AppScaffold.slivers(
      title: 'Virtual machines',
      onRefresh: _load,
      banner: showBanner ? OfflineBanner(lastUpdated: _loadedAt, onRetry: _load) : null,
      // Not while the list is empty: the empty state has the same button,
      // and one action shouldn't be offered twice.
      floatingActionButton: vms != null && vms.isNotEmpty && !_notSetUp
          ? FloatingActionButton.extended(
              onPressed: () => _openForm(),
              icon: const Icon(Icons.add),
              label: const Text('New VM'),
            )
          : null,
      slivers: [_content(context, vms, error)],
    );
  }

  Widget _content(BuildContext context, List<Vm>? vms, VmException? error) {
    if (_notSetUp) {
      return EmptyState(
        sliver: true,
        icon: Icons.computer_outlined,
        title: "VMs aren't set up yet",
        message: 'Set them up once in the NivaroOS web dashboard (open VMs there), then check again here.',
        actionLabel: 'Check again',
        onAction: _load,
      );
    }
    if (vms == null) {
      if (error == null) return const _SkeletonCards();
      return switch (error.kind) {
        VmErrorKind.offline => ErrorState.offline(sliver: true, onRetry: _load, details: error.details),
        VmErrorKind.unauthorized => ErrorState(
            sliver: true,
            icon: Icons.lock_outline,
            title: 'Signed out',
            message: 'Your session with the server ended. Sign in again to see your virtual machines.',
            onRetry: _load,
          ),
        _ => ErrorState(
            sliver: true,
            title: "Couldn't load virtual machines",
            message: error.message,
            details: error.details,
            onRetry: _load,
          ),
      };
    }
    if (vms.isEmpty) {
      return EmptyState(
        sliver: true,
        icon: Icons.computer_outlined,
        title: 'No virtual machines yet',
        message: 'Run Windows or Linux on the server. Create a VM, start it, then use its console from here.',
        actionLabel: 'New VM',
        onAction: () => _openForm(),
      );
    }
    final running = vms.where((v) => v.isActive).length;
    final gutter = Space.gutter(context);
    final theme = Theme.of(context);
    return SliverList.list(children: [
      Padding(
        padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.md),
        child: Text(
          running == 0 ? '${_count(vms.length)} · none running' : '${_count(vms.length)} · $running running',
          style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant),
        ),
      ),
      for (final vm in vms)
        Padding(
          key: ValueKey(vm.name),
          padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.md),
          child: _VmCard(
            vm: vm,
            client: _client,
            tick: _tick,
            busyLabel: _busy[vm.name],
            onStart: () => _start(vm),
            onResume: () => _resume(vm),
            onShutdown: () => _shutdown(vm),
            onConsole: () => _openConsole(vm),
            onMore: () => _showActions(vm),
          ),
        ),
      // Room for the extended FAB over the last card; it grows with the
      // text size.
      SizedBox(height: Space.lg + MediaQuery.textScalerOf(context).scale(56)),
    ]);
  }

  static String _count(int n) => n == 1 ? '1 virtual machine' : '$n virtual machines';
}

class _VmCard extends StatelessWidget {
  const _VmCard({
    required this.vm,
    required this.client,
    required this.tick,
    required this.busyLabel,
    required this.onStart,
    required this.onResume,
    required this.onShutdown,
    required this.onConsole,
    required this.onMore,
  });

  final Vm vm;
  final VmClient client;
  final int tick;
  final String? busyLabel;
  final VoidCallback onStart;
  final VoidCallback onResume;
  final VoidCallback onShutdown;
  final VoidCallback onConsole;
  final VoidCallback onMore;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final state = vm.powerState;
    final busy = busyLabel != null;
    final showPicture = state == VmPowerState.running || state == VmPowerState.paused;
    final large = MediaQuery.textScalerOf(context).scale(10) > 13;

    final Widget primary = switch (state) {
      VmPowerState.running => FilledButton.tonalIcon(
          onPressed: busy ? null : onConsole,
          icon: const Icon(Icons.desktop_windows_outlined),
          label: const Text('Console'),
        ),
      VmPowerState.paused => FilledButton.tonalIcon(
          onPressed: busy ? null : onResume,
          icon: const Icon(Icons.play_arrow_outlined),
          label: const Text('Resume'),
        ),
      _ => FilledButton.tonalIcon(
          onPressed: busy ? null : onStart,
          icon: const Icon(Icons.play_arrow_outlined),
          label: const Text('Start'),
        ),
    };

    return Card.outlined(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(Space.lg, Space.lg, Space.lg, Space.md),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(
                      child: Padding(
                        padding: const EdgeInsets.only(top: 2),
                        child: Text(vm.name, style: theme.textTheme.titleMedium, maxLines: 2, overflow: TextOverflow.ellipsis),
                      ),
                    ),
                    const SizedBox(width: Space.md),
                    VmStateChip(state: state, busyLabel: busyLabel),
                    if (large) _menu(busy),
                  ],
                ),
                const SizedBox(height: Space.xs),
                Text(vmSummary(vm), style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
              ],
            ),
          ),
          if (showPicture)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: Space.lg),
              // A paused VM's picture is dimmed: it is frozen, not live.
              child: Opacity(
                opacity: state == VmPowerState.paused ? 0.6 : 1,
                child: VmThumbnail(client: client, vm: vm, tick: tick, onTap: state == VmPowerState.running ? onConsole : null),
              ),
            ),
          if (large) _largeActions(primary, busy) else _actions(primary, busy),
        ],
      ),
    );
  }

  Widget _menu(bool busy) => busy
      ? Padding(
          padding: const EdgeInsets.symmetric(horizontal: Space.md),
          child: Semantics(
            label: busyLabel,
            child: const SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2.5)),
          ),
        )
      : IconButton(tooltip: 'More for ${vm.name}', icon: const Icon(Icons.more_vert), onPressed: onMore);

  Widget _actions(Widget primary, bool busy) => Padding(
        padding: const EdgeInsets.fromLTRB(Space.lg, Space.md, Space.sm, Space.md),
        child: Row(
          children: [
            Expanded(
              child: Wrap(
                spacing: Space.sm,
                runSpacing: Space.sm,
                children: [
                  primary,
                  if (vm.powerState == VmPowerState.running)
                    OutlinedButton(onPressed: busy ? null : onShutdown, child: const Text('Shut down')),
                ],
              ),
            ),
            _menu(busy),
          ],
        ),
      );

  // At large text the buttons don't fit side by side: they stack at full
  // width, and the menu moves up beside the name.
  Widget _largeActions(Widget primary, bool busy) => Padding(
        padding: const EdgeInsets.fromLTRB(Space.lg, Space.md, Space.lg, Space.lg),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            primary,
            if (vm.powerState == VmPowerState.running) ...[
              const SizedBox(height: Space.sm),
              OutlinedButton(onPressed: busy ? null : onShutdown, child: const Text('Shut down')),
            ],
          ],
        ),
      );
}

/// A VM's current screen, refreshed whenever [tick] changes. Fetched with
/// the session's auth (header, refreshed on 401) rather than an image URL
/// carrying the token (plan M-03). Other screens can use it too (Home's
/// running VMs).
class VmThumbnail extends StatefulWidget {
  const VmThumbnail({super.key, required this.client, required this.vm, this.tick = 0, this.onTap});

  final VmClient client;
  final Vm vm;
  final int tick;
  final VoidCallback? onTap;

  @override
  State<VmThumbnail> createState() => _VmThumbnailState();
}

/// Last picture per VM, so a rebuilt list doesn't flash empty boxes.
final Map<String, Uint8List> _thumbnails = {};

class _VmThumbnailState extends State<VmThumbnail> {
  Uint8List? _bytes;
  bool _failed = false;
  bool _fetching = false;

  @override
  void initState() {
    super.initState();
    _bytes = _thumbnails[widget.vm.name];
    _fetch();
  }

  @override
  void didUpdateWidget(VmThumbnail oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.tick != widget.tick || oldWidget.vm.name != widget.vm.name) _fetch();
  }

  Future<void> _fetch() async {
    if (_fetching) return;
    _fetching = true;
    try {
      final bytes = await widget.client.screenshot(widget.vm.name);
      if (!mounted) return;
      _thumbnails[widget.vm.name] = bytes;
      setState(() {
        _bytes = bytes;
        _failed = false;
      });
    } on VmException {
      if (mounted) setState(() => _failed = true);
    } finally {
      _fetching = false;
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final bytes = _bytes;
    final Widget content = bytes != null
        ? Image.memory(
            bytes,
            fit: BoxFit.cover,
            gaplessPlayback: true,
            excludeFromSemantics: true,
            errorBuilder: (_, _, _) => _placeholder(scheme, failed: true),
          )
        : _placeholder(scheme, failed: _failed);
    return Semantics(
      button: widget.onTap != null,
      label: widget.onTap != null ? 'Screen of ${widget.vm.name}. Opens the console' : 'Screen of ${widget.vm.name}',
      child: ClipRRect(
        borderRadius: BorderRadius.circular(Corners.small),
        child: AspectRatio(
          aspectRatio: widget.vm.displayAspect,
          child: Material(
            color: scheme.surfaceContainerHighest,
            child: InkWell(onTap: widget.onTap, child: content),
          ),
        ),
      ),
    );
  }

  Widget _placeholder(ColorScheme scheme, {required bool failed}) => Center(
        child: Icon(failed ? Icons.desktop_access_disabled_outlined : Icons.desktop_windows_outlined,
            size: 32, color: scheme.onSurfaceVariant),
      );
}

/// Loading: cards shaped like the real ones.
class _SkeletonCards extends StatelessWidget {
  const _SkeletonCards();

  @override
  Widget build(BuildContext context) {
    final gutter = Space.gutter(context);
    final scaler = MediaQuery.textScalerOf(context);
    return SliverSemantics(
      label: 'Loading',
      liveRegion: true,
      sliver: SkeletonPulse.sliver(
        child: SliverList.list(children: [
          Padding(
            padding: EdgeInsets.fromLTRB(gutter, Space.xs, gutter, Space.md + Space.xs),
            child: Align(alignment: Alignment.centerLeft, child: SkeletonBox(width: 180, height: scaler.scale(14))),
          ),
          for (var i = 0; i < 3; i++)
            Padding(
              padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.md),
              child: ExcludeSemantics(
                child: Card.outlined(
                  child: Padding(
                    padding: const EdgeInsets.all(Space.lg),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(children: [
                          SkeletonBox(width: [120.0, 90.0, 140.0][i], height: scaler.scale(18)),
                          const Spacer(),
                          SkeletonBox(width: 72, height: scaler.scale(24), radius: Corners.small),
                        ]),
                        const SizedBox(height: Space.sm),
                        SkeletonBox(width: 220, height: scaler.scale(14)),
                        // The first card stands in for a running VM, which
                        // shows its screen.
                        if (i == 0) ...[
                          const SizedBox(height: Space.lg),
                          const AspectRatio(aspectRatio: 16 / 9, child: SkeletonBox(height: double.infinity, radius: Corners.small)),
                        ],
                        const SizedBox(height: Space.lg),
                        SkeletonBox(width: 104, height: 40, radius: 20),
                      ],
                    ),
                  ),
                ),
              ),
            ),
        ]),
      ),
    );
  }
}

/// Everything else for one VM, in a sheet.
class _VmActionsSheet extends StatelessWidget {
  const _VmActionsSheet({required this.vm, required this.onPick, required this.state});

  final Vm vm;
  final void Function(void Function() action) onPick;
  final _VmListScreenState state;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final power = vm.powerState;
    final running = power == VmPowerState.running;
    final paused = power == VmPowerState.paused;
    final gutter = Space.gutter(context);
    return ListView(
      shrinkWrap: true,
      padding: const EdgeInsets.only(bottom: Space.lg),
      children: [
        Padding(
          padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.sm),
          child: Row(
            children: [
              Expanded(
                child: Semantics(
                  header: true,
                  child: Text(vm.name, style: theme.textTheme.titleLarge, maxLines: 2, overflow: TextOverflow.ellipsis),
                ),
              ),
              const SizedBox(width: Space.md),
              VmStateChip(state: power),
            ],
          ),
        ),
        if (running)
          ListTile(
            leading: const Icon(Icons.desktop_windows_outlined),
            title: const Text('Open console'),
            onTap: () => onPick(() => state._openConsole(vm)),
          ),
        if (running)
          ListTile(
            leading: const Icon(Icons.pause_circle_outline),
            title: const Text('Pause'),
            subtitle: const Text('Freezes it where it is, until you resume it'),
            onTap: () => onPick(() => state._pause(vm)),
          ),
        if (paused)
          ListTile(
            leading: const Icon(Icons.play_arrow_outlined),
            title: const Text('Resume'),
            onTap: () => onPick(() => state._resume(vm)),
          ),
        if (running)
          ListTile(
            leading: const Icon(Icons.restart_alt_outlined),
            title: const Text('Reset'),
            subtitle: const Text('Restarts it at once, without shutting down'),
            onTap: () => onPick(() => state._reset(vm)),
          ),
        if (running || paused)
          ListTile(
            leading: const Icon(Icons.power_off_outlined),
            title: const Text('Force stop'),
            subtitle: const Text('Cuts the power. Unsaved work in the VM is lost'),
            onTap: () => onPick(() => state._forceStop(vm)),
          ),
        if (!running && !paused)
          ListTile(
            leading: const Icon(Icons.play_arrow_outlined),
            title: const Text('Start'),
            onTap: () => onPick(() => state._start(vm)),
          ),
        ListTile(
          leading: const Icon(Icons.tune_outlined),
          title: const Text('Edit settings'),
          subtitle: Text(vm.isActive ? 'CPU, memory and disks change only while it is off' : 'CPU, memory, disks, network'),
          onTap: () => onPick(() => state._openForm(vm)),
        ),
        ListTile(
          enabled: !vm.isActive,
          leading: Icon(Icons.delete_outline, color: vm.isActive ? null : scheme.error),
          title: Text('Delete', style: vm.isActive ? null : TextStyle(color: scheme.error)),
          subtitle: vm.isActive ? const Text('Shut it down first') : null,
          onTap: () => onPick(() => state._delete(vm)),
        ),
      ],
    );
  }
}

/// "Delete “mint”?" with the choice to delete its disks too. Returns
/// null for Cancel, otherwise whether to wipe the disks.
class DeleteVmDialog extends StatefulWidget {
  const DeleteVmDialog({super.key, required this.vm});

  final Vm vm;

  static Future<bool?> show(BuildContext context, Vm vm) =>
      showDialog<bool>(context: context, builder: (_) => DeleteVmDialog(vm: vm));

  @override
  State<DeleteVmDialog> createState() => _DeleteVmDialogState();
}

class _DeleteVmDialogState extends State<DeleteVmDialog> {
  bool _wipe = false;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final vm = widget.vm;
    final size = vm.totalDiskGib > 0 ? '${vm.totalDiskGib} GB. ' : '';
    return AlertDialog(
      title: Text('Delete “${vm.name}”?'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text("The VM is removed from the server. This can't be undone."),
          const SizedBox(height: Space.sm),
          CheckboxListTile(
            contentPadding: EdgeInsets.zero,
            value: _wipe,
            onChanged: (v) => setState(() => _wipe = v ?? false),
            title: const Text('Also delete its disks'),
            subtitle: Text('${size}Everything stored inside the VM is lost.'),
            controlAffinity: ListTileControlAffinity.leading,
          ),
        ],
      ),
      actions: [
        TextButton(autofocus: true, onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton(
          style: FilledButton.styleFrom(backgroundColor: scheme.error, foregroundColor: scheme.onError),
          onPressed: () {
            HapticFeedback.heavyImpact();
            Navigator.of(context).pop(_wipe);
          },
          child: Text(_wipe ? 'Delete VM and disks' : 'Delete VM'),
        ),
      ],
    );
  }
}
