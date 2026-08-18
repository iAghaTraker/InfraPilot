package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/iAghaTraker/InfraPilot/internal/core"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/service"
)

func runService(ctx context.Context, args []string, out IO) error {
	if len(args) == 0 {
		return errors.New(errors.KindUsage, "cli.service", "service subcommand is required")
	}
	m := newServiceManager()
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return errors.New(errors.KindUsage, "cli.service", "service list takes no arguments")
		}
		items, err := m.List(ctx)
		if err != nil {
			return err
		}
		writeServices(out.Out, items)
	case "status":
		if len(args) != 2 {
			return errors.New(errors.KindUsage, "cli.service", "usage: infrapilot service status <service>")
		}
		item, err := m.Status(ctx, args[1])
		if err != nil {
			return err
		}
		writeService(out.Out, item)
	case "start", "stop", "restart", "enable", "disable":
		if len(args) != 2 {
			return errors.New(errors.KindUsage, "cli.service", "usage: infrapilot service "+args[0]+" <service>")
		}
		var err error
		switch args[0] {
		case "start":
			err = m.Start(ctx, args[1])
		case "stop":
			err = m.Stop(ctx, args[1])
		case "restart":
			err = m.Restart(ctx, args[1])
		case "enable":
			err = m.Enable(ctx, args[1])
		case "disable":
			err = m.Disable(ctx, args[1])
		}
		if err != nil {
			return err
		}
		past := map[string]string{"start": "started", "stop": "stopped", "restart": "restarted", "enable": "enabled", "disable": "disabled"}
		fmt.Fprintf(out.Out, "Service %s %s\n", args[1], past[args[0]])
	case "logs":
		if len(args) < 2 || len(args) > 4 {
			return errors.New(errors.KindUsage, "cli.service", "usage: infrapilot service logs <service> [--lines N]")
		}
		lines := 50
		if len(args) == 4 && args[2] == "--lines" {
			var err error
			lines, err = strconv.Atoi(args[3])
			if err != nil {
				return errors.New(errors.KindValidation, "cli.service", "lines must be a number")
			}
		} else if len(args) != 2 {
			return errors.New(errors.KindUsage, "cli.service", "usage: infrapilot service logs <service> [--lines N]")
		}
		logs, err := m.Logs(ctx, args[1], lines)
		if err != nil {
			return err
		}
		io.WriteString(out.Out, logs)
		if !strings.HasSuffix(logs, "\n") {
			io.WriteString(out.Out, "\n")
		}
	default:
		return errors.Newf(errors.KindUsage, "cli.service", "unknown service subcommand %q", args[0])
	}
	return nil
}

var newServiceManager = core.ServiceManager

func writeServices(w io.Writer, items []service.Service) {
	fmt.Fprintln(w, "InfraPilot Services\n\nNAME\tSTATUS\tENABLED")
	for _, s := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, s.State, s.Enablement)
	}
}
func writeService(w io.Writer, s service.Service) {
	fmt.Fprintf(w, "Service: %s\n\nStatus:\t%s\nEnabled:\t%s\n", s.Name, s.State, s.Enablement)
	if s.PID > 0 {
		fmt.Fprintf(w, "PID:\t%d\n", s.PID)
	}
	if s.StartedAt != "" {
		fmt.Fprintf(w, "Started:\t%s\n", s.StartedAt)
	}
	if s.MemoryBytes > 0 {
		fmt.Fprintf(w, "Memory:\t%s\n", formatBytes(int64(s.MemoryBytes)))
	}
}
