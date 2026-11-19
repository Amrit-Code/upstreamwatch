package model

import "time"

// HTTPValidator is what a conditional GET needs to ask "has this changed":
// the validators a server sent with its last full response to URL. The
// http_cache table stores one per exact URL.
type HTTPValidator struct {
	URL          string
	ETag         string
	LastModified string
	// FetchedAt is when the validators were received, in UTC.
	FetchedAt time.Time
	// Status is the HTTP status of that response.
	Status int
}
