package npmrc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_DefaultRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".npmrc")
	mustWrite(t, path, `registry=https://my-registry.example.com/`)

	cfg, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.RegistryFor("any-pkg"); got != "https://my-registry.example.com" {
		t.Errorf("got %q", got)
	}
}

func TestParse_ScopedRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".npmrc")
	mustWrite(t, path, `
registry=https://registry.npmjs.org/
@my-org:registry=https://npm.pkg.github.com/
`)
	cfg, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"@my-org/private": "https://npm.pkg.github.com",
		"@my-org/utils":   "https://npm.pkg.github.com",
		"public-pkg":      "https://registry.npmjs.org",
		"@other/pkg":      "https://registry.npmjs.org",
	}
	for pkg, want := range cases {
		if got := cfg.RegistryFor(pkg); got != want {
			t.Errorf("%s: got %q want %q", pkg, got, want)
		}
	}
}

func TestParse_AuthToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".npmrc")
	mustWrite(t, path, `//npm.pkg.github.com/:_authToken=ghp_secret123`)

	cfg, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		url, want string
	}{
		{"https://npm.pkg.github.com/@org/pkg", "Bearer ghp_secret123"},
		{"http://npm.pkg.github.com/foo/-/foo-1.0.0.tgz", "Bearer ghp_secret123"},
		{"https://registry.npmjs.org/lodash", ""},
	}
	for _, tc := range cases {
		if got := cfg.AuthHeaderFor(tc.url); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.url, got, tc.want)
		}
	}
}

func TestParse_EnvVarSubstitution(t *testing.T) {
	t.Setenv("PHI_TEST_TOKEN", "supersecret")
	dir := t.TempDir()
	path := filepath.Join(dir, ".npmrc")
	mustWrite(t, path, `//private.example.com/:_authToken=${PHI_TEST_TOKEN}`)

	cfg, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.AuthHeaderFor("https://private.example.com/foo"); got != "Bearer supersecret" {
		t.Errorf("got %q", got)
	}
}

func TestParse_CommentsIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".npmrc")
	mustWrite(t, path, `
# this is a comment
; this is also a comment
registry=https://r.example.com/
`)
	cfg, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.RegistryFor("x"); got != "https://r.example.com" {
		t.Errorf("got %q", got)
	}
}

func TestParse_LaterFileOverrides(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "home.npmrc")
	b := filepath.Join(dir, "project.npmrc")
	mustWrite(t, a, `registry=https://home.example.com/`)
	mustWrite(t, b, `registry=https://project.example.com/`)

	cfg, err := Parse(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.RegistryFor("x"); got != "https://project.example.com" {
		t.Errorf("project should override home, got %q", got)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
