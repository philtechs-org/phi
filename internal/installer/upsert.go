package installer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Masterminds/semver/v3"
)

// packageJSONSpec converts the user-provided version spec and the resolved
// concrete version into the spec we should persist to package.json.
//
//   - exact == true: bare resolvedVersion, no operator (--save-exact)
//   - "" or "latest": "^<resolvedVersion>" (npm default)
//   - bare exact ("4.1.2"): "^4.1.2" (npm convention)
//   - explicit range (^/~/>=): preserved as the user typed it
func packageJSONSpec(userSpec, resolvedVersion string, exact bool) string {
	if exact {
		return resolvedVersion
	}
	if userSpec == "" || userSpec == "latest" {
		return "^" + resolvedVersion
	}
	if _, err := semver.StrictNewVersion(userSpec); err == nil {
		return "^" + userSpec
	}
	return userSpec
}

// upsertDependency writes name=spec into package.json. If targetField is
// non-empty (e.g. "devDependencies"), the package is moved/added to that
// field — and removed from any other dependency field it may have lived
// in, matching npm's --save-dev / --save-peer semantics.
//
// If targetField is empty, the package is updated in place: kept in
// devDependencies / peerDependencies if it already lives there, otherwise
// added to dependencies.
//
// If package.json doesn't exist, a minimal one is created. Other fields
// (name, version, scripts, …) are preserved untouched (modulo encoding/json's
// alphabetical key ordering).
func upsertDependency(name, spec, targetField string) error {
	var data map[string]any
	body, err := os.ReadFile("package.json")
	switch {
	case err == nil:
		body = bytes.TrimPrefix(body, utf8BOM)
		if err := json.Unmarshal(body, &data); err != nil {
			return fmt.Errorf("parse package.json: %w", err)
		}
	case os.IsNotExist(err):
		data = map[string]any{}
	default:
		return err
	}

	field := targetField
	if field != "" {
		// Explicit save: clear any other dep field that already has this name.
		for _, k := range []string{"dependencies", "devDependencies", "peerDependencies"} {
			if k == field {
				continue
			}
			if deps, ok := data[k].(map[string]any); ok {
				delete(deps, name)
			}
		}
	} else {
		// Implicit: keep wherever it already lives.
		field = "dependencies"
		for _, k := range []string{"devDependencies", "peerDependencies"} {
			if deps, ok := data[k].(map[string]any); ok {
				if _, in := deps[name]; in {
					field = k
					break
				}
			}
		}
	}

	deps, _ := data[field].(map[string]any)
	if deps == nil {
		deps = map[string]any{}
		data[field] = deps
	}
	deps[name] = spec

	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("package.json", append(out, '\n'), 0o644)
}
