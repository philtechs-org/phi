package installer

import (
	"github.com/philtechs-org/phi/internal/advisory"
	"github.com/philtechs-org/phi/internal/analyzer"
	"github.com/philtechs-org/phi/internal/resolver"
	"github.com/philtechs-org/phi/internal/scorer"
	"github.com/philtechs-org/phi/internal/ui"
)

// queryAdvisories asks osv.dev for known vulnerabilities affecting any
// package in the resolved tree. Returns a map keyed by install path.
// Returns an empty map (never nil) so callers don't have to nil-check.
// Network failures are non-fatal — they print a warning and yield no data.
func queryAdvisories(tree *resolver.Tree, opts Options) map[string][]*advisory.Advisory {
	out := map[string][]*advisory.Advisory{}
	if opts.NoAdvisories {
		return out
	}
	if len(tree.All) == 0 {
		return out
	}

	type pathPkg struct {
		path string
		pkg  advisory.Pkg
	}
	items := make([]pathPkg, 0, len(tree.All))
	pkgs := make([]advisory.Pkg, 0, len(tree.All))
	for path, p := range tree.All {
		ap := advisory.Pkg{Name: p.Name, Version: p.Version}
		items = append(items, pathPkg{path, ap})
		pkgs = append(pkgs, ap)
	}

	client := advisory.New()
	results, err := client.Query(pkgs)
	if err != nil {
		if !opts.JSON {
			ui.PrintWarning("advisory query failed: " + err.Error() + " — proceeding without advisory data")
		}
		return out
	}
	for _, it := range items {
		if advs := results[it.pkg]; len(advs) > 0 {
			out[it.path] = advs
		}
	}
	return out
}

// mergeAdvisories adds advisory severity points into each scan's risk score
// and recomputes the verdict. Detector points + advisory points combine and
// cap at 100 (delegated to scorer).
func mergeAdvisories(scans map[string]*analyzer.AnalysisReport, advs map[string][]*advisory.Advisory) {
	for path, r := range scans {
		bonus := 0
		for _, a := range advs[path] {
			bonus += advisoryPoints(a.Severity)
		}
		if bonus == 0 {
			continue
		}
		score := r.RiskScore + bonus
		if score > 100 {
			score = 100
		}
		r.RiskScore = score
		r.Verdict = scorer.Verdict(score)
	}
}

// advisoryPoints maps OSV severity labels to the same point scale phi uses
// for its detectors. UNKNOWN is treated as LOW so an unflagged-severity
// advisory still nudges the score upward.
func advisoryPoints(s advisory.Severity) int {
	switch s {
	case advisory.SeverityCritical:
		return 35
	case advisory.SeverityHigh:
		return 20
	case advisory.SeverityModerate:
		return 10
	case advisory.SeverityLow, advisory.SeverityUnknown:
		return 5
	}
	return 5
}
