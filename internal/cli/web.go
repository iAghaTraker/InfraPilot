package cli

import (
	"context"
	"fmt"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/firewall"
	"github.com/iAghaTraker/InfraPilot/internal/web"
	"io"
	"strings"
)

var newWebManager = func() *web.Manager { return web.NewManager(nil) }

func runWeb(ctx context.Context, env Env, args []string, out IO) error {
	if len(args) == 0 {
		return errors.New(errors.KindUsage, "cli.web", "web subcommand is required")
	}
	m := newWebManager()
	switch args[0] {
	case "setup":
		if len(args) != 1 {
			return errors.New(errors.KindUsage, "cli.web", "web setup takes no arguments")
		}
		return runWebSetup(ctx, env, out)
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
		fw := firewall.Detect(ctx, 8090)
		bind := "127.0.0.1"
		if env.Config.Web.BindAddress != "" {
			bind = env.Config.Web.BindAddress
		}
		paired := "unknown"
		if db, dbErr := openIdentityDB(ctx, env); dbErr == nil {
			defer db.Close()
			var n int
			_ = db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM device_identities WHERE status='active'").Scan(&n)
			paired = fmt.Sprint(n)
		}
		fmt.Fprintf(out.Out, "InfraPilot Web\n\nService: %s\nLocal: http://127.0.0.1:8090\nPublic: http://%s:8090\nBinding: %s\nAuthentication: Enabled\nPaired Devices: %s\nSession Timeout: 15 minutes\nFirewall: %s (%s)\nWeb Port: %d\n", strings.TrimSpace(v), publicIP(ctx), bind, paired, fw.Detected, fw.Status, fw.Port)
	case "url":
		if len(args) != 1 {
			return errors.New(errors.KindUsage, "cli.web", "web url takes no arguments")
		}
		fmt.Fprintf(out.Out, "InfraPilot Web Panel\n\nOpen:\nhttp://%s\n\nThe URL does not replace device authentication.\n", web.DefaultAddress)
	case "tls":
		if len(args) != 2 || args[1] != "status" {
			return errors.New(errors.KindUsage, "cli.web", "usage: infrapilot web tls status")
		}
		fmt.Fprintln(out.Out, "InfraPilot Web TLS\n\nStatus: Not configured\nHTTPS enablement is reserved for a future release.")
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
