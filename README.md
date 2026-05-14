# Phi

[![Latest release](https://img.shields.io/github/v/release/philtechs-org/phi-releases?label=release&color=84cc16)](https://github.com/philtechs-org/phi-releases/releases/latest)
[![Downloads (total)](https://img.shields.io/github/downloads/philtechs-org/phi-releases/total?label=downloads&color=84cc16)](https://github.com/philtechs-org/phi-releases/releases)
[![Downloads (latest)](https://img.shields.io/github/downloads/philtechs-org/phi-releases/latest/total?label=downloads%40latest&color=84cc16)](https://github.com/philtechs-org/phi-releases/releases/latest)
[![License](https://img.shields.io/github/license/philtechs-org/phi?color=84cc16)](./LICENSE)

**Nothing touches your system unverified.** Phi is install-time interception
for software supply chains — every dependency, top-level and transitive, gets
scanned in memory before any code reaches disk and scored on a 0–100 risk
scale. Then phi installs it, prompts you, or blocks it. Lifecycle scripts off
by default. Single Go binary. Drop-in for npm today; Go modules next.

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

## Install

**Linux / macOS**

```sh
curl -sSL https://phi.philtechs.org/install.sh | sh
```

**Windows (PowerShell)**

```powershell
iwr -useb https://phi.philtechs.org/install.ps1 | iex
```

Both scripts pull the latest release, verify the sha256 against
`checksums.txt`, and drop `phi` on your PATH.

Manual download:
<https://github.com/philtechs-org/phi-releases/releases/latest>

If you already have phi installed:

```sh
phi self-update
```

## Repository layout

This repository is a public landing page. The source code for phi is
maintained privately. Public-facing surfaces are split across two repos:

| Repo                                                                             | Contents                                            |
|----------------------------------------------------------------------------------|-----------------------------------------------------|
| **[philtechs-org/phi](https://github.com/philtechs-org/phi)**                    | This page — README, LICENSE, CHANGELOG              |
| **[philtechs-org/phi-releases](https://github.com/philtechs-org/phi-releases)**  | Per-version binary release artifacts (`.tar.gz`, `.zip`, `checksums.txt`) |

[`CHANGELOG.md`](./CHANGELOG.md) tracks every released version.

## Issues, security, contact

- **Bug reports & feature requests:** email
  [bugs@phi.philtechs.org](mailto:bugs@phi.philtechs.org). Include the
  package name, version, and what phi said vs what you expected.
  False-positive and missed-malware reports get the fastest attention.
- **Security:** the same address —
  [bugs@phi.philtechs.org](mailto:bugs@phi.philtechs.org). Please do not
  file security issues publicly.
- **Docs:** <https://phi.philtechs.org>.

## License

MIT. See [`LICENSE`](./LICENSE).
