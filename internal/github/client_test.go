package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
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

func TestClient_RateLimitError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		status        int
		headers       map[string]string
		body          string
		want          bool
		wantSecondary bool
		wantResetAt   time.Time
	}{
		{
			name: "429 with Retry-After", status: http.StatusTooManyRequests,
			headers: map[string]string{"Retry-After": "30"},
			want:    true, wantSecondary: true, wantResetAt: now.Add(30 * time.Second),
		},
		{
			name: "403 with Retry-After", status: http.StatusForbidden,
			headers: map[string]string{"Retry-After": "60"},
			want:    true, wantSecondary: true, wantResetAt: now.Add(time.Minute),
		},
		{
			name: "429 with exhausted primary limit", status: http.StatusTooManyRequests,
			headers: map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1700000000"},
			want:    true, wantResetAt: time.Unix(1700000000, 0),
		},
		{
			name: "429 without headers waits a minute", status: http.StatusTooManyRequests,
			want: true, wantSecondary: true, wantResetAt: now.Add(time.Minute),
		},
		{
			name: "403 secondary limit message", status: http.StatusForbidden,
			headers: map[string]string{"X-RateLimit-Remaining": "4000"},
			body:    `{"message":"You have exceeded a secondary rate limit."}`,
			want:    true, wantSecondary: true, wantResetAt: now.Add(time.Minute),
		},
		{
			name: "403 forbidden is not a rate limit", status: http.StatusForbidden,
			headers: map[string]string{"X-RateLimit-Remaining": "10"},
			body:    `{"message":"Resource protected by organization SAML enforcement."}`,
		},
	}

	client := NewClient()
	for _, tt := range tests {
		resp := &http.Response{StatusCode: tt.status, Header: http.Header{}}
		for k, v := range tt.headers {
			resp.Header.Set(k, v)
		}
		got := client.rateLimitError(resp, []byte(tt.body), now)
		if (got != nil) != tt.want {
			t.Errorf("%s: rateLimitError() = %v, want rate limit %v", tt.name, got, tt.want)
			continue
		}
		if got == nil {
			continue
		}
		if got.Secondary != tt.wantSecondary || !got.ResetAt.Equal(tt.wantResetAt) {
			t.Errorf("%s: rateLimitError() = %+v, want Secondary=%v ResetAt=%v", tt.name, got, tt.wantSecondary, tt.wantResetAt)
		}
	}
}

func TestRateLimitError_Message(t *testing.T) {
	t.Parallel()

	resetAt := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		err          RateLimitError
		wantContains string
		wantHint     bool
	}{
		{name: "anonymous primary", err: RateLimitError{ResetAt: resetAt, Anonymous: true}, wantContains: "rate limit exceeded; resets at", wantHint: true},
		{name: "authenticated primary", err: RateLimitError{ResetAt: resetAt}, wantContains: "rate limit exceeded; resets at"},
		{name: "secondary", err: RateLimitError{ResetAt: resetAt, Secondary: true, Anonymous: true}, wantContains: "secondary rate limit exceeded; retry after"},
	}
	for _, tt := range tests {
		msg := tt.err.Error()
		if !strings.Contains(msg, tt.wantContains) {
			t.Errorf("%s: Error() = %q, want it to contain %q", tt.name, msg, tt.wantContains)
		}
		if hint := strings.Contains(msg, "gh auth login"); hint != tt.wantHint {
			t.Errorf("%s: Error() = %q, authentication hint present = %v, want %v", tt.name, msg, hint, tt.wantHint)
		}
	}
}

func TestClient_TooManyRequests(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "too many requests", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL)
	var rl *RateLimitError

	_, err := client.GetLatestRelease("owner", "repo")
	if !isRateLimitError(err, &rl) || !rl.Secondary {
		t.Errorf("GetLatestRelease() error = %v, want secondary RateLimitError", err)
	}

	_, err = client.DownloadAsset(Asset{DownloadURL: srv.URL + "/download/asset"}, filepath.Join(t.TempDir(), "asset"))
	if !isRateLimitError(err, &rl) || !rl.Secondary {
		t.Errorf("DownloadAsset() error = %v, want secondary RateLimitError", err)
	}
}

func TestClient_DownloadAsset_SecondaryRateLimitBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "4000")
		http.Error(w, `{"message":"You have exceeded a secondary rate limit."}`, http.StatusForbidden)
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL)
	dest := filepath.Join(t.TempDir(), "asset")
	before := time.Now()
	n, err := client.DownloadAsset(Asset{DownloadURL: srv.URL + "/download/asset"}, dest)
	after := time.Now()

	var rateErr *RateLimitError
	if !errors.As(err, &rateErr) || !rateErr.Secondary || !rateErr.Anonymous {
		t.Fatalf("DownloadAsset() error = %v, want anonymous secondary RateLimitError", err)
	}
	if rateErr.ResetAt.Before(before.Add(time.Minute)) || rateErr.ResetAt.After(after.Add(time.Minute)) {
		t.Errorf("ResetAt = %v, want one minute after the response", rateErr.ResetAt)
	}
	if !strings.Contains(err.Error(), "secondary rate limit exceeded; retry after") {
		t.Errorf("DownloadAsset() error = %v, want secondary-limit retry guidance", err)
	}
	if n != 0 {
		t.Errorf("DownloadAsset() wrote %d bytes, want 0", n)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Errorf("failed download created %s (stat err: %v)", dest, statErr)
	}
}

func TestClient_DownloadAsset_ErrorBody(t *testing.T) {
	t.Parallel()

	const bodyLimit = 64 << 10
	tests := []struct {
		name          string
		status        int
		headers       map[string]string
		body          string
		readError     bool
		wantRead      int
		wantRateLimit bool
		wantSecondary bool
		wantReadError bool
	}{
		{
			name: "body read is bounded", status: http.StatusForbidden,
			body: strings.Repeat("x", bodyLimit) + "secondary rate limit", wantRead: bodyLimit,
		},
		{
			name: "ordinary forbidden is not a rate limit", status: http.StatusForbidden,
			body:     `{"message":"Resource protected by organization SAML enforcement."}`,
			wantRead: len(`{"message":"Resource protected by organization SAML enforcement."}`),
		},
		{
			name: "body read failure is reported", status: http.StatusForbidden,
			readError: true, wantReadError: true,
		},
		{
			name: "primary limit does not depend on reading body", status: http.StatusForbidden,
			headers:   map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1700000000"},
			readError: true, wantRateLimit: true,
		},
		{
			name: "retry after does not depend on reading body", status: http.StatusForbidden,
			headers:   map[string]string{"Retry-After": "5"},
			readError: true, wantRateLimit: true, wantSecondary: true,
		},
		{
			name: "429 does not depend on reading body", status: http.StatusTooManyRequests,
			readError: true, wantRateLimit: true, wantSecondary: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := strings.NewReader(tt.body)
			var body io.Reader = reader
			if tt.readError {
				body = iotest.ErrReader(io.ErrUnexpectedEOF)
			}
			headers := http.Header{}
			for key, value := range tt.headers {
				headers.Set(key, value)
			}
			client := NewClientWithHTTP(&http.Client{
				Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: tt.status, Header: headers, Body: io.NopCloser(body)}, nil
				}),
			}, "")
			dest := filepath.Join(t.TempDir(), "asset")
			n, err := client.DownloadAsset(Asset{DownloadURL: "https://example.invalid/asset"}, dest)
			if n != 0 || err == nil {
				t.Fatalf("DownloadAsset() = %d, %v, want 0 and an error", n, err)
			}
			var rateErr *RateLimitError
			if got := errors.As(err, &rateErr); got != tt.wantRateLimit {
				t.Fatalf("DownloadAsset() error = %v, rate limit = %v, want %v", err, got, tt.wantRateLimit)
			}
			if rateErr != nil && rateErr.Secondary != tt.wantSecondary {
				t.Errorf("Secondary = %v, want %v", rateErr.Secondary, tt.wantSecondary)
			}
			if got := errors.Is(err, io.ErrUnexpectedEOF); got != tt.wantReadError {
				t.Errorf("DownloadAsset() error = %v, read error = %v, want %v", err, got, tt.wantReadError)
			}
			if !tt.wantRateLimit && !tt.wantReadError && err.Error() != "download returned status 403" {
				t.Errorf("DownloadAsset() error = %v, want generic forbidden status", err)
			}
			if got := len(tt.body) - reader.Len(); got != tt.wantRead {
				t.Errorf("read %d error-body bytes, want %d", got, tt.wantRead)
			}
			if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
				t.Errorf("failed download created %s (stat err: %v)", dest, statErr)
			}
		})
	}
}

func TestClient_NotFoundExplanations(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	encode := func(w http.ResponseWriter, v any) {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}
	// o/missing does not exist, so every path under it 404s.
	mux.HandleFunc("/repos/o/empty/releases", func(w http.ResponseWriter, _ *http.Request) {
		encode(w, []Release{})
	})
	mux.HandleFunc("/repos/o/pre/releases", func(w http.ResponseWriter, _ *http.Request) {
		encode(w, []Release{{TagName: "v2.0.0-draft", Draft: true}, {TagName: "v2.0.0-rc.1", Prerelease: true}})
	})
	mux.HandleFunc("/repos/o/real/releases", func(w http.ResponseWriter, _ *http.Request) {
		encode(w, []Release{{TagName: "v0.24.0"}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	anonymous := NewClientWithHTTP(srv.Client(), srv.URL)
	authenticated := NewClientWithHTTP(srv.Client(), srv.URL).WithToken("token")

	tests := []struct {
		name         string
		call         func() error
		want         string
		wantNotFound bool
	}{
		{
			name: "missing repository, anonymous",
			call: func() error { _, err := anonymous.GetLatestRelease("o", "missing"); return err },
			want: "repository o/missing not found; if it is private, set GETRELEASE_TOKEN", wantNotFound: true,
		},
		{
			name: "missing repository, authenticated",
			call: func() error { _, err := authenticated.GetLatestRelease("o", "missing"); return err },
			want: "repository o/missing not found, or the configured token cannot access it", wantNotFound: true,
		},
		{
			name: "no releases",
			call: func() error { _, err := anonymous.GetLatestRelease("o", "empty"); return err },
			want: "o/empty has no published releases",
		},
		{
			name: "only prereleases",
			call: func() error { _, err := anonymous.GetLatestRelease("o", "pre"); return err },
			want: "o/pre has no stable release, only prereleases (newest: v2.0.0-rc.1)",
		},
		{
			name: "missing tag",
			call: func() error { _, err := anonymous.GetReleaseByTag("o", "real", "0.24.0"); return err },
			want: "release 0.24.0 in o/real not found", wantNotFound: true,
		},
		{
			name: "tag in missing repository",
			call: func() error { _, err := anonymous.GetReleaseByTag("o", "missing", "v1.0.0"); return err },
			want: "repository o/missing not found", wantNotFound: true,
		},
		{
			name: "list missing repository",
			call: func() error { _, err := anonymous.ListReleases("o", "missing", 30); return err },
			want: "repository o/missing not found", wantNotFound: true,
		},
	}
	for _, tt := range tests {
		err := tt.call()
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error = %v, want it to contain %q", tt.name, err, tt.want)
			continue
		}
		if strings.Contains(err.Error(), "/repos/") {
			t.Errorf("%s: error = %q, want no raw API path", tt.name, err)
		}
		var nf *NotFoundError
		if got := errors.As(err, &nf); got != tt.wantNotFound {
			t.Errorf("%s: errors.As(*NotFoundError) = %v, want %v", tt.name, got, tt.wantNotFound)
		}
	}
}

func TestAlternateTag(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"v1.2.3":     "1.2.3",
		"V1.2.3":     "1.2.3",
		"1.2.3":      "v1.2.3",
		"2026-09-21": "v2026-09-21",
		"nightly":    "",
		"v":          "",
		"version-1":  "",
		"":           "",
	}
	for tag, want := range tests {
		if got := alternateTag(tag); got != want {
			t.Errorf("alternateTag(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestClient_GetReleaseByTagTriesAlternateSpelling(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	for _, tag := range []string{"v0.24.0", "1.2.3"} {
		mux.HandleFunc("/repos/o/r/releases/tags/"+tag, func(w http.ResponseWriter, _ *http.Request) {
			if err := json.NewEncoder(w).Encode(Release{TagName: tag}); err != nil {
				t.Errorf("encode release: %v", err)
			}
		})
	}
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode([]Release{{TagName: "v0.24.0"}}); err != nil {
			t.Errorf("encode releases: %v", err)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := NewClientWithHTTP(srv.Client(), srv.URL)

	for requested, want := range map[string]string{"0.24.0": "v0.24.0", "v1.2.3": "1.2.3", "v0.24.0": "v0.24.0"} {
		got, err := client.GetReleaseByTag("o", "r", requested)
		if err != nil {
			t.Errorf("GetReleaseByTag(%q) error: %v", requested, err)
			continue
		}
		if got.TagName != want {
			t.Errorf("GetReleaseByTag(%q) = %q, want %q", requested, got.TagName, want)
		}
	}

	_, err := client.GetReleaseByTag("o", "r", "2.0.0")
	if err == nil || err.Error() != "release 2.0.0 in o/r not found (also tried v2.0.0)" {
		t.Errorf("GetReleaseByTag(2.0.0) error = %v, want both spellings reported", err)
	}
	_, err = client.GetReleaseByTag("o", "r", "nightly")
	if err == nil || err.Error() != "release nightly in o/r not found" {
		t.Errorf("GetReleaseByTag(nightly) error = %v, want no alternate spelling", err)
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

func TestClient_MissingLatest_Paginates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		drafts    int
		published bool
		older     int
		wantPages []string
	}{
		{name: "prerelease after ten drafts", drafts: 10, published: true, wantPages: []string{"1"}},
		{name: "prerelease on third page", drafts: 200, published: true, wantPages: []string{"1", "2", "3"}},
		{name: "stop at first published release", drafts: 100, published: true, older: 199, wantPages: []string{"1", "2"}},
		{name: "no releases", wantPages: []string{"1"}},
		{name: "short draft page", drafts: 10, wantPages: []string{"1"}},
		{name: "drafts on multiple pages", drafts: 125, wantPages: []string{"1", "2"}},
		{name: "full draft pages then empty page", drafts: 200, wantPages: []string{"1", "2", "3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			count := tt.drafts + tt.older
			if tt.published {
				count++
			}
			all := make([]Release, count)
			for i := range all {
				all[i] = Release{TagName: fmt.Sprintf("draft-%d", i), Draft: true}
			}
			want := "owner/repo has no published releases"
			if tt.published {
				all[tt.drafts] = Release{TagName: "v2.0.0-rc.1", Prerelease: true}
				want = "owner/repo has no stable release, only prereleases (newest: v2.0.0-rc.1); request a prerelease by its tag to install it"
			}

			var mu sync.Mutex
			var pages []string
			mux := http.NewServeMux()
			mux.HandleFunc("/repos/owner/repo/releases/latest", http.NotFound)
			mux.HandleFunc("/repos/owner/repo/releases", func(w http.ResponseWriter, r *http.Request) {
				perPage, err := strconv.Atoi(r.URL.Query().Get("per_page"))
				if err != nil || perPage != maxReleasesPerPage {
					t.Errorf("per_page = %q, want %d", r.URL.Query().Get("per_page"), maxReleasesPerPage)
					http.Error(w, "invalid page size", http.StatusBadRequest)
					return
				}
				page, err := strconv.Atoi(r.URL.Query().Get("page"))
				if err != nil || page < 1 {
					t.Errorf("invalid page %q", r.URL.Query().Get("page"))
					http.Error(w, "invalid page", http.StatusBadRequest)
					return
				}
				mu.Lock()
				pages = append(pages, r.URL.Query().Get("page"))
				mu.Unlock()
				start := min((page-1)*perPage, len(all))
				end := min(start+perPage, len(all))
				if err := json.NewEncoder(w).Encode(all[start:end]); err != nil {
					t.Errorf("encode page: %v", err)
				}
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			client := NewClientWithHTTP(srv.Client(), srv.URL).WithToken("token")
			got, err := client.GetLatestRelease("owner", "repo")
			if err == nil || err.Error() != want {
				t.Errorf("GetLatestRelease() error = %v, want %q", err, want)
			}
			if got != nil {
				t.Errorf("GetLatestRelease() = %+v, want nil", got)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(pages, tt.wantPages) {
				t.Errorf("requested pages %v, want %v", pages, tt.wantPages)
			}
		})
	}
}

func TestClient_MissingLatest_LaterPageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		status        int
		body          string
		want          string
		wantNotFound  bool
		wantRateLimit bool
	}{
		{name: "repository not found", status: http.StatusNotFound, want: "repository owner/repo not found", wantNotFound: true},
		{name: "server error", status: http.StatusInternalServerError, body: "server error", want: "listing releases for owner/repo: unexpected status 500"},
		{name: "rate limit", status: http.StatusTooManyRequests, want: "rate limit exceeded", wantRateLimit: true},
		{name: "invalid JSON", status: http.StatusOK, body: "[", want: "decoding releases:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			drafts := make([]Release, maxReleasesPerPage)
			for i := range drafts {
				drafts[i].Draft = true
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/repos/owner/repo/releases/latest", http.NotFound)
			mux.HandleFunc("/repos/owner/repo/releases", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("page") == "1" {
					if err := json.NewEncoder(w).Encode(drafts); err != nil {
						t.Errorf("encode drafts: %v", err)
					}
					return
				}
				if r.URL.Query().Get("page") != "2" {
					t.Errorf("page = %q, want 2", r.URL.Query().Get("page"))
				}
				if tt.wantRateLimit {
					w.Header().Set("Retry-After", "10")
				}
				w.WriteHeader(tt.status)
				if _, err := io.WriteString(w, tt.body); err != nil {
					t.Errorf("write error response: %v", err)
				}
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			client := NewClientWithHTTP(srv.Client(), srv.URL).WithToken("token")
			_, err := client.GetLatestRelease("owner", "repo")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("GetLatestRelease() error = %v, want it to contain %q", err, tt.want)
			}
			var nf *NotFoundError
			if got := errors.As(err, &nf); got != tt.wantNotFound {
				t.Errorf("errors.As(*NotFoundError) = %v, want %v", got, tt.wantNotFound)
			}
			var rl *RateLimitError
			if got := errors.As(err, &rl); got != tt.wantRateLimit {
				t.Errorf("errors.As(*RateLimitError) = %v, want %v", got, tt.wantRateLimit)
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

func TestClient_DownloadAsset_Timeouts(t *testing.T) {
	t.Parallel()

	// waitForClient blocks a handler until the client gives up, bounded so a
	// regression cannot hang the test server's Close.
	waitForClient := func(r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}

	t.Run("slow but progressing download outlives the client timeout", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for range 10 {
				if _, err := w.Write([]byte("chunk")); err != nil {
					return
				}
				w.(http.Flusher).Flush()
				time.Sleep(30 * time.Millisecond)
			}
		}))
		defer srv.Close()

		httpClient := srv.Client()
		httpClient.Timeout = 100 * time.Millisecond
		client := NewClientWithHTTP(httpClient, srv.URL)
		client.downloadStallTimeout = time.Second

		dest := filepath.Join(t.TempDir(), "asset")
		n, err := client.DownloadAsset(Asset{DownloadURL: srv.URL + "/slow"}, dest)
		if err != nil {
			t.Fatalf("DownloadAsset() error: %v", err)
		}
		if n != 50 {
			t.Errorf("DownloadAsset() wrote %d bytes, want 50", n)
		}
	})

	t.Run("stalled body aborts and removes the partial file", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := w.Write([]byte("partial")); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			waitForClient(r)
		}))
		defer srv.Close()

		client := NewClientWithHTTP(srv.Client(), srv.URL)
		client.downloadStallTimeout = 100 * time.Millisecond

		dest := filepath.Join(t.TempDir(), "asset")
		_, err := client.DownloadAsset(Asset{DownloadURL: srv.URL + "/stall"}, dest)
		if err == nil || !strings.Contains(err.Error(), "download stalled") {
			t.Fatalf("DownloadAsset() error = %v, want download stalled", err)
		}
		if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
			t.Errorf("partial download left at %s (stat err: %v)", dest, statErr)
		}
	})

	t.Run("stalled error body aborts without creating a file", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			if _, err := w.Write([]byte("partial")); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			waitForClient(r)
		}))
		defer srv.Close()

		client := NewClientWithHTTP(srv.Client(), srv.URL)
		client.downloadStallTimeout = 100 * time.Millisecond

		dest := filepath.Join(t.TempDir(), "asset")
		_, err := client.DownloadAsset(Asset{DownloadURL: srv.URL + "/stall"}, dest)
		if !errors.Is(err, errDownloadStalled) {
			t.Fatalf("DownloadAsset() error = %v, want download stalled", err)
		}
		if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
			t.Errorf("failed download created %s (stat err: %v)", dest, statErr)
		}
	})

	t.Run("stall before response headers aborts", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			waitForClient(r)
		}))
		defer srv.Close()

		client := NewClientWithHTTP(srv.Client(), srv.URL)
		client.downloadStallTimeout = 100 * time.Millisecond

		_, err := client.DownloadAsset(Asset{DownloadURL: srv.URL + "/hang"}, filepath.Join(t.TempDir(), "asset"))
		if err == nil || !strings.Contains(err.Error(), "download stalled") {
			t.Fatalf("DownloadAsset() error = %v, want download stalled", err)
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

func TestClient_ListReleases_Paginates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		available int
		limit     int
		wantCount int
		wantPages []string
	}{
		{name: "limit spans three pages", available: 300, limit: 250, wantCount: 250, wantPages: []string{"1", "2", "3"}},
		{name: "repository runs out first", available: 130, limit: 250, wantCount: 130, wantPages: []string{"1", "2"}},
		{name: "limit within one page", available: 300, limit: 100, wantCount: 100, wantPages: []string{"1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			all := make([]Release, tt.available)
			for i := range all {
				all[i] = Release{TagName: fmt.Sprintf("v1.0.%d", tt.available-i)}
			}
			var mu sync.Mutex
			var pages []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				mu.Lock()
				pages = append(pages, r.URL.Query().Get("page"))
				mu.Unlock()
				if perPage != 100 {
					t.Errorf("per_page = %d, want 100", perPage)
				}
				start := min((page-1)*perPage, len(all))
				end := min(start+perPage, len(all))
				if err := json.NewEncoder(w).Encode(all[start:end]); err != nil {
					t.Errorf("encode page: %v", err)
				}
			}))
			defer srv.Close()

			client := NewClientWithHTTP(srv.Client(), srv.URL)
			got, err := client.ListReleases("owner", "repo", tt.limit)
			if err != nil {
				t.Fatalf("ListReleases() error: %v", err)
			}
			if len(got) != tt.wantCount {
				t.Errorf("ListReleases() returned %d releases, want %d", len(got), tt.wantCount)
			}
			if len(got) > 0 && got[0].TagName != all[0].TagName {
				t.Errorf("ListReleases()[0] = %q, want newest %q", got[0].TagName, all[0].TagName)
			}
			if !reflect.DeepEqual(pages, tt.wantPages) {
				t.Errorf("requested pages %v, want %v", pages, tt.wantPages)
			}
		})
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

func TestClient_Unauthorized(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad credentials", http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL).WithToken("stale-token")
	_, err := client.GetLatestRelease("owner", "repo")
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "authentication failed for github.com") {
		t.Errorf("error %q should name the host and mention authentication", err)
	}
	if !strings.Contains(err.Error(), "gh auth status") {
		t.Errorf("error %q should hint at 'gh auth status'", err)
	}
}

func TestClient_UnauthorizedAnonymous(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "requires authentication", http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewClientWithHTTP(srv.Client(), srv.URL)
	_, err := client.GetLatestRelease("owner", "repo")
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "no token is configured") {
		t.Errorf("error %q should indicate no token is configured", err)
	}
	if !strings.Contains(err.Error(), "GETRELEASE_TOKEN") {
		t.Errorf("error %q should mention GETRELEASE_TOKEN", err)
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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
