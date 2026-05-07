package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscover_NoWorkspaces(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"name":"app","version":"1.0.0"}`)

	ws, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ws != nil {
		t.Errorf("expected nil for non-monorepo, got %v", ws)
	}
}

func TestDiscover_ArrayForm(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{
		"name": "monorepo",
		"workspaces": ["packages/*"]
	}`)
	mustMkdir(t, filepath.Join(dir, "packages", "utils"))
	writeFile(t, filepath.Join(dir, "packages", "utils", "package.json"), `{"name":"@org/utils","version":"1.0.0","dependencies":{"lodash":"^4"}}`)
	mustMkdir(t, filepath.Join(dir, "packages", "app"))
	writeFile(t, filepath.Join(dir, "packages", "app", "package.json"), `{"name":"@org/app","version":"1.0.0","dependencies":{"@org/utils":"*","express":"^4"}}`)

	ws, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(ws))
	}
	if ws[0].Name != "@org/app" || ws[1].Name != "@org/utils" {
		t.Errorf("unexpected workspace order: %s, %s", ws[0].Name, ws[1].Name)
	}
}

func TestDiscover_ObjectForm(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{
		"workspaces": {"packages": ["apps/*"]}
	}`)
	mustMkdir(t, filepath.Join(dir, "apps", "web"))
	writeFile(t, filepath.Join(dir, "apps", "web", "package.json"), `{"name":"web","version":"1.0.0"}`)

	ws, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 1 || ws[0].Name != "web" {
		t.Errorf("expected web workspace, got %v", ws)
	}
}

func TestAggregate_RemovesSiblings(t *testing.T) {
	rootDeps := map[string]string{"lodash": "^4"}
	workspaces := []*Workspace{
		{Name: "@org/utils", Dir: "packages/utils", Dependencies: map[string]string{"chalk": "^4"}},
		{Name: "@org/app", Dir: "packages/app", Dependencies: map[string]string{"@org/utils": "*", "express": "^4"}},
	}
	deps, siblings := Aggregate(rootDeps, workspaces)

	if !siblings["@org/utils"] || !siblings["@org/app"] {
		t.Errorf("siblings should include both workspaces: %v", siblings)
	}
	if _, has := deps["@org/utils"]; has {
		t.Errorf("sibling ref @org/utils should not be in resolver deps: %v", deps)
	}
	for _, want := range []string{"lodash", "chalk", "express"} {
		if _, has := deps[want]; !has {
			t.Errorf("deps missing %s: %v", want, deps)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
