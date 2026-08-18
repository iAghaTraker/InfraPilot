package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/iAghaTraker/InfraPilot/internal/core"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

// runDoctor renders a diagnostic report and fails when a check failed.
//
// The report goes to stdout for a human; the returned error is what gives the
// process a non-zero exit code, so a script can act on the verdict without
// parsing the text. Its kind is chosen so that errors.ExitCode agrees with
// core.Report.ExitCode: warnings never fail a run, failures always do.
func runDoctor(ctx context.Context, env Env, out IO) error {
	report := core.Diagnose(ctx, core.Env{
		Config:    env.Config,
		Paths:     env.Paths,
		ConfigErr: env.ConfigErr,
	})

	writeReport(out.Out, report)

	if !report.OK() {
		return errors.New(errors.KindValidation, "cli.doctor",
			"one or more checks failed; see the report above")
	}
	return nil
}

// writeReport renders a Report as one line per check plus a summary.
func writeReport(w io.Writer, report core.Report) {
	fmt.Fprintf(w, "InfraPilot Doctor\n\n")

	for _, result := range report.Results {
		fmt.Fprintf(w, "%-4s  %s\n", result.Status.String(), result.Name)
		if result.Message != "" {
			// Indented under its check so the status column stays scannable.
			fmt.Fprintf(w, "      %s\n", result.Message)
		}
	}

	fmt.Fprintf(w, "\n%d passed, %d warnings, %d failed\n",
		report.Count(core.StatusPass),
		report.Count(core.StatusWarn),
		report.Count(core.StatusFail),
	)

	switch {
	case report.Count(core.StatusFail) > 0:
		fmt.Fprintf(w, "\nFix the failures above, then run infrapilot doctor again.\n")
	case report.Count(core.StatusWarn) > 0:
		fmt.Fprintf(w, "\nNo failures. Warnings are safe to ignore if they are expected.\n")
	}
}
