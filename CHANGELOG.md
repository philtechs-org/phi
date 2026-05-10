# Changelog

All notable changes to phi are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.1] — 2026-05-10

### Fixed

- **`phi self-update` on Windows: clearer errors + auto-retry through
  Defender's real-time scan window.** Previously a stale `phi.exe.old`
  from a prior failed update would silently block the next update with
  a confusing `rename current binary: Access is denied` — the actual
  cause (a locked sibling file the rename couldn't replace) wasn't
  surfaced, leaving users with nothing actionable. Two changes:

  - Stale `phi.exe.old` is now detected explicitly. If it can't be
    removed, the error message names the file, identifies the typical
    cause (Defender quarantine), and prints the exact remediation
    commands (`Add-MpPreference -ExclusionPath …` and the install
    one-liner fallback).
  - Both `os.Rename` calls in the Windows path now retry for ~2s
    (200/400/600/800ms backoff) on access-denied errors, which absorbs
    Defender's brief real-time-scan locks on freshly-replaced binaries
    without the user having to re-run anything.

  When retry exhausts, the rename error includes the same actionable
  guidance as the stale-`.old` case.

## [0.3.0] — 2026-05-10

### Added

- **`phi x` (npx-equivalent): scan-and-run for missing binaries.** The
  existing `phi exec` / `phi x` aliases now mirror `npx`: if the requested
  bin isn't already in `node_modules/.bin`, phi resolves the providing
  package, runs the full scanner + advisory pipeline over the transitive
  tree, and stages the result under `$UserCacheDir/phi/run/<name>@<ver>/`
  before executing — without polluting the caller's `node_modules`. A
  `.phi-scan-passed` marker means subsequent invocations of the same
  resolved version skip the scan and run instantly.

  Behavior matches `npx` where it makes sense (`phi x prettier`,
  `phi x cowsay@1.5.0`, `phi x -p typescript tsc`, `--` separator,
  `-y` / `--yes` for non-interactive review-approval) and stays
  phi-stricter where the threat model demands it (lifecycle scripts
  remain off; blocked verdicts still abort unless `--force`). New flags
  on `phi exec` / `phi x`:

  - `-p, --package <pkg>` — package name when bin name differs (npx
    parity for `tsc` / `typescript`).
  - `--no-install` — strict-local; preserves the previous behavior.
  - `-y, --yes` — auto-approve review verdicts (CI / scaffold mode).
  - `--rescan` — invalidate the cached scan, re-fetch and re-scan.
  - `-f, --force` — proceed past blocked verdicts (loud warning).

### Changed

- **Animated spinner during the resolver phase**, with the first frame
  drawn immediately so there's no perceptible gap between the banner
  and the indicator. Replaces the static "resolving dependency tree…"
  line in `install`, `update`, `audit`, `audit fix`, and the new staged
  `phi x` flow. Same cross-shell-uniform technique already used for the
  scan progress bar (ASCII-only frames + carriage return + fixed-width
  pad — renders identically on cmd.exe, PowerShell, Windows Terminal,
  git-bash, and Linux/macOS ttys).

## [0.2.4] — 2026-05-09

### Fixed

- **Atomic writes for user-mutable files.** `package.json` (after
  `phi install <pkg>`, `phi remove`, `phi audit fix`), `phi.lock`, and
  `phi-report.json` are now written via a write-temp-then-rename
  pattern. Either the new content fully replaces the old or the old is
  preserved — no half-written intermediates if the process is killed
  or the machine loses power mid-write. Same pattern Postgres / git use
  for their on-disk state. Previously a Ctrl-C between truncate and
  write could leave the user's `package.json` empty.

- **Panic recovery in scan + prefetch goroutines.** A panic anywhere
  in the analyzer (corrupt tarball blowing up gzip/tar, goja crashing
  on a particular JS edge case, an OOB in any detector) used to tear
  down the whole phi process — taking the other 80 packages mid-scan
  with it. Each scan goroutine now has a `defer recover()` that
  converts the panic into a clean per-package error; the rest of the
  tree still finishes. Same fix applied to `resolver/packument_cache`'s
  background prefetch goroutines.

- **`phi self-update` preflight permission check.** Verifies the
  install directory is writable BEFORE downloading the new binary.
  Catches the "phi installed system-wide, user isn't root" case with a
  platform-aware suggestion (`sudo phi self-update --yes` on Unix,
  "run from elevated PowerShell" on Windows) instead of the previous
  failure mid-install after several MB of bandwidth.

- **Windows Defender false-positive guidance** — `install.ps1` now
  detects the `Operation did not complete successfully because the file
  contains a virus or potentially unwanted software` error (common
  Defender heuristic on every unsigned Go CLI — same issue affects `gh`,
  `cosign`, `goreleaser`) and prints a clear remediation message with
  the exact `Add-MpPreference` / `Unblock-File` commands the user needs.
  Distinguishes copy-stage blocks (binary never reaches disk; install
  fails) from execute-stage blocks (binary installed fine, just blocked
  from running; install exits clean and the user just unblocks).

  The script verifies sha256 against `checksums.txt` *before* this
  point — if execution reaches the Defender block, the bytes are
  exactly the published release. Defender is wrong about behavior, not
  lying about a swap.

  Companion: new FAQ entry at
  https://phi.philtechs.org/faq.html#windows-defender, install page
  callout, and `RELEASE.md` per-release submission checklist for the
  Microsoft Defender FP review portal.

## [0.2.3] — 2026-05-08

### Added

- **`phi audit fix [--apply | --force]`** — proposes (and optionally
  applies) actionable fixes for direct dependencies. Three sources of
  fixes, in order of confidence:

  1. **Typosquats** → swap to the popular package name. Always safe.
  2. **Advisory bumps** → upgrade to the OSV-documented "fixed in"
     version. Same-major bumps are safe; cross-major are flagged as
     breaking and require `--force`.
  3. **Deprecated packages** → swap to the curated successor (vm2 →
     isolated-vm, request → undici, node-uuid → uuid, etc.). Always
     breaking — public API differs — so `--force` is required.

  Default is preview-only (zero filesystem changes). `--apply` writes
  the safe fixes to `package.json`. `--force` writes everything,
  including breaking changes. The user runs `phi install` afterwards
  to materialize the new versions.

  Different from `npm audit fix` in three ways: phi handles
  typosquats and deprecations (not just CVE bumps); phi shows the
  proposal before applying; phi explicitly distinguishes safe from
  breaking changes.

### Changed

- **Scan progress indicator rewritten.** Replaced the schollz progressbar
  dependency with a small in-house implementation. The previous bar
  wasn't emitting ANSI clear-line codes on every shell we ship to,
  which left residual characters when the bar's width changed between
  updates ("loader duplicates everywhere"). The new indicator uses
  fixed-width formatted lines + carriage return + space-pad — uniform
  rendering on cmd.exe, PowerShell, Windows Terminal, git-bash, macOS
  Terminal, iTerm, and Linux ttys. Throttled to ~80ms between draws,
  so 80 ticks become 5–10 visible updates instead of a flicker stream.
  `github.com/schollz/progressbar/v3` is gone from `go.mod`.

- **OSV advisory parsing extended** — `Advisory.Fixed` is now populated
  from `affected[].ranges[].events[].fixed` in the OSV detail response.
  Used by `phi audit fix` to compute the safe upgrade target. Existing
  consumers (report card, JSON report) are unchanged.

## [0.2.2] — 2026-05-08

### Added

- **Detector: Credential Exfil Flow** (CRITICAL). Fires when the same
  source file BOTH reads a third-party credential (env var name in the
  known-token list, or a sensitive file path like `id_rsa` /
  `.aws/credentials` / `.npmrc`) AND makes an outbound HTTP call. Smart
  matcher with a token-to-canonical-host allowlist: a package reading
  `GITHUB_TOKEN` and posting to `api.github.com` is silent (octokit and
  similar legitimate clients), the same token going to `evil.example.com`
  fires. Combined with the existing Credential Theft detector this
  pushes real exfiltration cases firmly into BLOCKED territory.
- **Detector: Linux System Tampering** (CRITICAL). Conservative regex
  set for PAM (`pam_authenticate`, `libpam.so`), eBPF (`BPF_PROG_LOAD`,
  `bpf_load_program`, `perf_event_open`), kernel module loading
  (`init_module`, `finit_module`, `delete_module`), and `LD_PRELOAD` /
  `/etc/ld.so.preload`. None of these symbols belong in a normal npm
  package — direct response to recent QLNX-style RAT delivery patterns.
- **Deprecation notices in scan reports.** Curated, info-only annotation
  — no score impact, no verdict change. Initial set:
  - `vm2` → `isolated-vm` (12 critical sandbox-escape CVEs through
    CVE-2026-44009; the architecture cannot be fully secured)
  - `request` / `request-promise` → `undici` / `axios` / `got`
  - `node-uuid` → `uuid` (renamed)
  - `tslint` → `eslint` + `@typescript-eslint`
  - `bower` → `npm` / `yarn` / `pnpm`
  - `node-sass` → `sass` (Dart Sass)
  - `babel-eslint` → `@babel/eslint-parser`
  - `left-pad`, `is-array`, `har-validator`, `node-sass` (with stdlib
    or replacement guidance)

  Notices render under the report card as `note deprecated — <message>`
  in cyan, and ship in `phi-report.json` under a per-package `notices`
  array.

### Changed

- Report cards print for any package with detections, advisories, OR
  notices (previously: only detections + advisories). Lets a deprecated
  but otherwise-safe package surface its replacement guidance.

## [0.2.1] — 2026-05-08

### Added

- `phi self-update [--check] [--version v0.X.Y] [--yes]` — replace the
  running phi binary with the latest GitHub release (or a pinned tag).
  The new binary's sha256 is verified against `checksums.txt` from the
  release before installation. On Windows the running binary is renamed
  to `<phi.exe>.old` to bypass the file-lock; the leftover is cleaned
  up automatically on the next phi run. `--check` reports whether an
  update is available without installing it; `--yes` skips the
  confirmation prompt for non-interactive use.

  Distinct from `phi update`, which re-resolves your project's
  dependencies. `phi self-update` updates phi itself.

  Manual fallback (no command needed) still works: re-run the install
  one-liner — `curl -sSL https://phi.philtechs.org/install.sh | sh` on
  Linux/macOS, `iwr -useb https://phi.philtechs.org/install.ps1 | iex`
  on Windows. Both always pull the latest release.

## [0.2.0] — 2026-05-08

### Added

- `phi create <framework> <project-name> [-- pass-through-args…]` — scaffold a
  new project. Five frameworks shipped:
  - `phi create react <name>` — React + Vite + TypeScript via `create-vite`
  - `phi create next <name>` — Next.js (App Router) via `create-next-app`
  - `phi create express <name>` — built-in minimal Express template (no
    network fetch — bundled in the phi binary via `embed.FS`)
  - `phi create fastify <name>` — Fastify HTTP server via `fastify-cli`
  - `phi create nest <name>` — NestJS application via `@nestjs/cli`

  Proxy-mode frameworks (everything except Express) install the canonical
  scaffolder package into a temp directory through phi's normal scan +
  extract pipeline (lifecycle scripts off — same as `phi install`), then
  invoke the scaffolder's binary in the user's current directory. The
  temp directory is cleaned up regardless of outcome.

  Pass-through args after `--` go straight to the scaffolder. User flags
  override phi's defaults — e.g. `phi create react app -- --template
  vanilla-ts` swaps phi's default `react-ts` template.

- `phi install` / `update` now accept `--force` (or `-f`). Overrides
  the BLOCKED verdict and proceeds with installation. The scan still
  runs and `phi-report.json` is still written, so the audit trail is
  intact — the user has just chosen to install regardless. Loud warning
  printed for any blocked package being force-installed. Implies
  auto-approval of REVIEW packages. Use case: a known-trusted package
  that phi has flagged (e.g. discord.js's internal `_eval` method).

### Changed

- `installer.Options` gained two internal fields used by `phi create`:
  - `Quiet bool` — suppresses the banner, progress bar, and per-package
    report cards while still surfacing errors and warnings. Used so the
    scaffolder's UI dominates the user's terminal.
  - `AutoApproveReview bool` — skips the interactive REVIEW prompt.
    BLOCKED packages still abort. Used only by `phi create` for
    ephemeral scaffolder installs (the temp dir is wiped after one run);
    the user's actual project deps are reviewed normally when they later
    run `phi install` in their new project.

  Neither field is exposed as a CLI flag for `phi install` — bypassing
  REVIEW by default would defeat phi's purpose.

### Fixed

- **False positive: `discord.js` flagged for Credential Theft** when it
  read its own `process.env.DISCORD_TOKEN`. Cause: `normalizePkgName`
  didn't replace `.` so "discord.js" stayed "DISCORD.JS", and the
  package-vs-envvar overlap heuristic never matched the "DISCORD"
  token against "DISCORD_TOKEN". Fix: dots are now normalized to
  underscores along with `/` and `-`. (Same bug class would have
  affected `lodash.merge`, `body-parser.json`, etc.)
- **False positive: `undici` flagged for Code Obfuscation** on the
  `Buffer.from('AGFzbQ...', 'base64')` call that loads the embedded
  llhttp WASM parser. Cause: any base64 payload of 40+ chars matched
  the obfuscation pattern, even legitimate WebAssembly. Fix: base64
  payloads starting with `AGFzbQ` (the WASM magic "\0asm" encoded) are
  now skipped — that prefix is unambiguously a WASM module.

## [0.1.2] — 2026-05-08

### Changed

- `phi version` output trimmed to just `phi <version>`, matching the
  npm/yarn/pnpm convention. The previous `(commit <hash>, built <date>)`
  parenthetical is gone; the commit hash for any release is on its
  GitHub Release page if you need it.

## [0.1.1] — 2026-05-08

### Added

- `phi init [--yes] [--force] [--name … --version … --description … --author … --license …]` —
  bootstrap a new project. Creates `package.json` (with sensible defaults from
  the cwd basename), a starter `.gitignore`, and a stub `README.md`.
- `phi do <script> [args…]` (alias `d`) — run a script from `package.json` with
  `node_modules/.bin` prepended to PATH. Pre/post hooks (`pre<name>`, `post<name>`)
  honored. Extra args pass through to the script. phi's distinctive verb;
  reads naturally as "do dev" / "do migrate:js".
- `phi exec <bin> [args…]` (alias `x`) — run a binary from `node_modules/.bin`.
  Like `npm exec` / `pnpm exec`; never auto-installs.
- Direct script shortcuts: `phi dev`, `phi build`, `phi start`, `phi test`,
  `phi lint`, `phi preview`, `phi prod` — each equivalent to `phi do <name>`.
- Single-letter command aliases: `phi i` / `phi a` (install), `phi u` (update),
  `phi rm` (remove), `phi d` (do), `phi x` (exec).

### Fixed

- **Windows installer** (`install.ps1`):
  - Replaced unicode arrows (`→`) with ASCII (`->`) — the previous version
    showed `???` on consoles without UTF-8 rendering.
  - Replaced the unsafe `setx PATH "<full path string>"` suggestion (which
    truncates at 1024 chars and clobbers system PATH) with the idiomatic
    `[Environment]::SetEnvironmentVariable('Path', "$env:Path;<dir>", 'User')`.
- **Go 1.21 CI compatibility**: replaced `t.Chdir` (added in Go 1.24) with a
  small package-level `chdir(t, dir)` helper that does `os.Chdir` + `t.Cleanup`.
  CI now passes on Go 1.21 / 1.22 as advertised.

### Changed

- Install one-liners now use `phi.philtechs.org/install.{sh,ps1}` — short,
  branded, and decoupled from the GitHub repo path. The Vercel-hosted site
  proxies `/install.sh` and `/install.ps1` to the GitHub raw URLs, so the
  scripts are still version-controlled in the repo.
- Documentation site moved to **https://phi.philtechs.org** (Vercel) with
  the new dark-on-bone "scanner / inspection lab" design.

## [0.1.0] — 2026-05-07

First public release.

### Installer

- Independent of npm — phi resolves the full transitive dependency tree itself,
  fetches every tarball, verifies sha512 integrity, and decides what to extract
  based on its own analysis. No `npm install` shell-out anywhere.
- Lifecycle scripts (`preinstall`, `install`, `postinstall`) **never run by
  default**. Opt in per-package with `--allow-scripts <pkg>`.
- Real semver via Masterminds — `^1.2.3`, `~1.2.0`, ranges, `>=2 <3`, dist-tags.
- Hoisting + nested install paths for transitive version conflicts. Sibling
  workspaces in monorepos are linked instead of installed.
- Concurrent packument prefetch — children's packuments warm in the background
  while the parent is processed.
- Tarball cache at `$XDG_CACHE_HOME/phi/tarballs/` (or `%LOCALAPPDATA%\phi\` on
  Windows), keyed by sha512 integrity.
- `phi.lock` (npm-style JSON) generated on every install. When present and it
  covers `package.json`, install reuses it without resolving.
- `phi-report.json` per-install scan report with detector hits, scores, and
  advisory references.

### Detection

- Eleven detectors covering arbitrary code execution, dynamic code compilation,
  obfuscation, credential theft, install-script abuse, crypto mining, wallet
  drain, reverse shell, network exfiltration, and typosquatting.
- AST-validated detection for `eval`, `child_process.exec/spawn`, and
  `new Function` — parses `.js` / `.cjs` / `.mjs` files with goja and only fires
  on real call/new expressions, suppressing string/comment/identifier matches.
- Smart credential-theft matcher — knows the package's own normalized name and
  silently skips `process.env.<OWN_PKG>_*` reads. Only flags well-known
  third-party credentials (AWS, GitHub, npm, Stripe, Twilio, …).
- Smart install-script-abuse matcher — only inspects
  `scripts.{preinstall,install,postinstall}` of `package.json`. Test/build/
  prepublish scripts that happen to use `node -e` or `curl | sh` don't fire.
- OSV-aware — every resolved (name, version) is queried against the
  [OSV vulnerability database](https://osv.dev), aggregating GHSA, OpenSSF
  malicious-packages, and CVE feeds. Hits add to the same risk score and
  appear in `phi-report.json`. Disable with `--no-advisories`.

### Commands

- `phi install [pkg...]`, `phi add`, `phi update`, `phi remove`
- `phi audit` (with `--json` for CI)
- `phi why <pkg>` — print the dep chain that pulled a package in
- `phi outdated` — direct deps with newer versions, color-coded by major bump
- `phi cache stat` / `phi cache clean [--older-than 30d | --all]`
- `phi version` / `phi help`

### Flags

- `--allow-scripts a,b` — explicit lifecycle script allowlist
- `--frozen-lockfile` — CI mode; `phi.lock` must exactly cover `package.json`
- `--no-lockfile` — force a fresh resolve
- `--save-dev` / `-D`, `--save-peer`, `--save-exact` / `-E`
- `--no-advisories` — skip OSV query (offline mode)
- `--json` — machine-readable output for CI integration

### Workspaces & registries

- `workspaces` field in root `package.json` (array or object form). Sibling
  refs link to the source dir as junctions on Windows / symlinks elsewhere.
- `.npmrc` parsing: default registry, scoped registries
  (`@scope:registry=…`), bearer tokens (`//host/path/:_authToken=…`), and
  `${ENV_VAR}` substitution. Project `.npmrc` overrides `$HOME/.npmrc`.

### Distribution

- Pre-built binaries for linux-amd64 / linux-arm64 / darwin-amd64 /
  darwin-arm64 / windows-amd64 published on each tagged release.
- Cross-platform install scripts (`install.sh`, `install.ps1`) that detect
  platform, fetch the right archive, verify sha256, and place the binary.
- Static documentation site at https://phi.philtechs.org.
