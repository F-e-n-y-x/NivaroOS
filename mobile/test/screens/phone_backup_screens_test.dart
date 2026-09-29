// "Back up this phone" against the fake server: first setup (a restricted
// permission refused, enrolment, the saved choices), Change location
// (folder picker, move or start fresh, a missing drive refused), and the
// status page while the backup drive is missing.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/backup/backup_models.dart';
import 'package:nivaroos_mobile/phone_backup/pb_client.dart';
import 'package:nivaroos_mobile/phone_backup/pb_models.dart';
import 'package:nivaroos_mobile/phone_backup/pb_platform.dart';
import 'package:nivaroos_mobile/phone_backup/pb_service.dart';
import 'package:nivaroos_mobile/phone_backup/pb_store.dart';
import 'package:nivaroos_mobile/screens/phone_backup/phone_backup_common.dart';
import 'package:nivaroos_mobile/screens/phone_backup/phone_backup_location.dart';
import 'package:nivaroos_mobile/screens/phone_backup/phone_backup_screen.dart';
import 'package:nivaroos_mobile/screens/phone_backup/phone_backup_setup_screen.dart';

import '../screenshots/backup_test.dart' show backupServer;
import '../screenshots/harness.dart';
import '../screenshots/phone_backup_fixtures.dart';

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 8; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 200));
  }
}

typedef Sent = List<(String, Object?)>;

Future<Sent> _run(WidgetTester tester, Widget screen, Future<void> Function() body, {Map<String, Object> overrides = const {}}) async {
  await loadRealFonts();
  tester.view.physicalSize = const Size(412, 915) * 3;
  tester.view.devicePixelRatio = 3;
  addTearDown(tester.view.reset);
  final server = FakeServer(overrides: {...backupServer, ...phoneServer, ...overrides});
  final inner = server.client;
  final sent = <(String, Object?)>[];
  final client = MockClient((req) async {
    sent.add(('${req.method} ${req.url.path}', req.body.isEmpty ? null : jsonDecode(req.body)));
    final copy = http.Request(req.method, req.url)
      ..headers.addAll(req.headers)
      ..bodyBytes = req.bodyBytes;
    return http.Response.fromStream(await inner.send(copy));
  });
  await http.runWithClient(() async {
    await tester.pumpWidget(testApp(screen));
    await _settle(tester);
    await body();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => client);
  return sent;
}

/// Permissions answered from a map; requests recorded.
class FakePermissions extends PhonePermissions {
  FakePermissions(this.granted);
  final Map<String, bool> granted;
  final List<List<String>> requested = [];
  int settingsOpened = 0;

  @override
  Future<Map<String, bool>> check(List<String> names) async => {for (final n in names) n: granted[n] ?? false};

  @override
  Future<Map<String, bool>> request(List<String> names) async {
    requested.add(names);
    return check(names);
  }

  @override
  Future<void> openSettings() async => settingsOpened++;
}

void main() {
  setUp(() async {
    await signIn();
  });

  testWidgets('first setup: a refused SMS permission explains restricted settings; saving enrols the phone', (tester) async {
    final creds = MemoryCredentialStore();
    final store = PhoneBackupStore(Directory.systemTemp.createTempSync('pbtest'));
    final service = PhoneBackupService(credentials: creds, store: store, platform: false);
    final perms = FakePermissions({
      AndroidPermissions.readMediaImages: true,
      AndroidPermissions.readMediaVideo: true,
      AndroidPermissions.accessMediaLocation: true,
      AndroidPermissions.readContacts: true,
      AndroidPermissions.readCalendar: true,
      AndroidPermissions.readSms: false,
    });
    final sent = await _run(tester, PhoneBackupSetupScreen(service: service, permissions: perms, deviceName: 'Pixel 8'), () async {
      expect(find.text('Back up this phone'), findsOneWidget);

      await tester.tap(find.text('Messages (SMS & MMS)'));
      await tester.pumpAndSettle();
      expect(find.text('Back up messages (sms & mms)?'), findsOneWidget);
      expect(find.textContaining('Allow restricted settings'), findsOneWidget);
      await tester.tap(find.text('Continue'));
      await tester.pumpAndSettle();
      expect(perms.requested.last, [AndroidPermissions.readSms]);
      expect(find.text('Messages (SMS & MMS) not allowed'), findsOneWidget);
      expect(find.textContaining('Tap ⋮ (top right) and “Allow restricted settings”'), findsOneWidget);
      await tester.tap(find.text('Open app info'));
      await tester.pumpAndSettle();
      expect(perms.settingsOpened, 1);
      final sms = tester.widget<SwitchListTile>(find.widgetWithText(SwitchListTile, 'Messages (SMS & MMS)'));
      expect(sms.value, isFalse);

      // Wi-Fi only off, then save.
      await tester.scrollUntilVisible(find.text('Wi-Fi only'), 300, scrollable: find.byType(Scrollable).first);
      await tester.ensureVisible(find.widgetWithText(SwitchListTile, 'Wi-Fi only'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(SwitchListTile, 'Wi-Fi only'));
      await tester.pump();
      expect(tester.widget<SwitchListTile>(find.widgetWithText(SwitchListTile, 'Wi-Fi only')).value, isFalse);
      await tester.scrollUntilVisible(find.text('Start backing up'), 300, scrollable: find.byType(Scrollable).first);
      expect(find.text('On your server, in /DATA/Backup'), findsOneWidget);
      await tester.tap(find.text('Start backing up'));
      await _settle(tester);
    }, overrides: {
      'POST /v1/backup/devices': const FakeResponse({
        'success': 201,
        'message': 'ok',
        'data': {
          'device': {'id': 'dev_new', 'name': 'Pixel 8'},
          'token': 'nvd_q3Zp9m1W0yXcVb7aL2kR5tNe8fHs4dJu6gOi1PwQxYc',
        },
      }, status: 201),
    });
    final enrol = sent.firstWhere((s) => s.$1 == 'POST /v1/backup/devices');
    expect(enrol.$2, {'name': 'Pixel 8', 'platform': 'android'});
    expect(creds.value!.deviceId, 'dev_new');
    expect(creds.value!.server, fakeServer);
    final saved = store.loadSettings()!;
    expect(saved.categories, {PhoneCategory.media, PhoneCategory.contacts, PhoneCategory.calendar});
    expect(saved.conditions.wifiOnly, isFalse);
    expect(store.loadState().nextRunAt, isNotNull);
    expect(perms.requested.last, [AndroidPermissions.postNotifications]);
  });

  testWidgets('change location: pick a USB drive folder, move the backups there', (tester) async {
    final service = phoneService(state: phoneStateOk);
    final sent = await _run(tester, PhoneBackupScreen(service: service), () async {
      await tester.tap(find.text('Change location…'));
      await _settle(tester);
      // The server's folder picker, only places a phone can use.
      expect(find.text('Sandisk 128G'), findsOneWidget);
      expect(find.text('TeraBox'), findsNothing);
      await tester.tap(find.text('Sandisk 128G'));
      await _settle(tester);
      await tester.tap(find.text('Phones'));
      await _settle(tester);
      await tester.tap(find.text('Choose folder'));
      await _settle(tester);
      expect(find.text('What about the backups so far?'), findsOneWidget);
      await tester.tap(find.text('Move them'));
      await _settle(tester);
      expect(find.textContaining('Moving the backups'), findsWidgets);
    }, overrides: {
      'GET /v1/backup/locations/browse': const {
        'success': 200,
        'message': 'ok',
        'data': {
          'path': '',
          'entries': [
            {'name': 'Phones', 'dir': true},
          ],
        },
      },
      'GET /v1/backup/locations/browse?kind=usb&ref_id=3A4F-1C22&path=Phones&dirs_only=1': const {
        'success': 200,
        'message': 'ok',
        'data': {'path': 'Phones', 'entries': []},
      },
      'POST /v1/backup/devices/$phoneId/destination': {
        'success': 200,
        'message': 'ok',
        'data': {
          ...(phoneDetail()['data'] as Map),
          'move': {'state': 'moving', 'target_path': '/media/usb-3A4F/Phones/Pixel 8', 'files': 0, 'total_files': 18287},
        },
      },
    });
    final post = sent.firstWhere((s) => s.$1 == 'POST /v1/backup/devices/$phoneId/destination');
    final body = post.$2 as Map;
    expect(body['mode'], 'move');
    expect((body['location'] as Map)['kind'], 'usb');
    expect((body['location'] as Map)['ref_id'], '3A4F-1C22');
    expect((body['location'] as Map)['sub_path'], 'Phones');
  });

  testWidgets('change location to a drive that is gone: refused, with the reason', (tester) async {
    final detail = PhoneDeviceDetail.fromJson(phoneDetail()['data']);
    await _run(tester, Builder(builder: (context) => Scaffold(
      body: Center(
        child: FilledButton(
          onPressed: () => changePhoneBackupLocation(
            context,
            deviceId: phoneId,
            detail: detail,
            pick: (_) async => const BackupEndpoint(kind: 'usb', refId: '3A4F-1C22', subPath: 'Phones', label: 'Backup Stick'),
          ),
          child: const Text('go'),
        ),
      ),
    )), () async {
      await tester.tap(find.text('go'));
      await _settle(tester);
      await tester.tap(find.text('Start fresh'));
      await _settle(tester);
      expect(find.text('That drive isn’t connected'), findsOneWidget);
      expect(find.textContaining('never written to another disk'), findsOneWidget);
    }, overrides: {
      'POST /v1/backup/devices/$phoneId/destination': const FakeResponse({
        'success': 409,
        'message': 'dest_offline',
        'data': {'error_code': 'dest_offline'},
      }, status: 409),
    });
  });

  testWidgets('the drive holding the backups is missing: a waiting state, not an error', (tester) async {
    final service = phoneService(state: phoneStateWaiting);
    await _run(tester, PhoneBackupScreen(service: service), () async {
      expect(find.text('Backup drive not connected'), findsNWidgets(2));
      expect(find.bySemanticsLabel(RegExp('Tries again')), findsOneWidget);
      expect(find.textContaining('go on by themselves'), findsOneWidget);
      expect(find.text('/media/usb-3A4F/Phones/Pixel 8'), findsOneWidget);
      // Back up now stays available (it waits for the drive).
      expect(find.text('Back up now'), findsOneWidget);
    }, overrides: phoneDriveMissing);
  });

  testWidgets('a revoked token asks to link the phone again', (tester) async {
    final service = phoneService(state: phoneStateOk.copyWith(outcome: 'revoked'));
    await _run(tester, PhoneBackupScreen(service: service), () async {
      expect(find.text('Link this phone again'), findsNWidgets(2));
      expect(find.text('Back up now'), findsNothing);
    }, overrides: {
      'GET /v1/backup/devices/$phoneId/config': const FakeResponse({'success': 401, 'message': 'unauthorized', 'data': {'error_code': 'unauthorized'}}, status: 401),
    });
    expect(DeviceCredential.looksLikeToken('nvd_q3Zp9m1W0yXcVb7aL2kR5tNe8fHs4dJu6gOi1PwQxYc'), isTrue);
  });
}
