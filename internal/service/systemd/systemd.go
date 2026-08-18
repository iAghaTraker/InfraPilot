// Package systemd adapts Linux systemd commands to the service backend.
package systemd

import (
	"context"
	stderrors "errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	iperrors "github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/service"
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, command string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, command, args...).CombinedOutput()
}

type Adapter struct{ runner Runner }

func New() *Adapter                   { return &Adapter{runner: execRunner{}} }
func NewWithRunner(r Runner) *Adapter { return &Adapter{runner: r} }

func (a *Adapter) List(ctx context.Context) ([]service.Service, error) {
	units, err := a.run(ctx, "systemctl", "list-units", "--type=service", "--all", "--no-legend", "--plain", "--no-pager")
	if err != nil {
		return nil, err
	}
	files, err := a.run(ctx, "systemctl", "list-unit-files", "--type=service", "--no-legend", "--no-pager")
	if err != nil {
		return nil, err
	}

	enabled := make(map[string]service.Enablement)
	for _, line := range strings.Split(files, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			enabled[fields[0]] = enablement(fields[1])
		}
	}
	var result []service.Service
	for _, line := range strings.Split(units, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.HasSuffix(fields[0], ".service") {
			continue
		}
		result = append(result, service.Service{
			Name: strings.TrimSuffix(fields[0], ".service"), State: state(fields[2]),
			Enablement: enabled[fields[0]], Description: strings.Join(fields[4:], " "),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (a *Adapter) Status(ctx context.Context, name string) (service.Service, error) {
	out, err := a.run(ctx, "systemctl", "show", name, "--no-pager",
		"--property=Id,Description,LoadState,ActiveState,UnitFileState,MainPID,ActiveEnterTimestamp,MemoryCurrent")
	if err != nil {
		return service.Service{}, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			values[key] = value
		}
	}
	if values["LoadState"] == "not-found" || values["Id"] == "" {
		return service.Service{}, iperrors.Newf(iperrors.KindNotFound, "systemd.Status", "service %q was not found", strings.TrimSuffix(name, ".service"))
	}
	pid, _ := strconv.Atoi(values["MainPID"])
	memory, _ := strconv.ParseUint(values["MemoryCurrent"], 10, 64)
	return service.Service{Name: strings.TrimSuffix(values["Id"], ".service"), Description: values["Description"], State: state(values["ActiveState"]), Enablement: enablement(values["UnitFileState"]), PID: pid, StartedAt: values["ActiveEnterTimestamp"], MemoryBytes: memory}, nil
}

func (a *Adapter) Start(ctx context.Context, name string) error {
	_, err := a.run(ctx, "systemctl", "start", "--", name)
	return err
}
func (a *Adapter) Stop(ctx context.Context, name string) error {
	_, err := a.run(ctx, "systemctl", "stop", "--", name)
	return err
}
func (a *Adapter) Restart(ctx context.Context, name string) error {
	_, err := a.run(ctx, "systemctl", "restart", "--", name)
	return err
}
func (a *Adapter) Enable(ctx context.Context, name string) error {
	_, err := a.run(ctx, "systemctl", "enable", "--", name)
	return err
}
func (a *Adapter) Disable(ctx context.Context, name string) error {
	_, err := a.run(ctx, "systemctl", "disable", "--", name)
	return err
}
func (a *Adapter) Logs(ctx context.Context, name string, lines int) (string, error) {
	return a.run(ctx, "journalctl", "--unit", name, "--lines", strconv.Itoa(lines), "--no-pager")
}

func (a *Adapter) run(ctx context.Context, command string, args ...string) (string, error) {
	out, err := a.runner.Run(ctx, command, args...)
	if err == nil {
		return string(out), nil
	}
	msg := strings.TrimSpace(string(out))
	kind := iperrors.KindUnknown
	lower := strings.ToLower(msg)
	switch {
	case stderrors.Is(err, exec.ErrNotFound), strings.Contains(lower, "system has not been booted with systemd"), strings.Contains(lower, "failed to connect to bus"):
		kind = iperrors.KindUnsupported
		msg = "systemd is unavailable"
	case strings.Contains(lower, "access denied"), strings.Contains(lower, "permission denied"), strings.Contains(lower, "authentication is required"), strings.Contains(lower, "interactive authentication required"):
		kind = iperrors.KindPermission
		msg = "permission denied; run this operation with appropriate privileges"
	case strings.Contains(lower, "not found"), strings.Contains(lower, "could not be found"), strings.Contains(lower, "not loaded"):
		kind = iperrors.KindNotFound
		msg = "service was not found"
	}
	if msg == "" {
		msg = fmt.Sprintf("%s command failed", command)
	}
	return "", iperrors.New(kind, "systemd.run", msg)
}

func state(value string) service.State {
	switch value {
	case "active", "reloading":
		return service.StateRunning
	case "inactive", "deactivating":
		return service.StateStopped
	case "failed":
		return service.StateFailed
	default:
		return service.StateUnknown
	}
}
func enablement(value string) service.Enablement {
	switch value {
	case "enabled", "enabled-runtime", "linked", "linked-runtime", "alias":
		return service.Enabled
	case "disabled", "masked", "masked-runtime":
		return service.Disabled
	case "static", "indirect", "generated", "transient":
		return service.Static
	default:
		return service.Unknown
	}
}
