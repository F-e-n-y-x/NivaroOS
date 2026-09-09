import 'dart:convert';
import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:path_provider/path_provider.dart';
import '../models/server_profile.dart';

/// Everything this app needs to remember between launches - multi-server profiles,
/// active server connection, and credentials - kept in local app storage and cached in memory.
class StorageService {
  StorageService._();
  static final StorageService instance = StorageService._();

  final Map<String, String> _cache = {};
  bool _initialized = false;
  File? _file;

  static const _keyServerUrl = 'server_url';
  static const _keyAccessToken = 'access_token';
  static const _keyRefreshToken = 'refresh_token';
  static const _keyUsername = 'username';
  static const _keyServerProfiles = 'saved_server_profiles';
  static const _keyActiveProfileId = 'active_profile_id';
  static const _keyCompanionDeviceName = 'companion_device_name';
  static const _keyCompanionDeviceId = 'companion_device_id';

  Future<void> init() async {
    if (_initialized) return;
    try {
      final dir = await getApplicationDocumentsDirectory();
      _file = File('${dir.path}/nivaroos_storage.json');
      if (await _file!.exists()) {
        final content = await _file!.readAsString();
        if (content.trim().isNotEmpty) {
          final data = jsonDecode(content) as Map<String, dynamic>;
          for (final entry in data.entries) {
            if (entry.value != null) {
              _cache[entry.key] = entry.value.toString();
            }
          }
        }
      }
    } catch (e) {
      debugPrint('[StorageService] Error loading storage file: $e');
    }
    _initialized = true;
  }

  Future<void> _persist() async {
    try {
      if (_file == null) {
        final dir = await getApplicationDocumentsDirectory();
        _file = File('${dir.path}/nivaroos_storage.json');
      }
      await _file!.writeAsString(jsonEncode(_cache), flush: true);
    } catch (e) {
      debugPrint('[StorageService] Error saving storage file: $e');
    }
  }

  Future<String?> getCompanionDeviceName() async {
    if (!_initialized) await init();
    return _cache[_keyCompanionDeviceName];
  }

  Future<void> setCompanionDeviceName(String name) async {
    if (!_initialized) await init();
    _cache[_keyCompanionDeviceName] = name;
    await _persist();
  }

  Future<String?> getCompanionDeviceId() async {
    if (!_initialized) await init();
    return _cache[_keyCompanionDeviceId];
  }

  Future<void> setCompanionDeviceId(String id) async {
    if (!_initialized) await init();
    _cache[_keyCompanionDeviceId] = id;
    await _persist();
  }

  Future<String?> getServerUrl() async {
    if (!_initialized) await init();
    return _cache[_keyServerUrl];
  }

  Future<void> setServerUrl(String url) async {
    if (!_initialized) await init();
    _cache[_keyServerUrl] = url;
    await _persist();
  }

  Future<String?> getAccessToken() async {
    if (!_initialized) await init();
    return _cache[_keyAccessToken];
  }

  Future<String?> getRefreshToken() async {
    if (!_initialized) await init();
    return _cache[_keyRefreshToken];
  }

  Future<String?> getUsername() async {
    if (!_initialized) await init();
    return _cache[_keyUsername];
  }

  Future<void> setSession({
    required String accessToken,
    required String refreshToken,
    required String username,
  }) async {
    if (!_initialized) await init();
    _cache[_keyAccessToken] = accessToken;
    _cache[_keyRefreshToken] = refreshToken;
    _cache[_keyUsername] = username;
    await _persist();

    // Keep active server profile in sync
    try {
      final currentUrl = await getServerUrl();
      if (currentUrl != null && currentUrl.isNotEmpty) {
        final profiles = await getProfiles();
        final idx = profiles.indexWhere((p) => p.url.trim() == currentUrl.trim() || p.id == 'default');
        if (idx >= 0) {
          final updated = profiles[idx].copyWith(
            accessToken: accessToken,
            refreshToken: refreshToken,
            username: username,
            lastConnected: DateTime.now(),
          );
          await saveProfile(updated);
        } else {
          final newProfile = ServerProfile(
            id: currentUrl,
            name: 'Primary Server',
            url: currentUrl,
            username: username,
            accessToken: accessToken,
            refreshToken: refreshToken,
            lastConnected: DateTime.now(),
          );
          await saveProfile(newProfile);
        }
      }
    } catch (_) {}
  }

  Future<void> clearSession() async {
    if (!_initialized) await init();
    _cache.remove(_keyAccessToken);
    _cache.remove(_keyRefreshToken);
    await _persist();
  }

  // --- Multi-Server Profiles Management ---

  Future<List<ServerProfile>> getProfiles() async {
    if (!_initialized) await init();
    try {
      final raw = _cache[_keyServerProfiles];
      if (raw != null && raw.isNotEmpty) {
        final List<dynamic> list = jsonDecode(raw);
        final profiles = list.map((e) => ServerProfile.fromJson(e as Map<String, dynamic>)).toList();
        if (profiles.isNotEmpty) return profiles;
      }
    } catch (_) {}

    // Fallback: create default profile from current active server if available
    try {
      final currentUrl = await getServerUrl();
      final currentUsername = await getUsername();
      if (currentUrl != null && currentUrl.isNotEmpty) {
        final defaultProfile = ServerProfile(
          id: 'default',
          name: 'Primary Server',
          url: currentUrl,
          username: currentUsername ?? 'Admin',
          accessToken: await getAccessToken(),
          refreshToken: await getRefreshToken(),
          lastConnected: DateTime.now(),
        );
        final raw = jsonEncode([defaultProfile.toJson()]);
        _cache[_keyServerProfiles] = raw;
        await _persist();
        return [defaultProfile];
      }
    } catch (_) {}
    return [];
  }

  Future<void> saveProfile(ServerProfile profile) async {
    if (!_initialized) await init();
    List<ServerProfile> profiles = [];
    try {
      final raw = _cache[_keyServerProfiles];
      if (raw != null && raw.isNotEmpty) {
        final List<dynamic> list = jsonDecode(raw);
        profiles = list.map((e) => ServerProfile.fromJson(e as Map<String, dynamic>)).toList();
      }
    } catch (_) {}

    final idx = profiles.indexWhere((p) => p.id == profile.id || p.url.trim() == profile.url.trim());
    if (idx >= 0) {
      profiles[idx] = profile;
    } else {
      profiles.add(profile);
    }

    final raw = jsonEncode(profiles.map((p) => p.toJson()).toList());
    _cache[_keyServerProfiles] = raw;
    await _persist();
  }

  Future<void> deleteProfile(String id) async {
    if (!_initialized) await init();
    List<ServerProfile> profiles = [];
    try {
      final raw = _cache[_keyServerProfiles];
      if (raw != null && raw.isNotEmpty) {
        final List<dynamic> list = jsonDecode(raw);
        profiles = list.map((e) => ServerProfile.fromJson(e as Map<String, dynamic>)).toList();
      }
    } catch (_) {}

    profiles.removeWhere((p) => p.id == id);
    final raw = jsonEncode(profiles.map((p) => p.toJson()).toList());
    _cache[_keyServerProfiles] = raw;
    await _persist();
  }

  Future<String?> getActiveProfileId() async {
    if (!_initialized) await init();
    return _cache[_keyActiveProfileId];
  }

  Future<void> setActiveProfileId(String id) async {
    if (!_initialized) await init();
    _cache[_keyActiveProfileId] = id;
    await _persist();
  }

  Future<void> switchProfile(ServerProfile profile) async {
    if (!_initialized) await init();
    _cache[_keyActiveProfileId] = profile.id;
    await setServerUrl(profile.url);
    if (profile.accessToken != null && profile.refreshToken != null) {
      await setSession(
        accessToken: profile.accessToken!,
        refreshToken: profile.refreshToken!,
        username: profile.username,
      );
    }
    final updated = profile.copyWith(lastConnected: DateTime.now());
    await saveProfile(updated);
  }

  Future<void> clearAll() async {
    if (!_initialized) await init();
    _cache.clear();
    await _persist();
  }
}
