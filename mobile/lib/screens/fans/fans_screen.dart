/// Fans: live speeds and the temperatures they follow, the fan profile,
/// and each fan's mode (Auto / Fixed / Curve). Opened from the Processor
/// and Graphics detail pages and from Server health. The server enforces
/// every safety rule (floor, emergency speed, stall and sensor fallbacks,
/// hand-back on stop); this screen only offers what it will accept.
library;

import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';

import '../../models/fans.dart';
import '../../services/api_client.dart';
import '../../services/fans_api.dart';
import '../../ui/ui.dart';
import 'fan_curve_view.dart';

/// Polls /v1/fans/status while a fans screen is open and runs the changes.
class FansController extends ChangeNotifier {
  FansController({FansApi? api, this.interval = const Duration(seconds: 2)}) : api = api ?? FansApi();

  final FansApi api;
  final Duration interval;

  FansStatus? status;
  Object? loadError;
  bool unavailable = false;
  DateTime? updatedAt;

  /// The last failed change, shown until dismissed.
  String? actionError;

  /// What is being changed (`preset`, `fan:ID`, `identify:ID`).
  String busy = '';

  Timer? _timer;
  int _listeners = 0;
  bool _disposed = false;

  bool get stale => loadError != null && status != null;

  /// Screens call [attach]/[detach]; polling runs while any is attached.
  void attach() {
    _listeners++;
    if (_listeners == 1) {
      unawaited(refresh());
      _timer = Timer.periodic(interval, (_) {
        if (busy.isEmpty) unawaited(refresh());
      });
    }
  }

  void detach() {
    _listeners--;
    if (_listeners <= 0) {
      _timer?.cancel();
      _timer = null;
    }
  }

  Future<void> refresh() async {
    try {
      final s = await api.status();
      status = s;
      loadError = null;
      unavailable = false;
      updatedAt = clock.now();
    } catch (e) {
      loadError = e;
      unavailable = FansApi.isUnavailable(e);
    }
    _notify();
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void dismissError() {
    actionError = null;
    _notify();
  }

  Future<bool> _run(String tag, Future<FansStatus> Function() fn) async {
    busy = tag;
    actionError = null;
    _notify();
    try {
      status = await fn();
      updatedAt = clock.now();
      return true;
    } catch (e) {
      actionError = e is ApiException ? e.message : e.toString();
      return false;
    } finally {
      busy = '';
      _notify();
    }
  }

  Future<bool> applyPreset(String preset) => _run('preset', () => api.applyPreset(preset));
  Future<bool> allAuto() => _run('auto', api.allAuto);
  Future<bool> update(String id, Map<String, Object?> changes) => _run('fan:$id', () => api.updateFan(id, changes));
  Future<bool> setCritical({int? cpu, int? gpu}) => _run('critical', () => api.setCritical(cpu: cpu, gpu: gpu));

  /// Null on failure (the reason is in [actionError]).
  Future<String?> identify(String id) async {
    busy = 'identify:$id';
    actionError = null;
    _notify();
    try {
      return await api.identify(id);
    } catch (e) {
      actionError = e is ApiException ? e.message : e.toString();
      return null;
    } finally {
      busy = '';
      _notify();
      unawaited(refresh());
    }
  }

  @override
  void dispose() {
    _disposed = true;
    _timer?.cancel();
    super.dispose();
  }
}

IconData _fanIcon(FanInfo f) => f.isGpu ? Icons.developer_board_outlined : Icons.air_outlined;

Status _stateStatus(FanState s) => switch (s) {
      FanState.emergency => Status.error,
      FanState.manual => Status.info,
      FanState.identifying => Status.info,
      FanState.readonly => Status.neutral,
      FanState.auto => Status.success,
    };

String _deg(double? c) => c == null ? '—' : c.toStringAsFixed(0);

class FansScreen extends StatefulWidget {
  const FansScreen({super.key, this.controller});

  /// Tests pass one with a fake API; otherwise the screen makes its own.
  final FansController? controller;

  @override
  State<FansScreen> createState() => _FansScreenState();
}

class _FansScreenState extends State<FansScreen> {
  late final FansController _c = widget.controller ?? FansController();

  @override
  void initState() {
    super.initState();
    _c.attach();
  }

  @override
  void dispose() {
    _c.detach();
    if (widget.controller == null) _c.dispose();
    super.dispose();
  }

  void _openFan(FanInfo f) {
    Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => FanDetailScreen(controller: _c, fanId: f.id)));
  }

  Future<void> _editCritical(String label, int current, int min, int max, bool gpu) async {
    final v = await showModalBottomSheet<int>(
      context: context,
      showDragHandle: true,
      builder: (context) => _NumberSheet(
        title: '$label emergency temperature',
        message: 'At this temperature every fan NivaroOS drives runs at 100% until it is 5 °C lower.',
        value: current,
        min: min,
        max: max,
        unit: '°C',
      ),
    );
    if (v == null || v == current) return;
    await (gpu ? _c.setCritical(gpu: v) : _c.setCritical(cpu: v));
  }

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: _c,
      builder: (context, _) {
        final s = _c.status;
        final anyDriven = s?.fans.any((f) => f.mode != FanMode.auto) ?? false;
        return AppScaffold.slivers(
          title: 'Fans',
          onRefresh: _c.refresh,
          banner: _c.stale ? OfflineBanner(lastUpdated: _c.updatedAt, onRetry: _c.refresh) : null,
          actions: [
            if (s != null && s.controllable && anyDriven)
              IconButton(
                tooltip: 'All fans to Auto',
                icon: const Icon(Icons.restart_alt),
                onPressed: _c.busy.isEmpty ? _c.allAuto : null,
              ),
          ],
          slivers: _body(context, s),
        );
      },
    );
  }

  List<Widget> _body(BuildContext context, FansStatus? s) {
    if (s == null) {
      if (_c.unavailable) {
        return const [
          EmptyState(
            sliver: true,
            icon: Icons.air_outlined,
            title: "Fan control isn't available",
            message: "This server doesn't run NivaroOS fan control. Updating NivaroOS adds it; until then the BIOS controls the fans.",
          ),
        ];
      }
      final e = _c.loadError;
      if (e != null) {
        final offline = e is ApiException && e.statusCode == null;
        return [
          offline
              ? ErrorState.offline(onRetry: _c.refresh, sliver: true)
              : ErrorState(title: "Couldn't load the fans", message: e.toString(), onRetry: _c.refresh, details: 'GET /v1/fans/status: $e', sliver: true),
        ];
      }
      return const [SliverLoadingList(rows: 6, trailing: true)];
    }
    final theme = Theme.of(context);
    return [
      SliverList.list(children: [
        if (s.emergency)
          const Notice(
            status: Status.error,
            icon: Icons.local_fire_department_outlined,
            title: 'Emergency cooling',
            message: 'A temperature reached its limit, so every fan NivaroOS drives runs at 100% until it is 5 °C lower.',
          ),
        if (_c.actionError != null) Notice(message: _c.actionError!, actionLabel: 'Dismiss', onAction: _c.dismissError),
        for (final n in s.notes)
          Notice(
            status: n.level == 'warn' ? Status.warning : Status.info,
            message: n.message,
            details: n.guidance.isEmpty ? null : [Text(n.guidance, style: Notice.detailStyle(context, n.level == 'warn' ? Status.warning : Status.info))],
          ),
        TileGroup(
          title: 'Temperatures',
          children: [
            for (final t in s.temps)
              MetricRow(
                icon: t.isGpu ? Icons.developer_board_outlined : Icons.memory_outlined,
                label: t.label,
                value: _deg(t.c),
                unit: t.c == null ? null : '°C',
                supporting: t.c == null ? 'No reading' : (t.hot ? 'Over its emergency limit' : 'Emergency at ${t.criticalC} °C'),
              ),
          ],
        ),
        if (s.controllable)
          TileGroup(
            title: 'Profile',
            footer: fanPresets.any((p) => p.$1 == s.preset) ? null : 'Custom: fans are set one by one.',
            children: [
              RadioGroup<String>(
                groupValue: s.preset,
                onChanged: (v) {
                  if (v != null && _c.busy.isEmpty && v != s.preset) _c.applyPreset(v);
                },
                child: Column(
                  children: [
                    for (final (id, label, desc) in fanPresets)
                      RadioListTile<String>(value: id, title: Text(label), subtitle: Text(desc), enabled: _c.busy.isEmpty),
                  ],
                ),
              ),
            ],
          ),
        TileGroup(
          title: 'Fans',
          footer: s.fans.isEmpty ? null : 'Every fan goes back to BIOS control whenever the fan service stops.',
          children: [
            if (s.fans.isEmpty)
              const ListTile(
                leading: Icon(Icons.mode_fan_off_outlined),
                title: Text('No fans found'),
                subtitle: Text('This machine exposes no fan NivaroOS can read. The BIOS keeps controlling them.'),
              )
            else
              for (final f in s.fans)
                ListTile(
                  leading: Icon(_fanIcon(f)),
                  title: Text(f.label),
                  subtitle: Text(f.reading, style: theme.textTheme.bodyMedium?.tabular.copyWith(color: theme.colorScheme.onSurfaceVariant)),
                  trailing: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      StatusChip(label: f.stateLabel, status: _stateStatus(f.state)),
                      const SizedBox(width: Space.xs),
                      ExcludeSemantics(child: Icon(Icons.chevron_right, color: theme.colorScheme.onSurfaceVariant)),
                    ],
                  ),
                  onTap: () => _openFan(f),
                ),
          ],
        ),
        if (s.controllable)
          TileGroup(
            title: 'Emergency temperatures',
            footer: 'Every fan NivaroOS drives goes to 100% at these temperatures.',
            children: [
              MetricRow(
                icon: Icons.memory_outlined,
                label: 'CPU',
                value: '${s.criticalCpuC}',
                unit: '°C',
                onTap: () => _editCritical('CPU', s.criticalCpuC, s.limits.minCriticalC, s.limits.maxCriticalCpuC, false),
              ),
              if (s.temps.any((t) => t.isGpu))
                MetricRow(
                  icon: Icons.developer_board_outlined,
                  label: 'GPU',
                  value: '${s.criticalGpuC}',
                  unit: '°C',
                  onTap: () => _editCritical('GPU', s.criticalGpuC, s.limits.minCriticalC, s.limits.maxCriticalGpuC, true),
                ),
            ],
          ),
        const SizedBox(height: Space.xl),
      ]),
    ];
  }

}

/// One fan: its reading, mode, speed or curve, and name.
class FanDetailScreen extends StatefulWidget {
  const FanDetailScreen({super.key, required this.controller, required this.fanId});

  final FansController controller;
  final String fanId;

  @override
  State<FanDetailScreen> createState() => _FanDetailScreenState();
}

class _FanDetailScreenState extends State<FanDetailScreen> {
  FansController get _c => widget.controller;

  /// The slider while it is being dragged.
  double? _fixed;

  @override
  void initState() {
    super.initState();
    _c.attach();
  }

  @override
  void dispose() {
    _c.detach();
    super.dispose();
  }

  Future<void> _rename(FanInfo f) async {
    final name = await showDialog<String>(context: context, builder: (_) => _RenameDialog(current: f.label == f.defaultLabel ? '' : f.label, hint: f.defaultLabel));
    if (name != null) await _c.update(f.id, {'label': name});
  }

  Future<void> _pickSource(FansStatus s, FanInfo f) async {
    final v = await showModalBottomSheet<String>(
      context: context,
      showDragHandle: true,
      builder: (context) => SafeArea(
        child: RadioGroup<String>(
          groupValue: f.source,
          onChanged: (v) => Navigator.pop(context, v),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              for (final src in s.sources) RadioListTile<String>(value: src.id, title: Text(src.label)),
            ],
          ),
        ),
      ),
    );
    if (v != null && v != f.source) await _c.update(f.id, {'source': v});
  }

  Future<void> _editMin(FanInfo f) async {
    final v = await showModalBottomSheet<int>(
      context: context,
      showDragHandle: true,
      builder: (context) => _NumberSheet(
        title: 'Minimum speed',
        message: 'NivaroOS never runs this fan slower, whatever its mode. It can go no lower than ${f.floorPct}%.',
        value: f.minPct,
        min: f.floorPct,
        max: 100,
        unit: '%',
      ),
    );
    if (v != null && v != f.minPct) await _c.update(f.id, {'min_pct': v});
  }

  Future<void> _identify(FanInfo f) async {
    final msg = await _c.identify(f.id);
    if (!mounted || msg == null) return;
    ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(msg)));
  }

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: _c,
      builder: (context, _) {
        final s = _c.status;
        final f = s?.fan(widget.fanId);
        return AppScaffold.slivers(
          title: f?.label ?? 'Fan',
          banner: _c.stale ? OfflineBanner(lastUpdated: _c.updatedAt, onRetry: _c.refresh) : null,
          slivers: s == null || f == null
              ? const [EmptyState(sliver: true, icon: Icons.mode_fan_off_outlined, title: 'This fan is gone', message: 'The server no longer reports it.')]
              : [SliverList.list(children: _rows(context, s, f))],
        );
      },
    );
  }

  List<Widget> _rows(BuildContext context, FansStatus s, FanInfo f) {
    final busy = _c.busy.isNotEmpty;
    final followsTemp = s.sourceTemp(f.source);
    return [
      if (f.state == FanState.emergency)
        const Notice(status: Status.error, icon: Icons.local_fire_department_outlined, message: 'Emergency cooling: this fan runs at 100% until the temperature is 5 °C below its limit.'),
      if (_c.actionError != null) Notice(message: _c.actionError!, actionLabel: 'Dismiss', onAction: _c.dismissError),
      if (f.alert.isNotEmpty) Notice(message: f.alert),
      if (!f.controllable && f.readonlyReason.isNotEmpty) Notice(status: Status.info, icon: Icons.lock_outline, title: 'Read-only', message: f.readonlyReason),
      TileGroup(
        children: [
          MetricRow(
            icon: _fanIcon(f),
            label: 'Speed',
            value: f.rpm == null ? '—' : groupThousands(f.rpm!),
            unit: f.rpm == null ? null : 'RPM',
            supporting: f.rpm == null ? 'This fan reports no RPM' : (f.rpm == 0 ? (f.hasTach ? 'Stopped' : 'No speed signal on this header') : null),
          ),
          MetricRow(icon: Icons.speed_outlined, label: 'Power', value: f.pct == null ? '—' : '${f.pct}', unit: f.pct == null ? null : '%'),
          MetricRow(
            icon: Icons.thermostat_outlined,
            label: 'Follows ${s.sourceLabel(f.source)}',
            value: _deg(followsTemp),
            unit: followsTemp == null ? null : '°C',
            onTap: f.controllable && f.mode == FanMode.curve && s.sources.length > 1 ? () => _pickSource(s, f) : null,
          ),
        ],
      ),
      if (f.controllable)
        TileGroup(
          title: 'Mode',
          footer: switch (f.mode) {
            FanMode.auto => 'The BIOS or driver controls this fan.',
            FanMode.fixed => 'One speed, whatever the temperature (100% in an emergency).',
            FanMode.curve => 'Speed follows the temperature along the curve.',
          },
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: Space.lg, vertical: Space.md),
              child: SizedBox(
                width: double.infinity,
                child: SegmentedButton<FanMode>(
                  showSelectedIcon: false,
                  segments: [for (final m in FanMode.values) ButtonSegment(value: m, label: Text(m.label))],
                  selected: {f.mode},
                  onSelectionChanged: busy ? null : (v) => _c.update(f.id, {'mode': v.first.name}),
                ),
              ),
            ),
          ],
        ),
      if (f.controllable && f.mode == FanMode.fixed)
        TileGroup(
          title: 'Fixed speed',
          footer: 'From ${f.minPct}% (this fan\'s minimum) to 100%.',
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(Space.lg, Space.sm, Space.lg, Space.sm),
              child: Row(
                children: [
                  Expanded(
                    child: Slider(
                      value: (_fixed ?? f.fixedPct.toDouble()).clamp(f.minPct.toDouble(), 100),
                      min: f.minPct.toDouble(),
                      max: 100,
                      divisions: 100 - f.minPct,
                      label: '${(_fixed ?? f.fixedPct).round()}%',
                      onChanged: busy ? null : (v) => setState(() => _fixed = v),
                      onChangeEnd: (v) async {
                        await _c.update(f.id, {'fixed_pct': v.round()});
                        if (mounted) setState(() => _fixed = null);
                      },
                    ),
                  ),
                  SizedBox(
                    width: 48,
                    child: Text('${(_fixed ?? f.fixedPct).round()}%', textAlign: TextAlign.end, style: Theme.of(context).textTheme.titleMedium?.tabular),
                  ),
                ],
              ),
            ),
          ],
        ),
      if (f.curve.isNotEmpty && (f.mode == FanMode.curve || !f.controllable))
        TileGroup(
          title: 'Curve',
          footer: f.controllable ? 'Drag a point to change it. Speed never drops as it gets hotter, and never below ${f.minPct}%.' : null,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(Space.sm, Space.md, Space.lg, Space.md),
              child: FanCurveView(
                points: f.curve,
                minPct: f.minPct,
                limits: s.limits,
                currentTemp: followsTemp,
                criticalC: s.criticalFor(f.source),
                editable: f.controllable && !busy,
                onChanged: (pts) => _c.update(f.id, {'curve': [for (final p in pts) p.toJson()]}),
              ),
            ),
          ],
        ),
      TileGroup(
        title: 'About this fan',
        children: [
          ListTile(
            leading: const Icon(Icons.edit_outlined),
            title: const Text('Name'),
            subtitle: Text(f.label),
            onTap: busy ? null : () => _rename(f),
          ),
          ListTile(
            leading: const Icon(Icons.developer_board_outlined),
            title: const Text('Channel'),
            subtitle: Text([f.chip, f.channel, if (f.tach.isNotEmpty) 'speed from ${f.tach}'].where((x) => x.isNotEmpty).join(' · ')),
          ),
          if (f.controllable)
            MetricRow(icon: Icons.south_outlined, label: 'Minimum speed', value: '${f.minPct}', unit: '%', onTap: busy ? null : () => _editMin(f)),
          if (f.canIdentify)
            ListTile(
              leading: const Icon(Icons.search_outlined),
              title: const Text('Identify'),
              subtitle: const Text('Changes its speed for about 7 seconds to show which fan it is'),
              trailing: _c.busy == 'identify:${f.id}' ? const SizedBox.square(dimension: 20, child: CircularProgressIndicator(strokeWidth: 2)) : null,
              onTap: busy ? null : () => _identify(f),
            ),
        ],
      ),
      const SizedBox(height: Space.xl),
    ];
  }
}

class _RenameDialog extends StatefulWidget {
  const _RenameDialog({required this.current, required this.hint});
  final String current;
  final String hint;

  @override
  State<_RenameDialog> createState() => _RenameDialogState();
}

class _RenameDialogState extends State<_RenameDialog> {
  late final TextEditingController _t = TextEditingController(text: widget.current);

  @override
  void dispose() {
    _t.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
        title: const Text('Rename fan'),
        content: TextField(
          controller: _t,
          autofocus: true,
          maxLength: 40,
          decoration: InputDecoration(hintText: widget.hint, helperText: 'Empty for the default name'),
          onSubmitted: (v) => Navigator.pop(context, v.trim()),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context), child: const Text('Cancel')),
          FilledButton(onPressed: () => Navigator.pop(context, _t.text.trim()), child: const Text('Save')),
        ],
      );
}

/// A bottom sheet with one bounded number on a slider.
class _NumberSheet extends StatefulWidget {
  const _NumberSheet({required this.title, required this.message, required this.value, required this.min, required this.max, required this.unit});
  final String title;
  final String message;
  final int value;
  final int min;
  final int max;
  final String unit;

  @override
  State<_NumberSheet> createState() => _NumberSheetState();
}

class _NumberSheetState extends State<_NumberSheet> {
  late double _v = widget.value.clamp(widget.min, widget.max).toDouble();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final unit = widget.unit == '%' ? '%' : ' ${widget.unit}';
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(Space.xl, 0, Space.xl, Space.lg),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(widget.title, style: theme.textTheme.titleLarge),
            const SizedBox(height: Space.sm),
            Text(widget.message, style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
            const SizedBox(height: Space.lg),
            Row(
              children: [
                Expanded(
                  child: Slider(
                    value: _v,
                    min: widget.min.toDouble(),
                    max: widget.max.toDouble(),
                    divisions: widget.max - widget.min,
                    label: '${_v.round()}$unit',
                    onChanged: (v) => setState(() => _v = v),
                  ),
                ),
                SizedBox(width: 64, child: Text('${_v.round()}$unit', textAlign: TextAlign.end, style: theme.textTheme.titleMedium?.tabular)),
              ],
            ),
            const SizedBox(height: Space.md),
            Align(
              alignment: AlignmentDirectional.centerEnd,
              child: FilledButton(onPressed: () => Navigator.pop(context, _v.round()), child: const Text('Save')),
            ),
          ],
        ),
      ),
    );
  }
}
