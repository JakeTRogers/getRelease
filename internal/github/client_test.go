package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestClient_ListReleases(t *testing.T) {
	t.Parallel()

	releases := []Release{
		{TagName: "v2.0.0", Name: "Release 2", Assets: []Asset{{Name: "binary.tar.gz"}}},
		{TagName: "v1.0.0", Name: "Release 1", Assets: []Asset{{Name: "binary.tar.gz"}}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(releases); err != nil {
			t.Errorf("encode releases: %v", err)
		}
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL)
	got, err := client.ListReleases("owner", "repo", 10)
	if err != nil {
		t.Fatalf("ListReleases() error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("ListReleases() returned %d releases, want 2", len(got))
	}
	if got[0].TagName != "v2.0.0" {
		t.Errorf("ListReleases()[0].TagName = %q, want %q", got[0].TagName, "v2.0.0")
	}
}

func TestClient_GetLatestRelease(t *testing.T) {
	t.Parallel()

	release := Release{
		TagName: "v3.0.0",
		Name:    "Latest",
		Assets:  []Asset{{Name: "app_linux_amd64.tar.gz", Size: 1024}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(release); err != nil {
			t.Errorf("encode latest release: %v", err)
		}
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL)
	got, err := client.GetLatestRelease("owner", "repo")
	if err != nil {
		t.Fatalf("GetLatestRelease() error: %v", err)
	}
	if got.TagName != "v3.0.0" {
		t.Errorf("GetLatestRelease().TagName = %q, want %q", got.TagName, "v3.0.0")
	}
	if len(got.Assets) != 1 {
		t.Errorf("GetLatestRelease() returned %d assets, want 1", len(got.Assets))
	}
}

func TestClient_GetReleaseByTag(t *testing.T) {
	t.Parallel()

	release := Release{TagName: "v1.5.0", Name: "Specific Release"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases/tags/v1.5.0" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(release); err != nil {
			t.Errorf("encode tagged release: %v", err)
		}
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL)
	got, err := client.GetReleaseByTag("owner", "repo", "v1.5.0")
	if err != nil {
		t.Fatalf("GetReleaseByTag() error: %v", err)
	}
	if got.TagName != "v1.5.0" {
		t.Errorf("GetReleaseByTag().TagName = %q, want %q", got.TagName, "v1.5.0")
	}
}

func TestClient_GetReleaseByTag_EscapesSlashInTag(t *testing.T) {
	t.Parallel()

	release := Release{TagName: "release/2026-03"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/repos/owner/repo/releases/tags/release%2F2026-03" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(release); err != nil {
			t.Errorf("encode escaped tag release: %v", err)
		}
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL)
	got, err := client.GetReleaseByTag("owner", "repo", "release/2026-03")
	if err != nil {
		t.Fatalf("GetReleaseByTag() error: %v", err)
	}
	if got.TagName != release.TagName {
		t.Fatalf("GetReleaseByTag().TagName = %q, want %q", got.TagName, release.TagName)
	}
}

func TestClient_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL)
	_, err := client.GetLatestRelease("owner", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent repo")
	}

	var nf *NotFoundError
	if !isNotFoundError(err, &nf) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestClient_RateLimit(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1700000000")
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL)
	_, err := client.GetLatestRelease("owner", "repo")
	if err == nil {
		t.Fatal("expected error for rate limit")
	}

	var rl *RateLimitError
	if !isRateLimitError(err, &rl) {
		t.Errorf("expected RateLimitError, got %T: %v", err, err)
	}
}

func TestRelease_DisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		release Release
		want    string
	}{
		{name: "has name", release: Release{TagName: "v1.0.0", Name: "Release 1"}, want: "Release 1"},
		{name: "empty name", release: Release{TagName: "v1.0.0", Name: ""}, want: "v1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.release.DisplayName(); got != tt.want {
				t.Errorf("DisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	t.Parallel()
	c := NewClient()
	if c == nil {
		t.Fatal("NewClient() returned nil")
	}
	if c.baseURL != defaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, defaultBaseURL)
	}
	if c.httpClient == nil {
		t.Error("httpClient is nil")
	}
}

func TestClient_DownloadAsset(t *testing.T) {
	t.Parallel()

	content := "binary-content-here"

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte(content)); err != nil {
				t.Errorf("write download response: %v", err)
			}
		}))
		defer srv.Close()

		client := NewClientWithHTTP(srv.Client(), srv.URL)
		dest := filepath.Join(t.TempDir(), "asset.tar.gz")
		n, err := client.DownloadAsset(srv.URL+"/download/asset.tar.gz", dest)
		if err != nil {
			t.Fatalf("DownloadAsset() error: %v", err)
		}
		if n != int64(len(content)) {
			t.Errorf("DownloadAsset() wrote %d bytes, want %d", n, len(content))
		}
		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("read downloaded file: %v", err)
		}
		if string(got) != content {
			t.Errorf("downloaded content = %q, want %q", string(got), content)
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		client := NewClientWithHTTP(srv.Client(), srv.URL)
		dest := filepath.Join(t.TempDir(), "missing")
		_, err := client.DownloadAsset(srv.URL+"/download/missing", dest)
		if err == nil {
			t.Fatal("expected error for 404")
		}
	})
}

func TestClient_GetReleaseByTag_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL)
	_, err := client.GetReleaseByTag("owner", "repo", "v999")
	if err == nil {
		t.Fatal("expected error for not found tag")
	}
}

func TestClient_ListReleases_DefaultLimit(t *testing.T) {
	t.Parallel()

	releases := make([]Release, 5)
	for i := range releases {
		releases[i] = Release{TagName: "v" + string(rune('1'+i))}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(releases); err != nil {
			t.Errorf("encode default-limit releases: %v", err)
		}
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL)
	got, err := client.ListReleases("owner", "repo", 0) // limit=0 → default 30
	if err != nil {
		t.Fatalf("ListReleases() error: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("ListReleases() returned %d releases, want 5", len(got))
	}
}

func TestClient_ListReleases_Truncate(t *testing.T) {
	t.Parallel()

	releases := make([]Release, 5)
	for i := range releases {
		releases[i] = Release{TagName: "v" + string(rune('1'+i))}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(releases); err != nil {
			t.Errorf("encode truncated releases: %v", err)
		}
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL)
	got, err := client.ListReleases("owner", "repo", 2) // limit=2 → truncate
	if err != nil {
		t.Fatalf("ListReleases() error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("ListReleases() returned %d releases, want 2", len(got))
	}
}

func TestClient_Forbidden_NonRateLimit(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "10")
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL)
	_, err := client.GetLatestRelease("owner", "repo")
	if err == nil {
		t.Fatal("expected error for forbidden")
	}
	// Should NOT be a RateLimitError since remaining > 0
	var rl *RateLimitError
	if isRateLimitError(err, &rl) {
		t.Error("should not be RateLimitError when remaining > 0")
	}
}

func TestClient_UnexpectedStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL)
	_, err := client.GetLatestRelease("owner", "repo")
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

// helpers to unwrap errors for type assertion
func isNotFoundError(err error, target **NotFoundError) bool {
	for err != nil {
		if nf, ok := err.(*NotFoundError); ok {
			*target = nf
			return true
		}
		if unwrapper, ok := err.(interface{ Unwrap() error }); ok {
			err = unwrapper.Unwrap()
		} else {
			return false
		}
	}
	return false
}

func isRateLimitError(err error, target **RateLimitError) bool {
	for err != nil {
		if rl, ok := err.(*RateLimitError); ok {
			*target = rl
			return true
		}
		if unwrapper, ok := err.(interface{ Unwrap() error }); ok {
			err = unwrapper.Unwrap()
		} else {
			return false
		}
	}
	return false
}
