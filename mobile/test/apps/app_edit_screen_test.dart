// Edit app: what Save sends. The server takes the whole compose file back
// as YAML on PUT /v2/app_management/compose/{id} (the web form's
// applyComposeAppSettings), with the user's edits and nothing else changed.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/models/compose_edit.dart';
import 'package:nivaroos_mobile/models/container_entry.dart';
import 'package:nivaroos_mobile/screens/app_edit_screen.dart';
import 'package:nivaroos_mobile/utils/app_icons.dart';

import '../screenshots/harness.dart';

const _app = InstalledApp(id: 'jellyfin', title: 'Jellyfin', kind: AppKind.compose, status: 'running');
const _path = '/v2/app_management/compose/jellyfin';

final String _yaml = File('test/screenshots/fixtures/v2/app_management/compose/jellyfin.yaml').readAsStringSync();

class _Server {
  _Server({this.putStatus = 200, this.putBody});

  final int putStatus;
  final Object? putBody;
  final puts = <http.Request>[];
  final gets = <http.Request>[];

  late final client = MockClient((req) async {
    if (req.url.path == _path && req.method == 'GET') {
      gets.add(req);
      return http.Response(_yaml, 200, headers: {'content-type': 'application/yaml'});
    }
    if (req.url.path == _path && req.method == 'PUT') {
      puts.add(req);
      return http.Response(
        jsonEncode(putBody ?? {'message': 'compose app is being applied with changes asynchroniously'}),
        putStatus,
        headers: {'content-type': 'application/json'},
      );
    }
    return http.Response(jsonEncode({'message': 'not found'}), 404);
  });
}

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 6; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 5)));
    await tester.pump(const Duration(milliseconds: 100));
  }
}

Future<void> _open(WidgetTester tester, _Server server, Future<void> Function() body) async {
  tester.view.physicalSize = const Size(412, 915) * 2;
  tester.view.devicePixelRatio = 2;
  addTearDown(tester.view.reset);
  await http.runWithClient(() async {
    await tester.pumpWidget(testApp(const AppEditScreen(app: _app), pushed: true));
    await _settle(tester);
    await body();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(minutes: 1));
  }, () => server.client);
}

Finder _field(String label) => find.widgetWithText(TextFormField, label);

Future<void> _type(WidgetTester tester, String label, String text) async {
  await tester.scrollUntilVisible(_field(label).first, 200, scrollable: find.byType(Scrollable).first);
  await tester.enterText(_field(label).first, text);
  await tester.pump();
}

Future<void> _saveAndConfirm(WidgetTester tester) async {
  await tester.tap(find.widgetWithText(TextButton, 'Save'));
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 400));
  expect(find.text('Save and restart Movies?'), findsOneWidget);
  await tester.tap(find.widgetWithText(TextButton, 'Save and restart'));
  await _settle(tester);
}

void main() {
  setUp(() async {
    await signIn();
    stubPlatformChannels();
    // No pictures from the internet in a widget test.
    NivaroAppIcon.debugNetworkIcon = (url, size) => SizedBox.square(dimension: size);
  });
  tearDown(() => NivaroAppIcon.debugNetworkIcon = null);

  testWidgets('Save PUTs the compose file with only the edited fields changed', (tester) async {
    final server = _Server();
    await _open(tester, server, () async {
      expect(server.gets.single.headers['Accept'] ?? server.gets.single.headers['accept'], 'application/yaml');
      await _type(tester, 'Name', 'Movies');
      await _type(tester, 'Tag', '10.11.11');
      await _saveAndConfirm(tester);
      expect(find.byType(AppEditScreen), findsNothing, reason: 'the screen closes once the server took it');
    });
    final put = server.puts.single;
    expect(put.url.queryParameters, {'check_port_conflict': 'true'});
    expect(put.headers['Content-Type'] ?? put.headers['content-type'], startsWith('application/yaml'));
    final sent = parseComposeYaml(put.body);
    final expected = parseComposeYaml(_yaml);
    ((expected['services'] as Map)['jellyfin'] as Map)['image'] = 'linuxserver/jellyfin:10.11.11';
    (expected['x-casaos'] as Map)['title'] = {'custom': 'Movies', 'en_us': 'Movies'};
    expect(deepEquals(sent, expected), isTrue, reason: put.body);
  });

  testWidgets('an invalid field blocks Save and says what to fix', (tester) async {
    final server = _Server();
    await _open(tester, server, () async {
      await _type(tester, 'Port', '99999');
      await tester.tap(find.widgetWithText(TextButton, 'Save'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));
      expect(find.text('Use a port from 1 to 65535'), findsWidgets);
      await tester.pump(const Duration(seconds: 1));
      expect(find.textContaining('Web UI port: Use a port from 1 to 65535'), findsOneWidget);
      expect(find.byType(AlertDialog), findsNothing);
    });
    expect(server.puts, isEmpty);
  });

  testWidgets('leaving with unsaved changes asks first', (tester) async {
    final server = _Server();
    await _open(tester, server, () async {
      await _type(tester, 'Name', 'Movies');
      await tester.scrollUntilVisible(find.byTooltip('Close'), -200, scrollable: find.byType(Scrollable).first);
      await tester.tap(find.byTooltip('Close'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));
      expect(find.text('Discard changes?'), findsOneWidget);
      await tester.tap(find.text('Cancel'));
      await tester.pump(const Duration(milliseconds: 400));
      expect(find.byType(AppEditScreen), findsOneWidget);
    });
    expect(server.puts, isEmpty);
  });

  testWidgets('the compose file editor saves what was typed', (tester) async {
    final server = _Server();
    await _open(tester, server, () async {
      await tester.tap(find.text('Compose file'));
      await tester.pump(const Duration(milliseconds: 300));
      final edited = _yaml.replaceFirst('cpu_shares: 90', 'cpu_shares: 512');
      await tester.enterText(find.byKey(const ValueKey('compose-yaml')), edited);
      await tester.pump();
      await tester.tap(find.widgetWithText(TextButton, 'Save'));
      await tester.pump(const Duration(milliseconds: 400));
      await tester.tap(find.widgetWithText(TextButton, 'Save and restart'));
      await _settle(tester);
    });
    final sent = parseComposeYaml(server.puts.single.body);
    expect(((sent['services'] as Map)['jellyfin'] as Map)['cpu_shares'], 512);
  });

  testWidgets('a broken compose file is caught before it is sent', (tester) async {
    final server = _Server();
    await _open(tester, server, () async {
      await tester.tap(find.text('Compose file'));
      await tester.pump(const Duration(milliseconds: 300));
      await tester.enterText(find.byKey(const ValueKey('compose-yaml')), _yaml.replaceFirst('name: jellyfin', 'name: other'));
      await tester.pump();
      expect(find.textContaining('Keep the first line as “name: jellyfin”'), findsOneWidget);
      await tester.tap(find.widgetWithText(TextButton, 'Save'));
      await tester.pump(const Duration(milliseconds: 400));
      expect(find.byType(AlertDialog), findsNothing);
    });
    expect(server.puts, isEmpty);
  });

  testWidgets('a port in use is explained', (tester) async {
    final server = _Server(
      putStatus: 400,
      putBody: {
        'message': 'there are ports in use',
        'data': {
          'ports_in_use': {
            'TCP': ['8098'],
          },
        },
      },
    );
    await _open(tester, server, () async {
      await _type(tester, 'Port', '8098');
      await tester.tap(find.widgetWithText(TextButton, 'Save'));
      await tester.pump(const Duration(milliseconds: 400));
      await tester.tap(find.widgetWithText(TextButton, 'Save and restart'));
      await _settle(tester);
      await tester.pump(const Duration(seconds: 1));
      expect(find.byType(AppEditScreen), findsOneWidget);
      expect(find.text('Server port 8098 is already used by something else. Pick another server port.'), findsOneWidget);
    });
  });
}
