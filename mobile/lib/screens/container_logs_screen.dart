import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:intl/intl.dart';

import '../models/container_entry.dart';
import '../services/api_client.dart';
import '../ui/ui.dart';
import '../widgets/log_controls.dart' show LogFilter, LogFilterBar;

/// How serious a log line looks, from the words in it.
enum LogLevel { normal, warning, error }

/// One log line, with Docker's timestamp (when the server sent one) split
/// off so it can be shown in its own column or hidden.
class LogLine {
  const LogLine(this.text, {this.time, this.level = LogLevel.normal});

  final String text;
  final DateTime? time;
  final LogLevel level;

  static final _stamp = RegExp(r'^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d))\s?');
  static final _error = RegExp(r'\b(err|error|fatal|panic|crit|critical|exception)\b', caseSensitive: false);
  static final _warn = RegExp(r'\b(wrn|warn|warning)\b', caseSensitive: false);

  static LogLine parse(String raw) {
    var text = raw.replaceAll('\r', '');
    DateTime? time;
    final m = _stamp.firstMatch(text);
    if (m != null) {
      // Dart parses at most microseconds; Docker sends nanoseconds.
      final stamp = m.group(1)!.replaceFirstMapped(RegExp(r'\.(\d{6})\d+'), (x) => '.${x.group(1)}');
      time = DateTime.tryParse(stamp);
      text = text.substring(m.end);
    }
    final level = _error.hasMatch(text)
        ? LogLevel.error
        : _warn.hasMatch(text)
            ? LogLevel.warning
            : LogLevel.normal;
    return LogLine(text, time: time, level: level);
  }

  /// Splits a log dump into lines, dropping the empty last one.
  static List<LogLine> parseAll(String logs) {
    final lines = logs.split('\n');
    if (lines.isNotEmpty && lines.last.trim().isEmpty) lines.removeLast();
    return lines.map(parse).toList();
  }
}

/// An app's container logs: the last lines, following new ones every few
/// seconds, with a filter, timestamps and copy.
class ContainerLogsScreen extends StatefulWidget {
  const ContainerLogsScreen({super.key, required this.app});

  /// Same as the constructor; reads well at call sites.
  factory ContainerLogsScreen.forApp(InstalledApp app, {Key? key}) => ContainerLogsScreen(key: key, app: app);

  final InstalledApp app;

  /// How many lines can be asked for.
  static const lineChoices = [200, 500, 1000, 5000];

  @override
  State<ContainerLogsScreen> createState() => _ContainerLogsScreenState();
}

class _ContainerLogsScreenState extends State<ContainerLogsScreen> {
  final _scroll = ScrollController();
  final _filter = TextEditingController();
  List<LogLine>? _lines;
  Object? _error;
  DateTime? _updatedAt;
  bool _follow = true;
  bool _timestamps = true;
  LogFilter _level = LogFilter.all;
  int _count = 500;
  bool _loading = false;
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _filter.addListener(() => setState(() {}));
    _load();
    _startFollowing();
  }

  @override
  void dispose() {
    _timer?.cancel();
    _scroll.dispose();
    _filter.dispose();
    super.dispose();
  }

  void _startFollowing() {
    _timer?.cancel();
    if (_follow) _timer = Timer.periodic(const Duration(seconds: 3), (_) => _load());
  }

  bool get _atBottom => !_scroll.hasClients || _scroll.position.pixels >= _scroll.position.maxScrollExtent - 48;

  Future<void> _load() async {
    if (_loading) return;
    _loading = true;
    final stick = _atBottom;
    try {
      final req = widget.app.logsRequest(_count);
      final res = await ApiClient.instance.get(req.path, query: req.query);
      final lines = LogLine.parseAll(res['data']?.toString() ?? '');
      if (!mounted) return;
      setState(() {
        _lines = lines;
        _error = null;
        _updatedAt = clock.now();
      });
      if (_follow && stick) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (_scroll.hasClients) _scroll.jumpTo(_scroll.position.maxScrollExtent);
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      _loading = false;
    }
  }

  List<LogLine> get _visible {
    final lines = _lines ?? const [];
    final q = _filter.text.trim().toLowerCase();
    if (q.isEmpty && _level == LogFilter.all) return lines;
    return lines.where((l) {
      if (_level == LogFilter.errors && l.level != LogLevel.error) return false;
      if (_level == LogFilter.problems && l.level == LogLevel.normal) return false;
      return q.isEmpty || l.text.toLowerCase().contains(q);
    }).toList();
  }

  void _copy() {
    final text = _visible.map((l) => _timestamps && l.time != null ? '${l.time!.toIso8601String()} ${l.text}' : l.text).join('\n');
    Clipboard.setData(ClipboardData(text: text));
    HapticFeedback.lightImpact();
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(_visible.length == 1 ? '1 line copied' : '${_visible.length} lines copied')));
  }

  void _toggleFollow() {
    setState(() => _follow = !_follow);
    _startFollowing();
    if (_follow) {
      _load();
      if (_scroll.hasClients) _scroll.jumpTo(_scroll.position.maxScrollExtent);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final lines = _lines;
    final error = _error;
    final gutter = Space.gutter(context);

    final Widget body;
    if (lines == null) {
      if (error == null) {
        body = const LoadingList(rows: 12, leading: SkeletonLeading.none, subtitle: false);
      } else if (error is ApiException && error.isUnreachable) {
        body = ErrorState.offline(onRetry: _load, details: error.details);
      } else {
        body = ErrorState(
          title: "Couldn't load the logs",
          message: error is ApiException ? error.message : error.toString(),
          onRetry: _load,
          details: error is ApiException ? error.details : null,
        );
      }
    } else if (lines.isEmpty) {
      body = EmptyState(
        icon: Icons.receipt_long_outlined,
        title: 'No log lines yet',
        message: _follow ? '${widget.app.title} hasn’t printed anything. New lines appear here as they come.' : '${widget.app.title} hasn’t printed anything.',
      );
    } else {
      final visible = _visible;
      final q = _filter.text.trim();
      body = visible.isEmpty
          ? EmptyState(
              icon: Icons.search_off_outlined,
              title: q.isNotEmpty ? 'No lines match “$q”' : (_level == LogFilter.errors ? 'No errors' : 'No warnings or errors'),
              message: 'The search looks at the last $_count lines.',
              actionLabel: 'Show everything',
              onAction: () => setState(() {
                _filter.clear();
                _level = LogFilter.all;
              }),
            )
          : SelectionArea(
              child: ListView.builder(
                controller: _scroll,
                padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.lg),
                itemCount: visible.length,
                itemBuilder: (context, i) => _LogLineView(line: visible[i], showTime: _timestamps),
              ),
            );
    }

    final offline = error is ApiException && error.isUnreachable;
    // With lines on screen, a failed refresh is said in the status bar
    // (being offline gets the banner) instead of replacing the lines.
    final staleNote = lines != null && error != null && !offline ? " · couldn't refresh" : '';
    final status = lines == null
        ? null
        : Material(
            color: scheme.surfaceContainer,
            child: Padding(
              padding: EdgeInsets.fromLTRB(gutter, Space.xs, gutter, Space.xs + MediaQuery.paddingOf(context).bottom),
              child: Row(children: [
                Expanded(
                  child: Text(
                    '${_visible.length == lines.length ? '${lines.length}' : '${_visible.length} of ${lines.length}'} lines'
                    '${_updatedAt == null ? '' : ' · updated ${_lower(formatRelative(_updatedAt!))}'}$staleNote',
                    style: theme.textTheme.bodySmall?.tabular.copyWith(color: scheme.onSurfaceVariant),
                  ),
                ),
                const SizedBox(width: Space.sm),
                FilterChip(
                  label: const Text('Follow'),
                  tooltip: 'Load new lines every few seconds and keep the newest in view',
                  selected: _follow,
                  onSelected: (_) => _toggleFollow(),
                ),
              ]),
            ),
          );

    return AppScaffold(
      title: '${widget.app.title} logs',
      maxContentWidth: null,
      onRefresh: _load,
      banner: lines != null && offline ? OfflineBanner(lastUpdated: _updatedAt, onRetry: _load) : null,
      actions: [
        IconButton(tooltip: 'Copy log', icon: const Icon(Icons.copy_outlined), onPressed: lines == null || lines.isEmpty ? null : _copy),
        PopupMenuButton<Object>(
          tooltip: 'More options',
          onSelected: (v) {
            if (v is int) {
              setState(() => _count = v);
              _load();
            } else if (v == 'time') {
              setState(() => _timestamps = !_timestamps);
            }
          },
          itemBuilder: (_) => [
            CheckedPopupMenuItem(value: 'time', checked: _timestamps, child: const Text('Show times')),
            const PopupMenuDivider(),
            for (final n in ContainerLogsScreen.lineChoices)
              CheckedPopupMenuItem(value: n, checked: _count == n, child: Text('Last ${NumberFormat.decimalPattern().format(n)} lines')),
          ],
        ),
      ],
      body: Column(children: [
        if (lines != null && lines.isNotEmpty)
          LogFilterBar(search: _filter, filter: _level, onFilter: (f) => setState(() => _level = f)),
        Expanded(child: body),
        ?status,
      ]),
    );
  }
}

String _lower(String relative) => relative == 'Just now' ? 'just now' : relative;

class _LogLineView extends StatelessWidget {
  const _LogLineView({required this.line, required this.showTime});

  final LogLine line;
  final bool showTime;

  static final _time = DateFormat.Hms();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final mono = theme.textTheme.bodySmall?.copyWith(fontFamily: 'monospace', color: scheme.onSurface);
    final color = switch (line.level) {
      LogLevel.error => scheme.error,
      LogLevel.warning => StatusColors.of(context).warning.color,
      LogLevel.normal => scheme.onSurface,
    };
    final time = line.time;
    final text = Text(line.text, style: mono?.copyWith(color: color));
    final stamp = time == null ? null : Text(_time.format(time.toLocal()), style: mono?.tabular.copyWith(color: scheme.onSurfaceVariant));
    // At large text a time column leaves a few words per line, so the
    // time goes above its line instead.
    final above = MediaQuery.textScalerOf(context).scale(10) > 13;
    return Padding(
      padding: EdgeInsets.symmetric(vertical: above ? Space.xs : Space.xs / 2),
      child: !showTime || stamp == null
          ? text
          : above
              ? Column(crossAxisAlignment: CrossAxisAlignment.start, children: [stamp, text])
              : Row(crossAxisAlignment: CrossAxisAlignment.start, children: [stamp, const SizedBox(width: Space.md), Expanded(child: text)]),
    );
  }
}
