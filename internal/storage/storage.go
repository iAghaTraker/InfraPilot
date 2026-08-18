// Package storage owns InfraPilot's SQLite database: opening it with safe
// pragmas, applying schema migrations, reporting health, and closing cleanly.
//
// The database is a private implementation detail of the Agent. Nothing outside
// this package holds a *sql.DB, so connection settings, pragmas, and migration
// policy have exactly one home.
//
// In v0.1.0 the schema contains only the migration ledger. Tables are added by
// the version that first needs them, so the schema never describes features
// that do not exist.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"

	// The pure-Go SQLite driver. It registers itself as "sqlite" and needs no
	// C toolchain, so InfraPilot cross-compiles to any target the Go compiler
	// supports and CI does not need cgo.
	_ "modernc.org/sqlite"
)

// driverName is the name modernc.org/sqlite registers with database/sql.
const driverName = "sqlite"

// Connection limits.
//
// SQLite permits one writer at a time. Capping the pool at a single connection
// removes lock contention between goroutines inside this process, which is the
// most common source of SQLITE_BUSY. A single agent does not need write
// concurrency, and this is far easier to reason about than tuning retries.
const (
	maxOpenConns = 1
	maxIdleConns = 1
	connLifetime = time.Hour
)

// dbFileMode is the permission set for the database file. The database may hold
// operational detail about the host, so it is readable only by its owner.
const dbFileMode os.FileMode = 0o600

// Options configures Open.
type Options struct {
	// Path is the database file. It must be an absolute path; the parent
	// directory must already exist.
	Path string

	// BusyTimeout is how long a statement waits for a lock before failing.
	BusyTimeout time.Duration

	// ReadOnly opens an existing database for inspection without writing to
	// it. Read-only handles skip migration and leave file permissions alone,
	// so a command such as "status" cannot alter the agent's data. Opening a
	// database that does not exist fails rather than creating one.
	ReadOnly bool
}

// DB is a handle to the InfraPilot database.
type DB struct {
	sql      *sql.DB
	path     string
	readOnly bool
}

// Open opens the database, applies connection pragmas, and runs any pending
// migrations. The returned DB is ready for use, or the error explains what an
// operator must fix.
//
// Open is safe to call when the file does not exist: SQLite creates it. With
// Options.ReadOnly the file must already exist, and neither its contents nor
// its permissions are modified.
func Open(ctx context.Context, opts Options) (*DB, error) {
	const op = "storage.Open"

	if opts.Path == "" {
		return nil, errors.New(errors.KindConfig, op, "the database path must not be empty")
	}
	if !filepath.IsAbs(opts.Path) {
		return nil, errors.New(errors.KindConfig, op,
			fmt.Sprintf("the database path must be absolute, got %q", opts.Path))
	}
	if opts.BusyTimeout <= 0 {
		return nil, errors.New(errors.KindConfig, op, "the busy timeout must be greater than zero")
	}

	// Fail with a clear message when the parent directory is missing, rather
	// than letting SQLite report a bare "unable to open database file".
	dir := filepath.Dir(opts.Path)
	info, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		return nil, errors.New(errors.KindNotFound, op,
			fmt.Sprintf("the data directory %s does not exist; run 'infrapilot doctor' for guidance", dir))
	case err != nil:
		return nil, errors.Wrap(errors.KindStorage, op,
			fmt.Sprintf("cannot inspect the data directory %s", dir), err)
	case !info.IsDir():
		return nil, errors.New(errors.KindConfig, op,
			fmt.Sprintf("the data directory %s is not a directory", dir))
	}

	// A read-only open of an absent file yields an opaque driver error, so the
	// common case of "the agent has never run" is named explicitly.
	if opts.ReadOnly {
		if _, err := os.Stat(opts.Path); err != nil {
			if os.IsNotExist(err) {
				return nil, errors.New(errors.KindNotFound, op,
					fmt.Sprintf("no database at %s; the agent may not have started yet", opts.Path))
			}
			return nil, errors.Wrap(errors.KindStorage, op,
				fmt.Sprintf("cannot inspect the database at %s", opts.Path), err)
		}
	}

	sqlDB, err := sql.Open(driverName, dsn(opts))
	if err != nil {
		return nil, errors.Wrap(errors.KindStorage, op, "cannot open the database", err)
	}

	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(connLifetime)

	db := &DB{sql: sqlDB, path: opts.Path, readOnly: opts.ReadOnly}

	// sql.Open is lazy, so nothing has touched the file yet. Ping forces a
	// real connection and surfaces permission and corruption problems here,
	// where the error can still be explained clearly.
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, classifyOpenError(op, opts.Path, err)
	}

	if err := db.applyPragmas(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	// A read-only handle must not change the file it is inspecting, so both
	// the permission fix and migration are skipped.
	if opts.ReadOnly {
		return db, nil
	}

	// Tighten permissions after SQLite has created the file. The file is
	// created honouring the process umask, which may be more permissive than
	// intended, so the mode is set explicitly rather than assumed.
	if err := os.Chmod(opts.Path, dbFileMode); err != nil {
		_ = sqlDB.Close()
		return nil, errors.Wrap(errors.KindPermission, op,
			fmt.Sprintf("cannot set permissions on %s", opts.Path), err)
	}

	if err := db.Migrate(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	return db, nil
}

// dsn builds the driver connection string.
//
// Pragmas that must hold for every connection are set here as well as in
// applyPragmas: the DSN covers connections the pool opens later, while
// applyPragmas verifies the settings actually took effect.
func dsn(opts Options) string {
	q := url.Values{}
	q.Set("_pragma", "busy_timeout("+fmt.Sprint(opts.BusyTimeout.Milliseconds())+")")
	q.Add("_pragma", "foreign_keys(ON)")

	if opts.ReadOnly {
		// mode=ro also prevents SQLite from creating the file, which is what
		// makes an absent database an error rather than a silent empty one.
		q.Set("mode", "ro")
	} else {
		// Setting journal_mode writes to the database header, so it is only
		// safe on a writable handle. A read-only connection inherits whatever
		// journal mode the file already records.
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "synchronous(NORMAL)")
	}

	return "file:" + opts.Path + "?" + q.Encode()
}

// applyPragmas confirms the connection-level settings that correctness depends
// on. A pragma that silently failed to apply would mean foreign keys are not
// enforced or writes are not durable, so the values are read back.
func (db *DB) applyPragmas(ctx context.Context) error {
	const op = "storage.applyPragmas"

	// WAL is a property of the file, which a read-only handle cannot set. It
	// is verified only where it was requested.
	if !db.readOnly {
		var journal string
		if err := db.sql.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
			return errors.Wrap(errors.KindStorage, op, "cannot read journal_mode", err)
		}
		// WAL lets readers proceed during a write and survives process crashes.
		if journal != "wal" {
			return errors.New(errors.KindStorage, op,
				fmt.Sprintf("journal_mode is %q, want wal; the filesystem may not support WAL", journal))
		}
	}

	var foreignKeys int
	if err := db.sql.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return errors.Wrap(errors.KindStorage, op, "cannot read foreign_keys", err)
	}
	if foreignKeys != 1 {
		return errors.New(errors.KindStorage, op, "foreign key enforcement is disabled")
	}

	return nil
}

// classifyOpenError maps a driver error to a domain kind so the CLI can pick a
// useful exit code and the message can name a fix.
func classifyOpenError(op, path string, err error) error {
	if os.IsPermission(err) {
		return errors.Wrap(errors.KindPermission, op,
			fmt.Sprintf("cannot open %s; check that the service account owns it", path), err)
	}
	return errors.Wrap(errors.KindStorage, op,
		fmt.Sprintf("cannot open the database at %s", path), err)
}

// Close releases the database. It is safe to call on a nil DB, which keeps
// deferred cleanup simple on the error paths of a caller's start-up sequence.
func (db *DB) Close() error {
	const op = "storage.Close"

	if db == nil || db.sql == nil {
		return nil
	}

	// Checkpoint the WAL so a clean shutdown leaves no sidecar files holding
	// committed data. A failure here is not fatal: the data is committed, and
	// the next open recovers the WAL. A read-only handle must not write, and
	// has nothing of its own to flush.
	if !db.readOnly {
		_, _ = db.sql.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	}

	if err := db.sql.Close(); err != nil {
		return errors.Wrap(errors.KindStorage, op, "cannot close the database", err)
	}
	return nil
}

// Path reports the database file location.
func (db *DB) Path() string {
	if db == nil {
		return ""
	}
	return db.path
}

// SQL exposes the underlying handle for packages that need to run queries.
//
// It exists so that future packages can share this connection rather than
// opening their own, which SQLite's single-writer model does not tolerate well.
func (db *DB) SQL() *sql.DB {
	if db == nil {
		return nil
	}
	return db.sql
}
