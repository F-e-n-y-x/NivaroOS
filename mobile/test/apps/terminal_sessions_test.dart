// The terminal on top of persistent sessions
// (docs/specs/2026-09-29-terminal-sessions.md): create, attach with replay,
// reattach after a drop, a session that is gone, the old-server fallback,
// touch scrolling, selection and copy, paste, pinch, and the sessions list.
// ignore_for_file: implementation_imports
import 'dart:convert';

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:nivaroos_mobile/screens/terminal_screen.dart';
import 'package:nivaroos_mobile/screens/terminal_sessions_screen.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/services/terminal_sessions.dart';
import 'package:nivaroos_mobile/ui/ui.dart';
import 'package:nivaroos_mobile/widgets/terminal_surface.dart';
import 'package:xterm/src/ui/render.dart';
import 'package:xterm/xterm.dart';

import 'terminal_fakes.dart';

String? _clipboard;

void _mockClipboard(WidgetTester tester) {
  _clipboard = null;
  tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, (call) async {
    if (call.method == 'Clipboard.setData') _clipboard = (call.arguments as Map)['text'] as String?;
    if (call.method == 'Clipboard.getData') return _clipboard == null ? null : {'text': _clipboard};
    if (call.method == 'Clipboard.hasStrings') return {'value': _clipboard != null};
    return null;
  });
}

Terminal _terminal(WidgetTester tester) => tester.widget<TerminalSurface>(find.byType(TerminalSurface)).terminal;
RenderTerminal _render(WidgetTester tester) => tester.allRenderObjects.whereType<RenderTerminal>().first;
ScrollPosition _scroll(WidgetTester tester) => tester.widget<TerminalSurface>(find.byType(TerminalSurface)).scrollController.position;

/// The middle of cell ([x], [y]) on screen.
Offset _cell(WidgetTester tester, int x, int y) {
  final r = _render(tester);
  return r.localToGlobal(r.getOffset(CellOffset(x, y)) + Offset(r.cellSize.width / 2, r.cellSize.height / 2));
}

/// Pumps [screen] with the fake server answering HTTP, runs [body], then
/// tears the screen down.
Future<void> _run(WidgetTester tester, FakeSessionServer server, Widget screen, Future<void> Function() body) async {
  await http.runWithClient(() async {
    await tester.pumpWidget(MaterialApp(
      theme: AppTheme.light(),
      home: Builder(builder: (context) => Scaffold(body: Center(child: TextButton(
        onPressed: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => screen)),
        child: const Text('open'),
      )))),
    ));
    await tester.tap(find.text('open'));
    // Not pumpAndSettle: the connecting bar animates until the fake
    // server answers.
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));
    await body();
    await tester.pumpWidget(const SizedBox());
    await tester.pump(const Duration(minutes: 1));
  }, () => server.client);
}

/// Lets the async steps (HTTP, the connect) run.
Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 6; i++) {
    await tester.pump(const Duration(milliseconds: 20));
  }
}

void main() {
  setUp(() async {
    FlutterSecureStorage.setMockInitialValues({});
    await StorageService.instance.init();
    ApiClient.instance.setBaseUrl('http://nivaro.test');
    ApiClient.instance.setSession('t', 'r');
    TerminalSessionsApi.instance.debugReset();
  });

  group('protocol', () {
    test('control frames', () {
      expect(Wsterm.control('plain status line'), isNull);
      final hello = Wsterm.control('\u0000${jsonEncode({'type': 'hello', 'session': sessionJson('a'), 'replay_bytes': 42})}');
      expect(hello, isA<HelloControl>().having((h) => h.replayBytes, 'bytes', 42).having((h) => h.session?.id, 'id', 'a'));
      expect(Wsterm.control('\u0000{"type":"live"}'), isA<LiveControl>());
      expect(Wsterm.control('\u0000{"type":"exit","code":7,"reason":"exited"}'), isA<ExitControl>().having((e) => e.code, 'code', 7));
      expect(Wsterm.control('\u0000not json'), isA<UnknownControl>());
    });

    test('session JSON', () {
      final s = TerminalSession.fromJson(sessionJson('a', command: 'vim notes.md', cwd: '/home/alex/projects'));
      expect(s.family, TerminalFamily.host);
      expect(s.running, isTrue);
      expect(s.activity, 'vim notes.md · ~/projects');
      expect(s.attachPath, '/v1/sys/terminal-sessions/a/attach');
      final shell = TerminalSession.fromJson(sessionJson('b', command: '-bash', cwd: '/home/alex'));
      expect(shell.activity, '~', reason: 'the shell itself is not a job');
      final ended = TerminalSession.fromJson(sessionJson('c', state: 'exited', exitCode: 0, exitReason: 'exited'));
      expect(ended.running, isFalse);
      expect(ended.endedText, 'The shell ended (exit code 0)');
    });

    test('lists both families, running first, newest first; an old server is reported', () async {
      final server = FakeSessionServer()
        ..host.addAll([
          sessionJson('old', lastActivity: '2026-09-25T10:00:00Z'),
          sessionJson('done', state: 'exited', lastActivity: '2026-09-25T14:00:00Z'),
        ])
        ..containers.add(sessionJson('c1', kind: 'container', container: 'jellyfin', lastActivity: '2026-09-25T13:00:00Z'));
      final list = await http.runWithClient(() => TerminalSessionsApi.instance.list(), () => server.client);
      expect([for (final s in list.sessions) s.id], ['c1', 'old', 'done']);
      expect(TerminalSessionsApi.instance.runningCount.value, 2);
      expect(list.detachedTimeout, const Duration(days: 1));

      server.hostSupported = false;
      final old = await http.runWithClient(() => TerminalSessionsApi.instance.list(), () => server.client);
      expect(old.unsupported, {TerminalFamily.host});
      expect(TerminalSessionsApi.instance.supports(TerminalFamily.host), isFalse);
    });
  });

  testWidgets('a new terminal is created at the view size, attached with replay, then live', (tester) async {
    final server = FakeSessionServer();
    final ws = FakeConnector();
    await _run(tester, server, TerminalScreen(connector: ws.call), () async {
      await _settle(tester);
      expect(server.requests, contains('POST /v1/sys/terminal-sessions'));
      final sock = ws.last;
      expect(sock.uri.path, '/v1/sys/terminal-sessions/s1/attach');
      expect(sock.uri.queryParameters['cols'], '${_terminal(tester).viewWidth}');
      expect(sock.uri.queryParameters.containsKey('token'), isFalse, reason: 'token only in the header');
      expect(sock.headers['Authorization'], 't');

      sock.hello(sessionJson('s1'), replay: 12);
      sock.out('earlier line\r\n\$ ');
      await tester.pump();
      expect(find.text('Restoring recent output…'), findsOneWidget);
      sock.live();
      await tester.pump();
      expect(find.text('Terminal 1'), findsOneWidget);
      expect(_terminal(tester).buffer.getText(), contains('earlier line'));
      expect(sock.resizes, isNotEmpty, reason: 'a resize right after live');

      await tester.tap(find.bySemanticsLabel('Escape'));
      await tester.pump();
      expect(sock.input, '\x1b');
    });
  });

  testWidgets('a dropped connection reattaches by itself, and the replay is not printed twice', (tester) async {
    final server = FakeSessionServer()..host.add(sessionJson('a'));
    final ws = FakeConnector();
    final s = TerminalSession.fromJson(sessionJson('a'));
    await _run(tester, server, TerminalScreen(session: s, connector: ws.call), () async {
      await _settle(tester);
      final first = ws.last;
      first.hello(sessionJson('a'));
      first.out('build step 1\r\n');
      first.live();
      await tester.pump();

      await first.drop(1006);
      await tester.pump();
      expect(find.textContaining('Connection lost'), findsOneWidget);
      expect(find.text('Retry now'), findsOneWidget);
      await tester.pump(const Duration(milliseconds: 400));
      await _settle(tester);
      expect(ws.sockets, hasLength(2));
      final second = ws.last;
      second.hello(sessionJson('a'));
      second.out('build step 1\r\nbuild step 2\r\n');
      second.live();
      await tester.pump();
      await tester.pump();
      final text = _terminal(tester).buffer.getText();
      expect('build step 1'.allMatches(text).length, 1);
      expect(text, contains('build step 2'));
      expect(find.textContaining('Connection lost'), findsNothing);
    });
  });

  testWidgets('a viewer that fell behind (1013) reattaches at once', (tester) async {
    final server = FakeSessionServer()..host.add(sessionJson('a'));
    final ws = FakeConnector();
    await _run(tester, server, TerminalScreen(session: TerminalSession.fromJson(sessionJson('a')), connector: ws.call), () async {
      await _settle(tester);
      ws.last
        ..hello(sessionJson('a'))
        ..live();
      await tester.pump();
      await ws.last.drop(Wsterm.tooSlow);
      await _settle(tester);
      expect(ws.sockets, hasLength(2));
    });
  });

  testWidgets('a session the server no longer has (404) says so and offers a new one', (tester) async {
    final server = FakeSessionServer();
    final ws = FakeConnector()..failures.add(const TerminalHandshakeException(404));
    await _run(tester, server, TerminalScreen(session: TerminalSession.fromJson(sessionJson('gone')), connector: ws.call), () async {
      await _settle(tester);
      expect(find.textContaining('no longer on the server'), findsOneWidget);
      expect(find.text('New terminal'), findsOneWidget);
    });
  });

  testWidgets('the shell ending shows why', (tester) async {
    final server = FakeSessionServer()..host.add(sessionJson('a'));
    final ws = FakeConnector();
    await _run(tester, server, TerminalScreen(session: TerminalSession.fromJson(sessionJson('a')), connector: ws.call), () async {
      await _settle(tester);
      ws.last
        ..hello(sessionJson('a'))
        ..live()
        ..exit(7, 'exited');
      await tester.pump();
      await ws.last.drop(1000);
      await tester.pump();
      expect(find.text('The shell ended (exit code 7).'), findsOneWidget);
      expect(ws.sockets, hasLength(1), reason: 'no reattach after an exit');
    });
  });

  testWidgets('too many terminals (429) points to the list', (tester) async {
    final server = FakeSessionServer()..failCreate = (429, 'too many terminal sessions: close one first (limit 12)');
    final ws = FakeConnector();
    await _run(tester, server, TerminalScreen(connector: ws.call), () async {
      await _settle(tester);
      expect(find.textContaining('End one to start another'), findsOneWidget);
      expect(find.text('See terminals'), findsOneWidget);
      expect(ws.sockets, isEmpty);
    });
  });

  testWidgets('an older server without sessions uses the plain-connect route', (tester) async {
    final server = FakeSessionServer(hostSupported: false);
    final ws = FakeConnector();
    await _run(tester, server, TerminalScreen(connector: ws.call), () async {
      await _settle(tester);
      expect(ws.last.uri.path, '/v1/sys/wsterm');
      ws.last.out('\$ ');
      await tester.pump();
      expect(ws.last.resizes, isNotEmpty);

      // A latched Alt applies to the next key: Alt + "/" sends ESC "/".
      await tester.ensureVisible(find.bySemanticsLabel('Alt'));
      await tester.pump();
      await tester.tap(find.bySemanticsLabel('Alt'));
      await tester.pump();
      await tester.ensureVisible(find.bySemanticsLabel('Slash'));
      await tester.pump();
      await tester.tap(find.bySemanticsLabel('Slash'));
      await tester.pump();
      expect(ws.last.input, '\x1b/');
      await tester.tap(find.bySemanticsLabel('Slash'));
      await tester.pump();
      expect(ws.last.input, '\x1b//', reason: 'the latch releases after one key');
      // ...and a latched Ctrl applies to an arrow key (Ctrl+Left = word
      // left), then releases instead of hitting the next letter.
      await tester.ensureVisible(find.bySemanticsLabel('Control'));
      await tester.pump();
      await tester.tap(find.bySemanticsLabel('Control'));
      await tester.pump();
      await tester.ensureVisible(find.bySemanticsLabel('Left'));
      await tester.pump();
      await tester.tap(find.bySemanticsLabel('Left'));
      await tester.pump();
      expect(ws.last.input, '\x1b//\x1b[1;5D');
      await tester.ensureVisible(find.bySemanticsLabel('Slash'));
      await tester.pump();
      await tester.tap(find.bySemanticsLabel('Slash'));
      await tester.pump();
      expect(ws.last.input, '\x1b//\x1b[1;5D/', reason: 'Ctrl was used up by the arrow');
      ws.last.code = 1011;
      ws.last.reason = 'failed to start terminal';
      await ws.last.drop(1011);
      await tester.pump();
      expect(find.text('The server stopped the session: failed to start terminal'), findsOneWidget);
      expect(find.text('Reconnect'), findsOneWidget);
    });
  });

  testWidgets('leaving detaches: the shell keeps running and the user is told', (tester) async {
    final server = FakeSessionServer()..host.add(sessionJson('a', title: 'build box'));
    final ws = FakeConnector();
    await _run(tester, server, TerminalScreen(session: TerminalSession.fromJson(sessionJson('a', title: 'build box')), connector: ws.call), () async {
      await _settle(tester);
      ws.last
        ..hello(sessionJson('a', title: 'build box'))
        ..live();
      await tester.pump();
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(ws.last.closed, isTrue);
      expect(server.requests.where((r) => r.startsWith('DELETE')), isEmpty);
      expect(find.text('“build box” keeps running on the server'), findsOneWidget);
      await tester.tap(find.text('End it'));
      await tester.pump();
      await _settle(tester);
      expect(server.requests, contains('DELETE /v1/sys/terminal-sessions/a'));
    });
  });

  testWidgets('drag scrolls back through the history; new output does not pull the view down', (tester) async {
    final server = FakeSessionServer()..host.add(sessionJson('a'));
    final ws = FakeConnector();
    await _run(tester, server, TerminalScreen(session: TerminalSession.fromJson(sessionJson('a')), connector: ws.call), () async {
      await _settle(tester);
      ws.last
        ..hello(sessionJson('a'))
        ..out(List.generate(300, (i) => 'line $i').join('\r\n'))
        ..live();
      await tester.pump();
      await tester.pump();
      final p = _scroll(tester);
      expect(p.pixels, p.maxScrollExtent);
      expect(find.bySemanticsLabel('Scroll to the latest output'), findsNothing);

      // Keyboard down, to read.
      FocusManager.instance.primaryFocus?.unfocus();
      await tester.pump();
      // One finger, held a moment first (as a thumb does), then dragged.
      final g = await tester.startGesture(tester.getCenter(find.byType(TerminalSurface)));
      await tester.pump(const Duration(milliseconds: 200));
      await g.moveBy(const Offset(0, 40));
      await g.moveBy(const Offset(0, 300));
      await g.up();
      await tester.pumpAndSettle();
      expect(p.pixels, lessThan(p.maxScrollExtent - 200));
      expect(FocusManager.instance.primaryFocus, isNot(tester.widget<TerminalSurface>(find.byType(TerminalSurface)).focusNode),
          reason: 'a drag never opens the keyboard');

      final reading = p.pixels;
      ws.last.out('\r\nnew output while reading');
      await tester.pump();
      await tester.pump();
      expect(p.pixels, reading);
      expect(find.text('New output'), findsOneWidget);

      await tester.tap(find.text('New output'));
      await tester.pumpAndSettle();
      expect(p.pixels, p.maxScrollExtent);
      expect(find.text('New output'), findsNothing);
    });
  });

  testWidgets('in a full-screen program a drag sends arrow keys, not a scroll', (tester) async {
    final server = FakeSessionServer()..host.add(sessionJson('a'));
    final ws = FakeConnector();
    await _run(tester, server, TerminalScreen(session: TerminalSession.fromJson(sessionJson('a')), connector: ws.call), () async {
      await _settle(tester);
      ws.last
        ..hello(sessionJson('a'))
        ..out(List.generate(100, (i) => 'line $i').join('\r\n'))
        // less: the alternate screen.
        ..out('\x1b[?1049h\x1b[Hpage one')
        ..live();
      await tester.pump();
      await tester.pump();
      expect(_terminal(tester).isUsingAltBuffer, isTrue);
      ws.last.sent.clear();
      await tester.drag(find.byType(TerminalSurface), const Offset(0, 120));
      await tester.pumpAndSettle();
      expect(ws.last.input, contains('\x1b[A'), reason: 'dragging down reads back: arrow up');
      ws.last.sent.clear();
      await tester.drag(find.byType(TerminalSurface), const Offset(0, -120));
      await tester.pumpAndSettle();
      expect(ws.last.input, contains('\x1b[B'));
    });
  });

  testWidgets('an app with a shell running offers to go back to it', (tester) async {
    final server = FakeSessionServer()..containers.add(sessionJson('c', kind: 'container', container: 'jellyfin', title: 'jellyfin (bash)'));
    final ws = FakeConnector();
    await http.runWithClient(() async {
      await tester.pumpWidget(MaterialApp(
        theme: AppTheme.light(),
        home: Builder(builder: (context) => Scaffold(body: Center(child: TextButton(
          onPressed: () => openContainerTerminal(context, container: 'jellyfin', title: 'Jellyfin', connector: ws.call),
          child: const Text('open'),
        )))),
      ));
      await tester.tap(find.text('open'));
      await _settle(tester);
      await tester.pumpAndSettle();
      expect(server.requests, contains('GET /v1/container/terminal-sessions?container=jellyfin'));
      expect(find.text('Shells in Jellyfin'), findsOneWidget);
      await tester.tap(find.text('jellyfin (bash)'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      await _settle(tester);
      expect(ws.last.uri.path, '/v1/container/terminal-sessions/c/attach');
      await tester.pumpWidget(const SizedBox());
      await tester.pump(const Duration(minutes: 1));
    }, () => server.client);
  });

  testWidgets('fling scrolls further than the drag', (tester) async {
    final server = FakeSessionServer()..host.add(sessionJson('a'));
    final ws = FakeConnector();
    await _run(tester, server, TerminalScreen(session: TerminalSession.fromJson(sessionJson('a')), connector: ws.call), () async {
      await _settle(tester);
      ws.last
        ..hello(sessionJson('a'))
        ..out(List.generate(500, (i) => 'line $i').join('\r\n'))
        ..live();
      await tester.pump();
      await tester.pump();
      final p = _scroll(tester);
      final start = p.pixels;
      await tester.fling(find.byType(TerminalSurface), const Offset(0, 200), 3000);
      await tester.pumpAndSettle();
      expect(start - p.pixels, greaterThan(400));
    });
  });

  testWidgets('long-press selects a word with handles; Copy puts it on the clipboard', (tester) async {
    _mockClipboard(tester);
    final server = FakeSessionServer()..host.add(sessionJson('a'));
    final ws = FakeConnector();
    await _run(tester, server, TerminalScreen(session: TerminalSession.fromJson(sessionJson('a')), connector: ws.call), () async {
      await _settle(tester);
      ws.last
        ..hello(sessionJson('a'))
        ..out('docker ps\r\njellyfin   Up 10 hours\r\n')
        ..live();
      await tester.pump();
      await tester.pump();

      await tester.longPressAt(_cell(tester, 3, 1));
      await tester.pumpAndSettle();
      expect(find.bySemanticsLabel('Selection start'), findsOneWidget);
      expect(find.bySemanticsLabel('Selection end'), findsOneWidget);

      // Widen it with the end handle to the end of the line.
      final end = tester.getCenter(find.bySemanticsLabel('Selection end'));
      final to = _cell(tester, 26, 1);
      await tester.dragFrom(end, Offset(to.dx - end.dx + 8, 0));
      await tester.pumpAndSettle();

      await tester.tap(find.text('Copy'));
      await tester.pump();
      expect(_clipboard, 'jellyfin   Up 10 hours');
      expect(find.text('Copied'), findsOneWidget);
      expect(find.bySemanticsLabel('Selection start'), findsNothing);
    });
  });

  testWidgets('paste: several lines ask first, and go as CR; bracketed paste goes at once', (tester) async {
    _mockClipboard(tester);
    final server = FakeSessionServer()..host.add(sessionJson('a'));
    final ws = FakeConnector();
    await _run(tester, server, TerminalScreen(session: TerminalSession.fromJson(sessionJson('a')), connector: ws.call), () async {
      await _settle(tester);
      ws.last
        ..hello(sessionJson('a'))
        ..live();
      await tester.pump();
      _clipboard = 'cd /tmp\nls -la';

      await tester.tap(find.bySemanticsLabel('Paste'));
      await tester.pumpAndSettle();
      expect(find.text('Paste 2 lines?'), findsOneWidget);
      await tester.tap(find.widgetWithText(TextButton, 'Paste'));
      await tester.pumpAndSettle();
      expect(ws.last.input, 'cd /tmp\rls -la');

      ws.last.sent.clear();
      ws.last.out('\x1b[?2004h');
      await tester.pump();
      await tester.tap(find.bySemanticsLabel('Paste'));
      await tester.pumpAndSettle();
      expect(find.text('Paste 2 lines?'), findsNothing);
      expect(ws.last.input, '\x1b[200~cd /tmp\rls -la\x1b[201~');
    });
  });

  testWidgets('pinch changes the text size and remembers it', (tester) async {
    final server = FakeSessionServer()..host.add(sessionJson('a'));
    final ws = FakeConnector();
    await _run(tester, server, TerminalScreen(session: TerminalSession.fromJson(sessionJson('a')), connector: ws.call), () async {
      await _settle(tester);
      ws.last
        ..hello(sessionJson('a'))
        ..live();
      await tester.pump();
      final before = tester.widget<TerminalSurface>(find.byType(TerminalSurface)).fontSize;
      final c = tester.getCenter(find.byType(TerminalSurface));
      final a = await tester.startGesture(c - const Offset(0, 40), kind: PointerDeviceKind.touch);
      final b = await tester.startGesture(c + const Offset(0, 40), pointer: 7, kind: PointerDeviceKind.touch);
      await tester.pump();
      for (var i = 0; i < 10; i++) {
        await a.moveBy(const Offset(0, -8));
        await b.moveBy(const Offset(0, 8));
        await tester.pump();
      }
      await a.up();
      await b.up();
      await tester.pumpAndSettle();
      final after = tester.widget<TerminalSurface>(find.byType(TerminalSurface)).fontSize;
      expect(after, greaterThan(before + 4));
      expect(await StorageService.instance.getTerminalFontSize(), after);
    });
  });

  testWidgets('the sessions list merges the server and app shells, renames and ends', (tester) async {
    final server = FakeSessionServer()
      ..host.addAll([
        sessionJson('a', title: 'build box', command: 'make', cwd: '/home/alex/src', clients: 1),
        sessionJson('z', title: 'Terminal 2', state: 'exited', exitCode: 0, exitReason: 'exited'),
      ])
      ..containers.add(sessionJson('c', kind: 'container', container: 'jellyfin', title: 'jellyfin (bash)'));
    final ws = FakeConnector();
    await _run(tester, server, TerminalSessionsScreen(connector: ws.call), () async {
      await _settle(tester);
      expect(find.text('On the server'), findsOneWidget);
      expect(find.text('In apps'), findsOneWidget);
      expect(find.text('Ended recently'), findsOneWidget);
      expect(find.text('build box'), findsOneWidget);
      expect(find.text('make · ~/src'), findsOneWidget);
      expect(find.text('Open on another device'), findsOneWidget);
      expect(find.text('jellyfin (bash)'), findsOneWidget);

      await tester.tap(find.byTooltip('Options for build box'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Rename'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), 'deploy');
      await tester.tap(find.widgetWithText(TextButton, 'Rename'));
      await tester.pumpAndSettle();
      await _settle(tester);
      expect(server.requests, contains('PUT /v1/sys/terminal-sessions/a'));
      expect(find.text('deploy'), findsOneWidget);

      await tester.tap(find.byTooltip('Options for jellyfin (bash)'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('End'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'End'));
      await tester.pumpAndSettle();
      await _settle(tester);
      expect(server.requests, contains('DELETE /v1/container/terminal-sessions/c'));
      expect(find.text('jellyfin (bash)'), findsNothing);

      // Tap a running one: it reattaches.
      await tester.tap(find.text('deploy'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      await _settle(tester);
      expect(ws.last.uri.path, '/v1/sys/terminal-sessions/a/attach');
    });
  });
}
