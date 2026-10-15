import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../services/storage_service.dart';
import '../ui/ui.dart';
import 'console_edge_handle.dart';

/// A remembered console choice (StorageService console prefs); null when
/// storage can't be read.
Future<String?> consolePref(String name) async {
  try {
    return await StorageService.instance.getConsolePref(name);
  } catch (_) {
    return null;
  }
}

Future<void> saveConsolePref(String name, String value) async {
  try {
    await StorageService.instance.setConsolePref(name, value);
  } catch (e) {
    debugPrint('[console] Could not save $name: $e');
  }
}

/// The session toolbar of a full-screen console, after Microsoft's
/// Windows App: a pill on the top edge with the console's [buttons], then
/// Pin, Hide and Disconnect. It hides itself after [hideAfter], leaving a
/// tab to bring it back, and slides along the edge by its grip. Hidden
/// with its button, it stays hidden (on connect too, and next time) until
/// its tab is tapped; pinned, it never hides. That and where it sits are
/// remembered per [kind] ('host' or 'vm').
///
/// The console keeps it mounted across reconnects ([visible] false while
/// there is no session) and calls [ConsoleSessionToolbarState.poke] on
/// activity and [ConsoleSessionToolbarState.menuOpened]/`menuClosed`
/// around its menus.
class ConsoleSessionToolbar extends StatefulWidget {
  const ConsoleSessionToolbar({
    super.key,
    required this.kind,
    required this.buttons,
    required this.onDisconnect,
    this.visible = true,
  });

  static const hideAfter = Duration(seconds: 4);

  final String kind;
  final List<Widget> buttons;
  final VoidCallback onDisconnect;
  final bool visible;

  @override
  State<ConsoleSessionToolbar> createState() => ConsoleSessionToolbarState();
}

class ConsoleSessionToolbarState extends State<ConsoleSessionToolbar> {
  final _pillKey = GlobalKey();
  bool _shown = true;
  bool _hidden = false;
  bool _pinned = false;
  double _x = 0;
  int _menusOpen = 0;
  Timer? _hideTimer;

  /// Whether the pill is showing (rather than its tab).
  bool get shown => _shown;

  String get _kind => widget.kind;

  @override
  void initState() {
    super.initState();
    poke();
    _load();
  }

  @override
  void dispose() {
    _hideTimer?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    final bar = await consolePref('bar@$_kind');
    final x = double.tryParse(await consolePref('barx@$_kind') ?? '');
    if (!mounted) return;
    setState(() {
      _hidden = bar == 'hidden';
      _pinned = bar == 'pinned';
      if (_hidden) _shown = false;
      if (x != null && x.isFinite) _x = x.clamp(-1.0, 1.0);
    });
    // The timer started before the choice loaded.
    if (_pinned) _hideTimer?.cancel();
  }

  /// Shows the toolbar and restarts its hide timer. It stays while a menu
  /// is open, and for good while TalkBack or Switch Access is on.
  void poke() {
    if (_hidden) return;
    _hideTimer?.cancel();
    if (!_shown) setState(() => _shown = true);
    if (_pinned) return;
    _hideTimer = Timer(ConsoleSessionToolbar.hideAfter, () {
      if (!mounted || _menusOpen > 0 || MediaQuery.maybeAccessibleNavigationOf(context) == true) return;
      setState(() => _shown = false);
    });
  }

  void menuOpened() {
    _menusOpen++;
    _hideTimer?.cancel();
  }

  void menuClosed() {
    _menusOpen = _menusOpen > 0 ? _menusOpen - 1 : 0;
    poke();
  }

  void _slide(double dx) {
    final width = _shown ? _pillKey.currentContext?.size?.width : ConsoleEdgeHandle.size.width;
    setState(() => _x = slideAlongTopEdge(context, _x, dx, width ?? double.infinity));
    poke();
  }

  void _slid() => saveConsolePref('barx@$_kind', _x.toStringAsFixed(3));

  /// The toolbar's own Hide button, and its tab: the choice sticks.
  void _setHidden(bool hidden) {
    HapticFeedback.selectionClick();
    _hideTimer?.cancel();
    setState(() {
      _hidden = hidden;
      _pinned = false;
      _shown = !hidden;
    });
    saveConsolePref('bar@$_kind', hidden ? 'hidden' : 'shown');
    if (!hidden) poke();
  }

  /// The toolbar's Pin button: it stays up until unpinned.
  void _setPinned(bool pinned) {
    HapticFeedback.selectionClick();
    setState(() => _pinned = pinned);
    saveConsolePref('bar@$_kind', pinned ? 'pinned' : 'shown');
    poke();
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.visible) return const SizedBox.shrink();
    final scheme = Theme.of(context).colorScheme;
    final Widget child;
    if (_shown) {
      child = Padding(
        key: const ValueKey('pill'),
        padding: const EdgeInsets.only(top: Space.sm),
        child: Material(
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
                  onHorizontalDragUpdate: (d) => _slide(d.delta.dx),
                  onHorizontalDragEnd: (_) => _slid(),
                  child: SizedBox(
                    width: 24,
                    height: 48,
                    child: Icon(Icons.drag_indicator, size: 20, color: scheme.onSurfaceVariant),
                  ),
                ),
              ),
              // 48dp targets whatever the style's density. On a narrow
              // screen (or at large text) the middle scrolls; Pin, Hide
              // and Disconnect stay.
              Flexible(
                child: IconButtonTheme(
                  data: IconButtonThemeData(style: IconButton.styleFrom(minimumSize: const Size.square(48))),
                  child: Row(mainAxisSize: MainAxisSize.min, children: [
                    Flexible(
                      child: SingleChildScrollView(
                        scrollDirection: Axis.horizontal,
                        child: Row(mainAxisSize: MainAxisSize.min, children: widget.buttons),
                      ),
                    ),
                    IconButton(
                      tooltip: _pinned ? 'Unpin toolbar' : 'Pin toolbar',
                      isSelected: _pinned,
                      icon: const Icon(Icons.push_pin_outlined),
                      selectedIcon: const Icon(Icons.push_pin),
                      onPressed: () => _setPinned(!_pinned),
                    ),
                    IconButton(
                      tooltip: 'Hide toolbar',
                      icon: const Icon(Icons.expand_less),
                      onPressed: () => _setHidden(true),
                    ),
                    Padding(
                      padding: const EdgeInsets.only(right: Space.xs),
                      child: IconButton(
                        tooltip: 'Disconnect',
                        icon: Icon(Icons.close, color: scheme.error),
                        onPressed: widget.onDisconnect,
                      ),
                    ),
                  ]),
                ),
              ),
            ],
          ),
        ),
      );
    } else {
      child = ConsoleEdgeHandle(
        key: const ValueKey('tab'),
        label: 'Show session toolbar',
        onTap: () => _hidden ? _setHidden(false) : poke(),
        onSlide: _slide,
        onSlideEnd: _slid,
      );
    }
    return consoleTopEdge(
      context,
      x: _x,
      child: Semantics(
        container: true,
        label: 'Session toolbar',
        explicitChildNodes: true,
        child: AnimatedSwitcher(duration: Motion.of(context).short, child: child),
      ),
    );
  }
}
