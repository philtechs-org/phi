package installer

import (
	"encoding/json"
	"os"
	"testing"
)

func TestPackageJSONSpec(t *testing.T) {
	cases := []struct {
		name                     string
		userSpec, resolved, want string
		exact                    bool
	}{
		{"empty → caret", "", "4.1.2", "^4.1.2", false},
		{"latest → caret", "latest", "4.1.2", "^4.1.2", false},
		{"bare → caret", "4.1.2", "4.1.2", "^4.1.2", false},
		{"caret preserved", "^4.1.0", "4.1.2", "^4.1.0", false},
		{"tilde preserved", "~4.1.0", "4.1.2", "~4.1.0", false},
		{"range preserved", ">=4.0 <5", "4.1.2", ">=4.0 <5", false},
		{"prerelease bare", "5.0.0-beta.1", "5.0.0-beta.1", "^5.0.0-beta.1", false},
		// --save-exact
		{"exact pins resolved", "4.1.2", "4.1.2", "4.1.2", true},
		{"exact ignores user range", "^4.1.0", "4.1.2", "4.1.2", true},
		{"exact pins from latest", "latest", "4.1.2", "4.1.2", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := packageJSONSpec(tc.userSpec, tc.resolved, tc.exact); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUpsertDependency_CreatesPackageJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := upsertDependency("chalk", "^4.1.2", ""); err != nil {
		t.Fatal(err)
	}
	pj := readPackageJSON(t)
	if pj.Dependencies["chalk"] != "^4.1.2" {
		t.Errorf("got %v, want chalk=^4.1.2", pj.Dependencies)
	}
}

func TestUpsertDependency_AddsToDependencies(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeJSON(t, "package.json", `{"name":"app","dependencies":{"existing":"^1.0.0"}}`)

	if err := upsertDependency("chalk", "^4.1.2", ""); err != nil {
		t.Fatal(err)
	}
	pj := readPackageJSON(t)
	if pj.Dependencies["chalk"] != "^4.1.2" {
		t.Errorf("missing chalk: %v", pj.Dependencies)
	}
	if pj.Dependencies["existing"] != "^1.0.0" {
		t.Errorf("clobbered existing dep: %v", pj.Dependencies)
	}
	if pj.Name != "app" {
		t.Errorf("clobbered name field: got %q", pj.Name)
	}
}

func TestUpsertDependency_PreservesDevDependencyLocation(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeJSON(t, "package.json", `{"devDependencies":{"jest":"^28.0.0"}}`)

	if err := upsertDependency("jest", "^29.0.0", ""); err != nil {
		t.Fatal(err)
	}
	pj := readPackageJSON(t)
	if pj.DevDependencies["jest"] != "^29.0.0" {
		t.Errorf("jest should be updated in devDependencies: %v", pj.DevDependencies)
	}
	if _, ok := pj.Dependencies["jest"]; ok {
		t.Errorf("jest should not have been added to dependencies")
	}
}

func TestUpsertDependency_OverwritesExistingSpec(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeJSON(t, "package.json", `{"dependencies":{"chalk":"^4.0.0"}}`)

	if err := upsertDependency("chalk", "^5.0.0", ""); err != nil {
		t.Fatal(err)
	}
	pj := readPackageJSON(t)
	if pj.Dependencies["chalk"] != "^5.0.0" {
		t.Errorf("expected ^5.0.0, got %q", pj.Dependencies["chalk"])
	}
}

// --save-dev moves chalk from dependencies → devDependencies.
func TestUpsertDependency_SaveDevMoves(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeJSON(t, "package.json", `{"dependencies":{"chalk":"^4.0.0"}}`)

	if err := upsertDependency("chalk", "^4.1.2", "devDependencies"); err != nil {
		t.Fatal(err)
	}
	pj := readPackageJSON(t)
	if pj.DevDependencies["chalk"] != "^4.1.2" {
		t.Errorf("chalk should be in devDependencies: %v", pj)
	}
	if _, ok := pj.Dependencies["chalk"]; ok {
		t.Errorf("chalk should have moved out of dependencies: %v", pj.Dependencies)
	}
}

// --save-peer adds to peerDependencies.
func TestUpsertDependency_SavePeerNew(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeJSON(t, "package.json", `{}`)

	if err := upsertDependency("react", "^18.0.0", "peerDependencies"); err != nil {
		t.Fatal(err)
	}
	pj := readPackageJSON(t)
	if pj.PeerDependencies["react"] != "^18.0.0" {
		t.Errorf("react should be in peerDependencies: %v", pj)
	}
}

type packageJSONFile struct {
	Name             string            `json:"name"`
	Dependencies     map[string]string `json:"dependencies"`
	DevDependencies  map[string]string `json:"devDependencies"`
	PeerDependencies map[string]string `json:"peerDependencies"`
}

func readPackageJSON(t *testing.T) packageJSONFile {
	t.Helper()
	body, err := os.ReadFile("package.json")
	if err != nil {
		t.Fatal(err)
	}
	var pj packageJSONFile
	if err := json.Unmarshal(body, &pj); err != nil {
		t.Fatal(err)
	}
	return pj
}

func writeJSON(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
