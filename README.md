# Phi

**A security-first package manager for Node.js.** Phi scans every package — top-level and transitive — *before* anything is written to disk, scores it on a 0–100 risk scale, and either installs it, prompts you, or blocks it. Built in Go for speed.

```
phi install express
```

```
Phi  · secure package manager
resolving dependency tree...
scanning 53 packages...
extracting approved packages...
✔ installed 53 package(s) (safe=53 review=0)
  lockfile: phi.lock
  report:   phi-report.json
```

Full documentation: <https://phi.philtechs.org>

## Why

Supply-chain attacks against npm have moved from rare incidents to a steady drumbeat. Compromised maintainer accounts, typosquats, and payloads buried inside transitive dependencies have drained crypto wallets, stolen credentials, and installed backdoors on developer machines. Existing tools (`npm audit`, Snyk, OWASP Dependency-Check) check known CVE databases *after* code is on disk. Phi inspects every package's actual code *before* any extraction or script execution.

## How it differs from `npm install`

Phi is a real package manager — it does not wrap or shell out to npm:

1. Resolves the full transitive tree itself (Masterminds/semver, npm registry).
2. Fetches every tarball, verifies sha512 integrity from the packument.
3. Runs an 11-detector malware analysis on each tarball **before extraction**.
4. Aggregates verdicts: any *blocked* package aborts the install; *review* prompts.
5. Extracts approved packages into `node_modules/<name>/`.
6. **Lifecycle scripts (`preinstall`, `install`, `postinstall`) never run by default.** This is the single biggest attack surface npm exposes; phi closes it. Opt in per-package via `--allow-scripts <pkg>`.

## Install

**Linux / macOS:**

```sh
curl -sSL https://phi.philtechs.org/install.sh | sh
```

**Windows (PowerShell):**

```powershell
iwr -useb https://phi.philtechs.org/install.ps1 | iex
```

Both scripts detect platform + arch, download the right archive from [GitHub Releases](https://github.com/philtechs-org/phi/releases), verify sha256, and place the binary on PATH (or print where to add it).

**Direct download:** grab the archive for your platform from the [Releases page](https://github.com/philtechs-org/phi/releases) and extract `phi` into any directory on PATH.

**Build from source:** Go 1.21+ required.

```sh
git clone https://github.com/philtechs-org/phi
cd phi
go mod tidy
go build -o phi ./cmd/phi
```

## Commands

| Command | Aliases | Purpose |
|---|---|---|
| `phi init` | — | Create `package.json` + `.gitignore` + `README.md`. `--yes` skips prompts. |
| `phi install [pkg…]` | `i`, `a` | Scan and install. Args are added to `package.json`. |
| `phi update [pkg…]` | `u` | Re-resolve fresh, ignoring `phi.lock`. |
| `phi remove <pkg…>` | `rm` | Drop from `package.json`, `phi.lock`, and `node_modules`. |
| `phi audit` | — | Scan all dependencies without installing. |
| `phi do <script> [args…]` | `d` | Run a script from `package.json` with `node_modules/.bin` on PATH. |
| `phi exec <bin> [args…]` | `x` | Run a binary from `node_modules/.bin` directly. |
| `phi dev` / `build` / `start` / `test` / `lint` / `preview` / `prod` | — | Direct shortcuts to `phi do <name>`. |
| `phi outdated` | — | Show direct deps with newer versions available. |
| `phi why <pkg>` | — | Print the dep chain that pulled a package in. |
| `phi cache stat` / `clean` | — | Inspect or prune the on-disk tarball cache. |
| `phi version` / `phi help` | — | Self-explanatory. |

`phi do` is phi's distinctive verb (npm/yarn/pnpm all use `run`). It reads naturally — "do the dev script", "do migrate:js" — and has the same lifecycle hook semantics as npm: `predev` and `postdev` run automatically around `dev`. Extra args pass through (`phi do dev --port 3000` → `<dev script> --port 3000`).

`phi exec` follows the npm/pnpm convention — it executes a binary from `node_modules/.bin`. Unlike `npx`, it never auto-installs.

## Usage examples

```sh
phi init                          # bootstrap a new project
phi install                       # scan + install all of package.json
phi i lodash                      # add lodash (short alias)
phi install chalk@^5.0.0          # specific version range
phi install --frozen-lockfile     # CI mode: phi.lock must match package.json
phi install --no-lockfile         # force a fresh resolve
phi install --save-dev jest       # write to devDependencies

phi update                        # re-resolve everything from package.json
phi update lodash                 # re-resolve lodash, keep the rest

phi rm lodash                     # remove lodash + its unique transitives

phi audit                         # scan without installing
phi audit --json | jq             # machine-readable report

phi do dev                        # run the "dev" script
phi dev                           # same — direct shortcut
phi do migrate:js -- --dry-run    # colon-named scripts work; args pass through
phi exec eslint .                 # run a binary from node_modules/.bin

phi why ms                        # "why is this in my tree?"
phi outdated                      # what has a newer version available?
phi cache stat                    # how big is the tarball cache?

phi install --allow-scripts esbuild,sharp    # opt-in to lifecycle scripts
```

## Detection engine

Eleven detectors cover the npm threat landscape. Hits from any layer add to a single per-package risk score.

| Detector | Severity | Catches |
|---|---|---|
| Arbitrary Code Execution | CRITICAL | `eval()`, `child_process.exec/spawn(...)` (AST-validated — only fires on real call expressions) |
| Dynamic Code Compilation | HIGH | `new Function(...)` (AST-validated — separate from `eval` because it's legitimately used by validator generators, JSON serializers, route compilers) |
| Code Obfuscation | CRITICAL | hex-escape strings (with diversity check), long base64, `String.fromCharCode(N,N,N,N+)` with 4+ numeric args, `atob("…40+chars…")` |
| Credential Theft | CRITICAL | `.npmrc` / `.netrc` / `id_rsa` / `id_ed25519` references, plus `process.env.<X>` against a third-party-cred allowlist (AWS, GitHub, npm, Stripe, Twilio, …). Skips the package's *own* config (`process.env.RESEND_API_KEY` from inside resend is silent). |
| Install Script Abuse | CRITICAL | `curl \| bash` / `node -e` *only inside `scripts.{preinstall,install,postinstall}` of `package.json`*. Test scripts and prepublish hooks don't fire. |
| Crypto Mining | CRITICAL | CoinHive, Monero/XMR, `stratum+tcp://` URLs, CryptoNight refs |
| Wallet Drain | CRITICAL | `web3.eth.sendTransaction`, `ethers.Wallet`, `drainTokens` / `drainWallet` |
| Reverse Shell | CRITICAL | `bash -i`, `/dev/tcp/`, `mkfifo`, `nc -e /bin/` |
| Network Exfiltration | HIGH | `.onion` URLs and known exfil services (pastebin, requestbin, webhook.site, ngrok, transfer.sh, …) |
| File System Access | HIGH | reads of `/etc/passwd`, `/etc/shadow`, `.aws/credentials`, `.kube/config`, `.docker/config.json` |
| Typosquatting | HIGH | package name within Levenshtein distance 1 of a popular package (lodash, express, axios, react, vue, …) |

### Scoring

| Severity | Points | Verdict thresholds |
|---|---|---|
| CRITICAL | +35 | **safe** 0–19 → install silently |
| HIGH | +20 | **review** 20–59 → prompt the developer |
| MODERATE | +10 | **blocked** 60–100 → reject, no extraction |
| LOW | +5 | (capped at 100) |

The blocked threshold is 60 (not 50), so a single CRITICAL+HIGH combination (e.g. `eval` + `new Function` in a code-generation library) lands in REVIEW for the user to decide. Real malware typically combines two CRITICAL signals (e.g. `eval` + credential theft = 70) which trip the threshold.

Detections are deduplicated per package — a noisy codebase doesn't artificially inflate the score.

### Three layers of analysis

1. **Pattern detectors** (regex) — fast checks for URL patterns, env-var names, sensitive paths.
2. **AST-validated detectors** — for `eval`, `child_process.exec/spawn`, and `new Function`, phi parses `.js` / `.cjs` / `.mjs` files with [goja](https://github.com/dop251/goja) and only fires on real `CallExpression` / `NewExpression` nodes — not string literals, comments, or identifier references. TypeScript and ES2022+ files goja can't parse fall back to regex.
3. **Known-vulnerability check (OSV)** — every resolved (name, version) is queried against the [OSV database](https://osv.dev), aggregating GHSA, OpenSSF malicious-packages, and CVE feeds. Advisory severities map to phi points (CRITICAL +35 / HIGH +20 / MODERATE +10 / LOW +5) and appear in `phi-report.json` with their IDs and references. Disable with `--no-advisories` for offline use.

```
$ phi audit  # against package.json: { "lodash": "4.17.20" }
lodash@4.17.20  files=1047  score=70  verdict=BLOCKED
  - [HIGH]     advisory GHSA-35jh-r3h4-6jhm — Command Injection in lodash
  - [HIGH]     advisory GHSA-r5fr-rjxr-66jc — lodash vulnerable to Code Injection via `_.template`
  - [MODERATE] advisory GHSA-29mw-wpgm-hmr9 — Regular Expression Denial of Service in lodash
  - [MODERATE] advisory GHSA-f23m-r3pf-42rh — lodash Prototype Pollution via array path
  - [MODERATE] advisory GHSA-xxjr-mmjv-4gpg — lodash Prototype Pollution in `_.unset`
```

## Lockfile and cache

- `phi.lock` is generated on every install. Format mirrors npm's `package-lock.json` shape with extra `score` and `verdict` fields per entry.
- When `phi.lock` exists and covers `package.json`, phi skips resolution entirely and installs from the lock — no registry calls beyond cache hits.
- Tarballs cache at `$XDG_CACHE_HOME/phi/tarballs/` (Unix) or `%LOCALAPPDATA%\phi\tarballs\` (Windows), keyed by sha512 integrity. Repeat installs are near-instant.

## Hoisting & peer dependencies

Phi resolves the full transitive tree, hoists shared deps to `node_modules/<name>` when there's no conflict, and nests conflicting versions under their consumer (`node_modules/<consumer>/node_modules/<name>`) so Node's module-resolution sees the right version at every level.

Peer dependencies are read from each packument and validated after resolution. Missing required peers produce a warning; missing optional peers (`peerDependenciesMeta` `optional: true`) are silent. Phi never auto-installs peers — the user adds them to `package.json` explicitly.

```
$ phi install react-dom
warning react-dom requires peer react@^19.2.6 but no provider found
✔ installed 2 package(s)

$ phi install react react-dom    # both supplied → no warning
```

## Workspaces (monorepos)

If your root `package.json` declares a `workspaces` field, phi aggregates dependencies from every workspace package, installs the union into the root `node_modules/`, and links each workspace into `node_modules/<workspace-name>` as a junction (Windows) or symlink (Unix). Sibling references (`"@org/utils": "*"` from inside `@org/app`) are recognized and bypass the registry.

```json
{ "workspaces": ["packages/*"] }
```

Both array form and `{packages: [...]}` object form are supported.

## Private registries (.npmrc)

Phi reads `.npmrc` from `$HOME` and the project root (project wins on conflict). Supported settings:

- `registry=https://...` — default registry override
- `@scope:registry=https://...` — scoped registry routing
- `//host/path/:_authToken=...` — bearer token, sent as `Authorization: Bearer <token>` to matching URLs
- `${ENV_VAR}` substitution — keep secrets out of committed files

```
//npm.pkg.github.com/:_authToken=${GITHUB_PAT}
@my-org:registry=https://npm.pkg.github.com/
```

## CI

```yaml
- run: phi install --frozen-lockfile
- run: phi audit --json > phi-report.json
- run: jq '.summary.blocked == 0 and .summary.review == 0' phi-report.json
```

`--frozen-lockfile` requires `phi.lock` to exactly cover `package.json` and fails otherwise. `--json` suppresses interactive output and emits the full scan report to stdout. Non-zero exit on any blocked or review-flagged package in `--json` mode.

## Current limitations

- **Git/file/tarball deps** — non-semver specs (`git+…`, `file:…`, `https://…tgz`) warn and skip in resolution.
- **Older lockfile formats** — phi reads `phi.lock` only; npm v6 `package-lock.json` is not parsed.
- **Sandboxed dynamic analysis** — phi is static-analysis only. Detection covers regex + AST + OSV; runtime behavior is not observed.

## Roadmap

- `phi create <react|next|express|fastify|nest>` — secure project scaffolders
- Optional sandboxed execution probe (observe behavior, not just static patterns)
- Threat-feed live updates (auto-pull new detection rules)
- Git / file / tarball dep specs
- Future: SaaS / dashboard, partnership proposal to OpenJS / npm Inc.

## Contributing

Adding a new detector is intentionally trivial. In `internal/analyzer/detectors.go`:

```go
{
    name:        "Your Detector",
    description: "What it catches",
    severity:    SeverityHigh,
    patterns: mustCompile(
        `your\s+regex\s+here`,
    ),
}
```

For context-aware detection (e.g. needs the package name or a parsed AST), set a `matcher` function instead of `patterns`. The scoring and UI layers pick up new detectors automatically. Tests live in `internal/analyzer/analyzer_test.go` — add a benign case and a malicious case for any new pattern.

## License

MIT.

---

phi · Brayne / Philtechs · v0.1.0
