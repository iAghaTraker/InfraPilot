package storage

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

// Health describes the state of the database.
//
// It carries no row counts or file contents: it is rendered by user-facing
// commands, so it holds only what an operator needs to judge whether storage
// is working.
type Health struct {
	// Healthy reports whether the database answered a query successfully.
	Healthy bool

	// Path is the database file location.
	Path string

	// SchemaVersion is the migration version recorded in the database.
	SchemaVersion int

	// SizeBytes is the size of the database file, or 0 when unknown.
	SizeBytes int64

	// Detail explains an unhealthy result. It is empty when healthy.
	Detail string
}

// healthCheckTimeout bounds the health query so a locked database reports a
// problem instead of hanging a status command indefinitely.
const healthCheckTimeout = 3 * time.Second

// Check reports the current health of the database.
//
// It returns a Health value with Healthy set to false and Detail populated,
// rather than an error, when the database is reachable but unwell: a status
// command should print a diagnosis instead of failing outright. A nil DB is
// reported as unhealthy for the same reason.
func (db *DB) Check(ctx context.Context) Health {
	if db == nil || db.sql == nil {
		return Health{Detail: "the database is not open"}
	}

	health := Health{Path: db.path}

	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	// A trivial query proves the connection works end to end, which Ping alone
	// does not for a pooled driver.
	var one int
	if err := db.sql.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		health.Detail = detailFrom("the database did not answer a query", err)
		return health
	}
	if one != 1 {
		health.Detail = "the database returned an unexpected result"
		return health
	}

	version, err := db.CurrentVersion(ctx)
	if err != nil {
		health.Detail = detailFrom("cannot read the schema version", err)
		return health
	}
	health.SchemaVersion = version

	// A schema older than the binary means migrations did not complete.
	if expected := SchemaVersion(); version != expected {
		health.Detail = fmt.Sprintf("schema version is %d, expected %d", version, expected)
		return health
	}

	if size, err := db.fileSize(); err == nil {
		health.SizeBytes = size
	}

	health.Healthy = true
	return health
}

// Verify runs SQLite's integrity check.
//
// It is deliberately not part of Check: on a large database it reads every
// page, which is too slow for a status command. It is exposed for future
// maintenance commands and for tests that need to prove the file is sound.
func (db *DB) Verify(ctx context.Context) error {
	const op = "storage.Verify"

	if db == nil || db.sql == nil {
		return errors.New(errors.KindStorage, op, "the database is not open")
	}

	var result string
	if err := db.sql.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return errors.Wrap(errors.KindStorage, op, "cannot run the integrity check", err)
	}
	if result != "ok" {
		return errors.New(errors.KindStorage, op,
			fmt.Sprintf("the database failed its integrity check: %s", result))
	}

	return nil
}

// fileSize returns the size of the database file.
func (db *DB) fileSize() (int64, error) {
	info, err := os.Stat(db.path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// detailFrom builds a short diagnosis from an error.
//
// The driver's error text is included because an operator needs it to act.
// That is safe only while the DSN carries no credentials, which is true today
// and must stay true: if a future version adds an encryption key or similar to
// the connection string, this function must start filtering rather than
// concatenating, since SQLite errors can quote the DSN back.
func detailFrom(what string, err error) string {
	if err == nil {
		return what
	}
	return what + ": " + err.Error()
}
