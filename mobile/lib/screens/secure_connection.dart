import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../services/api_client.dart';
import '../services/server_trust.dart';

/// Trust on first use for a server's own certificate (HTTPS on your
/// network): finds the server's https address, shows its CA fingerprint and
/// pins it only when the user accepts. Returns the https base URL to use,
/// or null to keep [base] as it is.
///
/// [base] is either the server's http address (it says whether it offers
/// https) or an https address that just failed with a certificate error.
Future<String?> offerSecureConnection(BuildContext context, String base) async {
  final String secure;
  if (base.startsWith('https://')) {
    secure = base;
  } else {
    final offer = await ServerTrust.offeredHttps(base);
    if (offer == null) return null;
    secure = ServerTrust.httpsBase(base, offer.port);
  }
  final Uint8List der;
  try {
    der = await ServerTrust.fetchCa(secure);
  } catch (_) {
    return null; // not a NivaroOS https listener: the caller keeps its own error
  }
  if (!context.mounted) return null;

  if (!ServerTrust.instance.isPinned(der)) {
    final fp = ServerTrust.fingerprint(der);
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        icon: const Icon(Icons.lock_outline),
        title: const Text('Use a secure connection?'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('${ApiClient.displayHost(secure)} encrypts the connection with its own certificate. '
                  'Trust it only if this fingerprint matches Settings › Network & Sharing › HTTPS on your network on the server:'),
              const SizedBox(height: 12),
              SelectableText(fp, style: const TextStyle(fontFamily: 'monospace', fontSize: 12)),
            ],
          ),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: Text(base.startsWith('https://') ? 'Cancel' : 'Keep http')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Trust')),
        ],
      ),
    );
    if (ok != true) return null;
    await ServerTrust.instance.pin(der);
  }
  // Now with the full checks: the chain to the pinned CA and the host name.
  final probe = await ApiClient.probe(secure);
  return probe.url;
}
