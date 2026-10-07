import 'dart:math' as math;
import 'dart:ui';

/// How the remote screen sits on the phone's before any pinch zoom.
enum RfbFit {
  /// All of it, as large as fits: no bars beyond the shape difference.
  fit('Fit to screen'),

  /// The phone's screen filled, the overflow cropped (pan to see it).
  fill('Fill screen'),

  /// One remote pixel per phone pixel.
  actual('1:1 pixels'),

  /// The remote screen resized to this phone's shape, then fitted.
  matchPhone('Match this phone');

  const RfbFit(this.label);
  final String label;
}

/// The remote screen's place in the view: a base scale from [fit], a pinch
/// [zoom] on top (1x to [maxZoom]) and the picture's top-left [offset], kept
/// so the picture never leaves a gap it doesn't need (a picture smaller
/// than the view on an axis is centred on it).
class RfbViewport {
  RfbViewport({this.fit = RfbFit.fit});

  static const maxZoom = 4.0;

  RfbFit fit;
  Size box = Size.zero;
  Size remote = const Size(1280, 720);
  double dpr = 1;
  double zoom = 1;
  Offset offset = Offset.zero;

  double get baseScale {
    if (box.isEmpty || remote.isEmpty) return 1;
    final sx = box.width / remote.width, sy = box.height / remote.height;
    return switch (fit) {
      RfbFit.fill => math.max(sx, sy),
      RfbFit.actual => 1 / dpr,
      RfbFit.fit || RfbFit.matchPhone => math.min(sx, sy),
    };
  }

  double get scale => baseScale * zoom;

  /// Where the picture is drawn, in view coordinates.
  Rect get rect => offset & remote * scale;

  /// Whether there is more picture than view on some axis.
  bool get pannable => rect.width > box.width + 0.5 || rect.height > box.height + 0.5;

  /// Takes a new view size, remote size, pixel ratio or fit. The remote
  /// point at the view's centre stays there; a new fit starts unzoomed.
  void layout({required Size box, required Size remote, double dpr = 1, RfbFit? fit}) {
    final newFit = fit ?? this.fit;
    if (box == this.box && remote == this.remote && dpr == this.dpr && newFit == this.fit) return;
    final centre = this.box.isEmpty ? null : toRemote(this.box.center(Offset.zero));
    final sameRemote = remote == this.remote;
    this.box = box;
    this.remote = remote;
    this.dpr = dpr;
    if (newFit != this.fit) {
      this.fit = newFit;
      zoom = 1;
    }
    final keep = centre != null && sameRemote ? centre : remote.center(Offset.zero);
    offset = _clamp(box.center(Offset.zero) - keep * scale);
  }

  Offset _clamp(Offset o) {
    final shown = remote * scale;
    double axis(double v, double size, double room) => size <= room ? (room - size) / 2 : v.clamp(room - size, 0.0);
    return Offset(axis(o.dx, shown.width, box.width), axis(o.dy, shown.height, box.height));
  }

  /// The remote pixel under [local], clamped to the remote screen.
  Offset toRemote(Offset local) {
    final p = (local - offset) / scale;
    return Offset(p.dx.clamp(0.0, math.max(0.0, remote.width - 1)), p.dy.clamp(0.0, math.max(0.0, remote.height - 1)));
  }

  Offset toLocal(Offset remotePoint) => offset + remotePoint * scale;

  /// Zooms to [z] keeping the remote point under [focal] where it is.
  void zoomAt(Offset focal, double z) {
    final p = (focal - offset) / scale;
    zoom = z.clamp(1.0, maxZoom);
    offset = _clamp(focal - p * scale);
  }

  /// Double-tap zoom: in to 2.5x around [focal], or back out to 1x.
  void toggleZoom(Offset focal) => zoomAt(focal, zoom > 1.01 ? 1 : 2.5);

  void panBy(Offset delta) => offset = _clamp(offset + delta);

  /// Pans the least so [local] is at least [margin] inside the view.
  void reveal(Offset local, {double margin = 48}) {
    double axis(double v, double room) {
      final m = math.min(margin, room / 2);
      if (v < m) return m - v;
      if (v > room - m) return room - m - v;
      return 0;
    }

    panBy(Offset(axis(local.dx, box.width), axis(local.dy, box.height)));
  }

  /// How far to slide the whole view up so [caret] (a view point) sits
  /// [margin] above [visibleBottom] (the top of the keyboard), never
  /// more than the part the keyboard covers.
  double liftFor(Offset caret, double visibleBottom, {double margin = 32}) {
    final covered = math.max(0.0, box.height - visibleBottom);
    return (caret.dy + margin - visibleBottom).clamp(0.0, covered);
  }

  /// The slide to keep [caret] (a view point, before sliding) in sight above
  /// [visibleBottom], starting from the [current] slide and changing it only
  /// when the caret would leave the visible band - then by just enough.
  /// [liftFor] recomputed from the caret moved the whole view with every
  /// pointer move while the keyboard was up, so the screen raced along
  /// with the cursor.
  double keepInSight(double current, Offset caret, double visibleBottom, {double margin = 32}) {
    final covered = math.max(0.0, box.height - visibleBottom);
    var lift = current.clamp(0.0, covered);
    final onScreen = caret.dy - lift;
    if (onScreen > visibleBottom - margin) lift += onScreen - (visibleBottom - margin);
    if (onScreen < margin) lift -= margin - onScreen;
    return lift.clamp(0.0, covered);
  }

  /// While a drag is held near an edge: the pan for one frame (about
  /// 16 ms), faster the closer to the edge, zero away from the edges.
  Offset edgePan(Offset local, {double edge = 40, double speed = 14}) {
    double axis(double v, double room) {
      if (v < edge) return speed * (1 - v.clamp(0.0, edge) / edge);
      if (v > room - edge) return -speed * (1 - (room - v).clamp(0.0, edge) / edge);
      return 0;
    }

    return Offset(axis(local.dx, box.width), axis(local.dy, box.height));
  }
}
