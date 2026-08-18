package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

const purgePhrase = "REMOVE ALL INFRAPILOT DATA"

var uninstallRun = func(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

func runUninstall(ctx context.Context, env Env, args []string, out IO) error {
	purge := false
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			fmt.Fprintln(out.Out, "Usage: infrapilot uninstall [--purge]\n\nRemoves InfraPilot services and binaries while preserving data by default.\n--purge additionally removes configuration, identity, database, and logs after exact confirmation.")
			return nil
		}
		if arg == "--purge" {
			purge = true
			continue
		}
		return errors.Newf(errors.KindUsage, "cli.uninstall", "unknown argument %q", arg)
	}
	if out.In == nil {
		return errors.New(errors.KindPermission, "cli.uninstall", "confirmation input is unavailable; run interactively")
	}
	fmt.Fprintln(out.Out, "WARNING: InfraPilot uninstall will remove installed services and binaries.")
	if purge {
		fmt.Fprintln(out.Out, "Purge mode will also remove configuration, identities, databases, and logs.")
	}
	fmt.Fprint(out.Out, "Continue? Type YES to confirm: ")
	line, _ := bufio.NewReader(out.In).ReadString('\n')
	if strings.TrimSpace(line) != "YES" {
		return errors.New(errors.KindPermission, "cli.uninstall", "uninstall cancelled; type YES to confirm")
	}
	if purge {
		fmt.Fprintf(out.Out, "Type %q to remove all InfraPilot data: ", purgePhrase)
		line, _ = bufio.NewReader(out.In).ReadString('\n')
		if strings.TrimSpace(line) != purgePhrase {
			return errors.New(errors.KindPermission, "cli.uninstall", "purge cancelled; exact confirmation phrase did not match")
		}
	}
	for _, unit := range []string{"infrapilot-web.service", "infrapilot-agent.service"} {
		_ = uninstallRun(ctx, "systemctl", "stop", unit)
		_ = uninstallRun(ctx, "systemctl", "disable", unit)
	}
	for _, unit := range []string{"infrapilot-web.service", "infrapilot-agent.service"} {
		_ = os.Remove(filepath.Join("/etc/systemd/system", unit))
	}
	_ = uninstallRun(ctx, "systemctl", "daemon-reload")
	for _, binary := range []string{"infrapilot", "infrapilot-agent", "infrapilot-web"} {
		_ = os.Remove(filepath.Join("/usr/local/bin", binary))
	}
	if purge {
		if err := removeContents(env.Paths.DataDir); err != nil {
			return err
		}
		if err := removeContents(filepath.Dir(env.Paths.ConfigFile)); err != nil {
			return err
		}
	}
	if purge {
		fmt.Fprintln(out.Out, "InfraPilot uninstalled and all data removed.")
	} else {
		fmt.Fprintln(out.Out, "InfraPilot uninstalled. Configuration, identities, databases, logs, and user data were preserved.")
	}
	return nil
}

func removeContents(dir string) error {
	if dir == "" || dir == string(filepath.Separator) || dir == "." {
		return errors.New(errors.KindValidation, "cli.uninstall", "refusing to remove an unsafe data path")
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
