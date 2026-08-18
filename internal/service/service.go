// Package service provides the reusable service-management domain layer.
package service

import (
	"context"
	"regexp"
	"strings"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

type State string

const (
	StateRunning State = "running"
	StateStopped State = "stopped"
	StateFailed  State = "failed"
	StateUnknown State = "unknown"
)

type Enablement string

const (
	Enabled  Enablement = "enabled"
	Disabled Enablement = "disabled"
	Static   Enablement = "static"
	Unknown  Enablement = "unknown"
)

type Service struct {
	Name        string
	Description string
	State       State
	Enablement  Enablement
	PID         int
	StartedAt   string
	MemoryBytes uint64
}

type Backend interface {
	List(context.Context) ([]Service, error)
	Status(context.Context, string) (Service, error)
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Restart(context.Context, string) error
	Enable(context.Context, string) error
	Disable(context.Context, string) error
	Logs(context.Context, string, int) (string, error)
}

type Manager struct{ backend Backend }

func New(backend Backend) *Manager { return &Manager{backend: backend} }

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,254}$`)

func NormalizeName(name string) (string, error) {
	const op = "service.NormalizeName"
	if !validName.MatchString(name) || strings.Contains(name, "..") {
		return "", errors.Newf(errors.KindValidation, op, "invalid service name %q", name)
	}
	if !strings.HasSuffix(name, ".service") {
		name += ".service"
	}
	return name, nil
}

func (m *Manager) List(ctx context.Context) ([]Service, error) { return m.backend.List(ctx) }

func (m *Manager) Status(ctx context.Context, name string) (Service, error) {
	name, err := NormalizeName(name)
	if err != nil {
		return Service{}, err
	}
	return m.backend.Status(ctx, name)
}

func (m *Manager) Start(ctx context.Context, name string) error {
	return m.action(ctx, "start", name, m.backend.Start)
}
func (m *Manager) Stop(ctx context.Context, name string) error {
	return m.action(ctx, "stop", name, m.backend.Stop)
}
func (m *Manager) Restart(ctx context.Context, name string) error {
	return m.action(ctx, "restart", name, m.backend.Restart)
}
func (m *Manager) Enable(ctx context.Context, name string) error {
	return m.action(ctx, "enable", name, m.backend.Enable)
}
func (m *Manager) Disable(ctx context.Context, name string) error {
	return m.action(ctx, "disable", name, m.backend.Disable)
}

func (m *Manager) Logs(ctx context.Context, name string, lines int) (string, error) {
	name, err := NormalizeName(name)
	if err != nil {
		return "", err
	}
	if lines < 1 || lines > 10000 {
		return "", errors.New(errors.KindValidation, "service.Manager.Logs", "log lines must be between 1 and 10000")
	}
	return m.backend.Logs(ctx, name, lines)
}

func (m *Manager) action(ctx context.Context, action, name string, fn func(context.Context, string) error) error {
	normalized, err := NormalizeName(name)
	if err != nil {
		return err
	}
	if err := fn(ctx, normalized); err != nil {
		return errors.Wrapf(errors.KindOf(err), "service.Manager."+action, err, "failed to %s service %q", action, name)
	}
	return nil
}
