import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Everything this app needs to remember between launches - which server,
/// and that server's own login token - kept in the platform keystore
/// (Android Keystore-backed), not plain SharedPreferences, since a login
/// token is a real credential.
class StorageService {
  StorageService._();
  static final StorageService instance = StorageService._();

  final _storage = const FlutterSecureStorage();

  static const _keyServerUrl = 'server_url';
  static const _keyAccessToken = 'access_token';
  static const _keyRefreshToken = 'refresh_token';
  static const _keyUsername = 'username';

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

  /// "Change Server" - forgets everything, not just the session, since a
  /// different server means a different account entirely.
  Future<void> clearAll() async {
    await _storage.deleteAll();
  }
}
