// Package npmrc parses .npmrc config files and exposes the subset of fields
// phi cares about: the default registry, scoped registries, and bearer
// auth tokens. ${ENV_VAR} substitution is honored so projects can keep
// secrets out of committed .npmrc files.
package npmrc

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const DefaultRegistry = "https://registry.npmjs.org"

var envVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Config captures the registry + auth settings extracted from one or more
// .npmrc files.
type Config struct {
	Default    string
	Scoped     map[string]string // "@scope" -> registry URL
	AuthTokens map[string]string // "//host/path/" prefix -> token
}

// New returns a fresh config pointing at the public npm registry.
func New() *Config {
	return &Config{
		Default:    DefaultRegistry,
		Scoped:     map[string]string{},
		AuthTokens: map[string]string{},
	}
}

// LoadDefault reads .npmrc in standard order: $HOME/.npmrc first (least
// specific), then ./.npmrc (most specific) so project settings win.
// Missing files are not errors.
func LoadDefault() (*Config, error) {
	var paths []string
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".npmrc"))
	}
	paths = append(paths, ".npmrc")
	return Parse(paths...)
}

// Parse merges the given files into a single config. Later files override
// earlier ones for any duplicated key.
func Parse(paths ...string) (*Config, error) {
	cfg := New()
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := parseFile(p, cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
			return cfg, err
		}
	}
	return cfg, nil
}

func parseFile(path string, cfg *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := substituteEnv(strings.TrimSpace(line[eq+1:]))
		val = strings.Trim(val, `"`)
		applySetting(cfg, key, val)
	}
	return sc.Err()
}

func applySetting(cfg *Config, key, val string) {
	switch {
	case key == "registry":
		cfg.Default = val
	case strings.HasPrefix(key, "@") && strings.HasSuffix(key, ":registry"):
		scope := strings.TrimSuffix(key, ":registry")
		cfg.Scoped[scope] = val
	case strings.HasPrefix(key, "//") && strings.HasSuffix(key, ":_authToken"):
		prefix := strings.TrimSuffix(key, ":_authToken")
		cfg.AuthTokens[prefix] = val
	}
}

func substituteEnv(s string) string {
	return envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		m := envVarRe.FindStringSubmatch(match)
		return os.Getenv(m[1])
	})
}

// RegistryFor returns the URL to use for fetching the given package.
// Scoped packages (`@org/foo`) check Scoped[@org] first; everything else
// uses Default. Trailing slash stripped so callers can build paths cleanly.
func (c *Config) RegistryFor(packageName string) string {
	if strings.HasPrefix(packageName, "@") {
		if i := strings.Index(packageName, "/"); i > 0 {
			scope := packageName[:i]
			if r, ok := c.Scoped[scope]; ok {
				return strings.TrimRight(r, "/")
			}
		}
	}
	return strings.TrimRight(c.Default, "/")
}

// AuthHeaderFor returns the value for an Authorization header when the
// given URL falls under one of the configured auth-token prefixes; "" if
// no token applies. Longest-prefix match wins.
func (c *Config) AuthHeaderFor(url string) string {
	u := stripScheme(url)
	var bestKey, bestToken string
	for prefix, token := range c.AuthTokens {
		normalized := strings.TrimRight(prefix, "/")
		if matchesPrefix(u, normalized) && len(normalized) > len(bestKey) {
			bestKey = normalized
			bestToken = token
		}
	}
	if bestToken == "" {
		return ""
	}
	return "Bearer " + bestToken
}

func stripScheme(url string) string {
	if i := strings.Index(url, "://"); i >= 0 {
		return url[i+1:] // keep the leading "//"
	}
	return url
}

func matchesPrefix(u, prefix string) bool {
	return strings.HasPrefix(u, prefix)
}
