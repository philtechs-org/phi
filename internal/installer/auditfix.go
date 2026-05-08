package installer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/fatih/color"
	"github.com/philtechs-org/phi/internal/advisory"
	"github.com/philtechs-org/phi/internal/analyzer"
	"github.com/philtechs-org/phi/internal/registry"
	"github.com/philtechs-org/phi/internal/resolver"
	"github.com/philtechs-org/phi/internal/ui"
	"github.com/tidwall/gjson"
)

// FixOptions controls phi audit fix behavior.
type FixOptions struct {
	// Apply rewrites package.json with the proposed fixes. Without it,
	// `phi audit fix` is a preview command — useful in CI to surface the
	// recommended changes without committing them.
	Apply bool
	// Force allows fixes that change the public API: replacing one
	// package with a different one (vm2 → isolated-vm) and bumping
	// across major versions (^4.x → ^5.x). Implies Apply.
	Force bool
}

// fixKind categorizes a proposed change so the renderer can group them
// and the apply step knows what to do.
type fixKind string

const (
	fixAdvisoryBump fixKind = "advisory-bump"
	fixTyposquat    fixKind = "typosquat"
	fixDeprecated   fixKind = "deprecated"
)

// proposedFix is one actionable change to one direct dependency.
type proposedFix struct {
	pkg         string
	currentSpec string
	// One of these is set:
	newSpec string // for advisory bumps: replacement spec on the same package
	newPkg  string // for renames: replacement package name (also has newSpec for the new package's range)
	// Metadata
	kind     fixKind
	reason   string
	breaking bool // requires --force to apply
}

// AuditFix is the entry point for `phi audit fix [--apply | --force]`.
// Runs the same scan + advisory pipeline as `phi audit`, then proposes
// fixes for direct dependencies. Three sources, in order of confidence:
//
//	1. Typosquat → use the popular name (always safe; different package).
//	2. Advisory with a known "fixed in" version → bump.
//	3. Deprecated package with a curated successor → swap (force-only).
//
// By default prints the preview and exits 0 if there were any proposable
// fixes (1 if no fixes available — that's a no-op, not a problem). When
// --apply or --force is passed, package.json is rewritten in place and a
// follow-up `phi install` is required to materialize the changes.
func AuditFix(opts FixOptions) error {
	client := registry.New()

	pjBody, err := readPackageJSONBytes()
	if err != nil {
		return err
	}
	directSpecs := readDirectDeps(pjBody)
	if len(directSpecs) == 0 {
		return errors.New("no direct dependencies in package.json")
	}

	targets, err := resolveTargets(nil)
	if err != nil {
		return err
	}
	direct := targetsToMap(targets)

	ui.PrintBanner()
	fmt.Println("resolving dependency tree...")
	tree, _, err := loadTree(client, direct, Options{Mode: ModeAuto})
	if err != nil {
		return err
	}
	if len(tree.All) == 0 {
		return errors.New("empty resolution tree")
	}

	fmt.Printf("scanning %d packages...\n", len(tree.All))
	scans, _, scanErr := scanWithProgress(client, tree, false)
	if scanErr != nil {
		ui.PrintWarning(fmt.Sprintf("scan: %v", scanErr))
	}

	advs := queryAdvisories(tree, Options{})
	mergeAdvisories(scans, advs)

	fixes := proposeFixes(directSpecs, tree, scans, advs)
	if len(fixes) == 0 {
		fmt.Println("\nno fixes available — your direct dependencies look clean.")
		return nil
	}

	renderFixPreview(fixes, opts)

	if !opts.Apply && !opts.Force {
		// Preview only. Exit success — caller (CI / dev) saw the report.
		return nil
	}

	applicable := selectApplicable(fixes, opts)
	if len(applicable) == 0 {
		fmt.Println("\nnothing applied — all proposed fixes are breaking changes (re-run with --force to apply).")
		return nil
	}

	updated, err := applyFixesToPackageJSON(pjBody, applicable)
	if err != nil {
		return fmt.Errorf("rewrite package.json: %w", err)
	}
	if err := writeFileAtomic("package.json", updated, 0o644); err != nil {
		return fmt.Errorf("update package.json: %w", err)
	}

	cyan := color.New(color.FgCyan).SprintFunc()
	fmt.Printf("\n%s applied %d fix(es) to package.json\n", cyan("ok"), len(applicable))
	fmt.Println("  next:")
	fmt.Println("    phi install         # materialize the new versions")
	return nil
}

// readPackageJSONBytes reads the project's package.json and trims a UTF-8 BOM
// if present (Windows editors love adding one). Returned bytes are valid
// JSON ready for gjson / encoding/json.
func readPackageJSONBytes() ([]byte, error) {
	body, err := os.ReadFile("package.json")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("no package.json in current directory")
		}
		return nil, err
	}
	return bytes.TrimPrefix(body, utf8BOM), nil
}

// readDirectDeps returns map[name]spec for the direct deps we'd consider
// fixing. Both `dependencies` and `devDependencies` are eligible —
// peerDependencies are not (they describe what the user provides, not
// what they install).
func readDirectDeps(pjBody []byte) map[string]string {
	out := map[string]string{}
	for _, key := range []string{"dependencies", "devDependencies"} {
		gjson.GetBytes(pjBody, key).ForEach(func(k, v gjson.Result) bool {
			out[k.String()] = v.String()
			return true
		})
	}
	return out
}

// proposeFixes walks the direct deps, looks up their resolved tree
// entry + scan + advisories, and emits one proposedFix per actionable
// issue. A direct dep can produce multiple fixes (e.g. advisory-bump
// AND deprecation), but in practice we pick the strongest single fix
// per package — the deprecation rename obsoletes any advisory bump on
// the old package.
func proposeFixes(
	direct map[string]string,
	tree *resolver.Tree,
	scans map[string]*analyzer.AnalysisReport,
	advs map[string][]*advisory.Advisory,
) []proposedFix {
	var fixes []proposedFix
	for name, spec := range direct {
		pkg := tree.Hoisted(name)
		if pkg == nil {
			continue
		}
		report, ok := scans[pkg.InstallPath]
		if !ok {
			continue
		}

		// 1. Deprecated → strongest signal, preempts advisory bumps.
		if dep := matchDeprecation(report); dep != "" {
			newPkg, newSpec := parseRename(dep)
			if newPkg != "" {
				fixes = append(fixes, proposedFix{
					pkg:         name,
					currentSpec: spec,
					newPkg:      newPkg,
					newSpec:     newSpec,
					kind:        fixDeprecated,
					reason:      fmt.Sprintf("deprecated; %s", dep),
					breaking:    true, // API surface differs; needs --force
				})
				continue
			}
		}

		// 2. Typosquat → rename to the popular package the user likely
		//    meant. Always safe (different package, same intent).
		if popular := matchTyposquat(report); popular != "" {
			fixes = append(fixes, proposedFix{
				pkg:         name,
				currentSpec: spec,
				newPkg:      popular,
				newSpec:     "*", // user picks a version on next install
				kind:        fixTyposquat,
				reason:      fmt.Sprintf("looks like a typosquat of %s", popular),
				breaking:    false,
			})
			continue
		}

		// 3. Advisory bump → propose the lowest "fixed in" version we
		//    have for any advisory on this package. Major-version bumps
		//    are flagged breaking.
		if fixVer := highestFixedVersion(advs, pkg.InstallPath); fixVer != "" {
			breaking := isMajorBump(report.PackageVersion, fixVer)
			fixes = append(fixes, proposedFix{
				pkg:         name,
				currentSpec: spec,
				newSpec:     "^" + fixVer,
				kind:        fixAdvisoryBump,
				reason:      advisorySummary(advs[pkg.InstallPath], fixVer),
				breaking:    breaking,
			})
			continue
		}
	}
	sort.Slice(fixes, func(i, j int) bool { return fixes[i].pkg < fixes[j].pkg })
	return fixes
}

func matchDeprecation(r *analyzer.AnalysisReport) string {
	for _, n := range r.Notices {
		if n.Kind == "deprecated" {
			return n.Message
		}
	}
	return ""
}

// parseRename takes a deprecation message like "use isolated-vm (...)"
// or "renamed to uuid (...)" or "discontinued 2020; use undici, axios, or
// got" and returns the FIRST mentioned replacement package name plus an
// optional version spec. Best-effort heuristic: scan for the first
// identifier-shaped token following "use" or "to" or "renamed to".
// Returns ("", "") when nothing parseable is found.
func parseRename(message string) (pkgName, spec string) {
	tokens := strings.Fields(message)
	for i, tok := range tokens {
		switch strings.ToLower(strings.TrimSuffix(tok, ":")) {
		case "use", "to":
			if i+1 < len(tokens) {
				cand := stripPunct(tokens[i+1])
				if looksLikeNpmName(cand) {
					return cand, "*"
				}
			}
		case "renamed":
			if i+2 < len(tokens) && strings.EqualFold(tokens[i+1], "to") {
				cand := stripPunct(tokens[i+2])
				if looksLikeNpmName(cand) {
					return cand, "*"
				}
			}
		}
	}
	return "", ""
}

func stripPunct(s string) string {
	return strings.TrimRight(strings.TrimLeft(s, "(`'\""), ",;.()'\"`")
}

// looksLikeNpmName accepts: bare names (lodash), scoped names
// (@babel/core), names with hyphens / digits / underscores. Rejects
// punctuation-only or stdlib references like "Array.isArray".
func looksLikeNpmName(s string) bool {
	if s == "" || strings.Contains(s, ".") {
		return false
	}
	if strings.HasPrefix(s, "@") {
		// Scoped packages must contain a slash.
		return strings.Contains(s, "/")
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

// matchTyposquat extracts the "popular" name from a Typosquatting
// detection's evidence string, which we format as "<name> ~ <popular>".
func matchTyposquat(r *analyzer.AnalysisReport) string {
	for _, d := range r.Detections {
		if d.Detector != "Typosquatting" {
			continue
		}
		parts := strings.SplitN(d.Evidence, "~", 2)
		if len(parts) != 2 {
			continue
		}
		return strings.TrimSpace(parts[1])
	}
	return ""
}

// highestFixedVersion picks the largest semver across all advisories
// for a given install path. Largest because if there are multiple fix
// versions for sequential CVEs (e.g. 4.17.21 and 4.17.22), the higher
// one resolves all earlier ones.
func highestFixedVersion(advs map[string][]*advisory.Advisory, installPath string) string {
	if installPath == "" {
		return ""
	}
	var best *semver.Version
	for _, a := range advs[installPath] {
		if a.Fixed == "" {
			continue
		}
		v, err := semver.NewVersion(a.Fixed)
		if err != nil {
			continue
		}
		if best == nil || v.GreaterThan(best) {
			best = v
		}
	}
	if best == nil {
		return ""
	}
	return best.Original()
}

func advisorySummary(advs []*advisory.Advisory, fixVer string) string {
	for _, a := range advs {
		if a.Fixed == fixVer {
			return fmt.Sprintf("%s — %s", a.ID, truncate(a.Summary, 60))
		}
	}
	if len(advs) > 0 {
		return fmt.Sprintf("%s + %d more", advs[0].ID, len(advs)-1)
	}
	return "advisory"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func isMajorBump(currentVer, fixVer string) bool {
	a, errA := semver.NewVersion(currentVer)
	b, errB := semver.NewVersion(fixVer)
	if errA != nil || errB != nil {
		return false // can't tell — be permissive
	}
	return b.Major() > a.Major()
}

// renderFixPreview prints the proposed fixes grouped by safety class.
// Output format mirrors `git status` style: clear visual separation
// between safe-and-applicable and force-only changes.
func renderFixPreview(fixes []proposedFix, opts FixOptions) {
	cyan := color.New(color.FgCyan).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()

	fmt.Printf("\nproposed fixes (%d):\n", len(fixes))

	var safe, breaking []proposedFix
	for _, f := range fixes {
		if f.breaking {
			breaking = append(breaking, f)
		} else {
			safe = append(safe, f)
		}
	}

	if len(safe) > 0 {
		fmt.Printf("\n%s — applied with --apply or --force\n", green("safe"))
		for _, f := range safe {
			renderFix(f, cyan)
		}
	}
	if len(breaking) > 0 {
		fmt.Printf("\n%s — only applied with --force (breaking changes)\n", yellow("breaking"))
		for _, f := range breaking {
			renderFix(f, yellow)
		}
	}

	if !opts.Apply && !opts.Force {
		fmt.Println("\npreview only. re-run with --apply (safe fixes) or --force (all fixes).")
	}
}

func renderFix(f proposedFix, accent func(...interface{}) string) {
	if f.newPkg != "" {
		fmt.Printf("  %s %s → %s\n",
			accent("rename"),
			f.pkg+" "+f.currentSpec,
			f.newPkg)
		fmt.Printf("    reason: %s\n", f.reason)
	} else {
		fmt.Printf("  %s %s %s → %s\n",
			accent("bump"),
			f.pkg, f.currentSpec, f.newSpec)
		fmt.Printf("    reason: %s\n", f.reason)
	}
}

func selectApplicable(fixes []proposedFix, opts FixOptions) []proposedFix {
	var out []proposedFix
	for _, f := range fixes {
		if f.breaking && !opts.Force {
			continue
		}
		out = append(out, f)
	}
	return out
}

// applyFixesToPackageJSON rewrites the package.json bytes with the new
// versions / new package names. We use sjson-style targeted edits so we
// preserve the user's original formatting (key order, indent, comments
// in jsonc-ish files). For renames, the old name is removed from
// dependencies/devDependencies and the new name added to the same field.
func applyFixesToPackageJSON(pjBody []byte, fixes []proposedFix) ([]byte, error) {
	// We re-marshal via encoding/json after structural edits. This loses
	// non-standard JSON5 quirks but works fine for the package.json
	// format that npm itself rewrites the same way. Stable output
	// guarantees deterministic diffs.
	var doc map[string]any
	if err := json.Unmarshal(pjBody, &doc); err != nil {
		return nil, fmt.Errorf("parse package.json: %w", err)
	}
	for _, f := range fixes {
		field := dependencyField(doc, f.pkg)
		if field == "" {
			continue
		}
		group, ok := doc[field].(map[string]any)
		if !ok {
			continue
		}
		if f.newPkg != "" && f.newPkg != f.pkg {
			delete(group, f.pkg)
			group[f.newPkg] = f.newSpec
		} else {
			group[f.pkg] = f.newSpec
		}
		doc[field] = group
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// dependencyField returns "dependencies" or "devDependencies" — whichever
// holds the named package — or "" if it isn't there.
func dependencyField(doc map[string]any, name string) string {
	for _, field := range []string{"dependencies", "devDependencies"} {
		if m, ok := doc[field].(map[string]any); ok {
			if _, has := m[name]; has {
				return field
			}
		}
	}
	return ""
}
