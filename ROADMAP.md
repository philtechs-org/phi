# phi — roadmap

phi started as a Node.js install-time scanner. It's becoming **install-time interception for software supply chains** across ecosystems. This page is the trajectory at an overview level; release notes live in [`CHANGELOG.md`](./CHANGELOG.md).

Status legend:

- **shipped** — released, on GitHub Releases, runnable today
- **in progress** — actively being worked on
- **next up** — committed direction, scoped, queued behind the in-progress item
- **on the horizon** — direction we're going, not committed to dates
- **out of scope** — explicit boundary; phi will not become this

---

## Shipped

- **v0.3.0** — `phi x` (npx-equivalent: scan-and-run for missing binaries) + animated resolver spinner with no first-frame gap
- **v0.2.4** — robustness pass + better error handling on Windows
- **v0.2.3** — `phi audit fix` (auto-fix typosquats, advisories, deprecations) + cross-shell scan indicator
- **v0.2.2** — supply-chain detector additions + deprecation guidance
- **v0.2.1** — `phi self-update`
- **v0.2.0** — `phi create` for popular framework scaffolders + a `--force` escape hatch
- **v0.1.0 – 0.1.2** — foundation: independent installer, OSV layer, cross-platform binaries

See [`CHANGELOG.md`](./CHANGELOG.md) for per-version detail.

---

## In progress

- **Code signing for Windows release artifacts.** Cleans up the install experience on first run.

---

## Next up

- **Multi-ecosystem support, starting with Go modules.** Same install-time interception pipeline, applied to `go.mod` instead of `package.json`. Existing Go tooling catches vulnerabilities once a CVE has been filed; phi catches the patterns themselves.

---

## On the horizon

Direction, not commitment. Order is rough priority.

- **Python (PyPI) ecosystem support**
- **Rust (crates.io) ecosystem support**
- **IDE integration** — live scan as you edit your manifest
- **CI integrations** — first-class GitHub Action, GitLab CI templates
- **Hosting-provider native integration** — Railway, Render, Vercel, Fly.io, and similar build pipelines recognizing `phi.lock` as a first-class lockfile, so deploys use phi's verified install path the same way they use `package-lock.json` today. (This is a partnership conversation, not a phi-side feature.)
- **Sandboxed dynamic analysis** — opt-in per package, results feed the same risk score
- **Custom detector plugins** with a strict trust model

---

## Out of scope

phi will not become these things. Listed so the boundary is explicit.

- **Runtime sandbox.** phi is install-time only. Once code is in `node_modules/` and your app runs it, that's the application's concern — use container limits or an EDR.
- **Forced migration.** `phi.lock` is its own complete, integrity-verified lockfile — phi can be your only package manager, in CI, in Docker, in production. But if you'd rather keep `package-lock.json` / `yarn.lock` / `pnpm-lock.yaml` alongside for tooling that expects them, phi leaves them alone. Drop-in, not rip-out.
- **Telemetry / centralized policy.** No daemon. No phone-home. Ever. Decisions happen on your machine, with your data, full stop.
- **Paid SaaS tier on the install path.** phi stays free and open-source. Commercial offerings would be separate products built on top — never behind a paywall on the install path itself.

---

## How to influence the roadmap

- **Open an issue** at https://github.com/philtechs-org/phi/issues with the use case + pain point. Concrete attack examples and false-positive reports get the fastest attention.
- **Star the repo** to signal interest in a planned item.
- **Pull requests with detector definitions** are welcome — the contribution pattern + tests are documented in the repo.

---

## Related documents

- [`README.md`](./README.md) — getting started + commands
- [`THREAT_MODEL.md`](./THREAT_MODEL.md) — what phi defends against, what it doesn't
- [`ARCHITECTURE.md`](./ARCHITECTURE.md) — pipeline overview
- [`CHANGELOG.md`](./CHANGELOG.md) — what shipped, when
- [Site roadmap (rendered)](https://phi.philtechs.org/roadmap.html) — same content, in-aesthetic
