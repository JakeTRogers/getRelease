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

// RepoRef is a repository named by a single command-line argument.
type RepoRef struct {
	Owner string
	Repo  string
	Host  string // empty when the argument names no host
	Tag   string // empty when the argument names no tag
}

// ParseRepoRef parses a repository argument in one of these forms:
//   - owner/repo[@tag], naming no host
//   - host/owner/repo[@tag], such as acme.ghe.com/owner/repo@v1.0.0
//   - any repository URL accepted by ParseRepoURL, without a tag
//
// The tag follows the first '@', since owner and repository names cannot
// contain one, so the tag may itself contain '@' or '/' (pkg@1.2.0,
// release/1.0). An '@' in a URL with a scheme or in git's scp-like syntax is
// part of the URL instead, so those forms take no tag.
func ParseRepoRef(s string) (RepoRef, error) {
	// Every accepted form has an owner/repo path.
	if !strings.Contains(s, "/") {
		return RepoRef{}, invalidRepoRefError(s)
	}
	before, tag, hasTag := strings.Cut(s, "@")
	if strings.Contains(before, ":") || !strings.Contains(before, "/") {
		owner, repo, host, err := ParseRepoURL(s)
		if err != nil {
			return RepoRef{}, err
		}
		if strings.Contains(repo, "@") {
			return RepoRef{}, fmt.Errorf("invalid repository %q: a tag cannot follow a URL with a scheme; use --tag, or write it as host/owner/repo@tag", s)
		}
		return RepoRef{Owner: owner, Repo: repo, Host: host}, nil
	}
	if hasTag && tag == "" {
		return RepoRef{}, fmt.Errorf("invalid repository %q: missing tag after @", s)
	}

	if strings.Count(before, "/") > 1 {
		owner, repo, host, err := ParseRepoURL(before)
		if err != nil {
			return RepoRef{}, err
		}
		return RepoRef{Owner: owner, Repo: repo, Host: host, Tag: tag}, nil
	}

	owner, repo, _ := strings.Cut(before, "/")
	if owner == "" || repo == "" {
		return RepoRef{}, invalidRepoRefError(s)
	}
	return RepoRef{Owner: owner, Repo: repo, Tag: tag}, nil
}

func invalidRepoRefError(s string) error {
	return fmt.Errorf("invalid repository %q: use [host/]owner/repo[@tag] or a repository URL", s)
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
