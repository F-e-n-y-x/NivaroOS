import 'package:flutter/material.dart';

import '../../backup/backup_api.dart';
import '../../phone_backup/pb_models.dart';
import '../../phone_backup/pb_service.dart';
import '../../ui/ui.dart';
import '../../utils/format.dart';
import 'phone_backup_common.dart';
import 'phone_backup_history_screen.dart';

/// The other phones backed up to this server, from the owner's session
/// (GET /devices). Picking one opens its backups in the same screens as
/// this phone's own, read with the owner's session (GET only): what is
/// restored lands on this phone, that phone's backup is never changed.
class PhoneOtherBackupsScreen extends StatefulWidget {
  const PhoneOtherBackupsScreen({super.key, this.service});
  final PhoneBackupService? service;

  @override
  State<PhoneOtherBackupsScreen> createState() => _PhoneOtherBackupsScreenState();
}

/// Every phone of [devices] but this one ([ownId]), the most recently
/// backed up first; phones never backed up last.
List<PhoneDevice> otherPhones(List<PhoneDevice> devices, String? ownId) {
  final out = [for (final d in devices) if (d.id.isNotEmpty && d.id != ownId) d];
  out.sort((a, b) => (b.lastBackupAt?.millisecondsSinceEpoch ?? -1).compareTo(a.lastBackupAt?.millisecondsSinceEpoch ?? -1));
  return out;
}

class _PhoneOtherBackupsScreenState extends State<PhoneOtherBackupsScreen> {
  late final PhoneBackupService _service = widget.service ?? PhoneBackupService.instance;
  List<PhoneDevice>? _phones;
  Object? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final all = await _service.owner.devices();
      final own = (await _service.credential())?.deviceId;
      if (mounted) setState(() => _phones = otherPhones(all, own));
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  void _open(PhoneDevice d) => Navigator.of(context).push(MaterialPageRoute<void>(
      builder: (_) => PhoneBackupHistoryScreen(service: _service.readingPhone(d.id), title: d.name.isEmpty ? 'Another phone' : d.name)));

  @override
  Widget build(BuildContext context) {
    final phones = _phones;
    final Widget body;
    if (phones == null && _error != null) {
      body = ErrorState(title: 'Couldn’t load the phones', message: BackupError.from(_error!).title, onRetry: _load);
    } else if (phones == null) {
      body = const LoadingList(rows: 4);
    } else if (phones.isEmpty) {
      body = const EmptyState(icon: Icons.phone_android_outlined, title: 'No other phones', message: 'Only this phone backs up to this server.');
    } else {
      body = RefreshIndicator(
        onRefresh: _load,
        child: ListView(children: [
          const PhoneBackupHint('Pick a phone to browse its backups and get photos, files, contacts or messages from it onto this phone. Its backups stay as they are.'),
          TileGroup(children: [
            for (final d in phones)
              ListTile(
                leading: const Icon(Icons.phone_android_outlined),
                title: Text(d.name.isEmpty ? d.id : d.name),
                subtitle: Text([
                  d.lastBackupAt == null ? 'No backup yet' : 'Last backup: ${formatRelative(d.lastBackupAt!)}',
                  if (d.sizeBytes > 0) formatSize(d.sizeBytes),
                  if (d.revokedAt != null) 'Not linked any more',
                ].join(' · ')),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => _open(d),
              ),
          ]),
          const SizedBox(height: Space.xl),
        ]),
      );
    }
    return AppScaffold(title: 'Another phone’s backups', body: body);
  }
}
