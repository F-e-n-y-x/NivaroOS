import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_background_service/flutter_background_service.dart';
import 'background_sync_isolate.dart';

/// Controls the app's TRUE background execution: a headless Dart isolate
/// (flutter_background_service) that keeps running independently of the
/// Activity/UI, so companion file sharing and device sync keep working after
/// the screen turns off or the app is swiped away from recents - not just
/// while the app is open.
///
/// This used to be a hand-rolled native Android Service
/// (BackgroundCompanionService.kt) that only held a wakelock and posted a
/// notification, with no Dart code running inside it at all - every actual
/// piece of sync logic (DeviceSyncService's 30s timer, CompanionFileServer's
/// HTTP server, the WebSocket tunnel) lived in the main UI isolate and died
/// the moment Android tore down the Activity/FlutterEngine. Restarting that
/// bare service after task-removal (its onTaskRemoved handler) just brought
/// back an empty shell showing "Companion Active" while doing nothing -
/// which is exactly the bug this class now fixes: flutter_background_service
/// runs onBackgroundServiceStart (background_sync_isolate.dart) in a real,
/// independent Dart isolate hosted by its own native service, so the sync
/// loop and file server actually keep executing.
class BackgroundService {
  BackgroundService._();
  static final BackgroundService instance = BackgroundService._();

  static const MethodChannel _channel = MethodChannel('com.fenyx.nivaroos/background_service');
  static bool _configured = false;

  Future<void> _ensureConfigured() async {
    if (_configured) return;
    _configured = true;
    // Reboot-survival needs a real BOOT_COMPLETED path - flutter_background_service's
    // own autoStartOnBoot config bundles that (its plugin registers the actual
    // receiver), driven by the same user-facing toggle settings_screen.dart
    // already exposes via setAutoStartOnBoot(). Read once here rather than
    // reinventing boot handling with a custom native receiver that would have
    // to guess this plugin's internal service class name to restart it.
    final autoStartOnBoot = await isAutoStartOnBoot();
    final service = FlutterBackgroundService();
    await service.configure(
      androidConfiguration: AndroidConfiguration(
        onStart: onBackgroundServiceStart,
        autoStart: false,
        autoStartOnBoot: autoStartOnBoot,
        isForegroundMode: true,
        notificationChannelId: 'nivaroos_companion_service',
        initialNotificationTitle: 'NivaroOS Companion Active',
        initialNotificationContent: 'Storage sharing & device sync running unattended',
        foregroundServiceNotificationId: 42843,
        foregroundServiceTypes: [AndroidForegroundType.dataSync],
      ),
      iosConfiguration: IosConfiguration(
        onForeground: onBackgroundServiceStart,
        onBackground: onBackgroundServiceIosBackground,
      ),
    );
  }

  /// Starts the headless background isolate (idempotent - flutter_background_service
  /// no-ops if it's already running).
  Future<bool> startService() async {
    if (!Platform.isAndroid) return false;
    try {
      await _ensureConfigured();
      await FlutterBackgroundService().startService();
      return true;
    } catch (e) {
      debugPrint('[BackgroundService] Failed to start service: $e');
      return false;
    }
  }

  /// Signals the background isolate to stop itself (see background_sync_isolate.dart's
  /// 'stopService' listener, which calls DeviceSyncService.stopAutoSync() first).
  Future<bool> stopService() async {
    if (!Platform.isAndroid) return false;
    try {
      await _ensureConfigured();
      FlutterBackgroundService().invoke('stopService');
      return true;
    } catch (e) {
      debugPrint('[BackgroundService] Failed to stop service: $e');
      return false;
    }
  }

  Future<bool> isServiceRunning() async {
    if (!Platform.isAndroid) return false;
    try {
      await _ensureConfigured();
      return await FlutterBackgroundService().isRunning();
    } catch (e) {
      debugPrint('[BackgroundService] Failed to query service state: $e');
      return false;
    }
  }

  /// Checks if the app is exempt from battery optimizations (Doze mode)
  Future<bool> isIgnoringBatteryOptimizations() async {
    if (!Platform.isAndroid) return true;
    try {
      return await _channel.invokeMethod<bool>('isIgnoringBatteryOptimizations') ?? false;
    } catch (e) {
      debugPrint('[BackgroundService] Error checking battery optimizations: $e');
      return false;
    }
  }

  /// Requests the system dialog to whitelist the app from battery optimizations
  Future<bool> requestIgnoreBatteryOptimizations() async {
    if (!Platform.isAndroid) return true;
    try {
      return await _channel.invokeMethod<bool>('requestIgnoreBatteryOptimizations') ?? false;
    } catch (e) {
      debugPrint('[BackgroundService] Error requesting battery optimizations exemption: $e');
      return false;
    }
  }

  /// Opens the system Battery Optimization Settings list
  Future<bool> openBatteryOptimizationSettings() async {
    if (!Platform.isAndroid) return false;
    try {
      return await _channel.invokeMethod<bool>('openBatteryOptimizationSettings') ?? false;
    } catch (e) {
      debugPrint('[BackgroundService] Error opening battery optimization settings: $e');
      return false;
    }
  }

  /// Checks whether Samsung's own OEM battery manager ("Sleeping apps" /
  /// "Deep sleeping apps") is present on this device - separate from, and in
  /// addition to, stock Android's Doze whitelist above. One UI has
  /// historically killed background apps/services even when they're exempt
  /// from the stock optimization, unless also excluded from this OEM-specific
  /// list, so a Samsung device needs both prompts, not just one.
  Future<bool> isSamsungDevice() async {
    if (!Platform.isAndroid) return false;
    try {
      return await _channel.invokeMethod<bool>('isSamsungDevice') ?? false;
    } catch (e) {
      return false;
    }
  }

  /// Opens Samsung's device-care battery settings screen so the user can
  /// manually add this app to "Never sleeping apps" - there's no public,
  /// reliable Intent action to deep-link straight into that specific list
  /// (it's not part of AOSP), so this opens the closest documented entry
  /// point (device care) and the app's own battery settings page.
  Future<bool> openSamsungBatterySettings() async {
    if (!Platform.isAndroid) return false;
    try {
      return await _channel.invokeMethod<bool>('openSamsungBatterySettings') ?? false;
    } catch (e) {
      debugPrint('[BackgroundService] Error opening Samsung battery settings: $e');
      return false;
    }
  }

  /// Checks whether auto-start on boot is enabled
  Future<bool> isAutoStartOnBoot() async {
    if (!Platform.isAndroid) return false;
    try {
      return await _channel.invokeMethod<bool>('isAutoStartOnBoot') ?? true;
    } catch (e) {
      return true;
    }
  }

  /// Sets whether auto-start on boot is enabled
  Future<bool> setAutoStartOnBoot(bool enabled) async {
    if (!Platform.isAndroid) return false;
    try {
      return await _channel.invokeMethod<bool>('setAutoStartOnBoot', {'enabled': enabled}) ?? false;
    } catch (e) {
      debugPrint('[BackgroundService] Error setting auto-start on boot: $e');
      return false;
    }
  }
}
