package advisory

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestQuery_FlowsBatchAndDetails(t *testing.T) {
	var batchCalls, detailCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/querybatch":
			batchCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []any{
					map[string]any{"vulns": []any{map[string]string{"id": "GHSA-x-1"}}},
					map[string]any{"vulns": []any{}},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/v1/vulns/"):
			detailCalls.Add(1)
			id := strings.TrimPrefix(r.URL.Path, "/v1/vulns/")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      id,
				"summary": "test advisory " + id,
				"aliases": []string{"CVE-2024-0000"},
				"database_specific": map[string]string{
					"severity": "HIGH",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := New()
	c.Endpoint = server.URL

	pkgs := []Pkg{{Name: "evil", Version: "1.0.0"}, {Name: "safe", Version: "1.0.0"}}
	results, err := c.Query(pkgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 pkg with advisories, got %d: %v", len(results), results)
	}
	advs := results[Pkg{Name: "evil", Version: "1.0.0"}]
	if len(advs) != 1 || advs[0].ID != "GHSA-x-1" || advs[0].Severity != SeverityHigh {
		t.Errorf("unexpected advisory: %+v", advs)
	}
	if advs[0].Reference != "https://osv.dev/vulnerability/GHSA-x-1" {
		t.Errorf("reference: %s", advs[0].Reference)
	}
}

func TestQuery_EmptyInput(t *testing.T) {
	c := New()
	got, err := c.Query(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestQuery_DedupsDetailFetch(t *testing.T) {
	var detailCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/querybatch":
			// Both packages report the same vuln ID.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []any{
					map[string]any{"vulns": []any{map[string]string{"id": "GHSA-shared"}}},
					map[string]any{"vulns": []any{map[string]string{"id": "GHSA-shared"}}},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/v1/vulns/"):
			detailCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "GHSA-shared",
				"summary": "shared",
				"database_specific": map[string]string{"severity": "MODERATE"},
			})
		}
	}))
	defer server.Close()

	c := New()
	c.Endpoint = server.URL
	_, err := c.Query([]Pkg{{Name: "a", Version: "1"}, {Name: "b", Version: "2"}})
	if err != nil {
		t.Fatal(err)
	}
	if detailCalls.Load() != 1 {
		t.Errorf("shared vuln should be fetched once, got %d detail calls", detailCalls.Load())
	}
}

func TestNormalizeSeverity(t *testing.T) {
	cases := map[string]Severity{
		"CRITICAL":   SeverityCritical,
		"critical":   SeverityCritical,
		"HIGH":       SeverityHigh,
		"MODERATE":   SeverityModerate,
		"medium":     SeverityModerate,
		"LOW":        SeverityLow,
		"":           SeverityUnknown,
		"  ":         SeverityUnknown,
		"WHATEVER":   SeverityUnknown,
	}
	for in, want := range cases {
		if got := normalizeSeverity(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
