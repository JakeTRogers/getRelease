package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/JakeTRogers/getRelease/internal/github"
	"github.com/JakeTRogers/getRelease/internal/selector"
)

func TestNormalizeOutputFormat(t *testing.T) {
	t.Parallel()

	got, err := normalizeOutputFormat("JSON")
	if err != nil {
		t.Fatalf("normalizeOutputFormat() error: %v", err)
	}
	if got != "json" {
		t.Fatalf("normalizeOutputFormat() = %q, want json", got)
	}

	if _, err := normalizeOutputFormat("yaml"); err == nil {
		t.Fatal("normalizeOutputFormat() error = nil, want error")
	}
}

func TestOutputRootResult(t *testing.T) {
	t.Parallel()

	result := rootCommandResult{
		Owner:        "owner",
		Repo:         "repo",
		RequestedTag: "latest",
		ReleaseTag:   "v1.2.3",
		Asset:        github.Asset{Name: "tool_linux_amd64.tar.gz"},
		DownloadPath: "/tmp/tool_linux_amd64.tar.gz",
		DownloadSize: 1024,
		Extracted:    true,
		Binaries:     []string{"tool"},
		Installed:    []string{"/usr/local/bin/tool"},
	}

	var buf bytes.Buffer
	if err := outputRootResult(&buf, result); err != nil {
		t.Fatalf("outputRootResult() error: %v", err)
	}

	var got rootCommandResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if got.ReleaseTag != result.ReleaseTag {
		t.Fatalf("outputRootResult() releaseTag = %q, want %q", got.ReleaseTag, result.ReleaseTag)
	}
	if len(got.Installed) != 1 || got.Installed[0] != result.Installed[0] {
		t.Fatalf("outputRootResult() installed = %v, want %v", got.Installed, result.Installed)
	}
}

func TestResolveInstallNamesForSelection(t *testing.T) {
	t.Parallel()

	t.Run("defaults to heuristic name", func(t *testing.T) {
		t.Parallel()

		got, err := resolveInstallNamesForSelection("argo-cd", "argocd-linux-amd64", "linux", "amd64", "v1.0.0", []string{"argocd-linux-amd64"}, "")
		if err != nil {
			t.Fatalf("resolveInstallNamesForSelection() error: %v", err)
		}
		if got["argocd-linux-amd64"] != "argocd" {
			t.Fatalf("resolveInstallNamesForSelection() = %q, want %q", got["argocd-linux-amd64"], "argocd")
		}
	})

	t.Run("uses forced install name", func(t *testing.T) {
		t.Parallel()

		got, err := resolveInstallNamesForSelection("argo-cd", "argocd-linux-amd64", "linux", "amd64", "v1.0.0", []string{"argocd-linux-amd64"}, "argocd")
		if err != nil {
			t.Fatalf("resolveInstallNamesForSelection() error: %v", err)
		}
		if got["argocd-linux-amd64"] != "argocd" {
			t.Fatalf("resolveInstallNamesForSelection() = %q, want %q", got["argocd-linux-amd64"], "argocd")
		}
	})

	t.Run("rejects override for multiple binaries", func(t *testing.T) {
		t.Parallel()

		_, err := resolveInstallNamesForSelection("repo", "asset", "linux", "amd64", "v1.0.0", []string{"tool", "helper"}, "tool")
		if err == nil {
			t.Fatal("resolveInstallNamesForSelection() error = nil, want error")
		}
	})

	t.Run("rejects path override", func(t *testing.T) {
		t.Parallel()

		_, err := resolveInstallNamesForSelection("repo", "asset", "linux", "amd64", "v1.0.0", []string{"tool"}, "nested/tool")
		if err == nil {
			t.Fatal("resolveInstallNamesForSelection() error = nil, want error")
		}
	})
}

func TestReportError(t *testing.T) {
	t.Parallel()

	rateLimit := &github.RateLimitError{ResetAt: time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)}
	tests := []struct {
		name       string
		err        error
		wantCode   int
		wantOutput string
	}{
		{name: "cancelled prompt", err: fmt.Errorf("selecting asset: %w", selector.ErrCancelled), wantCode: 2},
		{name: "rate limit drops wrapping", err: fmt.Errorf("fetching release: %w", rateLimit), wantCode: 1, wantOutput: "Error: " + rateLimit.Error() + "\n"},
		{name: "other error", err: errors.New("boom"), wantCode: 1, wantOutput: "Error: boom\n"},
	}
	for _, tt := range tests {
		var out bytes.Buffer
		if got := reportError(&out, tt.err); got != tt.wantCode {
			t.Errorf("%s: reportError() = %d, want %d", tt.name, got, tt.wantCode)
		}
		if out.String() != tt.wantOutput {
			t.Errorf("%s: reportError() wrote %q, want %q", tt.name, out.String(), tt.wantOutput)
		}
	}
}
