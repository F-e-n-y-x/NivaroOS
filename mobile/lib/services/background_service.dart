import 'dart:io';

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

// The Dart entry points the native side starts (shareServiceMain,
// heartbeatMain) must be part of the app's code for release builds.
// ignore: unused_import
import 'background_sync_isolate.dart';

/// Where a sharing session stands, as the native service reports it.
@immutable
class ShareStatus {
  const ShareStatus({required this.running, this.endsAt, this.lastStopReason});

  static const off = ShareStatus(running: false);

  final bool running;

  /// When the running session ends by itself.
  final DateTime? endsAt;

  /// Why the last session ended: `stopped` (by the user), `ended` (its
  /// time was up), `timeout` (Android's daily limit for this kind of
  /// service), `not_allowed` (Android refused to start it), `signed_out`,
  /// `error`. Null when nothing ended yet.
  final String? lastStopReason;

  @override
  bool operator ==(Object other) =>
      other is ShareStatus && other.running == running && other.endsAt == endsAt && other.lastStopReason == lastStopReason;

  @override
  int get hashCode => Object.hash(running, endsAt, lastStopReason);
}

/// The app's background work on Android (plan M-18, M-33):
///
/// - **Heartbeat**: a JobScheduler job about every 15 minutes (network
///   required) that tells the server this phone is still there. Scheduled
///   while signed in, cancelled on sign-out and on a server switch.
/// - **Storage sharing**: a foreground service the user starts for a set
///   time (at most [maxShare]), which lets the server browse this phone's
///   shared storage and ends by itself. Nothing runs all the time, and
///   nothing starts at boot.
///
/// Both run the app's own Dart code in a headless engine
/// (background_sync_isolate.dart); the native side is CompanionShareService
/// and HeartbeatJobService.
class BackgroundService {
  BackgroundService._();
  static final BackgroundService instance = BackgroundService._();

  static const MethodChannel _share = MethodChannel('com.fenyx.nivaroos/companion_share');
  static const MethodChannel _battery = MethodChannel('com.fenyx.nivaroos/background_service');

  /// The longest sharing session offered. Android 15 gives this kind of
  /// service 6 hours a day, so a session always fits with room to spare.
  static const maxShare = Duration(hours: 3);

  /// Session lengths offered when sharing is turned on.
  /// "Never": sharing stays on until the user turns it off.
  static const shareNever = Duration.zero;

  static const shareChoices = [Duration(minutes: 30), Duration(hours: 1), Duration(hours: 3), shareNever];

  /// Lets tests and screenshots render the Android-only parts of a screen
  /// on the machine running them.
  @visibleForTesting
  static bool? debugIsAndroid;

  static bool get isAndroid => debugIsAndroid ?? Platform.isAndroid;

  /// Starts sharing until now + [length] (capped at [maxShare]). [server]
  /// names the server in the notification. Returns false when it could not
  /// be started.
  Future<bool> startSharing(Duration length, {required String server}) async {
    if (!isAndroid) return false;
    // End time 0 = no end time (Never).
    final int endAt;
    if (length == shareNever) {
      endAt = 0;
    } else {
      final capped = length > maxShare ? maxShare : length;
      endAt = clock.now().add(capped).millisecondsSinceEpoch;
    }
    try {
      await _share.invokeMethod('start', {'endAt': endAt, 'server': server});
      return true;
    } catch (e) {
      debugPrint('[BackgroundService] Could not start sharing: $e');
      return false;
    }
  }

  Future<void> stopSharing() async {
    if (!isAndroid) return;
    try {
      await _share.invokeMethod('stop');
    } catch (e) {
      debugPrint('[BackgroundService] Could not stop sharing: $e');
    }
  }

  Future<ShareStatus> sharingStatus() async {
    if (!isAndroid) return ShareStatus.off;
    try {
      final res = await _share.invokeMethod<Object?>('status');
      if (res is! Map) return ShareStatus.off;
      final running = res['running'] == true;
      final endsAt = (res['endsAt'] as num?)?.toInt() ?? 0;
      return ShareStatus(
        running: running,
        endsAt: running && endsAt > 0 ? DateTime.fromMillisecondsSinceEpoch(endsAt) : null,
        lastStopReason: res['lastStopReason'] as String?,
      );
    } catch (_) {
      return ShareStatus.off;
    }
  }

  /// Schedules the 15-minute heartbeat (a no-op when it already is).
  Future<void> scheduleHeartbeat() async {
    if (!isAndroid) return;
    try {
      await _share.invokeMethod('scheduleHeartbeat');
    } catch (e) {
      debugPrint('[BackgroundService] Could not schedule the heartbeat: $e');
    }
  }

  Future<void> cancelHeartbeat() async {
    if (!isAndroid) return;
    try {
      await _share.invokeMethod('cancelHeartbeat');
    } catch (e) {
      debugPrint('[BackgroundService] Could not cancel the heartbeat: $e');
    }
  }

  /// Stops everything that runs for the current server: the sharing
  /// session and the heartbeat. Before signing out or switching servers
  /// (plan M-17), so nothing keeps talking to the old one.
  Future<void> stopAll() async {
    await stopSharing();
    await cancelHeartbeat();
  }

  /// Checks if the app is exempt from battery optimizations (Doze mode)
  Future<bool> isIgnoringBatteryOptimizations() async {
    if (!isAndroid) return true;
    try {
      return await _battery.invokeMethod<bool>('isIgnoringBatteryOptimizations') ?? false;
    } catch (e) {
      return false;
    }
  }

  /// Requests the system dialog to whitelist the app from battery optimizations
  Future<bool> requestIgnoreBatteryOptimizations() async {
    if (!isAndroid) return true;
    try {
      return await _battery.invokeMethod<bool>('requestIgnoreBatteryOptimizations') ?? false;
    } catch (e) {
      debugPrint('[BackgroundService] Error requesting battery optimizations exemption: $e');
      return false;
    }
  }

  /// Opens the system Battery Optimization Settings list
  Future<bool> openBatteryOptimizationSettings() async {
    if (!isAndroid) return false;
    try {
      return await _battery.invokeMethod<bool>('openBatteryOptimizationSettings') ?? false;
    } catch (e) {
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
    if (!isAndroid) return false;
    try {
      return await _battery.invokeMethod<bool>('isSamsungDevice') ?? false;
    } catch (e) {
      return false;
    }
  }

  /// Opens this app's battery page, one tap from Samsung's "Never sleeping
  /// apps" list (there's no public intent for the list itself).
  Future<bool> openSamsungBatterySettings() async {
    if (!isAndroid) return false;
    try {
      return await _battery.invokeMethod<bool>('openSamsungBatterySettings') ?? false;
    } catch (e) {
      return false;
    }
  }
}
