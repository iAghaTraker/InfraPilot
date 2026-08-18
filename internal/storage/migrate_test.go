package storage

import (
	"strings"
	"testing"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

// Every migration must be uniquely numbered and strictly increasing. A
// duplicate or out-of-order version would silently skip a schema change on
// some installations, so this is checked at test time rather than trusted.
func TestMigrationsAreOrderedAndUnique(t *testing.T) {
	if len(migrations) == 0 {
		t.Fatal("no migrations are defined")
	}

	seen := make(map[int]bool, len(migrations))
	previous := 0

	for i, m := range migrations {
		if m.Version <= 0 {
			t.Errorf("migration %d has version %d, want a positive number", i, m.Version)
		}
		if seen[m.Version] {
			t.Errorf("migration version %d is defined more than once", m.Version)
		}
		seen[m.Version] = true

		if m.Version <= previous {
			t.Errorf("migration %d has version %d, which does not follow %d",
				i, m.Version, previous)
		}
		previous = m.Version

		if strings.TrimSpace(m.Name) == "" {
			t.Errorf("migration %d has no name", m.Version)
		}
		if strings.TrimSpace(m.SQL) == "" {
			t.Errorf("migration %d has no SQL", m.Version)
		}
	}
}

// v0.1.0 ships only the ledger. This guards the scope rule that tables arrive
// with the feature that needs them, so a stray speculative table fails the build.
func TestSchemaVersionMatchesShippedMigrations(t *testing.T) {
	if got, want := SchemaVersion(), 2; got != want {
		t.Errorf("SchemaVersion = %d, want %d", got, want)
	}
}

func TestMigrateAppliesLedger(t *testing.T) {
	db := openTestDB(t)

	// Open already migrated, so the ledger must exist and be at the latest
	// version.
	version, err := db.CurrentVersion(t.Context())
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if version != SchemaVersion() {
		t.Errorf("version = %d, want %d", version, SchemaVersion())
	}

	var name string
	if err := db.SQL().QueryRowContext(t.Context(),
		"SELECT name FROM schema_migrations WHERE version = 1").Scan(&name); err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	if name != "create_schema_migrations" {
		t.Errorf("name = %q, want create_schema_migrations", name)
	}

	// The timestamp must be a parseable RFC 3339 instant so an operator can
	// tell when a schema change landed.
	var appliedAt string
	if err := db.SQL().QueryRowContext(t.Context(),
		"SELECT applied_at FROM schema_migrations WHERE version = 1").Scan(&appliedAt); err != nil {
		t.Fatalf("read applied_at: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, appliedAt); err != nil {
		t.Errorf("applied_at %q is not RFC 3339: %v", appliedAt, err)
	}
}

// Migrate runs on every start-up, so repeating it must be a no-op.
func TestMigrateIsIdempotent(t *testing.T) {
	db := openTestDB(t)

	for i := range 3 {
		if err := db.Migrate(t.Context()); err != nil {
			t.Fatalf("Migrate run %d: %v", i+1, err)
		}
	}

	var rows int
	if err := db.SQL().QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM schema_migrations").Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != SchemaVersion() {
		t.Errorf("ledger has %d rows after repeated migration, want %d", rows, SchemaVersion())
	}
}

// A database written by a newer InfraPilot must be refused. Running an old
// binary against a new schema risks writing data the new version cannot read,
// and no migration can undo that.
func TestMigrateRefusesNewerSchema(t *testing.T) {
	db := openTestDB(t)

	future := SchemaVersion() + 10
	if _, err := db.SQL().ExecContext(t.Context(),
		"INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)",
		future, "from_the_future", time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("seed a future version: %v", err)
	}

	err := db.Migrate(t.Context())
	if err == nil {
		t.Fatal("Migrate accepted a newer schema, want error")
	}
	if !errors.IsKind(err, errors.KindStorage) {
		t.Errorf("kind = %v, want storage", errors.KindOf(err))
	}
	// The operator needs both numbers to understand what to do.
	msg := err.Error()
	if !strings.Contains(msg, "10") && !strings.Contains(msg, "11") {
		t.Errorf("the error does not report the database version: %v", err)
	}
	if !strings.Contains(msg, "upgrade") {
		t.Errorf("the error does not suggest a fix: %v", err)
	}
}

// Reopening a database whose schema is ahead must fail during Open, not later
// at first query, so the agent refuses to start rather than half-working.
func TestOpenRefusesNewerSchema(t *testing.T) {
	opts := testOptions(t)

	db, err := Open(t.Context(), opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := db.SQL().ExecContext(t.Context(),
		"INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)",
		SchemaVersion()+1, "from_the_future", time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, reopenErr := Open(t.Context(), opts)
	if reopenErr == nil {
		_ = reopened.Close()
		t.Fatal("Open accepted a newer schema, want error")
	}
}

// A migration that fails must leave nothing behind, or a retry would hit a
// half-created schema that no version knows how to repair.
func TestApplyMigrationRollsBackOnFailure(t *testing.T) {
	db := openTestDB(t)

	broken := migration{
		Version: 999,
		Name:    "broken",
		SQL: `
CREATE TABLE good_table (id INTEGER PRIMARY KEY) STRICT;
THIS IS NOT VALID SQL;`,
	}

	err := db.applyMigration(t.Context(), broken)
	if err == nil {
		t.Fatal("applyMigration succeeded on invalid SQL, want error")
	}
	if !errors.IsKind(err, errors.KindStorage) {
		t.Errorf("kind = %v, want storage", errors.KindOf(err))
	}

	// Neither the table nor the ledger entry may survive the rollback.
	var tables int
	if err := db.SQL().QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'good_table'",
	).Scan(&tables); err != nil {
		t.Fatalf("inspect sqlite_master: %v", err)
	}
	if tables != 0 {
		t.Error("the partially applied table survived the rollback")
	}

	var recorded int
	if err := db.SQL().QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM schema_migrations WHERE version = 999").Scan(&recorded); err != nil {
		t.Fatalf("inspect the ledger: %v", err)
	}
	if recorded != 0 {
		t.Error("a failed migration was recorded in the ledger")
	}

	// The database must still be usable afterwards.
	if version, err := db.CurrentVersion(t.Context()); err != nil {
		t.Errorf("CurrentVersion after a failed migration: %v", err)
	} else if version != SchemaVersion() {
		t.Errorf("version = %d, want %d", version, SchemaVersion())
	}
}

// The ledger uses STRICT typing, so a migration recorded twice must be rejected
// by the primary key rather than duplicated.
func TestLedgerRejectsDuplicateVersion(t *testing.T) {
	db := openTestDB(t)

	_, err := db.SQL().ExecContext(t.Context(),
		"INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)",
		1, "duplicate", time.Now().UTC().Format(time.RFC3339))
	if err == nil {
		t.Fatal("a duplicate ledger version was accepted, want a primary key violation")
	}
}

// CurrentVersion must report 0 rather than failing when the ledger is empty,
// so a fresh database is distinguishable from a broken one.
func TestCurrentVersionOnEmptyLedger(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.SQL().ExecContext(t.Context(), "DELETE FROM schema_migrations"); err != nil {
		t.Fatalf("clear the ledger: %v", err)
	}

	version, err := db.CurrentVersion(t.Context())
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if version != 0 {
		t.Errorf("version = %d, want 0", version)
	}
}
