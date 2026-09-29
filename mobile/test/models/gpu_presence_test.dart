import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/gpu_stats.dart';

void main() {
  const idle = GpuStats(name: 'GTX 1080 Ti');

  test('a failed poll after a good one keeps the card and keeps polling', () {
    final p = GpuPresence();
    expect(p.record(idle, null), idle);
    for (var i = 0; i < 10; i++) {
      expect(p.record(null, idle), idle);
    }
    expect(p.worthPolling, isTrue);
  });

  test('a server with no GPU stops being polled after a few misses in a row', () {
    final p = GpuPresence();
    expect(p.record(null, null), isNull);
    expect(p.worthPolling, isTrue);
    p.record(null, null);
    p.record(null, null);
    expect(p.worthPolling, isFalse);
  });

  test('a good reading between misses starts the count again', () {
    final p = GpuPresence();
    p.record(null, null);
    p.record(null, null);
    expect(p.record(idle, null), idle);
    expect(p.worthPolling, isTrue);
  });

  test('pull-to-refresh asks again from scratch', () {
    final p = GpuPresence()
      ..record(null, null)
      ..record(null, null)
      ..record(null, null);
    expect(p.worthPolling, isFalse);
    p.reset();
    expect(p.worthPolling, isTrue);
  });

  test('0% utilisation is a reading like any other', () {
    expect(GpuStats.tryParse({'name': 'GTX 1080 Ti', 'utilization_percent': 0}), isNotNull);
    expect(GpuStats.tryParse({'name': 'GTX 1080 Ti', 'utilization_percent': 0, 'stale': true})!.stale, isTrue);
  });
}
