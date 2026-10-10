// Golden comparison for the screenshot tests: exact, except that a channel
// off by one passes. Skia's shadow cache reuses a tessellation drawn at
// another offset once enough shots have run, so a shadow edge can come out
// one level lighter in a batch than alone (server_profiles 360x740: 4
// pixels, each one channel off by 1). Real changes differ by far more.
import 'dart:async';
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';

Future<void> testExecutable(FutureOr<void> Function() testMain) async {
  final base = goldenFileComparator;
  if (base is LocalFileComparator) {
    goldenFileComparator = _RoundingTolerantComparator(base.basedir.resolve('flutter_test_config.dart'));
  }
  await testMain();
}

class _RoundingTolerantComparator extends LocalFileComparator {
  _RoundingTolerantComparator(super.testFile);

  @override
  Future<bool> compare(Uint8List imageBytes, Uri golden) async {
    final goldenBytes = await getGoldenBytes(golden);
    final result = await GoldenFileComparator.compareLists(imageBytes, goldenBytes);
    if (result.passed || await _offByOneAtMost(imageBytes, goldenBytes)) {
      result.dispose();
      return true;
    }
    final error = await generateFailureOutput(result, golden, basedir);
    result.dispose();
    throw FlutterError(error);
  }
}

Future<bool> _offByOneAtMost(Uint8List a, List<int> b) async {
  final ra = await _rgba(a);
  final rb = await _rgba(Uint8List.fromList(b));
  if (ra == null || rb == null || ra.lengthInBytes != rb.lengthInBytes) return false;
  final x = ra.buffer.asUint8List(), y = rb.buffer.asUint8List();
  for (var i = 0; i < x.length; i++) {
    if ((x[i] - y[i]).abs() > 1) return false;
  }
  return true;
}

Future<ByteData?> _rgba(Uint8List png) async {
  final codec = await ui.instantiateImageCodec(png);
  final frame = await codec.getNextFrame();
  final data = await frame.image.toByteData();
  frame.image.dispose();
  codec.dispose();
  return data;
}
