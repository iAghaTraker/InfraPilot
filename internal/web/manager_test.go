package web

import (
	"context"
	"reflect"
	"testing"
)

type fakeRunner struct {
	command string
	args    []string
	output  string
	err     error
}

func (f *fakeRunner) Run(_ context.Context, c string, a ...string) (string, error) {
	f.command = c
	f.args = append([]string(nil), a...)
	return f.output, f.err
}
func TestManagerUsesFixedSystemdArguments(t *testing.T) {
	f := &fakeRunner{}
	m := NewManager(f)
	if err := m.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.command != "systemctl" || !reflect.DeepEqual(f.args, []string{"start", "infrapilot-web.service"}) {
		t.Fatalf("call=%s %#v", f.command, f.args)
	}
	f.output = "active\n"
	got, err := m.Status(t.Context())
	if err != nil || got != "active\n" {
		t.Fatalf("status=%q err=%v", got, err)
	}
	_, _ = m.Logs(t.Context())
	if !reflect.DeepEqual(f.args, []string{"-u", "infrapilot-web.service", "-n", "100", "--no-pager"}) {
		t.Fatalf("logs args=%#v", f.args)
	}
}
