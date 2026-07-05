package github

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
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

	// Raw JSON with GitHub's field names, so the struct tags are what's
	// actually under test (a struct fixture would round-trip even with
	// wrong tags).
	body := `{
		"tag_name": "v3.0.0",
		"name": "Latest",
		"published_at": "2026-05-01T10:00:00Z",
		"assets": [{
			"name": "app_linux_amd64.tar.gz",
			"size": 1024,
			"created_at": "2026-05-01T10:05:00Z",
			"updated_at": "2026-06-15T08:30:00Z"
		}]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write latest release: %v", err)
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
		t.Fatalf("GetLatestRelease() returned %d assets, want 1", len(got.Assets))
	}
	if got.PublishedAt.IsZero() {
		t.Error("GetLatestRelease().PublishedAt is zero, want decoded timestamp")
	}
	asset := got.Assets[0]
	if want := time.Date(2026, time.May, 1, 10, 5, 0, 0, time.UTC); !asset.CreatedAt.Equal(want) {
		t.Errorf("asset CreatedAt = %v, want %v", asset.CreatedAt, want)
	}
	if want := time.Date(2026, time.June, 15, 8, 30, 0, 0, time.UTC); !asset.UpdatedAt.Equal(want) {
		t.Errorf("asset UpdatedAt = %v, want %v", asset.UpdatedAt, want)
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

func TestNewClientForHost(t *testing.T) {
	t.Parallel()

	t.Run("github.com uses default base URL", func(t *testing.T) {
		t.Parallel()
		for _, host := range []string{"", "github.com"} {
			c := NewClientForHost(host)
			if c.baseURL != defaultBaseURL {
				t.Errorf("NewClientForHost(%q).baseURL = %q, want %q", host, c.baseURL, defaultBaseURL)
			}
		}
	})

	t.Run("enterprise host uses api.<host> base URL", func(t *testing.T) {
		t.Parallel()
		c := NewClientForHost("acme.ghe.com")
		want := "https://api.acme.ghe.com"
		if c.baseURL != want {
			t.Errorf("NewClientForHost(\"acme.ghe.com\").baseURL = %q, want %q", c.baseURL, want)
		}
		if c.webHost != "acme.ghe.com" {
			t.Errorf("NewClientForHost(\"acme.ghe.com\").webHost = %q, want %q", c.webHost, "acme.ghe.com")
		}
	})
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
		n, err := client.DownloadAsset(Asset{DownloadURL: srv.URL + "/download/asset.tar.gz"}, dest)
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
		_, err := client.DownloadAsset(Asset{DownloadURL: srv.URL + "/download/missing"}, dest)
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

func TestClient_AuthorizationHeader(t *testing.T) {
	t.Parallel()

	release := Release{TagName: "v1.0.0"}

	newServer := func(t *testing.T, gotAuth *string) *httptest.Server {
		t.Helper()
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(release); err != nil {
				t.Errorf("encode release: %v", err)
			}
		}))
	}

	t.Run("token sent on API requests", func(t *testing.T) {
		t.Parallel()
		var gotAuth string
		srv := newServer(t, &gotAuth)
		defer srv.Close()

		client := NewClientWithHTTP(srv.Client(), srv.URL).WithToken("test-token")
		if _, err := client.GetLatestRelease("owner", "repo"); err != nil {
			t.Fatalf("GetLatestRelease() error: %v", err)
		}
		if gotAuth != "Bearer test-token" {
			t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-token")
		}
	})

	t.Run("no header without token", func(t *testing.T) {
		t.Parallel()
		var gotAuth string
		srv := newServer(t, &gotAuth)
		defer srv.Close()

		client := NewClientWithHTTP(srv.Client(), srv.URL)
		if _, err := client.GetLatestRelease("owner", "repo"); err != nil {
			t.Fatalf("GetLatestRelease() error: %v", err)
		}
		if gotAuth != "" {
			t.Errorf("Authorization header = %q, want empty", gotAuth)
		}
	})

	t.Run("no header on asset downloads from non-github hosts", func(t *testing.T) {
		t.Parallel()
		var gotAuth string
		srv := newServer(t, &gotAuth)
		defer srv.Close()

		client := NewClientWithHTTP(srv.Client(), srv.URL).WithToken("test-token")
		dest := filepath.Join(t.TempDir(), "asset")
		if _, err := client.DownloadAsset(Asset{DownloadURL: srv.URL + "/download/asset"}, dest); err != nil {
			t.Fatalf("DownloadAsset() error: %v", err)
		}
		if gotAuth != "" {
			t.Errorf("Authorization header = %q, want empty on downloads", gotAuth)
		}
	})

	t.Run("token sent to github.com but stripped after cross-host redirect", func(t *testing.T) {
		t.Parallel()

		var githubAuth, cdnAuth string
		cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cdnAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte("asset-bytes"))
		}))
		defer cdn.Close()

		gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			githubAuth = r.Header.Get("Authorization")
			http.Redirect(w, r, "http://objects.githubusercontent.com/asset", http.StatusFound)
		}))
		defer gh.Close()

		// Route requests for github.com and objects.githubusercontent.com to
		// the local test servers so we can exercise real cross-host redirect
		// handling without touching the network.
		dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			switch host {
			case "github.com":
				return net.Dial(network, gh.Listener.Addr().String())
			case "objects.githubusercontent.com":
				return net.Dial(network, cdn.Listener.Addr().String())
			default:
				return net.Dial(network, addr)
			}
		}
		httpClient := &http.Client{Transport: &http.Transport{DialContext: dial}}

		client := NewClientWithHTTP(httpClient, "").WithToken("test-token")
		dest := filepath.Join(t.TempDir(), "asset")
		if _, err := client.DownloadAsset(Asset{DownloadURL: "http://github.com/owner/repo/releases/download/v1/asset"}, dest); err != nil {
			t.Fatalf("DownloadAsset() error: %v", err)
		}
		if githubAuth != "Bearer test-token" {
			t.Errorf("github.com Authorization header = %q, want %q", githubAuth, "Bearer test-token")
		}
		if cdnAuth != "" {
			t.Errorf("CDN Authorization header = %q, want empty after cross-host redirect", cdnAuth)
		}
	})

	t.Run("authenticated download prefers API asset URL", func(t *testing.T) {
		t.Parallel()

		var apiAuth string
		var apiHits, webHits int
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiHits++
			apiAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte("asset-bytes"))
		}))
		defer api.Close()

		web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			webHits++
			_, _ = w.Write([]byte("asset-bytes"))
		}))
		defer web.Close()

		dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			switch host {
			case "api.github.com":
				return net.Dial(network, api.Listener.Addr().String())
			case "github.com":
				return net.Dial(network, web.Listener.Addr().String())
			default:
				return net.Dial(network, addr)
			}
		}
		httpClient := &http.Client{Transport: &http.Transport{DialContext: dial}}

		client := NewClientWithHTTP(httpClient, "").WithToken("test-token")
		dest := filepath.Join(t.TempDir(), "asset")
		asset := Asset{
			DownloadURL: "http://github.com/owner/repo/releases/download/v1/asset",
			APIURL:      "http://api.github.com/repos/owner/repo/releases/assets/1",
		}
		if _, err := client.DownloadAsset(asset, dest); err != nil {
			t.Fatalf("DownloadAsset() error: %v", err)
		}
		if apiHits != 1 || webHits != 0 {
			t.Errorf("hits = api %d, web %d; want api 1, web 0", apiHits, webHits)
		}
		if apiAuth != "Bearer test-token" {
			t.Errorf("API Authorization header = %q, want %q", apiAuth, "Bearer test-token")
		}
	})

	t.Run("anonymous download uses browser URL without auth", func(t *testing.T) {
		t.Parallel()
		var gotAuth string
		srv := newServer(t, &gotAuth)
		defer srv.Close()

		client := NewClientWithHTTP(srv.Client(), srv.URL)
		dest := filepath.Join(t.TempDir(), "asset")
		asset := Asset{
			DownloadURL: srv.URL + "/download/asset",
			APIURL:      srv.URL + "/repos/owner/repo/releases/assets/1",
		}
		if _, err := client.DownloadAsset(asset, dest); err != nil {
			t.Fatalf("DownloadAsset() error: %v", err)
		}
		if gotAuth != "" {
			t.Errorf("Authorization header = %q, want empty for anonymous download", gotAuth)
		}
	})
}

func TestClient_IsTrustedDownloadHost(t *testing.T) {
	t.Parallel()

	t.Run("default client trusts github.com", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			url  string
			want bool
		}{
			{"github.com", "https://github.com/owner/repo/releases/download/v1/asset", true},
			{"www.github.com", "https://www.github.com/owner/repo/releases/download/v1/asset", true},
			{"case insensitive", "https://GitHub.Com/owner/repo/releases/download/v1/asset", true},
			{"api host", "https://api.github.com/repos/owner/repo/releases/assets/1", true},
			{"cdn host", "https://objects.githubusercontent.com/asset", false},
			{"enterprise host", "https://acme.ghe.com/owner/repo/releases/download/v1/asset", false},
			{"enterprise api host", "https://api.acme.ghe.com/repos/owner/repo/releases/assets/1", false},
			{"invalid url", "://not-a-url", false},
		}

		client := NewClient()
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := client.isTrustedDownloadHost(tt.url); got != tt.want {
					t.Errorf("isTrustedDownloadHost(%q) = %v, want %v", tt.url, got, tt.want)
				}
			})
		}
	})

	t.Run("enterprise client trusts only its own host", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			url  string
			want bool
		}{
			{"matching enterprise host", "https://acme.ghe.com/owner/repo/releases/download/v1/asset", true},
			{"matching enterprise api host", "https://api.acme.ghe.com/repos/owner/repo/releases/assets/1", true},
			{"case insensitive", "https://Acme.Ghe.Com/owner/repo/releases/download/v1/asset", true},
			{"github.com not trusted", "https://github.com/owner/repo/releases/download/v1/asset", false},
			{"api.github.com not trusted", "https://api.github.com/repos/owner/repo/releases/assets/1", false},
			{"different enterprise host", "https://other.ghe.com/owner/repo/releases/download/v1/asset", false},
		}

		client := NewClientForHost("acme.ghe.com")
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := client.isTrustedDownloadHost(tt.url); got != tt.want {
					t.Errorf("isTrustedDownloadHost(%q) = %v, want %v", tt.url, got, tt.want)
				}
			})
		}
	})
}
