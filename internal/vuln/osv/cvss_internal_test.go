package osv

import "testing"

func TestCVSSv3BaseScore(t *testing.T) {
	t.Parallel()
	// Scores are the published base scores of each vector (NVD and the FIRST
	// CVSS v3.1 calculator).
	tests := []struct {
		vector string
		want   float64
		wantOK bool
	}{
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 9.8, true},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H", 10.0, true},
		{"CVSS:3.1/AV:N/AC:L/PR:L/UI:N/S:C/C:H/I:H/A:H", 9.9, true},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:R/S:C/C:L/I:L/A:N", 6.1, true},
		{"CVSS:3.1/AV:N/AC:L/PR:H/UI:R/S:C/C:L/I:L/A:N", 4.8, true},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H", 7.5, true},
		{"CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:H/A:H", 7.8, true},
		{"CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:N/A:N", 5.9, true},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N", 5.3, true},
		{"CVSS:3.1/AV:L/AC:H/PR:H/UI:R/S:U/C:H/I:H/A:N", 5.6, true},
		{"CVSS:3.1/AV:P/AC:H/PR:H/UI:R/S:U/C:L/I:N/A:N", 1.6, true},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N", 0, true},
		{"CVSS:3.0/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 9.8, true},
		// Metric order is free, and temporal metrics do not change the base.
		{"CVSS:3.1/S:U/AV:N/AC:L/PR:N/UI:N/C:H/I:H/A:H/E:H/RL:O", 9.8, true},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H", 0, false},
		{"CVSS:3.1/AV:X/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 0, false},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:X/C:H/I:H/A:H", 0, false},
		{"CVSS:3.1/AV:N/AC:L/PR:X/UI:N/S:U/C:H/I:H/A:H", 0, false},
		{"CVSS:3.1/AV:N/AV:L/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 0, false},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H/", 0, false},
		{"AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 0, false},
		{"CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.vector, func(t *testing.T) {
			t.Parallel()
			got, ok := cvssV3BaseScore(tt.vector)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("cvssV3BaseScore(%q) = %v, %v, want %v, %v", tt.vector, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestRoundUp(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want float64 }{
		{4.0, 4.0},
		{4.02, 4.1},
		{4.000001, 4.0},
		{9.81, 9.9},
		{0, 0},
	}
	for _, tt := range tests {
		if got := roundUp(tt.in); got != tt.want {
			t.Errorf("roundUp(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
