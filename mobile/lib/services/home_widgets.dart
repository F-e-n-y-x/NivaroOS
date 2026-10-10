import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart' show AppLifecycleListener;
import 'package:intl/intl.dart';

import '../models/dashboard_stats.dart';
import '../phone_backup/pb_service.dart';
import '../phone_backup/pb_store.dart' show RunOutcomes;
import '../utils/format.dart';
import 'api_client.dart';
import 'background_service.dart';
import 'download_station_api.dart';

/// The home-screen widgets and Quick Settings tiles (android/.../HomeWidgets.kt).
/// They draw what this pushes: ready-made text, kept by the native side in
/// SharedPreferences and merged (a key left out keeps its last value, so an
/// offline check keeps the last good numbers). Pushed by the 15-minute
/// heartbeat, when the app is left, and after a phone backup run; nothing
/// polls on its own.
///
/// Keys: `signed_in`, `server`, `status`, `cpu`, `ram`, `disk`, `updated`,
/// `stale_at` (ms; past it the widget shows `stale` instead of `status`),
/// `stale`, `downloads`, `backup`, `backup_ready`, `backup_any_network`.
class HomeWidgets {
  HomeWidgets._();
  static final HomeWidgets instance = HomeWidgets._();

  static const _channel = MethodChannel('com.fenyx.nivaroos/home_widgets');

  /// Data older than this reads as "not updated" (Doze can hold the
  /// heartbeat back well past its 15 minutes).
  static const staleAfter = Duration(hours: 1);

  AppLifecycleListener? _app;

  /// In the app: pushes now and each time the app is left.
  void watchApp() {
    if (!BackgroundService.isAndroid) return;
    _app ??= AppLifecycleListener(onPause: () => unawaited(refresh()));
    unawaited(refresh());
  }

  /// Reads the server, Download Station and the phone backup, and pushes it all.
  Future<void> refresh({ApiClient? api}) async {
    if (!BackgroundService.isAndroid) return;
    final c = api ?? ApiClient.instance;
    if (!c.hasSession) return;
    final now = clock.now();
    DashboardStats? stats;
    try {
      final util = await c.get('/sys/utilization');
      var disks = const <DiskUsage>[];
      try {
        final res = await c.get('/sys/disks-usage');
        disks = [
          for (final e in res['data'] as List<dynamic>? ?? const [])
            if (e is Map) DiskUsage.fromJson(Map<String, dynamic>.from(e)),
        ].where((d) => d.mountPoint.isNotEmpty).toList();
      } catch (_) {}
      stats = DashboardStats.fromUtilization(util['data'] as Map<String, dynamic>? ?? const {}).withDisks(disks);
    } catch (_) {}
    int? active;
    if (stats != null) {
      try {
        active = await activeDownloads(DownloadStationApi(c));
      } catch (_) {}
    }
    await _push({
      'signed_in': true,
      'server': Uri.tryParse(c.baseUrl)?.host ?? '',
      ...serverFields(stats, now),
      if (stats != null) 'downloads': downloadsText(active),
      ...await _backup(now),
    });
  }

  /// After a phone backup run (its own engine, no app session needed).
  Future<void> refreshBackup() async => _push(await _backup(clock.now()));

  /// Signed out: the widgets say so, with nothing of the old server (the
  /// backup line stays: phone backup has its own link).
  Future<void> clear() async {
    if (!BackgroundService.isAndroid) return;
    try {
      await _channel.invokeMethod('clear');
    } catch (_) {}
  }

  Future<Map<String, Object?>> _backup(DateTime now) async {
    try {
      return backupFields(await PhoneBackupService.instance.status(), now);
    } catch (_) {
      return const {};
    }
  }

  Future<void> _push(Map<String, Object?> fields) async {
    if (!BackgroundService.isAndroid || fields.isEmpty) return;
    try {
      await _channel.invokeMethod('update', fields);
    } catch (e) {
      debugPrint('[HomeWidgets] ${e.runtimeType}');
    }
  }
}

/// Running downloads and torrents still downloading; null when Download
/// Station isn't on the server.
Future<int?> activeDownloads(DownloadStationApi ds) async {
  try {
    final links = (await ds.downloads()).where((d) => d.isRunning).length;
    var torrents = 0;
    try {
      torrents = (await ds.torrents()).torrents.where((t) => !t.isPaused && !t.isFinished).length;
    } catch (_) {}
    return links + torrents;
  } catch (e) {
    if (DownloadStationApi.isUnavailable(e)) return null;
    rethrow;
  }
}

/// "14:05" today, "3 Oct, 14:05" before.
@visibleForTesting
String widgetTime(DateTime t, DateTime now) =>
    t.year == now.year && t.month == now.month && t.day == now.day ? DateFormat.Hm().format(t) : DateFormat('d MMM, HH:mm').format(t);

/// "42%", or a dash for a reading that isn't there.
@visibleForTesting
String widgetPercent(double? v) => v == null || v.isNaN || v < 0 ? '—' : '${v.clamp(0, 100).round()}%';

/// The server widget's fields for one check: [stats] null when the
/// server didn't answer (the last numbers stay, the status says Offline).
Map<String, Object?> serverFields(DashboardStats? stats, DateTime now) {
  final staleAt = now.add(HomeWidgets.staleAfter).millisecondsSinceEpoch;
  if (stats == null) return {'status': 'Offline', 'stale_at': staleAt};
  final when = widgetTime(now, now);
  return {
    'status': 'Online',
    'cpu': widgetPercent(stats.cpuPercent),
    'ram': widgetPercent(stats.memTotal > 0 || stats.memUsedPercent > 0 ? stats.memUsedPercent : null),
    'disk': stats.storageTotal > 0 ? widgetPercent(stats.storageFraction * 100) : '—',
    'updated': 'Updated $when',
    'stale': 'Not updated since $when',
    'stale_at': staleAt,
  };
}

/// "3 active downloads"; null [active] = Download Station isn't there.
String downloadsText(int? active) => switch (active) {
      null => 'Download Station not installed',
      0 => 'No active downloads',
      final n => formatCount(n, 'active download'),
    };

/// The backup line and whether the "Back up now" tile can start one.
Map<String, Object?> backupFields(PhoneBackupStatus s, DateTime now) {
  final settings = s.settings;
  final st = s.state;
  final String text;
  if (!s.setUp || settings == null) {
    text = 'Phone backup not set up';
  } else if (st.isRunning) {
    final p = st.progress;
    text = p != null && p.total > 0 ? 'Backing up · ${p.done} of ${p.total}' : 'Backing up this phone';
  } else if (settings.paused) {
    text = 'Phone backup paused';
  } else if (st.outcome == RunOutcomes.success && st.lastSuccessAt != null) {
    text = 'Backed up ${widgetTime(st.lastSuccessAt!, now)}';
  } else {
    text = outcomeTitle(st.outcome);
  }
  return {
    'backup': text,
    'backup_ready': s.setUp,
    'backup_any_network': settings != null && !settings.conditions.wifiOnly,
  };
}
