import 'package:flutter/material.dart';

import '../services/api_client.dart';
import '../ui/ui.dart';
import '../utils/format.dart';

// "Free up memory": the confirm sheet behind the Memory card's Free up
// button and the Memory page's row. It asks the server (POST
// /v1/sys/memory/clear) to drop its file cache - and, when ticked and
// safe, to move swap back into RAM - then says what that freed.

/// One /proc/meminfo reading from the server, in bytes.
class MemorySnapshot {
  const MemorySnapshot({required this.free, required this.available, required this.cached, required this.swapUsed});

  final int free;
  final int available;
  final int cached;
  final int swapUsed;

  factory MemorySnapshot.fromJson(Map<String, dynamic> j) => MemorySnapshot(
        free: _int(j['mem_free']),
        available: _int(j['mem_available']),
        cached: _int(j['cached']) + _int(j['buffers']),
        swapUsed: _int(j['swap_used']),
      );
}

int _int(Object? v) => v is num ? v.toInt() : int.tryParse('$v') ?? 0;

/// What a clear did: the readings before and after, and how much memory
/// became free.
class MemoryClearResult {
  const MemoryClearResult({required this.before, required this.after, required this.freed, this.swapReclaimed = false, this.note = ''});

  final MemorySnapshot before;
  final MemorySnapshot after;
  final int freed;
  final bool swapReclaimed;

  /// Something that didn't stop the clear ("swap was left alone").
  final String note;

  factory MemoryClearResult.fromJson(Map<String, dynamic> j) => MemoryClearResult(
        before: MemorySnapshot.fromJson(Map<String, dynamic>.from(j['before'] as Map? ?? const {})),
        after: MemorySnapshot.fromJson(Map<String, dynamic>.from(j['after'] as Map? ?? const {})),
        freed: _int(j['freed']),
        swapReclaimed: j['swap_reclaimed'] == true,
        note: (j['note'] ?? '').toString(),
      );
}

typedef FreeMemoryRunner = Future<MemoryClearResult> Function({required bool swap});

/// Asks the server to free its memory. Emptying swap can take minutes,
/// so that request gets a longer wait than the usual minute.
Future<MemoryClearResult> freeServerMemory({required bool swap}) async {
  final res = await ApiClient.instance.post(
    '/sys/memory/clear',
    body: {'swap': swap},
    timeout: swap ? const Duration(minutes: 12) : null,
  );
  final data = res['data'];
  if (data is! Map) throw ApiException('Unexpected answer from the server.');
  return MemoryClearResult.fromJson(Map<String, dynamic>.from(data));
}

/// Opens the sheet. [swapUsed] is the server's swap in use now; the "Also
/// empty swap" choice only shows when there is some. [onDone] runs after
/// a clear worked (refresh the reading).
Future<void> showFreeMemorySheet(BuildContext context, {required int swapUsed, VoidCallback? onDone, FreeMemoryRunner? run}) =>
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      useSafeArea: true,
      isScrollControlled: true,
      builder: (_) => FreeMemorySheet(swapUsed: swapUsed, onDone: onDone, run: run ?? freeServerMemory),
    );

class FreeMemorySheet extends StatefulWidget {
  const FreeMemorySheet({super.key, required this.swapUsed, required this.run, this.onDone});

  final int swapUsed;
  final FreeMemoryRunner run;
  final VoidCallback? onDone;

  @override
  State<FreeMemorySheet> createState() => _FreeMemorySheetState();
}

class _FreeMemorySheetState extends State<FreeMemorySheet> {
  bool _swap = false;
  bool _running = false;
  String? _error;
  MemoryClearResult? _result;

  static const explanation =
      'Linux uses spare memory as a cache to speed things up; clearing it frees memory now, but things may be slower for a moment while the cache refills.';

  Future<void> _go() async {
    setState(() {
      _running = true;
      _error = null;
    });
    try {
      final r = await widget.run(swap: _swap && widget.swapUsed > 0);
      if (!mounted) return;
      setState(() {
        _running = false;
        _result = r;
      });
      widget.onDone?.call();
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _running = false;
        _error = e.toString().replaceFirst('Exception: ', '');
      });
    }
  }

  static String _headline(MemoryClearResult r) => r.freed < 1024 * 1024 ? 'Memory was already clear' : 'Freed ${formatBytes(r.freed)}';

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final r = _result;
    return SafeArea(
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(Space.lg, 0, Space.lg, Space.lg),
        child: AnimatedSize(
          duration: Motion.of(context).medium,
          alignment: Alignment.topCenter,
          child: r == null ? _confirm(context, theme) : _done(context, theme, r),
        ),
      ),
    );
  }

  Widget _confirm(BuildContext context, ThemeData theme) {
    final scheme = theme.colorScheme;
    final error = _error;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Semantics(header: true, child: Text('Free up memory?', style: theme.textTheme.titleLarge)),
        const SizedBox(height: Space.sm),
        Text(explanation, style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
        if (widget.swapUsed > 0) ...[
          const SizedBox(height: Space.sm),
          CheckboxListTile(
            value: _swap,
            onChanged: _running ? null : (v) => setState(() => _swap = v ?? false),
            contentPadding: EdgeInsets.zero,
            controlAffinity: ListTileControlAffinity.leading,
            title: const Text('Also empty swap'),
            subtitle: Text("Moves ${formatBytes(widget.swapUsed)} from swap back into memory. The server skips it if there isn't enough room. It can take a minute or two."),
          ),
        ],
        if (error != null) ...[
          const SizedBox(height: Space.md),
          Semantics(
            liveRegion: true,
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(Icons.error_outline, color: scheme.error, size: 20),
                const SizedBox(width: Space.sm),
                Expanded(child: Text(error, style: theme.textTheme.bodyMedium?.copyWith(color: scheme.error))),
              ],
            ),
          ),
        ],
        const SizedBox(height: Space.lg),
        if (_running) ...[
          Semantics(liveRegion: true, child: Text(_swap ? 'Freeing memory and emptying swap…' : 'Freeing memory…', style: theme.textTheme.bodyMedium)),
          const SizedBox(height: Space.sm),
          const LinearProgressIndicator(),
          const SizedBox(height: Space.md),
        ],
        Wrap(
          alignment: WrapAlignment.end,
          spacing: Space.sm,
          runSpacing: Space.sm,
          children: [
            TextButton(onPressed: _running ? null : () => Navigator.of(context).pop(), child: const Text('Cancel')),
            FilledButton(onPressed: _running ? null : _go, child: const Text('Free up memory')),
          ],
        ),
      ],
    );
  }

  Widget _done(BuildContext context, ThemeData theme, MemoryClearResult r) {
    final scheme = theme.colorScheme;
    final data = DesignTokens.of(context).data.copyWith(color: scheme.onSurfaceVariant);
    // "1.3 GB → 3.4 GB", the arrow drawn as an icon (not every text face
    // has one) and read as "to".
    Widget change(int a, int b) {
      final from = formatBytes(a), to = formatBytes(b);
      return Semantics(
        label: '$from to $to',
        excludeSemantics: true,
        child: Text.rich(
          TextSpan(children: [
            TextSpan(text: from),
            WidgetSpan(
              alignment: PlaceholderAlignment.middle,
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: Space.xs),
                child: Icon(Icons.arrow_forward, size: (data.fontSize ?? 12) + 2, color: scheme.onSurfaceVariant),
              ),
            ),
            TextSpan(text: to),
          ]),
          style: data,
        ),
      );
    }

    Widget row(IconData icon, String label, Widget value) => MergeSemantics(
          child: ListTile(contentPadding: EdgeInsets.zero, leading: Icon(icon), title: Text(label), subtitle: value),
        );
    final swapChanged = r.swapReclaimed || r.before.swapUsed != r.after.swapUsed;
    return Column(
      key: const ValueKey('done'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Semantics(
          header: true,
          liveRegion: true,
          child: Row(children: [
            Icon(Icons.check_circle_outline, color: StatusColors.of(context).success.color),
            const SizedBox(width: Space.sm),
            Expanded(child: Text(_headline(r), style: theme.textTheme.titleLarge)),
          ]),
        ),
        const SizedBox(height: Space.xs),
        Text('The cache fills up again as the server reads files.', style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
        const SizedBox(height: Space.sm),
        row(Icons.check_box_outline_blank, 'Free', change(r.before.free, r.after.free)),
        row(Icons.cached_outlined, 'Cache and buffers', change(r.before.cached, r.after.cached)),
        if (swapChanged) row(Icons.swap_vert, 'Swap in use', change(r.before.swapUsed, r.after.swapUsed)),
        if (r.note.isNotEmpty) ...[
          const SizedBox(height: Space.xs),
          Text(r.note, style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant)),
        ],
        const SizedBox(height: Space.lg),
        Align(
          alignment: AlignmentDirectional.centerEnd,
          child: FilledButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Done')),
        ),
      ],
    );
  }
}
