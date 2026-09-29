// "Change location…" for this phone's backups (plan §7.2): a server
// folder from the folder picker (a drive, a USB drive, merged storage),
// then, when there are backups already, move them there or start fresh.
// A missing drive is refused with a clear reason; the server never writes
// phone backups to another disk instead.
import 'package:flutter/material.dart';

import '../../backup/backup_api.dart';
import '../../backup/backup_models.dart';
import '../../phone_backup/pb_models.dart';
import '../../phone_backup/pb_service.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import '../backup/backup_folder_picker.dart';

/// The kinds of place a phone's folder may live in.
const phoneBackupLocationKinds = {'volume', 'usb', 'merge'};

enum LocationMode { move, fresh }

/// Asks move or start fresh; null when cancelled.
Future<LocationMode?> askMoveOrFresh(BuildContext context, {required String target}) => showDialog<LocationMode>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('What about the backups so far?'),
        content: Text(
          'Backups of this phone will go to $target.\n\n'
          'Move them: the backups made so far go along, with their history. On another drive they are copied first, which can take a while.\n\n'
          'Start fresh: the new place starts empty. The old backups stay where they are on the server, but the app no longer shows them.',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
          TextButton(onPressed: () => Navigator.of(context).pop(LocationMode.fresh), child: const Text('Start fresh')),
          FilledButton(onPressed: () => Navigator.of(context).pop(LocationMode.move), child: const Text('Move them')),
        ],
      ),
    );

/// The reason a location change was refused, in words.
({String title, String message}) locationErrorText(Object error) {
  final e = BackupError.from(error);
  return switch (e.code) {
    'dest_offline' => (
        title: 'That drive isn’t connected',
        message: 'Connect the drive to the server and try again. Backups of this phone are never written to another disk instead.',
      ),
    'dest_inside_source' => (title: 'Choose another folder', message: 'That folder is inside this phone’s backup folder, or holds it.'),
    'invalid_state' => (
        title: 'Can’t change it right now',
        message: 'A backup is running, the backups are being moved, or the folder is already used (by another phone or a backup job) or isn’t empty. Try again when the backup has finished, or pick an empty folder.',
      ),
    'no_space' => (title: 'Not enough space there', message: 'The backups so far don’t fit on that drive. Free up space, or start fresh there.'),
    'forbidden' => (title: 'Only an administrator can do this', message: 'Sign in to the server as an administrator to change where this phone’s backups go.'),
    _ => (title: e.title, message: [e.causeText, e.fix].where((s) => s.isNotEmpty).join(' ')),
  };
}

/// The whole flow; the phone's new detail, or null when nothing changed.
Future<PhoneDeviceDetail?> changePhoneBackupLocation(
  BuildContext context, {
  required String deviceId,
  required PhoneDeviceDetail detail,
  PhoneOwnerApi? owner,
  BackupApi? api,
  Future<BackupEndpoint?> Function(BuildContext context)? pick,
}) async {
  final o = owner ?? PhoneOwnerApi();
  BackupEndpoint? target;
  var toDefault = false;
  if (!detail.destination.isDefault) {
    final choice = await showModalBottomSheet<String>(
      context: context,
      showDragHandle: true,
      builder: (context) => SafeArea(
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          ListTile(leading: const Icon(Icons.folder_open_outlined), title: const Text('Choose a folder…'), onTap: () => Navigator.of(context).pop('pick')),
          ListTile(
            leading: const Icon(Icons.home_outlined),
            title: const Text('Use the default location'),
            subtitle: const Text('/DATA/Backup on the server'),
            onTap: () => Navigator.of(context).pop('default'),
          ),
        ]),
      ),
    );
    if (choice == null || !context.mounted) return null;
    toDefault = choice == 'default';
  }
  if (!toDefault) {
    target = await (pick ?? (c) => pickBackupFolder(c, role: 'dest', title: 'Where backups of this phone go', api: api, kinds: phoneBackupLocationKinds))(context);
    if (target == null || !context.mounted) return null;
  }
  String? mode;
  if (detail.hasBackups) {
    final m = await askMoveOrFresh(context, target: toDefault ? 'the default location' : target!.display);
    if (m == null || !context.mounted) return null;
    mode = m.name;
  }
  try {
    final next = await o.setDestination(deviceId, toDefault ? null : target, mode: mode);
    if (context.mounted) {
      final moving = next.move?.state == 'moving';
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(
        content: Text(moving ? 'Moving the backups. Backups of this phone wait until it’s done.' : 'Backups of this phone now go to ${next.destination.path}'),
      ));
    }
    return next;
  } catch (e) {
    if (!context.mounted) return null;
    final t = locationErrorText(e);
    await showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        icon: const Icon(Icons.error_outline),
        title: Text(t.title),
        content: Text(t.message),
        actions: [FilledButton(onPressed: () => Navigator.of(context).pop(), child: const Text('OK'))],
      ),
    );
    return null;
  }
}

/// The destination rows: where the backups go and "Change location…";
/// a missing drive shows as a waiting notice.
class PhoneDestinationGroup extends StatelessWidget {
  const PhoneDestinationGroup({super.key, required this.destination, this.move, this.onChange, this.footer});

  final PhoneDestination destination;
  final PhoneMove? move;
  final VoidCallback? onChange;
  final String? footer;

  @override
  Widget build(BuildContext context) {
    final d = destination;
    final theme = Theme.of(context);
    final where = d.isDefault ? 'Default location' : (d.label.isNotEmpty ? d.label : 'Chosen folder');
    final free = d.freeBytes;
    return Column(children: [
      if (d.driveMissing)
        const Notice(
          title: 'Backup drive not connected',
          message: 'The drive that holds this phone’s backups isn’t connected to the server. Backups wait for it and go on by themselves when it’s back.',
          icon: Icons.usb_off_outlined,
        )
      else if (!d.usable)
        Notice(title: 'This location can’t be used', message: d.detail.isNotEmpty ? d.detail : 'Choose the location again with Change location.', status: Status.error),
      if (move?.state == 'moving')
        Notice(
          status: Status.info,
          icon: Icons.drive_file_move_outline,
          message: 'Moving the backups to ${move!.targetPath}: ${move!.files} of ${move!.totalFiles} files.',
        ),
      if (move?.state == 'failed') Notice(status: Status.error, message: 'Moving the backups failed: ${move!.error}. Everything is still where it was.'),
      TileGroup(title: 'Where backups go', footer: footer, children: [
        ListTile(
          leading: Icon(d.driveMissing ? Icons.usb_off_outlined : Icons.dns_outlined),
          title: Text(d.path.isEmpty ? 'On your server' : d.path, style: theme.textTheme.bodyLarge),
          subtitle: Text([where, if (free != null && d.online) '${formatSize(free)} free'].join(' · ')),
        ),
        if (onChange != null)
          ListTile(
            leading: const Icon(Icons.drive_file_move_outline),
            title: const Text('Change location…'),
            trailing: const Icon(Icons.chevron_right),
            onTap: onChange,
          ),
      ]),
    ]);
  }

}
