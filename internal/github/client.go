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
	"strings"
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
	webHost    string
	token      string
}

// NewClient creates a new GitHub API client targeting github.com.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    defaultBaseURL,
		webHost:    DefaultHost,
	}
}

// NewClientForHost creates a client targeting the given GitHub host, which
// must already be normalized by NormalizeHost: either "github.com" or a
// *.ghe.com host (GitHub Enterprise Cloud with data residency). For
// *.ghe.com hosts, the REST API is served from api.<host> rather than
// api.github.com.
func NewClientForHost(host string) *Client {
	if host == "" || host == DefaultHost {
		return NewClient()
	}
	return &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    "https://api." + host,
		webHost:    host,
	}
}

// WithToken sets the token used to authenticate API requests and returns the
// client for chaining. An empty token leaves the client anonymous. The token
// is also used for asset downloads from the client's configured GitHub
// host, which is required for private-repo downloads; Go's http.Client
// strips the Authorization header on the redirect to the actual storage
// host.
func (c *Client) WithToken(token string) *Client {
	c.token = token
	return c
}

// isTrustedDownloadHost reports whether rawURL points at this client's
// configured GitHub web or API host, the only hosts release asset downloads
// may carry the token to.
func (c *Client) isTrustedDownloadHost(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	webHost := c.webHost
	if webHost == "" {
		webHost = DefaultHost
	}
	if webHost == DefaultHost {
		return host == DefaultHost || host == "www."+DefaultHost || host == "api."+DefaultHost
	}
	return host == webHost || host == "api."+webHost
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
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

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
	case http.StatusUnauthorized:
		host := c.webHost
		if host == "" {
			host = DefaultHost
		}
		if c.token == "" {
			return nil, fmt.Errorf("authentication failed for %s (HTTP 401): no token is configured; set GETRELEASE_TOKEN, GH_TOKEN, or GITHUB_TOKEN, or run 'gh auth login --hostname %s'", host, host)
		}
		return nil, fmt.Errorf("authentication failed for %s (HTTP 401): the token was rejected; check 'gh auth status --hostname %s' or the token configured via GETRELEASE_TOKEN, GH_TOKEN, or GITHUB_TOKEN", host, host)
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

// DownloadAsset downloads a release asset to destPath, writing directly to
// disk, following GitHub's redirect to the actual file URL. Anonymous
// downloads use the asset's browser download URL. When a token is set, the
// asset's API URL is preferred with the token attached, since private-repo
// assets are only served through the API; GitHub redirects to a signed
// storage URL and Go's http.Client strips the Authorization header on that
// cross-host redirect.
func (c *Client) DownloadAsset(asset Asset, destPath string) (n int64, err error) {
	downloadURL := asset.DownloadURL
	if c.token != "" && asset.APIURL != "" && c.isTrustedDownloadHost(asset.APIURL) {
		downloadURL = asset.APIURL
	}

	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return 0, fmt.Errorf("creating download request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	if c.token != "" && c.isTrustedDownloadHost(downloadURL) {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

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
