# Use your own app for Google Drive, OneDrive or Dropbox

When you add Google Drive, OneDrive or Dropbox in **Settings → Cloud**, NivaroOS lets you choose how to sign in:

| | rclone's app (default) | Your own app |
|---|---|---|
| Setup | Nothing extra | About 10 minutes, once |
| Speed | Shared by every rclone user, so the provider slows it down. Big backups crawl and checks can time out | Your own speed limits, much faster for big backups like photo libraries |
| Best for | Small folders, trying things out | Backups of hundreds of GB or tens of thousands of files |

Your client ID and secret are stored only on your server, in the same protected config file as the sign-in itself. You can switch at any time: **Settings → Cloud → the account → Reconnect**. Files already uploaded stay where they are, and backups carry on without uploading them again.

---

## Google Drive

### 1. Create the app (once)

1. Open <https://console.cloud.google.com/> and sign in with the Google account you back up to.
2. At the top, open the project list → **New project**. Name it, for example `NivaroOS`, then **Create** and select it.
3. Go to **APIs & Services → Library**, search for **Google Drive API**, and click **Enable**.
4. Go to **Google Auth Platform** (called **OAuth consent screen** in older consoles):
   1. **Branding:** set an app name (for example `NivaroOS`), your email as the support email, then **Save**.
   2. **Audience:** choose **External**. Under **Test users**, add your own Google address.
   3. Still on **Audience**, click **Publish app** so it shows **In production**. **This matters:** while an app is in *Testing*, Google ends its sign-ins after **7 days**. A personal app doesn't need Google's review. You'll only see an "unverified app" warning when you sign in.
5. Go to **Clients → Create client**:
   - **Application type:** **Desktop app**. Don't pick "Web application": rclone's sign-in returns to `http://127.0.0.1:53682/`, which only Desktop apps accept without extra setup.
   - There is no URL field for a Desktop app. That's expected: Google accepts the `127.0.0.1` address rclone uses on its own.
   - Name: anything. Click **Create**.
6. Copy the **Client ID** (ends in `.apps.googleusercontent.com`) and the **Client secret** (starts with `GOCSPX-`).

### 2. Use it in NivaroOS

1. **New account:** Settings → Cloud → **Add** → Google Drive.
   **Existing account:** Settings → Cloud → the account → **Reconnect** (the ↻ button).
2. Choose **Your own app**, then paste the Client ID and Client secret.
3. Click **Run it in Terminal** and open the link it prints.
4. Sign in. Google says **"Google hasn't verified this app"**: click **Advanced → Go to NivaroOS (unsafe)**. It's your own app. Then allow access.
5. If the browser ends on a `127.0.0.1:53682` page that doesn't load, replace `127.0.0.1` in the address bar with your server's address (the one in the dashboard's address bar), then press Enter.
6. The terminal prints a token, a line starting with `{"access_token"`. Copy it, paste it into step 3 of the form, and click **Connect** (or **Reconnect**).

The account list now shows **Your own app** next to the account.

> Running `rclone authorize` on your own computer works too. Install rclone there, run the command the form shows, and paste the token back.

---

## OneDrive

1. Open <https://entra.microsoft.com/> → **Applications → App registrations → New registration**.
2. Name it, and under **Supported account types** pick **Accounts in any organizational directory and personal Microsoft accounts**.
3. **Redirect URI:** platform **Web**, `http://localhost:53682/`. Click **Register**.
4. Copy the **Application (client) ID**.
5. **Certificates & secrets → New client secret**, then copy its **Value**. It is shown only once.
6. **API permissions → Add a permission → Microsoft Graph → Delegated:** add `Files.Read`, `Files.ReadWrite`, `Files.Read.All`, `Files.ReadWrite.All`, `offline_access`, `User.Read`, `Sites.Read.All`.
7. In NivaroOS, follow **Use it in NivaroOS** above with **Your own app**.

## Dropbox

1. Open <https://www.dropbox.com/developers/apps> → **Create app** → **Scoped access** → **Full Dropbox**.
2. **Permissions:** tick `files.metadata.write`, `files.content.write`, `files.content.read`, `sharing.write`, `account_info.read`, then click **Submit**.
3. **Settings:** under **Redirect URIs**, add `http://localhost:53682/`. Copy the **App key** (the client ID) and the **App secret**.
4. In NivaroOS, follow **Use it in NivaroOS** above with **Your own app**.

---

## Going back to rclone's app

Settings → Cloud → the account → **Reconnect** → **rclone's app** → sign in again. NivaroOS clears your client ID and secret.

## Troubleshooting

- **"has not completed the Google verification process" / `Error 403: access_denied` (Google):** the app is still in *Testing* and your account isn't a test user. Go to **Audience → Publish app** (or add your address under **Test users**), then sign in again.
- **`redirect_uri_mismatch` (Google):** the client isn't a **Desktop app**. Create a Desktop client and use that one.
- **Signed out again after a week (Google):** the app is still in *Testing*. Publish it (step 1.4.3), then reconnect.
- **`invalid_client`:** the ID or secret was pasted with a missing character. Copy both again.
- **Still slow:** Google allows 750 GB of uploads per account per day. A backup that reaches it stops cleanly and continues the next day.
