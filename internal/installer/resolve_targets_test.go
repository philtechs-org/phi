package installer

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// withTempCwd writes pkgJSON to a fresh tmpdir, chdirs into it for the test,
// and restores cwd on cleanup. resolveTargets reads package.json from the
// current working directory, so each test needs its own.
func withTempCwd(t *testing.T, pkgJSON string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func names(ts []target) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.name
	}
	sort.Strings(out)
	return out
}

func TestResolveTargets_IncludesDevDepsByDefault(t *testing.T) {
	withTempCwd(t, `{
  "name": "x",
  "dependencies": {"lodash": "^4.17.21"},
  "devDependencies": {"eslint": "^9.0.0", "typescript": "^5.0.0"}
}`)
	got, err := resolveTargets(nil, Options{})
	if err != nil {
		t.Fatalf("resolveTargets: %v", err)
	}
	want := []string{"eslint", "lodash", "typescript"}
	if g := names(got); !equalSlices(g, want) {
		t.Errorf("got %v, want %v", g, want)
	}
}

func TestResolveTargets_OmitDevSkipsDevDeps(t *testing.T) {
	withTempCwd(t, `{
  "name": "x",
  "dependencies": {"lodash": "^4.17.21"},
  "devDependencies": {"eslint": "^9.0.0", "typescript": "^5.0.0"}
}`)
	got, err := resolveTargets(nil, Options{OmitDev: true})
	if err != nil {
		t.Fatalf("resolveTargets: %v", err)
	}
	want := []string{"lodash"}
	if g := names(got); !equalSlices(g, want) {
		t.Errorf("got %v, want %v (devDependencies should be skipped)", g, want)
	}
}

func TestResolveTargets_OmitDevWithArgsStillPicksUpArgs(t *testing.T) {
	// Explicit args (e.g. `phi install --omit=dev chalk@5.0.0`) should be
	// included regardless of OmitDev — the user named them explicitly.
	withTempCwd(t, `{
  "name": "x",
  "dependencies": {"lodash": "^4.17.21"},
  "devDependencies": {"eslint": "^9.0.0"}
}`)
	got, err := resolveTargets([]string{"chalk@^5.0.0"}, Options{OmitDev: true})
	if err != nil {
		t.Fatalf("resolveTargets: %v", err)
	}
	want := []string{"chalk", "lodash"}
	if g := names(got); !equalSlices(g, want) {
		t.Errorf("got %v, want %v", g, want)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
