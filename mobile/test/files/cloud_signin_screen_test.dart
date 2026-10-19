// Files › Cloud drives › Add / Reconnect: the screen starts the server's
// sign-in, opens the provider's page (never the server) in the browser,
// keeps the phone's redirect listener up meanwhile and finishes the drive.
// All HTTP is a fake server; no secret is ever sent back by it.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/models/cloud_account.dart';
import 'package:nivaroos_mobile/screens/files/cloud_signin_screen.dart';

import '../screenshots/harness.dart';

const _google = 'https://accounts.google.com/o/oauth2/auth?client_id=383322&state=xyz';

http.Response _ok(Object data) => http.Response(jsonEncode({'success': 200, 'message': 'ok', 'data': data}), 200, headers: {'content-type': 'application/json'});

class _Run {
  final requests = <String>[];
  final bodies = <String, Object?>{};
  final opened = <Uri>[];
  final forwarders = <Uri>[];
  var stopped = 0;
  var polls = 0;
  bool? result;
}

Future<_Run> _drive(WidgetTester tester, {CloudAccount? account, required http.Response Function(http.Request req, _Run run) answer, Future<void> Function()? fill}) async {
  // Tall enough that the whole form is laid out.
  tester.view
    ..devicePixelRatio = 1
    ..physicalSize = const Size(412, 2400);
  addTearDown(tester.view.reset);
  final run = _Run();
  final client = MockClient((req) async {
    run.requests.add('${req.method} ${req.url.path}${req.url.hasQuery ? '?${req.url.query}' : ''}');
    if (req.body.isNotEmpty) run.bodies['${req.method} ${req.url.path}'] = jsonDecode(req.body);
    if (req.url.path == '/v1/rclone-auth/auth') return http.Response('', 307, headers: {'location': _google});
    return answer(req, run);
  });
  await http.runWithClient(() async {
    await tester.pumpWidget(testApp(Builder(
      builder: (context) => Scaffold(
        body: Center(
          child: TextButton(
            onPressed: () async => run.result = await Navigator.of(context).push<bool>(MaterialPageRoute(
              builder: (_) => CloudSignInScreen(
                account: account,
                pollEvery: const Duration(milliseconds: 10),
                startForwarder: (relay, _) async {
                  run.forwarders.add(relay);
                  return () async => run.stopped++;
                },
                openBrowser: (url) async {
                  run.opened.add(url);
                  return true;
                },
              ),
            )),
            child: const Text('open'),
          ),
        ),
      ),
    )));
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    await fill?.call();
    await tester.tap(find.byType(FilledButton));
    for (var i = 0; i < 30 && run.result == null; i++) {
      await tester.pump(const Duration(milliseconds: 20));
    }
    await tester.pumpAndSettle();
  }, () => client);
  return run;
}

void main() {
  setUp(signIn);

  testWidgets('add: own app keys go to the server once, which saves the drive', (tester) async {
    final run = await _drive(
      tester,
      fill: () async {
        await tester.tap(find.text('Your own app'));
        await tester.pumpAndSettle();
        expect(find.text('Publish the app or it expires in 7 days'), findsOneWidget);
        await tester.enterText(find.widgetWithText(TextField, 'Client ID'), '383322-abc.apps.googleusercontent.com');
        await tester.enterText(find.widgetWithText(TextField, 'Client secret'), 'GOCSPX-s3cret');
        await tester.enterText(find.widgetWithText(TextField, 'Name'), 'Photos');
      },
      answer: (req, run) {
        if (req.method == 'POST' && req.url.path == '/v1/cloud/accounts/authorize') {
          return _ok({'running': true, 'path': '/auth?state=xyz', 'name': 'photos_drive_1'});
        }
        if (req.url.path == '/v1/cloud/accounts/photos_drive_1/authorize') {
          return _ok(++run.polls < 3 ? {'running': true, 'path': '/auth?state=xyz', 'name': 'photos_drive_1'} : {'running': false, 'name': 'photos_drive_1', 'saved': true});
        }
        return http.Response('{}', 404);
      },
    );
    expect(run.bodies['POST /v1/cloud/accounts/authorize'], {
      'type': 'drive',
      'label': 'Photos',
      'client_id': '383322-abc.apps.googleusercontent.com',
      'client_secret': 'GOCSPX-s3cret',
    });
    expect(run.forwarders.single.toString(), '$fakeServer/v1/rclone-auth');
    expect(run.opened.single.toString(), _google, reason: "the browser opens the provider's page, not the server");
    expect(run.requests.where((r) => r.startsWith('POST')), ['POST /v1/cloud/accounts/authorize'], reason: 'the keys are sent once; the server saves the drive');
    expect(run.stopped, 1);
    expect(run.result, isTrue);
    expect(find.text('Google Drive connected'), findsOneWidget);
  });

  testWidgets("add: rclone's app sends no keys", (tester) async {
    final run = await _drive(
      tester,
      fill: () async {
        await tester.tap(find.text('Dropbox'));
        await tester.pumpAndSettle();
      },
      answer: (req, run) => req.method == 'POST'
          ? _ok({'running': true, 'path': '/auth?state=xyz', 'name': 'dropbox_dropbox_1'})
          : _ok({'running': false, 'name': 'dropbox_dropbox_1', 'saved': true}),
    );
    expect(run.bodies['POST /v1/cloud/accounts/authorize'], {'type': 'dropbox', 'label': ''});
    expect(run.result, isTrue);
  });

  testWidgets('add: own app without keys asks for them and starts nothing', (tester) async {
    final run = await _drive(
      tester,
      fill: () async {
        await tester.tap(find.text('Your own app'));
        await tester.pumpAndSettle();
      },
      answer: (req, run) => http.Response('{}', 500),
    );
    expect(find.text('Enter both the client ID and the client secret'), findsOneWidget);
    expect(run.requests, isEmpty);
    expect(run.forwarders, isEmpty);
  });

  testWidgets('reconnect: the same remote, saved app server-side, only the token is saved', (tester) async {
    final account = CloudAccount.fromJson({
      'name': 'My Drive',
      'fs': 'gdrive:',
      'type': 'drive',
      'mount_point': '/mnt/gdrive',
      'status': 'sign_in_expired',
      'problem': 'Your Google app is in Testing mode, so Google signs it out every 7 days.',
      'client_id': '383322-abc.apps.googleusercontent.com',
    });
    final run = await _drive(
      tester,
      account: account,
      answer: (req, run) => switch ('${req.method} ${req.url.path}') {
        'POST /v1/cloud/accounts/gdrive/authorize' => _ok({'running': true, 'path': '/auth?state=xyz'}),
        'GET /v1/cloud/accounts/gdrive/authorize' => _ok({'running': false, 'token': '{"access_token":"ya29.a"}'}),
        'POST /v1/cloud/accounts/gdrive/reconnect' => _ok({'name': 'gdrive'}),
        _ => http.Response('{}', 404),
      },
    );
    expect(run.bodies['POST /v1/cloud/accounts/gdrive/authorize'], isEmpty, reason: 'no keys from the phone');
    expect(run.bodies['POST /v1/cloud/accounts/gdrive/reconnect'], {
      'params': {'token': '{"access_token":"ya29.a"}'},
    });
    expect(run.opened.single.toString(), _google);
    expect(run.result, isTrue);
  });

  testWidgets("a server error is shown and the listener stops", (tester) async {
    final run = await _drive(
      tester,
      answer: (req, run) => req.method == 'POST'
          ? _ok({'running': true, 'path': '/auth?state=xyz', 'name': 'drive_drive_1'})
          : _ok({'running': false, 'name': 'drive_drive_1', 'error': 'oauth2: invalid_client'}),
    );
    expect(find.text('oauth2: invalid_client'), findsOneWidget);
    expect(run.stopped, 1);
    expect(run.result, isNull);
    expect(find.text('Sign in to Google Drive'), findsOneWidget, reason: 'can try again');
  });
}
