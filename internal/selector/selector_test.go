package selector

import (
	"errors"
	"os"
	"testing"

	"github.com/charmbracelet/huh"
)

func TestCheckTTYAndPromptsRequireInteractiveStdin(t *testing.T) {
	oldStdin := os.Stdin
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	os.Stdin = stdin
	t.Cleanup(func() {
		os.Stdin = oldStdin
		if err := stdin.Close(); err != nil {
			t.Fatalf("close stdin replacement: %v", err)
		}
	})

	if err := checkTTY(); !errors.Is(err, ErrNotInteractive) {
		t.Fatalf("checkTTY() error = %v, want %v", err, ErrNotInteractive)
	}
	if _, err := Select([]string{"one"}, "pick one"); !errors.Is(err, ErrNotInteractive) {
		t.Fatalf("Select() error = %v, want %v", err, ErrNotInteractive)
	}
	if _, err := Confirm("continue", true); !errors.Is(err, ErrNotInteractive) {
		t.Fatalf("Confirm() error = %v, want %v", err, ErrNotInteractive)
	}
}

func TestMapErrTranslatesUserAbort(t *testing.T) {
	if err := mapErr(huh.ErrUserAborted); !errors.Is(err, ErrCancelled) {
		t.Fatalf("mapErr() error = %v, want %v", err, ErrCancelled)
	}

	other := errors.New("boom")
	if err := mapErr(other); !errors.Is(err, other) {
		t.Fatalf("mapErr() error = %v, want original error", err)
	}
}
