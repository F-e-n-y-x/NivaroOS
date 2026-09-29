import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/terminal_screen.dart';

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
}
