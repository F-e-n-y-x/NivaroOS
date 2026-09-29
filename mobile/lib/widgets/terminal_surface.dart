// The terminal's drawing and touch layer. xterm's own TerminalView opens
// the keyboard on every touch-down (and then jumps to the bottom), selects
// without handles and has no pinch, which made reading back through a
// shell on a phone near impossible. This widget keeps xterm's renderer,
// IME bridge and alternate-screen scroll handling and replaces the
// gestures, the way phone terminals (Termux, Blink) behave:
//
// - one-finger drag scrolls the history, with fling; in a full-screen
//   program (vim, htop, less) it sends wheel events or arrow keys instead;
// - a tap shows the keyboard (and clicks, when the program asked for the
//   mouse); nothing happens on touch-down, so a drag never pops it up;
// - long-press selects a word, with handles to widen it and a Copy menu;
//   a double tap selects a word too;
// - two fingers pinch the text size;
// - new output never pulls the view down while you are reading back; a
//   button takes you to the latest output.
//
// It builds on xterm 4.0.0's src/ classes (RenderTerminal, CustomTextEdit,
// TerminalScrollGestureHandler), so an xterm upgrade must be checked here.
// ignore_for_file: implementation_imports
import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:xterm/src/ui/custom_text_edit.dart';
import 'package:xterm/src/ui/input_map.dart';
import 'package:xterm/src/ui/render.dart';
import 'package:xterm/src/ui/scroll_handler.dart';
import 'package:xterm/xterm.dart';

import '../ui/ui.dart';

/// The text sizes pinch and the menu move between.
abstract final class TerminalFontSize {
  static const double min = 8;
  static const double max = 28;
  static const double fallback = 14;

  static double clamp(double v) => (v * 2).roundToDouble().clamp(min * 2, max * 2) / 2;
}

class TerminalSurface extends StatefulWidget {
  const TerminalSurface({
    super.key,
    required this.terminal,
    required this.controller,
    required this.scrollController,
    required this.focusNode,
    required this.theme,
    required this.fontSize,
    this.onFontSizeChanged,
    this.onFontSizeChangeEnd,
    this.onCopy,
    this.onPaste,
    this.readOnly = false,
    this.autofocus = true,
  });

  final Terminal terminal;
  final TerminalController controller;
  final ScrollController scrollController;
  final FocusNode focusNode;
  final TerminalTheme theme;
  final double fontSize;

  /// While pinching: the new size.
  final ValueChanged<double>? onFontSizeChanged;

  /// When the pinch ends, to remember the size.
  final ValueChanged<double>? onFontSizeChangeEnd;

  /// The selected text, when the user copies it (menu or Ctrl+Shift+C).
  final ValueChanged<String>? onCopy;

  /// Ctrl+Shift+V (or Ctrl+V) on a hardware keyboard.
  final VoidCallback? onPaste;

  /// No keyboard and no input (a session that has ended).
  final bool readOnly;
  final bool autofocus;

  @override
  State<TerminalSurface> createState() => TerminalSurfaceState();
}

class TerminalSurfaceState extends State<TerminalSurface> {
  final _renderKey = GlobalKey();
  final _editKey = GlobalKey<CustomTextEditState>();
  final _stackKey = GlobalKey();

  String? _composing;

  // Pinch: the pointers down now and the distance/size it started at.
  final Map<int, Offset> _pointers = {};
  double? _pinchStartDistance;
  double _pinchStartSize = TerminalFontSize.fallback;
  bool get _pinching => _pinchStartDistance != null;

  // Double tap, detected by hand so a single tap isn't delayed.
  Offset? _lastTapAt;
  DateTime? _lastTapTime;

  // Long-press selection and handle drags.
  Offset? _longPressFrom;
  bool _selecting = false;
  _Handle? _dragging;
  Offset _dragAdjust = Offset.zero;
  Timer? _edgeScroll;

  // The "latest output" button.
  bool _atBottom = true;
  bool _newOutput = false;

  RenderTerminal? get _render => _renderKey.currentContext?.findRenderObject() as RenderTerminal?;

  /// Whether the view is at the latest output.
  bool get atBottom => _atBottom;

  @override
  void initState() {
    super.initState();
    widget.scrollController.addListener(_onScroll);
    widget.terminal.addListener(_onTerminal);
    widget.controller.addListener(_onSelection);
  }

  @override
  void didUpdateWidget(TerminalSurface oldWidget) {
    super.didUpdateWidget(oldWidget);
    final old = oldWidget;
    if (old.scrollController != widget.scrollController) {
      old.scrollController.removeListener(_onScroll);
      widget.scrollController.addListener(_onScroll);
    }
    if (old.terminal != widget.terminal) {
      old.terminal.removeListener(_onTerminal);
      widget.terminal.addListener(_onTerminal);
      _newOutput = false;
    }
    if (old.controller != widget.controller) {
      old.controller.removeListener(_onSelection);
      widget.controller.addListener(_onSelection);
    }
  }

  @override
  void dispose() {
    _edgeScroll?.cancel();
    widget.scrollController.removeListener(_onScroll);
    widget.terminal.removeListener(_onTerminal);
    widget.controller.removeListener(_onSelection);
    super.dispose();
  }

  void _onScroll() {
    final c = widget.scrollController;
    if (!c.hasClients) return;
    final p = c.position;
    final bottom = p.pixels >= p.maxScrollExtent - 2;
    if (bottom != _atBottom || (bottom && _newOutput)) {
      setState(() {
        _atBottom = bottom;
        if (bottom) _newOutput = false;
      });
    } else if (widget.controller.selection != null) {
      // Handles follow the text.
      setState(() {});
    }
  }

  void _onTerminal() {
    if (!_atBottom && !_newOutput && mounted) setState(() => _newOutput = true);
  }

  void _onSelection() {
    if (mounted) setState(() {});
  }

  // --- keyboard -----------------------------------------------------------

  /// Shows the soft keyboard (focusing the terminal first).
  void requestKeyboard() => _editKey.currentState?.requestKeyboard();

  void closeKeyboard() => _editKey.currentState?.closeKeyboard();

  /// Jumps to the latest output.
  void scrollToBottom({bool animate = false}) {
    final c = widget.scrollController;
    if (!c.hasClients) return;
    final max = c.position.maxScrollExtent;
    if (animate && (max - c.position.pixels) < 4000) {
      c.animateTo(max, duration: Motion.short, curve: Motion.standard).then((_) {
        if (c.hasClients) c.jumpTo(c.position.maxScrollExtent);
      });
    } else {
      c.jumpTo(max);
    }
  }

  void _onInsert(String text) {
    final key = charToTerminalKey(text.trim());
    final consumed = key == null ? false : widget.terminal.keyInput(key);
    if (!consumed) widget.terminal.textInput(text);
    scrollToBottom();
  }

  KeyEventResult _onKeyEvent(FocusNode node, KeyEvent event) {
    if (event is KeyUpEvent) return KeyEventResult.ignored;
    final keys = HardwareKeyboard.instance;
    // Ctrl+Shift+C and Ctrl+Shift+V copy and paste, as in desktop
    // terminals; plain Ctrl+C stays the shell's interrupt.
    if (keys.isControlPressed && keys.isShiftPressed) {
      if (event.logicalKey == LogicalKeyboardKey.keyC) {
        copySelection();
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.keyV) {
        widget.onPaste?.call();
        return KeyEventResult.handled;
      }
    }
    if (keys.isShiftPressed && event.logicalKey == LogicalKeyboardKey.insert) {
      widget.onPaste?.call();
      return KeyEventResult.handled;
    }
    final key = keyToTerminalKey(event.logicalKey);
    if (key == null) return KeyEventResult.ignored;
    final handled = widget.terminal.keyInput(key, ctrl: keys.isControlPressed, alt: keys.isAltPressed, shift: keys.isShiftPressed);
    if (handled) scrollToBottom();
    return handled ? KeyEventResult.handled : KeyEventResult.ignored;
  }

  // --- selection ----------------------------------------------------------

  /// The selected text, with each line's trailing blanks trimmed.
  String? get selectedText {
    final sel = widget.controller.selection;
    if (sel == null) return null;
    final text = widget.terminal.buffer.getText(sel);
    return text.split('\n').map((l) => l.trimRight()).join('\n');
  }

  void copySelection() {
    final text = selectedText;
    if (text == null || text.isEmpty) return;
    widget.onCopy?.call(text);
    widget.controller.clearSelection();
  }

  /// Selects everything: the history and the screen.
  void selectAll() {
    final buffer = widget.terminal.buffer;
    if (buffer.height == 0) return;
    widget.controller.setSelection(
      buffer.createAnchor(0, 0),
      buffer.createAnchor(widget.terminal.viewWidth, buffer.height - 1),
      mode: SelectionMode.line,
    );
  }

  Offset? _local(Offset global) => _render?.globalToLocal(global);

  void _onTapUp(TapUpDetails d) {
    final render = _render;
    if (render == null) return;
    final now = DateTime.now();
    final double = _lastTapTime != null &&
        now.difference(_lastTapTime!) < kDoubleTapTimeout &&
        (_lastTapAt! - d.globalPosition).distance < kDoubleTapSlop;
    _lastTapTime = double ? null : now;
    _lastTapAt = d.globalPosition;
    if (double) {
      render.selectWord(render.globalToLocal(d.globalPosition));
      HapticFeedback.selectionClick();
      return;
    }
    if (widget.controller.selection != null) {
      widget.controller.clearSelection();
      return;
    }
    if (widget.readOnly) return;
    // A program that asked for the mouse (htop, vim with mouse=a) gets
    // the click.
    if (widget.terminal.mouseMode != MouseMode.none) {
      final local = render.globalToLocal(d.globalPosition);
      render.mouseEvent(TerminalMouseButton.left, TerminalMouseButtonState.down, local);
      render.mouseEvent(TerminalMouseButton.left, TerminalMouseButtonState.up, local);
    }
    requestKeyboard();
  }

  void _onLongPressStart(LongPressStartDetails d) {
    final local = _local(d.globalPosition);
    if (local == null || _pinching) return;
    HapticFeedback.selectionClick();
    _longPressFrom = local;
    _selecting = true;
    _render!.selectWord(local);
  }

  void _onLongPressMove(LongPressMoveUpdateDetails d) {
    final from = _longPressFrom;
    final local = _local(d.globalPosition);
    if (from == null || local == null) return;
    _render!.selectWord(from, local);
    _edgeScrollFor(d.globalPosition, () {
      final l = _local(d.globalPosition);
      if (l != null && _longPressFrom != null) _render?.selectWord(_longPressFrom!, l);
    });
  }

  void _onLongPressEnd(LongPressEndDetails d) {
    _stopEdgeScroll();
    setState(() {
      _selecting = false;
      _longPressFrom = null;
    });
  }

  // Mouse: press and drag selects characters, as on a desktop.
  Offset? _mouseFrom;
  void _onMouseDragStart(DragStartDetails d) {
    _mouseFrom = _local(d.globalPosition);
    if (_mouseFrom != null) _render!.selectCharacters(_mouseFrom!);
  }

  void _onMouseDragUpdate(DragUpdateDetails d) {
    final l = _local(d.globalPosition);
    if (_mouseFrom != null && l != null) _render!.selectCharacters(_mouseFrom!, l);
  }

  /// Scrolls while a finger holds a selection past the top or bottom edge.
  void _edgeScrollFor(Offset global, VoidCallback reselect) {
    final render = _render;
    final c = widget.scrollController;
    if (render == null || !c.hasClients) return;
    final y = render.globalToLocal(global).dy;
    final line = render.lineHeight;
    final double step = y < line ? -line : (y > render.size.height - line ? line : 0);
    if (step == 0) {
      _stopEdgeScroll();
      return;
    }
    _edgeScroll ??= Timer.periodic(const Duration(milliseconds: 60), (_) {
      if (!c.hasClients) return;
      final p = c.position;
      final to = (p.pixels + step).clamp(0.0, p.maxScrollExtent);
      if (to == p.pixels) return;
      c.jumpTo(to);
      reselect();
    });
  }

  void _stopEdgeScroll() {
    _edgeScroll?.cancel();
    _edgeScroll = null;
  }

  // Handles. Positions are in the stack's coordinates.
  Offset? _toStack(Offset renderLocal) {
    final render = _render;
    final stack = _stackKey.currentContext?.findRenderObject() as RenderBox?;
    if (render == null || stack == null || !render.attached || !stack.attached) return null;
    return stack.globalToLocal(render.localToGlobal(renderLocal));
  }

  void _onHandleStart(_Handle h, DragStartDetails d) {
    final sel = widget.controller.selection?.normalized;
    final render = _render;
    if (sel == null || render == null) return;
    final cell = h == _Handle.start ? sel.begin : sel.end;
    // Where the finger is relative to the middle of the handle's line, so
    // the selection doesn't jump under the finger.
    final mid = render.localToGlobal(render.getOffset(cell) + Offset(0, render.lineHeight / 2));
    _dragAdjust = mid - d.globalPosition;
    setState(() => _dragging = h);
  }

  void _onHandleUpdate(_Handle h, DragUpdateDetails d) {
    void apply() {
      final render = _render;
      final sel = widget.controller.selection?.normalized;
      if (render == null || sel == null) return;
      final local = render.globalToLocal(d.globalPosition + _dragAdjust);
      final cell = _boundaryAt(render, local);
      final buffer = widget.terminal.buffer;
      var begin = h == _Handle.start ? cell : sel.begin;
      var end = h == _Handle.end ? cell : sel.end;
      // Keep at least one character, and the handles in order.
      if (!begin.isBefore(end)) {
        if (h == _Handle.start) {
          begin = CellOffset(math.max(0, end.x - 1), end.y);
        } else {
          end = CellOffset(math.min(widget.terminal.viewWidth, begin.x + 1), begin.y);
        }
      }
      widget.controller.setSelection(buffer.createAnchorFromOffset(begin), buffer.createAnchorFromOffset(end), mode: SelectionMode.line);
    }

    apply();
    _edgeScrollFor(d.globalPosition + _dragAdjust, apply);
  }

  void _onHandleEnd() {
    _stopEdgeScroll();
    setState(() => _dragging = null);
  }

  /// The cell boundary nearest [local]: a handle sits between characters.
  CellOffset _boundaryAt(RenderTerminal render, Offset local) {
    final base = render.getCellOffset(local);
    final left = render.getOffset(CellOffset(0, base.y)).dx;
    final x = ((local.dx - left) / render.cellSize.width).round().clamp(0, widget.terminal.viewWidth);
    return CellOffset(x, base.y);
  }

  // --- pinch --------------------------------------------------------------

  void _pointerDown(PointerDownEvent e) {
    if (e.kind != PointerDeviceKind.touch) return;
    _pointers[e.pointer] = e.position;
    if (_pointers.length == 2 && widget.onFontSizeChanged != null) {
      final p = _pointers.values.toList();
      setState(() {
        _pinchStartDistance = math.max(1, (p[0] - p[1]).distance);
        _pinchStartSize = widget.fontSize;
        _stopEdgeScroll();
      });
    }
  }

  void _pointerMove(PointerMoveEvent e) {
    if (!_pointers.containsKey(e.pointer)) return;
    _pointers[e.pointer] = e.position;
    final start = _pinchStartDistance;
    if (start == null || _pointers.length < 2) return;
    final p = _pointers.values.take(2).toList();
    final size = TerminalFontSize.clamp(_pinchStartSize * (p[0] - p[1]).distance / start);
    if (size != widget.fontSize) widget.onFontSizeChanged?.call(size);
  }

  void _pointerUp(PointerEvent e) {
    if (_pointers.remove(e.pointer) == null) return;
    if (_pinching && _pointers.length < 2) {
      setState(() => _pinchStartDistance = null);
      widget.onFontSizeChangeEnd?.call(widget.fontSize);
    }
  }

  // --- build --------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    final terminal = widget.terminal;
    Widget view = Scrollable(
      controller: widget.scrollController,
      // Two fingers are pinching: the view holds still.
      physics: _pinching ? const NeverScrollableScrollPhysics() : const ClampingScrollPhysics(),
      viewportBuilder: (context, offset) => _TerminalRender(
        key: _renderKey,
        terminal: terminal,
        controller: widget.controller,
        offset: offset,
        textStyle: TerminalStyle(fontSize: widget.fontSize, fontFamily: 'monospace'),
        theme: widget.theme,
        focusNode: widget.focusNode,
        composingText: _composing,
        onEditableRect: (rect, caret) => _editKey.currentState?.setEditableRect(rect, caret),
      ),
    );

    // Full-screen programs: a drag becomes wheel events or arrow keys.
    view = TerminalScrollGestureHandler(
      terminal: terminal,
      simulateScroll: true,
      getCellOffset: (o) => _render!.getCellOffset(o),
      getLineHeight: () => _render!.lineHeight,
      child: view,
    );

    view = CustomTextEdit(
      key: _editKey,
      focusNode: widget.focusNode,
      autofocus: widget.autofocus && !widget.readOnly,
      inputType: TextInputType.visiblePassword,
      keyboardAppearance: Brightness.dark,
      deleteDetection: true,
      readOnly: widget.readOnly,
      onInsert: _onInsert,
      onDelete: () {
        scrollToBottom();
        terminal.keyInput(TerminalKey.backspace);
      },
      onComposing: (t) => setState(() => _composing = t),
      onAction: (action) {
        scrollToBottom();
        if (action == TextInputAction.done) terminal.keyInput(TerminalKey.enter);
      },
      onKeyEvent: _onKeyEvent,
      child: view,
    );

    view = RawGestureDetector(
      behavior: HitTestBehavior.opaque,
      gestures: {
        TapGestureRecognizer: GestureRecognizerFactoryWithHandlers<TapGestureRecognizer>(
          () => TapGestureRecognizer(debugOwner: this),
          (r) => r.onTapUp = _onTapUp,
        ),
        LongPressGestureRecognizer: GestureRecognizerFactoryWithHandlers<LongPressGestureRecognizer>(
          () => LongPressGestureRecognizer(debugOwner: this, supportedDevices: const {PointerDeviceKind.touch, PointerDeviceKind.stylus}),
          (r) => r
            ..onLongPressStart = _onLongPressStart
            ..onLongPressMoveUpdate = _onLongPressMove
            ..onLongPressEnd = _onLongPressEnd,
        ),
        PanGestureRecognizer: GestureRecognizerFactoryWithHandlers<PanGestureRecognizer>(
          () => PanGestureRecognizer(debugOwner: this, supportedDevices: const {PointerDeviceKind.mouse}),
          (r) => r
            ..dragStartBehavior = DragStartBehavior.down
            ..onStart = _onMouseDragStart
            ..onUpdate = _onMouseDragUpdate,
        ),
      },
      child: MouseRegion(cursor: SystemMouseCursors.text, child: view),
    );

    final selection = widget.controller.selection;
    return Listener(
      onPointerDown: _pointerDown,
      onPointerMove: _pointerMove,
      onPointerUp: _pointerUp,
      onPointerCancel: _pointerUp,
      child: Stack(
        key: _stackKey,
        children: [
          Positioned.fill(
            child: ColoredBox(
              color: widget.theme.background,
              child: Padding(padding: const EdgeInsets.fromLTRB(Space.xs, Space.xs, Space.xs, 0), child: view),
            ),
          ),
          if (selection != null) ..._selectionOverlay(context, selection),
          if (!_atBottom)
            Positioned(
              right: Space.md,
              bottom: Space.md,
              child: _LatestButton(newOutput: _newOutput, onPressed: () => scrollToBottom(animate: true)),
            ),
        ],
      ),
    );
  }

  List<Widget> _selectionOverlay(BuildContext context, BufferRange selection) {
    final render = _render;
    if (render == null || !render.hasSize) return const [];
    final sel = selection.normalized;
    final line = render.lineHeight;
    final controls = materialTextSelectionHandleControls;
    final handleSize = controls.getHandleSize(line);
    final out = <Widget>[];
    Offset? startPoint;
    Offset? endPoint;
    for (final h in _Handle.values) {
      final cell = h == _Handle.start ? sel.begin : sel.end;
      final bottom = _toStack(render.getOffset(cell) + Offset(0, line));
      if (bottom == null) continue;
      if (h == _Handle.start) {
        startPoint = bottom;
      } else {
        endPoint = bottom;
      }
      // Off screen (scrolled away): no handle.
      final viewTop = _toStack(Offset.zero)?.dy;
      if (viewTop == null) continue;
      if (bottom.dy < viewTop || bottom.dy > viewTop + render.size.height + line) continue;
      final type = h == _Handle.start ? TextSelectionHandleType.left : TextSelectionHandleType.right;
      final anchor = controls.getHandleAnchor(type, line);
      var topLeft = bottom - anchor;
      // Kept on screen at the edges (a selection from column 0 would put
      // the start handle off the left side).
      final width = (_stackKey.currentContext?.findRenderObject() as RenderBox?)?.size.width ?? double.infinity;
      topLeft = Offset(topLeft.dx.clamp(0, math.max(0, width - handleSize.width)).toDouble(), topLeft.dy);
      // A 48dp target round the 22dp handle.
      const target = 48.0;
      final pad = Offset((target - handleSize.width) / 2, (target - handleSize.height) / 2);
      out.add(Positioned(
        left: topLeft.dx - pad.dx,
        top: topLeft.dy - pad.dy,
        child: Semantics(
          label: h == _Handle.start ? 'Selection start' : 'Selection end',
          child: GestureDetector(
            behavior: HitTestBehavior.opaque,
            dragStartBehavior: DragStartBehavior.down,
            onPanStart: (d) => _onHandleStart(h, d),
            onPanUpdate: (d) => _onHandleUpdate(h, d),
            onPanEnd: (_) => _onHandleEnd(),
            onPanCancel: _onHandleEnd,
            child: SizedBox.square(
              dimension: target,
              child: Center(child: controls.buildHandle(context, type, line)),
            ),
          ),
        ),
      ));
    }
    if (!_selecting && _dragging == null && startPoint != null && endPoint != null) {
      final stack = _stackKey.currentContext?.findRenderObject() as RenderBox?;
      final height = stack?.size.height ?? 0;
      final above = Offset((startPoint.dx + endPoint.dx) / 2, (startPoint.dy - line).clamp(0, height));
      final below = Offset(above.dx, (endPoint.dy + Space.xl).clamp(0, height));
      out.add(Positioned.fill(
        child: CustomSingleChildLayout(
          delegate: _ToolbarLayout(above: above, below: below),
          child: _SelectionToolbar(onCopy: copySelection, onSelectAll: selectAll),
        ),
      ));
    }
    return out;
  }
}

enum _Handle { start, end }

/// Puts the selection menu centred over [above], or under [below] when
/// there is no room above, inside the terminal.
class _ToolbarLayout extends SingleChildLayoutDelegate {
  const _ToolbarLayout({required this.above, required this.below});

  final Offset above;
  final Offset below;

  @override
  BoxConstraints getConstraintsForChild(BoxConstraints constraints) => constraints.loosen();

  @override
  Offset getPositionForChild(Size size, Size child) {
    const gap = Space.sm;
    final x = (above.dx - child.width / 2).clamp(gap, math.max(gap, size.width - child.width - gap)).toDouble();
    final top = above.dy - child.height - gap;
    final y = top >= gap ? top : math.min(below.dy + gap, size.height - child.height - gap);
    return Offset(x, y);
  }

  @override
  bool shouldRelayout(_ToolbarLayout old) => old.above != above || old.below != below;
}

/// Copy and Select all, drawn in the app's style (a menu surface with the
/// style's corners and, on bordered styles, its hairline).
class _SelectionToolbar extends StatelessWidget {
  const _SelectionToolbar({required this.onCopy, required this.onSelectAll});

  final VoidCallback onCopy;
  final VoidCallback onSelectAll;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final tokens = DesignTokens.of(context);
    Widget item(String label, IconData icon, VoidCallback onTap) => TextButton.icon(
          onPressed: onTap,
          icon: Icon(icon, size: 18),
          label: Text(label),
          style: TextButton.styleFrom(
            foregroundColor: scheme.onSurface,
            minimumSize: const Size(48, 48),
            padding: const EdgeInsets.symmetric(horizontal: Space.md),
            shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(tokens.radii.sm)),
          ),
        );
    return Material(
      color: scheme.surfaceContainerHigh,
      elevation: 6,
      shadowColor: Colors.black,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(tokens.radii.md),
        side: BorderSide(color: scheme.outlineVariant),
      ),
      clipBehavior: Clip.antiAlias,
      child: Padding(
        padding: const EdgeInsets.all(Space.xs),
        child: Row(mainAxisSize: MainAxisSize.min, children: [
          item('Copy', Icons.copy_outlined, onCopy),
          item('Select all', Icons.select_all_outlined, onSelectAll),
        ]),
      ),
    );
  }
}

/// Back to the latest output, over the terminal's bottom right.
class _LatestButton extends StatelessWidget {
  const _LatestButton({required this.newOutput, required this.onPressed});

  final bool newOutput;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final radius = DesignTokens.of(context).radii.xl;
    return Semantics(
      button: true,
      label: newOutput ? 'New output. Scroll to the latest output' : 'Scroll to the latest output',
      excludeSemantics: true,
      child: Material(
        color: newOutput ? scheme.primaryContainer : scheme.surfaceContainerHighest,
        elevation: 3,
        shadowColor: Colors.black,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(radius), side: BorderSide(color: scheme.outlineVariant)),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onPressed,
          child: ConstrainedBox(
            constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
            child: Padding(
              padding: EdgeInsets.symmetric(horizontal: newOutput ? Space.lg : Space.md),
              child: Row(mainAxisSize: MainAxisSize.min, children: [
                Icon(Icons.arrow_downward, size: 20, color: newOutput ? scheme.onPrimaryContainer : scheme.onSurface),
                if (newOutput) ...[
                  const SizedBox(width: Space.sm),
                  Text('New output', style: theme.textTheme.labelLarge?.copyWith(color: scheme.onPrimaryContainer)),
                ],
              ]),
            ),
          ),
        ),
      ),
    );
  }
}

/// xterm's renderer, with no padding of its own (the surface pads it) and
/// no text scaling (the terminal has its own text size; the phone's scale
/// would make a 40-column shell unusable).
class _TerminalRender extends LeafRenderObjectWidget {
  const _TerminalRender({
    super.key,
    required this.terminal,
    required this.controller,
    required this.offset,
    required this.textStyle,
    required this.theme,
    required this.focusNode,
    required this.composingText,
    required this.onEditableRect,
  });

  final Terminal terminal;
  final TerminalController controller;
  final ViewportOffset offset;
  final TerminalStyle textStyle;
  final TerminalTheme theme;
  final FocusNode focusNode;
  final String? composingText;
  final EditableRectCallback onEditableRect;

  @override
  RenderTerminal createRenderObject(BuildContext context) => RenderTerminal(
        terminal: terminal,
        controller: controller,
        offset: offset,
        padding: EdgeInsets.zero,
        autoResize: true,
        textStyle: textStyle,
        textScaler: TextScaler.noScaling,
        theme: theme,
        focusNode: focusNode,
        cursorType: TerminalCursorType.block,
        alwaysShowCursor: false,
        onEditableRect: onEditableRect,
        composingText: composingText,
      );

  @override
  void updateRenderObject(BuildContext context, RenderTerminal r) {
    r
      ..terminal = terminal
      ..controller = controller
      ..offset = offset
      ..textStyle = textStyle
      ..theme = theme
      ..focusNode = focusNode
      ..onEditableRect = onEditableRect
      ..composingText = composingText;
  }
}
