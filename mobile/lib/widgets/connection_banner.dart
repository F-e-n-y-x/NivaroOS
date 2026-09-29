import 'package:flutter/material.dart';

import '../services/server_routes.dart';
import '../ui/ui.dart';

/// The icon for a way of reaching the server.
IconData routeIcon(RouteKind? kind) => switch (kind) {
      RouteKind.home => Icons.home_outlined,
      RouteKind.tailscale => Icons.vpn_lock_outlined,
      RouteKind.internet => Icons.public_outlined,
      null => Icons.dns_outlined,
    };

/// What the banner says when the server can't be reached on any of its
/// addresses; null when there is nothing to say (reachable, or a server
/// with no Tailscale address, where the screens' own offline states say
/// it).
({String title, String message, bool openTailscale})? connectionProblem(RouteState s) {
  if (s.status != RouteStatus.unreachable || !s.hasTailscaleAddress) return null;
  final name = (s.serverName == null || s.serverName!.isEmpty) ? 'the server' : s.serverName!;
  if (s.phoneTailscaleUp == true) {
    return (
      title: "Can't reach $name",
      message: "Tailscale is on, but $name isn't answering on any of its addresses. It may be off, or Tailscale may be down on it.",
      openTailscale: false,
    );
  }
  return (
    title: "Can't reach $name — turn on Tailscale",
    message: "Away from home, this phone reaches $name through Tailscale, and it isn't connected.",
    openTailscale: true,
  );
}

/// Wraps every screen (MaterialApp.builder): when the server can't be
/// reached and Tailscale is off on the phone, a banner at the top says so,
/// with a button that opens the Tailscale app. It goes away by itself once
/// an address answers (coming back to the app, a network change).
class ConnectionBannerHost extends StatelessWidget {
  const ConnectionBannerHost({super.key, required this.child, this.routes});

  final Widget child;

  /// The routes to show; the app's by default.
  final ServerRoutes? routes;

  @override
  Widget build(BuildContext context) {
    final r = routes ?? ServerRoutes.instance;
    return ValueListenableBuilder<RouteState>(
      valueListenable: r.state,
      builder: (context, s, _) {
        final problem = connectionProblem(s);
        // The same tree whether the banner shows or not, so the screens
        // under it keep their state.
        return Column(
          children: [
            AnimatedSize(
              duration: Motion.of(context).medium,
              alignment: Alignment.topCenter,
              child: problem == null
                  ? const SizedBox(width: double.infinity)
                  : Material(
                      color: Theme.of(context).colorScheme.surface,
                      child: SafeArea(
                        bottom: false,
                        child: Notice(
                          status: Status.warning,
                          icon: Icons.vpn_lock_outlined,
                          title: problem.title,
                          message: problem.message,
                          actionLabel: problem.openTailscale ? 'Open Tailscale' : 'Try again',
                          onAction: problem.openTailscale ? () => ServerRoutes.openTailscale() : () => r.select(),
                        ),
                      ),
                    ),
            ),
            Expanded(
              child: MediaQuery.removePadding(context: context, removeTop: problem != null, child: child),
            ),
          ],
        );
      },
    );
  }
}
