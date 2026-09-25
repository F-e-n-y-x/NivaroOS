import 'package:flutter/material.dart';

import '../services/api_client.dart';
import '../ui/ui.dart';

/// Restart or shut down the server, after a confirmation that names it:
/// core's `PUT /v1/sys/state/:state` (restart | off), as the web UI uses
/// (plan M-09). Home's power menu and Settings both call this, so the
/// wording and the route are the same everywhere.
Future<void> confirmServerPower(BuildContext context, {required bool restart}) async {
  final url = ApiClient.instance.baseUrl;
  final host = url.isEmpty ? 'the server' : ApiClient.displayHost(url);
  final ok = await ConfirmDialog.destructive(
    context,
    title: restart ? 'Restart $host?' : 'Shut down $host?',
    message: restart
        ? 'Apps, virtual machines and file transfers stop until it is back, usually in a few minutes.'
        : 'Everything on it stops, and it stays off until someone switches it on again.',
    confirmLabel: restart ? 'Restart' : 'Shut down',
    permanent: false,
  );
  if (!ok || !context.mounted) return;
  final messenger = ScaffoldMessenger.of(context);
  try {
    await ApiClient.instance.put('/v1/sys/state/${restart ? 'restart' : 'off'}');
    messenger.showSnackBar(SnackBar(content: Text(restart ? '$host is restarting' : '$host is shutting down')));
  } on ApiException catch (e) {
    messenger.showSnackBar(SnackBar(content: Text(restart ? "Couldn't restart $host. ${e.message}" : "Couldn't shut down $host. ${e.message}")));
  } catch (_) {
    messenger.showSnackBar(SnackBar(content: Text(restart ? "Couldn't restart $host." : "Couldn't shut down $host.")));
  }
}
