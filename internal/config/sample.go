package config

import (
	"strings"
	"text/template"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

// sampleTemplate is the annotated configuration file shipped by the installer.
//
// It is a template rather than a static string so that the documented values
// are the real defaults from Default(). A sample that drifts from the code is
// worse than no sample, because it teaches the wrong thing.
const sampleTemplate = `# InfraPilot configuration
#
# Every setting below is optional: InfraPilot runs on its built-in defaults
# when this file is absent. The values shown are those defaults.
#
# Permissions: this file is owned by root, group infrapilot, mode 0640. The
# Agent only reads it, so it must not be writable by the service account, and
# it must never be world-readable.
#
# Environment variables override these values. See docs/configuration.md.

version: {{ .Version }}

agent:
  # Directory holding InfraPilot state, including the database.
  data_dir: {{ .DataDir }}

  # How long the Agent waits for a graceful shutdown before exiting.
  # Keep systemd's TimeoutStopSec above this value.
  shutdown_timeout: {{ .ShutdownTimeout }}

  # How often the Agent records a liveness entry in its log.
  heartbeat_interval: {{ .HeartbeatInterval }}

logging:
  # One of: debug, info, warn, error.
  level: {{ .LogLevel }}

  # One of: text, json. Use json when shipping logs to an aggregator.
  format: {{ .LogFormat }}

storage:
  # Database file. A relative path resolves inside agent.data_dir;
  # it may not escape that directory.
  path: {{ .StoragePath }}

  # How long a query waits for a database lock before failing.
  busy_timeout: {{ .BusyTimeout }}
`

// Sample renders an annotated configuration file showing the defaults that
// apply for the given data directory.
func Sample(dataDir string) (string, error) {
	const op = "config.Sample"

	def := Default()
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "/var/lib/infrapilot"
	}

	tmpl, err := template.New("config").Parse(sampleTemplate)
	if err != nil {
		return "", errors.Wrap(errors.KindInternal, op, "cannot parse the sample configuration template", err)
	}

	data := struct {
		Version           int
		DataDir           string
		ShutdownTimeout   string
		HeartbeatInterval string
		LogLevel          string
		LogFormat         string
		StoragePath       string
		BusyTimeout       string
	}{
		Version:           SchemaVersion,
		DataDir:           dataDir,
		ShutdownTimeout:   def.Agent.ShutdownTimeout.String(),
		HeartbeatInterval: def.Agent.HeartbeatInterval.String(),
		LogLevel:          def.Logging.Level,
		LogFormat:         def.Logging.Format,
		StoragePath:       def.Storage.Path,
		BusyTimeout:       def.Storage.BusyTimeout.String(),
	}

	var out strings.Builder
	if err := tmpl.Execute(&out, data); err != nil {
		return "", errors.Wrap(errors.KindInternal, op, "cannot render the sample configuration", err)
	}

	return out.String(), nil
}
