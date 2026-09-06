import 'dart:ui' as ui;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme.dart';
import '../services/rfb_client.dart';

/// X11 keysyms for non-printable and modifier keys.
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
const int _keysymDelete = 0xFFFF;
const int _keysymEscape = 0xFF1B;
const int _keysymTab = 0xFF09;
const int _keysymEnter = 0xFF0D;
const int _keysymSuperL = 0xFFEB;
const int _keysymBackspace = 0xFF08;

/// Native RFB touch canvas with pinch-to-zoom, pan, touch gestures,
/// soft keyboard integration, and virtual shortcut keys.
class RfbView extends StatefulWidget {
  final RfbClient client;
  final VoidCallback? onPower;
  const RfbView({super.key, required this.client, this.onPower});

  @override
  State<RfbView> createState() => _RfbViewState();
}

class _RfbViewState extends State<RfbView> {
  final _keyboardFocus = FocusNode();
  final _keyboardController = TextEditingController();
  String _lastText = '';
  bool _keyboardOpen = false;
  bool _showExtendedKeys = false;

  @override
  void dispose() {
    _keyboardFocus.dispose();
    _keyboardController.dispose();
    super.dispose();
  }

  void _toggleKeyboard() {
    HapticFeedback.lightImpact();
    setState(() => _keyboardOpen = !_keyboardOpen);
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
        widget.client.sendKey(rune, true);
        widget.client.sendKey(rune, false);
      }
    } else if (text.length < _lastText.length) {
      widget.client.sendKey(_keysymBackspace, true);
      widget.client.sendKey(_keysymBackspace, false);
    }
    _lastText = text;
  }

  KeyEventResult _onHardwareKey(FocusNode node, KeyEvent event) {
    final keysym = _keysymTable[event.logicalKey];
    if (keysym == null) return KeyEventResult.ignored;
    if (event is KeyDownEvent) {
      widget.client.sendKey(keysym, true);
    } else if (event is KeyUpEvent) {
      widget.client.sendKey(keysym, false);
    }
    return KeyEventResult.handled;
  }

  void _sendKeyCombination(List<int> keys) {
    HapticFeedback.mediumImpact();
    for (final k in keys) {
      widget.client.sendKey(k, true);
    }
    for (final k in keys.reversed) {
      widget.client.sendKey(k, false);
    }
  }

  void _sendSingleKey(int key) {
    HapticFeedback.lightImpact();
    widget.client.sendKey(key, true);
    widget.client.sendKey(key, false);
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        // Live Screen Canvas
        Expanded(
          child: Focus(
            onKeyEvent: (node, event) => _onHardwareKey(node, event),
            child: Stack(
              children: [
                Positioned.fill(
                  child: ValueListenableBuilder<ui.Image?>(
                    valueListenable: widget.client.frame,
                    builder: (context, image, _) {
                      if (image == null) {
                        return Center(
                          child: Column(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              const CircularProgressIndicator(strokeWidth: 3),
                              const SizedBox(height: 16),
                              Text(
                                'Connecting to VM RFB sidecar...',
                                style: TextStyle(color: Colors.white.withValues(alpha: 0.7), fontSize: 13),
                              ),
                            ],
                          ),
                        );
                      }
                      return LayoutBuilder(
                        builder: (context, constraints) {
                          final scale = (constraints.maxWidth / image.width < constraints.maxHeight / image.height)
                              ? constraints.maxWidth / image.width
                              : constraints.maxHeight / image.height;
                          final dispW = image.width * scale;
                          final dispH = image.height * scale;
                          return InteractiveViewer(
                            minScale: 1,
                            maxScale: 5,
                            child: Center(
                              child: SizedBox(
                                width: dispW,
                                height: dispH,
                                child: GestureDetector(
                                  onTapDown: (d) => widget.client.sendPointer(
                                    (d.localPosition.dx / scale).round(),
                                    (d.localPosition.dy / scale).round(),
                                    1,
                                  ),
                                  onTapUp: (d) => widget.client.sendPointer(
                                    (d.localPosition.dx / scale).round(),
                                    (d.localPosition.dy / scale).round(),
                                    0,
                                  ),
                                  onPanStart: (d) => widget.client.sendPointer(
                                    (d.localPosition.dx / scale).round(),
                                    (d.localPosition.dy / scale).round(),
                                    1,
                                  ),
                                  onPanUpdate: (d) => widget.client.sendPointer(
                                    (d.localPosition.dx / scale).round(),
                                    (d.localPosition.dy / scale).round(),
                                    1,
                                  ),
                                  onPanEnd: (_) {},
                                  child: CustomPaint(
                                    size: Size(dispW, dispH),
                                    painter: _FramePainter(image),
                                  ),
                                ),
                              ),
                            ),
                          );
                        },
                      );
                    },
                  ),
                ),

                // Invisible IME input receiver
                Positioned(
                  left: 0,
                  right: 0,
                  bottom: 0,
                  child: SizedBox(
                    height: 0.1,
                    width: 0.1,
                    child: TextField(
                      focusNode: _keyboardFocus,
                      controller: _keyboardController,
                      onChanged: _onTextChanged,
                      autofocus: false,
                      decoration: const InputDecoration(border: InputBorder.none),
                      style: const TextStyle(color: Colors.transparent, fontSize: 1),
                    ),
                  ),
                ),

                // Error overlay
                ValueListenableBuilder<String?>(
                  valueListenable: widget.client.error,
                  builder: (context, err, _) {
                    if (err == null) return const SizedBox.shrink();
                    return Container(
                      color: Colors.black.withValues(alpha: 0.85),
                      alignment: Alignment.center,
                      child: Padding(
                        padding: const EdgeInsets.all(24),
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            const Icon(Icons.cloud_off_rounded, color: NivaroColors.danger, size: 42),
                            const SizedBox(height: 14),
                            Text(
                              err,
                              style: const TextStyle(color: Colors.white, fontSize: 14),
                              textAlign: TextAlign.center,
                            ),
                          ],
                        ),
                      ),
                    );
                  },
                ),
              ],
            ),
          ),
        ),

        // Extended Shortcut Bar (Scrollable)
        if (_showExtendedKeys)
          Container(
            height: 48,
            color: NivaroColors.surfaceRaised,
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
            child: ListView(
              scrollDirection: Axis.horizontal,
              children: [
                _keyButton('Tab', () => _sendSingleKey(_keysymTab)),
                _keyButton('Super (Win)', () => _sendSingleKey(_keysymSuperL)),
                _keyButton('Alt+Tab', () => _sendKeyCombination([_keysymAltL, _keysymTab])),
                _keyButton('Ctrl+C', () => _sendKeyCombination([_keysymCtrlL, 0x0063])),
                _keyButton('Enter ↵', () => _sendSingleKey(_keysymEnter)),
                _keyButton('F1', () => _sendSingleKey(0xFFBE)),
                _keyButton('F11', () => _sendSingleKey(0xFFC8)),
                _keyButton('F12', () => _sendSingleKey(0xFFC9)),
              ],
            ),
          ),

        // Bottom Console Control Bar
        Container(
          decoration: const BoxDecoration(
            color: NivaroColors.surfaceRaised,
            border: Border(top: BorderSide(color: NivaroColors.borderSubtle)),
          ),
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          child: Row(
            mainAxisAlignment: MainAxisAlignment.spaceEvenly,
            children: [
              IconButton(
                icon: Icon(
                  _keyboardOpen ? Icons.keyboard_hide_rounded : Icons.keyboard_rounded,
                  color: _keyboardOpen ? NivaroColors.primaryLight : Colors.white,
                ),
                onPressed: _toggleKeyboard,
                tooltip: 'Virtual Keyboard',
              ),
              IconButton(
                icon: Icon(
                  _showExtendedKeys ? Icons.more_horiz_rounded : Icons.more_rounded,
                  color: _showExtendedKeys ? NivaroColors.primaryLight : Colors.white,
                ),
                onPressed: () => setState(() => _showExtendedKeys = !_showExtendedKeys),
                tooltip: 'Shortcut Keys',
              ),
              FilledButton.tonal(
                onPressed: () => _sendKeyCombination([_keysymCtrlL, _keysymAltL, _keysymDelete]),
                style: FilledButton.styleFrom(
                  backgroundColor: NivaroColors.surfaceMuted,
                  padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.medium)),
                ),
                child: const Text('Ctrl+Alt+Del', style: TextStyle(fontSize: 12, fontWeight: FontWeight.w700)),
              ),
              FilledButton.tonal(
                onPressed: () => _sendSingleKey(_keysymEscape),
                style: FilledButton.styleFrom(
                  backgroundColor: NivaroColors.surfaceMuted,
                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.medium)),
                ),
                child: const Text('ESC', style: TextStyle(fontSize: 12, fontWeight: FontWeight.w700)),
              ),
              if (widget.onPower != null)
                IconButton(
                  icon: const Icon(Icons.power_settings_new_rounded, color: NivaroColors.danger),
                  onPressed: () {
                    HapticFeedback.mediumImpact();
                    widget.onPower?.call();
                  },
                  tooltip: 'Power Operations',
                ),
            ],
          ),
        ),
      ],
    );
  }

  Widget _keyButton(String label, VoidCallback onTap) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 4),
      child: OutlinedButton(
        onPressed: onTap,
        style: OutlinedButton.styleFrom(
          side: const BorderSide(color: NivaroColors.borderSubtle),
          backgroundColor: NivaroColors.surfaceMuted,
          foregroundColor: Colors.white,
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
          minimumSize: const Size(40, 32),
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.medium)),
        ),
        child: Text(label, style: const TextStyle(fontSize: 11.5, fontWeight: FontWeight.w600)),
      ),
    );
  }
}

class _FramePainter extends CustomPainter {
  final ui.Image image;
  _FramePainter(this.image);

  @override
  void paint(Canvas canvas, Size size) {
    final src = Rect.fromLTWH(0, 0, image.width.toDouble(), image.height.toDouble());
    final dst = Rect.fromLTWH(0, 0, size.width, size.height);
    canvas.drawImageRect(image, src, dst, Paint()..filterQuality = FilterQuality.low);
  }

  @override
  bool shouldRepaint(covariant _FramePainter oldDelegate) => oldDelegate.image != image;
}

