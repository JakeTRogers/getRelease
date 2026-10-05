package platform

import "testing"

func TestSuggestInstallName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		repo     string
		asset    string
		binary   string
		osName   string
		arch     string
		tag      string
		wantName string
	}{
		{
			name:     "strips linux amd64 suffix",
			repo:     "argo-cd",
			asset:    "argocd-linux-amd64",
			binary:   "argocd-linux-amd64",
			osName:   "linux",
			arch:     "amd64",
			tag:      "v1.0.0",
			wantName: "argocd",
		},
		{
			name:     "strips underscore x86_64 suffix",
			repo:     "k9s",
			asset:    "k9s_Linux_x86_64.tar.gz",
			binary:   "k9s_Linux_x86_64",
			osName:   "linux",
			arch:     "amd64",
			tag:      "v1.0.0",
			wantName: "k9s",
		},
		{
			name:     "strips version after platform suffixes",
			repo:     "kubectl-convert",
			asset:    "kubectl-convert-v0.1.0-linux-amd64.tar.gz",
			binary:   "kubectl-convert-v0.1.0-linux-amd64",
			osName:   "linux",
			arch:     "amd64",
			tag:      "v0.1.0",
			wantName: "kubectl-convert",
		},
		{
			name:     "preserves exe extension",
			repo:     "gh",
			asset:    "gh-windows-amd64.zip",
			binary:   "gh-windows-amd64.exe",
			osName:   "windows",
			arch:     "amd64",
			tag:      "v1.0.0",
			wantName: "gh.exe",
		},
		{
			name:     "strips rust target triple suffix",
			repo:     "fd",
			asset:    "fd-v9.0.0-x86_64-unknown-linux-gnu.tar.gz",
			binary:   "fd-v9.0.0-x86_64-unknown-linux-gnu",
			osName:   "linux",
			arch:     "amd64",
			tag:      "v9.0.0",
			wantName: "fd",
		},
		{
			name:     "strips apple darwin target suffix",
			repo:     "delta",
			asset:    "delta-aarch64-apple-darwin.tar.gz",
			binary:   "delta-aarch64-apple-darwin",
			osName:   "darwin",
			arch:     "arm64",
			tag:      "v1.0.0",
			wantName: "delta",
		},
		{
			name:     "strips windows msvc target suffix",
			repo:     "gh",
			asset:    "gh-x86_64-pc-windows-msvc.zip",
			binary:   "gh-x86_64-pc-windows-msvc.exe",
			osName:   "windows",
			arch:     "amd64",
			tag:      "v1.0.0",
			wantName: "gh.exe",
		},
		{
			name:     "keeps original without platform suffix",
			repo:     "ripgrep",
			asset:    "ripgrep.tar.gz",
			binary:   "rg",
			osName:   "linux",
			arch:     "amd64",
			tag:      "v1.0.0",
			wantName: "rg",
		},
		{
			name:     "strips tag version from raw binary asset",
			repo:     "sh",
			asset:    "shfmt_v3.10.0_linux_amd64",
			binary:   "shfmt_v3.10.0_linux_amd64",
			osName:   "linux",
			arch:     "amd64",
			tag:      "v3.10.0",
			wantName: "shfmt",
		},
		{
			name:     "strips unprefixed tag version when repo differs",
			repo:     "tools",
			asset:    "mytool_1.2.3_linux_amd64.tar.gz",
			binary:   "mytool_1.2.3_linux_amd64",
			osName:   "linux",
			arch:     "amd64",
			tag:      "v1.2.3",
			wantName: "mytool",
		},
		{
			name:     "strips tag version without platform suffix",
			repo:     "tools",
			asset:    "tool-1.2.3.tar.gz",
			binary:   "tool-1.2.3",
			osName:   "linux",
			arch:     "amd64",
			tag:      "1.2.3",
			wantName: "tool",
		},
		{
			name:     "strips version of prefixed tag",
			repo:     "jq",
			asset:    "jq-1.7.1-linux-amd64",
			binary:   "jq-1.7.1-linux-amd64",
			osName:   "linux",
			arch:     "amd64",
			tag:      "jq-1.7.1",
			wantName: "jq",
		},
		{
			name:     "keeps version that differs from tag",
			repo:     "other",
			asset:    "tool-v2-linux-amd64",
			binary:   "tool-v2-linux-amd64",
			osName:   "linux",
			arch:     "amd64",
			tag:      "v1.0.0",
			wantName: "tool-v2",
		},
		{
			name:     "keeps trailing digits without separator",
			repo:     "python",
			asset:    "python3.12.tar.gz",
			binary:   "python3.12",
			osName:   "linux",
			arch:     "amd64",
			tag:      "3.12",
			wantName: "python3.12",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := SuggestInstallName(tt.repo, tt.asset, tt.binary, tt.osName, tt.arch, tt.tag); got != tt.wantName {
				t.Fatalf("SuggestInstallName() = %q, want %q", got, tt.wantName)
			}
		})
	}
}

func TestResolveInstallNames_CollisionFallsBackToOriginal(t *testing.T) {
	t.Parallel()

	binaries := []string{"foo", "nested/foo-linux-amd64"}
	got := ResolveInstallNames("foo", "foo-linux-amd64", "linux", "amd64", "v1.0.0", binaries)

	if got["foo"] != "foo" {
		t.Fatalf("ResolveInstallNames()[%q] = %q, want %q", "foo", got["foo"], "foo")
	}
	if got["nested/foo-linux-amd64"] != "foo-linux-amd64" {
		t.Fatalf("ResolveInstallNames()[%q] = %q, want %q", "nested/foo-linux-amd64", got["nested/foo-linux-amd64"], "foo-linux-amd64")
	}
}
