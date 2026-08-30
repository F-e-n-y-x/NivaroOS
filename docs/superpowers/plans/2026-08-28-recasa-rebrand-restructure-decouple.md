# Recasa Rebrand, Restructure & Decouple Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the current eight-separate-forks-glued-by-a-meta-repo "casaos-fork" project into one real, self-contained repository called Recasa — renamed, restructured, and with every live dependency on IceWhale-controlled infrastructure removed — without touching this host's currently running CasaOS services.

**Architecture:** A brand-new local repo at `/root/recasa` is built up service-by-service by copying each of the 8 existing forked repos (plus the two `extras/` sidecars and the UI) into a flat `services/`/`ui/`/`cli/` layout, renaming every identity surface (Go module path, binary name, systemd unit name, config/data paths, UI branding strings) and cutting three concrete upstream ties along the way. History is not carried over — the final task squashes everything into one fresh commit before pushing to a new GitHub repo.

**Tech Stack:** Go 1.21+ (multi-module workspace via `go.work`), Vue 2 / vue-cli / pnpm (UI), goreleaser (build configs, updated not re-verified), git, gh CLI.

**Spec:** `docs/superpowers/specs/2026-08-28-recasa-rebrand-restructure-decouple-design.md`

## Global Constraints

- Product name is **Recasa** everywhere a user or developer sees it; "CasaOS" must not appear in any binary name, systemd unit, config/data path, Go module path, npm package name, or user-visible UI string produced by this plan (exceptions: `docs/` historical specs, `BACKLOG.md`, and third-party App Store catalog URLs — see spec).
- Go module path root: `github.com/F-e-n-y-x/NivaroOS/services/<name>` (one module per service, tied together with a root `go.work` — never merge into a single module).
- Binary/systemd-unit naming: `recasa` (core), `recasa-gateway`, `recasa-user`, `recasa-app-management`, `recasa-local-storage`, `recasa-message-bus`, `recasa-vm-sidecar`, `recasa-gpu-sidecar`.
- Config dir `/etc/recasa`, data dir `/var/lib/recasa`, log dir `/var/log/recasa`, shell helpers `/usr/share/recasa/shell` (all replacing their `casaos`-named equivalents in code defaults — this plan does not create these directories on disk or touch the live host).
- Three upstream ties must be fully removed, not just documented: (1) the `get.casaos.io` self-update `curl | bash` path in `services/core`, (2) the `@icewhale/casaos-openapi` / `@icewhale/casaos-appmanagement-openapi` npm registry dependencies, (3) every one of the 8 services' `github.com/IceWhaleTech/CasaOS-Common` public-registry dependency, replaced by a locally forked `services/common` module.
- No headless browser testing (standing project instruction) — UI verification is `pnpm run build` succeeding plus code review; nothing in this plan drives a browser.
- This plan makes zero changes to the live host's running `casaos-*.service` units or `/etc/casaos` / `/var/lib/casaos` — everything happens inside the new `/root/recasa` tree.
- Frequent small commits inside `/root/recasa` during the build (normal git hygiene) — the single-fresh-commit requirement is satisfied only by the final task, which squashes all of them before the first (and only) push to the new public repo.

---

## Task 1: Scaffold the new repo

**Files:**
- Create: `/root/recasa/.gitignore`
- Create: `/root/recasa/go.work`
- Create: `/root/recasa/README.md`
- Create: `/root/recasa/docs/` (copied from `/root/casaos-fork/docs/`)
- Create: `/root/recasa/BACKLOG.md` (copied from `/root/casaos-fork/BACKLOG.md`)

**Interfaces:**
- Produces: `/root/recasa` as a git repo with an initial commit; every later task works inside this tree and amends `go.work`.

- [ ] **Step 1: Create the directory and copy over non-code project history**

```bash
mkdir -p /root/recasa
cd /root/recasa
git init -q
cp -a /root/casaos-fork/docs .
cp -a /root/casaos-fork/BACKLOG.md .
```

- [ ] **Step 2: Write `.gitignore`**

```bash
cat > /root/recasa/.gitignore <<'EOF'
/build/
/backups/
node_modules/
*.log
EOF
```

- [ ] **Step 3: Write a minimal `go.work` (modules are added by later tasks)**

```bash
cat > /root/recasa/go.work <<'EOF'
go 1.21
EOF
```

- [ ] **Step 4: Write a one-paragraph `README.md`**

```bash
cat > /root/recasa/README.md <<'EOF'
# Recasa

A self-hosted home server platform: file management, an app store, and
optional add-ons like a VM Manager, all served from one web UI.

This is a standalone project — not a CasaOS fork with live ties back to
upstream. See `docs/superpowers/specs/2026-08-28-recasa-rebrand-restructure-decouple-design.md`
for how this repo is organized and what was deliberately decoupled.
EOF
```

- [ ] **Step 5: Verify and commit**

Run: `cd /root/recasa && git status --porcelain | head -20 && ls`
Expected: shows `.gitignore`, `go.work`, `README.md`, `BACKLOG.md`, `docs/` as untracked/new.

```bash
cd /root/recasa
git add -A
git commit -q -m "Scaffold recasa repo root"
git log --oneline
```

---

## Task 2: Fork CasaOS-Common into `services/common`

**Files:**
- Create: `/root/recasa/services/common/` (forked from the Go module cache copy of `github.com/IceWhaleTech/CasaOS-Common@v0.4.11-alpha4` — the newest of the 7 versions the 8 services currently pin)
- Modify: `/root/recasa/go.work`

**Interfaces:**
- Produces: Go module `github.com/F-e-n-y-x/NivaroOS/services/common`, importable by every later service task via `go.work` (no `replace` directives needed — `go.work`'s `use` entry resolves it locally as long as each consumer's `go.mod` requires the exact module path `github.com/F-e-n-y-x/NivaroOS/services/common`).

- [ ] **Step 1: Copy the cached source out (module cache is read-only)**

```bash
mkdir -p /root/recasa/services/common
cp -a "/root/go/pkg/mod/github.com/!ice!whale!tech/!casa!o!s-!common@v0.4.11-alpha4/." /root/recasa/services/common/
chmod -R u+w /root/recasa/services/common
rm -f /root/recasa/services/common/go.sum
```

- [ ] **Step 2: Rename the module and fix its own internal self-imports**

```bash
cd /root/recasa/services/common
sed -i 's#^module github.com/IceWhaleTech/CasaOS-Common#module github.com/F-e-n-y-x/NivaroOS/services/common#' go.mod
grep -rl 'github.com/IceWhaleTech/CasaOS-Common' --include=*.go . | xargs -r sed -i 's#github.com/IceWhaleTech/CasaOS-Common#github.com/F-e-n-y-x/NivaroOS/services/common#g'
```

- [ ] **Step 3: Add it to the workspace and verify it builds standalone**

```bash
cd /root/recasa
sed -i '/^go 1.21/a\\nuse ./services/common' go.work
cd services/common
go mod tidy
go build ./...
```

Expected: `go build ./...` exits 0, no errors.

- [ ] **Step 4: Verify no remaining IceWhaleTech self-reference**

Run: `grep -ri "icewhaletech" /root/recasa/services/common/*.go /root/recasa/services/common/**/*.go 2>/dev/null | grep -v go.sum`
Expected: no output (module path fully renamed; any remaining hits would be leftover self-imports to fix).

- [ ] **Step 5: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Fork CasaOS-Common into services/common, rename module path"
```

---

## Task 3: Migrate `services/core` (was CasaOS)

**Files:**
- Create: `/root/recasa/services/core/` (copied from `/root/casaos-fork/CasaOS/`, `.git` removed)
- Modify: `/root/recasa/go.work`

**Interfaces:**
- Consumes: `github.com/F-e-n-y-x/NivaroOS/services/common` from Task 2.
- Produces: Go module `github.com/F-e-n-y-x/NivaroOS/services/core`, binary name `recasa`.

- [ ] **Step 1: Copy the tree, dropping the nested repo**

```bash
cp -a /root/casaos-fork/CasaOS /root/recasa/services/core
rm -rf /root/recasa/services/core/.git
```

- [ ] **Step 2: Rename the module path and repoint the CasaOS-Common dependency**

```bash
cd /root/recasa/services/core
sed -i 's#^module github.com/IceWhaleTech/CasaOS$#module github.com/F-e-n-y-x/NivaroOS/services/core#' go.mod
grep -rl 'github.com/IceWhaleTech/CasaOS-Common' --include=*.go . | xargs -r sed -i 's#github.com/IceWhaleTech/CasaOS-Common#github.com/F-e-n-y-x/NivaroOS/services/common#g'
go mod edit -droprequire=github.com/IceWhaleTech/CasaOS-Common
go mod edit -require=github.com/F-e-n-y-x/NivaroOS/services/common@v0.0.0-00010101000000-000000000000
```

- [ ] **Step 3: Rename config/data path defaults**

```bash
cd /root/recasa/services/core
grep -rl '/etc/casaos\|/var/lib/casaos\|/var/log/casaos\|/usr/share/casaos' --include=*.go --include=*.sample --include=*.conf . | xargs -r sed -i \
  -e 's#/etc/casaos#/etc/recasa#g' \
  -e 's#/var/lib/casaos#/var/lib/recasa#g' \
  -e 's#/var/log/casaos#/var/log/recasa#g' \
  -e 's#/usr/share/casaos#/usr/share/recasa#g'
```

- [ ] **Step 4: Remove the self-update-and-execute-remote-script default, and disable the ServerApi/Handshake defaults**

```bash
cd /root/recasa/services/core
sed -i 's#go command\.OnlyExec("curl -fsSL https://get\.casaos\.io/update?t=" + osRelease\["MANUFACTURER"\] + " | bash")#logger.Info("no default update URL configured; skipping self-update")#' service/system.go
sed -i 's#ServerApi = https://api\.casaos\.io/casaos-api#ServerApi =#' conf/casaos.conf.sample
sed -i 's#Handshake = socket\.casaos\.io#Handshake =#' conf/casaos.conf.sample
```

Run: `grep -n "get.casaos.io\|api.casaos.io\|socket.casaos.io" /root/recasa/services/core/service/system.go /root/recasa/services/core/conf/casaos.conf.sample`
Expected: no output.

- [ ] **Step 5: Repoint the `go:generate` directives at local sibling files instead of raw.githubusercontent.com**

```bash
cd /root/recasa/services/core
grep -rn "go:generate" main.go
```

Edit `main.go`'s `go:generate` line that fetches the message-bus OpenAPI spec from
`https://raw.githubusercontent.com/IceWhaleTech/CasaOS-MessageBus/main/api/message_bus/openapi.yaml`
to instead read `../message-bus/api/message_bus/openapi.yaml` (the sibling module added in Task 8):

```bash
sed -i 's#https://raw.githubusercontent.com/IceWhaleTech/CasaOS-MessageBus/main/api/message_bus/openapi.yaml#../message-bus/api/message_bus/openapi.yaml#' main.go
```

- [ ] **Step 6: Rename the binary in the goreleaser config and add to the workspace**

```bash
cd /root/recasa/services/core
sed -i 's#build/sysroot/usr/bin/casaos-migration-tool#build/sysroot/usr/bin/recasa-migration-tool#g; s#build/sysroot/usr/bin/casaos#build/sysroot/usr/bin/recasa#g' .goreleaser.yaml
cd /root/recasa
sed -i '/use .\/services\/common/a use ./services/core' go.work
```

- [ ] **Step 7: Build and test**

```bash
cd /root/recasa/services/core
go mod tidy
go build ./...
go test ./...
```

Expected: build succeeds; existing test suite passes (same pass/fail status as it had in `/root/casaos-fork/CasaOS` before this migration — this task renames identity, it does not change behavior).

- [ ] **Step 8: Verify no leftover CasaOS branding in code (excluding go.sum and vendored license text)**

Run: `grep -rli "icewhaletech\|casaos" /root/recasa/services/core --include=*.go | grep -v _test.go`
Expected: only comment-header hits like `@Website: https://www.casaos.io` remain (acceptable per spec — cosmetic, not functional) — no module paths, import paths, or config defaults.

- [ ] **Step 9: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Migrate services/core (was CasaOS): rename module, paths, remove self-update"
```

---

## Task 4: Migrate `services/app-management` (was CasaOS-AppManagement)

**Files:**
- Create: `/root/recasa/services/app-management/` (copied from `/root/casaos-fork/CasaOS-AppManagement/`)
- Modify: `/root/recasa/go.work`

**Interfaces:**
- Consumes: `github.com/F-e-n-y-x/NivaroOS/services/common` from Task 2.
- Produces: Go module `github.com/F-e-n-y-x/NivaroOS/services/app-management`, binaries `recasa-app-management`, `recasa-app-management-migration-tool`, `recasa-app-management-validator`.

- [ ] **Step 1: Copy the tree, dropping the nested repo**

```bash
cp -a /root/casaos-fork/CasaOS-AppManagement /root/recasa/services/app-management
rm -rf /root/recasa/services/app-management/.git
```

- [ ] **Step 2: Rename the module path and repoint the CasaOS-Common dependency**

```bash
cd /root/recasa/services/app-management
sed -i 's#^module github.com/IceWhaleTech/CasaOS-AppManagement$#module github.com/F-e-n-y-x/NivaroOS/services/app-management#' go.mod
grep -rl 'github.com/IceWhaleTech/CasaOS-Common' --include=*.go . | xargs -r sed -i 's#github.com/IceWhaleTech/CasaOS-Common#github.com/F-e-n-y-x/NivaroOS/services/common#g'
grep -rl 'github.com/IceWhaleTech/CasaOS-AppManagement' --include=*.go . | xargs -r sed -i 's#github.com/IceWhaleTech/CasaOS-AppManagement#github.com/F-e-n-y-x/NivaroOS/services/app-management#g'
go mod edit -droprequire=github.com/IceWhaleTech/CasaOS-Common
go mod edit -require=github.com/F-e-n-y-x/NivaroOS/services/common@v0.0.0-00010101000000-000000000000
```

- [ ] **Step 3: Rename config/data path defaults (do not touch the App Store catalog URLs — those stay, per spec)**

```bash
cd /root/recasa/services/app-management
grep -rl '/etc/casaos\|/var/lib/casaos\|/var/log/casaos\|/var/run/casaos' --include=*.go --include=*.sample --include=*.conf . | xargs -r sed -i \
  -e 's#/etc/casaos#/etc/recasa#g' \
  -e 's#/var/lib/casaos#/var/lib/recasa#g' \
  -e 's#/var/log/casaos#/var/log/recasa#g' \
  -e 's#/var/run/casaos#/var/run/recasa#g'
grep -n "appstore = " conf/app-management.conf.sample
```

Expected for the last command: the two `appstore = https://casaos.app/...` and `appstore = https://github.com/bigbeartechworld/...` lines are unchanged.

- [ ] **Step 4: Repoint `go:generate` directives at sibling modules**

```bash
cd /root/recasa/services/app-management
grep -rn "go:generate" main.go
sed -i 's#https://raw.githubusercontent.com/IceWhaleTech/CasaOS-MessageBus/main/api/message_bus/openapi.yaml#../message-bus/api/message_bus/openapi.yaml#' main.go
```

- [ ] **Step 5: Rename binaries in goreleaser config and add to workspace**

```bash
cd /root/recasa/services/app-management
sed -i \
  -e 's#build/sysroot/usr/bin/casaos-app-management-migration-tool#build/sysroot/usr/bin/recasa-app-management-migration-tool#g' \
  -e 's#build/sysroot/usr/bin/casaos-app-management-validator#build/sysroot/usr/bin/recasa-app-management-validator#g' \
  -e 's#build/sysroot/usr/bin/casaos-app-management#build/sysroot/usr/bin/recasa-app-management#g' \
  .goreleaser.yaml
cd /root/recasa
sed -i '/use .\/services\/core/a use ./services/app-management' go.work
```

- [ ] **Step 6: Build and test**

```bash
cd /root/recasa/services/app-management
go mod tidy
go build ./...
go test ./...
```

Expected: build succeeds; test suite passes at parity with the pre-migration copy.

- [ ] **Step 7: Verify no leftover functional branding**

Run: `grep -rli "icewhaletech" /root/recasa/services/app-management --include=*.go`
Expected: no output.

- [ ] **Step 8: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Migrate services/app-management (was CasaOS-AppManagement)"
```

---

## Task 5: Migrate `services/gateway` (was CasaOS-Gateway)

**Files:**
- Create: `/root/recasa/services/gateway/` (copied from `/root/casaos-fork/CasaOS-Gateway/`)
- Modify: `/root/recasa/go.work`

**Interfaces:**
- Consumes: `github.com/F-e-n-y-x/NivaroOS/services/common` from Task 2.
- Produces: Go module `github.com/F-e-n-y-x/NivaroOS/services/gateway`, binaries `recasa-gateway`, `recasa-gateway-migration-tool`.

- [ ] **Step 1: Copy the tree, dropping the nested repo**

```bash
cp -a /root/casaos-fork/CasaOS-Gateway /root/recasa/services/gateway
rm -rf /root/recasa/services/gateway/.git
```

- [ ] **Step 2: Rename the module path and repoint the CasaOS-Common dependency**

```bash
cd /root/recasa/services/gateway
sed -i 's#^module github.com/IceWhaleTech/CasaOS-Gateway$#module github.com/F-e-n-y-x/NivaroOS/services/gateway#' go.mod
grep -rl 'github.com/IceWhaleTech/CasaOS-Common' --include=*.go . | xargs -r sed -i 's#github.com/IceWhaleTech/CasaOS-Common#github.com/F-e-n-y-x/NivaroOS/services/common#g'
grep -rl 'github.com/IceWhaleTech/CasaOS-Gateway' --include=*.go . | xargs -r sed -i 's#github.com/IceWhaleTech/CasaOS-Gateway#github.com/F-e-n-y-x/NivaroOS/services/gateway#g'
go mod edit -droprequire=github.com/IceWhaleTech/CasaOS-Common
go mod edit -require=github.com/F-e-n-y-x/NivaroOS/services/common@v0.0.0-00010101000000-000000000000
```

- [ ] **Step 3: Rename config path defaults**

```bash
cd /root/recasa/services/gateway
grep -rl '/etc/casaos\|/var/lib/casaos\|/var/log/casaos\|/var/run/casaos' --include=*.go --include=*.sample --include=*.ini . | xargs -r sed -i \
  -e 's#/etc/casaos#/etc/recasa#g' \
  -e 's#/var/lib/casaos#/var/lib/recasa#g' \
  -e 's#/var/log/casaos#/var/log/recasa#g' \
  -e 's#/var/run/casaos#/var/run/recasa#g'
```

- [ ] **Step 4: Rename binaries in goreleaser config and add to workspace**

```bash
cd /root/recasa/services/gateway
sed -i \
  -e 's#build/sysroot/usr/bin/casaos-gateway-migration-tool#build/sysroot/usr/bin/recasa-gateway-migration-tool#g' \
  -e 's#build/sysroot/usr/bin/casaos-gateway#build/sysroot/usr/bin/recasa-gateway#g' \
  .goreleaser.yaml
cd /root/recasa
sed -i '/use .\/services\/app-management/a use ./services/gateway' go.work
```

- [ ] **Step 5: Build and test**

```bash
cd /root/recasa/services/gateway
go mod tidy
go build ./...
go test ./...
```

Expected: build succeeds; test suite passes at parity with the pre-migration copy.

- [ ] **Step 6: Verify no leftover functional branding**

Run: `grep -rli "icewhaletech" /root/recasa/services/gateway --include=*.go`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Migrate services/gateway (was CasaOS-Gateway)"
```

---

## Task 6: Migrate `services/user` (was CasaOS-UserService)

**Files:**
- Create: `/root/recasa/services/user/` (copied from `/root/casaos-fork/CasaOS-UserService/`)
- Modify: `/root/recasa/go.work`

**Interfaces:**
- Consumes: `github.com/F-e-n-y-x/NivaroOS/services/common` from Task 2.
- Produces: Go module `github.com/F-e-n-y-x/NivaroOS/services/user`, binaries `recasa-user`, `recasa-user-migration-tool`.

- [ ] **Step 1: Copy the tree, dropping the nested repo**

```bash
cp -a /root/casaos-fork/CasaOS-UserService /root/recasa/services/user
rm -rf /root/recasa/services/user/.git
```

- [ ] **Step 2: Rename the module path and repoint the CasaOS-Common dependency**

```bash
cd /root/recasa/services/user
sed -i 's#^module github.com/IceWhaleTech/CasaOS-UserService$#module github.com/F-e-n-y-x/NivaroOS/services/user#' go.mod
grep -rl 'github.com/IceWhaleTech/CasaOS-Common' --include=*.go . | xargs -r sed -i 's#github.com/IceWhaleTech/CasaOS-Common#github.com/F-e-n-y-x/NivaroOS/services/common#g'
grep -rl 'github.com/IceWhaleTech/CasaOS-UserService' --include=*.go . | xargs -r sed -i 's#github.com/IceWhaleTech/CasaOS-UserService#github.com/F-e-n-y-x/NivaroOS/services/user#g'
go mod edit -droprequire=github.com/IceWhaleTech/CasaOS-Common
go mod edit -require=github.com/F-e-n-y-x/NivaroOS/services/common@v0.0.0-00010101000000-000000000000
```

- [ ] **Step 3: Rename config path defaults**

```bash
cd /root/recasa/services/user
grep -rl '/etc/casaos\|/var/lib/casaos\|/var/log/casaos\|/var/run/casaos' --include=*.go --include=*.sample --include=*.conf . | xargs -r sed -i \
  -e 's#/etc/casaos#/etc/recasa#g' \
  -e 's#/var/lib/casaos#/var/lib/recasa#g' \
  -e 's#/var/log/casaos#/var/log/recasa#g' \
  -e 's#/var/run/casaos#/var/run/recasa#g'
```

- [ ] **Step 4: Repoint `go:generate` directives at sibling modules**

```bash
cd /root/recasa/services/user
grep -rn "go:generate" main.go
sed -i 's#https://raw.githubusercontent.com/IceWhaleTech/CasaOS-MessageBus/main/api/message_bus/openapi.yaml#../message-bus/api/message_bus/openapi.yaml#' main.go
```

- [ ] **Step 5: Rename binaries in goreleaser config and add to workspace**

```bash
cd /root/recasa/services/user
sed -i \
  -e 's#build/sysroot/usr/bin/casaos-user-service-migration-tool#build/sysroot/usr/bin/recasa-user-migration-tool#g' \
  -e 's#build/sysroot/usr/bin/casaos-user-service#build/sysroot/usr/bin/recasa-user#g' \
  .goreleaser.yaml
cd /root/recasa
sed -i '/use .\/services\/gateway/a use ./services/user' go.work
```

- [ ] **Step 6: Build and test**

```bash
cd /root/recasa/services/user
go mod tidy
go build ./...
go test ./...
```

Expected: build succeeds; test suite passes at parity with the pre-migration copy.

- [ ] **Step 7: Verify no leftover functional branding**

Run: `grep -rli "icewhaletech" /root/recasa/services/user --include=*.go`
Expected: no output.

- [ ] **Step 8: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Migrate services/user (was CasaOS-UserService)"
```

---

## Task 7: Migrate `services/local-storage` (was CasaOS-LocalStorage)

**Files:**
- Create: `/root/recasa/services/local-storage/` (copied from `/root/casaos-fork/CasaOS-LocalStorage/`)
- Modify: `/root/recasa/go.work`

**Interfaces:**
- Consumes: `github.com/F-e-n-y-x/NivaroOS/services/common` from Task 2.
- Produces: Go module `github.com/F-e-n-y-x/NivaroOS/services/local-storage`, binaries `recasa-local-storage`, `recasa-local-storage-migration-tool`.

- [ ] **Step 1: Copy the tree, dropping the nested repo**

```bash
cp -a /root/casaos-fork/CasaOS-LocalStorage /root/recasa/services/local-storage
rm -rf /root/recasa/services/local-storage/.git
```

- [ ] **Step 2: Rename the module path and repoint the CasaOS-Common dependency**

```bash
cd /root/recasa/services/local-storage
sed -i 's#^module github.com/IceWhaleTech/CasaOS-LocalStorage$#module github.com/F-e-n-y-x/NivaroOS/services/local-storage#' go.mod
grep -rl 'github.com/IceWhaleTech/CasaOS-Common' --include=*.go . | xargs -r sed -i 's#github.com/IceWhaleTech/CasaOS-Common#github.com/F-e-n-y-x/NivaroOS/services/common#g'
grep -rl 'github.com/IceWhaleTech/CasaOS-LocalStorage' --include=*.go . | xargs -r sed -i 's#github.com/IceWhaleTech/CasaOS-LocalStorage#github.com/F-e-n-y-x/NivaroOS/services/local-storage#g'
go mod edit -droprequire=github.com/IceWhaleTech/CasaOS-Common
go mod edit -require=github.com/F-e-n-y-x/NivaroOS/services/common@v0.0.0-00010101000000-000000000000
```

- [ ] **Step 3: Rename config path defaults**

```bash
cd /root/recasa/services/local-storage
grep -rl '/etc/casaos\|/var/lib/casaos\|/var/log/casaos\|/var/run/casaos\|/usr/share/casaos' --include=*.go --include=*.sample --include=*.conf . | xargs -r sed -i \
  -e 's#/etc/casaos#/etc/recasa#g' \
  -e 's#/var/lib/casaos#/var/lib/recasa#g' \
  -e 's#/var/log/casaos#/var/log/recasa#g' \
  -e 's#/var/run/casaos#/var/run/recasa#g' \
  -e 's#/usr/share/casaos#/usr/share/recasa#g'
```

- [ ] **Step 4: Repoint `go:generate` directives at sibling modules**

```bash
cd /root/recasa/services/local-storage
grep -rn "go:generate" main.go
sed -i 's#https://raw.githubusercontent.com/IceWhaleTech/CasaOS-MessageBus/main/api/message_bus/openapi.yaml#../message-bus/api/message_bus/openapi.yaml#' main.go
```

- [ ] **Step 5: Rename binaries in goreleaser config and add to workspace**

```bash
cd /root/recasa/services/local-storage
sed -i \
  -e 's#build/sysroot/usr/bin/casaos-local-storage-migration-tool#build/sysroot/usr/bin/recasa-local-storage-migration-tool#g' \
  -e 's#build/sysroot/usr/bin/casaos-local-storage#build/sysroot/usr/bin/recasa-local-storage#g' \
  .goreleaser.yaml
cd /root/recasa
sed -i '/use .\/services\/user/a use ./services/local-storage' go.work
```

- [ ] **Step 6: Build and test**

```bash
cd /root/recasa/services/local-storage
go mod tidy
go build ./...
go test ./...
```

Expected: build succeeds; test suite passes at parity with the pre-migration copy.

- [ ] **Step 7: Verify no leftover functional branding**

Run: `grep -rli "icewhaletech" /root/recasa/services/local-storage --include=*.go`
Expected: no output.

- [ ] **Step 8: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Migrate services/local-storage (was CasaOS-LocalStorage)"
```

---

## Task 8: Migrate `services/message-bus` (was CasaOS-MessageBus)

**Files:**
- Create: `/root/recasa/services/message-bus/` (copied from `/root/casaos-fork/CasaOS-MessageBus/`)
- Modify: `/root/recasa/go.work`

**Interfaces:**
- Consumes: `github.com/F-e-n-y-x/NivaroOS/services/common` from Task 2.
- Produces: Go module `github.com/F-e-n-y-x/NivaroOS/services/message-bus`, binaries `recasa-message-bus`, `recasa-message-bus-migration-tool`; its `api/message_bus/openapi.yaml` is the file Tasks 3, 4, 6, 7, and 11 point their `go:generate` directives at.

- [ ] **Step 1: Copy the tree, dropping the nested repo**

```bash
cp -a /root/casaos-fork/CasaOS-MessageBus /root/recasa/services/message-bus
rm -rf /root/recasa/services/message-bus/.git
```

- [ ] **Step 2: Rename the module path and repoint the CasaOS-Common dependency**

```bash
cd /root/recasa/services/message-bus
sed -i 's#^module github.com/IceWhaleTech/CasaOS-MessageBus$#module github.com/F-e-n-y-x/NivaroOS/services/message-bus#' go.mod
grep -rl 'github.com/IceWhaleTech/CasaOS-Common' --include=*.go . | xargs -r sed -i 's#github.com/IceWhaleTech/CasaOS-Common#github.com/F-e-n-y-x/NivaroOS/services/common#g'
grep -rl 'github.com/IceWhaleTech/CasaOS-MessageBus' --include=*.go . | xargs -r sed -i 's#github.com/IceWhaleTech/CasaOS-MessageBus#github.com/F-e-n-y-x/NivaroOS/services/message-bus#g'
go mod edit -droprequire=github.com/IceWhaleTech/CasaOS-Common
go mod edit -require=github.com/F-e-n-y-x/NivaroOS/services/common@v0.0.0-00010101000000-000000000000
```

- [ ] **Step 3: Rename config path defaults**

```bash
cd /root/recasa/services/message-bus
grep -rl '/etc/casaos\|/var/lib/casaos\|/var/log/casaos\|/var/run/casaos' --include=*.go --include=*.sample --include=*.conf . | xargs -r sed -i \
  -e 's#/etc/casaos#/etc/recasa#g' \
  -e 's#/var/lib/casaos#/var/lib/recasa#g' \
  -e 's#/var/log/casaos#/var/log/recasa#g' \
  -e 's#/var/run/casaos#/var/run/recasa#g'
```

- [ ] **Step 4: Rename binaries in goreleaser config and add to workspace**

```bash
cd /root/recasa/services/message-bus
sed -i \
  -e 's#build/sysroot/usr/bin/casaos-message-bus-migration-tool#build/sysroot/usr/bin/recasa-message-bus-migration-tool#g' \
  -e 's#build/sysroot/usr/bin/casaos-message-bus#build/sysroot/usr/bin/recasa-message-bus#g' \
  .goreleaser.yaml
cd /root/recasa
sed -i '/use .\/services\/local-storage/a use ./services/message-bus' go.work
```

- [ ] **Step 5: Build and test**

```bash
cd /root/recasa/services/message-bus
go mod tidy
go build ./...
go test ./...
```

Expected: build succeeds; test suite passes at parity with the pre-migration copy.

- [ ] **Step 6: Verify no leftover functional branding**

Run: `grep -rli "icewhaletech" /root/recasa/services/message-bus --include=*.go`
Expected: no output.

- [ ] **Step 7: Rebuild the sibling `go:generate` references now that this module exists, and rerun those services' builds**

```bash
cd /root/recasa/services/core && go build ./...
cd /root/recasa/services/app-management && go build ./...
cd /root/recasa/services/user && go build ./...
cd /root/recasa/services/local-storage && go build ./...
```

Expected: all four still build (the `go:generate` directive edits from Tasks 3/4/6/7 only changed a comment used for manual regeneration, not a compile-time import, so this just re-confirms nothing broke — no output beyond a clean exit).

- [ ] **Step 8: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Migrate services/message-bus (was CasaOS-MessageBus)"
```

---

## Task 9: Migrate `services/vm-sidecar` (was extras/casaos-vm-sidecar)

**Files:**
- Create: `/root/recasa/services/vm-sidecar/` (copied from `/root/casaos-fork/extras/casaos-vm-sidecar/`)
- Modify: `/root/recasa/go.work`

**Interfaces:**
- Produces: Go module `github.com/F-e-n-y-x/NivaroOS/services/vm-sidecar`, binary `recasa-vm-sidecar`. This module has no CasaOS-Common dependency (written from scratch for this fork) and no upstream ties to remove — this task is a pure rename.

- [ ] **Step 1: Copy the tree**

```bash
cp -a /root/casaos-fork/extras/casaos-vm-sidecar /root/recasa/services/vm-sidecar
rm -rf /root/recasa/services/vm-sidecar/.git
```

- [ ] **Step 2: Rename the module path**

```bash
cd /root/recasa/services/vm-sidecar
sed -i 's#^module github.com/F-e-n-y-x/casaos-vm-sidecar$#module github.com/F-e-n-y-x/NivaroOS/services/vm-sidecar#' go.mod
grep -rl 'github.com/F-e-n-y-x/casaos-vm-sidecar' --include=*.go . | xargs -r sed -i 's#github.com/F-e-n-y-x/casaos-vm-sidecar#github.com/F-e-n-y-x/NivaroOS/services/vm-sidecar#g'
```

- [ ] **Step 3: Rename the systemd unit description string and any config path defaults**

```bash
cd /root/recasa/services/vm-sidecar
grep -rn "casaos-vm-sidecar\|CasaOS VM Sidecar" --include=*.go .
```

Rename any hits found (unit description strings, log prefixes) from `CasaOS VM Sidecar` to `Recasa VM Sidecar` and `casaos-vm-sidecar` to `recasa-vm-sidecar` using the same `sed` pattern as prior tasks, then re-grep to confirm zero remaining hits.

- [ ] **Step 4: Add to workspace, build, and test**

```bash
cd /root/recasa
sed -i '/use .\/services\/message-bus/a use ./services/vm-sidecar' go.work
cd services/vm-sidecar
go mod tidy
go build ./...
go test ./...
```

Expected: build succeeds; test suite passes at parity with the pre-migration copy (this module has real test coverage from earlier work — `TestCreateVM_SetsDisplayResolution`, the reset/reboot rename tests, etc. — all must still pass).

- [ ] **Step 5: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Migrate services/vm-sidecar (was extras/casaos-vm-sidecar)"
```

---

## Task 10: Migrate `services/gpu-sidecar` (was extras/casaos-gpu-sidecar)

**Files:**
- Create: `/root/recasa/services/gpu-sidecar/` (copied from `/root/casaos-fork/extras/casaos-gpu-sidecar/`)
- Modify: `/root/recasa/go.work`

**Interfaces:**
- Produces: Go module `github.com/F-e-n-y-x/NivaroOS/services/gpu-sidecar`, binary `recasa-gpu-sidecar`. Same shape as Task 9 — pure rename, no upstream ties.

- [ ] **Step 1: Copy the tree**

```bash
cp -a /root/casaos-fork/extras/casaos-gpu-sidecar /root/recasa/services/gpu-sidecar
rm -rf /root/recasa/services/gpu-sidecar/.git
```

- [ ] **Step 2: Rename the module path**

```bash
cd /root/recasa/services/gpu-sidecar
sed -i 's#^module github.com/F-e-n-y-x/casaos-gpu-sidecar$#module github.com/F-e-n-y-x/NivaroOS/services/gpu-sidecar#' go.mod
grep -rl 'github.com/F-e-n-y-x/casaos-gpu-sidecar' --include=*.go . | xargs -r sed -i 's#github.com/F-e-n-y-x/casaos-gpu-sidecar#github.com/F-e-n-y-x/NivaroOS/services/gpu-sidecar#g'
```

- [ ] **Step 3: Rename any unit-description/log-prefix strings**

```bash
cd /root/recasa/services/gpu-sidecar
grep -rn "casaos-gpu-sidecar\|CasaOS GPU Sidecar" --include=*.go .
```

Rename any hits the same way as Task 9 Step 3, then re-grep to confirm zero remaining hits.

- [ ] **Step 4: Add to workspace, build, and test**

```bash
cd /root/recasa
sed -i '/use .\/services\/vm-sidecar/a use ./services/gpu-sidecar' go.work
cd services/gpu-sidecar
go mod tidy
go build ./...
go test ./...
```

Expected: build succeeds; test suite passes at parity with the pre-migration copy.

- [ ] **Step 5: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Migrate services/gpu-sidecar (was extras/casaos-gpu-sidecar)"
```

---

## Task 11: Migrate `cli/` (was CasaOS-CLI)

**Files:**
- Create: `/root/recasa/cli/` (copied from `/root/casaos-fork/CasaOS-CLI/`)
- Modify: `/root/recasa/go.work`

**Interfaces:**
- Consumes: `github.com/F-e-n-y-x/NivaroOS/services/common` from Task 2.
- Produces: Go module `github.com/F-e-n-y-x/NivaroOS/cli`, binary `recasa-cli` — this is the base Sub-project B builds the install/feature-toggle subcommands onto; this task only renames it.

- [ ] **Step 1: Copy the tree, dropping the nested repo**

```bash
cp -a /root/casaos-fork/CasaOS-CLI /root/recasa/cli
rm -rf /root/recasa/cli/.git
```

- [ ] **Step 2: Rename the module path and repoint the CasaOS-Common dependency**

```bash
cd /root/recasa/cli
sed -i 's#^module github.com/IceWhaleTech/CasaOS-CLI$#module github.com/F-e-n-y-x/NivaroOS/cli#' go.mod
grep -rl 'github.com/IceWhaleTech/CasaOS-Common' --include=*.go . | xargs -r sed -i 's#github.com/IceWhaleTech/CasaOS-Common#github.com/F-e-n-y-x/NivaroOS/services/common#g'
grep -rl 'github.com/IceWhaleTech/CasaOS-CLI' --include=*.go . | xargs -r sed -i 's#github.com/IceWhaleTech/CasaOS-CLI#github.com/F-e-n-y-x/NivaroOS/cli#g'
go mod edit -droprequire=github.com/IceWhaleTech/CasaOS-Common
go mod edit -require=github.com/F-e-n-y-x/NivaroOS/services/common@v0.0.0-00010101000000-000000000000
```

- [ ] **Step 3: Repoint all five `go:generate` directives at the now-local sibling modules**

```bash
cd /root/recasa/cli
grep -n "go:generate" main.go
sed -i \
  -e 's#https://raw.githubusercontent.com/IceWhaleTech/CasaOS-AppManagement/main/api/app_management/openapi.yaml#../services/app-management/api/app_management/openapi.yaml#' \
  -e 's#https://raw.githubusercontent.com/IceWhaleTech/CasaOS/main/api/casaos/openapi.yaml#../services/core/api/casaos/openapi.yaml#' \
  -e 's#https://raw.githubusercontent.com/IceWhaleTech/CasaOS-LocalStorage/main/api/local_storage/openapi.yaml#../services/local-storage/api/local_storage/openapi.yaml#' \
  -e 's#https://raw.githubusercontent.com/IceWhaleTech/CasaOS-MessageBus/main/api/message_bus/openapi.yaml#../services/message-bus/api/message_bus/openapi.yaml#' \
  -e 's#https://raw.githubusercontent.com/IceWhaleTech/CasaOS-UserService/main/api/user-service/openapi.yaml#../services/user/api/user-service/openapi.yaml#' \
  main.go
grep -n "go:generate" main.go
```

Expected (last command): all five lines now reference relative `../services/...` paths, zero `raw.githubusercontent.com` hits remain.

- [ ] **Step 4: Rename the binary in the goreleaser config and add to workspace**

```bash
cd /root/recasa/cli
sed -i 's#build/sysroot/usr/bin/casaos-cli#build/sysroot/usr/bin/recasa-cli#g' .goreleaser.yaml
cd /root/recasa
sed -i '/use .\/services\/gpu-sidecar/a use ./cli' go.work
```

- [ ] **Step 5: Build and test**

```bash
cd /root/recasa/cli
go mod tidy
go build ./...
go test ./...
```

Expected: build succeeds; test suite passes at parity with the pre-migration copy.

- [ ] **Step 6: Verify no leftover functional branding**

Run: `grep -rli "icewhaletech" /root/recasa/cli --include=*.go`
Expected: no output.

- [ ] **Step 7: Whole-workspace sanity build**

```bash
cd /root/recasa
go build ./services/... ./cli/... 2>&1 | tee /tmp/recasa-full-build.log
grep -c "" /tmp/recasa-full-build.log
```

Expected: the grep count is `0` (empty build log — every module in the workspace compiles).

- [ ] **Step 8: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Migrate cli/ (was CasaOS-CLI), repoint all go:generate directives at local siblings"
```

---

## Task 12: Migrate `ui/` — identity, config paths, and build output

**Files:**
- Create: `/root/recasa/ui/` (copied from `/root/casaos-fork/CasaOS-UI/`)
- Modify: `/root/recasa/ui/package.json`
- Modify: `/root/recasa/ui/message_bus.build.js`

**Interfaces:**
- Produces: `ui/` as a git-clean copy with its own product identity (`package.json` name) and build output pointed at `/var/lib/recasa` instead of `/var/lib/casaos`. Vendoring the two `@icewhale/*` npm packages and the branding-string sweep are separate tasks (13, 14) so each has its own independently reviewable/testable deliverable.

- [ ] **Step 1: Copy the tree, dropping the nested repo**

```bash
cp -a /root/casaos-fork/CasaOS-UI /root/recasa/ui
rm -rf /root/recasa/ui/.git
```

- [ ] **Step 2: Rename the npm package identity**

```bash
cd /root/recasa/ui
sed -i 's#"name": "casaos-main"#"name": "recasa-ui"#' package.json
```

- [ ] **Step 3: Repoint the build output destination from `/var/lib/casaos` to `/var/lib/recasa`**

```bash
cd /root/recasa/ui
sed -i "s#--dest ./build/sysroot/var/lib/casaos/www/#--dest ./build/sysroot/var/lib/recasa/www/#" package.json
sed -i \
  -e "s#'../build/sysroot/var/lib/casaos/'#'../build/sysroot/var/lib/recasa/'#" \
  -e "s#'../build/sysroot/etc/casaos/start.d/'#'../build/sysroot/etc/recasa/start.d/'#" \
  message_bus.build.js
```

- [ ] **Step 4: Verify the rename took and nothing else in these two files still says casaos**

Run: `grep -n "casaos" /root/recasa/ui/package.json /root/recasa/ui/message_bus.build.js`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Migrate ui/ (was CasaOS-UI): rename package identity and build output paths"
```

---

## Task 13: Vendor the `@icewhale/*` npm API clients

**Files:**
- Create: `/root/recasa/ui/vendor/api-clients/recasa-core-openapi/` (extracted from the existing `@icewhale/casaos-openapi@0.4.13-2adb795` resolution)
- Create: `/root/recasa/ui/vendor/api-clients/recasa-appmanagement-openapi/`
- Modify: `/root/recasa/ui/package.json`
- Modify: `/root/recasa/ui/src/service/index.js`

**Interfaces:**
- Produces: two local packages under `ui/vendor/api-clients/`, referenced from `ui/package.json` via the `link:` protocol — no `@icewhale` scope resolved from the npm registry after this task.

- [ ] **Step 1: Find the currently-installed copies to extract from**

```bash
cd /root/casaos-fork/CasaOS-UI
ls node_modules/@icewhale/ 2>/dev/null || (pnpm install --frozen-lockfile && ls node_modules/@icewhale/)
```

Expected: lists `casaos-openapi` and `casaos-appmanagement-openapi` directories (install first if `node_modules` isn't already populated).

- [ ] **Step 2: Copy each package out into the new repo's vendor directory**

```bash
mkdir -p /root/recasa/ui/vendor/api-clients
cp -a /root/casaos-fork/CasaOS-UI/node_modules/@icewhale/casaos-openapi /root/recasa/ui/vendor/api-clients/recasa-core-openapi
cp -a /root/casaos-fork/CasaOS-UI/node_modules/@icewhale/casaos-appmanagement-openapi /root/recasa/ui/vendor/api-clients/recasa-appmanagement-openapi
```

- [ ] **Step 3: Rename each vendored package's own identity so it no longer claims the `@icewhale` scope**

```bash
cd /root/recasa/ui/vendor/api-clients/recasa-core-openapi
sed -i 's#"name": "@icewhale/casaos-openapi"#"name": "recasa-core-openapi"#' package.json
cd /root/recasa/ui/vendor/api-clients/recasa-appmanagement-openapi
sed -i 's#"name": "@icewhale/casaos-appmanagement-openapi"#"name": "recasa-appmanagement-openapi"#' package.json
```

- [ ] **Step 4: Repoint `ui/package.json`'s dependency entries at the vendored local packages**

```bash
cd /root/recasa/ui
sed -i \
  -e 's#"@icewhale/casaos-openapi": "latest"#"recasa-core-openapi": "link:./vendor/api-clients/recasa-core-openapi"#' \
  -e 's#"@icewhale/casaos-appmanagement-openapi": "latest"#"recasa-appmanagement-openapi": "link:./vendor/api-clients/recasa-appmanagement-openapi"#' \
  package.json
```

- [ ] **Step 5: Update the one file that imports these packages**

```bash
grep -n "@icewhale" /root/recasa/ui/src/service/index.js
```

Replace each `@icewhale/casaos-openapi` import specifier with `recasa-core-openapi` and each `@icewhale/casaos-appmanagement-openapi` with `recasa-appmanagement-openapi` in `src/service/index.js`:

```bash
sed -i \
  -e "s#@icewhale/casaos-openapi#recasa-core-openapi#g" \
  -e "s#@icewhale/casaos-appmanagement-openapi#recasa-appmanagement-openapi#g" \
  /root/recasa/ui/src/service/index.js
```

- [ ] **Step 6: Reinstall and confirm no `@icewhale` package is resolved**

```bash
cd /root/recasa/ui
rm -rf node_modules pnpm-lock.yaml
pnpm install
grep -c "@icewhale" pnpm-lock.yaml
```

Expected: the `grep -c` prints `0` — the lockfile no longer references the `@icewhale` npm scope at all.

- [ ] **Step 7: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Vendor @icewhale openapi clients as local packages, remove npm registry dependency"
```

---

## Task 14: Branding string sweep in the UI

**Files:**
- Modify: any `ui/src/**/*.vue`, `ui/src/**/*.js` file containing a user-visible "CasaOS" string (discovered in Step 1, not enumerated in advance)

**Interfaces:**
- Produces: no remaining user-facing "CasaOS" text in the UI (login page, window titles, about/settings panels, page `<title>`).

- [ ] **Step 1: Find every user-visible occurrence**

```bash
cd /root/recasa/ui
grep -rln "CasaOS" src/ public/ 2>/dev/null
```

- [ ] **Step 2: Replace the product name in each file found**

For each file the previous step listed, replace the literal string `CasaOS` with `Recasa` (case-sensitive — this is the display name, not a path or identifier, so a plain literal substitution is correct here unlike the path/module renames in earlier tasks):

```bash
cd /root/recasa/ui
grep -rl "CasaOS" src/ public/ 2>/dev/null | xargs -r sed -i 's#CasaOS#Recasa#g'
```

- [ ] **Step 3: Verify**

Run: `grep -rn "CasaOS" /root/recasa/ui/src /root/recasa/ui/public 2>/dev/null`
Expected: no output.

- [ ] **Step 4: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Rebrand user-visible CasaOS strings to Recasa in the UI"
```

---

## Task 15: Windowed UI audit

**Files:**
- Read: `/root/recasa/ui/src/views/Home.vue`
- Read: `/root/recasa/ui/src/components/desktop/Dock.vue`
- Read: `/root/recasa/ui/src/components/desktop/WindowManager.vue`
- Modify: whichever app-registry entries are found not to open windowed (if any)

**Interfaces:**
- Consumes: the existing window-registry pattern established by `Home.vue`'s app list (each entry has `id`, `title`, `component`, `width`, `height` — confirmed present for `FilesApp` and `SettingsApp` in earlier work).
- Produces: every desktop app opens through `WindowManager`/`DesktopWindow`; no app bypasses it with a bare route push.

- [ ] **Step 1: List every app registered in Home.vue's window registry**

Run: `grep -n "id:\|component:" /root/recasa/ui/src/views/Home.vue`

Expected: one `id`/`component` pair per desktop app (Files, Settings, VM Manager, and any others added since). Record the full list.

- [ ] **Step 2: List every entry Dock.vue offers to launch**

Run: `grep -n "@click\|openWindow\|component" /root/recasa/ui/src/components/desktop/Dock.vue`

Cross-check every Dock entry maps to one of the `id`s from Step 1 (i.e., every dock icon opens via the same windowed registry, none navigate via `this.$router.push` or an equivalent bare-route call).

- [ ] **Step 3: Confirm the pre-desktop views are intentionally unwindowed, not stragglers**

Run: `grep -n "component:" /root/recasa/ui/src/router/route.js`

Expected: only `Login.vue`, `Welcome.vue`, `VmConsoleStandalone.vue`, and `Home.vue` itself are registered as top-level routes. These three are correctly outside the windowed system (they exist before/around the desktop shell, not as apps within it) — no change needed for them.

- [ ] **Step 4: Fix any straggler found**

If Step 1 or 2 turned up an app that navigates via a route push instead of the window registry, add it to `Home.vue`'s app list (matching the existing `{ id, title: this.$t('...'), component: '<ComponentName>', width, height }` shape already used for `FilesApp`/`SettingsApp`/`VmManagerApp`) and change its Dock entry to call the existing open-window handler instead of a route push. (If Steps 1–3 find no stragglers, skip this step — do not invent a change to make.)

- [ ] **Step 5: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Windowed UI audit: confirm all desktop apps use the window registry"
```

(If Step 4 made no changes, commit only if some other file in this task was touched; otherwise note in the task tracker that the audit found no stragglers and move on without an empty commit.)

---

## Task 16: Build-verify the UI and commit

**Files:**
- None created — verification only.

**Interfaces:**
- Produces: a working `ui/build/sysroot/var/lib/recasa/www/` production bundle, confirming Tasks 12–15 didn't break the build.

- [ ] **Step 1: Full production build**

```bash
cd /root/recasa/ui
pnpm run build
```

Expected: exits 0, and `build/sysroot/var/lib/recasa/www/index.html` exists.

- [ ] **Step 2: Confirm the built bundle contains the new branding, not the old**

```bash
grep -c "Recasa" /root/recasa/ui/build/sysroot/var/lib/recasa/www/index.html
grep -c "CasaOS" /root/recasa/ui/build/sysroot/var/lib/recasa/www/index.html
```

Expected: first command > 0, second command == 0.

- [ ] **Step 3: Run the existing unit test suite**

```bash
cd /root/recasa/ui
pnpm run test
```

Expected: passes at parity with the pre-migration copy.

- [ ] **Step 4: Clean up the local build artifact (it's gitignored, but confirm)**

Run: `cd /root/recasa && git status --porcelain | grep "ui/build"`
Expected: no output (already covered by the root `.gitignore`'s `/build/` — wait, `ui/build` is nested, not repo-root `build/`; if this shows untracked files, add `ui/build/` and `ui/node_modules/` to `.gitignore`).

If the previous check shows untracked build/node_modules output, fix the `.gitignore`:

```bash
cd /root/recasa
cat >> .gitignore <<'EOF'
ui/build/
ui/node_modules/
EOF
git add .gitignore
```

- [ ] **Step 5: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Verify ui/ builds and tests pass after rebrand"
```

---

## Task 17: Full-repo residual-branding sweep

**Files:**
- Modify: any file the sweep below finds (none enumerated in advance — this is the final catch-all pass).

**Interfaces:**
- Produces: confirmation that `services/`, `ui/src`, and `cli/` contain zero remaining functional (non-cosmetic) references to `casaos` or `icewhale`.

- [ ] **Step 1: Run the sweep**

```bash
cd /root/recasa
grep -rli "casaos\|icewhale" services ui/src cli --include=*.go --include=*.vue --include=*.js 2>/dev/null | grep -v _test.go
```

- [ ] **Step 2: Triage each hit**

For each file listed: if the hit is a cosmetic `@Website:`/license-header comment (acceptable per spec), leave it. If it's a module path, import path, config default, binary name, systemd description, or user-visible string that earlier tasks should have caught, fix it with the same `sed` pattern used in that file's migration task, then re-run Step 1 until only cosmetic hits remain.

- [ ] **Step 3: Final workspace-wide build**

```bash
cd /root/recasa
go build ./services/... ./cli/...
cd ui && pnpm run build
```

Expected: both exit 0.

- [ ] **Step 4: Commit**

```bash
cd /root/recasa
git add -A
git commit -q -m "Final residual-branding sweep"
```

---

## Task 18: Squash to one commit and push to the new GitHub repo

**Files:**
- None created — this is a git/GitHub operation on the already-built `/root/recasa` tree.

**Interfaces:**
- Produces: `https://github.com/F-e-n-y-x/NivaroOS`, containing exactly one commit with the full final tree from Task 17.

- [ ] **Step 1: Confirm the working tree is clean before squashing**

Run: `cd /root/recasa && git status --porcelain`
Expected: no output (everything committed by Task 17).

- [ ] **Step 2: Create a fresh orphan branch and commit the current tree as a single commit**

```bash
cd /root/recasa
git checkout --orphan recasa-init
git add -A
git commit -q -m "Initial commit: Recasa

Standalone rebrand and restructure of the prior CasaOS-based fork.
See docs/superpowers/specs/2026-08-28-recasa-rebrand-restructure-decouple-design.md
for what changed and why."
git branch -D master
git branch -m master
git log --oneline
```

Expected: `git log --oneline` shows exactly one commit.

- [ ] **Step 3: Create the new GitHub repo**

```bash
gh repo create F-e-n-y-x/NivaroOS --private --description "Self-hosted home server platform (standalone, decoupled from CasaOS)"
```

- [ ] **Step 4: Push**

```bash
cd /root/recasa
git remote add origin https://github.com/F-e-n-y-x/NivaroOS.git
git push -u origin master
```

- [ ] **Step 5: Verify on GitHub**

```bash
gh repo view F-e-n-y-x/NivaroOS --json name,description,defaultBranchRef
```

Expected: `name` is `recasa`, `defaultBranchRef.name` is `master`.

No further commit needed — this task's own steps constitute its verification.

---

## Self-Review Notes

- **Spec coverage:** naming scheme (Global Constraints + every migration task), repo layout (Task 1's structure + Tasks 2–13), decoupling — self-update removed (Task 3 Step 4), npm clients vendored (Task 13), CasaOS-Common forked (Task 2, wired into every consumer task) — windowed UI audit (Task 15), single-commit push (Task 18). All spec sections have a task.
- **Placeholder scan:** every step carries a real command using strings verified against this exact codebase (config path defaults, binary names, module paths, `go:generate` URLs) — no "similar to Task N" shorthand; each per-service task repeats its own full commands since an executor may work tasks out of order.
- **Type/name consistency:** module path root `github.com/F-e-n-y-x/NivaroOS/...` and binary names are used identically across every task; `services/common`'s pseudo-version string (`v0.0.0-00010101000000-000000000000`) is the same literal in every consumer task's `go mod edit -require`.
