import 'dart:ui';
import 'package:flutter_background_service/flutter_background_service.dart';
import 'api_client.dart';
import 'storage_service.dart';
import 'device_sync_service.dart';

/// Entry point for the headless background isolate flutter_background_service
/// spawns. This is a SEPARATE isolate from the UI's - it has its own memory,
/// so every singleton it touches (StorageService, ApiClient, DeviceSyncService)
/// starts uninitialized here and must load its own state, even though the UI
/// isolate already did the same. What's actually shared across isolates is
/// the underlying platform storage (Android Keystore-backed secure storage)
/// those singletons read from, not the singletons themselves.
///
/// This isolate is what makes background sync survive the Activity being
/// destroyed (screen off long enough for Android to tear down the UI, or the
/// app swiped away from recents) - it's hosted by its own native Android
/// Service (via flutter_background_service), not by MainActivity.
@pragma('vm:entry-point')
Future<void> onBackgroundServiceStart(ServiceInstance service) async {
  DartPluginRegistrant.ensureInitialized();

  // No explicit setAsForegroundService() call here - AndroidConfiguration's
  // isForegroundMode: true (background_service.dart) already makes the
  // native plugin call startForeground() automatically as soon as this
  // isolate starts (BackgroundService.java's runService() ->
  // updateNotificationInfo(), unconditional on isForeground). An unawaited,
  // redundant second call to it here served no purpose the config didn't
  // already cover, and matches neither this package's own example (which
  // only ever calls it in response to an explicit foreground/background
  // toggle event, never unconditionally at startup) nor the "no unnecessary
  // native calls in a codepath already known to be crash-prone" standard
  // the rest of this fix applies.

  await StorageService.instance.init();
  await ApiClient.instance.init();

  service.on('stopService').listen((event) {
    DeviceSyncService.instance.stopAutoSync();
    service.stopSelf();
  });

  if (!ApiClient.instance.hasSession) {
    // Not logged in - the UI only starts this service after a successful
    // login (see DeviceSyncService.enableBackgroundSync()), so this is a
    // defensive fallback rather than the expected path.
    service.stopSelf();
    return;
  }

  DeviceSyncService.instance.startAutoSync();
}

@pragma('vm:entry-point')
bool onBackgroundServiceIosBackground(ServiceInstance service) => true;
