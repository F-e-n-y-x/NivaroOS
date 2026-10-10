import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../services/crash_reports.dart';
import '../ui/ui.dart';

/// Settings > Crash reports: what went wrong in the app, kept on this
/// phone. Send puts them on the signed-in NivaroOS server (Settings >
/// About in its web UI); nothing is sent on its own or anywhere else.
class CrashReportsScreen extends StatefulWidget {
  const CrashReportsScreen({super.key});

  @override
  State<CrashReportsScreen> createState() => _CrashReportsScreenState();
}

class _CrashReportsScreenState extends State<CrashReportsScreen> {
  bool _sending = false;

  Future<void> _send() async {
    setState(() => _sending = true);
    final messenger = ScaffoldMessenger.of(context);
    try {
      await CrashReports.instance.send();
      messenger.showSnackBar(const SnackBar(content: Text('Sent to your server')));
    } catch (e) {
      messenger.showSnackBar(SnackBar(content: Text('Could not send: $e')));
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  Future<void> _clear() async {
    final ok = await ConfirmDialog.destructive(context, title: 'Clear crash reports?', message: 'They are removed from this phone.', confirmLabel: 'Clear');
    if (ok) await CrashReports.instance.clear();
  }

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<List<CrashReport>>(
      valueListenable: CrashReports.instance.reports,
      builder: (context, reports, _) => AppScaffold.slivers(
        title: 'Crash reports',
        actions: [if (reports.isNotEmpty) IconButton(tooltip: 'Clear', icon: const Icon(Icons.delete_outline), onPressed: _clear)],
        slivers: [
          if (reports.isEmpty)
            const EmptyState(
              sliver: true,
              icon: Icons.check_circle_outline,
              title: 'No crashes',
              message: 'When something goes wrong in the app, the details are kept here, on this phone, for you to send to your server if you want.',
            )
          else
            SliverList.list(
              children: [
                TileGroup(
                  footer: 'Only to your own NivaroOS server, which shows them in Settings > About on the web. Nothing is sent anywhere else.',
                  children: [
                    ListTile(
                      leading: const Icon(Icons.send_outlined),
                      title: const Text('Send to my server'),
                      subtitle: Text('${reports.length} report${reports.length == 1 ? '' : 's'}'),
                      enabled: !_sending,
                      onTap: _send,
                    ),
                  ],
                ),
                TileGroup(
                  children: [
                    for (final r in reports)
                      ExpansionTile(
                        title: Text(r.error, maxLines: 2, overflow: TextOverflow.ellipsis),
                        subtitle: Text(DateFormat.yMMMd().add_jm().format(r.time)),
                        children: [
                          Padding(
                            padding: const EdgeInsets.fromLTRB(Space.lg, 0, Space.lg, Space.md),
                            child: SelectableText('${r.error}\n\n${r.stack}', style: const TextStyle(fontFamily: 'monospace', fontSize: 12)),
                          ),
                        ],
                      ),
                  ],
                ),
              ],
            ),
        ],
      ),
    );
  }
}
