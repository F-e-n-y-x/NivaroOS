# NivaroOS Privacy Policy

_Last updated: 10 October 2026_

NivaroOS is free, open-source software that you install and run on **your own server**. This policy covers the NivaroOS server software, its web dashboard, the NivaroOS Android app, and the "NivaroOS" Google, Microsoft and Dropbox sign-in apps that a NivaroOS owner creates for their own server.

## The short version

- **Your data stays on your server.** The NivaroOS project and its developers never receive, see, store or sell your files, accounts or usage.
- **No telemetry, analytics, ads or tracking.** NivaroOS doesn't report back to anyone.
- **Cloud accounts you connect** talk directly between your server and that provider. Nothing passes through us.

## What NivaroOS stores, and where

All of it is stored only on the server you installed NivaroOS on, under your control:

- **Your account on the server:** username, password hash, profile picture and settings.
- **Your files**, apps, virtual machines, backups and their logs.
- **Connected cloud accounts** (for example Google Drive, OneDrive, Dropbox, TeraBox): the sign-in tokens, and the client ID and secret if you use your own sign-in app. They live in the server's rclone configuration, readable only by the system administrator (root).
- **Phone backups** made with the Android app: the photos, files, contacts, messages, call logs and other categories you choose to back up.

You can delete any of it at any time. Remove the account in Settings, delete the files, or uninstall NivaroOS with `sudo nivaroos-uninstall --delete-data`.

## Google user data

If you connect Google Drive, NivaroOS asks Google for the **`https://www.googleapis.com/auth/drive`** permission. It uses it only to do what you ask:

- show your Drive in Files and mount it on your server;
- copy files to and from your Drive for backups, restores and syncs you set up;
- move files you delete into Drive's trash.

Google user data is **never** sent to the NivaroOS developers or to anyone else. It's never used for advertising, never sold, and never used to train AI models. It goes only between your server and Google. Disconnecting the account in **Settings → Cloud → Remove** deletes the stored token. You can also revoke access at any time at <https://myaccount.google.com/permissions>.

NivaroOS's use of information received from Google APIs follows the [Google API Services User Data Policy](https://developers.google.com/terms/api-services-user-data-policy), including the Limited Use requirements.

The same rules apply to Microsoft OneDrive and Dropbox accounts.

## When NivaroOS connects to the internet

Your server makes outgoing connections only for features you use:

- **App Store:** downloads app catalogs (for example from casaos.app and the BigBear app store) and app icons, and pulls container images from registries like Docker Hub and GitHub.
- **Updates:** checks GitHub for new NivaroOS app releases.
- **Cloud drives and backups:** the providers you connect.
- **Remote access:** Tailscale, if you set it up.
- **Download Station, speed tests and the built-in browser:** the sites you open or download from.

These services see your server's IP address, as with any internet connection, and their own privacy policies apply.

## Android app

The app connects only to the NivaroOS servers you add to it. It asks Android for permissions (photos, files, contacts, messages, call log, notifications) only for features you turn on, such as **Back up this phone**. The data goes only to your own server. The app has no ads, analytics or crash-reporting services.

## Children

NivaroOS isn't directed at children under 13 and doesn't knowingly collect their data. The project collects no data at all.

## Changes

Changes to this policy are published in this file, and its history is visible on GitHub.

## Contact

Questions: open an issue at <https://github.com/F-e-n-y-x/NivaroOS/issues>.
