import 'dart:async';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';

import '../ui/ui.dart';

/// The width and height a PNG's header gives, or null when [bytes] isn't
/// a PNG (a proxy's error page answered with 200, say).
({int width, int height})? pngSize(Uint8List bytes) {
  const signature = [0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A];
  if (bytes.length < 24) return null;
  for (var i = 0; i < signature.length; i++) {
    if (bytes[i] != signature[i]) return null;
  }
  // The IHDR chunk comes first: width and height, big-endian, at 16.
  final header = ByteData.sublistView(bytes, 16, 24);
  final w = header.getUint32(0), h = header.getUint32(4);
  return w == 0 || h == 0 ? null : (width: w, height: h);
}

/// A running VM's screen, refreshed while Home is on screen - the one
/// running VM's console at a glance. Tapping it opens the console.
///
/// It asks [fetch] for a new picture every [every] (null: only when
/// [refreshToken] changes, i.e. on pull-to-refresh), and only while it can
/// be seen: not while the app is in the background, Home is a hidden tab
/// or a screen covers it. Failures are quiet: the last picture stays, or
/// a placeholder says there is none yet, and the next tries come slower.
class VmConsolePreview extends StatefulWidget {
  const VmConsolePreview({
    super.key,
    required this.name,
    required this.fetch,
    required this.every,
    this.aspectRatio = 16 / 9,
    this.refreshToken = 0,
    this.onOpen,
  });

  final String name;

  /// PNG bytes of the VM's screen; null when it has no picture (no
  /// display yet); throws when the server can't be reached.
  final Future<Uint8List?> Function() fetch;

  final Duration? every;

  /// The VM's display shape until the first picture gives the real one.
  final double aspectRatio;

  /// Changes on each pull-to-refresh: takes a new picture at once.
  final int refreshToken;

  final VoidCallback? onOpen;

  /// The longest wait between tries after repeated failures.
  static const maxBackoff = Duration(minutes: 1);

  @override
  State<VmConsolePreview> createState() => _VmConsolePreviewState();
}

class _VmConsolePreviewState extends State<VmConsolePreview> with WidgetsBindingObserver {
  ImageProvider? _image;
  double? _aspect;

  /// A try has finished (with or without a picture).
  bool _tried = false;
  int _failures = 0;
  bool _busy = false;
  DateTime? _lastTry;
  Timer? _timer;
  bool _visible = true;
  bool _foreground = true;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    final state = WidgetsBinding.instance.lifecycleState;
    _foreground = state == null || state == AppLifecycleState.resumed;
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    // A hidden IndexedStack tab and a covered route have their tickers
    // off; this runs again whenever that changes.
    final visible = TickerMode.valuesOf(context).enabled;
    final first = _lastTry == null && !_busy;
    if (visible != _visible || first) {
      _visible = visible;
      if (first) {
        // One picture on open, even with refreshing off.
        _take();
      } else {
        _schedule();
      }
    }
  }

  @override
  void didUpdateWidget(VmConsolePreview old) {
    super.didUpdateWidget(old);
    if (widget.refreshToken != old.refreshToken) {
      _take();
    } else if (widget.every != old.every) {
      _schedule();
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    final foreground = state == AppLifecycleState.resumed;
    if (foreground == _foreground) return;
    _foreground = foreground;
    _schedule();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _timer?.cancel();
    _image?.evict();
    super.dispose();
  }

  bool get _active => _visible && _foreground && widget.every != null;

  // The wait before the next try: the interval, doubled per failure in a
  // row, up to a minute.
  Duration get _delay {
    final base = widget.every!;
    if (_failures == 0) return base;
    final backedOff = base * (1 << math.min(_failures, 4));
    return backedOff < VmConsolePreview.maxBackoff ? backedOff : VmConsolePreview.maxBackoff;
  }

  /// Times the next try, or stops when the preview can't be seen.
  void _schedule() {
    _timer?.cancel();
    _timer = null;
    if (!_active || _busy) return;
    final last = _lastTry;
    final wait = last == null ? Duration.zero : _delay - clock.now().difference(last);
    // Overdue (back on screen after a while): a picture now.
    if (wait <= Duration.zero) {
      _take();
    } else {
      _timer = Timer(wait, _take);
    }
  }

  Future<void> _take() async {
    if (_busy || !mounted) return;
    _timer?.cancel();
    _busy = true;
    _lastTry = clock.now();
    try {
      final bytes = await widget.fetch();
      if (!mounted) return;
      _failures = 0;
      final size = bytes == null ? null : pngSize(bytes);
      if (bytes != null && size != null) {
        final previous = _image;
        _image = MemoryImage(bytes);
        _aspect = size.width / size.height;
        // Each picture is a new cache entry; drop the one it replaces.
        if (previous != null) WidgetsBinding.instance.addPostFrameCallback((_) => previous.evict());
      } else {
        // No picture to give (the VM has no display yet): the placeholder.
        _image?.evict();
        _image = null;
      }
    } catch (_) {
      _failures++;
    } finally {
      _busy = false;
      if (mounted) {
        setState(() => _tried = true);
        _schedule();
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final t = DesignTokens.of(context);
    final text = Theme.of(context).textTheme;
    final radius = BorderRadius.circular(math.max(t.groupRadius - Space.md, 2));
    final image = _image;

    final Widget picture = image != null
        ? Image(image: image, fit: BoxFit.fill, gaplessPlayback: true, filterQuality: FilterQuality.medium)
        : ColoredBox(
            color: scheme.surfaceContainerHighest,
            child: Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(Icons.desktop_windows_outlined, color: scheme.onSurfaceVariant),
                  // Nothing while the first picture loads; a word once a
                  // try came back without one.
                  if (_tried) ...[
                    const SizedBox(height: Space.sm),
                    Text('No picture yet', style: text.bodySmall?.copyWith(color: scheme.onSurfaceVariant)),
                  ],
                ],
              ),
            ),
          );

    return Padding(
      padding: const EdgeInsets.fromLTRB(Space.lg, Space.lg, Space.lg, Space.md),
      child: Semantics(
        button: widget.onOpen != null,
        label: image == null ? 'Console of ${widget.name}, no picture yet' : 'Console of ${widget.name}, live picture',
        hint: widget.onOpen == null ? null : 'Opens the console',
        excludeSemantics: true,
        child: Material(
          color: scheme.surfaceContainerHighest,
          clipBehavior: Clip.antiAlias,
          // A hairline edge, so a black desktop still reads as a screen
          // on a dark or true black card.
          shape: RoundedRectangleBorder(borderRadius: radius, side: BorderSide(color: scheme.outlineVariant)),
          child: InkWell(
            onTap: widget.onOpen,
            child: AspectRatio(
              aspectRatio: _aspect ?? widget.aspectRatio,
              child: Stack(
                fit: StackFit.expand,
                children: [
                  picture,
                  if (widget.onOpen != null)
                    PositionedDirectional(
                      end: Space.sm,
                      bottom: Space.sm,
                      child: DecoratedBox(
                        decoration: BoxDecoration(color: scheme.surface.withValues(alpha: .82), borderRadius: BorderRadius.circular(t.radii.sm)),
                        child: Padding(
                          padding: const EdgeInsets.all(Space.xs),
                          child: Icon(Icons.open_in_full, size: 18, color: scheme.onSurface),
                        ),
                      ),
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
