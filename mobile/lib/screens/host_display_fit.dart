import 'dart:math' as math;
import 'dart:ui' show Size;

/// A host screen size that keeps the phone's shape.
class FitSize {
  const FitSize(this.width, this.height, this.percent);
  final int width;
  final int height;

  /// Share of the phone screen's real pixels (100 = pixel for pixel).
  final int percent;

  @override
  bool operator ==(Object other) => other is FitSize && other.width == width && other.height == height && other.percent == percent;

  @override
  int get hashCode => Object.hash(width, height, percent);

  @override
  String toString() => '$width×$height ($percent%)';
}

/// Host screen sizes with the same shape as this phone's screen, held
/// sideways (a desktop is used in landscape): pixel for pixel, then 75%, 66%
/// and 50% of that - lower resolutions still fill the screen with no black
/// bars and stream faster over a slow link. Even numbers, within the range
/// the sidecar accepts (640×480 to 7680×4320).
List<FitSize> phoneFitSizes(Size physicalScreen, {List<int> percents = const [100, 75, 66, 50]}) {
  final long = math.max(physicalScreen.width, physicalScreen.height);
  final short = math.min(physicalScreen.width, physicalScreen.height);
  if (long <= 0 || short <= 0) return const [];
  int even(double v) {
    final n = v.round();
    return n - n % 2;
  }

  final out = <FitSize>[];
  for (final p in percents) {
    var w = even(long * p / 100);
    var h = even(short * p / 100);
    if (w > 7680 || h > 4320) {
      // Scale down to the largest size the sidecar allows, same shape.
      final k = math.min(7680 / w, 4320 / h);
      w = even(w * k);
      h = even(h * k);
    }
    if (w < 640 || h < 480) continue;
    if (out.any((o) => o.width == w && o.height == h)) continue;
    out.add(FitSize(w, h, p));
  }
  return out;
}
