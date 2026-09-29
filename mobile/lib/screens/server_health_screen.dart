import 'package:flutter/material.dart';

import '../models/dashboard_stats.dart';
import '../services/api_client.dart';
import 'backup/backup_decide_screen.dart';
import 'backup/backup_job_screen.dart';
import 'backup/backup_screen.dart';
import '../ui/ui.dart';
import '../widgets/monitor_modals.dart';
import '../widgets/tailscale_modal.dart';
import 'companion_devices_screen.dart';
import 'dashboard_screen.dart';
import 'system_updates_screen.dart';

/// The status a health verdict shows in: calm success when all is good,
/// amber for warnings, red for problems, blue for "worth a look".
Status healthStatus(AttentionSeverity? worst) => switch (worst) {
      null => Status.success,
      AttentionSeverity.error => Status.error,
      AttentionSeverity.warning => Status.warning,
      AttentionSeverity.info => Status.info,
    };

/// One glyph per status, so the colour is never the only signal.
IconData healthIcon(Status status) => switch (status) {
      Status.success => Icons.check_circle_outline,
      Status.error => Icons.error_outline,
      Status.warning => Icons.warning_amber_outlined,
      _ => Icons.info_outline,
    };

IconData _areaIcon(HealthArea area) => switch (area) {
      HealthArea.temperature => Icons.thermostat_outlined,
      HealthArea.memory => Icons.memory_outlined,
      HealthArea.storage => Icons.storage_outlined,
      HealthArea.driveHealth => Icons.monitor_heart_outlined,
      HealthArea.backups => Icons.backup_outlined,
      HealthArea.updates => Icons.update_outlined,
      HealthArea.apps => Icons.apps_outlined,
      HealthArea.tailscale => Icons.vpn_key_outlined,
      HealthArea.sharing => Icons.phone_android_outlined,
    };

IconData _kindIcon(AttentionKind kind) => switch (kind) {
      AttentionKind.serverUpdate || AttentionKind.packages => Icons.update_outlined,
      AttentionKind.disk => Icons.storage_outlined,
      AttentionKind.driveHealth => Icons.monitor_heart_outlined,
      AttentionKind.backup => Icons.backup_outlined,
      AttentionKind.apps => Icons.apps_outlined,
      AttentionKind.temperature => Icons.thermostat_outlined,
      AttentionKind.memory => Icons.memory_outlined,
      AttentionKind.tailscale => Icons.vpn_key_outlined,
      AttentionKind.sharing => Icons.phone_android_outlined,
    };

/// Where each thing that needs attention is fixed, the same from Home and
/// from the Server health page: the backup job (or the decision it waits
/// for), the Updates page, the drive, the processor or memory
/// page, the Apps tab, Tailscale, companion sharing.
class HealthActions {
  const HealthActions({required this.controller, this.pushDetail, this.onOpenApps, this.onOpenFiles});

  final HomeController controller;

  /// Pushes a live detail screen; Home's own keeps its polling going while
  /// the screen is open. Null pushes it plainly.
  final Future<void> Function(Widget screen)? pushDetail;
  final VoidCallback? onOpenApps;
  final VoidCallback? onOpenFiles;

  /// Opens something outside the app (the web UI) rather than a page.
  /// Nothing does since backups got their own screens.
  static bool opensOutside(AttentionKind kind) => false;

  /// The action a screen reader announces for the row.
  static String hint(AttentionKind kind) => switch (kind) {
        AttentionKind.serverUpdate || AttentionKind.packages => 'open updates',
        AttentionKind.disk || AttentionKind.driveHealth => 'open storage',
        AttentionKind.backup => 'open the backup',
        AttentionKind.apps => 'open apps',
        AttentionKind.temperature => 'open processor',
        AttentionKind.memory => 'open memory',
        AttentionKind.tailscale => 'open Tailscale',
        AttentionKind.sharing => 'open companion devices',
      };

  Future<void> _push(BuildContext context, Widget screen) async {
    final push = pushDetail;
    if (push != null) return push(screen);
    await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => screen));
  }

  Future<void> open(BuildContext context, AttentionItem a) async {
    final c = controller;
    switch (a.kind) {
      case AttentionKind.serverUpdate:
      case AttentionKind.packages:
        final page = a.kind == AttentionKind.packages ? UpdatesPage.packages : UpdatesPage.nivaroos;
        await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => SystemUpdatesScreen(page: page)));
        await c.loadUpdates();
      case AttentionKind.disk:
      case AttentionKind.driveHealth:
        await _push(context, StorageDetailScreen(live: c.live, onRetry: c.refreshLive, onOpenFiles: onOpenFiles));
      case AttentionKind.temperature:
        await _push(context, CpuDetailScreen(live: c.live, onRetry: c.refreshLive, history: c.history));
      case AttentionKind.memory:
        await _push(context, MemoryDetailScreen(live: c.live, onRetry: c.refreshLive, history: c.history));
      case AttentionKind.apps:
        // A tab of the shell: back to Home first when on the health page.
        Navigator.of(context).popUntil((r) => r.isFirst);
        onOpenApps?.call();
      case AttentionKind.backup:
        final Widget screen = a.backupRunId.isNotEmpty
            ? BackupDecideScreen(runId: a.backupRunId, jobId: a.backupJobId)
            : a.backupJobId.isNotEmpty
                ? BackupJobScreen(jobId: a.backupJobId)
                : const BackupScreen();
        await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => screen));
        await c.refreshAll();
      case AttentionKind.tailscale:
        await TailscaleModal.show(context);
        await c.recheckTailscale();
      case AttentionKind.sharing:
        await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => const CompanionDevicesScreen()));
        await c.recheckSharing();
    }
  }
}

/// One thing that needs attention: its icon in the severity's colour, what
/// it is, one line of why, and a tap that goes where it is fixed.
class AttentionTile extends StatelessWidget {
  const AttentionTile({super.key, required this.item, required this.onTap, this.security = 0});

  final AttentionItem item;
  final VoidCallback? onTap;

  /// Security updates among the system packages, shown as a chip.
  final int security;

  @override
  Widget build(BuildContext context) {
    final a = item;
    final scheme = Theme.of(context).colorScheme;
    final status = switch (a.severity) {
      AttentionSeverity.error => Status.error,
      AttentionSeverity.warning => Status.warning,
      AttentionSeverity.info => Status.neutral,
    };
    final color = status == Status.neutral ? scheme.onSurfaceVariant : StatusColors.toneOf(context, status).color;
    final chip = a.kind == AttentionKind.packages && security > 0;
    return MergeSemantics(
      child: Semantics(
        onTapHint: HealthActions.hint(a.kind),
        child: ListTile(
          leading: Icon(_kindIcon(a.kind), color: color),
          title: Text(a.title),
          subtitle: chip
              ? Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Text('Debian packages'),
                    const SizedBox(height: Space.xs),
                    StatusChip(label: '$security security', status: Status.warning, icon: Icons.shield_outlined),
                  ],
                )
              : Text(a.detail),
          isThreeLine: chip,
          trailing: onTap == null ? null : Icon(HealthActions.opensOutside(a.kind) ? Icons.open_in_new_outlined : Icons.chevron_right),
          onTap: onTap,
        ),
      ),
    );
  }
}

/// Server health: whether the server is fine, what needs the owner and
/// where to fix it, and every check that passed - so the icon at the top
/// of Home always has an answer behind it.
///
/// It reads Home's [HomeController], so it shows what Home shows (one
/// model, server_health.dart) and stays live while it is open; pull to
/// refresh checks everything again. Offline, it keeps the last answer with
/// its age.
class ServerHealthScreen extends StatefulWidget {
  const ServerHealthScreen({super.key, required this.controller, this.actions});

  final HomeController controller;

  /// Where the rows go; Home passes its own so detail screens keep its
  /// polling. Null: plain pushes.
  final HealthActions? actions;

  @override
  State<ServerHealthScreen> createState() => _ServerHealthScreenState();
}

class _ServerHealthScreenState extends State<ServerHealthScreen> {
  HomeController get _c => widget.controller;
  late final HealthActions _actions = widget.actions ?? HealthActions(controller: widget.controller);

  @override
  void initState() {
    super.initState();
    _c.addListener(_changed);
    _c.live.addListener(_changed);
    // Opened on its own (not from a loaded Home): load everything.
    if (_c.live.value == null && _c.liveError == null) _c.refreshAll();
  }

  @override
  void dispose() {
    _c.removeListener(_changed);
    _c.live.removeListener(_changed);
    super.dispose();
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  @override
  Widget build(BuildContext context) {
    final live = _c.live.value;
    final error = _c.liveError;
    final List<Widget> slivers;
    if (live == null && error == null) {
      slivers = const [SliverLoadingList(rows: 8)];
    } else if (live == null) {
      final offline = error is ApiException && error.statusCode == null;
      slivers = [
        offline
            ? ErrorState.offline(onRetry: _c.refreshAll, sliver: true)
            : ErrorState(
                title: "Couldn't check the server",
                message: error.toString(),
                onRetry: _c.refreshAll,
                details: 'GET /v1/sys/utilization: $error',
                sliver: true,
              ),
      ];
    } else {
      final health = _c.health;
      slivers = [
        SliverList.list(children: [
          HealthSummary(health: health, checkedAt: _c.checkedAt ?? live.updatedAt, stale: live.stale),
          if (health.attention.isNotEmpty)
            TileGroup(title: 'Needs attention', children: [
              for (final a in health.attention)
                AttentionTile(
                  item: a,
                  security: _c.updates?.security ?? 0,
                  onTap: () => _actions.open(context, a),
                ),
            ]),
          if (health.fine.isNotEmpty)
            TileGroup(
              title: health.attention.isEmpty ? 'What was checked' : 'Everything else is fine',
              children: [for (final c in health.fine) _CheckTile(check: c, ok: true)],
            ),
          if (health.unchecked.isNotEmpty)
            TileGroup(title: 'Not checked', children: [for (final c in health.unchecked) _CheckTile(check: c, ok: false)]),
          const SizedBox(height: Space.lg),
        ]),
      ];
    }
    return AppScaffold.slivers(
      title: 'Server health',
      onRefresh: _c.refreshAll,
      banner: live != null && live.stale ? OfflineBanner(lastUpdated: live.updatedAt, onRetry: _c.refreshAll) : null,
      slivers: slivers,
    );
  }
}

/// The page's answer: a large status disc, "All good" or "3 things need
/// attention", and when it was checked.
class HealthSummary extends StatelessWidget {
  const HealthSummary({super.key, required this.health, required this.checkedAt, this.stale = false});

  final ServerHealth health;
  final DateTime checkedAt;

  /// Offline: this is the last known answer.
  final bool stale;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final t = DesignTokens.of(context);
    final status = healthStatus(health.worst);
    final passed = health.fine.length;
    final line = [
      '${stale ? 'Last checked' : 'Checked'} ${_lower(formatRelative(checkedAt))}',
      if (passed > 0) passed == 1 ? '1 check passed' : '$passed checks passed',
    ];
    final why = health.allGood
        ? (health.unchecked.isEmpty ? 'Nothing needs you right now.' : 'Nothing needs you right now. A few things couldn\'t be checked; they are listed below.')
        : null;
    final tone = StatusColors.toneOf(context, status);
    final dark = theme.brightness == Brightness.dark;
    final calm = status == Status.success || dark;
    final panel = !t.statusPanel
        ? null
        : status == Status.success
            ? scheme.surfaceContainerHighest
            : dark
                ? Color.alphaBlend(tone.color.withValues(alpha: .16), scheme.surfaceContainerHigh)
                : tone.container;
    final on = panel == null || calm ? scheme.onSurface : tone.onContainer;
    final muted = panel == null || calm ? scheme.onSurfaceVariant : tone.onContainer;

    final disc = StatusDisc(status: status, icon: healthIcon(status), size: 56);
    final text = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        AnimatedSwitcher(
          duration: Motion.of(context).short,
          child: Text(health.verdict, key: ValueKey(health.verdict), style: theme.textTheme.headlineSmall?.copyWith(color: on)),
        ),
        const SizedBox(height: Space.xs),
        FactLine.plain(line, style: theme.textTheme.bodyMedium?.copyWith(color: muted)),
        if (why != null) ...[
          const SizedBox(height: Space.xs),
          Text(why, style: theme.textTheme.bodyMedium?.copyWith(color: muted)),
        ],
      ],
    );
    // Large text: the disc above the words, so the headline gets the
    // whole width.
    final stacked = MediaQuery.textScalerOf(context).scale(10) > 15;

    final content = Semantics(
      container: true,
      liveRegion: true,
      label: [health.verdict, ...line, ?why].join('. '),
      child: ExcludeSemantics(child: stacked ? Column(crossAxisAlignment: CrossAxisAlignment.start, children: [disc, const SizedBox(height: Space.md), text]) : Row(
          crossAxisAlignment: CrossAxisAlignment.center,
          children: [disc, const SizedBox(width: Space.lg), Expanded(child: text)],
        )),
    );

    final gutter = Space.gutter(context);
    if (panel == null) return Padding(padding: EdgeInsets.fromLTRB(gutter, Space.md, gutter, Space.lg), child: content);
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, t.gap),
      child: Material(
        color: panel,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(t.cardRadius)),
        child: Padding(padding: const EdgeInsets.all(Space.lg), child: content),
      ),
    );
  }

  // "3 h ago" and "Sep 12" stay; "Just now" and "Yesterday" go lower
  // case mid-sentence.
  static String _lower(String s) => s == 'Just now' || s == 'Yesterday' ? s.toLowerCase() : s;
}

/// A check that passed (a quiet check mark) or couldn't be made (why).
class _CheckTile extends StatelessWidget {
  const _CheckTile({required this.check, required this.ok});

  final HealthCheck check;
  final bool ok;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final at = check.at;
    return MergeSemantics(
      child: ListTile(
        leading: Icon(_areaIcon(check.area), color: scheme.onSurfaceVariant),
        title: Text(check.title),
        subtitle: Text([check.detail, if (at != null) formatRelative(at)].join(' · ')),
        trailing: ok
            ? Icon(Icons.check_circle_outline, color: StatusColors.toneOf(context, Status.success).color, semanticLabel: 'OK')
            : Icon(Icons.remove_circle_outline, color: scheme.onSurfaceVariant, semanticLabel: 'Not checked'),
      ),
    );
  }
}
