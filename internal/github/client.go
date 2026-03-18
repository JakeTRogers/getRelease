package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

const (
	defaultBaseURL   = "https://api.github.com"
	defaultTimeout   = 30 * time.Second
	acceptHeader     = "application/vnd.github+json"
	apiVersionHeader = "2022-11-28"
)

// Client wraps an HTTP client for GitHub API interactions.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new GitHub API client with sensible defaults.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    defaultBaseURL,
	}
}

// NewClientWithHTTP creates a client with a custom http.Client (useful for testing).
func NewClientWithHTTP(httpClient *http.Client, baseURL string) *Client {
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

// RateLimitError is returned when the GitHub API rate limit is exceeded.
type RateLimitError struct {
	ResetAt time.Time
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("GitHub API rate limit exceeded; resets at %s", e.ResetAt.Local().Format(time.RFC1123))
}

// NotFoundError is returned when the requested resource is not found.
type NotFoundError struct {
	Resource string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s not found", e.Resource)
}

func (c *Client) doRequest(path string) (body []byte, err error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept", acceptHeader)
	req.Header.Set("X-GitHub-Api-Version", apiVersionHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing response body: %w", closeErr))
		}
	}()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusNotFound:
		return nil, &NotFoundError{Resource: path}
	case http.StatusForbidden:
		remaining := resp.Header.Get("X-RateLimit-Remaining")
		if remaining == "0" {
			resetUnix, _ := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
			return nil, &RateLimitError{ResetAt: time.Unix(resetUnix, 0)}
		}
		return nil, fmt.Errorf("forbidden: %s", string(body))
	default:
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
}

func repoReleasePath(owner, repo string, parts ...string) string {
	escaped := make([]string, 0, len(parts)+4)
	escaped = append(escaped, "", "repos", url.PathEscape(owner), url.PathEscape(repo))
	for _, part := range parts {
		escaped = append(escaped, url.PathEscape(part))
	}
	return joinURLPath(escaped)
}

func joinURLPath(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	path := parts[0]
	for _, part := range parts[1:] {
		if path == "" {
			path = "/" + part
			continue
		}
		path += "/" + part
	}
	return path
}

// GetLatestRelease fetches the latest published release for a repository.
func (c *Client) GetLatestRelease(owner, repo string) (*Release, error) {
	path := repoReleasePath(owner, repo, "releases", "latest")
	body, err := c.doRequest(path)
	if err != nil {
		return nil, fmt.Errorf("fetching latest release for %s/%s: %w", owner, repo, err)
	}

	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("decoding release: %w", err)
	}
	return &release, nil
}

// GetReleaseByTag fetches a specific release by its tag name.
func (c *Client) GetReleaseByTag(owner, repo, tag string) (*Release, error) {
	path := repoReleasePath(owner, repo, "releases", "tags", tag)
	body, err := c.doRequest(path)
	if err != nil {
		return nil, fmt.Errorf("fetching release %s for %s/%s: %w", tag, owner, repo, err)
	}

	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("decoding release: %w", err)
	}
	return &release, nil
}

// ListReleases fetches up to limit releases for a repository, ordered by most recent first.
func (c *Client) ListReleases(owner, repo string, limit int) ([]Release, error) {
	if limit <= 0 {
		limit = 30
	}

	perPage := limit
	if perPage > 100 {
		perPage = 100
	}

	path := fmt.Sprintf("%s?per_page=%d", repoReleasePath(owner, repo, "releases"), perPage)
	body, err := c.doRequest(path)
	if err != nil {
		return nil, fmt.Errorf("listing releases for %s/%s: %w", owner, repo, err)
	}

	var releases []Release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("decoding releases: %w", err)
	}

	if len(releases) > limit {
		releases = releases[:limit]
	}
	return releases, nil
}

// DownloadAsset downloads a release asset to destPath, writing directly to disk.
// It follows GitHub's redirect to the actual file URL.
func (c *Client) DownloadAsset(downloadURL, destPath string) (n int64, err error) {
	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return 0, fmt.Errorf("creating download request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("downloading asset: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing download response body: %w", closeErr))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return 0, fmt.Errorf("creating file %s: %w", destPath, err)
	}
	defer func() {
		if closeErr := out.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing destination file %s: %w", destPath, closeErr))
		}
	}()

	n, err = io.Copy(out, resp.Body)
	if err != nil {
		return n, fmt.Errorf("writing asset to disk: %w", err)
	}
	return n, nil
}
