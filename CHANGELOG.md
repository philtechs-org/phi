# Changelog

All notable changes to phi are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
