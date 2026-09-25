import 'dart:convert';

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:intl/intl.dart';

import '../services/api_client.dart';
import '../ui/ui.dart';
import '../widgets/log_controls.dart';

enum LogLevel { error, warning, info, debug, other }

/// One entry of the NivaroOS log (`GET /v1/sys/logs`), which is zap's
/// console format: `time<TAB>level<TAB>message<TAB>{json fields}`.
@immutable
class LogEntry {
  const LogEntry({required this.raw, required this.message, this.time, this.level = LogLevel.other, this.fields = const {}, this.repeats = 1});

  final String raw;
  final DateTime? time;
  final LogLevel level;
  final String message;
  final Map<String, dynamic> fields;

  /// How many identical entries in a row this one stands for.
  final int repeats;

  /// Where it was logged: the file name, or the function.
  String get source {
    final file = fields['file']?.toString() ?? '';
    if (file.isNotEmpty) return file.split('/').last;
    final fn = fields['func']?.toString() ?? '';
    return fn;
  }

  /// The fields that say something beyond where it was logged.
  Map<String, dynamic> get extra => {
        for (final e in fields.entries)
          if (!const {'func', 'file', 'line'}.contains(e.key)) e.key: e.value,
      };

  static LogLevel _level(String s) => switch (s.toLowerCase()) {
        'error' || 'fatal' || 'panic' || 'dpanic' => LogLevel.error,
        'warn' || 'warning' => LogLevel.warning,
        'info' => LogLevel.info,
        'debug' => LogLevel.debug,
        _ => LogLevel.other,
      };

  static LogEntry parse(String line) {
    final parts = line.split('\t');
    if (parts.length >= 3) {
      final time = DateTime.tryParse(parts[0].trim());
      final level = _level(parts[1].trim());
      if (time != null && level != LogLevel.other) {
        var fields = <String, dynamic>{};
        var message = parts.sublist(2).join('\t');
        if (parts.length >= 4) {
          try {
            final decoded = jsonDecode(parts.last);
            if (decoded is Map) {
              fields = Map<String, dynamic>.from(decoded);
              message = parts.sublist(2, parts.length - 1).join('\t');
            }
          } catch (_) {}
        }
        return LogEntry(raw: line, time: time, level: level, message: message.trim(), fields: fields);
      }
    }
    // Anything else (a stack trace, a panic, output from a helper) is kept
    // as it is.
    final l = line.toLowerCase();
    final level = l.contains('error') || l.contains('fatal') || l.contains('panic')
        ? LogLevel.error
        : l.contains('warn')
            ? LogLevel.warning
            : LogLevel.other;
    return LogEntry(raw: line, message: line.trim(), level: level);
  }
}

/// The log text as entries, newest first, with runs of the same message
/// folded into one entry that counts them.
List<LogEntry> parseLogs(String text) {
  final out = <LogEntry>[];
  for (final line in text.split('\n')) {
    if (line.trim().isEmpty) continue;
    final e = LogEntry.parse(line);
    final prev = out.isEmpty ? null : out.last;
    if (prev != null && prev.level == e.level && prev.message == e.message && _sameExtra(prev, e)) {
      // Keep the newest occurrence's time and fields.
      out[out.length - 1] = LogEntry(raw: e.raw, message: e.message, time: e.time, level: e.level, fields: e.fields, repeats: prev.repeats + 1);
    } else {
      out.add(e);
    }
  }
  return out.reversed.toList();
}

bool _sameExtra(LogEntry a, LogEntry b) => mapEquals(a.extra, b.extra);

/// The server's own log: what NivaroOS did and what went wrong, newest
/// first. Tap an entry for its details.
class SystemLogsScreen extends StatefulWidget {
  const SystemLogsScreen({super.key});

  @override
  State<SystemLogsScreen> createState() => _SystemLogsScreenState();
}

class _SystemLogsScreenState extends State<SystemLogsScreen> {
  static const _pageSizes = [200, 1000, 5000];

  final _search = TextEditingController();
  List<LogEntry>? _entries;
  String _raw = '';
  Object? _error;
  bool _stale = false;
  DateTime? _loadedAt;
  int _page = 0;
  LogFilter _filter = LogFilter.all;
  bool _loadingMore = false;

  @override
  void initState() {
    super.initState();
    _load();
    _search.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final res = await ApiClient.instance.get('/sys/logs', query: {'line': _pageSizes[_page]});
      final data = res['data'];
      final text = data is String ? data : (data is List ? data.join('\n') : '');
      if (!mounted) return;
      setState(() {
        _raw = text;
        _entries = parseLogs(text);
        _error = null;
        _stale = false;
        _loadedAt = clock.now();
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        if (_entries == null) {
          _error = e;
        } else {
          _stale = true;
        }
      });
    }
  }

  Future<void> _loadMore() async {
    if (_page >= _pageSizes.length - 1) return;
    setState(() {
      _page++;
      _loadingMore = true;
    });
    await _load();
    if (mounted) setState(() => _loadingMore = false);
  }

  void _retry() {
    setState(() => _error = null);
    _load();
  }

  Future<void> _copyAll() async {
    await Clipboard.setData(ClipboardData(text: _raw));
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Log copied')));
  }

  List<LogEntry> _visible(List<LogEntry> all) {
    final q = _search.text.trim().toLowerCase();
    return all.where((e) {
      if (_filter == LogFilter.errors && e.level != LogLevel.error) return false;
      if (_filter == LogFilter.problems && e.level != LogLevel.error && e.level != LogLevel.warning) return false;
      if (q.isNotEmpty && !e.raw.toLowerCase().contains(q)) return false;
      return true;
    }).toList();
  }

  @override
  Widget build(BuildContext context) {
    final entries = _entries;
    final List<Widget> slivers;
    if (entries == null && _error == null) {
      slivers = const [SliverLoadingList(rows: 10)];
    } else if (entries == null) {
      final e = _error;
      slivers = [
        e is ApiException && e.statusCode == null
            ? ErrorState.offline(onRetry: _retry, sliver: true)
            : ErrorState(title: "Couldn't load the log", message: e.toString(), onRetry: _retry, details: 'GET /v1/sys/logs: $e', sliver: true),
      ];
    } else {
      final visible = _visible(entries);
      slivers = [
        PinnedHeaderSliver(child: _controls(context)),
        if (entries.isEmpty)
          const EmptyState(
            sliver: true,
            icon: Icons.receipt_long_outlined,
            title: 'The log is empty',
            message: 'NivaroOS has not written anything since the log was last cleared.',
          )
        else if (visible.isEmpty)
          EmptyState(
            sliver: true,
            icon: Icons.search_off_outlined,
            title: _search.text.trim().isNotEmpty ? 'Nothing matches' : (_filter == LogFilter.errors ? 'No errors' : 'No warnings or errors'),
            message: _search.text.trim().isNotEmpty
                ? 'No entry in the last ${_pageSizes[_page]} lines contains “${_search.text.trim()}”.'
                : 'Nothing went wrong in the last ${_pageSizes[_page]} lines.',
            actionLabel: 'Show everything',
            onAction: () => setState(() {
              _search.clear();
              _filter = LogFilter.all;
            }),
          )
        else ...[
          SliverList.builder(itemCount: visible.length, itemBuilder: (context, i) => _LogTile(entry: visible[i])),
          if (_page < _pageSizes.length - 1)
            SliverToBoxAdapter(
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: Space.lg),
                child: Center(
                  child: _loadingMore
                      ? const SizedBox(height: 48, child: Center(child: CircularProgressIndicator()))
                      : OutlinedButton(onPressed: _loadMore, child: const Text('Show older entries')),
                ),
              ),
            ),
        ],
      ];
    }

    return AppScaffold.slivers(
      title: 'Logs',
      onRefresh: _load,
      banner: _stale ? OfflineBanner(lastUpdated: _loadedAt, onRetry: _load) : null,
      actions: [
        if (entries != null && entries.isNotEmpty)
          IconButton(onPressed: _copyAll, tooltip: 'Copy log', icon: const Icon(Icons.copy_outlined)),
      ],
      slivers: slivers,
    );
  }

  Widget _controls(BuildContext context) =>
      LogFilterBar(search: _search, filter: _filter, onFilter: (f) => setState(() => _filter = f));
}

String _time(DateTime t) {
  final local = t.toLocal();
  final now = clock.now();
  final sameDay = local.year == now.year && local.month == now.month && local.day == now.day;
  return sameDay ? DateFormat.Hms().format(local) : DateFormat.MMMd().add_Hms().format(local);
}

class _LogTile extends StatelessWidget {
  const _LogTile({required this.entry});

  final LogEntry entry;

  static (IconData, Color, String) look(BuildContext context, LogLevel level) {
    final scheme = Theme.of(context).colorScheme;
    return switch (level) {
      LogLevel.error => (Icons.error_outline, scheme.error, 'Error'),
      LogLevel.warning => (Icons.warning_amber_outlined, StatusColors.of(context).warning.color, 'Warning'),
      LogLevel.info => (Icons.info_outline, scheme.onSurfaceVariant, 'Info'),
      LogLevel.debug => (Icons.bug_report_outlined, scheme.onSurfaceVariant, 'Debug'),
      LogLevel.other => (Icons.notes_outlined, scheme.onSurfaceVariant, 'Output'),
    };
  }

  @override
  Widget build(BuildContext context) {
    final (icon, color, levelName) = look(context, entry.level);
    final t = entry.time;
    final meta = [
      if (t != null) _time(t),
      if (entry.source.isNotEmpty) entry.source,
      if (entry.repeats > 1) '${entry.repeats} times',
    ].join(' · ');
    return MergeSemantics(
      child: ListTile(
        leading: Semantics(label: levelName, child: Icon(icon, color: color)),
        title: Text(entry.message, maxLines: 3, overflow: TextOverflow.ellipsis),
        subtitle: meta.isEmpty ? null : Text(meta, style: Theme.of(context).textTheme.bodySmall?.tabular.copyWith(color: Theme.of(context).colorScheme.onSurfaceVariant)),
        onTap: () => _showDetails(context),
      ),
    );
  }

  void _showDetails(BuildContext context) {
    final theme = Theme.of(context);
    final (icon, color, levelName) = look(context, entry.level);
    final mono = DesignTokens.of(context).mono(theme.textTheme.bodySmall).copyWith(color: theme.colorScheme.onSurfaceVariant);
    final t = entry.time;
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      useSafeArea: true,
      isScrollControlled: true,
      builder: (sheet) => SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(Space.lg, 0, Space.lg, Space.lg),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Row(children: [
                Icon(icon, color: color, size: 20),
                const SizedBox(width: Space.sm),
                Expanded(
                  child: Text(
                    [levelName, if (t != null) formatExact(t), if (entry.repeats > 1) '${entry.repeats} times in a row'].join(' · '),
                    style: theme.textTheme.labelLarge,
                  ),
                ),
              ]),
              const SizedBox(height: Space.md),
              SelectableText(entry.message, style: theme.textTheme.bodyLarge),
              if (entry.extra.isNotEmpty || entry.source.isNotEmpty) ...[
                const SizedBox(height: Space.lg),
                SelectableText(
                  [
                    for (final e in entry.extra.entries) '${e.key}: ${e.value}',
                    if (entry.fields['file'] != null) 'file: ${entry.fields['file']}${entry.fields['line'] == null ? '' : ':${entry.fields['line']}'}',
                    if (entry.fields['func'] != null) 'func: ${entry.fields['func']}',
                  ].join('\n'),
                  style: mono,
                ),
              ],
              const SizedBox(height: Space.lg),
              FilledButton.tonalIcon(style: tonalButtonStyle(context), 
                onPressed: () async {
                  await Clipboard.setData(ClipboardData(text: entry.raw));
                  if (sheet.mounted) Navigator.of(sheet).pop();
                  if (context.mounted) ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Entry copied')));
                },
                icon: const Icon(Icons.copy_outlined),
                label: const Text('Copy entry'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
