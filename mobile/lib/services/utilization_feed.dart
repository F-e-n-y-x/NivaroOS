import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';

import '../models/container_entry.dart' show AppEvent;
import 'api_client.dart';

/// An open bus socket: its frames, and how to close it.
typedef BusSocket = ({Stream<Object?> frames, void Function() close});

/// Opens the message bus WebSocket at [uri] with [headers].
typedef BusConnect = Future<BusSocket> Function(Uri uri, Map<String, String> headers);

Future<BusSocket> _connect(Uri uri, Map<String, String> headers) async {
  final ws = await WebSocket.connect(uri.toString(), headers: headers).timeout(const Duration(seconds: 10));
  return (frames: ws, close: () => ws.close());
}

/// Home's "Real time" readings: the server's utilization events over the
/// message bus (`/v2/message_bus/event/nivaroos`, token in the handshake's
/// Authorization header like the store's install events), while a fast
/// lease (`POST /v1/sys/utilization/live`, renewed every [leaseEvery])
/// has the server sample every 500 ms instead of 5 s.
///
/// Home calls [want] on each of its 1 s ticks: true while it (or a detail
/// page) is on screen and the app in front, false otherwise - false
/// closes the socket. A socket that won't open or drops is retried with
/// backoff (1 s doubling to [maxBackoff]); until readings flow ([streaming])
/// Home polls every second instead.
class UtilizationFeed {
  UtilizationFeed({required this.onReading, ApiClient? api, BusConnect? connect})
      : _api = api ?? ApiClient.instance,
        _open = connect ?? _connect;

  /// One reading's properties (`sys_cpu`, `sys_mem`, `sys_net`: JSON).
  final void Function(Map<String, String> properties) onReading;
  final ApiClient _api;
  final BusConnect _open;

  static const names = ['nivaroos:system:utilization', 'nivaroos:system:utilization:live'];
  static const leaseEvery = Duration(seconds: 5);
  static const maxBackoff = Duration(seconds: 30);

  /// Readings this far apart still count as streaming (the server sends
  /// two a second; a server without the fast lease, one every 5 s).
  static const staleAfter = Duration(seconds: 2);

  BusSocket? _socket;
  StreamSubscription<Object?>? _sub;
  bool _connecting = false;

  // Bumped by [close], so a socket that finishes opening after it is shut.
  int _gen = 0;
  int _failures = 0;
  DateTime? _retryAt;
  DateTime? _lastFrame;
  DateTime? _leaseAt;

  /// Readings are arriving: Home needn't poll for them.
  bool get streaming => _socket != null && _lastFrame != null && clock.now().difference(_lastFrame!) < staleAfter;

  /// Whether a socket is open (or opening); for tests.
  @visibleForTesting
  bool get connected => _socket != null || _connecting;

  void want(bool on) {
    if (!on) return close();
    final now = clock.now();
    if (_socket != null) {
      _renewLease(now);
      return;
    }
    if (_connecting || (_retryAt != null && now.isBefore(_retryAt!))) return;
    unawaited(_connectNow());
  }

  void _renewLease(DateTime now) {
    if (_leaseAt != null && now.difference(_leaseAt!) < leaseEvery) return;
    _leaseAt = now;
    // An older server has no lease: its readings just come every 5 s.
    unawaited(_api.post('/sys/utilization/live').then((_) {}, onError: (Object _) {}));
  }

  Future<void> _connectNow() async {
    _connecting = true;
    final gen = _gen;
    try {
      final uri = _api.webSocketUri('/v2/message_bus/event/nivaroos').replace(queryParameters: {'names': names});
      final socket = await _open(uri, await _api.authHeaders());
      if (gen != _gen) {
        socket.close();
        return;
      }
      _socket = socket;
      _leaseAt = null;
      _renewLease(clock.now());
      _sub = socket.frames.listen(_frame, onDone: _dropped, onError: (Object _) => _dropped(), cancelOnError: true);
    } catch (_) {
      if (gen == _gen) _backoff();
    } finally {
      if (gen == _gen) _connecting = false;
    }
  }

  void _frame(Object? frame) {
    if (frame is! String) return;
    try {
      final e = AppEvent.tryParse(jsonDecode(frame));
      if (e == null || !names.contains(e.name)) return;
      _lastFrame = clock.now();
      _failures = 0;
      onReading(e.properties);
    } catch (_) {}
  }

  void _dropped() {
    _sub = null;
    _socket = null;
    _lastFrame = null;
    _backoff();
  }

  void _backoff() {
    _failures++;
    final ms = math.min(1000 * (1 << math.min(_failures - 1, 5)), maxBackoff.inMilliseconds);
    _retryAt = clock.now().add(Duration(milliseconds: ms));
  }

  /// Closes the socket; the next [want] (true) opens a new one at once.
  void close() {
    _gen++;
    _sub?.cancel();
    _sub = null;
    _socket?.close();
    _socket = null;
    _connecting = false;
    _lastFrame = null;
    _failures = 0;
    _retryAt = null;
  }
}
