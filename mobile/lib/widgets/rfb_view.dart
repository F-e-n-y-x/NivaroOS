import 'dart:async';
import 'dart:ui' as ui;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import '../theme.dart';
import '../services/rfb_client.dart';
import '../services/vm_client.dart';

enum InputControlMode {
  trackpad,
  touch,
}

enum ConsoleDisplayMode {
  vnc,
  stream,
}

final Map<LogicalKeyboardKey, int> _keysymTable = {
  LogicalKeyboardKey.backspace: 0xFF08,
  LogicalKeyboardKey.tab: 0xFF09,
  LogicalKeyboardKey.enter: 0xFF0D,
  LogicalKeyboardKey.escape: 0xFF1B,
  LogicalKeyboardKey.delete: 0xFFFF,
  LogicalKeyboardKey.home: 0xFF50,
  LogicalKeyboardKey.end: 0xFF57,
  LogicalKeyboardKey.pageUp: 0xFF55,
  LogicalKeyboardKey.pageDown: 0xFF56,
  LogicalKeyboardKey.arrowLeft: 0xFF51,
  LogicalKeyboardKey.arrowUp: 0xFF52,
  LogicalKeyboardKey.arrowRight: 0xFF53,
  LogicalKeyboardKey.arrowDown: 0xFF54,
  LogicalKeyboardKey.shiftLeft: 0xFFE1,
  LogicalKeyboardKey.shiftRight: 0xFFE2,
  LogicalKeyboardKey.controlLeft: 0xFFE3,
  LogicalKeyboardKey.controlRight: 0xFFE4,
  LogicalKeyboardKey.altLeft: 0xFFE9,
  LogicalKeyboardKey.altRight: 0xFFEA,
  LogicalKeyboardKey.metaLeft: 0xFFEB,
  LogicalKeyboardKey.metaRight: 0xFFEC,
  LogicalKeyboardKey.space: 0x0020,
  LogicalKeyboardKey.f1: 0xFFBE,
  LogicalKeyboardKey.f2: 0xFFBF,
  LogicalKeyboardKey.f3: 0xFFC0,
  LogicalKeyboardKey.f4: 0xFFC1,
  LogicalKeyboardKey.f5: 0xFFC2,
  LogicalKeyboardKey.f6: 0xFFC3,
  LogicalKeyboardKey.f7: 0xFFC4,
  LogicalKeyboardKey.f8: 0xFFC5,
  LogicalKeyboardKey.f9: 0xFFC6,
  LogicalKeyboardKey.f10: 0xFFC7,
  LogicalKeyboardKey.f11: 0xFFC8,
  LogicalKeyboardKey.f12: 0xFFC9,
};

const int _keysymCtrlL = 0xFFE3;
const int _keysymAltL = 0xFFE9;
const int _keysymShiftL = 0xFFE1;
const int _keysymSuperL = 0xFFEB;
const int _keysymDelete = 0xFFFF;
const int _keysymEscape = 0xFF1B;
const int _keysymTab = 0xFF09;
const int _keysymEnter = 0xFF0D;
const int _keysymBackspace = 0xFF08;

class RfbView extends StatefulWidget {
  final RfbClient client;
  final VmClient vmClient;
  final String vmName;
  final ConsoleDisplayMode initialMode;
  final VoidCallback? onPower;
  final VoidCallback? onSnapshots;
  final VoidCallback? onIso;
  final VoidCallback? onHardware;
  final bool isFullscreen;
  final VoidCallback? onToggleFullscreen;
  final bool isLandscape;
  final VoidCallback? onToggleOrientation;

  const RfbView({
    super.key,
    required this.client,
    required this.vmClient,
    required this.vmName,
    this.initialMode = ConsoleDisplayMode.vnc,
    this.onPower,
    this.onSnapshots,
    this.onIso,
    this.onHardware,
    this.isFullscreen = false,
    this.onToggleFullscreen,
    this.isLandscape = false,
    this.onToggleOrientation,
  });

  @override
  State<RfbView> createState() => _RfbViewState();
}

class _RfbViewState extends State<RfbView> {
  final _keyboardFocus = FocusNode();
  final _keyboardController = TextEditingController();
  final TransformationController _transformController = TransformationController();
  String _lastText = '';

  InputControlMode _inputMode = InputControlMode.trackpad;
  late ConsoleDisplayMode _mode;
  bool _keyboardOpen = false;
  bool _showExtendedKeys = false;
  bool _showHudControls = true;
  bool _showTrackpadDock = true;
  double _trackpadSensitivity = 1.35;

  Offset _hudOffset = const Offset(16, 16);

  bool _ctrlLatched = false;
  bool _altLatched = false;
  bool _shiftLatched = false;
  bool _superLatched = false;
  bool _dragLocked = false;

  double _cursorX = 512;
  double _cursorY = 384;
  int _vmWidth = 1024;
  int _vmHeight = 768;

  Timer? _streamTimer;
  Uint8List? _currentFrameBytes;
  bool _isFetchingFrame = false;
  int _fpsCount = 0;
  int _fpsDisplay = 0;
  Timer? _fpsTimer;
  String? _streamError;

  final Map<int, Offset> _pointerPositions = {};
  DateTime? _lastTapTime;
  Offset? _firstPointerStart;
  DateTime? _firstPointerStartTime;
  double _twoFingerScrollAccumulator = 0;

  @override
  void initState() {
    super.initState();
    _mode = widget.initialMode;

    widget.client.connect().catchError((e) {
      if (mounted) setState(() => _streamError = e.toString());
    });

    if (_mode == ConsoleDisplayMode.stream) {
      _startStream();
    }

    widget.client.connected.addListener(_onVncConnected);
    widget.client.frameCount.addListener(_onNewVncFrame);

    _fpsTimer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) {
        setState(() {
          _fpsDisplay = _fpsCount;
          _fpsCount = 0;
        });
      }
    });
  }

  @override
  void dispose() {
    _stopStream();
    _fpsTimer?.cancel();
    widget.client.connected.removeListener(_onVncConnected);
    widget.client.frameCount.removeListener(_onNewVncFrame);
    _keyboardFocus.dispose();
    _keyboardController.dispose();
    _transformController.dispose();
    super.dispose();
  }

  void _onNewVncFrame() {
    _fpsCount++;
  }

  void _onVncConnected() {
    if (mounted && widget.client.width > 0 && widget.client.height > 0) {
      setState(() {
        _vmWidth = widget.client.width;
        _vmHeight = widget.client.height;
      });
    }
  }

  void _startStream() {
    _streamTimer?.cancel();
    _streamTimer = Timer.periodic(const Duration(milliseconds: 33), (_) {
      _fetchStreamFrame();
    });
    _fetchStreamFrame();
  }

  void _stopStream() {
    _streamTimer?.cancel();
  }

  Future<void> _fetchStreamFrame() async {
    if (_isFetchingFrame) return;
    _isFetchingFrame = true;
    try {
      final url = widget.vmClient.screenshotUrl(widget.vmName);
      final res = await http.get(Uri.parse(url)).timeout(const Duration(milliseconds: 600));
      if (res.statusCode == 200 && res.bodyBytes.isNotEmpty) {
        if (!mounted) return;
        setState(() {
          _currentFrameBytes = res.bodyBytes;
          _streamError = null;
          _fpsCount++;
        });
      }
    } catch (_) {
      if (mounted && _currentFrameBytes == null) {
        setState(() => _streamError = 'Connecting to VM display...');
      }
    } finally {
      _isFetchingFrame = false;
    }
  }

  void setMode(ConsoleDisplayMode mode) {
    if (_mode == mode) return;
    HapticFeedback.selectionClick();
    setState(() => _mode = mode);
    if (mode == ConsoleDisplayMode.stream) {
      _startStream();
    } else {
      _stopStream();
      widget.client.connect().catchError((_) {});
    }
  }

  void _toggleKeyboard() {
    HapticFeedback.lightImpact();
    setState(() {
      _keyboardOpen = !_keyboardOpen;
      if (_keyboardOpen) {
        _showExtendedKeys = true;
      }
    });
    if (_keyboardOpen) {
      FocusScope.of(context).requestFocus(_keyboardFocus);
    } else {
      _keyboardFocus.unfocus();
    }
  }

  void _onTextChanged(String text) {
    if (text.length > _lastText.length) {
      final added = text.substring(_lastText.length);
      for (final rune in added.runes) {
        _sendRuneKey(rune);
      }
    } else if (text.length < _lastText.length) {
      _sendSingleKey(_keysymBackspace);
    }
    _lastText = text;
  }

  void _sendRuneKey(int rune) {
    widget.client.sendKey(rune, true);
    widget.client.sendKey(rune, false);
  }

  void _sendSingleKey(int key) {
    HapticFeedback.lightImpact();
    widget.client.sendKey(key, true);
    widget.client.sendKey(key, false);
  }

  void _sendKeyCombination(List<int> keys) {
    HapticFeedback.mediumImpact();
    for (final k in keys) {
      widget.client.sendKey(k, true);
    }
    for (final k in keys.reversed) {
      widget.client.sendKey(k, false);
    }
    _clearLatches();
  }

  void _toggleLatch(String mod) {
    HapticFeedback.lightImpact();
    setState(() {
      if (mod == 'ctrl') _ctrlLatched = !_ctrlLatched;
      if (mod == 'alt') _altLatched = !_altLatched;
      if (mod == 'shift') _shiftLatched = !_shiftLatched;
      if (mod == 'super') _superLatched = !_superLatched;
    });
    if (mod == 'ctrl') widget.client.sendKey(_keysymCtrlL, _ctrlLatched);
    if (mod == 'alt') widget.client.sendKey(_keysymAltL, _altLatched);
    if (mod == 'shift') widget.client.sendKey(_keysymShiftL, _shiftLatched);
    if (mod == 'super') widget.client.sendKey(_keysymSuperL, _superLatched);
  }

  void _clearLatches() {
    if (_ctrlLatched) widget.client.sendKey(_keysymCtrlL, false);
    if (_altLatched) widget.client.sendKey(_keysymAltL, false);
    if (_shiftLatched) widget.client.sendKey(_keysymShiftL, false);
    if (_superLatched) widget.client.sendKey(_keysymSuperL, false);
    setState(() {
      _ctrlLatched = false;
      _altLatched = false;
      _shiftLatched = false;
      _superLatched = false;
    });
  }

  void _sendMouseClick(int button) {
    HapticFeedback.lightImpact();
    final x = _cursorX.round().clamp(0, _vmWidth - 1);
    final y = _cursorY.round().clamp(0, _vmHeight - 1);
    widget.client.sendPointer(x, y, button);
    Future.delayed(const Duration(milliseconds: 40), () {
      widget.client.sendPointer(x, y, _dragLocked ? 1 : 0);
    });
  }

  void _sendDoubleClick() {
    HapticFeedback.mediumImpact();
    final x = _cursorX.round().clamp(0, _vmWidth - 1);
    final y = _cursorY.round().clamp(0, _vmHeight - 1);
    widget.client.sendPointer(x, y, 1);
    Future.delayed(const Duration(milliseconds: 30), () {
      widget.client.sendPointer(x, y, 0);
      Future.delayed(const Duration(milliseconds: 50), () {
        widget.client.sendPointer(x, y, 1);
        Future.delayed(const Duration(milliseconds: 30), () {
          widget.client.sendPointer(x, y, _dragLocked ? 1 : 0);
        });
      });
    });
  }

  void _sendWheel(bool up) {
    HapticFeedback.selectionClick();
    final x = _cursorX.round().clamp(0, _vmWidth - 1);
    final y = _cursorY.round().clamp(0, _vmHeight - 1);
    widget.client.sendPointer(x, y, up ? 8 : 16);
    Future.delayed(const Duration(milliseconds: 40), () {
      widget.client.sendPointer(x, y, _dragLocked ? 1 : 0);
    });
  }

  void _toggleDragLock() {
    HapticFeedback.mediumImpact();
    setState(() => _dragLocked = !_dragLocked);
    final x = _cursorX.round().clamp(0, _vmWidth - 1);
    final y = _cursorY.round().clamp(0, _vmHeight - 1);
    widget.client.sendPointer(x, y, _dragLocked ? 1 : 0);
  }

  void _cycleSensitivity() {
    HapticFeedback.selectionClick();
    setState(() {
      if (_trackpadSensitivity < 1.4) {
        _trackpadSensitivity = 1.75;
      } else if (_trackpadSensitivity < 2.0) {
        _trackpadSensitivity = 2.4;
      } else {
        _trackpadSensitivity = 1.15;
      }
    });
  }

  void _onPointerDown(PointerDownEvent event, Size canvasSize) {
    _pointerPositions[event.pointer] = event.localPosition;
    if (_pointerPositions.length == 1) {
      _firstPointerStart = event.localPosition;
      _firstPointerStartTime = DateTime.now();
      _twoFingerScrollAccumulator = 0;
    }
  }

  void _onPointerMove(PointerMoveEvent event, Size canvasSize) {
    _pointerPositions[event.pointer] = event.localPosition;

    if (_inputMode == InputControlMode.trackpad) {
      if (_pointerPositions.length == 1) {
        final currentScale = _transformController.value.getMaxScaleOnAxis();
        final effectiveScale = currentScale > 0 ? currentScale : 1.0;
        final rect = _calculateVmDisplayRect(canvasSize);
        final scaleX = (_vmWidth / (rect.width > 0 ? rect.width : 1)) / effectiveScale;
        final scaleY = (_vmHeight / (rect.height > 0 ? rect.height : 1)) / effectiveScale;
        final avgScale = (scaleX + scaleY) / 2;

        setState(() {
          _cursorX = (_cursorX + event.delta.dx * avgScale * _trackpadSensitivity).clamp(0.0, _vmWidth - 1.0);
          _cursorY = (_cursorY + event.delta.dy * avgScale * _trackpadSensitivity).clamp(0.0, _vmHeight - 1.0);
        });

        widget.client.sendPointer(_cursorX.round(), _cursorY.round(), _dragLocked ? 1 : 0);
      } else if (_pointerPositions.length == 2) {
        _twoFingerScrollAccumulator += event.delta.dy;
        if (_twoFingerScrollAccumulator <= -20) {
          _sendWheel(false);
          _twoFingerScrollAccumulator = 0;
        } else if (_twoFingerScrollAccumulator >= 20) {
          _sendWheel(true);
          _twoFingerScrollAccumulator = 0;
        }
      }
    }
  }

  void _onPointerUp(PointerUpEvent event, Size canvasSize) {
    final pointerCount = _pointerPositions.length;
    final startPos = _firstPointerStart;
    final startTime = _firstPointerStartTime;

    if (startPos != null && startTime != null) {
      final duration = DateTime.now().difference(startTime);
      final dist = (event.localPosition - startPos).distance;

      if (_inputMode == InputControlMode.touch) {
        // Direct touch mode: tap maps accurately through zoom matrix to VM coordinates
        if (dist < 14 && duration < const Duration(milliseconds: 320)) {
          final scenePos = _transformController.toScene(event.localPosition);
          final rect = _calculateVmDisplayRect(canvasSize);
          if (rect.contains(scenePos)) {
            final x = ((scenePos.dx - rect.left) / rect.width * _vmWidth).clamp(0.0, _vmWidth - 1.0);
            final y = ((scenePos.dy - rect.top) / rect.height * _vmHeight).clamp(0.0, _vmHeight - 1.0);
            setState(() {
              _cursorX = x;
              _cursorY = y;
            });
            if (pointerCount == 2) {
              _sendMouseClick(4); // 2-finger tap = right click
            } else {
              final now = DateTime.now();
              if (_lastTapTime != null && now.difference(_lastTapTime!) < const Duration(milliseconds: 320)) {
                _sendDoubleClick();
                _lastTapTime = null;
              } else {
                _sendMouseClick(1);
                _lastTapTime = now;
              }
            }
          }
        }
      } else {
        // Trackpad mode: tap anywhere dispatches click at virtual cursor position
        if (dist < 12 && duration < const Duration(milliseconds: 280)) {
          if (pointerCount == 2) {
            _sendMouseClick(4); // Right click
          } else if (pointerCount == 1) {
            final now = DateTime.now();
            if (_lastTapTime != null && now.difference(_lastTapTime!) < const Duration(milliseconds: 320)) {
              _sendDoubleClick();
              _lastTapTime = null;
            } else {
              _sendMouseClick(1);
              _lastTapTime = now;
            }
          }
        }
      }
    }

    _pointerPositions.remove(event.pointer);
    if (_pointerPositions.isEmpty) {
      _firstPointerStart = null;
      _firstPointerStartTime = null;
      _twoFingerScrollAccumulator = 0;
    }
  }

  void _onPointerCancel(PointerCancelEvent event) {
    _pointerPositions.remove(event.pointer);
    if (_pointerPositions.isEmpty) {
      _firstPointerStart = null;
      _firstPointerStartTime = null;
      _twoFingerScrollAccumulator = 0;
    }
  }

  Rect _calculateVmDisplayRect(Size viewport) {
    final vmAspect = _vmWidth / (_vmHeight > 0 ? _vmHeight : 1);
    final viewAspect = viewport.width / (viewport.height > 0 ? viewport.height : 1);

    if (viewAspect > vmAspect) {
      final h = viewport.height;
      final w = h * vmAspect;
      final x = (viewport.width - w) / 2;
      return Rect.fromLTWH(x, 0, w, h);
    } else {
      final w = viewport.width;
      final h = w / vmAspect;
      final y = (viewport.height - h) / 2;
      return Rect.fromLTWH(0, y, w, h);
    }
  }

  Future<void> _openTextInputDialog() async {
    final ctrl = TextEditingController();
    final text = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceContainerHighest,
        title: const Text('Send Text / String to VM'),
        content: TextField(
          controller: ctrl,
          autofocus: true,
          decoration: const InputDecoration(hintText: 'Type or paste clipboard text...'),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.primary),
            onPressed: () => Navigator.pop(context, ctrl.text),
            child: const Text('Send String'),
          ),
        ],
      ),
    );
    if (text != null && text.isNotEmpty) {
      for (final rune in text.runes) {
        _sendRuneKey(rune);
        await Future.delayed(const Duration(milliseconds: 8));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final screenSize = MediaQuery.of(context).size;

    return Container(
      color: Colors.black,
      width: double.infinity,
      height: double.infinity,
      child: Stack(
        children: [
          Offstage(
            offstage: !_keyboardOpen,
            child: SizedBox(
              width: 1,
              height: 1,
              child: TextField(
                controller: _keyboardController,
                focusNode: _keyboardFocus,
                autocorrect: false,
                enableSuggestions: false,
                onChanged: _onTextChanged,
              ),
            ),
          ),

          // Edge-to-Edge Remote VM Canvas Viewport with multi-touch gestural surface
          Positioned.fill(
            child: LayoutBuilder(
              builder: (context, constraints) {
                final canvasSize = Size(constraints.maxWidth, constraints.maxHeight);

                return Listener(
                  behavior: HitTestBehavior.opaque,
                  onPointerDown: (e) => _onPointerDown(e, canvasSize),
                  onPointerMove: (e) => _onPointerMove(e, canvasSize),
                  onPointerUp: (e) => _onPointerUp(e, canvasSize),
                  onPointerCancel: _onPointerCancel,
                  child: InteractiveViewer(
                    transformationController: _transformController,
                    minScale: 1.0,
                    maxScale: 6.0,
                    panEnabled: _inputMode == InputControlMode.touch,
                    scaleEnabled: true,
                    clipBehavior: Clip.none,
                    child: Center(
                      child: FittedBox(
                        fit: BoxFit.contain,
                        child: SizedBox(
                          width: _vmWidth.toDouble(),
                          height: _vmHeight.toDouble(),
                          child: Stack(
                            fit: StackFit.expand,
                            children: [
                              if (_mode == ConsoleDisplayMode.vnc)
                                _buildVncCanvas()
                              else
                                _buildStreamCanvas(),
                            ],
                          ),
                        ),
                      ),
                    ),
                  ),
                );
              },
            ),
          ),

          // Draggable Floating Setting / HUD Pill (Directly draggable via setting icon when collapsed)
          Positioned(
            left: _hudOffset.dx,
            top: _hudOffset.dy,
            child: GestureDetector(
              onPanUpdate: (d) {
                setState(() {
                  _hudOffset = Offset(
                    (_hudOffset.dx + d.delta.dx).clamp(4.0, (screenSize.width - 240.0).clamp(4.0, double.infinity)),
                    (_hudOffset.dy + d.delta.dy).clamp(4.0, (screenSize.height - 70.0).clamp(4.0, double.infinity)),
                  );
                });
              },
              child: _showHudControls ? _buildFullHud() : _buildMiniHud(),
            ),
          ),

          // Floating Responsive Trackpad Controls Dock
          if (_inputMode == InputControlMode.trackpad && _showTrackpadDock)
            Positioned(
              left: widget.isLandscape ? 60 : 12,
              right: widget.isLandscape ? 60 : 12,
              bottom: _showExtendedKeys ? 52 : (widget.isLandscape ? 10 : 14),
              child: _buildTrackpadDock(),
            ),

          // Mini Toggle to expand Trackpad dock if collapsed
          if (_inputMode == InputControlMode.trackpad && !_showTrackpadDock)
            Positioned(
              right: 16,
              bottom: _showExtendedKeys ? 54 : 16,
              child: InkWell(
                onTap: () {
                  HapticFeedback.lightImpact();
                  setState(() => _showTrackpadDock = true);
                },
                borderRadius: BorderRadius.circular(20),
                child: Container(
                  padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
                  decoration: BoxDecoration(
                    color: const Color(0xDD121622),
                    borderRadius: BorderRadius.circular(20),
                    border: Border.all(color: NivaroColors.borderSubtle),
                    boxShadow: const [
                      BoxShadow(color: Colors.black54, blurRadius: 8, offset: Offset(0, 2)),
                    ],
                  ),
                  child: const Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(Icons.mouse_rounded, size: 14, color: NivaroColors.primaryLight),
                      SizedBox(width: 4),
                      Text('Trackpad', style: TextStyle(color: Colors.white, fontSize: 11, fontWeight: FontWeight.bold)),
                    ],
                  ),
                ),
              ),
            ),

          // Collapsible Extended Keys Toolbar
          if (_showExtendedKeys)
            Positioned(
              left: 0,
              right: 0,
              bottom: 0,
              child: Container(
                height: 44,
                decoration: const BoxDecoration(
                  color: Color(0xF20D0F16),
                  border: Border(top: BorderSide(color: NivaroColors.borderSubtle)),
                ),
                child: ListView(
                  scrollDirection: Axis.horizontal,
                  padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
                  children: [
                    _KeyBtn(label: 'Ctrl', active: _ctrlLatched, onTap: () => _toggleLatch('ctrl')),
                    _KeyBtn(label: 'Alt', active: _altLatched, onTap: () => _toggleLatch('alt')),
                    _KeyBtn(label: 'Shift', active: _shiftLatched, onTap: () => _toggleLatch('shift')),
                    _KeyBtn(label: 'Win', active: _superLatched, onTap: () => _toggleLatch('super')),
                    _KeyBtn(label: 'Esc', onTap: () => _sendSingleKey(_keysymEscape)),
                    _KeyBtn(label: 'Tab', onTap: () => _sendSingleKey(_keysymTab)),
                    _KeyBtn(label: 'Enter', onTap: () => _sendSingleKey(_keysymEnter)),
                    _KeyBtn(label: 'Del', onTap: () => _sendSingleKey(_keysymDelete)),
                    const VerticalDivider(width: 10, color: NivaroColors.borderSubtle),
                    _KeyBtn(label: 'Ctrl+Alt+Del', isMacro: true, onTap: () => _sendKeyCombination([_keysymCtrlL, _keysymAltL, _keysymDelete])),
                    _KeyBtn(label: 'Alt+Tab', isMacro: true, onTap: () => _sendKeyCombination([_keysymAltL, _keysymTab])),
                    _KeyBtn(label: 'Alt+F4', isMacro: true, onTap: () => _sendKeyCombination([_keysymAltL, _keysymTable[LogicalKeyboardKey.f4]!])),
                    _KeyBtn(label: 'Win+D', isMacro: true, onTap: () => _sendKeyCombination([_keysymSuperL, 0x0064])),
                    _KeyBtn(label: 'Win+E', isMacro: true, onTap: () => _sendKeyCombination([_keysymSuperL, 0x0065])),
                    _KeyBtn(label: 'Ctrl+C', isMacro: true, onTap: () => _sendKeyCombination([_keysymCtrlL, 0x0063])),
                    _KeyBtn(label: 'Ctrl+V', isMacro: true, onTap: () => _sendKeyCombination([_keysymCtrlL, 0x0076])),
                    _KeyBtn(label: 'Ctrl+Z', isMacro: true, onTap: () => _sendKeyCombination([_keysymCtrlL, 0x007A])),
                    _KeyBtn(label: 'Ctrl+A', isMacro: true, onTap: () => _sendKeyCombination([_keysymCtrlL, 0x0061])),
                    const VerticalDivider(width: 10, color: NivaroColors.borderSubtle),
                    _KeyBtn(label: 'F1', onTap: () => _sendSingleKey(_keysymTable[LogicalKeyboardKey.f1]!)),
                    _KeyBtn(label: 'F2', onTap: () => _sendSingleKey(_keysymTable[LogicalKeyboardKey.f2]!)),
                    _KeyBtn(label: 'F5', onTap: () => _sendSingleKey(_keysymTable[LogicalKeyboardKey.f5]!)),
                    _KeyBtn(label: 'F11', onTap: () => _sendSingleKey(_keysymTable[LogicalKeyboardKey.f11]!)),
                    _KeyBtn(label: 'F12', onTap: () => _sendSingleKey(_keysymTable[LogicalKeyboardKey.f12]!)),
                    const VerticalDivider(width: 10, color: NivaroColors.borderSubtle),
                    _KeyBtn(label: '▲', onTap: () => _sendSingleKey(_keysymTable[LogicalKeyboardKey.arrowUp]!)),
                    _KeyBtn(label: '▼', onTap: () => _sendSingleKey(_keysymTable[LogicalKeyboardKey.arrowDown]!)),
                    _KeyBtn(label: '◄', onTap: () => _sendSingleKey(_keysymTable[LogicalKeyboardKey.arrowLeft]!)),
                    _KeyBtn(label: '►', onTap: () => _sendSingleKey(_keysymTable[LogicalKeyboardKey.arrowRight]!)),
                  ],
                ),
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildTrackpadDock() {
    final sensText = _trackpadSensitivity > 2.0 ? '2.4x' : (_trackpadSensitivity > 1.4 ? '1.8x' : '1.2x');

    if (!widget.isLandscape) {
      // Clean, ergonomic dual-row portrait layout with wide click pads
      return Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
        decoration: BoxDecoration(
          color: const Color(0xF2111520),
          borderRadius: BorderRadius.circular(18),
          border: Border.all(color: NivaroColors.borderSubtle),
          boxShadow: const [
            BoxShadow(color: Color(0xAA000000), blurRadius: 16, offset: Offset(0, 4)),
          ],
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Row(
              children: [
                _TrackpadBtn(
                  label: 'LEFT CLICK',
                  icon: Icons.mouse_rounded,
                  flex: 3,
                  onTap: () => _sendMouseClick(1),
                ),
                const SizedBox(width: 8),
                _TrackpadBtn(
                  label: 'RIGHT CLICK',
                  icon: Icons.menu_open_rounded,
                  flex: 3,
                  onTap: () => _sendMouseClick(4),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Row(
              children: [
                _TrackpadBtn(
                  label: 'MID',
                  flex: 2,
                  onTap: () => _sendMouseClick(2),
                ),
                const SizedBox(width: 5),
                _TrackpadBtn(
                  label: _dragLocked ? 'LOCKED' : 'DRAG',
                  icon: Icons.pan_tool_rounded,
                  active: _dragLocked,
                  flex: 2,
                  activeColor: NivaroColors.warning,
                  onTap: _toggleDragLock,
                ),
                const SizedBox(width: 5),
                Container(
                  decoration: BoxDecoration(
                    color: NivaroColors.surfaceRaised,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(color: NivaroColors.borderSubtle),
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      InkWell(
                        onTap: () => _sendWheel(true),
                        borderRadius: const BorderRadius.horizontal(left: Radius.circular(10)),
                        child: const Padding(
                          padding: EdgeInsets.symmetric(horizontal: 8, vertical: 5),
                          child: Icon(Icons.arrow_drop_up_rounded, size: 20, color: Colors.white70),
                        ),
                      ),
                      Container(width: 1, height: 16, color: NivaroColors.borderSubtle),
                      InkWell(
                        onTap: () => _sendWheel(false),
                        borderRadius: const BorderRadius.horizontal(right: Radius.circular(10)),
                        child: const Padding(
                          padding: EdgeInsets.symmetric(horizontal: 8, vertical: 5),
                          child: Icon(Icons.arrow_drop_down_rounded, size: 20, color: Colors.white70),
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 5),
                InkWell(
                  onTap: _cycleSensitivity,
                  borderRadius: BorderRadius.circular(10),
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
                    decoration: BoxDecoration(
                      color: NivaroColors.surfaceRaised,
                      borderRadius: BorderRadius.circular(10),
                      border: Border.all(color: NivaroColors.borderSubtle),
                    ),
                    child: Text(
                      sensText,
                      style: const TextStyle(color: NivaroColors.primaryLight, fontSize: 11, fontWeight: FontWeight.w800),
                    ),
                  ),
                ),
                const SizedBox(width: 4),
                IconButton(
                  icon: const Icon(Icons.keyboard_arrow_down_rounded, size: 20, color: Colors.white60),
                  tooltip: 'Collapse Dock',
                  visualDensity: VisualDensity.compact,
                  padding: EdgeInsets.zero,
                  onPressed: () {
                    HapticFeedback.lightImpact();
                    setState(() => _showTrackpadDock = false);
                  },
                ),
              ],
            ),
          ],
        ),
      );
    }

    // Landscape layout: Sleek single row
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
      decoration: BoxDecoration(
        color: const Color(0xEE111520),
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: NivaroColors.borderSubtle),
        boxShadow: const [
          BoxShadow(color: Color(0xAA000000), blurRadius: 16, offset: Offset(0, 4)),
        ],
      ),
      child: Row(
        children: [
          _TrackpadBtn(
            label: 'LEFT',
            icon: Icons.mouse_rounded,
            flex: 3,
            onTap: () => _sendMouseClick(1),
          ),
          const SizedBox(width: 5),
          _TrackpadBtn(
            label: 'RIGHT',
            icon: Icons.menu_open_rounded,
            flex: 3,
            onTap: () => _sendMouseClick(4),
          ),
          const SizedBox(width: 5),
          _TrackpadBtn(
            label: 'MID',
            flex: 2,
            onTap: () => _sendMouseClick(2),
          ),
          const SizedBox(width: 5),
          _TrackpadBtn(
            label: _dragLocked ? 'LOCKED' : 'DRAG',
            icon: Icons.pan_tool_rounded,
            active: _dragLocked,
            flex: 2,
            activeColor: NivaroColors.warning,
            onTap: _toggleDragLock,
          ),
          const SizedBox(width: 6),
          Container(
            decoration: BoxDecoration(
              color: NivaroColors.surfaceRaised,
              borderRadius: BorderRadius.circular(10),
              border: Border.all(color: NivaroColors.borderSubtle),
            ),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                InkWell(
                  onTap: () => _sendWheel(true),
                  borderRadius: const BorderRadius.horizontal(left: Radius.circular(10)),
                  child: const Padding(
                    padding: EdgeInsets.symmetric(horizontal: 7, vertical: 6),
                    child: Icon(Icons.arrow_drop_up_rounded, size: 20, color: Colors.white70),
                  ),
                ),
                Container(width: 1, height: 18, color: NivaroColors.borderSubtle),
                InkWell(
                  onTap: () => _sendWheel(false),
                  borderRadius: const BorderRadius.horizontal(right: Radius.circular(10)),
                  child: const Padding(
                    padding: EdgeInsets.symmetric(horizontal: 7, vertical: 6),
                    child: Icon(Icons.arrow_drop_down_rounded, size: 20, color: Colors.white70),
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(width: 5),
          InkWell(
            onTap: _cycleSensitivity,
            borderRadius: BorderRadius.circular(10),
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 6),
              decoration: BoxDecoration(
                color: NivaroColors.surfaceRaised,
                borderRadius: BorderRadius.circular(10),
                border: Border.all(color: NivaroColors.borderSubtle),
              ),
              child: Text(
                sensText,
                style: const TextStyle(color: NivaroColors.primaryLight, fontSize: 10.5, fontWeight: FontWeight.w800),
              ),
            ),
          ),
          const SizedBox(width: 2),
          IconButton(
            icon: const Icon(Icons.keyboard_arrow_down_rounded, size: 18, color: Colors.white60),
            tooltip: 'Collapse Dock',
            visualDensity: VisualDensity.compact,
            padding: EdgeInsets.zero,
            onPressed: () {
              HapticFeedback.lightImpact();
              setState(() => _showTrackpadDock = false);
            },
          ),
        ],
      ),
    );
  }

  Widget _buildFullHud() {
    final isPortrait = !widget.isLandscape && MediaQuery.of(context).size.width < 500;

    if (isPortrait) {
      // Clean, non-clipping 2-row layout in portrait
      return Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
        decoration: BoxDecoration(
          color: const Color(0xF212151E),
          borderRadius: BorderRadius.circular(18),
          border: Border.all(color: NivaroColors.borderSubtle),
          boxShadow: const [
            BoxShadow(color: Color(0xAA000000), blurRadius: 16, offset: Offset(0, 4)),
          ],
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // Row 1: Mode, Touch/Trackpad, Keyboard, Hide
            Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Padding(
                  padding: EdgeInsets.only(right: 6),
                  child: Icon(Icons.drag_indicator_rounded, size: 16, color: NivaroColors.textMuted),
                ),
                InkWell(
                  onTap: () {
                    setMode(_mode == ConsoleDisplayMode.vnc ? ConsoleDisplayMode.stream : ConsoleDisplayMode.vnc);
                  },
                  borderRadius: BorderRadius.circular(8),
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 3),
                    decoration: BoxDecoration(
                      color: Colors.black45,
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Icon(
                          _mode == ConsoleDisplayMode.vnc ? Icons.bolt_rounded : Icons.photo_camera_rounded,
                          size: 11,
                          color: _mode == ConsoleDisplayMode.vnc ? NivaroColors.successLight : NivaroColors.infoLight,
                        ),
                        const SizedBox(width: 3),
                        Text(
                          '$_fpsDisplay FPS',
                          style: TextStyle(
                            color: _mode == ConsoleDisplayMode.vnc ? NivaroColors.successLight : NivaroColors.infoLight,
                            fontSize: 10,
                            fontWeight: FontWeight.bold,
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
                const SizedBox(width: 6),
                InkWell(
                  onTap: () {
                    HapticFeedback.lightImpact();
                    setState(() {
                      _inputMode = _inputMode == InputControlMode.trackpad ? InputControlMode.touch : InputControlMode.trackpad;
                    });
                  },
                  borderRadius: BorderRadius.circular(10),
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                    decoration: BoxDecoration(
                      color: NivaroColors.surfaceRaised,
                      borderRadius: BorderRadius.circular(10),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Icon(
                          _inputMode == InputControlMode.trackpad ? Icons.mouse_rounded : Icons.touch_app_rounded,
                          size: 13,
                          color: NivaroColors.primaryLight,
                        ),
                        const SizedBox(width: 4),
                        Text(
                          _inputMode == InputControlMode.trackpad ? 'Trackpad' : 'Touch',
                          style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 11, color: Colors.white),
                        ),
                      ],
                    ),
                  ),
                ),
                const SizedBox(width: 4),
                IconButton(
                  icon: Icon(
                    _keyboardOpen ? Icons.keyboard_hide_rounded : Icons.keyboard_rounded,
                    size: 17,
                    color: _keyboardOpen ? NivaroColors.primaryLight : Colors.white70,
                  ),
                  tooltip: 'Keyboard',
                  visualDensity: VisualDensity.compact,
                  padding: const EdgeInsets.all(4),
                  constraints: const BoxConstraints(),
                  onPressed: _toggleKeyboard,
                ),
                const SizedBox(width: 4),
                IconButton(
                  icon: Icon(
                    Icons.keyboard_option_key_rounded,
                    size: 17,
                    color: _showExtendedKeys ? NivaroColors.primaryLight : Colors.white70,
                  ),
                  tooltip: 'Extra Keys',
                  visualDensity: VisualDensity.compact,
                  padding: const EdgeInsets.all(4),
                  constraints: const BoxConstraints(),
                  onPressed: () => setState(() => _showExtendedKeys = !_showExtendedKeys),
                ),
                const SizedBox(width: 4),
                IconButton(
                  icon: const Icon(Icons.visibility_off_outlined, size: 16, color: Colors.white60),
                  tooltip: 'Hide Controls',
                  visualDensity: VisualDensity.compact,
                  padding: const EdgeInsets.all(4),
                  constraints: const BoxConstraints(),
                  onPressed: () {
                    HapticFeedback.lightImpact();
                    setState(() => _showHudControls = false);
                  },
                ),
              ],
            ),
            const SizedBox(height: 4),
            // Row 2: Actions (Paste, Reset Zoom, Orientation, Snapshots, ISO, Power)
            Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                IconButton(
                  icon: const Icon(Icons.paste_rounded, size: 16, color: Colors.white70),
                  tooltip: 'Paste Text',
                  visualDensity: VisualDensity.compact,
                  padding: const EdgeInsets.all(4),
                  constraints: const BoxConstraints(),
                  onPressed: _openTextInputDialog,
                ),
                const SizedBox(width: 4),
                IconButton(
                  icon: const Icon(Icons.center_focus_strong_rounded, size: 16, color: Colors.white70),
                  tooltip: 'Reset Zoom (1:1)',
                  visualDensity: VisualDensity.compact,
                  padding: const EdgeInsets.all(4),
                  constraints: const BoxConstraints(),
                  onPressed: () {
                    HapticFeedback.lightImpact();
                    _transformController.value = Matrix4.identity();
                  },
                ),
                if (widget.onToggleOrientation != null) ...[
                  const SizedBox(width: 4),
                  IconButton(
                    icon: Icon(
                      widget.isLandscape ? Icons.stay_current_portrait_rounded : Icons.stay_current_landscape_rounded,
                      size: 16,
                      color: widget.isLandscape ? NivaroColors.primaryLight : Colors.white70,
                    ),
                    tooltip: widget.isLandscape ? 'Switch to Portrait' : 'Switch to Landscape',
                    visualDensity: VisualDensity.compact,
                    padding: const EdgeInsets.all(4),
                    constraints: const BoxConstraints(),
                    onPressed: widget.onToggleOrientation,
                  ),
                ],
                if (widget.onSnapshots != null) ...[
                  const SizedBox(width: 4),
                  IconButton(
                    icon: const Icon(Icons.camera_alt_rounded, size: 16, color: Colors.white70),
                    tooltip: 'Snapshots',
                    visualDensity: VisualDensity.compact,
                    padding: const EdgeInsets.all(4),
                    constraints: const BoxConstraints(),
                    onPressed: widget.onSnapshots,
                  ),
                ],
                if (widget.onIso != null) ...[
                  const SizedBox(width: 4),
                  IconButton(
                    icon: const Icon(Icons.album_rounded, size: 16, color: Colors.white70),
                    tooltip: 'ISO / CD-ROM',
                    visualDensity: VisualDensity.compact,
                    padding: const EdgeInsets.all(4),
                    constraints: const BoxConstraints(),
                    onPressed: widget.onIso,
                  ),
                ],
                if (widget.onPower != null) ...[
                  const SizedBox(width: 4),
                  IconButton(
                    icon: const Icon(Icons.power_settings_new_rounded, size: 16, color: NivaroColors.dangerLight),
                    tooltip: 'Power Menu',
                    visualDensity: VisualDensity.compact,
                    padding: const EdgeInsets.all(4),
                    constraints: const BoxConstraints(),
                    onPressed: widget.onPower,
                  ),
                ],
              ],
            ),
          ],
        ),
      );
    }

    // Landscape single row layout
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: const Color(0xEE12151E),
        borderRadius: BorderRadius.circular(22),
        border: Border.all(color: NivaroColors.borderSubtle),
        boxShadow: const [
          BoxShadow(color: Color(0x99000000), blurRadius: 14, offset: Offset(0, 3)),
        ],
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Padding(
            padding: EdgeInsets.only(left: 2, right: 6),
            child: Icon(Icons.drag_indicator_rounded, size: 16, color: NivaroColors.textMuted),
          ),
          InkWell(
            onTap: () {
              setMode(_mode == ConsoleDisplayMode.vnc ? ConsoleDisplayMode.stream : ConsoleDisplayMode.vnc);
            },
            borderRadius: BorderRadius.circular(10),
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 3),
              margin: const EdgeInsets.only(right: 6),
              decoration: BoxDecoration(
                color: Colors.black45,
                borderRadius: BorderRadius.circular(10),
              ),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(
                    _mode == ConsoleDisplayMode.vnc ? Icons.bolt_rounded : Icons.photo_camera_rounded,
                    size: 11,
                    color: _mode == ConsoleDisplayMode.vnc ? NivaroColors.successLight : NivaroColors.infoLight,
                  ),
                  const SizedBox(width: 3),
                  Text(
                    '$_fpsDisplay FPS',
                    style: TextStyle(
                      color: _mode == ConsoleDisplayMode.vnc ? NivaroColors.successLight : NivaroColors.infoLight,
                      fontSize: 9.5,
                      fontWeight: FontWeight.bold,
                    ),
                  ),
                ],
              ),
            ),
          ),
          InkWell(
            onTap: () {
              HapticFeedback.lightImpact();
              setState(() {
                _inputMode = _inputMode == InputControlMode.trackpad ? InputControlMode.touch : InputControlMode.trackpad;
              });
            },
            borderRadius: BorderRadius.circular(14),
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
              decoration: BoxDecoration(
                color: NivaroColors.surfaceRaised,
                borderRadius: BorderRadius.circular(14),
              ),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(
                    _inputMode == InputControlMode.trackpad ? Icons.mouse_rounded : Icons.touch_app_rounded,
                    size: 14,
                    color: NivaroColors.primaryLight,
                  ),
                  const SizedBox(width: 4),
                  Text(
                    _inputMode == InputControlMode.trackpad ? 'Trackpad' : 'Touch',
                    style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 11, color: Colors.white),
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(width: 4),
          IconButton(
            icon: Icon(
              _keyboardOpen ? Icons.keyboard_hide_rounded : Icons.keyboard_rounded,
              size: 18,
              color: _keyboardOpen ? NivaroColors.primaryLight : Colors.white70,
            ),
            tooltip: 'Keyboard',
            visualDensity: VisualDensity.compact,
            onPressed: _toggleKeyboard,
          ),
          IconButton(
            icon: Icon(
              Icons.keyboard_option_key_rounded,
              size: 18,
              color: _showExtendedKeys ? NivaroColors.primaryLight : Colors.white70,
            ),
            tooltip: 'Extra Keys',
            visualDensity: VisualDensity.compact,
            onPressed: () => setState(() => _showExtendedKeys = !_showExtendedKeys),
          ),
          IconButton(
            icon: const Icon(Icons.paste_rounded, size: 16, color: Colors.white70),
            tooltip: 'Paste Text',
            visualDensity: VisualDensity.compact,
            onPressed: _openTextInputDialog,
          ),
          IconButton(
            icon: const Icon(Icons.center_focus_strong_rounded, size: 16, color: Colors.white70),
            tooltip: 'Reset Zoom (1:1)',
            visualDensity: VisualDensity.compact,
            onPressed: () {
              HapticFeedback.lightImpact();
              _transformController.value = Matrix4.identity();
            },
          ),
          if (widget.onToggleOrientation != null)
            IconButton(
              icon: Icon(
                widget.isLandscape ? Icons.stay_current_portrait_rounded : Icons.stay_current_landscape_rounded,
                size: 16,
                color: widget.isLandscape ? NivaroColors.primaryLight : Colors.white70,
              ),
              tooltip: widget.isLandscape ? 'Switch to Portrait' : 'Switch to Landscape',
              visualDensity: VisualDensity.compact,
              onPressed: widget.onToggleOrientation,
            ),
          if (widget.onSnapshots != null)
            IconButton(
              icon: const Icon(Icons.camera_alt_rounded, size: 16, color: Colors.white70),
              tooltip: 'Snapshots',
              visualDensity: VisualDensity.compact,
              onPressed: widget.onSnapshots,
            ),
          if (widget.onIso != null)
            IconButton(
              icon: const Icon(Icons.album_rounded, size: 16, color: Colors.white70),
              tooltip: 'ISO / CD-ROM',
              visualDensity: VisualDensity.compact,
              onPressed: widget.onIso,
            ),
          if (widget.onPower != null)
            IconButton(
              icon: const Icon(Icons.power_settings_new_rounded, size: 16, color: NivaroColors.dangerLight),
              tooltip: 'Power Menu',
              visualDensity: VisualDensity.compact,
              onPressed: widget.onPower,
            ),
          IconButton(
            icon: const Icon(Icons.visibility_off_outlined, size: 16, color: Colors.white60),
            tooltip: 'Hide Controls',
            visualDensity: VisualDensity.compact,
            onPressed: () {
              HapticFeedback.lightImpact();
              setState(() => _showHudControls = false);
            },
          ),
        ],
      ),
    );
  }

  // Collapsed setting pill: only shows the setting icon with zero drag clutter, user drags directly by the icon
  Widget _buildMiniHud() {
    return InkWell(
      onTap: () {
        HapticFeedback.lightImpact();
        setState(() => _showHudControls = true);
      },
      borderRadius: BorderRadius.circular(22),
      child: Container(
        width: 44,
        height: 44,
        decoration: BoxDecoration(
          color: const Color(0xF212151E),
          shape: BoxShape.circle,
          border: Border.all(color: NivaroColors.primary.withOpacity(0.6), width: 1.5),
          boxShadow: const [
            BoxShadow(color: Color(0xAA000000), blurRadius: 12, offset: Offset(0, 3)),
          ],
        ),
        alignment: Alignment.center,
        child: const Icon(Icons.tune_rounded, size: 22, color: NivaroColors.primaryLight),
      ),
    );
  }

  Widget _buildStreamCanvas() {
    if (_currentFrameBytes != null) {
      return Image.memory(
        _currentFrameBytes!,
        fit: BoxFit.contain,
        gaplessPlayback: true,
      );
    }
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const CircularProgressIndicator(color: NivaroColors.primaryLight, strokeWidth: 2.5),
          const SizedBox(height: 12),
          Text(_streamError ?? 'Connecting to VM display feed...', style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13)),
        ],
      ),
    );
  }

  Widget _buildVncCanvas() {
    return ValueListenableBuilder<ui.Image?>(
      valueListenable: widget.client.frame,
      builder: (context, frame, _) {
        if (frame != null) {
          return RawImage(
            image: frame,
            fit: BoxFit.contain,
          );
        }
        return ValueListenableBuilder<String?>(
          valueListenable: widget.client.error,
          builder: (context, err, _) {
            if (err != null) {
              return Center(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Icon(Icons.videocam_off_rounded, size: 40, color: NivaroColors.warningLight),
                    const SizedBox(height: 10),
                    Text(err, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12), textAlign: TextAlign.center),
                    const SizedBox(height: 12),
                    ElevatedButton(
                      onPressed: () {
                        widget.client.connect();
                      },
                      child: const Text('Reconnect'),
                    ),
                  ],
                ),
              );
            }
            return const Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  CircularProgressIndicator(color: NivaroColors.primaryLight, strokeWidth: 2.5),
                  SizedBox(height: 12),
                  Text('Connecting to High-Speed VNC Display...', style: TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
                ],
              ),
            );
          },
        );
      },
    );
  }
}

class _TrackpadBtn extends StatelessWidget {
  final String label;
  final IconData? icon;
  final bool active;
  final int flex;
  final Color? activeColor;
  final VoidCallback onTap;

  const _TrackpadBtn({
    required this.label,
    this.icon,
    this.active = false,
    this.flex = 1,
    this.activeColor,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final effectiveActiveColor = activeColor ?? NivaroColors.primary;
    return Expanded(
      flex: flex,
      child: Material(
        color: Colors.transparent,
        child: InkWell(
          onTap: () {
            HapticFeedback.lightImpact();
            onTap();
          },
          borderRadius: BorderRadius.circular(10),
          child: Container(
            padding: const EdgeInsets.symmetric(vertical: 8),
            decoration: BoxDecoration(
              color: active ? effectiveActiveColor : NivaroColors.surfaceRaised,
              borderRadius: BorderRadius.circular(10),
              border: Border.all(
                color: active ? effectiveActiveColor : NivaroColors.borderSubtle,
              ),
            ),
            alignment: Alignment.center,
            child: Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                if (icon != null) ...[
                  Icon(icon, size: 13, color: active ? Colors.black : Colors.white),
                  const SizedBox(width: 4),
                ],
                Text(
                  label,
                  style: TextStyle(
                    color: active ? Colors.black : Colors.white,
                    fontWeight: FontWeight.w800,
                    fontSize: 10.5,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _KeyBtn extends StatelessWidget {
  final String label;
  final bool active;
  final bool isMacro;
  final VoidCallback onTap;

  const _KeyBtn({
    required this.label,
    this.active = false,
    this.isMacro = false,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(right: 5),
      child: InkWell(
        onTap: () {
          HapticFeedback.lightImpact();
          onTap();
        },
        borderRadius: BorderRadius.circular(6),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 3),
          decoration: BoxDecoration(
            color: active
                ? NivaroColors.primary
                : (isMacro ? NivaroColors.primary.withOpacity(0.15) : NivaroColors.surfaceRaised),
            borderRadius: BorderRadius.circular(6),
            border: Border.all(
              color: active
                  ? NivaroColors.primaryLight
                  : (isMacro ? NivaroColors.primary.withOpacity(0.4) : NivaroColors.borderSubtle),
            ),
          ),
          alignment: Alignment.center,
          child: Text(
            label,
            style: TextStyle(
              color: active
                  ? Colors.white
                  : (isMacro ? NivaroColors.primaryLight : NivaroColors.textPrimary),
              fontWeight: FontWeight.w700,
              fontSize: 11,
            ),
          ),
        ),
      ),
    );
  }
}

