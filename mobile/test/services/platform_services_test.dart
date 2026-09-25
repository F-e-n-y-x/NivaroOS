// Companion hardening (M-20, M-21), per-server state (M-17), Tailscale
// states (M-10) and the self-update checks (WPR-2).
import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/server_profile.dart';
import 'package:nivaroos_mobile/services/app_update_service.dart';
import 'package:nivaroos_mobile/services/companion_file_server.dart';
import 'package:nivaroos_mobile/services/device_sync_service.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/services/tailscale_service.dart';

Future<void> _storage(Map<String, String> values) async {
  StorageService.instance.resetForTest();
  FlutterSecureStorage.setMockInitialValues(values);
  await StorageService.instance.init();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  group('shared storage policy (M-20)', () {
    String? ok(String p, {bool allowRoot = true}) {
      try {
        return SharedStoragePolicy.resolve(p, allowRoot: allowRoot);
      } on PathRefused {
        return null;
      }
    }

    test('allows the shared volumes', () {
      expect(ok('/storage/emulated/0/DCIM/Camera'), '/storage/emulated/0/DCIM/Camera');
      expect(ok('/storage/emulated/10/Download'), '/storage/emulated/10/Download');
      expect(ok('/storage/1A2B-3C4D/Music'), '/storage/1A2B-3C4D/Music');
      expect(ok('/storage/emulated/0/Android/media/com.whatsapp'), isNotNull);
      expect(ok('/storage/emulated/0'), '/storage/emulated/0');
    });

    test("refuses the app's private files, other apps' data and the system", () {
      expect(ok('/data/user/0/com.fenyx.nivaroos_mobile/shared_prefs'), isNull);
      expect(ok('/data/data/com.fenyx.nivaroos_mobile'), isNull);
      expect(ok('/storage/emulated/0/Android/data/com.other.app'), isNull);
      expect(ok('/storage/emulated/0/Android/obb'), isNull);
      expect(ok('/system/etc/hosts'), isNull);
      expect(ok('/storage/self/primary'), isNull);
      expect(ok('relative/path'), isNull);
    });

    test('.. cannot climb out', () {
      expect(ok('/storage/emulated/0/../../../data/user/0'), isNull);
      expect(ok('/storage/emulated/0/DCIM/../../0/Pictures'), '/storage/emulated/0/Pictures');
      expect(ok('/../../etc'), isNull);
    });

    test('the volume root itself cannot be deleted or renamed', () {
      expect(ok('/storage/emulated/0', allowRoot: false), isNull);
      expect(ok('/storage/emulated/0/', allowRoot: false), isNull);
      expect(ok('/storage/emulated/0/Old', allowRoot: false), isNotNull);
    });

    test('a tunnel listing outside shared storage is refused', () {
      final res = CompanionFileServer.listForTunnel('/data/user/0');
      expect(res['success'], isFalse);
      expect(res['files'], isEmpty);
    });

    test('secrets compare in full', () {
      expect(CompanionFileServer.secretsMatch('abc123', 'abc123'), isTrue);
      expect(CompanionFileServer.secretsMatch('abc12', 'abc123'), isFalse);
      expect(CompanionFileServer.secretsMatch('abc1234', 'abc123'), isFalse);
      expect(CompanionFileServer.secretsMatch(null, 'abc123'), isFalse);
      expect(CompanionFileServer.secretsMatch('', 'abc123'), isFalse);
    });
  });

  group('device data is never made up (M-21)', () {
    test('missing numbers stay unknown', () {
      final d = CompanionDevice.fromJson({'id': 'dev_1', 'name': 'Tab', 'battery_level': 0, 'connection': 'offline'});
      expect(d.battery, isNull);
      expect(d.storageTotal, isNull);
      expect(d.connection, CompanionConnection.offline);
      expect(d.isOnline, isFalse);
    });

    test('registration leaves unknown numbers out', () {
      final d = CompanionDevice(id: 'dev_1', name: 'Phone', model: 'Pixel', osVersion: '', appVersion: '', platform: 'Android', ipAddress: '');
      final body = d.toRegistration(sharing: false, port: 8765);
      expect(body.containsKey('battery_level'), isFalse);
      expect(body.containsKey('storage_total'), isFalse);
      expect(body.containsKey('port'), isFalse);
      expect(body['shares_storage'], isFalse);
      expect(body['id'], 'dev_1');
    });
  });

  group('per-server state (M-17)', () {
    test('the companion secret belongs to one server, and the old single one moves to the current server', () async {
      await _storage({'server_url': 'http://a.test', 'companion_secret': 'legacy'});
      expect(await StorageService.instance.getCompanionSecret(), 'legacy');
      await StorageService.instance.setServerUrl('http://b.test');
      expect(await StorageService.instance.getCompanionSecret(), isNull);
      await StorageService.instance.setCompanionSecret('for-b');
      await StorageService.instance.setServerUrl('http://a.test/');
      expect(await StorageService.instance.getCompanionSecret(), 'legacy');
    });

    test('refreshed tokens are written back to the active profile', () async {
      await _storage({
        'server_url': 'http://a.test',
        'active_profile_id': 'p1',
        'saved_server_profiles': jsonEncode([
          ServerProfile(id: 'p1', name: 'A', url: 'http://a.test', username: 'alex', accessToken: 'old', refreshToken: 'old').toJson(),
          ServerProfile(id: 'p2', name: 'B', url: 'http://b.test', username: 'alex', accessToken: 'b', refreshToken: 'b').toJson(),
        ]),
      });
      await StorageService.instance.setSession(accessToken: 'new', refreshToken: 'new-r', username: 'alex');
      final profiles = await StorageService.instance.getProfiles();
      expect(profiles.firstWhere((p) => p.id == 'p1').refreshToken, 'new-r');
      expect(profiles.firstWhere((p) => p.id == 'p2').refreshToken, 'b');
    });

    test('an ended session removes the tokens from the profile too', () async {
      await _storage({
        'server_url': 'http://a.test',
        'access_token': 'x',
        'refresh_token': 'y',
        'saved_server_profiles': jsonEncode([
          ServerProfile(id: 'p1', name: 'A', url: 'http://a.test', username: 'alex', accessToken: 'x', refreshToken: 'y').toJson(),
        ]),
      });
      await StorageService.instance.clearSession();
      final p = (await StorageService.instance.getProfiles()).single;
      expect(p.hasSession, isFalse);
      expect(p.url, 'http://a.test');
    });

    test('switching to a profile without a session leaves no old tokens behind', () async {
      await _storage({'server_url': 'http://a.test', 'access_token': 'x', 'refresh_token': 'y'});
      await StorageService.instance.switchProfile(ServerProfile(id: 'p2', name: 'B', url: 'http://b.test', username: 'sam'));
      expect(await StorageService.instance.getServerUrl(), 'http://b.test');
      expect(await StorageService.instance.getAccessToken(), isNull);
      expect(await StorageService.instance.getUsername(), 'sam');
    });

    test('old default names show the host', () {
      expect(ServerProfile(id: 'd', name: 'Primary Server', url: 'http://nas.local', username: '').displayName, 'nas.local');
      expect(ServerProfile(id: 'd', name: 'Home', url: 'http://nas.local', username: '').displayName, 'Home');
    });
  });

  group('Tailscale (M-10)', () {
    test('states', () {
      TailscaleState of(Map<String, dynamic> j) => TailscaleStatus.fromJson(j).state;
      expect(of({'BackendState': 'Running'}), TailscaleState.running);
      expect(of({'BackendState': 'NeedsLogin'}), TailscaleState.needsLogin);
      expect(of({'BackendState': 'Stopped'}), TailscaleState.stopped);
      expect(of({'BackendState': 'NoDaemon'}), TailscaleState.noDaemon);
      expect(of({'BackendState': 'Stopped', 'AuthURL': 'https://login.tailscale.com/a/x'}), TailscaleState.needsLogin);
      expect(TailscaleStatus.absent().state, TailscaleState.notInstalled);
    });

    test('peers: online first, zero times are "never"', () {
      final s = TailscaleStatus.fromJson({
        'BackendState': 'Running',
        'Self': {'HostName': 'atom', 'DNSName': 'atom.example.ts.net.', 'TailscaleIPs': ['100.64.0.1']},
        'Peer': {
          'a': {'HostName': 'zed', 'Online': true, 'LastSeen': '0001-01-01T00:00:00Z'},
          'b': {'HostName': 'alpha', 'Online': false, 'LastSeen': '2026-09-20T10:00:00Z'},
        },
      });
      expect(s.dnsName, 'atom.example.ts.net');
      expect(s.peers.map((p) => p.hostName), ['zed', 'alpha']);
      expect(s.peers.first.lastSeen, isNull);
      expect(s.peers.last.lastSeen, isNotNull);
    });

    test('knows when the app itself goes through Tailscale', () {
      expect(TailscaleService.isTailscaleAddress('http://100.64.0.1'), isTrue);
      expect(TailscaleService.isTailscaleAddress('http://100.127.255.1'), isTrue);
      expect(TailscaleService.isTailscaleAddress('https://atom.example.ts.net'), isTrue);
      expect(TailscaleService.isTailscaleAddress('http://100.128.0.1'), isFalse);
      expect(TailscaleService.isTailscaleAddress('http://192.168.1.20'), isFalse);
    });
  });

  group('self-update (WPR-2)', () {
    Map<String, Object?> release(String tag, {bool apk = true, bool pre = false}) => {
          'tag_name': tag,
          'prerelease': pre,
          'draft': false,
          'body': 'notes',
          'html_url': 'https://github.com/x/releases/$tag',
          'assets': [
            if (apk) {'name': 'nivaroos-android.apk', 'browser_download_url': 'https://github.com/x/$tag/nivaroos-android.apk', 'size': 1000},
            {'name': 'SHA256SUMS', 'browser_download_url': 'https://github.com/x/$tag/SHA256SUMS'},
          ],
        };

    test('picks the newest mobile release with an APK', () {
      final r = AppRelease.pick([
        release('v2.0.0'),
        release('mobile-v1.4.0', pre: true),
        release('mobile-v1.3.0'),
        release('mobile-v1.10.0', apk: false),
        release('mobile-v1.2.9'),
      ])!;
      expect(r.version, '1.3.0');
      expect(r.sumsUrl, isNotNull);
      expect(AppRelease.pick([release('server-only', apk: false)]), isNull);
    });

    test('compares versions numerically', () {
      expect(compareVersions('1.10.0', '1.9.2'), 1);
      expect(compareVersions('1.2.1', '1.2.1'), 0);
      expect(compareVersions('1.2', '1.2.1'), -1);
      expect(UpdateCheck(installed: '1.2.1', checkedAt: DateTime(2026), latest: AppRelease.pick([release('mobile-v1.3.0')])).updateAvailable, isTrue);
    });

    test('reads SHA256SUMS', () {
      const sums = 'aa11bb22cc33dd44ee55ff66aa11bb22cc33dd44ee55ff66aa11bb22cc33dd44  nivaroos-android.apk\n'
          '0000000000000000000000000000000000000000000000000000000000000000 *other.apk\n';
      expect(AppUpdateService.expectedHash(sums, 'nivaroos-android.apk'), 'aa11bb22cc33dd44ee55ff66aa11bb22cc33dd44ee55ff66aa11bb22cc33dd44');
      expect(AppUpdateService.expectedHash(sums, 'other.apk'), '0' * 64);
      expect(AppUpdateService.expectedHash(sums, 'missing.apk'), isNull);
    });

    test('an APK signed with another key does not continue the signature', () {
      expect(AppUpdateService.signaturesContinue({'k1'}, ['k1']), isTrue);
      // A rotated key whose history includes ours, current last.
      expect(AppUpdateService.signaturesContinue({'k1', 'k2'}, ['k1', 'k2']), isTrue);
      expect(AppUpdateService.signaturesContinue({'k1'}, ['k9']), isFalse);
      expect(AppUpdateService.signaturesContinue({'k1'}, []), isFalse);
      expect(AppUpdateService.signaturesContinue({}, ['k1']), isFalse);
    });
  });
}
