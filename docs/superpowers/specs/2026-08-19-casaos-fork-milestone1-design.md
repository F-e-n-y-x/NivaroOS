# CasaOS Fork — Milestone 1: Working Fork, Matching Current Install

## Status: DONE (2026-08-19)

All 8 repos forked, cloned, and built. All 7 Go binaries plus the UI
swapped into the live install; all 6 systemd services active with no
persistent errors; dashboard reachable (HTTP 200) with `custom.css`
override preserved. Pre-swap backup kept at
`/root/casaos-fork/backups/pre-fork-swap/`.

One real-world casualty of CasaOS being discontinued, found during the
UI build: `@icewhale/icewhale-files-openapi` has been fully removed from
npm (404, no CDN mirrors, no matching GitHub repo). Confirmed it was
unused dead code (zero references anywhere in `CasaOS-UI/src`) and
removed it from `package.json`/the lockfile — build succeeded after
that. This is a concrete preview of the kind of breakage "maintaining
your own fork" is meant to get ahead of.

Still needs the user to confirm in an actual browser that login, the
app list, and the file browser all look and behave as before — that's
the one check that can't be done from the CLI.

## Context

CasaOS (upstream: IceWhaleTech) is discontinued. The user runs a native
(non-Docker) CasaOS v0.4.15 install on this PC and wants to maintain their
own fork going forward — building it locally now, and on other PCs later —
so they can add features and reduce bloat without depending on upstream.

This is the first of several planned milestones. Later work (already queued)
includes a GPU monitoring widget, removing the branding/contact bar at the
source level, and a properly-implemented compact/85% view. None of that
should be attempted until this milestone proves the fork builds and runs
correctly.

## Goal

Fork all CasaOS repos under the user's GitHub account (`F-e-n-y-x`), build
them locally, and swap the freshly built artifacts into the live install —
ending with a CasaOS instance built entirely from the user's own fork,
behaviorally identical to the current install.

## Scope — repos and pinned versions

Version tags identified from the original installer's cached tarballs
(`/tmp/casaos-installer/`):

| Repo | Version to pin |
|---|---|
| CasaOS (main) | v0.4.15 |
| CasaOS-CLI | v0.4.4-3-alpha1 |
| CasaOS-AppManagement | v0.4.10-alpha2 |
| CasaOS-LocalStorage | v0.4.4 |
| CasaOS-UserService | v0.4.8 |
| CasaOS-MessageBus | v0.4.4-3-alpha2 |
| CasaOS-Gateway | v0.4.9-alpha4 |
| CasaOS-UI | v0.4.15 — confirmed by matching `pnpm-lock.yaml` resolved versions (`@icewhale/casaos-appmanagement-openapi@0.4.10-alpha2`, `@kangc/v-md-editor@1.7.12`, `@vue-office/docx@1.6.2`, `artplayer@4.6.2`, `gsap@3.12.5`) against the installed bundle's chunk filenames |

Repo structure: one fork per upstream repo (mirrors upstream), not a
consolidated monorepo, to keep pulling upstream updates straightforward.

## Environment

- Toolchain installed on this PC: Go 1.23.4, Node.js 20.20.2, pnpm 9.15.0,
  GitHub CLI 2.97.0.
- GitHub auth: user runs `gh auth login` interactively themselves; this
  workspace repo and the build process assume `gh auth status` succeeds
  before any fork/push step runs.
- Workspace: `/root/casaos-fork/` — this directory is itself a git repo
  used to track specs/plans/scripts for the overall project (not one of
  the 8 forked upstream repos). Forked upstream repos will be cloned as
  siblings, e.g. `/root/casaos-fork/CasaOS/`, `/root/casaos-fork/CasaOS-UI/`,
  etc.

## Process

1. Fork each of the 8 repos under `F-e-n-y-x` via `gh repo fork`.
2. Clone each fork locally into `/root/casaos-fork/<repo>/`, checked out
   at the pinned version tag (as a branch, so changes can be committed).
3. Build each repo in dependency order — backend Go services first
   (CasaOS-MessageBus, CasaOS-Gateway, CasaOS-LocalStorage,
   CasaOS-UserService, CasaOS-AppManagement, CasaOS main, CasaOS-CLI),
   CasaOS-UI last since it depends on the AppManagement OpenAPI spec.
4. Back up current live binaries/config/`www` to a timestamped directory
   under `/root/casaos-fork/backups/` before touching anything running.
5. Stop each systemd service, replace its binary/assets with the freshly
   built ones, restart.
6. Smoke test: confirm all 6 systemd services (`casaos`,
   `casaos-gateway`, `casaos-app-management`, `casaos-local-storage`,
   `casaos-user-service`, `casaos-message-bus`) report
   `active (running)`; hit each service's status/health endpoint; load
   the dashboard in a browser and confirm login, app list, and file
   browser behave as before.

## Validation approach

Direct swap, chosen explicitly by the user over side-by-side validation on
alternate ports. The pre-swap backup (step 4) is the safety net — if
something breaks, reverting is a file copy + service restart, not a
re-install.

## Error handling

- If a build fails, stop and report which repo/step failed rather than
  partially swapping services.
- If a smoke test fails post-swap, revert that specific service from the
  backup immediately, then diagnose before retrying.

## Out of scope for this milestone

- GPU widget, source-level base-bar removal, compact-view rework — queued
  for later milestones, not attempted here.
- Any actual "lightening" (dependency trimming, removing unused features)
  — comes after this milestone proves a clean baseline build.
