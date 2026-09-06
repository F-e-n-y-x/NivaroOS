import 'dart:convert';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import '../models/server_profile.dart';

/// Everything this app needs to remember between launches - multi-server profiles,
/// active server connection, and credentials - kept in the platform keystore.
class StorageService {
  StorageService._();
  static final StorageService instance = StorageService._();

  final _storage = const FlutterSecureStorage();

  static const _keyServerUrl = 'server_url';
  static const _keyAccessToken = 'access_token';
  static const _keyRefreshToken = 'refresh_token';
  static const _keyUsername = 'username';
  static const _keyServerProfiles = 'saved_server_profiles';
  static const _keyActiveProfileId = 'active_profile_id';
  static const _keyCompanionDeviceName = 'companion_device_name';
  static const _keyCompanionDeviceId = 'companion_device_id';

  Future<String?> getCompanionDeviceName() => _storage.read(key: _keyCompanionDeviceName);
  Future<void> setCompanionDeviceName(String name) => _storage.write(key: _keyCompanionDeviceName, value: name);

  Future<String?> getCompanionDeviceId() => _storage.read(key: _keyCompanionDeviceId);
  Future<void> setCompanionDeviceId(String id) => _storage.write(key: _keyCompanionDeviceId, value: id);


  Future<String?> getServerUrl() => _storage.read(key: _keyServerUrl);
  Future<void> setServerUrl(String url) => _storage.write(key: _keyServerUrl, value: url);

  Future<String?> getAccessToken() => _storage.read(key: _keyAccessToken);
  Future<String?> getRefreshToken() => _storage.read(key: _keyRefreshToken);
  Future<String?> getUsername() => _storage.read(key: _keyUsername);

  Future<void> setSession({
    required String accessToken,
    required String refreshToken,
    required String username,
  }) async {
    await _storage.write(key: _keyAccessToken, value: accessToken);
    await _storage.write(key: _keyRefreshToken, value: refreshToken);
    await _storage.write(key: _keyUsername, value: username);
  }

  Future<void> clearSession() async {
    await _storage.delete(key: _keyAccessToken);
    await _storage.delete(key: _keyRefreshToken);
    await _storage.delete(key: _keyUsername);
  }

  // --- Multi-Server Profiles Management ---

  Future<List<ServerProfile>> getProfiles() async {
    try {
      final raw = await _storage.read(key: _keyServerProfiles);
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
        await _storage.write(key: _keyServerProfiles, value: raw);
        return [defaultProfile];
      }
    } catch (_) {}
    return [];
  }

  Future<void> saveProfile(ServerProfile profile) async {
    List<ServerProfile> profiles = [];
    try {
      final raw = await _storage.read(key: _keyServerProfiles);
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
    await _storage.write(key: _keyServerProfiles, value: raw);
  }

  Future<void> deleteProfile(String id) async {
    List<ServerProfile> profiles = [];
    try {
      final raw = await _storage.read(key: _keyServerProfiles);
      if (raw != null && raw.isNotEmpty) {
        final List<dynamic> list = jsonDecode(raw);
        profiles = list.map((e) => ServerProfile.fromJson(e as Map<String, dynamic>)).toList();
      }
    } catch (_) {}
    profiles.removeWhere((p) => p.id == id);
    final raw = jsonEncode(profiles.map((p) => p.toJson()).toList());
    await _storage.write(key: _keyServerProfiles, value: raw);
  }

  Future<String?> getActiveProfileId() => _storage.read(key: _keyActiveProfileId);

  Future<void> switchProfile(ServerProfile profile) async {
    await _storage.write(key: _keyActiveProfileId, value: profile.id);
    await setServerUrl(profile.url);
    if (profile.accessToken != null && profile.refreshToken != null) {
      await setSession(
        accessToken: profile.accessToken!,
        refreshToken: profile.refreshToken!,
        username: profile.username,
      );
    }
    // Update last connected time
    final updated = profile.copyWith(lastConnected: DateTime.now());
    await saveProfile(updated);
  }

  Future<void> clearAll() async {
    await _storage.deleteAll();
  }
}
