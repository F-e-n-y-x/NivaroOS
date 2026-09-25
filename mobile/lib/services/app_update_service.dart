import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:clock/clock.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import 'package:package_info_plus/package_info_plus.dart';
import 'package:path_provider/path_provider.dart';

import 'background_service.dart';
import 'storage_service.dart';

/// A release of the Android app on GitHub.
@immutable
class AppRelease {
  const AppRelease({
    required this.version,
    required this.tag,
    required this.notes,
    required this.apkUrl,
    required this.apkSize,
    required this.pageUrl,
    this.sumsUrl,
    this.publishedAt,
  });

  /// "1.3.0", from the tag (`mobile-v1.3.0`, or `v1.3.0`).
  final String version;
  final String tag;

  /// The changelog (Markdown from the release notes).
  final String notes;
  final String apkUrl;
  final int apkSize;
  final String pageUrl;

  /// The `SHA256SUMS` asset, when the release has one.
  final String? sumsUrl;
  final DateTime? publishedAt;

  Map<String, dynamic> toJson() => {
        'version': version,
        'tag': tag,
        'notes': notes,
        'apk_url': apkUrl,
        'apk_size': apkSize,
        'page_url': pageUrl,
        'sums_url': sumsUrl,
        'published_at': publishedAt?.toIso8601String(),
      };

  static AppRelease? fromJson(Object? j) {
    if (j is! Map) return null;
    final version = j['version'] as String?;
    final apk = j['apk_url'] as String?;
    if (version == null || apk == null) return null;
    return AppRelease(
      version: version,
      tag: j['tag'] as String? ?? version,
      notes: j['notes'] as String? ?? '',
      apkUrl: apk,
      apkSize: (j['apk_size'] as num?)?.toInt() ?? 0,
      pageUrl: j['page_url'] as String? ?? '',
      sumsUrl: j['sums_url'] as String?,
      publishedAt: DateTime.tryParse(j['published_at'] as String? ?? ''),
    );
  }

  /// The newest usable release from GitHub's `/releases` list: not a draft
  /// or pre-release, with an APK asset. Tags named `mobile-v…` (the app's
  /// own releases) win over others.
  static AppRelease? pick(List<dynamic> releases) {
    AppRelease? mobile;
    AppRelease? other;
    for (final r in releases) {
      if (r is! Map || r['draft'] == true || r['prerelease'] == true) continue;
      final tag = (r['tag_name'] ?? '').toString();
      final version = versionFromTag(tag);
      if (version == null) continue;
      String? apk;
      int size = 0;
      String? sums;
      for (final a in (r['assets'] as List<dynamic>? ?? const [])) {
        if (a is! Map) continue;
        final name = (a['name'] ?? '').toString();
        final url = a['browser_download_url']?.toString();
        if (name.toLowerCase().endsWith('.apk') && apk == null) {
          apk = url;
          size = (a['size'] as num?)?.toInt() ?? 0;
        } else if (name == 'SHA256SUMS' || name == 'SHA256SUMS.txt') {
          sums = url;
        }
      }
      if (apk == null) continue;
      final release = AppRelease(
        version: version,
        tag: tag,
        notes: (r['body'] ?? '').toString(),
        apkUrl: apk,
        apkSize: size,
        pageUrl: (r['html_url'] ?? '').toString(),
        sumsUrl: sums,
        publishedAt: DateTime.tryParse((r['published_at'] ?? '').toString()),
      );
      if (tag.startsWith('mobile-')) {
        if (mobile == null || compareVersions(version, mobile.version) > 0) mobile = release;
      } else if (other == null || compareVersions(version, other.version) > 0) {
        other = release;
      }
    }
    return mobile ?? other;
  }

  /// "1.3.0" from `mobile-v1.3.0`, `v1.3.0` or `1.3.0`; null for tags that
  /// aren't versions.
  static String? versionFromTag(String tag) {
    final m = RegExp(r'(\d+\.\d+(?:\.\d+)?)').firstMatch(tag);
    return m?.group(1);
  }
}

/// -1, 0 or 1 comparing dotted versions ("1.10.0" > "1.9.2").
int compareVersions(String a, String b) {
  List<int> parts(String v) => v.split(RegExp(r'[.+-]')).map((p) => int.tryParse(p) ?? 0).toList();
  final pa = parts(a);
  final pb = parts(b);
  for (var i = 0; i < 3; i++) {
    final x = i < pa.length ? pa[i] : 0;
    final y = i < pb.length ? pb[i] : 0;
    if (x != y) return x < y ? -1 : 1;
  }
  return 0;
}

/// The result of an update check.
@immutable
class UpdateCheck {
  const UpdateCheck({required this.installed, required this.checkedAt, this.latest});

  final String installed;
  final AppRelease? latest;
  final DateTime checkedAt;

  bool get updateAvailable => latest != null && compareVersions(latest!.version, installed) > 0;
}

/// Why an update stopped, in words for the user.
class UpdateException implements Exception {
  const UpdateException(this.message, {this.needsInstallPermission = false});
  final String message;

  /// Android needs "Install unknown apps" allowed for this app first.
  final bool needsInstallPermission;
  @override
  String toString() => message;
}

/// Self-update from GitHub releases (plan WPR-2): the app is sideloaded
/// only (§7.1), so this is how fixes reach phones.
///
/// Install steps: download the APK, check its SHA-256 against the
/// release's `SHA256SUMS`, check that it is this app signed with the same
/// certificate as the installed one (signature continuity: Android refuses
/// an update signed with another key, and a new key would force everyone
/// to uninstall and lose their data - so a mismatch stops here, with a
/// clear message), then hand it to Android's package installer.
class AppUpdateService {
  AppUpdateService._();
  static final AppUpdateService instance = AppUpdateService._();

  static const repo = 'F-e-n-y-x/NivaroOS';
  static const releasesPage = 'https://github.com/$repo/releases';

  /// Where releases are listed. Tests point it at their fake server.
  @visibleForTesting
  static String releasesApi = 'https://api.github.com/repos/$repo/releases?per_page=30';
  static const _channel = MethodChannel('com.fenyx.nivaroos/app_update');

  /// Checks at most this often on its own ([check] with force: false).
  static const checkEvery = Duration(hours: 24);

  /// The installed version ("1.2.1") and build number ("6").
  Future<({String version, String build})> installedVersion() async {
    final info = await PackageInfo.fromPlatform();
    return (version: info.version, build: info.buildNumber);
  }

  /// The last check's result, if one is saved.
  Future<UpdateCheck?> cached() async {
    try {
      final raw = await StorageService.instance.getUpdateCheck();
      if (raw == null) return null;
      final j = jsonDecode(raw) as Map<String, dynamic>;
      final installed = (await installedVersion()).version;
      final at = DateTime.tryParse(j['checked_at'] as String? ?? '');
      if (at == null) return null;
      return UpdateCheck(installed: installed, checkedAt: at, latest: AppRelease.fromJson(j['latest']));
    } catch (_) {
      return null;
    }
  }

  /// Asks GitHub for the newest release, unless the saved check is less
  /// than [checkEvery] old and [force] is false. Throws [UpdateException]
  /// when GitHub can't be reached.
  Future<UpdateCheck> check({bool force = false}) async {
    if (!force) {
      final last = await cached();
      if (last != null && clock.now().difference(last.checkedAt) < checkEvery) return last;
    }
    final installed = (await installedVersion()).version;
    final http.Response res;
    try {
      res = await http.get(
        Uri.parse(releasesApi),
        headers: {'Accept': 'application/vnd.github+json', 'User-Agent': 'NivaroOS-Android'},
      ).timeout(const Duration(seconds: 15));
    } catch (_) {
      throw const UpdateException("Couldn't reach GitHub to check for updates. Check the phone's internet connection.");
    }
    if (res.statusCode == 403 || res.statusCode == 429) {
      throw const UpdateException('GitHub is limiting update checks from this network. Try again in an hour.');
    }
    if (res.statusCode != 200) throw UpdateException("Couldn't check for updates (GitHub answered ${res.statusCode}).");
    final List<dynamic> list;
    try {
      list = jsonDecode(res.body) as List<dynamic>;
    } catch (_) {
      throw const UpdateException("Couldn't read GitHub's answer. Try again later.");
    }
    final result = UpdateCheck(installed: installed, checkedAt: clock.now(), latest: AppRelease.pick(list));
    await StorageService.instance.setUpdateCheck(jsonEncode({
      'checked_at': result.checkedAt.toIso8601String(),
      'latest': result.latest?.toJson(),
    }));
    return result;
  }

  /// Downloads [release]'s APK into the app's cache, reporting bytes
  /// received and the total (0 when unknown).
  Future<File> download(AppRelease release, {void Function(int received, int total)? onProgress}) async {
    final dir = await getTemporaryDirectory();
    final file = File('${dir.path}/nivaroos-${release.version}.apk');
    final client = http.Client();
    try {
      final res = await client.send(http.Request('GET', Uri.parse(release.apkUrl))..headers['User-Agent'] = 'NivaroOS-Android');
      if (res.statusCode != 200) throw UpdateException('The download failed (HTTP ${res.statusCode}). Try again.');
      final total = res.contentLength ?? release.apkSize;
      final sink = file.openWrite();
      var received = 0;
      try {
        await for (final chunk in res.stream) {
          sink.add(chunk);
          received += chunk.length;
          onProgress?.call(received, total);
        }
      } finally {
        await sink.close();
      }
      if (total > 0 && received != total) throw const UpdateException('The download was cut off. Try again.');
      return file;
    } on UpdateException {
      rethrow;
    } catch (_) {
      throw const UpdateException('The download failed. Check the connection and try again.');
    } finally {
      client.close();
    }
  }

  /// The expected SHA-256 of [apkName] from a `SHA256SUMS` file
  /// (`<hash>  <name>` lines, as `sha256sum` writes them).
  static String? expectedHash(String sums, String apkName) {
    for (final line in const LineSplitter().convert(sums)) {
      final m = RegExp(r'^([0-9a-fA-F]{64})\s+\*?(.+)$').firstMatch(line.trim());
      if (m != null && m.group(2)!.trim() == apkName) return m.group(1)!.toLowerCase();
    }
    return null;
  }

  /// Checks [apk] before it is installed: checksum (when the release
  /// publishes one) and signing certificate. Throws [UpdateException].
  Future<void> verify(AppRelease release, File apk) async {
    if (!BackgroundService.isAndroid) throw const UpdateException('Updates install on Android only.');
    final sumsUrl = release.sumsUrl;
    if (sumsUrl != null) {
      final String sums;
      try {
        final res = await http.get(Uri.parse(sumsUrl)).timeout(const Duration(seconds: 15));
        if (res.statusCode != 200) throw Exception();
        sums = res.body;
      } catch (_) {
        throw const UpdateException("Couldn't download the release's checksums, so the update wasn't installed. Try again.");
      }
      final name = Uri.parse(release.apkUrl).pathSegments.last;
      final expected = expectedHash(sums, name);
      final actual = await _channel.invokeMethod<String>('sha256', {'path': apk.path});
      if (expected == null || actual == null || expected != actual.toLowerCase()) {
        await _discard(apk);
        throw const UpdateException("The downloaded file doesn't match the release's checksum, so it wasn't installed.");
      }
    }
    final info = await _channel.invokeMethod<Object?>('apkInfo', {'path': apk.path});
    if (info is! Map) {
      await _discard(apk);
      throw const UpdateException("The downloaded file isn't a valid app package.");
    }
    final ownPackage = (await PackageInfo.fromPlatform()).packageName;
    if (info['packageName'] != ownPackage) {
      await _discard(apk);
      throw const UpdateException('The downloaded app is not NivaroOS for Android, so it wasn\'t installed.');
    }
    final installed = ((await _channel.invokeMethod<List<Object?>>('installedSigners')) ?? const []).whereType<String>().toSet();
    final signers = (info['signers'] as List<Object?>? ?? const []).whereType<String>().toList();
    if (!signaturesContinue(installed, signers)) {
      await _discard(apk);
      throw const UpdateException(
        "This update is signed with a different key than the app on this phone, so Android can't install it over this one. "
        "It wasn't installed. Get the app from the project's releases page.",
      );
    }
  }

  /// True when the APK's current signer ([apkSigners], oldest first as
  /// Android lists a signing history) is one the installed app is signed
  /// with.
  @visibleForTesting
  static bool signaturesContinue(Set<String> installed, List<String> apkSigners) =>
      installed.isNotEmpty && apkSigners.isNotEmpty && installed.contains(apkSigners.last);

  /// Hands [apk] to Android's installer, which asks the user to confirm.
  Future<void> install(File apk) async {
    final allowed = await _channel.invokeMethod<bool>('canRequestInstalls') ?? false;
    if (!allowed) {
      throw const UpdateException('Allow NivaroOS to install apps, then tap Install again.', needsInstallPermission: true);
    }
    try {
      await _channel.invokeMethod('install', {'path': apk.path});
    } on PlatformException catch (e) {
      throw UpdateException("Android couldn't start the install: ${e.message ?? 'unknown error'}.");
    }
  }

  Future<void> openInstallPermissionSettings() async {
    try {
      await _channel.invokeMethod('openInstallSettings');
    } catch (_) {}
  }

  Future<void> _discard(File f) async {
    try {
      await f.delete();
    } catch (_) {}
  }
}
