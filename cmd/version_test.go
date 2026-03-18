package cmd

import (
	"bytes"
	"fmt"
	"runtime"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var out bytes.Buffer
	versionCmd.SetOut(&out)

	if err := versionCmd.RunE(versionCmd, nil); err != nil {
		t.Fatalf("versionCmd.RunE() error: %v", err)
	}

	want := fmt.Sprintf("getRelease %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	if out.String() != want {
		t.Fatalf("versionCmd output = %q, want %q", out.String(), want)
	}
}
