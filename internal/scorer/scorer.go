package scorer

import "github.com/philtechs-org/phi/internal/analyzer"

const (
	VerdictSafe    = "safe"
	VerdictReview  = "review"
	VerdictBlocked = "blocked"
)

func Score(detections []analyzer.DetectionResult) int {
	score := 0
	for _, d := range detections {
		switch d.Severity {
		case analyzer.SeverityCritical:
			score += 35
		case analyzer.SeverityHigh:
			score += 20
		case analyzer.SeverityMedium:
			score += 10
		case analyzer.SeverityLow:
			score += 5
		}
	}
	if score > 100 {
		score = 100
	}
	return score
}

func Verdict(score int) string {
	// 60 (not 50): a CRITICAL + HIGH combination — eval(35) + new Function(20)
	// totals 55 — should not auto-block. Pino legitimately uses both for fast
	// logger compilation. Real malware combines two CRITICAL signals
	// (e.g. eval + credential theft = 70) and still trips the threshold.
	switch {
	case score < 20:
		return VerdictSafe
	case score < 60:
		return VerdictReview
	default:
		return VerdictBlocked
	}
}
