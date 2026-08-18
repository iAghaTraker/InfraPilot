package cli

import (
	"context"
	"fmt"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/web"
	"io"
	"strings"
)

var newWebManager = func() *web.Manager { return web.NewManager(nil) }

func runWeb(ctx context.Context, args []string, out IO) error {
	if len(args) == 0 {
		return errors.New(errors.KindUsage, "cli.web", "web subcommand is required")
	}
	m := newWebManager()
	switch args[0] {
	case "start":
		if len(args) > 2 {
			return errors.New(errors.KindUsage, "cli.web", "usage: infrapilot web start [background]")
		}
		if len(args) == 2 && args[1] != "background" {
			return errors.Newf(errors.KindUsage, "cli.web", "unknown web start mode %q", args[1])
		}
		if err := m.Start(ctx); err != nil {
			return err
		}
		if len(args) == 2 && args[1] == "background" {
			fmt.Fprintln(out.Out, "InfraPilot Web started in background\nService: running")
		} else {
			fmt.Fprintf(out.Out, "InfraPilot Web started\nAddress: http://%s\n", web.DefaultAddress)
		}
	case "stop":
		if len(args) != 1 {
			return errors.New(errors.KindUsage, "cli.web", "web stop takes no arguments")
		}
		if err := m.Stop(ctx); err != nil {
			return err
		}
		fmt.Fprintln(out.Out, "InfraPilot Web stopped")
	case "restart":
		if len(args) != 1 {
			return errors.New(errors.KindUsage, "cli.web", "web restart takes no arguments")
		}
		if err := m.Restart(ctx); err != nil {
			return err
		}
		fmt.Fprintln(out.Out, "InfraPilot Web restarted")
	case "enable", "disable":
		if len(args) != 1 {
			return errors.New(errors.KindUsage, "cli.web", "web "+args[0]+" takes no arguments")
		}
		var err error
		if args[0] == "enable" {
			err = m.Enable(ctx)
		} else {
			err = m.Disable(ctx)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(out.Out, "InfraPilot Web %sed\n", args[0])
	case "status":
		if len(args) != 1 {
			return errors.New(errors.KindUsage, "cli.web", "web status takes no arguments")
		}
		v, err := m.Status(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(out.Out, "InfraPilot Web\n\nService: %s\n", strings.TrimSpace(v))
	case "logs":
		if len(args) != 1 {
			return errors.New(errors.KindUsage, "cli.web", "web logs takes no arguments")
		}
		v, err := m.Logs(ctx)
		if err != nil {
			return err
		}
		io.WriteString(out.Out, v)
	default:
		return errors.Newf(errors.KindUsage, "cli.web", "unknown web subcommand %q", args[0])
	}
	return nil
}
