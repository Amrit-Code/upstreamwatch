package httpx

import (
	"context"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

// CacheStore keeps the validators of conditional requests whose cache httpx
// manages itself (feeds such as KEV, EPSS and the OSV modified_id files).
type CacheStore interface {
	// GetHTTPValidator returns the validators stored for the exact URL, or an
	// error wrapping model.ErrNotFound if there are none.
	GetHTTPValidator(ctx context.Context, url string) (model.HTTPValidator, error)
	// SaveHTTPValidator stores v, replacing any validators held for v.URL.
	SaveHTTPValidator(ctx context.Context, v model.HTTPValidator) error
}
