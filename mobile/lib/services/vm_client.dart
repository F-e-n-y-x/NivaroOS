import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../models/vm_snapshot.dart';
import 'api_client.dart';

/// Why a VM call failed, so screens can pick the right state (offline,
/// signed out, not set up) instead of parsing messages.
enum VmErrorKind {
  /// The server could not be reached at all (no network, wrong address,
  /// TLS failure, timeout).
  offline,

  /// 401 even after refreshing the token.
  unauthorized,

  /// The gateway answered but the VM service behind it did not (502/503).
  unavailable,

  /// 404: no such VM (or snapshot).
  notFound,

  /// 409: the VM is in the wrong state for this ("still running").
  conflict,

  /// Anything else the sidecar refused (400, 500) - its message says why.
  failed,
}

class VmException implements Exception {
  VmException(this.message, {this.kind = VmErrorKind.failed, this.statusCode, this.details});

  final String message;
  final VmErrorKind kind;
  final int? statusCode;

  /// Method, route and status for "Copy details".
  final String? details;

  @override
  String toString() => message;
}

/// Where VM calls go and how they authenticate: the gateway the rest of
/// the app talks to, with its JWT. Tests replace it.
///
/// Everything goes through the gateway's same-origin route
/// `/v1/vm-sidecar/*` (plan M-01/M-02): the dashboard's own scheme, host
/// and port, so it works on the LAN, over https and through tunnels that
/// only publish that one port. The sidecar's own port (28641) is never
/// used.
class VmSession {
  const VmSession();

  static const prefix = '/v1/vm-sidecar';

  /// A REST URL: `http(s)://<dashboard>/v1/vm-sidecar<path>`.
  Uri uri(String path, [Map<String, dynamic>? query]) => ApiClient.instance.buildUri('$prefix$path', query);

  /// The same URL with ws:// or wss://, following the dashboard's scheme.
  Uri webSocketUri(String path) => ApiClient.instance.webSocketUri('$prefix$path');

  String? get token => ApiClient.instance.accessToken;

  /// Gets a new access token after a 401. True when one is in place; a
  /// refresh the server rejects also ends the session (ApiClient).
  Future<bool> refresh() async => await ApiClient.instance.refresh() == RefreshResult.refreshed;

  /// The token to open a WebSocket with, refreshed first when it is about
  /// to expire: a WebSocket can't retry a 401 the way a REST call does.
  Future<String?> freshToken() => ApiClient.instance.freshToken();
}

class VmDisk {
  const VmDisk({required this.path, required this.gib, required this.bus, this.target, this.ssd = false});

  final String path;
  final int gib;

  /// "virtio", "sata" or "ide".
  final String bus;

  /// The device name libvirt gave it (vda, sdb). Empty for a disk that
  /// is only being asked for.
  final String? target;
  final bool ssd;

  factory VmDisk.fromJson(Map<String, dynamic> j) => VmDisk(
        path: j['path'] as String? ?? '',
        gib: (j['gib'] as num?)?.toInt() ?? 0,
        bus: j['bus'] as String? ?? 'virtio',
        target: j['target'] as String?,
        ssd: j['ssd'] as bool? ?? false,
      );

  /// The request shape (DiskSpec). An existing disk keeps its path, which
  /// is how the sidecar recognises it; an empty path asks for a new one.
  Map<String, dynamic> toJson() => {
        if (path.isNotEmpty) 'path': path,
        'gib': gib,
        'bus': bus,
        if (target != null && target!.isNotEmpty) 'target': target,
        'ssd': ssd,
      };

  VmDisk withSize(int newGib) => VmDisk(path: path, gib: newGib, bus: bus, target: target, ssd: ssd);
}

class VmNetwork {
  const VmNetwork({required this.mode, this.bridgeName, required this.model, this.mac, this.linkState = 'up'});

  /// "nat" or "bridge".
  final String mode;
  final String? bridgeName;

  /// "virtio", "e1000e", "e1000", "rtl8139", ...
  final String model;

  /// Set for an existing adapter - it is how the sidecar tells adapters
  /// apart, so it must go back unchanged or the VM gets a new NIC.
  final String? mac;
  final String linkState;

  factory VmNetwork.fromJson(Map<String, dynamic> j) => VmNetwork(
        mode: j['mode'] as String? ?? 'nat',
        bridgeName: j['bridge_name'] as String?,
        model: j['model'] as String? ?? 'virtio',
        mac: j['mac'] as String?,
        linkState: j['link_state'] as String? ?? 'up',
      );

  Map<String, dynamic> toJson() => {
        'mode': mode,
        if (mode == 'bridge' && bridgeName != null && bridgeName!.isNotEmpty) 'bridge_name': bridgeName,
        'model': model,
        if (mac != null && mac!.isNotEmpty) 'mac': mac,
        'link_state': linkState,
      };

  VmNetwork copyWith({String? mode, String? bridgeName, String? model}) => VmNetwork(
        mode: mode ?? this.mode,
        bridgeName: bridgeName ?? this.bridgeName,
        model: model ?? this.model,
        mac: mac,
        linkState: linkState,
      );
}

class VmUsbDevice {
  const VmUsbDevice({required this.vendorId, required this.productId});
  final String vendorId;
  final String productId;

  factory VmUsbDevice.fromJson(Map<String, dynamic> j) =>
      VmUsbDevice(vendorId: j['vendor_id'] as String? ?? '', productId: j['product_id'] as String? ?? '');
  Map<String, dynamic> toJson() => {'vendor_id': vendorId, 'product_id': productId};
}

class VmPciDevice {
  const VmPciDevice({required this.address});
  final String address;

  factory VmPciDevice.fromJson(Map<String, dynamic> j) => VmPciDevice(address: j['address'] as String? ?? '');
  Map<String, dynamic> toJson() => {'address': address};
}

class VmSharedFolder {
  const VmSharedFolder({required this.sourceDir, required this.targetTag, this.readOnly = false});
  final String sourceDir;
  final String targetTag;
  final bool readOnly;

  factory VmSharedFolder.fromJson(Map<String, dynamic> j) => VmSharedFolder(
        sourceDir: j['source_dir'] as String? ?? '',
        targetTag: j['target_tag'] as String? ?? '',
        readOnly: j['read_only'] as bool? ?? false,
      );
  Map<String, dynamic> toJson() => {'source_dir': sourceDir, 'target_tag': targetTag, 'read_only': readOnly};
}

/// A VM's power state, from libvirt's (`domainStateString` in the sidecar).
enum VmPowerState {
  running,
  paused,
  stopped,
  crashed,
  suspended,
  unknown;

  static VmPowerState parse(String s) => switch (s) {
        'running' => running,
        'paused' => paused,
        'shutoff' => stopped,
        'crashed' => crashed,
        'suspended' || 'pmsuspended' => suspended,
        _ => unknown,
      };
}

/// A VM as GET /vms returns it - every field, so a save can send it back
/// complete (plan M-04, M-05).
class Vm {
  const Vm({
    required this.name,
    required this.state,
    required this.vcpus,
    required this.memoryMib,
    this.diskGib = 0,
    this.networkMode = '',
    this.disks = const [],
    this.networks = const [],
    this.isoPath,
    this.usbDevices = const [],
    this.pciDevices = const [],
    this.sharedFolders = const [],
    this.bootOrder = const [],
    this.firmware = 'bios',
    this.displayWidth = 0,
    this.displayHeight = 0,
    this.clipboardChannel = false,
    this.warning,
  });

  final String name;

  /// The raw state string ("running", "shutoff", ...); see [powerState].
  final String state;
  final int vcpus;
  final int memoryMib;

  /// The first disk's size (the sidecar's own shortcut).
  final int diskGib;
  final String networkMode;
  final List<VmDisk> disks;
  final List<VmNetwork> networks;
  final String? isoPath;
  final List<VmUsbDevice> usbDevices;
  final List<VmPciDevice> pciDevices;
  final List<VmSharedFolder> sharedFolders;

  /// Boot targets, first first: disk targets ("vda"), "cdrom", "network".
  final List<String> bootOrder;

  /// "uefi" or "bios".
  final String firmware;

  /// The preferred guest resolution; 0 when the guest picks its own.
  final int displayWidth;
  final int displayHeight;

  /// The VM has the qemu-vdagent channel for copy and paste (from its
  /// running definition, so only true once the running instance has it).
  final bool clipboardChannel;

  /// Only on a create response: created, but something after that failed.
  final String? warning;

  factory Vm.fromJson(Map<String, dynamic> j) {
    List<T> list<T>(String key, T Function(Map<String, dynamic>) f) =>
        (j[key] as List<dynamic>? ?? const []).whereType<Map<String, dynamic>>().map(f).toList();
    return Vm(
      name: j['name'] as String? ?? '',
      state: j['state'] as String? ?? 'unknown',
      vcpus: (j['vcpus'] as num?)?.toInt() ?? 0,
      memoryMib: (j['memory_mib'] as num?)?.toInt() ?? 0,
      diskGib: (j['disk_gib'] as num?)?.toInt() ?? 0,
      networkMode: j['network_mode'] as String? ?? '',
      disks: list('disks', VmDisk.fromJson),
      networks: list('networks', VmNetwork.fromJson),
      isoPath: (j['iso_path'] as String?)?.isEmpty == true ? null : j['iso_path'] as String?,
      usbDevices: list('usb_devices', VmUsbDevice.fromJson),
      pciDevices: list('pci_devices', VmPciDevice.fromJson),
      sharedFolders: list('shared_folders', VmSharedFolder.fromJson),
      bootOrder: (j['boot_order'] as List<dynamic>? ?? const []).whereType<String>().toList(),
      firmware: j['firmware'] as String? ?? 'bios',
      displayWidth: (j['display_width'] as num?)?.toInt() ?? 0,
      displayHeight: (j['display_height'] as num?)?.toInt() ?? 0,
      clipboardChannel: j['clipboard_channel'] as bool? ?? false,
      warning: j['warning'] as String?,
    );
  }

  VmPowerState get powerState => VmPowerState.parse(state);
  bool get isRunning => powerState == VmPowerState.running;

  /// Running or paused: the hardware can't change, and it can't be deleted.
  bool get isActive => powerState == VmPowerState.running || powerState == VmPowerState.paused;

  String get isoFileName => isoPath?.split('/').last ?? '';

  /// Total size of every disk.
  int get totalDiskGib => disks.isEmpty ? diskGib : disks.fold(0, (sum, d) => sum + d.gib);

  /// The aspect of its screen, for thumbnails (16:9 when the guest picks).
  double get displayAspect => displayWidth > 0 && displayHeight > 0 ? displayWidth / displayHeight : 16 / 9;
}

/// What the edit form changed. Everything left null keeps the VM's
/// current value; [VmClient.buildUpdateBody] merges it into the full spec.
@immutable
class VmEdits {
  const VmEdits({
    this.vcpus,
    this.memoryMib,
    this.firmware,
    this.isoPath,
    this.displayWidth,
    this.displayHeight,
    this.diskSizes = const {},
    this.networks = const {},
  });

  final int? vcpus;
  final int? memoryMib;
  final String? firmware;

  /// Null keeps the inserted ISO, '' ejects it, a path inserts that one.
  final String? isoPath;

  /// Both 0 clears the resolution hint.
  final int? displayWidth;
  final int? displayHeight;

  /// New sizes for existing disks by position. Grow only.
  final Map<int, int> diskSizes;

  /// Replacement mode, bridge and model for existing adapters by position.
  /// Their MAC and link state always stay the VM's own.
  final Map<int, VmNetwork> networks;
}

/// A new VM, as the create form fills it in.
@immutable
class VmCreateSpec {
  const VmCreateSpec({
    required this.name,
    required this.vcpus,
    required this.memoryMib,
    required this.firmware,
    this.diskGib = 0,
    this.diskBus = 'virtio',
    this.diskSsd = true,
    this.isoPath,
    required this.networkMode,
    this.bridgeName,
    this.nicModel = 'virtio',
    this.displayWidth = 0,
    this.displayHeight = 0,
  });

  final String name;
  final int vcpus;
  final int memoryMib;
  final String firmware;

  /// 0 for no disk.
  final int diskGib;
  final String diskBus;
  final bool diskSsd;
  final String? isoPath;
  final String networkMode;
  final String? bridgeName;
  final String nicModel;
  final int displayWidth;
  final int displayHeight;

  Map<String, dynamic> toJson() => {
        'name': name,
        'vcpus': vcpus,
        'memory_mib': memoryMib,
        'firmware': firmware,
        if (diskGib > 0) 'disks': [VmDisk(path: '', gib: diskGib, bus: diskBus, ssd: diskSsd).toJson()],
        if (isoPath != null && isoPath!.isNotEmpty) 'iso_path': isoPath,
        'networks': [VmNetwork(mode: networkMode, bridgeName: bridgeName, model: nicModel).toJson()],
        if (displayWidth > 0 && displayHeight > 0) ...{'display_width': displayWidth, 'display_height': displayHeight},
      };
}

class VmIso {
  const VmIso({required this.name, required this.path, this.sizeMib = 0});
  final String name;
  final String path;
  final int sizeMib;

  /// The sidecar serves ISOs from this one folder and lists them by file
  /// name only; a VM's iso_path is the full path inside it.
  static const folder = '/DATA/VMs/ISOs';

  factory VmIso.fromJson(Map<String, dynamic> j) {
    final path = j['path'] as String?;
    final name = j['name'] as String? ?? (path ?? '').split('/').last;
    return VmIso(
      name: name,
      path: path != null && path.isNotEmpty ? path : '$folder/$name',
      sizeMib: (j['size_mib'] as num?)?.toInt() ?? 0,
    );
  }
}

/// A libvirt network the form can attach to (GET /networks).
class VmHostNetwork {
  const VmHostNetwork({required this.name, required this.mode, this.hostNic, this.active = true});
  final String name;
  final String mode;
  final String? hostNic;
  final bool active;

  factory VmHostNetwork.fromJson(Map<String, dynamic> j) => VmHostNetwork(
        name: j['name'] as String? ?? '',
        mode: j['mode'] as String? ?? '',
        hostNic: j['host_nic'] as String?,
        active: j['active'] as bool? ?? true,
      );
}

/// GET /setup/status: whether the VM Manager's packages are installed.
class VmSetupStatus {
  const VmSetupStatus({required this.ready, this.missingPackages = const []});
  final bool ready;
  final List<String> missingPackages;

  factory VmSetupStatus.fromJson(Map<String, dynamic> j) => VmSetupStatus(
        ready: j['ready'] as bool? ?? false,
        missingPackages: (j['missing_packages'] as List<dynamic>? ?? const []).whereType<String>().toList(),
      );
}

class DisplayResolution {
  const DisplayResolution({required this.width, required this.height, required this.label});
  final int width;
  final int height;
  final String label;
  factory DisplayResolution.fromJson(Map<String, dynamic> j) => DisplayResolution(
        width: (j['width'] as num?)?.toInt() ?? 0,
        height: (j['height'] as num?)?.toInt() ?? 0,
        label: j['label'] as String? ?? '',
      );
}

class HostDisplay {
  const HostDisplay({required this.current, required this.width, required this.height, required this.resolutions});
  final String current;
  final int width;
  final int height;
  final List<DisplayResolution> resolutions;
  factory HostDisplay.fromJson(Map<String, dynamic> j) => HostDisplay(
        current: j['current'] as String? ?? '',
        width: (j['width'] as num?)?.toInt() ?? 0,
        height: (j['height'] as num?)?.toInt() ?? 0,
        resolutions: (j['resolutions'] as List<dynamic>? ?? const [])
            .whereType<Map<String, dynamic>>()
            .map(DisplayResolution.fromJson)
            .toList(),
      );
}

/// REST client for the VM sidecar (VM Manager and Host Desktop), through
/// the gateway with the app's session: every call retries once after a
/// 401 with a refreshed token (plan M-03).
class VmClient {
  /// [ignoredHost] is accepted so callers written for the old direct-port
  /// client (`VmClient(host)`) still compile; the gateway is always used.
  VmClient([String? ignoredHost]) : session = const VmSession();

  VmClient.withSession(this.session);

  final VmSession session;

  static const _timeout = Duration(seconds: 20);

  http.Client? _http;

  // Created on first use, so it is the zone's client when one is set
  // (http.runWithClient, in tests).
  http.Client get _client => _http ??= http.Client();

  void close() {
    _http?.close();
    _http = null;
  }

  String _vm(String name) => '/vms/${Uri.encodeComponent(name)}';

  Future<http.Response> _send(
    String method,
    String path, {
    Map<String, dynamic>? query,
    Object? body,
    List<int> ok = const [200],
  }) async {
    for (var attempt = 0;; attempt++) {
      final req = http.Request(method, session.uri(path, query));
      final token = session.token;
      if (token != null && token.isNotEmpty) req.headers['Authorization'] = token;
      if (body != null) {
        req.headers['Content-Type'] = 'application/json';
        req.body = jsonEncode(body);
      }
      http.Response res;
      try {
        res = await http.Response.fromStream(await _client.send(req)).timeout(_timeout);
      } on TimeoutException {
        throw VmException("The server didn't answer in time.", kind: VmErrorKind.offline, details: '$method $path: timeout');
      } on HandshakeException {
        throw VmException("The server's certificate is not trusted.", kind: VmErrorKind.offline, details: '$method $path: TLS');
      } catch (e) {
        throw VmException("Can't reach the server.", kind: VmErrorKind.offline, details: '$method $path: $e');
      }
      if (res.statusCode == 401 && attempt == 0 && await session.refresh()) continue;
      if (!ok.contains(res.statusCode)) throw _error(method, path, res);
      return res;
    }
  }

  static VmException _error(String method, String path, http.Response res) {
    String? message;
    try {
      final decoded = jsonDecode(res.body);
      if (decoded is Map) message = (decoded['error'] ?? decoded['message'])?.toString();
    } catch (_) {}
    final status = res.statusCode;
    final excerpt = res.body.length > 300 ? '${res.body.substring(0, 300)}…' : res.body;
    final details = '$method ${VmSession.prefix}$path → HTTP $status\n$excerpt';
    final kind = switch (status) {
      401 => VmErrorKind.unauthorized,
      404 => VmErrorKind.notFound,
      409 => VmErrorKind.conflict,
      502 || 503 || 504 => VmErrorKind.unavailable,
      _ => VmErrorKind.failed,
    };
    final text = switch (kind) {
      VmErrorKind.unauthorized => 'Your session expired. Sign in again.',
      VmErrorKind.unavailable => "The VM service on the server isn't responding.",
      _ => _sentence(message) ?? 'The server refused the request (HTTP $status).',
    };
    return VmException(text, kind: kind, statusCode: status, details: details);
  }

  /// The sidecar's messages are lower-case fragments ("vm \"x\" not
  /// found"); shown on their own they read better as a sentence.
  static String? _sentence(String? s) {
    if (s == null || s.trim().isEmpty) return null;
    final t = s.trim().replaceFirst(RegExp(r'^vm\b', caseSensitive: false), 'VM');
    final capital = t[0].toUpperCase() + t.substring(1);
    return RegExp(r'[.!?]$').hasMatch(capital) ? capital : '$capital.';
  }

  static dynamic _json(http.Response res) => res.body.trim().isEmpty ? null : jsonDecode(res.body);

  static List<Map<String, dynamic>> _list(http.Response res) {
    final decoded = _json(res);
    final list = decoded is List ? decoded : (decoded is Map ? decoded['isos'] ?? decoded['data'] : null);
    return (list as List<dynamic>? ?? const []).whereType<Map<String, dynamic>>().toList();
  }

  // --- VMs ---

  Future<List<Vm>> listVms() async => _list(await _send('GET', '/vms')).map(Vm.fromJson).toList();

  Future<Vm> getVm(String name) async => Vm.fromJson(_json(await _send('GET', _vm(name))) as Map<String, dynamic>);

  Future<Vm> createVm(VmCreateSpec spec) async =>
      Vm.fromJson(_json(await _send('POST', '/vms', body: spec.toJson(), ok: const [200, 201])) as Map<String, dynamic>);

  /// PUT /vms/{name} with the complete spec (see [buildUpdateBody]).
  Future<Vm> updateVm(Vm current, VmEdits edits) async {
    final body = buildUpdateBody(current, edits);
    return Vm.fromJson(_json(await _send('PUT', _vm(current.name), body: body)) as Map<String, dynamic>);
  }

  /// The body for PUT /vms/{name}: [current] sent back whole, with only
  /// [edits] changed (plan M-04).
  ///
  /// The sidecar's UpdateVMRequest is a full spec, not a patch
  /// (services/vm-sidecar/domain.go, UpdateVM): disks are matched by
  /// path - a disk sent without its path is a *new* disk to provision -,
  /// adapters are matched by MAC - one without is a new adapter with a new
  /// MAC -, and USB and PCI passthrough lists replace the current ones. So
  /// every disk keeps its path and target, every adapter its MAC and link
  /// state, and the passthrough devices, shared folders and boot order go
  /// back as they are. A disk size can only grow.
  static Map<String, dynamic> buildUpdateBody(Vm current, VmEdits edits) {
    final disks = <Map<String, dynamic>>[];
    for (var i = 0; i < current.disks.length; i++) {
      final d = current.disks[i];
      final wanted = edits.diskSizes[i];
      if (wanted != null && wanted < d.gib) {
        throw ArgumentError('Disk ${i + 1} can only grow (it is ${d.gib} GB, asked for $wanted GB).');
      }
      disks.add(d.withSize(wanted ?? d.gib).toJson());
    }
    final networks = <Map<String, dynamic>>[];
    for (var i = 0; i < current.networks.length; i++) {
      final n = current.networks[i];
      final e = edits.networks[i];
      final merged = e == null ? n : n.copyWith(mode: e.mode, bridgeName: e.bridgeName, model: e.model);
      networks.add(merged.toJson());
    }
    final iso = edits.isoPath ?? current.isoPath ?? '';
    final width = edits.displayWidth ?? current.displayWidth;
    final height = edits.displayHeight ?? current.displayHeight;
    return {
      'vcpus': edits.vcpus ?? current.vcpus,
      'memory_mib': edits.memoryMib ?? current.memoryMib,
      'firmware': edits.firmware ?? current.firmware,
      'disks': disks,
      'networks': networks,
      'usb_devices': [for (final u in current.usbDevices) u.toJson()],
      'pci_devices': [for (final p in current.pciDevices) p.toJson()],
      // Omitted (null) keeps the current ones on the server; an empty
      // list would ask for none.
      if (current.sharedFolders.isNotEmpty) 'shared_folders': [for (final s in current.sharedFolders) s.toJson()],
      if (current.bootOrder.isNotEmpty) 'boot_order': current.bootOrder,
      if (iso.isNotEmpty) 'iso_path': iso,
      if (width > 0 && height > 0) ...{'display_width': width, 'display_height': height},
    };
  }

  /// Adds a new, empty disk (POST /vms/{name}/disks) - the explicit
  /// action that replaces "a disk with no path in the save" (M-04).
  Future<VmDisk> addDisk(String name, {required int gib, String bus = 'virtio', bool ssd = true}) async {
    final res = await _send('POST', '${_vm(name)}/disks', body: {'gib': gib, 'bus': bus, 'ssd': ssd}, ok: const [200, 201]);
    return VmDisk.fromJson(_json(res) as Map<String, dynamic>);
  }

  Future<void> deleteVm(String name, {bool wipeDisk = false}) =>
      _send('DELETE', _vm(name), query: {if (wipeDisk) 'wipe_disk': 'true'}, ok: const [200, 204]);

  Future<void> start(String name) => _action(name, 'start');
  Future<void> shutdown(String name) => _action(name, 'shutdown');
  Future<void> forceOff(String name) => _action(name, 'force-off');
  Future<void> reset(String name) => _action(name, 'reset');
  Future<void> pause(String name) => _action(name, 'pause');
  Future<void> resume(String name) => _action(name, 'resume');

  Future<void> _action(String name, String action) => _send('POST', '${_vm(name)}/$action', ok: const [200, 204]);

  /// Polls until [name] reaches a state [done] accepts, or [timeout]
  /// passes; returns the last VM read (null if every read failed).
  Future<Vm?> waitForState(String name, bool Function(VmPowerState) done,
      {Duration timeout = const Duration(seconds: 20), Duration every = const Duration(milliseconds: 800)}) async {
    final deadline = clock.now().add(timeout);
    Vm? last;
    while (true) {
      try {
        last = await getVm(name);
        if (done(last.powerState)) return last;
      } on VmException {
        // Keep polling; the caller shows whatever it ends up with.
      }
      if (clock.now().isAfter(deadline)) return last;
      await Future<void>.delayed(every);
    }
  }

  /// The VM's current screen as a PNG (GET /vms/{name}/screenshot), with
  /// the session's auth and refresh, for thumbnails.
  Future<Uint8List> screenshot(String name) async =>
      (await _send('GET', '${_vm(name)}/screenshot', query: {'t': clock.now().millisecondsSinceEpoch})).bodyBytes;

  // --- CD-ROM ---

  Future<List<VmIso>> listIsos() async => _list(await _send('GET', '/isos')).map(VmIso.fromJson).toList();

  Future<void> insertCDROM(String name, String isoPath) =>
      _send('POST', '${_vm(name)}/cdrom', body: {'iso_path': isoPath}, ok: const [200, 204]);

  Future<void> ejectCDROM(String name) => _send('POST', '${_vm(name)}/cdrom/eject', ok: const [200, 204]);

  Future<void> insertVirtioWin(String name) => _send('POST', '${_vm(name)}/insert-virtio-win', ok: const [200, 204]);

  // --- Snapshots ---

  Future<List<VmSnapshot>> listSnapshots(String name) async =>
      _list(await _send('GET', '${_vm(name)}/snapshots')).map(VmSnapshot.fromJson).toList();

  Future<VmSnapshot> createSnapshot(String name, {String? snapName, String? description}) async {
    final res = await _send('POST', '${_vm(name)}/snapshots',
        body: {
          if (snapName != null && snapName.isNotEmpty) 'name': snapName,
          if (description != null && description.isNotEmpty) 'description': description,
        },
        ok: const [200, 201]);
    return VmSnapshot.fromJson(_json(res) as Map<String, dynamic>);
  }

  Future<void> revertSnapshot(String name, String snapName) =>
      _send('POST', '${_vm(name)}/snapshots/${Uri.encodeComponent(snapName)}/revert', ok: const [200, 204]);

  Future<void> deleteSnapshot(String name, String snapName, {bool children = false}) =>
      _send('DELETE', '${_vm(name)}/snapshots/${Uri.encodeComponent(snapName)}',
          query: {if (children) 'children': 'true'}, ok: const [200, 204]);

  Future<void> setNetworkLink(String name, String mac, String state) =>
      _send('POST', '${_vm(name)}/network/link', body: {'mac': mac, 'state': state}, ok: const [200, 204]);

  // --- Server and setup ---

  Future<VmSetupStatus> setupStatus() async =>
      VmSetupStatus.fromJson(_json(await _send('GET', '/setup/status')) as Map<String, dynamic>);

  Future<List<VmHostNetwork>> listNetworks() async =>
      _list(await _send('GET', '/networks')).map(VmHostNetwork.fromJson).toList();

  /// Whether the host desktop streaming service is installed.
  Future<bool> hostDesktopInstalled() async {
    final j = _json(await _send('GET', '/host/desktop/installed'));
    return j is Map && j['installed'] == true;
  }

  // The host desktop's own display resolution: the host X server's mode,
  // which x11vnc serves as it is. No per-VM equivalent.
  Future<HostDisplay> getHostDisplay() async =>
      HostDisplay.fromJson(_json(await _send('GET', '/host/display')) as Map<String, dynamic>);

  Future<HostDisplay> setHostDisplay(int width, int height) async {
    final res = await _send('POST', '/host/display', body: {'width': width, 'height': height}, ok: const [200, 204]);
    final j = _json(res);
    return j is Map<String, dynamic> ? HostDisplay.fromJson(j) : getHostDisplay();
  }
}
