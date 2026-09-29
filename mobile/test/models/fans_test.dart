import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/fans.dart';

Map<String, dynamic> _fixture(String p) => jsonDecode(File('test/screenshots/fixtures/$p.json').readAsStringSync()) as Map<String, dynamic>;

void main() {
  test('parses the server status (this box: NCT6793D + GTX 1080 Ti)', () {
    final s = FansStatus.fromJson(_fixture('v1/fans/status'));
    expect(s.controllable, isTrue);
    expect(s.fans, hasLength(6));
    final cpu = s.fan('hw-nct6793-nct6775-656-pwm2')!;
    expect(cpu.label, 'CPU fan');
    expect(cpu.mode, FanMode.curve);
    expect(cpu.state, FanState.manual);
    expect(cpu.reading, '1,480 RPM · 52%');
    expect(cpu.curve.first, const FanPoint(40, 30));
    final gpu = s.fans.last;
    expect(gpu.isGpu, isTrue);
    expect(gpu.rpm, isNull);
    expect(gpu.floorPct, 23);
    expect(gpu.reading, '27%');
    expect(s.sourceTemp('cpu'), 58.4);
    expect(s.sourceTemp('max'), 58.4);
    expect(s.criticalFor(gpu.source), 83);
    expect(s.limits.maxPoints, 8);
  });

  test('read-only status carries the ACPI note and no fans', () {
    final s = FansStatus.fromJson(_fixture('fans/readonly'));
    expect(s.controllable, isFalse);
    expect(s.notes.single.code, 'acpi_conflict');
    expect(s.notes.single.guidance, contains('acpi_enforce_resources=lax'));
  });

  test('missing fields fall back safely', () {
    final f = FanInfo.fromJson({'id': 'x', 'mode': 'turbo', 'state': '?'});
    expect(f.mode, FanMode.auto);
    expect(f.state, FanState.auto);
    expect(f.controllable, isFalse);
    expect(f.minPct, 20);
    expect(f.reading, '—');
  });

  const c = [FanPoint(40, 30), FanPoint(60, 50), FanPoint(80, 100)];

  test('evalCurve matches the server', () {
    expect(evalCurve(c, 20), 30);
    expect(evalCurve(c, 50), 40);
    expect(evalCurve(c, 70), 75);
    expect(evalCurve(c, 99), 100);
  });

  test('validateCurve refuses what the server refuses', () {
    expect(validateCurve(c, 20), isNull);
    expect(validateCurve(const [FanPoint(40, 30)], 20), contains('2 to 8'));
    expect(validateCurve(const [FanPoint(40, 60), FanPoint(60, 50)], 20), contains('slow the fan down'));
    expect(validateCurve(const [FanPoint(40, 10), FanPoint(60, 50)], 20), contains('20%'));
    expect(validateCurve(const [FanPoint(40, 30), FanPoint(40, 50)], 20), contains('rise'));
  });

  test('movePoint keeps every drag valid', () {
    var m = movePoint(c, 1, 95, 3, 20);
    expect(m[1], const FanPoint(79, 30));
    expect(validateCurve(m, 20), isNull);
    m = movePoint(c, 1, 55.4, 130, 20);
    expect(m[1], const FanPoint(55, 100));
    m = movePoint(c, 0, 0, 0, 25);
    expect(m[0], const FanPoint(20, 25));
    expect(c[1], const FanPoint(60, 50));
  });

  test('groupThousands', () {
    expect(groupThousands(2380), '2,380');
    expect(groupThousands(999), '999');
    expect(groupThousands(1234567), '1,234,567');
  });
}
