# NivaroOS Mobile

A real, native Android app for NivaroOS (built with [Flutter](https://flutter.dev),
so the same codebase can add iOS later - `flutter create --platforms ios`,
built on a Mac with Xcode, no code here assumes Android specifically). This
is **not** a wrapper around the web dashboard - it's its own native UI
talking directly to NivaroOS's REST APIs. The one deliberate exception is
a VM's live console screen, which embeds the existing, already-working
web console for that one specialized, protocol-heavy view - see
`lib/screens/vm_console_screen.dart` for the reasoning.

## Features

- **Auto-discovery**: finds NivaroOS servers on your local network via
  mDNS/DNS-SD (the same mechanism printers and Chromecasts use to announce
  themselves) - no typing an IP address unless your network blocks
  multicast traffic, in which case manual entry is right there too.
- **Native login**, talking to the real `/v1/users/login` API.
- **Dashboard**: live CPU/memory/storage stats.
- **Files**: browse, download, upload, delete - real native Android file
  handling (native file picker, native "open with", proper folder
  navigation), not an embedded file browser page.
- **VMs**: list, start/stop, and open a live console.
- **Settings**: change server, log out.

## Architecture

- `lib/services/api_client.dart` - the main NivaroOS gateway API (auth,
  dashboard, files). Auto-refreshes an expired session token once before
  giving up, matching the web app's own retry behavior.
- `lib/services/vm_client.dart` - the VM sidecar's own separate API (a
  different port from the main gateway, and - as found and documented
  during this app's own security review of NivaroOS itself - currently no
  authentication of its own; this client reflects that reality rather than
  pretending otherwise).
- `lib/services/discovery_service.dart` - mDNS server discovery.
- `lib/services/storage_service.dart` - the server address + session
  tokens, kept in the platform keystore (`flutter_secure_storage`), not
  plain SharedPreferences.
- `lib/screens/` - one file per screen; `home_shell.dart` is the bottom
  tab bar shell.

## Server-side requirement for auto-discovery

The app finds servers via mDNS (`_nivaroos._tcp`). NivaroOS's own installer
(`installer/install.sh`'s `install_mdns_advertisement` step) sets this up
automatically - it installs `avahi-daemon` if missing and publishes
`/etc/avahi/services/nivaroos.service` advertising whatever port the
dashboard was configured on. Without this, the app still works fine -
users just enter their server's address manually the first time.

## Building it yourself

Requires a JDK (17-21), the Android SDK (`platform-tools`,
`platforms;android-34` or newer, `build-tools;34.0.0` or newer), and
[Flutter](https://docs.flutter.dev/get-started/install) (stable channel).

```sh
cd mobile
flutter pub get
flutter build apk --debug   # -> build/app/outputs/flutter-apk/app-debug.apk
```

For a release build you'll need your own signing key (see
[Flutter's signing guide](https://docs.flutter.dev/deployment/android)) -
`app/build.gradle` currently builds debug-signed only.

## Installing the APK

This is a debug build, not a Play Store release - Android will warn about
installing from an unknown source the first time; that's expected for a
self-hosted app like this one.

## Regenerating the app icon

Source images live in `assets/icon/` (`icon.png` - the full app icon;
`icon_foreground.png` - the same mark centered with padding, for Android's
adaptive icon safe zone). After changing either:

```sh
flutter pub get
dart run flutter_launcher_icons
```

## Known limitations (v1)

- File uploads are single-shot (no chunking/resume) - fine for typical
  phone-sized files, but the web app's own chunked-upload protocol for
  very large files isn't replicated here yet.
- Self-signed/untrusted HTTPS certificates aren't handled specially -
  either use plain HTTP on your LAN (cleartext traffic is allowed), or a
  certificate a real CA (or one Android already trusts) has issued.
- No push notifications / biometric app-lock yet.
- iOS: not yet added.
