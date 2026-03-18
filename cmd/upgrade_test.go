package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/JakeTRogers/getRelease/internal/github"
	"github.com/JakeTRogers/getRelease/internal/history"
)

func TestValidateUpgradeArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		all       bool
		owner     string
		repo      string
		wantError bool
	}{
		{name: "single target", args: []string{"k9s"}},
		{name: "all without target", all: true},
		{name: "missing target", wantError: true},
		{name: "all with target", args: []string{"k9s"}, all: true, wantError: true},
		{name: "all with owner", all: true, owner: "derailed", wantError: true},
		{name: "all with repo", all: true, repo: "k9s", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{Use: "upgrade"}
			cmd.Flags().Bool("all", false, "")
			cmd.Flags().String("owner", "", "")
			cmd.Flags().String("repo", "", "")

			if err := cmd.Flags().Set("all", strconv.FormatBool(tt.all)); err != nil {
				t.Fatalf("set all flag: %v", err)
			}
			if tt.owner != "" {
				if err := cmd.Flags().Set("owner", tt.owner); err != nil {
					t.Fatalf("set owner flag: %v", err)
				}
			}
			if tt.repo != "" {
				if err := cmd.Flags().Set("repo", tt.repo); err != nil {
					t.Fatalf("set repo flag: %v", err)
				}
			}

			err := validateUpgradeArgs(cmd, tt.args)
			if (err != nil) != tt.wantError {
				t.Fatalf("validateUpgradeArgs() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestPresentHistoryRecords(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	presentPath := filepath.Join(tmp, "installed-bin")
	if err := os.WriteFile(presentPath, []byte("x"), 0o755); err != nil {
		t.Fatalf("write present binary: %v", err)
	}

	records := []history.Record{
		{
			Owner: "one",
			Repo:  "present",
			Binaries: []history.Binary{
				{InstallPath: presentPath},
			},
			InstalledAt: time.Now(),
		},
		{
			Owner: "two",
			Repo:  "missing",
			Binaries: []history.Binary{
				{InstallPath: filepath.Join(tmp, "missing-bin")},
			},
			InstalledAt: time.Now(),
		},
		{
			Owner:       "three",
			Repo:        "empty",
			Binaries:    nil,
			InstalledAt: time.Now(),
		},
	}

	present := presentHistoryRecords(records)
	if len(present) != 1 {
		t.Fatalf("presentHistoryRecords() returned %d records, want 1", len(present))
	}
	if present[0].Owner != "one" || present[0].Repo != "present" {
		t.Fatalf("presentHistoryRecords() returned unexpected record: %+v", present[0])
	}
}

func TestRecordInstalled(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	path := filepath.Join(tmp, "installed-bin")
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatalf("write present binary: %v", err)
	}

	tests := []struct {
		name string
		rec  history.Record
		want bool
	}{
		{
			name: "installed binary exists",
			rec:  history.Record{Binaries: []history.Binary{{InstallPath: path}}},
			want: true,
		},
		{
			name: "binary missing",
			rec:  history.Record{Binaries: []history.Binary{{InstallPath: filepath.Join(tmp, "missing")}}},
			want: false,
		},
		{
			name: "empty install path",
			rec:  history.Record{Binaries: []history.Binary{{InstallPath: ""}}},
			want: false,
		},
		{
			name: "no binaries",
			rec:  history.Record{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := recordInstalled(tt.rec); got != tt.want {
				t.Fatalf("recordInstalled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildArchiveUpgradeMappings(t *testing.T) {
	t.Parallel()

	rec := history.Record{
		Binaries: []history.Binary{
			{Name: "tool", InstalledAs: "tool", InstallPath: "/usr/local/bin/tool"},
			{Name: "helper", InstalledAs: "helper", InstallPath: "/usr/local/bin/helper"},
		},
	}

	maps, missing := buildArchiveUpgradeMappings(rec, "/tmp/extracted", []string{"tool", "nested/helper"})
	if len(missing) != 0 {
		t.Fatalf("buildArchiveUpgradeMappings() missing = %v, want none", missing)
	}
	if len(maps) != 2 {
		t.Fatalf("buildArchiveUpgradeMappings() returned %d mappings, want 2", len(maps))
	}
	if maps[0].src != "/tmp/extracted/tool" {
		t.Fatalf("first mapping src = %q, want %q", maps[0].src, "/tmp/extracted/tool")
	}
	if maps[1].src != "/tmp/extracted/nested/helper" {
		t.Fatalf("second mapping src = %q, want %q", maps[1].src, "/tmp/extracted/nested/helper")
	}
}

func TestBuildArchiveUpgradeMappings_UsesInstalledAsFallback(t *testing.T) {
	t.Parallel()

	rec := history.Record{
		Binaries: []history.Binary{
			{Name: "argocd-linux-amd64", InstalledAs: "argocd", InstallPath: "/usr/local/bin/argocd"},
		},
	}

	maps, missing := buildArchiveUpgradeMappings(rec, "/tmp/extracted", []string{"argocd"})
	if len(missing) != 0 {
		t.Fatalf("buildArchiveUpgradeMappings() missing = %v, want none", missing)
	}
	if len(maps) != 1 {
		t.Fatalf("buildArchiveUpgradeMappings() returned %d mappings, want 1", len(maps))
	}
	if maps[0].src != "/tmp/extracted/argocd" {
		t.Fatalf("mapping src = %q, want %q", maps[0].src, "/tmp/extracted/argocd")
	}
	if maps[0].dst != "/usr/local/bin/argocd" {
		t.Fatalf("mapping dst = %q, want %q", maps[0].dst, "/usr/local/bin/argocd")
	}
}

func TestBuildSingleAssetUpgradeMappings_MissingBinary(t *testing.T) {
	t.Parallel()

	rec := history.Record{
		Binaries: []history.Binary{
			{Name: "tool", InstalledAs: "tool", InstallPath: "/usr/local/bin/tool"},
			{Name: "helper", InstalledAs: "helper", InstallPath: "/usr/local/bin/helper"},
		},
	}

	maps, missing := buildSingleAssetUpgradeMappings(rec, github.Asset{Name: "tool"}, "/tmp/tool")
	if len(maps) != 1 {
		t.Fatalf("buildSingleAssetUpgradeMappings() returned %d mappings, want 1", len(maps))
	}
	wantMissing := []string{"helper"}
	if !reflect.DeepEqual(missing, wantMissing) {
		t.Fatalf("buildSingleAssetUpgradeMappings() missing = %v, want %v", missing, wantMissing)
	}
}
