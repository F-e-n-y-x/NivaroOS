import 'dart:convert';
import 'dart:io';
import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:path_provider/path_provider.dart';
import '../models/server_profile.dart';

/// Everything this app needs to remember between launches - multi-server profiles,
/// active server connection, and credentials - kept in the platform secure
/// storage (Android Keystore-wrapped AES-GCM / iOS Keychain), cached in
/// memory for synchronous-feeling reads after init().
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

  // Builds up to 1.2.1 used flutter_secure_storage 9.x with
  // EncryptedSharedPreferences. 10.x re-encrypts that data into its own
  // cipher storage on first access (migrateOnAlgorithmChange, on by
  // default), so the default options keep every saved token. 11.x drops
  // that migration - see the pin in pubspec.yaml.
  static const _secureStorage = FlutterSecureStorage(
    aOptions: AndroidOptions(),
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
  static const _keyThemeMode = 'theme_mode';
  static const _keyThemeAccent = 'theme_accent';
  static const _keyThemeWallpaper = 'theme_wallpaper';
  static const _keyDesignDirection = 'design_direction';
  static const _keyWidgetRefresh = 'widget_refresh';
  // Per server: the secret the server gave this phone, keyed by server URL
  // ("companion_secret@http://nas.local"), so switching servers never sends
  // one server's secret to another (plan M-17).
  static const _keyCompanionSecretPrefix = 'companion_secret@';
  static const _keyCompanionRemovedPrefix = 'companion_removed@';
  static const _keyNotificationsAsked = 'notifications_asked';
  static const _keyShareMinutes = 'share_minutes';
  static const _keyDoneMigrations = 'done_migrations';
  static const _keyUpdateCheck = 'app_update_check';

  // Display preferences, not account data: clearAll() (sign out) keeps them.
  static const _preservedKeys = {_keyThemeMode, _keyThemeAccent, _keyThemeWallpaper, _keyDesignDirection, _keyWidgetRefresh, _keyNotificationsAsked};

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

  // Shared secret established over the already-JWT-authenticated
  // registration call (see PostRegisterCompanionDevice on the server) - sent
  // as the X-Companion-Secret header on every request the server makes
  // directly to this device's embedded file server (CompanionFileServer),
  // which has no other way to authenticate a LAN caller. One per server.
  Future<String?> getCompanionSecret() async {
    if (!_initialized) await init();
    final url = _cache[_keyServerUrl];
    if (url == null || url.isEmpty) return null;
    final key = '$_keyCompanionSecretPrefix${_serverKey(url)}';
    final own = _cache[key];
    if (own != null && own.isNotEmpty) return own;
    // Builds before 1.3 kept one secret for whichever server was active;
    // it belongs to the current one.
    final legacy = _cache[_keyCompanionSecret];
    if (legacy != null && legacy.isNotEmpty) {
      await _set(key, legacy);
      await _remove(_keyCompanionSecret);
      return legacy;
    }
    return null;
  }

  /// Whether the current server removed this phone (from its settings):
  /// the heartbeat then stays quiet until the user pairs it again.
  Future<bool> getCompanionRemoved() async {
    if (!_initialized) await init();
    final url = _cache[_keyServerUrl];
    if (url == null || url.isEmpty) return false;
    return _cache['$_keyCompanionRemovedPrefix${_serverKey(url)}'] == '1';
  }

  Future<void> setCompanionRemoved(bool removed) async {
    if (!_initialized) await init();
    final url = _cache[_keyServerUrl];
    if (url == null || url.isEmpty) return;
    final key = '$_keyCompanionRemovedPrefix${_serverKey(url)}';
    if (removed) {
      await _set(key, '1');
    } else {
      await _remove(key);
    }
  }

  /// Forgets the pairing secret for the current server.
  Future<void> clearCompanionSecret() async {
    if (!_initialized) await init();
    final url = _cache[_keyServerUrl];
    if (url == null || url.isEmpty) return;
    await _remove('$_keyCompanionSecretPrefix${_serverKey(url)}');
  }

  Future<void> setCompanionSecret(String secret) async {
    if (!_initialized) await init();
    final url = _cache[_keyServerUrl];
    if (url == null || url.isEmpty) return;
    await _set('$_keyCompanionSecretPrefix${_serverKey(url)}', secret);
  }

  static String _serverKey(String url) {
    var s = url.trim().toLowerCase();
    while (s.endsWith('/')) {
      s = s.substring(0, s.length - 1);
    }
    return s;
  }

  /// Whether the app has already asked for the notification permission
  /// (it asks once, when a feature needs it, never at launch).
  Future<bool> getNotificationsAsked() async {
    if (!_initialized) await init();
    return _cache[_keyNotificationsAsked] == 'true';
  }

  Future<void> setNotificationsAsked() async {
    if (!_initialized) await init();
    await _set(_keyNotificationsAsked, 'true');
  }

  /// How long the last storage-sharing session was, in minutes.
  Future<int?> getShareMinutes() async {
    if (!_initialized) await init();
    return int.tryParse(_cache[_keyShareMinutes] ?? '');
  }

  Future<void> setShareMinutes(int minutes) async {
    if (!_initialized) await init();
    await _set(_keyShareMinutes, '$minutes');
  }

  /// One-time clean-ups already done, by name (a server-specific one
  /// includes the server URL in its name).
  Future<bool> isMigrationDone(String name) async {
    if (!_initialized) await init();
    return (_cache[_keyDoneMigrations] ?? '').split('\n').contains(name);
  }

  Future<void> markMigrationDone(String name) async {
    if (!_initialized) await init();
    final done = (_cache[_keyDoneMigrations] ?? '').split('\n').where((e) => e.isNotEmpty).toSet()..add(name);
    await _set(_keyDoneMigrations, done.join('\n'));
  }

  /// The last app-update check (JSON written by AppUpdateService).
  Future<String?> getUpdateCheck() async {
    if (!_initialized) await init();
    return _cache[_keyUpdateCheck];
  }

  Future<void> setUpdateCheck(String json) async {
    if (!_initialized) await init();
    await _set(_keyUpdateCheck, json);
  }

  /// 'system', 'light', 'dark' or 'black'; null until the user picks one.
  Future<String?> getThemeMode() async {
    if (!_initialized) await init();
    return _cache[_keyThemeMode];
  }

  Future<void> setThemeMode(String mode) async {
    if (!_initialized) await init();
    await _set(_keyThemeMode, mode);
  }

  /// The accent colour's name (AccentColor); null until the user picks one.
  Future<String?> getThemeAccent() async {
    if (!_initialized) await init();
    return _cache[_keyThemeAccent];
  }

  Future<void> setThemeAccent(String accent) async {
    if (!_initialized) await init();
    await _set(_keyThemeAccent, accent);
  }

  /// 'true' when the accent follows the phone's wallpaper.
  Future<String?> getThemeWallpaper() async {
    if (!_initialized) await init();
    return _cache[_keyThemeWallpaper];
  }

  Future<void> setThemeWallpaper(bool on) async {
    if (!_initialized) await init();
    await _set(_keyThemeWallpaper, on.toString());
  }

  /// The design direction's name (DesignDirection); null for the default.
  Future<String?> getDesignDirection() async {
    if (!_initialized) await init();
    return _cache[_keyDesignDirection];
  }

  Future<void> setDesignDirection(String direction) async {
    if (!_initialized) await init();
    await _set(_keyDesignDirection, direction);
  }

  /// How often Home's widgets refresh (WidgetRefresh's name); null for the
  /// default.
  Future<String?> getWidgetRefresh() async {
    if (!_initialized) await init();
    return _cache[_keyWidgetRefresh];
  }

  Future<void> setWidgetRefresh(String refresh) async {
    if (!_initialized) await init();
    await _set(_keyWidgetRefresh, refresh);
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

  /// Reads the tokens again from secure storage, bypassing this isolate's
  /// cache: the UI, the sharing service and the heartbeat job each run
  /// their own copy of this class, and one of them may have refreshed the
  /// session since this one loaded it.
  Future<({String accessToken, String refreshToken})?> reloadSession() async {
    if (!_initialized) await init();
    try {
      final access = await _secureStorage.read(key: _keyAccessToken);
      final refresh = await _secureStorage.read(key: _keyRefreshToken);
      if (access == null || refresh == null) return null;
      _cache[_keyAccessToken] = access;
      _cache[_keyRefreshToken] = refresh;
      return (accessToken: access, refreshToken: refresh);
    } catch (_) {
      return null;
    }
  }

  /// Whether secure storage still holds a session, read fresh (not from
  /// this engine's cache): false once the user signed out in another
  /// engine. A read error counts as "still there", so a flaky Keystore
  /// never drops a session.
  Future<bool> hasStoredSession() async {
    try {
      final refresh = await _secureStorage.read(key: _keyRefreshToken);
      return refresh != null && refresh.isNotEmpty;
    } catch (_) {
      return true;
    }
  }

  /// The saved server list as secure storage has it now. The UI, the
  /// sharing service and the heartbeat job each keep their own cache, and
  /// the sharing engine can run for hours, so a change is always made to
  /// the stored list, never to a stale in-memory copy (review finding 8).
  Future<String?> _freshProfilesRaw() async {
    try {
      final raw = await _secureStorage.read(key: _keyServerProfiles);
      if (raw == null) {
        _cache.remove(_keyServerProfiles);
      } else {
        _cache[_keyServerProfiles] = raw;
      }
      final active = await _secureStorage.read(key: _keyActiveProfileId);
      if (active == null) {
        _cache.remove(_keyActiveProfileId);
      } else {
        _cache[_keyActiveProfileId] = active;
      }
    } catch (_) {}
    return _cache[_keyServerProfiles];
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

    // Keep the active server profile in sync, so switching away and back
    // later uses the newest tokens, not the ones from sign-in (plan M-17).
    try {
      final currentUrl = await getServerUrl();
      if (currentUrl != null && currentUrl.isNotEmpty) {
        await _freshProfilesRaw();
        final profiles = await getProfiles();
        final activeId = _cache[_keyActiveProfileId];
        var idx = activeId == null ? -1 : profiles.indexWhere((p) => p.id == activeId && _sameUrl(p.url, currentUrl));
        if (idx < 0) idx = profiles.indexWhere((p) => _sameUrl(p.url, currentUrl));
        if (idx >= 0) {
          final updated = profiles[idx].copyWith(
            accessToken: accessToken,
            refreshToken: refreshToken,
            username: username,
            lastConnected: clock.now(),
          );
          await saveProfile(updated);
          await _set(_keyActiveProfileId, updated.id);
        } else {
          final newProfile = ServerProfile(
            id: 'srv_${clock.now().millisecondsSinceEpoch}',
            name: ServerProfile.defaultName(currentUrl),
            url: currentUrl,
            username: username,
            accessToken: accessToken,
            refreshToken: refreshToken,
            lastConnected: clock.now(),
          );
          await saveProfile(newProfile);
          await _set(_keyActiveProfileId, newProfile.id);
        }
      }
    } catch (_) {}
  }

  static bool _sameUrl(String a, String b) => _serverKey(a) == _serverKey(b);

  /// Drops the current tokens only, leaving the saved profiles as they
  /// are - for pointing the app at another server while the old one's
  /// profile keeps its session for switching back.
  Future<void> clearSessionTokensOnly() async {
    if (!_initialized) await init();
    await _remove(_keyAccessToken);
    await _remove(_keyRefreshToken);
  }

  /// Ends the session: the tokens go, from the active server profile too
  /// (they no longer work, and switching back must ask to sign in).
  Future<void> clearSession() async {
    if (!_initialized) await init();
    await _remove(_keyAccessToken);
    await _remove(_keyRefreshToken);
    try {
      final currentUrl = _cache[_keyServerUrl];
      if (currentUrl == null) return;
      await _freshProfilesRaw();
      final profiles = await getProfiles();
      for (final p in profiles) {
        if (_sameUrl(p.url, currentUrl) && (p.accessToken != null || p.refreshToken != null)) {
          await saveProfile(p.withoutTokens());
        }
      }
    } catch (_) {}
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
          name: ServerProfile.defaultName(currentUrl),
          url: currentUrl,
          username: currentUsername ?? '',
          accessToken: await getAccessToken(),
          refreshToken: await getRefreshToken(),
          lastConnected: clock.now(),
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
      final raw = await _freshProfilesRaw();
      if (raw != null && raw.isNotEmpty) {
        final List<dynamic> list = jsonDecode(raw);
        profiles = list.map((e) => ServerProfile.fromJson(e as Map<String, dynamic>)).toList();
      }
    } catch (_) {}

    // One profile per id; a new profile for a server already saved
    // replaces that one rather than adding a twin.
    var idx = profiles.indexWhere((p) => p.id == profile.id);
    if (idx < 0) idx = profiles.indexWhere((p) => _sameUrl(p.url, profile.url));
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
      final raw = await _freshProfilesRaw();
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

  /// Makes [profile] the active server: its URL and its saved session (or
  /// none, so the app asks to sign in). Background work must be stopped
  /// before and started again after - see SessionController.switchTo.
  Future<void> switchProfile(ServerProfile profile) async {
    if (!_initialized) await init();
    await _set(_keyActiveProfileId, profile.id);
    await setServerUrl(profile.url);
    final access = profile.accessToken;
    final refresh = profile.refreshToken;
    if (access != null && access.isNotEmpty && refresh != null && refresh.isNotEmpty) {
      await setSession(accessToken: access, refreshToken: refresh, username: profile.username);
    } else {
      await _remove(_keyAccessToken);
      await _remove(_keyRefreshToken);
      await _set(_keyUsername, profile.username);
    }
    final updated = (await getProfiles()).where((p) => p.id == profile.id).firstOrNull ?? profile;
    await saveProfile(updated.copyWith(lastConnected: clock.now()));
  }

  /// Forgets everything loaded, so the next call reads the platform
  /// storage again. Tests only.
  @visibleForTesting
  void resetForTest() {
    _cache.clear();
    _initialized = false;
  }

  Future<void> clearAll() async {
    if (!_initialized) await init();
    final kept = {
      for (final key in _preservedKeys)
        if (_cache[key] != null) key: _cache[key]!,
    };
    _cache.clear();
    await _secureStorage.deleteAll();
    for (final entry in kept.entries) {
      await _set(entry.key, entry.value);
    }
  }
}
