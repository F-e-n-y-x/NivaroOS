# Full NivaroOS Rename — Design

## Context

The project was renamed from "Recasa" to "NivaroOS" and the GitHub repo moved
to `F-e-n-y-x/NivaroOS`. Go module paths (`github.com/F-e-n-y-x/NivaroOS/...`)
and most UI/doc branding already reflect this. What's left is everything the
module-path rename didn't touch: real binary names, systemd unit names,
install paths, message-bus event topics, OpenAPI codegen package names, CLI
help text, translation files, and assorted internal identifiers/comments -
482 files total reference "casaos" or "recasa" (337 + 145, case-insensitive).

An audit (see "Audit findings" below) confirmed this is safe to do without
breaking external compatibility: no service imports an external
`github.com/IceWhaleTech/CasaOS-*` Go module (those dependencies are gone -
everything is already vendored/rewritten under our own module path), and no
HTTP route literally contains a `/casaos/...` path segment. The risk is
internal: several "casaos"/"recasa" strings are load-bearing identifiers
(message-bus topics, systemd unit names referenced from Go code, OpenAPI
codegen package names), not just cosmetic text.

## Naming convention

- **Display text** (UI headings, docs prose, banner text, CLI help
  descriptions users read): `NivaroOS`.
- **Internal identifiers** (binaries, install paths, systemd unit names,
  package/directory names, env/profile files, message-bus topic strings,
  test fixture strings): lowercase `nivaroos`.
- **Preserved, not renamed**: genuine historical/legal attribution -
  `LICENSE`, and the `ui/src/App.vue` console banner crediting IceWhale and
  the original CasaOS project as what this was forked from. That is
  accurate history, not branding, and removing it would misrepresent the
  project's origin.
- **Updated, not preserved**: stale `@Website: https://www.casaos.io`
  swagger doc-comments scattered in a few route files. These are not legal
  attribution (that's `LICENSE`'s job) - they're copy-pasted API doc
  metadata that is simply wrong now. Remove them (no accurate NivaroOS docs
  site exists to point to instead).
- **Out of scope**: `docs/superpowers/plans/**` and
  `docs/superpowers/specs/**` (this file's own directory). These are dated
  historical records of past planning/implementation work (e.g.
  `2026-08-19-casaos-fork-milestone1-design.md`) - rewriting them would be
  revisionist, not a rename. `BACKLOG.md` gets only its title and
  currently-live product-name references updated; its historical feature
  notes (which already reference some now-superseded internal names) are
  left as-is.

## Audit findings (already gathered, informs phase scope below)

- **No external CasaOS Go dependency remains** - confirmed via `go.mod`
  scan across all 10 modules.
- **No HTTP route path segment is literally `/casaos/...`** - confirmed via
  repo-wide grep.
- Message-bus topics are defined once and published from a small, fully
  internal set of call sites - safe to rename atomically, but the **UI must
  also be checked** for the same topic strings (a socket listener
  subscribing to the old name would silently stop receiving events after a
  backend-only rename - exactly the class of silent failure this session
  has already hit twice).
- `services/core/build/scripts/setup/service.d/casaos/**` looks unused by
  any current code path (`install_service()` in `install.sh` only globs
  `build/scripts/setup/script.d/*.sh`, a sibling directory, not this one).
  Rename it for consistency but do not delete it as part of this project -
  deleting dead code is a separate decision from renaming live code.
- `ui/src/assets/lang/zh_CN.json` already has a **real, pre-existing
  key/value drift bug**: the JSON key was updated to English
  "NivaroOS...nivaroos.io" by an earlier pass, but the Chinese *value*
  still reads "CasaOS...casaos.io". Phase 4 must audit all 32 language
  files for this same drift, not do a blind literal find/replace.

## Phases

Each phase is its own commit (or small commit group), gated on
`go build ./...` (and `go test ./...` where applicable) passing for every
affected module before moving to the next phase. Phases 1-2 additionally
get a full container install test, matching how every other installer
change this project has been verified.

### Phase 1 - Installer & build config
Rename `recasa-*` binary names to `nivaroos-*` in all 8 `.goreleaser.yaml`
files (core stays suffix-less: `recasa` → `nivaroos`). Update
`installer/install.sh` and `installer/uninstall.sh`: `/opt/recasa` →
`/opt/nivaroos`, `/var/lib/recasa` → `/var/lib/nivaroos`,
`/etc/profile.d/recasa-go.sh` → `nivaroos-go.sh`, every binary name and
path string. Mechanical, no ambiguity.

### Phase 2 - systemd units
Rename the 6 real `.service` files under each service's
`build/sysroot/.../systemd/system/`, and every place their filename is
hardcoded: each service's `cmd/migration-tool/main.go` constant,
`install.sh`'s `LEGACY_SERVICE_UNITS`, `uninstall.sh`'s `ALL_UNITS`, and the
`service.d/03-*-casaos.sh` cleanup scripts that reference `casaos.service`
by name. Rename (don't delete) the likely-unused
`build/scripts/setup/service.d/casaos/**` subtree per the audit note above.

### Phase 3 - Message-bus topics & OpenAPI codegen
Rename the 3 topic strings (`casaos:system:utilization`,
`casaos:file:recover`, `casaos:file:operate`) at every publish site *and*
grep `ui/` for the same strings before considering this phase done. Rename
`codegen/casaos` → `codegen/nivaroos` package directories (both
`cli/codegen/casaos` and `services/core/api/casaos`), update the
`go:generate` lines' package/output-path arguments in `cli/main.go` and
`services/core/main.go`, regenerate, and commit the regenerated output
(these are tracked now, not gitignored, per the earlier codegen fix).
Acceptance gate: `go build ./...` across every module - a missed import
will fail loudly here rather than silently misbehave at runtime.

### Phase 4 - UI text, translations, docs, CLI help
All 32 `ui/src/assets/lang/*` files - audited for key/value drift (per the
`zh_CN.json` finding), not just literal string replacement. `cli/cmd/*.go`
help/usage strings (`Use: "recasa-cli"`, `Short`/`Long` descriptions,
`qrcode.go`'s "Recasa WebUI" string, `healthcheckLogs.go`/
`healthcheckServices.go`'s `casaos-*` unit-name-prefix references - these
must track whatever Phase 2 renamed the units to). `README.md`'s remaining
mentions. `BACKLOG.md`'s title only, per the scope note above.

### Phase 5 - Deep Go-internal cosmetic pass
Remaining `@Website: https://www.casaos.io` swagger comments (removed, not
updated - see naming convention above), and the cosmetic test-fixture
string `"casaos-gateway-route-test"` in
`services/gateway/route/management_route_test.go`. Lowest risk, done last;
includes `_test.go` files, which should not be skipped just because
they're tests.

## Testing strategy

- Phases 1-2: full container install test (the same `debian:trixie` +
  systemd harness used throughout this project) - install succeeds, all
  services active, `nivaroos-cli vm enable`/`disable` both work.
- Phase 3: `go build ./...` and `go test ./...` for every module that
  publishes or subscribes to a renamed topic, plus a repo-wide grep
  (backend and UI) confirming zero remaining occurrences of the three old
  topic strings.
- Phase 4: a repo-wide grep confirming no remaining literal "casaos"/
  "recasa" in `ui/src/assets/lang/*` or `cli/cmd/*.go`, and a manual spot
  check of 3-4 language files' key/value pairs for drift (not just the
  already-known `zh_CN.json` case).
- Phase 5: `go build ./...` and `go test ./...` repo-wide as a final
  sanity pass.
- After all 5 phases: one more full container install test end-to-end
  (fresh clone, matching every prior verification this session) plus a
  final repo-wide grep for "casaos"/"recasa" outside the excluded
  `docs/superpowers/plans|specs` paths, to confirm the rename is complete.
