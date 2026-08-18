package config

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/logging"
)

// Bounds applied during validation.
//
// These are sanity limits, not policy. They exist to catch values that are
// almost certainly mistakes, such as a one-millisecond shutdown timeout that
// would make every restart look like a crash.
const (
	minShutdownTimeout = time.Second
	maxShutdownTimeout = 10 * time.Minute

	minHeartbeatInterval = time.Second
	maxHeartbeatInterval = 24 * time.Hour

	minBusyTimeout = 100 * time.Millisecond
	maxBusyTimeout = time.Minute
)

// Validate checks that the configuration is usable.
//
// It reports every problem it finds rather than only the first, so an operator
// fixing a configuration file does not have to rerun once per mistake.
func (c Config) Validate() error {
	const op = "config.Validate"

	var problems []string

	// Logging. The level and format are parsed with the same functions the
	// logger uses, so validation cannot drift from actual behaviour.
	if _, err := logging.ParseLevel(c.Logging.Level); err != nil {
		problems = append(problems, "logging.level: "+err.Error())
	}
	if _, err := logging.ParseFormat(c.Logging.Format); err != nil {
		problems = append(problems, "logging.format: "+err.Error())
	}

	// Agent.
	if strings.TrimSpace(c.Agent.DataDir) == "" {
		problems = append(problems, "agent.data_dir: must not be empty")
	} else if !filepath.IsAbs(c.Agent.DataDir) {
		problems = append(problems,
			"agent.data_dir: must be an absolute path, got "+c.Agent.DataDir)
	}

	problems = appendRangeProblem(problems, "agent.shutdown_timeout",
		c.Agent.ShutdownTimeout, minShutdownTimeout, maxShutdownTimeout)
	problems = appendRangeProblem(problems, "agent.heartbeat_interval",
		c.Agent.HeartbeatInterval, minHeartbeatInterval, maxHeartbeatInterval)
	problems = appendRangeProblem(problems, "storage.busy_timeout",
		c.Storage.BusyTimeout, minBusyTimeout, maxBusyTimeout)

	// Storage. A relative path is resolved inside the data directory, and
	// DatabasePath rejects anything that escapes it.
	if strings.TrimSpace(c.Storage.Path) == "" {
		problems = append(problems, "storage.path: must not be empty")
	} else if _, err := c.DatabasePath(); err != nil {
		problems = append(problems, "storage.path: "+err.Error())
	}

	if len(problems) == 0 {
		return nil
	}

	return errors.New(errors.KindConfig, op, strings.Join(problems, "; "))
}

// appendRangeProblem records a problem when d falls outside [min, max].
func appendRangeProblem(problems []string, field string, d, minimum, maximum time.Duration) []string {
	switch {
	case d <= 0:
		return append(problems, field+": must be greater than zero, got "+d.String())
	case d < minimum:
		return append(problems, field+": must be at least "+minimum.String()+", got "+d.String())
	case d > maximum:
		return append(problems, field+": must be at most "+maximum.String()+", got "+d.String())
	default:
		return problems
	}
}
