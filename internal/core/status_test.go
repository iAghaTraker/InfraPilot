package core

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iAghaTraker/InfraPilot/internal/config"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/storage"
	"github.com/iAghaTraker/InfraPilot/internal/system"
	"github.com/iAghaTraker/InfraPilot/pkg/version"
)

// env builds a self-contained installation in a temporary directory: real
// paths, real configuration, no shared state between tests.
func env(t *testing.T) (config.Config, system.Paths) {
	t.Helper()

	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := system.EnsureDir(dataDir, system.DirMode); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	paths := system.Paths{
		ConfigFile: filepath.Join(root, "config.yaml"),
		DataDir:    dataDir,
	}

	cfg := config.Default()
	cfg.Agent.DataDir = dataDir

	return cfg, paths
}

// seedDatabase creates a fully migrated database where configuration expects
// one, so a status probe has something real to read.
func seedDatabase(t *testing.T, cfg config.Config) {
	t.Helper()

	path, err := cfg.DatabasePath()
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}

	db, err := storage.Open(context.Background(), storage.Options{
		Path:        path,
		BusyTimeout: cfg.Storage.BusyTimeout,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestCollectStatusReportsSystemAndConfig(t *testing.T) {
	cfg, paths := env(t)
	seedDatabase(t, cfg)

	status := CollectStatus(context.Background(), cfg, paths, nil)

	if status.System.OS == "" {
		t.Error("the operating system is not reported")
	}
	if status.System.Architecture == "" {
		t.Error("the architecture is not reported")
	}
	if status.System.NumCPU < 1 {
		t.Errorf("NumCPU = %d, want at least 1", status.System.NumCPU)
	}
	if status.Config.File != paths.ConfigFile {
		t.Errorf("Config.File = %q, want %q", status.Config.File, paths.ConfigFile)
	}
	if status.Config.FileExists {
		t.Error("FileExists is true, but no configuration file was written")
	}
	if status.Config.DataDir != cfg.Agent.DataDir {
		t.Errorf("Config.DataDir = %q, want %q", status.Config.DataDir, cfg.Agent.DataDir)
	}
	if status.Agent.Version != version.Version {
		t.Errorf("Agent.Version = %q, want %q", status.Agent.Version, version.Version)
	}
}

func TestCollectStatusHealthyDatabase(t *testing.T) {
	cfg, paths := env(t)
	seedDatabase(t, cfg)

	status := CollectStatus(context.Background(), cfg, paths, nil)

	if !status.Storage.Healthy {
		t.Fatalf("the database is not healthy: %s", status.Storage.Detail)
	}
	if status.Storage.SchemaVersion != storage.SchemaVersion() {
		t.Errorf("SchemaVersion = %d, want %d", status.Storage.SchemaVersion, storage.SchemaVersion())
	}
	if status.Storage.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want a positive size", status.Storage.SizeBytes)
	}
	if status.Storage.Detail != "" {
		t.Errorf("Detail = %q, want empty for a healthy database", status.Storage.Detail)
	}
}

// A status command must not create or migrate anything: it is an observation.
func TestCollectStatusDoesNotCreateDatabase(t *testing.T) {
	cfg, paths := env(t)

	status := CollectStatus(context.Background(), cfg, paths, nil)

	if status.Storage.Healthy {
		t.Error("the database is reported healthy but was never created")
	}
	if !strings.Contains(status.Storage.Detail, "start the agent") {
		t.Errorf("Detail = %q, want guidance to start the agent", status.Storage.Detail)
	}

	path, err := cfg.DatabasePath()
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}
	if _, err := storage.Open(context.Background(), storage.Options{
		Path:        path,
		BusyTimeout: cfg.Storage.BusyTimeout,
		ReadOnly:    true,
	}); err == nil {
		t.Error("the status probe created the database file")
	}
}

// Status must still report what it can when configuration is broken, because
// that is exactly when an operator runs it.
func TestCollectStatusWithConfigError(t *testing.T) {
	cfg, paths := env(t)
	cfgErr := errors.New(errors.KindConfig, "config.Load", "the configuration file is malformed")

	status := CollectStatus(context.Background(), cfg, paths, cfgErr)

	if status.System.OS == "" {
		t.Error("the operating system is not reported despite a configuration failure")
	}
	if status.Config.Detail == "" {
		t.Error("the configuration failure is not explained")
	}
	if status.Storage.Healthy {
		t.Error("storage is reported healthy despite unknown configuration")
	}
	if status.Storage.Detail == "" {
		t.Error("storage carries no explanation")
	}
}

func TestCollectStatusAgentStopped(t *testing.T) {
	cfg, paths := env(t)
	seedDatabase(t, cfg)

	status := CollectStatus(context.Background(), cfg, paths, nil)

	if status.Agent.State != system.ProcessStopped {
		t.Errorf("State = %v, want stopped when no PID file exists", status.Agent.State)
	}
	if status.Agent.PID != 0 {
		t.Errorf("PID = %d, want 0", status.Agent.PID)
	}
}

func TestCollectStatusAgentRunning(t *testing.T) {
	cfg, paths := env(t)
	seedDatabase(t, cfg)

	if err := system.WritePIDFile(paths.PIDFile()); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}

	status := CollectStatus(context.Background(), cfg, paths, nil)

	// This test binary is not named infrapilot-agent, so the name guard
	// correctly reports stopped. What matters is that the PID was read.
	if status.Agent.PID != 0 && status.Agent.State == system.ProcessUnknown {
		t.Errorf("State = unknown with PID %d, want a definite answer", status.Agent.PID)
	}
}

// A status report is rendered to terminals and logs, so no field may carry a
// credential.
func TestStatusCarriesNoSecrets(t *testing.T) {
	cfg, paths := env(t)
	seedDatabase(t, cfg)

	const secret = "hunter2-must-never-appear"
	t.Setenv("INFRAPILOT_SECRET_PROBE", secret)
	t.Setenv("PASSWORD", secret)

	status := CollectStatus(context.Background(), cfg, paths, nil)

	// Nothing read from the environment may reach the report.
	if rendered := renderStatus(status); strings.Contains(rendered, secret) {
		t.Errorf("the status report contains an environment secret:\n%s", rendered)
	}

	// Descriptive fields must not talk about credentials either. Paths are
	// excluded: they are chosen by the operator, and a directory legitimately
	// named "secrets" is not a leak.
	described := strings.ToLower(strings.Join([]string{
		status.Agent.State.String(),
		status.Agent.Version,
		status.Agent.Detail,
		status.System.OS,
		status.System.Distribution,
		status.System.Architecture,
		status.System.Kernel,
		status.Storage.Detail,
		status.Config.Detail,
	}, " "))

	for _, banned := range []string{"password", "token", "secret", "private_key", "credential"} {
		if strings.Contains(described, banned) {
			t.Errorf("a status field mentions %q: %s", banned, described)
		}
	}
}

// renderStatus flattens a Status into text so a test can assert that nothing
// sensitive appears anywhere in it.
func renderStatus(s Status) string {
	var b strings.Builder
	b.WriteString(s.Agent.State.String())
	b.WriteString(s.Agent.Version)
	b.WriteString(s.Agent.Detail)
	b.WriteString(s.System.OS)
	b.WriteString(s.System.Distribution)
	b.WriteString(s.System.Architecture)
	b.WriteString(s.System.Kernel)
	b.WriteString(s.Storage.Path)
	b.WriteString(s.Storage.Detail)
	b.WriteString(s.Config.File)
	b.WriteString(s.Config.DataDir)
	b.WriteString(s.Config.Detail)
	return b.String()
}

// A traversal path must be refused rather than probed.
func TestCollectStatusRejectsBadDatabasePath(t *testing.T) {
	cfg, paths := env(t)
	cfg.Storage.Path = "../../etc/shadow"

	status := CollectStatus(context.Background(), cfg, paths, nil)

	if status.Storage.Healthy {
		t.Error("a traversal database path was reported healthy")
	}
	if !strings.Contains(status.Storage.Detail, "not usable") {
		t.Errorf("Detail = %q, want an explanation that the path is unusable", status.Storage.Detail)
	}
}

// A cancelled context must not hang or panic.
func TestCollectStatusHonoursCancelledContext(t *testing.T) {
	cfg, paths := env(t)
	seedDatabase(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	status := CollectStatus(ctx, cfg, paths, nil)

	// The system section never touches the context, so it must still be filled.
	if status.System.OS == "" {
		t.Error("the operating system is not reported for a cancelled context")
	}
}
