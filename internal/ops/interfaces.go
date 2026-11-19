package ops

import "context"

// Backuper writes a consistent copy of the database.
type Backuper interface {
	// BackupTo writes a copy of the database to path with VACUUM INTO. path
	// must not exist.
	BackupTo(ctx context.Context, path string) error
}
