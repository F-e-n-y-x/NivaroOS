// Phone to phone directly over Tailscale (design 2026-10, phase 1).
//
// A file on another of the user's phones normally travels through the
// server (`/v1/file?path=/DATA/Companion/<phone>/...`). When this phone is
// on the tailnet, it asks the server for a peer ticket
// (`POST /v1/companion/:id/peer-ticket`) and reads the file straight from
// the other phone's file server on its Tailscale address (WireGuard end to
// end), with the ticket in the X-Peer-Ticket header only. Anything that
// doesn't work - no Tailscale here, none there, the phone not answering -
// falls back to the server, so nothing gets worse.
import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import 'api_client.dart';
import 'companion_file_server.dart';
import 'device_sync_service.dart';

/// Where to read one file directly, and with which ticket.
@immutable
class PeerLink {
  const PeerLink(this.uri, this.ticket);
  final Uri uri;
  final String ticket;
  Map<String, String> get headers => {'X-Peer-Ticket': ticket};
}

/// The status-line words for a route: direct or through the server.
String peerRouteLabel(bool direct) => direct ? CompanionRoute.tailscale.label! : 'Through the server';

class PeerDirect {
  PeerDirect({
    Future<List<String>> Function()? tailnetIps,
    Future<List<CompanionDevice>> Function()? devices,
    Future<Map<String, dynamic>> Function(String deviceId, Map<String, String> body)? ticket,
    http.Client Function()? client,
    this.probeTimeout = const Duration(milliseconds: 1500),
  })  : _tailnetIps = tailnetIps ?? CompanionFileServer.detectTailnetIps,
        _devices = devices ?? DeviceSyncService.instance.fetchCompanionDevices,
        _ticket = ticket ?? ((id, body) => ApiClient.instance.post('/companion/$id/peer-ticket', body: body)),
        _client = client ?? http.Client.new;

  static PeerDirect instance = PeerDirect();

  final Future<List<String>> Function() _tailnetIps;
  final Future<List<CompanionDevice>> Function() _devices;
  final Future<Map<String, dynamic>> Function(String deviceId, Map<String, String> body) _ticket;
  final http.Client Function() _client;
  final Duration probeTimeout;

  List<CompanionDevice>? _deviceList;
  DateTime _devicesAt = DateTime(0);
  final _hosts = <String, String>{}; // device id -> the address that answered
  final _tickets = <String, ({PeerLink link, DateTime expires})>{};
  final _noDirectUntil = <String, DateTime>{}; // device id -> relay only until

  /// The other phone [serverPath] lives on, or null for anything else.
  Future<CompanionDevice?> _deviceOf(String serverPath) async {
    if (_deviceList == null || clock.now().difference(_devicesAt) > const Duration(minutes: 1)) {
      _deviceList = await _devices();
      _devicesAt = clock.now();
    }
    for (final d in _deviceList!) {
      final root = d.storagePath;
      if (!d.isCurrentDevice && root.isNotEmpty && (serverPath == root || serverPath.startsWith('$root/'))) return d;
    }
    return null;
  }

  /// A direct link to read [serverPath] (a file under another phone's
  /// `/DATA/Companion/<phone>` folder), or null to use the server.
  Future<PeerLink?> resolve(String serverPath) async {
    try {
      if ((await _tailnetIps()).isEmpty) return null;
      final dev = await _deviceOf(serverPath);
      if (dev == null || !dev.isOnline) return null;
      final until = _noDirectUntil[dev.id];
      if (until != null && clock.now().isBefore(until)) return null;
      final key = '${dev.id}\n$serverPath';
      final cached = _tickets[key];
      if (cached != null && clock.now().isBefore(cached.expires) && _hosts[dev.id] == cached.link.uri.authority) return cached.link;

      final res = await _ticket(dev.id, {'path': serverPath, 'op': 'read'});
      final data = res['data'];
      if (data is! Map) return null;
      final ticket = data['ticket']?.toString() ?? '';
      final phonePath = data['path']?.toString() ?? '';
      final cands = [for (final c in (data['candidates'] as List? ?? const [])) c.toString()];
      final expires = DateTime.tryParse(data['expires_at']?.toString() ?? '');
      if (ticket.isEmpty || phonePath.isEmpty || cands.isEmpty || expires == null) return null;
      Uri at(String host) => Uri.parse('http://$host/download').replace(queryParameters: {'path': phonePath});

      var host = _hosts[dev.id];
      if (host == null || !cands.contains(host)) host = await _probe(cands.map(at).toList(), ticket);
      if (host == null) {
        _avoid(dev.id);
        return null;
      }
      _hosts[dev.id] = host;
      final link = PeerLink(at(host), ticket);
      // Asked again a little before it runs out (B gets a new one then).
      _tickets[key] = (link: link, expires: expires.subtract(const Duration(minutes: 5)));
      return link;
    } catch (e) {
      debugPrint('[PeerDirect] using the server: ${e.runtimeType}');
      return null;
    }
  }

  /// HEAD on every candidate at once; the first to answer wins.
  Future<String?> _probe(List<Uri> uris, String ticket) async {
    final client = _client();
    final won = Completer<String?>();
    var left = uris.length;
    for (final u in uris) {
      client.head(u, headers: {'X-Peer-Ticket': ticket}).timeout(probeTimeout).then((r) {
        if ((r.statusCode == 200 || r.statusCode == 206) && !won.isCompleted) won.complete(u.authority);
      }, onError: (_) {}).whenComplete(() {
        if (--left == 0 && !won.isCompleted) won.complete(null);
      });
    }
    try {
      return await won.future;
    } finally {
      client.close();
    }
  }

  void _avoid(String deviceId) => _noDirectUntil[deviceId] = clock.now().add(const Duration(minutes: 1));

  /// A direct read of [link] failed: forget the address and use the server
  /// for a minute.
  void failed(PeerLink link) {
    final dev = _hosts.entries.where((e) => e.value == link.uri.authority).map((e) => e.key).firstOrNull;
    _tickets.removeWhere((_, v) => v.link.uri.authority == link.uri.authority);
    if (dev != null) {
      _hosts.remove(dev);
      _avoid(dev);
    }
  }
}
