// Test doubles for the VM area: an in-memory RFB (VNC) server behind a
// fake WebSocket, a fake desktop picture, and a VmClient whose
// screenshots come from that picture instead of the network.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:nivaroos_mobile/services/vm_client.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

/// A fake desktop, `w`×`h` RGBA: a dark green wallpaper, a light window
/// with a title bar and lines of "text", and a taskbar - enough to read as
/// a Linux desktop in a screenshot.
Uint8List fakeDesktopRgba(int w, int h) {
  final px = Uint8List(w * h * 4);
  void fill(int x0, int y0, int x1, int y1, int r, int g, int b) {
    for (var y = y0.clamp(0, h); y < y1.clamp(0, h); y++) {
      for (var x = x0.clamp(0, w); x < x1.clamp(0, w); x++) {
        final i = (y * w + x) * 4;
        px[i] = r;
        px[i + 1] = g;
        px[i + 2] = b;
        px[i + 3] = 255;
      }
    }
  }

  fill(0, 0, w, h, 0x2d, 0x3b, 0x34); // wallpaper
  final ww = (w * 0.56).round(), wh = (h * 0.58).round();
  final wx = (w * 0.2).round(), wy = (h * 0.14).round();
  fill(wx + 6, wy + 8, wx + ww + 6, wy + wh + 8, 0x22, 0x2c, 0x27); // soft edge under the window
  fill(wx, wy, wx + ww, wy + wh, 0xf4, 0xf4, 0xf2); // window
  fill(wx, wy, wx + ww, wy + (h * 0.05).round(), 0xd9, 0xdb, 0xd6); // title bar
  final line = (h * 0.024).round().clamp(2, 40);
  for (var i = 0; i < 7; i++) {
    final y = wy + (h * 0.09).round() + i * line * 2;
    final len = [0.8, 0.65, 0.72, 0.4, 0.78, 0.55, 0.3][i];
    fill(wx + (w * 0.02).round(), y, wx + (ww * len).round(), y + line, 0x9a, 0xa1, 0x9c);
  }
  final bar = (h * 0.06).round();
  fill(0, h - bar, w, h, 0x1c, 0x22, 0x1f); // taskbar
  for (var i = 0; i < 5; i++) {
    final x = (w * 0.012).round() + i * (bar + 6);
    fill(x, h - bar + 5, x + bar - 10, h - 5, i == 0 ? 0x86 : 0x55, i == 0 ? 0xbe : 0x5e, i == 0 ? 0x43 : 0x58);
  }
  return px;
}

/// The same picture as a PNG, the way the sidecar's screenshot route
/// answers. Needs the engine: call it from `tester.runAsync` or setUpAll.
Future<Uint8List> fakeDesktopPng(int w, int h) async {
  final completer = Completer<ui.Image>();
  ui.decodeImageFromPixels(fakeDesktopRgba(w, h), w, h, ui.PixelFormat.rgba8888, completer.complete);
  final image = await completer.future;
  final data = await image.toByteData(format: ui.ImageByteFormat.png);
  image.dispose();
  return data!.buffer.asUint8List();
}

/// A VmClient that talks to the fake server for REST but serves
/// screenshots from [png] (the FakeServer only serves JSON and text).
class PictureVmClient extends VmClient {
  PictureVmClient(this.png) : super();
  final Uint8List? png;

  @override
  Future<Uint8List> screenshot(String name) async {
    final p = png;
    if (p == null) throw VmException('No picture');
    return p;
  }
}

/// A session for unit tests: a fixed base URL, a token that [refresh]
/// replaces.
class FakeSession extends VmSession {
  FakeSession({this.base = 'http://nivaro.test', this.currentToken = 'old-token', this.refreshTo});

  final String base;
  String? currentToken;

  /// What [refresh] swaps the token for; null means the refresh fails.
  final String? refreshTo;
  int refreshes = 0;
  int freshTokenCalls = 0;

  @override
  Uri uri(String path, [Map<String, dynamic>? query]) {
    final u = Uri.parse('$base${VmSession.prefix}$path');
    return query == null || query.isEmpty ? u : u.replace(queryParameters: query.map((k, v) => MapEntry(k, '$v')));
  }

  @override
  Uri webSocketUri(String path) {
    final u = uri(path);
    return u.replace(scheme: u.scheme == 'https' ? 'wss' : 'ws');
  }

  @override
  String? get token => currentToken;

  @override
  Future<bool> refresh() async {
    refreshes++;
    if (refreshTo == null) return false;
    currentToken = refreshTo;
    return true;
  }

  @override
  Future<String?> freshToken() async {
    freshTokenCalls++;
    return currentToken;
  }
}

/// A sink that hands what the client writes to the fake server.
class _Sink implements WebSocketSink {
  _Sink(this._onData, this._onClose);
  final void Function(List<int>) _onData;
  final void Function() _onClose;
  final _done = Completer<void>();

  @override
  void add(dynamic data) {
    if (!_done.isCompleted) _onData(data as List<int>);
  }

  @override
  void addError(Object error, [StackTrace? stackTrace]) {}

  @override
  Future<void> addStream(Stream<dynamic> stream) => stream.forEach(add);

  @override
  Future<void> close([int? closeCode, String? closeReason]) async {
    if (!_done.isCompleted) {
      _done.complete();
      _onClose();
    }
  }

  @override
  Future<void> get done => _done.future;
}

/// A WebSocket whose other end is a [FakeRfbServer].
class FakeWebSocketChannel implements WebSocketChannel {
  FakeWebSocketChannel(this._server);
  final FakeRfbServer _server;
  final _incoming = StreamController<dynamic>();
  late final _Sink _sink = _Sink(_server._fromClient, () => _incoming.close());

  @override
  int? closeCode;
  @override
  String? closeReason;

  @override
  String? get protocol => null;

  @override
  Future<void> get ready => Future.value();

  @override
  Stream<dynamic> get stream => _incoming.stream;

  @override
  WebSocketSink get sink => _sink;

  void _send(List<int> bytes) {
    if (!_incoming.isClosed) _incoming.add(Uint8List.fromList(bytes));
  }

  /// The server hangs up with [code].
  void serverClose(int code) {
    closeCode = code;
    _incoming.close();
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

/// An RFB 3.8 server with no password: it greets, sends a [width]×[height]
/// screen of [fakeDesktopRgba] on the first update request, and records
/// every message the client sends.
class FakeRfbServer {
  FakeRfbServer({this.width = 1280, this.height = 720, this.extendedClipboard = false, this.sendFrame = true});

  final int width;
  final int height;

  /// Offer the Extended Clipboard (as QEMU does with a vdagent channel).
  final bool extendedClipboard;
  final bool sendFrame;

  FakeWebSocketChannel? channel;
  Uri? uri;
  Map<String, String>? headers;
  final List<Uint8List> received = [];
  int _stage = 0;
  bool _framed = false;

  /// For RfbClient(channelFactory: server.factory).
  Future<WebSocketChannel> factory(Uri uri, Map<String, String> headers) async {
    this.uri = uri;
    this.headers = headers;
    final ch = FakeWebSocketChannel(this);
    channel = ch;
    _stage = 0;
    _framed = false;
    received.clear();
    scheduleMicrotask(() => ch._send(ascii.encode('RFB 003.008\n')));
    return ch;
  }

  void send(List<int> bytes) => channel?._send(bytes);

  /// Client messages of RFB type [type] (after the handshake).
  List<Uint8List> messages(int type) => received.skip(3).where((m) => m.isNotEmpty && m[0] == type).toList();

  void _fromClient(List<int> data) {
    final bytes = Uint8List.fromList(data);
    received.add(bytes);
    switch (_stage) {
      case 0: // version
        _stage = 1;
        send([1, 1]); // one security type: None
      case 1: // chose None
        _stage = 2;
        send([0, 0, 0, 0]); // SecurityResult OK
      case 2: // ClientInit
        _stage = 3;
        final name = utf8.encode('fake');
        final init = ByteData(24)
          ..setUint16(0, width)
          ..setUint16(2, height)
          ..setUint32(20, name.length);
        send([...init.buffer.asUint8List(), ...name]);
      default:
        if (bytes.isNotEmpty && bytes[0] == 2 && extendedClipboard) {
          // After SetEncodings: the server's clipboard caps (text, all actions).
          sendExtendedClipboard((1 << 24) | (1 << 25) | (1 << 26) | (1 << 27) | (1 << 28) | 1, [0, 0, 0, 0]);
        }
        if (bytes.isNotEmpty && bytes[0] == 3 && bytes[1] == 0 && sendFrame && !_framed) {
          _framed = true;
          _sendFrame();
        }
    }
  }

  void _sendFrame() {
    final rgba = fakeDesktopRgba(width, height);
    final bgrx = Uint8List(rgba.length);
    for (var i = 0; i < rgba.length; i += 4) {
      bgrx[i] = rgba[i + 2];
      bgrx[i + 1] = rgba[i + 1];
      bgrx[i + 2] = rgba[i];
    }
    final hdr = ByteData(16)
      ..setUint8(0, 0)
      ..setUint16(2, 1)
      ..setUint16(8, width)
      ..setUint16(10, height)
      ..setInt32(12, 0);
    send([...hdr.buffer.asUint8List(), ...bgrx]);
  }

  /// ServerCutText, Latin-1.
  void sendCutText(String text) {
    final data = latin1.encode(text);
    final hdr = ByteData(8)
      ..setUint8(0, 3)
      ..setInt32(4, data.length);
    send([...hdr.buffer.asUint8List(), ...data]);
  }

  /// ServerCutText in the Extended Clipboard form: flags and payload.
  void sendExtendedClipboard(int flags, List<int> payload) {
    final body = [...(ByteData(4)..setUint32(0, flags)).buffer.asUint8List(), ...payload];
    final hdr = ByteData(8)
      ..setUint8(0, 3)
      ..setInt32(4, -body.length);
    send([...hdr.buffer.asUint8List(), ...body]);
  }

  /// A zlib Provide payload for [text] (what QEMU sends after a request).
  static List<int> providePayload(String text) {
    final utf = utf8.encode('${text.replaceAll('\n', '\r\n')}\u0000');
    return ZLibCodec().encode([...(ByteData(4)..setUint32(0, utf.length)).buffer.asUint8List(), ...utf]);
  }
}
