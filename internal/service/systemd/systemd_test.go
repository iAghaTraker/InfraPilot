package systemd

import (
	"context"
	stderrors "errors"
	iperrors "github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/service"
	"reflect"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}
type fakeRunner struct {
	outputs [][]byte
	errs    []error
	calls   []call
}

func (f *fakeRunner) Run(_ context.Context, n string, a ...string) ([]byte, error) {
	f.calls = append(f.calls, call{n, append([]string(nil), a...)})
	i := len(f.calls) - 1
	return f.outputs[i], f.errs[i]
}
func TestListParsesAndMergesUnits(t *testing.T) {
	f := &fakeRunner{outputs: [][]byte{[]byte("nginx.service loaded active running Web server\ncron.service loaded inactive dead Scheduler\n"), []byte("nginx.service enabled\ncron.service disabled\n")}, errs: []error{nil, nil}}
	got, err := NewWithRunner(f).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []service.Service{{Name: "cron", Description: "Scheduler", State: service.StateStopped, Enablement: service.Disabled}, {Name: "nginx", Description: "Web server", State: service.StateRunning, Enablement: service.Enabled}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}
func TestStatusParsesProperties(t *testing.T) {
	f := &fakeRunner{outputs: [][]byte{[]byte("Id=nginx.service\nDescription=Web server\nLoadState=loaded\nActiveState=active\nUnitFileState=enabled\nMainPID=42\nActiveEnterTimestamp=Tue 2026\nMemoryCurrent=4096\n")}, errs: []error{nil}}
	got, err := NewWithRunner(f).Status(context.Background(), "nginx.service")
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != 42 || got.MemoryBytes != 4096 || got.State != service.StateRunning {
		t.Fatalf("got %#v", got)
	}
}
func TestMutationsUseArgumentBoundary(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart", "enable", "disable"} {
		f := &fakeRunner{outputs: [][]byte{nil}, errs: []error{nil}}
		a := NewWithRunner(f)
		switch action {
		case "start":
			a.Start(context.Background(), "nginx.service")
		case "stop":
			a.Stop(context.Background(), "nginx.service")
		case "restart":
			a.Restart(context.Background(), "nginx.service")
		case "enable":
			a.Enable(context.Background(), "nginx.service")
		case "disable":
			a.Disable(context.Background(), "nginx.service")
		}
		if !reflect.DeepEqual(f.calls[0].args, []string{action, "--", "nginx.service"}) {
			t.Errorf("%s args: %#v", action, f.calls[0].args)
		}
	}
}
func TestCommandErrorsAreClassified(t *testing.T) {
	tests := []struct {
		msg  string
		kind iperrors.Kind
	}{{"Authentication is required", iperrors.KindPermission}, {"Unit x.service not found", iperrors.KindNotFound}, {"System has not been booted with systemd", iperrors.KindUnsupported}}
	for _, tt := range tests {
		f := &fakeRunner{outputs: [][]byte{[]byte(tt.msg)}, errs: []error{stderrors.New("exit 1")}}
		_, err := NewWithRunner(f).Logs(context.Background(), "x.service", 10)
		if !iperrors.IsKind(err, tt.kind) {
			t.Errorf("%q kind %s: %v", tt.msg, iperrors.KindOf(err), err)
		}
		if strings.Contains(err.Error(), "exit 1") && tt.kind != iperrors.KindUnknown {
			t.Errorf("raw exit leaked: %v", err)
		}
	}
}
