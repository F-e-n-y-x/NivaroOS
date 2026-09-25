import 'dart:async';
import 'dart:ui' as ui;

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../services/rfb_client.dart';
import '../ui/ui.dart';

// The remote console UI shared by the VM console and the host desktop:
// the framebuffer view with touch and trackpad input, the frame around it
// (app bar, special keys, mouse buttons, full screen), and the clipboard
// sheet with its history. The two screens only add their own menu items
// and the state to show before a connection makes sense.

/// How touches on the remote screen become pointer input.
enum RfbInputMode {
  /// Like a laptop trackpad: drag moves the pointer, tap clicks where it is.
  trackpad,

  /// Tap where you want to click; drag pans a zoomed screen.
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

/// The remote screen, fitted to the space it gets, with pinch zoom and
/// touch or trackpad pointer input.
class RfbView extends StatefulWidget {
  const RfbView({super.key, required this.client, required this.inputMode, this.dragLocked = false, this.label});

  final RfbClient client;
  final RfbInputMode inputMode;

  /// Trackpad mode: the left button is held down while the pointer moves.
  final bool dragLocked;

  /// What TalkBack calls the screen ("Screen of mint").
  final String? label;

  @override
  State<RfbView> createState() => RfbViewState();
}

class RfbViewState extends State<RfbView> {
  final _transform = TransformationController();
  final Map<int, Offset> _pointers = {};
  double _cursorX = 0;
  double _cursorY = 0;
  Offset? _downAt;
  DateTime? _downTime;
  int _maxPointers = 0;
  bool _moved = false;
  double _scrollAccumulator = 0;
  Timer? _longPress;

  RfbClient get _c => widget.client;
  int get _w => _c.width > 0 ? _c.width : 1280;
  int get _h => _c.height > 0 ? _c.height : 720;

  static const _trackpadSpeed = 1.6;

  @override
  void dispose() {
    _longPress?.cancel();
    _transform.dispose();
    super.dispose();
  }

  void resetZoom() => _transform.value = Matrix4.identity();

  int get _held => widget.dragLocked ? 1 : 0;

  /// Presses and releases [button] (1 left, 2 middle, 4 right) where the
  /// pointer is. Two taps in quick succession arrive as two clicks, which
  /// the remote system reads as a double-click.
  void click(int button) {
    final x = _cursorX.round(), y = _cursorY.round();
    _c.sendPointer(x, y, button | _held);
    _c.sendPointer(x, y, _held);
  }

  void scroll({required bool up}) {
    final x = _cursorX.round(), y = _cursorY.round();
    _c.sendPointer(x, y, (up ? 8 : 16) | _held);
    _c.sendPointer(x, y, _held);
  }

  /// Presses or releases the left button where the pointer is.
  void setDrag(bool down) => _c.sendPointer(_cursorX.round(), _cursorY.round(), down ? 1 : 0);

  Rect _screenRect(Size box) {
    final aspect = _w / _h;
    final boxAspect = box.width / (box.height > 0 ? box.height : 1);
    if (boxAspect > aspect) {
      final w = box.height * aspect;
      return Rect.fromLTWH((box.width - w) / 2, 0, w, box.height);
    }
    final h = box.width / aspect;
    return Rect.fromLTWH(0, (box.height - h) / 2, box.width, h);
  }

  bool _moveToTouch(Offset local, Size box) {
    final scene = _transform.toScene(local);
    final rect = _screenRect(box);
    if (!rect.contains(scene)) return false;
    _cursorX = ((scene.dx - rect.left) / rect.width * _w).clamp(0, _w - 1).toDouble();
    _cursorY = ((scene.dy - rect.top) / rect.height * _h).clamp(0, _h - 1).toDouble();
    return true;
  }

  void _down(PointerDownEvent e, Size box) {
    _pointers[e.pointer] = e.localPosition;
    if (_pointers.length == 1) {
      _downAt = e.localPosition;
      _downTime = DateTime.now();
      _maxPointers = 1;
      _moved = false;
      _scrollAccumulator = 0;
      if (widget.inputMode == RfbInputMode.touch) {
        _longPress = Timer(const Duration(milliseconds: 550), () {
          if (_moved || _pointers.length != 1) return;
          if (_moveToTouch(e.localPosition, box)) {
            HapticFeedback.mediumImpact();
            click(4);
            _downAt = null; // consumed
          }
        });
      }
    } else {
      _maxPointers = _pointers.length > _maxPointers ? _pointers.length : _maxPointers;
      _longPress?.cancel();
    }
  }

  void _move(PointerMoveEvent e, Size box) {
    _pointers[e.pointer] = e.localPosition;
    final start = _downAt;
    if (start != null && (e.localPosition - start).distance > 12) {
      _moved = true;
      _longPress?.cancel();
    }
    if (widget.inputMode != RfbInputMode.trackpad) return;
    if (_pointers.length == 1) {
      final rect = _screenRect(box);
      final zoom = _transform.value.getMaxScaleOnAxis();
      final perPixel = (_w / (rect.width > 0 ? rect.width : 1)) / (zoom > 0 ? zoom : 1);
      _cursorX = (_cursorX + e.delta.dx * perPixel * _trackpadSpeed).clamp(0, _w - 1).toDouble();
      _cursorY = (_cursorY + e.delta.dy * perPixel * _trackpadSpeed).clamp(0, _h - 1).toDouble();
      _c.sendPointer(_cursorX.round(), _cursorY.round(), _held);
    } else if (_pointers.length == 2) {
      _scrollAccumulator += e.delta.dy / 2;
      if (_scrollAccumulator.abs() >= 24) {
        scroll(up: _scrollAccumulator > 0);
        _scrollAccumulator = 0;
      }
    }
  }

  void _up(PointerUpEvent e, Size box) {
    _pointers.remove(e.pointer);
    if (_pointers.isNotEmpty) return;
    _longPress?.cancel();
    final start = _downAt, time = _downTime;
    _downAt = null;
    if (start == null || time == null || _moved) return;
    if (DateTime.now().difference(time) > const Duration(milliseconds: 350)) return;
    if (_maxPointers >= 2) {
      click(4); // two-finger tap: right click
      return;
    }
    if (widget.inputMode == RfbInputMode.touch && !_moveToTouch(e.localPosition, box)) return;
    click(1);
  }

  void _cancel(PointerCancelEvent e) {
    _pointers.remove(e.pointer);
    _longPress?.cancel();
    if (_pointers.isEmpty) _downAt = null;
  }

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(builder: (context, constraints) {
      final box = constraints.biggest;
      return Semantics(
        label: widget.label,
        hint: widget.inputMode == RfbInputMode.trackpad
            ? 'Drag to move the pointer, tap to click, tap with two fingers to right-click'
            : 'Tap to click, hold to right-click, pinch to zoom',
        child: Listener(
          behavior: HitTestBehavior.opaque,
          onPointerDown: (e) => _down(e, box),
          onPointerMove: (e) => _move(e, box),
          onPointerUp: (e) => _up(e, box),
          onPointerCancel: _cancel,
          child: InteractiveViewer(
            transformationController: _transform,
            minScale: 1,
            maxScale: 6,
            panEnabled: widget.inputMode == RfbInputMode.touch,
            child: SizedBox.fromSize(
              size: box,
              child: ValueListenableBuilder<ui.Image?>(
                valueListenable: _c.frame,
                builder: (context, image, _) => image == null
                    ? const SizedBox.expand()
                    : RawImage(image: image, fit: BoxFit.contain, filterQuality: FilterQuality.medium),
              ),
            ),
          ),
        ),
      );
    });
  }
}

// ---------------------------------------------------------------------------
// The console frame
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

/// The page around a remote screen: app bar with keyboard, clipboard and
/// menu; the screen; and, below it, mouse buttons and special keys. It is
/// always drawn in the dark theme, like a video player, so the remote
/// picture keeps its own colours whatever the app's theme.
///
/// The screen that owns it connects [client]; until it wants a connection
/// (a stopped VM, a desktop that isn't installed) it passes [placeholder].
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
  });

  final RfbClient client;
  final String title;

  /// Who the clipboard sheet sends to ("mint", "the server").
  final String clipboardTarget;
  final String? clipboardHint;

  /// The screen's own items, shown first in the menu.
  final List<ConsoleMenuItem> menuItems;

  /// Shown instead of the console while there is nothing to connect to.
  final Widget? placeholder;

  /// The app bar's status line while [placeholder] shows ("Off").
  final String? placeholderStatus;
  final String? screenLabel;
  final RemoteClipboardHistory? history;

  @override
  State<RemoteConsoleFrame> createState() => RemoteConsoleFrameState();
}

class RemoteConsoleFrameState extends State<RemoteConsoleFrame> {
  final _viewKey = GlobalKey<RfbViewState>();
  final _keyboardFocus = FocusNode(debugLabel: 'remote keyboard');
  final _keyboardText = TextEditingController(text: _sentinel);
  final _keyScroll = ScrollController();
  StreamSubscription<String>? _clipSub;

  RfbInputMode _inputMode = RfbInputMode.trackpad;
  bool _showKeys = false;
  bool _fullscreen = false;
  bool _landscapeLocked = false;
  bool _dragLocked = false;
  final Set<int> _latched = {};

  // The hidden field always holds this, so a backspace on an "empty"
  // field still arrives as a change.
  static const _sentinel = '​​';

  RfbClient get _client => widget.client;
  RemoteClipboardHistory get _history => widget.history ?? RemoteClipboardHistory.instance;

  @override
  void initState() {
    super.initState();
    _client.status.addListener(_statusChanged);
    _clipSub = _client.remoteClipboard.listen(_remoteCopied);
    _keyboardFocus.addListener(() => setState(() {}));
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
  }

  @override
  void dispose() {
    _client.status.removeListener(_statusChanged);
    _clipSub?.cancel();
    _keyboardFocus.dispose();
    _keyboardText.dispose();
    _keyScroll.dispose();
    if (_fullscreen || _landscapeLocked) {
      SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
      SystemChrome.setPreferredOrientations(const []);
    }
    super.dispose();
  }

  void _statusChanged() {
    if (!mounted) return;
    if (!_client.isConnected) {
      _latched.clear();
      _dragLocked = false;
    }
    setState(() {});
  }

  void _remoteCopied(String text) {
    if (!mounted || !_history.record(text, ClipDirection.copied, widget.clipboardTarget)) return;
    final who = widget.clipboardTarget == RemoteClipboardHistory.hostTarget ? 'The server' : widget.clipboardTarget;
    ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(
      content: Text('$who copied text'),
      action: SnackBarAction(label: 'Copy here', onPressed: () => Clipboard.setData(ClipboardData(text: text))),
    ));
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
      _keyboardFocus.unfocus();
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

  KeyEventResult _onHardwareKey(FocusNode node, KeyEvent event) {
    final keysym = Keysym.hardware[event.logicalKey];
    if (keysym == null) return KeyEventResult.ignored;
    if (event is KeyDownEvent || event is KeyRepeatEvent) {
      _client.sendKey(keysym, true);
    } else if (event is KeyUpEvent) {
      _client.sendKey(keysym, false);
      _releaseLatches();
    }
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

  // --- modes ---

  void _setFullscreen(bool on) {
    setState(() => _fullscreen = on);
    SystemChrome.setEnabledSystemUIMode(on ? SystemUiMode.immersiveSticky : SystemUiMode.edgeToEdge);
  }

  void _setLandscape(bool on) {
    setState(() => _landscapeLocked = on);
    SystemChrome.setPreferredOrientations(
        on ? const [DeviceOrientation.landscapeLeft, DeviceOrientation.landscapeRight] : const []);
  }

  void _toggleDrag() {
    HapticFeedback.selectionClick();
    setState(() => _dragLocked = !_dragLocked);
    _viewKey.currentState?.setDrag(_dragLocked);
  }

  Future<void> _openClipboard(BuildContext context) => RemoteClipboardSheet.show(context,
      client: _client, target: widget.clipboardTarget, hint: widget.clipboardHint, history: widget.history);

  Future<void> _onBack(bool didPop, Object? result) async {
    if (didPop) return;
    if (_fullscreen) {
      _setFullscreen(false);
      return;
    }
    final close = await ConfirmDialog.confirm(
      context,
      title: 'Close the console?',
      message: widget.clipboardTarget == RemoteClipboardHistory.hostTarget
          ? 'The server keeps running. You can open it again from More.'
          : '${widget.clipboardTarget} keeps running. You can open the console again from the VM list.',
      confirmLabel: 'Close',
    );
    if (close && mounted) Navigator.of(context).pop();
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

  @override
  Widget build(BuildContext context) {
    final dark = AppTheme.dark();
    return Theme(
      data: dark,
      child: Builder(builder: (context) {
        final connected = _client.isConnected && widget.placeholder == null;
        return PopScope(
          canPop: !connected && !_fullscreen,
          onPopInvokedWithResult: _onBack,
          child: Scaffold(
            backgroundColor: dark.colorScheme.surfaceContainerLowest,
            appBar: _fullscreen ? null : _appBar(context, connected),
            body: Column(
              children: [
                Expanded(child: _screen(context, connected)),
                if (connected && !_fullscreen) _controls(context),
              ],
            ),
          ),
        );
      }),
    );
  }

  PreferredSizeWidget _appBar(BuildContext context, bool connected) {
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
      actions: [
        if (connected) ...[
          IconButton(
            tooltip: _keyboardFocus.hasFocus ? 'Hide keyboard' : 'Show keyboard',
            isSelected: _keyboardFocus.hasFocus,
            icon: const Icon(Icons.keyboard_outlined),
            selectedIcon: const Icon(Icons.keyboard_hide_outlined),
            onPressed: _toggleKeyboard,
          ),
          IconButton(
            tooltip: 'Clipboard',
            icon: const Icon(Icons.content_paste_outlined),
            onPressed: () => _openClipboard(context),
          ),
        ],
        _menu(context, connected),
      ],
    );
  }

  Widget _menu(BuildContext context, bool connected) {
    return MenuAnchor(
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
        if (widget.menuItems.isNotEmpty) const Divider(),
        if (connected) ...[
          MenuItemButton(
            leadingIcon: Icon(_inputMode == RfbInputMode.trackpad ? Icons.touch_app_outlined : Icons.mouse_outlined),
            onPressed: () => setState(() {
              _inputMode = _inputMode == RfbInputMode.trackpad ? RfbInputMode.touch : RfbInputMode.trackpad;
              if (_dragLocked) _toggleDrag();
            }),
            child: Text(_inputMode == RfbInputMode.trackpad ? 'Use touch mode' : 'Use trackpad mode'),
          ),
          CheckboxMenuButton(
            value: _showKeys,
            onChanged: (v) => setState(() => _showKeys = v ?? false),
            child: const Text('Special keys'),
          ),
          MenuItemButton(
            leadingIcon: const Icon(Icons.zoom_out_map_outlined),
            onPressed: () => _viewKey.currentState?.resetZoom(),
            child: const Text('Fit to screen'),
          ),
          MenuItemButton(
            leadingIcon: const Icon(Icons.fullscreen),
            onPressed: () => _setFullscreen(true),
            child: const Text('Full screen'),
          ),
        ],
        CheckboxMenuButton(
          value: _landscapeLocked,
          onChanged: (v) => _setLandscape(v ?? false),
          child: const Text('Stay in landscape'),
        ),
        if (widget.placeholder == null)
          MenuItemButton(
            leadingIcon: const Icon(Icons.refresh),
            onPressed: _client.status.value == RfbStatus.connecting
                ? null
                : () {
                    _client.close();
                    _client.connect();
                  },
            child: const Text('Reconnect'),
          ),
      ],
    );
  }

  Widget _screen(BuildContext context, bool connected) {
    final scheme = Theme.of(context).colorScheme;
    final placeholder = widget.placeholder;
    if (placeholder != null) return placeholder;
    final status = _client.status.value;
    final hasFrame = _client.frame.value != null;
    return Stack(
      fit: StackFit.expand,
      children: [
        RfbView(
          key: _viewKey,
          client: _client,
          inputMode: _inputMode,
          dragLocked: _dragLocked,
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
              child: Focus(
                onKeyEvent: _onHardwareKey,
                skipTraversal: true,
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
        ),
        if (status == RfbStatus.connecting)
          const Align(alignment: Alignment.topCenter, child: LinearProgressIndicator()),
        if (!connected)
          ColoredBox(
            color: hasFrame ? scheme.scrim.withValues(alpha: 0.7) : scheme.surfaceContainerLowest,
            child: _ConsoleMessage(
              status: status,
              title: widget.title,
              error: _client.error.value,
              onRetry: () => _client.connect(),
            ),
          ),
        if (_fullscreen)
          Positioned(
            top: MediaQuery.paddingOf(context).top + Space.sm,
            right: Space.sm,
            child: FloatingActionButton.small(
              heroTag: null,
              tooltip: 'Exit full screen',
              backgroundColor: scheme.secondaryContainer.withValues(alpha: 0.85),
              foregroundColor: scheme.onSecondaryContainer,
              elevation: 0,
              onPressed: () => _setFullscreen(false),
              child: const Icon(Icons.fullscreen_exit),
            ),
          ),
      ],
    );
  }

  Widget _controls(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final gutter = Space.gutter(context);
    return Material(
      color: scheme.surfaceContainer,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: Space.xs),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (_showKeys) _keyBar(context),
              if (_inputMode == RfbInputMode.trackpad) ...[
                // A caption, so "Left", "Right", the arrows and the hand
                // read as one set of mouse controls.
                Padding(
                  padding: EdgeInsets.fromLTRB(gutter, Space.xs, gutter, 0),
                  child: Align(
                    alignment: AlignmentDirectional.centerStart,
                    child: Text('Mouse', style: Theme.of(context).textTheme.labelSmall?.copyWith(color: scheme.onSurfaceVariant)),
                  ),
                ),
                Padding(
                  padding: EdgeInsets.symmetric(horizontal: gutter),
                  child: Row(
                    children: [
                      Expanded(
                        child: OutlinedButton(
                          onPressed: () => _viewKey.currentState?.click(1),
                          style: OutlinedButton.styleFrom(padding: const EdgeInsets.symmetric(horizontal: Space.md)),
                          child: const Tooltip(message: 'Left click', child: Text('Left', maxLines: 1, overflow: TextOverflow.ellipsis)),
                        ),
                      ),
                      const SizedBox(width: Space.sm),
                      Expanded(
                        child: OutlinedButton(
                          onPressed: () => _viewKey.currentState?.click(4),
                          style: OutlinedButton.styleFrom(padding: const EdgeInsets.symmetric(horizontal: Space.md)),
                          child: const Tooltip(message: 'Right click', child: Text('Right', maxLines: 1, overflow: TextOverflow.ellipsis)),
                        ),
                      ),
                      const SizedBox(width: Space.xs),
                      IconButton(
                        tooltip: 'Scroll up',
                        icon: const Icon(Icons.keyboard_arrow_up),
                        onPressed: () => _viewKey.currentState?.scroll(up: true),
                      ),
                      IconButton(
                        tooltip: 'Scroll down',
                        icon: const Icon(Icons.keyboard_arrow_down),
                        onPressed: () => _viewKey.currentState?.scroll(up: false),
                      ),
                      IconButton(
                        tooltip: _dragLocked ? 'Release the held button' : 'Hold the left button to drag',
                        isSelected: _dragLocked,
                        icon: const Icon(Icons.pan_tool_outlined),
                        selectedIcon: const Icon(Icons.pan_tool),
                        style: IconButton.styleFrom(
                          backgroundColor: _dragLocked ? scheme.secondaryContainer : null,
                          foregroundColor: _dragLocked ? scheme.onSecondaryContainer : null,
                        ),
                        onPressed: _toggleDrag,
                      ),
                    ],
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }

  Widget _keyBar(BuildContext context) {
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
    return SizedBox(
      height: 56,
      child: FadingEdges(
        controller: _keyScroll,
        child: ListView(
        controller: _keyScroll,
        scrollDirection: Axis.horizontal,
        padding: EdgeInsets.symmetric(horizontal: gutter),
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
    );
  }
}

/// Connecting, failed or lost, over the console area.
class _ConsoleMessage extends StatelessWidget {
  const _ConsoleMessage({required this.status, required this.title, required this.error, required this.onRetry});

  final RfbStatus status;
  final String title;
  final String? error;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final connecting = status == RfbStatus.connecting || status == RfbStatus.idle;
    final heading = switch (status) {
      RfbStatus.disconnected => 'Connection lost',
      RfbStatus.failed => "Couldn't connect",
      _ => 'Connecting to $title',
    };
    return SingleChildScrollView(
      padding: EdgeInsets.symmetric(horizontal: Space.gutter(context), vertical: Space.xl),
      child: ConstrainedBox(
        constraints: BoxConstraints(minHeight: MediaQuery.sizeOf(context).height / 2),
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 360),
            child: Semantics(
              liveRegion: true,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(connecting ? Icons.desktop_windows_outlined : Icons.desktop_access_disabled_outlined,
                      size: 48, color: scheme.onSurfaceVariant),
                  const SizedBox(height: Space.lg),
                  Text(heading, style: theme.textTheme.titleLarge, textAlign: TextAlign.center),
                  if (!connecting && error != null) ...[
                    const SizedBox(height: Space.sm),
                    Text(error!,
                        style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
                        textAlign: TextAlign.center),
                  ],
                  if (!connecting) ...[
                    const SizedBox(height: Space.xl),
                    FilledButton.icon(onPressed: onRetry, icon: const Icon(Icons.refresh), label: const Text('Reconnect')),
                  ],
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
              const SkeletonBox(width: 48, height: 48, radius: Corners.medium),
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
                  FilledButton.tonal(
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
