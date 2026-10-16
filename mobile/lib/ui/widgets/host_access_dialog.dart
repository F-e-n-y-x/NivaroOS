import 'package:flutter/material.dart';

import '../../services/api_client.dart';

/// Why the server wants the admin's OK first (HTTP 428 with
/// `data.host_access`: the app would get root access to the server - host
/// network, privileged, the Docker socket, /etc...), or null for any other
/// error. The request goes through again with `allow_host_access=true`.
List<String>? hostAccessReasons(Object e) {
  if (e is! ApiException || e.statusCode != 428) return null;
  final data = e.data;
  final why = data is Map ? data['host_access'] : null;
  return why is List && why.isNotEmpty ? [for (final w in why) '$w'] : null;
}

/// Asks before an app gets full access to the server; true means go ahead.
/// [lead] says what happens (install, save, update), [why] lists the
/// server's reasons.
Future<bool> confirmHostAccess(BuildContext context, {required String lead, required List<String> why, required String confirmLabel}) async {
  final go = await showDialog<bool>(
    context: context,
    builder: (d) => AlertDialog(
      title: const Text('Give this app full access to the server?'),
      content: SingleChildScrollView(child: Text('$lead\n\n${why.map((w) => '• $w').join('\n')}')),
      actions: [
        TextButton(autofocus: true, onPressed: () => Navigator.of(d).pop(false), child: const Text('Cancel')),
        TextButton(onPressed: () => Navigator.of(d).pop(true), child: Text(confirmLabel)),
      ],
    ),
  );
  return go ?? false;
}
