package model

// FindingDetail is one finding with everything needed to show it: on the
// finding page, in the findings list and in the digest.
type FindingDetail struct {
	Finding Finding
	// RepoSlug and Package are empty for a job_failure finding.
	RepoSlug string
	Package  Package
	// Interval is the interval the finding links to (Finding.DepVersionID);
	// zero when the finding is not linked to one.
	Interval DepInterval
	// Advisory is set for a finding of kind advisory: the advisory with its
	// Affected entries and exploit signals, Raw left nil.
	Advisory Advisory
	// Exposures are, for a finding of kind advisory, every match of the
	// finding's advisory against an interval of the finding's repo and
	// package, ordered by exposure start, then interval ID.
	Exposures []Exposure
}

// Exposure is one advisory match with the interval it matched.
type Exposure struct {
	Match    AdvisoryMatch
	Interval DepInterval
}
