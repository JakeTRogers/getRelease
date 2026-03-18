package cmd

import (
	"fmt"
	"net/url"

	"github.com/JakeTRogers/getRelease/internal/github"
)

const githubWebBaseURL = "https://github.com"

func githubRepoReleasesURL(owner, repo string) string {
	return fmt.Sprintf("%s/%s/%s/releases", githubWebBaseURL, owner, repo)
}

func githubReleasePageURL(owner, repo string, release *github.Release) string {
	if release == nil {
		return githubRepoReleasesURL(owner, repo)
	}
	if release.HTMLURL != "" {
		return release.HTMLURL
	}
	if release.TagName == "" {
		return githubRepoReleasesURL(owner, repo)
	}
	return fmt.Sprintf("%s/%s/%s/releases/tag/%s", githubWebBaseURL, owner, repo, url.PathEscape(release.TagName))
}
