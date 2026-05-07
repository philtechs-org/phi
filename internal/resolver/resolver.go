package resolver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/philtechs-org/phi/internal/registry"
)

type Pkg struct {
	Name          string
	Version       string
	Resolved      string
	Integrity     string
	Spec          string
	Deps          map[string]string
	PeerDeps      map[string]string // declared peer deps (name -> spec)
	OptionalPeers map[string]bool   // peers that are optional (warn-free if missing)

	// InstallPath is where this package will be extracted, relative to the
	// project root. For hoisted (root-level) installs: "node_modules/<name>".
	// For nested installs caused by version conflicts:
	// "node_modules/<consumer>/node_modules/<name>".
	InstallPath string

	declared map[string]string
}

type Tree struct {
	Roots    []*Pkg
	All      map[string]*Pkg // keyed by InstallPath — the canonical map
	Warnings []string
}

// Hoisted returns the package installed at the root level (node_modules/<name>),
// or nil if no version of name is at root. Most direct-dep + peer-dep checks
// should go through Hoisted.
func (t *Tree) Hoisted(name string) *Pkg {
	return t.All["node_modules/"+name]
}

func Resolve(client *registry.Client, direct map[string]string) (*Tree, error) {
	tree := &Tree{All: map[string]*Pkg{}}

	type job struct {
		name         string
		spec         string
		consumerPath string // installPath of consumer; "" for direct deps
	}
	queue := make([]job, 0, len(direct))
	rootSpecs := map[string]string{}
	for n, s := range direct {
		queue = append(queue, job{n, s, ""})
		rootSpecs[n] = s
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i].name < queue[j].name })

	pc := newPackumentCache(client, 16)
	// Seed: prefetch direct deps so the very first iteration doesn't block.
	directNames := make([]string, 0, len(direct))
	for n := range direct {
		directNames = append(directNames, n)
	}
	pc.Prefetch(directNames)

	for len(queue) > 0 {
		j := queue[0]
		queue = queue[1:]

		visiblePath := findVisible(tree, j.consumerPath, j.name)
		if visiblePath != "" {
			visible := tree.All[visiblePath]
			if specSatisfies(j.spec, visible.Version) {
				continue
			}
			// conflict — fall through to install a different version nested
			// under the consumer
		}

		pack, err := pc.Get(j.name)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", j.name, err)
		}

		version, err := pickVersion(pack, j.spec)
		if err != nil {
			tree.Warnings = append(tree.Warnings, fmt.Sprintf("%s@%s: %v", j.name, j.spec, err))
			continue
		}
		info, ok := pack.VersionInfo(version)
		if !ok {
			return nil, fmt.Errorf("%s: missing version info for %s", j.name, version)
		}

		installPath := computeInstallPath(tree, j.consumerPath, j.name)
		// If we'd nest under a consumer that itself isn't installed yet,
		// fall back to root. (Shouldn't happen given BFS order, but defensive.)
		if installPath != "node_modules/"+j.name && j.consumerPath != "" {
			if _, ok := tree.All[j.consumerPath]; !ok {
				installPath = "node_modules/" + j.name
			}
		}

		pkg := &Pkg{
			Name:          j.name,
			Version:       version,
			Resolved:      info.Tarball,
			Integrity:     info.Integrity,
			Spec:          j.spec,
			Deps:          map[string]string{},
			PeerDeps:      info.PeerDependencies,
			OptionalPeers: info.OptionalPeers,
			InstallPath:   installPath,
			declared:      map[string]string{},
		}
		for k, v := range info.Dependencies {
			pkg.declared[k] = v
		}
		tree.All[installPath] = pkg

		depNames := make([]string, 0, len(info.Dependencies))
		for n := range info.Dependencies {
			depNames = append(depNames, n)
		}
		sort.Strings(depNames)
		// Warm the cache for children we're about to walk into.
		pc.Prefetch(depNames)
		for _, n := range depNames {
			queue = append(queue, job{n, info.Dependencies[n], installPath})
		}
	}

	// Fill Deps with concrete versions: each child's resolved version comes
	// from the version visible to this package.
	for _, pkg := range tree.All {
		for childName := range pkg.declared {
			visiblePath := findVisible(tree, pkg.InstallPath, childName)
			if visiblePath == "" {
				continue
			}
			pkg.Deps[childName] = tree.All[visiblePath].Version
		}
	}

	for name := range rootSpecs {
		if pkg := tree.Hoisted(name); pkg != nil {
			tree.Roots = append(tree.Roots, pkg)
		}
	}
	sort.Slice(tree.Roots, func(i, j int) bool { return tree.Roots[i].Name < tree.Roots[j].Name })

	validatePeers(tree)

	return tree, nil
}

// findVisible walks the node-resolution chain from consumerPath up to the
// project root, returning the install path of the first package matching
// name (or "" if none).
//
//	consumerPath = "node_modules/A/node_modules/B" checks, in order:
//	   node_modules/A/node_modules/B/node_modules/<name>   (B's own deps)
//	   node_modules/A/node_modules/<name>                  (A's level)
//	   node_modules/<name>                                 (root)
func findVisible(tree *Tree, consumerPath, name string) string {
	base := consumerPath
	for {
		var candidate string
		if base == "" {
			candidate = "node_modules/" + name
		} else {
			candidate = base + "/node_modules/" + name
		}
		if _, ok := tree.All[candidate]; ok {
			return candidate
		}
		if base == "" {
			return ""
		}
		idx := strings.LastIndex(base, "/node_modules/")
		if idx < 0 {
			base = ""
		} else {
			base = base[:idx]
		}
	}
}

// computeInstallPath picks where a new package should live: at root if no
// other version of name lives there yet, otherwise nested under the consumer.
func computeInstallPath(tree *Tree, consumerPath, name string) string {
	rootPath := "node_modules/" + name
	if _, exists := tree.All[rootPath]; !exists {
		return rootPath
	}
	if consumerPath == "" {
		return rootPath
	}
	return consumerPath + "/node_modules/" + name
}

func validatePeers(tree *Tree) {
	paths := make([]string, 0, len(tree.All))
	for p := range tree.All {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		pkg := tree.All[p]
		peerNames := make([]string, 0, len(pkg.PeerDeps))
		for n := range pkg.PeerDeps {
			peerNames = append(peerNames, n)
		}
		sort.Strings(peerNames)
		for _, peerName := range peerNames {
			peerSpec := pkg.PeerDeps[peerName]
			provider := tree.Hoisted(peerName)
			if provider == nil {
				if !pkg.OptionalPeers[peerName] {
					tree.Warnings = append(tree.Warnings, fmt.Sprintf(
						"%s requires peer %s@%s but no provider found",
						pkg.Name, peerName, peerSpec,
					))
				}
				continue
			}
			if !specSatisfies(peerSpec, provider.Version) {
				tree.Warnings = append(tree.Warnings, fmt.Sprintf(
					"%s requires peer %s@%s but tree has %s@%s",
					pkg.Name, peerName, peerSpec, peerName, provider.Version,
				))
			}
		}
	}
}

func pickVersion(p *registry.Packument, spec string) (string, error) {
	if v := p.DistTag(spec); v != "" {
		return v, nil
	}
	if spec == "" {
		if v := p.DistTag("latest"); v != "" {
			return v, nil
		}
	}
	constraint, err := semver.NewConstraint(spec)
	if err != nil {
		return "", fmt.Errorf("invalid spec %q: %w", spec, err)
	}
	var candidates []*semver.Version
	for _, v := range p.Versions() {
		sv, err := semver.NewVersion(v)
		if err != nil {
			continue
		}
		if constraint.Check(sv) {
			candidates = append(candidates, sv)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no version matches %q", spec)
	}
	sort.Sort(semver.Collection(candidates))
	return candidates[len(candidates)-1].Original(), nil
}

// Satisfies reports whether the given concrete version satisfies the spec.
func Satisfies(spec, version string) bool {
	return specSatisfies(spec, version)
}

func specSatisfies(spec, version string) bool {
	if spec == "" {
		return true
	}
	sv, err := semver.NewVersion(version)
	if err != nil {
		return false
	}
	c, err := semver.NewConstraint(spec)
	if err != nil {
		return false
	}
	return c.Check(sv)
}
