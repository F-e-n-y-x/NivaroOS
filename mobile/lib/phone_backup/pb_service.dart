// "Back up this phone" for the app's screens: set up (enrol with the
// owner's session, once), save the choices and schedule the job, back up
// now, stop, unlink; and the owner-only calls of the phone's page (its
// status on the server, change location). The backup itself always runs
// in the job (PhoneBackupJobService + [runPhoneBackupJob]), never in the
// UI isolate, so there is one runner.
import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import 'package:package_info_plus/package_info_plus.dart';

import '../backup/backup_api.dart';
import '../backup/backup_models.dart';
import '../services/api_client.dart';
import 'pb_client.dart';
import 'pb_engine.dart';
import 'pb_models.dart';
import 'pb_platform.dart';
import 'pb_schedule.dart';
import 'pb_store.dart';

/// The owner's (admin JWT) routes for phones.
class PhoneOwnerApi {
  PhoneOwnerApi([ApiClient? client]) : _c = client ?? ApiClient.instance;
  final ApiClient _c;

  Future<T> _call<T>(Future<Map<String, dynamic>> Function() request, T Function(Object? data) parse) async {
    try {
      final res = await request();
      return parse(res['data']);
    } catch (e) {
      throw BackupError.from(e);
    }
  }

  /// POST /devices: the device and its token (shown only now).
  Future<({PhoneDevice device, String token})> enroll(String name) => _call(
        () => _c.post('/backup/devices', body: {'name': name, 'platform': 'android'}),
        (d) {
          final m = d is Map ? d : const {};
          return (device: PhoneDevice.fromJson(m['device']), token: m['token']?.toString() ?? '');
        },
      );

  Future<List<PhoneDevice>> devices() => _call(() => _c.get('/backup/devices'), (d) => [for (final x in d is List ? d : const []) PhoneDevice.fromJson(x)]);

  Future<PhoneDeviceDetail> detail(String id) => _call(() => _c.get('/backup/devices/${Uri.encodeComponent(id)}'), PhoneDeviceDetail.fromJson);

  /// Change location: [location] null goes back to the default; [mode]
  /// move | fresh (required when the phone has backups).
  Future<PhoneDeviceDetail> setDestination(String id, BackupEndpoint? location, {String? mode}) => _call(
        () => _c.post('/backup/devices/${Uri.encodeComponent(id)}/destination', body: {'location': location?.toJson(), 'mode': ?mode}),
        PhoneDeviceDetail.fromJson,
      );

  /// Stops the phone's token; its backups stay browsable.
  Future<void> revoke(String id) => _call(() => _c.post('/backup/devices/${Uri.encodeComponent(id)}/revoke'), (_) {});
}

/// What the status screen shows.
class PhoneBackupStatus {
  const PhoneBackupStatus({this.credential, this.settings, this.state = const PhoneBackupState()});
  final DeviceCredential? credential;
  final PhoneBackupSettings? settings;
  final PhoneBackupState state;

  bool get enrolled => credential != null;
  bool get setUp => credential != null && settings != null;
}

class PhoneBackupService {
  PhoneBackupService({CredentialStore? credentials, PhoneBackupStore? store, PhoneOwnerApi? owner, this.platform = true, this.httpClient})
      : _credentials = credentials ?? const SecureCredentialStore(),
        // ignore: prefer_initializing_formals
        _store = store,
        owner = owner ?? PhoneOwnerApi();

  final CredentialStore _credentials;
  PhoneBackupStore? _store;
  final PhoneOwnerApi owner;

  /// False in tests: no platform channel calls.
  final bool platform;

  /// For tests: the device calls' HTTP client.
  final http.Client? httpClient;

  static PhoneBackupService instance = PhoneBackupService();

  Future<PhoneBackupStore> get store async => _store ??= await PhoneBackupStore.open();

  Future<PhoneBackupStatus> status() async {
    final s = await store;
    return PhoneBackupStatus(credential: await _credentials.load(), settings: s.loadSettings(), state: s.loadState());
  }

  Future<DeviceCredential?> credential() => _credentials.load();

  /// A device client with the stored token (null when not linked).
  Future<DeviceClient?> deviceClient() async {
    final c = await _credentials.load();
    return c == null ? null : DeviceClient(c, client: httpClient, onRotated: _credentials.save);
  }

  /// Links this phone to the signed-in server: the owner's session makes
  /// a device and its token, kept in secure storage.
  Future<DeviceCredential> enroll(String name) async {
    final r = await owner.enroll(name);
    if (!DeviceCredential.looksLikeToken(r.token) || r.device.id.isEmpty) throw BackupError('internal');
    final c = DeviceCredential(server: ApiClient.instance.baseUrl, deviceId: r.device.id, token: r.token, name: r.device.name, enrolledAt: clock.now());
    await _credentials.save(c);
    return c;
  }

  /// Saves the choices and (re)schedules the next run.
  Future<void> saveSettings(PhoneBackupSettings settings) async {
    final s = await store;
    s.saveSettings(settings);
    await reschedule();
    unawaited(_reportSettings(settings));
  }

  Future<void> reschedule() async {
    final s = await store;
    final settings = s.loadSettings();
    if (settings == null) return;
    final st = s.loadState();
    final now = clock.now();
    final next = settings.paused ? null : settings.schedule.nextRun(now: now, lastBackup: st.lastSuccessAt);
    s.saveState(st.copyWith(nextRunAt: next, clearNextRun: next == null));
    if (!platform) return;
    try {
      if (next == null) {
        await const ChannelPhoneSources().cancelSchedule();
      } else {
        final d = next.difference(now);
        await const ChannelPhoneSources().schedule(delay: d.isNegative ? Duration.zero : d, wifiOnly: settings.conditions.wifiOnly, charging: settings.conditions.chargingOnly);
      }
    } on MissingPluginException {
      // Not on Android.
    }
  }

  Future<void> _reportSettings(PhoneBackupSettings settings) async {
    final client = await deviceClient();
    if (client == null || !platform) return;
    try {
      final info = await ChannelPhoneSources.deviceInfo();
      final pkg = await PackageInfo.fromPlatform();
      await client.putPhoneSettings(settings.toPhoneSettings(appVersion: pkg.version, osVersion: info['os'] ?? '', model: info['model'] ?? ''));
    } catch (_) {}
  }

  /// Starts a backup now (the job runs it). [anyNetwork] allows mobile data.
  Future<bool> backUpNow({bool anyNetwork = false}) async {
    final s = await store;
    s.clearCancel();
    if (!platform) return true;
    final ok = await ChannelPhoneSources.runNow(anyNetwork: anyNetwork);
    if (ok) s.updateState((st) => st.copyWith(running: true, heartbeat: clock.now(), progress: const RunProgress(phase: 'Starting'), outcome: ''));
    return ok;
  }

  /// Asks the running backup to stop (it finishes as cancelled).
  Future<void> stop() async => (await store).requestCancel();

  /// Forgets this phone's link: the token, the schedule and the local
  /// lists. With [revokeOnServer] (the owner's session) the server stops
  /// the token too; the backups stay there.
  Future<void> unlink({bool revokeOnServer = false}) async {
    final c = await _credentials.load();
    if (c != null && revokeOnServer) {
      try {
        await owner.revoke(c.deviceId);
      } catch (_) {}
    }
    await _credentials.clear();
    final s = await store;
    s.resetRunData();
    if (platform) {
      try {
        await const ChannelPhoneSources().cancelSchedule();
      } on MissingPluginException {
        // Not on Android.
      }
    }
  }
}

/// The backup job's Dart side (background_sync_isolate.dart
/// `phoneBackupMain`): one run or slice, then "done".
Future<void> runPhoneBackupJob() async {
  const job = MethodChannel('com.fenyx.nivaroos/phone_backup_job');
  var more = false;
  try {
    final m = await job.invokeMapMethod<String, Object?>('start') ?? const {};
    final budgetMs = (m['budgetMs'] as num?)?.toInt() ?? 0;
    final start = JobStart(reason: m['reason']?.toString() ?? 'schedule', budget: budgetMs > 0 ? Duration(milliseconds: budgetMs) : null, userInitiated: m['userInitiated'] == true);
    const creds = SecureCredentialStore();
    final store = await PhoneBackupStore.open();
    final settings = store.loadSettings();
    final cred = await creds.load();
    if (settings == null || cred == null || (settings.paused && start.reason == 'schedule')) return;
    final info = await ChannelPhoneSources.deviceInfo();
    var version = '';
    try {
      version = (await PackageInfo.fromPlatform()).version;
    } catch (_) {}
    final client = DeviceClient(cred, onRotated: creds.save);
    final engine = PhoneBackupEngine(
      client: client,
      phone: const ChannelPhoneSources(),
      store: store,
      settings: settings,
      appVersion: version,
      osVersion: info['os'] ?? '',
      model: info['model'] ?? '',
    );
    final r = await engine.run(start);
    more = r.end == RunEnd.more;
    client.close();
  } catch (_) {
    // Never leave the job hanging.
  } finally {
    try {
      await job.invokeMethod('done', {'more': more});
    } catch (_) {}
  }
}

/// Labels for the status screen.
String outcomeTitle(String outcome) => switch (outcome) {
      RunOutcomes.success => 'Backed up',
      RunOutcomes.partial => 'Backed up, with problems',
      RunOutcomes.waitingDrive => 'Backup drive not connected',
      RunOutcomes.waitingNetwork => 'Waiting for the server',
      RunOutcomes.noSpace => 'Backup drive is full',
      RunOutcomes.revoked => 'Link this phone again',
      RunOutcomes.failed => 'Backup failed',
      RunOutcomes.cancelled => 'Backup stopped',
      RunOutcomes.destProblem => 'Backup location needs you',
      _ => 'Not backed up yet',
    };

/// The schedule's words with its conditions: "Every day at 2:00 · Wi-Fi only".
String scheduleSummary(PhoneBackupSettings s) => [
      s.schedule.label,
      if (s.schedule.kind != ScheduleKind.manual && s.conditions.wifiOnly) 'Wi-Fi only',
      if (s.schedule.kind != ScheduleKind.manual && s.conditions.chargingOnly) 'while charging',
    ].join(' · ');
