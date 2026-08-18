package service

import (
	"context"
	stderrors "errors"
	"testing"

	iperrors "github.com/iAghaTraker/InfraPilot/internal/errors"
)

type fakeBackend struct {
	called   string
	name     string
	services []Service
	status   Service
	logs     string
	err      error
}

func (f *fakeBackend) List(context.Context) ([]Service, error) {
	f.called = "list"
	return f.services, f.err
}
func (f *fakeBackend) Status(_ context.Context, n string) (Service, error) {
	f.called = "status"
	f.name = n
	return f.status, f.err
}
func (f *fakeBackend) Start(_ context.Context, n string) error {
	f.called = "start"
	f.name = n
	return f.err
}
func (f *fakeBackend) Stop(_ context.Context, n string) error {
	f.called = "stop"
	f.name = n
	return f.err
}
func (f *fakeBackend) Restart(_ context.Context, n string) error {
	f.called = "restart"
	f.name = n
	return f.err
}
func (f *fakeBackend) Enable(_ context.Context, n string) error {
	f.called = "enable"
	f.name = n
	return f.err
}
func (f *fakeBackend) Disable(_ context.Context, n string) error {
	f.called = "disable"
	f.name = n
	return f.err
}
func (f *fakeBackend) Logs(_ context.Context, n string, _ int) (string, error) {
	f.called = "logs"
	f.name = n
	return f.logs, f.err
}

func TestManagerOperations(t *testing.T) {
	for _, action := range []string{"status", "start", "stop", "restart", "enable", "disable", "logs"} {
		t.Run(action, func(t *testing.T) {
			f := &fakeBackend{status: Service{Name: "nginx"}, logs: "ok"}
			m := New(f)
			var err error
			switch action {
			case "status":
				_, err = m.Status(context.Background(), "nginx")
			case "start":
				err = m.Start(context.Background(), "nginx")
			case "stop":
				err = m.Stop(context.Background(), "nginx")
			case "restart":
				err = m.Restart(context.Background(), "nginx")
			case "enable":
				err = m.Enable(context.Background(), "nginx")
			case "disable":
				err = m.Disable(context.Background(), "nginx")
			case "logs":
				_, err = m.Logs(context.Background(), "nginx", 50)
			}
			if err != nil {
				t.Fatal(err)
			}
			if f.called != action || f.name != "nginx.service" {
				t.Fatalf("called %q with %q", f.called, f.name)
			}
		})
	}
}
func TestList(t *testing.T) {
	f := &fakeBackend{services: []Service{{Name: "cron"}}}
	got, err := New(f).List(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("got %#v, %v", got, err)
	}
}
func TestInvalidNamesNeverReachBackend(t *testing.T) {
	for _, name := range []string{"", "-rf", "../ssh", "nginx;reboot", "a/b", "nginx service"} {
		f := &fakeBackend{}
		err := New(f).Start(context.Background(), name)
		if !iperrors.IsKind(err, iperrors.KindValidation) {
			t.Errorf("%q: %v", name, err)
		}
		if f.called != "" {
			t.Errorf("backend called for %q", name)
		}
	}
}
func TestErrorsKeepClassification(t *testing.T) {
	cause := iperrors.Wrap(iperrors.KindPermission, "fake", "permission denied", stderrors.New("exit 1"))
	err := New(&fakeBackend{err: cause}).Restart(context.Background(), "nginx")
	if !iperrors.IsKind(err, iperrors.KindPermission) {
		t.Fatalf("kind = %s", iperrors.KindOf(err))
	}
}
func TestLogLimit(t *testing.T) {
	for _, n := range []int{0, 10001} {
		_, err := New(&fakeBackend{}).Logs(context.Background(), "nginx", n)
		if !iperrors.IsKind(err, iperrors.KindValidation) {
			t.Errorf("lines %d: %v", n, err)
		}
	}
}
