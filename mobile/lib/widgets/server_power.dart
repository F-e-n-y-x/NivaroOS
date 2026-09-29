import 'package:flutter/material.dart';

import '../services/api_client.dart';
import '../services/app_lock.dart';
import '../ui/ui.dart';

/// Restart or shut down the server, after a confirmation that names it:
/// core's `PUT /v1/sys/state/:state` (restart | off), as the web UI uses
/// (plan M-09). Home's power menu and Settings both call this, so the
/// wording and the route are the same everywhere. With the app lock on,
/// the owner also confirms it's them ([AppLock.confirm]).
Future<void> confirmServerPower(BuildContext context, {required bool restart, AppLock? lock}) async {
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
  if (!await (lock ?? AppLock.instance).confirm(restart ? 'Confirm it’s you to restart $host' : 'Confirm it’s you to shut down $host')) return;
  try {
    await ApiClient.instance.put('/v1/sys/state/${restart ? 'restart' : 'off'}');
    messenger.showSnackBar(SnackBar(content: Text(restart ? '$host is restarting' : '$host is shutting down')));
  } on ApiException catch (e) {
    messenger.showSnackBar(SnackBar(content: Text(restart ? "Couldn't restart $host. ${e.message}" : "Couldn't shut down $host. ${e.message}")));
  } catch (_) {
    messenger.showSnackBar(SnackBar(content: Text(restart ? "Couldn't restart $host." : "Couldn't shut down $host.")));
  }
}
