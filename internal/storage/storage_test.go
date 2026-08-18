package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

// testOptions returns options pointing at a fresh database inside a temporary
// directory that the test framework removes afterwards.
func testOptions(t *testing.T) Options {
	t.Helper()
	return Options{
		Path:        filepath.Join(t.TempDir(), "test.db"),
		BusyTimeout: 2 * time.Second,
	}
}

// openTestDB opens a database and closes it when the test ends.
func openTestDB(t *testing.T) *DB {
	t.Helper()

	db, err := Open(t.Context(), testOptions(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return db
}

func TestOpenCreatesDatabase(t *testing.T) {
	opts := testOptions(t)

	db, err := Open(t.Context(), opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := os.Stat(opts.Path); err != nil {
		t.Fatalf("the database file was not created: %v", err)
	}
	if db.Path() != opts.Path {
		t.Errorf("Path = %q, want %q", db.Path(), opts.Path)
	}
	if db.SQL() == nil {
		t.Error("SQL() is nil on an open database")
	}
}

// The database can describe the host it manages, so it must not be readable by
// other local accounts.
func TestOpenSetsRestrictivePermissions(t *testing.T) {
	t.Run("on a new file", func(t *testing.T) {
		opts := testOptions(t)

		db, err := Open(t.Context(), opts)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer func() { _ = db.Close() }()

		assertMode(t, opts.Path, dbFileMode)
	})

	// An existing database left world-readable by an earlier version, a manual
	// copy, or a permissive umask must be tightened rather than accepted. A
	// zero-length file is a valid empty SQLite database, so this exercises the
	// real path without depending on the process umask.
	t.Run("on an existing permissive file", func(t *testing.T) {
		opts := testOptions(t)
		if err := os.WriteFile(opts.Path, nil, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := os.Chmod(opts.Path, 0o666); err != nil {
			t.Fatalf("chmod: %v", err)
		}

		db, err := Open(t.Context(), opts)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer func() { _ = db.Close() }()

		assertMode(t, opts.Path, dbFileMode)
	})
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode of %s = %#o, want %#o", path, got, want)
	}
}

func TestOpenAppliesPragmas(t *testing.T) {
	db := openTestDB(t)

	var journal string
	if err := db.SQL().QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Errorf("journal_mode = %q, want wal", journal)
	}

	var foreignKeys int
	if err := db.SQL().QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Error("foreign_keys is off, want on")
	}

	// The busy timeout guards against SQLITE_BUSY under contention, so a
	// silent failure to apply it would only show up under load.
	var busy int
	if err := db.SQL().QueryRowContext(t.Context(), "PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if want := int((2 * time.Second).Milliseconds()); busy != want {
		t.Errorf("busy_timeout = %d, want %d", busy, want)
	}
}

func TestOpenRejectsBadOptions(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name string
		opts Options
		kind errors.Kind
	}{
		{
			name: "empty path",
			opts: Options{Path: "", BusyTimeout: time.Second},
			kind: errors.KindConfig,
		},
		{
			name: "relative path",
			opts: Options{Path: "relative.db", BusyTimeout: time.Second},
			kind: errors.KindConfig,
		},
		{
			name: "zero busy timeout",
			opts: Options{Path: filepath.Join(dir, "x.db")},
			kind: errors.KindConfig,
		},
		{
			name: "negative busy timeout",
			opts: Options{Path: filepath.Join(dir, "x.db"), BusyTimeout: -time.Second},
			kind: errors.KindConfig,
		},
		{
			name: "missing parent directory",
			opts: Options{Path: filepath.Join(dir, "absent", "x.db"), BusyTimeout: time.Second},
			kind: errors.KindNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := Open(t.Context(), tt.opts)
			if err == nil {
				_ = db.Close()
				t.Fatal("Open succeeded, want error")
			}
			if !errors.IsKind(err, tt.kind) {
				t.Errorf("kind = %v, want %v (%v)", errors.KindOf(err), tt.kind, err)
			}
		})
	}
}

// Pointing the database path at a directory must be reported clearly rather
// than surfacing a bare driver error.
func TestOpenRejectsDirectoryAsParent(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	db, err := Open(t.Context(), Options{
		Path:        filepath.Join(notADir, "x.db"),
		BusyTimeout: time.Second,
	})
	if err == nil {
		_ = db.Close()
		t.Fatal("Open succeeded, want error")
	}
	if !errors.IsKind(err, errors.KindConfig) {
		t.Errorf("kind = %v, want config (%v)", errors.KindOf(err), err)
	}
}

// A data directory the service account cannot write to is a common
// misconfiguration and must produce a permission error, not a generic one.
func TestOpenUnwritableDirectoryFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	db, err := Open(t.Context(), Options{
		Path:        filepath.Join(locked, "x.db"),
		BusyTimeout: time.Second,
	})
	if err == nil {
		_ = db.Close()
		t.Fatal("Open succeeded on an unwritable directory, want error")
	}
	if kind := errors.KindOf(err); kind != errors.KindPermission && kind != errors.KindStorage {
		t.Errorf("kind = %v, want permission or storage (%v)", kind, err)
	}
}

// A file that is not a database must be refused rather than silently
// overwritten, since it may be an operator's unrelated data.
func TestOpenRejectsNonDatabaseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not.db")
	if err := os.WriteFile(path, []byte("this is not a SQLite database, it is prose"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	db, err := Open(t.Context(), Options{Path: path, BusyTimeout: time.Second})
	if err == nil {
		_ = db.Close()
		t.Fatal("Open succeeded on a non-database file, want error")
	}
	if !errors.IsKind(err, errors.KindStorage) {
		t.Errorf("kind = %v, want storage (%v)", errors.KindOf(err), err)
	}
}

// Opening the same path twice must work: the agent restarting is normal, and
// migrations must be skipped rather than reapplied.
func TestOpenIsIdempotent(t *testing.T) {
	opts := testOptions(t)

	first, err := Open(t.Context(), opts)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	second, err := Open(t.Context(), opts)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer func() { _ = second.Close() }()

	version, err := second.CurrentVersion(t.Context())
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if version != SchemaVersion() {
		t.Errorf("version = %d, want %d", version, SchemaVersion())
	}

	// Each migration records exactly one ledger row, so a reapplied migration
	// would show up here as a duplicate or a primary key violation on open.
	var rows int
	if err := second.SQL().QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM schema_migrations").Scan(&rows); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if rows != SchemaVersion() {
		t.Errorf("ledger has %d rows, want %d", rows, SchemaVersion())
	}
}

func TestCloseIsSafeToRepeatAndOnNil(t *testing.T) {
	var nilDB *DB
	if err := nilDB.Close(); err != nil {
		t.Errorf("Close on a nil DB: %v", err)
	}
	if nilDB.Path() != "" {
		t.Error("Path on a nil DB is not empty")
	}
	if nilDB.SQL() != nil {
		t.Error("SQL on a nil DB is not nil")
	}

	db, err := Open(t.Context(), testOptions(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// database/sql tolerates a repeated Close, and shutdown paths may call it
	// more than once when an error unwinds through several defers.
	if err := db.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// A clean shutdown must checkpoint the WAL so no committed data is left in the
// sidecar file for the next start-up to recover.
func TestCloseCheckpointsWAL(t *testing.T) {
	opts := testOptions(t)

	db, err := Open(t.Context(), opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := db.SQL().ExecContext(t.Context(),
		"CREATE TABLE t (v TEXT) STRICT"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.SQL().ExecContext(t.Context(),
		"INSERT INTO t (v) VALUES (?)", "value"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if info, err := os.Stat(opts.Path + "-wal"); err == nil && info.Size() > 0 {
		t.Errorf("the WAL still holds %d bytes after Close", info.Size())
	}
}

// The database path is not a credential, but errors are shown to operators and
// written to logs, so a failure must not quote the whole connection string.
func TestOpenErrorsDoNotLeakDSN(t *testing.T) {
	db, err := Open(t.Context(), Options{
		Path:        filepath.Join(t.TempDir(), "absent", "x.db"),
		BusyTimeout: time.Second,
	})
	if err == nil {
		_ = db.Close()
		t.Fatal("Open succeeded, want error")
	}
	if strings.Contains(err.Error(), "_pragma") {
		t.Errorf("the error exposes the raw DSN: %v", err)
	}
}
