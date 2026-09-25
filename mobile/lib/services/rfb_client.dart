import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:web_socket_channel/io.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import 'vm_client.dart';

class RfbException implements Exception {
  RfbException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Where a console connection is.
enum RfbStatus {
  /// Not asked to connect yet (or closed by the app).
  idle,
  connecting,
  connected,

  /// Was connected, then the connection ended ([RfbClient.error] says how).
  disconnected,

  /// Never got connected ([RfbClient.error] says why).
  failed,
}

/// Opens the WebSocket for a console. The default dials it with dart:io;
/// tests pass one that talks to an in-memory RFB server.
typedef RfbChannelFactory = Future<WebSocketChannel> Function(Uri uri, Map<String, String> headers);

/// A minimal RFB (VNC) client over the sidecar's console WebSocket: raw and
/// copy-rect frames, pointer and keys, and the clipboard - both the plain
/// Latin-1 cut text and QEMU's Extended Clipboard (UTF-8, zlib), which it
/// offers when the VM has the qemu-vdagent channel.
///
/// One class for both consoles: a [vmName] targets that VM
/// (`/vms/{name}/console`), null the server's own desktop
/// (`/host/console`). Both go through the gateway at
/// `/v1/vm-sidecar/...` with ws:// or wss:// following the dashboard, and
/// the token in the handshake's Authorization header - never in the URL,
/// where proxies log it (plan M-02, M-16).
class RfbClient {
  RfbClient({this.vmName, VmSession? session, RfbChannelFactory? channelFactory})
      : session = session ?? const VmSession(),
        _factory = channelFactory ?? _ioChannel;

  final String? vmName;
  final VmSession session;
  final RfbChannelFactory _factory;

  static Future<WebSocketChannel> _ioChannel(Uri uri, Map<String, String> headers) async {
    final channel = IOWebSocketChannel.connect(uri, headers: headers, connectTimeout: const Duration(seconds: 15));
    await channel.ready;
    return channel;
  }

  final ValueNotifier<RfbStatus> status = ValueNotifier(RfbStatus.idle);

  /// Why the last connection failed or ended, in words for the screen.
  final ValueNotifier<String?> error = ValueNotifier(null);

  /// The latest picture of the remote screen.
  final ValueNotifier<ui.Image?> frame = ValueNotifier(null);

  /// Counts published frames (for tests and a frame-rate readout).
  final ValueNotifier<int> frameCount = ValueNotifier(0);

  final StreamController<String> _clipboard = StreamController<String>.broadcast();

  /// Text the remote machine copied (ServerCutText).
  Stream<String> get remoteClipboard => _clipboard.stream;

  int width = 0;
  int height = 0;

  /// The server offered the Extended Clipboard (UTF-8 copy and paste).
  bool get extendedClipboard => _serverCaps != 0;

  WebSocketChannel? _channel;
  StreamSubscription<dynamic>? _sub;
  _ByteQueue _reader = _ByteQueue();
  Uint8List? _framebuffer;
  int _generation = 0;
  bool _disposed = false;
  bool _publishing = false;
  bool _dirty = false;

  // Extended Clipboard state: the server's advertised actions, and the
  // text waiting for the server to ask for it.
  int _serverCaps = 0;
  String? _outgoingClipboard;

  static const _encodingRaw = 0;
  static const _encodingCopyRect = 1;
  static const _encodingDesktopSize = -223;

  /// 0xC0A1E5CE as a signed 32-bit number.
  static const encodingExtendedClipboard = -1063131698;

  static const _clipText = 1 << 0;
  static const _clipCaps = 1 << 24;
  static const _clipRequest = 1 << 25;
  static const _clipPeek = 1 << 26;
  static const _clipNotify = 1 << 27;
  static const _clipProvide = 1 << 28;
  static const _clipActionMask = 0xff000000;

  String get _path => vmName != null ? '/vms/${Uri.encodeComponent(vmName!)}/console' : '/host/console';

  bool get isConnected => status.value == RfbStatus.connected;

  /// Connects (or reconnects). Completes when connected or failed; never
  /// throws - the outcome is in [status] and [error].
  Future<void> connect() async {
    if (_disposed || status.value == RfbStatus.connecting || status.value == RfbStatus.connected) return;
    final gen = ++_generation;
    _teardown();
    _reader = _ByteQueue();
    _serverCaps = 0;
    error.value = null;
    status.value = RfbStatus.connecting;

    final WebSocketChannel channel;
    try {
      final token = await session.freshToken();
      channel = await _factory(session.webSocketUri(_path), {
        if (token != null && token.isNotEmpty) 'Authorization': token,
      });
    } catch (e) {
      _fail(gen, _describeConnectError(e), connected: false);
      return;
    }
    if (gen != _generation) {
      unawaited(channel.sink.close());
      return;
    }
    _channel = channel;
    final reader = _reader;
    _sub = channel.stream.listen(
      (event) {
        if (event is List<int>) reader.add(event);
      },
      onError: (Object e) => _fail(gen, 'The console connection was lost.', connected: isConnected),
      onDone: () {
        final code = channel.closeCode;
        final message = switch (code) {
          1011 => 'The console closed because of a problem on the server.',
          1008 => 'The server refused this console session. Sign in again, then retry.',
          _ => vmName != null
              ? 'The console connection closed. The VM may have shut down.'
              : 'The console connection closed.',
        };
        _fail(gen, message, connected: isConnected);
      },
    );

    try {
      await _handshake().timeout(const Duration(seconds: 20));
      if (gen != _generation) return;
      status.value = RfbStatus.connected;
      unawaited(_loop(gen).catchError((Object e) {
        _fail(gen, e is RfbException ? e.message : 'The console stopped: $e', connected: true);
      }));
    } on TimeoutException {
      _fail(gen, "The console didn't answer in time.", connected: false);
    } catch (e) {
      _fail(gen, e is RfbException ? e.message : "Couldn't start the console.", connected: false);
    }
  }

  String _describeConnectError(Object e) {
    if (e is SocketException || e is TimeoutException) return "Can't reach the server.";
    if (e is HandshakeException) return "The server's certificate is not trusted.";
    if (e is VmException) return e.message;
    // A refused upgrade: 400 (the VM isn't running), 401 or 404.
    return vmName != null
        ? "Couldn't open the console. The VM may not be running, or the server refused the connection."
        : "Couldn't open the host desktop. Its streaming service may not be running.";
  }

  void _fail(int gen, String message, {required bool connected}) {
    if (gen != _generation || _disposed) return;
    _generation++;
    _teardown();
    error.value = message;
    status.value = connected ? RfbStatus.disconnected : RfbStatus.failed;
  }

  void _teardown() {
    _sub?.cancel();
    _sub = null;
    _channel?.sink.close();
    _channel = null;
    _reader.close();
  }

  /// Closes the connection on purpose (no error shown).
  void close() {
    _generation++;
    _teardown();
    if (!_disposed) status.value = RfbStatus.idle;
  }

  void dispose() {
    close();
    _disposed = true;
    _clipboard.close();
    frame.value?.dispose();
    status.dispose();
    error.dispose();
    frame.dispose();
    frameCount.dispose();
  }

  void _write(List<int> bytes) => _channel?.sink.add(bytes is Uint8List ? bytes : Uint8List.fromList(bytes));

  Future<void> _handshake() async {
    final version = ascii.decode(await _reader.read(12), allowInvalid: true);
    final match = RegExp(r'RFB (\d{3})\.(\d{3})').firstMatch(version);
    if (match == null) throw RfbException("The server didn't answer like a VNC console.");
    final minor = int.parse(match.group(2)!);
    _write(ascii.encode(minor >= 8 ? 'RFB 003.008\n' : 'RFB 003.00${minor >= 7 ? 7 : 3}\n'));

    if (minor <= 3) {
      final secType = _u32(await _reader.read(4));
      if (secType == 0) throw RfbException(await _failureReason());
      if (secType != 1) throw RfbException('This console asks for a VNC password, which the app does not support.');
    } else {
      final count = (await _reader.read(1))[0];
      if (count == 0) throw RfbException(await _failureReason());
      final types = await _reader.read(count);
      if (!types.contains(1)) {
        throw RfbException('This console asks for a VNC password, which the app does not support.');
      }
      _write([1]);
      if (minor >= 8) {
        final result = _u32(await _reader.read(4));
        if (result != 0) throw RfbException(await _failureReason());
      }
    }

    _write([1]); // ClientInit: shared session

    final init = await _reader.read(24);
    width = (init[0] << 8) | init[1];
    height = (init[2] << 8) | init[3];
    final nameLen = _u32(init.sublist(20, 24));
    if (nameLen > 0) await _reader.read(nameLen);
    if (width <= 0 || height <= 0 || width > 10000 || height > 10000) {
      throw RfbException('The console reported a screen size the app cannot show.');
    }
    _framebuffer = Uint8List(width * height * 4);

    _setPixelFormat();
    _setEncodings();
    _requestUpdate(incremental: false);
  }

  Future<String> _failureReason() async {
    final len = _u32(await _reader.read(4));
    if (len <= 0 || len > 65536) return 'The console refused this connection.';
    return utf8.decode(await _reader.read(len), allowMalformed: true);
  }

  // 32 bpp, depth 24, little endian, true colour, RGB at 16/8/0: each
  // pixel arrives as B, G, R, padding.
  void _setPixelFormat() {
    final msg = Uint8List(20);
    msg.setRange(4, 20, const [32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0]);
    _write(msg);
  }

  void _setEncodings() {
    const encodings = [_encodingCopyRect, _encodingRaw, _encodingDesktopSize, encodingExtendedClipboard];
    final msg = ByteData(4 + encodings.length * 4)
      ..setUint8(0, 2)
      ..setUint16(2, encodings.length);
    for (var i = 0; i < encodings.length; i++) {
      msg.setInt32(4 + i * 4, encodings[i]);
    }
    _write(msg.buffer.asUint8List());
  }

  void _requestUpdate({required bool incremental}) {
    final msg = ByteData(10)
      ..setUint8(0, 3)
      ..setUint8(1, incremental ? 1 : 0)
      ..setUint16(6, width)
      ..setUint16(8, height);
    _write(msg.buffer.asUint8List());
  }

  /// Pointer event: [buttonMask] bit 0 left, 1 middle, 2 right, 3/4 wheel.
  void sendPointer(int x, int y, int buttonMask) {
    if (!isConnected || width <= 0 || height <= 0) return;
    final msg = ByteData(6)
      ..setUint8(0, 5)
      ..setUint8(1, buttonMask)
      ..setUint16(2, x.clamp(0, width - 1))
      ..setUint16(4, y.clamp(0, height - 1));
    _write(msg.buffer.asUint8List());
  }

  void sendKey(int keysym, bool down) {
    if (!isConnected) return;
    final msg = ByteData(8)
      ..setUint8(0, 4)
      ..setUint8(1, down ? 1 : 0)
      ..setUint32(4, keysym);
    _write(msg.buffer.asUint8List());
  }

  void tapKey(int keysym) {
    sendKey(keysym, true);
    sendKey(keysym, false);
  }

  /// Presses [keys] in order and releases them in reverse (Ctrl+Alt+Del).
  void sendCombo(List<int> keys) {
    for (final k in keys) {
      sendKey(k, true);
    }
    for (final k in keys.reversed) {
      sendKey(k, false);
    }
  }

  /// Puts [text] on the remote clipboard. Returns false when some
  /// characters could not go: without the Extended Clipboard, cut text is
  /// Latin-1 only, so anything else arrives as "?" (use [typeText]).
  bool sendClipboard(String text) {
    if (!isConnected) return false;
    if (_serverCaps & _clipNotify != 0) {
      _outgoingClipboard = text;
      _writeExtendedClipboard(_clipNotify | _clipText, const []);
      return true;
    }
    var lossless = true;
    final bytes = Uint8List(text.runes.length);
    var i = 0;
    for (final rune in text.runes) {
      if (rune > 0xff) lossless = false;
      bytes[i++] = rune > 0xff ? 0x3f : rune;
    }
    _writeCutText(bytes, extended: false);
    return lossless;
  }

  /// Types [text] as key presses - works on login screens and without a
  /// clipboard agent in the guest. Newlines are Enter, tabs Tab; at most
  /// [maxTypedChars]. Returns how many characters were typed.
  Future<int> typeText(String text) async {
    if (!isConnected) return 0;
    final normalized = text.replaceAll(RegExp(r'\r\n?'), '\n');
    var n = 0;
    for (final rune in normalized.runes) {
      if (n >= maxTypedChars || !isConnected) break;
      tapKey(keysymForRune(rune));
      n++;
      // Yield now and then so a long text doesn't freeze the UI.
      if (n % 64 == 0) await Future<void>.delayed(Duration.zero);
    }
    return n;
  }

  static const maxTypedChars = 5000;

  /// The X11 keysym for a character: Latin-1 maps to itself, anything else
  /// to the Unicode keysym range.
  static int keysymForRune(int rune) => switch (rune) {
        0x0a => 0xff0d,
        0x09 => 0xff09,
        <= 0xff => rune,
        _ => 0x01000000 + rune,
      };

  void _writeCutText(Uint8List data, {required bool extended}) {
    final msg = ByteData(8)
      ..setUint8(0, 6)
      ..setInt32(4, extended ? -data.length : data.length);
    _write([...msg.buffer.asUint8List(), ...data]);
  }

  void _writeExtendedClipboard(int flags, List<int> payload) {
    final head = ByteData(4)..setUint32(0, flags);
    _writeCutText(Uint8List.fromList([...head.buffer.asUint8List(), ...payload]), extended: true);
  }

  Future<void> _loop(int gen) async {
    while (gen == _generation) {
      final type = (await _reader.read(1))[0];
      switch (type) {
        case 0:
          await _framebufferUpdate();
        case 1: // SetColourMapEntries: not used with true colour; skip.
          final hdr = await _reader.read(5);
          await _reader.read(((hdr[3] << 8) | hdr[4]) * 6);
        case 2: // Bell
          break;
        case 3:
          await _serverCutText();
        default:
          throw RfbException('The console sent something the app does not understand.');
      }
    }
  }

  Future<void> _serverCutText() async {
    final hdr = await _reader.read(7);
    final length = ByteData.sublistView(hdr, 3, 7).getInt32(0);
    if (length >= 0) {
      if (length > 0) {
        final text = latin1.decode(await _reader.read(length));
        if (text.isNotEmpty) _clipboard.add(text);
      }
      return;
    }
    final size = -length;
    if (size < 4 || size > 16 << 20) throw RfbException('The console sent a clipboard message the app cannot read.');
    _extendedClipboard(await _reader.read(size));
  }

  void _extendedClipboard(Uint8List data) {
    final flags = ByteData.sublistView(data, 0, 4).getUint32(0);
    final actions = flags & _clipActionMask;
    final formats = flags & 0xffff;
    if (actions & _clipCaps != 0) {
      _serverCaps = actions & ~_clipCaps;
      // Our caps: text only, every action. The per-format size is the
      // largest unsolicited text we take; 0 means we ask first.
      _writeExtendedClipboard(
        _clipCaps | _clipRequest | _clipPeek | _clipNotify | _clipProvide | _clipText,
        const [0, 0, 0, 0],
      );
      return;
    }
    if (actions & _clipRequest != 0 && formats & _clipText != 0) {
      final text = _outgoingClipboard;
      if (text != null) _provide(text);
    }
    if (actions & _clipPeek != 0) {
      _writeExtendedClipboard(_clipNotify | (_outgoingClipboard != null ? _clipText : 0), const []);
    }
    if (actions & _clipNotify != 0 && formats & _clipText != 0) {
      // The guest copied text: ask for it.
      _writeExtendedClipboard(_clipRequest | _clipText, const []);
    }
    if (actions & _clipProvide != 0 && formats & _clipText != 0 && data.length > 4) {
      final text = decodeClipboardProvide(Uint8List.sublistView(data, 4));
      if (text != null && text.isNotEmpty) _clipboard.add(text);
    }
  }

  void _provide(String text) => _writeExtendedClipboard(_clipProvide | _clipText, encodeClipboardProvide(text));

  /// A Provide payload for text: zlib of (u32 length, UTF-8 with CRLF line
  /// ends and a trailing NUL), as noVNC and QEMU write it.
  static Uint8List encodeClipboardProvide(String text) {
    final body = utf8.encode('${text.replaceAll(RegExp(r'\r\n|\r|\n'), '\r\n')}\u0000');
    final plain = BytesBuilder()
      ..add((ByteData(4)..setUint32(0, body.length)).buffer.asUint8List())
      ..add(body);
    return Uint8List.fromList(ZLibCodec().encode(plain.takeBytes()));
  }

  /// The text in a Provide payload whose formats start with text, or null
  /// if it can't be read.
  static String? decodeClipboardProvide(Uint8List compressed) {
    try {
      final plain = Uint8List.fromList(ZLibCodec().decode(compressed));
      if (plain.length < 4) return null;
      final len = ByteData.sublistView(plain, 0, 4).getUint32(0);
      if (4 + len > plain.length) return null;
      var text = utf8.decode(Uint8List.sublistView(plain, 4, 4 + len), allowMalformed: true);
      if (text.endsWith('\u0000')) text = text.substring(0, text.length - 1);
      return text.replaceAll('\r\n', '\n');
    } catch (_) {
      return null;
    }
  }

  Future<void> _framebufferUpdate() async {
    final hdr = await _reader.read(3);
    final rects = (hdr[1] << 8) | hdr[2];
    for (var i = 0; i < rects; i++) {
      final r = await _reader.read(12);
      final rx = (r[0] << 8) | r[1];
      final ry = (r[2] << 8) | r[3];
      final rw = (r[4] << 8) | r[5];
      final rh = (r[6] << 8) | r[7];
      final encoding = ByteData.sublistView(r, 8, 12).getInt32(0);
      switch (encoding) {
        case _encodingRaw:
          _blitRaw(rx, ry, rw, rh, await _reader.read(rw * rh * 4));
        case _encodingCopyRect:
          final src = await _reader.read(4);
          _copyRect(rx, ry, rw, rh, (src[0] << 8) | src[1], (src[2] << 8) | src[3]);
        case _encodingDesktopSize:
          if (rw > 0 && rh > 0 && rw <= 10000 && rh <= 10000) {
            width = rw;
            height = rh;
            _framebuffer = Uint8List(rw * rh * 4);
          }
        default:
          throw RfbException('The console used a picture format the app does not support.');
      }
    }
    _requestUpdate(incremental: true);
    _schedulePublish();
  }

  void _blitRaw(int rx, int ry, int rw, int rh, Uint8List data) {
    final fb = _framebuffer;
    if (fb == null) return;
    final cols = (rx + rw > width ? width - rx : rw).clamp(0, rw);
    final rows = (ry + rh > height ? height - ry : rh).clamp(0, rh);
    for (var row = 0; row < rows; row++) {
      var s = row * rw * 4;
      var d = ((ry + row) * width + rx) * 4;
      for (var col = 0; col < cols; col++) {
        fb[d] = data[s + 2];
        fb[d + 1] = data[s + 1];
        fb[d + 2] = data[s];
        fb[d + 3] = 255;
        d += 4;
        s += 4;
      }
    }
  }

  void _copyRect(int rx, int ry, int rw, int rh, int sx, int sy) {
    final fb = _framebuffer;
    if (fb == null) return;
    final cols = [rw, width - rx, width - sx].reduce((a, b) => a < b ? a : b);
    if (cols <= 0) return;
    final tmp = Uint8List(cols * 4 * rh);
    for (var row = 0; row < rh && sy + row < height; row++) {
      final start = ((sy + row) * width + sx) * 4;
      tmp.setRange(row * cols * 4, (row + 1) * cols * 4, fb, start);
    }
    for (var row = 0; row < rh && ry + row < height; row++) {
      final start = ((ry + row) * width + rx) * 4;
      fb.setRange(start, start + cols * 4, tmp, row * cols * 4);
    }
  }

  // One decode at a time; updates that arrive meanwhile are folded into
  // the next one, so the last picture always shows.
  void _schedulePublish() {
    if (_publishing) {
      _dirty = true;
      return;
    }
    _publishing = true;
    _publish().whenComplete(() {
      _publishing = false;
      if (_dirty && !_disposed) {
        _dirty = false;
        _schedulePublish();
      }
    });
  }

  Future<void> _publish() async {
    final fb = _framebuffer;
    if (fb == null || width <= 0 || height <= 0) return;
    final completer = Completer<ui.Image>();
    ui.decodeImageFromPixels(Uint8List.fromList(fb), width, height, ui.PixelFormat.rgba8888, completer.complete);
    final image = await completer.future;
    if (_disposed) {
      image.dispose();
      return;
    }
    final old = frame.value;
    frame.value = image;
    frameCount.value++;
    old?.dispose();
  }

  static int _u32(Uint8List b) => ByteData.sublistView(b, 0, 4).getUint32(0);
}

/// Bytes from the WebSocket, read back in the sizes the protocol asks for.
/// Chunks are kept as they arrive and copied once, on read.
class _ByteQueue {
  final Queue<Uint8List> _chunks = Queue();
  int _offset = 0;
  int _available = 0;
  int _wanted = 0;
  Completer<Uint8List>? _pending;
  bool _closed = false;

  void add(List<int> data) {
    if (_closed || data.isEmpty) return;
    final chunk = data is Uint8List ? data : Uint8List.fromList(data);
    _chunks.add(chunk);
    _available += chunk.length;
    _drain();
  }

  void close() {
    if (_closed) return;
    _closed = true;
    _pending?.completeError(RfbException('The console connection closed.'));
    _pending = null;
    _chunks.clear();
  }

  Future<Uint8List> read(int length) {
    if (_closed) return Future.error(RfbException('The console connection closed.'));
    assert(_pending == null, 'one read at a time');
    if (length == 0) return Future.value(Uint8List(0));
    final completer = Completer<Uint8List>();
    _pending = completer;
    _wanted = length;
    _drain();
    return completer.future;
  }

  void _drain() {
    final pending = _pending;
    if (pending == null || _available < _wanted) return;
    final out = Uint8List(_wanted);
    var filled = 0;
    while (filled < _wanted) {
      final chunk = _chunks.first;
      final take = (chunk.length - _offset).clamp(0, _wanted - filled);
      out.setRange(filled, filled + take, chunk, _offset);
      filled += take;
      _offset += take;
      if (_offset >= chunk.length) {
        _chunks.removeFirst();
        _offset = 0;
      }
    }
    _available -= _wanted;
    _pending = null;
    pending.complete(out);
  }
}
