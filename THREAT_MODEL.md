# phi — threat model

What phi protects against, what it doesn't, and the assumptions you accept by adopting it.

This document is for security-conscious developers, application security teams, and anyone evaluating phi for production use. It's deliberately specific — vague threat models are how supply-chain failures hide.

---

## Scope: what phi defends against

phi is a **pre-extraction interception layer** for the npm install path. Its job is to refuse to write malicious code to your disk in the first place. Concretely:

### 1. Malicious packages on npm

- **Typosquats** of popular packages (`loadsh`, `expresss`, `react-dom-uuid`). Levenshtein distance 1 against a curated list of high-traffic names triggers a HIGH severity warning.
- **Compromised maintainer accounts** publishing malicious versions of legitimate packages. phi's behavioral detectors fire on the patterns the malicious version exhibits, regardless of who published it.
- **Dependency confusion** attacks (private package name shadowed by a public one). phi reads `.npmrc` for scoped registry routing the same way npm does — if your `@my-org/*` packages route to a private registry, phi honors that.

### 2. Malicious behavior in package code

The 13 detectors in `internal/analyzer/detectors.go` look for:

- Direct code execution (`eval`, `child_process.exec/spawn`)
- Dynamic code compilation (`new Function`)
- Code obfuscation (hex escapes, base64 payloads, `String.fromCharCode` chains, `atob` of long strings — with a WASM-magic exception so legitimate WebAssembly modules don't trip)
- Credential theft (env-var reads of known third-party tokens — AWS, GitHub, npm, Stripe, Twilio, etc. — with own-config skip so packages reading their own config stay quiet)
- Install-script abuse (lifecycle hooks `preinstall`/`install`/`postinstall` piping remote code into shells)
- Crypto mining (CoinHive, Monero/XMR, `stratum+tcp://` URLs, CryptoNight references)
- Wallet drain (`web3.eth.sendTransaction`, `ethers.Wallet`, `drainTokens`)
- Reverse shells (`/bin/bash -i`, `/dev/tcp/`, `mkfifo`, `nc -e /bin/`)
- Network exfiltration to known C2/exfil services (`.onion` URLs, pastebin, hastebin, requestbin, webhook.site, ngrok, transfer.sh, anonfiles, gofile, paste.ee, controlc)
- File-system access to OS-level credential paths (`/etc/passwd`, `/etc/shadow`, `~/.aws/credentials`, `~/.kube/config`, `~/.docker/config.json`)
- **Credential exfil flow** (combined-flow detector): a credential read AND outbound HTTP in the same file, to a non-canonical host. Octokit reading `GITHUB_TOKEN` and posting to `api.github.com` is silent; the same token going elsewhere fires.
- **Linux system tampering**: PAM (`pam_authenticate`, `libpam.so`), eBPF (`BPF_PROG_LOAD`, `bpf_load_program`, `perf_event_open`), kernel module loading (`init_module`, `finit_module`, `delete_module`), `LD_PRELOAD` and `/etc/ld.so.preload` writes. Direct response to QLNX-style Linux RAT delivery patterns.

### 3. Known vulnerabilities

Every resolved (name, version) is queried against the [OSV database](https://osv.dev), which aggregates GHSA, OpenSSF malicious-packages, and CVE feeds. Hits append to the per-package risk score by their advisory severity.

### 4. Lifecycle script execution

`preinstall` / `install` / `postinstall` scripts are **off by default**. The single most-abused attack vector in npm is the lifecycle script that runs as soon as a package is on disk. phi treats them as opt-in: `--allow-scripts esbuild,sharp` enables them per-package after the user has explicitly named them.

### 5. State corruption from interrupted operations

`package.json`, `phi.lock`, and `phi-report.json` are all written through an atomic write-temp-then-rename pattern. A `Ctrl-C` mid-write or a power loss preserves the previous file content rather than leaving a half-written or empty file.

---

## Out of scope: what phi does NOT protect against

phi is one layer in a defense-in-depth posture. It is **not** a substitute for:

- **Runtime attacks after install.** Once code is in `node_modules/` and your app `require()`s it, anything that code does at runtime is your application's responsibility. Use a runtime sandbox (gVisor, Firecracker, container limits) for that.
- **System-level RATs delivered by other vectors.** If malware lands on your machine via phishing, a malicious binary download, a compromised container image, or an OS-level exploit, phi has no visibility. Use endpoint detection and response (EDR) for that.
- **Kernel-level threats.** Rootkits, eBPF abuse from outside the npm install path, kernel exploits — out of scope.
- **Supply-chain attacks against phi itself or its build pipeline.** We mitigate via: reproducible builds, sha256 checksums published with every release, sigstore signing on roadmap, atomic self-update with checksum verification before swap. But a sufficiently capable attacker compromising the phi build pipeline could ship a malicious phi binary. Verify checksums on download (the install scripts already do this).
- **Source-code-level vulnerabilities in legitimate packages.** A package with a SQL injection bug doesn't trigger any detector — that's the package's concern, not the package manager's. Use a SAST tool for that.
- **Zero-day novel attack patterns.** phi's detectors codify known patterns. A wholly new technique that doesn't match any pattern won't fire until we add a detector for it. The OSV layer covers known-CVE cases regardless.
- **Malicious binaries inside packages** (when a package ships a precompiled `.node` native module or `.wasm` blob). phi scans source files; binary blobs are opaque. Mitigation: lifecycle scripts off blocks the typical install-time invocation; native modules don't execute until your code loads them.
- **The user's own decisions to bypass the verdict.** `--force` proceeds with BLOCKED packages. The audit trail (`phi-report.json`) is preserved, so the bypass is logged, but phi will not refuse to install code the user has explicitly approved. The escape hatch exists because false positives on legitimate libraries (e.g., discord.js's internal `_eval()` method) are real — refusing it would make phi unusable. Trust is the user's to grant.

---

## Trust assumptions

By adopting phi you implicitly trust:

| Trust | What it covers | How phi treats it |
|---|---|---|
| **npm registry's TLS** | The bytes of a tarball are what npm served | phi additionally verifies sha512 integrity from the packument before scanning |
| **OSV database** | Vulnerability data is current and accurate | phi treats network failure as non-fatal — falls through to static detectors. `--no-advisories` opts out entirely. |
| **Go toolchain** | The binary you're running is the one we built | Mitigated by reproducible builds + per-release `checksums.txt`. Code signing on the roadmap. |
| **GitHub Releases** | The artifact you downloaded is the one we tagged | Install scripts verify sha256 against checksums.txt before extraction. Self-update verifies sha256 against the release's checksums.txt before swap. |
| **The user's environment** | Your shell, PATH, and filesystem aren't already compromised | phi doesn't (and can't) verify this — it's outside the install boundary |
| **Your `.npmrc` auth tokens** | If you've configured private registry auth, phi needs to use those tokens | phi reads `.npmrc` via the same logic npm uses; tokens never leave your machine |

---

## Failure modes

How phi behaves when something goes wrong:

| Failure | Behavior |
|---|---|
| Network unavailable mid-resolve | Resolve fails with clear error; lockfile (if present) can be used as a fallback resolution source |
| OSV API unavailable | Warning printed; install continues with static detectors only. `--no-advisories` skips the call entirely (offline mode) |
| Corrupt tarball (bad gzip, bad tar) | Skipped, error logged, install continues with the rest of the tree |
| Tarball sha512 mismatch | Hard error; the affected package and its dependents fail to install. No silent fallback. |
| Goroutine panic during scan | Caught by `defer recover()`; converted to a per-package error. The rest of the scan tree finishes. One bad package can't kill the run. |
| Mid-write `Ctrl-C` on `package.json` / `phi.lock` / `phi-report.json` | Atomic rename pattern preserves the previous file content |
| `phi self-update` permission denied at install dir | Detected before download via preflight permission probe; clear platform-aware hint (sudo / elevated PowerShell) |
| Windows Defender flags the binary as PUA | Install script detects the specific error message and prints exact `Add-MpPreference` / `Unblock-File` remediation. The binary's sha256 was already verified against `checksums.txt` — the bytes are the published release. |
| False positive on a trusted library | `--force` overrides the BLOCKED verdict. The scan still runs and `phi-report.json` is still written, so the audit trail is preserved. |

---

## Reporting a vulnerability

If you discover a security issue in phi itself (not in a scanned package — those go to the package maintainer):

- For non-critical issues: open a GitHub issue at https://github.com/philtechs-org/phi/issues
- For critical issues that would benefit from coordinated disclosure: email the maintainer directly (contact in the GitHub profile). Include reproduction steps, affected versions, and your suggested mitigation if any.

We don't currently have a bug bounty. We do publicly credit reporters in CHANGELOG.md when a security fix ships.

---

## Related documents

- [`ARCHITECTURE.md`](./ARCHITECTURE.md) — internal mechanics, the document a security reviewer needs to evaluate trust assumptions
- [`CHANGELOG.md`](./CHANGELOG.md) — what shipped, when, with security-fix annotations
- [`README.md`](./README.md) — getting started + commands
- [Site FAQ](https://phi.philtechs.org/faq.html) — user-facing answers to common concerns including the Windows Defender false-positive guidance
