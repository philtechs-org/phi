package installer

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/philtechs-org/phi/internal/registry"
)

func computeIntegrity(data []byte) string {
	h := sha512.Sum512(data)
	return "sha512-" + base64.StdEncoding.EncodeToString(h[:])
}

// stubRegistryServer serves a single package with the given tarball.
func stubRegistryServer(t *testing.T, pkgName, pkgVersion string, tarball []byte) *httptest.Server {
	t.Helper()
	integrity := computeIntegrity(tarball)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		switch {
		case r.URL.Path == "/"+pkgName:
			packument := map[string]any{
				"name":      pkgName,
				"dist-tags": map[string]string{"latest": pkgVersion},
				"versions": map[string]any{
					pkgVersion: map[string]any{
						"name":    pkgName,
						"version": pkgVersion,
						"dist": map[string]string{
							"tarball":   base + "/tarball",
							"integrity": integrity,
						},
						"dependencies": map[string]string{},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(packument)
		case r.URL.Path == "/tarball":
			w.Write(tarball)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestInstall_BlocksMaliciousPackage(t *testing.T) {
	tarball := makeRawTarball(t, []tarEntry{
		// 2 critical detectors → score 70 → BLOCKED
		{
			name: "package/index.js",
			body: `eval("payload"); var t = process.env.GITHUB_TOKEN;`,
		},
		{
			name: "package/package.json",
			body: `{"name":"evil-pkg","version":"1.0.0"}`,
		},
	})
	server := stubRegistryServer(t, "evil-pkg", "1.0.0", tarball)
	defer server.Close()

	client := &registry.Client{BaseURL: server.URL, HTTP: server.Client()}

	dir := t.TempDir()
	chdir(t, dir)

	err := install(client, []string{"evil-pkg"}, Options{})
	if err == nil {
		t.Fatal("expected install to fail (package was blocked)")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected error mentioning 'blocked', got: %v", err)
	}

	// Side-effect assertions: nothing extracted, lockfile not written, but report exists.
	if _, err := os.Stat("node_modules"); !os.IsNotExist(err) {
		t.Errorf("node_modules should not exist when install blocked")
	}
	if _, err := os.Stat("phi.lock"); !os.IsNotExist(err) {
		t.Errorf("phi.lock should not exist when blocked")
	}
	body, err := os.ReadFile("phi-report.json")
	if err != nil {
		t.Fatalf("phi-report.json should be written: %v", err)
	}
	if !strings.Contains(string(body), `"verdict": "blocked"`) {
		t.Errorf("expected blocked verdict in report, got:\n%s", body)
	}
}

func TestInstall_SafePackageSucceeds(t *testing.T) {
	tarball := makeRawTarball(t, []tarEntry{
		{name: "package/index.js", body: `module.exports = function add(a, b) { return a + b; };`},
		{name: "package/package.json", body: `{"name":"clean-pkg","version":"1.0.0"}`},
	})
	server := stubRegistryServer(t, "clean-pkg", "1.0.0", tarball)
	defer server.Close()

	client := &registry.Client{BaseURL: server.URL, HTTP: server.Client()}

	dir := t.TempDir()
	chdir(t, dir)

	if err := install(client, []string{"clean-pkg"}, Options{}); err != nil {
		t.Fatalf("install should succeed: %v", err)
	}
	for _, want := range []string{"phi.lock", "phi-report.json", "node_modules/clean-pkg/index.js"} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected %s, got error: %v", want, err)
		}
	}
}

func TestInstall_IntegrityMismatchAborts(t *testing.T) {
	realTarball := makeRawTarball(t, []tarEntry{
		{name: "package/index.js", body: `module.exports = 1;`},
	})
	tampered := append([]byte{}, realTarball...)
	tampered[len(tampered)-10] ^= 0xFF // corrupt a byte near the end

	// Server advertises the integrity of the original but serves the tampered bytes.
	integrity := computeIntegrity(realTarball)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		switch r.URL.Path {
		case "/tampered":
			packument := map[string]any{
				"name":      "tampered",
				"dist-tags": map[string]string{"latest": "1.0.0"},
				"versions": map[string]any{
					"1.0.0": map[string]any{
						"name":    "tampered",
						"version": "1.0.0",
						"dist": map[string]string{
							"tarball":   base + "/tarball",
							"integrity": integrity,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(packument)
		case "/tarball":
			w.Write(tampered)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &registry.Client{BaseURL: server.URL, HTTP: server.Client()}

	dir := t.TempDir()
	chdir(t, dir)

	err := install(client, []string{"tampered"}, Options{})
	if err == nil {
		t.Fatal("expected install to fail on integrity mismatch")
	}
	if !strings.Contains(err.Error(), "integrity") {
		t.Errorf("expected integrity error, got: %v", err)
	}
}
