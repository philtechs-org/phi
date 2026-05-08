package installer

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/philtechs-org/phi/internal/advisory"
	"github.com/philtechs-org/phi/internal/analyzer"
	"github.com/philtechs-org/phi/internal/resolver"
)

func TestParseRename(t *testing.T) {
	cases := []struct {
		msg, wantPkg string
	}{
		{"use isolated-vm (V8-isolate-based)", "isolated-vm"},
		{"discontinued 2020; use undici, axios, or got", "undici"},
		{"renamed to uuid (the same package)", "uuid"},
		{"use String.prototype.padStart (built into Node)", ""}, // stdlib hint, not an npm package
		{"use Array.isArray (stdlib)", ""},
		{"migrate to eslint with @typescript-eslint", "eslint"},
		{"renamed to @babel/eslint-parser", "@babel/eslint-parser"},
		{"project archived (was a request dep)", ""}, // no replacement
	}
	for _, c := range cases {
		gotPkg, _ := parseRename(c.msg)
		if gotPkg != c.wantPkg {
			t.Errorf("parseRename(%q) pkg = %q, want %q", c.msg, gotPkg, c.wantPkg)
		}
	}
}

func TestLooksLikeNpmName(t *testing.T) {
	cases := map[string]bool{
		"lodash":              true,
		"isolated-vm":         true,
		"@babel/eslint-parser": true,
		"@scope/foo":          true,
		"foo_bar":             true,
		"foo123":              true,
		"":                    false,
		"Array.isArray":       false,
		"String.prototype":    false,
		"@noslash":            false, // scoped names need a slash
		"foo!bar":             false,
		"foo bar":             false,
	}
	for in, want := range cases {
		if got := looksLikeNpmName(in); got != want {
			t.Errorf("looksLikeNpmName(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestMatchTyposquat(t *testing.T) {
	r := &analyzer.AnalysisReport{
		Detections: []analyzer.DetectionResult{
			{Detector: "Typosquatting", Evidence: "loadsh ~ lodash"},
		},
	}
	if got := matchTyposquat(r); got != "lodash" {
		t.Errorf("matchTyposquat() = %q, want %q", got, "lodash")
	}
}

func TestMatchDeprecation(t *testing.T) {
	r := &analyzer.AnalysisReport{
		Notices: []analyzer.Notice{
			{Kind: "deprecated", Message: "use isolated-vm (V8-isolate-based)"},
		},
	}
	got := matchDeprecation(r)
	if !strings.Contains(got, "isolated-vm") {
		t.Errorf("matchDeprecation() = %q, want to contain isolated-vm", got)
	}
}

func TestHighestFixedVersionPicksMaxSemver(t *testing.T) {
	advs := map[string][]*advisory.Advisory{
		"node_modules/lodash": {
			{ID: "GHSA-aaa", Fixed: "4.17.20"},
			{ID: "GHSA-bbb", Fixed: "4.17.21"}, // higher
			{ID: "GHSA-ccc", Fixed: ""},        // ignored
		},
	}
	got := highestFixedVersion(advs, "node_modules/lodash")
	if got != "4.17.21" {
		t.Errorf("highestFixedVersion() = %q, want 4.17.21", got)
	}
}

func TestHighestFixedVersionEmpty(t *testing.T) {
	advs := map[string][]*advisory.Advisory{
		"node_modules/x": {
			{ID: "GHSA-aaa", Fixed: ""},
		},
	}
	if got := highestFixedVersion(advs, "node_modules/x"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestIsMajorBump(t *testing.T) {
	cases := []struct {
		cur, fix string
		want     bool
	}{
		{"4.17.20", "4.17.21", false}, // patch
		{"4.17.20", "4.18.0", false},  // minor
		{"4.17.20", "5.0.0", true},    // major
		{"3.10.4", "3.11.2", false},   // minor
		{"3.11.2", "4.0.0", true},     // major
	}
	for _, c := range cases {
		if got := isMajorBump(c.cur, c.fix); got != c.want {
			t.Errorf("isMajorBump(%q, %q) = %v, want %v", c.cur, c.fix, got, c.want)
		}
	}
}

func TestProposeFixesTyposquat(t *testing.T) {
	tree := stubTree("loadsh", "1.0.0")
	scans := map[string]*analyzer.AnalysisReport{
		"node_modules/loadsh": {
			PackageName:    "loadsh",
			PackageVersion: "1.0.0",
			Detections: []analyzer.DetectionResult{
				{Detector: "Typosquatting", Evidence: "loadsh ~ lodash"},
			},
		},
	}
	fixes := proposeFixes(map[string]string{"loadsh": "^1.0.0"}, tree, scans, nil)
	if len(fixes) != 1 {
		t.Fatalf("got %d fixes, want 1", len(fixes))
	}
	f := fixes[0]
	if f.kind != fixTyposquat || f.newPkg != "lodash" || f.breaking {
		t.Errorf("fix = %+v, want typosquat → lodash, not breaking", f)
	}
}

func TestProposeFixesDeprecation(t *testing.T) {
	tree := stubTree("vm2", "3.11.2")
	scans := map[string]*analyzer.AnalysisReport{
		"node_modules/vm2": {
			PackageName:    "vm2",
			PackageVersion: "3.11.2",
			Notices: []analyzer.Notice{
				{Kind: "deprecated", Message: "use isolated-vm (V8-isolate-based)"},
			},
		},
	}
	fixes := proposeFixes(map[string]string{"vm2": "^3.10.0"}, tree, scans, nil)
	if len(fixes) != 1 {
		t.Fatalf("got %d fixes, want 1", len(fixes))
	}
	f := fixes[0]
	if f.kind != fixDeprecated || f.newPkg != "isolated-vm" || !f.breaking {
		t.Errorf("fix = %+v, want deprecated → isolated-vm, breaking=true", f)
	}
}

func TestProposeFixesAdvisoryBump(t *testing.T) {
	tree := stubTree("lodash", "4.17.20")
	scans := map[string]*analyzer.AnalysisReport{
		"node_modules/lodash": {PackageName: "lodash", PackageVersion: "4.17.20"},
	}
	advs := map[string][]*advisory.Advisory{
		"node_modules/lodash": {
			{ID: "GHSA-xxx", Fixed: "4.17.21", Summary: "Prototype pollution"},
		},
	}
	fixes := proposeFixes(map[string]string{"lodash": "^4.17.0"}, tree, scans, advs)
	if len(fixes) != 1 {
		t.Fatalf("got %d fixes, want 1", len(fixes))
	}
	f := fixes[0]
	if f.kind != fixAdvisoryBump || f.newSpec != "^4.17.21" || f.breaking {
		t.Errorf("fix = %+v, want advisory bump → ^4.17.21, not breaking", f)
	}
}

func TestProposeFixesAdvisoryMajorBumpIsBreaking(t *testing.T) {
	tree := stubTree("oldlib", "1.5.0")
	scans := map[string]*analyzer.AnalysisReport{
		"node_modules/oldlib": {PackageName: "oldlib", PackageVersion: "1.5.0"},
	}
	advs := map[string][]*advisory.Advisory{
		"node_modules/oldlib": {
			{ID: "GHSA-zzz", Fixed: "2.0.0", Summary: "RCE"},
		},
	}
	fixes := proposeFixes(map[string]string{"oldlib": "^1.0.0"}, tree, scans, advs)
	if len(fixes) != 1 || !fixes[0].breaking {
		t.Errorf("major-version bump should be breaking; got %+v", fixes)
	}
}

func TestSelectApplicableSafeOnly(t *testing.T) {
	fixes := []proposedFix{
		{pkg: "a", breaking: false},
		{pkg: "b", breaking: true},
	}
	got := selectApplicable(fixes, FixOptions{Apply: true})
	if len(got) != 1 || got[0].pkg != "a" {
		t.Errorf("safe-only filter failed: %+v", got)
	}
	got = selectApplicable(fixes, FixOptions{Force: true})
	if len(got) != 2 {
		t.Errorf("force should include breaking: %+v", got)
	}
}

func TestApplyFixesToPackageJSONRename(t *testing.T) {
	pj := []byte(`{"name":"app","dependencies":{"vm2":"^3.10.0"}}`)
	updated, err := applyFixesToPackageJSON(pj, []proposedFix{
		{pkg: "vm2", currentSpec: "^3.10.0", newPkg: "isolated-vm", newSpec: "*", kind: fixDeprecated, breaking: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(updated, &doc); err != nil {
		t.Fatal(err)
	}
	deps := doc["dependencies"].(map[string]any)
	if _, has := deps["vm2"]; has {
		t.Error("vm2 should be removed")
	}
	if got, ok := deps["isolated-vm"]; !ok || got != "*" {
		t.Errorf("isolated-vm spec = %v, want *", got)
	}
}

func TestApplyFixesToPackageJSONBump(t *testing.T) {
	pj := []byte(`{"name":"app","dependencies":{"lodash":"^4.17.0"}}`)
	updated, err := applyFixesToPackageJSON(pj, []proposedFix{
		{pkg: "lodash", currentSpec: "^4.17.0", newSpec: "^4.17.21", kind: fixAdvisoryBump},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(updated, &doc); err != nil {
		t.Fatal(err)
	}
	deps := doc["dependencies"].(map[string]any)
	if got := deps["lodash"]; got != "^4.17.21" {
		t.Errorf("lodash = %v, want ^4.17.21", got)
	}
}

func TestApplyFixesToPackageJSONDevDependency(t *testing.T) {
	pj := []byte(`{"name":"app","devDependencies":{"vm2":"^3.10.0"}}`)
	updated, err := applyFixesToPackageJSON(pj, []proposedFix{
		{pkg: "vm2", newPkg: "isolated-vm", newSpec: "*", kind: fixDeprecated, breaking: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	json.Unmarshal(updated, &doc)
	devDeps := doc["devDependencies"].(map[string]any)
	if _, has := devDeps["isolated-vm"]; !has {
		t.Errorf("expected isolated-vm in devDependencies, got %v", devDeps)
	}
}

func TestReadDirectDeps(t *testing.T) {
	pj := []byte(`{
		"name": "app",
		"dependencies": {"a": "1.0.0", "b": "^2.0.0"},
		"devDependencies": {"c": "~3.0.0"},
		"peerDependencies": {"d": "*"}
	}`)
	got := readDirectDeps(pj)
	want := map[string]string{"a": "1.0.0", "b": "^2.0.0", "c": "~3.0.0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v (peerDependencies excluded)", got, want)
	}
}

// stubTree builds a minimal *resolver.Tree where the named package is
// hoisted at the canonical install path. Sufficient for the
// proposeFixes flow which only needs Hoisted + InstallPath + version.
func stubTree(name, version string) *resolver.Tree {
	pkg := &resolver.Pkg{
		Name:        name,
		Version:     version,
		InstallPath: "node_modules/" + name,
	}
	return &resolver.Tree{
		All: map[string]*resolver.Pkg{pkg.InstallPath: pkg},
	}
}
