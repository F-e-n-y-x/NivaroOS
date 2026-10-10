// Restore extras against the fake server and a stubbed phone: reinstalling
// a backed-up app (one installer session, splits after base.apk) and the
// messages restore writing picture messages after the text ones.
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:nivaroos_mobile/phone_backup/pb_messages.dart';
import 'package:nivaroos_mobile/screens/phone_backup/phone_backup_apps_restore.dart';
import 'package:nivaroos_mobile/screens/phone_backup/phone_backup_messages_restore.dart';

import '../screenshots/harness.dart';
import '../screenshots/phone_backup_fixtures.dart';
import '../screenshots/phone_backup_restore_test.dart' show phoneAppsServer, stubAppsPhone;

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 8; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 20)));
    await tester.pump(const Duration(milliseconds: 200));
  }
}

Future<void> _run(WidgetTester tester, Widget screen, Map<String, Object> overrides, Future<void> Function() body) async {
  await loadRealFonts();
  tester.view.physicalSize = const Size(412, 915) * 3;
  tester.view.devicePixelRatio = 3;
  addTearDown(tester.view.reset);
  final server = FakeServer(overrides: {...phoneServer, ...overrides});
  await http.runWithClient(() async {
    await tester.pumpWidget(testApp(screen));
    await _settle(tester);
    await body();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => server.client);
}

void main() {
  setUp(() async {
    await signIn();
  });

  testWidgets('reinstall: an app with a split goes to one installer session, base.apk first', (tester) async {
    final installs = <Object?>[];
    stubAppsPhone(installs: installs);
    await _run(tester, PhoneAppsRestoreScreen(service: phoneService(state: phoneStateOk), snapshot: 'snap_3'), {
      ...phoneAppsServer,
      'GET /v1/backup/devices/$phoneId/files/content': const FakeResponse('PK-not-really-an-apk'),
    }, () async {
      expect(find.text('Same Version'), findsNothing, reason: 'installed in the backed-up version');
      await tester.tap(find.bySemanticsLabel('Update Maps'));
      await _settle(tester);
      expect(installs, hasLength(1));
      final args = installs.single as Map;
      expect(args['package'], 'org.example.maps');
      final paths = (args['paths'] as List).cast<String>();
      expect(paths, hasLength(2));
      expect(paths.first, endsWith('base.apk'));
      expect(paths.last, endsWith('split_config.arm64_v8a.apk'));
      expect(find.textContaining('Sent to the installer'), findsOneWidget);
    });
  });

  testWidgets('reinstall: installing waits for "Install unknown apps"', (tester) async {
    stubAppsPhone(canInstall: false);
    await _run(tester, PhoneAppsRestoreScreen(service: phoneService(state: phoneStateOk), snapshot: 'snap_3'), phoneAppsServer, () async {
      expect(find.text('Allow'), findsOneWidget);
      final b = tester.widget<FilledButton>(find.widgetWithText(FilledButton, 'Install').first);
      expect(b.onPressed, isNull);
    });
  });

  testWidgets('messages restore writes the picture messages too, skipping the ones on the phone', (tester) async {
    final mms = <Map<Object?, Object?>>[];
    var isDefault = false;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(const MethodChannel('com.fenyx.nivaroos/phone_backup'), (call) async {
      switch (call.method) {
        case 'isDefaultSmsApp':
          return isDefault;
        case 'requestSmsRole':
          return isDefault = true;
        case 'restoreSms':
          return {'inserted': 3, 'skipped': 1, 'mms': 3, 'failed': 0};
        case 'readMms':
          // The phone has the first picture message already.
          return call.arguments['offset'] == 0
              ? [
                  {'_id': 1, 'date': 1700000000, 'msg_box': 1, 'm_id': '<a@mms>', 'addrs': [{'address': '+15550100', 'type': 137}]},
                ]
              : [];
        case 'insertMms':
          mms.add(call.arguments as Map);
          return 'content://mms/9';
      }
      return null;
    });
    String el(String id, int ms) => '<mms date="$ms" msg_box="1" address="+15550100" m_id="$id"><parts><part seq="0" ct="text/plain" text="hi" /></parts>'
        '<addrs><addr address="+15550100" type="137" charset="106" /></addrs></mms>';
    final xml = '<?xml version="1.0"?><smses count="4"><sms address="+15550100" date="1" type="1" body="x" />'
        '${el('&lt;a@mms&gt;', 1700000000000)}${el('&lt;b@mms&gt;', 1700000100000)}${el('&lt;c@mms&gt;', 1700000200000)}</smses>';
    await _run(tester, PhoneMessagesRestoreScreen(service: phoneService(state: phoneStateOk), snapshot: 'snap_3', category: 'sms'), {
      'GET /v1/backup/devices/$phoneId/exports/full': FakeResponse(xml),
    }, () async {
      expect(find.textContaining('picture messages already on the phone are skipped'), findsOneWidget);
      await tester.tap(find.text('Get'));
      await _settle(tester);
      expect(find.text('4 messages ready'), findsOneWidget);
      await tester.tap(find.text('Allow'));
      await _settle(tester);
      await tester.tap(find.text('Restore'));
      await _settle(tester);
      expect([for (final m in mms) (m['pdu'] as Map)['m_id']], ['<b@mms>', '<c@mms>']);
      expect((mms.first['pdu'] as Map)['date'], 1700000100);
      expect(mms.first['recipients'], ['+15550100']);
      expect(find.text('5 added, 2 already there'), findsOneWidget);
    });
  });

  test('the dedup key ignores milliseconds the provider drops', () {
    expect(mmsDedupKey(parseMmsElement('<mms date="1700000000500" msg_box="1" address="+1" m_id="x">')), mmsDedupKey(parseMmsElement('<mms date="1700000000000" msg_box="1" address="+1" m_id="x">')));
  });
}
