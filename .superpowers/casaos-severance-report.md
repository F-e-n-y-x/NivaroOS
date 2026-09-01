# CasaOS/IceWhaleTech Severance Report

Date: 2026-09-01
Scope: Sever remaining connections between NivaroOS and the upstream IceWhaleTech/CasaOS project, per explicit owner instruction (including removing the 39 Apache-2.0-implicated copyright headers, a risk the owner was warned about and accepted).

## 1. Copyright headers (39 files)

Removed the `Copyright (c) 2022/2023 by IceWhale, All Rights Reserved.` header comment block (along with the surrounding `@Author`/`@LastEditors`/`@FilePath` boilerplate comment) from all 39 files found via:
`grep -rl "Copyright (c) .* by IceWhale" --include="*" . | grep -v -E "node_modules|\.git/"`

All were UI source files (`ui/message_bus.build.js`, `ui/mock/meta_data.js`, `ui/public/favicon.svg`, various `.vue`/`.js`/`.svg` files under `ui/src/**`). Each header was a self-contained `/* ... */` or `<!-- ... -->` block at the very top of the file with no other legally-required boilerplate mixed in, so full removal was syntactically safe — no replacement header was needed or inserted. Verified afterward: the grep now returns zero files.

## 2. GitHub Actions workflows tied to upstream automation — deleted entirely

- `services/core/.github/workflows/sync_openapi.yml`
- `services/core/.github/sync_openapi.yml` (a duplicate/stray copy outside `workflows/`)
- `services/core/.github/workflows/add_issues_to_projects.yml`
- `services/core/.github/workflows/move_alpha_bug_to_project.yml`
- `services/core/.github/workflows/casa.yml` (an old xgo-based CasaOS build workflow, clearly superseded by the goreleaser `release.yml`)
- `services/app-management/.github/workflows/sync_openapi.yml`
- `ui/.github/workflows/push_test_server.yml` — not explicitly named in the dispatch, but on inspection this pushes UI builds via SSH+ZeroTier to a specific internal IP, guarded by `if: github.repository == 'IceWhaleTech/CasaOS-UI'`. It's IceWhaleTech's own private internal test-server infrastructure (ZeroTier network ID secret, hardcoded IP, a "CasaOS-UI push error" webhook) with no NivaroOS equivalent, so it was deleted rather than repointed.

All of these referenced `IceWhaleTech`'s own GitHub Projects boards, a private reusable "sync_openapi" workflow, or IceWhaleTech-only infrastructure — none of it applicable to this independent project.

## 3. `.github/ISSUE_TEMPLATE/config.yml` (core)

All four `contact_links` pointed at IceWhaleTech's own AppStore issue tracker, GitHub Discussions (x2), and their Discord — no NivaroOS-equivalent community exists yet. Removed the file entirely (the sibling `alpha_bug_report.yml`, `bug_report.md`, `feature_request.md` templates in the same directory are untouched and still functional).

## 4. `release.yml` — real release CI, kept working, IceWhaleTech dependency removed

This turned out to be more than a simple text swap. `services/{core,app-management,user,gateway,message-bus}/.github/workflows/release.yml` all called a **reusable workflow hosted in IceWhaleTech's own repo**: `uses: IceWhaleTech/github/.github/workflows/go_release.yml@main`. I fetched that file's actual content (it's public) to understand it precisely before touching anything: it does cross-compile setup, `goreleaser-action`, then uploads artifacts via **IceWhaleTech's own custom action** `IceWhaleTech/oss-action@v1.0.1` into their own OSS bucket, plus a `GOPRIVATE=github.com/IceWhaleTech` git-credential rewrite for private-repo access (confirmed via `go.mod` grep that none of these services actually depend on any private `github.com/IceWhaleTech/*` Go module, so that rewrite was dead weight here).

I inlined the safe, well-understood parts (checkout, setup-go, `goreleaser-action` with the same secrets each already declared) directly into each `release.yml`, dropping the IceWhaleTech-only reusable workflow, their custom action, and the unnecessary `GOPRIVATE` rewrite. For the OSS-mirror upload step (a supplementary China-CDN mirror, not the primary GitHub-Releases publish path, which is unaffected and still handled by `goreleaser` itself), I reconstructed it using the **same third-party action already used elsewhere in this repo** (`tvrcgo/upload-to-oss`, as seen in `local-storage`/`cli`'s pre-existing `release.yml`), with exact artifact filenames verified against each service's own `.goreleaser.yaml` `project_name`/archive `name_template` (I did not guess these — I read each file), and repointed the OSS object-key path prefix from `/IceWhaleTech/<old-name>/...` to `/F-e-n-y-x/NivaroOS<-Suffix>/...`.

For `services/local-storage/.github/workflows/release.yml` and `cli/.github/workflows/release.yml` (which already had this pattern inlined, not touched by any earlier rename pass), I did a straight text swap of the `IceWhaleTech` org and the stale `casaos-local-storage`/`casaos-cli` artifact-name segments (these no longer matched the actual goreleaser output name — `nivaroos-local-storage`/`nivaroos-cli` — so this fixes a pre-existing latent bug as a side effect, not something I introduced).

Also fixed a dead commented-out legacy `owner: IceWhaleTech / name: CasaOS` block in `services/core/.goreleaser.yaml` (superseded by the active block right below it, itself already correctly set to `F-e-n-y-x/nivaroos`).

**Caveat on the OSS-mirror uploads**: I cannot verify the `OSS_KEY_ID`/`OSS_KEY_SECRET` GitHub secrets actually point at a bucket the owner controls, or that `bucket: casaos` is still a real, owned resource — I left that bucket name untouched (it's not an "IceWhaleTech" string) and only changed the path prefixes inside it. If this OSS mirror isn't actually in active use, these steps are harmless (they'll just fail auth and the job continues... actually they may hard-fail the job — worth the owner spot-checking on the next real tag push).

## 5. `README.md` files — badges/links repointed or removed

Per-service READMEs (`services/{common,app-management,user,gateway,local-storage,message-bus}/README.md`, `cli/README.md`, `ui/README.md`) had badges referencing IceWhaleTech's GitHub repos, SonarCloud, and codecov:
- **Go Reference / Go Report Card / goreleaser badges**: repointed to the real current Go module path (verified from each `go.mod`) and `F-e-n-y-x/NivaroOS<-Suffix>` — these work automatically for any public repo, no registration needed.
- **codecov / SonarCloud badges**: removed outright (no NivaroOS-registered project/token exists for these services on those platforms — a broken/misleading badge is worse than no badge).

`services/core/README.md` (the full original CasaOS marketing README, much larger) got the same treatment for its banner image, version/license/PR/issues/stars badges, Discord badge, YouTube badge, and Website/Demo/GitHub links — all removed or repointed to `F-e-n-y-x/NivaroOS` where a real equivalent exists, removed where it doesn't (banner image hosted on IceWhaleTech's own logo repo, YouTube channel, `casaos.io` website — none of these have a NivaroOS equivalent). The "Changelog → release notes" link was repointed to `F-e-n-y-x/NivaroOS/releases`.

**Left alone (historical), by design**: the "Credits" contributor table in `services/core/README.md` (and the identical copy in `ui/vendor/api-clients/recasa-core-openapi/README.md`) still links to `github.com/IceWhaleTech/CasaOS/commits?author=X` for ~20 real historical contributors. I treated this the same way as the CHANGELOG.md guidance (item 5) — it's a genuine historical attribution record of real contributions made to the actual upstream repo; rewriting the org would misattribute/break those specific links rather than sever a branding connection. The prose "Community"/Discord/`wiki.casaos.io` sections in that same README also don't literally say "IceWhaleTech" and were left out of scope (matches the strict grep-driven file list, and the earlier full CasaOS-branding rename was described as already completed as a separate project).

### Top-level `/root/recasa/README.md` — correction applied mid-task

The coordinator sent a correction while I was working: the owner wants the top-level README to **keep** clear fork attribution to CasaOS/IceWhaleTech (unlike per-service READMEs, which should still be severed). The top-level README had **no such statement at all** before I touched it (I hadn't modified it up to that point). I added a dedicated section, matching the README's existing heading/emoji style and `---` separator convention, placed between "Authors & Contributors" and "Contributing":

```
## 🙏 Acknowledgments

NivaroOS is a fork of [CasaOS](https://github.com/IceWhaleTech/CasaOS), originally created by [IceWhaleTech](https://github.com/IceWhaleTech). Thank you to the original CasaOS team and community for the foundation this project was built on.
```

This is the one deliberate exception to "sever every link" in the whole task, per explicit owner instruction — a credit/attribution link, not a functional connection.

## 6. `services/core/CHANGELOG.md` — genuine historical changelog, left untouched

Read the full 616-line file. It's a real, dated `Keep a Changelog`-format changelog going back to v0.1.0 (2021), with real historical bug-fix entries linking to real IceWhaleTech/CasaOS GitHub issue numbers. There is no separate header/badge/"compare on GitHub" link to remove (unlike what the dispatch anticipated as the likely target) — the IceWhaleTech references are all inline within the historical dated entries themselves. Per the historical-record guidance, left entirely unchanged.

## 7. `services/core/.gitmodules` — deleted

Confirmed vestigial as described: no `UI` directory exists under `services/core`, `git submodule status` returns nothing. Deleted the file.

## 8. `.goreleaser.debug.yaml` files — fixed, not superseded

All 7 (`core`, `app-management`, `user`, `gateway`, `local-storage`, `message-bus`, `cli`) had a stale `owner: IceWhaleTech` / `name: CasaOS<-Suffix>` release block, mismatched against their already-renamed main `.goreleaser.yaml` (`owner: F-e-n-y-x` / `name: nivaroos`). Fixed all 7 to match. Additionally, `services/app-management/.goreleaser.debug.yaml` had a stale Go import path (`github.com/IceWhaleTech/CasaOS-AppManagement/cmd/appfile2compose`) in 3 build hooks that no longer compiles (confirmed by diffing against the main `.goreleaser.yaml`, which already uses the correct current path) — fixed to match. These are debug/local-only configs, not part of CI, so this doesn't affect any automated build.

## 9. Test fixtures / sample data — case-by-case, documented reasoning

- **`sample.docker-compose.yaml`, `sample-appfile-export.json`, `validator_test.go`**: icon/thumbnail/screenshot URLs pointing at `cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/...`. Confirmed by reading `pkg.VaildDockerCompose` (the only function these fixtures feed into) that it's pure YAML-schema validation — no network fetch of these URLs occurs. Updated freely to `F-e-n-y-x/NivaroOS-AppStore@main`. Ran the validator tests after the change — all pass.
- **`getter_test.go`**: `TestDownload` genuinely downloads `https://github.com/IceWhaleTech/get/archive/refs/heads/main.zip` over the network and asserts no error. I initially swapped this to `github.com/F-e-n-y-x/NivaroOS/archive/refs/heads/master.zip` (confirmed reachable via `curl`), but reverted: the original IceWhaleTech test repo is a deliberately tiny (~48KB) purpose-built fixture, while the NivaroOS monorepo zip is ~13MB — a much heavier, slower download for a unit test. **Left unchanged and flagged** rather than degrade test speed/CI bandwidth for a branding-only concern.
- **`appstore_test.go` / `appstore_management_test.go`**: `TestGetComposeApp`/`TestGetApp`/`TestRegisterAppStore` genuinely fetch and parse a real app catalog from `https://github.com/IceWhaleTech/_appstore/archive/refs/heads/main.zip` (confirmed reachable, ~1.2MB, real content — verified via `curl`). No NivaroOS-hosted equivalent app-store catalog repo exists (`F-e-n-y-x/_appstore` and `F-e-n-y-x/NivaroOS-AppStore` both 404). **Left unchanged and flagged** — these are real functional dependencies with real content assertions (`composeApp.Services`, etc.), not just "any zip works."
  - However, in the same file, `TestWorkDir` and `TestStoreRoot` test pure string-parsing/hashing logic (`WorkDir()`'s MD5-based cache-path derivation) with **no network I/O at all** — these I updated confidently: changed the test URL/path to `F-e-n-y-x/NivaroOS-AppStore`, recomputed the expected MD5 hash by hand (`94c295f55b20d91b89db102df31ba07b`, verified by reading the actual `WorkDir()` implementation in `appstore.go`), and reran the tests — pass.
- **`migration_044_and_older.go`**: **Left entirely unchanged.** This is not test data — it's production migration logic whose entire purpose is to detect literal old `OldUrl` string values that real historical CasaOS installations (pre-0.4.4) actually wrote to disk in their config files, and rewrite them. Changing `OldUrl` would break the migration tool's ability to detect real, existing user configs. This is the clearest case of "leave the functional dependency alone."

## 10. `services/gateway/route/gateway_route.go`

Two comments cite real historical GitHub issue/security-advisory numbers from the upstream repo (`// fix https://github.com/IceWhaleTech/CasaOS/issues/1247`, `// to fix .../security/advisories/GHSA-...`) explaining why specific code exists. Treated as historical documentation (same reasoning as the CHANGELOG/Credits) — left unchanged rather than rewriting to a link that wouldn't actually point at the real historical issue.

## 11. `services/local-storage/PKGBUILD` — flagged, needs a human decision

Confirmed content: hardcoded `url="https://github.com/IceWhaleTech/CasaOS-LocalStorage"`, with `source_*`/`sha256sums_*` arrays built from `pkgname=casaos-local-storage`. This is **not a simple URL swap**:
- The actual current goreleaser artifact name is `nivaroos-local-storage` (from `project_name:` in `.goreleaser.yaml`), not `casaos-local-storage` — so `pkgname` itself would need to change, which cascades into `pkgdesc`, the `backup=()` config path, and the `package()` function's binary/service names.
- The `sha256sums_*` arrays are real checksums of specific already-published release artifacts. I have no way to compute correct checksums for artifacts that don't yet exist under any new naming/URL — this can only be done by the owner after cutting a real release.
- Repointing the URL alone, without fixing `pkgname` and checksums to match, would produce a PKGBUILD that's broken in a *new* way (fetches from the right repo but wrong filenames with wrong hashes) rather than fixed.

**Left entirely unchanged, flagged for the owner** — this needs a human to decide the new package name and regenerate checksums against a real release.

## 12. UI-facing IceWhaleTech links — repointed where safe, left alone where functional/live

- **`ContactBar.vue`** ("Visit our Github" icon link) → repointed to `https://github.com/F-e-n-y-x/NivaroOS`.
- **`UpdateCompleteModal.vue`** (`githubUrl` used as the post-update "share this" link) → repointed to `https://github.com/F-e-n-y-x/NivaroOS`.
- **`FeedbackPanel.vue`** (both the "more feedback options" link and the `submitIssue()` new-issue URL) → repointed to `https://github.com/F-e-n-y-x/NivaroOS/issues/new...`, per the explicit dispatch guidance for feedback/issue-reporting links.
- **`ShareModal.vue`**: `githubUrl` here actually holds a real *image* URL (`raw.githubusercontent.com/IceWhaleTech/logo/.../casaos_social_share.png`), used both as the share-preview image and (oddly, a pre-existing quirk in the original code, not something I introduced) as the shared link URL. No NivaroOS-hosted share-preview image exists. **Left unchanged, flagged** — repointing to a nonexistent path would just break the preview image.
- **`ImportPanel.vue`** (dynamically builds `https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/${serviceName}/icon.png` for an exported app's icon) and **`LegacyAppEditPanel.vue`** (`POPULAR_STORE_ICONS`, ~20 hardcoded real app icon URLs from the same CDN) — both are live, real, currently-working icon lookups against IceWhaleTech's actual hosted AppStore CDN, with no NivaroOS-hosted app-icon catalog to substitute. **Left unchanged, flagged** per the explicit "don't blindly repoint a real runtime data source with no equivalent" guidance.
- **`AppStoreSourceManagement.vue`** (`if (pathnameList[1] === "IceWhaleTech") return false` inside the app-store source list filter) — this hides any *currently registered* app-store source whose URL path starts with `IceWhaleTech` from the manageable-sources UI. I could not determine with confidence whether this exists to hide a still-genuinely-active default/built-in source (so it doesn't appear as removable/duplicate) or is dead legacy logic — the backend's default `AppStoreList` config is empty (`[]string{}`), so this only matters if a source was registered via a legacy migration or manually by a user. **Left unchanged, flagged as genuinely ambiguous** rather than guessed at.
- **`ComposeConfig.vue`** — a single doc comment citing a historical issue number (same treatment as `gateway_route.go`, item 10) — left unchanged.

## 13. `ui/mock/meta_data.js`

One mock icon URL, updated freely (pure dev-server mock data, not asserted by any test) to `F-e-n-y-x/NivaroOS-AppStore@main`.

## 14. `ui/` module — README and workflows

- `ui/README.md`: removed 5 SonarCloud badges (no NivaroOS-registered project/token; unlike Go Reference/Report Card these require real account registration, so no clean equivalent).
- `ui/.github/workflows/ci.yml`: a real (if currently dormant) unit-test CI workflow, gated by `if: github.repository == 'IceWhaleTech/CasaOS-UI'` — this guard made it never run at all on this repo. Fixed the guard to `F-e-n-y-x/NivaroOS` so the CI can actually execute.
- `ui/.github/workflows/push_test_server.yml`: deleted (see item 2 above — IceWhaleTech's own private deployment target).
- `ui/.github/workflows/node-prerelease.js.yml`: this is the UI's real prerelease/release pipeline (pnpm build → tar → GitHub release via two custom third-party actions by an individual contributor, `zhanghengxin/...@ice` — not literally "IceWhaleTech" so out of the strict grep scope, left as-is since I can't verify or safely rebuild those private actions' behavior). Only the literal `IceWhaleTech`-referencing OSS-upload path was repointed, matching the treatment given to the Go services' release workflows.

## 15. `ui/vendor/api-clients/` — checked, not deleted (partially dead, not wholly dead)

Grepped `ui/src` and `ui/package.json`: `recasa-appmanagement-openapi` **is** imported (`ui/src/service/index.js`), so it's a live, functional dependency — the whole-directory-deletion precondition ("confirm nothing imports either package") does not hold. `recasa-core-openapi` is declared in `package.json` but genuinely unused (no import anywhere in `ui/src`) — confirmed, but since its sibling package is live, I did not delete either package or the directory (per the dispatch's explicit "IF you confirm... nothing imports them" condition for the whole-directory deletion). Instead, per the fallback instruction, fixed both packages' internal `package.json` (`homepage`, `description`, `keywords`) and `README.md` (badges, banner, links) the same way as the corresponding service READMEs — `recasa-core-openapi/README.md` is a byte-for-byte copy of the old `services/core/README.md`, so it got the identical banner/badge treatment (including leaving the historical Credits table alone, and removing a `buymeacoffee.com/icewhaletech` donate badge that had no NivaroOS equivalent).

## Verification performed

- `go build ./...` — clean in all 8 touched Go modules: `services/core`, `services/gateway`, `services/app-management`, `services/local-storage`, `services/message-bus`, `services/user`, `services/common`, `cli`.
- `go test ./...` — all pass in every module, including the full `services/app-management` suite (which does real network fetches to the IceWhaleTech test repos I deliberately left alone — confirmed still green) and the specific `TestWorkDir`/`TestStoreRoot` tests whose hash/path I changed.
- `cd ui && pnpm run build` — succeeds (one pre-existing unrelated warning about `@novnc/novnc`'s top-level-await, not something I touched).
- `cd ui && pnpm vitest run` — 58/58 tests pass across 10 files.
- Final `grep -rln "IceWhaleTech" ...` (same filter as the dispatch) returns exactly the files I deliberately decided to leave and documented above: the top-level `README.md` (intentional fork-credit exception), historical Credits tables (x2), 4 real-network-fetch/functional test/migration files, 2 historical doc-comment files, `PKGBUILD` (flagged), and the CHANGELOG.
- `grep -rl "Copyright (c) .* by IceWhale" ...` — empty.

## Summary of what's deliberately left, and why

| File(s) | Reason |
|---|---|
| `README.md` (top-level) | Explicit owner instruction: keep fork/credit attribution |
| `services/core/README.md`, `ui/vendor/.../recasa-core-openapi/README.md` (Credits table only) | Historical contributor attribution, real commit-history links |
| `services/core/CHANGELOG.md` | Genuine historical changelog, no severable header exists |
| `services/gateway/route/gateway_route.go`, `ui/src/components/Apps/ComposeConfig.vue` | Historical bug/security-advisory doc comments |
| `services/app-management/pkg/utils/downloadHelper/getter_test.go` | Real network fetch; NivaroOS repo zip is 270x larger, degrades test |
| `services/app-management/service/{appstore_test.go,appstore_management_test.go}` (2 of the URLs) | Real network fetch + content assertions against a real IceWhaleTech test fixture repo; no NivaroOS equivalent exists |
| `services/app-management/cmd/migration-tool/migration_044_and_older.go` | Production logic matching literal historical values on real user disks |
| `ui/src/components/share/ShareModal.vue` | Real external image asset, no NivaroOS-hosted equivalent |
| `ui/src/components/forms/ImportPanel.vue`, `ui/src/components/Apps/LegacyAppEditPanel.vue` | Live app-icon CDN lookups against IceWhaleTech's real AppStore, no equivalent catalog |
| `ui/src/components/Apps/AppStoreSourceManagement.vue` | Ambiguous — may hide a still-active real app-store source; flagging rather than guessing |
| `services/local-storage/PKGBUILD` | **Needs a human decision** — not a simple URL swap; requires new `pkgname`, cascading field changes, and real checksums of an unreleased build |

No other lingering "IceWhaleTech" references remain.
