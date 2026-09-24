import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

/// "2 min ago", "Yesterday", "Sep 12" for [time] relative to [now].
///
/// [now] defaults to `clock.now()` (package:clock) rather than
/// `DateTime.now()`, so tests and screenshots can freeze the time with
/// `withClock`. Anything the app shows relative to the current time should
/// read it the same way.
String formatRelative(DateTime time, {DateTime? now}) {
  final current = now ?? clock.now();
  final local = time.toLocal();
  final diff = current.difference(local);
  if (diff.inSeconds < 60) return 'Just now';
  if (diff.inMinutes < 60) return '${diff.inMinutes} min ago';
  final today = DateTime(current.year, current.month, current.day);
  final day = DateTime(local.year, local.month, local.day);
  final days = today.difference(day).inDays;
  if (days == 0) return '${diff.inHours} h ago';
  if (days == 1) return 'Yesterday';
  if (days < 7) return '$days days ago';
  if (local.year == current.year) return DateFormat.MMMd().format(local);
  return DateFormat.yMMMd().format(local);
}

/// The exact date and time, as shown on long-press: "Sep 25, 2026 2:03 PM".
String formatExact(DateTime time) => DateFormat.yMMMd().add_jm().format(time.toLocal());

/// A relative timestamp that keeps itself current (once a minute) and shows
/// the exact time on long-press.
class RelativeTime extends StatefulWidget {
  const RelativeTime(this.time, {super.key, this.prefix, this.style});

  final DateTime time;

  /// Text before the time, e.g. "Updated " - part of the same label so
  /// TalkBack reads it as one phrase.
  final String? prefix;
  final TextStyle? style;

  @override
  State<RelativeTime> createState() => _RelativeTimeState();
}

class _RelativeTimeState extends State<RelativeTime> {
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _timer = Timer.periodic(const Duration(minutes: 1), (_) => setState(() {}));
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Tooltip(
      message: formatExact(widget.time),
      triggerMode: TooltipTriggerMode.longPress,
      child: Text('${widget.prefix ?? ''}${formatRelative(widget.time)}', style: widget.style),
    );
  }
}
