// ignore_for_file: non_constant_identifier_names, unused_field
import 'dart:async';
import 'dart:ffi';
import 'dart:io';
import 'dart:math';
import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:ffi/ffi.dart';
import 'package:flutter/services.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:device_info_plus/device_info_plus.dart';
import 'package:battery_plus/battery_plus.dart';
import 'api_client.dart';
import 'storage_service.dart';
import 'companion_file_server.dart';
import 'companion_route.dart';
import 'background_service.dart';
import '../models/file_entry.dart';

export 'companion_route.dart';

// POSIX struct statvfs on 64-bit Linux / Android / iOS
final class StatVfs64 extends Struct {
  @Uint64()
  external int f_bsize;
  @Uint64()
  external int f_frsize;
  @Uint64()
  external int f_blocks;
  @Uint64()
  external int f_bfree;
  @Uint64()
  external int f_bavail;
  @Uint64()
  external int f_files;
  @Uint64()
  external int f_ffree;
  @Uint64()
  external int f_favail;
  @Uint64()
  external int f_fsid;
  @Uint64()
  external int f_flag;
  @Uint64()
  external int f_namemax;
  @Array(6)
  external Array<Uint32> __f_spare;
}

typedef StatVfsNative = Int32 Function(Pointer<Utf8> path, Pointer<StatVfs64> buf);
typedef StatVfsDart = int Function(Pointer<Utf8> path, Pointer<StatVfs64> buf);

/// How the server reaches a device right now (the server's `connection`
/// field): directly on the LAN (files open and copy), only through the
/// tunnel or heartbeat (files list but don't open), or not at all.
enum CompanionConnection { lan, remote, offline, unknown }

class CompanionDevice {
  final String id;
  final String name;
  final String model;
  final String osVersion;
  final String appVersion;
  final String platform;
  final String ipAddress;

  /// Storage size and use in bytes; null when unknown (never made up,
  /// plan M-21).
  final int? storageTotal;
  final int? storageUsed;

  /// Battery percentage; null when unknown.
  final int? battery;
  final bool isOnline;
  final bool isCurrentDevice;
  final String storagePath;
  final int serverStorageUsed; // bytes backed up on the server companion folder
  final DateTime? lastSeen;
  final CompanionConnection connection;

  /// How files travel to the server right now (the server's `route`).
  final CompanionRoute route;
  final Map<String, dynamic>? customProps;

  CompanionDevice({
    required this.id,
    required this.name,
    required this.model,
    required this.osVersion,
    required this.appVersion,
    required this.platform,
    required this.ipAddress,
    this.storageTotal,
    this.storageUsed,
    this.battery,
    this.isOnline = true,
    this.isCurrentDevice = false,
    this.storagePath = '',
    this.serverStorageUsed = 0,
    this.lastSeen,
    this.connection = CompanionConnection.unknown,
    this.route = CompanionRoute.none,
    this.customProps,
  });

  // Older names, kept for the screens that read them: 0 means unknown.
  int get totalStorageBytes => storageTotal ?? 0;
  int get usedStorageBytes => storageUsed ?? 0;
  int get batteryLevel => battery ?? 0;
  DateTime get lastActive => lastSeen ?? DateTime.fromMillisecondsSinceEpoch(0);

  int get freeStorageBytes => (totalStorageBytes - usedStorageBytes).clamp(0, totalStorageBytes);
  double get storageUsagePercent => totalStorageBytes > 0 ? (usedStorageBytes / totalStorageBytes).clamp(0.0, 1.0) : 0.0;

  /// What this phone tells the server when it registers. Unknown numbers
  /// are left out, so the server keeps what it had instead of showing a
  /// made-up value. [sharing] says whether the file server is running.
  ///
  /// [name] is only a suggestion: the server keeps the name a user gave
  /// the phone (from the web or here) and answers with it. [nameIsUsers]
  /// says [name] is such a name the app learned from the server, so a
  /// phone paired again after it was removed gets it back.
  Map<String, dynamic> toRegistration({required bool sharing, int? port, bool nameIsUsers = false}) => {
        'id': id,
        'name': name,
        if (nameIsUsers) 'name_source': 'user',
        'model': model,
        'os_version': osVersion,
        'app_version': appVersion,
        'platform': platform,
        'ip': ipAddress,
        // Every address the file server listens on (Wi-Fi, Tailscale), so
        // the server can reach it away from home (S-04).
        if (sharing && port != null) 'addresses': CompanionFileServer.instance.reachableAddresses,
        if (sharing && port != null) 'port': port,
        'shares_storage': sharing,
        'root_path': CompanionFileServer.defaultRootPath,
        'storage_total': ?storageTotal,
        'storage_used': ?storageUsed,
        'battery_level': ?battery,
        'is_online': true,
        'last_seen': (lastSeen ?? clock.now()).toUtc().toIso8601String(),
        'custom_props': customProps ?? {},
      };

  factory CompanionDevice.fromJson(Map<String, dynamic> json, {String? currentDeviceId}) {
    final devId = json['id'] as String? ?? '';
    final name = (json['name'] as String?)?.trim();
    int? positive(Object? v) {
      final n = (v as num?)?.toInt();
      return n != null && n > 0 ? n : null;
    }

    final connection = switch (json['connection']) {
      'lan' => CompanionConnection.lan,
      'remote' => CompanionConnection.remote,
      'offline' => CompanionConnection.offline,
      _ => CompanionConnection.unknown,
    };
    return CompanionDevice(
      id: devId,
      name: name != null && name.isNotEmpty ? name : (json['model'] as String? ?? 'Unnamed device'),
      model: json['model'] as String? ?? '',
      osVersion: json['os_version'] as String? ?? '',
      appVersion: json['app_version'] as String? ?? '',
      platform: json['platform'] as String? ?? '',
      ipAddress: json['ip'] as String? ?? json['ip_address'] as String? ?? '',
      storageTotal: positive(json['storage_total'] ?? json['total_storage_bytes']),
      storageUsed: positive(json['storage_used'] ?? json['used_storage_bytes']),
      battery: positive(json['battery_level']),
      isOnline: json['is_online'] as bool? ?? (connection == CompanionConnection.lan || connection == CompanionConnection.remote),
      isCurrentDevice: currentDeviceId != null && currentDeviceId == devId,
      storagePath: json['storage_path'] as String? ?? '',
      serverStorageUsed: (json['server_storage_used'] as num?)?.toInt() ?? 0,
      lastSeen: DateTime.tryParse(json['last_seen'] as String? ?? json['last_active'] as String? ?? ''),
      connection: connection,
      route: CompanionRoute.parse(json['route']),
      customProps: json['custom_props'] as Map<String, dynamic>?,
    );
  }

  CompanionDevice copyWith({String? name, bool? isOnline, bool? isCurrentDevice, CompanionConnection? connection}) => CompanionDevice(
        id: id,
        name: name ?? this.name,
        model: model,
        osVersion: osVersion,
        appVersion: appVersion,
        platform: platform,
        ipAddress: ipAddress,
        storageTotal: storageTotal,
        storageUsed: storageUsed,
        battery: battery,
        isOnline: isOnline ?? this.isOnline,
        isCurrentDevice: isCurrentDevice ?? this.isCurrentDevice,
        storagePath: storagePath,
        serverStorageUsed: serverStorageUsed,
        lastSeen: lastSeen,
        connection: connection ?? this.connection,
        route: route,
        customProps: customProps,
      );
}

/// Why the last registration with the server failed, for the companion
/// screen. Null after a successful one.
class RegistrationProblem {
  const RegistrationProblem(this.message, {this.otherAccount = false});

  final String message;

  /// The server says this phone belongs to another NivaroOS account.
  final bool otherAccount;
}

/// This phone as a companion device of the server: registration (the
/// heartbeat), the device list, rename and remove. Registration follows the
/// server's rules (S-02, S-03): the device is identified by its id only,
/// owned by the signed-in account, and proves itself with the
/// X-Companion-Secret the server gave it earlier.
class DeviceSyncService {
  DeviceSyncService._();
  static final DeviceSyncService instance = DeviceSyncService._();

  CompanionDevice? _currentDevice;
  CompanionDevice? get currentDevice => _currentDevice;

  /// The last registration problem (null when it worked or wasn't tried).
  final ValueNotifier<RegistrationProblem?> registrationProblem = ValueNotifier(null);

  String? _cachedDeviceId;
  Timer? _shareTimer;

  /// This phone's own name (brand and model), from the last
  /// [getLocalDeviceInfo].
  String? _defaultName;

  /// Set by [pairAfterSignIn]: the next registration pairs the phone
  /// again even if the server removed it - the user just signed in on it.
  bool _pairOnSignIn = false;
  final DeviceInfoPlugin _deviceInfo = DeviceInfoPlugin();
  static const MethodChannel _platformChannel = MethodChannel('com.fenyx.nivaroos/device_info');

  Future<String> getDeviceId() async {
    if (_cachedDeviceId != null) return _cachedDeviceId!;
    var id = await StorageService.instance.getCompanionDeviceId();
    if (Platform.isAndroid) {
      try {
        final hardwareId = await _platformChannel.invokeMethod<String>('getHardwareId');
        if (hardwareId != null && hardwareId.trim().isNotEmpty) {
          id = 'dev_android_${hardwareId.trim()}';
        }
      } catch (_) {}
    }
    if (id == null || id.isEmpty) {
      final rand = Random.secure().nextInt(1 << 32);
      final ts = clock.now().millisecondsSinceEpoch;
      id = 'dev_${Platform.operatingSystem}_${ts}_$rand';
    }
    await StorageService.instance.setCompanionDeviceId(id);
    _cachedDeviceId = id;
    return id;
  }

  /// Replaces the statvfs reading in tests and screenshots, where the
  /// host machine's own disk (whose free space changes from run to run)
  /// would otherwise show up as this phone's storage.
  @visibleForTesting
  static Map<String, int>? storageMetricsOverride;

  /// (brand, model, system version) instead of the machine's, for tests
  /// and screenshots.
  @visibleForTesting
  static (String, String, String)? deviceInfoOverride;

  /// This phone's shared storage size and use via POSIX statvfs, or null
  /// when it can't be read.
  Map<String, int>? getRealStorageMetrics() {
    final override = storageMetricsOverride;
    if (override != null) return override;
    try {
      final dylib = Platform.isAndroid ? DynamicLibrary.open('libc.so') : DynamicLibrary.process();
      final statvfs = dylib.lookupFunction<StatVfsNative, StatVfsDart>('statvfs');

      final candidates = ['/storage/emulated/0', '/data', '/data/user/0', '/'];
      for (final candidate in candidates) {
        if (Directory(candidate).existsSync()) {
          final pathPtr = candidate.toNativeUtf8();
          final buf = calloc<StatVfs64>();
          try {
            final res = statvfs(pathPtr, buf);
            if (res == 0) {
              final frsize = buf.ref.f_frsize > 0 ? buf.ref.f_frsize : buf.ref.f_bsize;
              final total = buf.ref.f_blocks * frsize;
              final free = buf.ref.f_bavail * frsize;
              if (total > 0) {
                final used = total - free;
                return {'total': total, 'used': used, 'free': free};
              }
            }
          } finally {
            calloc.free(pathPtr);
            calloc.free(buf);
          }
        }
      }
    } catch (e) {
      debugPrint('[DeviceSyncService] statvfs lookup notice: $e');
    }
    return null;
  }

  // battery_plus, not the custom `_platformChannel` (com.fenyx.nivaroos/
  // device_info): that channel only exists on the Activity's engine, and
  // the heartbeat and sharing engines have none. battery_plus registers on
  // every engine.
  final Battery _battery = Battery();

  /// The battery percentage, or null when unknown (never a made-up 100%).
  Future<int?> getRealBatteryLevel() async {
    try {
      final level = await _battery.batteryLevel.timeout(const Duration(seconds: 3));
      if (level > 0 && level <= 100) return level;
    } catch (e) {
      debugPrint('[DeviceSyncService] Battery query notice: $e');
    }
    return null;
  }

  /// This phone as the app sees it. Its name is the one the server holds
  /// for it (kept from the last registration or device list - the server
  /// is the source of truth, whether the user renamed it on the web or
  /// here), or the phone's own brand and model before it has one.
  Future<CompanionDevice> getLocalDeviceInfo() async {
    final devId = await getDeviceId();
    final serverName = (await StorageService.instance.getCompanionDeviceName())?.trim();
    final packageInfo = await PackageInfo.fromPlatform();

    String brand = '';
    String model = '';
    String osVer = '';
    final platform = Platform.isAndroid ? 'Android' : (Platform.isIOS ? 'iOS' : Platform.operatingSystem);

    final fake = deviceInfoOverride;
    if (fake != null) {
      (brand, model, osVer) = fake;
    } else if (Platform.isAndroid) {
      try {
        final android = await _deviceInfo.androidInfo;
        brand = android.brand.isNotEmpty ? _capitalize(android.brand) : '';
        model = android.model;
        osVer = 'Android ${android.version.release} (SDK ${android.version.sdkInt})';
      } catch (_) {}
    } else if (Platform.isIOS) {
      try {
        final ios = await _deviceInfo.iosInfo;
        brand = 'Apple';
        model = ios.name;
        osVer = 'iOS ${ios.systemVersion}';
      } catch (_) {}
    }

    final batteryPct = await getRealBatteryLevel();
    final storage = getRealStorageMetrics();

    final String defaultName;
    if (model.isEmpty) {
      defaultName = 'This phone';
    } else if (brand.isEmpty || brand == model || model.startsWith(brand)) {
      defaultName = model;
    } else {
      defaultName = '$brand $model';
    }

    _defaultName = defaultName;
    final device = CompanionDevice(
      id: devId,
      name: serverName != null && serverName.isNotEmpty ? serverName : defaultName,
      model: model,
      osVersion: osVer,
      appVersion: 'v${packageInfo.version}+${packageInfo.buildNumber}',
      platform: platform,
      ipAddress: CompanionFileServer.instance.localIp ?? '',
      storageTotal: storage?['total'],
      storageUsed: storage?['used'],
      battery: batteryPct,
      isCurrentDevice: true,
      isOnline: true,
      lastSeen: clock.now(),
      customProps: const {},
    );

    _currentDevice = device;
    return device;
  }

  static String _capitalize(String s) {
    if (s.isEmpty) return s;
    return s[0].toUpperCase() + s.substring(1);
  }

  /// Renames this phone on the server - the same rename the web does - and
  /// shows the name the server kept. Throws an [ApiException] when the
  /// server can't be reached: the name is the server's, so it isn't
  /// changed here only (it used to be, and then overwrote renames made on
  /// the web).
  Future<void> setDeviceCustomName(String newName) async {
    final trimmed = newName.trim();
    final devId = await getDeviceId();
    final res = await ApiClient.instance.put('/companion/devices/$devId', body: {'name': trimmed});
    final data = res['data'];
    final kept = data is Map ? (data['name'] as String?)?.trim() : null;
    await _adoptServerName(kept != null && kept.isNotEmpty ? kept : trimmed);
  }

  /// Keeps the server's name for this phone, to show and to suggest when
  /// it is paired again.
  Future<void> _adoptServerName(String? name) async {
    final n = name?.trim();
    if (n == null || n.isEmpty) return;
    if (await StorageService.instance.getCompanionDeviceName() != n) {
      await StorageService.instance.setCompanionDeviceName(n);
    }
    final current = _currentDevice;
    if (current != null && current.name != n) _currentDevice = current.copyWith(name: n);
  }

  /// Called right after the user signed in on this phone: its first
  /// registration pairs it again, even if the server removed it (signing
  /// in on it is how a removed phone is paired again).
  void pairAfterSignIn() => _pairOnSignIn = true;

  /// After sign-in or launch with a session, on the phone's screen:
  /// registers once and schedules the 15-minute heartbeat.
  Future<void> onSignedIn() async {
    if (!_pairOnSignIn && await StorageService.instance.getCompanionRemoved()) {
      // Removed by an app version that stayed signed in afterwards: finish
      // what removal means now - sign out, and pair again by signing in.
      await markRemoved();
      return;
    }
    await BackgroundService.instance.scheduleHeartbeat();
    await syncWithServer(repair: _pairOnSignIn);
  }

  /// Before sign-out or a server switch: stops sharing and the heartbeat,
  /// so nothing keeps talking to this server with its tokens (plan M-17).
  Future<void> onSigningOut() async {
    registrationProblem.value = null;
    await BackgroundService.instance.stopAll();
  }

  /// Inside the sharing service's engine: registers as sharing now and
  /// every minute while the session lasts (the server counts a device as
  /// online for 75 s after it last heard from it).
  void startSharingHeartbeat() {
    _shareTimer?.cancel();
    syncWithServer(sharing: true);
    _shareTimer = Timer.periodic(const Duration(minutes: 1), (_) => syncWithServer(sharing: true));
  }

  void stopSharingHeartbeat() {
    _shareTimer?.cancel();
    _shareTimer = null;
  }

  /// Called when the server removed this phone, in whichever engine
  /// noticed it (the app, or the sharing service - which stops itself).
  static void Function()? onRemovedByServer;

  /// The server removed this phone from its device list (a 410 from
  /// register, or "removed" over the tunnel; the server also ended the
  /// app's session, see [ApiClient]): the app signs out of that server.
  /// Sharing and the heartbeat stop, the pairing secret and this server's
  /// saved state go, and the sign-in screen says why. Signing in again
  /// pairs the phone again.
  Future<void> markRemoved() async {
    _pairOnSignIn = false;
    await StorageService.instance.setCompanionRemoved(true);
    await forgetServerState();
    stopSharingHeartbeat();
    registrationProblem.value = null;
    final hook = onRemovedByServer;
    if (hook != null) {
      hook();
    } else {
      await BackgroundService.instance.stopAll();
    }
    await ApiClient.instance.endSession(reason: SessionEndReason.companionRemoved);
  }

  /// What the app keeps for the current server that a removed phone must
  /// not keep: the file-server secret and the open Files tabs.
  Future<void> forgetServerState() async {
    await StorageService.instance.clearCompanionSecret();
    await StorageService.instance.setFilesTabs(null);
  }

  /// Registers this phone with the server (id only; the secret proves it
  /// is this phone). With [sharing], the file server's port goes with it.
  /// A phone the server removed isn't registered again unless [repair]
  /// (the first registration after the user signed in on it). Returns true
  /// when the server accepted it.
  Future<bool> syncWithServer({bool sharing = false, bool repair = false}) async {
    if (!ApiClient.instance.hasSession) return false;
    // Removed, and not yet signed in again (another engine's heartbeat in
    // the moment between the sign-in and its first registration): quiet.
    if (!repair && await StorageService.instance.getCompanionRemoved()) return false;
    try {
      final device = await getLocalDeviceInfo();
      final heldSecret = await StorageService.instance.getCompanionSecret();
      final nameIsUsers = device.name != _defaultName;
      final res = await ApiClient.instance.post(
        '/companion/register',
        body: {
          ...device.toRegistration(
            sharing: sharing,
            port: CompanionFileServer.instance.isRunning ? CompanionFileServer.instance.port : null,
            nameIsUsers: nameIsUsers,
          ),
          if (repair) 'repair': true,
        },
        headers: (heldSecret != null && heldSecret.isNotEmpty) ? {'X-Companion-Secret': heldSecret} : null,
      );
      // The server hands back the secret its requests to this phone's file
      // server carry (see PostRegisterCompanionDevice). An empty one means
      // the server kept an older pairing it could not link to this account.
      final data = res['data'] is Map ? res['data'] as Map : const {};
      final secret = data['secret'] as String?;
      if (secret != null && secret.isNotEmpty) {
        await StorageService.instance.setCompanionSecret(secret);
        registrationProblem.value = null;
      } else if (secret != null) {
        registrationProblem.value = RegistrationProblem(res['message']?.toString() ?? 'The server kept an older pairing of this phone.');
      }
      final serverDev = data['device'] is Map<String, dynamic> ? data['device'] as Map<String, dynamic> : null;
      // The server's name wins (a rename on the web shows here).
      await _adoptServerName(serverDev?['name'] as String?);
      if (repair) {
        _pairOnSignIn = false;
        await StorageService.instance.setCompanionRemoved(false);
      }
      return true;
    } on ApiException catch (e) {
      if (e.statusCode == 410) {
        await markRemoved();
        return false;
      }
      if (e.statusCode == 403) {
        registrationProblem.value = RegistrationProblem(
          'This phone is paired with another NivaroOS account on this server. Remove it there first, or sign in with that account.',
          otherAccount: true,
        );
      }
      debugPrint('[DeviceSyncService] Register failed: ${e.kind.name} ${e.statusCode ?? ''}');
      return false;
    } catch (e) {
      debugPrint('[DeviceSyncService] Register failed: ${e.runtimeType}');
      return false;
    }
  }

  /// The signed-in account's devices from the server, with this phone
  /// first (from its own readings). Throws an [ApiException] when the list
  /// can't be loaded.
  /// Replaces [fetchCompanionDevices] in tests (a list that is still
  /// loading, for the skeleton shot).
  @visibleForTesting
  static Future<List<CompanionDevice>> Function()? debugFetchDevices;

  Future<List<CompanionDevice>> fetchCompanionDevices() async {
    final fake = debugFetchDevices;
    if (fake != null) return fake();
    final currentDev = await getLocalDeviceInfo();
    final res = await ApiClient.instance.get('/companion/devices');
    final data = res['data'] as List<dynamic>? ?? [];

    // Devices are identified by their id only: two phones can share a
    // private LAN address (different home networks, a reused DHCP lease)
    // or a model and default name.
    final list = <CompanionDevice>[];
    final seenIds = <String>{};
    for (final item in data) {
      if (item is Map<String, dynamic>) {
        final d = CompanionDevice.fromJson(item, currentDeviceId: currentDev.id);
        if (d.id.isNotEmpty && seenIds.add(d.id)) list.add(d);
      }
    }

    final matchIndex = list.indexWhere((d) => d.id == currentDev.id);
    if (matchIndex >= 0) {
      final matched = list.removeAt(matchIndex);
      // The name is the server's (renamed on the web: shown here too).
      await _adoptServerName(matched.name);
      list.insert(
        0,
        CompanionDevice(
          id: matched.id,
          name: matched.name,
          model: currentDev.model.isNotEmpty ? currentDev.model : matched.model,
          osVersion: currentDev.osVersion,
          appVersion: currentDev.appVersion,
          platform: currentDev.platform,
          ipAddress: currentDev.ipAddress.isNotEmpty ? currentDev.ipAddress : matched.ipAddress,
          storageTotal: currentDev.storageTotal ?? matched.storageTotal,
          storageUsed: currentDev.storageUsed ?? matched.storageUsed,
          battery: currentDev.battery ?? matched.battery,
          isOnline: true,
          isCurrentDevice: true,
          storagePath: matched.storagePath,
          serverStorageUsed: matched.serverStorageUsed,
          lastSeen: currentDev.lastSeen,
          connection: matched.connection,
        ),
      );
    } else {
      list.insert(0, currentDev);
    }
    return list;
  }

  /// [fetchCompanionDevices] for screens that show what they can: on a
  /// failure, just this phone.
  Future<List<CompanionDevice>> listCompanionDevices() async {
    try {
      return await fetchCompanionDevices();
    } catch (e) {
      debugPrint('[DeviceSyncService] Error fetching devices: $e');
      return [await getLocalDeviceInfo()];
    }
  }

  Future<void> updateRemoteDeviceName(String id, String newName) async {
    final currentId = await getDeviceId();
    if (id == currentId) {
      await setDeviceCustomName(newName);
      return;
    }
    await ApiClient.instance.put('/companion/devices/$id', body: {'name': newName.trim()});
  }

  /// Removes another device from the server; its app is signed out.
  Future<void> deleteCompanionDevice(String id) async {
    await ApiClient.instance.delete('/companion/devices/$id');
  }

  Future<List<FileEntry>> getCompanionFiles(String deviceId, {String? path}) async {
    try {
      final res = await ApiClient.instance.get('/companion/devices/$deviceId/files', query: {
        if (path != null && path.isNotEmpty) 'path': path,
      });
      final data = res['data'] as Map<String, dynamic>? ?? {};
      final filesRaw = data['files'] as List<dynamic>? ?? [];
      return filesRaw.map((f) => FileEntry.fromJson(f as Map<String, dynamic>)).toList();
    } catch (_) {
      return [];
    }
  }
}
