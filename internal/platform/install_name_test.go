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
		wantName string
	}{
		{
			name:     "strips linux amd64 suffix",
			repo:     "argo-cd",
			asset:    "argocd-linux-amd64",
			binary:   "argocd-linux-amd64",
			osName:   "linux",
			arch:     "amd64",
			wantName: "argocd",
		},
		{
			name:     "strips underscore x86_64 suffix",
			repo:     "k9s",
			asset:    "k9s_Linux_x86_64.tar.gz",
			binary:   "k9s_Linux_x86_64",
			osName:   "linux",
			arch:     "amd64",
			wantName: "k9s",
		},
		{
			name:     "strips version after platform suffixes",
			repo:     "kubectl-convert",
			asset:    "kubectl-convert-v0.1.0-linux-amd64.tar.gz",
			binary:   "kubectl-convert-v0.1.0-linux-amd64",
			osName:   "linux",
			arch:     "amd64",
			wantName: "kubectl-convert",
		},
		{
			name:     "preserves exe extension",
			repo:     "gh",
			asset:    "gh-windows-amd64.zip",
			binary:   "gh-windows-amd64.exe",
			osName:   "windows",
			arch:     "amd64",
			wantName: "gh.exe",
		},
		{
			name:     "strips rust target triple suffix",
			repo:     "fd",
			asset:    "fd-v9.0.0-x86_64-unknown-linux-gnu.tar.gz",
			binary:   "fd-v9.0.0-x86_64-unknown-linux-gnu",
			osName:   "linux",
			arch:     "amd64",
			wantName: "fd",
		},
		{
			name:     "strips apple darwin target suffix",
			repo:     "delta",
			asset:    "delta-aarch64-apple-darwin.tar.gz",
			binary:   "delta-aarch64-apple-darwin",
			osName:   "darwin",
			arch:     "arm64",
			wantName: "delta",
		},
		{
			name:     "strips windows msvc target suffix",
			repo:     "gh",
			asset:    "gh-x86_64-pc-windows-msvc.zip",
			binary:   "gh-x86_64-pc-windows-msvc.exe",
			osName:   "windows",
			arch:     "amd64",
			wantName: "gh.exe",
		},
		{
			name:     "keeps original without platform suffix",
			repo:     "ripgrep",
			asset:    "ripgrep.tar.gz",
			binary:   "rg",
			osName:   "linux",
			arch:     "amd64",
			wantName: "rg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := SuggestInstallName(tt.repo, tt.asset, tt.binary, tt.osName, tt.arch); got != tt.wantName {
				t.Fatalf("SuggestInstallName() = %q, want %q", got, tt.wantName)
			}
		})
	}
}

func TestResolveInstallNames_CollisionFallsBackToOriginal(t *testing.T) {
	t.Parallel()

	binaries := []string{"foo", "nested/foo-linux-amd64"}
	got := ResolveInstallNames("foo", "foo-linux-amd64", "linux", "amd64", binaries)

	if got["foo"] != "foo" {
		t.Fatalf("ResolveInstallNames()[%q] = %q, want %q", "foo", got["foo"], "foo")
	}
	if got["nested/foo-linux-amd64"] != "foo-linux-amd64" {
		t.Fatalf("ResolveInstallNames()[%q] = %q, want %q", "nested/foo-linux-amd64", got["nested/foo-linux-amd64"], "foo-linux-amd64")
	}
}
