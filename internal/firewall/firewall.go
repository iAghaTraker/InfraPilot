package firewall

import (
	"context"
	"os/exec"
	"strings"
)

type Info struct {
	Detected string
	Port     int
	Status   string
}
type Runner func(context.Context, string, ...string) ([]byte, error)

func Detect(ctx context.Context, port int) Info {
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
	return DetectWith(ctx, port, run)
}
func DetectWith(ctx context.Context, port int, run Runner) Info {
	if _, err := exec.LookPath("ufw"); err == nil {
		b, _ := run(ctx, "ufw", "status")
		return Info{"ufw", port, status(string(b))}
	}
	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		b, _ := run(ctx, "firewall-cmd", "--state")
		return Info{"firewalld", port, status(string(b))}
	}
	return Info{Port: port, Status: "unknown"}
}
func status(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if strings.Contains(v, "active") || strings.Contains(v, "running") {
		return "active"
	}
	if strings.Contains(v, "inactive") || strings.Contains(v, "not running") {
		return "blocked"
	}
	return "unknown"
}
