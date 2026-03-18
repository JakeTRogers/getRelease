package platform

import (
	"testing"

	"github.com/JakeTRogers/getRelease/internal/github"
)

func TestShouldSkipAsset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{"checksums.sha256", true},
		{"app.sha512", true},
		{"signature.sig", true},
		{"release.asc", true},
		{"metadata.json", true},
		{"notes.txt", true},
		{"app.sbom", true},
		{"app_linux_amd64.tar.gz", false},
		{"app_darwin_arm64.zip", false},
		{"app_windows_amd64.exe", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ShouldSkipAsset(tt.name); got != tt.want {
				t.Errorf("ShouldSkipAsset(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestMatchesExcludePattern(t *testing.T) {
	t.Parallel()

	patterns := []string{"*.deb", "*.rpm", "*.msi"}

	tests := []struct {
		name string
		want bool
	}{
		{"app_amd64.deb", true},
		{"app_amd64.rpm", true},
		{"app_amd64.msi", true},
		{"app_linux_amd64.tar.gz", false},
		{"app.zip", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := MatchesExcludePattern(tt.name, patterns); got != tt.want {
				t.Errorf("MatchesExcludePattern(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestContainsKeyword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		asset    string
		keywords []string
		want     bool
	}{
		{"linux match", "k9s_Linux_amd64.tar.gz", []string{"linux"}, true},
		{"darwin match", "k9s_Darwin_arm64.tar.gz", []string{"darwin"}, true},
		{"windows match", "k9s_Windows_amd64.zip", []string{"windows"}, true},
		{"amd64 match", "k9s_Linux_amd64.tar.gz", []string{"amd64", "x86_64"}, true},
		{"x86_64 match", "k9s_Linux_x86_64.tar.gz", []string{"amd64", "x86_64"}, true},
		{"no match", "k9s_FreeBSD_amd64.tar.gz", []string{"linux"}, false},
		// win should NOT match darwin (word boundary check)
		{"win should not match darwin", "k9s_Darwin_arm64.tar.gz", []string{"win"}, false},
		// "win" does NOT match "Windows" at word boundary (followed by 'd');
		// in practice, OSKeywords sends both "windows" and "win"
		{"win does not match windows", "k9s_Windows_amd64.zip", []string{"win"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ContainsKeyword(tt.asset, tt.keywords); got != tt.want {
				t.Errorf("ContainsKeyword(%q, %v) = %v, want %v", tt.asset, tt.keywords, got, tt.want)
			}
		})
	}
}

func TestFormatScore(t *testing.T) {
	t.Parallel()

	formats := []string{"tar.gz", "zip"}

	tests := []struct {
		name  string
		asset string
		want  int
	}{
		{"tar.gz gets highest", "app.tar.gz", 2},
		{"zip gets second", "app.zip", 1},
		{"unknown gets zero", "app.deb", 0},
		{"raw binary gets zero", "app", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := FormatScore(tt.asset, formats); got != tt.want {
				t.Errorf("FormatScore(%q) = %d, want %d", tt.asset, got, tt.want)
			}
		})
	}
}

func TestMatchAssets(t *testing.T) {
	t.Parallel()

	allAssets := []github.Asset{
		{Name: "app_linux_amd64.tar.gz", DownloadURL: "https://example.com/app_linux_amd64.tar.gz"},
		{Name: "app_linux_amd64.zip", DownloadURL: "https://example.com/app_linux_amd64.zip"},
		{Name: "app_linux_amd64.deb", DownloadURL: "https://example.com/app_linux_amd64.deb"},
		{Name: "app_linux_arm64.tar.gz", DownloadURL: "https://example.com/app_linux_arm64.tar.gz"},
		{Name: "app_darwin_arm64.tar.gz", DownloadURL: "https://example.com/app_darwin_arm64.tar.gz"},
		{Name: "app_windows_amd64.zip", DownloadURL: "https://example.com/app_windows_amd64.zip"},
		{Name: "checksums.sha256", DownloadURL: "https://example.com/checksums.sha256"},
		{Name: "app_freebsd_amd64.tar.gz", DownloadURL: "https://example.com/app_freebsd_amd64.tar.gz"},
	}

	tests := []struct {
		name            string
		osName          string
		arch            string
		formats         []string
		excludePatterns []string
		wantCount       int
		wantFirstName   string
	}{
		{
			name:            "linux amd64 default formats",
			osName:          "linux",
			arch:            "amd64",
			formats:         []string{"tar.gz", "zip"},
			excludePatterns: []string{"*.deb"},
			wantCount:       2,
			wantFirstName:   "app_linux_amd64.tar.gz",
		},
		{
			name:            "linux arm64",
			osName:          "linux",
			arch:            "arm64",
			formats:         []string{"tar.gz"},
			excludePatterns: nil,
			wantCount:       1,
			wantFirstName:   "app_linux_arm64.tar.gz",
		},
		{
			name:            "darwin arm64",
			osName:          "darwin",
			arch:            "arm64",
			formats:         []string{"tar.gz"},
			excludePatterns: nil,
			wantCount:       1,
			wantFirstName:   "app_darwin_arm64.tar.gz",
		},
		{
			name:            "linux amd64 no excludes",
			osName:          "linux",
			arch:            "amd64",
			formats:         []string{"tar.gz", "zip"},
			excludePatterns: nil,
			wantCount:       3,
			wantFirstName:   "app_linux_amd64.tar.gz",
		},
		{
			name:            "no matches freebsd arm64",
			osName:          "freebsd",
			arch:            "arm64",
			formats:         []string{"tar.gz"},
			excludePatterns: nil,
			wantCount:       0,
		},
		{
			name:            "windows amd64",
			osName:          "windows",
			arch:            "amd64",
			formats:         []string{"zip"},
			excludePatterns: nil,
			wantCount:       1,
			wantFirstName:   "app_windows_amd64.zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := MatchAssets(allAssets, tt.osName, tt.arch, tt.formats, tt.excludePatterns)
			if len(got) != tt.wantCount {
				names := make([]string, len(got))
				for i, a := range got {
					names[i] = a.Name
				}
				t.Fatalf("MatchAssets() returned %d assets %v, want %d", len(got), names, tt.wantCount)
			}
			if tt.wantCount > 0 && got[0].Name != tt.wantFirstName {
				t.Errorf("MatchAssets() first asset = %q, want %q", got[0].Name, tt.wantFirstName)
			}
		})
	}
}

func TestMatchAssets_PrefersGNUOverMusl(t *testing.T) {
	t.Parallel()

	assets := []github.Asset{
		{Name: "bat-v0.26.1-x86_64-unknown-linux-musl.tar.gz"},
		{Name: "bat-v0.26.1-x86_64-unknown-linux-gnu.tar.gz"},
	}

	got := MatchAssets(assets, "linux", "amd64", []string{"tar.gz"}, nil)
	if len(got) != 2 {
		t.Fatalf("MatchAssets() returned %d assets, want 2", len(got))
	}
	if got[0].Name != "bat-v0.26.1-x86_64-unknown-linux-gnu.tar.gz" {
		t.Fatalf("MatchAssets() first asset = %q, want GNU asset first", got[0].Name)
	}
}

func TestBestAsset(t *testing.T) {
	t.Parallel()

	t.Run("unique preferred linux libc match", func(t *testing.T) {
		t.Parallel()

		assets := []github.Asset{
			{Name: "bat-v0.26.1-x86_64-unknown-linux-musl.tar.gz"},
			{Name: "bat-v0.26.1-x86_64-unknown-linux-gnu.tar.gz"},
		}

		best, ok := BestAsset(assets, "linux", []string{"tar.gz"})
		if !ok {
			t.Fatal("BestAsset() unique = false, want true")
		}
		if best.Name != "bat-v0.26.1-x86_64-unknown-linux-gnu.tar.gz" {
			t.Fatalf("BestAsset() = %q, want GNU asset", best.Name)
		}
	})

	t.Run("ambiguous equal scores", func(t *testing.T) {
		t.Parallel()

		assets := []github.Asset{
			{Name: "app_linux_amd64.tar.gz"},
			{Name: "tool_linux_amd64.tar.gz"},
		}

		_, ok := BestAsset(assets, "linux", []string{"tar.gz"})
		if ok {
			t.Fatal("BestAsset() unique = true, want false")
		}
	})
}
