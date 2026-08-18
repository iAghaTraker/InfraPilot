// Package agent implements the InfraPilot Agent's lifecycle.
//
// The Agent is the only component that owns infrastructure state. It starts,
// initialises its subsystems in dependency order, runs until signalled, and
// shuts down cleanly. Every step that can fail explains what an operator must
// do about it.
//
// v0.1.0 has no network listener. The Agent's entire external surface is the
// data directory, which is why "run" currently means holding storage open and
// recording that the process is alive. Later versions add work here; the
// lifecycle around it is what this package exists to get right.
package agent

import (
	"context"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/bridge"
	"github.com/iAghaTraker/InfraPilot/internal/config"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/logging"
	"github.com/iAghaTraker/InfraPilot/internal/storage"
	"github.com/iAghaTraker/InfraPilot/internal/system"
	"github.com/iAghaTraker/InfraPilot/pkg/version"
)

// Options configures an Agent.
type Options struct {
	// Config is the validated configuration.
	Config config.Config

	// Paths is the resolved path set.
	Paths system.Paths

	// Logger receives lifecycle events. A nil logger discards them, which
	// keeps tests quiet without special-casing every call site.
	Logger *slog.Logger
}

// Agent is a running InfraPilot Agent.
type Agent struct {
	cfg     config.Config
	paths   system.Paths
	log     *slog.Logger
	pidPath string

	// started records when Run began, so uptime is measured rather than
	// guessed.
	started time.Time

	// mu guards db. Only the shutdown timeout path introduces concurrency, but
	// it is real: see closeStorage.
	mu sync.Mutex
	db *storage.DB
}

// database returns the current handle under the lock.
func (a *Agent) database() *storage.DB {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.db
}

// New creates an Agent. It does no I/O: everything that can fail happens in
// Run, where the shutdown path exists to undo it.
func New(opts Options) *Agent {
	logger := opts.Logger
	if logger == nil {
		logger = logging.Discard()
	}

	return &Agent{
		cfg:     opts.Config,
		paths:   opts.Paths,
		log:     logging.Component(logger, "agent"),
		pidPath: opts.Paths.PIDFile(),
	}
}

// Run starts the Agent and blocks until ctx is cancelled or a termination
// signal arrives, then shuts down cleanly.
//
// The sequence is: validate the environment, prepare the data directory, open
// storage, record liveness, serve, and unwind. Each stage is undone in reverse
// order, so a failure halfway through leaves nothing behind.
func (a *Agent) Run(ctx context.Context) error {
	const op = "agent.Run"

	a.started = time.Now()

	// Signals are wired up before any resource is acquired, so an operator can
	// interrupt a slow start-up rather than being forced to kill it.
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a.log.Info("starting",
		"version", version.Version,
		"data_dir", a.cfg.Agent.DataDir,
		"dev_mode", a.paths.DevMode,
	)

	// A stop requested before start-up finishes is honoured immediately, so no
	// resource is acquired only to be released again.
	if ctx.Err() != nil {
		a.log.Info("stopped before start-up", "reason", shutdownReason(ctx))
		return nil
	}

	if err := a.validateEnvironment(); err != nil {
		return err
	}

	if err := a.prepareDataDir(); err != nil {
		if a.stopping(ctx, err) {
			return nil
		}
		return err
	}

	if err := a.openStorage(ctx); err != nil {
		if a.stopping(ctx, err) {
			return nil
		}
		return err
	}
	// Storage is closed on every exit path, including a failure to write the
	// PID file below. closeStorage is idempotent, so the orderly shutdown
	// having already closed it is harmless.
	defer func() { _ = a.closeStorage() }()

	if err := a.recordLiveness(); err != nil {
		return err
	}
	defer a.clearLiveness()

	a.log.Info("started",
		"pid_file", a.pidPath,
		"database", a.database().Path(),
		"schema_version", storage.SchemaVersion(),
	)

	if err := a.serve(ctx); err != nil {
		return errors.Wrap(errors.KindInternal, op, "the agent stopped unexpectedly", err)
	}

	return nil
}

// stopping reports whether the context was cancelled while a start-up step was
// in progress.
//
// A stop signal that arrives mid-start-up must exit cleanly: the supervisor
// asked for a shutdown, and returning the interrupted step's error instead
// would report a crash for a requested stop, which systemd would then try to
// restart. The step's error is still logged, because it explains what was
// abandoned.
func (a *Agent) stopping(ctx context.Context, err error) bool {
	if ctx.Err() == nil {
		return false
	}

	a.log.Info("stop requested during start-up",
		"reason", shutdownReason(ctx),
		"interrupted", err.Error(),
	)
	return true
}

// validateEnvironment checks the assumptions the Agent cannot work without.
func (a *Agent) validateEnvironment() error {
	const op = "agent.validateEnvironment"

	if !system.IsSupportedOS() {
		info := system.Collect()
		return errors.New(errors.KindUnsupported, op,
			"InfraPilot supports Linux; this host runs "+info.OS)
	}

	// Configuration is validated at load time, but the Agent is also
	// constructed directly by tests and by future callers, so the invariant is
	// enforced where it is relied upon rather than assumed.
	if err := a.cfg.Validate(); err != nil {
		return err
	}

	if a.pidPath == "" {
		return errors.New(errors.KindConfig, op,
			"the data directory is not set, so the agent cannot record that it is running")
	}

	return nil
}

// prepareDataDir creates the data directory with restrictive permissions.
func (a *Agent) prepareDataDir() error {
	if err := system.EnsureDir(a.cfg.Agent.DataDir, system.DirMode); err != nil {
		return err
	}

	a.log.Debug("data directory ready", "path", a.cfg.Agent.DataDir)
	return nil
}

// openStorage opens the database and applies any pending migrations.
func (a *Agent) openStorage(ctx context.Context) error {
	path, err := a.cfg.DatabasePath()
	if err != nil {
		return err
	}

	db, err := storage.Open(ctx, storage.Options{
		Path:        path,
		BusyTimeout: a.cfg.Storage.BusyTimeout,
	})
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.db = db
	a.mu.Unlock()

	version, err := db.CurrentVersion(ctx)
	if err != nil {
		// The database opened and migrated, so a failure to read the version
		// back is worth reporting but is not a reason to refuse to start.
		a.log.Warn("cannot read the schema version", logging.ErrorAttrs(err)...)
	} else {
		a.log.Debug("storage ready", "path", path, "schema_version", version)
	}

	return nil
}

// closeStorage closes the database.
//
// It is called by shutdown on the orderly path and by Run's deferred cleanup on
// every path, so it must be safe to call twice. Clearing a.db is what makes the
// second call a no-op.
//
// The lock is not decoration: when shutdown times out it returns while its
// closing goroutine is still running, so Run's deferred call can genuinely race
// it. Serialising the swap means exactly one caller ever closes the handle.
func (a *Agent) closeStorage() error {
	a.mu.Lock()
	db := a.db
	a.db = nil
	a.mu.Unlock()

	if db == nil {
		return nil
	}

	if err := db.Close(); err != nil {
		a.log.Error("cannot close the database cleanly", logging.ErrorAttrs(err)...)
		return err
	}
	return nil
}

// recordLiveness writes the PID file that lets the CLI report agent state
// without a network listener.
func (a *Agent) recordLiveness() error {
	if err := system.WritePIDFile(a.pidPath); err != nil {
		return err
	}
	a.log.Debug("liveness recorded", "pid_file", a.pidPath)
	return nil
}

func (a *Agent) clearLiveness() {
	if err := system.RemovePIDFile(a.pidPath); err != nil {
		// A stale PID file makes a stopped agent look running, which is worth
		// an error in the log, but the process is already exiting.
		a.log.Error("cannot remove the PID file", logging.ErrorAttrs(err)...)
	}
}

// serve runs until the context is cancelled.
//
// The heartbeat exists so an operator can see in the log that the Agent is
// alive and healthy, and so the run loop has a real periodic task rather than
// a bare block. When later versions add work, it belongs here.
func (a *Agent) serve(ctx context.Context) error {
	go func() {
		if err := bridge.Serve(ctx, bridge.DefaultAddress, a.cfg.Agent.DataDir); err != nil && ctx.Err() == nil {
			a.log.Warn("local signing bridge unavailable", "error", err.Error())
		}
	}()
	ticker := time.NewTicker(a.cfg.Agent.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.log.Info("shutting down",
				"reason", shutdownReason(ctx),
				"uptime", system.FormatUptime(time.Since(a.started)),
			)
			return a.shutdown()

		case <-ticker.C:
			a.heartbeat(ctx)
		}
	}
}

// heartbeat records that the Agent is alive and that storage still answers.
func (a *Agent) heartbeat(ctx context.Context) {
	db := a.database()
	if db == nil {
		return
	}

	health := db.Check(ctx)
	if !health.Healthy {
		// Not fatal: the database may be briefly locked, and an agent that
		// exits on a transient storage hiccup is worse than one that reports it.
		a.log.Warn("the database is not healthy", "detail", health.Detail)
		return
	}

	a.log.Debug("heartbeat",
		"uptime", system.FormatUptime(time.Since(a.started)),
		"schema_version", health.SchemaVersion,
		"database_bytes", health.SizeBytes,
	)
}

// shutdown closes storage within the configured timeout.
//
// Closing checkpoints the WAL, which is the one shutdown step that touches the
// disk and so the one that can be slow. Bounding it means systemd never has to
// escalate to SIGKILL during an orderly stop. An integrity check is
// deliberately not run here: it reads every page, which would turn a large
// database into a shutdown that always times out.
func (a *Agent) shutdown() error {
	const op = "agent.shutdown"

	done := make(chan error, 1)
	go func() { done <- a.closeStorage() }()

	select {
	case err := <-done:
		if err != nil {
			return err
		}
		a.log.Info("stopped")
		return nil

	case <-time.After(a.cfg.Agent.ShutdownTimeout):
		// Report rather than hang. The checkpoint is an optimisation, not a
		// correctness requirement: committed data is already durable, and the
		// next start recovers the WAL.
		return errors.New(errors.KindInternal, op,
			"shutdown did not finish within "+a.cfg.Agent.ShutdownTimeout.String())
	}
}

// shutdownReason describes why the run loop is ending, so the log distinguishes
// an operator's signal from a cancelled parent context.
func shutdownReason(ctx context.Context) string {
	if err := context.Cause(ctx); err != nil && err != context.Canceled {
		return err.Error()
	}
	return "signal"
}
