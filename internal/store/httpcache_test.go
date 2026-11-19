package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Amrit-Code/upstreamwatch/internal/model"
)

const feedURL = "https://feeds.example.org/data/known_exploited.json"

func TestDB_HTTPValidator_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		in            model.HTTPValidator
		want          model.HTTPValidator
		wantNullETag  bool
		wantNullLastM bool
		inTx          bool
	}{
		{
			name: "all fields",
			in:   model.HTTPValidator{URL: feedURL, ETag: `"abc123"`, LastModified: "Sun, 01 Mar 2026 11:00:00 GMT", FetchedAt: t0, Status: 200},
			want: model.HTTPValidator{URL: feedURL, ETag: `"abc123"`, LastModified: "Sun, 01 Mar 2026 11:00:00 GMT", FetchedAt: t0, Status: 200},
		},
		{
			name:         "empty etag stored as NULL",
			in:           model.HTTPValidator{URL: feedURL, LastModified: "Sun, 01 Mar 2026 11:00:00 GMT", FetchedAt: t0, Status: 200},
			want:         model.HTTPValidator{URL: feedURL, LastModified: "Sun, 01 Mar 2026 11:00:00 GMT", FetchedAt: t0, Status: 200},
			wantNullETag: true,
		},
		{
			name:          "empty last-modified stored as NULL",
			in:            model.HTTPValidator{URL: feedURL, ETag: `W/"v1"`, FetchedAt: t0, Status: 200},
			want:          model.HTTPValidator{URL: feedURL, ETag: `W/"v1"`, FetchedAt: t0, Status: 200},
			wantNullLastM: true,
		},
		{
			name:          "fetched time in another zone with fraction is stored as UTC seconds",
			in:            model.HTTPValidator{URL: feedURL, ETag: `"x"`, FetchedAt: t0.Add(500 * time.Millisecond).In(time.FixedZone("plus2", 2*3600)), Status: 200},
			want:          model.HTTPValidator{URL: feedURL, ETag: `"x"`, FetchedAt: t0, Status: 200},
			wantNullLastM: true,
		},
		{
			name:          "inside InTx",
			in:            model.HTTPValidator{URL: feedURL, ETag: `"tx"`, FetchedAt: t0, Status: 200},
			want:          model.HTTPValidator{URL: feedURL, ETag: `"tx"`, FetchedAt: t0, Status: 200},
			wantNullLastM: true,
			inTx:          true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			ctx := t.Context()

			var got model.HTTPValidator
			if tc.inTx {
				err := db.InTx(ctx, func(tx *DB) error {
					if err := tx.SaveHTTPValidator(ctx, tc.in); err != nil {
						return err
					}
					var err error
					got, err = tx.GetHTTPValidator(ctx, tc.in.URL)
					return err
				})
				if err != nil {
					t.Fatalf("InTx(save, get) error = %v", err)
				}
			} else {
				if err := db.SaveHTTPValidator(ctx, tc.in); err != nil {
					t.Fatalf("SaveHTTPValidator() error = %v", err)
				}
				var err error
				if got, err = db.GetHTTPValidator(ctx, tc.in.URL); err != nil {
					t.Fatalf("GetHTTPValidator() error = %v", err)
				}
			}
			if got != tc.want {
				t.Errorf("GetHTTPValidator() = %+v, want %+v", got, tc.want)
			}

			if n := count(t, db, "SELECT COUNT(*) FROM http_cache WHERE url = ? AND etag IS NULL", feedURL); (n == 1) != tc.wantNullETag {
				t.Errorf("rows with NULL etag = %d, want NULL = %v", n, tc.wantNullETag)
			}
			if n := count(t, db, "SELECT COUNT(*) FROM http_cache WHERE url = ? AND last_modified IS NULL", feedURL); (n == 1) != tc.wantNullLastM {
				t.Errorf("rows with NULL last_modified = %d, want NULL = %v", n, tc.wantNullLastM)
			}
			if n := count(t, db, "SELECT COUNT(*) FROM http_cache WHERE url = ? AND updated_ts = ?", feedURL, "2026-03-01T12:00:00Z"); n != 1 {
				t.Errorf("rows with updated_ts from the store clock = %d, want 1", n)
			}
		})
	}
}

func TestDB_HTTPValidator_NotFound(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	_, err := db.GetHTTPValidator(t.Context(), "https://feeds.example.org/private/path.json?key=s3cret")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("GetHTTPValidator(unknown) error = %v, want ErrNotFound", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "feeds.example.org") {
		t.Errorf("error %q does not name the host", msg)
	}
	for _, leak := range []string{"/private", "path.json", "key=", "s3cret"} {
		if strings.Contains(msg, leak) {
			t.Errorf("error %q contains %q from the path or query", msg, leak)
		}
	}
}

func TestDB_HTTPValidator_Replace(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()

	first := model.HTTPValidator{URL: feedURL, ETag: `"v1"`, LastModified: "Sun, 01 Mar 2026 11:00:00 GMT", FetchedAt: t0, Status: 200}
	if err := db.SaveHTTPValidator(ctx, first); err != nil {
		t.Fatalf("SaveHTTPValidator(first) error = %v", err)
	}
	later := t0.Add(24 * time.Hour)
	db.now = func() time.Time { return later }
	second := model.HTTPValidator{URL: feedURL, ETag: `"v2"`, FetchedAt: later, Status: 203}
	if err := db.SaveHTTPValidator(ctx, second); err != nil {
		t.Fatalf("SaveHTTPValidator(second) error = %v", err)
	}

	got, err := db.GetHTTPValidator(ctx, feedURL)
	if err != nil {
		t.Fatalf("GetHTTPValidator() error = %v", err)
	}
	if got != second {
		t.Errorf("GetHTTPValidator() = %+v, want %+v", got, second)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM http_cache"); n != 1 {
		t.Errorf("http_cache rows = %d, want 1", n)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM http_cache WHERE updated_ts = ?", "2026-03-02T12:00:00Z"); n != 1 {
		t.Errorf("rows with the second save's updated_ts = %d, want 1", n)
	}
}

func TestDB_HTTPValidator_EmptyURL(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	err := db.SaveHTTPValidator(t.Context(), model.HTTPValidator{ETag: `"v1"`, FetchedAt: t0, Status: 200})
	if err == nil {
		t.Fatal("SaveHTTPValidator(empty URL) error = nil, want an error")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM http_cache"); n != 0 {
		t.Errorf("http_cache rows = %d, want 0", n)
	}
}

func TestDB_HTTPValidator_ExactURLMatch(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()

	a := model.HTTPValidator{URL: "https://feeds.example.org/data.json?page=1", ETag: `"a"`, FetchedAt: t0, Status: 200}
	b := model.HTTPValidator{URL: "https://feeds.example.org/data.json?page=2", ETag: `"b"`, FetchedAt: t0, Status: 200}
	for _, v := range []model.HTTPValidator{a, b} {
		if err := db.SaveHTTPValidator(ctx, v); err != nil {
			t.Fatalf("SaveHTTPValidator(%s) error = %v", v.URL, err)
		}
	}
	for _, want := range []model.HTTPValidator{a, b} {
		got, err := db.GetHTTPValidator(ctx, want.URL)
		if err != nil {
			t.Fatalf("GetHTTPValidator(%s) error = %v", want.URL, err)
		}
		if got != want {
			t.Errorf("GetHTTPValidator(%s) = %+v, want %+v", want.URL, got, want)
		}
	}
	if _, err := db.GetHTTPValidator(ctx, "https://feeds.example.org/data.json"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("GetHTTPValidator(without query) error = %v, want ErrNotFound", err)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM http_cache"); n != 2 {
		t.Errorf("http_cache rows = %d, want 2", n)
	}
}
