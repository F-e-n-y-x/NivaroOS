import 'dart:async';

import 'package:flutter/material.dart';

import '../services/rfb_client.dart';
import '../services/vm_client.dart';
import '../ui/ui.dart';
import '../widgets/rfb_view.dart';
import 'host_display_fit.dart';

/// The NivaroOS server's own desktop (x11vnc on its display), through the
/// same console as a VM (`/v1/vm-sidecar/host/console`). The one thing
/// only the host has is its display resolution.
class HostDesktopScreen extends StatefulWidget {
  const HostDesktopScreen({super.key, this.client, this.rfb, this.history});

  /// For tests; the app uses the session's gateway clients.
  final VmClient? client;
  final RfbClient? rfb;
  final RemoteClipboardHistory? history;

  @override
  State<HostDesktopScreen> createState() => _HostDesktopScreenState();
}

class _HostDesktopScreenState extends State<HostDesktopScreen> {
  late final VmClient _client = widget.client ?? VmClient();
  late final RfbClient _rfb = widget.rfb ?? RfbClient();
  bool _checking = true;
  bool? _installed;
  VmException? _error;
  HostDisplay? _display;

  @override
  void initState() {
    super.initState();
    _check();
  }

  @override
  void dispose() {
    if (widget.rfb == null) _rfb.dispose();
    if (widget.client == null) _client.close();
    super.dispose();
  }

  Future<void> _check() async {
    setState(() {
      _checking = true;
      _error = null;
    });
    try {
      final installed = await _client.hostDesktopInstalled();
      if (!mounted) return;
      setState(() {
        _installed = installed;
        _checking = false;
      });
      if (installed) {
        unawaited(_rfb.connect());
        unawaited(_loadDisplay());
      }
    } on VmException catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e;
        _checking = false;
      });
    }
  }

  Future<void> _loadDisplay() async {
    try {
      final d = await _client.getHostDisplay();
      if (mounted) setState(() => _display = d);
    } on VmException {
      // The resolution menu says it couldn't read them.
    }
  }

  Future<void> _resolution(BuildContext sheetContext) async {
    final display = _display;
    final choice = await showModalBottomSheet<DisplayResolution>(
      context: sheetContext,
      showDragHandle: true,
      useSafeArea: true,
      isScrollControlled: true,
      builder: (context) => _ResolutionSheet(display: display, onRetry: _loadDisplay),
    );
    if (choice == null || !mounted) return;
    try {
      final d = await _client.setHostDisplay(choice.width, choice.height);
      if (!mounted) return;
      setState(() => _display = d);
      ScaffoldMessenger.maybeOf(context)
          ?.showSnackBar(SnackBar(content: Text('The server screen is now ${choice.width} × ${choice.height}')));
    } on VmException catch (e) {
      if (mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(e.message)));
    }
  }

  Widget? _placeholder() {
    if (_checking && _installed == null) {
      return const ConsolePlaceholder(icon: Icons.screen_share_outlined, title: '', message: '', loading: true);
    }
    final error = _error;
    if (error != null) {
      return ConsolePlaceholder(
        icon: error.kind == VmErrorKind.offline ? Icons.cloud_off_outlined : Icons.error_outline,
        title: error.kind == VmErrorKind.offline ? "Can't reach the server" : "Couldn't open the host desktop",
        message: error.message,
        actionLabel: 'Retry',
        onAction: _check,
      );
    }
    if (_installed == false) {
      return ConsolePlaceholder(
        icon: Icons.screen_share_outlined,
        title: "Host desktop isn't set up",
        message: "Turn on host desktop streaming in the NivaroOS web dashboard. Then you can use the server's own screen from here.",
        actionLabel: 'Check again',
        onAction: _check,
      );
    }
    return null;
  }

  @override
  Widget build(BuildContext context) {
    return RemoteConsoleFrame(
      client: _rfb,
      title: 'Host desktop',
      screenLabel: "The server's screen",
      clipboardTarget: RemoteClipboardHistory.hostTarget,
      history: widget.history,
      placeholder: _placeholder(),
      menuItems: [
        if (_installed == true)
          ConsoleMenuItem(icon: Icons.aspect_ratio_outlined, label: 'Screen resolution', onPressed: _resolution),
      ],
    );
  }
}

class _ResolutionSheet extends StatelessWidget {
  const _ResolutionSheet({required this.display, required this.onRetry});

  final HostDisplay? display;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    final d = display;
    final options = d?.resolutions ?? const <DisplayResolution>[];
    // This phone's real screen, for the sizes that keep its shape.
    final media = MediaQuery.of(context);
    final fits = phoneFitSizes(media.size * media.devicePixelRatio);
    return ListView(
      shrinkWrap: true,
      padding: const EdgeInsets.only(bottom: Space.lg),
      children: [
        Padding(
          padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.xs),
          child: Semantics(header: true, child: Text('Screen resolution', style: theme.textTheme.titleLarge)),
        ),
        Padding(
          padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.sm),
          child: Text(
            d == null ? "Couldn't read the server's screen modes." : 'The size of the server’s own screen. Now ${d.width} × ${d.height}.',
            style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
        ),
        if (d != null && fits.isNotEmpty) ...[
          const SectionHeader(title: 'Fit this phone'),
          Padding(
            padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.sm),
            child: Text(
              'Same shape as this screen held sideways, so the desktop fills it. Lower sizes stream faster on a slow connection.',
              style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
            ),
          ),
          for (final f in fits)
            Builder(builder: (context) {
              final current = f.width == d.width && f.height == d.height;
              return ListTile(
                leading: Icon(f.percent == 100 ? Icons.smartphone_outlined : Icons.aspect_ratio_outlined),
                title: Text('${f.width} × ${f.height}'),
                subtitle: Text(f.percent == 100 ? 'Sharpest · pixel for pixel' : '${f.percent}% · faster'),
                selected: current,
                trailing: current ? const Icon(Icons.check, semanticLabel: 'Current') : null,
                onTap: current ? null : () => Navigator.of(context).pop(DisplayResolution(width: f.width, height: f.height, label: '')),
              );
            }),
          const SectionHeader(title: "Server's screen modes"),
        ],
        if (d == null)
          Padding(
            padding: EdgeInsets.symmetric(horizontal: gutter),
            child: Align(
              alignment: AlignmentDirectional.centerStart,
              child: TextButton(
                onPressed: () {
                  Navigator.of(context).pop();
                  onRetry();
                },
                child: const Text('Retry'),
              ),
            ),
          )
        else if (options.isEmpty)
          Padding(
            padding: EdgeInsets.symmetric(horizontal: gutter, vertical: Space.sm),
            child: Text('This screen offers no other sizes.', style: theme.textTheme.bodyMedium),
          )
        else
          for (final r in options)
            Builder(builder: (context) {
              final current = r.width == d.width && r.height == d.height;
              return ListTile(
                leading: const Icon(Icons.desktop_windows_outlined),
                title: Text('${r.width} × ${r.height}'),
                subtitle: r.label.isNotEmpty && r.label != '${r.width} × ${r.height}' ? Text(r.label) : null,
                selected: current,
                trailing: current ? const Icon(Icons.check, semanticLabel: 'Current') : null,
                onTap: current ? null : () => Navigator.of(context).pop(r),
              );
            }),
      ],
    );
  }
}
