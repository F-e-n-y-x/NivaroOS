# NivaroOS Mobile

A native Android app for NivaroOS, built with [Flutter](https://flutter.dev)
(iOS is possible later; nothing here is Android-only by design). It is its
own UI talking to NivaroOS's REST APIs through the gateway, not a wrapper
around the web dashboard. Requires Android 7.0 or newer.

## Features

- **Servers**: found on the network by mDNS (or typed in), several server
  profiles, sign-in with the server's accounts, app lock, profile pictures.
- **Home**: live CPU, memory, storage, network and GPU widgets (refresh down
  to 1 s or *Real time* over the message bus), **Server health** (failed
  apps, drives that didn't mount with *Repair drive*, **drive health** with
  SMART verdict, history and self-tests) and a **notifications inbox**.
- **Notifications**: the server's feed (downloads, torrents, backups, app
  updates, drive health) as phone notifications, without Google's push
  service - see below.
- **Files**: tabs with a shared clipboard, transfers with resumable
  uploads, Trash, cloud drives, paired phones, share links (one-time,
  expiring, password, QR) and **Upload to NivaroOS** from Android's Share
  sheet.
- **Apps**: App Store, install/update, logs, Edit app.
- **VMs and Host Desktop**: power, snapshots, and a native RFB console
  (`lib/services/rfb_client.dart`) modelled on Microsoft's Windows App:
  Touch or Mouse pointer mode, pinch zoom, Fit / Fill / 1:1 / Match this
  phone, extra keys, clipboard, auto-reconnect.
- **Download Station**: downloads and torrents; *Download on server* for
  shared links and magnets; opens magnet links and `.torrent` files.
- **Backup & Sync**: jobs, runs, restore, create/edit; **phone backup** of
  photos, folders, contacts, calendar, SMS, call log and apps.
- **Fans**, **terminal sessions** that survive, system updates and logs.
- Six styles (Rack, Tonal, Console, Soft, Terminal, Bold) in light, dark
  and true black.

What changed per release: [`CHANGELOG.md`](../CHANGELOG.md). Since 1.4.11
(next APK): the notifications inbox and phone notifications; a Backup &
Sync overview that opens at once (and says "Can't reach the server" with
Retry instead of spinning); every web wizard option in the job form; link
speed and today's traffic on Home and Network.

## Architecture

- `lib/services/api_client.dart` - the gateway API (auth, dashboard,
  files). Refreshes an expired session token once before giving up, like
  the web app.
- `lib/services/vm_client.dart` - VM Manager and Host Desktop through the
  gateway's `/v1/vm-sidecar/*` route, with the same authentication.
- `lib/services/discovery_service.dart` - mDNS server discovery.
- `lib/services/storage_service.dart` - server address and session tokens
  in the platform keystore (`flutter_secure_storage`).
- `lib/screens/` - one file or folder per screen; `home_shell.dart` is the
  bottom navigation shell. Shared UI: `lib/ui/`.

## Server-side requirement for auto-discovery

The app finds servers via mDNS (`_nivaroos._tcp`). NivaroOS's own installer
(`installer/install.sh`'s `install_mdns_advertisement` step) sets this up
automatically - it installs `avahi-daemon` if missing and publishes
`/etc/avahi/services/nivaroos.service` advertising whatever port the
dashboard was configured on. Without this, the app still works fine -
users just enter their server's address manually the first time.

## Building it yourself

Requires a JDK (17-21), the Android SDK (`platform-tools`,
`platforms;android-37.0`, `build-tools;36.0.0` or newer, `ndk;28.2.13676358`),
and [Flutter](https://docs.flutter.dev/get-started/install) 3.47.5 or newer
(stable channel).

```sh
cd mobile
flutter pub get
flutter build apk --release   # -> build/app/outputs/flutter-apk/app-release.apk
```

A release build is signed with the key named in `android/key.properties`
(see [Flutter's signing guide](https://docs.flutter.dev/deployment/android));
without that file `android/app/build.gradle.kts` falls back to the debug key.

## Installing the APK

1. Open the [latest GitHub release](https://github.com/F-e-n-y-x/NivaroOS/releases/latest)
   on the phone and download `NivaroOS.apk`.
2. Open it. Android asks once to allow installs from your browser or file
   manager (it isn't on the Play Store) - allow it, then Install.
3. The app finds NivaroOS servers on your network, or type the address.

Later versions install from inside the app (More -> NivaroOS update): it
checks the APK against the release's `SHA256SUMS` and that it is signed
with the same key. Signing certificate SHA-256:
`d36dcc45e03a0cd047c4b20c5f4e3f70c3d9908c8ae1954d1eeed64f73f86565`.

The APK from the "Android app" GitHub Actions check is a test build signed
with a throwaway key: it can't update an installed NivaroOS app. Use the
release one.

## Notifications

The server keeps a notification feed (message bus, `GET /v2/message_bus/notifications`;
what goes in it: `services/message-bus/service/notification_classify.go`) -
finished/failed downloads and torrents, Backup & Sync runs, app installs and
updates, drive health. The app shows it under Home's bell and More ->
Notifications (`lib/screens/notifications_screen.dart`), and posts new entries
on the phone in three channels (Downloads, Backups, System) without Google's
push service: live over the bus WebSocket while the app runs, and from the
15-minute heartbeat job (`heartbeatMain`) when it doesn't. Each entry is posted
once: unread, newer than the last id seen on that server, in a channel left on
(the tune icon in the inbox). The first check on a server only records where
the feed is. The Android permission is asked from the inbox, never at launch.
Logic and tests: `lib/services/notification_feed.dart`,
`test/services/notification_feed_test.dart`; posting:
`android/.../FeedNotifier.kt`.

## Regenerating the app icon

Source images live in `assets/icon/` (`icon.png` - the full app icon;
`icon_foreground.png` - the same mark centered with padding, for Android's
adaptive icon safe zone). After changing either:

```sh
flutter pub get
dart run flutter_launcher_icons
```

## Tests and screenshots

```sh
/opt/flutter-3.47/bin/flutter test                                   # everything
/opt/flutter-3.47/bin/flutter test test/screenshots --update-goldens # redraw the PNGs
```

`test/ui/` covers the design system (including a WCAG contrast check of
both themes), `test/shell/` the navigation shell, and `test/screenshots/`
renders every screen and shared widget against a fake server built from
`test/screenshots/fixtures/`. The UI rules and how to use the shared
widgets are in `docs/specs/2026-09-25-mobile-design-system.md`.

## Known limitations

- Self-signed/untrusted HTTPS certificates aren't handled specially -
  either use plain HTTP on your LAN or Tailscale (cleartext is allowed
  there; the app refuses to send passwords over plain HTTP to an internet
  address), or a certificate Android already trusts.
- Background notifications arrive within about 15 minutes (Android
  batches the job); only the server the app is signed in to is checked.
- iOS: not yet added.
