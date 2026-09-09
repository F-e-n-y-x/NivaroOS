import 'package:flutter/foundation.dart';
import 'package:permission_handler/permission_handler.dart';

/// Central service to handle Android runtime permissions for NivaroOS companion features:
/// - Notifications (server alerts, task progress)
/// - All Files Access (file explorer, phone-to-server file sync, ISO transfers)
class PermissionService {
  PermissionService._();

  static bool _hasRequested = false;

  /// Prompts the user on initial launch only for essential background notifications.
  static Future<void> requestInitialPermissions() async {
    if (_hasRequested) return;
    _hasRequested = true;

    try {
      final notifStatus = await Permission.notification.status;
      if (notifStatus.isDenied) {
        await Permission.notification.request();
      }
    } catch (e) {
      debugPrint('[PermissionService] Error requesting notification permission: $e');
    }

    try {
      final storageStatus = await Permission.manageExternalStorage.status;
      if (!storageStatus.isGranted) {
        await Permission.manageExternalStorage.request();
      }
    } catch (e) {
      debugPrint('[PermissionService] Error requesting storage permission: $e');
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
}
