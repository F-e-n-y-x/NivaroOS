// Runs the app's speed engine from a desktop against a NivaroOS server:
//   dart run tool/speedbench.dart http://192.168.1.10
// (or `dart compile exe` it for release-like numbers). Checks the method,
// not the phone: on a wired LAN it should land near the link's speed.
import 'dart:io';

import 'package:nivaroos_mobile/services/speed/speed_engine.dart';

Future<void> main(List<String> args) async {
  if (args.isNotEmpty && args.first == 'internet') return internet();
  final base = Uri.parse(args.isEmpty ? 'http://127.0.0.1' : args.first);
  final e = SpeedEngine();
  final lat = await e.latency(base.replace(path: '/speedtest/ping'));
  stdout.writeln('ping ${lat?.pingMs.toStringAsFixed(2)} ms, jitter ${lat?.jitterMs.toStringAsFixed(2)} ms (${lat?.samples} samples)');
  final down = await e.download((_) => base.replace(path: '/speedtest/download'));
  stdout.writeln('download ${down?.toStringAsFixed(0)} Mbps');
  final up = await e.upload((_) => base.replace(path: '/speedtest/upload'));
  stdout.writeln('upload ${up?.toStringAsFixed(0)} Mbps');
}

// `speedbench internet`: the phone-internet test, run from this machine.
Future<void> internet() async {
  final pick = await nearestSpeedTarget();
  if (pick == null) {
    stdout.writeln('no speedtest.net server answered');
    return;
  }
  final e = SpeedEngine();
  final lat = await e.latency(pick.target.ping);
  stdout.writeln('${pick.target.name}: ping ${lat?.pingMs.toStringAsFixed(1)} ms, jitter ${lat?.jitterMs.toStringAsFixed(1)} ms');
  stdout.writeln('download ${(await e.download((_) => pick.target.download()))?.toStringAsFixed(0)} Mbps');
  stdout.writeln('upload ${(await e.upload((_) => pick.target.upload, bodyBytes: 25 * 1000 * 1000))?.toStringAsFixed(0)} Mbps');
}
