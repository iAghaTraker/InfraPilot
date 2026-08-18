package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/system"
)

// clearEnv removes every InfraPilot environment variable so a test observes
// only what it sets. t.Setenv restores the prior value when the test ends.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		EnvLogLevel, EnvLogFormat, EnvShutdownTimeout,
		EnvHeartbeatInterval, EnvStoragePath, EnvBusyTimeout,
	} {
		t.Setenv(key, "")
	}
}

// testPaths returns paths rooted in a temporary directory.
func testPaths(t *testing.T) system.Paths {
	t.Helper()
	dir := t.TempDir()
	return system.Paths{
		ConfigFile: filepath.Join(dir, "config.yaml"),
		DataDir:    filepath.Join(dir, "data"),
	}
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestDefaultIsValid(t *testing.T) {
	cfg := Default()
	cfg.Agent.DataDir = "/var/lib/infrapilot"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("default configuration is invalid: %v", err)
	}
}

// A missing file must not be an error: defaults are a complete configuration.
func TestLoadWithoutFileUsesDefaults(t *testing.T) {
	clearEnv(t)
	paths := testPaths(t)

	cfg, err := Load(paths)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	def := Default()
	if cfg.Logging.Level != def.Logging.Level {
		t.Errorf("Level = %q, want %q", cfg.Logging.Level, def.Logging.Level)
	}
	if cfg.Agent.ShutdownTimeout != def.Agent.ShutdownTimeout {
		t.Errorf("ShutdownTimeout = %v, want %v", cfg.Agent.ShutdownTimeout, def.Agent.ShutdownTimeout)
	}
	if cfg.Agent.DataDir != paths.DataDir {
		t.Errorf("DataDir = %q, want %q", cfg.Agent.DataDir, paths.DataDir)
	}
}

func TestLoadFromFile(t *testing.T) {
	clearEnv(t)
	paths := testPaths(t)

	writeConfig(t, paths.ConfigFile, `
version: 1
agent:
  shutdown_timeout: 30s
  heartbeat_interval: 2m
logging:
  level: debug
  format: json
storage:
  path: custom.db
  busy_timeout: 10s
`)

	cfg, err := Load(paths)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Agent.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 30s", cfg.Agent.ShutdownTimeout)
	}
	if cfg.Agent.HeartbeatInterval != 2*time.Minute {
		t.Errorf("HeartbeatInterval = %v, want 2m", cfg.Agent.HeartbeatInterval)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Level = %q, want debug", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("Format = %q, want json", cfg.Logging.Format)
	}
	if cfg.Storage.BusyTimeout != 10*time.Second {
		t.Errorf("BusyTimeout = %v, want 10s", cfg.Storage.BusyTimeout)
	}
}

// A partial file must inherit defaults for the keys it omits.
func TestLoadPartialFileInheritsDefaults(t *testing.T) {
	clearEnv(t)
	paths := testPaths(t)

	writeConfig(t, paths.ConfigFile, "logging:\n  level: warn\n")

	cfg, err := Load(paths)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	def := Default()
	if cfg.Logging.Level != "warn" {
		t.Errorf("Level = %q, want warn", cfg.Logging.Level)
	}
	if cfg.Logging.Format != def.Logging.Format {
		t.Errorf("Format = %q, want the default %q", cfg.Logging.Format, def.Logging.Format)
	}
	if cfg.Agent.ShutdownTimeout != def.Agent.ShutdownTimeout {
		t.Errorf("ShutdownTimeout = %v, want the default %v", cfg.Agent.ShutdownTimeout, def.Agent.ShutdownTimeout)
	}
}

func TestLoadEmptyFileUsesDefaults(t *testing.T) {
	clearEnv(t)
	paths := testPaths(t)
	writeConfig(t, paths.ConfigFile, "")

	cfg, err := Load(paths)
	if err != nil {
		t.Fatalf("Load with an empty file: %v", err)
	}
	if cfg.Logging.Level != Default().Logging.Level {
		t.Errorf("Level = %q, want the default", cfg.Logging.Level)
	}
}

// Plain seconds must work as well as Go duration strings.
func TestLoadAcceptsBareSeconds(t *testing.T) {
	clearEnv(t)
	paths := testPaths(t)
	writeConfig(t, paths.ConfigFile, "agent:\n  shutdown_timeout: 45\n")

	cfg, err := Load(paths)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Agent.ShutdownTimeout != 45*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 45s", cfg.Agent.ShutdownTimeout)
	}
}

// An unknown key must fail loudly: silently ignoring it means an operator
// believes a setting applied when it did not.
func TestLoadRejectsUnknownKey(t *testing.T) {
	clearEnv(t)
	paths := testPaths(t)
	writeConfig(t, paths.ConfigFile, "logging:\n  level: info\n  colour: blue\n")

	_, err := Load(paths)
	if err == nil {
		t.Fatal("unknown key accepted, want error")
	}
	if !errors.IsKind(err, errors.KindConfig) {
		t.Errorf("kind = %v, want config", errors.KindOf(err))
	}
	if !strings.Contains(err.Error(), "colour") {
		t.Errorf("error does not name the offending key: %v", err)
	}

	// yaml.v3 reports an unknown key by printing the Go struct it was decoding
	// into, tags and all. An operator reading that learns nothing about their
	// own file, so the message must name the setting and the line instead.
	for _, leak := range []string{"type struct", "yaml:\\\"", "*string", "unmarshal errors"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error exposes the internal type (%q): %v", leak, err)
		}
	}
	if !strings.Contains(err.Error(), "unknown setting") {
		t.Errorf("error does not explain what is wrong: %v", err)
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("error does not report where the problem is: %v", err)
	}
}

func TestLoadRejectsMalformedYAML(t *testing.T) {
	clearEnv(t)
	paths := testPaths(t)
	writeConfig(t, paths.ConfigFile, "logging:\n  level: [unclosed\n")

	if _, err := Load(paths); err == nil {
		t.Fatal("malformed YAML accepted, want error")
	}
}

func TestLoadRejectsUnknownVersion(t *testing.T) {
	clearEnv(t)
	paths := testPaths(t)
	writeConfig(t, paths.ConfigFile, "version: 99\n")

	err := loadErr(t, paths)
	if err == nil {
		t.Fatal("unknown schema version accepted, want error")
	}
	if !strings.Contains(err.Error(), "99") {
		t.Errorf("error does not mention the version: %v", err)
	}
}

// loadErr loads and returns only the error, keeping tests terse.
func loadErr(t *testing.T, paths system.Paths) error {
	t.Helper()
	_, err := Load(paths)
	return err
}

func TestEnvOverridesFile(t *testing.T) {
	clearEnv(t)
	paths := testPaths(t)

	writeConfig(t, paths.ConfigFile, "logging:\n  level: debug\n  format: text\n")
	t.Setenv(EnvLogLevel, "error")
	t.Setenv(EnvLogFormat, "json")

	cfg, err := Load(paths)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Logging.Level != "error" {
		t.Errorf("Level = %q, want error (the environment must win)", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("Format = %q, want json", cfg.Logging.Format)
	}
}

func TestEnvDurationOverrides(t *testing.T) {
	clearEnv(t)
	paths := testPaths(t)

	t.Setenv(EnvShutdownTimeout, "20s")
	t.Setenv(EnvHeartbeatInterval, "90")
	t.Setenv(EnvBusyTimeout, "2s")

	cfg, err := Load(paths)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Agent.ShutdownTimeout != 20*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 20s", cfg.Agent.ShutdownTimeout)
	}
	if cfg.Agent.HeartbeatInterval != 90*time.Second {
		t.Errorf("HeartbeatInterval = %v, want 90s", cfg.Agent.HeartbeatInterval)
	}
	if cfg.Storage.BusyTimeout != 2*time.Second {
		t.Errorf("BusyTimeout = %v, want 2s", cfg.Storage.BusyTimeout)
	}
}

func TestEnvRejectsBadDuration(t *testing.T) {
	clearEnv(t)
	paths := testPaths(t)
	t.Setenv(EnvShutdownTimeout, "soon")

	err := loadErr(t, paths)
	if err == nil {
		t.Fatal("invalid duration accepted, want error")
	}
	if !strings.Contains(err.Error(), EnvShutdownTimeout) {
		t.Errorf("error does not name the variable: %v", err)
	}
}

// An unreadable file must fail rather than fall back to defaults, because the
// operator's intent could not be read.
func TestLoadUnreadableFileFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}

	clearEnv(t)
	paths := testPaths(t)
	writeConfig(t, paths.ConfigFile, "logging:\n  level: info\n")
	if err := os.Chmod(paths.ConfigFile, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	err := loadErr(t, paths)
	if err == nil {
		t.Fatal("unreadable file accepted, want error")
	}
	if !errors.IsKind(err, errors.KindPermission) {
		t.Errorf("kind = %v, want permission", errors.KindOf(err))
	}
}

func TestDatabasePath(t *testing.T) {
	t.Run("relative resolves inside the data dir", func(t *testing.T) {
		cfg := Default()
		cfg.Agent.DataDir = "/var/lib/infrapilot"

		got, err := cfg.DatabasePath()
		if err != nil {
			t.Fatalf("DatabasePath: %v", err)
		}
		if want := "/var/lib/infrapilot/" + DefaultDatabaseName; got != want {
			t.Errorf("DatabasePath = %q, want %q", got, want)
		}
	})

	t.Run("absolute is honoured", func(t *testing.T) {
		cfg := Default()
		cfg.Agent.DataDir = "/var/lib/infrapilot"
		cfg.Storage.Path = "/srv/data/ip.db"

		got, err := cfg.DatabasePath()
		if err != nil {
			t.Fatalf("DatabasePath: %v", err)
		}
		if got != "/srv/data/ip.db" {
			t.Errorf("DatabasePath = %q, want /srv/data/ip.db", got)
		}
	})

	t.Run("traversal is rejected", func(t *testing.T) {
		cfg := Default()
		cfg.Agent.DataDir = "/var/lib/infrapilot"
		cfg.Storage.Path = "../../etc/passwd"

		if got, err := cfg.DatabasePath(); err == nil {
			t.Fatalf("DatabasePath = %q, want an error for a traversal path", got)
		}
	})
}

// A traversal path in the file must be caught at load time, not at first use.
func TestLoadRejectsTraversalStoragePath(t *testing.T) {
	clearEnv(t)
	paths := testPaths(t)
	writeConfig(t, paths.ConfigFile, "storage:\n  path: ../../etc/shadow\n")

	if err := loadErr(t, paths); err == nil {
		t.Fatal("traversal storage path accepted, want error")
	}
}

func TestPathsAccessor(t *testing.T) {
	clearEnv(t)
	paths := testPaths(t)

	cfg, err := Load(paths)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Paths().ConfigFile != paths.ConfigFile {
		t.Errorf("Paths().ConfigFile = %q, want %q", cfg.Paths().ConfigFile, paths.ConfigFile)
	}
}

// The generated sample must load cleanly and reproduce the defaults, which is
// what keeps documentation and code from drifting apart.
func TestSampleIsLoadable(t *testing.T) {
	clearEnv(t)
	paths := testPaths(t)

	sample, err := Sample(paths.DataDir)
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	writeConfig(t, paths.ConfigFile, sample)

	cfg, err := Load(paths)
	if err != nil {
		t.Fatalf("the generated sample does not load: %v\n%s", err, sample)
	}

	def := Default()
	if cfg.Logging.Level != def.Logging.Level ||
		cfg.Logging.Format != def.Logging.Format ||
		cfg.Agent.ShutdownTimeout != def.Agent.ShutdownTimeout ||
		cfg.Agent.HeartbeatInterval != def.Agent.HeartbeatInterval ||
		cfg.Storage.BusyTimeout != def.Storage.BusyTimeout {
		t.Errorf("the sample does not reproduce the defaults:\ngot  %+v\nwant %+v", cfg, def)
	}
}
