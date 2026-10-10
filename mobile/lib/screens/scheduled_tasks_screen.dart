import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';

import '../backup/backup_state.dart' show cronText;
import '../backup/backup_strings.dart' show formatWhen;
import '../services/api_client.dart';
import '../ui/ui.dart';

/// One of the server's scheduled tasks (`GET /v1/schedules`, core's
/// ScheduleTask). Created and edited on the web; the phone lists them,
/// runs one now and turns one on or off, as the web's list does.
class ScheduledTask {
  const ScheduledTask({
    required this.id,
    required this.name,
    this.enabled = false,
    this.cron = '',
    this.type = '',
    this.action = '',
    this.target = '',
    this.lastRun,
    this.lastStatus = '',
    this.lastOutput = '',
    this.nextRun,
    this.migratedTo = '',
  });

  factory ScheduledTask.fromJson(Map<String, dynamic> j) {
    String s(String k) => j[k]?.toString() ?? '';
    final source = s('source_path'), dest = s('dest_path');
    return ScheduledTask(
      id: s('id'),
      name: s('name'),
      enabled: j['enabled'] == true,
      cron: s('cron'),
      type: s('type'),
      action: s('action').isNotEmpty ? s('action') : s('sync_mode'),
      target: source.isNotEmpty && dest.isNotEmpty ? '$source → $dest' : [s('target_name'), s('target_id')].firstWhere((v) => v.isNotEmpty, orElse: () => ''),
      lastRun: DateTime.tryParse(s('last_run')),
      lastStatus: s('last_status'),
      lastOutput: s('last_output'),
      nextRun: DateTime.tryParse(s('next_run')),
      migratedTo: s('migrated_to'),
    );
  }

  final String id, name, cron, type, action, target, lastStatus, lastOutput, migratedTo;
  final bool enabled;
  final DateTime? lastRun, nextRun;

  /// Backup & Sync took it over: core no longer runs it, on its timer or
  /// by Run now (409), and the switch does nothing.
  bool get moved => migratedTo.isNotEmpty;
  bool get running => lastStatus == 'running';

  ScheduledTask withEnabled(bool on) => ScheduledTask(
        id: id, name: name, enabled: on, cron: cron, type: type, action: action, target: target,
        lastRun: lastRun, lastStatus: lastStatus, lastOutput: lastOutput, nextRun: on ? nextRun : null, migratedTo: migratedTo,
      );

  IconData get icon => switch (type) {
        'vm' => Icons.computer_outlined,
        'container' => Icons.widgets_outlined,
        'maintenance' => Icons.build_outlined,
        'command' => Icons.code,
        'backup' || 'sync' => Icons.backup_outlined,
        _ => Icons.schedule_outlined,
      };

  /// How the last run went, as the web's dot says it.
  (String, Status) get result => switch (lastStatus) {
        'success' || '' => ('Succeeded', Status.success),
        'running' => ('Running', Status.info),
        'interrupted' => ('Interrupted', Status.warning),
        _ => ('Failed', Status.error),
      };

  /// "Every day at 03:00 · next Today at 3:00 AM", "Off", or why not.
  String get when {
    if (moved) return 'Moved to Backup & Sync';
    final schedule = cron.isEmpty ? 'No schedule' : cronText(cron);
    if (!enabled) return '$schedule · Off';
    final n = nextRun;
    return n == null ? schedule : '$schedule · next ${_lowerFirst(formatWhen(n))}';
  }

  // "Today at…" reads lower case mid-sentence; a weekday stays as it is.
  static String _lowerFirst(String s) => s.startsWith('To') ? s[0].toLowerCase() + s.substring(1) : s;
}

/// The server's scheduled tasks (More › Scheduled tasks): when each runs
/// next and how its last run went, with Run now and on/off. Creating and
/// editing stay on the web (Settings › Scheduled tasks).
class ScheduledTasksScreen extends StatefulWidget {
  const ScheduledTasksScreen({super.key});

  @override
  State<ScheduledTasksScreen> createState() => _ScheduledTasksScreenState();
}

class _ScheduledTasksScreenState extends State<ScheduledTasksScreen> {
  List<ScheduledTask>? _tasks;
  ApiException? _error;
  DateTime? _loadedAt;
  final Set<String> _busy = {};
  Timer? _follow;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _follow?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final res = await ApiClient.instance.get('/schedules');
      final list = [for (final t in (res['data'] as List? ?? const []).whereType<Map>()) ScheduledTask.fromJson(Map<String, dynamic>.from(t))];
      if (!mounted) return;
      setState(() {
        _tasks = list;
        _error = null;
        _loadedAt = clock.now();
      });
      // A run is asynchronous: look again until it finishes.
      _follow?.cancel();
      if (list.any((t) => t.running)) _follow = Timer(const Duration(seconds: 3), _load);
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _toggle(ScheduledTask t, bool on) async {
    final messenger = ScaffoldMessenger.of(context);
    _replace(t.withEnabled(on));
    try {
      await ApiClient.instance.post('/schedules/${Uri.encodeComponent(t.id)}/toggle', body: {'enabled': on});
    } on ApiException catch (e) {
      _replace(t);
      messenger.showSnackBar(SnackBar(content: Text("Couldn't turn “${t.name}” ${on ? 'on' : 'off'}. ${e.message}")));
    }
    unawaited(_load());
  }

  Future<void> _runNow(ScheduledTask t) async {
    final messenger = ScaffoldMessenger.of(context);
    final ok = await ConfirmDialog.confirm(
      context,
      title: 'Run “${t.name}” now?',
      message: 'It runs once now, as it would on its schedule${t.target.isEmpty ? '' : ' (${t.target})'}.',
      confirmLabel: 'Run now',
    );
    if (!ok || !mounted) return;
    setState(() => _busy.add(t.id));
    try {
      await ApiClient.instance.post('/schedules/${Uri.encodeComponent(t.id)}/run');
      messenger.showSnackBar(SnackBar(content: Text('“${t.name}” started')));
    } on ApiException catch (e) {
      messenger.showSnackBar(SnackBar(content: Text("Couldn't run “${t.name}”. ${e.message}")));
    }
    if (mounted) setState(() => _busy.remove(t.id));
    await _load();
  }

  void _replace(ScheduledTask t) {
    if (!mounted || _tasks == null) return;
    setState(() => _tasks = [for (final o in _tasks!) o.id == t.id ? t : o]);
  }

  Future<void> _details(ScheduledTask t) => showModalBottomSheet<void>(
        context: context,
        showDragHandle: true,
        useSafeArea: true,
        isScrollControlled: true,
        builder: (sheet) {
          final theme = Theme.of(sheet);
          final scheme = theme.colorScheme;
          final (label, _) = t.result;
          return SingleChildScrollView(
            padding: const EdgeInsets.only(bottom: Space.lg),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                ListTile(
                  leading: Icon(t.icon),
                  title: Text(t.name, style: theme.textTheme.titleMedium),
                  subtitle: Text(t.when),
                ),
                if (t.target.isNotEmpty) ListTile(leading: const Icon(Icons.adjust_outlined), title: const Text('Target'), subtitle: Text(t.target)),
                ListTile(
                  leading: const Icon(Icons.history_outlined),
                  title: const Text('Last run'),
                  subtitle: Text(t.lastRun == null ? 'Not run yet' : '$label · ${formatExact(t.lastRun!)}'),
                ),
                if (t.lastOutput.trim().isNotEmpty)
                  Container(
                    margin: EdgeInsets.symmetric(horizontal: Space.gutter(sheet), vertical: Space.sm),
                    padding: const EdgeInsets.all(Space.md),
                    constraints: const BoxConstraints(maxHeight: 240),
                    decoration: BoxDecoration(color: scheme.surfaceContainerHighest, borderRadius: BorderRadius.circular(DesignTokens.of(sheet).radii.md)),
                    child: SingleChildScrollView(
                      child: SelectableText(t.lastOutput.trim(), style: DesignTokens.of(sheet).mono(theme.textTheme.bodySmall)),
                    ),
                  ),
                Padding(
                  padding: EdgeInsets.fromLTRB(Space.gutter(sheet), Space.md, Space.gutter(sheet), 0),
                  child: FilledButton.icon(
                    style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
                    onPressed: t.moved || t.running || _busy.contains(t.id)
                        ? null
                        : () {
                            Navigator.of(sheet).pop();
                            _runNow(t);
                          },
                    icon: const Icon(Icons.play_arrow_rounded),
                    label: Text(t.moved ? 'Runs in Backup & Sync' : t.running ? 'Running…' : 'Run now'),
                  ),
                ),
              ],
            ),
          );
        },
      );

  Widget _row(ScheduledTask t) {
    final theme = Theme.of(context);
    final small = theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant);
    final (label, status) = t.result;
    return ListTile(
      leading: Icon(t.icon),
      title: Text(t.name, maxLines: 1, overflow: TextOverflow.ellipsis),
      subtitle: Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisSize: MainAxisSize.min, children: [
        Text(t.when),
        const SizedBox(height: Space.xs),
        if (t.lastRun == null)
          Text('Not run yet', style: small)
        else
          Wrap(spacing: Space.sm, runSpacing: Space.xs, crossAxisAlignment: WrapCrossAlignment.center, children: [
            StatusChip(label: label, status: status),
            RelativeTime(t.lastRun!, style: small),
          ]),
      ]),
      isThreeLine: true,
      onTap: () => _details(t),
      trailing: Switch(
        value: t.enabled && !t.moved,
        onChanged: t.moved || _busy.contains(t.id) ? null : (on) => _toggle(t, on),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final tasks = _tasks;
    final error = _error;
    final List<Widget> slivers;
    if (tasks == null && error == null) {
      slivers = const [SliverLoadingList(rows: 4)];
    } else if (tasks == null) {
      slivers = [
        error!.isUnreachable
            ? ErrorState.offline(sliver: true, onRetry: _load, details: error.details)
            : ErrorState(sliver: true, title: "Couldn't load the scheduled tasks", message: error.message, onRetry: _load, details: error.details),
      ];
    } else if (tasks.isEmpty) {
      slivers = const [
        EmptyState(
          sliver: true,
          icon: Icons.schedule_outlined,
          title: 'No scheduled tasks',
          message: 'Create one on the web, in Settings › Scheduled tasks: VM power cycles, container restarts, maintenance and scripts.',
        ),
      ];
    } else {
      slivers = [
        SliverList.list(children: [
          TileGroup(
            footer: 'Create and edit tasks on the web, in Settings › Scheduled tasks.',
            children: [for (final t in tasks) _row(t)],
          ),
          const SizedBox(height: Space.lg),
        ]),
      ];
    }
    return AppScaffold.slivers(
      title: 'Scheduled tasks',
      onRefresh: _load,
      banner: tasks != null && error != null && error.isUnreachable ? OfflineBanner(lastUpdated: _loadedAt, onRetry: _load) : null,
      slivers: slivers,
    );
  }
}
