package scorer

import (
	"testing"

	"github.com/philtechs-org/phi/internal/analyzer"
)

func d(s analyzer.Severity) analyzer.DetectionResult {
	return analyzer.DetectionResult{Severity: s}
}

func TestScore_Weights(t *testing.T) {
	cases := []struct {
		name string
		in   []analyzer.DetectionResult
		want int
	}{
		{"empty", nil, 0},
		{"one low", []analyzer.DetectionResult{d(analyzer.SeverityLow)}, 5},
		{"one medium", []analyzer.DetectionResult{d(analyzer.SeverityMedium)}, 10},
		{"one high", []analyzer.DetectionResult{d(analyzer.SeverityHigh)}, 20},
		{"one critical", []analyzer.DetectionResult{d(analyzer.SeverityCritical)}, 35},
		{"mixed",
			[]analyzer.DetectionResult{
				d(analyzer.SeverityCritical),
				d(analyzer.SeverityHigh),
				d(analyzer.SeverityLow),
			},
			60,
		},
		{"caps at 100",
			[]analyzer.DetectionResult{
				d(analyzer.SeverityCritical),
				d(analyzer.SeverityCritical),
				d(analyzer.SeverityCritical),
				d(analyzer.SeverityCritical),
			},
			100,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Score(tc.in); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestVerdict_Boundaries(t *testing.T) {
	cases := []struct {
		score int
		want  string
	}{
		{0, VerdictSafe},
		{19, VerdictSafe},
		{20, VerdictReview},
		{55, VerdictReview}, // critical + high stays REVIEW
		{59, VerdictReview},
		{60, VerdictBlocked},
		{70, VerdictBlocked}, // 2x critical = blocked
		{100, VerdictBlocked},
	}
	for _, tc := range cases {
		if got := Verdict(tc.score); got != tc.want {
			t.Errorf("Verdict(%d) = %q, want %q", tc.score, got, tc.want)
		}
	}
}
