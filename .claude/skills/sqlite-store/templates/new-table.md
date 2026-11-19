# Scaffold: one table end to end

Use this when a work order adds a table or a column. The example is the `digests` table, one of the accepted additions in `docs/database.md`. Take the real column list from that doc and your work order, not from this example.

## 1. Migration

`internal/store/migrations/0007_add_digests.sql` (use the filename and number the work order gives, not "highest plus one"):

```sql
-- Digests: one row per digest send attempt per channel, so "only send if
-- something changed" can compare with the last digest and a failed send can be retried.
CREATE TABLE digests (
    id         INTEGER PRIMARY KEY,
    sent_ts    TEXT,                      -- NULL until the send succeeds
    channel    TEXT NOT NULL,             -- email | discord_webhook | slack_webhook | ntfy | stdout
    status     TEXT NOT NULL,             -- pending | sent | failed
    payload    TEXT NOT NULL,             -- JSON of the digest as sent
    error      TEXT,
    created_ts TEXT NOT NULL,
    updated_ts TEXT NOT NULL
);

CREATE INDEX digests_channel_sent ON digests (channel, sent_ts);
```

Adding a column to an existing table:

```sql
-- llm_verdicts.request_payload: exactly what was sent to the model, for the finding page.
ALTER TABLE llm_verdicts ADD COLUMN request_payload TEXT;
```

The new column is nullable. `NOT NULL` without a constant default, or a non-constant default, fails on a table that already has rows. It succeeds on an empty table, so a fresh-database test would not catch it.

## 2. Model type

In `internal/model`, only if your work order lists it:

```go
// DigestStatus is the delivery state of one digest on one channel.
type DigestStatus string

// Digest delivery states. Stored as text; never renamed.
const (
	DigestPending DigestStatus = "pending"
	DigestSent    DigestStatus = "sent"
	DigestFailed  DigestStatus = "failed"
)

// DigestRecord is one digest send attempt on one channel.
type DigestRecord struct {
	ID      int64
	SentAt  time.Time // zero until sent
	Channel string
	Status  DigestStatus
	Payload Digest
	Error   string
}
```

## 3. Store code

```go
const digestColumns = `id, sent_ts, channel, status, payload, error`

func scanDigest(s scanner) (model.DigestRecord, error) {
	var (
		d       model.DigestRecord
		sent    sql.NullString
		status  string
		payload string
		errText sql.NullString
	)
	if err := s.Scan(&d.ID, &sent, &d.Channel, &status, &payload, &errText); err != nil {
		return model.DigestRecord{}, err
	}
	t, err := parseTime(sent)
	if err != nil {
		return model.DigestRecord{}, err
	}
	if err := json.Unmarshal([]byte(payload), &d.Payload); err != nil {
		return model.DigestRecord{}, fmt.Errorf("decode digest %d payload: %w", d.ID, err)
	}
	d.SentAt, d.Status, d.Error = t, model.DigestStatus(status), errText.String
	return d, nil
}

// LastSentDigest returns the most recent successfully sent digest on channel.
func (db *DB) LastSentDigest(ctx context.Context, channel string) (model.DigestRecord, error) {
	row := db.q.QueryRowContext(ctx,
		`SELECT `+digestColumns+` FROM digests
		 WHERE channel = ? AND status = ? ORDER BY sent_ts DESC LIMIT 1`,
		channel, string(model.DigestSent))
	d, err := scanDigest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.DigestRecord{}, fmt.Errorf("last sent digest on %s: %w", channel, model.ErrNotFound)
	}
	if err != nil {
		return model.DigestRecord{}, fmt.Errorf("last sent digest on %s: %w", channel, err)
	}
	return d, nil
}

// SaveDigest inserts a digest attempt and returns its id.
func (db *DB) SaveDigest(ctx context.Context, d model.DigestRecord) (int64, error) {
	payload, err := json.Marshal(d.Payload)
	if err != nil {
		return 0, fmt.Errorf("save digest: encode payload: %w", err)
	}
	now := formatTime(db.now())
	var id int64
	err = db.q.QueryRowContext(ctx,
		`INSERT INTO digests (sent_ts, channel, status, payload, error, created_ts, updated_ts)
		 VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		formatTime(d.SentAt), d.Channel, string(d.Status), string(payload), nullString(d.Error), now, now,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("save digest: %w", err)
	}
	return id, nil
}
```

`nullString` returns nil for an empty string so optional text stays NULL. Put it next to `formatTime`.

Marking a digest sent and stamping `findings.reported_in_digest_ts` must land together, so the service does both inside one `InTx`.

## 4. Interface

The consuming service's `interfaces.go` (here `internal/report`) must list `LastSentDigest` and `SaveDigest` in its `FindingStore`. The lead owns that file. If the methods are not there, report it; do not add them yourself.

## 5. Tests

Follow `go-test`'s `templates/store-tests.md`: a round trip including the JSON payload and a zero `SentAt`, not found, the ordering of `LastSentDigest`, and a failure for an unknown status value if the table has a `CHECK` constraint.

For a migration that alters an existing table, add an upgrade test:

1. Migrate a database up to the previous version.
2. Insert rows.
3. Run `Migrate` again.
4. Check the old rows survived and the new column reads back as its zero value.

## 6. Docs

In your report, name the updates for `docs-sync`: the `digests` table and its columns in `docs/database.md`, and `digests` under `FindingStore` in the table-to-interface map in `docs/data-access.md`.
