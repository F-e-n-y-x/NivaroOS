import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:url_launcher/url_launcher.dart';

import '../services/app_update_service.dart';
import '../ui/ui.dart';
import '../utils/format.dart';
import '../widgets/tailscale_modal.dart' show seenPhrase;

/// The update sheet: what's new, then download, check and install, with
/// each step's progress and a plain reason when one stops it.
class UpdateSheet extends StatefulWidget {
  const UpdateSheet({super.key, required this.release, this.installed = ''});

  final AppRelease release;

  /// The version on this phone now, for "You have …".
  final String installed;

  @override
  State<UpdateSheet> createState() => _UpdateSheetState();
}

enum _Step { idle, downloading, verifying, installing, handedOver }

class _UpdateSheetState extends State<UpdateSheet> {
  _Step _step = _Step.idle;
  int _received = 0;
  int _total = 0;
  UpdateException? _error;


  Future<void> _run() async {
    final svc = AppUpdateService.instance;
    setState(() {
      _error = null;
      _step = _Step.downloading;
      _received = 0;
    });
    try {
      final apk = await svc.download(widget.release, onProgress: (r, t) {
        if (mounted) {
          setState(() {
            _received = r;
            _total = t;
          });
        }
      });
      if (!mounted) return;
      setState(() => _step = _Step.verifying);
      await svc.verify(widget.release, apk);
      if (!mounted) return;
      setState(() => _step = _Step.installing);
      await svc.install(apk);
      if (mounted) setState(() => _step = _Step.handedOver);
    } on UpdateException catch (e) {
      if (mounted) {
        setState(() {
          _error = e;
          _step = _Step.idle;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final r = widget.release;
    final error = _error;
    final busy = _step == _Step.downloading || _step == _Step.verifying || _step == _Step.installing;
    // Same wording as the Updates row, from the same number.
    final size = r.apkSize > 0 ? formatBytes(r.apkSize) : null;

    final Widget action;
    if (error?.needsInstallPermission ?? false) {
      action = FilledButton(
        onPressed: () async {
          await AppUpdateService.instance.openInstallPermissionSettings();
          if (mounted) setState(() => _error = null);
        },
        child: const Text('Allow installs'),
      );
    } else if (_step == _Step.handedOver) {
      action = const FilledButton(onPressed: null, child: Text('Waiting for Android'));
    } else {
      action = FilledButton(
        onPressed: busy ? null : _run,
        child: Text(error == null ? 'Download and install' : 'Try again'),
      );
    }

    final status = switch (_step) {
      _Step.idle => null,
      _Step.downloading => _total > 0 ? 'Downloading · ${formatBytes(_received)} of ${formatBytes(_total)}' : 'Downloading…',
      _Step.verifying => 'Checking the download…',
      _Step.installing => 'Starting the install…',
      _Step.handedOver => 'Android asks you to confirm the update. The app restarts when it is done.',
    };

    return DraggableScrollableSheet(
      expand: false,
      initialChildSize: 0.7,
      maxChildSize: 0.95,
      builder: (context, controller) => ListView(
        controller: controller,
        padding: const EdgeInsets.fromLTRB(Space.xl, 0, Space.xl, Space.xl),
        children: [
          Semantics(header: true, child: Text('App version ${r.version}', style: theme.textTheme.headlineSmall)),
          const SizedBox(height: Space.xs),
          Text(
            [
              if (widget.installed.isNotEmpty) 'You have ${widget.installed}',
              ?size,
              if (r.publishedAt != null) 'Released ${seenPhrase(r.publishedAt!)}',
            ].join(' · '),
            style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
          ),
          const SizedBox(height: Space.xl),
          if (busy || status != null) ...[
            if (busy)
              LinearProgressIndicator(value: _step == _Step.downloading && _total > 0 ? (_received / _total).clamp(0.0, 1.0) : null),
            if (status != null) ...[
              const SizedBox(height: Space.sm),
              Semantics(liveRegion: true, child: Text(status, style: theme.textTheme.bodyMedium?.tabular)),
            ],
            const SizedBox(height: Space.lg),
          ],
          if (error != null) ...[
            Semantics(
              liveRegion: true,
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(error.needsInstallPermission ? Icons.info_outline : Icons.error_outline,
                      size: 20, color: error.needsInstallPermission ? scheme.onSurfaceVariant : scheme.error),
                  const SizedBox(width: Space.md),
                  Expanded(
                    child: Text(error.message,
                        style: theme.textTheme.bodyMedium?.copyWith(color: error.needsInstallPermission ? scheme.onSurface : scheme.error)),
                  ),
                ],
              ),
            ),
            const SizedBox(height: Space.lg),
          ],
          SizedBox(width: double.infinity, height: 48, child: action),
          const SizedBox(height: Space.xl),
          Text("What's new", style: theme.textTheme.titleMedium),
          const SizedBox(height: Space.sm),
          if (r.notes.trim().isEmpty)
            Text('This release has no notes.', style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant))
          else
            MarkdownBody(
              data: r.notes,
              styleSheet: MarkdownStyleSheet.fromTheme(theme),
              onTapLink: (text, href, title) {
                if (href != null) launchUrl(Uri.parse(href), mode: LaunchMode.externalApplication);
              },
            ),
          if (r.pageUrl.isNotEmpty) ...[
            const SizedBox(height: Space.lg),
            Align(
              alignment: AlignmentDirectional.centerStart,
              child: TextButton.icon(
                onPressed: () => launchUrl(Uri.parse(r.pageUrl), mode: LaunchMode.externalApplication),
                icon: const Icon(Icons.open_in_new_outlined),
                label: const Text('Release page'),
              ),
            ),
          ],
        ],
      ),
    );
  }
}
