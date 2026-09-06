import 'dart:convert';
import 'package:http/http.dart' as http;

class VmException implements Exception {
  final String message;
  VmException(this.message);
  @override
  String toString() => message;
}

class Vm {
  final String name;
  final String state;
  final int vcpus;
  final int memoryMib;
  final int diskGib;
  final String networkMode;

  Vm({
    required this.name,
    required this.state,
    required this.vcpus,
    required this.memoryMib,
    required this.diskGib,
    required this.networkMode,
  });

  factory Vm.fromJson(Map<String, dynamic> j) => Vm(
        name: j['name'] as String? ?? '',
        state: j['state'] as String? ?? 'unknown',
        vcpus: (j['vcpus'] as num?)?.toInt() ?? 0,
        memoryMib: (j['memory_mib'] as num?)?.toInt() ?? 0,
        diskGib: (j['disk_gib'] as num?)?.toInt() ?? 0,
        networkMode: j['network_mode'] as String? ?? '',
      );

  bool get isRunning => state == 'running';
}

/// The VM sidecar is a separate service from the main NivaroOS gateway -
/// same host, its own port (28641 by default), no "/v1" prefix, and (as
/// found and left as a known, documented gap during this app's own
/// security review of NivaroOS itself) no authentication of its own yet.
/// This client intentionally mirrors that reality rather than pretending
/// otherwise - see mobile/README.md.
class VmClient {
  VmClient(this.host, {this.port = 28641});
  final String host;
  final int port;

  Uri _uri(String path) => Uri.parse('http://$host:$port$path');

  Future<List<Vm>> listVms() async {
    final res = await http.get(_uri('/vms'));
    _checkOk(res);
    final list = jsonDecode(res.body) as List<dynamic>;
    return list.map((e) => Vm.fromJson(e as Map<String, dynamic>)).toList();
  }

  Future<void> start(String name) => _action(name, 'start');
  Future<void> shutdown(String name) => _action(name, 'shutdown');
  Future<void> forceOff(String name) => _action(name, 'force-off');
  Future<void> reset(String name) => _action(name, 'reset');

  Future<void> _action(String name, String action) async {
    final res = await http.post(_uri('/vms/${Uri.encodeComponent(name)}/$action'));
    _checkOk(res);
  }

  void _checkOk(http.Response res) {
    if (res.statusCode >= 200 && res.statusCode < 300) return;
    try {
      final decoded = jsonDecode(res.body) as Map<String, dynamic>;
      throw VmException(decoded['error']?.toString() ?? 'VM request failed (HTTP ${res.statusCode}).');
    } catch (e) {
      if (e is VmException) rethrow;
      throw VmException('VM request failed (HTTP ${res.statusCode}).');
    }
  }
}
