// Package core holds InfraPilot's agent-side domain logic.
//
// It answers questions about an installation — is it healthy, what is its
// state — without deciding how the answers are presented. The CLI, and in
// later versions the web and desktop clients, render what core reports.
//
// This is what "agent-first" means in practice: a client never inspects the
// filesystem, the database or the process table itself. If a client needs to
// know something, core learns it and returns a value.
package core

import (
	"context"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/config"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/storage"
	"github.com/iAghaTraker/InfraPilot/internal/system"
	"github.com/iAghaTraker/InfraPilot/pkg/version"
)

// AgentBinaryName is the agent executable's name. It is used to confirm that a
// PID file points at an actual agent rather than an unrelated process that
// inherited a recycled process ID.
const AgentBinaryName = "infrapilot-agent"

// Status is a point-in-time report on an installation.
//
// Every field is safe to display: nothing here is read from a credential, an
// environment variable or a database row. That is a deliberate constraint, not
// an accident of the current fields.
type Status struct {
	Agent   AgentStatus
	System  SystemStatus
	Storage StorageStatus
	Config  ConfigStatus
}

// AgentStatus describes whether the agent process is running.
type AgentStatus struct {
	// State is running, stopped or unknown.
	State system.ProcessState

	// PID is the recorded process ID, or 0 when there is none.
	PID int

	// Version is the version of the CLI reporting this status.
	//
	// v0.1.0 has no management API, so the running agent's own version cannot
	// be queried. This is the build that produced the report, which is the
	// same binary set in a normal installation.
	Version string

	// Detail explains an unknown state. It is empty otherwise.
	Detail string
}

// SystemStatus describes the host.
type SystemStatus struct {
	OS           string
	Distribution string
	Architecture string
	Kernel       string
	Uptime       time.Duration
	NumCPU       int
}

// StorageStatus describes the database.
type StorageStatus struct {
	// Healthy reports whether the database answered a query.
	Healthy bool

	// Path is the database file location, which may be empty when
	// configuration could not be read.
	Path string

	// SchemaVersion is the migration version recorded in the database.
	SchemaVersion int

	// SizeBytes is the database file size, or 0 when unknown.
	SizeBytes int64

	// Detail explains an unhealthy database. It is empty otherwise.
	Detail string
}

// ConfigStatus describes where configuration was read from.
type ConfigStatus struct {
	// File is the configuration file path.
	File string

	// FileExists reports whether that file is present. A missing file is
	// normal: defaults are a complete configuration.
	FileExists bool

	// DataDir is the resolved data directory.
	DataDir string

	// DevMode reports whether user-local development paths are in use.
	DevMode bool

	// Detail explains a configuration that could not be loaded. It is empty
	// otherwise.
	Detail string
}

// statusTimeout bounds the storage probe so a locked or unresponsive database
// cannot hang a status command.
const statusTimeout = 5 * time.Second

// CollectStatus gathers the current state of an installation.
//
// It never returns an error. A status command must produce a report even when
// parts of the system are broken — that is precisely when it is run — so each
// section carries its own Detail describing what could not be determined.
//
// cfgErr carries a configuration failure from the caller. Passing it in keeps
// the report accurate without making this function decide how configuration is
// discovered.
func CollectStatus(ctx context.Context, cfg config.Config, paths system.Paths, cfgErr error) Status {
	status := Status{
		System: collectSystem(),
		Config: collectConfig(cfg, paths, cfgErr),
	}

	status.Agent = collectAgent(paths)

	// Without valid configuration the database location is unknown, so
	// probing it would report a misleading failure about the wrong path.
	if cfgErr != nil {
		status.Storage = StorageStatus{Detail: "storage cannot be checked until configuration loads"}
		return status
	}

	status.Storage = collectStorage(ctx, cfg)
	return status
}

func collectSystem() SystemStatus {
	info := system.Collect()
	return SystemStatus{
		OS:           info.OS,
		Distribution: info.Distribution,
		Architecture: info.Arch,
		Kernel:       info.Kernel,
		Uptime:       info.Uptime,
		NumCPU:       info.NumCPU,
	}
}

func collectAgent(paths system.Paths) AgentStatus {
	agent := AgentStatus{Version: version.Version}

	pidFile := paths.PIDFile()
	if pidFile == "" {
		agent.State = system.ProcessUnknown
		agent.Detail = "the data directory is not known"
		return agent
	}

	state, pid := system.CheckProcess(pidFile, AgentBinaryName)
	agent.State = state
	agent.PID = pid

	if state == system.ProcessUnknown {
		// The usual cause is an unprivileged user inspecting a root-owned
		// installation, which is worth saying plainly.
		agent.Detail = "the agent state file could not be read; check permissions on " + pidFile
	}

	return agent
}

func collectConfig(cfg config.Config, paths system.Paths, cfgErr error) ConfigStatus {
	status := ConfigStatus{
		File:    paths.ConfigFile,
		DataDir: paths.DataDir,
		DevMode: paths.DevMode,
	}

	if exists, err := system.CheckFilePermissions(paths.ConfigFile); err == nil {
		status.FileExists = exists
	}

	if cfgErr != nil {
		status.Detail = cfgErr.Error()
		return status
	}

	// Configuration may redirect the data directory, in which case the
	// effective value is the one worth reporting.
	if cfg.Agent.DataDir != "" {
		status.DataDir = cfg.Agent.DataDir
	}

	return status
}

func collectStorage(ctx context.Context, cfg config.Config) StorageStatus {
	path, err := cfg.DatabasePath()
	if err != nil {
		return StorageStatus{Detail: "the database path is not usable: " + err.Error()}
	}

	status := StorageStatus{Path: path}

	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	// Read-only, so inspecting an installation never migrates it or alters
	// permissions. A status command must observe, not change.
	db, err := storage.Open(ctx, storage.Options{
		Path:        path,
		BusyTimeout: cfg.Storage.BusyTimeout,
		ReadOnly:    true,
	})
	if err != nil {
		status.Detail = storageDetail(err)
		return status
	}
	defer func() { _ = db.Close() }()

	health := db.Check(ctx)
	status.Healthy = health.Healthy
	status.SchemaVersion = health.SchemaVersion
	status.SizeBytes = health.SizeBytes
	status.Detail = health.Detail

	return status
}

// storageDetail turns an open failure into a short explanation, naming the
// likely fix for the two causes an operator actually hits.
func storageDetail(err error) string {
	switch errors.KindOf(err) {
	case errors.KindNotFound:
		return "the database has not been created yet; start the agent"
	case errors.KindPermission:
		return "the database cannot be read by this user; run as " + system.ServiceUser + " or with sudo"
	default:
		return err.Error()
	}
}
