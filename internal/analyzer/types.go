package analyzer

type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

type DetectionResult struct {
	Detector    string
	Severity    Severity
	Description string
	Evidence    string
	File        string
}

// Notice is an informational annotation on a package — no score impact,
// no verdict change. Used for things the user should know about but
// that aren't malicious in themselves: deprecated packages with known
// replacements, packages with a heavy CVE history, etc.
type Notice struct {
	Kind    string // "deprecated", "advisory-history", ...
	Message string
}

type AnalysisReport struct {
	PackageName    string
	PackageVersion string
	FilesScanned   int
	Detections     []DetectionResult
	Notices        []Notice
	RiskScore      int
	Verdict        string
}
