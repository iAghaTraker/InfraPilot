// Command infrapilot is the InfraPilot command-line client.
//
// It reports on and diagnoses a local installation. The Agent owns
// infrastructure state; this binary only asks and renders, which is why it
// needs no privileges beyond read access to the data directory.
//
// This binary is deliberately thin: everything worth testing lives in
// internal/cli.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/iAghaTraker/InfraPilot/internal/cli"
)

func main() {
	// A long-running probe, such as a status check against a locked database,
	// must be interruptible with Ctrl-C rather than needing a kill.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	code := cli.Execute(ctx, os.Args[1:], cli.IO{Out: os.Stdout, Err: os.Stderr, In: os.Stdin})

	// stop is released before exiting so signal handling is not left installed
	// on a process that is on its way out.
	stop()
	os.Exit(code)
}
