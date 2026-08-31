# Full NivaroOS Rename Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename every remaining "casaos"/"recasa" identifier, path, unit name, and piece of text in the repo to "nivaroos"/"NivaroOS", without breaking any build, service, or UI/backend integration.

**Architecture:** Five independently-verified phases (installer/build config & baked-in paths; systemd units; message-bus topics & OpenAPI codegen; UI/docs/CLI text; deep cosmetic pass), each its own commit, each gated on `go build ./...` (and a full container install test for the phases that touch install-time behavior) before the next phase starts.

**Tech Stack:** Bash, Go 1.23.4, `.goreleaser.yaml`, systemd units, Vue 2 i18n JSON, Docker (for install verification).

**Spec:** `docs/superpowers/specs/2026-09-01-full-nivaroos-rename-design.md`

## Global Constraints

- Display text (UI headings, docs prose, banner text, CLI help users read) → `NivaroOS`.
- Internal identifiers (binaries, install paths, systemd unit names, package/dir names, env/profile files, message-bus topics, test fixture strings) → lowercase `nivaroos`.
- Do NOT touch: `LICENSE`, and the `ui/src/App.vue` console banner crediting IceWhale/the original CasaOS project (real historical attribution, not branding).
- DO remove (don't just reword) stale `// @Website: https://www.casaos.io` swagger doc-comments - no accurate replacement URL exists.
- Do NOT touch `docs/superpowers/plans/**` or `docs/superpowers/specs/**` (dated historical records) except this plan and its own spec file.
- `BACKLOG.md`: update only its title line; leave its historical feature notes untouched.
- Every task's file edits must keep the repo building: after each task, `go build ./...` must succeed from repo root (via `go.work`) with zero errors.

---

### Task 1: Installer, build config, and baked-in install paths

**Files:**
- Modify: `installer/install.sh`, `installer/uninstall.sh`
- Modify: `services/{core,gateway,user,app-management,local-storage,message-bus}/.goreleaser.yaml`, `cli/.goreleaser.yaml` (7 files, 31 `binary:` lines total)
- Modify: `services/common/utils/constants/paths.go`
- Modify: `services/common/utils/version/migration.go`
- Modify: `services/app-management/route/v1/docker.go`, `services/app-management/cmd/migration-tool/main.go`
- Modify: `services/user/pkg/config/config.go`, `services/user/route/v1/user.go`, `services/user/cmd/migration-tool/main.go`
- Modify: `services/local-storage/pkg/config/init.go`, `services/local-storage/pkg/config/config.go`, `services/local-storage/route/v2/merge.go`, `services/local-storage/service/v2/fs/mergerfs.go`, `services/local-storage/cmd/migration-tool/main.go`
- Modify: `services/message-bus/config/config.go`, `services/message-bus/cmd/migration-tool/main.go`
- Modify: `services/core/pkg/config/init.go`, `services/core/route/v2/health.go`, `services/core/route/v1/pkg_updates.go`, `services/core/service/system.go`
- Modify: `cli/cmd/root.go` (path const only - its `Use`/`Short`/copyright text is Task 4), `cli/cmd/appManagementSetGlobal.go` (doc comment)
- Modify: `ui/register-ui-events.sh` (two hardcoded `/var/run/recasa` and `/var/lib/recasa` paths)
- Modify (paths/binaries only, NOT `Description=`/`After=`/`Before=` lines - those are Task 2's): `services/core/build/sysroot/usr/lib/systemd/system/casaos.service`, `services/gateway/build/sysroot/usr/lib/systemd/system/casaos-gateway.service`, `services/user/build/sysroot/usr/lib/systemd/system/casaos-user-service.service`, `services/app-management/build/sysroot/usr/lib/systemd/system/casaos-app-management.service`, `services/local-storage/build/sysroot/usr/lib/systemd/system/casaos-local-storage.service`, `services/message-bus/build/sysroot/usr/lib/systemd/system/casaos-message-bus.service`
- Rename (git mv) every `build/sysroot/etc/recasa` → `build/sysroot/etc/nivaroos` and `build/sysroot/usr/share/recasa` → `build/sysroot/usr/share/nivaroos` directory under `services/core`, `services/gateway`, `services/user`, `services/app-management`, `services/local-storage`, `services/message-bus` (whichever exist per service - not all services have both)
- Rename (git mv) `services/core/build/sysroot/var/lib/recasa` → `.../var/lib/nivaroos` and `ui/build/sysroot/var/lib/recasa` → `.../var/lib/nivaroos` if present (check with `find . -path '*/build/sysroot/var/lib/recasa' -o -path '*/build/sysroot/usr/share/recasa' -o -path '*/build/sysroot/etc/recasa'` first and rename exactly what's found - do not assume every service has every directory)

**Interfaces:**
- Produces: every install-time path now under `/opt/nivaroos`, `/etc/nivaroos`, `/usr/share/nivaroos`, `/var/lib/nivaroos`, `/var/log/nivaroos`; every built binary named `nivaroos*` instead of `recasa*`. Task 2 depends on these paths being final before it touches systemd units (unit files' `ExecStart=` lines reference these binary paths).

- [ ] **Step 1: Find every literal path/binary occurrence before editing**

Run this from the repo root and read the full output before making any edit - it's your source of truth for what Step 2 needs to touch:

```bash
grep -rn "recasa-\|/opt/recasa\|/etc/recasa\|/usr/share/recasa\|/var/lib/recasa\|/var/log/recasa\|recasa-go\.sh" \
  --include="*.go" --include="*.sh" --include="*.yaml" . \
  | grep -v -E "node_modules|/build/sysroot/|docs/superpowers/(plans|specs)/"
find . -path '*/build/sysroot/var/lib/recasa' -o -path '*/build/sysroot/usr/share/recasa' -o -path '*/build/sysroot/etc/recasa' 2>/dev/null | grep -v node_modules
```

- [ ] **Step 2: Rename binaries and paths in `.goreleaser.yaml` files**

In each of the 7 `.goreleaser.yaml` files listed above, every `binary: build/sysroot/usr/bin/recasa...` line loses the `recasa` and gains `nivaroos` at the same position, e.g. in `services/core/.goreleaser.yaml`:

```yaml
    binary: build/sysroot/usr/bin/recasa
```
becomes
```yaml
    binary: build/sysroot/usr/bin/nivaroos
```
and `services/core/.goreleaser.yaml`'s migration-tool binary line:
```yaml
    binary: build/sysroot/usr/bin/recasa-migration-tool
```
becomes
```yaml
    binary: build/sysroot/usr/bin/nivaroos-migration-tool
```
Apply the same `recasa` → `nivaroos` substring swap to every other module's `binary:` lines (`recasa-app-management` → `nivaroos-app-management`, `recasa-app-management-migration-tool` → `nivaroos-app-management-migration-tool`, `recasa-app-management-validator` → `nivaroos-app-management-validator`, `recasa-gateway`/`recasa-gateway-migration-tool`, `recasa-local-storage`/`-migration-tool`, `recasa-message-bus`/`-migration-tool`, `recasa-user`/`-migration-tool`, `recasa-cli`). Confirm you got all 31 with:
```bash
grep -c "binary:.*recasa" services/*/.goreleaser.yaml cli/.goreleaser.yaml
```
Expected: every file prints `0`.

- [ ] **Step 3: Rewrite `installer/install.sh`**

Change every occurrence of `recasa` (binary names, `SRC_DIR`, `/var/lib/recasa`, `/etc/profile.d/recasa-go.sh`) to `nivaroos`, and `NIVAROOS` banner text stays as-is (already correct). Concretely:
- `SRC_DIR="/opt/recasa/src"` → `SRC_DIR="/opt/nivaroos/src"`
- Every `local bin_name="recasa-$name"` / `bin_name="recasa"` (core special case) → `"nivaroos-$name"` / `"nivaroos"`
- `GPU_SIDECAR_UNIT=/usr/lib/systemd/system/recasa-gpu-sidecar.service` → `nivaroos-gpu-sidecar.service` (unit content's `ExecStart=/usr/bin/recasa-gpu-sidecar` → `/usr/bin/nivaroos-gpu-sidecar`) - **note:** the unit's own filename/description text is Task 2's concern for the *casaos*-named units, but `recasa-gpu-sidecar`/`recasa-vm-sidecar` are already `recasa`-prefixed (not `casaos`-prefixed) so their rename belongs here in Task 1, not Task 2.
- Same for `VM_SIDECAR_UNIT`, its `ExecStart=`, and its `After=network.target recasa-message-bus.service` line - **do not** rename `recasa-message-bus.service` here, that's a `casaos`-prefixed real unit renamed in Task 2 (`casaos-message-bus.service` → `nivaroos-message-bus.service`); leave this `After=` reference as `casaos-message-bus.service`-shaped for now, Task 2 will change it when it renames the unit itself. To avoid this dangling inconsistency, write it as `After=network.target casaos-message-bus.service` in this task (matching today's real unit name) - Task 2 renames it together with the other unit references.
- `install_cli()`'s `go build -o /usr/bin/recasa-cli .` → `/usr/bin/nivaroos-cli`
- `install_ui()`'s `/var/lib/recasa/www` (both the `mkdir -p` and the `cp -a` source path `$SRC_DIR/ui/build/sysroot/var/lib/recasa/www/`) → `/var/lib/nivaroos/www` and `.../var/lib/nivaroos/www/`
- `print_summary()`'s `'recasa-cli vm enable'` hint text → `'nivaroos-cli vm enable'`
- `install_go_toolchain()`'s `/etc/profile.d/recasa-go.sh` → `/etc/profile.d/nivaroos-go.sh`
- `install_vm_manager()`'s `go build -o /usr/bin/recasa-vm-sidecar .` → `/usr/bin/nivaroos-vm-sidecar`, and its `systemctl enable --now recasa-vm-sidecar.service` → `nivaroos-vm-sidecar.service`
- Leave `LEGACY_SERVICE_UNITS="casaos-gateway.service ..."` untouched in this task - Task 2 renames it.

- [ ] **Step 4: Rewrite `installer/uninstall.sh` the same way**

Mirror every change from Step 3: `SRC_DIR`, binary list (`/usr/bin/recasa*` → `/usr/bin/nivaroos*`), `/etc/recasa /usr/share/recasa /var/lib/recasa` → `/etc/nivaroos /usr/share/nivaroos /var/lib/nivaroos`, `/etc/profile.d/recasa-go.sh` → `nivaroos-go.sh`. In `ALL_UNITS`, only rename `recasa-gpu-sidecar.service` → `nivaroos-gpu-sidecar.service` and `recasa-vm-sidecar.service` → `nivaroos-vm-sidecar.service` (leave the `casaos-*` unit names for Task 2). Same for `remove_unit_files()`'s `rm -f` list - only its two `recasa-*.service` lines change here.

- [ ] **Step 5: Rewrite the Go path constants**

`services/common/utils/constants/paths.go`:
```go
DefaultConfigPath   = "/etc/nivaroos"
DefaultConstantPath = "/usr/share/nivaroos"
DefaultDataPath     = "/var/lib/nivaroos"
DefaultFilePath     = "/var/lib/nivaroos/files"
DefaultLogPath      = "/var/log/nivaroos"
DefaultRuntimePath  = "/var/run/nivaroos"
```
Then apply the equivalent `/etc/recasa`→`/etc/nivaroos`, `/usr/share/recasa`→`/usr/share/nivaroos`, `/var/lib/recasa`→`/var/lib/nivaroos`, `/var/log/recasa`→`/var/log/nivaroos`, `/var/run/recasa`→`/var/run/nivaroos` substitution to every other file listed under **Files** above (each is a small, self-contained string constant or literal - e.g. `services/core/route/v1/pkg_updates.go:140`'s `/var/log/recasa-apt-upgrade.log` becomes `/var/log/nivaroos-apt-upgrade.log`; `services/local-storage/pkg/config/init.go`'s `RuntimePath: "/var/run/recasa"` becomes `"/var/run/nivaroos"`). In `ui/register-ui-events.sh`, change `runtime_file="/var/run/recasa/message-bus.url"` → `"/var/run/nivaroos/message-bus.url"`, the `cat /var/run/recasa/message-bus.url` line the same way, and `ui_message_bus_file="/var/lib/recasa/ui-message-bus.json"` → `"/var/lib/nivaroos/ui-message-bus.json"`. Use the Step 1 grep output as your checklist and confirm zero remain when done:
```bash
grep -rn "/etc/recasa\|/usr/share/recasa\|/var/lib/recasa\|/var/log/recasa\|/var/run/recasa" --include="*.go" --include="*.sh" . | grep -v "/build/sysroot/"
```
Expected: no output (the 6 real `.service` files under `build/sysroot/` still contain these paths at this point in the plan - Step 6 below handles those specifically, which is why this check excludes that directory).

- [ ] **Step 6: Rewrite the 6 real systemd unit files' internal paths (NOT their `Description=`/`After=`/`Before=` lines - Task 2 owns those)**

Each of the 6 real unit files listed under **Files** above has `ExecStart=`/`ExecStartPre=`/`PIDFile=`/`ConditionFileNotEmpty=` lines that hardcode `/usr/bin/recasa*`, `/etc/recasa/*.conf`, and `/var/run/recasa/*.pid` paths. Rewrite only those lines' paths in each file (leave `Description=` and every `After=`/`Before=` line exactly as they are today - Task 2 renames those together with the unit filenames). For example, `services/gateway/build/sysroot/usr/lib/systemd/system/casaos-gateway.service` today reads:
```ini
[Unit]
After=network.target
Description=Recasa Gateway

[Service]
ExecStartPre=/usr/bin/recasa-gateway -v
ExecStart=/usr/bin/recasa-gateway
PIDFile=/var/run/recasa/gateway.pid
Restart=always
Type=notify

[Install]
WantedBy=multi-user.target
```
becomes (only the `ExecStartPre=`/`ExecStart=`/`PIDFile=` lines change):
```ini
[Unit]
After=network.target
Description=Recasa Gateway

[Service]
ExecStartPre=/usr/bin/nivaroos-gateway -v
ExecStart=/usr/bin/nivaroos-gateway
PIDFile=/var/run/nivaroos/gateway.pid
Restart=always
Type=notify

[Install]
WantedBy=multi-user.target
```
Read each of the other 5 files first (they're all under 15 lines) and apply the same pattern: only lines starting with `ExecStart=`, `ExecStartPre=`, `PIDFile=`, or `ConditionFileNotEmpty=` change, swapping `recasa` for `nivaroos` in binary and path names. `services/core/build/sysroot/usr/lib/systemd/system/casaos.service`'s `ExecStart=/usr/bin/recasa -c /etc/recasa/casaos.conf` becomes `ExecStart=/usr/bin/nivaroos -c /etc/nivaroos/casaos.conf` (the config file's own basename, `casaos.conf`, is untouched here - renaming config file basenames is out of scope for this plan; only the directory prefix changes).

- [ ] **Step 7: Rename the on-disk `build/sysroot` path directories**

For every directory Step 1's `find` command located, `git mv` it, e.g.:
```bash
git mv services/core/build/sysroot/etc/recasa services/core/build/sysroot/etc/nivaroos
git mv services/core/build/sysroot/usr/share/recasa services/core/build/sysroot/usr/share/nivaroos
```
Repeat for every service where these directories exist (not all services have all three - use Step 1's `find` output, don't guess). Do the same for `ui/build/sysroot/var/lib/recasa` → `.../var/lib/nivaroos` if it exists.

- [ ] **Step 8: Build and verify**

```bash
cd /root/recasa && go build ./... 2>&1 | tee /tmp/task1-build.log
```
Expected: no output (success). Fix any compile errors before continuing - a missed rename will show up here as an "undefined" or unused-import error.

- [ ] **Step 9: Full container install test**

```bash
mkdir -p /tmp/nivaroos-rename-test
cat > /tmp/nivaroos-rename-test/Dockerfile <<'EOF'
FROM debian:trixie
RUN apt-get update && apt-get install -y systemd systemd-sysv git ca-certificates curl && apt-get clean
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
EOF
docker build -q -t nivaroos-rename-img /tmp/nivaroos-rename-test
docker run -d --name nivaroos-rename-c --privileged --cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw nivaroos-rename-img
sleep 3
docker exec nivaroos-rename-c systemctl is-system-running --wait || true
docker cp installer/install.sh nivaroos-rename-c:/root/install.sh
```
Since this task's branch isn't pushed/merged yet, copy your locally-modified repo into the container instead of cloning from GitHub, then point `install.sh` at it by pre-seeding `/opt/nivaroos/src` and letting `clone_repo()`'s "existing checkout" branch take over:
```bash
docker exec nivaroos-rename-c mkdir -p /opt/nivaroos
docker cp . nivaroos-rename-c:/opt/nivaroos/src
docker exec nivaroos-rename-c bash -c "cd /opt/nivaroos/src && git add -A && git -c user.email=t@t -c user.name=t commit -q -m wip || true"
docker exec -t nivaroos-rename-c bash -c 'cat /root/install.sh | bash > /root/install.log 2>&1; echo "EXIT_CODE=$?" >> /root/install.log'
docker exec nivaroos-rename-c grep -a EXIT_CODE /root/install.log
docker exec nivaroos-rename-c tail -20 /root/install.log
```
Expected: `EXIT_CODE=0`. Note `clone_repo()`'s "existing checkout" path runs `git fetch origin master && git reset --hard origin/master` - since this test repo has no real `origin` remote reachable the way the container's copy is set up, this step is expected to either no-op safely or you may need to temporarily comment out the `run_step "Updating existing checkout..."` line for this local-only test and restore it afterward (do not commit that comment-out).

- [ ] **Step 10: Clean up test containers**

```bash
docker rm -f nivaroos-rename-c
docker rmi nivaroos-rename-img
rm -rf /tmp/nivaroos-rename-test
```

- [ ] **Step 11: Commit**

```bash
git add -A
git commit -m "rename: installer, build config, and install-time paths to nivaroos"
```

---

### Task 2: systemd units

**Files:**
- Rename (git mv): `services/core/build/sysroot/usr/lib/systemd/system/casaos.service` → `nivaroos.service`
- Rename (git mv): `services/gateway/build/sysroot/usr/lib/systemd/system/casaos-gateway.service` → `nivaroos-gateway.service` (and its `.service.buildroot` sibling)
- Rename (git mv): `services/user/build/sysroot/usr/lib/systemd/system/casaos-user-service.service` → `nivaroos-user-service.service`
- Rename (git mv): `services/app-management/build/sysroot/usr/lib/systemd/system/casaos-app-management.service` → `nivaroos-app-management.service` (and its `.service.buildroot` sibling)
- Rename (git mv): `services/local-storage/build/sysroot/usr/lib/systemd/system/casaos-local-storage.service` → `nivaroos-local-storage.service`
- Rename (git mv): `services/message-bus/build/sysroot/usr/lib/systemd/system/casaos-message-bus.service` → `nivaroos-message-bus.service`
- Modify: contents of each renamed unit file (`Description=` and any `After=`/`Before=` line referencing another renamed unit by name - NOT the `ExecStart=`/`ExecStartPre=`/`PIDFile=`/`ConditionFileNotEmpty=` lines, which Task 1 already updated)
- Modify: `services/core/cmd/migration-tool/main.go`, `services/gateway/cmd/migration-tool/main.go`, `services/user/cmd/migration-tool/main.go`, `services/app-management/cmd/migration-tool/main.go`, `services/local-storage/cmd/migration-tool/main.go`, `services/message-bus/cmd/migration-tool/main.go`
- Modify: `installer/install.sh` (`LEGACY_SERVICE_UNITS`, the VM sidecar unit's `After=` line from Task 1 Step 3), `installer/uninstall.sh` (`ALL_UNITS`, `remove_unit_files()`)
- Rename (git mv) + modify: `services/core/build/scripts/migration/script.d/03-migrate-casaos.sh` → `03-migrate-nivaroos.sh`, `services/core/build/scripts/setup/script.d/03-setup-casaos.sh` → `03-setup-nivaroos.sh`, `services/core/build/sysroot/usr/share/nivaroos/cleanup/script.d/03-cleanup-casaos.sh` → `03-cleanup-nivaroos.sh` (this file's parent dir was already renamed from `usr/share/recasa` in Task 1 Step 6)
- Rename (git mv) directories: `services/core/build/scripts/migration/service.d/casaos` → `.../service.d/nivaroos`, `services/core/build/scripts/setup/service.d/casaos` → `.../service.d/nivaroos`, `services/core/build/sysroot/usr/share/nivaroos/cleanup/service.d/casaos` → `.../service.d/nivaroos`
- Modify: contents of files inside those three renamed directory trees that reference `casaos.service` or `cleanup-casaos.sh`/`setup-casaos.sh`/`migrate-casaos.sh` by name (rename the referenced filenames to match their new names, and rename each `*-casaos.sh` script file itself, e.g. `cleanup-casaos.sh` → `cleanup-nivaroos.sh`, in all three `arch`/`debian`/`ubuntu` subdirectories)

**Interfaces:**
- Consumes: Task 1's renamed binary paths (unit `ExecStart=` lines must point at the now-`nivaroos-*`-named binaries).
- Produces: every real systemd unit is named `nivaroos*.service`. Task 3/4/5 don't depend on unit names.

- [ ] **Step 1: Rename the 6 real unit files, and update their `Description=`/`After=`/`Before=` lines (their `ExecStart=`/`ExecStartPre=`/`PIDFile=`/`ConditionFileNotEmpty=` lines were already updated by Task 1 - leave those alone)**

```bash
git mv services/core/build/sysroot/usr/lib/systemd/system/casaos.service services/core/build/sysroot/usr/lib/systemd/system/nivaroos.service
git mv services/gateway/build/sysroot/usr/lib/systemd/system/casaos-gateway.service services/gateway/build/sysroot/usr/lib/systemd/system/nivaroos-gateway.service
git mv services/gateway/build/sysroot/usr/lib/systemd/system/casaos-gateway.service.buildroot services/gateway/build/sysroot/usr/lib/systemd/system/nivaroos-gateway.service.buildroot
git mv services/user/build/sysroot/usr/lib/systemd/system/casaos-user-service.service services/user/build/sysroot/usr/lib/systemd/system/nivaroos-user-service.service
git mv services/app-management/build/sysroot/usr/lib/systemd/system/casaos-app-management.service services/app-management/build/sysroot/usr/lib/systemd/system/nivaroos-app-management.service
git mv services/app-management/build/sysroot/usr/lib/systemd/system/casaos-app-management.service.buildroot services/app-management/build/sysroot/usr/lib/systemd/system/nivaroos-app-management.service.buildroot
git mv services/local-storage/build/sysroot/usr/lib/systemd/system/casaos-local-storage.service services/local-storage/build/sysroot/usr/lib/systemd/system/nivaroos-local-storage.service
git mv services/message-bus/build/sysroot/usr/lib/systemd/system/casaos-message-bus.service services/message-bus/build/sysroot/usr/lib/systemd/system/nivaroos-message-bus.service
```

These 6 units reference each other by name in `After=`/`Before=` lines, so renaming the files without updating those cross-references would leave every unit pointing at a dependency that no longer exists. After Task 1's edits, each file (aside from the rename itself) should read exactly as follows - apply only the changes shown between "entering this task" and "target":

`nivaroos.service` (core) - entering this task:
```ini
[Unit]
After=casaos-message-bus.service
After=rclone.service
Description=Recasa Main Service
```
target (only `After=casaos-message-bus.service` and `Description=` change; `After=rclone.service` stays exactly as-is - `rclone.service` is a third-party unit name, not being renamed):
```ini
[Unit]
After=nivaroos-message-bus.service
After=rclone.service
Description=NivaroOS Main Service
```

`nivaroos-gateway.service` - entering this task: `After=network.target` / `Description=Recasa Gateway`. Target: `After=network.target` unchanged (no other unit referenced), `Description=NivaroOS Gateway`. Read `nivaroos-gateway.service.buildroot` and apply the same `Description=` rename to whatever text it currently has (it may say "CasaOS Gateway" rather than "Recasa Gateway" - the two files have drifted slightly; rename whichever word appears to `NivaroOS`).

`nivaroos-user-service.service` - entering this task: `After=casaos-message-bus.service` / `Description=Recasa User Service`. Target: `After=nivaroos-message-bus.service`, `Description=NivaroOS User Service`.

`nivaroos-app-management.service` - entering this task:
```ini
After=casaos-message-bus.service
After=docker.service
Description=Recasa App Management Service
```
target (`docker.service` is unrelated to this rename, leave it):
```ini
After=nivaroos-message-bus.service
After=docker.service
Description=NivaroOS App Management Service
```
Apply the same `Description=` rename to `nivaroos-app-management.service.buildroot`.

`nivaroos-local-storage.service` - entering this task:
```ini
After=casaos-gateway.service
After=casaos-message-bus.service
After=casaos-user-service.service
After=casaos.service
Before=docker.service
Description=Recasa Local Storage Service
```
target:
```ini
After=nivaroos-gateway.service
After=nivaroos-message-bus.service
After=nivaroos-user-service.service
After=nivaroos.service
Before=docker.service
Description=NivaroOS Local Storage Service
```

`nivaroos-message-bus.service` - entering this task: `After=casaos-gateway.service` / `Description=Recasa Message Bus Service`. Target: `After=nivaroos-gateway.service`, `Description=NivaroOS Message Bus Service`.

Confirm no unit references an old name when done:
```bash
grep -rn "casaos" services/*/build/sysroot/usr/lib/systemd/system/*.service*
```
Expected: no output.

- [ ] **Step 2: Update the migration-tool Go constants**

In `services/core/cmd/migration-tool/main.go`: `casaosServiceName = "casaos.service"` → `nivaroosServiceName = "nivaroos.service"` (rename the identifier too, and update its ~2 use sites in the same file accordingly). Do the equivalent in the other 5 migration-tool `main.go` files: `gatewayServiceName = "casaos-gateway.service"` → `"nivaroos-gateway.service"`, `appManagementName = "casaos-app-management.service"` → `"nivaroos-app-management.service"`, `userServiceName = "casaos-user-service.service"` → `"nivaroos-user-service.service"`, `localStorageName = "casaos-local-storage.service"` → `"nivaroos-local-storage.service"`, `messageBusName = "casaos-message-bus.service"` → `"nivaroos-message-bus.service"` (keep each existing Go identifier name as-is, only change the string value - these are already lowercase-neutral variable names, no rename needed there).

- [ ] **Step 3: Update `installer/install.sh` and `installer/uninstall.sh`**

`install.sh`: `LEGACY_SERVICE_UNITS="casaos-gateway.service casaos-message-bus.service casaos.service casaos-user-service.service casaos-app-management.service casaos-local-storage.service"` → `LEGACY_SERVICE_UNITS="nivaroos-gateway.service nivaroos-message-bus.service nivaroos.service nivaroos-user-service.service nivaroos-app-management.service nivaroos-local-storage.service"`. Also fix the VM sidecar unit's `After=network.target casaos-message-bus.service` (left dangling by Task 1) to `After=network.target nivaroos-message-bus.service`.

`uninstall.sh`: `ALL_UNITS="casaos-gateway.service casaos-message-bus.service casaos.service casaos-user-service.service casaos-app-management.service casaos-local-storage.service nivaroos-gpu-sidecar.service nivaroos-vm-sidecar.service rclone.service"` (note: the two `recasa-*` entries already became `nivaroos-*` in Task 1 - only the six `casaos-*` entries change here). Update `remove_unit_files()`'s `rm -f` list the same way (6 `casaos-*` paths → `nivaroos-*`, keep the `.buildroot` variants' renamed paths too).

- [ ] **Step 4: Rename the migration/setup/cleanup script files and their `service.d` subtrees**

```bash
git mv services/core/build/scripts/migration/script.d/03-migrate-casaos.sh services/core/build/scripts/migration/script.d/03-migrate-nivaroos.sh
git mv services/core/build/scripts/setup/script.d/03-setup-casaos.sh services/core/build/scripts/setup/script.d/03-setup-nivaroos.sh
git mv services/core/build/sysroot/usr/share/nivaroos/cleanup/script.d/03-cleanup-casaos.sh services/core/build/sysroot/usr/share/nivaroos/cleanup/script.d/03-cleanup-nivaroos.sh
git mv services/core/build/scripts/migration/service.d/casaos services/core/build/scripts/migration/service.d/nivaroos
git mv services/core/build/scripts/setup/service.d/casaos services/core/build/scripts/setup/service.d/nivaroos
git mv services/core/build/sysroot/usr/share/nivaroos/cleanup/service.d/casaos services/core/build/sysroot/usr/share/nivaroos/cleanup/service.d/nivaroos
```
Then rename every `*-casaos.sh` file inside those three now-`nivaroos`-named directory trees (e.g. `services/core/build/scripts/setup/service.d/nivaroos/debian/setup-casaos.sh` → `setup-nivaroos.sh`, and the `arch`/`ubuntu` siblings, plus the `cleanup-casaos.sh` files under the sysroot tree's `arch`/`debian`/`ubuntu` subdirectories) with `git mv`, and inside each renamed script, change any `casaos.service` reference to `nivaroos.service` and any reference to a sibling script's old filename to its new one. **Before editing these, run `grep -rln "casaos" services/core/build/scripts/setup/service.d/nivaroos services/core/build/scripts/migration/service.d/nivaroos services/core/build/sysroot/usr/share/nivaroos/cleanup/service.d/nivaroos` to get the exact current list of files/lines - don't assume the content shown in this plan is exhaustive.** Per the spec, this whole `service.d/nivaroos` subtree appears unused by any current code path (`install_service()` in `install.sh` only globs `build/scripts/setup/script.d/*.sh`, a sibling directory) - rename it for consistency, do not delete it, and do not spend extra time investigating further; that's a separate cleanup decision outside this plan's scope.

- [ ] **Step 5: Build and verify**

```bash
cd /root/recasa && go build ./... 2>&1 | tee /tmp/task2-build.log
```
Expected: no output.

- [ ] **Step 6: Full container install test**

This branch still isn't pushed to GitHub, so seed the container from your local working copy via `docker cp` rather than a real clone:
```bash
mkdir -p /tmp/nivaroos-rename-test
cat > /tmp/nivaroos-rename-test/Dockerfile <<'EOF'
FROM debian:trixie
RUN apt-get update && apt-get install -y systemd systemd-sysv git ca-certificates curl && apt-get clean
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
EOF
docker build -q -t nivaroos-rename-img /tmp/nivaroos-rename-test
docker run -d --name nivaroos-rename-c --privileged --cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw nivaroos-rename-img
sleep 3
docker exec nivaroos-rename-c systemctl is-system-running --wait || true
docker cp installer/install.sh nivaroos-rename-c:/root/install.sh
docker exec nivaroos-rename-c mkdir -p /opt/nivaroos
docker cp . nivaroos-rename-c:/opt/nivaroos/src
docker exec nivaroos-rename-c bash -c "cd /opt/nivaroos/src && git add -A && git -c user.email=t@t -c user.name=t commit -q -m wip || true"
docker exec -t nivaroos-rename-c bash -c 'cat /root/install.sh | bash > /root/install.log 2>&1; echo "EXIT_CODE=$?" >> /root/install.log'
docker exec nivaroos-rename-c grep -a EXIT_CODE /root/install.log
docker exec nivaroos-rename-c tail -20 /root/install.log
docker exec nivaroos-rename-c systemctl is-active nivaroos.service nivaroos-gateway.service nivaroos-message-bus.service nivaroos-user-service.service nivaroos-app-management.service nivaroos-local-storage.service nivaroos-gpu-sidecar.service
```
Expected: `EXIT_CODE=0` and `active` for all 7 units. As in Task 1 Step 8, if `clone_repo()`'s "existing checkout" branch (`git fetch origin master && git reset --hard origin/master`) fails because this test repo has no reachable real `origin`, temporarily comment out that `run_step` line for this local-only test and restore it afterward (do not commit that comment-out). Then clean up:
```bash
docker rm -f nivaroos-rename-c
docker rmi nivaroos-rename-img
rm -rf /tmp/nivaroos-rename-test
```

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "rename: systemd units and their references to nivaroos"
```

---

### Task 3: Message-bus topics & OpenAPI codegen

**Files:**
- Modify: `services/core/common/message.go` (the 3 topic string literals)
- Modify: `services/core/route/periodical.go`, `services/core/route/v1/recover.go`, `services/core/service/notify.go` (3 call sites), `services/local-storage/route/v1/recover.go` (12 call sites), `services/local-storage/main.go` (1 commented-out reference - update for consistency even though it's dead code)
- Rename (git mv): `services/core/api/casaos/` → `services/core/api/nivaroos/` (contains `openapi.yaml`)
- Rename (git mv): `cli/codegen/casaos/` → `cli/codegen/nivaroos/` (regenerated in this task, not hand-edited)
- Modify: `services/core/main.go` (`go:generate` line 1), `cli/main.go` (`go:generate` line 2)
- Rename (git mv): `services/core/codegen/casaos_api.go` → `services/core/codegen/nivaroos_api.go` (regenerated, not hand-edited - package name stays `codegen`, only the filename changes)
- Modify: `cli/cmd/healthcheckServices.go`, `cli/cmd/healthcheckPortsInUse.go`, `cli/cmd/healthcheckLogs.go` (import path + `casaos.` qualifier → `nivaroos.`)

**Interfaces:**
- Produces: message-bus topics `nivaroos:system:utilization`, `nivaroos:file:recover`, `nivaroos:file:operate`; codegen package `cli/codegen/nivaroos` (package name `nivaroos`, was `casaos`).

- [ ] **Step 1: Rename the 3 message-bus topic strings at their single definition site**

`services/core/common/message.go`:
```go
var EventTypes = []message_bus.EventType{
	{Name: "nivaroos:system:utilization", SourceID: SERVICENAME, PropertyTypeList: []message_bus.PropertyType{}},
	{Name: "nivaroos:file:recover", SourceID: SERVICENAME, PropertyTypeList: []message_bus.PropertyType{}},
	{Name: "nivaroos:file:operate", SourceID: SERVICENAME, PropertyTypeList: []message_bus.PropertyType{}},
}
```

- [ ] **Step 2: Rename every publish call site**

Replace the literal string at each of these locations (do not change any other part of the call):
- `services/core/route/periodical.go:74`: `SendNotify("casaos:system:utilization", body)` → `SendNotify("nivaroos:system:utilization", body)`
- `services/core/route/v1/recover.go:24`: `event := "casaos:file:recover"` → `event := "nivaroos:file:recover"`, and its use at line 32: `SendNotify("casaos:file:recover", notify)` → `SendNotify(event, notify)` or `SendNotify("nivaroos:file:recover", notify)` (match whatever the surrounding code already does - read the file first, don't guess)
- `services/core/service/notify.go` lines 91, 150, 219: each `"casaos:file:operate"` → `"nivaroos:file:operate"`
- `services/local-storage/route/v1/recover.go`: all 12 `SendNotify("casaos:file:recover", notify)` call sites (lines 31, 47, 57, 68, 86, 109, 118, 132, 141, 153, 173, 205) → `SendNotify("nivaroos:file:recover", notify)`
- `services/local-storage/main.go:183`: the commented-out `// //events = append(events, message_bus.EventType{Name: "casaos:file:recover", ...})` → update the string inside the comment to `"nivaroos:file:recover"` too, for consistency (it's dead code either way, but shouldn't reference a topic name that no longer exists)

Confirm zero old topic strings remain anywhere, **including the UI** (per the spec's explicit warning that a UI socket listener on the old name would silently stop receiving events):
```bash
grep -rn "casaos:system:utilization\|casaos:file:recover\|casaos:file:operate" --include="*.go" --include="*.vue" --include="*.js" . | grep -v node_modules
```
Expected: no output. If the UI *does* reference any of these strings, update it the same way and rebuild the UI (`cd ui && pnpm run build`) as part of this step - don't leave it for a later task.

- [ ] **Step 3: Rename the OpenAPI spec directory and regenerate core's codegen**

```bash
git mv services/core/api/casaos services/core/api/nivaroos
```
Edit `services/core/main.go`'s first `go:generate` line from:
```go
//go:generate bash -c "mkdir -p codegen && go run github.com/deepmap/oapi-codegen/cmd/oapi-codegen@v1.12.4 -generate types,server,spec -package codegen api/casaos/openapi.yaml > codegen/casaos_api.go"
```
to:
```go
//go:generate bash -c "mkdir -p codegen && go run github.com/deepmap/oapi-codegen/cmd/oapi-codegen@v1.12.4 -generate types,server,spec -package codegen api/nivaroos/openapi.yaml > codegen/nivaroos_api.go"
```
(package name `codegen` is unchanged - only the spec path and output filename change). Then regenerate and rename the old output file:
```bash
cd services/core && go generate ./... && cd -
git rm services/core/codegen/casaos_api.go 2>/dev/null || rm -f services/core/codegen/casaos_api.go
git add services/core/codegen/nivaroos_api.go
```

- [ ] **Step 4: Regenerate the CLI's codegen package**

Edit `cli/main.go`'s second `go:generate` line from:
```go
//go:generate bash -c "mkdir -p codegen/casaos && go run github.com/deepmap/oapi-codegen/cmd/oapi-codegen@v1.12.4 -generate types,client -package casaos ../services/core/api/casaos/openapi.yaml > codegen/casaos/api.go"
```
to:
```go
//go:generate bash -c "mkdir -p codegen/nivaroos && go run github.com/deepmap/oapi-codegen/cmd/oapi-codegen@v1.12.4 -generate types,client -package nivaroos ../services/core/api/nivaroos/openapi.yaml > codegen/nivaroos/api.go"
```
Then:
```bash
cd cli && go generate ./... && cd -
git rm -r cli/codegen/casaos 2>/dev/null || rm -rf cli/codegen/casaos
git add cli/codegen/nivaroos
```

- [ ] **Step 5: Update the CLI's 3 consumer files**

In `cli/cmd/healthcheckServices.go`, `cli/cmd/healthcheckPortsInUse.go`, and `cli/cmd/healthcheckLogs.go`: update the import path from `.../cli/codegen/casaos` to `.../cli/codegen/nivaroos`, and change every `casaos.NewClientWithResponses(url)` / `casaos.BaseResponse` reference to `nivaroos.NewClientWithResponses(url)` / `nivaroos.BaseResponse`. Read each file's import block first to get the exact existing import path string (it's a full module path like `github.com/F-e-n-y-x/NivaroOS/cli/codegen/casaos` - only rename the final path segment, not the whole path).

- [ ] **Step 6: Build and test**

```bash
cd /root/recasa && go build ./... 2>&1 | tee /tmp/task3-build.log
go test ./services/core/... ./services/local-storage/... ./cli/... 2>&1 | tee /tmp/task3-test.log
```
Expected: build produces no output; tests show `ok` for every package (no `FAIL`). A missed import or qualifier rename shows up here as a compile error - fix and re-run before continuing.

- [ ] **Step 7: Commit (including the regenerated, tracked codegen output)**

```bash
git add -A
git commit -m "rename: message-bus topics and OpenAPI codegen packages to nivaroos"
```

---

### Task 4: UI text, translations, docs, CLI help

**Files:**
- Modify: all 32 files under `ui/src/assets/lang/*.json`
- Modify: `cli/cmd/root.go`, `cli/cmd/qrcode.go`, `cli/cmd/healthcheckLogs.go`, `cli/cmd/healthcheckServices.go`, `cli/cmd/appManagementSetGlobal.go` (copyright headers, `Use`/`Short`/`Long`/flag-description strings - not the path/import changes already done in Tasks 1 and 3)
- Modify: `README.md` (any remaining `recasa`/`casaos` mentions not already fixed in earlier work this session - re-check, don't assume it's fully clean)
- Modify: `BACKLOG.md` (title line only, per Global Constraints)

**Interfaces:**
- Consumes: nothing from earlier tasks (pure text content).
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: List every lang file and check for key/value drift, not just literal strings**

```bash
ls ui/src/assets/lang/*.json | wc -l
grep -rln "asaos\|ecasa" ui/src/assets/lang/*.json
```
For each file the second command lists, open it and check whether the JSON *key* and *value* already disagree (per the known `zh_CN.json` case: key says English "NivaroOS...nivaroos.io" while the value still says "CasaOS...casaos.io"). Where they already agree (both still say CasaOS/casaos.io, or similar), update both key and value consistently to NivaroOS/nivaroos.io. Where they already disagree (one side already migrated), fix the side that's still wrong so both match. Do not do a blind global find/replace across these files without reading each match first - translated *values* in non-English files may phonetically transliterate "CasaOS" differently than a literal substring match would catch, so also spot-check 3-4 non-English files beyond what the grep finds by eye for anything the literal grep missed.

- [ ] **Step 2: Update CLI help/usage text and copyright headers**

`cli/cmd/root.go`:
```go
RootGroupID    = "nivaroos-cli"
```
```go
Use:   "nivaroos-cli",
Short: "A command line interface for NivaroOS",
```
and its `"root url of Recasa API"` flag description → `"root url of NivaroOS API"`. Its `Copyright © 2022 Recasa` header → `Copyright © 2022 NivaroOS`.

`cli/cmd/healthcheckLogs.go`: `Copyright © 2023 Recasa` → `Copyright © 2023 NivaroOS`; `` Short:   "get all `casaos-*` logs and save to a ZIP file" `` → `` "get all `nivaroos-*` logs and save to a ZIP file" `` (matches Task 2's unit rename); `os.MkdirTemp("", "recasa-cli-*")` → `"nivaroos-cli-*"`.

`cli/cmd/healthcheckServices.go`: `Copyright © 2023 Recasa` → `Copyright © 2023 NivaroOS`; `` Short:   "get running status of each `casaos-*` service" `` → `` "get running status of each `nivaroos-*` service" ``.

`cli/cmd/qrcode.go`: `Copyright © 2023 Recasa` → `Copyright © 2023 NivaroOS`; `Short: "show qrcode to Recasa WebUI"` → `"show qrcode to NivaroOS WebUI"`.

`cli/cmd/appManagementSetGlobal.go`: its doc comment `Global environment variables are stored at 'env' file at Recasa configuration path, e.g. /etc/recasa/env` → `... at NivaroOS configuration path, e.g. /etc/nivaroos/env` (the path itself was already renamed in Task 1 - this step is just the prose around it).

- [ ] **Step 3: Re-check README.md and fix BACKLOG.md's title**

```bash
grep -in "casaos\|recasa" README.md
```
Fix anything this finds that isn't already-correct NivaroOS branding or an intentional historical reference (e.g. the "ZimaOS-inspired" mention is a different, unrelated project name - do not touch that). Then in `BACKLOG.md`, change only its first line:
```markdown
# NivaroOS Fork — Feature Backlog
```
(from `# CasaOS Fork — Feature Backlog`). Leave every other line in `BACKLOG.md` untouched per Global Constraints.

- [ ] **Step 4: Build and verify**

```bash
cd /root/recasa && go build ./... 2>&1 | tee /tmp/task4-build.log
cd ui && pnpm run build 2>&1 | tail -30
```
Expected: Go build silent (JSON/markdown changes don't affect it, but confirms nothing else broke); UI build completes without error (a malformed JSON file from Step 1 would fail this build loudly).

- [ ] **Step 5: Commit**

```bash
cd /root/recasa
git add -A
git commit -m "rename: UI translations, CLI help text, and docs to NivaroOS"
```

---

### Task 5: Deep Go-internal cosmetic pass

**Files:**
- Modify: `services/core/route/v1/samba.go`, `services/core/route/init.go`, `services/local-storage/route/v1/storage.go`, `services/core/cmd/migration-tool/main.go`, `services/core/route/v1/samba_test.go` (remove stale `// @Website: https://www.casaos.io` lines)
- Modify: `services/gateway/route/management_route_test.go` (cosmetic test fixture string)
- Modify: any remaining comments/log messages surfaced by this task's own repo-wide grep (see Step 1 - do not assume the list above is exhaustive)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: nothing consumed by later tasks. This is the last content-changing task before Task 6's verification.

- [ ] **Step 1: Find everything left**

```bash
grep -rli "casaos\|recasa" --include="*" . 2>/dev/null \
  | grep -v -E "node_modules|\.git/|dist/|/build/|docs/superpowers/(plans|specs)/"
```
Read through the full list. Anything not already handled by Tasks 1-4 belongs here. Expect it to be short (mostly comments and doc-annotations) - if it isn't, stop and re-check whether an earlier task's step was actually completed rather than plowing ahead.

- [ ] **Step 2: Remove stale `@Website` swagger annotations**

Delete the `// @Website: https://www.casaos.io` line entirely (per Global Constraints - not "update to a NivaroOS URL", since no accurate one exists) from each of: `services/core/route/v1/samba.go:8`, `services/core/route/init.go:8`, `services/local-storage/route/v1/storage.go:7`, `services/core/cmd/migration-tool/main.go:8`, `services/core/route/v1/samba_test.go:8`, and anywhere else Step 1's grep surfaced this same line.

- [ ] **Step 3: Rename the cosmetic test fixture string**

`services/gateway/route/management_route_test.go:30`:
```go
tmpdir, _ := os.MkdirTemp("", "casaos-gateway-route-test")
```
→
```go
tmpdir, _ := os.MkdirTemp("", "nivaroos-gateway-route-test")
```

- [ ] **Step 4: Handle anything else Step 1 found**

For each remaining hit, apply the same convention as everywhere else in this plan (lowercase `nivaroos` for identifiers/paths/comments referring to internal implementation, `NivaroOS` for anything that reads as a product name in prose) - except genuine upstream attribution, which Global Constraints says to leave alone. If you find something that looks like upstream attribution you're not sure about, leave it untouched and flag it in your task report rather than guessing.

- [ ] **Step 5: Build and test everything**

```bash
cd /root/recasa && go build ./... 2>&1 | tee /tmp/task5-build.log
go test ./... 2>&1 | tee /tmp/task5-test.log
```
Expected: build silent, every package `ok`.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "rename: remove stale upstream doc annotations, final cosmetic pass"
```

---

### Task 6: Final end-to-end verification

**Files:** none modified - this task only verifies.

**Interfaces:**
- Consumes: the fully-renamed repo from Tasks 1-5.

- [ ] **Step 1: Repo-wide completeness grep**

```bash
grep -rli "casaos\|recasa" --include="*" . 2>/dev/null \
  | grep -v -E "node_modules|\.git/|dist/|/build/|docs/superpowers/(plans|specs)/|LICENSE|App\.vue"
```
Expected: no output (or only `App.vue`/`LICENSE`, which Global Constraints explicitly preserves). If anything else remains, go back and fix it as part of this task before proceeding - don't report success with stragglers left.

- [ ] **Step 2: Full build and test suite, every module**

```bash
cd /root/recasa && go build ./... && go test ./... 2>&1 | tail -60
```
Expected: build succeeds, no `FAIL` in test output.

- [ ] **Step 3: Full fresh-clone container install test**

Push the branch (or merge to whatever branch the installer's `REPO_URL` points at) first, since this final test must use a genuine `git clone` from GitHub - not a local `docker cp` - matching every other install verification done in this project:
```bash
mkdir -p /tmp/nivaroos-final-test
cat > /tmp/nivaroos-final-test/Dockerfile <<'EOF'
FROM debian:trixie
RUN apt-get update && apt-get install -y systemd systemd-sysv git ca-certificates curl && apt-get clean
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
EOF
docker build -q -t nivaroos-final-img /tmp/nivaroos-final-test
docker run -d --name nivaroos-final-c --privileged --cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw nivaroos-final-img
sleep 3
docker exec nivaroos-final-c systemctl is-system-running --wait || true
docker exec nivaroos-final-c bash -c 'curl -fsSL https://raw.githubusercontent.com/F-e-n-y-x/NivaroOS/master/installer/install.sh -o /root/install.sh'
timeout 600 docker exec -t nivaroos-final-c bash -c 'cat /root/install.sh | bash > /root/install.log 2>&1; echo "EXIT_CODE=$?" >> /root/install.log'
docker exec nivaroos-final-c grep -a EXIT_CODE /root/install.log
docker exec nivaroos-final-c systemctl is-active nivaroos.service nivaroos-gateway.service nivaroos-message-bus.service nivaroos-user-service.service nivaroos-app-management.service nivaroos-local-storage.service nivaroos-gpu-sidecar.service
docker exec nivaroos-final-c curl -sf http://localhost/ -o /dev/null && echo "PASS: UI reachable"
docker exec nivaroos-final-c nivaroos-cli vm enable
docker exec nivaroos-final-c systemctl is-active nivaroos-vm-sidecar.service
docker exec nivaroos-final-c nivaroos-cli vm disable
```
Expected: `EXIT_CODE=0`, all 7 core units `active`, UI reachable, VM enable/disable both work.

- [ ] **Step 4: Test the uninstaller against the renamed install**

```bash
docker exec nivaroos-final-c bash -c 'curl -fsSL https://raw.githubusercontent.com/F-e-n-y-x/NivaroOS/master/installer/uninstall.sh -o /root/uninstall.sh'
docker exec -t nivaroos-final-c bash -c 'cat /root/uninstall.sh | bash > /root/uninstall.log 2>&1; echo "EXIT_CODE=$?" >> /root/uninstall.log'
docker exec nivaroos-final-c grep -a EXIT_CODE /root/uninstall.log
for u in nivaroos.service nivaroos-gateway.service nivaroos-message-bus.service nivaroos-user-service.service nivaroos-app-management.service nivaroos-local-storage.service nivaroos-gpu-sidecar.service; do
  docker exec nivaroos-final-c systemctl status "$u" >/dev/null 2>&1 && echo "$u: STILL PRESENT (bad)" || echo "$u: gone (good)"
done
```
Expected: `EXIT_CODE=0`, every unit gone.

- [ ] **Step 5: Clean up test artifacts**

```bash
docker rm -f nivaroos-final-c
docker rmi nivaroos-final-img
rm -rf /tmp/nivaroos-final-test
```

- [ ] **Step 6: Report completion**

Summarize: all 5 phases done, repo-wide grep clean, full build/test suite green, fresh-clone install/uninstall both verified end-to-end with the new names. No further steps.
