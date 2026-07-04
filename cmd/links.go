package cmd

import (
	"fmt"
	"net/url"

	"github.com/JakeTRogers/getRelease/internal/github"
)

// githubWebBaseURL returns the web base URL for the given GitHub host; an
// empty host means github.com.
func githubWebBaseURL(host string) string {
	if host == "" {
		host = github.DefaultHost
	}
	return "https://" + host
}

func githubRepoReleasesURL(host, owner, repo string) string {
	return fmt.Sprintf("%s/%s/%s/releases", githubWebBaseURL(host), owner, repo)
}

func githubReleasePageURL(host, owner, repo string, release *github.Release) string {
	if release == nil {
		return githubRepoReleasesURL(host, owner, repo)
	}
	if release.HTMLURL != "" {
		return release.HTMLURL
	}
	if release.TagName == "" {
		return githubRepoReleasesURL(host, owner, repo)
	}
	return fmt.Sprintf("%s/%s/%s/releases/tag/%s", githubWebBaseURL(host), owner, repo, url.PathEscape(release.TagName))
}
