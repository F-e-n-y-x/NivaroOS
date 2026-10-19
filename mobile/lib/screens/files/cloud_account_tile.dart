import 'package:flutter/material.dart';

import '../../models/cloud_account.dart';

/// A cloud drive on the Files locations page. One whose sign-in expired
/// says why and offers Reconnect instead of opening.
class CloudAccountTile extends StatelessWidget {
  const CloudAccountTile({super.key, required this.account, required this.onOpen, required this.onReconnect});

  final CloudAccount account;
  final VoidCallback onOpen;
  final VoidCallback onReconnect;

  @override
  Widget build(BuildContext context) {
    final a = account;
    if (a.signInExpired) {
      final error = Theme.of(context).colorScheme.error;
      return ListTile(
        leading: Icon(Icons.cloud_off_outlined, color: error),
        title: Text(a.displayName, maxLines: 1, overflow: TextOverflow.ellipsis),
        subtitle: Text.rich(
          TextSpan(children: [
            TextSpan(text: 'Sign-in expired', style: TextStyle(color: error, fontWeight: FontWeight.w600)),
            if (a.problem.isNotEmpty) TextSpan(text: ' · ${a.problem}'),
          ]),
          maxLines: 4,
          overflow: TextOverflow.ellipsis,
        ),
        isThreeLine: true,
        trailing: FilledButton.tonal(onPressed: onReconnect, child: const Text('Reconnect')),
      );
    }
    return ListTile(
      leading: const Icon(Icons.cloud_outlined),
      title: Text(a.displayName, maxLines: 1, overflow: TextOverflow.ellipsis),
      subtitle: Text(a.isMounted ? a.providerTitle : '${a.providerTitle} · Not connected', maxLines: 1, overflow: TextOverflow.ellipsis),
      enabled: a.isMounted,
      onTap: onOpen,
    );
  }
}
