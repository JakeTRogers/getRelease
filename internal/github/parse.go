package github

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ParseRepoURL extracts the owner and repo name from a GitHub URL.
// Accepts formats like:
//   - https://github.com/owner/repo
//   - https://github.com/owner/repo.git
//   - https://github.com/owner/repo/
//   - github.com/owner/repo
//   - http://github.com/owner/repo
func ParseRepoURL(rawURL string) (owner, repo string, err error) {
	if rawURL == "" {
		return "", "", errors.New("empty URL")
	}

	// Add scheme if missing so url.Parse works correctly
	normalized := rawURL
	if !strings.Contains(normalized, "://") {
		normalized = "https://" + normalized
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", "", fmt.Errorf("parsing URL %q: %w", rawURL, err)
	}

	host := strings.ToLower(parsed.Hostname())
	if host != "github.com" && host != "www.github.com" {
		return "", "", fmt.Errorf("not a GitHub URL: %q", rawURL)
	}

	// Path should be /owner/repo with optional trailing segments
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("cannot extract owner/repo from URL %q", rawURL)
	}

	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")

	return owner, repo, nil
}
