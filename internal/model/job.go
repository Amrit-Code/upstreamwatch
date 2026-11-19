package model

// Scope says what a job run covers: every target repo, or one.
type Scope struct {
	// RepoSlug limits the run to one target repo. Empty means all repos.
	RepoSlug string
}

// All reports whether the scope covers every target repo.
func (s Scope) All() bool { return s.RepoSlug == "" }

// Stats is what a job run reports back: named counters such as "fetched" or
// "not_modified". The runs table stores it as a JSON object.
type Stats map[string]int64
