# Persistent terminal sessions: API contract

Status: implemented (server). Web and mobile clients build on this.
Code: `services/common/utils/termsession` (manager, ring buffer, HTTP/WS),
`services/core/route/v1/hostterm.go` (host shell),
`services/app-management/route/v1/container_terminal.go` (docker exec).

## What changes

A terminal no longer dies when its WebSocket closes. The shell (a pty on
the host, or a docker exec) keeps running on the server as a **session**.
Any number of clients can **attach** to it, detach, and come back later.
When a client attaches, the server first replays the session's recent
output (the **scrollback**, up to 2 MiB) and then streams live output.

There are two session families, one per service. They share the same JSON
shapes and WebSocket protocol:

| Family    | Base path                          | Service        |
|-----------|------------------------------------|----------------|
| host      | `/v1/sys/terminal-sessions`        | core           |
| container | `/v1/container/terminal-sessions`  | app-management |

A client that wants to show "all my terminals" fetches both lists and
merges them. Each entry has `kind` and an `attach_path`, so the client
never has to build URLs itself.

**Restarts:** sessions live in the service's memory. They do **not**
survive a restart of that service (core or app-management), a NivaroOS
update, or a reboot. After a restart the lists are empty. Clients should
say so plainly, e.g. "The server restarted, so this session ended."

## Auth

This uses the same auth as every other `/v1` route. Send the JWT as
`Authorization: <token>`, or, for WebSockets, which can't set headers
from a browser, as `?token=<token>`. The loopback "local automation" JWT
bypass never applies to these routes (the same rule as `/v1/sys/wsterm`).
WebSocket upgrades apply the same-origin check as the old endpoints:
non-browser clients (no `Origin`) are accepted, and browser pages must be
served from the same host (including `X-Forwarded-Host`, so the Cloudflare
tunnel works). All traffic goes through the gateway as before.

Sessions belong to the JWT user id. Another user's session answers 404,
never 403. Nothing about it is revealed.

## Response envelope

REST responses use the usual `{success, message, data}` envelope.
`success` mirrors the HTTP status: 200, 201, 400, 401, 404, 409, 429 or
500. `message` is human-readable. On an error, show `message` to the user.

## Session object

```json
{
  "id": "5f0c1e9a2b7d4c3e8a1f6b20",
  "title": "Terminal 1",
  "kind": "host",
  "user": "alice",
  "container": "jellyfin",
  "container_id": "3c9d...full id",
  "shell": "/bin/bash",
  "state": "running",
  "exit_code": null,
  "exit_reason": "",
  "exited_at": null,
  "created_at": "2026-09-29T10:00:00Z",
  "last_activity_at": "2026-09-29T10:05:12Z",
  "clients": 1,
  "cols": 120,
  "rows": 32,
  "cwd": "/home/alice/projects",
  "command": "vim",
  "scrollback_bytes": 48213,
  "legacy": false,
  "attach_path": "/v1/sys/terminal-sessions/5f0c1e9a2b7d4c3e8a1f6b20/attach"
}
```

- `id` is 24 hex characters and opaque.
- `kind` is `host` or `container`. `user` is only set for host sessions
  (the host account the shell runs as). `container` (the name, with no
  leading `/`) and `container_id` are only set for container sessions.
- `title` is 1–80 characters. It is generated when not given: host
  sessions get `Terminal N`, where N is the lowest unused number, and
  container sessions get `<container> (<shell>)`, e.g. `jellyfin (bash)`.
  Control characters are removed.
- `state` is `running` or `exited`. For an exited session, `exit_code` is
  an integer (-1 means unknown or killed by a signal), `exited_at` is set,
  and `exit_reason` is one of:
  - `exited`: the program ended by itself
  - `killed`: killed with DELETE
  - `timeout`: detached and idle for too long
  - `evicted`: an old-client session was ended to make room
- `last_activity_at` is the last input or output.
- `clients` is the number of attached viewers.
- `cols`/`rows` is the terminal's current size.
- `cwd` and `command` describe the terminal's **foreground** job at the
  moment of the request, e.g. `vim` while vim runs, otherwise the shell.
  They are best-effort and omitted when unknown. For container sessions,
  `cwd` is the path inside the container.
- `scrollback_bytes` is the number of bytes a reattach will replay.
- `legacy` is true for sessions opened through the old plain-connect
  endpoints (see below).

## Routes

Replace `{base}` with a base path from the table above.

### `GET {base}` lists your sessions

The newest session comes first. Exited sessions stay in the list for the
retention period (10 min), at most as many per user as the session limit
(and as many overall as the total limit); the oldest go first. An exited
session keeps the last 256 KiB of its output for replay.

Container family only: `?container=<name|id|id-prefix of 12 or more
characters>` filters the list to one container. The web container console
uses this.

```json
{"success":200,"message":"OK","data":{
  "sessions":[ Session, ... ],
  "running": 3,
  "limits":{"max_sessions":12,"scrollback_bytes":2097152,
            "detached_timeout_seconds":86400,"exited_retention_seconds":600}
}}
```

`detached_timeout_seconds` is -1 when there is no timeout. Only running
sessions count toward `max_sessions`. The limit applies to each family
separately.

### `POST {base}` creates a session

Body (JSON, all fields optional except `container` for the container family):

```json
{"title":"build box","cols":120,"rows":32,
 "container":"jellyfin","shell":"bash"}
```

- `cols`/`rows` is the initial size. The default is 120x32. Send the size
  of the view you are about to attach, so the first prompt lays out right.
- `shell` (container only) is one of `bash|zsh|fish|ash|dash|sh`, as
  listed by `GET /v1/container/:id/shells`. Leave it empty for the default.
- On success it returns **201** with a Session. Then attach to its
  `attach_path`. Output produced between create and attach is not lost,
  because it is in the scrollback.
- Errors:
  - 400: bad title, unknown or unavailable shell, missing `container`
  - 404: the container doesn't exist
  - 409: the container isn't running
  - 429: at the session limit. `message` says to close one first. Show
    the session list so the user can pick one to close.

### `GET {base}/:id` gets one session

Returns 200 with a Session, or 404.

### `PUT {base}/:id` renames a session

Body: `{"title":"new name"}`. The title is trimmed and control characters
are removed. It must be non-empty and no longer than 80 characters, or
the server returns 400. The response is 200 with the updated Session.
Attached viewers get a `session` control message. (This is PUT, not
PATCH, because the services' CORS policy allows only
GET/POST/PUT/DELETE.)

### `DELETE {base}/:id` kills a session

- If the session is running, the process is ended and the session is
  removed from the list right away. The host shell's whole process group
  gets SIGHUP, then SIGKILL after 2 s. A docker exec is closed and then
  signalled.
- Attached viewers receive `exit` with `reason:"killed"` and then a close
  frame.
- DELETE on an already-exited session just dismisses it from the list.
- Returns 200, or 404.

### `GET {base}/:id/attach` attaches over WebSocket

Query: `token=<jwt>`, plus optional `cols=N&rows=N` (the viewer's size).
With a size, this viewer becomes the active one, and the terminal is
resized to it if that differs. Without a size, the current size is kept.

The server checks the session before upgrading. An unknown session, or
another user's, gets a plain HTTP **404** instead of an upgrade. Check
the handshake status, and on 404 refresh the list.

## WebSocket protocol

This extends wsterm framing v2 (`services/common/utils/wsterm`). The only
change is that the attach route now also sends control messages from
server to client.

**Client → server:** unchanged from wsterm v2.
- BINARY frames carry raw input (keystrokes, paste). The server never
  parses them.
- A TEXT frame whose first byte is `0x00`, followed by JSON, is a control
  message: `{"type":"resize","cols":N,"rows":N}`. Other types, such as
  `{"type":"ping"}`, are accepted and ignored.
- For old UIs, a legacy TEXT frame that is exactly the resize object is
  still honoured as a resize. Any other TEXT frame is treated as input.

**Server → client** on the attach route:
- A BINARY frame is terminal output. Write it into the emulator as-is.
- A TEXT frame whose first byte is `0x00`, followed by JSON, is a control
  message. Clients must never print these. The types are:

| type      | payload                                  | when |
|-----------|------------------------------------------|------|
| `hello`   | `session` (Session), `replay_bytes` (int) | first frame after the upgrade |
| `live`    | none                                     | after the last replay frame. Everything after it is live |
| `session` | `session` (Session)                      | rename, or another viewer attached/detached (`clients` changed) |
| `exit`    | `code` (int), `reason` (string)          | the session ended. A CLOSE frame follows |

- A plain TEXT frame (not starting with `0x00`) is a human-readable
  status or error line, e.g. from a failed start on the legacy routes.
  Print it.
- The server closes with a CLOSE frame: 1000 with the exit reason after
  `exit`, 1000 when the server ends a viewer normally, **1013 "viewer too
  slow, reattach"** when the viewer couldn't keep up (more than 512
  queued frames), or 1011 on a server-side failure.
- The server sends a WebSocket **ping** every 25 s, and drops a viewer
  after 75 s of silence (no frames and no pongs). Browsers and `dart:io`
  answer pings automatically. The 25 s ping keeps the Cloudflare tunnel
  (100 s idle cut-off) alive.

### Attach sequence

```
client                      server
  |--- upgrade ------------->|  (404 if unknown / not yours)
  |<-- 0x00{"type":"hello","session":{...},"replay_bytes":N}
  |<-- BINARY ... (replay, <=32 KiB per frame, N bytes total)
  |<-- 0x00{"type":"live"}
  |<-> BINARY input/output, 0x00 resize ...   (live)
  |<-- 0x00{"type":"session",...}             (rename / viewers changed)
  |<-- 0x00{"type":"exit","code":0,"reason":"exited"}
  |<-- CLOSE 1000 "exited"
```

### Replay semantics

- The replay is the latest scrollback (up to `scrollback_bytes`, 2 MiB by
  default), oldest byte first. The server registers the viewer and takes
  the snapshot atomically, so no output is lost or duplicated between the
  replay and the live stream.
- Once the ring has wrapped, the replay starts at a line boundary (the
  first newline within the first 8 KiB) and never in the middle of a
  UTF-8 character.
- **Clients should reset the emulator before writing a replay**, e.g.
  xterm.js `term.reset()` when `hello` arrives, so that a reconnect
  doesn't print the history twice. Put the emulator in a "restoring"
  state until `live` arrives, and scroll to the bottom on `live`.
- If a full-screen program (vim, htop, less) is on the alternate screen,
  the server prefixes the replay with `ESC[?1049h`, so the view comes back
  on the alternate screen even when the original switch has scrolled out
  of the buffer. The server then makes the program repaint. When the size
  is unchanged, it resizes by one row and back, which sends SIGWINCH.
- The replay was produced at the session's size at that time. If your
  size differs, old lines may wrap differently. Live output is correct.
- For an **exited** session, attach sends `hello` (with `state:"exited"`),
  the final output, `live`, then `exit`, then closes. Use this to show
  "what happened".

### Resize semantics (several viewers)

- The pty/exec has a single size. It follows the **last active viewer**,
  meaning the one that most recently attached with a size, sent a resize,
  or typed.
- Each viewer should send a resize when its view changes size. It should
  also send one right after `live` when it isn't sure the attach query
  carried the right size.
- When the active viewer leaves, the size switches to the most recently
  active remaining viewer.

## Lifetime and limits

The defaults below can be changed with an optional `[terminal]` section in
`/etc/nivaroos/casaos.conf` (core) or `/etc/nivaroos/app-management.conf`
(app-management). The config is read at service start.

| Setting | Default | Config key |
|---------|---------|------------|
| Running sessions per user (per family) | 12 | `MaxSessionsPerUser` |
| Running sessions per service, all users | 48 | `MaxSessions` |
| Scrollback per session | 2 MiB | `ScrollbackKB` (16…65536) |
| Detached-idle timeout | 24 h | `DetachedTimeout` (Go duration ≥ 1m, `never`/`0` = never) |
| Detached-idle timeout, legacy sessions | 1 h (never above `DetachedTimeout`) | — |
| Exited session kept in the list | 10 min | `ExitedRetention` |

- **Detached-idle timeout:** a session is ended with `reason:"timeout"`
  only when it has had **no viewers** *and* no input or output for the
  whole timeout. A detached build that keeps printing stays alive. After
  that it is listed as exited for the retention period.
- **Memory:** at most `MaxSessions` × `ScrollbackKB` of scrollback, which
  is 96 MiB at the defaults. Each viewer also has a bounded queue (512
  frames). A viewer that falls behind is dropped with 1013, and can
  reattach to get the replay. A slow viewer never stalls the shell or the
  other viewers.

## Old endpoints (still work)

- `GET /v1/sys/wsterm?cols=&rows=&token=` (host)
- `GET /v1/container/:id/terminal?shell=&cols=&rows=&token=` (container)

These behave as before on the wire. They use wsterm v2 framing, send no
control TEXT frames, send plain TEXT error lines followed by CLOSE 1011
on failure, and send CLOSE 1000 when the shell exits. Internally, each
connect creates a new session with `legacy:true` and attaches to it. The
shell now **survives** the socket closing, so a user of an old app can
recover the shell from a new client's session list. Old clients can't
manage sessions, so they are never locked out. When the user is at the
limit, a legacy connect ends that user's oldest **detached legacy**
session (`reason:"evicted"`) to make room. Sessions created through the
new API are never evicted. Legacy sessions time out after 1 h detached.

## Client checklist

1. Opening a terminal: `POST` with the view's size, then attach to
   `attach_path` with `cols`/`rows`.
2. The socket dropped (network, app backgrounded, tab closed): reattach to
   the same `attach_path`. On `hello`, reset the emulator. When the
   handshake returns 404, the session is gone, so offer a new one.
3. Session switcher: list both families and merge them. Show `title`,
   `command`/`cwd`, `clients`, `last_activity_at`, and the exited state or
   reason. Offer rename (PUT) and close (DELETE, with a confirmation when
   `command` isn't the shell).
4. Handle 429 on create by showing the list so the user can close a
   session.
