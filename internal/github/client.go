package github

import (
	"bytes"
	"cmp"
	"context"
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
	defaultBaseURL           = "https://api.github.com"
	defaultTimeout           = 30 * time.Second
	acceptHeader             = "application/vnd.github+json"
	apiVersionHeader         = "2022-11-28"
	maxDownloadErrorBodySize = 64 << 10

	// defaultDownloadStallTimeout aborts an asset download that receives no
	// data for this long. Downloads have no overall deadline, since a large
	// asset on a slow link can legitimately take many minutes.
	defaultDownloadStallTimeout = 60 * time.Second
)

// errDownloadStalled is the cancellation cause of a download that stopped
// receiving data.
var errDownloadStalled = errors.New("download stalled")

// Client wraps an HTTP client for GitHub API interactions.
type Client struct {
	httpClient *http.Client
	baseURL    string
	webHost    string
	token      string
	// downloadStallTimeout overrides defaultDownloadStallTimeout when set.
	downloadStallTimeout time.Duration
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
	// Secondary marks a secondary (abuse) rate limit, which applies to bursts
	// of requests rather than the hourly quota.
	Secondary bool
	// Anonymous marks a limit hit by unauthenticated requests, whose hourly
	// quota is 60 rather than 5,000.
	Anonymous bool
}

func (e *RateLimitError) Error() string {
	at := e.ResetAt.Local().Format(time.RFC1123)
	if e.Secondary {
		return fmt.Sprintf("GitHub API secondary rate limit exceeded; retry after %s", at)
	}
	msg := fmt.Sprintf("GitHub API rate limit exceeded; resets at %s", at)
	if e.Anonymous {
		msg += "; unauthenticated requests are limited to 60/hour, set GETRELEASE_TOKEN, GH_TOKEN, or GITHUB_TOKEN, or run 'gh auth login', for 5,000/hour"
	}
	return msg
}

// rateLimitError returns a RateLimitError when a 403 or 429 response reports
// an exceeded rate limit, following GitHub's guidance: honor Retry-After when
// present, else X-RateLimit-Reset when no requests remain, else wait at least
// a minute for a secondary limit. It returns nil for other 403 responses.
func (c *Client) rateLimitError(resp *http.Response, body []byte, now time.Time) *RateLimitError {
	anonymous := c.token == ""
	if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
		return &RateLimitError{ResetAt: now.Add(time.Duration(seconds) * time.Second), Secondary: true, Anonymous: anonymous}
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		resetUnix, _ := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
		return &RateLimitError{ResetAt: time.Unix(resetUnix, 0), Anonymous: anonymous}
	}
	if resp.StatusCode == http.StatusTooManyRequests || bytes.Contains(bytes.ToLower(body), []byte("secondary rate limit")) {
		return &RateLimitError{ResetAt: now.Add(time.Minute), Secondary: true, Anonymous: anonymous}
	}
	return nil
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
	case http.StatusForbidden, http.StatusTooManyRequests:
		if rateErr := c.rateLimitError(resp, body, time.Now()); rateErr != nil {
			return nil, rateErr
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

func isNotFound(err error) bool {
	var notFound *NotFoundError
	return errors.As(err, &notFound)
}

// repoNotFound reports a repository that does not exist or that the client's
// credentials cannot see; GitHub answers 404 for both.
func (c *Client) repoNotFound(owner, repo string) error {
	err := &NotFoundError{Resource: fmt.Sprintf("repository %s/%s", owner, repo)}
	if c.token == "" {
		return fmt.Errorf("%w; if it is private, set GETRELEASE_TOKEN, GH_TOKEN, or GITHUB_TOKEN, or run 'gh auth login'", err)
	}
	return fmt.Errorf("%w, or the configured token cannot access it", err)
}

// explainMissingLatest explains a 404 for a repository's latest release: the
// repository is missing or inaccessible, has no published releases, or has
// only prereleases, which GitHub never reports as the latest release.
// Authenticated clients may need to page past drafts to find a published release.
func (c *Client) explainMissingLatest(owner, repo string) error {
	for page := 1; ; page++ {
		releases, err := c.listReleasesPage(owner, repo, maxReleasesPerPage, page)
		if err != nil {
			return err
		}
		for _, r := range releases {
			if !r.Draft {
				return fmt.Errorf("%s/%s has no stable release, only prereleases (newest: %s); request a prerelease by its tag to install it", owner, repo, r.TagName)
			}
		}
		if len(releases) < maxReleasesPerPage {
			return fmt.Errorf("%s/%s has no published releases", owner, repo)
		}
	}
}

// GetLatestRelease fetches the latest published release for a repository.
func (c *Client) GetLatestRelease(owner, repo string) (*Release, error) {
	path := repoReleasePath(owner, repo, "releases", "latest")
	body, err := c.doRequest(path)
	if isNotFound(err) {
		return nil, c.explainMissingLatest(owner, repo)
	}
	if err != nil {
		return nil, fmt.Errorf("fetching latest release for %s/%s: %w", owner, repo, err)
	}

	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("decoding release: %w", err)
	}
	return &release, nil
}

// GetReleaseByTag fetches a specific release by its tag name. Version tags
// are written both with and without a leading "v", so when tag is not found
// the other form is tried; the returned release's TagName is the tag found.
func (c *Client) GetReleaseByTag(owner, repo, tag string) (*Release, error) {
	release, err := c.getReleaseByTag(owner, repo, tag)
	if !isNotFound(err) {
		return release, err
	}
	alternate := alternateTag(tag)
	if alternate != "" {
		release, err := c.getReleaseByTag(owner, repo, alternate)
		if !isNotFound(err) {
			return release, err
		}
	}

	// Distinguish a missing tag from a missing repository.
	if _, listErr := c.ListReleases(owner, repo, 1); listErr != nil {
		return nil, listErr
	}
	notFound := &NotFoundError{Resource: fmt.Sprintf("release %s in %s/%s", tag, owner, repo)}
	if alternate != "" {
		return nil, fmt.Errorf("%w (also tried %s)", notFound, alternate)
	}
	return nil, notFound
}

// getReleaseByTag fetches the release for exactly tag, returning a bare
// *NotFoundError when there is none.
func (c *Client) getReleaseByTag(owner, repo, tag string) (*Release, error) {
	path := repoReleasePath(owner, repo, "releases", "tags", tag)
	body, err := c.doRequest(path)
	if isNotFound(err) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("fetching release %s for %s/%s: %w", tag, owner, repo, err)
	}

	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("decoding release: %w", err)
	}
	return &release, nil
}

// alternateTag returns the other spelling of a version tag: without its
// leading "v" ("v1.2.3" -> "1.2.3") or with one ("1.2.3" -> "v1.2.3"). It
// returns "" for tags that do not start with a version number.
func alternateTag(tag string) string {
	isDigit := func(b byte) bool { return b >= '0' && b <= '9' }
	switch {
	case len(tag) > 1 && (tag[0] == 'v' || tag[0] == 'V') && isDigit(tag[1]):
		return tag[1:]
	case len(tag) > 0 && isDigit(tag[0]):
		return "v" + tag
	default:
		return ""
	}
}

// maxReleasesPerPage is the largest page size the GitHub releases API serves.
const maxReleasesPerPage = 100

// ListReleases fetches up to limit releases for a repository, ordered by most
// recent first, requesting further pages of up to 100 releases as needed.
func (c *Client) ListReleases(owner, repo string, limit int) ([]Release, error) {
	if limit <= 0 {
		limit = 30
	}
	perPage := min(limit, maxReleasesPerPage)

	var releases []Release
	for page := 1; len(releases) < limit; page++ {
		batch, err := c.listReleasesPage(owner, repo, perPage, page)
		if err != nil {
			return nil, err
		}
		releases = append(releases, batch...)
		if len(batch) < perPage {
			break // last page
		}
	}

	if len(releases) > limit {
		releases = releases[:limit]
	}
	return releases, nil
}

func (c *Client) listReleasesPage(owner, repo string, perPage, page int) ([]Release, error) {
	path := fmt.Sprintf("%s?per_page=%d&page=%d", repoReleasePath(owner, repo, "releases"), perPage, page)
	body, err := c.doRequest(path)
	if isNotFound(err) {
		return nil, c.repoNotFound(owner, repo)
	}
	if err != nil {
		return nil, fmt.Errorf("listing releases for %s/%s: %w", owner, repo, err)
	}

	var releases []Release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("decoding releases: %w", err)
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
//
// The client's overall request timeout does not apply: the download is
// instead aborted when no data arrives for the stall timeout, so slow but
// progressing downloads of large assets complete. A partially written file
// is removed on failure. Rate limits are detected from response headers or a
// bounded error-body preview.
func (c *Client) DownloadAsset(asset Asset, destPath string) (n int64, err error) {
	downloadURL := asset.DownloadURL
	if c.token != "" && asset.APIURL != "" && c.isTrustedDownloadHost(asset.APIURL) {
		downloadURL = asset.APIURL
	}

	stallTimeout := cmp.Or(c.downloadStallTimeout, defaultDownloadStallTimeout)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	stall := time.AfterFunc(stallTimeout, func() {
		cancel(fmt.Errorf("%w: no data received for %s", errDownloadStalled, stallTimeout))
	})
	defer stall.Stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return 0, fmt.Errorf("creating download request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	if c.token != "" && c.isTrustedDownloadHost(downloadURL) {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	httpClient := *c.httpClient
	httpClient.Timeout = 0
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("downloading asset: %w", downloadError(ctx, err))
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing download response body: %w", closeErr))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			if rateErr := c.rateLimitError(resp, nil, time.Now()); rateErr != nil {
				return 0, rateErr
			}
			body, readErr := io.ReadAll(io.LimitReader(&stallResetReader{r: resp.Body, stall: stall, timeout: stallTimeout}, maxDownloadErrorBodySize))
			if readErr != nil {
				return 0, fmt.Errorf("reading download error response body: %w", downloadError(ctx, readErr))
			}
			if rateErr := c.rateLimitError(resp, body, time.Now()); rateErr != nil {
				return 0, rateErr
			}
		}
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
		if err != nil {
			if removeErr := os.Remove(destPath); removeErr != nil {
				err = errors.Join(err, fmt.Errorf("removing partial download %s: %w", destPath, removeErr))
			}
		}
	}()

	n, err = io.Copy(out, &stallResetReader{r: resp.Body, stall: stall, timeout: stallTimeout})
	if err != nil {
		return n, fmt.Errorf("writing asset to disk: %w", downloadError(ctx, err))
	}
	return n, nil
}

// stallResetReader restarts the stall timer whenever data is read.
type stallResetReader struct {
	r       io.Reader
	stall   *time.Timer
	timeout time.Duration
}

func (s *stallResetReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.stall.Reset(s.timeout)
	}
	return n, err
}

// downloadError reports a stall as the cause instead of the generic
// "context canceled" the HTTP client returns.
func downloadError(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); errors.Is(cause, errDownloadStalled) {
		return cause
	}
	return err
}
