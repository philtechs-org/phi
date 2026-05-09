# phi — architecture

How phi works internally. Companion to [`THREAT_MODEL.md`](./THREAT_MODEL.md) — that doc states what phi promises; this one shows the mechanism.

Audience: contributors, security reviewers, anyone evaluating phi's claims by reading the code.

---

## The pipeline

Every `phi install`, `phi update`, and `phi audit` runs through the same five stages, in this order:

```
package.json
     │
     ▼
[1] resolve     ← internal/resolver
     │             BFS over npm registry, dedupe by name, semver-aware
     │             (Masterminds/semver/v3)
     ▼
[2] fetch       ← internal/registry
     │             tarball download + sha512 integrity verification from
     │             packument's dist.integrity field
     ▼
[3] scan        ← internal/analyzer  (in memory — bytes never on disk yet)
     │             13 detectors over .js/.cjs/.mjs/.ts/.json/.sh + AST validation
     │             via goja for code-execution detectors
     ▼
[4] score       ← internal/scorer
     │             severity-weighted sum, capped at 100, verdict assigned:
     │             0–19 safe, 20–59 review, 60+ blocked
     │             (advisory hits from OSV merge in here)
     ▼
[5] extract     ← internal/installer
                   only packages that cleared step 4 reach disk
                   atomic rename for package.json / phi.lock / phi-report.json
```

The architectural moat is the **boundary between [3] and [5]**. Bytes are scanned in process memory before the extractor is allowed to touch them. There is no path through the code where a tarball's contents reach `node_modules/` without first having a verdict computed.

---

## Core invariants

These are the load-bearing properties. If any of them fails, phi has lost its security promise.

### Invariant 1: bytes are scanned before extraction

In [`internal/installer/installer.go`](./internal/installer/installer.go), `scanTree` populates a map of buffered tarball bytes keyed by install path. `splitVerdicts` then partitions packages by verdict. Only after that partition is computed does the extraction loop run, and only over the approved set. There is no goroutine that bypasses this ordering.

### Invariant 2: lifecycle scripts are off by default

The `Options.AllowScripts` field defaults to `nil`. `RunLifecycleScripts` is only called when `len(opts.AllowScripts) > 0`. There is no flag, environment variable, or configuration file that changes this default. Opt-in is per-package, by name, on the command line.

### Invariant 3: user-mutable files are atomically written

Every write to `package.json`, `phi.lock`, or `phi-report.json` goes through `writeFileAtomic` in [`internal/installer/atomic_write.go`](./internal/installer/atomic_write.go): write to sibling temp file → fsync → `os.Rename` onto target. `os.Rename` is atomic at the filesystem level on every OS we ship to (rename(2) on POSIX, MoveFileExW with MOVEFILE_REPLACE_EXISTING on Windows). A `Ctrl-C` or power loss between truncate and write is impossible — there is no truncate.

### Invariant 4: scan goroutines cannot crash the process

Every goroutine in `scanTree` and `packumentCache.Prefetch` has a `defer recover()` that converts a panic into a per-package error. A corrupt tarball blowing up `gzip.NewReader`, a goja parser crashing on edge-case JS, or an out-of-bounds in any detector becomes a clean error message — the rest of the tree finishes.

### Invariant 5: integrity is verified before scoring

Tarball sha512 is checked in `registry.VerifyIntegrity` against the value the registry served in the packument. A mismatch is a hard error — there is no fallback path where mismatched bytes proceed to the scanner.

---

## The 13 detectors, by layer

phi's scoring combines three signal layers. Higher layers don't preempt lower ones — every package gets every applicable check.

### Layer 1: pattern detectors (regex)

Fast, low-false-positive checks for unambiguous strings. No AST parsing.

- Network Exfiltration (HIGH) — `.onion` URLs and known exfil services
- Crypto Mining (CRITICAL) — CoinHive, Monero/XMR, `stratum+tcp://`, CryptoNight
- Wallet Drain (CRITICAL) — `web3.eth.sendTransaction`, `ethers.Wallet`, `drainTokens`/`drainWallet`
- Reverse Shell (CRITICAL) — `bash -i`, `/dev/tcp/`, `mkfifo`, `nc -e /bin/`
- File System Access (HIGH) — `/etc/passwd`, `~/.aws/credentials`, etc.
- Linux System Tampering (CRITICAL) — PAM/eBPF/kernel-module/`LD_PRELOAD` symbols (v0.2.2)

### Layer 2: AST-validated detectors

Regex pre-filter eliminates obviously-irrelevant files (cheap fast-path). For files that match, goja parses the JS and walks the AST. Detection fires only when the matching token is a real `CallExpression` / `NewExpression`, not a string literal, comment, or identifier reference. Files goja can't parse (TypeScript, ES2022+) fall back to the regex match for safety.

- Arbitrary Code Execution (CRITICAL) — `eval()`, `child_process.exec/spawn(...)`
- Dynamic Code Compilation (HIGH) — `new Function(...)`
- Code Obfuscation (CRITICAL) — hex escapes (with diversity check), long base64 (with WASM-magic exception), `String.fromCharCode(N,N,N,N+)`, long `atob`
- Credential Theft (CRITICAL) — file paths (`.npmrc`/`id_rsa`) + env-var reads against a third-party-cred allowlist with own-config skip
- Install Script Abuse (CRITICAL) — `package.json` lifecycle hooks specifically
- Credential Exfil Flow (CRITICAL) — combined-flow detector with canonical-host allowlist (v0.2.2)

### Layer 3: known-vulnerability check (OSV)

Every resolved (name, version) is queried against [osv.dev](https://osv.dev) — aggregating GHSA, OpenSSF malicious-packages, and CVE feeds. Hits append to the same risk score by their advisory severity (CRITICAL +35 / HIGH +20 / MODERATE +10 / LOW +5). Disable with `--no-advisories` for offline use; network failure is non-fatal (warning + continue).

Detector implementations live in [`internal/analyzer/detectors.go`](./internal/analyzer/detectors.go). New detector tests in [`internal/analyzer/analyzer_test.go`](./internal/analyzer/analyzer_test.go) follow the pattern `TestRunDetectors_<Name>` with at least one benign-case fixture and one malicious-case fixture per detector.

---

## Process model

- **Single Go binary**, statically linked. No daemon. No persistent background process. No telemetry.
- **Distribution**: cross-platform via goreleaser → GitHub Releases (linux-amd64/arm64, darwin-amd64/arm64, windows-amd64). Per-release `checksums.txt` published alongside artifacts.
- **Install path**: `/usr/local/bin/phi` (Linux/macOS, sudo if needed) or `%LOCALAPPDATA%\phi\phi.exe` (Windows). Self-contained — no runtime dependencies, no `node` required.
- **Self-update path**: `phi self-update` queries GitHub Releases API, downloads the platform archive, verifies sha256 against `checksums.txt`, extracts the binary, atomically replaces `os.Executable()`. On Windows the running `.exe` is renamed to `.old` first (Windows can't overwrite a running binary); the leftover is cleaned up on next phi run.
- **Cache**: tarballs cached at `$XDG_CACHE_HOME/phi/tarballs/` (Linux/macOS) or `%LOCALAPPDATA%\phi\tarballs\` (Windows), keyed by sha512. Repeat installs are near-instant.

---

## Concurrency model

- **Resolver**: sequential BFS over the dependency tree for determinism. Packument cache (`internal/resolver/packument_cache.go`) prefetches children's packuments in the background — concurrent fetches are deduplicated, panics in fetch goroutines are caught and stored as errors.
- **Scanner**: bounded worker pool of 8 goroutines (configurable via the `scanWorkers` const). Each goroutine has `defer recover()` so one bad tarball can't kill the run. Results merge into a shared map under a mutex.
- **Network IO**: 30-second timeout on the HTTP client (registry + advisory). Configurable via env in tests.
- **No global state mutated after init**. Each `phi` invocation is a fresh process; nothing persists across runs except the cache (read-only at scan time, written only after successful integrity verification).

---

## Files that matter

```
cmd/phi/main.go                       CLI dispatch
internal/installer/installer.go       orchestration: resolve → fetch → scan → score → extract
internal/installer/atomic_write.go    writeFileAtomic for user-mutable files
internal/installer/auditfix.go        phi audit fix [--apply|--force]
internal/installer/selfupdate.go      phi self-update
internal/installer/create.go          phi create <framework>
internal/installer/run.go             phi do / phi exec
internal/installer/extract.go         tar extraction (post-verdict only)
internal/installer/lockfile.go        phi.lock read/write
internal/installer/report.go          phi-report.json writer
internal/installer/upsert.go          package.json mutation (atomic)
internal/installer/remove.go          phi remove (atomic)
internal/analyzer/analyzer.go         tarball traversal + per-file dispatch
internal/analyzer/detectors.go        13 detector definitions
internal/analyzer/notices.go          deprecation map (vm2 → isolated-vm, etc.)
internal/analyzer/ast.go              goja-based AST helpers (JS only)
internal/scorer/scorer.go             severity weights + verdict thresholds
internal/registry/registry.go         npm registry client + integrity verify
internal/resolver/resolver.go         BFS resolution + hoisting + peer-dep validation
internal/resolver/packument_cache.go  background packument prefetch (concurrent + panic-protected)
internal/advisory/advisory.go         OSV client (querybatch + detail fetch + cache)
internal/cache/cache.go               tarball cache (keyed by sha512)
internal/npmrc/npmrc.go               .npmrc parser (registry + auth tokens)
internal/workspace/workspace.go       npm workspaces support
internal/ui/ui.go                     terminal output, progress indicator, prompts
```

---

## What's intentionally NOT in the architecture

- **No plugin system.** Detectors are compiled in. This is a security tradeoff: third-party plugins would be code that runs with phi's privileges, and "trust the plugin" is a worse posture than "audit phi itself." Adding a detector means a PR.
- **No remote configuration.** All decisions phi makes are based on local state + the registry/OSV calls. There is no central server phi pings for policy. If phi blocks a package on your machine, it would also block it offline (modulo the OSV layer, which is opt-in and graceful on failure).
- **No telemetry.** Zero phone-home. Download counts come from the GitHub Releases API (aggregate, anonymous). Site analytics are Vercel page views (no individual user tracking). The "no daemon" line in the FAQ is meant literally.
- **No ML.** Every detector is a deterministic regex or AST visitor. No model files, no training data, no inference at install time. Reproducibility matters.

---

## Where this is going

For the public roadmap — multi-ecosystem direction, queued items, and explicit out-of-scope boundaries — see [`ROADMAP.md`](./ROADMAP.md). This document covers what's shipped and how it works.
