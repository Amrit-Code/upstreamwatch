package model

import (
	"math"
	"testing"
)

func TestRankAdvisory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		in         AdvisoryRankInput
		wantScore  float64
		wantReason string
	}{
		{
			name: "plan example: high, EPSS p71, direct, exposed, high confidence",
			in: AdvisoryRankInput{
				Severity: SeverityHigh, HasEPSS: true, EPSSPercentile: 0.71,
				Direct: DirectYes, CurrentlyExposed: true, Confidence: ConfidenceHigh,
			},
			wantScore:  114,
			wantReason: "high 70; EPSS p71 +14; direct +10; exposed now +20",
		},
		{
			name: "every part",
			in: AdvisoryRankInput{
				Severity: SeverityHigh, KEV: true, HasEPSS: true, EPSSPercentile: 0.71,
				Direct: DirectYes, CurrentlyExposed: true, Confidence: ConfidenceMedium,
			},
			wantScore:  134,
			wantReason: "high 70; KEV +30; EPSS p71 +14; direct +10; exposed now +20; confidence medium -10",
		},
		{
			name: "critical KEV low confidence historical transitive",
			in: AdvisoryRankInput{
				Severity: SeverityCritical, KEV: true, Direct: DirectNo, Confidence: ConfidenceLow,
			},
			wantScore:  95,
			wantReason: "critical 90; KEV +30; confidence low -25",
		},
		{
			name:       "unknown severity",
			in:         AdvisoryRankInput{Severity: SeverityUnknown},
			wantScore:  30,
			wantReason: "unknown 30",
		},
		{
			name:       "unrecognised severity counts as unknown",
			in:         AdvisoryRankInput{Severity: "moderate", Direct: DirectUnknown},
			wantScore:  30,
			wantReason: "unknown 30",
		},
		{
			name:       "EPSS 0.874 rounds to +17 and p87",
			in:         AdvisoryRankInput{Severity: SeverityMedium, HasEPSS: true, EPSSPercentile: 0.874},
			wantScore:  57,
			wantReason: "medium 40; EPSS p87 +17",
		},
		{
			name:       "EPSS percentile without HasEPSS is ignored",
			in:         AdvisoryRankInput{Severity: SeverityLow, EPSSPercentile: 0.99},
			wantScore:  20,
			wantReason: "low 20",
		},
		{
			name:       "EPSS that rounds to no points is left out",
			in:         AdvisoryRankInput{Severity: SeverityLow, HasEPSS: true, EPSSPercentile: 0.02},
			wantScore:  20,
			wantReason: "low 20",
		},
		{
			name:       "EPSS top percentile",
			in:         AdvisoryRankInput{Severity: SeverityLow, HasEPSS: true, EPSSPercentile: 1},
			wantScore:  40,
			wantReason: "low 20; EPSS p100 +20",
		},
		{
			name:       "empty confidence has no penalty",
			in:         AdvisoryRankInput{Severity: SeverityMedium, CurrentlyExposed: true},
			wantScore:  60,
			wantReason: "medium 40; exposed now +20",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotScore, gotReason := RankAdvisory(tt.in)
			if gotScore != tt.wantScore {
				t.Errorf("RankAdvisory(%+v) score = %v, want %v", tt.in, gotScore, tt.wantScore)
			}
			if gotScore != math.Trunc(gotScore) {
				t.Errorf("RankAdvisory(%+v) score = %v, want an integer value", tt.in, gotScore)
			}
			if gotReason != tt.wantReason {
				t.Errorf("RankAdvisory(%+v) reason = %q, want %q", tt.in, gotReason, tt.wantReason)
			}
		})
	}
}

func TestRankAdvisory_EPSSDriftKeepsScore(t *testing.T) {
	t.Parallel()
	base := AdvisoryRankInput{Severity: SeverityHigh, HasEPSS: true, EPSSPercentile: 0.711}
	drift := base
	drift.EPSSPercentile = 0.718
	s1, _ := RankAdvisory(base)
	s2, _ := RankAdvisory(drift)
	if s1 != s2 {
		t.Errorf("score changed from %v to %v on EPSS drift 0.711 -> 0.718", s1, s2)
	}
}
