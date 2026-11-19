package model

import (
	"math"
	"strconv"
	"strings"
)

// AdvisoryRankInput is what the advisory score depends on.
type AdvisoryRankInput struct {
	Severity         SeverityLabel
	KEV              bool
	HasEPSS          bool
	EPSSPercentile   float64 // 0..1
	Direct           Directness
	CurrentlyExposed bool // at least one match is still open
	Confidence       Confidence
}

// JobFailureScore is the fixed score of a job_failure finding.
const JobFailureScore = 50

// RankAdvisory returns the score of an advisory finding and a one-line
// explanation of its parts.
//
// The score is the severity base (critical 90, high 70, medium 40, low 20,
// unknown 30), plus 30 for KEV, plus the EPSS percentile × 20 rounded to an
// integer, plus 10 for a direct dependency, plus 20 when still exposed, minus
// 10 for medium and 25 for low confidence. Every part is an integer, so the
// score is integer-valued and small daily EPSS drift rarely changes it. The
// reason lists the parts that contribute, in that order, separated by "; ",
// for example "high 70; KEV +30; EPSS p71 +14; direct +10; exposed now +20;
// confidence medium -10". Equal inputs give byte-equal reasons.
func RankAdvisory(in AdvisoryRankInput) (score float64, reason string) {
	label, base := severityBase(in.Severity)
	total := base
	parts := []string{label + " " + strconv.Itoa(base)}

	if in.KEV {
		total += 30
		parts = append(parts, "KEV +30")
	}
	if in.HasEPSS {
		// A percentile below p2.5 rounds to no points and, like every part
		// that contributes nothing, is left out of the reason.
		if pts := int(math.Round(in.EPSSPercentile * 20)); pts > 0 {
			pct := int(math.Round(in.EPSSPercentile * 100))
			total += pts
			parts = append(parts, "EPSS p"+strconv.Itoa(pct)+" +"+strconv.Itoa(pts))
		}
	}
	if in.Direct == DirectYes {
		total += 10
		parts = append(parts, "direct +10")
	}
	if in.CurrentlyExposed {
		total += 20
		parts = append(parts, "exposed now +20")
	}
	switch in.Confidence {
	case ConfidenceMedium:
		total -= 10
		parts = append(parts, "confidence medium -10")
	case ConfidenceLow:
		total -= 25
		parts = append(parts, "confidence low -25")
	}
	return float64(total), strings.Join(parts, "; ")
}

// severityBase returns the reason label and base score of a severity. A value
// outside the stored set counts as unknown.
func severityBase(s SeverityLabel) (string, int) {
	switch s {
	case SeverityCritical:
		return "critical", 90
	case SeverityHigh:
		return "high", 70
	case SeverityMedium:
		return "medium", 40
	case SeverityLow:
		return "low", 20
	default:
		return "unknown", 30
	}
}
