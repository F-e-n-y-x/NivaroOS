import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

class BackgroundService {
  BackgroundService._();
  static final BackgroundService instance = BackgroundService._();

  static const MethodChannel _channel = MethodChannel('com.fenyx.nivaroos/background_service');

  bool _isServiceActive = false;
  bool get isServiceActive => _isServiceActive;

  /// Starts the Android Foreground Service with WakeLock and ongoing notification
  Future<bool> startService({String? title, String? message}) async {
    if (!Platform.isAndroid) return false;
    try {
      final success = await _channel.invokeMethod<bool>('startService', {
        'title': title ?? 'NivaroOS Companion Active',
        'message': message ?? 'Storage sharing & device sync running unattended',
      }) ?? false;
      _isServiceActive = success;
      return success;
    } catch (e) {
      debugPrint('[BackgroundService] Failed to start service: $e');
      return false;
    }
  }

  /// Stops the Android Foreground Service and releases WakeLocks
  Future<bool> stopService() async {
    if (!Platform.isAndroid) return false;
    try {
      final success = await _channel.invokeMethod<bool>('stopService') ?? false;
      _isServiceActive = !success;
      return success;
    } catch (e) {
      debugPrint('[BackgroundService] Failed to stop service: $e');
      return false;
    }
  }

  /// Checks if the Android Foreground Service is currently running
  Future<bool> isServiceRunning() async {
    if (!Platform.isAndroid) return false;
    try {
      final running = await _channel.invokeMethod<bool>('isServiceRunning') ?? false;
      _isServiceActive = running;
      return running;
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
