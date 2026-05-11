# Deferred work

This file lists features and improvements that were explicitly considered for a
specific release and punted to a later one. Each entry records what, why, and
what would unblock shipping it. Entries are not promises — direction can change.

For shipped work see [CHANGELOG.md](./CHANGELOG.md); for planned direction
see [ROADMAP.md](./ROADMAP.md).

---

## Deferred from v0.4.0 — `phi ci` / `--yes` / `--omit=dev`

### `--omit=optional` and `--omit=peer`

- **Status:** deferred
- **Why:** v0.4.0 ships only `--omit=dev`. The other two raise separate questions
  about how phi handles `optionalDependencies` and `peerDependencies` today —
  optional deps in particular are currently treated as required, which would
  need to change before `--omit=optional` is meaningful.
- **Unblocker:** audit current handling of optional and peer deps in
  `internal/resolver`; design how to honor `optional: true` and how to skip
  peer fulfillment when requested.

### Workspace-level devDependency filtering

- **Status:** deferred
- **Why:** v0.4.0's `--omit=dev` filters at the root `package.json` only. If a
  monorepo has `workspaces` and `workspace.Aggregate` (`internal/workspace`)
  merges per-workspace devDeps into the resolution set, those still flow
  through. For most prod-deploy use cases this is acceptable (the root
  package.json is what gets shipped), but full filtering is correct.
- **Unblocker:** update `workspace.Aggregate` to take an `omitDev bool` and
  drop devDeps per-workspace before merging.

### Per-package approval marker in `phi.lock`

- **Status:** deferred
- **Why:** `phi ci` treats lockfile presence + frozen-lockfile policy as the
  proxy for "dev approved this version" — if a review-verdict package is in
  the lock, dev must have approved it during their `phi install`. This is a
  reasonable heuristic but not an explicit record.
- **Unblocker:** false-trust becomes a real problem in practice (e.g. a
  user reports that a `--force`d blocked package quietly carried into prod).
  Then store explicit `{name, version, integrity, approved_at, verdict}`
  triples in the lockfile.

### Environment variable equivalents (`PHI_CI=1`, etc.)

- **Status:** deferred
- **Why:** Flags are explicit and discoverable. Env vars are convenient for
  CI scripts but add a second surface area.
- **Unblocker:** users ask for it. Map `PHI_CI` → `phi ci`, `PHI_YES` → `-y`,
  `PHI_OMIT_DEV` → `--omit=dev`.

---

## Deferred from v0.3.0 — `phi x` (npx-equivalent)

### Git / GitHub shorthand sources

- **Status:** deferred
- **Why:** phi's resolver speaks the npm registry only. `phi x github:user/repo`
  would need a new code path for fetching from git URLs and scanning code that
  has no packument identity (no advisory feed match, no integrity hash from
  the registry).
- **Unblocker:** extend `internal/resolver` to accept git/GitHub specs;
  decide how to score packages with no advisory feed coverage.

### Tarball URL sources

- **Status:** deferred
- **Why:** Same root cause as git sources — resolver is registry-only.
- **Unblocker:** same as above; add a `fetchFromURL` path in `internal/registry`
  and route through the same scan + stage pipeline.

### Multi-package `phi x -p foo -p bar mybin`

- **Status:** deferred
- **Why:** ~1% of real npx invocations. Adds resolver complexity (merge
  multiple direct trees into one stage) for a niche use case.
- **Unblocker:** demand. Until then, users can `phi install foo bar &&
  phi x mybin` to get the same effect.

### Inline shell `phi x -c "cmd"`

- **Status:** deferred (likely permanently out of scope)
- **Why:** Overlaps almost entirely with `phi do`, which already runs a shell
  command with `node_modules/.bin` on PATH. Adding `-c` to `phi x` would
  duplicate that primitive for no gain.
- **Unblocker:** specific use case that `phi do` can't cover.

### Stage cache eviction policy

- **Status:** deferred
- **Why:** `$UserCacheDir/phi/run/<name>@<version>/` directories accumulate
  forever. `phi cache clean` currently prunes tarballs only.
- **Unblocker:** stages grow large enough to matter. Add `--runs` to
  `phi cache clean`, or auto-prune stages older than N days.
