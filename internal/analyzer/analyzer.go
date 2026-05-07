package analyzer

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"strings"
)

const maxFileBytes = 2 << 20 // 2 MiB per file

func Analyze(packageName, packageVersion string, tarball io.Reader) (*AnalysisReport, error) {
	report := &AnalysisReport{
		PackageName:    packageName,
		PackageVersion: packageVersion,
	}

	gz, err := gzip.NewReader(tarball)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	seen := map[string]bool{}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if !shouldScan(hdr.Name) {
			continue
		}
		report.FilesScanned++

		body, err := io.ReadAll(io.LimitReader(tr, maxFileBytes))
		if err != nil {
			continue
		}
		ctx := detectionContext{packageName: packageName, file: hdr.Name}
		runDetectors(string(body), ctx, seen, &report.Detections)
	}

	if d, ok := checkTyposquat(packageName); ok {
		report.Detections = append(report.Detections, d)
	}

	return report, nil
}

func shouldScan(name string) bool {
	lower := strings.ToLower(name)
	// TypeScript declaration files contain only type signatures — no
	// executable code. Skipping them eliminates a whole class of false
	// positives on @types/* packages (e.g. node/child_process.d.ts
	// literally documents child_process.spawn).
	for _, decl := range []string{".d.ts", ".d.mts", ".d.cts"} {
		if strings.HasSuffix(lower, decl) {
			return false
		}
	}
	for _, ext := range []string{".js", ".cjs", ".mjs", ".ts", ".mts", ".cts", ".json", ".sh", ".bash"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func runDetectors(content string, ctx detectionContext, seen map[string]bool, out *[]DetectionResult) {
	for _, d := range detectors {
		if seen[d.name] {
			continue
		}
		if d.matcher != nil {
			evidence, ok := d.matcher(content, ctx)
			if !ok {
				continue
			}
			seen[d.name] = true
			if len(evidence) > 80 {
				evidence = evidence[:80] + "..."
			}
			*out = append(*out, DetectionResult{
				Detector:    d.name,
				Severity:    d.severity,
				Description: d.description,
				Evidence:    evidence,
				File:        ctx.file,
			})
			continue
		}
		for _, re := range d.patterns {
			loc := re.FindStringIndex(content)
			if loc == nil {
				continue
			}
			seen[d.name] = true
			evidence := content[loc[0]:loc[1]]
			if len(evidence) > 80 {
				evidence = evidence[:80] + "..."
			}
			*out = append(*out, DetectionResult{
				Detector:    d.name,
				Severity:    d.severity,
				Description: d.description,
				Evidence:    evidence,
				File:        ctx.file,
			})
			break
		}
	}
}

var popularPackages = []string{
	"lodash", "express", "axios", "react", "vue", "moment", "chalk",
	"commander", "debug", "request", "underscore", "async", "minimist",
	"uuid", "yargs", "webpack", "babel", "typescript", "jest", "mocha",
}

func checkTyposquat(name string) (DetectionResult, bool) {
	lower := strings.ToLower(name)
	for _, pop := range popularPackages {
		if lower == pop {
			return DetectionResult{}, false
		}
		// Distance == 1 only: real typosquats are usually a single
		// adjacent-key swap, char insert, or char delete. Distance-2
		// matches produce too many false positives on legitimate short
		// names (fecha~mocha, ret~react).
		if levenshtein(lower, pop) == 1 {
			return DetectionResult{
				Detector:    "Typosquatting",
				Severity:    SeverityHigh,
				Description: "Package name is suspiciously close to a popular package",
				Evidence:    name + " ~ " + pop,
			}, true
		}
	}
	return DetectionResult{}, false
}

func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
