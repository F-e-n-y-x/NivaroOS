// Smoke tests for the Flutter 3.47 / dependency upgrade: each one exercises
// an API whose shape changed in the upgrade (ThemeData sub-theme data
// classes, flutter_secure_storage 10, flutter_markdown_plus), through the
// app's own code rather than the packages in isolation.
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/file_entry.dart';
import 'package:nivaroos_mobile/screens/file_viewer_screen.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/ui/theme/app_theme.dart';
import 'package:nivaroos_mobile/ui/theme/status_colors.dart';

import 'screenshots/harness.dart';

void main() {
  test('theme builds with the *ThemeData sub-themes', () {
    for (final theme in [AppTheme.light(), AppTheme.dark()]) {
      expect(theme.useMaterial3, isTrue);
      expect(theme.cardTheme.margin, EdgeInsets.zero);
      expect(theme.snackBarTheme.behavior, SnackBarBehavior.floating);
      expect(theme.extension<StatusColors>(), isNotNull);
    }
  });

  test('StorageService reads a session saved by the secure storage plugin', () async {
    FlutterSecureStorage.setMockInitialValues({
      'server_url': fakeServer,
      'access_token': 'test-token',
    });
    await StorageService.instance.init();
    expect(await StorageService.instance.getServerUrl(), fakeServer);
  });

  testWidgets('file viewer renders a local Markdown file', (tester) async {
    stubPlatformChannels();
    final dir = await tester.runAsync(() => Directory.systemTemp.createTemp('nivaro_md'));
    addTearDown(() => dir!.deleteSync(recursive: true));
    final file = File('${dir!.path}/README.md');
    await tester.runAsync(() => file.writeAsString('# Hello NivaroOS\n\nSome **bold** text.'));

    await tester.pumpWidget(MaterialApp(
      theme: AppTheme.dark(),
      home: FileViewerScreen(
        file: FileEntry(name: 'README.md', path: file.path, isDir: false, size: 0),
        path: file.path,
        isLocal: true,
      ),
    ));
    // The file is read with real IO, which only completes outside the
    // fake-async zone.
    for (var i = 0; i < 20 && find.byType(Markdown).evaluate().isEmpty; i++) {
      await tester.runAsync(() => Future.delayed(const Duration(milliseconds: 50)));
      await tester.pump();
    }

    expect(find.byType(Markdown), findsOneWidget);
    expect(find.text('Hello NivaroOS'), findsOneWidget);
  });
}
