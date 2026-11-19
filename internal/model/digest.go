package model

import "time"

// Digest is one daily summary message, built once by the digest job and sent
// unchanged to every enabled notifier. It is stored as JSON in
// digests.payload, so a failed send can be retried with exactly what was
// built.
type Digest struct {
	// GeneratedAt is when the digest was built, in the server's local time
	// zone (the compose file sets TZ); rendered dates and times use its
	// location.
	GeneratedAt time.Time `json:"generated_at"`
	// Heartbeat is true for the weekly "all quiet" message, which has no
	// items.
	Heartbeat bool `json:"heartbeat"`
	// New are open findings never reported in a digest, highest score first.
	// Changed are open findings that changed since they were last reported.
	New     []DigestItem `json:"new"`
	Changed []DigestItem `json:"changed"`
	// MoreNew and MoreChanged count the items left out of New and Changed
	// beyond the per-section cap.
	MoreNew     int `json:"more_new"`
	MoreChanged int `json:"more_changed"`
	// Waiting is the number of issue candidates waiting for LLM budget; zero
	// until the LLM stage exists.
	Waiting int `json:"waiting"`
	// LastRuns is the most recent run of each job, ordered by job name.
	LastRuns []Run `json:"last_runs"`
	// Open, Acked and Snoozed count unresolved findings by state.
	Open    int `json:"open"`
	Acked   int `json:"acked"`
	Snoozed int `json:"snoozed"`
	// FindingsURL links to the findings page; empty when http.base_url is
	// not set.
	FindingsURL string `json:"findings_url"`
}

// IsEmpty reports whether the digest has no items to report.
func (d Digest) IsEmpty() bool {
	return len(d.New) == 0 && len(d.Changed) == 0 && d.MoreNew == 0 && d.MoreChanged == 0
}

// DigestItem is one finding as a digest shows it. The digest job renders the
// text; notifiers only lay it out for their channel. Headline and Lines may
// contain text from upstream sources (advisory summaries), so a notifier
// escapes or neutralises them for its format.
type DigestItem struct {
	FindingID int64       `json:"finding_id"`
	Kind      FindingKind `json:"kind"`
	Score     float64     `json:"score"`
	// Tag is the short kind marker: "[A]" advisory, "[I]" issue, "[J]" job
	// failure.
	Tag string `json:"tag"`
	// Headline is the item's first line after the tag, for example
	// "high  GO-2026-4866  pkg:golang/toolchain 1.26.0  (repo: server)".
	Headline string `json:"headline"`
	// Lines are the detail lines below the headline, without indentation.
	Lines []string `json:"lines"`
	// URL links to the finding page; empty when http.base_url is not set.
	URL string `json:"url"`
}

// DigestStatus is the outcome of one digest send to one channel, as stored in
// digests.status.
type DigestStatus string

// The stored values of DigestStatus. A stored value is never renamed.
const (
	DigestSent   DigestStatus = "sent"
	DigestFailed DigestStatus = "failed"
	// DigestRetried marks a failed row whose payload was later sent
	// successfully (by a newer row).
	DigestRetried DigestStatus = "retried"
)

// DigestRecord is one row of the digests table: one digest sent, or tried,
// on one channel.
type DigestRecord struct {
	// ID is the row ID; zero before it is stored.
	ID int64
	// Channel is the notifier's name, for example "email" or "stdout#2"
	// (see the digest job for how names are made unique).
	Channel string
	Status  DigestStatus
	// SentAt is the time of the attempt, UTC.
	SentAt time.Time
	// Payload is the Digest as JSON.
	Payload []byte
	// Error is the redacted failure text, empty on success.
	Error string
}

// FindingFilter selects findings for the findings list.
type FindingFilter struct {
	// RepoID limits to one repo; zero means all.
	RepoID int64
	// Kind limits to one kind; empty means all.
	Kind FindingKind
	// Suppressed, when true, also returns findings that are snoozed (with
	// the snooze still running) or dismissed. When false only findings
	// whose effective state is open or acked are returned.
	Suppressed bool
	// Now is the time used to decide whether a snooze has expired; a
	// snoozed finding whose snooze has ended counts as open.
	Now time.Time
}
