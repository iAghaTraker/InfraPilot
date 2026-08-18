package storage

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

func TestCheckReportsHealthy(t *testing.T) {
	db := openTestDB(t)

	health := db.Check(t.Context())

	if !health.Healthy {
		t.Fatalf("Healthy = false, detail: %s", health.Detail)
	}
	if health.Detail != "" {
		t.Errorf("Detail = %q, want empty on a healthy database", health.Detail)
	}
	if health.Path != db.Path() {
		t.Errorf("Path = %q, want %q", health.Path, db.Path())
	}
	if health.SchemaVersion != SchemaVersion() {
		t.Errorf("SchemaVersion = %d, want %d", health.SchemaVersion, SchemaVersion())
	}
	if health.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want a positive size", health.SizeBytes)
	}
}

// A status command must be able to report on storage that never opened, so a
// nil handle is a diagnosis rather than a panic.
func TestCheckOnNilDB(t *testing.T) {
	var db *DB

	health := db.Check(t.Context())

	if health.Healthy {
		t.Error("Healthy = true on a nil database")
	}
	if health.Detail == "" {
		t.Error("Detail is empty, want an explanation")
	}
}

// A closed database must report unhealthy rather than returning an error, so
// the status command still prints a full report.
func TestCheckOnClosedDB(t *testing.T) {
	db, err := Open(t.Context(), testOptions(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	health := db.Check(t.Context())

	if health.Healthy {
		t.Error("Healthy = true on a closed database")
	}
	if health.Detail == "" {
		t.Error("Detail is empty, want an explanation")
	}
}

// A missing ledger means migrations did not complete. The check must notice,
// because every later query would otherwise fail in a less obvious place.
func TestCheckDetectsMissingLedger(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.SQL().ExecContext(t.Context(), "DROP TABLE schema_migrations"); err != nil {
		t.Fatalf("drop the ledger: %v", err)
	}

	health := db.Check(t.Context())

	if health.Healthy {
		t.Error("Healthy = true with no migration ledger")
	}
	if health.Detail == "" {
		t.Error("Detail is empty, want an explanation")
	}
}

// A schema behind the binary means an interrupted upgrade, which must be
// reported instead of quietly running against the wrong tables.
func TestCheckDetectsSchemaMismatch(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.SQL().ExecContext(t.Context(), "DELETE FROM schema_migrations"); err != nil {
		t.Fatalf("clear the ledger: %v", err)
	}

	health := db.Check(t.Context())

	if health.Healthy {
		t.Error("Healthy = true with a stale schema version")
	}
	if !strings.Contains(health.Detail, "schema version") {
		t.Errorf("Detail = %q, want it to mention the schema version", health.Detail)
	}
}

// A cancelled context must produce an unhealthy report promptly rather than
// blocking a status command.
func TestCheckHonoursContextCancellation(t *testing.T) {
	db := openTestDB(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	health := db.Check(ctx)

	if health.Healthy {
		t.Error("Healthy = true with a cancelled context")
	}
}

// Health is rendered to operators, so it must carry no connection string and
// no row contents.
func TestHealthDetailDoesNotLeakDSN(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.SQL().ExecContext(t.Context(), "DROP TABLE schema_migrations"); err != nil {
		t.Fatalf("drop the ledger: %v", err)
	}

	health := db.Check(t.Context())

	if strings.Contains(health.Detail, "_pragma") || strings.Contains(health.Detail, "busy_timeout(") {
		t.Errorf("Detail exposes the DSN: %q", health.Detail)
	}
}

func TestVerify(t *testing.T) {
	db := openTestDB(t)

	if err := db.Verify(t.Context()); err != nil {
		t.Errorf("Verify on a sound database: %v", err)
	}
}

func TestVerifyOnNilDB(t *testing.T) {
	var db *DB

	err := db.Verify(t.Context())
	if err == nil {
		t.Fatal("Verify succeeded on a nil database, want error")
	}
	if !errors.IsKind(err, errors.KindStorage) {
		t.Errorf("kind = %v, want storage", errors.KindOf(err))
	}
}

func TestVerifyOnClosedDB(t *testing.T) {
	db, err := Open(t.Context(), testOptions(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := db.Verify(t.Context()); err == nil {
		t.Fatal("Verify succeeded on a closed database, want error")
	}
}

func TestFileSize(t *testing.T) {
	db := openTestDB(t)

	size, err := db.fileSize()
	if err != nil {
		t.Fatalf("fileSize: %v", err)
	}
	if size <= 0 {
		t.Errorf("fileSize = %d, want a positive size", size)
	}

	info, err := os.Stat(db.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if size != info.Size() {
		t.Errorf("fileSize = %d, want %d", size, info.Size())
	}
}

// A missing file must be reported rather than treated as an empty database.
func TestFileSizeOnMissingFile(t *testing.T) {
	db := &DB{path: "/nonexistent/infrapilot/absent.db"}

	if _, err := db.fileSize(); err == nil {
		t.Fatal("fileSize succeeded on a missing file, want error")
	}
}

func TestDetailFrom(t *testing.T) {
	if got, want := detailFrom("something broke", nil), "something broke"; got != want {
		t.Errorf("detailFrom with a nil error = %q, want %q", got, want)
	}

	err := errors.New(errors.KindStorage, "storage.test", "the disk is full")
	got := detailFrom("cannot write", err)
	if !strings.Contains(got, "cannot write") || !strings.Contains(got, "the disk is full") {
		t.Errorf("detailFrom = %q, want it to carry both parts", got)
	}
}

// The health query is bounded so a locked database reports a problem instead of
// hanging the status command indefinitely.
func TestHealthCheckTimeoutIsBounded(t *testing.T) {
	if healthCheckTimeout <= 0 || healthCheckTimeout > 10*time.Second {
		t.Errorf("healthCheckTimeout = %v, want a short positive bound", healthCheckTimeout)
	}
}
