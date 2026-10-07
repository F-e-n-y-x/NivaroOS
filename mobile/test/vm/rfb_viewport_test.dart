import 'dart:ui';

import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/widgets/rfb_view.dart';

RfbViewport _vp(RfbFit fit, {Size box = const Size(412, 915), double dpr = 2.625}) =>
    RfbViewport()..layout(box: box, remote: const Size(1280, 720), dpr: dpr, fit: fit);

void main() {
  test('fit: the whole desktop, full width in portrait, centred, no extra bars', () {
    final v = _vp(RfbFit.fit);
    expect(v.rect.width, closeTo(412, 0.01));
    expect(v.rect.height, closeTo(412 * 720 / 1280, 0.01));
    expect(v.rect.center.dy, closeTo(915 / 2, 0.01));
    expect(v.pannable, isFalse);
  });

  test('fit in landscape: full height; fill crops to cover; 1:1 is one phone pixel each', () {
    final land = _vp(RfbFit.fit, box: const Size(915, 412));
    expect(land.rect.height, closeTo(412, 0.01));
    final fill = _vp(RfbFit.fill);
    expect(fill.rect.height, closeTo(915, 0.01));
    expect(fill.rect.width, greaterThan(412));
    expect(fill.pannable, isTrue);
    expect(fill.rect.center.dx, closeTo(206, 0.01), reason: 'starts centred');
    final actual = _vp(RfbFit.actual);
    expect(actual.scale * 2.625, closeTo(1, 1e-9));
  });

  test('pinch zoom keeps the point under the fingers, clamped to 1x-4x', () {
    final v = _vp(RfbFit.fit);
    const focal = Offset(100, 457.5);
    final before = v.toRemote(focal);
    v.zoomAt(focal, 2);
    expect(v.toRemote(focal).dx, closeTo(before.dx, 0.01));
    expect(v.toRemote(focal).dy, closeTo(before.dy, 0.01));
    v.zoomAt(focal, 10);
    expect(v.zoom, RfbViewport.maxZoom);
    v.zoomAt(focal, 0.2);
    expect(v.zoom, 1);
  });

  test('pan never leaves a gap; double-tap zoom toggles in and back out', () {
    final v = _vp(RfbFit.fit)..zoomAt(const Offset(206, 457), 2);
    v.panBy(const Offset(10000, 10000));
    expect(v.rect.left, 0);
    v.panBy(const Offset(-10000, 0));
    expect(v.rect.right, closeTo(412, 0.01));
    v.toggleZoom(const Offset(206, 457));
    expect(v.zoom, 1);
    v.toggleZoom(const Offset(206, 457));
    expect(v.zoom, 2.5);
  });

  test('toRemote and toLocal are inverses; toRemote clamps to the screen', () {
    final v = _vp(RfbFit.fit)..zoomAt(const Offset(50, 300), 3);
    const p = Offset(640, 360);
    expect((v.toRemote(v.toLocal(p)) - p).distance, lessThan(1e-6));
    expect(_vp(RfbFit.fit).toRemote(const Offset(-50, -50)), Offset.zero);
    expect(_vp(RfbFit.fit).toRemote(const Offset(900, 900)), const Offset(1279, 719));
  });

  test('reveal pans the least to keep a point inside; edge pan only near edges', () {
    final v = _vp(RfbFit.fit)..zoomAt(const Offset(206, 457), 3);
    final far = v.toLocal(const Offset(1000, 360));
    expect(far.dx, greaterThan(412));
    v.reveal(far);
    expect(v.toLocal(const Offset(1000, 360)).dx, lessThanOrEqualTo(412 - 48 + 0.01));
    expect(v.edgePan(const Offset(206, 457)), Offset.zero);
    expect(v.edgePan(const Offset(2, 457)).dx, greaterThan(0));
    expect(v.edgePan(const Offset(410, 457)).dx, lessThan(0));
  });

  test('the keyboard lift raises the caret above the keyboard, never more than it covers', () {
    final v = _vp(RfbFit.fit);
    expect(v.liftFor(const Offset(0, 300), 600), 0);
    expect(v.liftFor(const Offset(0, 700), 600), 132);
    expect(v.liftFor(const Offset(0, 914), 600), 315);
  });

  test('with the keyboard up the view only slides when the caret would leave sight, and only as far as needed', () {
    final v = _vp(RfbFit.fit); // 412 x 915 box, keyboard top at 600
    // Caret well inside the visible band: no slide, and small moves keep it.
    expect(v.keepInSight(0, const Offset(0, 300), 600), 0);
    expect(v.keepInSight(0, const Offset(0, 500), 600), 0);
    // Caret moves under the keyboard: slide just enough (32 margin).
    final lift = v.keepInSight(0, const Offset(0, 700), 600);
    expect(lift, 132);
    // Moving back up inside the band keeps that slide - the screen no
    // longer races along with every pointer move.
    expect(v.keepInSight(lift, const Offset(0, 650), 600), lift);
    expect(v.keepInSight(lift, const Offset(0, 400), 600), lift);
    // Up past the top margin of what's visible: slides back down.
    expect(v.keepInSight(lift, const Offset(0, 140), 600), 108);
    // Never more than the keyboard covers.
    expect(v.keepInSight(0, const Offset(0, 914), 600), 315);
  });

  test('a new fit starts unzoomed; a resize keeps the centre point', () {
    final v = _vp(RfbFit.fit)..zoomAt(const Offset(300, 457), 2);
    final centre = v.toRemote(const Offset(206, 457.5));
    v.layout(box: const Size(412, 900), remote: const Size(1280, 720), dpr: 2.625);
    expect((v.toRemote(const Offset(206, 450)) - centre).distance, lessThan(1));
    v.layout(box: const Size(412, 900), remote: const Size(1280, 720), dpr: 2.625, fit: RfbFit.fill);
    expect(v.zoom, 1);
  });

  test('the drawn pointer follows the zoom, within readable limits', () {
    expect(cursorSizeFor(0.12), const Size(8, 12)); // a 3392 px desktop fitted to a phone
    expect(cursorSizeFor(1).width, 12);
    expect(cursorSizeFor(4), const Size(13, 19.5));
  });
}
