import 'package:flutter/foundation.dart';
import 'package:permission_handler/permission_handler.dart';

import 'background_service.dart';
import 'storage_service.dart';

/// Where a permission stands, in the terms the app's screens use.
enum PermissionState {
  granted,

  /// Not granted yet; asking shows the system prompt.
  askable,

  /// Denied for good (or blocked by policy): only the app's system settings
  /// page can change it.
  blocked,
}

/// Android runtime permissions. The app never asks at launch: each one is
/// asked for when a feature needs it, after the screen has said why
/// (notifications when storage sharing starts, all-files access for
/// sharing and the phone's files).
class PermissionService {
  PermissionService._();

  static PermissionState _state(PermissionStatus s) {
    if (s.isGranted || s.isLimited) return PermissionState.granted;
    if (s.isPermanentlyDenied || s.isRestricted) return PermissionState.blocked;
    return PermissionState.askable;
  }

  /// Whether the app may post notifications (Android 13+ asks; older
  /// versions allow by default).
  static Future<PermissionState> notificationState() async {
    if (!BackgroundService.isAndroid) return PermissionState.granted;
    try {
      return _state(await Permission.notification.status);
    } catch (_) {
      return PermissionState.granted;
    }
  }

  /// Asks for notifications, once per install, when a feature that shows
  /// one starts. The caller has already explained why. Returns the state
  /// afterwards.
  static Future<PermissionState> requestNotifications() async {
    final current = await notificationState();
    if (current != PermissionState.askable) return current;
    if (await StorageService.instance.getNotificationsAsked()) return current;
    await StorageService.instance.setNotificationsAsked();
    try {
      return _state(await Permission.notification.request());
    } catch (e) {
      debugPrint('[PermissionService] Notification request failed: $e');
      return current;
    }
  }

  /// All-files access (MANAGE_EXTERNAL_STORAGE), needed to share the
  /// phone's storage with the server and to browse it in Files.
  static Future<PermissionState> storageState() async {
    if (!BackgroundService.isAndroid) return PermissionState.granted;
    try {
      final s = await Permission.manageExternalStorage.status;
      // This one is granted on a settings page, never "permanently denied".
      return s.isGranted ? PermissionState.granted : PermissionState.askable;
    } catch (_) {
      return PermissionState.askable;
    }
  }

  /// Request Manage External Storage (All Files Access) for full file explorer capabilities.
  static Future<bool> requestManageStorage() async {
    try {
      final status = await Permission.manageExternalStorage.status;
      if (status.isGranted) return true;
      final res = await Permission.manageExternalStorage.request();
      return res.isGranted;
    } catch (e) {
      debugPrint('[PermissionService] Manage storage error: $e');
      return false;
    }
  }

  /// This app's page in the system settings, for a blocked permission.
  static Future<void> openSettings() async {
    try {
      await openAppSettings();
    } catch (_) {}
  }
}
