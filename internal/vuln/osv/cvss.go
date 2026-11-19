package osv

import (
	"math"
	"strings"
)

// cvssWeights are the CVSS v3.1 base metric values, by metric and value.
// PR is scope-dependent and handled separately.
var cvssWeights = map[string]map[string]float64{
	"AV": {"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.2},
	"AC": {"L": 0.77, "H": 0.44},
	"UI": {"N": 0.85, "R": 0.62},
	"C":  {"H": 0.56, "L": 0.22, "N": 0},
	"I":  {"H": 0.56, "L": 0.22, "N": 0},
	"A":  {"H": 0.56, "L": 0.22, "N": 0},
}

// cvssV3BaseScore computes the CVSS v3.1 base score of a CVSS v3.0 or v3.1
// vector with the specification's formula. It reports false for a vector
// that lacks the prefix, lacks a base metric, repeats one or gives one a
// value the specification does not define. Temporal and environmental
// metrics are allowed and do not change the base score.
func cvssV3BaseScore(vector string) (float64, bool) {
	rest, ok := strings.CutPrefix(vector, "CVSS:3.1/")
	if !ok {
		if rest, ok = strings.CutPrefix(vector, "CVSS:3.0/"); !ok {
			return 0, false
		}
	}
	m := map[string]string{}
	for part := range strings.SplitSeq(rest, "/") {
		k, v, ok := strings.Cut(part, ":")
		if !ok || k == "" || v == "" {
			return 0, false
		}
		if _, base := cvssWeights[k]; base || k == "PR" || k == "S" {
			if _, dup := m[k]; dup {
				return 0, false
			}
			m[k] = v
		}
	}

	w := map[string]float64{}
	for k, values := range cvssWeights {
		x, ok := values[m[k]]
		if !ok {
			return 0, false
		}
		w[k] = x
	}
	var changed bool
	switch m["S"] {
	case "U":
	case "C":
		changed = true
	default:
		return 0, false
	}
	var pr float64
	switch m["PR"] {
	case "N":
		pr = 0.85
	case "L":
		pr = 0.62
		if changed {
			pr = 0.68
		}
	case "H":
		pr = 0.27
		if changed {
			pr = 0.5
		}
	default:
		return 0, false
	}

	iss := 1 - (1-w["C"])*(1-w["I"])*(1-w["A"])
	var impact float64
	if changed {
		impact = 7.52*(iss-0.029) - 3.25*math.Pow(iss-0.02, 15)
	} else {
		impact = 6.42 * iss
	}
	exploitability := 8.22 * w["AV"] * w["AC"] * pr * w["UI"]
	if impact <= 0 {
		return 0, true
	}
	if changed {
		return roundUp(math.Min(1.08*(impact+exploitability), 10)), true
	}
	return roundUp(math.Min(impact+exploitability, 10)), true
}

// roundUp is the CVSS v3.1 Roundup function: the smallest number with one
// decimal place that is at least x, computed in integers to avoid
// floating-point artefacts.
func roundUp(x float64) float64 {
	n := int64(math.Round(x * 100000))
	if n%10000 == 0 {
		return float64(n) / 100000
	}
	return float64(n/10000+1) / 10
}
