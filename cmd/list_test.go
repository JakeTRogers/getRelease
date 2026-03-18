package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/JakeTRogers/getRelease/internal/github"
)

func TestRunListReleasesText(t *testing.T) {
	client := &fakeReleaseClient{
		listReleases: func(owner, repo string, limit int) ([]github.Release, error) {
			if owner != "cli" || repo != "tool" {
				t.Fatalf("ListReleases() called with %s/%s", owner, repo)
			}
			if limit != 5 {
				t.Fatalf("ListReleases() limit = %d, want 5", limit)
			}
			return []github.Release{{
				TagName:     "v1.2.3",
				Name:        "Tool 1.2.3",
				PublishedAt: time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC),
				Assets:      []github.Asset{{Name: "tool_linux_amd64"}},
			}}, nil
		},
	}
	useTestCommandDeps(t, client)

	cmd := &cobra.Command{}
	addListTestFlags(cmd)
	if err := cmd.Flags().Set("owner", "cli"); err != nil {
		t.Fatalf("set owner: %v", err)
	}
	if err := cmd.Flags().Set("repo", "tool"); err != nil {
		t.Fatalf("set repo: %v", err)
	}
	if err := cmd.Flags().Set("limit", "5"); err != nil {
		t.Fatalf("set limit: %v", err)
	}

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runList(cmd, nil); err != nil {
		t.Fatalf("runList() error: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "Releases for cli/tool") {
		t.Fatalf("runList() output = %q, want releases heading", text)
	}
	if !strings.Contains(text, "v1.2.3") || !strings.Contains(text, "Tool 1.2.3") {
		t.Fatalf("runList() output = %q, want release row", text)
	}
	if !strings.Contains(text, "Browse releases: https://github.com/cli/tool/releases") {
		t.Fatalf("runList() output = %q, want releases footer", text)
	}
}

func TestRunListAssetsJSON(t *testing.T) {
	client := &fakeReleaseClient{
		getReleaseByTag: func(owner, repo, tag string) (*github.Release, error) {
			if owner != "cli" || repo != "tool" || tag != "v2.0.0" {
				t.Fatalf("GetReleaseByTag() called with %s/%s %s", owner, repo, tag)
			}
			return &github.Release{
				TagName: "v2.0.0",
				Name:    "Tool 2.0.0",
				Assets: []github.Asset{{
					Name: "tool_linux_amd64.tar.gz",
					Size: 4096,
				}},
			}, nil
		},
	}
	useTestCommandDeps(t, client)

	cmd := &cobra.Command{}
	addListTestFlags(cmd)
	if err := cmd.Flags().Set("owner", "cli"); err != nil {
		t.Fatalf("set owner: %v", err)
	}
	if err := cmd.Flags().Set("repo", "tool"); err != nil {
		t.Fatalf("set repo: %v", err)
	}
	if err := cmd.Flags().Set("tag", "v2.0.0"); err != nil {
		t.Fatalf("set tag: %v", err)
	}
	if err := cmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("set format: %v", err)
	}

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runList(cmd, nil); err != nil {
		t.Fatalf("runList() error: %v", err)
	}

	var got []github.Asset
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "tool_linux_amd64.tar.gz" {
		t.Fatalf("runList() assets = %+v, want one tagged asset", got)
	}
}

func TestListReleasesEmpty(t *testing.T) {
	client := &fakeReleaseClient{
		listReleases: func(owner, repo string, limit int) ([]github.Release, error) {
			return nil, nil
		},
	}

	cmd := &cobra.Command{}
	addListTestFlags(cmd)
	if err := cmd.Flags().Set("limit", "3"); err != nil {
		t.Fatalf("set limit: %v", err)
	}

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := listReleases(cmd, client, "cli", "empty", "text"); err != nil {
		t.Fatalf("listReleases() error: %v", err)
	}
	if !strings.Contains(out.String(), "No releases found for cli/empty") {
		t.Fatalf("listReleases() output = %q, want empty message", out.String())
	}
}

func TestListAssetsText(t *testing.T) {
	client := &fakeReleaseClient{
		getReleaseByTag: func(owner, repo, tag string) (*github.Release, error) {
			return &github.Release{
				TagName: "v1.0.0",
				Name:    "Tool 1.0.0",
				Assets: []github.Asset{{
					Name: "tool_linux_amd64.tar.gz",
					Size: 2048,
				}},
			}, nil
		},
	}

	cmd := &cobra.Command{}
	addListTestFlags(cmd)

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := listAssets(cmd, client, "cli", "tool", "v1.0.0", "text"); err != nil {
		t.Fatalf("listAssets() error: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "Assets for cli/tool Tool 1.0.0") || !strings.Contains(text, "2.0 KB") {
		t.Fatalf("listAssets() output = %q, want assets table", text)
	}
	if !strings.Contains(text, "Browse this release: https://github.com/cli/tool/releases/tag/v1.0.0") {
		t.Fatalf("listAssets() output = %q, want release footer", text)
	}
}

func TestListAssetsEmpty(t *testing.T) {
	client := &fakeReleaseClient{
		getReleaseByTag: func(owner, repo, tag string) (*github.Release, error) {
			return &github.Release{TagName: tag}, nil
		},
	}

	cmd := &cobra.Command{}
	addListTestFlags(cmd)

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := listAssets(cmd, client, "cli", "tool", "v1.0.0", "text"); err != nil {
		t.Fatalf("listAssets() error: %v", err)
	}
	if !strings.Contains(out.String(), "Release v1.0.0 has no downloadable assets") {
		t.Fatalf("listAssets() output = %q, want empty asset message", out.String())
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{name: "bytes", bytes: 42, want: "42 B"},
		{name: "kilobytes", bytes: 2 * 1024, want: "2.0 KB"},
		{name: "megabytes", bytes: 3 * 1024 * 1024, want: "3.0 MB"},
		{name: "gigabytes", bytes: 4 * 1024 * 1024 * 1024, want: "4.0 GB"},
	}

	for _, tt := range tests {
		if got := formatBytes(tt.bytes); got != tt.want {
			t.Fatalf("%s: formatBytes(%d) = %q, want %q", tt.name, tt.bytes, got, tt.want)
		}
	}
}
