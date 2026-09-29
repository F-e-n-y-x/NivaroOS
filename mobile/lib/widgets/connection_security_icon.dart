import 'package:flutter/material.dart';

import '../services/connection_security.dart';

/// A row icon for how safe the connection to [url] is
/// ([ConnectionSecurity.of]): a lock for https, a VPN lock for Tailscale,
/// the router for the home network, and an open lock in the error colour
/// for plain http over the internet. TalkBack reads the label.
class ConnectionSecurityIcon extends StatelessWidget {
  const ConnectionSecurityIcon({super.key, required this.url});

  final String url;

  @override
  Widget build(BuildContext context) {
    final security = ConnectionSecurity.of(url);
    final icon = switch (security) {
      TransportSecurity.https => Icons.lock_outline,
      TransportSecurity.tailscale => Icons.vpn_lock_outlined,
      TransportSecurity.localNetwork => Icons.router_outlined,
      TransportSecurity.insecure => Icons.lock_open_outlined,
    };
    return Tooltip(
      message: security.label,
      child: Icon(
        icon,
        semanticLabel: security.label,
        color: security == TransportSecurity.insecure ? Theme.of(context).colorScheme.error : null,
      ),
    );
  }
}
