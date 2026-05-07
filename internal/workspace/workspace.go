// Package workspace discovers monorepo workspaces declared in a root
// package.json's `workspaces` field. Supports the npm/yarn array form and
// the npm/yarn object form. The caller (installer) uses the result to
// aggregate dependencies across all workspaces and to mark sibling refs
// (deps whose name matches a workspace) so they bypass registry resolution.
package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/tidwall/gjson"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Workspace describes one package in a monorepo.
type Workspace struct {
	Name             string
	Version          string
	Dir              string
	Dependencies     map[string]string
	DevDependencies  map[string]string
	PeerDependencies map[string]string
}

// Discover reads rootDir/package.json and returns the workspaces it
// declares. Returns (nil, nil) when no workspaces field is present —
// distinguish from error so callers can fall back to single-package mode.
func Discover(rootDir string) ([]*Workspace, error) {
	pjPath := filepath.Join(rootDir, "package.json")
	body, err := os.ReadFile(pjPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	body = bytes.TrimPrefix(body, utf8BOM)

	patterns := extractPatterns(body)
	if len(patterns) == 0 {
		return nil, nil
	}

	var workspaces []*Workspace
	seen := map[string]bool{}
	for _, pat := range patterns {
		matches, err := globWorkspace(rootDir, pat)
		if err != nil {
			return nil, err
		}
		for _, dir := range matches {
			if seen[dir] {
				continue
			}
			seen[dir] = true
			ws, err := readWorkspace(dir)
			if err != nil {
				continue
			}
			workspaces = append(workspaces, ws)
		}
	}
	sort.Slice(workspaces, func(i, j int) bool { return workspaces[i].Name < workspaces[j].Name })
	return workspaces, nil
}

func extractPatterns(body []byte) []string {
	var patterns []string
	if arr := gjson.GetBytes(body, "workspaces"); arr.IsArray() {
		arr.ForEach(func(_, v gjson.Result) bool {
			patterns = append(patterns, v.String())
			return true
		})
		return patterns
	}
	if pkgs := gjson.GetBytes(body, "workspaces.packages"); pkgs.IsArray() {
		pkgs.ForEach(func(_, v gjson.Result) bool {
			patterns = append(patterns, v.String())
			return true
		})
	}
	return patterns
}

func globWorkspace(rootDir, pattern string) ([]string, error) {
	pat := filepath.Join(rootDir, filepath.FromSlash(pattern))
	matches, err := filepath.Glob(pat)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil || !info.IsDir() {
			continue
		}
		dirs = append(dirs, m)
	}
	return dirs, nil
}

func readWorkspace(dir string) (*Workspace, error) {
	body, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil, err
	}
	body = bytes.TrimPrefix(body, utf8BOM)
	var data struct {
		Name             string            `json:"name"`
		Version          string            `json:"version"`
		Dependencies     map[string]string `json:"dependencies"`
		DevDependencies  map[string]string `json:"devDependencies"`
		PeerDependencies map[string]string `json:"peerDependencies"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Join(dir, "package.json"), err)
	}
	if data.Name == "" {
		return nil, errors.New("workspace package.json missing name")
	}
	ws := &Workspace{
		Name:             data.Name,
		Version:          data.Version,
		Dir:              dir,
		Dependencies:     data.Dependencies,
		DevDependencies:  data.DevDependencies,
		PeerDependencies: data.PeerDependencies,
	}
	if ws.Dependencies == nil {
		ws.Dependencies = map[string]string{}
	}
	if ws.DevDependencies == nil {
		ws.DevDependencies = map[string]string{}
	}
	if ws.PeerDependencies == nil {
		ws.PeerDependencies = map[string]string{}
	}
	return ws, nil
}

// Aggregate returns the union of every workspace's deps + the root's deps.
// Sibling references (where the spec's name matches another workspace) are
// dropped from the result and returned separately as `siblings`. Caller
// handles them via filesystem links instead of registry fetches.
func Aggregate(rootDeps map[string]string, workspaces []*Workspace) (deps map[string]string, siblings map[string]bool) {
	siblings = map[string]bool{}
	for _, ws := range workspaces {
		siblings[ws.Name] = true
	}

	deps = map[string]string{}
	add := func(m map[string]string) {
		for n, s := range m {
			if siblings[n] {
				continue
			}
			deps[n] = s
		}
	}
	add(rootDeps)
	for _, ws := range workspaces {
		add(ws.Dependencies)
		add(ws.DevDependencies)
		// peerDependencies of workspaces are intentionally not auto-installed
	}
	return deps, siblings
}
