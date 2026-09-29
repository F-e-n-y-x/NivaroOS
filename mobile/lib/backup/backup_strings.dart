// The Backup & Sync texts, in the web UI's words.
//
// The backup service sends i18n keys and args, never English (backup spec
// §12.12): a run summary is {"key": "backup.run.summary.ok", "args":
// {"added": 4120, ...}}. The web renders them from its en_US.json
// (ui/src/apps/backup/messages.js); the app renders the same keys from a
// copy of those texts (backup_strings.g.dart, made by
// tool/gen_backup_strings.py), so a run ends with the same words on the
// phone as on the web.
import 'package:clock/clock.dart';
import 'package:intl/intl.dart';

import '../utils/format.dart';

part 'backup_strings.g.dart';

/// The text for [key] with `{name}` placeholders filled from [args] as
/// they are (format them first; [renderArgs] does it for server args). A
/// key the app doesn't know comes back as itself, as vue-i18n does.
String bt(String key, [Map<String, Object?> args = const {}]) => _fill(_en[key] ?? key, args);

/// Plural texts ("{count} job | {count} jobs"): the first form for 1,
/// the last for anything else. With three forms the first is for 0, as
/// vue-i18n's $tc picks them.
String btc(String key, int n, [Map<String, Object?> args = const {}]) {
  final forms = (_en[key] ?? key).split(' | ');
  final String form;
  if (forms.length >= 3) {
    form = n == 0 ? forms[0] : (n == 1 ? forms[1] : forms[2]);
  } else if (forms.length == 2) {
    form = n == 1 ? forms[0] : forms[1];
  } else {
    form = forms[0];
  }
  return _fill(form, {'count': formatNumber(n), 'n': formatNumber(n), ...args});
}

/// Whether the texts have [key] (an error code the web knows, say).
bool btHas(String key) => _en.containsKey(key);

String _fill(String text, Map<String, Object?> args) {
  if (args.isEmpty || !text.contains('{')) return text;
  return text.replaceAllMapped(RegExp(r'\{(\w+)\}'), (m) {
    final v = args[m[1]];
    return v == null ? '' : v.toString();
  });
}

final _number = NumberFormat.decimalPattern('en');

/// "4,120".
String formatNumber(num n) => _number.format(n.round());

final _iso = RegExp(r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}');

/// "Today 3:00 AM", "Tomorrow 2:00 AM", "Yesterday 9:12 PM", else
/// "Mon, Sep 22 3:00 AM" - the web's fmt.when, in the phone's time.
String formatWhen(DateTime t) {
  final local = t.toLocal();
  final now = clock.now();
  final days = DateTime(local.year, local.month, local.day).difference(DateTime(now.year, now.month, now.day)).inDays;
  final time = DateFormat.jm().format(local);
  return switch (days) {
    0 => bt('backup.time.today_at', {'time': time}),
    1 => bt('backup.time.tomorrow_at', {'time': time}),
    -1 => bt('backup.time.yesterday_at', {'time': time}),
    _ => formatDateTime(local),
  };
}

/// "Mon, Sep 22 3:00 AM" always (the web's fmt.dateTime), for labels
/// that read "on" a date.
String formatDateTime(DateTime t) => DateFormat('EEE, MMM d').add_jm().format(t.toLocal());

/// "Immich and Blinko", "a, b and c".
String formatList(List<Object?> items) {
  final s = items.map((e) => e.toString()).toList();
  if (s.isEmpty) return '';
  if (s.length == 1) return s.first;
  return '${s.sublist(0, s.length - 1).join(', ')} and ${s.last}';
}

/// "40 sec", "6 min", "1 hr 5 min", "3 days" - the web's short units.
String formatSeconds(num seconds) {
  final s = seconds.round().clamp(0, 1 << 31);
  if (s < 60) return '$s sec';
  final m = (s / 60).round();
  if (m < 60) return '$m min';
  final h = m ~/ 60;
  final rest = m % 60;
  if (h >= 48) return '${(h / 24).round()} days';
  return rest == 0 ? '$h hr' : '$h hr $rest min';
}

/// Formats one server arg the way the web does (messages.js formatArg):
/// `*_key` is another text rendered with the same args, lists are joined,
/// sizes and speeds are sizes, other numbers are grouped, ISO times are
/// dates.
Object? _formatArg(String name, Object? value, Map<String, Object?> raw) {
  if (value == null) return '';
  if (name.endsWith('_key') && value is String) return value.startsWith('backup.') ? bt(value, renderArgs(raw, nested: true)) : value;
  if (value is List) return formatList(value);
  if (value is num) {
    if (name == 'bytes' || name.endsWith('_bytes') || name.startsWith('bytes_')) return formatSize(value);
    if (name == 'speed_bps') return formatSpeed(value);
    return formatNumber(value);
  }
  if (value is String && _iso.hasMatch(value)) {
    final t = DateTime.tryParse(value);
    return t == null ? value : formatDateTime(t);
  }
  return value.toString();
}

/// Every arg of a server message, formatted for [bt].
Map<String, Object?> renderArgs(Map<String, Object?> args, {bool nested = false}) => {
      for (final e in args.entries)
        // A nested key (reason_key inside a summary) gets the plain args,
        // so it can't recurse forever.
        e.key: nested && e.key.endsWith('_key') ? e.value : _formatArg(e.key, e.value, args),
    };

/// A server {key, args} message as text; '' for none.
String renderMessage(String? key, Map<String, Object?> args) {
  if (key == null || key.isEmpty) return '';
  return bt(key, renderArgs(args));
}
