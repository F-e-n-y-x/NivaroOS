# Download Station browser v2: a real browser on the server

Status: built (2026-09-29; see "Built" at the end). Replaces the rewriting proxy as the
default Download Station browser. The proxy stays in the product as "Lite mode".

What the owner asked for:

> "in download station browser is a mess i cant open any website properly load
> time is just bad and not proper ux, i have tried multiple website but loading
> is just bad and adblock is not even working, no easy way to copy the url, no
> way to open in new tab in the browser of download station, add a right click
> menu in the browser, google is not even work cuz of some security issue"

Next task, designed here too:

> "for terabox in online storage ... it requires cookie and capturing cookie is
> a hassle so an in-app browser which helps in login and then auto captures the
> login cookie - or you have any better idea"

**Decision: build direction (b).** The server runs a real Chromium, controlled
over CDP. The page gets the browser as a picture stream, and the user's mouse
and keyboard input goes back to it. TeraBox sign-in uses the same browser: the
user signs in, and the cookie is read with `Network.getCookies` in one click.
The mobile app also gets an Android WebView sign-in as a second path.

---

## 1. What is wrong today (measured 2026-09-29)

How it was measured: a private copy of the current sidecar ran on
127.0.0.1:28699, using a copy of the live filter lists and settings, so the
live service was not touched. Real headless Chrome 154, driven over CDP, loaded
each site twice: once directly, and once through `/b/<sid>/...` as a top-level
page. The browser cache was off. The browser and the proxy were on the same
machine, so bandwidth was unlimited. On a real client over Wi-Fi or a remote
link, every byte difference below costs time.

| Site | Direct: load / reqs / KB | Proxy: load / reqs / KB | Proxy notes |
|---|---|---|---|
| en.wikipedia.org/wiki/Linux | 1.66 s / 44 / 595 | 1.89 s / 43 / **2 663** | 7 requests went around the proxy |
| bbc.com/news | 2.57 s / 145 / 2 904 | 0.51 s / 76 / **3 966** | about half the requests never started |
| github.com | 1.19 s / 152 / 3 829 | 1.31 s / 168 / **12 293** | 8 requests went around the proxy |
| youtube.com | 1.74 s / 141 / 4 017 | 1.88 s / 141 / **18 313** | 4.6x the bytes |
| google.com/search | (headless UA: `/sorry` captcha) | 13 reqs, stuck on the `enablejs` page | see §1.3 |
| duckduckgo.com/?q= | 0.93 s / 56 / 2 155 | **21.3 s** / 81 / 7 958 | load event held back by a hung request |
| theverge.com | 4.62 s / 252 / 4 549 | 4.96 s / 156 / 7 753 | 23 % less page text |
| terabox.com | 4.02 s / 299 / 7 598 | 2.58 s / **23** / 6 716 | **blank page**: 0 characters of text |

Loading the top-level document through the proxy is fast: time to first byte
was the same as direct, or better. The slowness the owner sees comes from
everything after that document:

1. **3x to 5x more bytes.** The proxy decompresses every upstream response and
   sends it to the browser with no compression. Every HTML page also carries
   about 43 KB of injected shim and generic cosmetic CSS. On a 50 Mbit/s Wi-Fi
   link, YouTube's 18 MB alone takes about 3 s before any rendering.
2. **The browser cache is useless.** Every URL contains the session ID, and a
   new session is created each time the window opens. HTML is also served
   `no-store`. Every visit is therefore a cold load.
3. **One origin carries every site.** All sites are served from
   `http://<box>:28642` over HTTP/1.1, so the browser opens at most 6
   connections for every site and every third party combined. A page with 150
   requests waits in that queue. Direct loading spreads the requests over many
   origins and uses h2 or h3.
4. **Requests go around the proxy (7 to 8 per page).** URLs built by workers,
   service workers, `import()` and some srcset and CSS-in-JS paths skip the
   rewriting. They are not ad-filtered, and CORS often breaks them.
5. **WebSockets return 501.** Chat, live players and many single-page apps
   break.

### 1.1 Why "open in new tab" and the right-click menu don't work
- The iframe `sandbox` does not include `allow-popups`. Because of that,
  `target=_blank` links and `window.open` fail silently.
- The right-click menu is the host browser's own menu, so "Copy link address"
  copies the `/b/<sid>/...` proxy URL. The real URL can only be copied from
  More > Copy address.
- The browser cannot be used at all when NivaroOS is opened over HTTPS (a
  mixed-content block: the page shows "isn't available over HTTPS").

### 1.2 Why the ad blocker "doesn't work"
The engine itself works: 138 770 network filters and 30 640 cosmetic filters
built in 200 ms. It fails in practice for three reasons:
- **Scriptlets (`##+js`) and procedural cosmetic filters are skipped** (see
  `adblock.go:24`). YouTube, Twitch and most anti-adblock sites are handled by
  exactly those filters, so the ads the owner notices are the ones that get
  through.
- Requests that go around the proxy (item 4 above) are never matched.
- Element hiding only covers content that `inject.js` sees.

### 1.3 Why Google fails ("some security issue")
Google search answers with its SearchGuard `enablejs` or `/sorry` challenge. The
challenge script checks the page's real origin and the browser's signals. On the
proxied origin (`127.0.0.1:28642`), with the sidecar's fixed Chrome/128 UA sent
in headers and the real UA visible in JS, it never passes. Google sign-in will
not work in a proxied frame either: it refuses embedded and third-party
contexts. A rewriting proxy cannot fix this. It is the same reason no
proxy-based browser works with Google.

A real Chrome got the same `/sorry` page while its UA said `HeadlessChrome`.
After setting a normal UA with `Emulation.setUserAgentOverride`, plus
client-hint brands (`Google Chrome`/`Chromium`), **the same headless Chrome
loaded a normal results page (9 results, `navigator.webdriver=false`).**

### 1.4 Security problems in the proxy design
- Every site shares one origin, so they share localStorage.
- Cookies that a page's JS sets land on the proxy origin at path `/`. The proxy
  then forwards the browser's `Cookie` header to **every** site. That is a
  cross-site cookie leak.

The README already warns against using this browser for logins. That rules it
out for the TeraBox sign-in the owner wants.

## 2. The two directions

### (a) Keep the rewriting proxy and fix it
Quick wins: gzip or brotli on proxy responses, a stable per-user session prefix
so the cache works, `allow-popups` handled as a DS tab, and a right-click menu
built from `inject.js`. About 300 LOC. These bring YouTube from 18 MB back to
about 5 MB and make "new tab" and "copy link" work.

A full fix means a service-worker rewriting engine in the style of Ultraviolet
or Scramjet, a WebSocket tunnel and a scriptlet engine: about 6 000 to 8 000
LOC. Even then, the following would stay broken:
- Google search and sign-in, Cloudflare Turnstile, and OAuth pop-ups.
- The shared-origin isolation problem, which cannot be fixed in this design.

It would be a permanent game of catching up with sites.

### (b) A real browser on the server (chosen)
Chromium runs headless on the box and is controlled over CDP. The UI draws the
page as a picture stream (`Page.startScreencast` JPEG frames on a canvas) and
sends input back (`Input.dispatch*`).

Every site then runs in a real, isolated browser:
- Google, sign-ins, WebSockets and new tabs all work.
- The UI origin never runs any site code. It only receives pixels.
- File downloads are captured at the source (`Browser.downloadWillBegin`) and
  handed to the multi-connection engine with the tab's real cookies.

Measured costs on this box (Chrome 154, sandboxed, running as an unprivileged
user):

| | |
|---|---|
| Browser idle, no pages | **251 MB** PSS |
| 3 tabs (Google x2, Wikipedia) | **594 MB** PSS |
| 5 tabs (+ YouTube, GitHub) | **975 MB** PSS (about 150 to 200 MB per heavy tab) |
| Screencast, JPEG q70, 1280x800, continuous wheel scroll | **21.8 fps, 2.9 MB/s**, about 135 KB per frame |
| Screencast, static page | 0 fps (frames are sent only when something changes) |
| Page load time | same as direct Chrome (first table above), with compression and cache |
| `Extensions.loadUnpacked`, `Browser.setDownloadBehavior` | both available on Chrome 154 |

The costs are bandwidth while scrolling and about 0.25 to 1 GB of RAM while in
use. Both are managed by adaptive quality (§5) and by starting the browser on
demand and stopping it when idle (§8). WebRTC video (H.264) was considered.
Chrome can only feed it through a capture-and-encode pipeline (ffmpeg plus a
virtual display). That is a large dependency for smoother scrolling only, so it
was rejected. JPEG screencast with a sharp still frame when idle is enough for
a browser used to reach download pages.

**Direction (a) is kept only as a fallback, "Lite mode".** It is used when
Chromium cannot be installed or the box has less than 2 GB of RAM. It gets the
300 LOC of quick wins, because they are cheap and it stays in the product.

## 3. Architecture

```
 Web UI (canvas)  ──WS /v1/download-station/rb/ws──▶ gateway ──▶ download-sidecar (root, hardened)
                                                                     │  auth (admin JWT), tabs API,
                                                                     │  download hand-off, cookie export
                                                                     ▼  unix socket /run/nivaroos/ds-browser.sock
                                              nivaroos-ds-browser.service  (User=nivaroos-browser)
                                                 `nivaroos-download-sidecar browser-host`
                                                   ├─ Chromium per NivaroOS user (--remote-debugging-pipe)
                                                   ├─ egress proxy 127.0.0.1:<rand> (netguard + host blocklist)
                                                   └─ idle reaper
```

- **Why the browser needs its own unit.** The sidecar unit is hardened with
  `MemoryDenyWriteExecute=true` and `RestrictNamespaces=true`. V8's JIT and
  Chrome's namespace sandbox cannot run under those settings. Chrome also
  refuses to sandbox as root. So the browser runs in
  **`nivaroos-ds-browser.service`**:
  - It is the same Go binary started in a `browser-host` subcommand. No new Go
    module is needed.
  - It runs as the system user `nivaroos-browser`, with Chrome's own sandbox
    **on**.
  - Hardening: `ProtectSystem=strict`, `ProtectHome=true`, `PrivateTmp`,
    `InaccessiblePaths=-/DATA -/media -/mnt -/var/lib/nivaroos` except its own
    `StateDirectory=nivaroos/ds-browser`, and `NoNewPrivileges`.
  - It keeps no capabilities.
  - It allows only `AF_UNIX AF_INET AF_INET6 AF_NETLINK`.
  - Resource limits: `MemoryHigh=1.5G`, `MemoryMax=2G` (settable in DS
    settings), `CPUQuota=200%`, `Nice=5`, `TasksMax=512`.
- **Starts on demand.** `nivaroos-ds-browser.socket` activates the service
  when the sidecar first connects. The host exits after 10 minutes with no
  viewer, systemd stops it, and the RAM goes back to 0. The sidecar needs no
  privilege to start it.
- **One Chromium instance per NivaroOS user.** Each user's profile lives in
  `/var/lib/nivaroos/ds-browser/profiles/<uid>/` (0700), so logins persist and
  no cookies are shared between users. At most 2 instances run at once (a
  third user queues with a clear message). Each user has at most 12 tabs.
- **CDP runs over `--remote-debugging-pipe`.** No TCP debug port exists for a
  page or a LAN host to reach. The client is a small hand-written CDP JSON
  client (about 400 LOC). chromedp's generated cdproto (more than 20 MB of
  code) is too heavy for the handful of domains used.
- **Chrome flags:**
  - `--headless=new --no-first-run --no-default-browser-check`
  - `--disable-background-networking --disable-sync --disable-quic`
  - `--force-webrtc-ip-handling-policy=disable_non_proxied_udp`
  - `--proxy-server=http://127.0.0.1:<rand> --proxy-bypass-list="<-loopback>"`
  - `--deny-permission-prompts --disable-features=MediaRouter,Translate,OptimizationHints`
  - `--user-agent=<normal Chrome UA>`

  On each target, `Emulation.setUserAgentOverride` also sets UA metadata with
  normal brands. This is the change that made Google work in §1.3. No
  `--enable-automation` is passed, so `navigator.webdriver` stays false.
- **Tabs are CDP page targets.** One flat CDP connection is used, with
  `Target.setAutoAttach` and `setDiscoverTargets`. A `window.open` or
  `target=_blank` link shows up as `Target.targetCreated` with an `openerId`,
  and appears in the UI as a new tab next to its opener.

## 4. Security

1. **Who can use it.** Admins only, as for all of Download Station
   (`auth.go`). The WS is at `/v1/download-station/rb/ws?token=`. Access goes
   through the gateway, so HTTPS works: this removes the "not available over
   HTTPS" dead end. Rules:
   - The same JWT check as the rest of the API.
   - An `Origin` check against the UI host.
   - The user in the token picks the Chromium profile.
   - A token for user A can never attach to user B's instance.
2. **SSRF / netguard.** All Chromium traffic goes through the host's egress
   proxy. It dials through the existing `netguard.go` dialer, so the rules are
   the same as today:
   - Loopback, link-local, 0/8 and multicast are never reachable.
   - Private, CGNAT (Tailscale) and the box's own addresses are reachable only
     for hosts the user **typed** in the URL bar. That list is kept per
     profile, and it is managed by the sidecar, never by pages.
   - DNS is resolved inside the proxy, and the check runs at dial time on the
     resolved IP, which also stops DNS-rebinding attacks.
   - QUIC and non-proxied WebRTC UDP are disabled, so nothing bypasses the
     proxy.
   - `<-loopback>` stops Chrome's implicit localhost bypass.

   Pages cannot navigate to `file:`, `chrome:` or `devtools:` (Chrome blocks
   this for web content). The URL bar accepts only `http`, `https` and
   `about:blank`. The browser user also cannot read `/DATA` or NivaroOS state
   (unit hardening).
3. **Isolation.** Site code runs only inside sandboxed Chromium renderers. The
   UI receives JPEG frames and a JSON tab list. Page titles and URLs are shown
   as text only (never `v-html`).
4. **Clipboard.** Pages get no clipboard permission. Copy works only through
   our own actions (Ctrl+C or the menu): the host reads the selection or link,
   and the UI writes it with `navigator.clipboard.writeText`. Paste works only
   when the user presses Ctrl+V or picks Paste: the UI reads its clipboard and
   the host runs `Input.insertText`.
5. **Permissions and dialogs.** Geolocation, camera, microphone, notifications,
   MIDI and USB are all denied. JS alert/confirm/prompt, HTTP basic auth
   (`Fetch.authRequired`) and certificate errors are shown as NivaroOS dialogs
   and never auto-accepted. A certificate error gets an interstitial page with
   "Go back" as the default action.
6. **Cookie export.** Only `POST /rb/cookies/export` returns cookies, with
   `{provider:"terabox"}`. It is limited to an allow-list of domains for each
   provider (§9), is admin only and is audit-logged. There is no general
   "dump all cookies" endpoint.
7. **Profiles.** The user can wipe their profile from the UI with "Clear
   browsing data & sign out of sites". Deleting a NivaroOS user deletes that
   user's profile.

## 5. API and WS protocol

REST (sidecar, admin JWT):
- `GET /rb/status`: `{available, reason?, running, mem_mb, tabs, engine:"chromium"|"lite"}`
- `POST /rb/cookies/export` `{provider, context?}`: `{cookie, account_hint, domains}` (§9)
- `DELETE /rb/profile`: stops the instance and wipes the profile
- `POST /rb/typed` `{host}`: allows a private host (same as today)

WS `/rb/ws`. Text frames are JSON `{t:...}`. Binary frames carry pictures.

Client to server:
| t | fields | effect |
|---|---|---|
| `hello` | `w,h,dpr,touch,quality?` | attach and resize the viewport (`Emulation.setDeviceMetricsOverride`, dpr capped at 2) |
| `resize` | `w,h,dpr` | same as above |
| `tab.new` | `url?, background?` | `Target.createTarget` |
| `tab.close` / `tab.activate` | `tab` | activating starts the screencast on that tab only; background tabs get `Page.setWebLifecycleState frozen` after 5 min |
| `nav` | `tab,url` | `Page.navigate`, after the URL bar checks |
| `back` / `fwd` / `reload` / `stop` | `tab, hard?` | history entry, `Page.reload` or `Page.stopLoading` |
| `mouse` | `type(down/up/move/wheel), x,y, button, buttons, clicks, mods, dx,dy` | `Input.dispatchMouseEvent` (moves merged to 1 per animation frame) |
| `touch` | `type, points[]` | `Input.dispatchTouchEvent` (mobile web UI) |
| `key` | `type, key, code, keyCode, mods, text?` | `Input.dispatchKeyEvent` |
| `ime` | `text, composing` | `Input.imeSetComposition` / `Input.insertText` |
| `paste` | `text` | `Input.insertText` |
| `hit` | `x,y, seq` | context-menu probe (see §6) |
| `act` | `action, seq` | runs a menu action |
| `find` | `tab, text, dir` | find in page |
| `zoom` | `tab, factor` | page zoom |
| `ack` | `frame` | frame flow control |
| `dialog` | `id, accept, text?` | answers alert/confirm/prompt/auth |
| `file` | `id, paths[] or cancel` | answers a file picker (§6) |

Server to client:
- Binary frame layout: `[u8 kind=1][u32 tab][u32 frame][u16 w][u16 h][u8 quality][jpeg]`.
- `tabs`: `[{id,url,title,favicon,loading,progress,canBack,canFwd,blocked,audible,opener}]`,
  sent when anything changes.
- `cursor` `{css}`: the hover cursor. Headless mode has no cursor event, so the
  host probes `getComputedStyle(elementFromPoint).cursor` at most 10 times per
  second, only when the hovered node changes.
- `hit` `{seq, link?, linkText?, image?, media?, selection?, editable, frame}`
- `clip` `{text}`: the result of a copy action.
- `dialog` `{id, kind, message, default?}`, `filechooser` `{id, multiple, accept}`
- `download` `{url, filename, size?, captured:id}`, `notice` `{level, text}`, `crashed` `{tab}` (offers a Reload button)

**Frame flow control.** At most 2 frames are in flight to each client. A frame
is acked to Chrome (`Page.screencastFrameAck`) only when the client acks it, so
a slow link lowers the frame rate instead of filling a queue.

**Adaptive quality.** The frame rate and ack delay are measured over a sliding
window:
- JPEG quality drops from 80 to 60 to 45 when frames back up.
- It climbs back up when the link keeps up.
- The frame size follows `w*dpr`.

**Sharp when still.** 250 ms after the last frame, the host captures one
`Page.captureScreenshot` (JPEG q92, or PNG if smaller) of the viewport, so text
at rest is crisp. This is the fix for JPEG blur on text.

## 6. UI spec (web, `DsBrowser.vue` rewrite)

The layout stays as it is today: the tab strip, then the address bar, then the
viewport. The iframe is replaced by a `<canvas>` filling the frame area, with a
hidden `<textarea>` that holds keyboard focus and IME input.

**Tab strip.**
- Shows favicon, title, spinner, audible icon and close. "+" opens a new tab.
- Middle-click closes a tab. Dragging reorders tabs.
- Right-clicking a tab gives: Reload, Duplicate, Copy address, Close, Close
  other tabs, Reopen closed tab.
- A tab a page opened appears next to its opener.

**Address bar.**
- Back and Forward. A long press on either shows a history list.
- Reload becomes Stop while loading. There is also Home and a lock icon.
- The URL field shows the **real URL** with the host highlighted. Focusing it
  selects everything.
- Enter navigates. Alt+Enter opens the URL in a new tab.
- Suggestions come from the existing server-side history.
- A copy-URL button sits inside the field, at the right. It shows a
  "Copied" check.
- A thin progress bar under the bar shows `progress` for the active tab.
- Around the field: the shield with its blocked count, a download button, and
  the "More" menu.

**Right-click menu.** It is our own menu, shown at the pointer. The client sends
`hit` and the menu contains only the sections that apply:
- **Link:** Open link in new tab · Open link in background tab · Copy link address · Copy link text · **Download link with Download Station**
- **Image:** Open image in new tab · Copy image address · Download image
- **Media:** Copy video address · Download video (only for a direct `src`, not `blob:`)
- **Selection:** Copy · Search Google/DuckDuckGo for "..." · Open as link (when the selection looks like a URL)
- **Editable field:** Undo · Cut · Copy · Paste · Select all
- **Page (always):** Back · Forward · Reload · Copy page address · Open page in a real browser tab · View page source (read-only, in a DS text window) · Save page link to Download Station

Keyboard access: arrow keys, Enter, Esc, and the menu key or Shift+F10.

**Shortcuts** (when the viewport has focus):

| Keys | Action |
|---|---|
| Ctrl+L / F6 | focus the URL field |
| Ctrl+T / Ctrl+W | new tab / close tab, where the host browser allows it; inside a normal tab, Chrome keeps these keys, so Alt+T and Alt+W also work and the menu shows them |
| Ctrl+Tab | next tab |
| Alt+Left / Alt+Right | back / forward |
| F5 / Ctrl+R | reload |
| Ctrl+F | find bar |
| Ctrl+C | copy the page selection to the real clipboard |
| Ctrl+V | paste from the real clipboard |
| Ctrl + / - / 0 | page zoom |

All other keys go to the page. In fullscreen, `navigator.keyboard.lock()` lets
Ctrl+T and Ctrl+W work as well.

**Scrolling and input.**
- Wheel and trackpad deltas are sent 1:1, merged per animation frame, so
  scrolling is smooth.
- Touch clients send `touch`, so pinch and flick work.
- The canvas matches the device pixel ratio, so it stays crisp on HiDPI
  screens.

**States.**
- **Starting** (cold start about 1 s): a skeleton with "Starting browser...".
- **Crashed tab:** a panel with a "Reload" button.
- **Instance busy:** "2 people are using the browser. Try again soon."
- **Chromium not installed:** "Using Lite mode" banner, plus an "Install full
  browser" button that runs the installer step.
- **Link too slow:** a quality badge appears.

**Downloads.**
- `downloadWillBegin` cancels the download inside Chrome. The URL, the tab's
  cookies for that URL (`Network.getCookies`), Referer and UA go to
  `Manager.Add`: the same capture flow as today, but with the correct cookies.
- A toast offers "Downloading in Download Station · Show".
- `blob:` and POST downloads, which cannot be re-fetched, finish inside Chrome
  into a staging dir. The sidecar (root) then moves the file into the chosen
  folder.

**Uploads.** `Page.setInterceptFileChooserDialog` sends a `filechooser`
message, and the NivaroOS file picker opens. The sidecar copies the chosen file
into the browser's staging dir, and `DOM.setFileInputFiles` attaches it.

**Ad blocker panel.** The existing `DsAdblockPanel` stays: global toggle,
trusted sites, lists and My filters (§7).

## 7. Ad blocking in the real browser

The **primary blocker is uBlock Origin Lite** (MV3, from gorhill). It is loaded
with `Extensions.loadUnpacked`, which is available on Chrome 154 (checked). The
reasons:
- It runs natively inside Chrome, with no per-request CDP round trip.
- It includes the **scriptlet and procedural** filtering that the Go engine
  skips. That is the part that handles YouTube and anti-adblock sites.

The installer vendors a pinned uBOL release zip, checked by sha256, and a DS
update refreshes it.

- **Counters.** CDP `Network.loadingFailed` reports `blockedReason` or
  `ERR_BLOCKED_BY_CLIENT`. The count is kept per page host, and the shield
  badge works as it does today.
- **Global toggle and trusted sites.** They are sent to uBOL's per-site
  filtering mode through its service-worker target (`Runtime.evaluate` against
  uBOL's own settings API). Turning the global toggle off disables the
  extension (`Extensions.uninstall` / load).
- **Second layer, and fallback if uBOL fails to load.** The existing Go engine
  runs in the egress proxy:
  - Host-level rules block at CONNECT (cheap, and the proxy never sees path
    detail).
  - Full-URL network filters apply through `Fetch.enable` patterns for
    `Script`, `Image`, `XHR`, `Fetch`, `Media`, `SubFrame` and `Ping`. The
    cost is about 0.1 ms per request over the pipe.
  - The existing cosmetic CSS is injected with
    `Page.addScriptToEvaluateOnNewDocument`.

  My filters and extra lists always apply through this layer, so every setting
  in the current UI keeps its meaning.
- **Spike gate (WP1).** Load uBOL on branded Chrome and on distro Chromium.
  Confirm that the YouTube pre-roll is blocked and that per-site toggling
  works. If either fails, ship the Go-engine layer only and add a small
  scriptlet set (`set-constant`, `json-prune`, `abort-on-property-read`,
  `no-setTimeout-if`, `trusted-replace-xhr-response`) as a later step.

## 8. Resource limits and lifecycle

- **Budget.** Idle, the host is 0 MB: the socket-activated unit is stopped.
  While in use it is about 250 MB plus about 150 to 200 MB for each tab loaded
  in the foreground. The unit caps are `MemoryHigh=1.5G` and `MemoryMax=2G`
  (DS setting "Browser memory limit", default `min(2G, 15 % of RAM)`).
- **Limits.**
  - 12 tabs per user.
  - Background tabs are frozen after 5 min and discarded after 30 min. A
    discarded tab keeps its URL and title, and reloads when clicked.
  - The screencast runs only for the visible tab, and only while a viewer is
    connected.
- **Idle stop.** With no viewer for 10 min, `Browser.close` runs cleanly, so
  the profile and cookies are flushed, and then the host exits. The tab list
  is saved as `tabs.json` in the profile and restored on the next start (lazy:
  only the active tab loads).
- **Crash handling.**
  - A crashed renderer triggers the `crashed` message.
  - If the browser process dies, the host restarts it once and restores the
    tabs.
  - Two crashes in 5 min switch that session to Lite mode with a notice.

## 9. TeraBox sign-in with automatic cookie capture

**Flow on the web or desktop** (Settings > Online Accounts > Add > TeraBox):

1. The dialog offers two choices:
   - **"Sign in with the browser" (recommended).** Shown when `GET /rb/status`
     returns `available:true`.
   - **"Paste cookie" (advanced).** This is the current form.
2. Choosing sign-in opens a **sign-in window**: the DS browser component in a
   compact frame, with a fixed address display and no tab strip. It runs in a
   fresh `Target.createBrowserContext`, which is an isolated cookie jar, so an
   account already signed in inside the user's normal DS profile does not
   bleed in. It opens `https://www.terabox.com/login`. Region domains are
   allowed: `terabox.com, 1024terabox.com, terabox.app, teraboxapp.com,
   1024tera.com, freeterabox.com, 4funbox.com, mirrobox.com, nephobox.com,
   momerybox.com, tibibox.com`.
3. The user signs in: email, Google, Apple, or the QR code scanned with the
   TeraBox phone app. Google sign-in works because this is a real browser.
   Captchas are solved by the user in the stream.
4. The host watches `Network.responseReceived` and polls `Network.getCookies`
   for the allowed domains every second. Once an `ndus` cookie exists, the host
   makes one check inside the page context: `GET /api/user/getinfo`, or the
   `passport` user check. The dialog then shows **"Signed in as <name>. Connect
   this account"**, with the label filled in from the account name.
5. On **Connect**:
   - The UI calls `POST /v1/download-station/rb/cookies/export`
     `{provider:"terabox", context}`.
   - The response is `{cookie:"ndus=...; lang=en; ...", account_hint}`: only
     the allowed domains' cookies, joined into the format that
     `valuedCookie()` in `backend/terabox/util.go` accepts.
   - The UI then sends that cookie straight to the existing
     `POST /v1/cloud/accounts {type:"terabox", label, params:{cookie}}`.
     local-storage creates and mounts the account.
   - The cookie is never kept in UI state after the call.
   - The sign-in browser context is disposed.
6. **When a mounted TeraBox account starts failing auth,** its card shows "Sign
   in again". That runs the same flow and ends in the existing
   `POST /v1/cloud/accounts/:name/reconnect`.

The cookie goes browser to sidecar to the admin's UI to local-storage, once,
over the gateway. That is the same path the pasted cookie takes today. No
service-to-service credentials are added.

Download Station is on by default. **When it is not installed,** the Paste form
shows "Install Download Station for one-click sign-in", and points to the
mobile path below.

**Mobile app alternative** (Android, Online Accounts > TeraBox > "Sign in"):
- A full-screen `webview_flutter` page opens on the TeraBox login.
- On each `onPageFinished`, a small `MethodChannel` (`nivaroos/cookies`) calls
  `android.webkit.CookieManager.getInstance().getCookie(url)` for the allowed
  domains. This native call also returns the HttpOnly `ndus`, which
  `webview_flutter`'s own cookie manager cannot read.
- Once `ndus` appears, the page shows the same confirmation, and the app posts
  to `/v1/cloud/accounts`.
- The app then clears the WebView's cookies for those domains
  (`CookieManager.removeAllCookies`), so the phone keeps no copy.

Pros: zero server RAM, and the phone's native keyboard and captcha UX. Con:
Google refuses OAuth inside WebViews (`disallowed_useragent`), so "Continue
with Google" must be hidden or explained there, and email/password or QR used
instead. The server browser has no such limit, which is why it is the primary
path.

**Why not a browser extension or bookmarklet to export cookies?** `ndus` is
HttpOnly, so a bookmarklet cannot read it, and an extension means an install
on every client. Both are more hassle than the owner's current copy-paste.
QR sign-in comes free with the real-browser flow, because TeraBox's login page
already shows the QR code.

## 10. Installer and migration

- **`install_download_station`** also runs **`install_ds_browser`**:
  1. If `google-chrome`, `google-chrome-stable`, `chromium` or
     `chromium-browser` is already present at a supported version (at least
     120), use it.
  2. Otherwise install `chromium` from the distro. Debian: `apt-get install -y
     chromium`. Ubuntu, where chromium is a snap: use Google's signed
     `google-chrome-stable` .deb repository. Fedora: `dnf install chromium`.
     Arch: `pacman -S chromium`.
  3. If none of those work, fetch **Chrome for Testing**
     (`chrome-linux64.zip`, pinned version and sha256) into
     `/opt/nivaroos/chromium/`.

  `chrome-headless-shell` is **not** used. It cannot load extensions and is
  easier for sites to fingerprint.
- It creates the system user `nivaroos-browser` (nologin, no home) and the
  directories `/var/lib/nivaroos/ds-browser/{profiles,staging}`. `staging` is
  group `nivaroos-browser` with mode 2770, so the root sidecar can move files
  out of it.
- It installs `nivaroos-ds-browser.socket` and `.service` (socket enabled, the
  service is never enabled on its own), plus the vendored uBOL zip in
  `/usr/share/nivaroos/ds-browser/ubol/`.
- It checks that user namespaces are allowed:
  `kernel.unprivileged_userns_clone`, or on Ubuntu 24.04 and later the AppArmor
  `apparmor_restrict_unprivileged_userns`. On Ubuntu, it ships an AppArmor
  profile for the Chromium binary that allows `userns`. It **never** falls back
  to `--no-sandbox`: if the sandbox cannot run, `/rb/status` reports
  `available:false, reason:"sandbox"` and the UI stays in Lite mode with an
  explanation.
- Low-RAM boxes (under 2 GB) skip the step and use Lite mode by default.
- A migration step for existing installs runs the same function on update, so
  current boxes gain the full browser when they update.
- `uninstall.sh` removes the unit, user, profiles and vendored files. It
  removes Chromium only if the installer added it (the manifest records this).
- The installer flag `--without-ds-browser` keeps Download Station in Lite
  mode only.

## 11. Work packages and estimates

| WP | Content | Est. LOC (code + tests) |
|---|---|---|
| WP1 spike (1 day) | pipe CDP client, launch as `nivaroos-browser` with the sandbox, Google search with the UA fix, uBOL load + YouTube check, screencast to canvas prototype | ~500, throwaway parts marked |
| WP2 host | `browser-host` subcommand: instance per user, CDP client, tabs/targets, screencast pump with acks, adaptive quality, idle reaper, crash restart, tabs.json restore | ~1 800 + 700 tests (fake CDP peer) |
| WP3 egress + blocking | egress proxy over the netguard dialer, typed hosts, CONNECT host rules, Fetch fallback, cosmetic inject, uBOL control, counters | ~700 + 400 |
| WP4 sidecar API | `/rb/ws` relay with auth and Origin checks, `/rb/status`, profile wipe, download hand-off from `downloadWillBegin`, staging moves, file chooser, cookie export with provider allow-lists | ~600 + 400 |
| WP5 UI | `DsBrowser.vue` rewrite: canvas viewport, input/IME/touch, tab strip, URL bar, context menu, dialogs, find, downloads toast, Lite-mode switch | ~1 600 + 500 vitest |
| WP6 installer | chromium install matrix, user, units, AppArmor, uBOL vendor, migration, uninstall | ~250 + installer tests |
| WP7 TeraBox (web) | sign-in window mode, cookie polling and check, Online Accounts dialog, reconnect | ~450 + 200 |
| WP8 TeraBox (mobile) | WebView login page, `MethodChannel` CookieManager (Kotlin ~40), confirmation, goldens | ~350 + tests |
| WP9 Lite-mode quick wins | gzip/br, stable per-user prefix, `allow-popups` as a DS tab, `inject.js` context menu with real URLs | ~300 + 150 |

Total is about 6 500 LOC of code plus about 3 000 LOC of tests. Order: WP1,
then WP2 to WP4 and WP6, then WP5, WP7, WP9 and WP8. TeraBox (WP7) needs WP2
to WP4 only, since it reuses the viewport component.

## 12. Acceptance checks

- Google search loads results, and Google account sign-in completes.
- YouTube plays without pre-roll ads while the ad blocker is on.
- Wikipedia opens in under 2 s on a warm browser over LAN, and the canvas
  shows sharp text at rest.
- `target=_blank`, "Open link in new tab" and `window.open` each create a DS
  tab. Copy page address and Copy link address give the real URLs.
- Right-clicking a download link and choosing "Download with Download Station"
  starts a multi-connection download with the site's cookies.
- A page cannot fetch `http://127.0.0.1:*`, `169.254.169.254` or an untyped
  LAN host (test fixtures prove this via the egress proxy).
- The browser works when NivaroOS is opened over HTTPS through the gateway.
- 10 min after the window closes, `nivaroos-ds-browser.service` is inactive and
  its RAM is back to 0. When the window reopens, the tabs are restored.
- TeraBox: signing in with the browser and pressing Connect gives a mounted
  account with no copy-paste. "Sign in again" fixes an expired account.

## 13. Built (2026-09-29)

What shipped, and where it differs from the plan above:

- **Host** (`services/download-sidecar/cdp.go`, `rb_host.go`, `rb_instance.go`,
  `rb_viewer.go`, `rb_egress.go`, `rb_signin.go`, `rb_proto.go`): as designed.
  Differences found while building:
  - Chromium writes an unpacked extension's indexed DNR rules into the
    extension folder, so the host copies the vendored uBO Lite into its own
    state dir (`/var/lib/nivaroos/ds-browser/ubol`) and loads it from there.
  - `navigator.webdriver` is true under the pipe unless
    `--disable-blink-features=AutomationControlled` is passed; it is.
  - A tab in the background of a headless window stops painting, so every
    tab gets its own window (`newWindow`), and a shown tab is brought to front.
  - A running screencast keeps its size/quality: changing either restarts it.
  - Headless Chromium does not always report title changes: titles are read
    from the page (load, DOMContentLoaded, and every 2 s for shown tabs).
  - uBO Lite's global switch and trusted sites are set through its own
    `setFilteringModeDetails` message from one of its extension pages
    (`none` for trusted sites / everything when off, `optimal` otherwise).
  - The Go engine gets the lists and My filters pushed from the sidecar
    (`PUT /adblock`, only when their hash changed) - the browser user can't
    read Download Station's state.
- **Sidecar** (`rb_sidecar.go`): `/rb/status`, `/rb/ws`, `/rb/typed`,
  `/rb/profile`, `/rb/cookies/export`, `/rb/upload/{id}`,
  `/rb/staged/{guid}/save`, `/rb/install`, and `rb_capture` on `POST
  /downloads` and `/probe`. Uploads come from the user's computer (a file
  input in the UI), not the NivaroOS file picker.
- **UI** (`ui/src/apps/download-station/rb/`, `DsBrowser.vue`): as §6, plus
  history suggestions in the address bar and a start page with recent
  sites. Lite mode (`DsLiteBrowser.vue`) got gzip for text responses only.
- **TeraBox (web)**: Online Accounts > TeraBox > "Sign in with the browser",
  and "Sign in again" on a connected account (`RbSigninWindow.vue`).
- **Installer**: `services/download-sidecar/build/scripts/install-ds-browser.sh`,
  run by `install_download_station` on every install/update
  (`--without-ds-browser` skips it) and by `nivaroos-ds-browser-install.service`
  (the "Install full browser" button). uBO Lite 2026.926.2202 and Chrome
  for Testing 154.0.8037.57 are pinned by sha256.

Measured on this box after deploy (loopback, warm browser, 1064x710,
cache on): example.com 0.29 s, en.wikipedia.org/wiki/Linux 0.8-2.5 s,
google.com 1.2 s (10 blocked), youtube.com 1.3-2.1 s, github.com 1.4-1.9 s,
bbc.com/news 1.3 s (17 blocked), cnn.com 1.7 s (25 blocked). Wikipedia's
first screen arrives as ~440 KB of pictures; scrolling runs at ~20 fps.
Cold start (service not running) to first picture: about 2 s. A right-click
menu appears in about 50 ms; a download link reaches Add Download in under
1 s with the tab's cookies.

Google search from this box's IP still answers with a reCAPTCHA page
("unusual traffic from your computer network") after a day of automated
tests - unlike the proxy's dead end, that page is a normal captcha the user
can solve in the real browser.

Not built yet:
- WP8, the Android WebView sign-in for TeraBox (the web flow covers phones
  through the web UI too).
- The rest of WP9 for Lite mode: a stable per-user proxy prefix (cache),
  `allow-popups` as DS tabs and a proxied right-click menu.
- Blocked counts from the egress proxy's own refusals (only Chromium's
  "blocked by client" failures are counted), and the per-tab audible icon.
