# NivaroOS — continue here (written 2026-11-01, before formatting the PC)

Give this file to Claude Code in the new install: "read /DATA/blue/Projects/NivaroOS/CONTINUE.md and continue".
A copy without the local paths is in the repo: `docs/CONTINUE.md`.

## Where everything is

| What | Where | Survives the format? |
|---|---|---|
| Code (all pushed) | GitHub `F-e-n-y-x/NivaroOS`, branch `master` | yes |
| Main checkout (59 uncommitted changes, 4 stashes from an older session) | `/DATA/blue/Projects/NivaroOS/repo` (blue drive) — also pushed as `refs/wip/main-checkout-uncommitted-2026-11-01` + `refs/wip/main-checkout-stash-0..3-2026-11-01` (tracked changes only; the 23 untracked files are only on the blue drive) | yes (blue) |
| Agent work area | `/DATA/blue/Projects/NivaroOS/wip` (rules file `AGENT-RULES-reliability.md`, `webrtc-contract.md`, `progress-*.md`, `bundle/` build worktree) | yes (blue) |
| Go caches | `/DATA/blue/Projects/NivaroOS/cache/go-build`, `go-mod` | yes (blue) |
| Saved-before-format copies | `/DATA/blue/Projects/NivaroOS/before-format-2026-11-01/` | yes (blue) |
| ↳ NivaroOS settings backups (encrypted .7z) | `…/settings-backup/` (copied from `/DATA/Backup/NivaroOS settings`, which is on the SYSTEM drive) | yes (blue) |
| ↳ Server release signing key (minisign) + Android release keystore + key.properties | `…/release-signing/` (also on `/DATA/tower/NivaroOS-release-key/`) | yes |
| ↳ Claude memory + settings.json | `…/claude/` | yes |
| **Settings backup KEY** | **not saved yet — owner: Settings › Backup › "NivaroOS settings" › Show settings key → save it next to the backups** | — |

Docker data (/var/lib/containerd, ~50 GB images incl. ComfyUI, Immich), /etc/nivaroos, /var/lib/nivaroos and rclone.conf live on the system drive: they are inside the encrypted settings backup (not Docker images — those re-download).

## After the format — rebuild steps
1. Install the OS, mount the drives (blue = ext4 UUID 3af9934e-2f68-4d24-9ed6-d3c5f4049ad0 at /DATA/blue; tower NTFS at /DATA/tower; tank at /DATA/tank).
2. Install NivaroOS with the settings restore:
   `curl … install.sh | sudo bash -s -- --restore "/DATA/blue/Projects/NivaroOS/before-format-2026-11-01/settings-backup/<newest>.7z.001"` (asks for the settings key). Or install normally, then Welcome screen › "Restore from a NivaroOS settings backup". Apps are recreated automatically (Immich etc.); their data under /DATA/AppData must be on a data drive.
3. Restore dev setup: symlink `/root/NivaroOS` → `/DATA/blue/Projects/NivaroOS/repo` and `/root/nivaroos-wip` → `/DATA/blue/Projects/NivaroOS/wip`; copy `…/release-signing/nivaroos-server.{key,pub}` to `/root/.nivaroos-release/` (0700/0600); `go env -w GOCACHE=/DATA/blue/Projects/NivaroOS/cache/go-build GOMODCACHE=/DATA/blue/Projects/NivaroOS/cache/go-mod`; Go 1.27.2 at /usr/local/go; Flutter at /opt/flutter-3.47; pnpm; gh (login); minisign (apt).
4. Claude: copy `…/claude/memory/*` to `~/.claude/projects/-root/memory/`, `…/claude/settings.json` to `~/.claude/`; add `"permissions":{"additionalDirectories":["/DATA/blue/Projects"]}` (stops repeated "accept" prompts).
5. Re-sign-in Google Drive if needed (own OAuth app, published "In production"): Settings › Online Storage › Reconnect (uses the saved app).

## State on 2026-11-01
- Server: **1.5.4** (signed releases, self-updater auto-installs ~04:00; 1.5.2/1.5.3 marked broken pre-releases). Fixes after 1.5.4 were deployed directly on this box but are NOT in a release yet: cloud sign-in visibility + "Sign-in expired"/Reconnect with saved app (local-storage), app add/reconnect cloud endpoint, Host Desktop encoder start-grace/hang fix + cursor shapes (vm-sidecar). → **publish server 1.5.5 first** (`installer/release-server.sh 1.5.5`, after a CHANGELOG section).
- App: **1.4.21** released (cloud add/reconnect in the app — owner to confirm on the phone).
- Google Drive: reconnected 2026-10-20 (own app, In production).

## Tasks left (in order)
1. **Release server 1.5.5** (above).
2. **Release-safety gate:** `installer/release-server.sh` must boot every service from the bundle (throwaway root/netns, like the files e2e) and check /ping + health before signing — 1.5.3 shipped a gateway that crashed (viper 1.21 dropped ini).
3. **Updater bug:** with 1.5.3 marked bad, `nivaroos update check` offered 1.5.2 instead of the newer 1.5.4 (cli/update pickRelease/caching) — reproduce in a test and fix.
4. **Held-back dependency updates** (≥2-weeks-old rule, eligible since ~24–30 Oct): Vue 3.5.44, DOMPurify 3.4.17, vue-i18n, qs, sass, @codemirror/*, marked 18.1, tiptap 3.32; rclone 1.75.2; Go x/crypto 0.58, x/sys, x/net, x/image, kin-openapi, docker/cli, moby client, pion/webrtc 4.2.24, dbus, klauspost/compress, gjson; app open_filex 5. Keep gateway on viper 1.19 (or add an ini codec + keep the gateway.ini test).
5. Owner decisions pending: Host Desktop "smooth adapt" (bitrate change without encoder restart) vs "fine as is"; "priority" (nice/IO priority for the WebRTC encoder under load); Docker data → blue drive; Gradle cache → blue.
6. Optional features (research list /DATA/Downloads/NivaroOS-feature-gaps-2026-10.md (copy on the blue drive under NivaroOS-before-format-2026-11-01/reports/)): alert channels (email/ntfy/Telegram), app-aware backups before app updates, "Previous versions" snapshots, user roles, Photos timeline, UPS (NUT). Smooth Host Desktop on KDE/wlroots Wayland.
7. Owner said NOT needed: LAN HTTPS (exists, off), two-factor login.

## Owner rules (also in Claude memory)
Commit/push straight to master; every fix ships in the project (installer/migrations); top-quality UI; build APKs here (keystore cert SHA-256 d36dcc45…86565, never regenerate); load ponytail for coding and tell subagents; ADHD answer format; owner uses **Firefox on Windows** — test web/Host Desktop there; never touch other sessions' Gradle/emulators or the owner's live :0 desktop/x11vnc/VMs; never take Drive/cloud actions (delete/untrash/start backups) without asking; agents max ~15 at a time.
