// Profile pictures: the account row in Settings (picture and initials) in
// each design direction, light and true black; More, the server list and
// sign-in with a picture; the change sheet and the crop screen.
//
//   flutter test test/screenshots/profile_picture_test.dart --update-goldens
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:nivaroos_mobile/models/server_profile.dart';
import 'package:nivaroos_mobile/screens/login_screen.dart';
import 'package:nivaroos_mobile/screens/more_screen.dart';
import 'package:nivaroos_mobile/screens/profile_picture.dart';
import 'package:nivaroos_mobile/screens/server_profiles_screen.dart';
import 'package:nivaroos_mobile/screens/settings_screen.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/avatar_service.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'harness.dart';

final _png = File('test/screenshots/fixtures/v1/users/avatar.png').readAsBytesSync();

/// alex has a picture on the fake server.
final Map<String, Object> _withPicture = {
  'GET /v1/users/current': {
    'success': 200,
    'data': {'id': 1, 'username': 'alex', 'avatar_version': sha256.convert(_png).toString().substring(0, 16)},
  },
};

Future<void> _signIn() async {
  stubPlatformChannels();
  StorageService.instance.resetForTest();
  FlutterSecureStorage.setMockInitialValues({
    'server_url': fakeServer,
    'access_token': 'test-token',
    'refresh_token': 'test-refresh',
    'username': 'alex',
    'active_profile_id': 'srv_home',
    'saved_server_profiles': jsonEncode([
      ServerProfile(id: 'srv_home', name: 'Home', url: fakeServer, username: 'alex', accessToken: 't', refreshToken: 'r').toJson(),
      ServerProfile(id: 'srv_office', name: 'Office', url: 'https://nas.example.com', username: 'alex.morgan', accessToken: 't', refreshToken: 'r').toJson(),
      ServerProfile(id: 'srv_lab', name: '', url: 'http://192.168.1.40:8080', username: 'admin').toJson(),
    ]),
  });
  await StorageService.instance.init();
  ApiClient.instance.setBaseUrl(fakeServer);
  ApiClient.instance.setSession('test-token', 'test-refresh');
  AvatarService.instance.debugReset();
}

/// The picture already seen on this phone (server list, sign-in).
Future<void> _cachePicture() => http.runWithClient(AvatarService.instance.refresh, () => FakeServer(overrides: _withPicture).client);

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 10; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 30)));
    await tester.pump(const Duration(milliseconds: 100));
  }
}

void main() {
  setUp(() async {
    FocusManager.instance.highlightStrategy = FocusHighlightStrategy.alwaysTouch;
    await _signIn();
  });

  // The account row, in each direction: a picture, and the initials.
  for (final d in DesignDirection.values.where((d) => d != DesignDirection.v2)) {
    for (final m in const [AppThemeMode.light, AppThemeMode.black]) {
      for (final picture in const [true, false]) {
        final name = 'settings_account${picture ? '' : '_initials'}';
        testWidgets('${d.name} $name ${m.name}', (tester) => shoot(
              tester,
              dir: 'profile_picture/${d.name}',
              name: name,
              screen: const SettingsScreen(),
              pushed: true,
              brightness: m == AppThemeMode.light ? Brightness.light : Brightness.dark,
              themeName: m.name,
              appearance: Appearance(mode: m, direction: d),
              overrides: picture ? _withPicture : const {},
            ));
      }
    }
  }

  final shots = <String, (Widget Function(), {bool tab, bool pushed, Future<void> Function()? setup, Future<void> Function(WidgetTester)? before})>{
    'more': (() => const MoreScreen(), tab: true, pushed: false, setup: null, before: null),
    'server_profiles': (() => const ServerProfilesScreen(), tab: false, pushed: true, setup: _cachePicture, before: null),
    'login': (() => const LoginScreen(isReauth: true, initialUsername: 'alex'), tab: false, pushed: true, setup: _cachePicture, before: null),
    'change_sheet': (() => const SettingsScreen(), tab: false, pushed: true, setup: null, before: (tester) async {
      await tester.tap(find.text('alex'));
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
    }),
    'crop': (
      () => AvatarCropScreen(bytes: File('test/screenshots/fixtures/avatar_photo.png').readAsBytesSync()),
      tab: false,
      pushed: true,
      setup: null,
      before: _settle,
    ),
  };
  for (final MapEntry(key: name, value: s) in shots.entries) {
    for (final b in Brightness.values) {
      testWidgets('$name ${b.name}', (tester) async {
        if (s.setup != null) await tester.runAsync(s.setup!);
        await shoot(tester, dir: 'profile_picture', name: name, screen: s.$1(), brightness: b, tab: s.tab, pushed: s.pushed, overrides: _withPicture, before: s.before);
      });
    }
  }
}
