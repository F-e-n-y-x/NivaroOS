// The compressed picture encodings: Zlib (the host desktop's Wayland
// server), ZRLE and Tight (QEMU, x11vnc), with golden pixels, zlib streams
// that carry across rectangles, hostile input, and the bytes they save.
import 'dart:convert';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/rfb_client.dart';

import 'vm_fakes.dart';

const _w = 64, _h = 36;

/// A zlib stream the way a VNC server keeps one: sync-flushed per rect.
class _Deflater {
  final _z = RawZLibFilter.deflateFilter(level: 6);
  List<int> call(List<int> data) {
    _z.process(data, 0, data.length);
    final out = <int>[];
    for (List<int>? c; (c = _z.processed(flush: true)) != null;) {
      out.addAll(c!);
    }
    return out;
  }
}

List<int> _u16(int v) => [v >> 8, v & 0xff];
List<int> _u32(int v) => (ByteData(4)..setUint32(0, v)).buffer.asUint8List();
List<int> _rectHeader(int x, int y, int w, int h, int enc) =>
    [..._u16(x), ..._u16(y), ..._u16(w), ..._u16(h), ...(ByteData(4)..setInt32(0, enc)).buffer.asUint8List()];
List<int> _update(List<List<int>> rects) => [0, 0, ..._u16(rects.length), for (final r in rects) ...r];
List<int> _compact(int n) => n < 0x80
    ? [n]
    : n < 0x4000
        ? [n & 0x7f | 0x80, n >> 7]
        : [n & 0x7f | 0x80, (n >> 7) & 0x7f | 0x80, n >> 14];

/// The pixels of [rgba] as Tight's R, G, B and Raw's B, G, R, padding.
List<int> _rgb(Uint8List rgba) => [for (var i = 0; i < rgba.length; i += 4) ...[rgba[i], rgba[i + 1], rgba[i + 2]]];
List<int> _bgrx(Uint8List rgba) => [for (var i = 0; i < rgba.length; i += 4) ...[rgba[i + 2], rgba[i + 1], rgba[i], 0]];

/// RGBA of a [w]×[h] block of one colour.
Uint8List _solid(int w, int h, int r, int g, int b) =>
    Uint8List.fromList([for (var i = 0; i < w * h; i++) ...[r, g, b, 255]]);

/// ZRLE tiles for [rgba]: solid tiles and plain RLE, like a real encoder
/// does for flat desktop content.
List<int> _zrleTiles(Uint8List rgba, int w, int h) {
  final out = <int>[];
  for (var ty = 0; ty < h; ty += 64) {
    for (var tx = 0; tx < w; tx += 64) {
      final px = <int>[];
      for (var y = ty; y < min(ty + 64, h); y++) {
        for (var x = tx; x < min(tx + 64, w); x++) {
          final i = (y * w + x) * 4;
          px.add(rgba[i] << 16 | rgba[i + 1] << 8 | rgba[i + 2]);
        }
      }
      List<int> c(int p) => [p & 0xff, (p >> 8) & 0xff, p >> 16];
      if (px.every((p) => p == px.first)) {
        out.addAll([1, ...c(px.first)]);
        continue;
      }
      out.add(128);
      for (var i = 0; i < px.length;) {
        var j = i;
        while (j < px.length && px[j] == px[i]) {
          j++;
        }
        out.addAll(c(px[i]));
        var len = j - i - 1;
        for (; len >= 255; len -= 255) {
          out.add(255);
        }
        out.add(len);
        i = j;
      }
    }
  }
  return out;
}

/// The client's screen as RGBA rows, for comparing with goldens.
Uint8List _region(RfbClient c, int x, int y, int w, int h) {
  final fb = c.pixels!;
  return Uint8List.fromList([
    for (var row = y; row < y + h; row++) ...fb.sublist((row * c.width + x) * 4, (row * c.width + x + w) * 4),
  ]);
}

Future<void> _settle() => Future<void>.delayed(const Duration(milliseconds: 30));

/// Runs [body] against a connected client of a [_w]×[_h] fake server.
void _session(String name, Future<void> Function(FakeRfbServer server, RfbClient client) body) {
  testWidgets(name, (tester) async {
    await tester.runAsync(() async {
      final server = FakeRfbServer(width: _w, height: _h, sendFrame: false);
      final client = RfbClient(vmName: 'mint', session: FakeSession(currentToken: 'tok'), channelFactory: server.factory);
      await client.connect();
      expect(client.status.value, RfbStatus.connected);
      await body(server, client);
      client.dispose();
    });
  });
}

final _desktop = fakeDesktopRgba(_w, _h);

void main() {
  group('SetEncodings', () {
    List<int> encodings(FakeRfbServer s) {
      final set = s.messages(2).last;
      return [for (var i = 0; i < ((set[2] << 8) | set[3]); i++) ByteData.sublistView(set, 4 + i * 4).getInt32(0)];
    }

    _session('prefers Tight, then ZRLE, then Zlib, with Raw to fall back on', (server, client) async {
      final e = encodings(server);
      expect(e.sublist(0, 5), [1, 7, 16, 6, 0]);
      expect(e.where((v) => v <= -23 && v >= -32), isEmpty, reason: 'lossless unless asked');
    });

    _session('a quality choice asks for JPEG (and, saving data, the strongest zlib) at once', (server, client) async {
      client.jpegQuality = 2;
      expect(encodings(server), containsAll([-32 + 2, -256 + 9]));
      client.jpegQuality = 7;
      expect(encodings(server), contains(-32 + 7));
      expect(encodings(server), isNot(contains(-256 + 9)));
      final sent = server.messages(2).length;
      client.jpegQuality = 7;
      expect(server.messages(2).length, sent, reason: 'no change, no message');
    });
  });

  group('Zlib (the Wayland host desktop)', () {
    _session('decodes rects that share one zlib stream', (server, client) async {
      final z = _Deflater();
      final top = Uint8List.sublistView(_desktop, 0, _w * 18 * 4);
      final bottom = Uint8List.sublistView(_desktop, _w * 18 * 4);
      final a = z(_bgrx(top));
      final b = z(_bgrx(bottom)); // only inflates with the first rect's dictionary
      server.send(_update([
        [..._rectHeader(0, 0, _w, 18, 6), ..._u32(a.length), ...a],
        [..._rectHeader(0, 18, _w, 18, 6), ..._u32(b.length), ...b],
      ]));
      await _settle();
      expect(client.pixels, _desktop);
      // And the stream carries on into the next update.
      final c = z(_bgrx(_solid(4, 4, 9, 8, 7)));
      server.send(_update([[..._rectHeader(10, 10, 4, 4, 6), ..._u32(c.length), ...c]]));
      await _settle();
      expect(_region(client, 10, 10, 4, 4), _solid(4, 4, 9, 8, 7));
    });
  });

  group('ZRLE', () {
    _session('solid and RLE tiles, across updates on one stream', (server, client) async {
      final z = _Deflater();
      final a = z(_zrleTiles(_desktop, _w, _h));
      server.send(_update([[..._rectHeader(0, 0, _w, _h, 16), ..._u32(a.length), ...a]]));
      await _settle();
      expect(client.pixels, _desktop);
      final patch = _solid(6, 3, 1, 2, 3);
      final b = z(_zrleTiles(patch, 6, 3));
      server.send(_update([[..._rectHeader(58, 33, 6, 3, 16), ..._u32(b.length), ...b]]));
      await _settle();
      expect(_region(client, 58, 33, 6, 3), patch);
    });

    test('raw, packed palette and palette RLE tiles', () {
      // 4×2 tiles. CPIXEL is B, G, R.
      const red = [0, 0, 255], green = [0, 255, 0], blue = [255, 0, 0];
      const r = [255, 0, 0, 255], g = [0, 255, 0, 255], b = [0, 0, 255, 255];
      Uint8List rgba(List<List<int>> px) => Uint8List.fromList(px.expand((p) => p).toList());
      Uint8List decode(List<int> tile) => RfbClient.decodeZrle(4, 2, Uint8List.fromList(tile)).buffer.asUint8List();

      expect(decode([0, for (var i = 0; i < 8; i++) ...(i.isEven ? red : blue)]), rgba([for (var i = 0; i < 8; i++) i.isEven ? r : b]));
      // Two colours, 1 bit each, rows padded to a byte: 0101 / 1010.
      expect(decode([2, ...red, ...green, 0x50, 0xa0]), rgba([r, g, r, g, g, r, g, r]));
      // Three colours, 2 bits: 0 1 2 0 / 2 2 1 1.
      expect(decode([3, ...red, ...green, ...blue, 0x18, 0xa5]), rgba([r, g, b, r, b, b, g, g]));
      // Palette RLE: red ×5, a single green, blue ×2.
      expect(decode([130 + 1, ...red, ...green, ...blue, 0x80, 4, 1, 0x82, 1]), rgba([r, r, r, r, r, g, b, b]));
    });

    test('hostile tiles throw an RfbException, never a RangeError', () {
      final bad = <List<int>>[
        [],
        [0, 1, 2], // raw tile cut short
        [2, 0, 0, 0, 0, 0, 0], // packed palette with no index bytes
        [3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff], // index 3 of a 3-colour palette
        [128, 0, 0, 0, 255, 255, 255, 0], // a run longer than the tile
        [130, 0, 0, 0, 0, 0, 0, 0x85, 0], // palette index beyond the palette
        [17], // unused subencodings
        [129],
      ];
      for (final tile in bad) {
        expect(() => RfbClient.decodeZrle(4, 2, Uint8List.fromList(tile)), throwsA(isA<RfbException>()), reason: '$tile');
      }
      final rnd = Random(42);
      for (var i = 0; i < 3000; i++) {
        final data = Uint8List.fromList(List.generate(rnd.nextInt(200), (_) => rnd.nextInt(256)));
        try {
          RfbClient.decodeZrle(1 + rnd.nextInt(80), 1 + rnd.nextInt(80), data);
        } on RfbException {
          // expected for most garbage
        }
      }
    });
  });

  group('Tight', () {
    _session('fill, copy on zlib streams, palettes, gradient and a stream reset', (server, client) async {
      final z0 = _Deflater(), z1 = _Deflater();
      final a = z0(_rgb(_desktop));
      final copy = [..._rectHeader(0, 0, _w, _h, 7), 0x00, ..._compact(a.length), ...a];
      final fill = [..._rectHeader(0, 0, 8, 2, 7), 0x80, 10, 20, 30];
      // Two colours on stream 1, explicit palette filter: 1 bit per pixel.
      final bits = z1(List.filled(8 * 2, 0xf0)); // 16 wide, 2 bytes a row: 4 set, 4 clear, twice
      final pal2 = [..._rectHeader(0, 4, 16, 8, 7), 0x50, 1, 1, 255, 0, 0, 0, 0, 255, ..._compact(bits.length), ...bits];
      // Three colours, under 12 bytes of data: sent raw, no zlib.
      final pal3 = [..._rectHeader(20, 4, 3, 2, 7), 0x40, 1, 2, 1, 1, 1, 2, 2, 2, 3, 3, 3, 0, 1, 2, 2, 1, 0];
      server.send(_update([copy, fill, pal2, pal3]));
      await _settle();
      expect(_region(client, 0, 0, 8, 2), _solid(8, 2, 10, 20, 30));
      expect(_region(client, 8, 0, _w - 8, 4), Uint8List.fromList([
        for (var y = 0; y < 4; y++) ..._desktop.sublist((y * _w + 8) * 4, (y * _w + _w) * 4),
      ]));
      expect(_region(client, 0, 4, 4, 8), _solid(4, 8, 0, 0, 255), reason: 'bit set: the second colour');
      expect(_region(client, 4, 4, 4, 8), _solid(4, 8, 255, 0, 0));
      expect(_region(client, 8, 11, 4, 1), _solid(4, 1, 0, 0, 255));
      expect(_region(client, 20, 4, 3, 2), Uint8List.fromList([1, 1, 1, 255, 2, 2, 2, 255, 3, 3, 3, 255, 3, 3, 3, 255, 2, 2, 2, 255, 1, 1, 1, 255]));

      // Gradient on stream 0 after a reset of stream 0 (bit 0): a fresh deflater.
      final fresh = _Deflater();
      const gw = 4, gh = 3;
      final want = Uint8List.fromList([for (var y = 0; y < gh; y++) for (var x = 0; x < gw; x++) ...[x * 40, y * 50, 100 + x + y]]);
      final residual = Uint8List(want.length);
      for (var y = 0; y < gh; y++) {
        for (var x = 0; x < gw; x++) {
          for (var k = 0; k < 3; k++) {
            final i = (y * gw + x) * 3 + k;
            final left = x > 0 ? want[i - 3] : 0, up = y > 0 ? want[i - gw * 3] : 0;
            final ul = x > 0 && y > 0 ? want[i - gw * 3 - 3] : 0;
            residual[i] = want[i] - (left + up - ul).clamp(0, 255);
          }
        }
      }
      final g = fresh(residual);
      server.send(_update([[..._rectHeader(30, 20, gw, gh, 7), 0x41, 2, ..._compact(g.length), ...g]]));
      await _settle();
      expect(_region(client, 30, 20, gw, gh), Uint8List.fromList([
        for (var i = 0; i < want.length; i += 3) ...[want[i], want[i + 1], want[i + 2], 255],
      ]));
    });

    _session('JPEG through the engine codec', (server, client) async {
      final jpeg = base64.decode(_jpeg16x8);
      server.send(_update([[..._rectHeader(2, 3, 16, 8, 7), 0x90, ..._compact(jpeg.length), ...jpeg]]));
      // The engine decodes off the thread; wait for the picture to land.
      for (var i = 0; i < 100 && client.pixels![(3 * _w + 2) * 4 + 3] == 0; i++) {
        await _settle();
      }
      expect(client.status.value, RfbStatus.connected);
      final px = _region(client, 2, 3, 16, 8);
      for (final (x, want) in [(1, [220, 30, 30]), (14, [30, 30, 220])]) {
        final i = (4 * 16 + x) * 4;
        for (var k = 0; k < 3; k++) {
          expect((px[i + k] - want[k]).abs(), lessThan(12), reason: 'pixel $x channel $k');
        }
      }
    });

    test('hostile basic data throws an RfbException', () {
      expect(() => RfbClient.decodeTightBasic(4, 2, 1, [1, 2, 3], Uint8List.fromList([0, 1, 2, 3, 0, 0, 0, 0])),
          throwsA(isA<RfbException>()), reason: 'index past the palette');
      expect(() => RfbClient.decodeTightBasic(4, 2, 0, const [], Uint8List(5)), throwsA(isA<RfbException>()));
      expect(() => RfbClient.decodeTightBasic(9, 2, 1, [1, 2], Uint8List(2)), throwsA(isA<RfbException>()),
          reason: '9 wide is 2 bytes a row');
    });
  });

  group('hostile servers end the session with a message, never a crash', () {
    final cases = <String, List<int> Function()>{
      'a rect outside the screen': () => [..._rectHeader(60, 0, 10, 10, 16), ..._u32(0)],
      'a compressed length bigger than the rect could need': () => [..._rectHeader(0, 0, 2, 2, 6), ..._u32(1 << 30)],
      'not zlib at all': () => [..._rectHeader(0, 0, 2, 2, 6), ..._u32(4), 1, 2, 3, 4],
      'a zlib bomb': () {
        final bomb = _Deflater()(Uint8List(1 << 20));
        return [..._rectHeader(0, 0, _w, _h, 16), ..._u32(bomb.length), ...bomb];
      },
      'a Zlib rect that inflates short': () {
        final short = _Deflater()(Uint8List(8));
        return [..._rectHeader(0, 0, 2, 2, 6), ..._u32(short.length), ...short];
      },
      'an unknown Tight type': () => [..._rectHeader(0, 0, 2, 2, 7), 0xa0],
      'an unknown Tight filter': () => [..._rectHeader(0, 0, 2, 2, 7), 0x40, 9],
      'a JPEG of the wrong size': () {
        final jpeg = base64.decode(_jpeg16x8);
        return [..._rectHeader(0, 0, 8, 8, 7), 0x90, ..._compact(jpeg.length), ...jpeg];
      },
      'a JPEG that is not one': () => [..._rectHeader(0, 0, 8, 8, 7), 0x90, 4, 1, 2, 3, 4],
    };
    for (final MapEntry(key: name, value: rect) in cases.entries) {
      _session(name, (server, client) async {
        server.send(_update([rect()]));
        await _settle();
        expect(client.status.value, RfbStatus.disconnected);
        expect(client.error.value, contains('The console'));
      });
    }
  });

  test('bytes over the wire for a 1280×720 desktop frame', () {
    const w = 1280, h = 720;
    final flat = fakeDesktopRgba(w, h);
    // The same desktop with a 480×270 photo-like patch (smooth gradient plus noise).
    final busy = Uint8List.fromList(flat);
    final rnd = Random(1);
    for (var y = 100; y < 370; y++) {
      for (var x = 300; x < 780; x++) {
        final i = (y * w + x) * 4;
        busy[i] = (x ~/ 3 + rnd.nextInt(24)) & 0xff;
        busy[i + 1] = (y ~/ 2 + rnd.nextInt(24)) & 0xff;
        busy[i + 2] = (128 + rnd.nextInt(24)) & 0xff;
      }
    }
    for (final (name, frame) in [('flat', flat), ('busy', busy)]) {
      final raw = 4 + 12 + w * h * 4;
      final zlib = 4 + 12 + 4 + _Deflater()(_bgrx(frame)).length;
      final zrle = 4 + 12 + 4 + _Deflater()(_zrleTiles(frame, w, h)).length;
      final tightData = _Deflater()(_rgb(frame));
      final tight = 4 + 12 + 1 + _compact(tightData.length).length + tightData.length;
      // ignore: avoid_print
      print('$name: raw $raw  zlib $zlib  zrle $zrle  tight(lossless) $tight bytes');
      expect(zlib, lessThan(raw ~/ 4));
      expect(zrle, lessThan(raw ~/ 4));
      expect(tight, lessThan(raw ~/ 4));
    }
  });
}

/// 16×8: the left half (220, 30, 30), the right (30, 30, 220); quality 95.
const _jpeg16x8 =
    '/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAIBAQEBAQIBAQECAgICAgQDAgICAgUEBAMEBgUGBgYFBgYGBwkIBgcJBwYGCAsICQoKCgoKBggLDAsKDAkKCgr/2wBDAQICAgICAgUDAwUKBwYHCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgr/wAARCAAIABADAREAAhEBAxEB/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwD5br+fz/Xw+c6/38P8Rz//2Q==';
