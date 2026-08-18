package web

import (
	"context"
	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"os/exec"
	"strings"
)

type Runner interface {
	Run(context.Context, string, ...string) (string, error)
}
type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, n string, a ...string) (string, error) {
	b, e := exec.CommandContext(ctx, n, a...).CombinedOutput()
	return string(b), e
}

type Manager struct {
	runner Runner
	unit   string
}

func NewManager(r Runner) *Manager {
	if r == nil {
		r = commandRunner{}
	}
	return &Manager{runner: r, unit: "infrapilot-web.service"}
}
func (m *Manager) Start(ctx context.Context) error   { return m.action(ctx, "start") }
func (m *Manager) Stop(ctx context.Context) error    { return m.action(ctx, "stop") }
func (m *Manager) Restart(ctx context.Context) error { return m.action(ctx, "restart") }
func (m *Manager) Enable(ctx context.Context) error  { return m.action(ctx, "enable") }
func (m *Manager) Disable(ctx context.Context) error { return m.action(ctx, "disable") }
func (m *Manager) Status(ctx context.Context) (string, error) {
	return m.runner.Run(ctx, "systemctl", "is-active", m.unit)
}
func (m *Manager) Logs(ctx context.Context) (string, error) {
	return m.runner.Run(ctx, "journalctl", "-u", m.unit, "-n", "100", "--no-pager")
}
func (m *Manager) action(ctx context.Context, a string) error {
	_, err := m.runner.Run(ctx, "systemctl", a, m.unit)
	if err != nil {
		return errors.Newf(errors.KindPermission, "web.Manager", "failed to %s web service", a)
	}
	return nil
}

var _ = strings.TrimSpace
