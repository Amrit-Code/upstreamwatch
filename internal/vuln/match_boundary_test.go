package vuln

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// This file checks evaluate against an expectation the test computes on its
// own: each range shape is evaluated at every boundary version (just below,
// at and just above each event), and the expected outcome comes from the
// wording of the OSV rule in WO-06 requirement 4, applied with the test's own
// version comparator rather than the vers library. A version is affected by a
// range when some introduced event is at or below it and no fixed or limit
// event from that introduction up to the version is at or below it, and no
// last_affected event from that introduction is below it.

// boundaryOracle is the test's independent model of one version scheme.
type boundaryOracle struct {
	// cmp returns -1, 0 or 1 as a sorts below, equal to or above b.
	cmp func(a, b string) int
}

// atOrAbove reports whether event value e is at or above introduction i,
// with "0" below everything.
func (o boundaryOracle) atOrAbove(e, i string) bool {
	return i == "0" || o.cmp(i, e) <= 0
}

// inRange reports whether v is affected by the events of one range.
func (o boundaryOracle) inRange(v string, events []model.RangeEvent) bool {
	for _, in := range events {
		if in.Introduced == "" {
			continue
		}
		i := in.Introduced
		if i != "0" && o.cmp(i, v) > 0 {
			continue
		}
		blocked := false
		for _, e := range events {
			switch {
			case e.Fixed != "" && o.atOrAbove(e.Fixed, i) && o.cmp(e.Fixed, v) <= 0:
				blocked = true
			case e.Limit != "" && o.atOrAbove(e.Limit, i) && o.cmp(e.Limit, v) <= 0:
				blocked = true
			case e.LastAffected != "" && o.atOrAbove(e.LastAffected, i) && o.cmp(e.LastAffected, v) < 0:
				blocked = true
			}
		}
		if !blocked {
			return true
		}
	}
	return false
}

// expect returns whether v is affected by any of ranges and, when it is, the
// lowest fixed value above v among the ranges that cover it.
func (o boundaryOracle) expect(v string, ranges []model.AffectedRange) (affected bool, fixed string) {
	for _, r := range ranges {
		if !o.inRange(v, r.Events) {
			continue
		}
		affected = true
		for _, e := range r.Events {
			if e.Fixed != "" && o.cmp(e.Fixed, v) > 0 && (fixed == "" || o.cmp(e.Fixed, fixed) < 0) {
				fixed = e.Fixed
			}
		}
	}
	return affected, fixed
}

// semverKey is a plain X.Y.Z version with an optional pre-release, which
// sorts just below its release.
type semverKey struct {
	nums [3]int
	pre  string
}

func parseSemverKey(t *testing.T, v string) semverKey {
	t.Helper()
	core, pre, _ := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		t.Fatalf("test version %q is not X.Y.Z", v)
	}
	var k semverKey
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("test version %q: %v", v, err)
		}
		k.nums[i] = n
	}
	k.pre = pre
	return k
}

func semverOracle(t *testing.T) boundaryOracle {
	return boundaryOracle{cmp: func(a, b string) int {
		ka, kb := parseSemverKey(t, a), parseSemverKey(t, b)
		for i := range 3 {
			if c := ka.nums[i] - kb.nums[i]; c != 0 {
				if c < 0 {
					return -1
				}
				return 1
			}
		}
		switch {
		case ka.pre == kb.pre:
			return 0
		case ka.pre == "":
			return 1
		case kb.pre == "":
			return -1
		}
		return strings.Compare(ka.pre, kb.pre)
	}}
}

// semverBoundaries returns, for every event value of ranges, the version
// just below it (a lower patch and a pre-release of it), the value itself and
// the version just above it, plus the lowest and a high version.
func semverBoundaries(t *testing.T, ranges []model.AffectedRange) []string {
	t.Helper()
	out := []string{"0.0.0", "0.0.1", "9.9.9"}
	for _, r := range ranges {
		for _, e := range r.Events {
			for _, v := range []string{e.Introduced, e.Fixed, e.LastAffected, e.Limit} {
				if v == "" || v == "0" {
					continue
				}
				k := parseSemverKey(t, v)
				x, y, z := k.nums[0], k.nums[1], k.nums[2]
				var below string
				switch {
				case z > 0:
					below = fmt.Sprintf("%d.%d.%d", x, y, z-1)
				case y > 0:
					below = fmt.Sprintf("%d.%d.99", x, y-1)
				default:
					below = fmt.Sprintf("%d.99.99", x-1)
				}
				out = append(out, below, v+"-rc.1", v, fmt.Sprintf("%d.%d.%d", x, y, z+1))
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// pep440Order is a run of PyPI versions in ascending PEP 440 order, written
// by hand.
var pep440Order = []string{
	"0.9", "1.0a1", "1.0b1", "1.0rc1", "1.0", "1.0.post1", "1.1", "1.9",
	"2.0rc1", "2.0", "2.0.post1", "2.1",
}

func pep440Oracle(t *testing.T) boundaryOracle {
	rank := func(v string) int {
		i := slices.Index(pep440Order, v)
		if i < 0 {
			t.Fatalf("test version %q is not in pep440Order", v)
		}
		return i
	}
	return boundaryOracle{cmp: func(a, b string) int {
		ra, rb := rank(a), rank(b)
		switch {
		case ra < rb:
			return -1
		case ra > rb:
			return 1
		}
		return 0
	}}
}

func TestEvaluate_RangeBoundaries(t *testing.T) {
	t.Parallel()
	goShapes := []struct {
		name   string
		ranges []model.AffectedRange
	}{
		{"introduced/fixed", []model.AffectedRange{semverRange(intro("1.0.0"), fix("1.5.0"))}},
		{"introduced 0/fixed", []model.AffectedRange{semverRange(intro("0"), fix("1.5.0"))}},
		{"introduced/last_affected", []model.AffectedRange{semverRange(intro("1.0.0"), last("1.4.0"))}},
		{"open-ended introduction", []model.AffectedRange{semverRange(intro("1.0.0"))}},
		{"two segments in one range, listed out of order",
			[]model.AffectedRange{semverRange(fix("2.3.0"), intro("2.0.0"), fix("1.2.0"), intro("1.0.0"))}},
		{"two last_affected segments",
			[]model.AffectedRange{semverRange(intro("1.0.0"), last("1.4.0"), intro("2.0.0"), last("2.4.0"))}},
		{"fixed segment then open-ended segment",
			[]model.AffectedRange{semverRange(intro("1.0.0"), fix("1.2.0"), intro("2.0.0"))}},
		{"overlapping ranges",
			[]model.AffectedRange{semverRange(intro("1.0.0"), fix("1.2.0")), semverRange(intro("1.1.0"), fix("1.3.0"))}},
		{"fixed range and last_affected range",
			[]model.AffectedRange{semverRange(intro("1.0.0"), fix("1.2.0")), semverRange(intro("1.1.0"), last("1.5.0"))}},
		{"limit", []model.AffectedRange{semverRange(intro("1.0.0"), limit("2.0.0"))}},
		{"introduced 0/limit", []model.AffectedRange{semverRange(intro("0"), limit("2.0.0"))}},
		{"fixed below a limit", []model.AffectedRange{semverRange(intro("1.0.0"), fix("1.5.0"), limit("2.0.0"))}},
	}
	o := semverOracle(t)
	for _, sh := range goShapes {
		aff := entry(goLib, nil, sh.ranges...)
		for _, v := range semverBoundaries(t, sh.ranges) {
			wantAffected, wantFixed := o.expect(v, sh.ranges)
			// The Go module form with a leading v must behave the same.
			for _, pin := range []string{v, "v" + v} {
				t.Run(sh.name+"/"+pin, func(t *testing.T) {
					t.Parallel()
					got := evaluate(adv("GO-2026-0100"), aff, pinned(goLib, pin))
					checkBoundary(t, got, pin, wantAffected, wantFixed)
				})
			}
		}
	}

	pyShapes := []struct {
		name   string
		ranges []model.AffectedRange
	}{
		{"pep440 introduced rc/fixed", []model.AffectedRange{ecosystemRange(intro("1.0rc1"), fix("2.0"))}},
		{"pep440 introduced 0/fixed post", []model.AffectedRange{ecosystemRange(intro("0"), fix("1.0.post1"))}},
		{"pep440 introduced/last_affected", []model.AffectedRange{ecosystemRange(intro("1.0"), last("2.0rc1"))}},
		{"pep440 introduced 0/limit", []model.AffectedRange{ecosystemRange(intro("0"), limit("1.0"))}},
		{"pep440 two segments", []model.AffectedRange{ecosystemRange(intro("1.0a1"), fix("1.0"), intro("1.9"), fix("2.0.post1"))}},
	}
	po := pep440Oracle(t)
	for _, sh := range pyShapes {
		aff := entry(pypiLib, nil, sh.ranges...)
		for _, v := range pep440Order {
			wantAffected, wantFixed := po.expect(v, sh.ranges)
			t.Run(sh.name+"/"+v, func(t *testing.T) {
				t.Parallel()
				got := evaluate(adv("PYSEC-2026-100"), aff, pinned(pypiLib, v))
				checkBoundary(t, got, v, wantAffected, wantFixed)
			})
		}
	}
}

// checkBoundary compares one evaluation with the oracle's outcome. For a
// native record and an exact pin the confidence is high exactly when a fixed
// version above the pin is known.
func checkBoundary(t *testing.T, got matchResult, pin string, wantAffected bool, wantFixed string) {
	t.Helper()
	if got.affected != wantAffected || got.fixedVersion != wantFixed {
		t.Errorf("evaluate(pin %s) = affected %v, fixed %q (%s); want affected %v, fixed %q",
			pin, got.affected, got.fixedVersion, got.reason, wantAffected, wantFixed)
	}
	wantConf := model.Confidence("")
	if wantAffected {
		wantConf = model.ConfidenceMedium
		if wantFixed != "" {
			wantConf = model.ConfidenceHigh
		}
	}
	if got.confidence != wantConf {
		t.Errorf("evaluate(pin %s) confidence = %q, want %q (%s)", pin, got.confidence, wantConf, got.reason)
	}
	if got.reason == "" {
		t.Errorf("evaluate(pin %s) reason is empty", pin)
	}
}
