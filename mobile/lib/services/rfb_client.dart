import 'dart:async';
import 'dart:convert';
import 'dart:ui' as ui;
import 'package:flutter/foundation.dart';
import 'package:web_socket_channel/io.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

/// A real RFB (VNC, RFC 6143) client, written from scratch against the spec -
/// not a webview wrapper. It connects directly to the VM sidecar's console
/// websocket (`ws://<host>:28641/vms/<name>/console`), which was confirmed
/// by reading the Go source to be an unauthenticated, byte-for-byte
/// RFB-over-websocket bridge (like websockify) straight to the VM's real
/// VNC server - so this client speaks the exact same protocol a desktop VNC
/// viewer would, just over a websocket transport instead of raw TCP.
///
/// Scope for v1: Raw + CopyRect encodings only (every RFB server supports
/// Raw; CopyRect covers cheap scroll/move updates) and "None" security only
/// (matches libvirt/QEMU's default loopback-only VNC with no password).
/// A server that requires a VNC password, or that somehow only advertises
/// other encodings, surfaces as a clear [RfbException] rather than a silent
/// black screen.
class RfbException implements Exception {
  final String message;
  RfbException(this.message);
  @override
  String toString() => message;
}

class RfbClient {
  final String host;
  final int port;
  final String vmName;

  RfbClient({required this.host, required this.port, required this.vmName});

  WebSocketChannel? _channel;
  final _ByteQueueReader _reader = _ByteQueueReader();
  StreamSubscription? _sub;

  int width = 0;
  int height = 0;
  Uint8List? _framebuffer;

  final ValueNotifier<ui.Image?> frame = ValueNotifier(null);
  final ValueNotifier<String?> error = ValueNotifier(null);
  bool _closed = false;
  bool _updateInFlight = false;

  static const _encodingRaw = 0;
  static const _encodingCopyRect = 1;

  Future<void> connect() async {
    const scheme = 'ws';
    final uri = Uri.parse('$scheme://$host:$port/vms/${Uri.encodeComponent(vmName)}/console');
    final channel = IOWebSocketChannel.connect(uri);
    _channel = channel;
    _sub = channel.stream.listen(
      (event) {
        if (event is List<int>) {
          _reader.addData(event);
        }
      },
      onError: (e) {
        _fail('Console connection lost: $e');
      },
      onDone: () {
        _fail('Console connection closed.');
      },
    );

    try {
      await _handshake();
      unawaited(_loop().catchError((e) {
        _fail(e is RfbException ? e.message : 'Console error: $e');
      }));
    } catch (e) {
      _fail(e is RfbException ? e.message : 'Failed to start console: $e');
      rethrow;
    }
  }

  void _fail(String message) {
    if (_closed) return;
    error.value = message;
  }

  void close() {
    _closed = true;
    _sub?.cancel();
    _channel?.sink.close();
    _reader.close();
  }

  Future<void> _handshake() async {
    final versionBytes = await _reader.read(12);
    final version = ascii.decode(versionBytes);
    final match = RegExp(r'RFB (\d{3})\.(\d{3})').firstMatch(version);
    if (match == null) throw RfbException('Unexpected server greeting.');
    final minor = int.parse(match.group(2)!);

    _channel!.sink.add(ascii.encode('RFB 003.008\n'));

    if (minor <= 3) {
      final secType = _readU32(await _reader.read(4));
      if (secType == 0) {
        throw RfbException(await _readFailureReason());
      } else if (secType != 1) {
        throw RfbException('This VM console requires a VNC password, which is not supported yet.');
      }
    } else {
      final numTypes = (await _reader.read(1))[0];
      if (numTypes == 0) {
        throw RfbException(await _readFailureReason());
      }
      final types = await _reader.read(numTypes);
      if (!types.contains(1)) {
        throw RfbException('This VM console requires a VNC password, which is not supported yet.');
      }
      _channel!.sink.add([1]);

      final result = _readU32(await _reader.read(4));
      if (result != 0) {
        throw RfbException(await _readFailureReason());
      }
    }

    // ClientInit: shared-flag = 1 (share the display with other viewers).
    _channel!.sink.add([1]);

    // ServerInit.
    final header = await _reader.read(24);
    width = (header[0] << 8) | header[1];
    height = (header[2] << 8) | header[3];
    final nameLen = _readU32(header.sublist(20, 24));
    if (nameLen > 0) {
      await _reader.read(nameLen);
    }
    if (width <= 0 || height <= 0 || width > 10000 || height > 10000) {
      throw RfbException('Server reported an implausible screen size.');
    }
    _framebuffer = Uint8List(width * height * 4);

    _setPixelFormat();
    _setEncodings();
    _requestUpdate(incremental: false);
  }

  Future<String> _readFailureReason() async {
    final len = _readU32(await _reader.read(4));
    if (len <= 0 || len > 65536) return 'The console rejected this connection.';
    final bytes = await _reader.read(len);
    return utf8.decode(bytes, allowMalformed: true);
  }

  void _setPixelFormat() {
    // Request 32bpp true-color with fixed R/G/B shifts so decoding is a
    // simple, predictable byte reorder regardless of the server's native
    // framebuffer format.
    final msg = Uint8List(20);
    msg[0] = 0; // message type: SetPixelFormat
    msg.setRange(4, 20, [
      32, 24, 0, 1, // bits-per-pixel, depth, big-endian=0, true-color=1
      0, 255, // red-max
      0, 255, // green-max
      0, 255, // blue-max
      16, 8, 0, // red-shift, green-shift, blue-shift
      0, 0, 0, // padding
    ]);
    _channel!.sink.add(msg);
  }

  void _setEncodings() {
    final encodings = [_encodingCopyRect, _encodingRaw];
    final msg = ByteData(4 + encodings.length * 4);
    msg.setUint8(0, 2); // message type: SetEncodings
    msg.setUint8(1, 0);
    msg.setUint16(2, encodings.length, Endian.big);
    for (var i = 0; i < encodings.length; i++) {
      msg.setInt32(4 + i * 4, encodings[i], Endian.big);
    }
    _channel!.sink.add(msg.buffer.asUint8List());
  }

  void _requestUpdate({required bool incremental, int x = 0, int y = 0, int? w, int? h}) {
    final msg = ByteData(10);
    msg.setUint8(0, 3); // message type: FramebufferUpdateRequest
    msg.setUint8(1, incremental ? 1 : 0);
    msg.setUint16(2, x, Endian.big);
    msg.setUint16(4, y, Endian.big);
    msg.setUint16(6, w ?? width, Endian.big);
    msg.setUint16(8, h ?? height, Endian.big);
    _channel?.sink.add(msg.buffer.asUint8List());
  }

  void sendPointer(int x, int y, int buttonMask) {
    if (_channel == null) return;
    final msg = ByteData(6);
    msg.setUint8(0, 5); // message type: PointerEvent
    msg.setUint8(1, buttonMask);
    msg.setUint16(2, x.clamp(0, width - 1), Endian.big);
    msg.setUint16(4, y.clamp(0, height - 1), Endian.big);
    _channel!.sink.add(msg.buffer.asUint8List());
  }

  void sendKey(int keysym, bool down) {
    if (_channel == null) return;
    final msg = ByteData(8);
    msg.setUint8(0, 4); // message type: KeyEvent
    msg.setUint8(1, down ? 1 : 0);
    msg.setUint32(4, keysym, Endian.big);
    _channel!.sink.add(msg.buffer.asUint8List());
  }

  Future<void> _loop() async {
    while (!_closed) {
      final typeByte = await _reader.read(1);
      final type = typeByte[0];
      switch (type) {
        case 0:
          await _handleFramebufferUpdate();
          break;
        case 1:
          final hdr = await _reader.read(5);
          final numColors = (hdr[3] << 8) | hdr[4];
          await _reader.read(numColors * 6);
          break;
        case 2:
          break; // Bell - no payload.
        case 3:
          final hdr = await _reader.read(7);
          final len = _readU32(hdr.sublist(3, 7));
          if (len > 0) await _reader.read(len);
          break;
        default:
          throw RfbException('Console sent an unsupported message; disconnecting.');
      }
    }
  }

  Future<void> _handleFramebufferUpdate() async {
    final hdr = await _reader.read(3);
    final numRects = (hdr[1] << 8) | hdr[2];
    for (var i = 0; i < numRects; i++) {
      final rect = await _reader.read(12);
      final rx = (rect[0] << 8) | rect[1];
      final ry = (rect[2] << 8) | rect[3];
      final rw = (rect[4] << 8) | rect[5];
      final rh = (rect[6] << 8) | rect[7];
      final encoding = _readInt32(rect.sublist(8, 12));

      if (encoding == _encodingRaw) {
        final data = await _reader.read(rw * rh * 4);
        _blitRaw(rx, ry, rw, rh, data);
      } else if (encoding == _encodingCopyRect) {
        final src = await _reader.read(4);
        final sx = (src[0] << 8) | src[1];
        final sy = (src[2] << 8) | src[3];
        _copyRect(rx, ry, rw, rh, sx, sy);
      } else {
        throw RfbException('Console sent an encoding this app does not support yet.');
      }
    }
    if (!_updateInFlight) {
      _updateInFlight = true;
      await _publishFrame();
      _updateInFlight = false;
    }
    if (!_closed) _requestUpdate(incremental: true);
  }

  void _blitRaw(int rx, int ry, int rw, int rh, Uint8List data) {
    final fb = _framebuffer;
    if (fb == null) return;
    var srcOffset = 0;
    for (var row = 0; row < rh; row++) {
      final destRow = ry + row;
      if (destRow >= height) {
        srcOffset += rw * 4;
        continue;
      }
      var destOffset = (destRow * width + rx) * 4;
      for (var col = 0; col < rw; col++) {
        if (rx + col < width) {
          // Server sends BGRX (matches the pixel format we requested);
          // Flutter's decodeImageFromPixels wants RGBA.
          fb[destOffset] = data[srcOffset + 2];
          fb[destOffset + 1] = data[srcOffset + 1];
          fb[destOffset + 2] = data[srcOffset];
          fb[destOffset + 3] = 255;
        }
        destOffset += 4;
        srcOffset += 4;
      }
    }
  }

  void _copyRect(int rx, int ry, int rw, int rh, int sx, int sy) {
    final fb = _framebuffer;
    if (fb == null) return;
    // Copy via a temporary buffer since source/destination regions can
    // overlap.
    final tmp = Uint8List(rw * rh * 4);
    for (var row = 0; row < rh; row++) {
      final srcRow = sy + row;
      if (srcRow >= height) continue;
      final srcStart = (srcRow * width + sx) * 4;
      tmp.setRange(row * rw * 4, row * rw * 4 + rw * 4, fb, srcStart);
    }
    for (var row = 0; row < rh; row++) {
      final destRow = ry + row;
      if (destRow >= height) continue;
      final destStart = (destRow * width + rx) * 4;
      fb.setRange(destStart, destStart + rw * 4, tmp, row * rw * 4);
    }
  }

  Future<void> _publishFrame() async {
    final fb = _framebuffer;
    if (fb == null) return;
    final completer = Completer<ui.Image>();
    ui.decodeImageFromPixels(fb, width, height, ui.PixelFormat.rgba8888, completer.complete);
    final image = await completer.future;
    final old = frame.value;
    frame.value = image;
    old?.dispose();
  }

  int _readU32(Uint8List b) => (b[0] << 24) | (b[1] << 16) | (b[2] << 8) | b[3];
  int _readInt32(Uint8List b) {
    final v = _readU32(b);
    return v > 0x7FFFFFFF ? v - 0x100000000 : v;
  }
}

class _PendingRead {
  final int length;
  final Completer<Uint8List> completer;
  _PendingRead(this.length, this.completer);
}

/// Turns a stream of arbitrarily-chunked websocket binary messages into a
/// byte stream that can be read in exact, protocol-dictated lengths - the
/// vm-sidecar's bridge does not align websocket message boundaries with RFB
/// message boundaries at all, so this is required for correctness, not an
/// optimization.
class _ByteQueueReader {
  final List<int> _buffer = [];
  final List<_PendingRead> _pending = [];
  bool _closed = false;

  void addData(List<int> data) {
    if (_closed) return;
    _buffer.addAll(data);
    _drain();
  }

  void close() {
    _closed = true;
    for (final p in _pending) {
      if (!p.completer.isCompleted) p.completer.completeError(RfbException('Console connection closed.'));
    }
    _pending.clear();
  }

  void _drain() {
    while (_pending.isNotEmpty && _buffer.length >= _pending.first.length) {
      final p = _pending.removeAt(0);
      final chunk = Uint8List.fromList(_buffer.sublist(0, p.length));
      _buffer.removeRange(0, p.length);
      p.completer.complete(chunk);
    }
  }

  Future<Uint8List> read(int length) {
    if (_closed) return Future.error(RfbException('Console connection closed.'));
    final completer = Completer<Uint8List>();
    _pending.add(_PendingRead(length, completer));
    _drain();
    return completer.future;
  }
}
