// Package cli implements the infrapilot command-line client.
//
// The CLI is a client, not a component that owns state. It parses arguments,
// asks internal/core for an answer, renders it, and picks an exit code. It
// never inspects the filesystem, the database or the process table itself;
// that is core's job, and keeping the boundary strict is what lets the web and
// desktop clients of later versions reuse the same answers.
//
// Rendering therefore lives here, and only here. core returns values.
package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/iAghaTraker/InfraPilot/internal/config"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/system"
	"github.com/iAghaTraker/InfraPilot/pkg/version"
)

// IO carries the streams a command writes to.
//
// Passing them in rather than reaching for os.Stdout is what makes every
// command's output assertable in a test.
type IO struct {
	Out io.Writer
	Err io.Writer
}

// command is one subcommand.
type command struct {
	Name string

	// Summary is the one-line description shown by help.
	Summary string

	// Run executes the command. Returning an error is how a command fails:
	// Execute turns it into a message and an exit code, so no command calls
	// os.Exit or prints its own failure.
	Run func(ctx context.Context, env Env, args []string, out IO) error
}

// Env is what a command needs to know about the installation.
//
// Config and ConfigErr are resolved once by Execute so that every command sees
// the same view, and so a command that must work despite broken configuration
// (status, doctor) can see the failure rather than being denied a chance to
// run.
type Env struct {
	Config    config.Config
	Paths     system.Paths
	ConfigErr error
}

// commands returns the registry, in the order help lists them.
//
// A new subcommand is added here and nowhere else.
func commands() []command {
	return []command{
		{
			Name:    "version",
			Summary: "Print version and platform information",
			Run:     noArgs(runVersion),
		},
		{
			Name:    "status",
			Summary: "Show the state of this installation",
			Run:     noArgs(runStatus),
		},
		{
			Name:    "doctor",
			Summary: "Check the installation and report problems",
			Run:     noArgs(runDoctor),
		},
		{Name: "service", Summary: "Inspect and manage system services", Run: func(ctx context.Context, _ Env, args []string, out IO) error { return runService(ctx, args, out) }},
		{Name: "system", Summary: "Show system information", Run: func(ctx context.Context, _ Env, args []string, out IO) error { return runSystem(ctx, args, out) }},
		{Name: "sk", Summary: "Create and manage secure device identities", Run: func(ctx context.Context, env Env, args []string, out IO) error { return runSK(ctx, env, args, out) }},
		{Name: "web", Summary: "Manage the local Web Panel service", Run: func(ctx context.Context, _ Env, args []string, out IO) error { return runWeb(ctx, args, out) }},
	}
}

// Execute runs the CLI and returns a process exit code.
//
// args excludes the program name. Configuration is loaded once, and a failure
// to load it is carried in Env rather than being fatal: status and doctor exist
// to explain exactly that situation, so refusing to run them when configuration
// is broken would remove the tools an operator needs most.
func Execute(ctx context.Context, args []string, out IO) int {
	name, rest, err := parse(args)
	if err != nil {
		fmt.Fprintf(out.Err, "%s\n\n", err)
		writeUsage(out.Err)
		return errors.ExitCode(err)
	}
	if name == "" {
		writeUsage(out.Out)
		return errors.ExitOK
	}

	cmd, err := lookup(name)
	if err != nil {
		fmt.Fprintf(out.Err, "%s\n\n", err)
		writeUsage(out.Err)
		return errors.ExitCode(err)
	}

	env, err := resolve()
	if err != nil {
		// Only an unresolvable path set reaches here, which no command can work
		// around: without paths there is nothing to report on.
		fmt.Fprintf(out.Err, "%s\n", err)
		return errors.ExitCode(err)
	}

	if err := cmd.Run(ctx, env, rest, out); err != nil {
		fmt.Fprintf(out.Err, "%s\n", err)
		if errors.IsKind(err, errors.KindUsage) {
			writeUsage(out.Err)
		}
		return errors.ExitCode(err)
	}
	return errors.ExitOK
}

func noArgs(run func(context.Context, Env, IO) error) func(context.Context, Env, []string, IO) error {
	return func(ctx context.Context, env Env, args []string, out IO) error {
		if len(args) > 0 {
			return errors.Newf(errors.KindUsage, "cli.Execute", "command takes no arguments, got %q", strings.Join(args, " "))
		}
		return run(ctx, env, out)
	}
}

// parse splits args into a subcommand name and its arguments.
//
// Help is handled as a name rather than a flag so that "infrapilot help" and
// "infrapilot --help" behave identically.
func parse(args []string) (name string, rest []string, err error) {
	const op = "cli.parse"

	for i, arg := range args {
		switch {
		case arg == "-h" || arg == "--help" || arg == "help":
			return "", nil, nil

		case arg == "-v" || arg == "--version":
			return "version", args[i+1:], nil

		case strings.HasPrefix(arg, "-"):
			return "", nil, errors.Newf(errors.KindUsage, op, "unknown flag %q", arg)

		default:
			return arg, args[i+1:], nil
		}
	}

	return "", nil, nil
}

// lookup finds a command by name.
func lookup(name string) (command, error) {
	for _, cmd := range commands() {
		if cmd.Name == name {
			return cmd, nil
		}
	}
	return command{}, errors.Newf(errors.KindUsage, "cli.lookup",
		"unknown command %q", name)
}

// resolve discovers paths and loads configuration.
//
// A configuration failure is returned inside Env, not as an error, because it
// is a condition to report rather than a reason to stop.
func resolve() (Env, error) {
	paths, err := system.ResolvePaths()
	if err != nil {
		return Env{}, err
	}

	cfg, cfgErr := config.Load(paths)
	return Env{Config: cfg, Paths: paths, ConfigErr: cfgErr}, nil
}

// writeUsage prints the help text.
func writeUsage(w io.Writer) {
	fmt.Fprintf(w, "%s %s — self-hosted infrastructure management\n\n", version.Name, version.Version)
	fmt.Fprintf(w, "Usage:\n  infrapilot <command> [arguments]\n\nCommands:\n")

	for _, cmd := range commands() {
		fmt.Fprintf(w, "  %-9s %s\n", cmd.Name, cmd.Summary)
	}

	fmt.Fprintf(w, "  %-9s %s\n", "help", "Show this help text")
	fmt.Fprintf(w, "\nThe agent runs as a service; see infrapilot-agent and installer/.\n")
}
