/// Share links (plan WP1-19): the server's Quick Share
/// (services/core/route/v1/quickshare.go). "Share link…" on a server file
/// or folder makes one - one-time or not, an expiry, an optional password -
/// then copies it, hands it to Android's share sheet or shows it as a QR
/// code. "Shared links" lists the live ones with Revoke.
library;

import 'dart:io';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:qr/qr.dart';

import '../../backup/backup_strings.dart' show formatSeconds;
import '../../models/file_entry.dart';
import '../../services/api_client.dart';
import '../../services/share_intent.dart';
import '../../ui/ui.dart';

/// One link, as `quickShareItem` in the core's quickshare.go.
class QuickShare {
  const QuickShare({
    required this.id,
    required this.name,
    required this.path,
    required this.url,
    this.expiresAt,
    this.maxDownloads = 0,
    this.downloads = 0,
    this.hasPassword = false,
    this.isDir = false,
  });

  final String id;
  final String name;
  final String path;
  final String url;

  /// Null: never expires.
  final DateTime? expiresAt;

  /// 0 = unlimited; 1 = one-time.
  final int maxDownloads;
  final int downloads;
  final bool hasPassword;
  final bool isDir;

  int? get remaining => maxDownloads == 0 ? null : (maxDownloads - downloads).clamp(0, maxDownloads);

  factory QuickShare.fromJson(Map<String, dynamic> j) {
    final exp = (j['expires_at'] as num?)?.toInt() ?? 0;
    return QuickShare(
      id: j['id']?.toString() ?? '',
      name: j['name']?.toString() ?? '',
      path: j['path']?.toString() ?? '',
      url: j['url']?.toString() ?? '',
      expiresAt: exp > 0 ? DateTime.fromMillisecondsSinceEpoch(exp * 1000) : null,
      maxDownloads: (j['max_downloads'] as num?)?.toInt() ?? 0,
      downloads: (j['downloads'] as num?)?.toInt() ?? 0,
      hasPassword: j['has_password'] == true,
      isDir: j['is_dir'] == true,
    );
  }

  /// "Expires in 6 days · One-time · Password".
  String get summary {
    final exp = expiresAt;
    final left = exp?.difference(clock.now());
    return [
      exp == null ? 'Never expires' : (left!.isNegative ? 'Expired' : 'Expires in ${formatSeconds(left.inSeconds)}'),
      if (maxDownloads == 1) 'One-time' else if (remaining != null) '$remaining downloads left',
      if (hasPassword) 'Password',
    ].join(' · ');
  }
}

/// The expiry choices the server accepts.
enum ShareExpiry {
  hour('1h', '1 hour'),
  day('1d', '1 day'),
  week('7d', '7 days'),
  never('never', 'Never');

  const ShareExpiry(this.code, this.label);
  final String code;
  final String label;
}

class QuickShareApi {
  QuickShareApi([ApiClient? client]) : _c = client ?? ApiClient.instance;
  final ApiClient _c;

  Future<List<QuickShare>> list() async {
    final d = (await _c.get('/quickshare'))['data'];
    return [for (final e in d is List ? d : const []) if (e is Map<String, dynamic>) QuickShare.fromJson(e)];
  }

  /// Makes a link. A server too old for one-time or password links would
  /// make a plain public one instead, so that one is revoked at once and
  /// this throws.
  Future<QuickShare> create(String path, {required ShareExpiry expiry, bool oneTime = false, String password = ''}) async {
    final d = (await _c.post('/quickshare', body: {
      'path': path,
      'expiry': expiry.code,
      if (oneTime) 'max_downloads': 1,
      if (password.isNotEmpty) 'password': password,
    }))['data'];
    if (d is! Map<String, dynamic>) throw ApiException("The server didn't return a link.");
    final s = QuickShare.fromJson(d);
    if ((oneTime && s.maxDownloads != 1) || (password.isNotEmpty && !s.hasPassword)) {
      await revoke(s.id);
      throw ApiException('This server needs a NivaroOS update for one-time and password links.');
    }
    return s;
  }

  Future<void> revoke(String id) => _c.delete('/quickshare/${Uri.encodeComponent(id)}');
}

/// True when [url]'s host only answers on the home network or the
/// tailnet: a private, link-local or Tailscale (100.64/10) address, a
/// `.local`/`.lan`/`.ts.net` name, or a bare machine name.
bool isLocalOnlyLink(String url) {
  final host = (Uri.tryParse(url)?.host ?? '').toLowerCase();
  if (host.isEmpty || host == 'localhost') return true;
  final ip = InternetAddress.tryParse(host);
  if (ip != null) {
    if (ip.isLoopback || ip.isLinkLocal) return true;
    if (ip.type == InternetAddressType.IPv6) return (ip.rawAddress[0] & 0xfe) == 0xfc;
    final [a, b, ...] = ip.rawAddress;
    return a == 10 || (a == 172 && b >= 16 && b < 32) || (a == 192 && b == 168) || (a == 100 && b >= 64 && b < 128);
  }
  return !host.contains('.') || RegExp(r'\.(local|lan|home|internal|home\.arpa|ts\.net)$').hasMatch(host);
}

/// "Share link…" on [entry]: pick the options, make the link, then
/// copy / share / QR it.
Future<void> showShareLinkSheet(BuildContext context, FileEntry entry, {QuickShareApi? api}) => showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (_) => ShareLinkSheet(entry: entry, api: api),
    );

class ShareLinkSheet extends StatefulWidget {
  const ShareLinkSheet({super.key, required this.entry, this.api});

  final FileEntry entry;
  final QuickShareApi? api;

  @override
  State<ShareLinkSheet> createState() => _ShareLinkSheetState();
}

class _ShareLinkSheetState extends State<ShareLinkSheet> {
  late final QuickShareApi _api = widget.api ?? QuickShareApi();
  bool _oneTime = false;
  ShareExpiry _expiry = ShareExpiry.day;
  bool _usePassword = false;
  final _password = TextEditingController();
  bool _busy = false;
  String? _error;
  QuickShare? _link;

  @override
  void dispose() {
    _password.dispose();
    super.dispose();
  }

  Future<void> _create() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final s = await _api.create(widget.entry.path, expiry: _expiry, oneTime: _oneTime, password: _usePassword ? _password.text : '');
      if (mounted) setState(() => _link = s);
    } catch (e) {
      if (mounted) setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    final link = _link;
    return SafeArea(
      child: Padding(
        padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
        child: SingleChildScrollView(
          padding: EdgeInsets.fromLTRB(gutter, 0, gutter, Space.lg),
          child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
            Text('Share link', style: theme.textTheme.titleLarge),
            const SizedBox(height: Space.xs),
            Text(widget.entry.isDir ? '${widget.entry.name} (as a ZIP)' : widget.entry.name,
                maxLines: 2, overflow: TextOverflow.ellipsis, style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
            const SizedBox(height: Space.md),
            if (link != null) LinkActions(link: link) else ..._options(theme),
          ]),
        ),
      ),
    );
  }

  List<Widget> _options(ThemeData theme) => [
        SwitchListTile(
          contentPadding: EdgeInsets.zero,
          title: const Text('One-time link'),
          subtitle: const Text('Stops working after one download'),
          value: _oneTime,
          onChanged: _busy ? null : (v) => setState(() => _oneTime = v),
        ),
        const SizedBox(height: Space.sm),
        Text('Expires in', style: theme.textTheme.titleSmall),
        const SizedBox(height: Space.sm),
        Wrap(spacing: Space.sm, runSpacing: Space.sm, children: [
          for (final e in ShareExpiry.values)
            ChoiceChip(label: Text(e.label), selected: _expiry == e, onSelected: _busy ? null : (_) => setState(() => _expiry = e)),
        ]),
        const SizedBox(height: Space.sm),
        SwitchListTile(
          contentPadding: EdgeInsets.zero,
          title: const Text('Password'),
          subtitle: const Text('Asked before the download starts'),
          value: _usePassword,
          onChanged: _busy ? null : (v) => setState(() => _usePassword = v),
        ),
        if (_usePassword)
          TextField(
            controller: _password,
            obscureText: true,
            autocorrect: false,
            enableSuggestions: false,
            maxLength: 72,
            onChanged: (_) => setState(() {}),
            decoration: const InputDecoration(labelText: 'Password'),
          ),
        if (_error != null) ...[
          const SizedBox(height: Space.sm),
          Text(_error!, style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.error)),
        ],
        const SizedBox(height: Space.lg),
        FilledButton(
          onPressed: _busy || (_usePassword && _password.text.isEmpty) ? null : _create,
          child: const Text('Create link'),
        ),
      ];
}

/// A made link: the URL, a warning when it only works at home, then Copy,
/// Share and QR code.
class LinkActions extends StatefulWidget {
  const LinkActions({super.key, required this.link, this.onRevoke});

  final QuickShare link;
  final VoidCallback? onRevoke;

  @override
  State<LinkActions> createState() => _LinkActionsState();
}

class _LinkActionsState extends State<LinkActions> {
  bool _qr = false;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final l = widget.link;
    final host = Uri.tryParse(l.url)?.host ?? '';
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
      SelectableText(l.url, style: theme.textTheme.bodyLarge?.copyWith(fontFamily: 'monospace')),
      const SizedBox(height: Space.xs),
      Text(l.summary, style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
      if (isLocalOnlyLink(l.url)) ...[
        const SizedBox(height: Space.md),
        Notice(
          icon: Icons.wifi_outlined,
          message: 'This link uses $host, so it only works on your home network or over Tailscale. '
              "To share it with anyone, connect the app through the server's public address (a Cloudflare tunnel, for example) and make the link there.",
        ),
      ],
      const SizedBox(height: Space.md),
      Wrap(spacing: Space.sm, runSpacing: Space.sm, children: [
        FilledButton.tonalIcon(
          icon: const Icon(Icons.copy_outlined),
          label: const Text('Copy link'),
          onPressed: () async {
            await Clipboard.setData(ClipboardData(text: l.url));
            if (context.mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(const SnackBar(content: Text('Link copied')));
          },
        ),
        FilledButton.tonalIcon(
          icon: const Icon(Icons.share_outlined),
          label: const Text('Share'),
          onPressed: () => ShareIntent.shareText(l.url, title: l.name),
        ),
        FilledButton.tonalIcon(
          icon: const Icon(Icons.qr_code_2),
          label: Text(_qr ? 'Hide QR code' : 'QR code'),
          onPressed: () => setState(() => _qr = !_qr),
        ),
        if (widget.onRevoke != null)
          TextButton.icon(
            style: TextButton.styleFrom(foregroundColor: theme.colorScheme.error),
            icon: const Icon(Icons.link_off),
            label: const Text('Revoke'),
            onPressed: widget.onRevoke,
          ),
      ]),
      if (_qr) ...[
        const SizedBox(height: Space.lg),
        Center(child: QrCodeView(data: l.url, size: 220)),
      ],
    ]);
  }
}

/// [data] as a QR code: always dark on white, with the quiet zone, so a
/// camera reads it in a black theme too.
class QrCodeView extends StatelessWidget {
  const QrCodeView({super.key, required this.data, this.size = 200});

  final String data;
  final double size;

  @override
  Widget build(BuildContext context) {
    final image = QrImage(QrCode(payload: QrPayload.fromString(data)));
    return Semantics(
      label: 'QR code of the link',
      image: true,
      child: Container(
        color: Colors.white,
        padding: EdgeInsets.all(size / 12),
        child: CustomPaint(size: Size.square(size), painter: _QrPainter(image)),
      ),
    );
  }
}

class _QrPainter extends CustomPainter {
  _QrPainter(this.image);
  final QrImage image;

  @override
  void paint(Canvas canvas, Size size) {
    final n = image.moduleCount;
    final cell = size.width / n;
    final paint = Paint()..color = Colors.black;
    for (var y = 0; y < n; y++) {
      for (var x = 0; x < n; x++) {
        // A hair over one cell, so neighbours never show a seam.
        if (image.isDark(y, x)) canvas.drawRect(Rect.fromLTWH(x * cell, y * cell, cell + 0.5, cell + 0.5), paint);
      }
    }
  }

  @override
  bool shouldRepaint(_QrPainter old) => old.image != image;
}

/// Every live link, newest first; tap for its actions, Revoke to end it
/// (the link then answers 404).
class SharedLinksScreen extends StatefulWidget {
  const SharedLinksScreen({super.key, this.api});

  final QuickShareApi? api;

  @override
  State<SharedLinksScreen> createState() => _SharedLinksScreenState();
}

class _SharedLinksScreenState extends State<SharedLinksScreen> {
  late final QuickShareApi _api = widget.api ?? QuickShareApi();
  List<QuickShare>? _links;
  Object? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final l = await _api.list();
      if (mounted) {
        setState(() {
          _links = l;
          _error = null;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _revoke(QuickShare s) async {
    final ok = await ConfirmDialog.destructive(context,
        title: 'Revoke link', message: 'The link to ${s.name} stops working for everyone who has it', confirmLabel: 'Revoke');
    if (!ok || !mounted) return;
    try {
      await _api.revoke(s.id);
      if (mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text('Link to ${s.name} revoked')));
    } catch (e) {
      if (mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text("Couldn't revoke the link: $e")));
    }
    await _load();
  }

  Future<void> _open(QuickShare s) => showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        showDragHandle: true,
        builder: (context) => SafeArea(
          child: SingleChildScrollView(
            padding: EdgeInsets.fromLTRB(Space.gutter(context), 0, Space.gutter(context), Space.lg),
            child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
              Text(s.name, style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: Space.md),
              LinkActions(
                link: s,
                onRevoke: () {
                  Navigator.of(context).pop();
                  _revoke(s);
                },
              ),
            ]),
          ),
        ),
      );

  @override
  Widget build(BuildContext context) {
    final links = _links;
    final List<Widget> slivers;
    if (links == null && _error != null) {
      final e = _error!;
      slivers = [
        e is ApiException && e.isUnreachable
            ? ErrorState.offline(onRetry: _load, sliver: true)
            : ErrorState(title: "Couldn't load the links", message: e.toString(), onRetry: _load, sliver: true),
      ];
    } else if (links == null) {
      slivers = const [SliverLoadingList(rows: 4, trailing: true)];
    } else if (links.isEmpty) {
      slivers = const [
        EmptyState(
          sliver: true,
          icon: Icons.link,
          title: 'No shared links',
          message: 'In Files, open the menu of a file or folder and choose Share link.',
        ),
      ];
    } else {
      slivers = [
        SliverList.list(children: [
          TileGroup(children: [
            for (final s in links)
              ListTile(
                leading: Icon(s.isDir ? Icons.folder_zip_outlined : Icons.insert_drive_file_outlined),
                title: Text(s.name, maxLines: 1, overflow: TextOverflow.ellipsis),
                subtitle: Text(s.summary),
                onTap: () => _open(s),
                trailing: IconButton(icon: const Icon(Icons.link_off), tooltip: 'Revoke link to ${s.name}', onPressed: () => _revoke(s)),
              ),
          ]),
        ]),
      ];
    }
    return AppScaffold.slivers(title: 'Shared links', onRefresh: _load, slivers: slivers);
  }
}
