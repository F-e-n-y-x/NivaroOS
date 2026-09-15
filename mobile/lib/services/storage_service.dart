import 'dart:convert';
import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:path_provider/path_provider.dart';
import '../models/server_profile.dart';

/// Everything this app needs to remember between launches - multi-server profiles,
/// active server connection, and credentials - kept in the platform secure
/// storage (Android Keystore-backed EncryptedSharedPreferences / iOS
/// Keychain), cached in memory for synchronous-feeling reads after init().
///
/// This used to write everything (including access/refresh tokens and every
/// saved server profile's credentials) to a plain JSON file in the app's
/// documents directory - trivially readable via `adb backup` or on a rooted
/// device despite flutter_secure_storage already being a declared dependency
/// that was never actually wired up. init() migrates any such legacy file
/// into secure storage once, then deletes it.
class StorageService {
  StorageService._();
  static final StorageService instance = StorageService._();

  static const _secureStorage = FlutterSecureStorage(
    aOptions: AndroidOptions(encryptedSharedPreferences: true),
  );

  final Map<String, String> _cache = {};
  bool _initialized = false;

  static const _keyServerUrl = 'server_url';
  static const _keyAccessToken = 'access_token';
  static const _keyRefreshToken = 'refresh_token';
  static const _keyUsername = 'username';
  static const _keyServerProfiles = 'saved_server_profiles';
  static const _keyActiveProfileId = 'active_profile_id';
  static const _keyCompanionDeviceName = 'companion_device_name';
  static const _keyCompanionDeviceId = 'companion_device_id';
  static const _keyCompanionSecret = 'companion_secret';

  Future<void> init() async {
    if (_initialized) return;
    try {
      final all = await _secureStorage.readAll();
      _cache.addAll(all);
      await _migrateLegacyFileIfPresent();
    } catch (e) {
      debugPrint('[StorageService] Error loading secure storage: $e');
    }
    _initialized = true;
  }

  // One-time migration from the old plaintext `nivaroos_storage.json` (pre
  // secure-storage versions of this app). Safe to call repeatedly - it's a
  // no-op once the file is gone.
  Future<void> _migrateLegacyFileIfPresent() async {
    try {
      final dir = await getApplicationDocumentsDirectory();
      final legacyFile = File('${dir.path}/nivaroos_storage.json');
      if (!await legacyFile.exists()) return;
      final content = await legacyFile.readAsString();
      if (content.trim().isNotEmpty) {
        final data = jsonDecode(content) as Map<String, dynamic>;
        for (final entry in data.entries) {
          if (entry.value == null) continue;
          final value = entry.value.toString();
          // Don't clobber a value secure storage already has (e.g. if a
          // previous migration attempt partially completed).
          if (_cache.containsKey(entry.key)) continue;
          await _secureStorage.write(key: entry.key, value: value);
          _cache[entry.key] = value;
        }
      }
      await legacyFile.delete();
      debugPrint('[StorageService] Migrated legacy plaintext storage into secure storage');
    } catch (e) {
      debugPrint('[StorageService] Legacy storage migration skipped: $e');
    }
  }

  Future<void> _set(String key, String value) async {
    _cache[key] = value;
    await _secureStorage.write(key: key, value: value);
  }

  Future<void> _remove(String key) async {
    _cache.remove(key);
    await _secureStorage.delete(key: key);
  }

  Future<String?> getCompanionDeviceName() async {
    if (!_initialized) await init();
    return _cache[_keyCompanionDeviceName];
  }

  Future<void> setCompanionDeviceName(String name) async {
    if (!_initialized) await init();
    await _set(_keyCompanionDeviceName, name);
  }

  Future<String?> getCompanionDeviceId() async {
    if (!_initialized) await init();
    return _cache[_keyCompanionDeviceId];
  }

  Future<void> setCompanionDeviceId(String id) async {
    if (!_initialized) await init();
    await _set(_keyCompanionDeviceId, id);
  }

  // Shared secret established once, over the already-JWT-authenticated
  // registration call (see PostRegisterCompanionDevice on the server) - sent
  // as the X-Companion-Secret header on every request the server makes
  // directly to this device's embedded file server (CompanionFileServer),
  // which has no other way to authenticate a LAN caller.
  Future<String?> getCompanionSecret() async {
    if (!_initialized) await init();
    return _cache[_keyCompanionSecret];
  }

  Future<void> setCompanionSecret(String secret) async {
    if (!_initialized) await init();
    await _set(_keyCompanionSecret, secret);
  }

  Future<String?> getServerUrl() async {
    if (!_initialized) await init();
    return _cache[_keyServerUrl];
  }

  Future<void> setServerUrl(String url) async {
    if (!_initialized) await init();
    await _set(_keyServerUrl, url);
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
    await _set(_keyAccessToken, accessToken);
    await _set(_keyRefreshToken, refreshToken);
    await _set(_keyUsername, username);

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
    await _remove(_keyAccessToken);
    await _remove(_keyRefreshToken);
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
        await _set(_keyServerProfiles, raw);
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
    await _set(_keyServerProfiles, raw);
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
    await _set(_keyServerProfiles, raw);
  }

  Future<String?> getActiveProfileId() async {
    if (!_initialized) await init();
    return _cache[_keyActiveProfileId];
  }

  Future<void> setActiveProfileId(String id) async {
    if (!_initialized) await init();
    await _set(_keyActiveProfileId, id);
  }

  Future<void> switchProfile(ServerProfile profile) async {
    if (!_initialized) await init();
    await _set(_keyActiveProfileId, profile.id);
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
    await _secureStorage.deleteAll();
  }
}
