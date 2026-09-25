// The shared console clipboard history (plan WP1-5): newest first, no
// duplicates, no echo of what was just sent, at most ten, memory only.
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/widgets/rfb_view.dart';

void main() {
  final t0 = DateTime(2026, 9, 25, 14);

  test('newest first, and the same text again moves to the top', () {
    final h = RemoteClipboardHistory();
    h.record('one', ClipDirection.sent, 'mint', now: t0);
    h.record('two', ClipDirection.sent, 'mint', now: t0.add(const Duration(seconds: 1)));
    h.record('one', ClipDirection.sent, 'mint', now: t0.add(const Duration(seconds: 2)));
    expect(h.items.map((i) => i.text), ['one', 'two']);
  });

  test("a copy that echoes a send within 5 seconds isn't added", () {
    final h = RemoteClipboardHistory();
    h.record('secret', ClipDirection.sent, 'mint', now: t0);
    expect(h.record('secret', ClipDirection.copied, 'mint', now: t0.add(const Duration(seconds: 2))), isFalse);
    expect(h.record('secret', ClipDirection.copied, 'mint', now: t0.add(const Duration(seconds: 9))), isTrue);
    expect(h.record('secret', ClipDirection.copied, 'win11', now: t0), isTrue, reason: 'another VM is not an echo');
  });

  test('keeps the last ten', () {
    final h = RemoteClipboardHistory();
    for (var i = 0; i < 15; i++) {
      h.record('item $i', ClipDirection.copied, 'mint', now: t0.add(Duration(seconds: i)));
    }
    expect(h.items, hasLength(RemoteClipboardHistory.maxItems));
    expect(h.items.first.text, 'item 14');
    expect(h.items.last.text, 'item 5');
  });

  test('empty text is ignored and clear empties it', () {
    final h = RemoteClipboardHistory();
    expect(h.record('', ClipDirection.sent, 'mint'), isFalse);
    h.record('x', ClipDirection.sent, 'mint');
    h.clear();
    expect(h.items, isEmpty);
  });
}
