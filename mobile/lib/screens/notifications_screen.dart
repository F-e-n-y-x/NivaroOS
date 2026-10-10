import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../models/server_health.dart' show DriveHealth;
import '../services/api_client.dart';
import '../services/notification_feed.dart';
import '../services/permission_service.dart';
import '../services/storage_service.dart';
import '../ui/ui.dart';
import 'apps_screen.dart' show AppsApi, AppDetailScreen;
import 'backup/backup_run_screen.dart';
import 'backup/backup_screen.dart';
import 'dashboard_screen.dart' show StatusDisc;
import 'download_station/download_station_screen.dart';
import 'download_station/torrent_detail_screen.dart';
import 'drive_health_screen.dart';

/// The server's notification feed (Home's bell, More): newest first in
/// day groups, unread ones marked, tap to open what it is about. Settings
/// (which kinds also notify on this phone) are behind the tune icon.
class NotificationsScreen extends StatefulWidget {
  const NotificationsScreen({super.key, this.feed});

  final NotificationFeed? feed;

  @override
  State<NotificationsScreen> createState() => _NotificationsScreenState();
}

class _NotificationsScreenState extends State<NotificationsScreen> {
  late final NotificationFeed _feed = widget.feed ?? NotificationFeed.instance;
  List<FeedItem>? _items;
  bool _hasMore = false;
  int _latest = 0;
  Object? _error;
  bool _loadingMore = false;
  PermissionState? _permission;

  @override
  void initState() {
    super.initState();
    _feed.changed.addListener(_load);
    _load();
    _checkPermission();
  }

  @override
  void dispose() {
    _feed.changed.removeListener(_load);
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final page = await _feed.load();
      if (!mounted) return;
      setState(() {
        _items = page.items;
        _hasMore = page.hasMore;
        _latest = page.latestId;
        _error = null;
      });
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _more() async {
    final items = _items;
    if (items == null || items.isEmpty || _loadingMore) return;
    setState(() => _loadingMore = true);
    try {
      final page = await _feed.load(before: items.last.id);
      if (mounted) {
        setState(() {
          _items = [...items, ...page.items];
          _hasMore = page.hasMore;
        });
      }
    } catch (e) {
      _snack("Couldn't load more. ${e is ApiException ? e.message : ''}");
    } finally {
      if (mounted) setState(() => _loadingMore = false);
    }
  }

  Future<void> _checkPermission() async {
    final s = await PermissionService.notificationState();
    if (mounted) setState(() => _permission = s);
  }

  // Asked here, where the user is looking at what it's for - never at launch.
  Future<void> _turnOnPhone() async {
    if (_permission == PermissionState.askable && !await StorageService.instance.getNotificationsAsked()) {
      await PermissionService.requestNotifications();
    } else {
      await PermissionService.openSettings();
    }
    await _checkPermission();
  }

  Future<void> _markAll() async {
    final items = _items;
    if (items == null) return;
    setState(() => _items = [for (final i in items) i.read ? i : i.asRead()]);
    try {
      await _feed.markAllRead(_latest);
    } catch (e) {
      _snack("Couldn't mark them read. ${e is ApiException ? e.message : ''}");
      unawaited(_load());
    }
  }

  Future<void> _open(FeedItem item) async {
    if (!item.read) {
      setState(() => _items = [for (final i in _items ?? const <FeedItem>[]) i.id == item.id ? i.asRead() : i]);
      unawaited(_feed.markRead([item.id]).catchError((Object _) {}));
    }
    await openFeedTarget(context, item);
  }

  void _snack(String text) => ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(text.trim())));

  @override
  Widget build(BuildContext context) {
    final items = _items;
    final unread = items?.where((i) => !i.read).length ?? 0;
    return AppScaffold.slivers(
      title: 'Notifications',
      onRefresh: _load,
      actions: [
        if (unread > 0) IconButton(tooltip: 'Mark all read', icon: const Icon(Icons.done_all), onPressed: _markAll),
        IconButton(
          tooltip: 'Notification settings',
          icon: const Icon(Icons.tune),
          onPressed: () => showFeedSettings(context, permission: _permission, onTurnOn: _turnOnPhone),
        ),
      ],
      slivers: [
        if (items == null && _error == null)
          const SliverLoadingList(rows: 5)
        else if (items == null)
          ErrorState(
            sliver: true,
            title: "Couldn't load notifications",
            message: _error is ApiException ? (_error as ApiException).message : "The server didn't answer.",
            onRetry: _load,
          )
        else if (items.isEmpty)
          const EmptyState(
            sliver: true,
            icon: Icons.notifications_none_outlined,
            title: 'No notifications',
            message: 'Finished downloads, backups that need you, app updates and drive warnings show up here.',
          )
        else
          SliverList.list(children: [
            if (_permission != null && _permission != PermissionState.granted)
              Padding(
                padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.sm, Space.gutter(context), 0),
                child: Notice(
                  status: Status.info,
                  icon: Icons.notifications_active_outlined,
                  message: 'Get these on this phone too, even when the app is closed.',
                  actionLabel: 'Turn on',
                  onAction: _turnOnPhone,
                ),
              ),
            for (final g in groupByDay(items))
              TileGroup(title: dayLabel(g.day), children: [for (final i in g.items) FeedTile(item: i, onTap: () => _open(i))]),
            if (_hasMore)
              Padding(
                padding: const EdgeInsets.all(Space.md),
                child: Center(
                  child: _loadingMore
                      ? const CircularProgressIndicator()
                      : OutlinedButton(onPressed: _more, child: const Text('Show older')),
                ),
              ),
          ]),
      ],
    );
  }
}

/// Home's top-bar bell: the unread count on a badge, opens the inbox.
class NotificationBell extends StatelessWidget {
  const NotificationBell({super.key});

  @override
  Widget build(BuildContext context) => ValueListenableBuilder<int?>(
        valueListenable: NotificationFeed.instance.unread,
        builder: (context, n, _) {
          final count = n ?? 0;
          return IconButton(
            tooltip: count == 0 ? 'Notifications' : 'Notifications, $count unread',
            icon: count == 0
                ? const Icon(Icons.notifications_outlined)
                : Badge(label: Text(count > 99 ? '99+' : '$count'), child: const Icon(Icons.notifications_outlined)),
            onPressed: () => Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => const NotificationsScreen())),
          );
        },
      );
}

/// "Today", "Yesterday", "Mon, Sep 22", "Sep 22, 2025".
String dayLabel(DateTime day) {
  final now = clock.now();
  final today = DateTime(now.year, now.month, now.day);
  final days = today.difference(DateTime(day.year, day.month, day.day)).inDays;
  if (days == 0) return 'Today';
  if (days == 1) return 'Yesterday';
  return day.year == now.year ? DateFormat.MMMEd().format(day) : DateFormat.yMMMd().format(day);
}

Status _status(String level) => switch (level) {
      'error' => Status.error,
      'warning' => Status.warning,
      'success' => Status.success,
      _ => Status.info,
    };

IconData _icon(FeedItem i) => switch (i.channel) {
      FeedChannel.downloads => i.level == 'error' ? Icons.file_download_off_outlined : Icons.download_done_outlined,
      FeedChannel.backups => Icons.backup_outlined,
      FeedChannel.system => switch (i.category) {
          'app' => Icons.apps_outlined,
          'storage' => Icons.storage_outlined,
          _ => Icons.dns_outlined,
        },
    };

/// One entry: what kind and how it went, title (bold while unread),
/// message, time, and an unread dot.
class FeedTile extends StatelessWidget {
  const FeedTile({super.key, required this.item, this.onTap});

  final FeedItem item;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final time = DateFormat.jm().format(item.time.toLocal());
    return ListTile(
      leading: StatusDisc(status: _status(item.level), icon: _icon(item), size: 40),
      title: Text(item.title, style: item.read ? null : const TextStyle(fontWeight: FontWeight.w600)),
      subtitle: Text(item.message.isEmpty ? time : '${item.message}\n$time', maxLines: 3, overflow: TextOverflow.ellipsis),
      isThreeLine: item.message.isNotEmpty,
      trailing: item.read
          ? null
          : Semantics(
              label: 'Unread',
              child: Container(width: 10, height: 10, decoration: BoxDecoration(color: scheme.primary, shape: BoxShape.circle)),
            ),
      onTap: onTap,
    );
  }
}

/// Opens what [item] is about; nothing when it has no page of its own.
Future<void> openFeedTarget(BuildContext context, FeedItem item) async {
  final t = item.target;
  if (t == null) return;
  final nav = Navigator.of(context);
  final messenger = ScaffoldMessenger.maybeOf(context);
  Future<void> push(Widget w) => nav.push(MaterialPageRoute<void>(builder: (_) => w));
  switch (t.kind) {
    case FeedTargetKind.download:
      return push(DownloadStationScreen(initialFilter: item.level == 'error' ? DsFilter.failed : DsFilter.completed));
    case FeedTargetKind.torrent:
      return push(TorrentDetailScreen(hash: t.id, name: '${item.args['filename'] ?? ''}'));
    case FeedTargetKind.backupRun:
      return push(BackupRunScreen(runId: t.id));
    case FeedTargetKind.backup:
      return push(const BackupScreen());
    case FeedTargetKind.drive:
      return push(DriveHealthScreen(drive: DriveHealth(name: t.id.split('/').last, path: t.id)));
    case FeedTargetKind.app:
      try {
        final apps = await AppsApi.load();
        final app = apps.where((a) => a.id == t.id).firstOrNull ?? apps.where((a) => a.title == t.id).firstOrNull;
        if (app == null) {
          messenger?.showSnackBar(const SnackBar(content: Text('That app is no longer installed.')));
          return;
        }
        await push(AppDetailScreen(app: app));
      } catch (e) {
        messenger?.showSnackBar(SnackBar(content: Text("Couldn't open the app. ${e is ApiException ? e.message : ''}".trim())));
      }
  }
}

/// Which kinds of entries also notify on this phone, and the permission.
Future<void> showFeedSettings(BuildContext context, {PermissionState? permission, Future<void> Function()? onTurnOn}) async {
  final prefs = await NotifyPrefs.load();
  if (!context.mounted) return;
  await showModalBottomSheet<void>(
    context: context,
    showDragHandle: true,
    useSafeArea: true,
    isScrollControlled: true,
    builder: (context) => StatefulBuilder(
      builder: (context, setState) => SingleChildScrollView(
        padding: const EdgeInsets.only(bottom: Space.lg),
        child: TileGroup(
          title: 'Notify on this phone',
          footer: 'Checked while the app is open, and about every 15 minutes when it is closed. Nothing goes through Google or anyone else: the phone asks your server.',
          children: [
            for (final c in FeedChannel.values)
              SwitchListTile(
                title: Text(c.label),
                subtitle: Text(c.about),
                value: prefs.enabled.contains(c),
                onChanged: (on) async {
                  setState(() => on ? prefs.enabled.add(c) : prefs.enabled.remove(c));
                  await prefs.saveEnabled();
                },
              ),
            if (permission != null && permission != PermissionState.granted)
              ListTile(
                leading: const Icon(Icons.notifications_off_outlined),
                title: const Text('Notifications are off for this app'),
                subtitle: const Text('Turn them on to get these on the phone'),
                onTap: onTurnOn == null
                    ? null
                    : () async {
                        Navigator.of(context).pop();
                        await onTurnOn();
                      },
              ),
          ],
        ),
      ),
    ),
  );
}
