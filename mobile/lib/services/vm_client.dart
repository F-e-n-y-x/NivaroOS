import 'dart:convert';
import 'package:http/http.dart' as http;

class VmException implements Exception {
  final String message;
  VmException(this.message);
  @override
  String toString() => message;
}

class VmDisk {
  final String path;
  final int gib;
  final String bus;
  VmDisk({required this.path, required this.gib, required this.bus});

  factory VmDisk.fromJson(Map<String, dynamic> j) => VmDisk(
        path: j['path'] as String? ?? '',
        gib: (j['gib'] as num?)?.toInt() ?? 0,
        bus: j['bus'] as String? ?? 'virtio',
      );

  Map<String, dynamic> toJson() => {'gib': gib, 'bus': bus};
}

class VmNetwork {
  final String mode;
  final String? bridgeName;
  final String model;
  final String? mac;

  VmNetwork({required this.mode, this.bridgeName, required this.model, this.mac});

  factory VmNetwork.fromJson(Map<String, dynamic> j) => VmNetwork(
        mode: j['mode'] as String? ?? 'nat',
        bridgeName: j['bridge_name'] as String?,
        model: j['model'] as String? ?? 'virtio',
        mac: j['mac'] as String?,
      );

  Map<String, dynamic> toJson() => {
        'mode': mode,
        if (bridgeName != null && bridgeName!.isNotEmpty) 'bridge_name': bridgeName,
        'model': model,
      };
}

class Vm {
  final String name;
  final String state;
  final int vcpus;
  final int memoryMib;
  final int diskGib;
  final String networkMode;
  final List<VmDisk> disks;
  final List<VmNetwork> networks;
  final String? isoPath;
  final String firmware;

  Vm({
    required this.name,
    required this.state,
    required this.vcpus,
    required this.memoryMib,
    required this.diskGib,
    required this.networkMode,
    required this.disks,
    required this.networks,
    required this.isoPath,
    required this.firmware,
  });

  factory Vm.fromJson(Map<String, dynamic> j) => Vm(
        name: j['name'] as String? ?? '',
        state: j['state'] as String? ?? 'unknown',
        vcpus: (j['vcpus'] as num?)?.toInt() ?? 0,
        memoryMib: (j['memory_mib'] as num?)?.toInt() ?? 0,
        diskGib: (j['disk_gib'] as num?)?.toInt() ?? 0,
        networkMode: j['network_mode'] as String? ?? '',
        disks: (j['disks'] as List<dynamic>? ?? []).map((e) => VmDisk.fromJson(e as Map<String, dynamic>)).toList(),
        networks: (j['networks'] as List<dynamic>? ?? []).map((e) => VmNetwork.fromJson(e as Map<String, dynamic>)).toList(),
        isoPath: j['iso_path'] as String?,
        firmware: j['firmware'] as String? ?? 'bios',
      );

  bool get isRunning => state == 'running';
}

class VmIso {
  final String name;
  final String path;
  VmIso({required this.name, required this.path});
  factory VmIso.fromJson(Map<String, dynamic> j) => VmIso(
        name: j['name'] as String? ?? (j['path'] as String? ?? '').split('/').last,
        path: j['path'] as String? ?? '',
      );
}

/// The VM sidecar is a separate service from the main NivaroOS gateway -
/// same host, its own port (28641 by default), no "/v1" prefix, and (as
/// confirmed by direct inspection of its Go source) no authentication of
/// its own. This client intentionally mirrors that reality rather than
/// pretending otherwise - see mobile/README.md.
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

  Future<Vm> getVm(String name) async {
    final res = await http.get(_uri('/vms/${Uri.encodeComponent(name)}'));
    _checkOk(res);
    return Vm.fromJson(jsonDecode(res.body) as Map<String, dynamic>);
  }

  Future<Vm> createVm({
    required String name,
    required int vcpus,
    required int memoryMib,
    int? diskGib,
    String diskBus = 'virtio',
    String? isoPath,
    required String networkMode,
    String? bridgeName,
    String firmware = 'bios',
  }) async {
    final body = <String, dynamic>{
      'name': name,
      'vcpus': vcpus,
      'memory_mib': memoryMib,
      'firmware': firmware,
      if (diskGib != null && diskGib > 0) 'disks': [VmDisk(path: '', gib: diskGib, bus: diskBus).toJson()],
      if (isoPath != null && isoPath.isNotEmpty) 'iso_path': isoPath,
      'networks': [VmNetwork(mode: networkMode, bridgeName: bridgeName, model: 'virtio').toJson()],
    };
    final res = await http.post(_uri('/vms'), headers: {'Content-Type': 'application/json'}, body: jsonEncode(body));
    _checkOk(res, okCodes: const [200, 201]);
    return Vm.fromJson(jsonDecode(res.body) as Map<String, dynamic>);
  }

  Future<Vm> updateVm(
    String name, {
    int? vcpus,
    int? memoryMib,
    String? firmware,
  }) async {
    final body = <String, dynamic>{
      if (vcpus != null) 'vcpus': vcpus,
      if (memoryMib != null) 'memory_mib': memoryMib,
      if (firmware != null) 'firmware': firmware,
    };
    final res = await http.put(_uri('/vms/${Uri.encodeComponent(name)}'), headers: {'Content-Type': 'application/json'}, body: jsonEncode(body));
    _checkOk(res);
    return Vm.fromJson(jsonDecode(res.body) as Map<String, dynamic>);
  }

  Future<void> deleteVm(String name, {bool wipeDisk = false}) async {
    final uri = _uri('/vms/${Uri.encodeComponent(name)}').replace(queryParameters: {'wipe_disk': '$wipeDisk'});
    final res = await http.delete(uri);
    _checkOk(res, okCodes: const [200, 204]);
  }

  Future<List<VmIso>> listIsos() async {
    final res = await http.get(_uri('/isos'));
    _checkOk(res);
    final decoded = jsonDecode(res.body);
    final list = decoded is List ? decoded : (decoded as Map<String, dynamic>)['isos'] as List<dynamic>? ?? [];
    return list.map((e) => VmIso.fromJson(e as Map<String, dynamic>)).toList();
  }

  Future<void> start(String name) => _action(name, 'start');
  Future<void> shutdown(String name) => _action(name, 'shutdown');
  Future<void> forceOff(String name) => _action(name, 'force-off');
  Future<void> reset(String name) => _action(name, 'reset');

  Future<void> _action(String name, String action) async {
    final res = await http.post(_uri('/vms/${Uri.encodeComponent(name)}/$action'));
    _checkOk(res, okCodes: const [200, 204]);
  }

  void _checkOk(http.Response res, {List<int> okCodes = const [200]}) {
    if (okCodes.contains(res.statusCode)) return;
    try {
      final decoded = jsonDecode(res.body) as Map<String, dynamic>;
      throw VmException(decoded['error']?.toString() ?? decoded['message']?.toString() ?? 'VM request failed (HTTP ${res.statusCode}).');
    } catch (e) {
      if (e is VmException) rethrow;
      throw VmException('VM request failed (HTTP ${res.statusCode}).');
    }
  }
}
