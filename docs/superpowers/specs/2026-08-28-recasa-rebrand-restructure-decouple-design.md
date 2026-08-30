# Recasa Rebrand, Restructure & Decouple — Design Spec

## Context

The current project ("casaos-fork") is not actually one repository. It is
a thin meta-repo whose `.gitignore` explicitly excludes eight directories
(`CasaOS`, `CasaOS-UI`, `CasaOS-AppManagement`, `CasaOS-Gateway`,
`CasaOS-LocalStorage`, `CasaOS-UserService`, `CasaOS-MessageBus`,
`CasaOS-CLI`), each of which is its own independent git checkout, forked
from `IceWhaleTech/<name>` to `F-e-n-y-x/<name>`, **and each still carries
an `upstream` remote pointing at the real IceWhaleTech repo**. Plus two
small standalone Go services under `extras/` (`casaos-vm-sidecar`,
`casaos-gpu-sidecar`) that were written from scratch for this fork.

This structure is the root cause of the problem described in the request:
the project has no identity of its own — it's eight forks of someone
else's product glued together, still one `git pull upstream` away from
being overwritten, still branded, packaged, and path-named as CasaOS
throughout. This spec covers turning that into one real project: **Recasa**.

Two concrete live-coupling risks to upstream were found during
investigation (beyond the obvious naming):

1. `CasaOS/service/system.go`'s `UpdateSystemVersion()` runs
   `curl -fsSL https://get.casaos.io/update?t=<manufacturer> | bash` (or a
   configurable `config.ServerInfo.UpdateUrl`) — a self-update path that,
   if ever triggered, downloads and executes a remote script that would
   overwrite this fork with vanilla upstream CasaOS.
2. `CasaOS-UI/package.json` depends on `@icewhale/casaos-openapi` and
   `@icewhale/casaos-appmanagement-openapi` pinned to the npm dist-tag
   `"latest"` — any publish by IceWhale silently changes what this project
   builds against next `pnpm install`.
3. (Lower severity, kept) `config.ServerInfo.ServerApi` defaults to
   `https://api.casaos.io/casaos-api` and is called for a version check
   (`GET /v1/sys/version`) and a `/token` endpoint. Not something that can
   overwrite the project, but it is an unnecessary live call to IceWhale's
   infrastructure and should be disabled.

App Store catalog URLs (`https://casaos.app/store/main.zip`, the
bigbeartechworld community catalog) are **not** treated as a coupling risk
to remove — fetching a remote app catalog is the actual intended function
of the App Store feature, for both upstream CasaOS and Recasa alike. They
are in scope only for branding-string cleanup, not removal.

## Goals

- One real git repository, `recasa`, containing everything: no nested
  repos, no `upstream` remotes anywhere, single fresh commit (per your
  choice — no CasaOS-fork history carried over).
- Every product-identity surface (Go module paths, binary names, systemd
  unit names, config/data directory paths, UI branding strings, npm
  package name) says Recasa, not CasaOS.
- No runtime dependency — network call, npm resolution, or otherwise —
  that lets IceWhale-controlled infrastructure change or break this
  project.
- Windowed-UI consistency audit folded in here since it's small and
  touches the same UI files being rebranded.

## Non-goals (separate specs, later)

- The single-command installer script and the `recasa` CLI with
  install-time optional-feature selection (VM Manager on/off) — **Sub-project B**.
- Actually cutting this live host over to the new services/binaries/data
  paths — **Sub-project C**, done only after A and B are built and verified
  elsewhere.
- This spec does not touch this host's running `casaos-*.service` units,
  `/etc/casaos`, or `/var/lib/casaos` at all. It produces a new codebase;
  nothing on this host's live services changes as a result of Sub-project A.

## Naming scheme

| Concept | Old | New |
|---|---|---|
| Product name | CasaOS | Recasa |
| GitHub repo | (8 separate forks + meta repo) | `F-e-n-y-x/recasa` (single repo) |
| Go module path root | `github.com/IceWhaleTech/CasaOS*` | `github.com/F-e-n-y-x/recasa/services/<name>` |
| Core binary | `casaos` | `recasa` |
| Service binaries | `casaos-gateway`, `casaos-user-service`, `casaos-app-management`, `casaos-local-storage`, `casaos-message-bus`, `casaos-vm-sidecar`, `casaos-gpu-sidecar` | `recasa-gateway`, `recasa-user`, `recasa-app-management`, `recasa-local-storage`, `recasa-message-bus`, `recasa-vm-sidecar`, `recasa-gpu-sidecar` |
| systemd units | `casaos*.service` | `recasa*.service` (same suffix mapping as binaries above) |
| Config dir | `/etc/casaos` | `/etc/recasa` |
| Data dir | `/var/lib/casaos` | `/var/lib/recasa` |
| Log dir | `/var/log/casaos` | `/var/log/recasa` |
| Shell helpers | `/usr/share/casaos/shell` | `/usr/share/recasa/shell` |
| npm package | `casaos-main` | `recasa-ui` |
| npm client packages | `@icewhale/casaos-openapi`, `@icewhale/casaos-appmanagement-openapi` (registry, `latest`) | vendored local packages, no registry dependency (see Decoupling) |
| UI display strings | "CasaOS" in titles/login/about | "Recasa" |

Go modules stay one-per-service (not merged into a single module) tied
together with a root `go.work` for local development — this avoids
touching internal import paths within each service, only the module
declaration line and any self-referential import paths.

## Repo layout

```
recasa/
  ui/                        (was CasaOS-UI)
  cli/                       (was CasaOS-CLI — admin CLI, base for Sub-project B's `recasa` command)
  services/
    core/                    (was CasaOS)
    app-management/          (was CasaOS-AppManagement)
    gateway/                 (was CasaOS-Gateway)
    user/                    (was CasaOS-UserService)
    local-storage/           (was CasaOS-LocalStorage)
    message-bus/             (was CasaOS-MessageBus)
    vm-sidecar/              (was extras/casaos-vm-sidecar)
    gpu-sidecar/             (was extras/casaos-gpu-sidecar)
  docs/                      (carried over as-is — historical spec/plan record, not rebranded retroactively)
  go.work
  BACKLOG.md
```

`backups/` and `build/` (currently gitignored, ~500MB of local snapshots
and build output) are not carried into the new repo at all — they're
local artifacts, not project source.

## Decoupling remediation

1. **Self-update-and-execute-remote-script**: `UpdateSystemVersion()` in
   `services/core` — remove the hardcoded `get.casaos.io` fallback entirely
   (no silent default to an IceWhale-controlled script). If a
   configurable `UpdateUrl` mechanism is kept at all, it must default to
   empty/disabled, never to an IceWhale URL.
2. **npm client packages**: vendor frozen local copies of both packages
   into `ui/vendor/api-clients/` as local workspace packages referenced
   via pnpm's `link:`/`workspace:` protocol. `pnpm install` never resolves
   anything from the `@icewhale` npm scope again.
3. **`ServerApi` version-check/token calls**: default
   `config.ServerInfo.ServerApi` to empty/disabled rather than
   `api.casaos.io`; guard the version-check and `/token` call sites to
   no-op when unset.
4. **Nested repos**: delete all 8 nested `.git` directories (which removes
   the `upstream` remotes with them) as part of flattening into
   `services/`/`ui/`/`cli/`.
5. **License/comment headers** (`@Website: https://www.casaos.io`, etc.):
   cosmetic, updated opportunistically while touching a file for renaming,
   not a dedicated pass.

## Windowed UI audit (folded into this spec)

`CasaOS-UI/src/views/Home.vue` already has a window-registry pattern
(`WindowManager`/`DesktopWindow`) and Files/Settings/VM Manager already use
it. Task: enumerate every entry Home.vue's Dock/app registry and every
`.vue` file under `components/desktop/` and confirm each app opens via
that registry rather than a bare route push; fix any stragglers found.
Pre-desktop views (`Login.vue`, `Welcome.vue`, `VmConsoleStandalone.vue`)
are intentionally not windowed (they exist outside the desktop shell) —
confirm that's still correct, not an oversight.

## Testing / verification approach

- Each service still builds standalone (`go build ./...` per module) and
  its existing test suite still passes after the module path / import
  rename.
- `ui/` still builds (`pnpm install && pnpm run build`) with vendored API
  clients, no network calls to the `@icewhale` scope during install
  (verify via `pnpm install --offline` after vendoring, or by checking the
  resolved lockfile has no `@icewhale/*` registry entries left).
- `grep -ri` sweep for `casaos`/`icewhale` (case-insensitive, excluding
  `docs/` historical specs and `BACKLOG.md`) returns zero hits in
  `services/`, `ui/src/`, `cli/` when done, other than intentional
  third-party mentions (e.g. app-store catalog URLs, which are explicitly
  out of scope for removal).
- No headless browser testing (standing instruction from earlier in this
  project) — UI verification is code review plus you doing a manual pass,
  or targeted API/curl checks I run myself.
- None of this touches the live host's running `casaos-*` services or
  `/var/lib/casaos` data — verification is entirely within the new repo's
  own build/test tooling.

## Out of scope reminders

- Installer script, `recasa` CLI feature-flag subcommands, install-time
  VM Manager opt-in/out — Sub-project B.
- Live host cutover (stopping old services, starting new ones, migrating
  `/var/lib/casaos` data to `/var/lib/recasa`) — Sub-project C, executed
  only after B is ready, with a parallel-install-then-switch approach and
  an explicit rollback plan (old units left installed-but-disabled for a
  rollback window).
