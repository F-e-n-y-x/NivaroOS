import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/speed/speed_engine.dart';
import 'package:nivaroos_mobile/services/speedtest_service.dart';

void main() {
  test('JSON server list: https hosts, "Sponsor (City)" names, nearest first', () {
    final list = parseOoklaJson('[{"host":"a.example:8080","name":"Delhi","sponsor":"Anonet"},{"host":"","name":"x"},{"host":"b.example","name":"Noida","sponsor":""}]');
    expect(list.map((t) => t.name), ['Anonet (Delhi)', 'Noida']);
    expect(list.first.ping.toString(), 'https://a.example:8080/hello');
    expect(list.first.download().toString(), 'https://a.example:8080/download?size=25000000');
    expect(parseOoklaJson('not json'), isEmpty);
  });

  test('XML server list: http host:port entries', () {
    final list = parseOoklaXml('<settings><servers>'
        '<server url="http://a.example:8080/speedtest/upload.php" name="Bangalore" sponsor="Sri Lakshmi" host="a.example:8080" />'
        '<server name="No host" /></servers></settings>');
    expect(list, hasLength(1));
    expect(list.single.name, 'Sri Lakshmi (Bangalore)');
    expect(list.single.upload.toString(), 'http://a.example:8080/upload');
  });

  test('trimmed mean drops the slowest 30% and fastest 10% of slices', () {
    // Ramp-up slices (10, 20, 30) and one burst (5000) don't count.
    expect(trimmedMean([10, 20, 30, 900, 940, 950, 960, 980, 1000, 5000]), closeTo((900 + 940 + 950 + 960 + 980 + 1000) / 6, 0.01));
    expect(trimmedMean(const []), 0);
  });

  test('home-network hosts: private, link-local and .local; not public names or Tailscale', () {
    for (final h in ['192.168.1.10', '10.0.0.5', '172.16.0.1', '172.31.255.1', '169.254.3.4', 'nas.local', 'fe80::1']) {
      expect(isHomeNetworkHost(h), isTrue, reason: h);
    }
    for (final h in ['nivaro.example.com', '172.32.0.1', '100.64.0.1', '8.8.8.8', '192.169.1.1', '']) {
      expect(isHomeNetworkHost(h), isFalse, reason: h);
    }
  });
}
