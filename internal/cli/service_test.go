package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/service"
)

type cliBackend struct {
	called, name string
	lines        int
}

func (f *cliBackend) List(context.Context) ([]service.Service, error) {
	return []service.Service{{Name: "nginx", State: service.StateRunning, Enablement: service.Enabled}}, nil
}
func (f *cliBackend) Status(_ context.Context, n string) (service.Service, error) {
	f.called = "status"
	f.name = n
	return service.Service{Name: "nginx", State: service.StateRunning, Enablement: service.Enabled, PID: 42}, nil
}
func (f *cliBackend) Start(_ context.Context, n string) error {
	f.called = "start"
	f.name = n
	return nil
}
func (f *cliBackend) Stop(_ context.Context, n string) error {
	f.called = "stop"
	f.name = n
	return nil
}
func (f *cliBackend) Restart(_ context.Context, n string) error {
	f.called = "restart"
	f.name = n
	return nil
}
func (f *cliBackend) Enable(_ context.Context, n string) error {
	f.called = "enable"
	f.name = n
	return nil
}
func (f *cliBackend) Disable(_ context.Context, n string) error {
	f.called = "disable"
	f.name = n
	return nil
}
func (f *cliBackend) Logs(_ context.Context, n string, l int) (string, error) {
	f.called = "logs"
	f.name = n
	f.lines = l
	return "line one\n", nil
}

func withCLIBackend(t *testing.T) *cliBackend {
	t.Helper()
	f := &cliBackend{}
	old := newServiceManager
	newServiceManager = func() *service.Manager { return service.New(f) }
	t.Cleanup(func() { newServiceManager = old })
	return f
}
func executeDirect(args ...string) (string, string, int) {
	var out, err bytes.Buffer
	code := Execute(context.Background(), args, IO{Out: &out, Err: &err})
	return out.String(), err.String(), code
}
func TestServiceCLICommands(t *testing.T) {
	for _, args := range [][]string{{"service", "list"}, {"service", "status", "nginx"}, {"service", "start", "nginx"}, {"service", "stop", "nginx"}, {"service", "restart", "nginx"}, {"service", "enable", "nginx"}, {"service", "disable", "nginx"}, {"service", "logs", "nginx", "--lines", "100"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			f := withCLIBackend(t)
			out, stderr, code := executeDirect(args...)
			if code != errors.ExitOK {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if out == "" {
				t.Fatal("empty output")
			}
			if args[1] == "logs" && f.lines != 100 {
				t.Fatalf("lines=%d", f.lines)
			}
		})
	}
}
func TestServiceCLIUsageAndValidation(t *testing.T) {
	for _, args := range [][]string{{"service"}, {"service", "status"}, {"service", "bogus"}, {"service", "start", "-bad"}, {"service", "logs", "nginx", "--lines", "nope"}} {
		withCLIBackend(t)
		_, stderr, code := executeDirect(args...)
		if code == errors.ExitOK || stderr == "" {
			t.Errorf("%v: exit=%d stderr=%q", args, code, stderr)
		}
	}
}
func TestSystemInfoCLI(t *testing.T) {
	out, stderr, code := executeDirect("system", "info")
	if code != 0 {
		t.Fatalf("%d: %s", code, stderr)
	}
	for _, want := range []string{"Operating System", "Architecture", "CPU", "Memory", "Disk", "Uptime"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}
