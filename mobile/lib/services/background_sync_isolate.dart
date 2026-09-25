import 'dart:async';
import 'dart:ui';

import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import 'api_client.dart';
import 'companion_file_server.dart';
import 'device_sync_service.dart';
import 'storage_service.dart';

/// Entry points for the headless Flutter engines the native side starts
/// (android/.../HeadlessDart.kt). Each runs in its own isolate with its own
/// copies of StorageService, ApiClient and DeviceSyncService, which load
/// their state from the shared platform storage; nothing in memory is
/// shared with the app's UI.

/// Starts plugins and the service bindings in a headless engine.
Future<void> _bootHeadless() async {
  WidgetsFlutterBinding.ensureInitialized();
  DartPluginRegistrant.ensureInitialized();
  await StorageService.instance.init();
  await ApiClient.instance.init();
}

/// A storage-sharing session (CompanionShareService): serves this phone's
/// shared storage to the signed-in server until the service ends it.
@pragma('vm:entry-point')
Future<void> shareServiceMain() async {
  const channel = MethodChannel('com.fenyx.nivaroos/share_engine');
  Future<void> stop(String reason) async {
    DeviceSyncService.instance.stopSharingHeartbeat();
    await CompanionFileServer.instance.stop();
    try {
      await channel.invokeMethod('stop', {'reason': reason});
    } catch (_) {}
  }

  try {
    await _bootHeadless();
    if (!ApiClient.instance.hasSession) {
      await stop('signed_out');
      return;
    }
    // The session ended on the server (a real 401 on refresh): stop
    // serving rather than keep a tunnel nobody can authenticate.
    ApiClient.sessionExpiredNotifier.addListener(() {
      if (ApiClient.sessionExpiredNotifier.value) stop('signed_out');
    });
    // Removed from the server while sharing: this engine stops itself.
    DeviceSyncService.onRemovedByServer = () => stop('removed');
    await CompanionFileServer.instance.start();
    DeviceSyncService.instance.startSharingHeartbeat();
    Timer.periodic(const Duration(minutes: 1), (_) => CompanionFileServer.instance.refreshAddress());
  } catch (e) {
    debugPrint('[share] Could not start: ${e.runtimeType}');
    await stop('error');
  }
}

/// One heartbeat (HeartbeatJobService): register with the server, then
/// tell the job it may finish.
@pragma('vm:entry-point')
Future<void> heartbeatMain() async {
  const channel = MethodChannel('com.fenyx.nivaroos/heartbeat');
  try {
    await _bootHeadless();
    if (ApiClient.instance.hasSession) {
      await DeviceSyncService.instance.syncWithServer().timeout(const Duration(seconds: 60));
    }
  } catch (e) {
    debugPrint('[heartbeat] ${e.runtimeType}');
  } finally {
    try {
      await channel.invokeMethod('done');
    } catch (_) {}
  }
}
