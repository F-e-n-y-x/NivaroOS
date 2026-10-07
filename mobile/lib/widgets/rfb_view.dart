import 'dart:async';
import 'dart:ui' as ui;

import 'package:clock/clock.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../services/rfb_client.dart';
import '../services/storage_service.dart';
import '../ui/ui.dart';
import 'rfb_viewport.dart';

export 'rfb_viewport.dart' show RfbFit, RfbViewport;

// The remote console UI shared by the VM console and the host desktop:
// the framebuffer view with touch and mouse pointer input, the session
// around it (immersive, with a floating toolbar and the extra keys row,
// after Microsoft's Windows App), and the clipboard sheet with its
// history. The two screens only add their own menu items and the state to
// show before a connection makes sense.

/// How touches on the remote screen become pointer input: Windows App's
/// two mouse modes.
enum RfbInputMode {
  /// Mouse pointer: a cursor moved like a laptop trackpad; tap clicks
  /// where it is.
  trackpad,

  /// Touch: tap where you want to click.
  touch,
}

/// X11 keysyms the key bar and a hardware keyboard send.
abstract final class Keysym {
  static const backspace = 0xff08;
  static const tab = 0xff09;
  static const enter = 0xff0d;
  static const escape = 0xff1b;
  static const delete = 0xffff;
  static const home = 0xff50;
  static const left = 0xff51;
  static const up = 0xff52;
  static const right = 0xff53;
  static const down = 0xff54;
  static const pageUp = 0xff55;
  static const pageDown = 0xff56;
  static const end = 0xff57;
  static const insert = 0xff63;
  static const f1 = 0xffbe;
  static const shift = 0xffe1;
  static const control = 0xffe3;
  static const alt = 0xffe9;
  static const superKey = 0xffeb;

  static int f(int n) => f1 + n - 1;

  /// Keys a hardware keyboard sends that don't arrive as text.
  static final Map<LogicalKeyboardKey, int> hardware = {
    LogicalKeyboardKey.escape: escape,
    LogicalKeyboardKey.tab: tab,
    LogicalKeyboardKey.delete: delete,
    LogicalKeyboardKey.home: home,
    LogicalKeyboardKey.end: end,
    LogicalKeyboardKey.pageUp: pageUp,
    LogicalKeyboardKey.pageDown: pageDown,
    LogicalKeyboardKey.insert: insert,
    LogicalKeyboardKey.arrowLeft: left,
    LogicalKeyboardKey.arrowUp: up,
    LogicalKeyboardKey.arrowRight: right,
    LogicalKeyboardKey.arrowDown: down,
    for (var i = 1; i <= 12; i++) LogicalKeyboardKey(LogicalKeyboardKey.f1.keyId + i - 1): f(i),
  };
}

// ---------------------------------------------------------------------------
// Clipboard history
// ---------------------------------------------------------------------------

/// Text sent to a remote machine ("sent") or copied on it ("copied").
enum ClipDirection { sent, copied }

@immutable
class RemoteClipItem {
  const RemoteClipItem({required this.text, required this.direction, required this.target, required this.at});
  final String text;
  final ClipDirection direction;

  /// Which machine: a VM's name, or [RemoteClipboardHistory.hostTarget].
  final String target;
  final DateTime at;
}

/// The clipboard history shared by every console, newest first: the last
/// [maxItems] texts, kept in memory only - clipboards hold passwords often
/// enough that writing them to disk isn't a sane default (plan WP1-5).
class RemoteClipboardHistory extends ChangeNotifier {
  RemoteClipboardHistory();

  static final RemoteClipboardHistory instance = RemoteClipboardHistory();

  static const maxItems = 10;
  static const maxStoredChars = 100000;
  static const hostTarget = 'the server';

  /// Sending text makes the remote report the same text straight back as
  /// a copy; within this window that echo isn't a new entry.
  static const echoWindow = Duration(seconds: 5);

  final List<RemoteClipItem> _items = [];
  List<RemoteClipItem> get items => List.unmodifiable(_items);

  /// Adds one entry; returns false when nothing was added (empty, or an
  /// echo of what was just sent).
  bool record(String text, ClipDirection direction, String target, {DateTime? now}) {
    if (text.isEmpty) return false;
    final at = now ?? clock.now();
    final stored = text.length > maxStoredChars ? text.substring(0, maxStoredChars) : text;
    if (direction == ClipDirection.copied &&
        _items.any((i) =>
            i.direction == ClipDirection.sent && i.target == target && i.text == stored && at.difference(i.at) < echoWindow)) {
      return false;
    }
    _items.removeWhere((i) => i.direction == direction && i.target == target && i.text == stored);
    _items.insert(0, RemoteClipItem(text: stored, direction: direction, target: target, at: at));
    if (_items.length > maxItems) _items.removeRange(maxItems, _items.length);
    notifyListeners();
    return true;
  }

  void clear() {
    if (_items.isEmpty) return;
    _items.clear();
    notifyListeners();
  }
}

// ---------------------------------------------------------------------------
// Clipboard sheet
// ---------------------------------------------------------------------------

/// The clipboard sheet: paste the phone's clipboard into the remote
/// machine, send or type any text, and the history. Same actions and
/// words as the web console's clipboard panel.
class RemoteClipboardSheet extends StatefulWidget {
  const RemoteClipboardSheet({super.key, required this.client, required this.target, this.hint, this.history});

  final RfbClient client;

  /// Who gets the text: a VM name, or [RemoteClipboardHistory.hostTarget].
  final String target;

  /// When copy and paste may not work, why and what to do instead.
  final String? hint;
  final RemoteClipboardHistory? history;

  static Future<void> show(BuildContext context,
      {required RfbClient client, required String target, String? hint, RemoteClipboardHistory? history}) {
    return showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      useSafeArea: true,
      builder: (_) => RemoteClipboardSheet(client: client, target: target, hint: hint, history: history),
    );
  }

  @override
  State<RemoteClipboardSheet> createState() => _RemoteClipboardSheetState();
}

class _RemoteClipboardSheetState extends State<RemoteClipboardSheet> {
  final _draft = TextEditingController();
  late final RemoteClipboardHistory _history = widget.history ?? RemoteClipboardHistory.instance;

  bool get _connected => widget.client.isConnected;

  @override
  void initState() {
    super.initState();
    _draft.addListener(() => setState(() {}));
    widget.client.status.addListener(_changed);
  }

  @override
  void dispose() {
    widget.client.status.removeListener(_changed);
    _draft.dispose();
    super.dispose();
  }

  void _changed() => setState(() {});

  void _done(String message) {
    final messenger = ScaffoldMessenger.maybeOf(context);
    Navigator.of(context).pop();
    messenger?.showSnackBar(SnackBar(content: Text(message)));
  }

  void _send(String text) {
    if (text.isEmpty) return;
    final lossless = widget.client.sendClipboard(text);
    _history.record(text, ClipDirection.sent, widget.target);
    _done(lossless
        ? 'Sent to the clipboard on ${widget.target}'
        : "Sent, but some characters can't go this way. Use Type it for them.");
  }

  Future<void> _type(String text) async {
    if (text.isEmpty) return;
    _history.record(text, ClipDirection.sent, widget.target);
    final future = widget.client.typeText(text);
    final long = text.runes.length > RfbClient.maxTypedChars;
    _done(long ? 'Typing the first ${RfbClient.maxTypedChars} characters' : 'Typing it on ${widget.target}');
    await future;
  }

  Future<void> _pastePhoneClipboard() async {
    final data = await Clipboard.getData(Clipboard.kTextPlain);
    final text = data?.text ?? '';
    if (!mounted) return;
    if (text.isEmpty) {
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(const SnackBar(content: Text("This phone's clipboard has no text")));
      return;
    }
    _send(text);
  }

  Future<void> _copyToPhone(RemoteClipItem item) async {
    await Clipboard.setData(ClipboardData(text: item.text));
    if (!mounted) return;
    ScaffoldMessenger.maybeOf(context)?.showSnackBar(const SnackBar(content: Text('Copied to this phone')));
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    final draft = _draft.text;
    final large = MediaQuery.textScalerOf(context).scale(10) > 13;
    return AnimatedPadding(
      duration: Motion.of(context).short,
      padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
      child: ListenableBuilder(
        listenable: _history,
        builder: (context, _) {
          final items = _history.items;
          return ListView(
            shrinkWrap: true,
            padding: const EdgeInsets.only(bottom: Space.lg),
            children: [
              Padding(
                padding: EdgeInsets.symmetric(horizontal: gutter),
                child: Semantics(header: true, child: Text('Clipboard', style: theme.textTheme.titleLarge)),
              ),
              if (widget.hint != null)
                Padding(
                  padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0),
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Padding(
                        padding: const EdgeInsets.only(top: 2),
                        child: Icon(Icons.info_outline, size: 20, color: scheme.onSurfaceVariant),
                      ),
                      const SizedBox(width: Space.md),
                      Expanded(
                        child: Text(widget.hint!,
                            style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant)),
                      ),
                    ],
                  ),
                ),
              const SizedBox(height: Space.sm),
              // A row rather than a pill: the label is a sentence, and at
              // large text a pill wraps it around a tiny icon.
              ListTile(
                leading: const Icon(Icons.content_paste_outlined),
                title: Text("Paste this phone's clipboard into ${widget.target}"),
                enabled: _connected,
                onTap: _pastePhoneClipboard,
              ),
              Padding(
                padding: EdgeInsets.fromLTRB(gutter, Space.lg, gutter, 0),
                child: TextField(
                  controller: _draft,
                  minLines: 2,
                  maxLines: 5,
                  keyboardType: TextInputType.multiline,
                  decoration: const InputDecoration(labelText: 'Or type the text to send'),
                ),
              ),
              Padding(
                padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0),
                child: Builder(builder: (context) {
                  final type = Tooltip(
                    message: 'Types the text as key presses. Works on login screens and without guest tools.',
                    child: OutlinedButton.icon(
                      onPressed: _connected && draft.isNotEmpty ? () => _type(draft) : null,
                      icon: const Icon(Icons.keyboard_outlined),
                      label: const Text('Type it'),
                    ),
                  );
                  final send = FilledButton.icon(
                    onPressed: _connected && draft.isNotEmpty ? () => _send(draft) : null,
                    icon: const Icon(Icons.send_outlined),
                    label: const Text('Send to clipboard'),
                  );
                  // At large text the two don't fit side by side: full
                  // width, the main one first.
                  if (large) {
                    return Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [send, const SizedBox(height: Space.sm), type],
                    );
                  }
                  return Row(
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: [type, const SizedBox(width: Space.sm), send],
                  );
                }),
              ),
              SectionHeader(
                title: 'History',
                actionLabel: items.isEmpty ? null : 'Clear',
                onAction: items.isEmpty ? null : _history.clear,
              ),
              if (items.isEmpty)
                Padding(
                  padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.sm),
                  child: Text(
                    'Nothing yet. Text you send, and text copied on ${widget.target}, shows up here.',
                    style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
                  ),
                )
              else
                for (final item in items) _historyTile(context, item),
              Padding(
                padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0),
                child: Text(
                  'Kept only until you close the app.',
                  style: theme.textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
                ),
              ),
            ],
          );
        },
      ),
    );
  }

  Widget _historyTile(BuildContext context, RemoteClipItem item) {
    final sent = item.direction == ClipDirection.sent;
    final where = sent ? 'Sent to ${item.target}' : 'Copied on ${item.target}';
    final preview = item.text.replaceAll(RegExp(r'\s+'), ' ').trim();
    return ListTile(
      leading: Icon(sent ? Icons.north_east : Icons.south_west, semanticLabel: sent ? 'Sent' : 'Copied'),
      title: Text(
        preview.isEmpty ? '(spaces only)' : preview,
        maxLines: MediaQuery.textScalerOf(context).scale(10) > 13 ? 4 : 2,
        overflow: TextOverflow.ellipsis,
      ),
      subtitle: Text('$where · ${formatRelative(item.at)}'),
      onTap: () {
        _draft.text = item.text;
        _draft.selection = TextSelection.collapsed(offset: item.text.length);
      },
      trailing: IconButton(
        tooltip: 'Copy to this phone',
        icon: const Icon(Icons.content_copy_outlined),
        onPressed: () => _copyToPhone(item),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// The framebuffer view
// ---------------------------------------------------------------------------

enum _Gesture { none, pending, drag, move, held, two, pinch, pan, scroll, done }

/// The remote screen, placed by an [RfbViewport] (fit, pinch zoom, pan),
/// with Windows App's two input modes and a real mouse:
///
/// * Touch: tap clicks there, hold right-clicks, drag drags.
/// * Mouse pointer: a cursor moved like a trackpad; tap clicks where it
///   is, hold then drag drags.
/// * Both: two-finger tap right-clicks, two-finger double-tap zooms in or
///   out, pinch zooms around the fingers, two-finger drag scrolls (or pans
///   a zoomed screen; hold the fingers still first to scroll instead).
/// * A mouse, on DeX or a tablet: moves, clicks and scrolls as itself.
class RfbView extends StatefulWidget {
  const RfbView({
    super.key,
    required this.client,
    required this.inputMode,
    this.fit = RfbFit.fit,
    this.bottomInset = 0,
    this.label,
  });

  final RfbClient client;
  final RfbInputMode inputMode;
  final RfbFit fit;

  /// How much of the view is covered from below (the keyboard and the
  /// keys row): the view slides up so the cursor stays above it.
  final double bottomInset;

  /// What TalkBack calls the screen ("Screen of mint").
  final String? label;

  @override
  State<RfbView> createState() => RfbViewState();
}

class RfbViewState extends State<RfbView> {
  final viewport = RfbViewport();
  final Map<int, Offset> _pointers = {};
  Offset? _cursorAt;
  int _buttons = 0;
  _Gesture _g = _Gesture.none;
  Offset _downAt = Offset.zero;
  int _maxPointers = 0;
  Timer? _longPress, _twoHold, _rightClick, _edgeTimer;
  bool _twoHeld = false;
  double _startSpan = 1, _startZoom = 1;
  Offset _startFocal = Offset.zero, _lastFocal = Offset.zero;
  Offset _scroll = Offset.zero;
  Offset? _edgeAt;
  double _lift = 0;
  bool _mouse = false;

  static const _slop = 10.0;
  static const _trackpadSpeed = 1.6;
  static const _scrollStep = 20.0;
  static const _longPressTime = Duration(milliseconds: 500);
  static const _doubleTapTime = Duration(milliseconds: 300);

  RfbClient get _c => widget.client;

  // The pointer starts in the middle of the remote screen.
  Offset get _cursor => _cursorAt ?? viewport.remote.center(Offset.zero);
  set _cursor(Offset p) => _cursorAt = p;

  /// Where the remote pointer is, in remote pixels.
  Offset get cursor => _cursor;

  /// How far the view is slid up to keep the cursor above the keyboard.
  double get lift => _lift;

  @override
  void didUpdateWidget(RfbView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.inputMode != widget.inputMode) _reset();
  }

  @override
  void dispose() {
    _reset();
    _rightClick?.cancel();
    super.dispose();
  }

  void _reset() {
    _longPress?.cancel();
    _twoHold?.cancel();
    _stopEdgePan();
    _pointers.clear();
    _g = _Gesture.none;
    if (_buttons != 0) {
      _buttons = 0;
      _send();
    }
  }

  void resetZoom() => setState(() => viewport.zoomAt(viewport.box.center(Offset.zero), 1));

  void _send([int? mask]) => _c.sendPointer(_cursor.dx.round(), _cursor.dy.round(), mask ?? _buttons);

  /// Presses and releases [button] (1 left, 2 middle, 4 right) where the
  /// pointer is. Two taps in quick succession arrive as two clicks, which
  /// the remote system reads as a double-click.
  void click(int button) {
    _send(button | _buttons);
    _send();
  }

  void _wheel(int mask) {
    _send(mask | _buttons);
    _send();
  }

  // View points from events: the view is drawn slid up by [_lift].
  Offset _at(Offset local) => local + Offset(0, _lift);

  Offset get _focal {
    final p = _pointers.values.take(2).toList();
    return (p[0] + p[1]) / 2;
  }

  double get _span {
    final p = _pointers.values.take(2).toList();
    return (p[0] - p[1]).distance;
  }

  void _down(PointerDownEvent e) {
    if (e.kind == PointerDeviceKind.mouse) return _mouseEvent(e.localPosition, e.buttons);
    _mouse = false;
    _pointers[e.pointer] = _at(e.localPosition);
    if (_pointers.length == 1) {
      _g = _Gesture.pending;
      _downAt = _pointers[e.pointer]!;
      _maxPointers = 1;
      _longPress = Timer(_longPressTime, _onLongPress);
    } else if (_pointers.length == 2) {
      _longPress?.cancel();
      _stopEdgePan();
      if (_buttons != 0) {
        _buttons = 0;
        _send();
      }
      _g = _Gesture.two;
      _maxPointers = 2;
      _startSpan = _span > 0 ? _span : 1;
      _startZoom = viewport.zoom;
      _startFocal = _lastFocal = _focal;
      _scroll = Offset.zero;
      _twoHeld = false;
      _twoHold?.cancel();
      _twoHold = Timer(const Duration(milliseconds: 250), () => _twoHeld = true);
    } else {
      _maxPointers = _pointers.length;
    }
  }

  void _onLongPress() {
    if (_g != _Gesture.pending || _pointers.length != 1) return;
    HapticFeedback.mediumImpact();
    if (widget.inputMode == RfbInputMode.touch) {
      _cursor = viewport.toRemote(_downAt);
      _send();
      click(4);
      _g = _Gesture.done;
    } else {
      // Mouse pointer: the left button goes down; moving now drags.
      _buttons = 1;
      _send();
      _g = _Gesture.held;
      setState(() {});
    }
  }

  void _move(PointerMoveEvent e) {
    if (e.kind == PointerDeviceKind.mouse) return _mouseEvent(e.localPosition, e.buttons);
    final prev = _pointers[e.pointer];
    if (prev == null) return;
    final p = _at(e.localPosition);
    _pointers[e.pointer] = p;
    if (_pointers.length == 1) {
      if (_g == _Gesture.pending && (p - _downAt).distance > _slop) {
        _longPress?.cancel();
        if (widget.inputMode == RfbInputMode.touch) {
          // Touch: press where the finger went down, then drag from there.
          _cursor = viewport.toRemote(_downAt);
          _send();
          _buttons = 1;
          _send();
          _g = _Gesture.drag;
          _startEdgePan();
        } else {
          _g = _Gesture.move;
        }
      }
      if (_g == _Gesture.drag) {
        _edgeAt = p;
        _cursor = viewport.toRemote(p);
        _send();
      } else if (_g == _Gesture.move || _g == _Gesture.held) {
        _moveCursor(p - prev);
      }
      return;
    }
    if (_pointers.length != 2) return;
    final focal = _focal, span = _span;
    if (_g == _Gesture.two) {
      if ((span - _startSpan).abs() > 24) {
        _g = _Gesture.pinch;
      } else if ((focal - _startFocal).distance > _slop) {
        _g = viewport.pannable && !_twoHeld ? _Gesture.pan : _Gesture.scroll;
      }
    }
    switch (_g) {
      case _Gesture.pinch:
        setState(() {
          viewport.panBy(focal - _lastFocal);
          viewport.zoomAt(focal, _startZoom * span / _startSpan);
        });
      case _Gesture.pan:
        setState(() => viewport.panBy(focal - _lastFocal));
      case _Gesture.scroll:
        // Natural scrolling, as on the phone: fingers up scrolls down.
        _scroll += focal - _lastFocal;
        while (_scroll.dy.abs() >= _scrollStep) {
          _wheel(_scroll.dy > 0 ? 8 : 16);
          _scroll -= Offset(0, _scroll.dy.sign * _scrollStep);
        }
        while (_scroll.dx.abs() >= _scrollStep) {
          _wheel(_scroll.dx > 0 ? 32 : 64);
          _scroll -= Offset(_scroll.dx.sign * _scrollStep, 0);
        }
      default:
        break;
    }
    _lastFocal = focal;
  }

  /// Mouse pointer mode: moves the cursor like a trackpad, and keeps it
  /// in view on a zoomed screen.
  void _moveCursor(Offset delta) {
    final perPixel = _trackpadSpeed / viewport.scale;
    _cursor = Offset(
      (_cursor.dx + delta.dx * perPixel).clamp(0.0, viewport.remote.width - 1),
      (_cursor.dy + delta.dy * perPixel).clamp(0.0, viewport.remote.height - 1),
    );
    _send();
    setState(() => viewport.reveal(viewport.toLocal(_cursor)));
  }

  void _up(PointerUpEvent e) {
    if (e.kind == PointerDeviceKind.mouse) return _mouseEvent(e.localPosition, e.buttons);
    final p = _pointers.remove(e.pointer);
    if (p == null || _pointers.isNotEmpty) return;
    _longPress?.cancel();
    _twoHold?.cancel();
    _stopEdgePan();
    switch (_g) {
      case _Gesture.pending:
        if (widget.inputMode == RfbInputMode.touch) {
          if (!viewport.rect.contains(_downAt)) break;
          _cursor = viewport.toRemote(_downAt);
          _send();
          setState(() {});
        }
        click(1);
      case _Gesture.drag || _Gesture.held:
        _buttons = 0;
        _send();
        setState(() {});
      case _Gesture.two when _maxPointers == 2:
        if (_rightClick?.isActive ?? false) {
          // The second two-finger tap: zoom, not a right-click.
          _rightClick!.cancel();
          setState(() => viewport.toggleZoom(_startFocal));
        } else {
          final at = _startFocal;
          _rightClick = Timer(_doubleTapTime, () {
            if (widget.inputMode == RfbInputMode.touch) {
              _cursor = viewport.toRemote(at);
              _send();
            }
            click(4);
          });
        }
      default:
        break;
    }
    _g = _Gesture.none;
  }

  void _cancel(PointerCancelEvent e) {
    _pointers.remove(e.pointer);
    if (_pointers.isEmpty) _reset();
  }

  /// A real mouse: the pointer goes where it points; its buttons are its own.
  void _mouseEvent(Offset local, int buttons) {
    _mouse = true;
    _cursor = viewport.toRemote(_at(local));
    _buttons = (buttons & kPrimaryButton != 0 ? 1 : 0) |
        (buttons & kMiddleMouseButton != 0 ? 2 : 0) |
        (buttons & kSecondaryButton != 0 ? 4 : 0);
    _send();
  }

  void _signal(PointerSignalEvent e) {
    if (e is! PointerScrollEvent) return;
    _cursor = viewport.toRemote(_at(e.localPosition));
    if (e.scrollDelta.dy != 0) _wheel(e.scrollDelta.dy < 0 ? 8 : 16);
    if (e.scrollDelta.dx != 0) _wheel(e.scrollDelta.dx < 0 ? 32 : 64);
  }

  // Touch drag near an edge of a zoomed screen pans it, so a window can
  // be dragged further than the view shows.
  void _startEdgePan() {
    _edgeTimer ??= Timer.periodic(const Duration(milliseconds: 16), (_) {
      final at = _edgeAt;
      if (at == null || !mounted) return;
      final before = viewport.offset;
      viewport.panBy(viewport.edgePan(at));
      if (viewport.offset == before) return;
      _cursor = viewport.toRemote(at);
      _send();
      setState(() {});
    });
  }

  void _stopEdgePan() {
    _edgeTimer?.cancel();
    _edgeTimer = null;
    _edgeAt = null;
  }

  @override
  Widget build(BuildContext context) {
    final dpr = MediaQuery.devicePixelRatioOf(context);
    return ValueListenableBuilder<ui.Image?>(
      valueListenable: _c.frame,
      builder: (context, image, _) => LayoutBuilder(builder: (context, constraints) {
        final remote = Size((_c.width > 0 ? _c.width : 1280).toDouble(), (_c.height > 0 ? _c.height : 720).toDouble());
        viewport.layout(box: constraints.biggest, remote: remote, dpr: dpr, fit: widget.fit);
        final caret = viewport.toLocal(_cursor);
        _lift = widget.bottomInset > 0 ? viewport.keepInSight(_lift, caret, viewport.box.height - widget.bottomInset) : 0;
        final showCursor = widget.inputMode == RfbInputMode.trackpad && !_mouse && image != null;
        return Semantics(
          label: widget.label,
          hint: widget.inputMode == RfbInputMode.trackpad
              ? 'Mouse pointer. Drag to move the pointer, tap to click, hold then drag to drag, tap with two fingers to right-click'
              : 'Touch. Tap to click, hold to right-click, drag to drag, pinch to zoom, drag two fingers to scroll',
          child: Listener(
            behavior: HitTestBehavior.opaque,
            onPointerDown: _down,
            onPointerMove: _move,
            onPointerUp: _up,
            onPointerCancel: _cancel,
            onPointerHover: (e) {
              if (e.kind == PointerDeviceKind.mouse) _mouseEvent(e.localPosition, 0);
            },
            onPointerSignal: _signal,
            child: ClipRect(
              child: Transform.translate(
                offset: Offset(0, -_lift),
                child: Stack(children: [
                  if (image != null)
                    Positioned.fromRect(
                      rect: viewport.rect,
                      child: RawImage(
                        image: image,
                        fit: BoxFit.fill,
                        filterQuality: viewport.scale < 1 ? FilterQuality.medium : FilterQuality.low,
                      ),
                    ),
                  if (showCursor)
                    Positioned(
                      left: caret.dx - 1,
                      top: caret.dy - 1,
                      child: const IgnorePointer(child: CustomPaint(size: Size(14, 21), painter: _CursorPainter())),
                    ),
                ]),
              ),
            ),
          ),
        );
      }),
    );
  }
}

/// The mouse pointer mode's cursor: a white arrow with a dark edge, legible
/// on any picture.
class _CursorPainter extends CustomPainter {
  const _CursorPainter();

  @override
  void paint(Canvas canvas, Size size) {
    final w = size.width, h = size.height;
    final path = Path()
      ..moveTo(0, 0)
      ..lineTo(0, h * 0.82)
      ..lineTo(w * 0.3, h * 0.6)
      ..lineTo(w * 0.52, h)
      ..lineTo(w * 0.68, h * 0.93)
      ..lineTo(w * 0.47, h * 0.55)
      ..lineTo(w, h * 0.55)
      ..close();
    canvas.drawPath(path, Paint()..color = Colors.white);
    canvas.drawPath(
        path,
        Paint()
          ..color = Colors.black
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1.2);
  }

  @override
  bool shouldRepaint(_CursorPainter oldDelegate) => false;
}

// ---------------------------------------------------------------------------
// The session
// ---------------------------------------------------------------------------

/// An item for the console's overflow menu.
@immutable
class ConsoleMenuItem {
  const ConsoleMenuItem({required this.icon, required this.label, required this.onPressed});
  final IconData icon;
  final String label;

  /// Gets a context inside the console's dark theme, so sheets and
  /// dialogs opened from it match the console.
  final void Function(BuildContext context)? onPressed;
}

/// How many pictures a second the console asks for.
enum RfbQuality {
  best('Best picture', 0),
  balanced('Balanced · 20 fps', 20),
  saver('Data saver · 5 fps', 5);

  const RfbQuality(this.label, this.fps);
  final String label;
  final int fps;
}

/// A remote screen session, laid out like Microsoft's Windows App: the
/// remote screen full screen and immersive (the system bars come back
/// with a swipe), with a small session toolbar - a pill at the top that
/// can be slid along the edge and hides itself after a few seconds,
/// leaving a tab to bring it back. It is drawn in the dark (or true
/// black) theme of the app's style, like a video player, so the remote
/// picture keeps its own colours.
///
/// The input mode, the stay-in-landscape choice, and per [clipboardTarget]
/// the fit and the quality, are remembered.
///
/// The screen that owns it connects [client]; until it wants a connection
/// (a stopped VM, a desktop that isn't installed) it passes [placeholder],
/// shown in an ordinary page with an app bar.
class RemoteConsoleFrame extends StatefulWidget {
  const RemoteConsoleFrame({
    super.key,
    required this.client,
    required this.title,
    required this.clipboardTarget,
    this.clipboardHint,
    this.menuItems = const [],
    this.placeholder,
    this.placeholderStatus,
    this.screenLabel,
    this.history,
    this.onMatchPhone,
  });

  final RfbClient client;
  final String title;

  /// Who the clipboard sheet sends to ("mint", "the server"); also the
  /// key the fit and quality are remembered under.
  final String clipboardTarget;
  final String? clipboardHint;

  /// The screen's own items, shown first in the More menu.
  final List<ConsoleMenuItem> menuItems;

  /// Shown instead of the console while there is nothing to connect to.
  final Widget? placeholder;

  /// The app bar's status line while [placeholder] shows ("Off").
  final String? placeholderStatus;
  final String? screenLabel;
  final RemoteClipboardHistory? history;

  /// Resizes the remote screen to this phone's shape, for "Match this
  /// phone". Null where the remote screen can't be resized from here.
  final Future<void> Function(BuildContext context)? onMatchPhone;

  @override
  State<RemoteConsoleFrame> createState() => RemoteConsoleFrameState();
}

class RemoteConsoleFrameState extends State<RemoteConsoleFrame> {
  final _viewKey = GlobalKey<RfbViewState>();
  final _pillKey = GlobalKey();
  final _sessionFocus = FocusNode(debugLabel: 'remote session');
  final _keyboardFocus = FocusNode(debugLabel: 'remote keyboard');
  final _keyboardText = TextEditingController(text: _sentinel);
  final _keyScroll = ScrollController();
  StreamSubscription<String>? _clipSub;

  RfbInputMode _inputMode = RfbInputMode.touch;
  RfbFit _fit = RfbFit.fit;
  RfbQuality _quality = RfbQuality.best;
  bool _showKeys = false;
  bool _landscapeLocked = false;
  bool _immersive = false;
  final Set<int> _latched = {};
  final Map<PhysicalKeyboardKey, int> _downKeys = {};

  // The toolbar: shown, where along the top edge, and when it hides.
  bool _toolbarShown = true;
  double _toolbarX = 0;
  int _menusOpen = 0;
  Timer? _hideTimer;
  static const toolbarHideAfter = Duration(seconds: 4);

  // Reconnecting after a lost connection: up to [_maxReconnects] tries,
  // 1, 2 then 4 seconds apart, over the last picture dimmed.
  int _reconnects = 0;
  Timer? _reconnectTimer;
  static const _maxReconnects = 3;

  static const keysRowHeight = 56.0;

  // The hidden field always holds this, so a backspace on an "empty"
  // field still arrives as a change.
  static const _sentinel = '​​';

  RfbClient get _client => widget.client;
  RemoteClipboardHistory get _history => widget.history ?? RemoteClipboardHistory.instance;
  bool get _session => widget.placeholder == null;

  /// Whether the session toolbar is showing (rather than its tab).
  bool get toolbarShown => _toolbarShown;

  @override
  void initState() {
    super.initState();
    _client.status.addListener(_statusChanged);
    _clipSub = _client.remoteClipboard.listen(_remoteCopied);
    _keyboardFocus.addListener(() => setState(() {}));
    _poke();
    _loadPrefs();
  }

  @override
  void didUpdateWidget(RemoteConsoleFrame oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.client != widget.client) {
      oldWidget.client.status.removeListener(_statusChanged);
      _clipSub?.cancel();
      _client.status.addListener(_statusChanged);
      _clipSub = _client.remoteClipboard.listen(_remoteCopied);
    }
    // The host desktop learns it can resize only once it has checked.
    if (oldWidget.onMatchPhone == null && widget.onMatchPhone != null && _fit == RfbFit.matchPhone) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) widget.onMatchPhone?.call(context);
      });
    }
  }

  @override
  void dispose() {
    _client.status.removeListener(_statusChanged);
    _clipSub?.cancel();
    _hideTimer?.cancel();
    _reconnectTimer?.cancel();
    _sessionFocus.dispose();
    _keyboardFocus.dispose();
    _keyboardText.dispose();
    _keyScroll.dispose();
    if (_immersive) SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
    if (_landscapeLocked) SystemChrome.setPreferredOrientations(const []);
    super.dispose();
  }

  // --- remembered choices ---

  static Future<String?> _pref(String name) async {
    try {
      return await StorageService.instance.getConsolePref(name);
    } catch (_) {
      return null;
    }
  }

  static Future<void> _savePref(String name, String value) async {
    try {
      await StorageService.instance.setConsolePref(name, value);
    } catch (e) {
      debugPrint('[console] Could not save $name: $e');
    }
  }

  Future<void> _loadPrefs() async {
    final target = widget.clipboardTarget;
    final input = RfbInputMode.values.asNameMap()[await _pref('input')];
    final fit = RfbFit.values.asNameMap()[await _pref('fit@$target')];
    final quality = RfbQuality.values.asNameMap()[await _pref('quality@$target')];
    final landscape = await _pref('landscape') == 'true';
    if (!mounted) return;
    setState(() {
      _inputMode = input ?? _inputMode;
      _fit = fit ?? _fit;
      _quality = quality ?? _quality;
    });
    _client.maxFps = _quality.fps;
    if (landscape) _setLandscape(true, save: false);
    if (_fit == RfbFit.matchPhone) await widget.onMatchPhone?.call(context);
  }

  // --- connection ---

  void _statusChanged() {
    if (!mounted) return;
    final s = _client.status.value;
    if (!_client.isConnected) {
      _latched.clear();
      _downKeys.clear();
    }
    if (s == RfbStatus.connected) {
      _reconnects = 0;
      _client.maxFps = _quality.fps;
      _poke();
    } else if (_session &&
        (s == RfbStatus.disconnected || (s == RfbStatus.failed && _reconnects > 0)) &&
        _reconnects < _maxReconnects) {
      _reconnects++;
      _reconnectTimer?.cancel();
      _reconnectTimer = Timer(Duration(seconds: 1 << (_reconnects - 1)), () {
        if (mounted && _session) _client.connect();
      });
    }
    setState(() {});
  }

  bool get _reconnecting =>
      _reconnects > 0 && ((_reconnectTimer?.isActive ?? false) || _client.status.value == RfbStatus.connecting);

  void _retry() {
    _reconnectTimer?.cancel();
    setState(() => _reconnects = 0);
    _client.connect();
  }

  void _remoteCopied(String text) {
    if (!mounted || !_history.record(text, ClipDirection.copied, widget.clipboardTarget)) return;
    final who = widget.clipboardTarget == RemoteClipboardHistory.hostTarget ? 'The server' : widget.clipboardTarget;
    ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(
      content: Text('$who copied text'),
      action: SnackBarAction(label: 'Copy here', onPressed: () => Clipboard.setData(ClipboardData(text: text))),
    ));
  }

  // --- toolbar ---

  /// Shows the toolbar and restarts its hide timer. It stays while a menu
  /// is open, and for good while TalkBack or Switch Access is on.
  void _poke() {
    _hideTimer?.cancel();
    if (!_toolbarShown) setState(() => _toolbarShown = true);
    _hideTimer = Timer(toolbarHideAfter, () {
      if (!mounted || _menusOpen > 0 || MediaQuery.maybeAccessibleNavigationOf(context) == true) return;
      setState(() => _toolbarShown = false);
    });
  }

  void _menuOpened() {
    _menusOpen++;
    _hideTimer?.cancel();
  }

  void _menuClosed() {
    _menusOpen = _menusOpen > 0 ? _menusOpen - 1 : 0;
    _poke();
  }

  void _slideToolbar(DragUpdateDetails d) {
    final width = MediaQuery.sizeOf(context).width;
    final pill = _pillKey.currentContext?.size?.width ?? width;
    final room = ((width - pill) / 2 - Space.sm).clamp(0.0, double.infinity);
    setState(() => _toolbarX = (_toolbarX + d.delta.dx).clamp(-room, room));
    _poke();
  }

  // --- keyboard ---

  void _toggleKeyboard() {
    final open = _keyboardFocus.hasFocus;
    if (open && MediaQuery.viewInsetsOf(context).bottom == 0) {
      // Focused, but the system hid the keyboard (back): show it again.
      SystemChannels.textInput.invokeMethod<void>('TextInput.show');
      return;
    }
    if (open) {
      _sessionFocus.requestFocus();
    } else {
      _keyboardFocus.requestFocus();
      setState(() => _showKeys = true);
    }
  }

  void _key(int keysym) {
    _client.tapKey(keysym);
    _releaseLatches();
  }

  void _onKeyboardText(String value) {
    if (value.length < _sentinel.length) {
      for (var i = value.length; i < _sentinel.length; i++) {
        _key(Keysym.backspace);
      }
    } else {
      final added = value.startsWith(_sentinel) ? value.substring(_sentinel.length) : value.replaceAll('​', '');
      for (final rune in added.runes) {
        _key(RfbClient.keysymForRune(rune));
      }
    }
    _keyboardText.value = const TextEditingValue(text: _sentinel, selection: TextSelection.collapsed(offset: 2));
  }

  static final Map<LogicalKeyboardKey, int> _modifierKeys = {
    LogicalKeyboardKey.shiftLeft: Keysym.shift,
    LogicalKeyboardKey.shiftRight: 0xffe2,
    LogicalKeyboardKey.controlLeft: Keysym.control,
    LogicalKeyboardKey.controlRight: 0xffe4,
    LogicalKeyboardKey.altLeft: Keysym.alt,
    LogicalKeyboardKey.altRight: 0xffea,
    LogicalKeyboardKey.metaLeft: Keysym.superKey,
    LogicalKeyboardKey.metaRight: 0xffec,
  };

  // Keys that also arrive through the soft keyboard field as text.
  static final Map<LogicalKeyboardKey, int> _textKeys = {
    LogicalKeyboardKey.enter: Keysym.enter,
    LogicalKeyboardKey.numpadEnter: Keysym.enter,
    LogicalKeyboardKey.backspace: Keysym.backspace,
  };

  /// A hardware keyboard (DeX, a tablet's keyboard cover): every key goes
  /// to the remote as itself, modifiers and shortcuts included. While the
  /// soft keyboard's field has focus, plain text still comes through it
  /// (so the phone's input methods keep working).
  KeyEventResult _onHardwareKey(FocusNode node, KeyEvent event) {
    if (!_client.isConnected) return KeyEventResult.ignored;
    final physical = event.physicalKey;
    if (event is KeyUpEvent) {
      final sym = _downKeys.remove(physical);
      if (sym == null) return KeyEventResult.ignored;
      _client.sendKey(sym, false);
      if (!_modifierKeys.containsValue(sym)) _releaseLatches();
      return KeyEventResult.handled;
    }
    final hw = HardwareKeyboard.instance;
    final shortcut = hw.isControlPressed || hw.isAltPressed || hw.isMetaPressed;
    final viaIme = _keyboardFocus.hasFocus && !shortcut;
    var sym = _downKeys[physical] ?? _modifierKeys[event.logicalKey] ?? Keysym.hardware[event.logicalKey];
    if (sym == null && !viaIme) sym = _textKeys[event.logicalKey];
    if (sym == null) {
      if (viaIme) return KeyEventResult.ignored;
      final label = event.logicalKey.keyLabel;
      final text = shortcut && label.runes.length == 1 ? label.toLowerCase() : event.character;
      if (text == null || text.isEmpty) return KeyEventResult.ignored;
      sym = RfbClient.keysymForRune(text.runes.first);
    }
    _downKeys[physical] = sym;
    _client.sendKey(sym, true);
    return KeyEventResult.handled;
  }

  void _toggleLatch(int keysym) {
    HapticFeedback.selectionClick();
    setState(() {
      if (_latched.remove(keysym)) {
        _client.sendKey(keysym, false);
      } else {
        _latched.add(keysym);
        _client.sendKey(keysym, true);
      }
    });
  }

  void _releaseLatches() {
    if (_latched.isEmpty) return;
    for (final k in _latched) {
      _client.sendKey(k, false);
    }
    setState(_latched.clear);
  }

  // --- choices ---

  void _setInputMode(RfbInputMode mode) {
    HapticFeedback.selectionClick();
    setState(() => _inputMode = mode);
    _savePref('input', mode.name);
    _poke();
  }

  Future<void> _setFit(RfbFit fit) async {
    setState(() => _fit = fit);
    await _savePref('fit@${widget.clipboardTarget}', fit.name);
    if (fit == RfbFit.matchPhone && mounted) await widget.onMatchPhone?.call(context);
  }

  void _setQuality(RfbQuality q) {
    setState(() => _quality = q);
    _client.maxFps = q.fps;
    _savePref('quality@${widget.clipboardTarget}', q.name);
  }

  void _setLandscape(bool on, {bool save = true}) {
    setState(() => _landscapeLocked = on);
    SystemChrome.setPreferredOrientations(
        on ? const [DeviceOrientation.landscapeLeft, DeviceOrientation.landscapeRight] : const []);
    if (save) _savePref('landscape', '$on');
  }

  void _setImmersive(bool on) {
    if (on == _immersive) return;
    _immersive = on;
    SystemChrome.setEnabledSystemUIMode(on ? SystemUiMode.immersiveSticky : SystemUiMode.edgeToEdge);
  }

  Future<void> _openClipboard(BuildContext context) => RemoteClipboardSheet.show(context,
      client: _client, target: widget.clipboardTarget, hint: widget.clipboardHint, history: widget.history);

  Future<void> _onBack(bool didPop, Object? result) async {
    if (didPop) return;
    final close = await ConfirmDialog.confirm(
      context,
      title: 'Disconnect?',
      message: widget.clipboardTarget == RemoteClipboardHistory.hostTarget
          ? 'The server keeps running. You can open it again from More.'
          : '${widget.clipboardTarget} keeps running. You can open the console again from the VM list.',
      confirmLabel: 'Disconnect',
    );
    if (close && mounted) Navigator.of(context).pop();
  }

  void _disconnect() {
    _reconnectTimer?.cancel();
    _client.close();
    Navigator.of(context).pop();
  }

  String _statusText() => widget.placeholder != null && widget.placeholderStatus != null
      ? widget.placeholderStatus!
      : switch (_client.status.value) {
          RfbStatus.connected => 'Connected · ${_client.width} × ${_client.height}',
          RfbStatus.connecting => 'Connecting…',
          RfbStatus.disconnected => 'Disconnected',
          RfbStatus.failed => 'Not connected',
          RfbStatus.idle => widget.placeholder != null ? 'Not connected' : 'Connecting…',
        };

  /// The console's theme: the app's style, always dark - true black when
  /// the app is in true black.
  static ThemeData _theme(BuildContext context) {
    final app = Theme.of(context);
    return AppTheme.build(
      brightness: Brightness.dark,
      black: app.brightness == Brightness.dark && app.colorScheme.surface == const Color(0xFF000000),
      accent: ThemeController.instance.value.accent,
      direction: DesignTokens.of(context).direction,
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = _theme(context);
    _setImmersive(_session);
    return Theme(
      data: theme,
      child: Builder(builder: (context) {
        final connected = _client.isConnected && _session;
        return PopScope(
          canPop: !connected,
          onPopInvokedWithResult: _onBack,
          child: _session
              ? AnnotatedRegion<SystemUiOverlayStyle>(
                  value: AppTheme.systemBarsStyle(Brightness.dark),
                  child: Scaffold(
                    backgroundColor: Colors.black,
                    resizeToAvoidBottomInset: false,
                    body: _sessionView(context, connected),
                  ),
                )
              : Scaffold(
                  backgroundColor: theme.colorScheme.surfaceContainerLowest,
                  appBar: _appBar(context),
                  body: widget.placeholder,
                ),
        );
      }),
    );
  }

  /// Before a session: the title, its state and the screen's own menu.
  PreferredSizeWidget _appBar(BuildContext context) {
    final theme = Theme.of(context);
    return AppBar(
      backgroundColor: theme.colorScheme.surfaceContainer,
      systemOverlayStyle: AppTheme.systemBarsStyle(Brightness.dark),
      title: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(widget.title, maxLines: 1, overflow: TextOverflow.ellipsis),
          Semantics(
            liveRegion: true,
            child: Text(
              _statusText(),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.bodySmall?.tabular.copyWith(color: theme.colorScheme.onSurfaceVariant),
            ),
          ),
        ],
      ),
      actions: [if (widget.menuItems.isNotEmpty) _moreMenu(context, connected: false)],
    );
  }

  Widget _sessionView(BuildContext context, bool connected) {
    final keyboard = MediaQuery.viewInsetsOf(context).bottom;
    final keysShown = _showKeys && connected;
    return Focus(
      focusNode: _sessionFocus,
      autofocus: true,
      onKeyEvent: _onHardwareKey,
      child: Stack(
        fit: StackFit.expand,
        children: [
          RfbView(
            key: _viewKey,
            client: _client,
            inputMode: _inputMode,
            fit: _fit,
            bottomInset: keyboard + (keysShown ? keysRowHeight : 0),
            label: widget.screenLabel ?? 'Screen of ${widget.title}',
          ),
          // The hidden field that brings up the soft keyboard and receives
          // its text. It must be on screen (1x1) for the IME to attach.
          Positioned(
            left: 0,
            top: 0,
            width: 1,
            height: 1,
            child: ExcludeSemantics(
              child: Opacity(
                opacity: 0,
                child: TextField(
                  focusNode: _keyboardFocus,
                  controller: _keyboardText,
                  autocorrect: false,
                  enableSuggestions: false,
                  enableIMEPersonalizedLearning: false,
                  keyboardType: TextInputType.visiblePassword,
                  textInputAction: TextInputAction.send,
                  onChanged: _onKeyboardText,
                  onSubmitted: (_) => _key(Keysym.enter),
                  onEditingComplete: () {},
                ),
              ),
            ),
          ),
          if (!connected) _overlay(context),
          if (keysShown)
            Positioned(left: 0, right: 0, bottom: keyboard, child: _keyBar(context)),
          if (connected) _toolbar(context),
        ],
      ),
    );
  }

  /// Connecting, reconnecting over the last picture, or lost.
  Widget _overlay(BuildContext context) {
    final status = _client.status.value;
    final hasFrame = _client.frame.value != null;
    final reconnecting = _reconnecting;
    final connecting = reconnecting || status == RfbStatus.connecting || status == RfbStatus.idle;
    return ColoredBox(
      color: hasFrame ? Colors.black.withValues(alpha: 0.6) : Colors.black,
      child: SafeArea(
        child: _SessionMessage(
          heading: reconnecting
              ? 'Reconnecting to ${widget.title}'
              : switch (status) {
                  RfbStatus.disconnected => 'Connection lost',
                  RfbStatus.failed => _reconnects > 0 ? 'Connection lost' : "Couldn't connect",
                  _ => 'Connecting to ${widget.title}',
                },
          detail: reconnecting
              ? 'Try $_reconnects of $_maxReconnects'
              : connecting
                  ? null
                  : _client.error.value,
          busy: connecting,
          onRetry: connecting ? null : _retry,
          onClose: _disconnect,
        ),
      ),
    );
  }

  Widget _toolbar(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final top = MediaQuery.paddingOf(context).top + Space.sm;
    final Widget child;
    if (_toolbarShown) {
      child = Material(
        key: _pillKey,
        color: scheme.surfaceContainerHigh.withValues(alpha: 0.94),
        shape: StadiumBorder(side: BorderSide(color: scheme.outlineVariant)),
        elevation: 3,
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            // The grip: slide the toolbar along the top edge.
            ExcludeSemantics(
              child: GestureDetector(
                behavior: HitTestBehavior.opaque,
                onHorizontalDragUpdate: _slideToolbar,
                child: SizedBox(
                  width: 24,
                  height: 48,
                  child: Icon(Icons.drag_indicator, size: 20, color: scheme.onSurfaceVariant),
                ),
              ),
            ),
            // 48dp targets whatever the style's density. On a narrow
            // screen (or at large text) the middle scrolls; Disconnect stays.
            Flexible(
              child: IconButtonTheme(
                data: IconButtonThemeData(style: IconButton.styleFrom(minimumSize: const Size.square(48))),
                child: Row(mainAxisSize: MainAxisSize.min, children: [
                  Flexible(
                    child: SingleChildScrollView(
                      scrollDirection: Axis.horizontal,
                      child: Row(mainAxisSize: MainAxisSize.min, children: _toolbarButtons(context)),
                    ),
                  ),
                  Padding(
                    padding: const EdgeInsets.only(right: Space.xs),
                    child: IconButton(
                      tooltip: 'Disconnect',
                      icon: Icon(Icons.close, color: scheme.error),
                      onPressed: _disconnect,
                    ),
                  ),
                ]),
              ),
            ),
          ],
        ),
      );
    } else {
      child = Semantics(
        button: true,
        label: 'Show session toolbar',
        excludeSemantics: true,
        child: InkWell(
          onTap: _poke,
          customBorder: const StadiumBorder(),
          child: SizedBox(
            width: 64,
            height: 48,
            child: Align(
              alignment: Alignment.topCenter,
              child: Container(
                width: 48,
                height: 20,
                decoration: ShapeDecoration(
                  color: scheme.surfaceContainerHigh.withValues(alpha: 0.7),
                  shape: StadiumBorder(side: BorderSide(color: scheme.outlineVariant)),
                ),
                child: Icon(Icons.expand_more, size: 18, color: scheme.onSurfaceVariant),
              ),
            ),
          ),
        ),
      );
    }
    return Positioned(
      top: top - (_toolbarShown ? 0 : Space.sm),
      left: Space.sm,
      right: Space.sm,
      child: Center(
        child: Transform.translate(
          offset: Offset(_toolbarX, 0),
          child: Semantics(
            container: true,
            label: 'Session toolbar',
            explicitChildNodes: true,
            child: AnimatedSwitcher(duration: Motion.of(context).short, child: child),
          ),
        ),
      ),
    );
  }

  List<Widget> _toolbarButtons(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final mouse = _inputMode == RfbInputMode.trackpad;
    ButtonStyle selected(bool on) => IconButton.styleFrom(
          backgroundColor: on ? scheme.secondaryContainer : null,
          foregroundColor: on ? scheme.onSecondaryContainer : null,
        );
    return [
      IconButton(
        tooltip: _keyboardFocus.hasFocus ? 'Hide keyboard' : 'Show keyboard',
        isSelected: _keyboardFocus.hasFocus,
        icon: const Icon(Icons.keyboard_outlined),
        selectedIcon: const Icon(Icons.keyboard_hide_outlined),
        style: selected(_keyboardFocus.hasFocus),
        onPressed: () {
          _toggleKeyboard();
          _poke();
        },
      ),
      IconButton(
        tooltip: mouse ? 'Input: mouse pointer. Switch to touch' : 'Input: touch. Switch to mouse pointer',
        icon: Icon(mouse ? Icons.mouse_outlined : Icons.touch_app_outlined),
        onPressed: () => _setInputMode(mouse ? RfbInputMode.touch : RfbInputMode.trackpad),
      ),
      _displayMenu(context),
      IconButton(
        tooltip: _showKeys ? 'Hide extra keys' : 'Extra keys',
        isSelected: _showKeys,
        icon: const Icon(Icons.keyboard_command_key),
        style: selected(_showKeys),
        onPressed: () {
          setState(() => _showKeys = !_showKeys);
          _poke();
        },
      ),
      IconButton(
        tooltip: 'Clipboard',
        icon: const Icon(Icons.content_paste_outlined),
        onPressed: () {
          _hideTimer?.cancel();
          _openClipboard(context).whenComplete(() {
            if (mounted) _poke();
          });
        },
      ),
      _moreMenu(context, connected: true),
    ];
  }

  /// Fit, zoom, quality and orientation.
  Widget _displayMenu(BuildContext context) {
    final zoomed = (_viewKey.currentState?.viewport.zoom ?? 1) > 1.01;
    return MenuAnchor(
      onOpen: _menuOpened,
      onClose: _menuClosed,
      builder: (context, controller, _) => IconButton(
        tooltip: 'Display',
        icon: const Icon(Icons.aspect_ratio_outlined),
        onPressed: () => controller.isOpen ? controller.close() : controller.open(),
      ),
      menuChildren: [
        MenuItemButton(
          onPressed: null,
          child: Text('${_client.width} × ${_client.height}', style: Theme.of(context).textTheme.labelMedium?.tabular),
        ),
        for (final f in RfbFit.values)
          if (f != RfbFit.matchPhone || widget.onMatchPhone != null)
            RadioMenuButton<RfbFit>(
              value: f,
              groupValue: _fit,
              onChanged: (v) => _setFit(v!),
              child: Text(f.label),
            ),
        if (zoomed)
          MenuItemButton(
            leadingIcon: const Icon(Icons.zoom_out_map_outlined),
            onPressed: () => _viewKey.currentState?.resetZoom(),
            child: const Text('Reset zoom'),
          ),
        const Divider(),
        for (final q in RfbQuality.values)
          RadioMenuButton<RfbQuality>(
            value: q,
            groupValue: _quality,
            onChanged: (v) => _setQuality(v!),
            child: Text(q.label),
          ),
        const Divider(),
        CheckboxMenuButton(
          value: _landscapeLocked,
          onChanged: (v) => _setLandscape(v ?? false),
          child: const Text('Stay in landscape'),
        ),
      ],
    );
  }

  Widget _moreMenu(BuildContext context, {required bool connected}) {
    return MenuAnchor(
      onOpen: _menuOpened,
      onClose: _menuClosed,
      builder: (context, controller, _) => IconButton(
        tooltip: 'More options',
        icon: const Icon(Icons.more_vert),
        onPressed: () => controller.isOpen ? controller.close() : controller.open(),
      ),
      menuChildren: [
        for (final item in widget.menuItems)
          MenuItemButton(
            leadingIcon: Icon(item.icon),
            onPressed: item.onPressed == null ? null : () => item.onPressed!(context),
            child: Text(item.label),
          ),
        if (connected) ...[
          if (widget.menuItems.isNotEmpty) const Divider(),
          MenuItemButton(
            leadingIcon: const Icon(Icons.refresh),
            onPressed: () {
              _client.close();
              _retry();
            },
            child: const Text('Reconnect'),
          ),
        ],
      ],
    );
  }

  Widget _keyBar(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final gutter = Space.gutter(context);
    Widget key(String label, int keysym, {String? semantics}) => Padding(
          padding: const EdgeInsets.only(right: Space.sm),
          child: Semantics(
            label: semantics,
            button: true,
            child: ActionChip(label: Text(label), onPressed: () => _key(keysym)),
          ),
        );
    Widget iconKey(IconData icon, String semantics, int keysym) => Padding(
          padding: const EdgeInsets.only(right: Space.sm),
          child: ActionChip(
            label: Icon(icon, size: 18, semanticLabel: semantics),
            tooltip: semantics,
            onPressed: () => _key(keysym),
          ),
        );
    Widget latch(String label, int keysym) => Padding(
          padding: const EdgeInsets.only(right: Space.sm),
          child: FilterChip(
            label: Text(label),
            showCheckmark: false,
            selected: _latched.contains(keysym),
            tooltip: '$label stays down for the next key',
            onSelected: (_) => _toggleLatch(keysym),
          ),
        );
    Widget combo(String label, List<int> keys) => Padding(
          padding: const EdgeInsets.only(right: Space.sm),
          child: ActionChip(
            label: Text(label),
            onPressed: () {
              HapticFeedback.selectionClick();
              _client.sendCombo(keys);
              _releaseLatches();
            },
          ),
        );
    return Material(
      color: scheme.surfaceContainer.withValues(alpha: 0.94),
      child: SizedBox(
        height: keysRowHeight,
        child: FadingEdges(
          controller: _keyScroll,
          child: ListView(
            controller: _keyScroll,
            scrollDirection: Axis.horizontal,
            padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.sm),
            children: [
              key('Esc', Keysym.escape, semantics: 'Escape'),
              key('Tab', Keysym.tab),
              latch('Ctrl', Keysym.control),
              latch('Alt', Keysym.alt),
              latch('Shift', Keysym.shift),
              latch('Win', Keysym.superKey),
              iconKey(Icons.arrow_back, 'Left arrow', Keysym.left),
              iconKey(Icons.arrow_upward, 'Up arrow', Keysym.up),
              iconKey(Icons.arrow_downward, 'Down arrow', Keysym.down),
              iconKey(Icons.arrow_forward, 'Right arrow', Keysym.right),
              key('Del', Keysym.delete, semantics: 'Delete'),
              key('Home', Keysym.home),
              key('End', Keysym.end),
              key('PgUp', Keysym.pageUp, semantics: 'Page up'),
              key('PgDn', Keysym.pageDown, semantics: 'Page down'),
              combo('Ctrl+Alt+Del', const [Keysym.control, Keysym.alt, Keysym.delete]),
              for (var i = 1; i <= 12; i++) key('F$i', Keysym.f(i)),
            ],
          ),
        ),
      ),
    );
  }
}

/// Connecting, reconnecting or lost, over the session.
class _SessionMessage extends StatelessWidget {
  const _SessionMessage({required this.heading, this.detail, required this.busy, this.onRetry, required this.onClose});

  final String heading;
  final String? detail;
  final bool busy;
  final VoidCallback? onRetry;
  final VoidCallback onClose;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return SingleChildScrollView(
      padding: EdgeInsets.symmetric(horizontal: Space.gutter(context), vertical: Space.xl),
      child: ConstrainedBox(
        constraints: BoxConstraints(minHeight: MediaQuery.sizeOf(context).height * 0.6),
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 360),
            child: Semantics(
              liveRegion: true,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  if (busy)
                    const SizedBox.square(dimension: 40, child: CircularProgressIndicator())
                  else
                    Icon(Icons.desktop_access_disabled_outlined, size: 48, color: scheme.onSurfaceVariant),
                  const SizedBox(height: Space.lg),
                  Text(heading, style: theme.textTheme.titleLarge, textAlign: TextAlign.center),
                  if (detail != null) ...[
                    const SizedBox(height: Space.sm),
                    Text(detail!,
                        style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
                        textAlign: TextAlign.center),
                  ],
                  const SizedBox(height: Space.xl),
                  Wrap(
                    alignment: WrapAlignment.center,
                    spacing: Space.sm,
                    runSpacing: Space.sm,
                    children: [
                      OutlinedButton(onPressed: onClose, child: Text(busy ? 'Cancel' : 'Close')),
                      if (onRetry != null)
                        FilledButton.icon(onPressed: onRetry, icon: const Icon(Icons.refresh), label: const Text('Reconnect')),
                    ],
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// A state in the console area before any connection: "mint is turned
/// off / Start it to use the console. / [Start]". Drawn in the console's
/// dark theme.
class ConsolePlaceholder extends StatelessWidget {
  const ConsolePlaceholder({
    super.key,
    required this.icon,
    required this.title,
    required this.message,
    this.actionLabel,
    this.onAction,
    this.busy = false,
    this.loading = false,
  });

  final IconData icon;
  final String title;
  final String message;
  final String? actionLabel;
  final VoidCallback? onAction;

  /// The action is running: its button shows progress.
  final bool busy;

  /// Nothing known yet: a skeleton of the message instead.
  final bool loading;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    if (loading) {
      return Semantics(
        label: 'Loading',
        child: SkeletonPulse(
          child: Center(
            child: Column(mainAxisSize: MainAxisSize.min, children: [
              const SkeletonBox(width: 48, height: 48, corner: Corner.md),
              const SizedBox(height: Space.lg),
              const SkeletonBox(width: 180, height: 22),
              const SizedBox(height: Space.sm),
              const SkeletonBox(width: 240, height: 16),
            ]),
          ),
        ),
      );
    }
    return SingleChildScrollView(
      padding: EdgeInsets.symmetric(horizontal: Space.gutter(context), vertical: Space.xl),
      child: ConstrainedBox(
        constraints: BoxConstraints(minHeight: MediaQuery.sizeOf(context).height / 2),
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 360),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(icon, size: 48, color: scheme.onSurfaceVariant),
                const SizedBox(height: Space.lg),
                Semantics(
                    header: true, child: Text(title, style: theme.textTheme.titleLarge, textAlign: TextAlign.center)),
                const SizedBox(height: Space.sm),
                Text(message,
                    style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
                    textAlign: TextAlign.center),
                if (actionLabel != null) ...[
                  const SizedBox(height: Space.xl),
                  FilledButton.tonal(style: tonalButtonStyle(context), 
                    onPressed: busy ? null : onAction,
                    child: busy
                        ? Semantics(
                            label: 'Working',
                            child: const SizedBox.square(dimension: 20, child: CircularProgressIndicator(strokeWidth: 2)),
                          )
                        : Text(actionLabel!),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}
