# NivaroOS Companion App

A native Android (and, later, iOS) app for NivaroOS. Like the Home
Assistant, Plex, or Portainer apps, this isn't a separate reimplementation
of the dashboard - it's a thin native shell that connects to *your own*
NivaroOS server and shows you the real, live dashboard, so every feature
(Files, VM console, Settings, everything) is available with zero
duplicated code to maintain, and updates the moment your server does.

## How it works

1. On first launch, the app asks for your server's address (the same
   thing you'd type into a browser - an IP like `192.168.1.10` or a
   domain name) and remembers it.
2. From then on, it loads your NivaroOS dashboard directly, full-screen,
   with a native app icon/splash screen instead of a browser tab.
3. "Change Server" is available from the account menu inside the app
   (Settings/account panel) if you ever need to point it at a different
   NivaroOS install.

This directory (`mobile/`) is a [Capacitor](https://capacitorjs.com/)
project. `www/index.html` is the *entire* bundled content - just that
first-run "enter your server" screen; everything else the app shows comes
live from your server, not from anything baked into the app itself.

## Building it yourself

Requires Node.js, a JDK (21+), and the Android SDK (command-line tools +
`platform-tools` + `platforms;android-34` + `build-tools;34.0.0`).

```sh
cd mobile
npm install
npx cap sync android
cd android
./gradlew assembleDebug   # -> app/build/outputs/apk/debug/app-debug.apk
```

Or push to `master` and download the built APK from this repo's GitHub
Actions run (`.github/workflows/android-build.yml`) - no local Android
toolchain needed.

## Installing the APK

This is a debug build (self-signed automatically by Gradle), not a Play
Store release - Android will warn about installing from an unknown source
the first time; that's expected for a self-hosted app like this one.

## Regenerating icons/splash screens

Source images live in `assets/` (`icon-only.png`, `icon-foreground.png`,
`icon-background.png`, `splash.png`, `splash-dark.png`). After changing
any of them:

```sh
npx capacitor-assets generate --android
```

## Known limitations (v1)

- Self-signed/untrusted HTTPS certificates aren't handled specially -
  either use plain HTTP on your LAN (cleartext traffic is allowed), or a
  certificate a real CA (or one Android already trusts) has issued.
- No native file picker / push notifications / biometric lock yet - the
  live dashboard's own upload/console/etc. UI is used as-is. These are
  natural follow-ups since the Capacitor plugin bridge is already wired
  in (see `ui/src/utils/nativeApp.js`).
- iOS: not yet added (`npx cap add ios` on a Mac with Xcode would do it -
  the bootstrap page and app code here don't assume Android specifically).
