// Command infrapilot-agent runs the InfraPilot Agent.
//
// The Agent owns all infrastructure state. It is intended to run under systemd
// as an unprivileged service account; see installer/ for the unit file.
//
// This binary is deliberately thin: it wires configuration, logging and the
// agent lifecycle together and translates a failure into an exit code. Anything
// that could be tested belongs in a package, not here.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/iAghaTraker/InfraPilot/internal/agent"
	"github.com/iAghaTraker/InfraPilot/internal/config"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/logging"
	"github.com/iAghaTraker/InfraPilot/internal/system"
	"github.com/iAghaTraker/InfraPilot/pkg/version"
)

func main() {
	os.Exit(run())
}

// run performs start-up and returns a process exit code.
//
// It exists so that every failure path funnels through one place that reports
// the error and picks the code, rather than calling os.Exit from wherever a
// problem is noticed.
func run() int {
	paths, err := system.ResolvePaths()
	if err != nil {
		return fail(err)
	}

	cfg, err := config.Load(paths)
	if err != nil {
		return fail(err)
	}

	logger, err := logging.New(logging.Options{
		Level:  cfg.Logging.Level,
		Format: logging.Format(cfg.Logging.Format),
		Output: os.Stderr,
	})
	if err != nil {
		return fail(err)
	}

	// Once logging exists, failures are reported through it rather than to bare
	// stderr, so systemd's journal has the structured record.
	instance := agent.New(agent.Options{
		Config: cfg,
		Paths:  paths,
		Logger: logger,
	})

	if err := instance.Run(context.Background()); err != nil {
		logger.Error("the agent could not run", logging.ErrorAttrs(err)...)
		return errors.ExitCode(err)
	}

	return errors.ExitOK
}

// fail reports an error that occurred before logging was available.
//
// Start-up problems must be visible even when the log destination is the thing
// that is broken, so this writes to stderr directly. systemd captures it.
func fail(err error) int {
	fmt.Fprintf(os.Stderr, "%s %s: %v\n", version.Name, "agent", err)
	return errors.ExitCode(err)
}
