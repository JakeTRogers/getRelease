package github

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ParseRepoURL extracts the owner, repo name, and GitHub host from a GitHub
// URL. The host is taken from the URL itself and normalized via
// NormalizeHost, so github.com and *.ghe.com URLs are accepted and any other
// host is rejected. Accepts formats like:
//   - https://github.com/owner/repo
//   - https://github.com/owner/repo.git
//   - https://github.com/owner/repo/
//   - github.com/owner/repo
//   - https://acme.ghe.com/owner/repo
//   - git@github.com:owner/repo.git (git's scp-like SSH syntax)
//   - ssh://git@github.com/owner/repo.git
func ParseRepoURL(rawURL string) (owner, repo, host string, err error) {
	if rawURL == "" {
		return "", "", "", errors.New("empty URL")
	}

	// Rewrite scp-like SSH syntax as a URL, and add a scheme if missing, so
	// url.Parse works correctly.
	normalized := rawURL
	if scpHost, scpPath, ok := splitSCPLikeURL(rawURL); ok {
		normalized = "https://" + scpHost + "/" + scpPath
	} else if !strings.Contains(normalized, "://") {
		normalized = "https://" + normalized
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", "", "", fmt.Errorf("parsing URL %q: %w", rawURL, err)
	}

	if parsed.Hostname() == "" {
		return "", "", "", fmt.Errorf("not a GitHub URL: %q", rawURL)
	}
	host, err = NormalizeHost(parsed.Hostname())
	if err != nil {
		return "", "", "", fmt.Errorf("not a supported GitHub URL %q: %w", rawURL, err)
	}

	// Path should be /owner/repo with optional trailing segments
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", fmt.Errorf("cannot extract owner/repo from URL %q", rawURL)
	}

	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")

	return owner, repo, host, nil
}

// splitSCPLikeURL splits git's scp-like SSH syntax, [user@]host:path, into
// host and path. As in git, it applies when there is no scheme and a colon
// comes before the first slash; a colon followed by digits and a slash is
// taken as a port instead ("github.com:443/owner/repo").
func splitSCPLikeURL(raw string) (host, path string, ok bool) {
	if strings.Contains(raw, "://") {
		return "", "", false
	}
	hostPart, path, found := strings.Cut(raw, ":")
	if !found || strings.Contains(hostPart, "/") {
		return "", "", false
	}
	if port, _, hasSlash := strings.Cut(path, "/"); hasSlash && port != "" && strings.Trim(port, "0123456789") == "" {
		return "", "", false
	}
	if _, afterUser, hasUser := strings.Cut(hostPart, "@"); hasUser {
		hostPart = afterUser
	}
	return hostPart, path, hostPart != ""
}
