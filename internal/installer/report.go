package installer

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/philtechs-org/phi/internal/advisory"
	"github.com/philtechs-org/phi/internal/analyzer"
	"github.com/philtechs-org/phi/internal/scorer"
)

type detectionJSON struct {
	Detector    string `json:"detector"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
	Evidence    string `json:"evidence"`
	File        string `json:"file,omitempty"`
}

type advisoryJSON struct {
	ID        string   `json:"id"`
	Severity  string   `json:"severity"`
	Summary   string   `json:"summary"`
	Aliases   []string `json:"aliases,omitempty"`
	Reference string   `json:"reference"`
}

type noticeJSON struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type packageJSON struct {
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	FilesScanned int             `json:"filesScanned"`
	Score        int             `json:"score"`
	Verdict      string          `json:"verdict"`
	Detections   []detectionJSON `json:"detections"`
	Advisories   []advisoryJSON  `json:"advisories,omitempty"`
	Notices      []noticeJSON    `json:"notices,omitempty"`
}

type summaryJSON struct {
	Total      int `json:"total"`
	Safe       int `json:"safe"`
	Review     int `json:"review"`
	Blocked    int `json:"blocked"`
	Advisories int `json:"advisories"`
}

type reportJSON struct {
	SchemaVersion int           `json:"schemaVersion"`
	ScannedAt     string        `json:"scannedAt"`
	Summary       summaryJSON   `json:"summary"`
	Packages      []packageJSON `json:"packages"`
}

// WriteReport persists phi-report.json. scans is keyed by install path;
// advs (optional, may be nil) is keyed the same way.
func WriteReport(path string, scans map[string]*analyzer.AnalysisReport, advs map[string][]*advisory.Advisory) error {
	pkgs := make([]packageJSON, 0, len(scans))
	var sum summaryJSON
	for installPath, r := range scans {
		sum.Total++
		switch r.Verdict {
		case scorer.VerdictSafe:
			sum.Safe++
		case scorer.VerdictReview:
			sum.Review++
		case scorer.VerdictBlocked:
			sum.Blocked++
		}
		dets := make([]detectionJSON, 0, len(r.Detections))
		for _, d := range r.Detections {
			dets = append(dets, detectionJSON{
				Detector:    d.Detector,
				Severity:    string(d.Severity),
				Description: d.Description,
				Evidence:    d.Evidence,
				File:        d.File,
			})
		}
		var advList []advisoryJSON
		if a := advs[installPath]; len(a) > 0 {
			sum.Advisories += len(a)
			advList = make([]advisoryJSON, 0, len(a))
			for _, x := range a {
				advList = append(advList, advisoryJSON{
					ID:        x.ID,
					Severity:  string(x.Severity),
					Summary:   x.Summary,
					Aliases:   x.Aliases,
					Reference: x.Reference,
				})
			}
		}
		var notList []noticeJSON
		if len(r.Notices) > 0 {
			notList = make([]noticeJSON, 0, len(r.Notices))
			for _, n := range r.Notices {
				notList = append(notList, noticeJSON{Kind: n.Kind, Message: n.Message})
			}
		}
		pkgs = append(pkgs, packageJSON{
			Name:         r.PackageName,
			Version:      r.PackageVersion,
			FilesScanned: r.FilesScanned,
			Score:        r.RiskScore,
			Verdict:      r.Verdict,
			Detections:   dets,
			Advisories:   advList,
			Notices:      notList,
		})
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })

	rep := reportJSON{
		SchemaVersion: 1,
		ScannedAt:     time.Now().UTC().Format(time.RFC3339),
		Summary:       sum,
		Packages:      pkgs,
	}
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(body, '\n'), 0o644)
}
