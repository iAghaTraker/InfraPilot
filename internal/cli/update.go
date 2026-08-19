package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/update"
	"github.com/iAghaTraker/InfraPilot/pkg/version"
)

var updateClient = update.Client{}

func runUpdate(ctx context.Context, env Env, args []string, out IO) error {
	if len(args) > 0 && args[0] == "--help" {
		fmt.Fprintln(out.Out, "Usage: infrapilot update [status|--check|--yes]")
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
	if len(args) > 0 && (args[0] == "check" || args[0] == "--check") {
		fmt.Fprintf(out.Out, "Current version: %s\nLatest version: %s\nUpdate available: %t\nChangelog:\n%s\n", version.Version, release.TagName, update.Compare(version.Version, release.TagName) < 0, release.Body)
		return nil
	}
	if update.Compare(version.Version, release.TagName) >= 0 {
		fmt.Fprintln(out.Out, "InfraPilot is up to date.")
		return nil
	}
	yes := len(args) > 0 && args[0] == "--yes"
	fmt.Fprintf(out.Out, "Update available: %s\n%s\n", release.TagName, release.Body)
	if !yes {
		fmt.Fprint(out.Out, "Type YES to install: ")
	}
	if !yes && out.In == nil {
		return errors.New(errors.KindPermission, "cli.update", "confirmation input is unavailable")
	}
	line := "YES"
	if !yes {
		line, _ = bufio.NewReader(out.In).ReadString('\n')
	}
	if strings.TrimSpace(line) != "YES" {
		return errors.New(errors.KindPermission, "cli.update", "update cancelled")
	}
	if len(args) > 0 && args[0] != "--yes" {
		return errors.New(errors.KindUsage, "cli.update", "usage: infrapilot update [status|--check|--yes]")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	metadata, err := update.Detect(update.MetadataPath, executable)
	if err != nil {
		return errors.Wrap(errors.KindUnsupported, "cli.update", "cannot update this installation", err)
	}
	if err := updateClient.InstallAll(ctx, release, metadata, nil); err != nil {
		return errors.Wrap(errors.KindInternal, "cli.update", "installation failed", err)
	}
	fmt.Fprintf(out.Out, "InfraPilot updated to %s.\n", release.TagName)
	return nil
}
