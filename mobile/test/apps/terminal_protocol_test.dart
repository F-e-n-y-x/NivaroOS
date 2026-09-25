import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/terminal_screen.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

class _FakeTransport implements TerminalTransport {
  final frames = StreamController<dynamic>();
  final sent = <Object>[];
  int? code;
  String? reason;

  @override
  Stream<dynamic> get stream => frames.stream;
  @override
  void add(Object frame) => sent.add(frame);
  @override
  Future<void> close() async {}
  @override
  int? get closeCode => code;
  @override
  String? get closeReason => reason;
}

void main() {
  group('wsterm v2 framing (plan M-06)', () {
    test('resize is a TEXT frame starting with 0x00 then JSON', () {
      final f = Wsterm.resize(120, 40);
      expect(f.codeUnitAt(0), 0);
      expect(jsonDecode(f.substring(1)), {'type': 'resize', 'cols': 120, 'rows': 40});
    });

    test('input is UTF-8 bytes (a BINARY frame)', () {
      expect(Wsterm.input('é\r'), [0xC3, 0xA9, 0x0D]);
    });

    test('close messages', () {
      expect(Wsterm.closeMessage(1011, 'local terminal user not found'), 'The server stopped the session: local terminal user not found');
      expect(Wsterm.closeMessage(1000, ''), 'Session ended');
      expect(Wsterm.closeMessage(1006, null), 'The connection was lost');
    });
  });

  group('TerminalOutputDecoder (plan M-07)', () {
    test('keeps a character split across frames', () {
      final d = TerminalOutputDecoder();
      final bytes = utf8.encode('─' * 5000);
      final out = StringBuffer();
      // 8 KiB frames like wsterm.CopyOutput, which cut through characters.
      for (var i = 0; i < bytes.length; i += 8192) {
        out.write(d.add(bytes.sublist(i, i + 8192 > bytes.length ? bytes.length : i + 8192)));
      }
      out.write(d.close());
      expect(out.toString(), '─' * 5000);
      expect(out.toString().contains('�'), isFalse);
    });

    test('emoji split byte by byte', () {
      final d = TerminalOutputDecoder();
      final parts = utf8.encode('a😀b').map((b) => d.add([b])).join();
      expect(parts, 'a😀b');
    });
  });

  testWidgets('terminal sends resize as v2 control, input as binary, and shows the close reason', (tester) async {
    ApiClient.instance.setBaseUrl('http://nivaro.test');
    ApiClient.instance.setSession('t', 'r');
    final t = _FakeTransport();
    Uri? opened;
    Map<String, String>? headers;
    await tester.pumpWidget(MaterialApp(
      theme: AppTheme.light(),
      home: TerminalScreen(connector: (uri, h) async {
        opened = uri;
        headers = h;
        return t;
      }),
    ));
    await tester.pump();
    await tester.pump();
    expect(opened!.scheme, 'ws');
    expect(opened!.path, '/v1/sys/wsterm');
    expect(opened!.queryParameters.containsKey('token'), isFalse, reason: 'token only in the header');
    expect(headers!['Authorization'], 't');

    final resize = t.sent.whereType<String>().firstWhere((f) => f.startsWith('\u0000'));
    expect(jsonDecode(resize.substring(1))['type'], 'resize');

    // A latched Alt applies to the next key: Alt + "/" sends ESC "/".
    await tester.tap(find.bySemanticsLabel('Alt'));
    await tester.pump();
    t.sent.clear();
    await tester.ensureVisible(find.bySemanticsLabel('Slash'));
    await tester.pump();
    await tester.tap(find.bySemanticsLabel('Slash'));
    await tester.pump();
    expect(t.sent.single, utf8.encode('\x1b/'));
    t.sent.clear();
    await tester.ensureVisible(find.bySemanticsLabel('Slash'));
    await tester.pump();
    await tester.tap(find.bySemanticsLabel('Slash'));
    await tester.pump();
    expect(t.sent.single, utf8.encode('/'), reason: 'the latch releases after one key');

    t.code = 1011;
    t.reason = 'failed to start terminal';
    await t.frames.close();
    await tester.pump();
    expect(find.text('The server stopped the session: failed to start terminal'), findsOneWidget);
    expect(find.text('Reconnect'), findsOneWidget);
  });
}
