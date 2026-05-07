package installer

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/philtechs-org/phi/internal/resolver"
	"github.com/tidwall/gjson"
)

// Why prints every dep chain from a root in package.json down to the named
// package — explains "why is this in my tree?".
func Why(args []string) error {
	if len(args) == 0 {
		return errors.New("why: package name required")
	}
	target := args[0]

	tree, err := ReadLockfile(lockPath)
	if err != nil {
		return err
	}

	matches := uniqueVersions(tree, target)
	if len(matches) == 0 {
		return fmt.Errorf("%s is not in phi.lock", target)
	}

	rootNames, err := readRootNames()
	if err != nil {
		return err
	}

	parents := buildReverseDeps(tree)

	for _, version := range matches {
		fmt.Printf("%s@%s\n", target, version)
		chains := findChains(target, parents, rootNames)
		if len(chains) == 0 {
			fmt.Println("  (orphan — no path from package.json roots)")
			continue
		}
		printChains(chains)
	}
	return nil
}

func uniqueVersions(tree *resolver.Tree, name string) []string {
	seen := map[string]bool{}
	var out []string
	for _, pkg := range tree.All {
		if pkg.Name == name && !seen[pkg.Version] {
			seen[pkg.Version] = true
			out = append(out, pkg.Version)
		}
	}
	sort.Strings(out)
	return out
}

func readRootNames() (map[string]bool, error) {
	body, err := os.ReadFile("package.json")
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	body = bytes.TrimPrefix(body, utf8BOM)
	rootNames := map[string]bool{}
	for _, key := range []string{"dependencies", "devDependencies"} {
		gjson.GetBytes(body, key).ForEach(func(k, v gjson.Result) bool {
			rootNames[k.String()] = true
			return true
		})
	}
	return rootNames, nil
}

func buildReverseDeps(tree *resolver.Tree) map[string][]string {
	parents := map[string]map[string]bool{}
	for _, pkg := range tree.All {
		for childName := range pkg.Deps {
			if parents[childName] == nil {
				parents[childName] = map[string]bool{}
			}
			parents[childName][pkg.Name] = true
		}
	}
	out := map[string][]string{}
	for child, set := range parents {
		for parent := range set {
			out[child] = append(out[child], parent)
		}
		sort.Strings(out[child])
	}
	return out
}

func findChains(target string, parents map[string][]string, rootNames map[string]bool) [][]string {
	var chains [][]string
	visited := map[string]bool{}
	var dfs func(name string, path []string)
	dfs = func(name string, path []string) {
		if visited[name] {
			return
		}
		visited[name] = true
		defer func() { visited[name] = false }()

		path = append(path, name)
		if rootNames[name] {
			c := make([]string, len(path))
			copy(c, path)
			chains = append(chains, c)
			return
		}
		for _, p := range parents[name] {
			dfs(p, path)
		}
	}
	dfs(target, nil)
	return chains
}

func printChains(chains [][]string) {
	// Sort for deterministic output and dedup identical chains.
	stringified := make([]string, 0, len(chains))
	seen := map[string]bool{}
	for _, c := range chains {
		// chains[i] runs target→…→root; reverse for human-readable root→target.
		reversed := make([]string, len(c))
		for i, n := range c {
			reversed[len(c)-1-i] = n
		}
		s := strings.Join(reversed, " > ")
		if !seen[s] {
			seen[s] = true
			stringified = append(stringified, s)
		}
	}
	sort.Strings(stringified)
	for _, s := range stringified {
		fmt.Printf("  %s\n", s)
	}
}
