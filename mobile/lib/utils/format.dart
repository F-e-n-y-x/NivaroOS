import 'dart:math' as math;

const _units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

/// "1.5 GB": [bytes] in binary units with [decimals] decimals (none for
/// bytes). Used across the app; keep its output stable.
String formatBytes(num bytes, {int decimals = 1}) {
  if (bytes <= 0) return '0 B';
  final i = (math.log(bytes) / math.log(1024)).floor().clamp(0, _units.length - 1);
  final value = bytes / math.pow(1024, i);
  return '${value.toStringAsFixed(i == 0 ? 0 : decimals)} ${_units[i]}';
}

/// A compact size for lists and progress: three significant figures at
/// most and no trailing ".0" ("4 KB", "2.4 MB", "183 GB", "1.5 TB").
String formatSize(num bytes) {
  if (bytes <= 0) return '0 B';
  final i = (math.log(bytes) / math.log(1024)).floor().clamp(0, _units.length - 1);
  final value = bytes / math.pow(1024, i);
  if (i == 0) return '${bytes.round()} B';
  // Rounding can reach the next unit's threshold ("1024 KB"); say it in
  // the bigger unit instead.
  if (value.round() >= 1024 && i < _units.length - 1) return '1 ${_units[i + 1]}';
  var text = value.toStringAsFixed(value >= 100 ? 0 : 1);
  if (text.endsWith('.0')) text = text.substring(0, text.length - 2);
  return '$text ${_units[i]}';
}

/// "3.2 MB/s".
String formatSpeed(num bytesPerSecond) => '${formatSize(bytesPerSecond)}/s';

/// "1 item" / "3 items".
String formatCount(int n, String singular, [String? plural]) => '$n ${n == 1 ? singular : (plural ?? '${singular}s')}';

/// "4:05", or "1:02:09" past an hour, for media positions.
String formatDuration(Duration d) {
  final negative = d.isNegative;
  final abs = d.abs();
  final h = abs.inHours;
  final m = abs.inMinutes.remainder(60);
  final s = abs.inSeconds.remainder(60).toString().padLeft(2, '0');
  final body = h > 0 ? '$h:${m.toString().padLeft(2, '0')}:$s' : '$m:$s';
  return negative ? '-$body' : body;
}
