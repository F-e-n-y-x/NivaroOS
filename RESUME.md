# app-remote + app-lock wiring: paused 2026-09-30 (not committed)

Worktree based on origin/master 7f11de1.

Done here (uncommitted):
- All app-remote code from /tmp/claude-0/wt-routes applied (patch: scratchpad/routes-final.patch):
  server_routes.dart, server_addresses_screen.dart, connection_banner.dart, NetworkChannels.kt,
  server_routes_test.dart, api_client failover (_attempt/_watch/_failover, login + refresh failover,
  useRoute/reportUnreachable), main.dart (ServerRoutes.startWatching/select, ConnectionBannerHost),
  settings "Server > Connection" tile, server_profiles addresses/route label/form keeps addresses,
  storage/session/speedtest/server_profile changes, manifest <package com.tailscale.ipn>, MainActivity NetworkChannels.register.
- dashboard_screen.dart conflict resolved by hand (imports merged with Server health/Tailscale upstream; route chip action kept).

Left:
1. App-lock wiring (reference: scratchpad/shared_with_mine.patch, scratchpad/paused/main-tree-final.patch):
   - main.dart: `await AppLock.instance.load()` before runApp; wrap in AppLockGate (outside ConnectionBannerHost).
   - login_screen: `with SecureWindowState<LoginScreen>`.
   - settings: "Privacy & security" TileGroup before "Server power".
   - server_profiles: leading ConnectionSecurityIcon(url: p.url), 'Not secure' part.
   - api_client: requestTokens refuses insecure (ConnectionSecurity.resolve(base).allowsSecrets, thrown as ApiErrorKind.network
     so login failover tries another address); refresh post() calls ConnectionSecurity.ensureAllowsSecrets; candidateUrls prefer https for public hosts.
   - Android (main-tree-final.patch lines 1-160): build.gradle.kts, manifest USE_BIOMETRIC + comment, MainActivity -> FlutterFragmentActivity
     + SecureWindowChannel register, res/values(-night)/styles.xml AppCompat parent; copy SecureWindowChannel.kt and
     res/xml/network_security_config.xml from /root/NivaroOS (or scratchpad/paused/main-untracked.tgz).
2. git add the untracked files; flutter analyze; flutter test -j 2 test/services test/apps test/files + affected screenshot tests
   (regenerate goldens: platform server_addresses/connection_banner/settings, screens home_shell, directions settings_refresh). Check free -g first.
3. One `flutter build apk --debug`; git pull --rebase; commit (Co-Authored-By trailer) + push.
