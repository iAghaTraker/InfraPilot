package errors

import (
	stderrors "errors"
	"io/fs"
	"testing"
)

func TestErrorMessageFormatting(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "message and cause",
			err:  &Error{Op: "storage.Open", Kind: KindStorage, Msg: "failed to initialize database", Err: fs.ErrPermission},
			want: "failed to initialize database: permission denied",
		},
		{
			name: "message only",
			err:  &Error{Op: "config.Validate", Kind: KindConfig, Msg: "logging.level must be one of debug, info, warn, error"},
			want: "logging.level must be one of debug, info, warn, error",
		},
		{
			name: "cause only",
			err:  &Error{Op: "storage.Open", Kind: KindStorage, Err: fs.ErrNotExist},
			want: "file does not exist",
		},
		{
			name: "neither, still traceable",
			err:  &Error{Op: "some.Op"},
			want: "unspecified error in some.Op",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The error model is only useful if the standard library's inspection helpers
// keep working through it.
func TestUnwrapInteroperatesWithStdlib(t *testing.T) {
	err := Wrap(KindStorage, "storage.Open", "failed to initialize database", fs.ErrPermission)

	if !stderrors.Is(err, fs.ErrPermission) {
		t.Error("errors.Is could not see through the wrapper")
	}

	var target *Error
	if !stderrors.As(err, &target) {
		t.Fatal("errors.As could not extract *Error")
	}
	if target.Op != "storage.Open" {
		t.Errorf("Op = %q, want %q", target.Op, "storage.Open")
	}
}

func TestWrapReturnsNilForNilCause(t *testing.T) {
	if err := Wrap(KindStorage, "storage.Open", "should vanish", nil); err != nil {
		t.Errorf("Wrap(nil) = %v, want nil", err)
	}
	if err := Wrapf(KindStorage, "storage.Open", nil, "should vanish %d", 1); err != nil {
		t.Errorf("Wrapf(nil) = %v, want nil", err)
	}
}

func TestKindOf(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Kind
	}{
		{"nil", nil, KindUnknown},
		{"foreign error", stderrors.New("external"), KindUnknown},
		{"direct", New(KindConfig, "config.Load", "bad"), KindConfig},
		{"nested", Wrap(KindStorage, "a.B", "outer", New(KindPermission, "c.D", "inner")), KindStorage},
		{
			// An unclassified outer layer must not mask a classified cause,
			// otherwise adding context would erase the reason for failure.
			name: "unclassified outer defers to inner",
			err:  &Error{Op: "a.B", Kind: KindUnknown, Msg: "outer", Err: New(KindPermission, "c.D", "inner")},
			want: KindPermission,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := KindOf(tt.err); got != tt.want {
				t.Errorf("KindOf() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"success", nil, ExitOK},
		{"usage error", New(KindUsage, "cli.Run", "unknown command"), ExitUsage},
		{"storage failure", New(KindStorage, "storage.Open", "boom"), ExitFailure},
		{"foreign failure", stderrors.New("external"), ExitFailure},
		{"wrapped usage error", Wrap(KindUsage, "cli.Run", "bad flag", stderrors.New("cause")), ExitUsage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestChainRecordsFailurePath(t *testing.T) {
	inner := New(KindPermission, "system.EnsureDir", "cannot create directory")
	mid := Wrap(KindStorage, "storage.Open", "failed to prepare data directory", inner)
	outer := Wrap(KindStorage, "agent.Run", "failed to initialize storage", mid)

	want := "agent.Run <- storage.Open <- system.EnsureDir"
	if got := Chain(outer); got != want {
		t.Errorf("Chain() = %q, want %q", got, want)
	}

	if got := Chain(stderrors.New("foreign")); got != "" {
		t.Errorf("Chain(foreign) = %q, want empty", got)
	}
}

func TestOpOf(t *testing.T) {
	if got := OpOf(New(KindConfig, "config.Load", "x")); got != "config.Load" {
		t.Errorf("OpOf() = %q, want %q", got, "config.Load")
	}
	if got := OpOf(stderrors.New("foreign")); got != "" {
		t.Errorf("OpOf(foreign) = %q, want empty", got)
	}
}

func TestKindStringsAreStableAndUnique(t *testing.T) {
	kinds := []Kind{
		KindUnknown, KindUsage, KindConfig, KindValidation,
		KindPermission, KindNotFound, KindStorage, KindUnsupported, KindInternal,
	}

	seen := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		s := k.String()
		if s == "" {
			t.Errorf("Kind(%d).String() is empty", k)
		}
		if seen[s] {
			t.Errorf("Kind(%d).String() = %q is duplicated", k, s)
		}
		seen[s] = true
	}
}
