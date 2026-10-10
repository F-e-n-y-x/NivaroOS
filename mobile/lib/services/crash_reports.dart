import 'dart:convert';
import 'dart:io';

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:path_provider/path_provider.dart';

import 'api_client.dart';
import 'device_sync_service.dart';

/// One uncaught error, as the phone saw it.
class CrashReport {
  const CrashReport({required this.time, required this.error, required this.stack, required this.source});

  final DateTime time;
  final String error;
  final String stack;

  /// flutter (a framework error) or platform (an uncaught async error).
  final String source;

  Map<String, dynamic> toJson() => {'time': time.toUtc().toIso8601String(), 'error': error, 'stack': stack, 'source': source};

  static CrashReport fromJson(Map<String, dynamic> j) => CrashReport(
    time: DateTime.tryParse(j['time'] as String? ?? '')?.toLocal() ?? DateTime.fromMillisecondsSinceEpoch(0),
    error: j['error'] as String? ?? '',
    stack: j['stack'] as String? ?? '',
    source: j['source'] as String? ?? '',
  );
}

/// Uncaught errors kept on the phone: the last [keep], in a file only this
/// app reads. Nothing leaves the phone unless the user taps Send, and then
/// only to their own NivaroOS server (no third-party crash service).
class CrashReports {
  CrashReports._();
  static final instance = CrashReports._();

  static const keep = 20;
  static const _maxStack = 16 * 1024;

  /// Where the reports live; tests point it at a temp folder.
  @visibleForTesting
  static Future<File> Function() fileOverride = _defaultFile;
  static Future<File> _defaultFile() async => File('${(await getApplicationSupportDirectory()).path}/crash_reports.json');

  final reports = ValueNotifier<List<CrashReport>>(const []);
  Future<void> _writing = Future.value();

  /// Catches what nothing else did: framework errors (FlutterError.onError,
  /// still shown in debug builds) and uncaught async errors anywhere in the
  /// root zone (PlatformDispatcher.onError, which also covers what a
  /// runZonedGuarded would). Call once, right after
  /// WidgetsFlutterBinding.ensureInitialized().
  void install() {
    final previous = FlutterError.onError;
    FlutterError.onError = (details) {
      previous?.call(details);
      record(details.exception, details.stack, source: 'flutter');
    };
    PlatformDispatcher.instance.onError = (error, stack) {
      record(error, stack, source: 'platform');
      return true;
    };
    load();
  }

  Future<void> load() async {
    try {
      final f = await fileOverride();
      if (!await f.exists()) return;
      final list = jsonDecode(await f.readAsString()) as List;
      reports.value = [for (final j in list) CrashReport.fromJson(Map<String, dynamic>.from(j as Map))];
    } catch (_) {
      // A damaged file is not worth a crash of its own.
    }
  }

  void record(Object error, StackTrace? stack, {required String source}) {
    var s = stack?.toString() ?? '';
    if (s.length > _maxStack) s = '${s.substring(0, _maxStack)}\n…';
    final r = CrashReport(time: clock.now(), error: error.toString(), stack: s, source: source);
    final next = [r, ...reports.value];
    reports.value = next.length > keep ? next.sublist(0, keep) : next;
    _save();
  }

  Future<void> clear() async {
    reports.value = const [];
    await _save();
  }

  Future<void> _save() {
    final snapshot = reports.value;
    return _writing = _writing.then((_) async {
      try {
        final f = await fileOverride();
        await f.parent.create(recursive: true);
        await f.writeAsString(jsonEncode([for (final r in snapshot) r.toJson()]));
      } catch (_) {}
    });
  }

  /// Sends every kept report to the signed-in server; throws on failure.
  Future<void> send() async {
    final list = reports.value;
    if (list.isEmpty) return;
    var version = '';
    var device = '';
    try {
      final p = await PackageInfo.fromPlatform();
      version = '${p.version}+${p.buildNumber}';
    } catch (_) {}
    try {
      final d = await DeviceSyncService.instance.getLocalDeviceInfo();
      device = [d.name, d.model, d.osVersion].where((s) => s.isNotEmpty).join(' · ');
    } catch (_) {}
    await ApiClient.instance.post(
      '/v1/sys/crash-reports',
      body: {
        'app': 'nivaroos-android',
        'app_version': version,
        'device': device,
        'os': Platform.operatingSystemVersion,
        'reports': [for (final r in list) r.toJson()],
      },
    );
  }
}
