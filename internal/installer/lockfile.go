package installer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/philtechs-org/phi/internal/analyzer"
	"github.com/philtechs-org/phi/internal/resolver"
)

// lockfileVersion is bumped to 2 with the introduction of nested install
// paths in Phase 2 (hoisting + peer-deps iteration). v1 lockfiles assumed
// every package lived at node_modules/<name>. v2 keys can be nested,
// e.g. node_modules/A/node_modules/lodash for conflict resolution.
const lockfileVersion = 2

// ErrNoLockfile is returned by ReadLockfile when phi.lock is absent.
var ErrNoLockfile = errors.New("no lockfile")

type lockEntry struct {
	Version      string            `json:"version"`
	Resolved     string            `json:"resolved"`
	Integrity    string            `json:"integrity"`
	Score        int               `json:"score"`
	Verdict      string            `json:"verdict"`
	Dependencies map[string]string `json:"dependencies"`
}

type lockFile struct {
	LockfileVersion int                  `json:"lockfileVersion"`
	Generator       string               `json:"generator"`
	GeneratedAt     string               `json:"generatedAt"`
	Packages        map[string]lockEntry `json:"packages"`
}

// WriteLockfile writes phi.lock at path. Map keys are install paths, sorted
// by encoding/json for stable diffs.
func WriteLockfile(path string, tree *resolver.Tree, scans map[string]*analyzer.AnalysisReport, generator string) error {
	pkgs := make(map[string]lockEntry, len(tree.All))
	for installPath, p := range tree.All {
		score := 0
		verdict := "safe"
		if r, ok := scans[installPath]; ok {
			score = r.RiskScore
			verdict = r.Verdict
		}
		deps := p.Deps
		if deps == nil {
			deps = map[string]string{}
		}
		pkgs[installPath] = lockEntry{
			Version:      p.Version,
			Resolved:     p.Resolved,
			Integrity:    p.Integrity,
			Score:        score,
			Verdict:      verdict,
			Dependencies: deps,
		}
	}

	lf := lockFile{
		LockfileVersion: lockfileVersion,
		Generator:       generator,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		Packages:        pkgs,
	}

	body, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(body, '\n'), 0o644)
}

// ReadLockfile parses phi.lock back into a tree we can install from. The
// returned tree's Roots is empty; the caller fills it from package.json
// since the lockfile alone does not distinguish direct deps from transitives.
func ReadLockfile(path string) (*resolver.Tree, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoLockfile
		}
		return nil, err
	}
	var lf lockFile
	if err := json.Unmarshal(body, &lf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if lf.LockfileVersion != lockfileVersion {
		return nil, fmt.Errorf("%s: unsupported lockfileVersion %d (expected %d) — re-run install to migrate",
			path, lf.LockfileVersion, lockfileVersion)
	}
	tree := &resolver.Tree{All: map[string]*resolver.Pkg{}}
	for installPath, entry := range lf.Packages {
		if !strings.HasPrefix(installPath, "node_modules/") {
			continue
		}
		name := nameFromInstallPath(installPath)
		if name == "" {
			continue
		}
		deps := entry.Dependencies
		if deps == nil {
			deps = map[string]string{}
		}
		tree.All[installPath] = &resolver.Pkg{
			Name:        name,
			Version:     entry.Version,
			Resolved:    entry.Resolved,
			Integrity:   entry.Integrity,
			Deps:        deps,
			InstallPath: installPath,
		}
	}
	return tree, nil
}

// nameFromInstallPath extracts the package name from an install path:
//
//	node_modules/lodash                                 → lodash
//	node_modules/A/node_modules/lodash                  → lodash
//	node_modules/@types/node                            → @types/node
//	node_modules/A/node_modules/@types/node             → @types/node
func nameFromInstallPath(installPath string) string {
	idx := strings.LastIndex(installPath, "/node_modules/")
	if idx < 0 {
		return strings.TrimPrefix(installPath, "node_modules/")
	}
	return installPath[idx+len("/node_modules/"):]
}

// LockfileCovers checks that every direct dep is present in the tree at the
// hoisted (root) level with a satisfying version. A direct dep that exists
// only nested doesn't count — by definition it must live at root.
func LockfileCovers(tree *resolver.Tree, direct map[string]string) bool {
	for name, spec := range direct {
		pkg := tree.Hoisted(name)
		if pkg == nil {
			return false
		}
		if !resolver.Satisfies(spec, pkg.Version) {
			return false
		}
	}
	return true
}
