package installer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/philtechs-org/phi/internal/ui"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Remove drops the named packages from package.json, prunes them and any
// transitives they alone pulled in from phi.lock and node_modules, and
// rewrites the lockfile. Existing scan scores/verdicts are preserved for the
// packages that remain.
func Remove(args []string) error {
	if len(args) == 0 {
		return errors.New("remove: at least one package name required")
	}

	if err := removeFromPackageJSON(args); err != nil {
		return fmt.Errorf("update package.json: %w", err)
	}

	targets, err := resolveTargets(nil, Options{})
	if err != nil {
		return err
	}
	roots := make(map[string]bool, len(targets))
	for _, t := range targets {
		roots[t.name] = true
	}

	body, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("nothing to remove (phi.lock not found)")
			return nil
		}
		return err
	}
	var lf lockFile
	if err := json.Unmarshal(body, &lf); err != nil {
		return fmt.Errorf("parse %s: %w", lockPath, err)
	}

	keep := reachableFromLockfile(lf.Packages, roots)

	removedSet := map[string]bool{}
	for key := range lf.Packages {
		name := nameFromInstallPath(key)
		if name == "" {
			continue
		}
		if !keep[name] {
			removedSet[name] = true
			delete(lf.Packages, key)
			if err := os.RemoveAll(filepath.FromSlash(key)); err != nil {
				ui.PrintWarning(fmt.Sprintf("rm %s: %v", key, err))
			}
		}
	}
	removed := make([]string, 0, len(removedSet))
	for n := range removedSet {
		removed = append(removed, n)
	}

	lf.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	out, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(lockPath, append(out, '\n'), 0o644); err != nil {
		return err
	}

	fmt.Printf("removed %d package(s)\n", len(removed))
	return nil
}

func reachableFromLockfile(packages map[string]lockEntry, roots map[string]bool) map[string]bool {
	keep := map[string]bool{}
	queue := make([]string, 0, len(roots))
	for name := range roots {
		queue = append(queue, name)
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if keep[n] {
			continue
		}
		entry, ok := packages["node_modules/"+n]
		if !ok {
			continue
		}
		keep[n] = true
		for child := range entry.Dependencies {
			queue = append(queue, child)
		}
	}
	return keep
}

func removeFromPackageJSON(names []string) error {
	body, err := os.ReadFile("package.json")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	body = bytes.TrimPrefix(body, utf8BOM)
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return err
	}
	for _, key := range []string{"dependencies", "devDependencies"} {
		deps, ok := data[key].(map[string]any)
		if !ok {
			continue
		}
		for _, name := range names {
			delete(deps, name)
		}
	}
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic("package.json", append(out, '\n'), 0o644)
}
