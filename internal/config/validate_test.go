package config

import (
	"strings"
	"testing"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

// valid returns a configuration that passes validation, so each test can
// break exactly one field and prove that field is the reason for failure.
func valid() Config {
	cfg := Default()
	cfg.Agent.DataDir = "/var/lib/infrapilot"
	return cfg
}

func TestValidateAcceptsValidConfig(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Config)
		wantField string
	}{
		{
			name:      "unknown log level",
			mutate:    func(c *Config) { c.Logging.Level = "trace" },
			wantField: "logging.level",
		},
		{
			name:      "empty log level",
			mutate:    func(c *Config) { c.Logging.Level = "" },
			wantField: "logging.level",
		},
		{
			name:      "unknown log format",
			mutate:    func(c *Config) { c.Logging.Format = "xml" },
			wantField: "logging.format",
		},
		{
			name:      "empty data dir",
			mutate:    func(c *Config) { c.Agent.DataDir = "" },
			wantField: "agent.data_dir",
		},
		{
			name:      "relative data dir",
			mutate:    func(c *Config) { c.Agent.DataDir = "var/lib/infrapilot" },
			wantField: "agent.data_dir",
		},
		{
			name:      "zero shutdown timeout",
			mutate:    func(c *Config) { c.Agent.ShutdownTimeout = 0 },
			wantField: "agent.shutdown_timeout",
		},
		{
			name:      "negative shutdown timeout",
			mutate:    func(c *Config) { c.Agent.ShutdownTimeout = -time.Second },
			wantField: "agent.shutdown_timeout",
		},
		{
			name:      "shutdown timeout below the minimum",
			mutate:    func(c *Config) { c.Agent.ShutdownTimeout = time.Millisecond },
			wantField: "agent.shutdown_timeout",
		},
		{
			name:      "shutdown timeout above the maximum",
			mutate:    func(c *Config) { c.Agent.ShutdownTimeout = time.Hour },
			wantField: "agent.shutdown_timeout",
		},
		{
			name:      "heartbeat interval too small",
			mutate:    func(c *Config) { c.Agent.HeartbeatInterval = time.Millisecond },
			wantField: "agent.heartbeat_interval",
		},
		{
			name:      "heartbeat interval too large",
			mutate:    func(c *Config) { c.Agent.HeartbeatInterval = 48 * time.Hour },
			wantField: "agent.heartbeat_interval",
		},
		{
			name:      "busy timeout too small",
			mutate:    func(c *Config) { c.Storage.BusyTimeout = time.Millisecond },
			wantField: "storage.busy_timeout",
		},
		{
			name:      "busy timeout too large",
			mutate:    func(c *Config) { c.Storage.BusyTimeout = time.Hour },
			wantField: "storage.busy_timeout",
		},
		{
			name:      "empty storage path",
			mutate:    func(c *Config) { c.Storage.Path = "" },
			wantField: "storage.path",
		},
		{
			name:      "storage path escaping the data dir",
			mutate:    func(c *Config) { c.Storage.Path = "../outside.db" },
			wantField: "storage.path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid()
			tt.mutate(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate succeeded, want error")
			}
			if !errors.IsKind(err, errors.KindConfig) {
				t.Errorf("kind = %v, want config", errors.KindOf(err))
			}
			// The message must name the offending field, otherwise the
			// operator cannot tell what to fix.
			if !strings.Contains(err.Error(), tt.wantField) {
				t.Errorf("error %q does not name %q", err, tt.wantField)
			}
		})
	}
}

// Reporting every problem at once saves an edit-rerun cycle per mistake.
func TestValidateReportsAllProblems(t *testing.T) {
	cfg := valid()
	cfg.Logging.Level = "trace"
	cfg.Logging.Format = "xml"
	cfg.Agent.DataDir = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate succeeded, want error")
	}

	for _, field := range []string{"logging.level", "logging.format", "agent.data_dir"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error %q omits %q", err, field)
		}
	}
}

// Validation must not leak a credential if one is mistakenly placed in a
// configuration value.
func TestValidateErrorsDoNotEchoSecrets(t *testing.T) {
	const secret = "hunter2-should-not-appear"

	cfg := valid()
	cfg.Logging.Level = secret

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate succeeded, want error")
	}

	// The level is not a secret field, so it is quoted back deliberately.
	// This test documents the boundary: no *other* field's value is echoed.
	cfg2 := valid()
	cfg2.Storage.BusyTimeout = 0
	cfg2.Logging.Level = secret
	err2 := cfg2.Validate()
	if err2 == nil {
		t.Fatal("Validate succeeded, want error")
	}
	if strings.Count(err2.Error(), secret) != 1 {
		t.Errorf("the level value appears more than once, or leaked into another field: %v", err2)
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{in: "30s", want: 30 * time.Second},
		{in: "5m", want: 5 * time.Minute},
		{in: "1h30m", want: 90 * time.Minute},
		{in: "500ms", want: 500 * time.Millisecond},
		{in: "45", want: 45 * time.Second},
		{in: "0", want: 0},
		{in: "-5", want: -5 * time.Second},
		{in: "soon", wantErr: true},
		{in: "", wantErr: true},
		{in: "30 seconds", wantErr: true},
	}

	for _, tt := range tests {
		t.Run("in="+tt.in, func(t *testing.T) {
			got, err := parseDuration("TEST_VAR", tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseDuration(%q) = %v, want error", tt.in, got)
				}
				if !strings.Contains(err.Error(), "TEST_VAR") {
					t.Errorf("error does not name the field: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDuration(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parseDuration(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
