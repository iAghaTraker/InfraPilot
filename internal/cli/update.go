package cli

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/update"
	"github.com/iAghaTraker/InfraPilot/pkg/version"
)

var updateClient = update.Client{}

func runUpdate(ctx context.Context, env Env, args []string, out IO) error {
	if len(args) > 0 && args[0] == "--help" {
		fmt.Fprintln(out.Out, "Usage: infrapilot update [check|status|--version VERSION]")
		return nil
	}
	if !env.Config.Update.Enabled {
		return errors.New(errors.KindPermission, "cli.update", "updates are disabled; enable update.enabled in configuration")
	}
	release, err := updateClient.Latest(ctx)
	if err != nil {
		return errors.Wrap(errors.KindInternal, "cli.update", "unable to check releases", err)
	}
	if len(args) > 0 && args[0] == "status" {
		fmt.Fprintf(out.Out, "Current version: %s\nLatest version: %s\nUpdate available: %t\n", version.Version, release.TagName, update.Compare(version.Version, release.TagName) < 0)
		return nil
	}
	if len(args) > 0 && args[0] == "check" {
		fmt.Fprintf(out.Out, "Current version: %s\nLatest version: %s\nUpdate available: %t\nChangelog:\n%s\n", version.Version, release.TagName, update.Compare(version.Version, release.TagName) < 0, release.Body)
		return nil
	}
	if update.Compare(version.Version, release.TagName) >= 0 {
		fmt.Fprintln(out.Out, "InfraPilot is up to date.")
		return nil
	}
	fmt.Fprintf(out.Out, "Update available: %s\n%s\nType YES to install: ", release.TagName, release.Body)
	if out.In == nil {
		return errors.New(errors.KindPermission, "cli.update", "confirmation input is unavailable")
	}
	line, _ := bufio.NewReader(out.In).ReadString('\n')
	if strings.TrimSpace(line) != "YES" {
		return errors.New(errors.KindPermission, "cli.update", "update cancelled")
	}
	return errors.New(errors.KindUnsupported, "cli.update", "binary installation requires the installed package manager path")
}
