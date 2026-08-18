package cli

import (
	"context"
	"fmt"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/system"
)

func runSystem(ctx context.Context, args []string, out IO) error {
	_ = ctx
	if len(args) != 1 || args[0] != "info" {
		return errors.New(errors.KindUsage, "cli.system", "usage: infrapilot system info")
	}
	i := system.Collect()
	fmt.Fprintf(out.Out, "InfraPilot System Information\n\nOperating System: %s\nDistribution: %s\nKernel: %s\nArchitecture: %s\nCPU: %s (%d)\nMemory: %s available of %s\nDisk: %s available of %s\nUptime: %s\n", i.OS, i.Distribution, i.Kernel, i.Arch, i.CPUModel, i.NumCPU, formatBytes(int64(i.MemoryAvailable)), formatBytes(int64(i.MemoryTotal)), formatBytes(int64(i.DiskAvailable)), formatBytes(int64(i.DiskTotal)), system.FormatUptime(i.Uptime))
	return nil
}
