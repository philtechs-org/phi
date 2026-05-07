package resolver_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/philtechs-org/phi/internal/registry"
	"github.com/philtechs-org/phi/internal/resolver"
)

type stubPkg struct {
	versions      map[string]map[string]string // version -> deps map
	peerVersions  map[string]map[string]string // version -> peer deps map (optional)
	optionalPeers map[string][]string          // version -> peer names marked optional (optional)
	distTags      map[string]string            // tag -> version
}

func newStub(t *testing.T, packages map[string]stubPkg) (*registry.Client, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		pkg, ok := packages[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		body, err := json.Marshal(buildPackument(name, pkg, "http://"+r.Host))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write(body)
	}))
	client := &registry.Client{
		BaseURL: server.URL,
		HTTP:    server.Client(),
	}
	return client, server.Close
}

func buildPackument(name string, p stubPkg, baseURL string) map[string]any {
	versions := make(map[string]any, len(p.versions))
	for v, deps := range p.versions {
		entry := map[string]any{
			"name":    name,
			"version": v,
			"dist": map[string]string{
				"tarball":   fmt.Sprintf("%s/tarball/%s/%s.tgz", baseURL, name, v),
				"integrity": "",
			},
			"dependencies": deps,
		}
		if peers, ok := p.peerVersions[v]; ok && len(peers) > 0 {
			entry["peerDependencies"] = peers
		}
		if opts, ok := p.optionalPeers[v]; ok && len(opts) > 0 {
			meta := map[string]any{}
			for _, name := range opts {
				meta[name] = map[string]any{"optional": true}
			}
			entry["peerDependenciesMeta"] = meta
		}
		versions[v] = entry
	}
	tags := p.distTags
	if tags == nil {
		// auto-set latest to highest-looking version key
		tags = map[string]string{}
		var newest string
		for v := range p.versions {
			if v > newest {
				newest = v
			}
		}
		tags["latest"] = newest
	}
	return map[string]any{
		"name":      name,
		"dist-tags": tags,
		"versions":  versions,
	}
}

func TestResolve_SimpleDirect(t *testing.T) {
	client, closeFn := newStub(t, map[string]stubPkg{
		"pkg-a": {versions: map[string]map[string]string{"1.0.0": {}}},
	})
	defer closeFn()

	tree, err := resolver.Resolve(client, map[string]string{"pkg-a": "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.All) != 1 {
		t.Fatalf("expected 1 pkg, got %d: %v", len(tree.All), tree.All)
	}
	if tree.Hoisted("pkg-a").Version != "1.0.0" {
		t.Errorf("got version %q", tree.Hoisted("pkg-a").Version)
	}
	if len(tree.Roots) != 1 {
		t.Errorf("expected 1 root, got %d", len(tree.Roots))
	}
}

func TestResolve_Transitive(t *testing.T) {
	client, closeFn := newStub(t, map[string]stubPkg{
		"a": {versions: map[string]map[string]string{"1.0.0": {"b": "1.0.0"}}},
		"b": {versions: map[string]map[string]string{"1.0.0": {"c": "1.0.0"}}},
		"c": {versions: map[string]map[string]string{"1.0.0": {}}},
	})
	defer closeFn()

	tree, err := resolver.Resolve(client, map[string]string{"a": "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.All) != 3 {
		t.Fatalf("expected 3 pkgs, got %d", len(tree.All))
	}
	if tree.Hoisted("a").Deps["b"] != "1.0.0" {
		t.Errorf("a should record b=1.0.0, got %v", tree.Hoisted("a").Deps)
	}
	if tree.Hoisted("b").Deps["c"] != "1.0.0" {
		t.Errorf("b should record c=1.0.0, got %v", tree.Hoisted("b").Deps)
	}
}

func TestResolve_CaretRangePicksHighestMatching(t *testing.T) {
	client, closeFn := newStub(t, map[string]stubPkg{
		"lib": {
			versions: map[string]map[string]string{
				"1.2.3": {},
				"1.5.0": {},
				"2.0.0": {},
			},
		},
	})
	defer closeFn()

	tree, err := resolver.Resolve(client, map[string]string{"lib": "^1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.Hoisted("lib").Version; got != "1.5.0" {
		t.Errorf("^1.2.3 should pick 1.5.0, got %s", got)
	}
}

func TestResolve_LatestDistTag(t *testing.T) {
	client, closeFn := newStub(t, map[string]stubPkg{
		"lib": {
			versions: map[string]map[string]string{
				"1.0.0": {},
				"2.0.0": {},
			},
			distTags: map[string]string{"latest": "1.0.0"}, // older marked as latest
		},
	})
	defer closeFn()

	tree, err := resolver.Resolve(client, map[string]string{"lib": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.Hoisted("lib").Version; got != "1.0.0" {
		t.Errorf("latest dist-tag should pick 1.0.0, got %s", got)
	}
}

func TestResolve_ConflictFirstWins(t *testing.T) {
	client, closeFn := newStub(t, map[string]stubPkg{
		"a": {versions: map[string]map[string]string{"1.0.0": {"shared": "1.0.0"}}},
		"b": {versions: map[string]map[string]string{"1.0.0": {"shared": "2.0.0"}}},
		"shared": {versions: map[string]map[string]string{
			"1.0.0": {},
			"2.0.0": {},
		}},
	})
	defer closeFn()

	tree, err := resolver.Resolve(client, map[string]string{"a": "1.0.0", "b": "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.All) != 4 {
		t.Errorf("expected 4 installs (a, b, shared@root, shared@nested-under-b), got %d", len(tree.All))
	}
	// alphabetical BFS: a's shared@1.0.0 hoists; b's shared@2.0.0 nests under b.
	if got := tree.Hoisted("shared").Version; got != "1.0.0" {
		t.Errorf("first-encountered should hoist shared@1.0.0, got %s", got)
	}
	nested := tree.All["node_modules/b/node_modules/shared"]
	if nested == nil {
		t.Errorf("expected shared@2.0.0 nested under b, but it isn't in the tree")
	} else if nested.Version != "2.0.0" {
		t.Errorf("nested shared should be 2.0.0, got %s", nested.Version)
	}
}

func TestResolve_PeerSatisfied(t *testing.T) {
	client, closeFn := newStub(t, map[string]stubPkg{
		"react":     {versions: map[string]map[string]string{"18.2.0": {}}},
		"react-dom": {
			versions:     map[string]map[string]string{"18.2.0": {}},
			peerVersions: map[string]map[string]string{"18.2.0": {"react": "^18.0.0"}},
		},
	})
	defer closeFn()

	tree, err := resolver.Resolve(client, map[string]string{
		"react":     "18.2.0",
		"react-dom": "18.2.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range tree.Warnings {
		if strings.Contains(w, "react") && strings.Contains(w, "peer") {
			t.Errorf("unexpected peer warning: %s", w)
		}
	}
}

func TestResolve_PeerMissingWarns(t *testing.T) {
	client, closeFn := newStub(t, map[string]stubPkg{
		"react-dom": {
			versions:     map[string]map[string]string{"18.2.0": {}},
			peerVersions: map[string]map[string]string{"18.2.0": {"react": "^18.0.0"}},
		},
	})
	defer closeFn()

	tree, err := resolver.Resolve(client, map[string]string{"react-dom": "18.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range tree.Warnings {
		if strings.Contains(w, "react-dom") && strings.Contains(w, "peer") && strings.Contains(w, "react") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected peer-missing warning for react, got %v", tree.Warnings)
	}
}

func TestResolve_PeerOptionalMissingSilent(t *testing.T) {
	client, closeFn := newStub(t, map[string]stubPkg{
		"plugin": {
			versions:      map[string]map[string]string{"1.0.0": {}},
			peerVersions:  map[string]map[string]string{"1.0.0": {"host": "^1.0.0"}},
			optionalPeers: map[string][]string{"1.0.0": {"host"}},
		},
	})
	defer closeFn()

	tree, err := resolver.Resolve(client, map[string]string{"plugin": "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range tree.Warnings {
		if strings.Contains(w, "host") {
			t.Errorf("optional peer should be silent, got %s", w)
		}
	}
}

func TestResolve_PeerVersionMismatchWarns(t *testing.T) {
	client, closeFn := newStub(t, map[string]stubPkg{
		"react":     {versions: map[string]map[string]string{"17.0.2": {}}},
		"react-dom": {
			versions:     map[string]map[string]string{"18.2.0": {}},
			peerVersions: map[string]map[string]string{"18.2.0": {"react": "^18.0.0"}},
		},
	})
	defer closeFn()

	tree, err := resolver.Resolve(client, map[string]string{
		"react":     "17.0.2",
		"react-dom": "18.2.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range tree.Warnings {
		if strings.Contains(w, "react-dom") && strings.Contains(w, "17.0.2") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected version-mismatch warning, got %v", tree.Warnings)
	}
}

func TestResolve_NonSemverSpecsSkipped(t *testing.T) {
	client, closeFn := newStub(t, map[string]stubPkg{
		"lib": {versions: map[string]map[string]string{"1.0.0": {}}},
	})
	defer closeFn()

	cases := []struct {
		name, spec string
	}{
		{"git URL", "git+https://github.com/x/y.git"},
		{"file URL", "file:./local-pkg"},
		{"tarball URL", "https://example.com/pkg.tgz"},
		{"github shorthand", "github:user/repo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree, err := resolver.Resolve(client, map[string]string{"lib": tc.spec})
			if err != nil {
				t.Fatalf("Resolve error: %v", err)
			}
			if tree.Hoisted("lib") != nil {
				t.Errorf("non-semver spec %q should be skipped, but lib is in tree", tc.spec)
			}
			if len(tree.Warnings) == 0 {
				t.Errorf("expected warning for non-semver spec %q", tc.spec)
			}
		})
	}
}

func TestResolve_UnresolvableSpec(t *testing.T) {
	client, closeFn := newStub(t, map[string]stubPkg{
		"lib": {versions: map[string]map[string]string{"1.0.0": {}}},
	})
	defer closeFn()

	tree, err := resolver.Resolve(client, map[string]string{"lib": "^99.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if tree.Hoisted("lib") != nil {
		t.Errorf("unresolvable spec should be skipped, but lib is in tree")
	}
	if len(tree.Warnings) == 0 {
		t.Errorf("expected warning for unresolvable spec")
	}
}
