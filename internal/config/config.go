// Package config defines InfraPilot's configuration and how it is loaded.
//
// Configuration is resolved in layers, each overriding the previous one:
//
//  1. Defaults, so InfraPilot runs correctly with no configuration file.
//  2. The YAML configuration file, if present.
//  3. Environment variables, for container and systemd deployments.
//
// The result is validated before it is returned. A process must never start
// with configuration that is syntactically valid but operationally wrong, so
// Load returns an error rather than silently correcting a bad value.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/logging"
	"github.com/iAghaTraker/InfraPilot/internal/system"
)

// Config is the complete runtime configuration.
//
// Only fields that v0.1.0 actually uses are present. Adding fields for planned
// features would create configuration that silently does nothing, which is
// worse than no configuration at all.
// The on-disk representation lives in fileConfig (see file.go); this type is
// the validated in-memory result, so it carries no YAML tags.
type Config struct {
	Agent   AgentConfig
	Logging LoggingConfig
	Storage StorageConfig

	// paths records where this configuration was resolved from. It is process
	// state rather than user input, so it is not settable from a file.
	paths system.Paths
}

// AgentConfig configures the Agent daemon.
type AgentConfig struct {
	// DataDir is where the Agent keeps its state. Empty means the resolved
	// default for the current mode.
	DataDir string

	// ShutdownTimeout bounds graceful shutdown. When it elapses, the Agent
	// stops waiting and exits, so a stuck subsystem cannot block a restart
	// indefinitely. systemd's TimeoutStopSec must exceed this.
	ShutdownTimeout time.Duration

	// HeartbeatInterval is how often the Agent records that it is alive.
	// In v0.1.0 this is written to the log only; there is no remote endpoint.
	HeartbeatInterval time.Duration
}

// LoggingConfig configures log output.
type LoggingConfig struct {
	// Level is debug, info, warn or error.
	Level string

	// Format is text or json. Under systemd, json is easier to ship onward.
	Format string
}

// StorageConfig configures the local database.
type StorageConfig struct {
	// Path is the database file location. A relative value is resolved inside
	// the data directory; an absolute value is used as given.
	Path string

	// BusyTimeout is how long a query waits for a lock before failing.
	BusyTimeout time.Duration
}

// DefaultDatabaseName is the database file name inside the data directory.
const DefaultDatabaseName = "infrapilot.db"

// Default returns the configuration used when nothing is specified.
//
// The defaults are chosen so that a fresh install works unattended: info-level
// text logs, a database in the data directory, and timeouts long enough for a
// slow disk but short enough that a restart is not perceived as a hang.
func Default() Config {
	return Config{
		Agent: AgentConfig{
			ShutdownTimeout:   15 * time.Second,
			HeartbeatInterval: 60 * time.Second,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: string(logging.FormatText),
		},
		Storage: StorageConfig{
			Path:        DefaultDatabaseName,
			BusyTimeout: 5 * time.Second,
		},
	}
}

// Paths reports where configuration and state were resolved to.
func (c Config) Paths() system.Paths { return c.paths }

// DatabasePath returns the absolute path to the database file.
//
// A relative Storage.Path is resolved inside the data directory and is checked
// against path traversal, because it may come from a file an operator edited.
func (c Config) DatabasePath() (string, error) {
	const op = "config.DatabasePath"

	path := strings.TrimSpace(c.Storage.Path)
	if path == "" {
		path = DefaultDatabaseName
	}

	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	resolved, err := system.ResolveInDir(c.Agent.DataDir, path)
	if err != nil {
		return "", errors.Wrap(errors.KindConfig, op,
			"storage.path must stay inside the data directory", err)
	}
	return resolved, nil
}

// Load resolves configuration from defaults, the file at paths.ConfigFile and
// the environment, then validates the result.
//
// A missing configuration file is not an error: defaults plus environment are a
// complete configuration. An unreadable or malformed file is an error, because
// the operator's stated intent could not be honoured and guessing would be
// worse than refusing to start.
func Load(paths system.Paths) (Config, error) {
	const op = "config.Load"

	cfg := Default()
	cfg.paths = paths

	// The data directory default comes from path resolution, so that dev mode
	// and production agree without duplicating the logic here.
	cfg.Agent.DataDir = paths.DataDir

	if err := applyFile(&cfg, paths.ConfigFile); err != nil {
		return Config{}, err
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}

	// The data directory must be absolute before storage paths resolve
	// against it, so this is normalised before validation runs.
	cfg.Agent.DataDir = strings.TrimSpace(cfg.Agent.DataDir)
	if cfg.Agent.DataDir == "" {
		cfg.Agent.DataDir = paths.DataDir
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, errors.Wrapf(errors.KindConfig, op, err,
			"invalid configuration (from %s)", paths.ConfigFile)
	}

	return cfg, nil
}

// applyFile overlays the YAML file at path onto cfg.
func applyFile(cfg *Config, path string) error {
	const op = "config.applyFile"

	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Defaults are a valid configuration.
			return nil
		}
		if os.IsPermission(err) {
			return errors.Wrapf(errors.KindPermission, op, err,
				"cannot read configuration file %s", path)
		}
		return errors.Wrapf(errors.KindConfig, op, err,
			"cannot read configuration file %s", path)
	}

	fc, err := decodeFile(path, data)
	if err != nil {
		return err
	}

	return fc.overlay(cfg, path)
}

// Environment variables recognised by applyEnv. INFRAPILOT_CONFIG and
// INFRAPILOT_DATA_DIR are handled during path resolution, not here.
const (
	EnvLogLevel          = "INFRAPILOT_LOG_LEVEL"
	EnvLogFormat         = "INFRAPILOT_LOG_FORMAT"
	EnvShutdownTimeout   = "INFRAPILOT_SHUTDOWN_TIMEOUT"
	EnvHeartbeatInterval = "INFRAPILOT_HEARTBEAT_INTERVAL"
	EnvStoragePath       = "INFRAPILOT_STORAGE_PATH"
	EnvBusyTimeout       = "INFRAPILOT_BUSY_TIMEOUT"
)

// applyEnv overlays environment variables onto cfg.
//
// Only variables that are set take effect; an unset variable leaves the
// previous layer untouched. A set-but-invalid variable is an error.
func applyEnv(cfg *Config) error {
	if v, ok := lookup(EnvLogLevel); ok {
		cfg.Logging.Level = v
	}
	if v, ok := lookup(EnvLogFormat); ok {
		cfg.Logging.Format = v
	}
	if v, ok := lookup(EnvStoragePath); ok {
		cfg.Storage.Path = v
	}

	durations := []struct {
		env    string
		target *time.Duration
	}{
		{EnvShutdownTimeout, &cfg.Agent.ShutdownTimeout},
		{EnvHeartbeatInterval, &cfg.Agent.HeartbeatInterval},
		{EnvBusyTimeout, &cfg.Storage.BusyTimeout},
	}

	for _, d := range durations {
		v, ok := lookup(d.env)
		if !ok {
			continue
		}
		parsed, err := parseDuration(d.env, v)
		if err != nil {
			return err
		}
		*d.target = parsed
	}

	return nil
}

func lookup(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	return v, true
}

// parseDuration accepts either a Go duration string ("30s") or a bare number
// of seconds ("30"), because operators reasonably expect both.
func parseDuration(env, value string) (time.Duration, error) {
	const op = "config.parseDuration"

	if d, err := time.ParseDuration(value); err == nil {
		return d, nil
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second, nil
	}

	return 0, errors.Newf(errors.KindValidation, op,
		"%s=%q is not a valid duration: use a value such as 30s, 5m or a plain number of seconds",
		env, value)
}
