package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/config"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/logging"
	"github.com/iAghaTraker/InfraPilot/internal/storage"
	"github.com/iAghaTraker/InfraPilot/internal/system"
)

// syncBuffer is a bytes.Buffer safe for a logger to write to while a test
// reads it. The agent logs from its run goroutine, so this is not optional.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// testHeartbeat is the shortest heartbeat the configuration accepts. The run
// loop is exercised rather than merely entered, without relaxing the validated
// minimum in config just to make a test faster.
const testHeartbeat = time.Second

// testAgent builds an agent over a temporary data directory.
func testAgent(t *testing.T) (*Agent, config.Config, system.Paths, *syncBuffer) {
	t.Helper()

	root := t.TempDir()
	paths := system.Paths{
		ConfigFile: filepath.Join(root, "config.yaml"),
		DataDir:    filepath.Join(root, "data"),
	}

	cfg := config.Default()
	cfg.Agent.DataDir = paths.DataDir
	cfg.Agent.HeartbeatInterval = testHeartbeat
	cfg.Agent.ShutdownTimeout = 5 * time.Second
	cfg.Logging.Level = "debug"

	out := &syncBuffer{}
	logger, err := logging.New(logging.Options{
		Level:  "debug",
		Format: logging.FormatJSON,
		Output: out,
	})
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	return New(Options{Config: cfg, Paths: paths, Logger: logger}), cfg, paths, out
}

// runUntilStarted starts the agent and waits until it has recorded liveness,
// which is the observable signal that start-up finished.
func runUntilStarted(t *testing.T, a *Agent, paths system.Paths) (context.CancelFunc, <-chan error) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- a.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(paths.PIDFile()); err == nil {
			return cancel, result
		}
		select {
		case err := <-result:
			cancel()
			t.Fatalf("the agent exited during start-up: %v", err)
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	t.Fatal("the agent did not start within the deadline")
	return cancel, result
}

// waitForLog blocks until the log contains want, so a test can wait for the
// event it cares about instead of sleeping for a guessed duration.
func waitForLog(t *testing.T, out *syncBuffer, want string) {
	t.Helper()

	deadline := time.Now().Add(3 * testHeartbeat)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the log never recorded %q:\n%s", want, out.String())
}

func TestAgentRunsAndShutsDownCleanly(t *testing.T) {
	a, cfg, paths, out := testAgent(t)

	cancel, result := runUntilStarted(t, a, paths)

	// The database must exist and be migrated by the time start-up completes.
	dbPath, err := cfg.DatabasePath()
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("the database was not created: %v", err)
	}

	// Wait for a tick, so the heartbeat path is covered rather than skipped.
	waitForLog(t, out, "heartbeat")

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}

	// A clean shutdown must remove the PID file, otherwise a stopped agent
	// looks like a running one.
	if _, err := os.Stat(paths.PIDFile()); !os.IsNotExist(err) {
		t.Error("the PID file survived a clean shutdown")
	}

	logged := out.String()
	for _, want := range []string{"starting", "started", "shutting down", "stopped"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the log does not record %q:\n%s", want, logged)
		}
	}
	if !strings.Contains(logged, "heartbeat") {
		t.Errorf("the run loop never produced a heartbeat:\n%s", logged)
	}
}

// The PID file must name this process, so status can distinguish a live agent
// from a stale file.
func TestAgentRecordsItsOwnPID(t *testing.T) {
	a, _, paths, _ := testAgent(t)

	cancel, result := runUntilStarted(t, a, paths)
	defer func() {
		cancel()
		<-result
	}()

	pid, err := system.ReadPIDFile(paths.PIDFile())
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("PID = %d, want %d", pid, os.Getpid())
	}
}

// The data directory holds the database, so the agent must create it with
// restrictive permissions rather than inheriting a permissive umask.
func TestAgentCreatesDataDirSecurely(t *testing.T) {
	a, cfg, paths, _ := testAgent(t)

	cancel, result := runUntilStarted(t, a, paths)
	cancel()
	if err := <-result; err != nil {
		t.Fatalf("Run: %v", err)
	}

	info, err := os.Stat(cfg.Agent.DataDir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != system.DirMode {
		t.Errorf("data directory mode = %#o, want %#o", got, system.DirMode)
	}
	if system.IsWorldAccessible(info.Mode()) {
		t.Error("the data directory is accessible to all local users")
	}

	dbPath, err := cfg.DatabasePath()
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}
	dbInfo, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat database: %v", err)
	}
	if system.IsWorldAccessible(dbInfo.Mode()) {
		t.Errorf("the database is accessible to all local users (mode %#o)", dbInfo.Mode().Perm())
	}
}

// A context already cancelled must not leave a PID file or an open database.
func TestAgentWithCancelledContext(t *testing.T) {
	a, _, paths, _ := testAgent(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := a.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(paths.PIDFile()); !os.IsNotExist(err) {
		t.Error("a PID file was left behind")
	}
}

// Restarting must succeed: migrations are already applied and the previous PID
// file must not block a new start.
func TestAgentRestarts(t *testing.T) {
	a, cfg, paths, _ := testAgent(t)

	for i := range 2 {
		instance := a
		if i > 0 {
			instance = New(Options{Config: cfg, Paths: paths, Logger: logging.Discard()})
		}

		cancel, result := runUntilStarted(t, instance, paths)
		cancel()
		if err := <-result; err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}

	// The schema must not have been reapplied.
	dbPath, err := cfg.DatabasePath()
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}
	db, err := storage.Open(context.Background(), storage.Options{
		Path:        dbPath,
		BusyTimeout: cfg.Storage.BusyTimeout,
		ReadOnly:    true,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	version, err := db.CurrentVersion(context.Background())
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if version != storage.SchemaVersion() {
		t.Errorf("schema version = %d, want %d", version, storage.SchemaVersion())
	}
}

// A stale PID file from a crashed agent must be overwritten, not treated as a
// reason to refuse to start.
func TestAgentOverwritesStalePIDFile(t *testing.T) {
	a, _, paths, _ := testAgent(t)

	if err := system.EnsureDir(paths.DataDir, system.DirMode); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(paths.PIDFile(), []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("write stale PID: %v", err)
	}

	cancel, result := runUntilStartedAfterStale(t, a, paths)
	cancel()
	if err := <-result; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// runUntilStartedAfterStale waits for the PID file to name this process, since
// a stale file already exists and its mere presence proves nothing.
func runUntilStartedAfterStale(t *testing.T, a *Agent, paths system.Paths) (context.CancelFunc, <-chan error) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- a.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pid, err := system.ReadPIDFile(paths.PIDFile()); err == nil && pid == os.Getpid() {
			return cancel, result
		}
		select {
		case err := <-result:
			cancel()
			t.Fatalf("the agent exited during start-up: %v", err)
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	t.Fatal("the agent did not claim the PID file within the deadline")
	return cancel, result
}

func TestAgentRejectsUnsupportedConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.Config, *system.Paths)
		kind   errors.Kind
	}{
		{
			name: "invalid log level",
			mutate: func(c *config.Config, _ *system.Paths) {
				c.Logging.Level = "trace"
			},
			kind: errors.KindConfig,
		},
		{
			name: "relative data directory",
			mutate: func(c *config.Config, _ *system.Paths) {
				c.Agent.DataDir = "relative/path"
			},
			kind: errors.KindConfig,
		},
		{
			name: "database path escaping the data directory",
			mutate: func(c *config.Config, _ *system.Paths) {
				c.Storage.Path = "../../etc/shadow"
			},
			kind: errors.KindConfig,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cfg, paths, _ := testAgent(t)
			tt.mutate(&cfg, &paths)

			a := New(Options{Config: cfg, Paths: paths, Logger: logging.Discard()})

			err := a.Run(context.Background())
			if err == nil {
				t.Fatal("Run succeeded, want error")
			}
			if !errors.IsKind(err, tt.kind) {
				t.Errorf("kind = %v, want %v (%v)", errors.KindOf(err), tt.kind, err)
			}
		})
	}
}

// A data directory the agent cannot create is a hard failure with a clear
// cause, not a crash.
func TestAgentFailsOnUncreatableDataDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can create directories anywhere")
	}

	_, cfg, paths, _ := testAgent(t)

	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg.Agent.DataDir = filepath.Join(locked, "data")
	paths.DataDir = cfg.Agent.DataDir

	a := New(Options{Config: cfg, Paths: paths, Logger: logging.Discard()})

	err := a.Run(context.Background())
	if err == nil {
		t.Fatal("Run succeeded on an uncreatable data directory, want error")
	}
	if kind := errors.KindOf(err); kind != errors.KindPermission && kind != errors.KindStorage {
		t.Errorf("kind = %v, want permission or storage (%v)", kind, err)
	}
}

// A nil logger must not panic: the agent is constructed directly by tests and
// by future callers.
func TestAgentWithNilLogger(t *testing.T) {
	root := t.TempDir()
	paths := system.Paths{DataDir: filepath.Join(root, "data")}

	cfg := config.Default()
	cfg.Agent.DataDir = paths.DataDir
	cfg.Agent.HeartbeatInterval = testHeartbeat

	a := New(Options{Config: cfg, Paths: paths})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if err := a.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// Lifecycle logs go to the journal, so they must never carry a credential.
func TestAgentLogsCarryNoSecrets(t *testing.T) {
	const secret = "hunter2-must-never-appear"
	t.Setenv("PASSWORD", secret)
	t.Setenv("INFRAPILOT_SECRET_PROBE", secret)

	a, _, paths, out := testAgent(t)

	cancel, result := runUntilStarted(t, a, paths)
	// Wait for a heartbeat, so the periodic log line is scrutinised too and not
	// only start-up.
	waitForLog(t, out, "heartbeat")
	cancel()
	if err := <-result; err != nil {
		t.Fatalf("Run: %v", err)
	}

	if logged := out.String(); strings.Contains(logged, secret) {
		t.Errorf("the agent log contains an environment secret:\n%s", logged)
	}
}

// Closing twice must be safe: shutdown closes storage, and Run's deferred
// cleanup runs afterwards on every path.
func TestCloseStorageIsIdempotent(t *testing.T) {
	a, cfg, paths, _ := testAgent(t)

	if err := system.EnsureDir(paths.DataDir, system.DirMode); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := a.openStorage(context.Background()); err != nil {
		t.Fatalf("openStorage: %v", err)
	}

	if err := a.closeStorage(); err != nil {
		t.Fatalf("first closeStorage: %v", err)
	}
	if err := a.closeStorage(); err != nil {
		t.Errorf("second closeStorage: %v", err)
	}

	// The database must still be readable afterwards, proving the close was
	// clean rather than an abort.
	dbPath, err := cfg.DatabasePath()
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}
	db, err := storage.Open(context.Background(), storage.Options{
		Path:        dbPath,
		BusyTimeout: cfg.Storage.BusyTimeout,
		ReadOnly:    true,
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Verify(context.Background()); err != nil {
		t.Errorf("the database is not sound after close: %v", err)
	}
}

// The heartbeat must survive a database that stops answering rather than
// bringing the agent down with it.
func TestHeartbeatToleratesClosedDatabase(t *testing.T) {
	a, _, paths, _ := testAgent(t)

	if err := system.EnsureDir(paths.DataDir, system.DirMode); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := a.openStorage(context.Background()); err != nil {
		t.Fatalf("openStorage: %v", err)
	}
	if err := a.closeStorage(); err != nil {
		t.Fatalf("closeStorage: %v", err)
	}

	// Must not panic with no database present.
	a.heartbeat(context.Background())
}

func TestShutdownReason(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := shutdownReason(ctx); got != "signal" {
		t.Errorf("shutdownReason for a plain cancel = %q, want signal", got)
	}

	cause := errors.New(errors.KindInternal, "test", "the parent gave up")
	causeCtx, causeCancel := context.WithCancelCause(context.Background())
	causeCancel(cause)
	if got := shutdownReason(causeCtx); !strings.Contains(got, "the parent gave up") {
		t.Errorf("shutdownReason = %q, want the cause", got)
	}
}
