import 'dart:math' as math;

String formatBytes(num bytes, {int decimals = 1}) {
  if (bytes <= 0) return '0 B';
  const suffixes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  final i = (math.log(bytes) / math.log(1024)).floor().clamp(0, suffixes.length - 1);
  final value = bytes / math.pow(1024, i);
  return '${value.toStringAsFixed(i == 0 ? 0 : decimals)} ${suffixes[i]}';
}
