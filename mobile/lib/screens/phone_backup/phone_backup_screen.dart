import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../backup/backup_strings.dart' show formatWhen, formatNumber;
import '../../phone_backup/pb_client.dart';
import '../../phone_backup/pb_models.dart';
import '../../phone_backup/pb_schedule.dart';
import '../../phone_backup/pb_service.dart';
import '../../phone_backup/pb_store.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import '../backup/backup_widgets.dart';
import '../dashboard_screen.dart' show StatusDisc;
import 'phone_backup_common.dart';
import 'phone_backup_history_screen.dart';
import 'phone_backup_location.dart';
import 'phone_backup_setup_screen.dart';

/// "Back up this phone", the status page (plan WP2-4): how the last backup
/// went, per category; what's running now; the next run; where the
/// backups go (Change location…); Back up now; history and Restore.
/// Not set up yet: what it does and a way to start.
class PhoneBackupScreen extends StatefulWidget {
  const PhoneBackupScreen({super.key, this.service, this.permissions});

  final PhoneBackupService? service;
  final PhonePermissions? permissions;

  @override
  State<PhoneBackupScreen> createState() => _PhoneBackupScreenState();
}

class _PhoneBackupScreenState extends State<PhoneBackupScreen> {
  late final PhoneBackupService _service = widget.service ?? PhoneBackupService.instance;

  PhoneBackupStatus? _status;
  PhoneConfig? _config;
  PhoneDeviceDetail? _detail;
  Object? _remoteError;
  Timer? _timer;
  bool _starting = false;

  @override
  void initState() {
    super.initState();
    _load();
    // The job writes its progress to disk; read it while on screen.
    _timer = Timer.periodic(const Duration(seconds: 2), (_) => _refreshLocal());
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<void> _refreshLocal() async {
    final st = await _service.status();
    if (!mounted) return;
    final wasRunning = _status?.state.isRunning ?? false;
    setState(() => _status = st);
    if (wasRunning && !st.state.isRunning) unawaited(_loadRemote());
  }

  Future<void> _load() async {
    final st = await _service.status();
    if (!mounted) return;
    setState(() => _status = st);
    await _loadRemote();
  }

  Future<void> _loadRemote() async {
    final st = _status;
    if (st == null || !st.enrolled) return;
    final client = await _service.deviceClient();
    try {
      final cfg = await client!.config();
      if (mounted) setState(() => _config = cfg);
      _remoteError = null;
    } catch (e) {
      if (mounted) setState(() => _remoteError = e);
    }
    try {
      final d = await _service.owner.detail(st.credential!.deviceId);
      if (mounted) setState(() => _detail = d);
    } catch (_) {
      // Not an administrator, or signed out: the device's own view is enough.
    }
  }

  Future<void> _open(Widget screen) async {
    await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => screen));
    if (mounted) unawaited(_load());
  }

  Future<void> _setup() => _open(PhoneBackupSetupScreen(service: widget.service, permissions: widget.permissions));

  Future<void> _backUpNow() async {
    final s = _status?.settings;
    if (s == null) return;
    setState(() => _starting = true);
    try {
      final ok = await _service.backUpNow(anyNetwork: !s.conditions.wifiOnly);
      if (!mounted) return;
      showBackupSnack(context, ok ? (s.conditions.wifiOnly ? 'Backing up. It starts as soon as the phone is on Wi-Fi.' : 'Backing up now.') : 'The backup couldn’t be started.');
      await _refreshLocal();
    } on PlatformException catch (e) {
      if (mounted) showBackupSnack(context, 'The backup couldn’t be started: ${e.message}');
    } finally {
      if (mounted) setState(() => _starting = false);
    }
  }

  Future<void> _stop() async {
    await _service.stop();
    if (mounted) showBackupSnack(context, 'Stopping at the next file. What was sent is kept.');
  }

  Future<void> _changeLocation() async {
    final c = _status?.credential;
    final d = _detail;
    if (c == null || d == null) return;
    final next = await changePhoneBackupLocation(context, deviceId: c.deviceId, detail: d, owner: _service.owner);
    if (next != null && mounted) {
      setState(() => _detail = next);
      unawaited(_loadRemote());
    }
  }

  Future<void> _unlink() async {
    final ok = await ConfirmDialog.confirm(
      context,
      title: 'Stop backing up this phone?',
      message: 'The phone stops sending backups and forgets its link to the server. The backups made so far stay on the server, where you can still browse them.',
      confirmLabel: 'Stop backing up',
    );
    if (!ok) return;
    await _service.unlink(revokeOnServer: _detail != null);
    if (mounted) unawaited(_load());
  }

  Future<void> _relink() async {
    await _service.unlink();
    if (mounted) await _setup();
  }

  @override
  Widget build(BuildContext context) {
    final st = _status;
    final List<Widget> slivers;
    if (st == null) {
      slivers = const [SliverLoadingList(rows: 6)];
    } else if (!st.setUp) {
      slivers = [
        EmptyState(
          icon: Icons.phonelink_setup_outlined,
          title: 'Back up this phone',
          message: 'Photos and videos, chosen folders, contacts, calendars, messages and more go to your NivaroOS server, on a schedule, in the background. Only what’s new or changed is sent.',
          actionLabel: 'Set up',
          onAction: _setup,
          sliver: true,
        ),
      ];
    } else {
      slivers = [SliverList.list(children: _content(st))];
    }
    return AppScaffold.slivers(
      title: 'This phone',
      onRefresh: _load,
      actions: [
        if (st?.setUp ?? false)
          PopupMenuButton<String>(
            onSelected: (v) => switch (v) {
              'settings' => _setup(),
              'unlink' => _unlink(),
              _ => null,
            },
            itemBuilder: (context) => const [
              PopupMenuItem(value: 'settings', child: Text('Backup settings')),
              PopupMenuItem(value: 'unlink', child: Text('Stop backing up this phone')),
            ],
          ),
      ],
      slivers: slivers,
    );
  }

  List<Widget> _content(PhoneBackupStatus st) {
    final s = st.settings!;
    final state = st.state;
    final dest = _detail?.destination ?? _config?.destination;
    final retention = _detail?.retention ?? _config?.retention;
    final revoked = state.outcome == RunOutcomes.revoked || (_remoteError is PhoneBackupError && (_remoteError as PhoneBackupError).revoked);
    return [
      _Header(state: state, settings: s, revoked: revoked, driveMissing: dest?.driveMissing ?? false),
      Padding(
        padding: EdgeInsets.fromLTRB(Space.gutter(context), 0, Space.gutter(context), Space.md),
        child: Wrap(spacing: Space.sm, runSpacing: Space.sm, children: [
          if (revoked)
            FilledButton.icon(onPressed: _relink, icon: const Icon(Icons.link), label: const Text('Link this phone again'))
          else if (state.isRunning)
            FilledButton.tonalIcon(style: tonalButtonStyle(context), onPressed: _stop, icon: const Icon(Icons.stop_circle_outlined), label: const Text('Stop'))
          else
            FilledButton.icon(onPressed: _starting ? null : _backUpNow, icon: const Icon(Icons.backup_outlined), label: const Text('Back up now')),
        ]),
      ),
      if (dest != null) PhoneDestinationGroup(destination: dest, move: _detail?.move, onChange: _detail == null ? null : _changeLocation),
      TileGroup(title: 'What’s backed up', children: [
        for (final c in s.runCategories) _CategoryRow(category: c, local: state.categories[c.wire], server: _detail?.category(c.wire)),
      ]),
      if (state.errors.isNotEmpty && !state.isRunning)
        TileGroup(
          title: state.errors.length == 1 ? '1 problem in the last backup' : '${state.errors.length} problems in the last backup',
          footer: state.errors.length > 5 ? 'And ${state.errors.length - 5} more.' : null,
          children: [
            for (final e in state.errors.take(5))
              ListTile(
                leading: Icon(Icons.warning_amber_outlined, color: StatusColors.toneOf(context, Status.warning).color),
                title: Text(e['path']?.isNotEmpty == true ? e['path']! : categoryLabelOf(e['category'] ?? '')),
                subtitle: Text(e['message'] ?? ''),
              ),
          ],
        ),
      TileGroup(children: [
        ListTile(
          leading: const Icon(Icons.history),
          title: const Text('Backups and restore'),
          subtitle: Text(_detail != null ? '${_detail!.snapshots} backups on the server' : 'Browse a backup, get files, contacts and messages back'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => _open(PhoneBackupHistoryScreen(service: widget.service)),
        ),
        ListTile(
          leading: const Icon(Icons.tune),
          title: const Text('Backup settings'),
          subtitle: Text(scheduleSummary(s)),
          trailing: const Icon(Icons.chevron_right),
          onTap: _setup,
        ),
      ]),
      if (retention != null)
        TileGroup(title: 'What’s kept', children: [ListTile(leading: const Icon(Icons.inventory_2_outlined), title: Text(retention.summary))]),
      const SizedBox(height: Space.xl),
    ];
  }
}

String categoryLabelOf(String wire) => PhoneCategory.fromWire(wire)?.label ?? wire;

class _Header extends StatelessWidget {
  const _Header({required this.state, required this.settings, required this.revoked, required this.driveMissing});

  final PhoneBackupState state;
  final PhoneBackupSettings settings;
  final bool revoked;
  final bool driveMissing;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final running = state.isRunning;
    final outcome = revoked ? RunOutcomes.revoked : (driveMissing && !running ? RunOutcomes.waitingDrive : state.outcome);
    final (status, icon) = running
        ? (Status.info, Icons.sync)
        : switch (outcome) {
            RunOutcomes.success => (Status.success, Icons.check_circle_outline),
            RunOutcomes.partial || RunOutcomes.waitingDrive || RunOutcomes.waitingNetwork || RunOutcomes.noSpace || RunOutcomes.cancelled => (Status.warning, Icons.warning_amber_outlined),
            RunOutcomes.revoked || RunOutcomes.failed || RunOutcomes.destProblem => (Status.error, Icons.error_outline),
            _ => (Status.neutral, Icons.backup_outlined),
          };
    final title = running ? 'Backing up' : outcomeTitle(outcome);
    final waiting = outcome == RunOutcomes.waitingDrive || outcome == RunOutcomes.waitingNetwork || outcome == RunOutcomes.noSpace;
    final p = state.progress;
    final facts = <String>[
      if (running && p != null && p.category.isNotEmpty) [categoryLabelOf(p.category), if (p.total > 0) '${formatNumber(p.done)} of ${formatNumber(p.total)}' else p.phase].join(' · '),
      if (!running && state.lastSuccessAt != null) 'Last backup ${_lower(formatRelative(state.lastSuccessAt!))}',
      if (!running && state.lastSuccessAt == null && state.lastRunAt == null) 'No backup yet',
      if (!running && settings.paused) 'Paused'
      else if (!running && state.nextRunAt != null) '${waiting ? 'Tries again' : 'Next'} ${_lower(formatWhen(state.nextRunAt!))}'
      else if (!running && settings.schedule.kind == ScheduleKind.manual) 'Only when you tap Back up now',
    ];
    // A missing drive has its own notice below.
    final message = running || driveMissing ? '' : (revoked ? 'The server no longer accepts this phone. Link it again to keep backing up.' : state.message);
    final g = Space.gutter(context);
    return Padding(
      padding: EdgeInsets.fromLTRB(g, Space.md, g, Space.lg),
      child: Semantics(
        container: true,
        liveRegion: true,
        label: [title, ...facts, message].where((x) => x.isNotEmpty).join('. '),
        child: ExcludeSemantics(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Row(children: [
              StatusDisc(status: status, icon: icon, size: 56),
              const SizedBox(width: Space.lg),
              Expanded(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Text(title, style: theme.textTheme.headlineSmall),
                  const SizedBox(height: Space.xs),
                  FactLine.plain(facts, style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
                ]),
              ),
            ]),
            if (running && p != null) ...[
              const SizedBox(height: Space.md),
              LinearProgressIndicator(value: p.total > 0 ? (p.done / p.total).clamp(0, 1).toDouble() : null),
              if (p.totalBytes > 0)
                Padding(
                  padding: const EdgeInsets.only(top: Space.xs),
                  child: Text('${formatSize(p.bytes)} of ${formatSize(p.totalBytes)} sent', style: theme.textTheme.bodySmall),
                ),
            ],
            if (message.isNotEmpty) ...[
              const SizedBox(height: Space.md),
              Text(message, style: theme.textTheme.bodyMedium),
            ],
          ]),
        ),
      ),
    );
  }

  static String _lower(String s) => s.isEmpty ? s : s[0].toLowerCase() + s.substring(1);
}

class _CategoryRow extends StatelessWidget {
  const _CategoryRow({required this.category, this.local, this.server});

  final PhoneCategory category;
  final CategoryRun? local;
  final PhoneCategoryStatus? server;

  @override
  Widget build(BuildContext context) {
    final l = local;
    final sv = server;
    final at = sv?.lastBackupAt ?? l?.at;
    final failed = l?.status == 'failed';
    final facts = <String>[
      if (failed) l!.message.isNotEmpty ? l.message : 'Failed'
      else if (at != null) formatRelative(at)
      else 'Not backed up yet',
      if (!failed && sv != null && category.isFiles && sv.files > 0) '${formatNumber(sv.files)} files',
      if (!failed && sv != null && !category.isFiles && sv.items > 0) '${formatNumber(sv.items)} ${category == PhoneCategory.calllog ? 'calls' : category == PhoneCategory.sms ? 'messages' : 'items'}',
      if (!failed && sv != null && sv.sizeBytes > 0) formatSize(sv.sizeBytes),
      if (!failed && sv == null && l != null && l.sent > 0) '${formatNumber(l.sent)} sent last time',
      if (!failed && l != null && l.errors > 0) l.errors == 1 ? '1 problem' : '${l.errors} problems',
    ];
    final tone = failed || (l?.errors ?? 0) > 0 ? StatusColors.toneOf(context, failed ? Status.error : Status.warning).color : null;
    return ListTile(
      leading: Icon(categoryInfo[category]!.icon, color: tone),
      title: Text(category.label),
      subtitle: Text(facts.join(' · ')),
    );
  }
}
