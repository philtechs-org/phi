// Package advisory queries the OSV (Open Source Vulnerabilities) database
// at osv.dev to flag known-vulnerable npm packages. OSV aggregates GHSA
// (GitHub), OpenSSF malicious-packages, and CVE feeds into a single API.
//
// Two-step protocol:
//   1. POST /v1/querybatch with (name, version) tuples → list of vuln IDs per package
//   2. GET /v1/vulns/{id} per unique ID → severity, summary, references
//
// Detail responses are cached in-process so a tree where 50 packages share
// the same advisory only triggers one detail fetch. Network failures are
// non-fatal — the caller logs a warning and continues without advisory data.
package advisory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const DefaultEndpoint = "https://api.osv.dev"

// Severity follows OSV's database_specific.severity convention (the GHSA
// flavor): LOW / MODERATE / HIGH / CRITICAL. UNKNOWN means the advisory
// didn't carry a severity field.
type Severity string

const (
	SeverityUnknown  Severity = "UNKNOWN"
	SeverityLow      Severity = "LOW"
	SeverityModerate Severity = "MODERATE"
	SeverityHigh     Severity = "HIGH"
	SeverityCritical Severity = "CRITICAL"
)

// Advisory describes one vulnerability against one package version.
//
// Fixed is the minimum semver where this advisory's "fixed" event lands
// (parsed from affected[].ranges[].events[].fixed in the OSV detail
// response). Empty string when OSV doesn't carry a fix version (some
// CVEs are advisory-only; some have ranges instead of fixed events).
// Used by `phi audit fix` to propose the safe upgrade target.
type Advisory struct {
	ID        string   `json:"id"`
	Summary   string   `json:"summary"`
	Severity  Severity `json:"severity"`
	Aliases   []string `json:"aliases,omitempty"`
	Reference string   `json:"reference"`
	Fixed     string   `json:"fixed,omitempty"`
}

// Pkg identifies a package + version we want to query.
type Pkg struct {
	Name    string
	Version string
}

// Client queries the OSV API.
type Client struct {
	Endpoint string
	HTTP     *http.Client

	mu         sync.Mutex
	detailCache map[string]*Advisory
}

func New() *Client {
	return &Client{
		Endpoint:    DefaultEndpoint,
		HTTP:        &http.Client{Timeout: 30 * time.Second},
		detailCache: map[string]*Advisory{},
	}
}

// Query returns all advisories for the given packages, keyed by Pkg.
// Packages without advisories are absent from the result map. A network
// error returns (nil, err); the caller decides whether to abort or warn.
func (c *Client) Query(pkgs []Pkg) (map[Pkg][]*Advisory, error) {
	if len(pkgs) == 0 {
		return map[Pkg][]*Advisory{}, nil
	}
	idsPerPkg, err := c.queryBatch(pkgs)
	if err != nil {
		return nil, err
	}

	// Collect unique IDs.
	idSet := map[string]bool{}
	for _, ids := range idsPerPkg {
		for _, id := range ids {
			idSet[id] = true
		}
	}

	// Fetch details for each unique ID (cached).
	for id := range idSet {
		if _, err := c.detailFor(id); err != nil {
			// One bad ID shouldn't kill the whole batch.
			continue
		}
	}

	out := map[Pkg][]*Advisory{}
	for pkg, ids := range idsPerPkg {
		for _, id := range ids {
			adv, err := c.detailFor(id)
			if err != nil || adv == nil {
				continue
			}
			out[pkg] = append(out[pkg], adv)
		}
	}
	return out, nil
}

func (c *Client) queryBatch(pkgs []Pkg) (map[Pkg][]string, error) {
	type query struct {
		Package struct {
			Name      string `json:"name"`
			Ecosystem string `json:"ecosystem"`
		} `json:"package"`
		Version string `json:"version"`
	}
	type body struct {
		Queries []query `json:"queries"`
	}

	queries := make([]query, len(pkgs))
	for i, p := range pkgs {
		queries[i].Package.Name = p.Name
		queries[i].Package.Ecosystem = "npm"
		queries[i].Version = p.Version
	}

	buf, err := json.Marshal(body{Queries: queries})
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Post(c.Endpoint+"/v1/querybatch", "application/json", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("querybatch: %s", resp.Status)
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Results []struct {
			Vulns []struct {
				ID string `json:"id"`
			} `json:"vulns"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("querybatch parse: %w", err)
	}
	if len(parsed.Results) != len(pkgs) {
		return nil, fmt.Errorf("querybatch: result count %d != %d queries", len(parsed.Results), len(pkgs))
	}

	out := map[Pkg][]string{}
	for i, r := range parsed.Results {
		if len(r.Vulns) == 0 {
			continue
		}
		ids := make([]string, 0, len(r.Vulns))
		for _, v := range r.Vulns {
			ids = append(ids, v.ID)
		}
		out[pkgs[i]] = ids
	}
	return out, nil
}

func (c *Client) detailFor(id string) (*Advisory, error) {
	c.mu.Lock()
	if a, ok := c.detailCache[id]; ok {
		c.mu.Unlock()
		return a, nil
	}
	c.mu.Unlock()

	resp, err := c.HTTP.Get(c.Endpoint + "/v1/vulns/" + id)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vuln %s: %s", id, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var raw struct {
		ID               string   `json:"id"`
		Summary          string   `json:"summary"`
		Aliases          []string `json:"aliases"`
		DatabaseSpecific struct {
			Severity string `json:"severity"`
		} `json:"database_specific"`
		// affected[] tells us which ecosystems / versions are vulnerable
		// and (importantly for `phi audit fix`) the version where the
		// vulnerability is fixed. The same advisory often spans multiple
		// ecosystems (npm + Packagist for shared crypto libs); we only
		// care about npm here.
		Affected []struct {
			Package struct {
				Name      string `json:"name"`
				Ecosystem string `json:"ecosystem"`
			} `json:"package"`
			Ranges []struct {
				Type   string `json:"type"`
				Events []struct {
					Introduced string `json:"introduced,omitempty"`
					Fixed      string `json:"fixed,omitempty"`
				} `json:"events"`
			} `json:"ranges"`
		} `json:"affected"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("vuln %s parse: %w", id, err)
	}
	adv := &Advisory{
		ID:        raw.ID,
		Summary:   raw.Summary,
		Severity:  normalizeSeverity(raw.DatabaseSpecific.Severity),
		Aliases:   raw.Aliases,
		Reference: "https://osv.dev/vulnerability/" + raw.ID,
		Fixed:     extractFixedVersion(raw.Affected),
	}

	c.mu.Lock()
	c.detailCache[id] = adv
	c.mu.Unlock()
	return adv, nil
}

// extractFixedVersion scans the OSV affected[] entries for npm packages
// and returns a single representative "fixed in" version. We pick the
// FIRST fixed event we encounter on an npm-ecosystem range — OSV may
// list multiple ranges for legacy-major-line backports (e.g. "fixed in
// 3.x at 3.10.5 and in 4.x at 4.2.0"), but `phi audit fix` proposes
// "the simplest safe upgrade", so the first hit is good enough. Returns
// empty string when no fix version is documented.
func extractFixedVersion(affected []struct {
	Package struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	} `json:"package"`
	Ranges []struct {
		Type   string `json:"type"`
		Events []struct {
			Introduced string `json:"introduced,omitempty"`
			Fixed      string `json:"fixed,omitempty"`
		} `json:"events"`
	} `json:"ranges"`
}) string {
	for _, a := range affected {
		if !strings.EqualFold(a.Package.Ecosystem, "npm") {
			continue
		}
		for _, r := range a.Ranges {
			for _, e := range r.Events {
				if e.Fixed != "" {
					return e.Fixed
				}
			}
		}
	}
	return ""
}

func normalizeSeverity(s string) Severity {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "CRITICAL":
		return SeverityCritical
	case "HIGH":
		return SeverityHigh
	case "MODERATE", "MEDIUM":
		return SeverityModerate
	case "LOW":
		return SeverityLow
	default:
		return SeverityUnknown
	}
}
