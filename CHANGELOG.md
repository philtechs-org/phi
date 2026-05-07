# Changelog

All notable changes to phi are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
