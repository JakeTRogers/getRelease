package cmd

import (
	"bytes"
	"encoding/json"
	"reflect"
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

func TestRunListRepositoryArgumentWithTagListsAssets(t *testing.T) {
	client := &fakeReleaseClient{
		getReleaseByTag: func(owner, repo, tag string) (*github.Release, error) {
			if owner != "cli" || repo != "tool" || tag != "v2.0.0" {
				t.Fatalf("GetReleaseByTag() called with %s/%s %s", owner, repo, tag)
			}
			return &github.Release{
				TagName: "v2.0.0",
				Assets:  []github.Asset{{Name: "tool_linux_amd64.tar.gz", Size: 4096}},
			}, nil
		},
	}
	useTestCommandDeps(t, client)

	cmd := &cobra.Command{}
	addListTestFlags(cmd)
	if err := cmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("set format: %v", err)
	}

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runList(cmd, []string{"cli/tool@v2.0.0"}); err != nil {
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

func TestListRejectsInvalidRepositoryArgument(t *testing.T) {
	cmd := &cobra.Command{}
	addListTestFlags(cmd)
	if err := validateRepoArgs(cmd, []string{"tool"}); err != nil {
		t.Fatalf("validateRepoArgs() error = %v, want the argument left to resolveRepo", err)
	}
	if err := runList(cmd, []string{"tool"}); err == nil || !strings.Contains(err.Error(), `invalid repository "tool"`) {
		t.Fatalf("runList(tool) error = %v, want invalid repository error", err)
	}
}

func TestListReleasesMarksPrereleasesAndDrafts(t *testing.T) {
	published := time.Date(2026, time.October, 4, 0, 0, 0, 0, time.UTC)
	client := &fakeReleaseClient{
		listReleases: func(_, _ string, _ int) ([]github.Release, error) {
			return []github.Release{
				{TagName: "v2.0.0-draft", Draft: true},
				{TagName: "nightly", Name: "Nightly build", Prerelease: true, PublishedAt: published},
				{TagName: "v1.2.3", PublishedAt: published},
			}, nil
		},
	}
	cmd := &cobra.Command{}
	addListTestFlags(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := listReleases(cmd, client, "github.com", "cli", "tool", "text"); err != nil {
		t.Fatalf("listReleases() error: %v", err)
	}

	rows := map[string]string{}
	for _, line := range strings.Split(out.String(), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			rows[fields[0]] = line
		}
	}
	if !strings.Contains(rows["TAG"], "TYPE") {
		t.Errorf("header = %q, want a TYPE column", rows["TAG"])
	}
	if got := strings.Fields(rows["v2.0.0-draft"]); !reflect.DeepEqual(got, []string{"v2.0.0-draft", "draft", "-", "0"}) {
		t.Errorf("draft row = %q, want draft type and no date", rows["v2.0.0-draft"])
	}
	if !strings.Contains(rows["nightly"], "prerelease") {
		t.Errorf("prerelease row = %q, want prerelease type", rows["nightly"])
	}
	if got := strings.Fields(rows["v1.2.3"]); !reflect.DeepEqual(got, []string{"v1.2.3", "2026-10-04", "0"}) {
		t.Errorf("release row = %q, want no type", rows["v1.2.3"])
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

	if err := listReleases(cmd, client, "github.com", "cli", "empty", "text"); err != nil {
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

	if err := listAssets(cmd, client, "github.com", "cli", "tool", "v1.0.0", "text"); err != nil {
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

	if err := listAssets(cmd, client, "github.com", "cli", "tool", "v1.0.0", "text"); err != nil {
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

func TestRunListRejectsUnknownFormat(t *testing.T) {
	useTestCommandDeps(t, &fakeReleaseClient{
		listReleases: func(_, _ string, _ int) ([]github.Release, error) {
			t.Fatal("ListReleases() should not be called with an invalid format")
			return nil, nil
		},
	})

	cmd := &cobra.Command{}
	addListTestFlags(cmd)
	for flag, value := range map[string]string{"owner": "cli", "repo": "tool", "format": "xml"} {
		if err := cmd.Flags().Set(flag, value); err != nil {
			t.Fatalf("set %s: %v", flag, err)
		}
	}

	err := runList(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), `unsupported output format "xml"`) {
		t.Fatalf("runList() error = %v, want unsupported format error", err)
	}
}

func TestRunListAcceptsUppercaseJSON(t *testing.T) {
	useTestCommandDeps(t, &fakeReleaseClient{
		listReleases: func(_, _ string, _ int) ([]github.Release, error) {
			return []github.Release{{TagName: "v1.0.0"}}, nil
		},
	})

	cmd := &cobra.Command{}
	addListTestFlags(cmd)
	for flag, value := range map[string]string{"owner": "cli", "repo": "tool", "format": "JSON"} {
		if err := cmd.Flags().Set(flag, value); err != nil {
			t.Fatalf("set %s: %v", flag, err)
		}
	}
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runList(cmd, nil); err != nil {
		t.Fatalf("runList() error: %v", err)
	}
	var got []github.Release
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("runList() output is not JSON: %v\n%s", err, out.String())
	}
}

func TestListEmptyJSONIsEmptyArray(t *testing.T) {
	client := &fakeReleaseClient{
		listReleases: func(_, _ string, _ int) ([]github.Release, error) {
			return nil, nil
		},
		getReleaseByTag: func(_, _, tag string) (*github.Release, error) {
			return &github.Release{TagName: tag}, nil
		},
	}
	cmd := &cobra.Command{}
	addListTestFlags(cmd)

	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := listReleases(cmd, client, "github.com", "cli", "empty", "json"); err != nil {
		t.Fatalf("listReleases() error: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "[]" {
		t.Errorf("listReleases() json output = %q, want []", got)
	}

	out.Reset()
	if err := listAssets(cmd, client, "github.com", "cli", "tool", "v1.0.0", "json"); err != nil {
		t.Fatalf("listAssets() error: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "[]" {
		t.Errorf("listAssets() json output = %q, want []", got)
	}
}
