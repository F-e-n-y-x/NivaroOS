import 'package:flutter/material.dart';

import '../models/drive_report.dart';
import '../models/server_health.dart' show DriveHealth;
import '../services/api_client.dart';
import '../ui/ui.dart';

/// One drive's health (Server health > Drives > a drive): the verdict and
/// why, the numbers that predict a failure with what they mean, how they
/// moved day by day, its self-tests (starting one is admin only - the
/// server says so otherwise) and the raw SMART table.
///
/// It reads `GET /v1/disks/health?fresh=1`: the server reads the drive
/// again, but never wakes a sleeping one for it.
class DriveHealthScreen extends StatefulWidget {
  const DriveHealthScreen({super.key, required this.drive});

  final DriveHealth drive;

  @override
  State<DriveHealthScreen> createState() => _DriveHealthScreenState();
}

class _DriveHealthScreenState extends State<DriveHealthScreen> {
  final _api = ApiClient.instance;
  DriveReport? _report;
  Object? _error;
  String _chartKey = '';
  bool _starting = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final res = await _api.get('/disks/health', query: {'path': widget.drive.path, 'fresh': '1'});
      final r = DriveReport.fromJson(Map<String, dynamic>.from(res['data'] as Map? ?? const {}));
      if (!mounted) return;
      setState(() {
        _report = r;
        _error = null;
        if (!r.chartKeys.contains(_chartKey)) _chartKey = r.defaultChartKey;
      });
      // A running test: check its progress again in a while.
      if (r.selfTest.running) Future.delayed(const Duration(seconds: 30), () => mounted ? _load() : null);
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _startTest(String type) async {
    final r = _report;
    if (r == null) return;
    final minutes = type == 'long' ? r.selfTest.longMinutes : r.selfTest.shortMinutes;
    final ok = await ConfirmDialog.confirm(
      context,
      title: type == 'long' ? 'Run a long self-test?' : 'Run a short self-test?',
      message: [
        'The drive tests itself in the background and stays usable.',
        if (minutes > 0) 'It takes about ${minutes >= 120 ? '${(minutes / 60).round()} hours' : '$minutes minutes'}.',
        if (type == 'long') 'A long test reads the whole surface.',
      ].join(' '),
      confirmLabel: 'Start test',
    );
    if (!ok || !mounted) return;
    final messenger = ScaffoldMessenger.of(context);
    setState(() => _starting = true);
    try {
      await _api.post('/disks/smart-test', body: {'path': widget.drive.path, 'type': type});
      messenger.showSnackBar(const SnackBar(content: Text('Self-test started')));
      await Future<void>.delayed(const Duration(seconds: 2));
      await _load();
    } on ApiException catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(e.message)));
    } finally {
      if (mounted) setState(() => _starting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final r = _report;
    final List<Widget> slivers;
    if (r == null && _error == null) {
      slivers = const [SliverLoadingList(rows: 8)];
    } else if (r == null) {
      final e = _error;
      slivers = [
        e is ApiException && e.statusCode == null
            ? ErrorState.offline(onRetry: _load, sliver: true)
            : ErrorState(title: "Couldn't read the drive's health", message: e.toString(), onRetry: _load, details: 'GET /v1/disks/health: $e', sliver: true),
      ];
    } else {
      slivers = [SliverList.list(children: _body(context, r))];
    }
    return AppScaffold.slivers(title: widget.drive.label, onRefresh: _load, slivers: slivers);
  }

  List<Widget> _body(BuildContext context, DriveReport r) {
    final status = switch (r.verdict) {
      DriveVerdict.good => Status.success,
      DriveVerdict.watch => Status.warning,
      DriveVerdict.failing => Status.error,
      DriveVerdict.unknown => Status.neutral,
    };
    final more = [...r.reasons.skip(1), ...r.changes];
    final st = r.selfTest;
    return [
      Notice(
        title: driveVerdictLabel(r.verdict),
        message: r.summary,
        status: status,
        icon: switch (status) {
          Status.success => Icons.check_circle_outline,
          Status.error => Icons.error_outline,
          Status.warning => Icons.warning_amber_outlined,
          _ => Icons.help_outline,
        },
        details: more.isEmpty ? null : [for (final m in more) Text(m, style: Notice.detailStyle(context, status))],
      ),
      if (r.stale) const GroupNote('The drive is asleep. This is its last reading while it was awake.'),
      for (final n in r.notes) GroupNote(n),
      if (r.metrics.isNotEmpty)
        TileGroup(title: 'Key numbers', children: [
          for (final m in r.metrics)
            MetricRow(
              icon: _metricIcon(m),
              label: m.label,
              value: m.valueText,
              unit: m.unit.isEmpty ? null : m.unit,
              supporting: [if (m.level == 'watch') 'Watch', if (m.level == 'failing') 'Failing', ?m.hint, m.explain].join(' · '),
            ),
        ]),
      if (r.chartKeys.isNotEmpty) _history(context, r),
      if (r.status == 'ok')
        TileGroup(
          title: 'Self-test',
          footer: st.supported ? 'The drive tests itself in the background and stays usable. Only an administrator can start one.' : null,
          children: [
            ListTile(
              leading: const Icon(Icons.fact_check_outlined),
              title: Text(st.status),
              subtitle: st.running ? LinearProgressIndicator(value: (100 - st.remainingPercent) / 100) : null,
            ),
            if (st.supported && !st.running)
              Padding(
                padding: const EdgeInsets.fromLTRB(Space.lg, 0, Space.lg, Space.md),
                child: Wrap(spacing: Space.sm, runSpacing: Space.sm, children: [
                  FilledButton.tonal(onPressed: _starting ? null : () => _startTest('short'), child: Text(st.shortMinutes > 0 ? 'Short test · ${st.shortMinutes} min' : 'Short test')),
                  OutlinedButton(onPressed: _starting ? null : () => _startTest('long'), child: Text(st.longMinutes > 0 ? 'Long test · ${_duration(st.longMinutes)}' : 'Long test')),
                ]),
              ),
            for (final t in st.last.skip(st.running ? 0 : 1))
              ListTile(
                dense: true,
                leading: Icon(t.passed ? Icons.check_circle_outline : Icons.error_outline,
                    color: t.passed ? StatusColors.toneOf(context, Status.success).color : StatusColors.toneOf(context, Status.error).color),
                title: Text(t.type),
                subtitle: Text('${t.result} · at ${groupDigits(t.hours)} h'),
              ),
          ],
        ),
      if (r.raw.isNotEmpty)
        TileGroup(children: [
          ExpansionTile(
            leading: const Icon(Icons.table_rows_outlined),
            title: const Text('Raw SMART data'),
            subtitle: Text('${r.raw.length} values'),
            children: [
              for (final a in r.raw)
                ListTile(
                  dense: true,
                  title: Text(a.id > 0 ? '${a.id} · ${a.name}' : a.name),
                  subtitle: a.value.isEmpty ? null : Text('Value ${a.value} · worst ${a.worst} · threshold ${a.thresh}${a.failed.isNotEmpty && a.failed != '-' ? ' · failed ${a.failed}' : ''}'),
                  trailing: Text(a.raw, style: Theme.of(context).textTheme.bodyMedium?.tabular),
                ),
            ],
          ),
        ]),
      const SizedBox(height: Space.lg),
    ];
  }

  Widget _history(BuildContext context, DriveReport r) {
    final key = _chartKey;
    final label = r.metrics.where((m) => m.key == key).firstOrNull?.label ?? key;
    final values = [for (final s in r.history) if (s.values.containsKey(key)) s.values[key]!.toDouble()];
    final lo = values.isEmpty ? 0 : values.reduce((a, b) => a < b ? a : b).round();
    final hi = values.isEmpty ? 0 : values.reduce((a, b) => a > b ? a : b).round();
    return TileGroup(
      title: 'History',
      footer: values.length < 2 ? 'One reading a day; the chart fills in over the coming days.' : '${r.history.first.date} to ${r.history.last.date}',
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(Space.lg, Space.md, Space.lg, 0),
          child: Wrap(spacing: Space.sm, runSpacing: Space.sm, children: [
            for (final k in r.chartKeys)
              ChoiceChip(
                label: Text(r.metrics.where((m) => m.key == k).firstOrNull?.label ?? k),
                selected: k == key,
                onSelected: (_) => setState(() => _chartKey = k),
              ),
          ]),
        ),
        Padding(
          padding: const EdgeInsets.all(Space.lg),
          child: LiveChart(
            series: [ChartSeries(values)],
            capacity: values.length < 2 ? 2 : values.length,
            grid: true,
            semanticLabel: '$label over the last ${values.length} days: $lo to $hi',
            axisLabels: values.isEmpty || lo == hi ? const [] : [(hi.toDouble(), '$hi'), (lo.toDouble(), '$lo')],
          ),
        ),
      ],
    );
  }

  static String _duration(int minutes) => minutes >= 120 ? '${(minutes / 60).round()} h' : '$minutes min';

  static IconData _metricIcon(DriveMetric m) => switch (m.key) {
        'temperature' || 'temperature_max' => Icons.thermostat_outlined,
        'power_on_hours' => Icons.schedule_outlined,
        'power_cycles' => Icons.power_settings_new_outlined,
        'unsafe_shutdowns' => Icons.power_off_outlined,
        'crc_errors' => Icons.cable_outlined,
        'wear' || 'available_spare' => Icons.battery_5_bar_outlined,
        _ => Icons.grid_view_outlined,
      };
}
