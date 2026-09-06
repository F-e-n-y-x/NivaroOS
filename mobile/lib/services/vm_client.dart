import 'dart:convert';
import 'package:http/http.dart' as http;
import '../models/vm_snapshot.dart';

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
  final String? target;
  final bool ssd;

  VmDisk({
    required this.path,
    required this.gib,
    required this.bus,
    this.target,
    this.ssd = true,
  });

  factory VmDisk.fromJson(Map<String, dynamic> j) => VmDisk(
        path: j['path'] as String? ?? '',
        gib: (j['gib'] as num?)?.toInt() ?? 0,
        bus: j['bus'] as String? ?? 'virtio',
        target: j['target'] as String?,
        ssd: j['ssd'] as bool? ?? true,
      );

  Map<String, dynamic> toJson() => {
        'gib': gib,
        'bus': bus,
        if (target != null) 'target': target,
        'ssd': ssd,
      };
}

class VmNetwork {
  final String mode;
  final String? bridgeName;
  final String model;
  final String? mac;
  final String linkState;

  VmNetwork({
    required this.mode,
    this.bridgeName,
    required this.model,
    this.mac,
    this.linkState = 'up',
  });

  factory VmNetwork.fromJson(Map<String, dynamic> j) => VmNetwork(
        mode: j['mode'] as String? ?? 'nat',
        bridgeName: j['bridge_name'] as String?,
        model: j['model'] as String? ?? 'virtio',
        mac: j['mac'] as String?,
        linkState: j['link_state'] as String? ?? 'up',
      );

  Map<String, dynamic> toJson() => {
        'mode': mode,
        if (bridgeName != null && bridgeName!.isNotEmpty) 'bridge_name': bridgeName,
        'model': model,
        if (mac != null) 'mac': mac,
        'link_state': linkState,
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
  final int displayWidth;
  final int displayHeight;

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
    this.displayWidth = 1280,
    this.displayHeight = 720,
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
        displayWidth: (j['display_width'] as num?)?.toInt() ?? 1280,
        displayHeight: (j['display_height'] as num?)?.toInt() ?? 720,
      );

  bool get isRunning => state == 'running';
  String get isoFileName => isoPath?.split('/').last ?? '';
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

class VmClient {
  VmClient(this.host, {this.port = 28641});
  final String host;
  final int port;

  Uri _uri(String path, [Map<String, dynamic>? query]) {
    final uri = Uri.parse('http://$host:$port$path');
    if (query == null || query.isEmpty) return uri;
    return uri.replace(queryParameters: query.map((k, v) => MapEntry(k, '$v')));
  }

  String screenshotUrl(String name, [int? timestamp]) {
    final t = timestamp ?? DateTime.now().millisecondsSinceEpoch;
    return 'http://$host:$port/vms/${Uri.encodeComponent(name)}/screenshot?t=$t';
  }

  String consoleWsUrl(String name) {
    return 'ws://$host:$port/vms/${Uri.encodeComponent(name)}/console';
  }

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
    bool ssd = true,
    String? isoPath,
    required String networkMode,
    String? bridgeName,
    String nicModel = 'virtio',
    String firmware = 'uefi',
    int? displayWidth,
    int? displayHeight,
    List<String>? bootOrder,
  }) async {
    final body = <String, dynamic>{
      'name': name,
      'vcpus': vcpus,
      'memory_mib': memoryMib,
      'firmware': firmware,
      if (diskGib != null && diskGib > 0) 'disks': [VmDisk(path: '', gib: diskGib, bus: diskBus, ssd: ssd).toJson()],
      if (isoPath != null && isoPath.isNotEmpty) 'iso_path': isoPath,
      'networks': [VmNetwork(mode: networkMode, bridgeName: bridgeName, model: nicModel).toJson()],
      if (displayWidth != null && displayWidth > 0) 'display_width': displayWidth,
      if (displayHeight != null && displayHeight > 0) 'display_height': displayHeight,
      if (bootOrder != null && bootOrder.isNotEmpty) 'boot_order': bootOrder,
    };
    final res = await http.post(_uri('/vms'), headers: {'Content-Type': 'application/json'}, body: jsonEncode(body));
    _checkOk(res, okCodes: const [200, 201]);
    return Vm.fromJson(jsonDecode(res.body) as Map<String, dynamic>);
  }

  Future<Vm> updateVm(
    String name, {
    int? vcpus,
    int? memoryMib,
    int? diskGib,
    String? diskBus,
    bool? ssd,
    String? isoPath,
    String? networkMode,
    String? bridgeName,
    String? nicModel,
    String? firmware,
    int? displayWidth,
    int? displayHeight,
    List<String>? bootOrder,
  }) async {
    final body = <String, dynamic>{
      if (vcpus != null) 'vcpus': vcpus,
      if (memoryMib != null) 'memory_mib': memoryMib,
      if (firmware != null) 'firmware': firmware,
      if (isoPath != null) 'iso_path': isoPath,
      if (diskGib != null && diskGib > 0) 'disks': [VmDisk(path: '', gib: diskGib, bus: diskBus ?? 'virtio', ssd: ssd ?? true).toJson()],
      if (networkMode != null) 'networks': [VmNetwork(mode: networkMode, bridgeName: bridgeName, model: nicModel ?? 'virtio').toJson()],
      if (displayWidth != null) 'display_width': displayWidth,
      if (displayHeight != null) 'display_height': displayHeight,
      if (bootOrder != null) 'boot_order': bootOrder,
    };
    final res = await http.put(_uri('/vms/${Uri.encodeComponent(name)}'), headers: {'Content-Type': 'application/json'}, body: jsonEncode(body));
    _checkOk(res);
    return Vm.fromJson(jsonDecode(res.body) as Map<String, dynamic>);
  }

  Future<void> deleteVm(String name, {bool wipeDisk = false}) async {
    final uri = _uri('/vms/${Uri.encodeComponent(name)}', {'wipe_disk': '$wipeDisk'});
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

  Future<void> insertCDROM(String name, String isoPath) async {
    final res = await http.post(_uri('/vms/${Uri.encodeComponent(name)}/cdrom'),
        headers: {'Content-Type': 'application/json'}, body: jsonEncode({'iso_path': isoPath}));
    _checkOk(res, okCodes: const [200, 204]);
  }

  Future<void> ejectCDROM(String name) async {
    final res = await http.post(_uri('/vms/${Uri.encodeComponent(name)}/cdrom/eject'));
    _checkOk(res, okCodes: const [200, 204]);
  }

  Future<void> insertVirtioWin(String name) async {
    final res = await http.post(_uri('/vms/${Uri.encodeComponent(name)}/insert-virtio-win'));
    _checkOk(res, okCodes: const [200, 204]);
  }

  Future<List<VmSnapshot>> listSnapshots(String name) async {
    final res = await http.get(_uri('/vms/${Uri.encodeComponent(name)}/snapshots'));
    _checkOk(res);
    final decoded = jsonDecode(res.body) as List<dynamic>;
    return decoded.map((e) => VmSnapshot.fromJson(e as Map<String, dynamic>)).toList();
  }

  Future<VmSnapshot> createSnapshot(String name, {String? snapName, String? description}) async {
    final body = <String, dynamic>{
      if (snapName != null && snapName.isNotEmpty) 'name': snapName,
      if (description != null && description.isNotEmpty) 'description': description,
    };
    final res = await http.post(
      _uri('/vms/${Uri.encodeComponent(name)}/snapshots'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode(body),
    );
    _checkOk(res, okCodes: const [200, 201]);
    return VmSnapshot.fromJson(jsonDecode(res.body) as Map<String, dynamic>);
  }

  Future<void> revertSnapshot(String name, String snapName) async {
    final res = await http.post(_uri('/vms/${Uri.encodeComponent(name)}/snapshots/${Uri.encodeComponent(snapName)}/revert'));
    _checkOk(res, okCodes: const [200, 204]);
  }

  Future<void> deleteSnapshot(String name, String snapName, {bool children = false}) async {
    final res = await http.delete(_uri('/vms/${Uri.encodeComponent(name)}/snapshots/${Uri.encodeComponent(snapName)}', {'children': '$children'}));
    _checkOk(res, okCodes: const [200, 204]);
  }

  Future<void> setNetworkLink(String name, String mac, String state) async {
    final res = await http.post(
      _uri('/vms/${Uri.encodeComponent(name)}/network/link'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'mac': mac, 'state': state}),
    );
    _checkOk(res, okCodes: const [200, 204]);
  }

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
