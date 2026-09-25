import 'package:flutter/foundation.dart';

import '../models/server_profile.dart';
import 'api_client.dart';
import 'device_sync_service.dart';
import 'storage_service.dart';
import 'tailscale_service.dart';

/// Sign-in, sign-out and server switching in one place, so every path does
/// the same clean-up: background work for the old server stops before
/// anything changes, and starts again only for the new one (plan M-17).
abstract final class SessionService {
  /// After a successful sign-in on the current server: keeps the session.
  /// The shell then calls [started].
  static Future<void> signedIn({required String accessToken, required String refreshToken, required String username}) async {
    ApiClient.instance.setSession(accessToken, refreshToken);
    await StorageService.instance.setSession(accessToken: accessToken, refreshToken: refreshToken, username: username);
  }

  /// Once the shell is on screen with a session (launch, sign-in, switch):
  /// registers the phone, schedules the heartbeat and runs the one-time
  /// clean-ups. Failures are quiet; the heartbeat tries again.
  static Future<void> started() async {
    try {
      await DeviceSyncService.instance.onSignedIn();
      await TailscaleService.instance.removeLegacyAuthKey();
    } catch (e) {
      debugPrint('[session] start notice: ${e.runtimeType}');
    }
  }

  /// Signs out of the current server: stops sharing and the heartbeat,
  /// then forgets the session (the server profile stays, without tokens).
  static Future<void> signOut() async {
    await DeviceSyncService.instance.onSigningOut();
    await StorageService.instance.clearSession();
    ApiClient.instance.clearSession();
  }

  /// Makes [profile] the active server. Returns true when it has a session
  /// to continue with, false when the user has to sign in to it.
  static Future<bool> switchTo(ServerProfile profile) async {
    await DeviceSyncService.instance.onSigningOut();
    await StorageService.instance.switchProfile(profile);
    ApiClient.instance.setBaseUrl(profile.url);
    if (profile.hasSession) {
      ApiClient.instance.setSession(profile.accessToken!, profile.refreshToken!);
      return true;
    }
    ApiClient.instance.clearSession();
    return false;
  }

  /// Points the app at another server address (discovery, manual entry)
  /// without a session yet. Background work for the previous server stops.
  static Future<void> useServer(String url) async {
    await DeviceSyncService.instance.onSigningOut();
    final current = await StorageService.instance.getServerUrl();
    if (current != null && current != url) {
      await StorageService.instance.clearSessionTokensOnly();
    }
    await StorageService.instance.setServerUrl(url);
    ApiClient.instance.setBaseUrl(url);
    if (current != url) ApiClient.instance.clearSession();
  }
}
