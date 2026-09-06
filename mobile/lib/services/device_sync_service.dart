// ignore_for_file: non_constant_identifier_names, unused_field
import 'dart:async';
import 'dart:ffi';
import 'dart:io';
import 'dart:math';
import 'package:flutter/foundation.dart';
import 'package:ffi/ffi.dart';
import 'package:flutter/services.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:device_info_plus/device_info_plus.dart';
import 'api_client.dart';
import 'storage_service.dart';
import 'companion_file_server.dart';
import 'background_service.dart';
import '../models/file_entry.dart';

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

class CompanionDevice {
  final String id;
  final String name;
  final String model;
  final String osVersion;
  final String appVersion;
  final String platform;
  final String ipAddress;
  final int totalStorageBytes;
  final int usedStorageBytes;
  final int batteryLevel;
  final bool isOnline;
  final bool isCurrentDevice;
  final String storagePath;
  final int serverStorageUsed; // bytes backed up on the server companion folder
  final DateTime lastActive;

  CompanionDevice({
    required this.id,
    required this.name,
    required this.model,
    required this.osVersion,
    required this.appVersion,
    required this.platform,
    required this.ipAddress,
    required this.totalStorageBytes,
    required this.usedStorageBytes,
    this.batteryLevel = 100,
    this.isOnline = true,
    this.isCurrentDevice = false,
    this.storagePath = '',
    this.serverStorageUsed = 0,
    required this.lastActive,
  });

  int get freeStorageBytes => (totalStorageBytes - usedStorageBytes).clamp(0, totalStorageBytes);
  double get storageUsagePercent => totalStorageBytes > 0 ? (usedStorageBytes / totalStorageBytes).clamp(0.0, 1.0) : 0.0;

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'model': model,
        'os_version': osVersion,
        'app_version': appVersion,
        'platform': platform,
        'ip': ipAddress,
        'port': CompanionFileServer.instance.port,
        'shares_storage': true,
        'root_path': CompanionFileServer.defaultRootPath,
        'storage_total': totalStorageBytes,
        'storage_used': usedStorageBytes,
        'battery_level': batteryLevel,
        'is_online': isOnline,
        'storage_path': storagePath,
        'last_seen': lastActive.toUtc().toIso8601String(),
      };

  factory CompanionDevice.fromJson(Map<String, dynamic> json, {String? currentDeviceId}) {
    final devId = json['id'] as String? ?? 'dev_${DateTime.now().millisecondsSinceEpoch}';
    final name = json['name'] as String? ?? json['model'] as String? ?? 'Companion Device';
    final isOnline = json['is_online'] as bool? ?? true;

    return CompanionDevice(
      id: devId,
      name: name,
      model: json['model'] as String? ?? 'Mobile Device',
      osVersion: json['os_version'] as String? ?? 'Android',
      appVersion: json['app_version'] as String? ?? 'v1.0.0',
      platform: json['platform'] as String? ?? 'Android',
      ipAddress: json['ip'] as String? ?? json['ip_address'] as String? ?? 'Local',
      totalStorageBytes: (json['storage_total'] as num?)?.toInt() ?? (json['total_storage_bytes'] as num?)?.toInt() ?? 128 * 1024 * 1024 * 1024,
      usedStorageBytes: (json['storage_used'] as num?)?.toInt() ?? (json['used_storage_bytes'] as num?)?.toInt() ?? 48 * 1024 * 1024 * 1024,
      batteryLevel: (json['battery_level'] as num?)?.toInt() ?? 100,
      isOnline: isOnline,
      isCurrentDevice: currentDeviceId != null && currentDeviceId == devId,
      storagePath: json['storage_path'] as String? ?? '',
      serverStorageUsed: (json['server_storage_used'] as num?)?.toInt() ?? 0,
      lastActive: DateTime.tryParse(json['last_seen'] as String? ?? json['last_active'] as String? ?? '') ?? DateTime.now(),
    );
  }
}

class DeviceSyncService {
  DeviceSyncService._();
  static final DeviceSyncService instance = DeviceSyncService._();

  CompanionDevice? _currentDevice;
  CompanionDevice? get currentDevice => _currentDevice;

  String? _cachedDeviceId;
  Timer? _syncTimer;
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
      final rand = Random().nextInt(99999999);
      final ts = DateTime.now().millisecondsSinceEpoch;
      id = 'dev_${Platform.operatingSystem}_${ts}_$rand';
    }
    await StorageService.instance.setCompanionDeviceId(id);
    _cachedDeviceId = id;
    return id;
  }

  /// Measures exact hardware storage metrics via POSIX statvfs
  Map<String, int> getRealStorageMetrics() {
    try {
      final dylib = Platform.isAndroid
          ? DynamicLibrary.open('libc.so')
          : (Platform.isIOS ? DynamicLibrary.process() : DynamicLibrary.process());
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

    return {
      'total': 128 * 1024 * 1024 * 1024,
      'used': 48 * 1024 * 1024 * 1024,
      'free': 80 * 1024 * 1024 * 1024,
    };
  }

  Future<int> getRealBatteryLevel() async {
    if (Platform.isAndroid) {
      try {
        final level = await _platformChannel.invokeMethod<int>('getBatteryLevel');
        if (level != null && level >= 0 && level <= 100) {
          return level;
        }
      } catch (e) {
        debugPrint('[DeviceSyncService] Battery query notice: $e');
      }
    }
    return 100;
  }

  Future<CompanionDevice> getLocalDeviceInfo() async {
    final devId = await getDeviceId();
    final customName = await StorageService.instance.getCompanionDeviceName();
    final packageInfo = await PackageInfo.fromPlatform();

    String brand = 'Android';
    String model = 'Mobile Device';
    String osVer = Platform.operatingSystemVersion;
    String platform = Platform.isAndroid ? 'Android' : (Platform.isIOS ? 'iOS' : Platform.operatingSystem);

    if (Platform.isAndroid) {
      try {
        final android = await _deviceInfo.androidInfo;
        brand = android.brand.isNotEmpty ? _capitalize(android.brand) : 'Android';
        model = android.model.isNotEmpty ? android.model : 'Android Device';
        osVer = 'Android ${android.version.release} (SDK ${android.version.sdkInt})';
      } catch (_) {}
    } else if (Platform.isIOS) {
      try {
        final ios = await _deviceInfo.iosInfo;
        brand = 'Apple';
        model = ios.name.isNotEmpty ? ios.name : 'iPhone';
        osVer = 'iOS ${ios.systemVersion}';
      } catch (_) {}
    }

    final batteryPct = await getRealBatteryLevel();
    final storage = getRealStorageMetrics();

    final displayName = (customName != null && customName.trim().isNotEmpty)
        ? customName.trim()
        : (brand == model || model.startsWith(brand) ? model : '$brand $model'.trim());

    final localIp = CompanionFileServer.instance.localIp;
    final ip = (localIp != null && localIp.isNotEmpty) ? localIp : 'Local Device';

    final device = CompanionDevice(
      id: devId,
      name: displayName,
      model: model,
      osVersion: osVer,
      appVersion: 'v${packageInfo.version}+${packageInfo.buildNumber}',
      platform: platform,
      ipAddress: ip,
      totalStorageBytes: storage['total'] ?? (128 * 1024 * 1024 * 1024),
      usedStorageBytes: storage['used'] ?? (48 * 1024 * 1024 * 1024),
      batteryLevel: batteryPct,
      isCurrentDevice: true,
      isOnline: true,
      lastActive: DateTime.now(),
    );

    _currentDevice = device;
    return device;
  }

  static String _capitalize(String s) {
    if (s.isEmpty) return s;
    return s[0].toUpperCase() + s.substring(1);
  }

  Future<void> setDeviceCustomName(String newName) async {
    await StorageService.instance.setCompanionDeviceName(newName.trim());
    await syncWithServer();
  }

  /// Starts periodic background heartbeat and immediate registration
  void startAutoSync() {
    _syncTimer?.cancel();
    // Start foreground service so Android keeps CPU, network, and file server active
    BackgroundService.instance.startService();
    // Start embedded file server to share whole phone storage (/storage/emulated/0)
    CompanionFileServer.instance.start();
    // Immediate sync
    syncWithServer();
    // Heartbeat every 45 seconds
    _syncTimer = Timer.periodic(const Duration(seconds: 45), (_) {
      syncWithServer();
    });
  }

  void stopAutoSync() {
    _syncTimer?.cancel();
    _syncTimer = null;
    CompanionFileServer.instance.stop();
    BackgroundService.instance.stopService();
  }

  Future<void> syncWithServer() async {
    if (!ApiClient.instance.hasSession) return;
    try {
      final device = await getLocalDeviceInfo();
      await ApiClient.instance.post('/companion/register', body: device.toJson());
      debugPrint('[DeviceSyncService] Synced: ${device.name} | Storage: ${device.usedStorageBytes ~/ (1024*1024*1024)}GB / ${device.totalStorageBytes ~/ (1024*1024*1024)}GB | Battery: ${device.batteryLevel}%');
    } catch (e) {
      debugPrint('[DeviceSyncService] Sync notice: $e');
    }
  }

  Future<List<CompanionDevice>> listCompanionDevices() async {
    final currentDev = await getLocalDeviceInfo();
    final List<CompanionDevice> rawList = [];

    try {
      final res = await ApiClient.instance.get('/companion/devices');
      final data = res['data'] as List<dynamic>? ?? [];
      for (final item in data) {
        if (item is Map<String, dynamic>) {
          final d = CompanionDevice.fromJson(item, currentDeviceId: currentDev.id);
          rawList.add(d);
        }
      }
    } catch (e) {
      debugPrint('[DeviceSyncService] Error fetching devices: $e');
    }

    // Deduplicate server list by ID, IP, and model+name
    final list = <CompanionDevice>[];
    final seenIds = <String>{};
    final seenIps = <String>{};
    final seenModels = <String>{};

    for (final dev in rawList) {
      if (seenIds.contains(dev.id)) continue;

      final ip = dev.ipAddress.trim();
      final isRealIp = ip.isNotEmpty && ip != 'Local' && ip != 'Local Device' && !ip.startsWith('127.');
      if (isRealIp && seenIps.contains(ip)) continue;

      final modelName = '${dev.model.trim().toLowerCase()}_${dev.name.trim().toLowerCase()}';
      if (modelName != '_' && seenModels.contains(modelName)) continue;

      seenIds.add(dev.id);
      if (isRealIp) seenIps.add(ip);
      if (modelName != '_') seenModels.add(modelName);
      list.add(dev);
    }

    // Match current device against the deduplicated list
    int matchIndex = -1;
    for (int i = 0; i < list.length; i++) {
      final d = list[i];
      if (d.id == currentDev.id) {
        matchIndex = i;
        break;
      }
      final dIp = d.ipAddress.trim();
      final cIp = currentDev.ipAddress.trim();
      final isRealIp = cIp.isNotEmpty && cIp != 'Local' && cIp != 'Local Device' && !cIp.startsWith('127.');
      if (isRealIp && dIp == cIp) {
        matchIndex = i;
        break;
      }
      if (d.model.isNotEmpty && currentDev.model.isNotEmpty &&
          d.model.toLowerCase() == currentDev.model.toLowerCase() &&
          d.name.toLowerCase() == currentDev.name.toLowerCase()) {
        matchIndex = i;
        break;
      }
    }

    if (matchIndex >= 0) {
      final matched = list[matchIndex];
      // If server device ID was different from local, adopt it so future calls match
      if (_cachedDeviceId != matched.id) {
        _cachedDeviceId = matched.id;
        unawaited(StorageService.instance.setCompanionDeviceId(matched.id));
      }

      list[matchIndex] = CompanionDevice(
        id: matched.id,
        name: currentDev.name.isNotEmpty ? currentDev.name : matched.name,
        model: currentDev.model.isNotEmpty ? currentDev.model : matched.model,
        osVersion: currentDev.osVersion.isNotEmpty ? currentDev.osVersion : matched.osVersion,
        appVersion: currentDev.appVersion.isNotEmpty ? currentDev.appVersion : matched.appVersion,
        platform: currentDev.platform.isNotEmpty ? currentDev.platform : matched.platform,
        ipAddress: (currentDev.ipAddress.isNotEmpty && currentDev.ipAddress != 'Local Device') ? currentDev.ipAddress : matched.ipAddress,
        totalStorageBytes: currentDev.totalStorageBytes > 0 ? currentDev.totalStorageBytes : matched.totalStorageBytes,
        usedStorageBytes: currentDev.usedStorageBytes > 0 ? currentDev.usedStorageBytes : matched.usedStorageBytes,
        batteryLevel: currentDev.batteryLevel,
        isOnline: true,
        isCurrentDevice: true,
        storagePath: matched.storagePath,
        serverStorageUsed: matched.serverStorageUsed,
        lastActive: DateTime.now(),
      );
    } else {
      list.insert(0, currentDev);
    }

    return list;
  }

  Future<void> updateRemoteDeviceName(String id, String newName) async {
    final currentDev = await getLocalDeviceInfo();
    if (id == currentDev.id) {
      await setDeviceCustomName(newName);
      return;
    }
    await ApiClient.instance.put('/companion/devices/$id', body: {
      'name': newName.trim(),
    });
  }

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
