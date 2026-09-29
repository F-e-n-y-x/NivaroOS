// Shared pieces of the "Back up this phone" screens: what each category
// is, the permissions it needs and why, and the permission flow (explain
// first, then ask; a refused or restricted permission gets the way to
// allow it in the system settings).
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../phone_backup/pb_models.dart';
import '../../phone_backup/pb_platform.dart';
import '../../ui/ui.dart';

class CategoryInfo {
  const CategoryInfo(this.category, this.icon, this.description, {this.permissions = const [], this.why = '', this.restricted = false});

  final PhoneCategory category;
  final IconData icon;
  final String description;

  /// Android permissions it reads with.
  final List<String> permissions;

  /// Why, said before the system asks.
  final String why;

  /// Android 13+ keeps these from sideloaded apps until "Allow restricted
  /// settings" is on in the app's info page.
  final bool restricted;
}

const categoryInfo = <PhoneCategory, CategoryInfo>{
  PhoneCategory.media: CategoryInfo(
    PhoneCategory.media,
    Icons.photo_library_outlined,
    'Every photo and video, in full quality, with where they were taken.',
    permissions: [AndroidPermissions.readMediaImages, AndroidPermissions.readMediaVideo, AndroidPermissions.accessMediaLocation],
    why: 'NivaroOS reads your photos and videos to copy them to your server. It asks for their locations too, so a restore gives them back exactly as they were. Nothing goes anywhere but your own server.',
  ),
  PhoneCategory.files: CategoryInfo(PhoneCategory.files, Icons.folder_outlined, 'Folders you choose, such as Documents or Download.'),
  PhoneCategory.contacts: CategoryInfo(
    PhoneCategory.contacts,
    Icons.contacts_outlined,
    'All contacts, as one vCard file each time they change.',
    permissions: [AndroidPermissions.readContacts],
    why: 'NivaroOS reads your contacts to save them on your server as a vCard file, which any phone can import.',
  ),
  PhoneCategory.calendar: CategoryInfo(
    PhoneCategory.calendar,
    Icons.event_outlined,
    'Every calendar on this phone, as iCalendar files.',
    permissions: [AndroidPermissions.readCalendar],
    why: 'NivaroOS reads your calendars to save them on your server as iCalendar files, which any calendar app can import.',
  ),
  PhoneCategory.sms: CategoryInfo(
    PhoneCategory.sms,
    Icons.sms_outlined,
    'Text and picture messages. Only new ones are sent each time.',
    permissions: [AndroidPermissions.readSms],
    restricted: true,
    why: 'NivaroOS reads your messages to save them on your server, in the SMS Backup & Restore format. They are stored as they are (not encrypted), on your server only.',
  ),
  PhoneCategory.calllog: CategoryInfo(
    PhoneCategory.calllog,
    Icons.call_outlined,
    'Calls made, received and missed.',
    permissions: [AndroidPermissions.readCallLog],
    restricted: true,
    why: 'NivaroOS reads your call history to save it on your server. It never makes or listens to calls.',
  ),
  PhoneCategory.apps: CategoryInfo(PhoneCategory.apps, Icons.apps_outlined, 'The list of installed apps and their versions, to set up a new phone.'),
  PhoneCategory.apks: CategoryInfo(PhoneCategory.apks, Icons.android_outlined, 'The install files of apps you added yourself. They can take a lot of space.'),
  PhoneCategory.settings: CategoryInfo(PhoneCategory.settings, Icons.tune_outlined, 'Settings apps can read: ringtone, screen timeout, font size and the like.'),
};

/// The phone's permissions (a seam for tests).
abstract class PhonePermissions {
  const PhonePermissions();
  Future<Map<String, bool>> check(List<String> names);
  Future<Map<String, bool>> request(List<String> names);
  Future<void> openSettings();

  static PhonePermissions instance = const ChannelPermissions();
}

class ChannelPermissions extends PhonePermissions {
  const ChannelPermissions();

  @override
  Future<Map<String, bool>> check(List<String> names) async {
    try {
      return await const ChannelPhoneSources().permissions(names);
    } on MissingPluginException {
      return {for (final n in names) n: true};
    }
  }

  @override
  Future<Map<String, bool>> request(List<String> names) async {
    try {
      return await ChannelPhoneSources.request(names);
    } on MissingPluginException {
      return {for (final n in names) n: true};
    }
  }

  @override
  Future<void> openSettings() async {
    try {
      await ChannelPhoneSources.openAppSettings();
    } on MissingPluginException {
      // Not on Android.
    }
  }
}

/// Explains, asks, and on a refusal shows how to allow it. True when the
/// category may be read. ACCESS_MEDIA_LOCATION is a nice-to-have: photos
/// are backed up without it (without their locations).
Future<bool> askForCategory(BuildContext context, CategoryInfo info, PhonePermissions perms, {String? title}) async {
  if (info.permissions.isEmpty) return true;
  final have = await perms.check(info.permissions);
  bool needed(String p) => p != AndroidPermissions.accessMediaLocation;
  if (info.permissions.where(needed).every((p) => have[p] == true)) {
    if (have[AndroidPermissions.accessMediaLocation] == false) await perms.request([AndroidPermissions.accessMediaLocation]);
    return true;
  }
  if (!context.mounted) return false;
  final go = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      icon: Icon(info.icon),
      title: Text(title ?? 'Back up ${info.category.label.toLowerCase()}?'),
      content: Text(info.restricted
          ? '${info.why}\n\nAndroid asks next. If it says the setting is restricted, open NivaroOS’s app info, tap ⋮ and “Allow restricted settings”, then try again.'
          : '${info.why}\n\nAndroid asks next.'),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(false), child: const Text('Not now')),
        FilledButton(onPressed: () => Navigator.of(context).pop(true), child: const Text('Continue')),
      ],
    ),
  );
  if (go != true) return false;
  final got = await perms.request(info.permissions);
  if (info.permissions.where(needed).every((p) => got[p] == true)) return true;
  if (!context.mounted) return false;
  await showDialog<void>(
    context: context,
    builder: (context) => AlertDialog(
      title: Text('${info.category.label} not allowed'),
      content: Text(info.restricted
          ? 'Android keeps this from apps installed outside an app store until you allow it:\n\n1. Open NivaroOS’s app info.\n2. Tap ⋮ (top right) and “Allow restricted settings”, and confirm.\n3. Open Permissions, choose ${info.category == PhoneCategory.sms ? 'SMS' : 'Call logs'} and allow it.\n\nThen turn this on again.'
          : 'NivaroOS can’t back this up without access. You can allow it in the app’s settings, under Permissions.'),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Close')),
        FilledButton(
          onPressed: () {
            Navigator.of(context).pop();
            perms.openSettings();
          },
          child: const Text('Open app info'),
        ),
      ],
    ),
  );
  return false;
}

/// A small heading line inside a TileGroup-less area.
class PhoneBackupHint extends StatelessWidget {
  const PhoneBackupHint(this.text, {super.key});
  final String text;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final g = Space.gutter(context);
    return Padding(
      padding: EdgeInsets.fromLTRB(g, Space.xs, g, Space.md),
      child: Text(text, style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
    );
  }
}
