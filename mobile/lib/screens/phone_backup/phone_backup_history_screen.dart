import 'dart:async';

import 'package:flutter/material.dart';
import 'package:open_filex/open_filex.dart';

import '../../backup/backup_strings.dart' show formatDateTime, formatNumber;
import '../../phone_backup/pb_client.dart';
import '../../phone_backup/pb_models.dart';
import '../../phone_backup/pb_platform.dart';
import '../../phone_backup/pb_restore.dart';
import '../../phone_backup/pb_service.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import '../backup/backup_widgets.dart';
import 'phone_backup_common.dart';
import 'phone_backup_messages_restore.dart';

String _phoneErrorText(Object e) {
  if (e is PhoneBackupError) {
    if (e.revoked) return 'The server no longer accepts this phone. Link it again.';
    if (e.driveMissing) return 'The drive that holds this phone’s backups isn’t connected to the server.';
    if (e.unreachable) return 'The server can’t be reached.';
    return e.message.isNotEmpty ? e.message : e.code;
  }
  return e.toString();
}

/// Every backup of this phone (snapshots, newest first) and the backup as
/// it is now; pick one to browse it and get things back.
class PhoneBackupHistoryScreen extends StatefulWidget {
  const PhoneBackupHistoryScreen({super.key, this.service});
  final PhoneBackupService? service;

  @override
  State<PhoneBackupHistoryScreen> createState() => _PhoneBackupHistoryScreenState();
}

class _PhoneBackupHistoryScreenState extends State<PhoneBackupHistoryScreen> {
  late final PhoneBackupService _service = widget.service ?? PhoneBackupService.instance;
  List<PhoneSnapshot>? _snaps;
  Object? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final c = await _service.deviceClient();
      if (c == null) throw PhoneBackupError('unauthorized', status: 401);
      final s = await c.snapshots();
      if (mounted) setState(() => _snaps = s);
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  void _open(String id, String title, List<String> cats) =>
      Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => PhoneSnapshotScreen(service: widget.service, snapshot: id, title: title, categories: cats)));

  @override
  Widget build(BuildContext context) {
    final snaps = _snaps;
    final Widget body;
    if (snaps == null && _error != null) {
      body = ErrorState(title: 'Couldn’t load the backups', message: _phoneErrorText(_error!), onRetry: _load);
    } else if (snaps == null) {
      body = const LoadingList(rows: 6);
    } else {
      body = RefreshIndicator(
        onRefresh: _load,
        child: ListView(children: [
          TileGroup(children: [
            ListTile(
              leading: const Icon(Icons.update),
              title: const Text('Latest'),
              subtitle: const Text('Everything backed up so far, as it is now'),
              trailing: const Icon(Icons.chevron_right),
              onTap: () => _open('latest', 'Latest', [for (final c in PhoneCategory.values) c.wire]),
            ),
          ]),
          if (snaps.isEmpty)
            const PhoneBackupHint('No finished backup yet. The first one appears here when it’s done.')
          else
            TileGroup(title: 'Backups', children: [
              for (final s in snaps)
                ListTile(
                  leading: Icon(s.status == 'partial' ? Icons.warning_amber_outlined : Icons.check_circle_outline,
                      color: StatusColors.toneOf(context, s.status == 'partial' ? Status.warning : Status.success).color),
                  title: Text(s.takenAt == null ? s.id : formatDateTime(s.takenAt!)),
                  subtitle: Text([
                    s.categories.map((c) => PhoneCategory.fromWire(c)?.label ?? c).join(', '),
                    if (s.counts.uploaded > 0) '${formatNumber(s.counts.uploaded)} new',
                    if (s.counts.bytes > 0) formatSize(s.counts.bytes),
                    if (s.status == 'partial') 'with problems',
                  ].join(' · ')),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => _open(s.id, s.takenAt == null ? s.id : formatDateTime(s.takenAt!), s.categories),
                ),
            ]),
          const SizedBox(height: Space.xl),
        ]),
      );
    }
    return AppScaffold(title: 'Backups and restore', body: body);
  }
}

/// One backup: its files categories to browse, and its exports to import.
class PhoneSnapshotScreen extends StatefulWidget {
  const PhoneSnapshotScreen({super.key, this.service, required this.snapshot, required this.title, required this.categories});
  final PhoneBackupService? service;
  final String snapshot;
  final String title;
  final List<String> categories;

  @override
  State<PhoneSnapshotScreen> createState() => _PhoneSnapshotScreenState();
}

class _PhoneSnapshotScreenState extends State<PhoneSnapshotScreen> {
  late final PhoneBackupService _service = widget.service ?? PhoneBackupService.instance;
  List<PhoneExport>? _exports;
  Object? _error;
  String _busy = '';

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final c = await _service.deviceClient();
      final e = await c!.exports(snapshot: widget.snapshot);
      if (mounted) setState(() => _exports = e);
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<PhoneRestore> _restore() async => PhoneRestore((await _service.deviceClient())!, await _service.store);

  Future<void> _import(PhoneExport e) async {
    setState(() => _busy = e.id);
    try {
      final f = await (await _restore()).downloadExport(e);
      final mime = e.category == 'contacts' ? 'text/x-vcard' : 'text/calendar';
      final r = await OpenFilex.open(f.path, type: mime);
      if (!mounted) return;
      if (r.type != ResultType.done) {
        final saved = await ChannelPhoneSources.saveToDownloads(tmpPath: f.path, subPath: 'Exports', name: f.path.split('/').last.replaceFirst(RegExp(r'^[0-9a-f]{16}-'), ''));
        if (mounted) showBackupSnack(context, saved == null ? 'No app on this phone opens this file.' : 'No app opened it; saved to Download/NivaroOS restore/Exports.');
      }
    } catch (err) {
      if (mounted) showBackupSnack(context, 'Couldn’t get it: ${_phoneErrorText(err)}');
    } finally {
      if (mounted) setState(() => _busy = '');
    }
  }

  Future<void> _saveExport(PhoneExport e) async {
    setState(() => _busy = e.id);
    try {
      final f = await (await _restore()).downloadExport(e);
      await ChannelPhoneSources.saveToDownloads(tmpPath: f.path, subPath: 'Exports', name: f.path.split('/').last.replaceFirst(RegExp(r'^[0-9a-f]{16}-'), ''));
      if (f.existsSync()) f.deleteSync();
      if (mounted) showBackupSnack(context, 'Saved to Download/NivaroOS restore/Exports.');
    } catch (err) {
      if (mounted) showBackupSnack(context, 'Couldn’t get it: ${_phoneErrorText(err)}');
    } finally {
      if (mounted) setState(() => _busy = '');
    }
  }

  void _browse(PhoneCategory c) => Navigator.of(context).push(MaterialPageRoute<void>(
      builder: (_) => PhoneBrowseScreen(service: widget.service, snapshot: widget.snapshot, category: c.wire, path: '', title: c.label)));

  void _messages(String cat) => Navigator.of(context).push(MaterialPageRoute<void>(
      builder: (_) => PhoneMessagesRestoreScreen(service: widget.service, snapshot: widget.snapshot, category: cat)));

  @override
  Widget build(BuildContext context) {
    final ex = _exports;
    final cats = widget.categories.toSet();
    final filesCats = [for (final c in PhoneCategory.values) if (c.isFiles && cats.contains(c.wire)) c];
    Widget exportRow(PhoneExport e) {
      final busy = _busy == e.id;
      final (title, action, onTap) = switch (e.category) {
        'contacts' => ('Contacts', 'Import', () => _import(e)),
        'calendar' => (e.name.isEmpty ? 'Calendar' : e.name, 'Import', () => _import(e)),
        'apps' => ('App list', 'Save', () => _saveExport(e)),
        'settings' => ('Phone settings', 'Save', () => _saveExport(e)),
        _ => (e.category, 'Save', () => _saveExport(e)),
      };
      final unit = switch (e.category) {
        'contacts' => e.items == 1 ? 'contact' : 'contacts',
        'calendar' => e.items == 1 ? 'event' : 'events',
        'apps' => e.items == 1 ? 'app' : 'apps',
        _ => e.items == 1 ? 'item' : 'items',
      };
      return ListTile(
        leading: Icon(categoryInfo[PhoneCategory.fromWire(e.category)]?.icon ?? Icons.description_outlined),
        title: Text(title),
        subtitle: Text([if (e.items > 0) '${formatNumber(e.items)} $unit', formatSize(e.size)].join(' · ')),
        trailing: busy
            ? const SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2))
            : TextButton(onPressed: onTap, child: Text(action, semanticsLabel: '$action $title')),
      );
    }

    final plain = (ex ?? const <PhoneExport>[]).where((e) => e.category != 'sms' && e.category != 'calllog').toList();
    final hasSms = (ex ?? const []).any((e) => e.category == 'sms');
    final hasCalls = (ex ?? const []).any((e) => e.category == 'calllog');
    return AppScaffold(
      title: widget.title,
      body: ListView(children: [
        if (filesCats.isNotEmpty)
          TileGroup(title: 'Files', children: [
            for (final c in filesCats)
              ListTile(
                leading: Icon(categoryInfo[c]!.icon),
                title: Text(c.label),
                subtitle: const Text('Browse, and get files back'),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => _browse(c),
              ),
          ]),
        if (ex == null && _error == null) const Padding(padding: EdgeInsets.all(Space.lg), child: LinearProgressIndicator()),
        if (_error != null) PhoneBackupHint('Couldn’t load the rest of this backup: ${_phoneErrorText(_error!)}'),
        if (plain.isNotEmpty) TileGroup(title: 'Contacts, calendars and more', children: [for (final e in plain) exportRow(e)]),
        if (hasSms || hasCalls)
          TileGroup(title: 'Messages and calls', children: [
            if (hasSms)
              ListTile(
                leading: const Icon(Icons.sms_outlined),
                title: const Text('Messages'),
                subtitle: const Text('Put them back into this phone'),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => _messages('sms'),
              ),
            if (hasCalls)
              ListTile(
                leading: const Icon(Icons.call_outlined),
                title: const Text('Call log'),
                subtitle: const Text('Put it back into this phone'),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => _messages('calllog'),
              ),
          ]),
        const SizedBox(height: Space.xl),
      ]),
    );
  }
}

/// A folder of a backup's files; files deleted on the phone are shown
/// (and restorable) too.
class PhoneBrowseScreen extends StatefulWidget {
  const PhoneBrowseScreen({super.key, this.service, required this.snapshot, required this.category, required this.path, required this.title});
  final PhoneBackupService? service;
  final String snapshot;
  final String category;
  final String path;
  final String title;

  @override
  State<PhoneBrowseScreen> createState() => _PhoneBrowseScreenState();
}

class _PhoneBrowseScreenState extends State<PhoneBrowseScreen> {
  late final PhoneBackupService _service = widget.service ?? PhoneBackupService.instance;
  PhoneBrowseResult? _r;
  Object? _error;
  final Set<String> _busy = {};

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final c = await _service.deviceClient();
      final r = await c!.browse(widget.snapshot, widget.category, widget.path);
      if (mounted) setState(() => _r = r);
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<PhoneRestore> _restore() async => PhoneRestore((await _service.deviceClient())!, await _service.store);

  Future<void> _restoreOne(PhoneBrowseEntry e) async {
    setState(() => _busy.add(e.path));
    try {
      await (await _restore()).restoreFile(widget.snapshot, widget.category, e.path, sha256: e.sha256, takenAt: e.takenAt, mtime: e.mtime);
      if (mounted) {
        showBackupSnack(context, widget.category == 'media' ? '${e.name} is back in ${mediaTarget(e.path).relativePath}' : '${e.name} saved to Download/NivaroOS restore');
      }
    } catch (err) {
      if (mounted) showBackupSnack(context, 'Couldn’t restore ${e.name}: ${_phoneErrorText(err)}');
    } finally {
      if (mounted) setState(() => _busy.remove(e.path));
    }
  }

  Future<void> _restoreMissing() async {
    final ok = await ConfirmDialog.confirm(
      context,
      title: 'Restore everything missing?',
      message: widget.category == 'media'
          ? 'Every photo and video in this backup that isn’t on the phone (by album, name and size) is downloaded back into its album, including ones deleted on the phone.'
          : 'Every file of this backup is downloaded into Download/NivaroOS restore.',
      confirmLabel: 'Restore',
    );
    if (!ok || !mounted) return;
    final restore = await _restore();
    if (!mounted) return;
    final progress = ValueNotifier<(int, int, RestoreSummary?)>((0, 0, null));
    var stop = false;
    final done = restore.restoreMissing(widget.snapshot, widget.category, onProgress: (d, t, s) => progress.value = (d, t, s), shouldStop: () => stop);
    unawaited(showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (context) => AlertDialog(
        title: const Text('Restoring'),
        content: ValueListenableBuilder<(int, int, RestoreSummary?)>(
          valueListenable: progress,
          builder: (context, v, _) => Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
            LinearProgressIndicator(value: v.$2 == 0 ? null : v.$1 / v.$2),
            const SizedBox(height: Space.md),
            Text(v.$2 == 0 ? 'Getting the list…' : '${formatNumber(v.$1)} of ${formatNumber(v.$2)}${v.$3 != null && v.$3!.skipped > 0 ? ' · ${formatNumber(v.$3!.skipped)} already on the phone' : ''}'),
          ]),
        ),
        actions: [TextButton(onPressed: () => stop = true, child: const Text('Stop'))],
      ),
    ));
    try {
      final s = await done;
      if (!mounted) return;
      Navigator.of(context).pop();
      showBackupSnack(context, 'Restored ${formatNumber(s.restored)}${s.skipped > 0 ? ', ${formatNumber(s.skipped)} already there' : ''}${s.failed > 0 ? ', ${formatNumber(s.failed)} failed' : ''}.');
    } catch (err) {
      if (!mounted) return;
      Navigator.of(context).pop();
      showBackupSnack(context, 'Restore stopped: ${_phoneErrorText(err)}');
    }
  }

  @override
  Widget build(BuildContext context) {
    final r = _r;
    final theme = Theme.of(context);
    final Widget body;
    if (r == null && _error != null) {
      body = ErrorState(title: 'Couldn’t open this folder', message: _phoneErrorText(_error!), onRetry: _load);
    } else if (r == null) {
      body = const LoadingList(rows: 8);
    } else if (r.entries.isEmpty) {
      body = const EmptyState(icon: Icons.folder_open_outlined, title: 'Nothing here', message: 'This folder of the backup is empty.');
    } else {
      body = ListView(children: [
        for (final e in r.entries)
          e.dir
              ? ListTile(
                  leading: Icon(Icons.folder_outlined, color: theme.colorScheme.primary),
                  title: Text(e.name),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => Navigator.of(context).push(MaterialPageRoute<void>(
                      builder: (_) => PhoneBrowseScreen(service: widget.service, snapshot: widget.snapshot, category: widget.category, path: e.path, title: e.name))),
                )
              : ListTile(
                  leading: Icon(_icon(e.name)),
                  title: Text(e.name),
                  subtitle: Text([
                    formatSize(e.size),
                    if (e.mtime > 0) formatDateTime(DateTime.fromMillisecondsSinceEpoch(e.mtime)),
                    if (e.deletedOnDeviceAt != null) 'Deleted on the phone',
                    if (e.version) 'Older version',
                  ].join(' · ')),
                  trailing: _busy.contains(e.path)
                      ? const SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2))
                      : IconButton(tooltip: 'Restore ${e.name}', icon: const Icon(Icons.download_outlined), onPressed: () => _restoreOne(e)),
                ),
        if (r.truncated) const PhoneBackupHint('Only the first 2,000 entries are shown.'),
        const SizedBox(height: Space.xl),
      ]);
    }
    return AppScaffold(
      title: widget.title,
      actions: [
        if (widget.path.isEmpty) IconButton(tooltip: 'Restore everything missing', icon: const Icon(Icons.restore), onPressed: r == null ? null : _restoreMissing),
      ],
      body: body,
    );
  }

  static IconData _icon(String name) {
    final ext = name.split('.').last.toLowerCase();
    return switch (ext) {
      'jpg' || 'jpeg' || 'png' || 'heic' || 'webp' || 'gif' || 'dng' => Icons.image_outlined,
      'mp4' || 'mov' || 'mkv' || '3gp' || 'webm' => Icons.movie_outlined,
      'apk' => Icons.android_outlined,
      _ => Icons.insert_drive_file_outlined,
    };
  }
}

