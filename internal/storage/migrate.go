package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

// migration is one forward schema change.
//
// Migrations are append-only: once a version has shipped, its SQL is frozen.
// Changing a released migration would leave existing installations with a
// schema that does not match what the ledger claims they have.
type migration struct {
	Version int
	Name    string
	SQL     string
}

// migrations is the ordered list of schema changes.
//
// v0.1.0 introduces only the ledger that records which migrations have run.
// Feature tables arrive with the features that need them.
var migrations = []migration{
	{
		Version: 1,
		Name:    "create_schema_migrations",
		SQL: `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    applied_at  TEXT    NOT NULL
) STRICT;`,
	},
	{
		Version: 2,
		Name:    "create_security_identity_tables",
		SQL: `
CREATE TABLE IF NOT EXISTS device_identities (
    device_id TEXT PRIMARY KEY,
    public_key BLOB NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    last_seen_at TEXT,
    status TEXT NOT NULL CHECK (status IN ('active','revoked'))
) STRICT;
CREATE TABLE IF NOT EXISTS pairing_tokens (
    token_id TEXT PRIMARY KEY,
    token_hash BLOB NOT NULL UNIQUE,
    device_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at TEXT,
    status TEXT NOT NULL CHECK (status IN ('pending','used','expired','rejected')),
    FOREIGN KEY (device_id) REFERENCES device_identities(device_id)
) STRICT;
CREATE TABLE IF NOT EXISTS security_audit (
    id INTEGER PRIMARY KEY,
    event_type TEXT NOT NULL,
    device_id TEXT,
    success INTEGER NOT NULL CHECK (success IN (0,1)),
    reason TEXT,
    occurred_at TEXT NOT NULL
) STRICT;
`,
	},
}

// SchemaVersion reports the highest migration this binary knows about.
func SchemaVersion() int {
	highest := 0
	for _, m := range migrations {
		if m.Version > highest {
			highest = m.Version
		}
	}
	return highest
}

// Migrate applies every migration the database has not yet recorded.
//
// It is safe to call on every start-up: applied migrations are skipped. Each
// migration runs inside its own transaction together with its ledger entry, so
// a failure leaves no partially applied version behind.
func (db *DB) Migrate(ctx context.Context) error {
	const op = "storage.Migrate"

	// A read-only handle cannot migrate. Saying so is clearer than letting the
	// driver report a generic "attempt to write a readonly database".
	if db.readOnly {
		return errors.New(errors.KindUnsupported, op,
			"the database is open read-only and cannot be migrated")
	}

	if err := db.ensureLedger(ctx); err != nil {
		return err
	}

	applied, err := db.appliedVersions(ctx)
	if err != nil {
		return err
	}

	current, err := db.CurrentVersion(ctx)
	if err != nil {
		return err
	}
	// A database written by a newer InfraPilot may use schema this binary does
	// not understand. Downgrading data is not something a migration can undo,
	// so refuse rather than risk corrupting it.
	if current > SchemaVersion() {
		return errors.New(errors.KindStorage, op, fmt.Sprintf(
			"the database schema is version %d (10+ migrations ahead) but this build understands only version %d; upgrade InfraPilot",
			current, SchemaVersion()))
	}

	for _, m := range migrations {
		if applied[m.Version] {
			continue
		}
		if err := db.applyMigration(ctx, m); err != nil {
			return err
		}
	}

	return nil
}

// ensureLedger creates the migration table when it is absent.
//
// This bootstraps the very mechanism that records migrations, so it runs
// outside the numbered sequence and must be idempotent.
func (db *DB) ensureLedger(ctx context.Context) error {
	const op = "storage.ensureLedger"

	if _, err := db.sql.ExecContext(ctx, migrations[0].SQL); err != nil {
		return errors.Wrap(errors.KindStorage, op, "cannot create the migration ledger", err)
	}
	return nil
}

// appliedVersions returns the set of versions already recorded.
func (db *DB) appliedVersions(ctx context.Context) (map[int]bool, error) {
	const op = "storage.appliedVersions"

	rows, err := db.sql.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, errors.Wrap(errors.KindStorage, op, "cannot read applied migrations", err)
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, errors.Wrap(errors.KindStorage, op, "cannot read a migration row", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(errors.KindStorage, op, "cannot iterate applied migrations", err)
	}

	return applied, nil
}

// applyMigration runs one migration and records it atomically.
func (db *DB) applyMigration(ctx context.Context, m migration) error {
	const op = "storage.applyMigration"

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(errors.KindStorage, op,
			fmt.Sprintf("cannot begin a transaction for migration %d", m.Version), err)
	}
	// Rollback after a successful commit returns ErrTxDone, which is expected
	// and ignored; on any error path this is what undoes the partial work.
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return errors.Wrap(errors.KindStorage, op,
			fmt.Sprintf("migration %d (%s) failed", m.Version, m.Name), err)
	}

	// The version and name are values from a compile-time constant list, but
	// they are still bound as parameters so this statement cannot be a template
	// for future callers to fill from untrusted input.
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)",
		m.Version, m.Name, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return errors.Wrap(errors.KindStorage, op,
			fmt.Sprintf("cannot record migration %d", m.Version), err)
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(errors.KindStorage, op,
			fmt.Sprintf("cannot commit migration %d", m.Version), err)
	}

	return nil
}

// CurrentVersion reports the highest migration recorded in the database, or 0
// when none have been applied.
func (db *DB) CurrentVersion(ctx context.Context) (int, error) {
	const op = "storage.CurrentVersion"

	var version sql.NullInt64
	err := db.sql.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&version)
	if err != nil {
		return 0, errors.Wrap(errors.KindStorage, op, "cannot read the schema version", err)
	}
	if !version.Valid {
		return 0, nil
	}

	return int(version.Int64), nil
}
