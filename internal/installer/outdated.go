package installer

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/Masterminds/semver/v3"
	"github.com/fatih/color"
	"github.com/philtechs-org/phi/internal/registry"
	"github.com/philtechs-org/phi/internal/resolver"
	"github.com/tidwall/gjson"
)

// Outdated lists root deps where the locked version, the highest version
// matching package.json's spec, or the registry's latest tag don't agree.
func Outdated() error {
	tree, err := ReadLockfile(lockPath)
	if err != nil {
		if errors.Is(err, ErrNoLockfile) {
			return errors.New("no phi.lock — run `phi install` first")
		}
		return err
	}

	pjBody, err := os.ReadFile("package.json")
	if err != nil {
		return fmt.Errorf("read package.json: %w", err)
	}
	pjBody = bytes.TrimPrefix(pjBody, utf8BOM)

	direct := map[string]string{}
	for _, key := range []string{"dependencies", "devDependencies"} {
		gjson.GetBytes(pjBody, key).ForEach(func(k, v gjson.Result) bool {
			direct[k.String()] = v.String()
			return true
		})
	}
	if len(direct) == 0 {
		fmt.Println("no dependencies in package.json")
		return nil
	}

	type row struct {
		name, current, wanted, latest string
		majorBump                     bool
	}
	var rows []row

	client := registry.New()
	for name, spec := range direct {
		pkg := tree.Hoisted(name)
		if pkg == nil {
			continue
		}
		pack, err := client.FetchPackument(name)
		if err != nil {
			continue
		}
		latest := pack.DistTag("latest")
		wanted := pickHighestMatching(pack, spec)
		if wanted == "" {
			wanted = pkg.Version
		}
		if pkg.Version == latest && pkg.Version == wanted {
			continue
		}
		rows = append(rows, row{
			name:      name,
			current:   pkg.Version,
			wanted:    wanted,
			latest:    latest,
			majorBump: latest != "" && wanted != "" && majorOf(latest) != majorOf(wanted),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	if len(rows) == 0 {
		fmt.Println("All dependencies up to date.")
		return nil
	}

	w := maxLen("PACKAGE", rows, func(r row) string { return r.name })
	cw := maxLen("CURRENT", rows, func(r row) string { return r.current })
	ww := maxLen("WANTED", rows, func(r row) string { return r.wanted })
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	fmt.Printf("%-*s  %-*s  %-*s  %s\n", w, "PACKAGE", cw, "CURRENT", ww, "WANTED", "LATEST")
	for _, r := range rows {
		latest := r.latest
		if r.majorBump {
			latest = red(latest)
		} else if r.current != r.wanted {
			latest = yellow(latest)
		}
		fmt.Printf("%-*s  %-*s  %-*s  %s\n", w, r.name, cw, r.current, ww, r.wanted, latest)
	}
	return nil
}

func maxLen[T any](header string, rows []T, get func(T) string) int {
	max := len(header)
	for _, r := range rows {
		if l := len(get(r)); l > max {
			max = l
		}
	}
	return max
}

func pickHighestMatching(pack *registry.Packument, spec string) string {
	if v := pack.DistTag(spec); v != "" {
		return v
	}
	if spec == "" || spec == "latest" {
		return pack.DistTag("latest")
	}
	var best *semver.Version
	for _, v := range pack.Versions() {
		if !resolver.Satisfies(spec, v) {
			continue
		}
		sv, err := semver.NewVersion(v)
		if err != nil {
			continue
		}
		if best == nil || sv.GreaterThan(best) {
			best = sv
		}
	}
	if best == nil {
		return ""
	}
	return best.Original()
}

func majorOf(version string) string {
	sv, err := semver.NewVersion(version)
	if err != nil {
		return version
	}
	return fmt.Sprintf("%d", sv.Major())
}
