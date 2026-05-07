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

type AnalysisReport struct {
	PackageName    string
	PackageVersion string
	FilesScanned   int
	Detections     []DetectionResult
	RiskScore      int
	Verdict        string
}
