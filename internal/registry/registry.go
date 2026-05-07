package registry

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/philtechs-org/phi/internal/npmrc"
	"github.com/tidwall/gjson"
)

const DefaultBaseURL = "https://registry.npmjs.org"

type Client struct {
	BaseURL string
	HTTP    *http.Client
	// Config, if non-nil, overrides BaseURL on a per-package basis (scoped
	// registries) and adds Authorization headers for matched URL prefixes.
	Config *npmrc.Config
}

type Packument struct {
	Name string
	Raw  []byte
}

type VersionInfo struct {
	Version              string
	Tarball              string
	Integrity            string
	Dependencies         map[string]string
	PeerDependencies     map[string]string
	OptionalPeers        map[string]bool // names listed in peerDependenciesMeta with optional=true
}

func New() *Client {
	cfg, _ := npmrc.LoadDefault()
	return &Client{
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
		Config:  cfg,
	}
}

// registryURL returns the base URL to use for fetching the given package.
// Honors scoped registries from .npmrc when Config is set.
func (c *Client) registryURL(name string) string {
	if c.Config != nil {
		return c.Config.RegistryFor(name)
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func (c *Client) doGet(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if c.Config != nil {
		if h := c.Config.AuthHeaderFor(url); h != "" {
			req.Header.Set("Authorization", h)
		}
	}
	return c.HTTP.Do(req)
}

func (c *Client) FetchPackument(name string) (*Packument, error) {
	url := c.registryURL(name) + "/" + name
	resp, err := c.doGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("packument %s: %s", name, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &Packument{Name: name, Raw: body}, nil
}

func (c *Client) FetchTarball(url string) ([]byte, error) {
	resp, err := c.doGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tarball %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func (p *Packument) DistTag(tag string) string {
	return gjson.GetBytes(p.Raw, "dist-tags."+escapeKey(tag)).String()
}

func (p *Packument) Versions() []string {
	var out []string
	gjson.GetBytes(p.Raw, "versions").ForEach(func(k, _ gjson.Result) bool {
		out = append(out, k.String())
		return true
	})
	return out
}

func (p *Packument) VersionInfo(v string) (*VersionInfo, bool) {
	base := "versions." + escapeKey(v)
	if !gjson.GetBytes(p.Raw, base).Exists() {
		return nil, false
	}
	info := &VersionInfo{
		Version:          v,
		Tarball:          gjson.GetBytes(p.Raw, base+".dist.tarball").String(),
		Integrity:        gjson.GetBytes(p.Raw, base+".dist.integrity").String(),
		Dependencies:     map[string]string{},
		PeerDependencies: map[string]string{},
		OptionalPeers:    map[string]bool{},
	}
	gjson.GetBytes(p.Raw, base+".dependencies").ForEach(func(k, val gjson.Result) bool {
		info.Dependencies[k.String()] = val.String()
		return true
	})
	gjson.GetBytes(p.Raw, base+".peerDependencies").ForEach(func(k, val gjson.Result) bool {
		info.PeerDependencies[k.String()] = val.String()
		return true
	})
	gjson.GetBytes(p.Raw, base+".peerDependenciesMeta").ForEach(func(k, val gjson.Result) bool {
		if val.Get("optional").Bool() {
			info.OptionalPeers[k.String()] = true
		}
		return true
	})
	return info, true
}

// VerifyIntegrity compares data against an npm-style integrity string
// of the form "sha512-<base64>" (or "sha1-<base64>" for older packages).
// An empty expected string is a no-op so callers can pass through packuments
// that lack integrity metadata.
func VerifyIntegrity(data []byte, expected string) error {
	if expected == "" {
		return nil
	}
	parts := strings.SplitN(expected, "-", 2)
	if len(parts) != 2 {
		return fmt.Errorf("malformed integrity: %s", expected)
	}
	algo, want := parts[0], parts[1]
	var got string
	switch algo {
	case "sha512":
		h := sha512.Sum512(data)
		got = base64.StdEncoding.EncodeToString(h[:])
	case "sha1":
		h := sha1.Sum(data)
		got = base64.StdEncoding.EncodeToString(h[:])
	default:
		return fmt.Errorf("unsupported integrity algo: %s", algo)
	}
	if got != want {
		return fmt.Errorf("integrity mismatch (%s)", algo)
	}
	return nil
}

func escapeKey(s string) string {
	out := make([]byte, 0, len(s)+4)
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, '\\', '.')
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}
