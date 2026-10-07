import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:path_provider/path_provider.dart';

import 'api_client.dart';
import 'storage_service.dart';

/// The signed-in account's profile picture: fetched from the server
/// (`GET /v1/users/avatar`, versioned by the user info's avatar_version)
/// and kept on the phone per server and account, so the server list and
/// the sign-in screen can show it without a session.
class AvatarService {
  AvatarService._();
  static final AvatarService instance = AvatarService._();

  /// The current account's picture; null = none (draw initials).
  final ValueNotifier<Uint8List?> current = ValueNotifier(null);

  final Map<String, Uint8List?> _memory = {};

  bool _disk = true;

  /// Tests: forget everything and keep pictures in memory only.
  @visibleForTesting
  void debugReset() {
    _memory.clear();
    _disk = false;
    current.value = null;
  }

  static String _key(String server, String username) =>
      sha1.convert(utf8.encode('${ApiClient.normalizeServerUrl(server)}\n$username')).toString().substring(0, 20);

  // ponytail: one PNG per server+account in app support; never pruned
  // (a few hundred KB each, only accounts this phone signed in to).
  Future<File?> _file(String key) async {
    if (!_disk) return null;
    try {
      final dir = Directory('${(await getApplicationSupportDirectory()).path}/avatars');
      await dir.create(recursive: true);
      return File('${dir.path}/$key.png');
    } catch (_) {
      return null; // no app storage: memory only
    }
  }

  /// The last picture seen for [username] on [server], if any.
  Future<Uint8List?> cachedFor(String server, String username) async {
    if (server.isEmpty || username.isEmpty) return null;
    final key = _key(server, username);
    if (_memory.containsKey(key)) return _memory[key];
    final f = await _file(key);
    final bytes = f != null && await f.exists() ? await f.readAsBytes() : null;
    return _memory[key] = bytes;
  }

  Future<void> _store(String server, String username, Uint8List? bytes) async {
    final key = _key(server, username);
    _memory[key] = bytes;
    final f = await _file(key);
    if (f == null) return;
    if (bytes == null) {
      if (await f.exists()) await f.delete();
    } else {
      await f.writeAsBytes(bytes, flush: true);
    }
  }

  /// Reads the account's avatar_version and fetches the picture when it
  /// changed. Quiet on failure: the last picture stays.
  Future<void> refresh() async {
    final server = ApiClient.instance.baseUrl;
    // The cached picture first (another account's must not linger after a
    // switch), then the server's answer.
    current.value = await cachedFor(server, await StorageService.instance.getUsername() ?? '');
    try {
      final res = await ApiClient.instance.get('/v1/users/current');
      final user = res['data'] as Map<String, dynamic>? ?? const {};
      await _apply(server, user);
    } catch (e) {
      debugPrint('[avatar] refresh: ${e.runtimeType}');
    }
  }

  Future<void> _apply(String server, Map<String, dynamic> user) async {
    final name = user['username'] as String? ?? '';
    final version = user['avatar_version'] as String? ?? '';
    if (version.isEmpty) {
      await _store(server, name, null);
      current.value = null;
      return;
    }
    // The version is the start of the stored PNG's SHA-256 (user service
    // avatar.go), so the cached copy says itself whether it is current.
    final cached = await cachedFor(server, name);
    if (cached == null || !sha256.convert(cached).toString().startsWith(version)) {
      final res = await ApiClient.instance.getRaw('/v1/users/avatar', query: {'v': version});
      if (res.statusCode != 200) return;
      await _store(server, name, res.bodyBytes);
    }
    current.value = await cachedFor(server, name);
  }

  /// Sets the picture (a square PNG the app cropped; the server re-encodes
  /// it to 512 px).
  Future<void> upload(Uint8List png) async {
    final res = await ApiClient.instance.put('/v1/users/avatar', body: {'file': base64Encode(png)});
    await _apply(ApiClient.instance.baseUrl, res['data'] as Map<String, dynamic>? ?? const {});
  }

  Future<void> remove() async {
    final res = await ApiClient.instance.delete('/v1/users/avatar');
    await _apply(ApiClient.instance.baseUrl, res['data'] as Map<String, dynamic>? ?? const {});
  }
}
