# NivaroOS mobile app: design system (v2, "calm native")

Status: agreed with the owner 2026-09-25. Replaces the old theme
(`lib/theme.dart`, a "Dribbble dark theme with neon badges and glowing
accents") in every screen. Companion to `2026-09-24-mobile-app-plan.md`.

The goal: the app should feel like a well-made Android app from Google or a
serious open-source project - quiet, fast, obvious - not like a concept shot.
Information first; colour only where it means something.

Skills to follow while building (read the files, they're on disk):
- Material 3: `/root/.claude/plugins/cache/material-3-skill/material-3/1.1.1/SKILL.md`
  and its `references/` (color-system, component-catalog, layout-and-responsive,
  navigation-patterns, theming-and-dynamic-color, typography-and-shape).
- Official Flutter skills: `/root/.claude/plugins/cache/dart-flutter/*/*/skills/`
  (flutter-apply-architecture-best-practices, flutter-build-responsive-layout,
  flutter-fix-layout-issues, flutter-add-widget-test, flutter-use-http-package,
  dart-run-static-analysis, dart-resolve-package-conflicts).
- UI/UX checks: `ui-ux-pro-max` `references/pro-rules.md` pre-delivery checklist
  (`/root/.claude/plugins/synced/*/ui-ux-pro-max/.claude/skills/ui-ux-pro-max/`).

## 1. Principles

1. **Native Material 3.** Standard M3 components (NavigationBar, top app bars,
   ListTile, Card.filled/outlined, FilledButton/OutlinedButton/TextButton,
   SegmentedButton, SearchBar, BottomSheet, SnackBar, Dialog) with their default
   behaviour. Custom widgets only where M3 has nothing (e.g. a usage bar).
2. **System light and dark.** `ThemeMode.system` by default, with a
   System / Light / Dark setting. Both themes are first-class and checked.
3. **One accent.** Seed `#2563EB` (NivaroOS blue, same as the web UI).
   Status colours (success/warning/error/info) appear only for status.
4. **List-first.** Most screens are lists of rows with a leading icon, title,
   supporting text and a trailing value or chevron - scannable, dense enough,
   48dp+ targets.
5. **Honest states.** Every data view has loading (skeleton, not a spinner in
   the middle of nothing), empty (what it means + the one action), error (what
   happened in plain words + Retry), and stale/offline states.
6. **Plain wording.** Sentence case, no jargon, no exclamation marks, no
   marketing copy. Say what happened and what to do.

## 1a. Quality bar: top design, not just "clean"

The owner wants the best design this app can have. Calm is the style, not
an excuse for bland. Benchmarks to study and match in craft (screens,
motion, states): Google Files, Google Home, Google Photos, Pixel Settings,
Tailscale, Home Assistant (2025+ UI), Immich, Synology Photos. For every
screen, before it's done:

- **Hierarchy you can read in one second**: one clear primary thing per
  screen (the server's health on Home, the current folder on Files); the rest
  recedes. Use size, weight and position, not colour, to rank.
- **Typography does the work**: consistent roles, tabular figures for
  changing numbers (`FontFeature.tabularFigures()`), units smaller than
  values, no truncated labels at 360dp or 200% text scale.
- **Rhythm**: 8dp grid everywhere, aligned left edges, consistent row
  heights, icons optically centred, nothing within 16dp of the screen edge
  except full-bleed lists.
- **Details that signal craft**: skeletons that match the real layout, smooth
  number changes (short `AnimatedSwitcher`/implicit animations, no bouncing),
  haptic feedback on long-press and destructive confirms, meaningful empty
  states with a real next action, relative times ("2 min ago") with the
  exact time on long-press, thumbnails for media, file-type icons that are
  consistent.
- **Speed you can feel**: cached last data shown instantly on open, then
  refreshed; optimistic UI for toggles with rollback on failure; no screen
  shows a bare spinner for more than 300ms.
- **Review passes**: each screen gets (1) the pro-rules checklist, (2) an MD3
  audit with the material-3 skill, (3) a critique pass (impeccable or
  design-critique skill) on its light and dark screenshots, and (4) an
  independent second opinion from the `agy` CLI (Gemini) on the same
  screenshots. Fix what they agree on; record decisions where they disagree.

## 2. Banned (these are what made it look "AI-made")

- Gradients, glows, neon, `BoxShadow` glows, blurred/glass surfaces,
  `BackdropFilter`, "obsidian" surfaces, animated shimmer borders.
- More than one decorative accent colour on a screen; rainbow icon tiles
  (each list icon in its own coloured blob).
- Emoji as icons. Mixed icon styles at one level (use Material Symbols
  *outlined* everywhere; *filled* only for the selected nav destination).
- Hand-picked hex colours in screens. Every colour comes from
  `Theme.of(context).colorScheme` or the `StatusColors` extension.
- Custom font sizes. Use `Theme.of(context).textTheme` roles only
  (displaySmall … labelSmall); weight tweaks only via the role.
- Giant rounded "hero" cards with big numbers everywhere; stats go in rows.
- Fake data or placeholder numbers when real data is missing (show "—" and
  say why).

## 3. Tokens

Implement in `lib/ui/theme/` (the old `lib/theme.dart` becomes a thin
re-export while screens migrate, then is removed):

- `app_theme.dart`: `AppTheme.light()` / `AppTheme.dark()` built from
  `ColorScheme.fromSeed(seedColor: Color(0xFF2563EB), brightness: …)`,
  `useMaterial3: true`, default M3 `TextTheme` (Roboto), component themes only
  where the default needs a nudge (e.g. `ListTileTheme` content padding,
  `CardTheme` margin zero, `SnackBarTheme` floating). No per-component colours
  beyond the scheme.
- `status_colors.dart`: `ThemeExtension<StatusColors>` with success, warning,
  info (error comes from the scheme) and their `on*`/container variants for
  light and dark, all ≥4.5:1 for text on their container and ≥3:1 for icons.
- `spacing.dart`: `Space.xs 4, sm 8, md 12, lg 16, xl 24, xxl 32`; screen
  gutter 16 (24 on ≥600dp).
- Shapes: M3 defaults (extra-small 4, small 8, medium 12, large 16,
  extra-large 28). Cards 12, sheets 28 top, dialogs 28, chips 8.
- Motion: M3 durations (short 150ms, medium 300ms) and standard easing; honour
  `MediaQuery.disableAnimations`.

## 4. Shared widgets (`lib/ui/widgets/`)

Build once, use everywhere; each gets a widget test and a screenshot.
- `SectionHeader(title, action?)` - titleSmall in primary, 16dp gutter.
- `StatusChip(label, status)` - small, tonal; icon + text (never colour only).
- `UsageBar(value, max, label)` - linear, semantic colour by threshold.
- `MetricRow(icon, label, value, supporting?)` - a ListTile variant for stats.
- `EmptyState(icon, title, message, action?)`,
  `ErrorState(title, message, onRetry)`, `LoadingList(rows)` (skeleton rows).
- `ConfirmDialog.destructive(...)` - names the thing, says it's permanent,
  destructive button in error colour, Cancel focused by default.
- `AppScaffold` - top app bar (small or medium), pull-to-refresh, safe areas,
  optional FAB, handles the offline banner.

## 5. Navigation and layout

- NavigationBar with at most 5 destinations (Home, Files, Apps, VMs, More);
  NavigationRail on ≥600dp width, same destinations.
- Back behaviour with `PopScope` (predictive back on Android 14+); edge-to-edge
  with correct insets (Android 15 enforces it).
- Push screens for detail; bottom sheets for short choices/actions; full-screen
  dialogs for forms longer than 3 fields; no nested bottom sheets.
- Landscape and tablets: content max width 840dp for reading views, two panes
  for list-detail where it helps (Files, Apps).

## 6. Accessibility (must pass)

- Text ≥4.5:1, icons/borders ≥3:1, in both themes.
- Touch targets ≥48×48dp; spacing between targets ≥8dp.
- Every icon button has a tooltip/semantics label; decorative icons excluded.
- Works at 200% text scale without clipping or overflow (screenshot-checked).
- TalkBack order = visual order; state (selected, expanded, busy) announced.

## 7. How screens are checked

`test/screenshots/` renders every screen against a fake server in light and
dark at 412×915 and 360×740, plus one 200%-text-scale shot, and compares with
goldens. A screen isn't done until its screenshots were reviewed against this
document and the pro-rules checklist, `flutter analyze` is clean, and its
widget tests pass.

## 8. Adopting the foundation in a screen

The foundation is in place (2026-09-25): tokens and themes in
`mobile/lib/ui/theme/`, shared widgets in `mobile/lib/ui/widgets/`, the
five-tab shell in `lib/screens/home_shell.dart`, and the `More` tab
(`lib/screens/more_screen.dart`) as the first screen built on it. Use that
file as the worked example.

### Imports

```dart
import '../ui/ui.dart'; // themes, tokens, every shared widget
```

A migrated screen does not import `../theme.dart` or `../widgets/common.dart`.
When the last screen stops importing them, delete both.

### Which widget for which job

| Job | Use |
|---|---|
| The frame of a screen | `AppScaffold(title:, body:)`; `AppScaffold.slivers(title:, slivers:)` for top-level tabs and long pages (medium app bar that collapses) |
| Tabs or search under the title | `bottom:` (a `TabBar`, or a `PreferredSize` search field) |
| Selection mode | `appBar:` - a contextual `AppBar` ("3 selected") that replaces the top bar while set; null restores it |
| FAB placement | `floatingActionButton:` + `floatingActionButtonLocation:` |
| Pull to refresh | `onRefresh:` on `AppScaffold` (the body must scroll; the state widgets do) |
| Offline / stale data | `banner: OfflineBanner(lastUpdated:, onRetry:)` over the cached data (stays pinned while scrolling) |
| First load | Box body: `LoadingList(rows:, leading: SkeletonLeading.icon/avatar/thumbnail/none, subtitle:, trailing:)`. In `slivers:`: `SliverLoadingList(...)`. Or `SkeletonBox`es in a `SkeletonPulse` shaped like the real layout |
| Nothing to show | `EmptyState(icon:, title:, message:, actionLabel:, onAction:)`; add `sliver: true` inside `slivers:` |
| Load failed | `ErrorState(title: "Couldn't load apps", message:, onRetry:, details:)` - the title is required and names what failed; `ErrorState.offline(onRetry:)` for "can't reach the server" with nothing cached. Both take `sliver: true` |
| Group label | `SectionHeader(title:, actionLabel:, onAction:)` |
| Settings-style rows | `TileGroup(title:, children: [ListTile(...)], footer:)` - rows are separate rounded segments 2dp apart, no dividers |
| A state (running, stopped, failed) | `StatusChip(label:, status: Status.x)` - never colour alone |
| How full (disk, memory) | `UsageBar(value:, max:, label:, detail:)` |
| One stat in a list | `MetricRow(icon:, label:, value:, unit:, supporting:)` |
| A timestamp | `RelativeTime(time, prefix:)` - exact time on long-press |
| Delete / wipe / anything permanent | `await ConfirmDialog.destructive(context, title: 'Delete “x”?', message:, confirmLabel:)` (curly quotes around names) |
| Reversible but disruptive | `ConfirmDialog.destructive(..., permanent: false)`; plain confirmation: `ConfirmDialog.confirm` |
| Theme setting | `ThemeModeTile()` |

Everything else is a stock M3 widget (ListTile, Card.filled, FilledButton,
SegmentedButton, SearchBar, showModalBottomSheet, SnackBar). Colours:
`Theme.of(context).colorScheme` and `StatusColors.of(context)` /
`StatusColors.toneOf(context, Status.x)`. Type: `Theme.of(context).textTheme`
roles; add `.tabular` (from `app_theme.dart`) to numbers that change. Spacing:
`Space.xs … Space.xxl`, `Space.gutter(context)` for the screen edge (inside
an `AppScaffold` it is measured from the pane, so it is right next to a
NavigationRail too; ListTiles get the same padding automatically). Motion:
`Motion.of(context).short / .medium` (zero when the user turned animations
off) with `Motion.standard` easing; `Motion.pulse` for looping effects.
Times: read "now" with `clock.now()` (package:clock), never
`DateTime.now()`, for anything shown relative to the current time, so the
screenshots can freeze it. Icons: `Icons.*_outlined` (Material Icons
outlined; the filled variant only for a selected destination) - the app
does not ship the Material Symbols font, and the outlined Material Icons
match it closely enough that adding it isn't worth the size. A few glyphs
have no outlined/filled pair (`Icons.more_horiz`, `Icons.refresh`,
`Icons.chevron_right`); use them as they are. One meaning, one glyph: the
VMs tab is `computer`, the host desktop is `screen_share`, Tailscale is
`vpn_key`, server updates are `update`.

Layout rules:
- Don't mix TileGroups and flat full-width lists on one screen: a TileGroup
  header sits 16dp further in (on its rows' icons) than a flat
  SectionHeader, and two left edges on one screen look like a mistake. (The
  component gallery mixes them only because it is a catalogue.)
- Account-style rows with a 40dp avatar go in a TileGroup of their own, so
  their text edge (88dp) never sits next to 24dp-icon rows (72dp).

### Rules the foundation already enforces

- Themes: `AppTheme.light()` / `AppTheme.dark()`, both `ColorScheme.fromSeed`
  of `#2563EB` with the M3 default (tonal spot) variant, so `primary` is a
  calmer tone of the NivaroOS blue (`#4B5C92` light, `#B4C5FF` dark), not
  the raw hex. `DynamicSchemeVariant.fidelity` keeps the raw blue but makes
  the navigation indicator and tonal buttons loud; revisit only with the
  owner. `MaterialApp` uses `theme` + `darkTheme` + `themeMode` from
  `ThemeController` (System by default, saved in secure storage, kept on
  sign-out).
- `test/ui/theme_contrast_test.dart` checks every text pair ≥4.5:1 and every
  icon/bar/border pair ≥3:1 in both themes. Add a pair there when a screen
  puts a colour on a new background.
- The shell handles back (Files steps out of selection, search, folders and
  extra tabs first, then back goes to Home). On Home it lets the pop through
  so the system closes the app with Android's predictive back-to-home
  animation. That is safe because the shell is always the only route: every
  way in (launch, login, switching server) replaces the whole stack with
  `pushAndRemoveUntil(..., (_) => false)` - keep it that way. Predictive back is on (`enableOnBackInvokedCallback`; Flutter's
  default Android page transition is the predictive one). The app draws edge
  to edge; `AppScaffold` and `ListView` take the insets - don't add your own
  `SafeArea` around a whole screen.
- NavigationBar below 600dp, NavigationRail from 600dp. Tabs are kept alive
  in an `IndexedStack`.
- `NivaroApp` sets the status/navigation bar icon brightness from the app's
  theme on every screen (`AppTheme.systemBarsStyle`), not only under an app
  bar. A screen with a fixed dark surface (the terminal) sets
  `systemOverlayStyle: AppTheme.systemBarsStyle(Brightness.dark)` on its bar.
- `TileGroup` segments are `surfaceContainer` on the `surface` page;
  `theme_contrast_test.dart` pins that they stay ≥1.1:1 apart.

### The compatibility layer (until every screen is migrated)

`lib/theme.dart` is deprecated. Its `NivaroColors.*` names are now getters
that read the active theme (via `LegacyThemeBridge` in `MaterialApp.builder`,
which rebuilds the tree once when the brightness flips), so un-migrated
screens follow light and dark. Old name → new role:

| Old names | Now |
|---|---|
| `primary`, `primaryLight`, `primaryGlow`, `primaryDark` | `colorScheme.primary` |
| `primaryContainer` | `colorScheme.primaryContainer` |
| `success*`, `warning*`, `info*` | `StatusColors` `.color` (the `*Container` names: `.container`) |
| `danger`, `dangerLight` / `dangerContainer` | `colorScheme.error` / `errorContainer` |
| `purple*` / `cyan*`, `accent*` | `colorScheme.tertiary` / `secondary` (decorative - remove when migrating) |
| `background`, `surfaceDim` | `colorScheme.surface` |
| `surface`, `surfaceRaised`, `surfaceMuted` | `surfaceContainerLow`, `surfaceContainer`, `surfaceContainerLowest` |
| `surfaceContainer*` | the scheme role of the same name |
| `border`, `borderSubtle` / `borderHighlight` | `outlineVariant` / `outline` |
| `textPrimary` / `textSecondary`, `textMuted` / `textFaint` | `onSurface` / `onSurfaceVariant` / `outline` |
| `folderAccent`, `fileNeutral` (`folderColor`, `fileColor`) | `primary`, `onSurfaceVariant` |
| `nivaro*Style` text styles | `textTheme` roles |
| `NivaroShape.*` | `Corners.*` (M3 defaults, usually nothing) |

Also deprecated: everything in `lib/widgets/common.dart`
(`LegacySectionHeader` - renamed from `SectionHeader` so the v2 one can have
the name - `DarkCard`, `StatusPill`, `MonitorCard`, `DriveCard`, …).
Hard-coded `Colors.white` / `Color(0x…)` in un-migrated screens do not follow
the theme. White text and icons that sit on theme surfaces (titles, sheet
headings, names, error text) were switched to `NivaroColors.textPrimary` /
`textSecondary` so light mode is readable; white on filled buttons, on
gradients and on the always-dark console, terminal and media overlays is
left as it is, because it is correct there.

**Release gate.** Light mode is the default for anyone whose phone is light
(the theme defaults to System), so no release build goes out until Files,
Apps, VMs and Companion devices are migrated and their light screenshots
reviewed. Until then, builds are for testing only.

### Migrating a screen: checklist

1. Replace `../theme.dart` / `../widgets/common.dart` with `../ui/ui.dart`;
   fix every compile error with the tables above - no hex colours, no font
   sizes.
2. Give every data view its four states: `LoadingList` (or a matching
   skeleton), `EmptyState`, `ErrorState`, and `OfflineBanner` over cached
   data.
3. Run the screen's screenshots (below) and review light, dark, 360dp and
   200% text against §1a, §2 and §6 before calling it done.

### Adding or updating a screenshot test

Every screen is in the `screens` map in
`mobile/test/screenshots/screens_test.dart`; shared widgets are in
`gallery_test.dart`. Each entry renders light and dark at 412×915 against
the fake server (`FakeServer` in `harness.dart`), which answers from
`test/screenshots/fixtures/<v1|v2|vm>/<path>.json` (see the README there).

- A screen that calls a new endpoint: add a fixture file at its path (GET it
  read-only from a box with `curl`, then scrub names, keys and public IPs), or
  pass `overrides: {'GET /v1/…': {...}}` to `shoot()` for a one-off state
  (empty list, `FakeResponse(..., status: 500)` for errors).
- Each entry is a `ScreenShots(builder, migrated:, tab:, extra:)`. After
  migrating a screen set `migrated: true` - its shots then run strict - and
  give it its extra shots: `extra: {Extra.smallPhone, Extra.text2x}` for
  any screen with dense rows, plus `Extra.tablet` for list-detail layouts.
  The `more` entry is the pattern to copy. Old screens run non-strict:
  overflows and uncaught async errors are printed as
  `[screenshots] <name>: …` findings instead of failing.
- `tab: true` for HomeShell's tabs, which draw on the shell's Scaffold; the
  harness gives them one so they render on the page colour.
- Every shot runs at a frozen time (`shotTime`, via `withClock`), and this
  phone's storage reading is fixed, so goldens compare pixel for pixel with
  no tolerance. A shot that differs between two runs is a bug in the screen
  or the harness: find what reads the real clock or machine and put it
  behind a seam.
- Write PNGs with `flutter test test/screenshots --update-goldens`, look at
  every changed PNG, and commit them with the change.

Known findings from the baseline shots (for the screen stages): App store
cards overflow by 1px at 412dp; Home overflows in three places at 200% text;
the VM console and Host desktop leave the WebSocket connection error
unhandled when the server can't be reached. Also from the legacy shots:
Home's Quick Access grid repeats the nav tabs (Files, VMs, Apps) and the
More rows (Terminal, Updates, Logs, Host desktop, Tailscale) - drop it when
Home is migrated, along with the Title Case "System Health & Metrics"
header and its subtitle; the legacy cards still draw the banned hard bottom
shadows (Home, Settings, VMs); the VMs' Start buttons are solid green, a
status colour used as decoration; Dashboard's hero `MonitorCard`s with
custom font sizes should become `TileGroup` + `MetricRow` / `UsageBar`
rows.

## 9. Decisions

Recorded after the stage-1 foundation review (2026-09-25: a Claude
critique and a second opinion from `agy`/Gemini).

- **Brand blue for actions, calm tonal surfaces (owner, 2026-09-25).** The
  scheme is tonal spot, but `primary`/`onPrimary` come from the fidelity
  variant, so buttons, links, switches and progress show the exact NivaroOS
  blue (#2563EB, as on the web UI) while containers, the navigation
  indicator and surfaces stay quiet.
- **Account row keeps its avatar.** It sits alone in its group (see Layout
  rules), the unlabelled swap icon is gone, and the subtitle says
  "<host> · Switch server". With no saved username it shows a person icon
  and "Unknown user", not an invented initial.
- **Security updates are a warning, not an error**: an amber chip with a
  shield, under the summary line rather than in the trailing slot, where
  at 200% text it squeezed the summary into a column. The Updates row is
  therefore three-line, with its icon at the top, as M3 lays those out.
- **Sign out stays in Settings** (subtitle "…, sign out") until the
  Settings screen is migrated; it then gets its own row.
- **Box-form `AppScaffold` on tablets:** content is top-aligned in the
  840dp column, but drags in the side margins don't scroll a box body -
  the body is any widget, so the cap has to be a box around it. The sliver
  form applies the cap as padding inside the scroll view and scrolls from
  the margins; use it for long lists on tablets.
- **Titles on tablets align with the content**, not the window edge, when
  the bar has no back button.
- **Usage meters use the M3 2024 linear indicator** (gap and stop dot) in
  every place, including `UsageBar`, so the app has one bar style. At very
  low values (1%) the fill is a short stub before the gap; that is the M3
  spec, not a bug.
- **Skeleton colour**: `surfaceDim` in light (the highest container tone is
  too close to the page there), `surfaceContainerHighest` in dark, pulsing
  between 60% and 100%.
- **Native splash follows the phone, not the in-app theme**: Android shows
  it before Flutter starts, so with an in-app Dark choice on a light phone
  the first frame can still be light. Accepted; not worth a native theme
  switch.
- **Light-mode legacy screens**: fixed only where white text sat on theme
  surfaces (see the compatibility section); anything more is migration
  work, and the release gate stands.
- **agy findings not taken:** `RadioGroup` is the current Flutter 3.47 API
  (the per-tile `groupValue` is deprecated), and the gallery already uses
  `Icons.apps_outlined`. Its Dashboard and Apps findings are the next
  stages' migration work.
