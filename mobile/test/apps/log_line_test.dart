import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/container_logs_screen.dart';

void main() {
  test('splits Docker nanosecond timestamps off and detects levels', () {
    final l = LogLine.parse('2026-09-25T05:52:10.004123456Z [ERR] Could not find file');
    expect(l.time, DateTime.utc(2026, 9, 25, 5, 52, 10, 4, 123));
    expect(l.text, '[ERR] Could not find file');
    expect(l.level, LogLevel.error);
    expect(LogLine.parse('[WRN] slow client').level, LogLevel.warning);
    expect(LogLine.parse('Terror of the deep').level, LogLevel.normal, reason: 'whole words only');
    expect(LogLine.parse('plain').time, isNull);
  });

  test('parseAll drops the trailing empty line', () {
    expect(LogLine.parseAll('a\nb\n').map((l) => l.text), ['a', 'b']);
    expect(LogLine.parseAll(''), isEmpty);
  });
}
